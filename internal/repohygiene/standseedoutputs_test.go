// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// ВЫХОД ПОСЕВА СТЕНДА НЕ ПАЧКАЕТ ДЕРЕВО И НЕ ЕДЕТ В КОНТЕКСТ СБОРКИ (kacho#3026).
//
// # Признак
//
// Посевы стенда (`deploy/scripts/seed-*.sh`) пишут идентификаторы для
// следующих прогонов в файл по умолчанию `<корень дерева>/.seeded-ids.env`.
// Исключения стояли на другой координате — `deploy/.seeded-ids.env` в
// `.dockerignore` и `.seeded-ids.env` в `deploy/.gitignore` (он покрывает только
// `deploy/`), — поэтому файл в корне был для git неотслеживаемым, но НЕ
// игнорируемым. Следствие измерено на стенде own: первый `make own-up` пишет
// файл, второй собирает образы с ревизией `<коммит>-dirty`, у КАЖДОГО образа
// меняется идентификатор содержимого, и все поды стенда перекатываются при
// неизменном дереве. Соседний гейт (buildcontextguard_test.go) этого не видит
// по построению: его единица — файл, который git ИГНОРИРУЕТ.
//
// # Предикат
//
// Единица — объявленный выход посева: присваивание вида
// `OUT_FILE="${OUT_FILE:-$REPO_ROOT/<путь>}"` в скрипте `deploy/scripts/`.
// Каждый такой путь обязан быть игнорируемым для git (`git check-ignore`) и
// исключённым `.dockerignore`. Ноль найденных объявлений — отказ: форма
// объявления сменилась, и молчание значило бы «не посмотрели».

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

var seedOutFileDecl = regexp.MustCompile(`OUT_FILE="\$\{OUT_FILE:-\$REPO_ROOT/([^}"]+)\}"`)

// standSeedOutputs — путь выхода → скрипты, его объявляющие.
func standSeedOutputs(t *testing.T, root string) map[string][]string {
	t.Helper()
	scripts, err := filepath.Glob(filepath.Join(root, "deploy", "scripts", "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("скриптов в deploy/scripts не найдено (%v) — предмет исчез, а не стал чистым", err)
	}
	out := map[string][]string{}
	for _, s := range scripts {
		body, err := os.ReadFile(s) // #nosec G304 -- скрипт этого дерева
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		for _, m := range seedOutFileDecl.FindAllStringSubmatch(string(body), -1) {
			out[m[1]] = append(out[m[1]], filepath.Base(s))
		}
	}
	return out
}

// judgeSeedOutputs — находки: выход, который git не игнорирует либо docker берёт
// в контекст. ignored — ответ git о каждом пути.
func judgeSeedOutputs(outputs map[string][]string, ignored func(string) bool, dockerPats []string) []string {
	paths := make([]string, 0, len(outputs))
	for p := range outputs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var findings []string
	for _, p := range paths {
		who := strings.Join(outputs[p], ", ")
		if !ignored(p) {
			findings = append(findings, p+" (пишет "+who+"): git его НЕ игнорирует — первый подъём стенда "+
				"пачкает дерево, и следующий собирает образы с ревизией «-dirty»: новые идентификаторы, "+
				"перекат всех подов при неизменном исходнике")
		}
		if !dockerIgnoreExcludes(dockerPats, p) {
			findings = append(findings, p+" (пишет "+who+"): .dockerignore его не исключает — "+
				"файл, записанный посевом, едет в контекст сборки и сбивает кэш слоёв")
		}
	}
	return findings
}

func TestStandSeedOutputsAreIgnoredByGitAndDocker(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	outputs := standSeedOutputs(t, root)
	if len(outputs) == 0 {
		t.Fatal("ни одного объявления OUT_FILE=\"${OUT_FILE:-$REPO_ROOT/…}\" в deploy/scripts — " +
			"форма объявления выхода посева сменилась, и суд не видит предмета")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatalf(".dockerignore не прочитан: %v", err)
	}
	ignored := func(p string) bool {
		cmd := gitenv.Command(root, "check-ignore", "-q", "--no-index", p)
		return cmd.Run() == nil
	}
	findings := judgeSeedOutputs(outputs, ignored, dockerIgnorePatterns(string(raw)))
	t.Logf("перепись: выходов посева %d, находок %d", len(outputs), len(findings))
	for _, f := range findings {
		t.Error(f)
	}
}

func TestStandSeedOutputJudgement_CanFailAndStaysSilent(t *testing.T) {
	t.Parallel()
	out := map[string][]string{".seeded-ids.env": {"seed-x.sh"}}
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	if f := judgeSeedOutputs(out, yes, []string{".seeded-ids.env"}); len(f) != 0 {
		t.Fatalf("близнец: игнорируемый и исключённый выход объявлен находкой: %v", f)
	}
	if f := judgeSeedOutputs(out, no, []string{".seeded-ids.env"}); len(f) != 1 || !strings.Contains(f[0], "git его НЕ игнорирует") {
		t.Fatalf("инъекция git: ожидалась одна находка о неигнорируемом выходе, получено %v", f)
	}
	// Прежняя координата исключения — ровно дефект kacho#3026.
	if f := judgeSeedOutputs(out, yes, []string{"deploy/.seeded-ids.env"}); len(f) != 1 || !strings.Contains(f[0], ".dockerignore") {
		t.Fatalf("инъекция docker: исключение на чужой координате не найдено, получено %v", f)
	}
}
