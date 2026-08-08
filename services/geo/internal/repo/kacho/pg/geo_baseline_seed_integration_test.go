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
