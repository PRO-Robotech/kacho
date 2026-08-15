// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// Фикстура учёта для проб, чей предмет — НЕ учёт.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// # Зачем это существует
//
// С появлением учёта вставка строки ресурса СПИСЫВАЕТ место, а списать его не с
// чего, пока у проекта нет строки учёта: «не сказано» — отказ, а не «без
// предела» (V2-3). На живом пути строку заводит материализация ПЕРЕД
// writer-транзакцией; пробы этого пакета идут мимо use-case'а, прямо в
// репозиторий, и потому обязаны привести базу в то же состояние, в каком её
// видит репозиторий на живом пути.
//
// # Почему это НЕ послабление
//
// Фикстура заводит строки ТЕМИ ЖЕ операторами, что и продукт
// (`kachopg.MaterializeQuotas` / `MaterializeNestedDefaults` — единственные), —
// она не подставная реализация, а вызов настоящей. Механизм при этом продолжает
// работать на каждой вставке: триггер списывает, удаление возвращает, отказ на
// исчерпании возможен. Меняется ровно одно — величина у этих проектов заведомо
// больше, чем нужно, потому что предмет этих проб лежит в другом месте.
//
// # Почему перечень, а не «всем подряд»
//
// Умолчание «любой проект получает место» сделало бы невыразимым состояние
// «потолка нет» — то самое, которое проверяют пробы учёта рядом
// (`quota_integration_test.go`). Они держат СВОИ идентичности (`prj-regq-*`), в
// этот перечень не входят и заводят строки сами.
//
// # Что произойдёт с пробой, заведённой позже
//
// Новая проба с неназванной здесь идентичностью получит отказ
// `resource count quota not provisioned: project <id> has no ceiling stated for
// registry.registries` — громкий, называющий предмет и указывающий, что делать.

// fixtureQuotaLimit — предел служебных проектов проб: заведомо больше, чем нужно
// любой пробе пакета. Проба, которой нужен ИСЧЕРПАННЫЙ предел, заводит свой
// проект и свою величину.
const fixtureQuotaLimit = 1_000_000

// fixtureQuotaKinds — два тенантных вида домена, ровно те, на которых висят
// триггеры учёта (миграция 0015).
//
// Выписаны токенами закрытой таблицы грантуемых пар, а не по памяти о названии
// ресурса: у этого домена вторая часть токена во МНОЖЕСТВЕННОМ числе.
var fixtureQuotaKinds = []string{
	"registry.registries",
	"registry.repositories",
}

// fixtureNestedKind — ось вложенности домена: репозиториев в одном реестре.
const fixtureNestedKind = "registry.registries.repositories"

// fixtureProjects — идентичности проектов, которыми пользуются пробы пакета.
//
// Перечень СНЯТ С ДЕРЕВА, а не придуман: предикат —
//
//	grep -rhoE '"prj[^"]*"' *_test.go
//
// На 2026-08-15 он даёт три идентичности; пробы учёта (`prj-regq-*`) в перечень
// намеренно не входят.
//
// Предикат намеренно ШИРОКИЙ — по литералу, а не по вызову конструктора. Узкая
// первая редакция читала первый аргумент `newReg(` и потеряла ровно одну
// идентичность: ту, что заводится в соседнем файле через помощника, а не прямым
// вызовом. Радиус берётся по имени механизма, а не по форме, в которой его
// заметили; цена ошибки — полный прогон пакета.
var fixtureProjects = []string{"prj-P", "prj-Q", "prj-REPOTUPLE"}

// seedFixtureQuotas приводит свежую базу пробы в состояние «проекты
// материализованы».
func seedFixtureQuotas(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	rows := make([]kachopg.QuotaRow, 0, len(fixtureProjects)*len(fixtureQuotaKinds))
	nested := make([]kachopg.QuotaRow, 0, len(fixtureProjects))
	for _, p := range fixtureProjects {
		for _, k := range fixtureQuotaKinds {
			rows = append(rows, kachopg.QuotaRow{
				CarrierType:   "project",
				CarrierID:     p,
				Kind:          k,
				Limit:         fixtureQuotaLimit,
				SourceScope:   "DEFAULT",
				LimitRevision: 0,
				// Зеркало аккаунта непусто: схема отвергает пустое, и отвергает
				// правильно — строка без зеркала невидима аккаунтной дельте.
				AccountID: "acc-fixture",
			})
		}
		nested = append(nested, kachopg.QuotaRow{
			CarrierID:     p,
			Kind:          fixtureNestedKind,
			Limit:         fixtureQuotaLimit,
			SourceScope:   "DEFAULT",
			LimitRevision: 0,
			AccountID:     "acc-fixture",
		})
	}

	n, err := kachopg.MaterializeQuotas(ctx, pool, rows)
	require.NoError(t, err, "фикстура учёта: заведение строк")
	require.Equal(t, int64(len(rows)), n,
		"перепись: заведено строк — столько же, сколько объявлено. Расхождение означает, "+
			"что фикстура работает не на свежей базе")

	nn, err := kachopg.MaterializeNestedDefaults(ctx, pool, nested)
	require.NoError(t, err, "фикстура учёта: резолв вложенного вида")
	require.Equal(t, int64(len(nested)), nn,
		"перепись: резолв вложенного вида заведён на каждый объявленный проект")
}
