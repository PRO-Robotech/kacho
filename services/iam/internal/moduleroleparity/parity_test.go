// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// parity_test.go — держатель #1891: манифест модуля ОБЪЯВЛЯЕТ системные роли,
// которые у модуля есть, и объявление сходится с живой базой.
//
// # Почему прогон против базы, а не разбор миграций
//
// Так требует предикат снятия задачи, и требует по существу. Действующее
// состояние ролей есть НАЛОЖЕНИЕ применённых миграций: ранние отменяются
// поздними, правила переписываются под словари каталога и глаголов. Разбор SQL
// — распознаватель: форму записи, которой он не знает, он пропускает МОЛЧА, и
// его молчание неотличимо от согласия. Здесь миграции исполняются, а строки
// читаются оттуда, где лежат.
//
// # Почему НЕ общий стенд
//
// Общий стенд отстаёт от линии (его старшая применённая миграция старше дерева)
// и несёт данные чужих прогонов. Вердикт по нему был бы вердиктом о ЧУЖОМ
// дереве. Здесь база поднимается из миграций ЭТОГО дерева, и вердикт
// воспроизводим.
//
// # Сторону манифеста производит ПРИМЕНИТЕЛЬ
//
// Не копия его правил: копия разошлась бы с оригиналом молча — обе стороны
// отвечают одинаково на законном входе. Применитель зовётся настоящий, а
// писатель подставлен ЗАПОМИНАЮЩИЙ. Подделка структурно неспособна дать
// зелёное сама по себе: она ничего не утверждает, она лишь отдаёт то, что
// применитель ей передал, — вердикт выносит сравнение с базой.
//
// # Ведомость #1891 ИСТЕКАЕТ САМА
//
// Пока задача не закрыта, модуль, чьи роли ещё не объявлены, стоит в ведомости
// ниже. Запись, чей модуль уже объявил роли, — находка. Запись модуля без живых
// ролей — тоже находка. На ПУСТОЙ ведомости гейт проходит: пустая ведомость и
// есть цель, а падение на достижении цели толкало бы держать запись ради
// зелёного.
package moduleroleparity_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/moduleroles"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/moduleroleparity"
)

// liveRoleFloor — системных ролей кластерного яруса, ниже которого чтение
// беспредметно. Их в базе после миграций сорок с лишним; обвал до единиц
// означает, что запрос перестал видеть предмет, и молчание гейта сказано ни о
// чём.
const liveRoleFloor = 20

// postponedModules — ВЕДОМОСТЬ #1891: модули, чьи живые роли ещё не объявлены
// манифестом.
//
// Ведомость точная, а не потолок: потолок не покраснел бы никогда и потому не
// истёк бы. Каждая запись снимается ТЕМ ЖЕ изменением, которым модуль объявляет
// свои роли, — иначе гейт скажет об этом сам.
//
// # У откладывания ОДНА причина на все три записи, и она не «руки не дошли»
//
// Загрузчик манифеста требует от `roles[].description` не менее шестнадцати
// знаков (`ErrRoleDescriptionTooShort`), а применённые миграции написали живым
// строкам от девяти до пятнадцати. Применитель кладёт назначение ДОСЛОВНО,
// поэтому объявить такую роль манифестом нельзя ни при каком написании: длинный
// текст разошёлся бы со строкой, короткий не прошёл бы разбор. Замер по живой
// базе: ролей с модулем-владельцем 42, из них короче предела 27 — compute 3 из
// 3, iam 10 из 19, vpc 14 из 18, loadbalancer 0 из 2. Именно поэтому объявлен
// оказался ровно тот модуль, чьи описания предел уже проходят.
//
// Снятие каждой записи требует РЕШЕНИЯ о живом тексте, а не одной правки YAML:
// назначение читает арендатор, выбирая роль. Предмет заведён задачей продукта
// #1904 и назван здесь, чтобы ведомость не читалась как отсрочка без основания.
var postponedModules = []moduleroleparity.Postponement{
	{
		Module: "compute",
		Why: "все 3 живые роли несут назначение короче предела прозы манифеста " +
			"(13, 13, 14 знаков при 16) — объявление невыразимо, пока текст не приведён; #1904",
	},
	{
		Module: "iam",
		Why: "10 из 19 живых ролей несут назначение короче предела прозы манифеста " +
			"(от 9 знаков при 16) — объявление невыразимо, пока текст не приведён; #1904",
	},
	{
		Module: "vpc",
		Why: "14 из 18 живых ролей несут назначение короче предела прозы манифеста " +
			"(от 11 знаков при 16) — объявление невыразимо, пока текст не приведён; #1904",
	},
}

// TestModuleManifestDeclaresTheSystemRolesTheLiveBaseHolds — сам гейт.
func TestModuleManifestDeclaresTheSystemRolesTheLiveBaseHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("нужен Postgres: вердикт этого гейта даёт ПРОГОН против живой базы, " +
			"а не разбор миграций")
	}
	ctx := context.Background()
	root := repoRoot(t)

	states, census := moduleStates(ctx, t, root)
	census.Postponed = len(postponedModules)

	// Перепись — ДО всякого вердикта и независимо от него.
	t.Logf("перепись: %s", census)
	for _, st := range states {
		t.Logf("  модуль %-13s объявлено %2d · живых %2d · манифест %s",
			st.Module, len(st.Declared), len(st.Live), st.ManifestFile)
	}

	require.NotZero(t, census.Manifests,
		"манифестов модулей прочитано ноль — каталог переехал, и гейт стережёт координату, "+
			"которой больше нет")
	require.GreaterOrEqual(t, census.Live, liveRoleFloor,
		"системных ролей прочитано %d при пороге %d — чтение перестало видеть предмет, "+
			"и его молчание сказано ни о чём", census.Live, liveRoleFloor)

	if findings := moduleroleparity.Diff(states, postponedModules); len(findings) > 0 {
		t.Fatalf("манифест расходится с живой базой — %d место(а):\n  %s\n\n"+
			"Снятие: объявить раздел `roles` модуля так, чтобы он сходился со строками, "+
			"которые уже лежат в базе, — либо, пока это не сделано, назвать модуль в "+
			"ведомости postponedModules с причиной и предикатом снятия (#1891).",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// moduleStates — обе стороны сверки по каждому модулю с манифестом.
func moduleStates(ctx context.Context, t *testing.T, root string) (
	[]moduleroleparity.ModuleState, moduleroleparity.Census,
) {
	t.Helper()

	pool, err := pgxpool.New(ctx, pgtest.NewDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	liveByOwner, ownerless, live := readLiveSystemRoles(ctx, t, pool)

	var (
		states  []moduleroleparity.ModuleState
		census  = moduleroleparity.Census{Live: live, Ownerless: ownerless}
		claimed = map[string]bool{}
	)
	for _, file := range manifestFiles(t, root) {
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		require.NoErrorf(t, rerr, "манифест %s не прочитан", file)

		m, lerr := manifest.Load(src)
		require.NoErrorf(t, lerr, "манифест %s не разобран: сверять нечем", file)

		census.Manifests++
		declared := declaredRoles(ctx, t, m)
		census.Declared += len(declared)
		claimed[m.Module] = true

		states = append(states, moduleroleparity.ModuleState{
			Module:       m.Module,
			ManifestFile: file,
			Declared:     declared,
			Live:         liveByOwner[m.Module],
		})
	}
	// Модуль закрытого набора, у которого живые роли есть, а манифеста нет,
	// молчал бы иначе: его строки не попали бы ни в одно состояние.
	for _, mod := range domain.KnownModules() {
		if claimed[mod] || len(liveByOwner[mod]) == 0 {
			continue
		}
		states = append(states, moduleroleparity.ModuleState{
			Module:       mod,
			ManifestFile: "(манифеста в дереве нет)",
			Live:         liveByOwner[mod],
		})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Module < states[j].Module })
	return states, census
}

// readLiveSystemRoles читает системные роли ЖИВОЙ базы и раскладывает их по
// модулю-владельцу — первому сегменту имени.
//
// Роль, чей первый сегмент не член закрытого набора модулей (`admin`, `edit`,
// `view`, `owner`, `kacho-system.*`), манифестом невыразима by construction:
// объявить её нечему, и находкой она быть не может. Такие считаются отдельно —
// иначе их отсутствие среди объявленных читалось бы как неполнота.
func readLiveSystemRoles(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (
	byOwner map[string][]moduleroleparity.Role, ownerless, total int,
) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT id, name, description, rules
		   FROM kacho_iam.roles
		  WHERE cluster_id IS NOT NULL
		  ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	byOwner = map[string][]moduleroleparity.Role{}
	for rows.Next() {
		var (
			id, name, description string
			raw                   []byte
		)
		require.NoError(t, rows.Scan(&id, &name, &description, &raw))
		total++

		rules, derr := domain.DecodeRules(raw)
		require.NoErrorf(t, derr, "правила роли %q не разобраны кодеком домена", name)

		owner, _, hasDot := strings.Cut(name, ".")
		if !hasDot || !domain.IsKnownModule(owner) {
			ownerless++
			continue
		}
		byOwner[owner] = append(byOwner[owner], moduleroleparity.Role{
			ID: id, Name: name, Description: description, Rules: rules,
		})
	}
	require.NoError(t, rows.Err())
	return byOwner, ownerless, total
}

// declaredRoles — сторона манифеста, произведённая НАСТОЯЩИМ применителем.
func declaredRoles(ctx context.Context, t *testing.T, m *manifest.Manifest) []moduleroleparity.Role {
	t.Helper()
	rec := &recordingTx{}
	rep, err := moduleroles.NewApplier(rec).Apply(ctx, m)
	require.NoErrorf(t, err, "применитель отверг манифест модуля %s: объявление негодно ДО базы",
		m.Module)
	require.Equalf(t, rep.Declared, len(rec.written),
		"применитель объявил %d ролей, а писателю передал %d — запоминающий писатель "+
			"перестал видеть то, что видит настоящий", rep.Declared, len(rec.written))

	out := make([]moduleroleparity.Role, 0, len(rec.written))
	for _, r := range rec.written {
		out = append(out, moduleroleparity.Role{
			ID:          string(r.ID),
			Name:        string(r.Name),
			Description: string(r.Description),
			Rules:       r.Rules,
		})
	}
	return out
}

// recordingTx — исполнитель транзакций, который ЗАПОМИНАЕТ вместо записи.
//
// Он не выносит ни одного вердикта и не может дать зелёное сам: всё, что он
// делает, — отдаёт наружу то, что применитель ему передал. Согласие с базой
// устанавливает сравнение, а не он.
type recordingTx struct{ written []domain.Role }

func (r *recordingTx) RunInWriteTx(ctx context.Context,
	fn func(ctx context.Context, w moduleroles.RoleWriter) error,
) error {
	return fn(ctx, r)
}

func (r *recordingTx) UpsertSystemRole(_ context.Context, role domain.Role) (domain.Role, bool, error) {
	r.written = append(r.written, role)
	return role, true, nil
}

func (r *recordingTx) ReplaceRuleRefs(context.Context, domain.RoleID, []domain.RoleRuleRef) error {
	return nil
}

// manifestFiles — манифесты модулей ВЫВОДЯТСЯ обходом дерева, а не выписываются.
//
// Выписанный перечень разошёлся бы с деревом молча в день появления седьмого
// манифеста — и разошёлся бы в сторону молчания: незнакомый файл просто не
// осматривался бы.
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
