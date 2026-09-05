// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Две колонки под DNS-записи NIC уходят из схемы (0023).
//
// Они стояли в таблице с самой первой миграции и не имели ни читателя, ни писателя:
// во всём дереве их имена встречались только в объявлении 0001 и в двух док-местах.
// Поддерево DNS только что снято с контракта — на входе оно было недостижимо, а на
// выходе доезжало до клиента ВСЕГДА ПУСТЫМ, и отличить «записей нет» от «поле не
// реализовано» было нельзя. Колонки под ним — последнее, что от него осталось.
//
// Почему это проверяется отдельным тестом, а не общим drop-guard'ом сервиса:
// dropguard читает `DROP TABLE`, а здесь уходят КОЛОНКИ. Гейт их не видит, поэтому
// измерение «сколько строк уничтожает этот drop» обязано приехать сюда — иначе
// колонки уходили бы ровно на том основании, из-за которого dropguard и появился:
// на силе абзаца. Ниже это отдельный тест, и он называет число.
const nicTable = "instance_network_interfaces"

// dropVersionNICDNS — версия, снимающая колонки; на version-1 они ещё считаемы.
const dropVersionNICDNS int64 = 23

// withdrawnNICDNSColumns — то, что уходит. Ровно это, и ничего кроме.
var withdrawnNICDNSColumns = []string{"primary_v4_dns_records", "primary_v6_dns_records"}

// nicColumnsAfterDrop — полный состав таблицы ПОСЛЕ снятия (0001 + 0002 + 0005).
//
// Список полный, а не «те, что интересны»: он ловит drop, забравший лишнее. Проверка
// «двух колонок нет» одна этого не поймает — таблица без половины колонок ей тоже
// зелёная.
var nicColumnsAfterDrop = []string{
	"instance_id",
	"idx",
	"mac_address",
	"subnet_id",
	"primary_v4_address",
	"primary_v4_address_id", // 0002
	"primary_v4_nat",
	"primary_v6_address",
	"primary_v6_nat",
	"security_group_ids",
	"nic_id", // 0005
}

// TestIntegration_DropNICDNSColumns_GoneAtHeadAndRepoStillWorks — предмет миграции.
//
// Утверждается И отсутствие двух колонок на head, И то, что таблица потеряла ТОЛЬКО
// их, И то, что существующий репозиторий продолжает работать поверх изменённой схемы.
// Последнее не формальность: `InstanceRepo.Delete` не удаляет строки NIC сам, он
// полагается на FK CASCADE от `instances` — поэтому каскад проверяется на живой
// строке, а не по описанию.
func TestIntegration_DropNICDNSColumns_GoneAtHeadAndRepoStillWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()

	// setupTestDB гонит цепочку до head и сеет каталог machine_types, на который
	// ссылается FK instances.machine_type_id.
	dsn := setupTestDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// 1. Двух колонок на head нет.
	for _, col := range withdrawnNICDNSColumns {
		require.Falsef(t, nicColumnExists(t, db, col),
			"%s.%s обязана уйти на head: у неё нет ни читателя, ни писателя, а DNS снят с контракта", nicTable, col)
	}

	// 2. Ушли ТОЛЬКО они.
	got := nicColumnSet(t, db)
	want := append([]string(nil), nicColumnsAfterDrop...)
	sort.Strings(want)
	require.Equal(t, want, got,
		"состав %s разошёлся с ожидаемым: миграция обязана снять две колонки и не тронуть ни одной другой", nicTable)

	// 3. Существующий репозиторий работает поверх изменённой схемы.
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	instRepo := repo.NewInstanceRepo(pool)

	inID := ids.NewID(ids.PrefixInstance)
	in := &domain.Instance{
		ID: inID, ProjectID: "proj-dns-drop", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: "vm-dns-drop", ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
		FQDN: inID + ".auto.internal", InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	}
	created, _, err := instRepo.Insert(ctx, in)
	require.NoError(t, err, "Insert обязан работать после снятия колонок")
	require.Equal(t, inID, created.ID)

	fetched, err := instRepo.Get(ctx, inID)
	require.NoError(t, err, "Get обязан работать после снятия колонок")
	require.Equal(t, inID, fetched.ID)

	// 4. Таблица NIC остаётся рабочей: строка пишется без снятых колонок.
	//
	// Пишется сырым SQL сознательно — у таблицы НЕТ Go-писателя (NIC материализуются
	// launch-сагой более поздней фазы), поэтому «репозиторий пишет NIC» проверить
	// нечем. Проверяется то, что есть: схема принимает строку, и каскад её убирает.
	insertNICRow(t, db, inID, "0")
	require.Equal(t, 1, nicRowCount(t, db), "строка NIC обязана записаться в таблицу без снятых колонок")

	// 5. FK CASCADE, на который опирается Delete, держится.
	require.NoError(t, instRepo.Delete(ctx, inID), "Delete обязан работать после снятия колонок")
	require.Equal(t, 0, nicRowCount(t, db),
		"FK CASCADE instance_network_interfaces.instance_id → instances обязан снять строку NIC вместе с инстансом")
}

// TestIntegration_DropNICDNSColumns_DownRestoresThemForReal — обратная сторона.
//
// Обратная сторона обязана быть настоящей: колонки возвращаются в ТОМ ЖЕ типе, с тем
// же умолчанием и той же обязательностью. Объявления в information_schema для этого
// недостаточно — умолчание, которое объявлено, но не материализуется, отличить от
// рабочего нельзя, поэтому строка пишется БЕЗ этих колонок и значение читается.
func TestIntegration_DropNICDNSColumns_DownRestoresThemForReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dsn := setupTestDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Откат ровно на версию перед снятием.
	require.NoError(t, goose.DownTo(db, ".", dropVersionNICDNS-1))

	for _, col := range withdrawnNICDNSColumns {
		require.Truef(t, nicColumnExists(t, db, col), "Down обязан вернуть %s.%s", nicTable, col)

		typ, nullable, def := nicColumnDef(t, db, col)
		require.Equalf(t, "jsonb", typ, "%s обязана вернуться тем же типом", col)
		require.Equalf(t, "NO", nullable, "%s обязана вернуться NOT NULL", col)
		require.Containsf(t, def, "[]", "%s обязана вернуться с тем же умолчанием '[]'::jsonb, получено %q", col, def)
	}

	// Умолчание материализуется, а не только объявлено: строка пишется без этих
	// колонок, значения читаются.
	inID := seedInstanceRow(t, dsn)
	insertNICRow(t, db, inID, "0")
	for _, col := range withdrawnNICDNSColumns {
		var val string
		require.NoError(t, db.QueryRow(
			`SELECT `+col+`::text FROM `+nicTable+` WHERE instance_id = $1`, inID).Scan(&val))
		require.Equalf(t, "[]", val, "умолчание %s обязано материализоваться, а не только объявляться", col)
	}

	// Круг замыкается: повторный Up снова их снимает.
	require.NoError(t, goose.Up(db, "."))
	for _, col := range withdrawnNICDNSColumns {
		require.Falsef(t, nicColumnExists(t, db, col), "повторный Up обязан снова снять %s.%s", nicTable, col)
	}
}

// TestIntegration_DropNICDNSColumns_DestroysNoRows — измерение, которого drop-guard
// для колонок не делает.
//
// Гейт сервиса считает строки перед КАЖДЫМ `DROP TABLE`, потому что «ничего там не
// было» — утверждение о данных, и оно перестаёт быть верным молча, когда строки уже
// уничтожены. Снятие колонки уничтожает данные так же, а гейт его не читает. Поэтому
// счёт делается здесь: на версии, где колонки ещё существуют, и с числом в сообщении.
func TestIntegration_DropNICDNSColumns_DestroysNoRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dsn := setupTestDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Назад на версию, где снимаемые колонки ещё на месте и потому считаемы.
	require.NoError(t, goose.DownTo(db, ".", dropVersionNICDNS-1))
	for _, col := range withdrawnNICDNSColumns {
		require.Truef(t, nicColumnExists(t, db, col),
			"на версии %d колонка %s обязана существовать — иначе считать нечего и измерение фиктивно",
			dropVersionNICDNS-1, col)
	}

	// Сколько строк вообще есть на базе, которую построила эта цепочка.
	total := nicRowCount(t, db)
	require.Equalf(t, 0, total,
		"цепочка не сеет ни одной строки NIC; найдено %d — значит у таблицы появился писатель, и снятие колонок уничтожит данные",
		total)

	// И сколько из них несут в снимаемых колонках хоть что-то, кроме умолчания.
	for _, col := range withdrawnNICDNSColumns {
		nonDefault := nicNonDefaultCount(t, db, col)
		require.Equalf(t, 0, nonDefault,
			"%s несёт значение в %d строк(ах) — снятие колонки уничтожило бы их, drop не проходит", col, nonDefault)
	}
}

// TestIntegration_DropNICDNSColumns_MeasurementRefusesRowsItFinds — инъекция.
//
// Тест выше зелёный и до снятия колонок, и после: он не детектор изменения, он
// измерение. Значит «он прошёл» само по себе не значит ничего — ровно до тех пор,
// пока не показано, что он УМЕЕТ отказать. Здесь в снимаемую колонку кладётся
// значение на той версии, где колонка ещё есть, и то же самое измерение обязано
// увидеть его и назвать число.
//
// Обратная сторона инъекции — умолчание: строка, записанная без этих колонок,
// измерением НЕ считается. Без этой половины гейт ловил бы форму (есть строка), а не
// существо (в колонке что-то лежит), и первая же строка NIC сделала бы drop невозможным.
func TestIntegration_DropNICDNSColumns_MeasurementRefusesRowsItFinds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	dsn := setupTestDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, goose.DownTo(db, ".", dropVersionNICDNS-1))
	col := withdrawnNICDNSColumns[0]
	require.True(t, nicColumnExists(t, db, col), "инъекция бессмысленна, если колонки нет")

	// Половина «молчит»: строка без этих колонок — умолчание, не данные.
	quiet := seedInstanceRow(t, dsn)
	insertNICRow(t, db, quiet, "0")
	require.Equal(t, 0, nicNonDefaultCount(t, db, col),
		"строка с умолчанием не данные: измерение обязано её пропустить, иначе оно запрещает любой drop")

	// Половина «краснеет»: значение в колонке обязано быть увидено и посчитано.
	loud := seedInstanceRow(t, dsn)
	insertNICRow(t, db, loud, "0")
	_, err = db.Exec(
		`UPDATE `+nicTable+` SET `+col+` = '[{"fqdn":"injected.example."}]'::jsonb WHERE instance_id = $1`, loud)
	require.NoError(t, err)

	require.Equal(t, 1, nicNonDefaultCount(t, db, col),
		"измерение не увидело значение, положенное в колонку — тогда его зелёный ничего не утверждает")
}

// ---- helpers (имена намеренно с префиксом nic: пакет общий на все repo-тесты) ----

// nicNonDefaultCount — то самое измерение, вынесенное так, чтобы гейт и его инъекция
// считали ОДНИМ предикатом. Два разных выражения разошлись бы, и доказательство
// относилось бы не к тому, что стоит на пути drop'а.
func nicNonDefaultCount(t *testing.T, db *sql.DB, col string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM `+nicTable+` WHERE `+col+` <> '[]'::jsonb`).Scan(&n))
	return n
}

func nicColumnExists(t *testing.T, db *sql.DB, col string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`,
		nicTable, col).Scan(&n))
	return n > 0
}

func nicColumnSet(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(
		`SELECT column_name FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 ORDER BY column_name`, nicTable)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		out = append(out, c)
	}
	require.NoError(t, rows.Err())
	require.NotEmptyf(t, out, "таблица %s не прочитана — пустой состав это не чистый результат", nicTable)
	return out
}

func nicColumnDef(t *testing.T, db *sql.DB, col string) (dataType, isNullable, colDefault string) {
	t.Helper()
	var def sql.NullString
	require.NoError(t, db.QueryRow(
		`SELECT data_type, is_nullable, column_default FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`,
		nicTable, col).Scan(&dataType, &isNullable, &def))
	return dataType, isNullable, def.String
}

func nicRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+nicTable).Scan(&n))
	return n
}

// insertNICRow пишет строку NIC, НЕ называя снимаемые колонки: так выглядит любой
// будущий писатель, и так проверяется, что схема его принимает.
func insertNICRow(t *testing.T, db *sql.DB, instanceID, idx string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO `+nicTable+` (instance_id, idx, mac_address, subnet_id, primary_v4_address, nic_id)
		 VALUES ($1, $2, '02:00:00:00:00:01', 'subnet-x', '10.0.0.5', 'nic-x')`,
		instanceID, idx)
	require.NoError(t, err)
}

// seedInstanceRow заводит инстанс сырым SQL — на откатанной версии Go-репозиторий
// пришлось бы согласовывать с той схемой, а нужна лишь строка-родитель для FK.
func seedInstanceRow(t *testing.T, dsn string) string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Имя выводится из id: на instances висит partial UNIQUE(project_id, name)
	// WHERE name<>'', поэтому фиксированное имя ловит 23505 на втором вызове —
	// инъекции нужны две строки в одном проекте.
	id := ids.NewID(ids.PrefixInstance)
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO instances (id, project_id, name, zone_id, status) VALUES ($1, $2, $3, $4, $5)`,
		id, "proj-dns-down", "vm-"+id, "ru-central1-a", "PROVISIONING")
	require.NoError(t, err)
	return id
}
