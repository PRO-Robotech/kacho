// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// applied_type_reaches_the_verdict_integration_test.go — ПОСЛЕДНЯЯ МИЛЯ пункта 1
// DoD эпика #1027 (задача продукта #1968): от применения манифеста до ответа
// ВЕРДИКТА прав, на живой базе, одной пробой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТА ПРОБА, ЕСЛИ ЗВЕНЬЯ ДОКАЗАНЫ ПОРОЗНЬ
//
// Доказано две вещи, обе с инъекцией: тип, заведённый в работающем процессе,
// доезжает до ПРОЕКЦИИ (`catalog.TestIAMCT2_14_AppliedTypeReachesTheProjection`)
// и доезжает через настоящий применитель
// (`TestIAMCT2_14_AppliedAfterStartReachesTheProjection`). Обе останавливаются на
// `role_verb`. Что проекцию читает ВЕРДИКТ, было установлено ЧТЕНИЕМ запроса, а
// не прогоном.
//
// В этой линии дважды находили разрыв, невидимый ни с одной стороны по
// отдельности: обе половины исправны, каждая проверена своими пробами, а вопрос,
// который задаёт одна, — не тот, на который отвечает другая. Ни одна проба
// половины покраснеть не может by construction. Исключить это умеет только
// проба, идущая СКВОЗЬ ОБЕ стороны, — она и есть предмет файла.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА ПРЕДМЕТА, И ИХ НЕЛЬЗЯ ПУТАТЬ
//
// «Новый тип» имеет ДВА смысла, и сквозной путь у них РАЗНЫЙ:
//
//	СТРОКА КАТАЛОГА заведена применением в работающем процессе, а блок типа
//	модели прав пришёл со сборкой   → `TestDoD1_...CarriesTheGrantToTheVerdict`
//
//	ТИП НЕ ЗНАЕТ И СБОРКА — ни блока модели, ни словаря
//	                                → `TestDoD1_...StopsAtTheModelAndSaysSo`
//
// Первый сходится и утверждается здесь целиком. Второй НЕ сходится, и это
// измерено, а не предположено: проекция полна, вердикт пуст. Разбор — во второй
// пробе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УТВЕРЖДАЕТСЯ, А ЧТО НЕТ
//
// Утверждается ВЕРДИКТ (`relverdict.Ask`), а не строка таблицы: строки уже
// утверждают пробы выше, и повторять их значило бы завести два места об одном
// предмете. Роль заводится ТЕМИ ЖЕ тремя писателями и в том же порядке, каким её
// заводит use-case создания роли (`apps/kacho/api/role/create.go`:
// `ReplaceRuleSelectors` → `ReplaceRoleVerbs` от `cat.Facts().RoleVerbsFromSelectors`
// → `ReplaceRuleRefs`), в одной транзакции записи.
//
// ЧЕГО ПРОБА НЕ ПОКРЫВАЕТ, сказано прямо: транспорт (RPC), операцию, стража прав
// вызывающего и материализацию кортежей. Их полосы свои, и утверждать о них здесь
// значило бы заявлять шире сделанного. Подставлены ВХОДЫ, а не звенья цепи:
// арендаторская обвязка и строка выдачи кладутся оператором вставки, тогда как
// каталог, снимок, проекция и вердикт ПРОИЗВОДЯТСЯ. Вопрос задаётся форме `Ask`
// (`relverdict/query.go`); `List` и `Expand` соединяют ту же `role_verb` и здесь
// не спрашиваются.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА КОНТРОЛЯ, БЕЗ КОТОРЫХ УТВЕРЖДЕНИЕ ПУСТО
//
// Пара «снято → отказ, заведено → разрешение» порознь выполнима двумя способами,
// не имеющими к предмету отношения, и оба измерены инъекцией, а не выведены:
//
//  1. ЖИВОЙ СОСЕД. Отказ в снятом состоянии выполним цепью вердикта, отвечающей
//     отказом ВСЕМУ. Инъекция — переселение проекций, расширенное со снятого
//     РЕСУРСА до всего МОДУЛЯ: дофиксовая проба остаётся зелёной и печатает
//     «сквозной путь СОШЁЛСЯ», дополненная краснеет и называет соседа;
//  2. ПРИПИСЫВАЕМОСТЬ. Непустая проекция после заведения выполнима снимком,
//     который снятия не заметил вовсе. Инъекция — `Snapshot.Refresh`, отвечающий
//     успехом и не подменяющий факт: дофиксовая проба зелена и печатает то же
//     «СОШЁЛСЯ» на снимке, ни разу не обновившемся; дополненная краснеет на
//     утверждении о потере.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/seed"
	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/catalog"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
	kachorepo "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/relverdict"
)

// Предмет ПЕРВОЙ пробы: ресурс ПОСТАВЛЯЕМОГО модуля. Блок его типа пришёл со
// сборкой, а СТРОКА каталога снимается и заводится заново применением манифеста
// в работающем процессе — то есть ровно тем путём, каким её заводит клиент.
const (
	verdictShippedModule = "vpc"
	verdictShippedRes    = "cidrGroup"
	verdictShippedDotted = verdictShippedModule + "." + verdictShippedRes
	verdictShippedModel  = "vpc_cidr_group"
)

// Предмет ЖИВОГО СОСЕДА: ресурс ТОГО ЖЕ модуля, которого снятие не касается.
//
// Он нужен как положительный контроль В СНЯТОМ СОСТОЯНИИ, и это не украшение:
// без него «после снятия — отказ» выполнимо цепью вердикта, отвечающей отказом
// ВСЕМУ, — то есть шаг снятия зеленел бы на сломанном целиком пути, и отличить
// это от исправной работы было бы нечем. Сосед поставляемый, живой на всём
// протяжении пробы, и спрашивается ТЕМ ЖЕ путём.
const (
	verdictNeighbourRes    = "network"
	verdictNeighbourDotted = verdictShippedModule + "." + verdictNeighbourRes
	verdictNeighbourModel  = "vpc_network"
)

// Предмет ВТОРОЙ пробы: тип, которого сборка не знает ни одним словарём. Модуль
// синтетический и не член платформенного набора — тот же, на котором стоят
// сценарии применителя.
const (
	verdictAppliedRes    = "alpha"
	verdictAppliedDotted = applierProbeModule + "." + verdictAppliedRes
	verdictAppliedModel  = applierProbeModule + "_" + verdictAppliedRes
)

// verdictProbeVerbs — глаголы синтетического ресурса. Совпадают с набором
// поставляемого соседа намеренно: две пробы обязаны отличаться РОВНО тем, что
// проверяется (знает ли тип сборка), а не ещё и составом глаголов.
var verdictProbeVerbs = []string{"get", "list", "update", "delete"}

// verdictTenant — арендаторская обвязка: аккаунт, пользователь, проект и две
// служебные учётки (одна получает выдачу, вторая — отрицание).
//
// Строки настоящие, а не подставные: выдача ссылается на учётку внешним ключом, а
// вердикт идёт по цепи областей (`resource_parent_edge`). Фикстура, обходящая
// ключ, доказывала бы работу запроса на данных, которых в проде не бывает.
type verdictTenant struct {
	accountID string
	userID    string
	projectID string
	granted   string
	bare      string
}

func seedVerdictTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) verdictTenant {
	t.Helper()
	tn := verdictTenant{
		accountID: "acc-dod1",
		userID:    "usr-dod1",
		projectID: "prj-dod1",
		granted:   "sva-dod1-granted",
		bare:      "sva-dod1-bare",
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	exec := func(sql string, args ...any) {
		t.Helper()
		_, eerr := tx.Exec(ctx, sql, args...)
		require.NoError(t, eerr, "посев обвязки: %s", sql)
	}
	exec(`INSERT INTO users (id, account_id, external_id, email, display_name, invite_status)
	      VALUES ($1, $2, $3, $4, $5, 'ACTIVE')`,
		tn.userID, tn.accountID, "ext-"+tn.userID, "u-dod1@example.com", "DoD1")
	exec(`INSERT INTO accounts (id, name, owner_user_id, labels)
	      VALUES ($1, 'dod1-acc', $2, '{}'::jsonb)`, tn.accountID, tn.userID)
	exec(`INSERT INTO projects (id, account_id, name, labels)
	      VALUES ($1, $2, 'dod1-prj', '{}'::jsonb)`, tn.projectID, tn.accountID)
	for _, sa := range []string{tn.granted, tn.bare} {
		exec(`INSERT INTO kacho_iam.service_accounts (id, account_id, name)
		      VALUES ($1, $2, $3)`, sa, tn.accountID, sa)
	}
	require.NoError(t, tx.Commit(ctx))
	return tn
}

// declareRole заводит роль и кладёт ТРИ проекции её правила теми же писателями и
// в том же порядке, каким это делает use-case создания роли.
//
// Проекция глаголов вычисляется СНИМКОМ каталога (`facts`), а не литералом: это и
// есть звено, ради которого проба написана. Подать сюда готовые пары значило бы
// обойти его.
func declareRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo kachorepo.Repository,
	facts *catalog.Facts, roleID, accountID, module, resource string) []domain.RoleVerb {
	t.Helper()

	rules := domain.Rules{{Module: module, Resources: []string{resource}, Verbs: []string{"*"}}}
	sels := rules.MaterializingSelectors()
	pairs := facts.RoleVerbsFromSelectors(sels)

	var exists bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM roles WHERE id = $1)`, roleID).Scan(&exists))
	if !exists {
		_, err := pool.Exec(ctx, `
			INSERT INTO roles (id, account_id, name, description, permissions)
			VALUES ($1, $2, $3, 'DoD-1 through probe', '["iam.users.*.read"]'::jsonb)`,
			roleID, accountID, strings.ToLower("dod1_"+module+"_"+resource))
		require.NoError(t, err, "завести роль")
	}

	w, err := repo.Writer(ctx)
	require.NoError(t, err, "открыть транзакцию записи")
	committed := false
	defer func() {
		if !committed {
			_ = w.Rollback(ctx)
		}
	}()
	require.NoError(t, w.RolesW().ReplaceRuleSelectors(ctx, domain.RoleID(roleID), sels),
		"писатель селекторов правила")
	require.NoError(t, w.RolesW().ReplaceRoleVerbs(ctx, domain.RoleID(roleID), pairs),
		"писатель проекции глаголов")
	require.NoError(t, w.RolesW().ReplaceRuleRefs(ctx, domain.RoleID(roleID), domain.RuleRefsOf(rules)),
		"писатель объявленных сегментов")
	require.NoError(t, w.Commit(ctx))
	committed = true
	return pairs
}

// snapshotProjectionOf — пары проекции по снимку, БЕЗ записи.
//
// Читающий близнец `declareRole`, и он нужен именно в СНЯТОМ состоянии: писать
// пару по снятому типу нельзя — её отвергнет внешний ключ `role_verb_type_fk`, и
// отказ пришёл бы ЧУЖОЙ полосой вместо утверждения этой пробы. Спрашивается ровно
// то, что кладёт писатель, и теми же селекторами.
func snapshotProjectionOf(facts *catalog.Facts, module, resource string) []domain.RoleVerb {
	rules := domain.Rules{{Module: module, Resources: []string{resource}, Verbs: []string{"*"}}}
	return facts.RoleVerbsFromSelectors(rules.MaterializingSelectors())
}

// grantOnProject выдаёт роль субъекту на проект и кладёт объект под этот проект.
func grantOnProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	tn verdictTenant, bindingID, roleID, objectModelType, objectID string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string, args ...any) {
		t.Helper()
		_, eerr := tx.Exec(ctx, sql, args...)
		require.NoError(t, eerr, "выдача: %s", sql)
	}
	exec(`INSERT INTO kacho_iam.resource_parent_edge
	        (object_type, object_id, parent_type, parent_id, depth)
	      VALUES ($1, $2, 'project', $3, 1)
	      ON CONFLICT DO NOTHING`, objectModelType, objectID, tn.projectID)
	exec(`INSERT INTO kacho_iam.access_bindings
	        (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
	      VALUES ($1, 'service_account', $2, $3, 'project', $4, 'ACTIVE')
	      ON CONFLICT DO NOTHING`,
		bindingID, tn.granted, roleID, tn.projectID)
	exec(`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
	      VALUES ($1, 'service_account', $2) ON CONFLICT DO NOTHING`, bindingID, tn.granted)
	require.NoError(t, tx.Commit(ctx))
}

// askVerdict задаёт вопрос форме E в СВОЕЙ читающей транзакции и возвращает все
// три составляющие ответа.
//
// Ошибка возвращается, а не гасится: у вердикта ТРИ исхода, и «не вычислено»
// (`Unknown` + ошибка) не есть «нет прав». Проба, приводящая их к булеву, потеряла
// бы ровно то различие, ради которого различимость и утверждается.
func askVerdict(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	subject, modelType, objectID, relation string) (relverdict.Verdict, relverdict.Grounds, error) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	return relverdict.Ask(ctx, tx, relverdict.Query{
		Subject: "service_account:" + subject, ObjectType: modelType,
		ObjectID: objectID, Relation: relation,
	})
}

// shippedManifest — поставляемый манифест модуля, разобранный из дерева.
//
// `drop` называет ресурс, который из него вынимается: снятие строки каталога
// выражается ОТСУТСТВИЕМ ресурса в объявлении, а не отдельным глаголом (см. шапку
// применителя, п. 1).
func shippedManifest(t *testing.T, module, drop string) *manifest.Manifest {
	t.Helper()
	// `../../../../..` от этого пакета — каталог `services` монорепо.
	body, err := os.ReadFile(filepath.Clean( // #nosec G304 -- путь собран из констант пробы
		filepath.Join("../../../../..", module, "manifest.yaml")))
	require.NoError(t, err, "прочитать манифест модуля %s", module)
	m, err := manifest.Load(body)
	require.NoError(t, err, "разобрать манифест модуля %s", module)
	if drop == "" {
		return m
	}
	kept := make([]manifest.Resource, 0, len(m.Resources))
	for _, r := range m.Resources {
		if r.Name == drop {
			continue
		}
		kept = append(kept, r)
	}
	require.Lenf(t, kept, len(m.Resources)-1,
		"ресурс %q в манифесте модуля %s не найден — вынимать нечего, и сценарий снятия "+
			"стал бы вакуумным", drop, module)
	m.Resources = kept
	return m
}

// verbsOf — глаголы пар проекции, для переписи в выводе.
func verbsOf(pairs []domain.RoleVerb) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.Verb)
	}
	return out
}

// TestDoD1_RuntimeAppliedCatalogRowCarriesTheGrantToTheVerdict — ПЕРВЫЙ предмет:
// строка каталога, снятая и заведённая заново применением манифеста В РАБОТАЮЩЕМ
// ПРОЦЕССЕ, доводит выдачу до НЕНУЛЕВОГО вердикта.
//
// Утверждаются ОБА направления, и это несущее: порознь каждое выполнимо путём,
// который не даёт права НИКОГДА (тогда «снятие прекращает» верно тривиально) либо
// даёт его ВСЕГДА (тогда «заведение восстанавливает» верно тривиально).
//
//	заведено (посев)      → Allow      исходное состояние, положительный контроль
//	снято применением     → Deny       и НЕ по незнанию типа: модель его знает
//	заведено применением  → Allow      снова, после пересчёта проекции по снимку
//	субъект без выдачи    → Deny       отрицание
//	типа нет вовсе        → Deny + «тип не объявлен»  ноль отличим от «не вычислено»
func TestDoD1_RuntimeAppliedCatalogRowCarriesTheGrantToTheVerdict(t *testing.T) {
	ctx, pool := catalogPool(t)
	catRepo := kachopg.NewCatalogRepo(pool)
	repo := kachopg.New(pool, nil)
	applier := applierOver(t, pool)

	census, err := seed.AssertCatalogParity(ctx, catRepo)
	require.NoError(t, err, "страж паритета каталога")
	snap, err := catalog.NewSnapshot(census.Live, catRepo, nil, nil)
	require.NoError(t, err, "снимок каталога")

	mods, res, verbs := liveCatalogCounts(t, ctx, pool)
	t.Logf("перепись каталога: модулей %d, ресурсов %d, действий %d", mods, res, verbs)

	tn := seedVerdictTenant(t, ctx, pool)
	const roleID = "rol-dod1-shipped"

	// ── (1) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: путь работает ──────────────────────────
	pairs := declareRole(t, ctx, pool, repo, snap.Facts(),
		roleID, tn.accountID, verdictShippedModule, verdictShippedRes)
	require.NotEmptyf(t, pairs,
		"проекция по %q пуста ещё до всякого применения — путь не работает НИ ДЛЯ КОГО, "+
			"и всё, что ниже, было бы беспредметно", verdictShippedDotted)
	grantOnProject(t, ctx, pool, tn, "acb-dod1", roleID, verdictShippedModel, "cg-dod1")
	t.Logf("исходно: тип %q → пар проекции %d, глаголы %v",
		verdictShippedDotted, len(pairs), verbsOf(pairs))

	// ── (1а) ЖИВОЙ СОСЕД: заводится СЕЙЧАС, спрашивается в СНЯТОМ состоянии ──
	//
	// Здесь утверждается только то, что путь соседа работает ДО снятия. Без
	// этого утверждения контроль ниже был бы двусмыслен: его краснота означала бы
	// и «снятие унесло соседа», и «сосед не работал никогда».
	const neighbourRoleID = "rol-dod1-neighbour"
	npairs := declareRole(t, ctx, pool, repo, snap.Facts(),
		neighbourRoleID, tn.accountID, verdictShippedModule, verdictNeighbourRes)
	require.NotEmptyf(t, npairs,
		"проекция по соседу %q пуста ещё до снятия — контроль живого соседа стал бы "+
			"беспредметным", verdictNeighbourDotted)
	grantOnProject(t, ctx, pool, tn, "acb-dod1-nb", neighbourRoleID, verdictNeighbourModel, "net-dod1")

	nGot, nGrounds, nErr := askVerdict(t, ctx, pool, tn.granted, verdictNeighbourModel, "net-dod1", "v_get")
	require.NoError(t, nErr, "вердикт по соседу не вычислен — это «не выполнилось», а не отказ")
	require.Equalf(t, relverdict.Allow, nGot,
		"сосед %q не даёт allow ДО всякого снятия (тип не объявлен моделью: %t) — контроль, "+
			"который не может быть верен, не отличает ничего",
		verdictNeighbourDotted, nGrounds.TypeNotDeclared)
	t.Logf("живой сосед: тип %q → пар проекции %d, вердикт до снятия = %v",
		verdictNeighbourDotted, len(npairs), nGot)

	got, grounds, aerr := askVerdict(t, ctx, pool, tn.granted, verdictShippedModel, "cg-dod1", "v_get")
	require.NoErrorf(t, aerr, "вердикт не вычислен — это «не выполнилось», а не отказ")
	require.Equalf(t, relverdict.Allow, got,
		"выдача не доехала до вердикта ещё на посеянной строке (тип не объявлен моделью: %t) — "+
			"дальнейшее о применении ничего не утверждало бы", grounds.TypeNotDeclared)

	// ── (2) СНЯТИЕ ПРИМЕНЕНИЕМ: вердикт прекращается ───────────────────────
	rep, err := applier.Apply(ctx, shippedManifest(t, verdictShippedModule, verdictShippedRes))
	require.NoError(t, err, "применение манифеста БЕЗ ресурса %q", verdictShippedRes)
	require.Truef(t, rep.Changed(), "снятие каталог не изменило (%s) — снимать было нечего", rep)
	t.Logf("снятие: %s", rep)
	require.NoError(t, snap.Refresh(ctx), "обновление снимка каталога")

	// ПРИПИСЫВАЕМОСТЬ шага (3), и без неё он не утверждает своего предмета:
	// непустая проекция после заведения получилась бы и у снимка, снятия не
	// заметившего вовсе. Тогда «заведено ПРИМЕНЕНИЕМ» доказывалось бы состоянием,
	// которое просто ни разу не менялось, и отставший снимок был бы неотличим от
	// исправной работы.
	require.Emptyf(t, snapshotProjectionOf(snap.Facts(), verdictShippedModule, verdictShippedRes),
		"снимок не потерял снятый тип %q: пары шага заведения тогда не приписываются "+
			"применению — их дал бы и снимок, ни разу не обновившийся", verdictShippedDotted)
	// ЗЕРКАЛО к утверждению выше: сосед в снимке остался. Без него «потеряно»
	// зеленело бы и на снимке, потерявшем ВСЁ, то есть на другой поломке.
	require.NotEmptyf(t, snapshotProjectionOf(snap.Facts(), verdictShippedModule, verdictNeighbourRes),
		"снимок после снятия %q потерял и живого соседа %q — утверждение о потере выше "+
			"зеленело бы на снимке, потерявшем всё",
		verdictShippedDotted, verdictNeighbourDotted)

	got, grounds, aerr = askVerdict(t, ctx, pool, tn.granted, verdictShippedModel, "cg-dod1", "v_get")
	require.NoError(t, aerr, "вердикт после снятия не вычислен — «не выполнилось», а не отказ")
	require.Equalf(t, relverdict.Deny, got,
		"строка каталога снята, а выдача продолжает действовать: снятие ресурса не доезжает "+
			"до вердикта (тип не объявлен моделью: %t)", grounds.TypeNotDeclared)
	// Ноль по ПРАВУ, а не по незнанию типа: блок типа модели на месте, снята
	// именно строка каталога. Без этого утверждения «Deny» выше зеленел бы и в
	// том случае, когда вердикт перестал понимать вопрос.
	require.Falsef(t, grounds.TypeNotDeclared,
		"отказ после снятия строки дан по основанию «типа нет в словаре модели» — это другой "+
			"отказ: модель типа %q знает, снята СТРОКА КАТАЛОГА, и различать их обязательно",
		verdictShippedModel)
	t.Logf("после снятия: исход=%v, тип не объявлен моделью=%t", got, grounds.TypeNotDeclared)

	// ЖИВОЙ СОСЕД В СНЯТОМ СОСТОЯНИИ — отказ выше сказан ИМЕННО о снятой строке.
	// Утверждение `TypeNotDeclared == false` выше говорит, что вердикт ПОНЯЛ
	// вопрос; оно не говорит, что вердикт кому-нибудь ещё отвечает «да» в этот
	// самый момент. Разные вопросы, и второй закрывается только соседом.
	nGot, nGrounds, nErr = askVerdict(t, ctx, pool, tn.granted, verdictNeighbourModel, "net-dod1", "v_get")
	require.NoError(t, nErr, "вердикт по соседу после снятия не вычислен")
	require.Equalf(t, relverdict.Allow, nGot,
		"снятие ресурса %q унесло с собой живого соседа %q (вердикт %v, тип не объявлен "+
			"моделью: %t): отказ выше тогда сказан не о снятой строке, а о цепи вердикта, "+
			"отвечающей отказом ВСЕМУ",
		verdictShippedDotted, verdictNeighbourDotted, nGot, nGrounds.TypeNotDeclared)
	t.Logf("живой сосед в снятом состоянии: %q → %v — отказ выше принадлежит снятой строке",
		verdictNeighbourDotted, nGot)

	// ── (3) ЗАВЕДЕНИЕ ПРИМЕНЕНИЕМ: вердикт восстанавливается ───────────────
	rep, err = applier.Apply(ctx, shippedManifest(t, verdictShippedModule, ""))
	require.NoError(t, err, "применение ПОЛНОГО манифеста модуля %q", verdictShippedModule)
	require.Truef(t, rep.Changed(), "заведение каталог не изменило (%s) — заводить было нечего", rep)
	t.Logf("заведение: %s", rep)
	require.NoError(t, snap.Refresh(ctx), "обновление снимка каталога")

	pairs = declareRole(t, ctx, pool, repo, snap.Facts(),
		roleID, tn.accountID, verdictShippedModule, verdictShippedRes)
	require.NotEmptyf(t, pairs,
		"строка заведена применением, а проекция по %q пуста: роль создалась бы без отказа, "+
			"и арендатор не получил бы НИЧЕГО", verdictShippedDotted)
	t.Logf("после заведения: тип %q → пар проекции %d, глаголы %v",
		verdictShippedDotted, len(pairs), verbsOf(pairs))

	got, grounds, aerr = askVerdict(t, ctx, pool, tn.granted, verdictShippedModel, "cg-dod1", "v_get")
	require.NoError(t, aerr, "вердикт после заведения не вычислен — «не выполнилось», а не отказ")
	require.Equalf(t, relverdict.Allow, got,
		"СКВОЗНОЙ ПУТЬ НЕ СОШЁЛСЯ: строка заведена применением (%s), пар проекции %d "+
			"(глаголы %v), выдача на месте — а вердикт даёт %v (тип не объявлен моделью: %t)",
		rep, len(pairs), verbsOf(pairs), got, grounds.TypeNotDeclared)
	t.Logf("сквозной путь СОШЁЛСЯ: применение → роль → выдача → вердикт = %v", got)

	// ── (4) ОТРИЦАНИЕ: субъект без выдачи права не получает ────────────────
	bare, bareGrounds, bareErr := askVerdict(t, ctx, pool, tn.bare, verdictShippedModel, "cg-dod1", "v_get")
	require.NoError(t, bareErr, "вердикт по субъекту без выдачи не вычислен")
	require.Equalf(t, relverdict.Deny, bare,
		"субъект БЕЗ выдачи получил %v (тип не объявлен моделью: %t) — утверждение «ненулевой "+
			"ответ» выше зеленело бы на вердикте, отвечающем «да» всем", bare, bareGrounds.TypeNotDeclared)

	// ── (5) НОЛЬ ОТЛИЧИМ ОТ «НЕ ВЫЧИСЛЕНО» ────────────────────────────────
	//
	// Отказ по ПРАВУ (п. 2 и 4) и отказ по НЕЗНАНИЮ ТИПА — разные ответы, и
	// вердикт обязан их различать: иначе опечатка в имени типа читается как «прав
	// не выдали» и ищется в правах (`relverdict.Asker.undeclaredType`).
	nosuch, nosuchGrounds, nosuchErr := askVerdict(t, ctx, pool,
		tn.granted, applierProbeModule+"_neverdeclared", "x-1", "v_get")
	require.NoError(t, nosuchErr, "вопрос о несуществующем типе обязан быть ОТВЕТОМ, а не ошибкой")
	require.Equal(t, relverdict.Deny, nosuch, "несуществующий тип обязан давать отказ")
	require.Truef(t, nosuchGrounds.TypeNotDeclared,
		"отказ по несуществующему типу не назвал своего основания — тогда «нет прав» и "+
			"«такого типа не бывает» отвечают одинаково, и различить их нечем")
	t.Logf("различимость нолей: по праву → Deny/тип объявлен; по незнанию типа → Deny/тип НЕ объявлен")
}

// TestDoD1_TypeUnknownToTheBuildStopsAtTheModelAndSaysSo — ВТОРОЙ предмет:
// граница, измеренная прогоном, а не выведенная чтением.
//
// # Что здесь установлено
//
// Тип, которого сборка не знает НИ ОДНИМ словарём, доезжает до ПРОЕКЦИИ (это
// закрыл #1816) и НЕ доезжает до ВЕРДИКТА. Звено названо: `relverdict` строит
// план вывода у `authzmodel.Shared()` — разбора модели, ВШИТОЙ в двоичный файл
// (`//go:embed fga_model.fga`). Блок типа порождается из манифеста СБОРКОЙ
// (`modelrender`, сверка `model-canon-check`), поэтому манифест, применённый в
// работающем процессе, блока не приносит, и `Plan` отвечает «тип не объявлен
// моделью».
//
// Строки каталога блок дать не могут: ему нужны указатели, ярусы и авторские
// отношения, а `catalog_resource` несёт модуль, ресурс, точечное имя и имя типа —
// и только их. То есть это не забытое звено, а ГРАНИЦА принятого решения
// (`authzmodel` дословно: «Модель неизменна в пределах процесса: она вшита»).
//
// # Что утверждается — и почему НЕ утверждается сам отказ
//
// Проба НЕ закрепляет отказ как верное поведение: закрепить дефект значило бы
// сделать его починку красной. Утверждаются два свойства, верные и сегодня, и
// после закрытия разрыва:
//
//  1. ПРОЕКЦИЯ ПОЛНА. Это предмет #1816, и он обязан держаться: пустая проекция
//     вернула бы состояние «роль создалась, арендатор не получил ничего»;
//  2. ОТКАЗ НАЗЫВАЕТ СВОЁ ОСНОВАНИЕ. Ответ вердикта — либо разрешение, либо отказ
//     с признаком «тип не объявлен моделью». ТИХОГО отказа быть не может: он
//     читался бы как «право не выдано» и искался бы в правах, а не в модели.
//
// Разрыв заведён задачей продукта #1969 (эпик #1027, ветвь #1087); её предикат
// снятия — вердикт `Allow` на этом же входе. Когда он наступит, вторая часть
// утверждения станет вакуумной — и снять её надо ТЕМ ЖЕ изменением.
func TestDoD1_TypeUnknownToTheBuildStopsAtTheModelAndSaysSo(t *testing.T) {
	ctx, pool := catalogPool(t)
	catRepo := kachopg.NewCatalogRepo(pool)
	repo := kachopg.New(pool, nil)

	// КОНТРОЛЬ ПРЕДПОСЫЛКИ: сборка синтетического типа не знает. Впиши его
	// кто-нибудь в манифест — и проба зеленела бы вхолостую, а отличить это от
	// исправного пути было бы нечем.
	if _, known := authzmap.FGAObjectType(verdictAppliedDotted); known {
		t.Fatalf("сборка знает %q — предпосылка отпала, проба стала бы вакуумной",
			verdictAppliedDotted)
	}
	// Зеркало: поставляемого соседа сборка знает. Без него контроль выше зеленел
	// бы и на словаре, не знающем НИЧЕГО.
	if _, known := authzmap.FGAObjectType(verdictShippedDotted); !known {
		t.Fatalf("сборка не знает поставляемого соседа %q — контроль предпосылки беспредметен",
			verdictShippedDotted)
	}

	census, err := seed.AssertCatalogParity(ctx, catRepo)
	require.NoError(t, err, "страж паритета каталога")
	snap, err := catalog.NewSnapshot(census.Live, catRepo, nil, nil)
	require.NoError(t, err, "снимок каталога")

	tn := seedVerdictTenant(t, ctx, pool)

	rep, err := applierOver(t, pool).Apply(ctx, probeManifest(
		probeResource(verdictAppliedRes, verdictProbeVerbs...),
	))
	require.NoError(t, err, "применение манифеста с типом, которого сборка не знает")
	require.Truef(t, rep.Changed(), "применение каталог не изменило (%s) — заводить было нечего", rep)
	require.NoError(t, snap.Refresh(ctx), "обновление снимка каталога")
	t.Logf("применение: %s", rep)

	mods, res, verbs := liveCatalogCounts(t, ctx, pool)
	t.Logf("перепись каталога после применения: модулей %d, ресурсов %d, действий %d", mods, res, verbs)

	// (1) ПРОЕКЦИЯ ПОЛНА — предмет #1816.
	const roleID = "rol-dod1-applied"
	pairs := declareRole(t, ctx, pool, repo, snap.Facts(),
		roleID, tn.accountID, applierProbeModule, verdictAppliedRes)
	require.NotEmptyf(t, pairs,
		"проекция по заведённому типу %q пуста: строки записаны, роль создалась без отказа, "+
			"а арендатор не получил бы НИЧЕГО — это дословно блокатор, названный эпиком #1027",
		verdictAppliedDotted)
	require.Lenf(t, pairs, len(verdictProbeVerbs),
		"проекция по %q неполна: объявлено глаголов %d, в проекции %d (%v)",
		verdictAppliedDotted, len(verdictProbeVerbs), len(pairs), verbsOf(pairs))
	t.Logf("проекция: тип %q → %q, пар %d, глаголы %v",
		verdictAppliedDotted, verdictAppliedModel, len(pairs), verbsOf(pairs))

	grantOnProject(t, ctx, pool, tn, "acb-dod1-applied", roleID, verdictAppliedModel, "alpha-dod1")

	// (2) ОТКАЗ НАЗЫВАЕТ ОСНОВАНИЕ — либо права выданы.
	got, grounds, aerr := askVerdict(t, ctx, pool, tn.granted, verdictAppliedModel, "alpha-dod1", "v_get")
	require.NoErrorf(t, aerr,
		"вердикт по типу, которого сборка не знает, НЕ ВЫЧИСЛЕН: %v. Это третий исход — "+
			"«не выполнилось», а не ответ: вызывающий не может отличить сбой от отказа", aerr)
	switch {
	case got == relverdict.Allow:
		t.Logf("РАЗРЫВ ЗАКРЫТ: тип, которого сборка не знает, доезжает до вердикта (%v). "+
			"Снимите вторую половину этой пробы и закройте #1969 — она стала вакуумной", got)
	default:
		require.Equalf(t, relverdict.Deny, got, "неожиданный исход вердикта: %v", got)
		require.Truef(t, grounds.TypeNotDeclared,
			"ТИХИЙ ОТКАЗ: вердикт по %q даёт %v, не назвав основания. Тогда «право не выдано» "+
				"и «модель этого типа не знает» отвечают одинаково — держатель права ищет "+
				"причину в правах и не находит её никогда",
			verdictAppliedModel, got)
		t.Logf("ГРАНИЦА (#1969): проекция полна (пар %d, глаголы %v), вердикт по %q = %v, "+
			"основание названо: тип не объявлен моделью=%t. Звено — relverdict.sourcesOf → "+
			"authzmodel.Shared(): модель вшита сборкой, применение манифеста блока не приносит",
			len(pairs), verbsOf(pairs), verdictAppliedModel, got, grounds.TypeNotDeclared)
	}

	// ОТРИЦАНИЕ рядом: субъект без выдачи. Оно осмысленно в обоих мирах — и пока
	// граница стоит, и после её снятия.
	bare, _, bareErr := askVerdict(t, ctx, pool, tn.bare, verdictAppliedModel, "alpha-dod1", "v_get")
	require.NoError(t, bareErr, "вердикт по субъекту без выдачи не вычислен")
	require.Equal(t, relverdict.Deny, bare, "субъект БЕЗ выдачи получил право")
}
