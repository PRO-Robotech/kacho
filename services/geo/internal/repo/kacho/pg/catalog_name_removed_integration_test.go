// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIntegration_Geo_CatalogHasNoNameColumn — задача #716.
//
// # Предмет
//
// Снятие поля из контракта и снятие колонки из схемы — РАЗНЫЕ утверждения.
// Первое видно по дереву, второе — только прогоном: колонка, пережившая своё
// поле, невидима отовсюду и обнаруживается лишь тогда, когда кто-нибудь напишет
// в неё второе написание того же предмета.
//
// Здесь стояли две пробы формы имени (`nameformdb.Probe` и её инъекция). Их
// предмет снят вместе с колонкой: формы у того, чего нет, не бывает. Взамен —
// утверждение о самом снятии, и оно исполняется на живой базе после всех
// миграций, а не читает их текст: `ADD CONSTRAINT … UNIQUE (name)` остаётся в
// тексте применённой 0004 и в откате 716001, хотя в действующей схеме ни
// колонки, ни ограничения нет.
//
// # Почему с положительным контролем
//
// Запрос, вернувший ноль строк, одинаково выглядит и когда колонки нет, и когда
// не та схема, не та таблица, не та база. Поэтому рядом стоит колонка, которая
// БЫТЬ ОБЯЗАНА: если её тоже не видно — предпосылка пробы неверна, и её молчание
// про `name` не значит ничего.
func TestIntegration_Geo_CatalogHasNoNameColumn(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t) // сам пропускается под -short

	for _, tc := range []struct {
		table   string
		present string // колонка, существование которой доказывает, что смотрим туда
	}{
		{"regions", "country_code"},
		{"zones", "region_id"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			var got []string
			rows, err := pool.Query(ctx,
				`SELECT column_name FROM information_schema.columns
				  WHERE table_schema = 'kacho_geo' AND table_name = $1`, tc.table)
			require.NoError(t, err)
			defer rows.Close()
			for rows.Next() {
				var c string
				require.NoError(t, rows.Scan(&c))
				got = append(got, c)
			}
			require.NoError(t, rows.Err())

			// Предпосылка: мы читаем ту таблицу, о которой утверждаем.
			require.NotEmpty(t, got, "у kacho_geo.%s не прочитано НИ ОДНОЙ колонки — "+
				"проба смотрит не туда, и её молчание про name ничего не значит", tc.table)
			require.Contains(t, got, tc.present,
				"положительный контроль: колонка %q обязана быть у kacho_geo.%s", tc.present, tc.table)

			require.NotContains(t, got, "name",
				"kacho_geo.%s снова несёт колонку name — идентичность каталога размещения "+
					"одна, и второе написание того же предмета заводит место для расхождения", tc.table)

			t.Logf("kacho_geo.%s: прочитано колонок %d — %v", tc.table, len(got), got)
		})
	}
}
