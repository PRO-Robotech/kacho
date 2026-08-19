// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Пробы базового посева каталога размещения geo, которым `make dev-up` делает
// свежий стенд пригодным. Читают ПОСТАВЛЯЕМЫЙ артефакт deploy/scripts/geo-baseline.sql
// и исполняют его против РЕАЛЬНОЙ схемы kacho_geo: копия SQL в пробе разъехалась бы
// с посевом молча и зеленела бы на сломанном.
//
// Вспомогательное (repoRoot/readRepoFile/openZoneIDs) живёт в
// geo_carry_migration_integration_test.go — того же пакета.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// ─── базовый посев стенда: deploy/scripts/geo-baseline.sql ──────────────────

const geoBaselineSQLRel = "deploy/scripts/geo-baseline.sql"

func applyStandBaseline(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	sql := readRepoFile(t, geoBaselineSQLRel)
	require.Contains(t, sql, "INSERT INTO regions", "%s перестал сеять регионы", geoBaselineSQLRel)
	require.Contains(t, sql, "INSERT INTO zones", "%s перестал сеять зоны", geoBaselineSQLRel)
	_, err := pool.Exec(context.Background(), sql)
	require.NoError(t, err, "посев стенда не исполнился против реальной схемы kacho_geo")
}

// TestStandGeoBaselineSeedOpensTheCatalog — то, ради чего шаг существует: после
// посева в каталоге есть зоны, ПРИГОДНЫЕ для размещения. Без шага их ноль, и
// каждое зональное создание отвечает «зона не найдена».
func TestStandGeoBaselineSeedOpensTheCatalog(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	var before int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM zones`).Scan(&before))
	require.Zero(t, before, "каталог обязан стартовать пустым (0001_initial.sql: seed нет)")

	applyStandBaseline(t, pool)

	open := openZoneIDs(t, pool)
	require.NotEmpty(t, open, "после посева нет ни одной зоны, открытой для размещения")
	t.Logf("посеяно зон, открытых для размещения: %d — %v", len(open), open)

	var closed int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM zones z JOIN regions r ON r.id = z.region_id
		  WHERE z.status <> 'UP' OR r.status <> 'UP'`).Scan(&closed))
	require.Zero(t, closed, "посев стенда не должен оставлять закрытых зон — стенд поднимают, чтобы размещать")
}

// TestStandGeoBaselineSeedIsIdempotent — повторный подъём стенда не падает и не
// пишет второй раз. Предикат — ЧИСЛО audit-строк: ресурсных дублей не даст и
// голый ON CONFLICT, а вот audit-строка отвязанная от RETURNING удвоилась бы,
// то есть это самый острый наблюдаемый признак повторной записи.
func TestStandGeoBaselineSeedIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	applyStandBaseline(t, pool)

	var regions1, zones1, audit1 int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&regions1))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM zones`).Scan(&zones1))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM geo_outbox`).Scan(&audit1))
	require.Equal(t, regions1+zones1, audit1,
		"первый посев обязан дать по одной audit-строке CREATED на ресурс (parity с outbox.Emit репозитория)")

	applyStandBaseline(t, pool)
	applyStandBaseline(t, pool)

	var regions2, zones2, audit2 int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&regions2))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM zones`).Scan(&zones2))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM geo_outbox`).Scan(&audit2))

	require.Equal(t, regions1, regions2, "повторный посев завёл регионы заново")
	require.Equal(t, zones1, zones2, "повторный посев завёл зоны заново")
	require.Equal(t, audit1, audit2, "повторный посев написал audit-строки заново — посев не идемпотентен")

	// Форма audit-payload сверяется с той, что пишет репозиторий: kind + event +
	// названный actor. Пустой actor был бы утратой атрибуции (CWE-778).
	var kinds, events, actors int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM geo_outbox WHERE resource_kind IN ('Region','Zone')`).Scan(&kinds))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM geo_outbox WHERE event_type = 'CREATED'`).Scan(&events))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM geo_outbox WHERE coalesce(payload->>'actor','') <> ''`).Scan(&actors))
	require.Equal(t, audit2, kinds)
	require.Equal(t, audit2, events)
	require.Equal(t, audit2, actors)
}

// TestStandGeoBaselineSeedCoversFixtureIds — посев обязан покрывать ИМЕННО те
// идентификаторы, которыми приёмочные фикстуры называют существующие регион и
// зону. Иначе стенд «пригоден», но не для кейсов, ради которых поднят, и
// расхождение двух артефактов остаётся невидимым.
func TestStandGeoBaselineSeedCoversFixtureIds(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	applyStandBaseline(t, pool)

	const fixturesRel = "tests/authz-fixtures/prodseed_matrix.py"
	body := readRepoFile(t, fixturesRel)

	// Ключи читаются из ЖИВОГО файла фикстур, а не выписываются: выписанная
	// копия разъедется молча. Значения — только строковые литералы; ключ,
	// заданный выражением (как existingRegionAltId = ALT_REGION), намеренно не
	// разбирается, и объём осмотренного печатается ниже.
	re := regexp.MustCompile(`"(existingRegionId|existingRegionAltId|existingZoneId|existingZoneAltId)":\s*"([^"]+)"`)
	found := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		found[m[1]] = m[2]
	}
	t.Logf("осмотрено: %s — извлечено литеральных geo-фикстур: %d %v", fixturesRel, len(found), found)
	require.NotEmpty(t, found,
		"в %s не найдено ни одного литерального geo-идентификатора — проба перестала читать свой предмет", fixturesRel)

	for key, id := range found {
		table := "zones"
		if strings.Contains(key, "Region") {
			table = "regions"
		}
		var status string
		q := "SELECT status FROM " + table + " WHERE id = $1"
		require.NoError(t, pool.QueryRow(ctx, q, id).Scan(&status),
			"фикстура %s называет %s.%s, а посев стенда его не заводит", key, table, id)
		require.Equal(t, "UP", status, "%s=%s посеян закрытым", key, id)
	}
}

// TestStandGeoBaselineSeedDoesNotClobberExistingRows — стенд поднимают и НА
// НЕПУСТОЙ базе, поэтому посев обязан не только не дублировать, но и не
// ПЕРЕЗАПИСЫВАТЬ уже существующее.
//
// Чем это отличается от TestStandGeoBaselineSeedIsIdempotent, и почему одной
// той пробы мало. Та применяет посев поверх строк, которые он же и записал, —
// все значения там тождественны тем, что посев собирается писать, поэтому
// затирание в ней НЕНАБЛЮДАЕМО: перезапись 'UP' на 'UP' не меняет ни одного
// столбца и не даёт ни одной audit-строки. Здесь предсуществующие строки
// ОТЛИЧАЮТСЯ от посевных (администратор закрыл зону на обслуживание, проставил
// код страны, завёл собственный регион), и только на таком входе разница между
// «не тронул» и «вернул к своему» становится видимой.
//
// Проверено инъекцией в обе стороны: добавление в geo-baseline.sql оператора
// `UPDATE zones SET status='UP' WHERE id IN (…)` — ровно того «оживления»,
// которым чинят непригодный стенд, — оставляет три пробы выше ЗЕЛЁНЫМИ и роняет
// эту. Обратно: на поставляемом ON CONFLICT DO NOTHING зелены все четыре.
//
// Отрицание стоит В ПАРЕ с положительным контролем: проба утверждает и то, что
// недостающие строки посев ВСЁ ЖЕ завёл. Без него «ничего не изменилось» было бы
// неотличимо от «посев умер», и проба зеленела бы на пустом файле.
func TestStandGeoBaselineSeedDoesNotClobberExistingRows(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	// Предсуществующее состояние, отличное от посевного:
	//  (1) регион посева, закрытый администратором + непустой country_code;
	//  (2) зона посева, снятая с размещения на обслуживание;
	//  (3) регион и зона, которых в посеве нет вовсе (чужая запись).
	_, err := pool.Exec(ctx, `
		INSERT INTO regions (id, country_code, status, numeric_infra_id)
		     VALUES ('ru-central1', 'RU', 'DOWN', 77),
		            ('ru-central9', 'ZZ', 'UP',   99);
		INSERT INTO zones (id, region_id, status, numeric_infra_id)
		     VALUES ('ru-central1-a', 'ru-central1', 'DOWN', 11),
		            ('ru-central9-a', 'ru-central9', 'UP',   99);`)
	require.NoError(t, err, "не удалось подготовить непустую базу")

	var auditBefore int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM geo_outbox`).Scan(&auditBefore))

	applyStandBaseline(t, pool)

	// (1) закрытый регион остался закрытым, его поля не переписаны посевными.
	var status, country string
	var infra int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, country_code, numeric_infra_id FROM regions WHERE id = 'ru-central1'`).
		Scan(&status, &country, &infra))
	require.Equal(t, "DOWN", status,
		"посев вернул администраторски закрытый регион в 'UP' — стенд затирает существующее")
	require.Equal(t, "RU", country, "посев переписал country_code существующего региона")
	require.EqualValues(t, 77, infra, "посев переписал numeric_infra_id существующего региона")

	// (2) зона, снятая с размещения, осталась снятой.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, numeric_infra_id FROM zones WHERE id = 'ru-central1-a'`).Scan(&status, &infra))
	require.Equal(t, "DOWN", status,
		"посев вернул снятую с обслуживания зону в 'UP' — стенд затирает существующее")
	require.EqualValues(t, 11, infra, "посев переписал numeric_infra_id существующей зоны")

	// (3) чужие строки, которых посев не называет, целы.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, country_code FROM regions WHERE id = 'ru-central9'`).Scan(&status, &country))
	require.Equal(t, "UP", status)
	require.Equal(t, "ZZ", country, "посев тронул регион, которого он не сеет")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM zones WHERE id = 'ru-central9-a'`).Scan(&status))
	require.Equal(t, "UP", status, "посев тронул зону, которой он не сеет")

	// Положительный контроль: недостающее посев всё же завёл — иначе «ничего не
	// изменилось» означало бы мёртвый посев, и все утверждения выше были бы
	// тождественно истинны.
	var seededNew int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM zones WHERE id IN ('ru-central1-b','ru-central1-c','ru-central1-d')`).
		Scan(&seededNew))
	require.Equal(t, 3, seededNew,
		"посев не завёл недостающие зоны — проба «ничего не затёрто» стала бы вакуумной")
	var region2 string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM regions WHERE id = 'ru-central2'`).Scan(&region2))
	require.Equal(t, "UP", region2, "посев не завёл недостающий регион")

	// Audit-строки написаны ТОЛЬКО на реально вставленное: конфликтующие id
	// аудита не порождают (иначе журнал утверждал бы создание того, что уже было).
	var auditAfter, auditForExisting int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM geo_outbox`).Scan(&auditAfter))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM geo_outbox
		  WHERE resource_id IN ('ru-central1','ru-central1-a','ru-central9','ru-central9-a')`).
		Scan(&auditForExisting))
	require.Zero(t, auditForExisting,
		"аудит объявил CREATED для строк, которые уже существовали до посева")
	t.Logf("непустая база: audit до=%d, после=%d (записано только на вставленное); "+
		"осмотрено предсуществующих строк: 4", auditBefore, auditAfter)
}
