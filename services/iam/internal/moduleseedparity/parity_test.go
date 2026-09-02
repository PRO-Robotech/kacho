// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// parity_test.go — держатель второй половины #1891: манифест модуля ОБЪЯВЛЯЕТ
// посев, который у модуля есть, и объявление сходится с живой базой.
//
// # Почему прогон против базы, а не разбор миграций
//
// Так требует предикат снятия задачи, и требует по существу. Действующий посев
// есть НАЛОЖЕНИЕ применённых миграций: запись, заведённая одной, снимается
// другой (служебная запись сетевого оператора была заведена и снята, и в базе
// её нет). Разбор SQL — распознаватель: форму записи, которой он не знает, он
// пропускает МОЛЧА, и его молчание неотличимо от согласия. Здесь миграции
// исполняются, а строки читаются оттуда, где лежат.
//
// # Почему НЕ общий стенд
//
// Общий стенд отстаёт от линии и несёт данные чужих прогонов; вердикт по нему
// был бы вердиктом о ЧУЖОМ дереве. База поднимается из миграций ЭТОГО дерева.
//
// # Границу сверки задаёт ФОРМА, и она проверяется отдельной пробой
//
// Сверяются служебные записи и вступления; группы и выдачи формой сегодня
// невыразимы (разбор — в шапке пакета). Это не послабление и не ведомость:
// проба предпосылки требует, чтобы у выдачи по-прежнему не было ключа для
// отношения, и краснеет в тот прогон, когда ключ появится.
package moduleseedparity_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/platformmodules"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/moduleseedparity"
)

// liveServiceAccountFloor — служебных записей в базе, ниже которого чтение
// беспредметно. Их после миграций семь; обвал до нуля означает, что запрос
// перестал видеть предмет, и молчание гейта сказано ни о чём.
const liveServiceAccountFloor = 3

// serviceAccountNamePrefix — по этому написанию живая запись переводится в
// модуль-владелец: `kacho-<служба>`, служба — из словаря платформы.
const serviceAccountNamePrefix = "kacho-"

// TestModuleManifestDeclaresTheSeedTheLiveBaseHolds — сам гейт.
func TestModuleManifestDeclaresTheSeedTheLiveBaseHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("нужен Postgres: вердикт этого гейта даёт ПРОГОН против живой базы, " +
			"а не разбор миграций")
	}
	ctx := context.Background()
	root := repoRoot(t)

	states, census := moduleStates(ctx, t, root)

	// Перепись — ДО всякого вердикта и независимо от него.
	t.Logf("перепись: %s", census)
	for _, st := range states {
		t.Logf("  модуль %-13s записей объявлено %d · живых %d · вступлений объявлено %d · живых %d · манифест %s",
			st.Module, len(st.DeclaredSA), len(st.LiveSA),
			len(st.DeclaredJoin), len(st.LiveJoin), st.ManifestFile)
	}

	require.NotZero(t, census.Manifests,
		"манифестов модулей прочитано ноль — каталог переехал, и гейт стережёт координату, "+
			"которой больше нет")
	require.GreaterOrEqual(t, census.LiveSA, liveServiceAccountFloor,
		"служебных записей прочитано %d при пороге %d — чтение перестало видеть предмет",
		census.LiveSA, liveServiceAccountFloor)
	require.NotZero(t, census.LiveJoin,
		"вступлений прочитано ноль — чтение членства перестало видеть предмет")

	if findings := moduleseedparity.Diff(states); len(findings) > 0 {
		t.Fatalf("раздел `seed` расходится с живой базой — %d место(а):\n  %s\n\n"+
			"Снятие: объявить `seed` модуля так, чтобы он сходился со строками, которые уже "+
			"лежат в базе (#1891). Группы и выдачи сюда НЕ входят: их форма сегодня выразить "+
			"не может — см. шапку пакета и #1936.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestBindingFormStillCannotExpressARelationGrant — ПРОБА ПРЕДПОСЫЛКИ гейта.
//
// Сверка выше намеренно не судит `seed.groups` и `seed.accessBindings`, и
// основание у этого одно: форма выдачи не несёт ключа, которым выдаётся
// ОТНОШЕНИЕ, а все живые выдачи посева — именно такие. Основание есть
// утверждение о дереве, и оно обязано истечь само: появится ключ — эта проба
// покраснеет и потребует расширить сверку, а не оставит слепую зону молча.
func TestBindingFormStillCannotExpressARelationGrant(t *testing.T) {
	keys := yamlKeysOf(reflect.TypeOf(manifest.AccessBinding{}))
	require.NotEmpty(t, keys, "у формы выдачи не прочитано ни одного ключа — разбор тегов сломан")
	t.Logf("ключи формы выдачи: %s", strings.Join(keys, " "))

	for _, k := range keys {
		require.NotContainsf(t, strings.ToLower(k), "relation",
			"у выдачи появился ключ %q: форма научилась выражать выдачу ОТНОШЕНИЕМ, а сверка "+
				"посева про такие строки по-прежнему молчит. Расширьте moduleseedparity на "+
				"`seed.accessBindings` и `seed.groups` — и снимите эту пробу вместе с её "+
				"предметом (#1891, #1936)", k)
	}
	require.Containsf(t, keys, "roleId",
		"у выдачи пропал ключ roleId — основание границы сверки описывает форму, которой "+
			"больше нет")
}

func yamlKeysOf(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("yaml"); tag != "" {
			out = append(out, strings.Split(tag, ",")[0])
		}
	}
	sort.Strings(out)
	return out
}

// moduleStates — обе стороны сверки по каждому модулю.
func moduleStates(ctx context.Context, t *testing.T, root string) (
	[]moduleseedparity.ModuleState, moduleseedparity.Census,
) {
	t.Helper()

	pool, err := pgxpool.New(ctx, pgtest.NewDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	liveSA, saByOwner, ownerlessSA := readLiveServiceAccounts(ctx, t, pool)
	liveJoin, joinByOwner, ownerlessJoin := readLiveJoins(ctx, t, pool, saByOwner)
	liveGroups, expressibleGroups, liveBindings, expressibleBindings := readFormBoundary(ctx, t, pool)

	census := moduleseedparity.Census{
		LiveSA: liveSA, OwnerlessSA: ownerlessSA,
		LiveJoin: liveJoin, OwnerlessJoin: ownerlessJoin,
		LiveGroups: liveGroups, ExpressibleGroups: expressibleGroups,
		LiveBindings: liveBindings, ExpressibleBindings: expressibleBindings,
	}

	var (
		states  []moduleseedparity.ModuleState
		claimed = map[string]bool{}
	)
	for _, file := range manifestFiles(t, root) {
		// #nosec G304 -- путь получен обходом каталога сервисов ЭТОГО репозитория
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		require.NoErrorf(t, rerr, "манифест %s не прочитан", file)

		m, lerr := manifest.Load(src)
		require.NoErrorf(t, lerr, "манифест %s не разобран: сверять нечем", file)

		census.Manifests++
		sa, joins := declaredSeed(m)
		census.DeclaredSA += len(sa)
		census.DeclaredJoin += len(joins)
		claimed[m.Module] = true

		states = append(states, moduleseedparity.ModuleState{
			Module:       m.Module,
			ManifestFile: file,
			DeclaredSA:   sa,
			LiveSA:       saByOwner[m.Module],
			DeclaredJoin: joins,
			LiveJoin:     joinByOwner[m.Module],
		})
	}
	// Модуль закрытого набора, у которого живой посев есть, а манифеста нет,
	// молчал бы иначе: его строки не попали бы ни в одно состояние.
	for _, mod := range domain.KnownModules() {
		if claimed[mod] || (len(saByOwner[mod]) == 0 && len(joinByOwner[mod]) == 0) {
			continue
		}
		states = append(states, moduleseedparity.ModuleState{
			Module:       mod,
			ManifestFile: "(манифеста в дереве нет)",
			LiveSA:       saByOwner[mod],
			LiveJoin:     joinByOwner[mod],
		})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Module < states[j].Module })
	return states, census
}

// declaredSeed — сторона манифеста.
func declaredSeed(m *manifest.Manifest) ([]moduleseedparity.ServiceAccount, []moduleseedparity.Join) {
	if m == nil || m.Seed == nil {
		return nil, nil
	}
	sa := make([]moduleseedparity.ServiceAccount, 0, len(m.Seed.ServiceAccounts))
	for _, s := range m.Seed.ServiceAccounts {
		sa = append(sa, moduleseedparity.ServiceAccount{
			Account: s.Account, Name: s.Name, Description: s.Description,
		})
	}
	joins := make([]moduleseedparity.Join, 0, len(m.Seed.Joins))
	for _, j := range m.Seed.Joins {
		joins = append(joins, moduleseedparity.Join{
			AccountName:  j.ServiceAccount.Account,
			SAName:       j.ServiceAccount.Name,
			GroupAccount: j.Group.Account,
			GroupName:    j.Group.Name,
		})
	}
	return sa, joins
}

// readLiveServiceAccounts читает служебные записи живой базы и раскладывает их
// по модулю-владельцу — имени `kacho-<служба>`.
func readLiveServiceAccounts(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (
	total int, byOwner map[string][]moduleseedparity.ServiceAccount, ownerless int,
) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT a.name, sa.name, sa.description
		   FROM kacho_iam.service_accounts sa
		   JOIN kacho_iam.accounts a ON a.id = sa.account_id
		  ORDER BY a.name, sa.name`)
	require.NoError(t, err)
	defer rows.Close()

	byOwner = map[string][]moduleseedparity.ServiceAccount{}
	for rows.Next() {
		var account, name, description string
		require.NoError(t, rows.Scan(&account, &name, &description))
		total++

		owner, ok := ownerOfServiceAccount(name)
		if !ok {
			ownerless++
			continue
		}
		byOwner[owner] = append(byOwner[owner], moduleseedparity.ServiceAccount{
			Account: account, Name: name, Description: description,
		})
	}
	require.NoError(t, rows.Err())
	return total, byOwner, ownerless
}

// ownerOfServiceAccount — модуль-владелец записи по её имени.
func ownerOfServiceAccount(name string) (string, bool) {
	service, ok := strings.CutPrefix(name, serviceAccountNamePrefix)
	if !ok {
		return "", false
	}
	module, ok := platformmodules.CatalogModuleOfService(service)
	if !ok || !domain.IsKnownModule(module) {
		return "", false
	}
	return module, true
}

// readLiveJoins читает членство живой базы и раскладывает его по владельцу
// ВСТУПАЮЩЕЙ записи: членство заявляет вступающий, а не владелец группы.
func readLiveJoins(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	saByOwner map[string][]moduleseedparity.ServiceAccount,
) (total int, byOwner map[string][]moduleseedparity.Join, ownerless int) {
	t.Helper()
	_ = saByOwner
	rows, err := pool.Query(ctx,
		`SELECT sa_acc.name, sa.name, grp_acc.name, g.name
		   FROM kacho_iam.group_members gm
		   JOIN kacho_iam.groups g ON g.id = gm.group_id
		   JOIN kacho_iam.accounts grp_acc ON grp_acc.id = g.account_id
		   JOIN kacho_iam.service_accounts sa ON sa.id = gm.member_id
		   JOIN kacho_iam.accounts sa_acc ON sa_acc.id = sa.account_id
		  WHERE gm.member_type = 'service_account'
		  ORDER BY sa.name, g.name`)
	require.NoError(t, err)
	defer rows.Close()

	byOwner = map[string][]moduleseedparity.Join{}
	for rows.Next() {
		var j moduleseedparity.Join
		require.NoError(t, rows.Scan(&j.AccountName, &j.SAName, &j.GroupAccount, &j.GroupName))
		total++

		owner, ok := ownerOfServiceAccount(j.SAName)
		if !ok {
			ownerless++
			continue
		}
		byOwner[owner] = append(byOwner[owner], j)
	}
	require.NoError(t, rows.Err())
	return total, byOwner, ownerless
}

// readFormBoundary считает ГРАНИЦУ ФОРМЫ обеими величинами: сколько строк
// живёт и сколько из них форма манифеста способна выразить. Одно число здесь
// скрывало бы ровно тот случай, ради которого граница названа.
func readFormBoundary(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (
	liveGroups, expressibleGroups, liveBindings, expressibleBindings int,
) {
	t.Helper()
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.groups`).Scan(&liveGroups))
	// Выдача выразима формой, когда она выдаёт РОЛЬ и её субъект — из тех, кого
	// заводит посев модуля. Выдача отношением ключа в форме не имеет вовсе.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_bindings`).Scan(&liveBindings))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_bindings
		  WHERE COALESCE(role_id, '') <> ''
		    AND subject_type IN ('service_account', 'group')`).Scan(&expressibleBindings))
	// Группа выразима, только если её называет выдача, выразимая формой:
	// валидатор связности требует, чтобы заведённая группа была кому-то выдана.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.groups g
		  WHERE EXISTS (
		        SELECT 1 FROM kacho_iam.access_bindings ab
		         WHERE ab.subject_id = g.id
		           AND ab.subject_type = 'group'
		           AND COALESCE(ab.role_id, '') <> '')`).Scan(&expressibleGroups))
	return liveGroups, expressibleGroups, liveBindings, expressibleBindings
}

// manifestFiles — манифесты модулей, ВЫВЕДЕННЫЕ обходом каталога сервисов.
func manifestFiles(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	require.NoError(t, err)

	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("services", e.Name(), "manifest.yaml"))
		if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); serr == nil {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// repoRoot — корень монорепо: ближайший вверх каталог с go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqualf(t, parent, dir, "go.mod не найден выше %s", dir)
		dir = parent
	}
}
