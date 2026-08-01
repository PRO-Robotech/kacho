// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// renderguard_test.go — офлайновый страж рендера чарта обязан ИСПОЛНЯТЬСЯ.
//
// Такой страж не требует кластера: только `helm template` и утверждения о том,
// что провязка межсервисного ребра действительно попала в манифест (адрес соседа
// в config.yaml, serverName для mTLS, отсутствие мёртвого хоста). Он и написан
// затем, чтобы ловить провязку до деплоя.
//
// Написан — и не вызывался НИКЕМ. У kacho-nlb цель `helm-render-guard`
// существовала, но не встречалась ни в одном конвейере и не была ничьей
// зависимостью; у kacho-vpc страж лежал в дереве и вообще не имел цели. То есть
// оба стража класса были мёртвыми, а с виду — на месте: файл есть, содержимое
// осмысленное, в Makefile строка. Класс уже случался в этом дереве: набор
// манифест-проверок deploy/tests/helm существовал и не звался, около двух тысяч
// строк, и первый же прогон нашёл в них неправду.
//
// Отдельным экземпляром это не чинится: следующий страж рендера, добавленный по
// образцу соседа, унаследует ту же немоту. Поэтому здесь проверяется СВОЙСТВО —
// каждый найденный в дереве страж достижим из конвейера.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// renderGuardName — предикат класса: офлайновый страж рендера сервисного чарта.
//
// Именно `services/*/deploy/**/render-guard.sh`, а не «любой скрипт со словом
// test»: у набора deploy/tests/helm свой запускающий (цель helm-manifest-test
// проходит их маской, и она из конвейера зовётся), у прогонов newman и k6 — свой
// (они требуют стенда либо нагрузочного окна). Смешать их в один предикат
// значило бы получить гейт, который краснеет на законном и будет снят.
const renderGuardName = "render-guard.sh"

// TestEveryOfflineRenderGuardIsInvoked — ни один страж рендера не остаётся немым.
func TestEveryOfflineRenderGuardIsInvoked(t *testing.T) {
	root := repoRoot(t)

	guards := findRenderGuards(t, root)
	// Предпосылка: предикат обязан находить предмет. Обход, переставший доходить
	// до стражей (каталог переименовали, файл назвали иначе), выходит ЗЕЛЁНЫМ на
	// пустом множестве — ровно тот класс, который здесь искореняют.
	if len(guards) == 0 {
		t.Fatal("в дереве не найдено НИ ОДНОГО " + renderGuardName + " под services/*/deploy — " +
			"это не «все провязаны», а слепой обход: гейт на пустом множестве зелен всегда. " +
			"Почини предикат либо, если стражей рендера больше нет, удали этот гейт вместе с ним.")
	}

	reachable := reachableFromWorkflows(t, root)
	t.Logf("осмотрено стражей рендера: %d; команд, достижимых из конвейеров: %d",
		len(guards), len(reachable))

	for _, g := range guards {
		if !anyMentions(reachable, g) {
			t.Errorf("страж рендера %s не вызывается НИЧЕМ: его цели нет ни в одном конвейере "+
				"и она не является ничьей зависимостью. Утверждения о провязке существуют как "+
				"текст и ни разу не проверялись — это ровно то, против чего страж написан.\n"+
				"Провяжи его: шаг в .github/workflows/ci.yaml (job `helm`, там уже пришпилен helm) "+
				"либо цель, которую этот шаг зовёт.", g)
		}
	}
}

// findRenderGuards собирает стражей рендера сервисных чартов.
func findRenderGuards(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("не прочитан %s: %v", servicesDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		deploy := filepath.Join(servicesDir, e.Name(), "deploy")
		err := filepath.Walk(deploy, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // у сервиса может не быть чарта — это не дефект
			}
			if !info.IsDir() && info.Name() == renderGuardName {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", deploy, err)
		}
	}
	sort.Strings(out)
	return out
}

// makeInvocation — вызов make: и как команда в шаге workflow (`make X`), и как
// рекурсивный вызов внутри рецепта (`$(MAKE) --no-print-directory X`).
//
// Форма `$(MAKE)` была пропущена, и это не мелочь оформления: `dev-prod-up`
// зовёт `assert-rollout-ready`, `assert-production-posture` и
// `assert-outbox-autovacuum` именно так, поэтому три живых гейта читались как
// «не вызывается НИ ОТКУДА». Ложное срабатывание такого рода выключает гейт
// целиком — его снимут при первой встрече, — так что предикат обязан узнавать
// обе формы.
var makeInvocation = regexp.MustCompile(`(?:\$\(MAKE\)|\bmake\b)([^\n;&|]*)`)

// reachableFromWorkflows — текст всех команд, достижимых из конвейеров:
// сами блоки `run:` плюс рецепты make-целей, которые эти блоки зовут, вместе с
// транзитивным замыканием по зависимостям и по вложенным вызовам `$(MAKE)`.
//
// Достижимость считается ТРАНЗИТИВНО намеренно: цели живого стенда
// (assert-production-posture и соседние) конвейер не зовёт по имени — их зовёт
// рецепт цели, которую он зовёт. Предикат «упомянута в workflow» посчитал бы их
// немыми и покрасил бы законное, а такой гейт снимают при первом ложном срабате.
func reachableFromWorkflows(t *testing.T, root string) []string {
	t.Helper()

	makefiles := map[string]map[string]makeTarget{} // каталог Makefile → цели
	loadMakefile := func(dir string) map[string]makeTarget {
		if tg, ok := makefiles[dir]; ok {
			return tg
		}
		tg := parseMakeTargets(filepath.Join(root, dir, "Makefile"))
		makefiles[dir] = tg
		return tg
	}

	var reachable []string
	seen := map[string]bool{}

	var pull func(dir, target string)
	pull = func(dir, target string) {
		key := dir + "\x00" + target
		if seen[key] {
			return
		}
		seen[key] = true
		tg, ok := loadMakefile(dir)[target]
		if !ok {
			return
		}
		reachable = append(reachable, tg.recipe)
		for _, p := range tg.prereqs {
			pull(dir, p)
		}
		// Вложенные вызовы того же Makefile: `$(MAKE) --no-print-directory X`.
		for _, m := range makeInvocation.FindAllStringSubmatch(tg.recipe, -1) {
			for _, word := range strings.Fields(m[1]) {
				if strings.HasPrefix(word, "-") || strings.Contains(word, "=") {
					continue
				}
				pull(dir, word)
			}
		}
	}

	wfDir := filepath.Join(root, ".github", "workflows")
	wfs, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("не прочитан %s: %v", wfDir, err)
	}
	for _, wf := range wfs {
		if wf.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(wfDir, wf.Name()))
		if rerr != nil {
			t.Fatalf("не прочитан workflow %s: %v", wf.Name(), rerr)
		}
		body := string(raw)
		reachable = append(reachable, body)

		for _, m := range makeInvocation.FindAllStringSubmatch(body, -1) {
			args := strings.Fields(m[1])
			dir := workflowWorkingDir(body, wf.Name())
			var targets []string
			for i := 0; i < len(args); i++ {
				switch {
				case args[i] == "-C":
					if i+1 < len(args) {
						dir = args[i+1]
						i++
					}
				case strings.HasPrefix(args[i], "-") || strings.Contains(args[i], "="):
				default:
					targets = append(targets, args[i])
				}
			}
			for _, tgt := range targets {
				pull(dir, tgt)
				// Каталог мог быть задан `working-directory:` на шаге, а не `-C`.
				pull("deploy", tgt)
			}
		}
	}
	if len(reachable) == 0 {
		t.Fatal("из конвейеров не собрано ни одной команды — сломан разбор workflow'ов, " +
			"и тогда любой страж выглядит немым")
	}
	return reachable
}

// workflowWorkingDir — корень по умолчанию для `make` без `-C`. Шаги задают его
// через `working-directory:`, но точная привязка шага к каталогу здесь не нужна:
// цели всё равно добираются и через "deploy" (см. вызывающего), а ложная
// достижимость невозможна — имя цели должно существовать в этом Makefile.
func workflowWorkingDir(body, name string) string {
	if strings.Contains(body, "working-directory: deploy") {
		return "deploy"
	}
	return "."
}

type makeTarget struct {
	prereqs []string
	recipe  string
}

var targetLine = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.\-]*)\s*:(?:[^=]|$)`)

// parseMakeTargets — цели Makefile с их зависимостями и телом рецепта.
func parseMakeTargets(path string) map[string]makeTarget {
	out := map[string]makeTarget{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	cur := ""
	var recipe []string
	flush := func() {
		if cur != "" {
			t := out[cur]
			t.recipe += strings.Join(recipe, "\n")
			out[cur] = t
		}
		recipe = nil
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "\t") {
			recipe = append(recipe, ln)
			continue
		}
		m := targetLine.FindStringSubmatch(ln)
		if m != nil && !strings.HasPrefix(strings.TrimSpace(ln), "#") && !strings.HasPrefix(m[1], ".") {
			flush()
			cur = m[1]
			rest := ln[strings.Index(ln, ":")+1:]
			t := out[cur]
			t.prereqs = append(t.prereqs, strings.Fields(rest)...)
			out[cur] = t
			continue
		}
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		flush()
		cur = ""
	}
	flush()
	return out
}

// anyMentions — упомянут ли скрипт в какой-либо достижимой команде: полным путём
// от корня, путём от каталога своего сервиса, либо маской каталога.
func anyMentions(reachable []string, guard string) bool {
	dir := filepath.ToSlash(filepath.Dir(guard))
	base := filepath.Base(guard)
	candidates := []string{guard, base}
	// `bash deploy/tests/render-guard.sh` из каталога сервиса.
	if i := strings.Index(dir, "/"); i >= 0 {
		candidates = append(candidates, dir[i+1:]+"/"+base)
	}
	for _, cmd := range reachable {
		for _, c := range candidates {
			if strings.Contains(cmd, c) {
				return true
			}
		}
		// Маска каталога: `for t in tests/helm/*-test.sh` и подобное.
		if strings.Contains(cmd, dir+"/*") {
			return true
		}
	}
	return false
}

// TestRenderGuardReachabilityIsSymmetric — предикат достижимости проверен
// инъекцией в обе стороны, на фикстурах.
//
// Без обратной половины «все провязаны» было бы неотличимо от предиката, который
// считает достижимым всё: он бы молчал и на настоящем немом страже.
func TestRenderGuardReachabilityIsSymmetric(t *testing.T) {
	guard := "services/x/deploy/tests/" + renderGuardName
	cases := []struct {
		name      string
		reachable []string
		want      bool
	}{
		{"прямой вызов полным путём", []string{"bash " + guard}, true},
		{"вызов путём от каталога сервиса", []string{"bash deploy/tests/" + renderGuardName}, true},
		{"маска каталога", []string{"for f in services/x/deploy/tests/*.sh; do bash $f; done"}, true},
		{"не вызывается ничем", []string{"make build", "go test ./..."}, false},
		{"упомянут ЧУЖОЙ страж того же имени", []string{"bash services/y/deploy/" + renderGuardName}, true},
		{"пустое множество команд", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := anyMentions(tc.reachable, guard); got != tc.want {
				t.Fatalf("достижим=%v, ожидалось %v", got, tc.want)
			}
		})
	}
}

// TestMakeTargetParserFollowsPrereqsAndSubMake — транзитивность разбора.
//
// Цели живого стенда конвейер не зовёт по имени: их зовёт рецепт цели, которую он
// зовёт. Предикат «упомянута в workflow» покрасил бы их — и первый же ложный
// срабат отключил бы гейт целиком.
func TestMakeTargetParserFollowsPrereqsAndSubMake(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"entry: dep",
		"\t@$(MAKE) --no-print-directory nested",
		"",
		"dep:",
		"\t@bash from-prereq.sh",
		"",
		"nested:",
		"\t@bash from-submake.sh",
		"",
		"unrelated:",
		"\t@bash never-called.sh",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tg := parseMakeTargets(filepath.Join(dir, "Makefile"))
	for _, want := range []string{"entry", "dep", "nested", "unrelated"} {
		if _, ok := tg[want]; !ok {
			t.Fatalf("цель %q не разобрана; разобрано: %v", want, keysOf(tg))
		}
	}
	if got := tg["entry"].prereqs; len(got) != 1 || got[0] != "dep" {
		t.Fatalf("зависимости entry: %v, ожидалась [dep]", got)
	}
	if !strings.Contains(tg["nested"].recipe, "from-submake.sh") {
		t.Fatalf("рецепт nested не разобран: %q", tg["nested"].recipe)
	}
	if !strings.Contains(tg["unrelated"].recipe, "never-called.sh") {
		t.Fatalf("рецепт unrelated не разобран: %q", tg["unrelated"].recipe)
	}
}

func keysOf(m map[string]makeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
