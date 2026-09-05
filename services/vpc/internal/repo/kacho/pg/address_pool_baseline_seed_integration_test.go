// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Пробы базового посева каталога внешних адресов, которым `make dev-up` делает
// свежий стенд пригодным для внешнего балансировщика. Читают ПОСТАВЛЯЕМЫЙ
// артефакт deploy/scripts/vpc-address-pool-baseline.sql и исполняют его против
// РЕАЛЬНОЙ схемы kacho_vpc: копия SQL в пробе разъехалась бы с посевом молча и
// зеленела бы на сломанном.
//
// ГЛАВНАЯ ИЗ НИХ — TestStandVpcPoolBaselineMatchesTheWriterPath. Посев SQL-ом
// это ВТОРОЙ способ завести пул: первый — путь записи сервиса (Insert +
// InsertCidrBlocks + PopulateFreelistForPool + InitIPv6PoolCursor + outbox).
// Два места об одном предмете расходятся молча, поэтому проба заводит один и тот
// же пул обоими способами и сличает ВСЕ затронутые таблицы. Без неё посев был бы
// самостоятельной реализацией записи, о расхождении которой узнавали бы по
// отказу выделения адреса на стенде.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

const vpcPoolBaselineSQLRel = "deploy/scripts/vpc-address-pool-baseline.sql"

// resolverLanePredicate — ДОСЛОВНО тот предикат, которым резолвер выбирает пул
// зоне-независимой полосы (addressPoolReader.GetDefaultForZone при zoneID == "").
// Проба утверждает пригодность стенда именно им, а не «пул с таким именем есть»:
// пул можно завести и не попасть в полосу.
const resolverLanePredicate = `SELECT count(*) FROM address_pools
	WHERE zone_id IS NULL AND kind = $1 AND is_default = true`

type poolBaseline struct {
	ID, Name, Description, V4, V6 string
	SQL                           string
}

func standSeedRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "не удалось определить путь пробы — читать поставляемый артефакт неоткуда")
	dir := filepath.Dir(thisFile)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "корень репозитория не найден — go.mod не встретился при подъёме")
		dir = parent
	}
	t.Fatal("корень репозитория не найден за 12 уровней")
	return ""
}

// readPoolBaseline читает поставляемый посев и ИЗВЛЕКАЕТ из него личность пула.
// Личность не выписывается в пробе: выписанная копия — третье место об одном
// предмете, и разошлась бы она ровно тогда, когда посев правят.
func readPoolBaseline(t *testing.T) poolBaseline {
	t.Helper()
	path := filepath.Join(standSeedRepoRoot(t), vpcPoolBaselineSQLRel)
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "не прочитан поставляемый посев %s", vpcPoolBaselineSQLRel)
	sql := string(raw)

	head := regexp.MustCompile(`(?s)VALUES\s*\(\s*'([^']+)',\s*'([^']+)',\s*'([^']+)'`).FindStringSubmatch(sql)
	require.Len(t, head, 4,
		"в %s не разобрана строка личности пула (id/имя/описание) — проба перестала читать свой предмет", vpcPoolBaselineSQLRel)
	blocks := regexp.MustCompile(`ARRAY\['([^']+)'\]::text\[\]`).FindAllStringSubmatch(sql, -1)
	require.Len(t, blocks, 2,
		"в %s не разобраны блоки пула (ожидались ровно два: v4 и v6)", vpcPoolBaselineSQLRel)

	b := poolBaseline{ID: head[1], Name: head[2], Description: head[3], V4: blocks[0][1], V6: blocks[1][1], SQL: sql}
	t.Logf("осмотрено: %s (%d байт) — пул %s (%s), блоки v4=%s v6=%s",
		vpcPoolBaselineSQLRel, len(sql), b.ID, b.Name, b.V4, b.V6)
	return b
}

func applyPoolBaseline(t *testing.T, pool *pgxpool.Pool, b poolBaseline) {
	t.Helper()
	_, err := pool.Exec(context.Background(), b.SQL)
	require.NoError(t, err, "посев стенда не исполнился против реальной схемы kacho_vpc")
}

func newBaselinePool(t *testing.T) (*pgxpool.Pool, poolBaseline) {
	t.Helper()
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	return pool, readPoolBaseline(t)
}

func laneCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), resolverLanePredicate,
		int16(domain.AddressPoolKindExternalPublic)).Scan(&n))
	return n
}

// TestStandVpcPoolBaselineOpensTheAnycastLane — то, ради чего шаг существует:
// после посева зоне-независимая полоса НЕ ПУСТА и из её пула есть что выделить.
//
// Отрицание здесь стоит В ПАРЕ с положительным: сначала утверждается, что полоса
// открылась, и только потом — что снятие признака «по умолчанию» её закрывает.
// Это та же инъекция, что была проделана на живом стенде при разборе задачи
// (#607): с признаком — операция завершается за секунды, без него — отказ кода 9
// и настоящий 404 весь бюджет пробы консоли.
func TestStandVpcPoolBaselineOpensTheAnycastLane(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	var before int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM address_pools`).Scan(&before))
	require.Zero(t, before, "каталог пулов обязан стартовать пустым (в миграциях vpc нет ни одного INSERT в address_pools)")
	require.Zero(t, laneCount(t, pool), "полоса аникаста обязана стартовать пустой")

	applyPoolBaseline(t, pool, b)

	require.Equal(t, 1, laneCount(t, pool),
		"после посева резолвер не находит пул зоне-независимой полосы — внешний балансировщик не получит адреса")

	var freeIPs int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM address_pool_free_ips f JOIN address_pools p ON p.id = f.pool_id
		 WHERE p.zone_id IS NULL AND p.kind = $1 AND p.is_default`,
		int16(domain.AddressPoolKindExternalPublic)).Scan(&freeIPs))
	require.Positive(t, freeIPs,
		"пул полосы есть, а выделить из него нечего: список свободных адресов пуст — отказ будет тем же самым")
	t.Logf("полоса аникаста открыта: свободных адресов %d (блок %s)", freeIPs, b.V4)

	// Блоки нормализованы — без них EXCLUDE не защищает, а :addCidrBlocks на
	// таком пуле повёл бы себя как на пустом.
	var cidrs int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM address_pool_cidrs WHERE pool_id = $1`, b.ID).Scan(&cidrs))
	require.Equal(t, 2, cidrs, "нормализованных блоков не два — v4 и v6 объявлены, но записаны не оба")

	// Курсор IPv6 — пул с блоком v6 обслуживается разрежённым счётчиком.
	var next int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT next_offset FROM ipv6_pool_cursors WHERE pool_id = $1`, b.ID).Scan(&next))
	require.EqualValues(t, 1, next, "курсор IPv6 не поставлен — выделение адреса v6 из пула не стартует")

	// ОТРИЦАНИЕ — только теперь, когда положительное доказано: предикат обязан
	// уметь ответить «нет». Проверка, которая не умеет краснеть, проверкой не является.
	_, err := pool.Exec(ctx, `UPDATE address_pools SET is_default = false WHERE id = $1`, b.ID)
	require.NoError(t, err)
	require.Zero(t, laneCount(t, pool),
		"снятие признака «по умолчанию» не закрыло полосу — предикат пригодности не различает состояния")
}

// poolSnapshot — снимок ВСЕХ таблиц, которые затрагивает заведение пула.
// Метки времени исключены: они у двух способов заведения разные by construction,
// и сравнивать их значило бы утверждать про часы, а не про запись.
type poolSnapshot struct {
	Row       string
	Cidrs     string
	FreeCount int
	FreeMin   string
	FreeMax   string
	Cursor    string
	Outbox    string
}

func snapshotPool(t *testing.T, pool *pgxpool.Pool, id string) poolSnapshot {
	t.Helper()
	ctx := context.Background()
	var s poolSnapshot
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT (to_jsonb(t) - 'created_at' - 'modified_at')::text FROM address_pools t WHERE id = $1`,
		id).Scan(&s.Row))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.block::text), '[]'::jsonb)::text
		   FROM address_pool_cidrs c WHERE c.pool_id = $1`, id).Scan(&s.Cidrs))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*), coalesce(min(ip)::text, ''), coalesce(max(ip)::text, '')
		   FROM address_pool_free_ips WHERE pool_id = $1`, id).Scan(&s.FreeCount, &s.FreeMin, &s.FreeMax))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce((SELECT next_offset::text FROM ipv6_pool_cursors WHERE pool_id = $1), '<нет>')`,
		id).Scan(&s.Cursor))
	// `actor` исключён намеренно и ровно один раз: посев стенда НАЗЫВАЕТ себя
	// автором строки, путь записи такого поля не пишет. Непустота actor'а
	// утверждается отдельно — пустой был бы утратой атрибуции.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce(jsonb_agg(jsonb_build_object(
		            'kind', resource_kind, 'event', event_type, 'payload', payload - 'actor')
		        ORDER BY sequence_no), '[]'::jsonb)::text
		   FROM vpc_outbox WHERE resource_id = $1`, id).Scan(&s.Outbox))
	return s
}

// TestStandVpcPoolBaselineMatchesTheWriterPath — посев и путь записи сервиса
// обязаны давать ОДИН И ТОТ ЖЕ пул во всех затронутых таблицах.
//
// Порядок именно такой: сперва пул заводится НАСТОЯЩИМ путём записи и
// снимается снимок, затем база очищается, применяется посев и снимается второй.
// Развести их по двум базам нельзя дёшево, а завести одновременно — нельзя
// вовсе: ограничение исключения по пересечению блоков глобально на kind, то
// есть два пула с одними блоками несовместимы by construction.
func TestStandVpcPoolBaselineMatchesTheWriterPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	// ── способ первый: путь записи сервиса ──────────────────────────────
	r := kachopg.New(pool, nil)
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	p := &domain.AddressPool{
		ID:               b.ID,
		Name:             domain.RcNameVPC(b.Name),
		Description:      domain.RcDescription(b.Description),
		Labels:           domain.LabelsFromMap(nil),
		V4CIDRBlocks:     []string{b.V4},
		V6CIDRBlocks:     []string{b.V6},
		Kind:             domain.AddressPoolKindExternalPublic,
		ZoneID:           "",
		IsDefault:        true,
		SelectorLabels:   domain.LabelsFromMap(nil),
		SelectorPriority: 0,
	}
	require.NoError(t, p.Validate(), "личность из посева не проходит доменную проверку — стенд заводит пул, который сервис отверг бы")

	created, err := w.AddressPools().Insert(ctx, p)
	require.NoError(t, err)
	require.NoError(t, w.AddressPools().InsertCidrBlocks(ctx, created.ID, created.Kind,
		created.V4CIDRBlocks, created.V6CIDRBlocks))
	require.NoError(t, w.AddressPools().PopulateFreelistForPool(ctx, created.ID))
	require.NoError(t, w.Addresses().InitIPv6PoolCursor(ctx, created.ID))
	require.NoError(t, w.Outbox().Emit(ctx, "AddressPool", created.ID, helpers.NoProjectAnchor, "CREATED",
		helpers.AddressPoolDomainPayload(&created.AddressPool)))
	require.NoError(t, w.Commit())

	byWriter := snapshotPool(t, pool, b.ID)
	require.Positive(t, byWriter.FreeCount, "путь записи не материализовал ни одного свободного адреса — сравнивать не с чем")

	// ── очистка: строка пула каскадит блоки, свободные адреса и курсор;
	//    журнал не каскадит (у него нет FK на ресурс) — его снимаем явно ──
	_, err = pool.Exec(ctx, `DELETE FROM address_pools WHERE id = $1`, b.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM vpc_outbox WHERE resource_id = $1`, b.ID)
	require.NoError(t, err)
	require.Zero(t, laneCount(t, pool), "очистка не удалила пул — второй способ писал бы поверх первого")

	// ── способ второй: поставляемый посев ───────────────────────────────
	applyPoolBaseline(t, pool, b)
	bySeed := snapshotPool(t, pool, b.ID)

	require.Equal(t, byWriter.Row, bySeed.Row, "строка пула у посева отличается от строки, которую пишет сервис")
	require.Equal(t, byWriter.Cidrs, bySeed.Cidrs, "нормализованные блоки у посева отличаются от тех, что пишет сервис")
	require.Equal(t, byWriter.FreeCount, bySeed.FreeCount, "число свободных адресов у посева отличается от того, что материализует сервис")
	require.Equal(t, byWriter.FreeMin, bySeed.FreeMin, "первый свободный адрес у посева другой — перечисление блока разошлось с сервисным")
	require.Equal(t, byWriter.FreeMax, bySeed.FreeMax, "последний свободный адрес у посева другой — перечисление блока разошлось с сервисным")
	require.Equal(t, byWriter.Cursor, bySeed.Cursor, "курсор IPv6 у посева отличается от того, что ставит сервис")
	require.Equal(t, byWriter.Outbox, bySeed.Outbox, "audit-строка у посева отличается по форме от той, что пишет репозиторий")

	var actor string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce(payload->>'actor', '') FROM vpc_outbox WHERE resource_id = $1`, b.ID).Scan(&actor))
	require.NotEmpty(t, actor,
		"audit-строка посева не называет автора — пустой actor это утрата атрибуции, а не её отсутствие")
	t.Logf("сличено таблиц: 5 (пул, блоки, свободные адреса, курсор, журнал); свободных адресов %d; автор строки %q",
		bySeed.FreeCount, actor)
}

// TestStandVpcPoolBaselineIsIdempotent — повторный подъём стенда не падает и не
// пишет второй раз. Предикат — ЧИСЛО audit-строк: ресурсных дублей не даст и
// голый ON CONFLICT, а вот audit-строка, отвязанная от RETURNING вставки,
// удвоилась бы, то есть это самый острый наблюдаемый признак повторной записи.
func TestStandVpcPoolBaselineIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	applyPoolBaseline(t, pool, b)
	first := snapshotPool(t, pool, b.ID)

	applyPoolBaseline(t, pool, b)
	applyPoolBaseline(t, pool, b)
	third := snapshotPool(t, pool, b.ID)

	require.Equal(t, first, third, "повторный посев изменил состояние — он не идемпотентен")

	var audit int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM vpc_outbox WHERE resource_id = $1`, b.ID).Scan(&audit))
	require.Equal(t, 1, audit, "повторный посев написал audit-строки заново — журнал объявит создание того, что уже было")
	require.Equal(t, 1, laneCount(t, pool), "после трёх посевов в полосе не ровно один пул")
}

// TestStandVpcPoolBaselineDoesNotTakeAnOccupiedSlot — слот «по умолчанию» для
// (zone_id IS NULL, kind) кластерный синглтон, и у него УЖЕ есть авторы: посев
// набора nlb и посев набора vpc. Посев стенда обязан уступать им, а не отбирать
// слот: на долгоживущем стенде порядок прогонов произволен.
//
// Отрицание в паре с положительным: та же проба утверждает, что на СВОБОДНОМ
// слоте посев всё же пишет — иначе «ничего не изменилось» было бы неотличимо от
// мёртвого посева, и утверждение стало бы тождественно истинным.
func TestStandVpcPoolBaselineDoesNotTakeAnOccupiedSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	// Чужой пул той же полосы, заведённый до подъёма (блоки — заведомо другие,
	// чтобы предметом пробы был именно слот, а не пересечение).
	const foreignID = "aplforeignanycast01"
	_, err := pool.Exec(ctx, `
		INSERT INTO address_pools (id, name, kind, is_default, zone_id, v4_cidr_blocks)
		     VALUES ($1, 'foreign-anycast', 1, true, NULL, ARRAY['100.99.0.0/24'])`, foreignID)
	require.NoError(t, err, "не удалось подготовить занятый слот")

	applyPoolBaseline(t, pool, b)

	var mine int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM address_pools WHERE id = $1`, b.ID).Scan(&mine))
	require.Zero(t, mine, "посев завёл свой пул при занятом слоте — у кластерного синглтона стало два автора")
	require.Equal(t, 1, laneCount(t, pool), "в полосе не ровно один пул — слот раздвоился")

	var stillDefault bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT is_default FROM address_pools WHERE id = $1`, foreignID).Scan(&stillDefault))
	require.True(t, stillDefault, "посев отобрал слот у чужого пула")

	var audit int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM vpc_outbox`).Scan(&audit))
	require.Zero(t, audit, "посев написал audit-строку, ничего не вставив")

	// Положительный контроль: освободили слот — посев пишет. Без него всё
	// перечисленное выше зеленело бы и на пустом файле.
	_, err = pool.Exec(ctx, `DELETE FROM address_pools WHERE id = $1`, foreignID)
	require.NoError(t, err)
	applyPoolBaseline(t, pool, b)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM address_pools WHERE id = $1`, b.ID).Scan(&mine))
	require.Equal(t, 1, mine, "на свободном слоте посев не завёл пул — проба «слот не отобран» была бы вакуумной")
}

// TestStandVpcPoolBaselineNeverLeavesAPoolItCannotAllocateFrom — если блоки уже
// заняты чужим пулом, посев обязан не писать НИЧЕГО.
//
// Почему это отдельная проба, а не следствие предыдущей: ограничение исключения
// по пересечению блоков глобально на kind и НЕ ловится `ON CONFLICT` по слоту.
// Без ограждения посев вставил бы строку пула (слот-то свободен), а её блоки и
// список свободных адресов — нет, и стенд получил бы пул «по умолчанию», из
// которого нельзя выделить ни одного адреса: тот же отказ, только теперь при
// видимом пуле, то есть диагностика стала бы ХУЖЕ, чем без посева вовсе.
func TestStandVpcPoolBaselineNeverLeavesAPoolItCannotAllocateFrom(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	pool, b := newBaselinePool(t)
	ctx := context.Background()

	// Чужой пул БЕЗ признака «по умолчанию» (слот свободен), но с
	// пересекающимся блоком.
	const foreignID = "aplforeignoverlap01"
	_, err := pool.Exec(ctx, `
		INSERT INTO address_pools (id, name, kind, is_default, zone_id, v4_cidr_blocks)
		     VALUES ($1, 'foreign-overlap', 1, false, NULL, ARRAY[$2::text])`, foreignID, b.V4)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO address_pool_cidrs (pool_id, kind, block) VALUES ($1, 1, $2::cidr)`,
		foreignID, b.V4)
	require.NoError(t, err, "не удалось занять блок чужим пулом")

	applyPoolBaseline(t, pool, b)

	var mine int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM address_pools WHERE id = $1`, b.ID).Scan(&mine))
	require.Zero(t, mine,
		"посев завёл пул, блоки которого заняты: он попал бы в полосу «по умолчанию» и не выделил бы ни одного адреса")
	require.Zero(t, laneCount(t, pool), "в полосе появился пул, из которого нечего выделить")
}
