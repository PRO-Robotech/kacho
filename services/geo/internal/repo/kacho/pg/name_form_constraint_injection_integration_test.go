// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/nameformdb"
)

// TestIntegration_NameFormProbe_FailsOnInjectedDefect — доказательство, что
// проба формы имени СПОСОБНА упасть, и что падает она на существе, а не на
// форме записи.
//
// Живёт у geo, а не у каждого из четырёх сервисов: двигатель один
// (`internal/nameform`), и четыре копии этой инъекции доказывали бы одно и то же
// про один и тот же код. Схема geo выбрана как самая дешёвая — две таблицы, ни
// одного внешнего ключа наружу и ни одного триггера учёта.
//
// Инъекция двусторонняя. Без ЗАКОННОГО БЛИЗНЕЦА (подслучай «схема нетронута»)
// красное ничего не значило бы: проба, краснеющая всегда, ловит форму, а не
// существо, и первый же ложный срабат её отключит.
func TestIntegration_NameFormProbe_FailsOnInjectedDefect(t *testing.T) {
	ctx := context.Background()

	t.Run("законный близнец: схема нетронута — находок ноль", func(t *testing.T) {
		pool := newTestPool(t)
		rep, err := geoNameFormProbe(t, pool).Check(ctx, pool)
		require.NoError(t, err)
		require.Empty(t, rep.Findings,
			"на нетронутой схеме проба обязана молчать, иначе её красное ничего не различает")
		require.Len(t, rep.Probed, 2, "перепись: %s", rep.Census())
	})

	t.Run("ограничение СНЯТО — находка называет таблицу", func(t *testing.T) {
		pool := newTestPool(t)
		_, err := pool.Exec(ctx, `ALTER TABLE kacho_geo.zones DROP CONSTRAINT zones_name_check`)
		require.NoError(t, err, "инъекция: снятие формы имени у зоны")

		rep, err := geoNameFormProbe(t, pool).Check(ctx, pool)
		require.NoError(t, err)
		require.NotEmpty(t, rep.Findings, "снятая форма имени обязана быть находкой")
		require.Truef(t, mentions(rep.Findings, "zones"),
			"находка обязана назвать координату — таблицу, у которой формы нет. Получено: %v", rep.Findings)
		// Нетронутая таблица не порождает СВОЕЙ находки. Проверяется префикс
		// «regions:» — им начинается находка ОБ ЭТОЙ таблице; в переписи имя
		// стоит законно (перечень обойдённого), и запрещать его там значило бы
		// требовать от находки не называть объём осмотренного.
		require.Falsef(t, mentions(rep.Findings, "regions: "),
			"нетронутая таблица не должна порождать своей находки: %v", rep.Findings)
	})

	t.Run("форма РАСШИРЕНА — находка на значении вне канона", func(t *testing.T) {
		// Форма на месте, имя ограничения прежнее, перепись сходится — то есть
		// проба, проверяющая лишь НАЛИЧИЕ ограничения, осталась бы зелёной.
		// Красное здесь производит только вставка негодного значения.
		pool := newTestPool(t)
		_, err := pool.Exec(ctx, `ALTER TABLE kacho_geo.zones DROP CONSTRAINT zones_name_check`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `ALTER TABLE kacho_geo.zones
			ADD CONSTRAINT zones_name_check CHECK (name ~ '^[-_a-zA-Z0-9]{1,63}$')`)
		require.NoError(t, err, "инъекция: форма, принимающая подчёркивание и заглавные")

		rep, err := geoNameFormProbe(t, pool).Check(ctx, pool)
		require.NoError(t, err)
		require.NotEmpty(t, rep.Findings, "расширенная форма обязана быть находкой")
		require.Truef(t, mentions(rep.Findings, "bad_name"),
			"находка обязана назвать значение, которое форма пропустила: %v", rep.Findings)
	})

	t.Run("таблица ПОЯВИЛАСЬ — перепись её не пропускает", func(t *testing.T) {
		// Направление, обратное снятию: форму получила таблица, которую проба не
		// обходит. Без этой стороны перечень таблиц был бы «объявлением сверху»,
		// и таблица, заведённая позже, осталась бы недоказанной молча.
		pool := newTestPool(t)
		_, err := pool.Exec(ctx, `CREATE TABLE kacho_geo.nameform_newcomer (
			id   text PRIMARY KEY,
			name text NOT NULL CONSTRAINT nameform_newcomer_name_check
			     CHECK (name ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'))`)
		require.NoError(t, err, "инъекция: таблица с формой имени, которой проба не знает")

		rep, err := geoNameFormProbe(t, pool).Check(ctx, pool)
		require.NoError(t, err)
		require.Truef(t, mentions(rep.Findings, "nameform_newcomer"),
			"таблица с формой имени вне перечня пробы обязана быть названа: %v", rep.Findings)
	})
}

// geoNameFormProbe — тот же объект пробы, что и в основном тесте. Родитель для
// зоны заводится здесь же: без него КАЖДАЯ вставка зоны отвергалась бы внешним
// ключом, и инъекция «форма снята» была бы неотличима от «строка не проходит».
func geoNameFormProbe(t *testing.T, pool *pgxpool.Pool) nameformdb.Probe {
	t.Helper()
	const parentRegion = "reg-nameform-inject-parent"
	_, err := pool.Exec(context.Background(),
		`INSERT INTO kacho_geo.regions (id, name) VALUES ($1, $2)
		 ON CONFLICT (id) DO NOTHING`,
		parentRegion, "nameform-inject-parent")
	require.NoError(t, err)

	return nameformdb.Probe{
		Schema: "kacho_geo",
		Tables: []nameformdb.Table{
			{
				Name: "regions",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_geo.regions (id, name) VALUES ($1, $2)`,
						[]any{fmt.Sprintf("reg-inj-%017d", seq), name}
				},
			},
			{
				Name: "zones",
				Row: func(name string, seq int) (string, []any) {
					return `INSERT INTO kacho_geo.zones (id, region_id, name) VALUES ($1, $2, $3)`,
						[]any{fmt.Sprintf("zone-inj-%017d", seq), parentRegion, name}
				},
			},
		},
	}
}

func mentions(findings []string, needle string) bool {
	for _, f := range findings {
		if strings.Contains(f, needle) {
			return true
		}
	}
	return false
}
