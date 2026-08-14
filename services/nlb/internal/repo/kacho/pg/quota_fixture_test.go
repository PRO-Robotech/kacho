// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
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
// Фикстура заводит строку учёта ТЕМ ЖЕ оператором, что и продукт
// (`kachopg.MaterializeQuotas` — единственный, и `Quotas().Materialize` writer'а
// зовёт его же), — она не подставная реализация, а вызов настоящей. Механизм при
// этом продолжает работать на каждой вставке: триггер списывает, удаление
// возвращает, отказ на исчерпании возможен. Меняется ровно одно — величина у
// этих проектов заведомо больше, чем им нужно, потому что предмет этих проб
// лежит в другом месте.
//
// # Почему перечень, а не «всем подряд»
//
// Умолчание «любой проект получает место» сделало бы невыразимым состояние
// «потолка нет» — то самое, которое проверяют пробы учёта рядом
// (`quota_integration_test.go`). Они держат СВОИ идентичности (`prj-nlbq-*`), в
// этот перечень не входят и заводят строки сами.
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

// fixtureQuotaKinds — три тенантных вида домена, ровно те, на которых висят
// триггеры учёта (миграция 0032).
//
// Выписаны токенами закрытой таблицы грантуемых пар, а не по памяти о названии
// ресурса: у этого домена вторая часть токена во МНОЖЕСТВЕННОМ числе.
var fixtureQuotaKinds = []string{
	"loadbalancer.networkLoadBalancers",
	"loadbalancer.targetGroups",
	"loadbalancer.listeners",
}

// fixtureNestedKind — ось вложенности домена: слушателей в одном балансировщике.
const fixtureNestedKind = "loadbalancer.networkLoadBalancers.listeners"

// fixtureProjects — идентичности проектов, которыми пользуются пробы пакета.
//
// Перечень СНЯТ С ДЕРЕВА, а не придуман: предикат —
//
//	grep -rhoE '"prj[^"]*"' *_test.go
//
// с последующей уникализацией. На 2026-08-15 он даёт 115 идентичностей.
//
// Пробы учёта (`prj-nlbq-*`) здесь отсутствуют НАМЕРЕННО: их предмет —
// поведение при заведённой и при отсутствующей строке, и заведи им строку
// фикстура, они бы утверждали про состояние, которого не создавали.
var fixtureProjects = []string{
	"prj01ABC", "prj01CASS1234567890ll", "prj01CASTEST0000001", "prj01CHKK1234567890ll",
	"prj01CHKN1234567890ll", "prj01CROSSZONE000001", "prj01CROSSZONE000002", "prj01CVR11234567890ll",
	"prj01CVR21234567890ll", "prj01CVR31234567890ll", "prj01CVR41234567890ll", "prj01CVR4DST7890ABCDl",
	"prj01CVR71234567890ll", "prj01CVR81234567890ll", "prj01CVR91234567890ll", "prj01DELP1234567890ll",
	"prj01DUPP1234567890ll", "prj01EXPAND123456ll1", "prj01EXPAND123456ll2", "prj01EXPAND123456ll3",
	"prj01EXPAND123456ll4", "prj01EXPAND123456ll5", "prj01FKNAME000000001", "prj01FKTGT0000000001",
	"prj01HASS1234567890ll", "prj01LBLS1234567890ll", "prj01LIST1234567890ll", "prj01LSTC1234567890ll",
	"prj01LSTD1234567890ll", "prj01LSTNAMERACE0001", "prj01LSTP1234567890ll", "prj01LSTS1234567890ll",
	"prj01LSTU1234567890ll", "prj01MOVE1234567890ll", "prj01MOVEFK0000000001", "prj01MOVEFKDST000001",
	"prj01MOVEOK234567890l", "prj01MVDD1234567890ll", "prj01MVSS1234567890ll", "prj01NOEXISTTG000001",
	"prj01NOTI1234567890ll", "prj01OCCLB000000001l", "prj01OCCLB000000002l", "prj01OCCLST00000001l",
	"prj01OCCLST00000002l", "prj01OCCTG000000001l", "prj01OCCTG000000002l", "prj01OUTB1234567890ll",
	"prj01RACE1234567890ll", "prj01RBP000000000001", "prj01REGCLAIM0000001",
	"prj01REGTEST0000001", "prj01REPOINT00000001", "prj01SEQTEST0000001", "prj01SG00000000000001",
	"prj01SG00000000000002", "prj01TESTPRJ123456ll", "prj01TG4W1234567890ll", "prj01TGCC1234567890ll",
	"prj01TGCP1234567890ll", "prj01TGDC1234567890ll", "prj01TGDD1234567890ll", "prj01TGDELRESTR00001",
	"prj01TGDF1234567890ll", "prj01TGDR1234567890ll", "prj01TGDS1234567890ll", "prj01TGIP1234567890ll",
	"prj01TGIT1234567890ll", "prj01TGLP1234567890ll", "prj01TGNAMERACE00001", "prj01TGPC1234567890ll",
	"prj01TGPT1234567890ll", "prj01TGRA1234567890ll", "prj01TRGS1234567890ll", "prj01UPDP1234567890ll",
	"prj01VIPRACE00000001", "prj01WIRE0000000001", "prj01XREGION00000001", "prj02MOVEOK234567890l",
	"prj02OTHER234567890ll", "prj0CAP01234567890lll", "prj0CAPCONC34567890ll", "prj0DELPRO34567890lll",
	"prj0LBMVRACE1A000001", "prj0LBMVRACE1B000001", "prj0LSMV1234567890lll", "prj0LSMV2234567890lll",
	"prj0LSTDELPARENT0001", "prj0LSUC1234567890lll", "prj0MDGUARD000000001", "prj0MDGUARD000000002",
	"prj0MDGUARD000000003", "prj0MDRACE0000000001", "prj0PPMSG34567890llll", "prj0TGDELWIRERACE001",
	"prj0TGMVOK2234567890l", "prj0TGMVOK234567890ll", "prj0TGMVRACE1A000001", "prj0TGMVRACE1B000001",
	"prj0TGMVRACE2A000001", "prj0TGMVRACE2B000001", "prj0TGMVX2234567890ll", "prj-A",
	"prj-aaaaaaaaaaaaaaaaa", "prj-bbbbbbbbbbbbbbbbb", "prj-ccccccccccccccccc", "prj-ddddddddddddddddd",
	"prj-eeeeeeeeeeeeeeeee", "prj-fffffffffffffffff", "prj-order-dst-aaaaa", "prj-order-src-aaaaa",
	"prj-t3aaaaaaaaaaaaaa", "prj-t3bbbbbbbbbbbbbb", "prj-x",
}

// seedFixtureQuotas приводит свежую базу пробы в состояние «проекты
// материализованы».
//
// Идёт через `kachopg.MaterializeQuotas` — тот же и единственный оператор,
// которым пользуется живой путь. Своего INSERT для строк учёта здесь нет
// намеренно: копия оператора разошлась бы с настоящим молча, и разошлась бы
// именно там, где расхождение не видно, — на составе столбцов.
//
// Резолв вложенного вида заводится отдельно: он живёт в своей таблице, потому
// что величина резолвится по проекту, а считается по родителю (миграция 0032).
func seedFixtureQuotas(t testing.TB, dsn string) {
	t.Helper()
	seedQuotasForProjects(t, dsn, fixtureProjects)
}

// seedQuotasForProjects — тот же посев для ЯВНО названных идентичностей.
//
// Существует потому, что перечень выше снят с дерева ЛИТЕРАЛАМИ, а литерал не
// видит идентичности, собранной в рантайме: проба гонки строит свои двенадцать
// проектов форматной строкой, и предикат забрал бы САМ ШАБЛОН вместо
// двенадцати настоящих имён — то есть запись, которой нечего покрывать.
//
// Шаблон в этом комментаре намеренно не воспроизводится в кавычках: предикат
// перечня читает и комментарии, и процитированный шаблон вернулся бы в перечень
// той же дорогой, которой однажды туда попал.
//
// Такая проба зовёт этот помощник сама, называя ровно те идентичности, которые
// произвела. Это не послабление: строки заводит тот же единственный оператор
// продукта, механизм продолжает списывать и отказывать, — меняется только то,
// кто назвал идентичность, потому что назвать её раньше было нечем.
func seedQuotasForProjects(t testing.TB, dsn string, projects []string) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "фикстура учёта: подключение к базе пробы")
	defer func() { _ = conn.Close(ctx) }()

	rows := make([]kacho.QuotaRow, 0, len(projects)*len(fixtureQuotaKinds))
	for _, p := range projects {
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

	tag, err := conn.Exec(ctx,
		`INSERT INTO kacho_nlb.nested_quota_defaults
		     (project_id, kind, limit_value, source_scope, source_scope_id,
		      limit_revision, account_id)
		 SELECT unnest($1::text[]), $2, $3, 'DEFAULT', '', 0, 'acc-fixture'`,
		projects, fixtureNestedKind, int64(fixtureQuotaLimit))
	require.NoError(t, err, "фикстура учёта: резолв вложенного вида")
	require.Equal(t, int64(len(projects)), tag.RowsAffected(),
		"перепись: резолв вложенного вида заведён на каждый объявленный проект")
}
