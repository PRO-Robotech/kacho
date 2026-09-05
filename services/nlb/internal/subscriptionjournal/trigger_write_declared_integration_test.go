// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

// trigger_write_declared_integration_test.go — ТРЕТЬЯ ФОРМА ЗАПИСИ СТРОКИ
// ЖУРНАЛА ОБЪЯВЛЕНА ПОИМЁННО.
//
// # Предмет
//
// Строку журнала пишут тремя формами (перечень и разбор — в шапке
// `emission_write_forms_test.go`). Первые две судит разбор Go. Третья — ТРИГГЕР
// БАЗЫ — не судилась ничем, и перепись о ней молчала.
//
// # Почему проба со схемой, а не разбор миграций
//
// Живое тело триггерной функции — ПОСЛЕДНЕЕ из череды переопределений: у
// `lb_status_recompute` их четыре, и три прежних лежат в ПРИМЕНЁННЫХ миграциях,
// править которые нельзя (ban #5). Разбор текста миграций считал бы мёртвые
// ревизии наравне с живой, то есть краснел бы на собственной истории. Живое тело
// знает только база — поэтому проба проигрывает всю цепочку миграций и
// спрашивает КАТАЛОГ.
//
// # Что именно утверждается
//
// Множество живых функций схемы, чьё тело пишет в таблицу журнала, РАВНО
// объявленной ведомости. Равенство в обе стороны, и обе стороны несущие:
//
//	не объявленная функция → находка: она пишет строку мимо порта `Emit`, а
//	    значит мимо словаря констант и мимо общего строителя нагрузки; ни один
//	    гейт разбора Go её не видит;
//	объявленная, которой в базе нет → находка: запись пережила свой предмет, и
//	    следующая слепая зона унаследует её молчание.
//
// # Чего проверка НЕ утверждает
//
// Она не судит НАГРУЗКУ триггера — согласие его формы с формой Go держит
// `TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo`, и ведомость обязана
// назвать эту пробу для каждой своей записи. Без такой пары объявление было бы
// разрешением, а не объяснением.
//
// # Граница названа: счёт идёт ПО ВХОЖДЕНИЮ ВСТАВКИ в тело функции
//
// Разобрать plpgsql проба не может, поэтому она спрашивает у каталога, чьё тело
// содержит ВСТАВКУ в таблицу журнала. Это ВЕРХНЯЯ оценка: тело, назвавшее
// вставку только в объяснении, тоже попадёт в множество и потребует записи в
// ведомости. Направление ошибки выбрано осознанно — она может лишь потребовать
// лишнего объявления и не может пропустить настоящую точку; обратная граница
// (пропустить) была бы тем самым молчанием, ради которого гейт и заведён.
//
// Первая редакция искала ПРОСТО ИМЯ ТАБЛИЦЫ и на первом же прогоне назвала
// нарушителем `nlb_outbox_notify` — функцию, которая в журнал не пишет вовсе, а
// лишь будит поток (`pg_notify('nlb_outbox', …)`). Верхняя оценка тем и опасна,
// что «потребовать лишнего объявления» на деле означало бы записать в ведомость
// НЕПРАВДУ. Предикат сужен до вставки: `pg_notify` под него не подпадает, а
// гейт, краснеющий на верном коде, отключают первым.

// declaredTriggerWriters — живые триггерные функции, пишущие строку журнала, и
// то, ЧЕМ удержано согласие каждой с формой Go.
//
// Ключ — имя функции в схеме сервиса. Значение — проба, утверждающая, что
// нагрузка этой функции совпадает с нагрузкой строителя Go на ОДНОЙ строке.
// Значение не украшение: без него запись была бы разрешением писать журнал мимо
// всех гейтов разбора, а с ним — объяснением, у которого есть держатель.
var declaredTriggerWriters = map[string]string{
	"lb_status_recompute": "TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo",
}

func TestEveryTriggerWritingTheJournalIsDeclared(t *testing.T) {
	if testing.Short() {
		t.Skip("нужна база: живое тело функции знает только она")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.NewDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	schema, table := splitQualified(subscriptionjournal.Table)
	require.NotEmpty(t, schema,
		"таблица журнала объявлена без схемы — запрос к каталогу спрашивал бы не о том")

	// Образец собирается ИЗ ОБЪЯВЛЕНИЯ журнала, а не выписывается: схема и таблица
	// названы один раз (`subscriptionjournal.Table`), и второе их написание
	// разошлось бы с первым молча. Квалификация схемой необязательна — триггер
	// вправе писать в таблицу своей схемы коротким именем.
	pattern := `insert[[:space:]]+into[[:space:]]+(` + schema +
		`[[:space:]]*\.[[:space:]]*)?` + table + `([^a-z0-9_]|$)`
	rows, err := pool.Query(ctx, `
        SELECT p.proname, p.prosrc ~* $2
          FROM pg_proc p
          JOIN pg_namespace n ON n.oid = p.pronamespace
         WHERE n.nspname = $1
           AND p.prorettype = 'trigger'::regtype`, schema, pattern)
	require.NoError(t, err)

	live := map[string]bool{}
	inspected := 0
	for rows.Next() {
		var name string
		var writes bool
		require.NoError(t, rows.Scan(&name, &writes))
		inspected++
		if writes {
			live[name] = true
		}
	}
	require.NoError(t, rows.Err())

	t.Logf("перепись формы 3 (триггером базы): триггерных функций схемы %q осмотрено %d · "+
		"пишут в %s — %d · объявлено ведомостью %d",
		schema, inspected, subscriptionjournal.Table, len(live), len(declaredTriggerWriters))
	for _, n := range sortedKeysOf(live) {
		t.Logf("  %s — держится пробой %q", n, declaredTriggerWriters[n])
	}

	require.NotZero(t, inspected,
		"в схеме %q не нашлось НИ ОДНОЙ триггерной функции — цепочка миграций не проиграна, "+
			"и перепись беспредметна, а не пуста", schema)

	for _, name := range sortedKeysOf(live) {
		held, declared := declaredTriggerWriters[name]
		require.True(t, declared,
			"функция %s.%s пишет строку журнала, а ведомость её не называет.\n"+
				"Такая точка идёт мимо порта `Emit`: словарь констант её не видит, общий "+
				"строитель нагрузки её не судит, и перепись гейтов разбора Go её не считает. "+
				"Объявите её здесь ВМЕСТЕ с пробой, утверждающей согласие её нагрузки с формой "+
				"Go на одной строке, — либо эмитьте через порт", schema, name)
		require.NotEmpty(t, held,
			"функция %s.%s объявлена без пробы согласия — это разрешение, а не объяснение",
			schema, name)
	}
	for _, name := range sortedKeysOf(declaredTriggerWriters) {
		require.True(t, live[name],
			"ведомость называет функцию %s.%s, а в живой схеме такой пишущей функции НЕТ.\n"+
				"Запись пережила свой предмет: снимите её — иначе следующая слепая зона "+
				"унаследует её молчание", schema, name)
	}
}

// splitQualified делит `схема.таблица` на части.
func splitQualified(qualified string) (schema, table string) {
	if i := strings.IndexByte(qualified, '.'); i >= 0 {
		return qualified[:i], qualified[i+1:]
	}
	return "", qualified
}

// sortedKeysOf — устойчивый порядок имён: иначе диагностика гейта зависела бы от
// порядка обхода карты.
func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
