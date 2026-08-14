// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Фикстура учёта для проб, чей предмет — НЕ учёт.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.5.
//
// # Зачем это существует
//
// С появлением учёта вставка строки ресурса СПИСЫВАЕТ место, а списать его не с
// чего, пока у проекта нет строки учёта: «не сказано» — отказ, а не «без
// предела» (V2-3). На живом пути строку заводит материализация ПЕРЕД
// writer-транзакцией; проба этого пакета идёт мимо use-case'а, прямо в
// репозиторий, и потому обязана привести базу в то же состояние, в каком её
// видит репозиторий на живом пути. Иначе девяносто две пробы, не имеющие к
// квотам никакого отношения, краснели бы одним и тем же «потолок не назван».
//
// # Почему это НЕ послабление
//
// Фикстура заводит строку учёта ТЕМ ЖЕ оператором, что и продукт
// (`kachopg.MaterializeQuotas` — единственный, и `Quotas().Materialize` writer'а
// зовёт его же), — она не подставная реализация, а вызов настоящей. Механизм при этом продолжает работать на каждой вставке:
// триггер списывает, удаление возвращает, отказ на исчерпании возможен. Меняется
// ровно одно — величина у этих проектов заведомо больше, чем им нужно, потому
// что предмет этих проб лежит в другом месте. Величину назначает администратор,
// и «большой предел у служебного проекта» — состояние, которое продукт
// производит штатно.
//
// # Почему перечень, а не «всем подряд»
//
// Умолчание «любой проект получает место» сделало бы невыразимым состояние
// «потолка нет» — то самое, которое проверяют пробы учёта рядом
// (`quota_integration_test.go`, `quota_bands_integration_test.go`). Они держат
// СВОИ идентичности (`prj-quota-*`, `prj-bands-*`), в этот перечень не входят и
// заводят строки сами.
//
// # Что произойдёт с пробой, заведённой позже
//
// Новая проба с неназванной здесь идентичностью получит отказ
// `resource count quota not provisioned: project <id> has no ceiling stated for
// <вид>` — громкий, называющий предмет и указывающий, что делать: либо добавить
// идентичность сюда, либо (если предмет пробы — сам учёт) завести строку самой.
// Перечень поэтому не дрейфует молча: расхождение с деревом наблюдаемо в тот же
// прогон.

// fixtureQuotaLimit — предел служебных проектов проб.
//
// Заведомо больше, чем нужно любой пробе пакета: предел здесь не предмет
// утверждения, а условие, при котором предмет вообще достижим. Проба, которой
// нужен ИСЧЕРПАННЫЙ предел, заводит свой проект и свою величину.
const fixtureQuotaLimit = 1_000_000

// fixtureQuotaKinds — восемь тенантных видов домена, ровно те, на которых висят
// триггеры учёта (миграция 0040).
//
// Выписаны, а не выведены из каталога: у владельца типа копии каталога нет by
// construction — перечень приезжает ответом владельца величин. Здесь соседа нет,
// поэтому фикстура называет виды прямо. Разойдясь с набором триггеров, перечень
// проявит себя тем же громким отказом, что и незаведённая идентичность.
var fixtureQuotaKinds = []string{
	"vpc.network",
	"vpc.subnet",
	"vpc.address",
	"vpc.networkInterface",
	"vpc.securityGroup",
	"vpc.routeTable",
	"vpc.gateway",
	"vpc.cidrGroup",
}

// fixtureProjects — идентичности проектов, которыми пользуются пробы пакета.
//
// Пробы учёта (`prj-quota-*`, `prj-bands-*`) здесь отсутствуют НАМЕРЕННО: их
// предмет — поведение при заведённой и при отсутствующей строке, и заведи им
// строку фикстура, они бы утверждали про состояние, которого не создавали.
var fixtureProjects = []string{
	"b1gtestproject00000", "prj-1", "prj-2", "prj-autoidx",
	"prj-detach", "prj-inuse", "prj-multi-nic", "prj-nic-status",
	"prj-region", "prj-region-unres", "prj-region-zonal", "prj-replay",
	"prj-sg-usedby-foreign", "prj-sg-usedby-own", "prj-sgref-blocked", "prj-sgref-conc-free",
	"prj-sgref-conc-ref", "prj-sgref-free", "prj-zone", "prj_narrow_addr",
	"prj_zf", "proj-aaaaaaaaaaaaaaaaa", "proj-backfill", "proj-sec-01",
	"proj-sec-02", "proj-sec-03", "proj-sec-09", "proj-sec-11",
	"proj-sec-12", "proj-sec-14", "proj-sec-16", "proj-sec-self",
	"proj-sec-selfok", "proj-sgdom", "proj-sgdom-repo", "project",
	"project-1", "project-abort", "project-alloc", "project-atomic",
	"project-cas", "project-cil0", "project-cil0-conc", "project-cil0-reuse",
	"project-cil0-upd", "project-cycle", "project-del", "project-nic-cqrs",
	"project-overlap", "project-self", "project-setdefault", "project-sg-1",
	"project-sg-abort", "project-slave-fallback", "project-slave-routed", "project-sub",
	"project-used", "project-vrf-conc", "project-vrf-frozen", "project-vrf-range",
	"project-vrf-reuse",
}

// seedFixtureQuotas приводит свежую базу пробы в состояние «проекты
// материализованы».
//
// Идёт через `kachopg.MaterializeQuotas` — тот же и единственный оператор,
// которым пользуется живой путь. Своего INSERT здесь нет намеренно: копия
// оператора разошлась бы с настоящим молча, и разошлась бы именно там, где
// расхождение не видно, — на составе столбцов.
func seedFixtureQuotas(t testing.TB, dsn string) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "фикстура учёта: подключение к базе пробы")
	defer func() { _ = conn.Close(ctx) }()

	rows := make([]kacho.QuotaRow, 0, len(fixtureProjects)*len(fixtureQuotaKinds))
	for _, p := range fixtureProjects {
		for _, k := range fixtureQuotaKinds {
			rows = append(rows, kacho.QuotaRow{
				CarrierType:   "project",
				CarrierID:     p,
				Kind:          k,
				Limit:         fixtureQuotaLimit,
				SourceScope:   "DEFAULT",
				SourceScopeID: "",
				LimitRevision: 0,
				// Зеркало аккаунта непусто: схема отвергает пустое, и отвергает
				// правильно — строка без зеркала невидима аккаунтной дельте.
				AccountID: "acc-fixture",
			})
		}
	}

	n, err := kachopg.MaterializeQuotas(ctx, conn, rows)
	require.NoError(t, err, "фикстура учёта: заведение строк")
	require.Equal(t, int64(len(rows)), n,
		"перепись: заведено строк — столько же, сколько объявлено. Расхождение означает, "+
			"что часть идентичностей уже существовала, то есть фикстура работает не на свежей базе")
}
