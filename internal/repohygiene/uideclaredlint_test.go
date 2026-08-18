// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// uideclaredlint_test.go — объявленная команда консоли ИСПОЛНИМА, и исполняет
// её конвейер.
//
// # Предмет
//
// Разработчику названа одна команда — `npm run lint`, — и она единственное, что
// стоит между правкой и стволом: работа копится в накопительной ветке, а PR
// внутрь неё конвейером не проверяется вовсе. Значит про эту команду обязаны
// быть верны ДВА утверждения, и оба ломались молча.
//
//  1. ОНА ИСПОЛНИМА. `stylelint`, которому шаблон не дал ни одного файла,
//     считает это ОШИБКОЙ (`NoFilesFoundError`), а не «нечего проверять».
//     Набор, который даёт шаблон, — функция дерева: сегодня непуст, завтра пуст.
//     Три модуля консоли объявляли `lint:css` и не несли ни одного файла стиля,
//     то есть их объявленная команда не проходила НИ ПРИ КАКОМ состоянии дерева
//     (#602). Падает средняя ступень цепочки и роняет всю.
//
//  2. ЕЁ ИСПОЛНЯЕТ КОНВЕЙЕР. Пока `ui.yml` звал линтер собственной строкой
//     (`../node_modules/.bin/eslint .`), а объявленную команду не звал вовсе,
//     расхождение «объявлено ≠ исполняется» было невидимо ИМЕННО ТАМ, где его
//     ищут: конвейер зелен, команда не работает. Предикат, которым это
//     измеряется, — `grep -n "npm run lint" .github/workflows/ui.yml` — давал
//     пусто.
//
// # Почему проверка формы, а не прогон
//
// Прогнать `npm run lint` из Go нельзя: нужен установленный набор зависимостей
// одиннадцати пакетов. Но оба свойства — свойства ОБЪЯВЛЕНИЯ, а не прогона:
// первое читается из аргументов вызова, второе — из графа `npm run`. Прогон их
// подтверждает, гейт не даёт им разойтись между прогонами.
//
// # Читается разобранный документ, а не текст
//
// Манифест разбирается как JSON, конвейер — как YAML. В обоих комментария не
// существует как узла, поэтому объяснение правила не может сойти за его
// исполнение. Гейт по подстроке краснел бы на собственном комментарии — этот
// файл называет и `stylelint "src/**/*.{css,scss}"`, и `npm run lint`.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: каждый гейт
// печатает, сколько манифестов прочитал, сколько вызовов разобрал и сколько
// узлов достиг. Пустой обход — провал, а не успех.
//
// # Пустой перечень — цель, а не поломка
//
// Если однажды ни один пакет не станет звать stylelint по шаблону, гейт 1
// пройдёт, объявив перепись: у него не останется предмета, и это правильный
// исход, а не повод держать вызов ради зелёного.
package repohygiene

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// uiWorkflow — конвейер консоли: единственное место, где эти команды исполняет
// не человек.
const uiWorkflow = ".github/workflows/ui.yml"

// uiManifests читает отслеживаемые манифесты консоли.
//
// Единица счёта — элемент индекса git, а не то, что лежит на диске: иначе
// объявление, `.gitignore` и поведение разъезжаются молча (установленный
// `node_modules` несёт тысячи чужих манифестов).
func uiManifests(t *testing.T) (map[string]uiManifest, []string) {
	t.Helper()
	root := repoRoot(t)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открывается: %v", root, err)
	}
	defer func() { _ = r.Close() }()

	byPkg := map[string]uiManifest{}
	var rels []string
	for _, rel := range trackedPaths(t, root) {
		body, ok := readTracked(r, rel)
		if !ok {
			continue
		}
		m, ok := uiParseManifest(rel, body)
		if !ok {
			continue
		}
		byPkg[uiPkgOf(m.Pkg)] = m
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return byPkg, rels
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 1: объявленная команда исполнима при ЛЮБОМ составе дерева.
// ─────────────────────────────────────────────────────────────────────────────

func TestUiDeclaredLintSurvivesAnEmptyMatchSet(t *testing.T) {
	byPkg, rels := uiManifests(t)

	var (
		invocations, patterns int
		findings              []uiEmptyMatchSite
	)
	for _, pkg := range uiSortedKeys(byPkg) {
		i, p, f := uiAuditEmptyMatch(byPkg[pkg])
		invocations += i
		patterns += p
		findings = append(findings, f...)
	}

	census := "перепись: прочитано манифестов " + strconv.Itoa(len(rels)) +
		", вызовов stylelint найдено " + strconv.Itoa(invocations) +
		", из них по шаблону " + strconv.Itoa(patterns)
	t.Log(census)

	// Предпосылка гейта: ему было что читать. Ноль манифестов означает, что
	// прочитано не то дерево либо предикат перестал их узнавать, — и тогда
	// «находок нет» неотличимо от «ничего не прочитано».
	if len(rels) == 0 {
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: не прочитано НИ ОДНОГО манифеста под %s/. %s", uiRoot, census)
	}
	// Ноль вызовов по шаблону находкой НЕ является: предмета не стало — гейту
	// нечего запрещать. Перепись выше это и объявляет.

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.Rel + " → scripts." + f.Script + ": `" + f.Command + "`" +
				"\n      шаблон " + f.Pattern + " может не дать ни одного файла, и тогда stylelint" +
				" объявит это ОШИБКОЙ, а не «нечего проверять»")
		}
		t.Fatalf("%d объявленн(ых) вызов(ов) падают на пустом наборе файлов. Набор, который даёт "+
			"шаблон, — функция ДЕРЕВА: модуль без файлов стиля делает объявленную команду "+
			"неисполнимой ни при каком его состоянии, а вместе с ней падает вся цепочка `lint`. "+
			"Объяви `--allow-empty-input` — тогда шаблон остаётся прежним и первый же добавленный "+
			"файл стиля проверяется сам, без второго решения.%s\n%s", len(findings), b.String(), census)
	}
}

// uiSortedKeys — устойчивый порядок обхода пакетов.
func uiSortedKeys(m map[string]uiManifest) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 2: объявленную команду исполняет КОНВЕЙЕР.
// ─────────────────────────────────────────────────────────────────────────────

// uiWorkflowDoc — ровно те узлы конвейера, которые нужны этому гейту.
type uiWorkflowDoc struct {
	Defaults struct {
		Run struct {
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"run"`
	} `yaml:"defaults"`
	Jobs map[string]struct {
		Defaults struct {
			Run struct {
				WorkingDirectory string `yaml:"working-directory"`
			} `yaml:"run"`
		} `yaml:"defaults"`
		Steps []struct {
			Run              string `yaml:"run"`
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// uiWorkflowSeeds — узлы, которые конвейер запускает НАПРЯМУЮ.
//
// Каталог запуска берётся по правилу GitHub: у шага, иначе у работы, иначе у
// файла. Он и определяет, чей манифест читает `npm run` без `--prefix`, —
// поэтому назвать его надо, а не предположить.
func uiWorkflowSeeds(t *testing.T, body string) (seeds []uiScriptNode, steps, unresolved int) {
	t.Helper()
	var doc uiWorkflowDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("%s: конвейер не разбирается: %v", uiWorkflow, err)
	}

	names := make([]string, 0, len(doc.Jobs))
	for n := range doc.Jobs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		job := doc.Jobs[name]
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) == "" {
				continue
			}
			steps++
			wd := step.WorkingDirectory
			if wd == "" {
				wd = job.Defaults.Run.WorkingDirectory
			}
			if wd == "" {
				wd = doc.Defaults.Run.WorkingDirectory
			}
			// Каталог запуска задан от корня репозитория; пакетом он становится
			// только внутри ui-future. Шаг, работающий вне консоли, узлов не даёт.
			if wd != uiRoot && !strings.HasPrefix(wd, uiRoot+"/") {
				continue
			}
			from := uiPkgOf(strings.TrimPrefix(strings.TrimPrefix(wd, uiRoot), "/"))
			for _, cmd := range uiShellCommands(step.Run) {
				node, ok, unres := uiRunTarget(from, cmd)
				if unres {
					unresolved++
					continue
				}
				if ok {
					seeds = append(seeds, node)
				}
			}
		}
	}
	return seeds, steps, unresolved
}

func TestUiDeclaredLintIsWhatThePipelineRuns(t *testing.T) {
	byPkg, rels := uiManifests(t)

	root := repoRoot(t)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открывается: %v", root, err)
	}
	defer func() { _ = r.Close() }()
	body, ok := readTracked(r, uiWorkflow)
	if !ok {
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: %s не прочитан — измерять нечего", uiWorkflow)
	}

	seeds, steps, unresolvedSeeds := uiWorkflowSeeds(t, body)
	reached, unresolvedWalk := uiReach(byPkg, seeds)

	// Пакеты, объявившие развёрнутую команду разработчика.
	var declaring []string
	for _, pkg := range uiSortedKeys(byPkg) {
		if _, ok := byPkg[pkg].Scripts["lint"]; ok {
			declaring = append(declaring, pkg)
		}
	}

	census := "перепись: манифестов " + strconv.Itoa(len(rels)) +
		", шагов конвейера с командой " + strconv.Itoa(steps) +
		", вызовов npm-скриптов прямо из конвейера " + strconv.Itoa(len(seeds)) +
		", узлов достигнуто " + strconv.Itoa(len(reached)) +
		", пакетов с объявленным `lint` " + strconv.Itoa(len(declaring)) +
		"; неразрешимых вызовов: у конвейера " + strconv.Itoa(unresolvedSeeds) +
		", по дороге " + strconv.Itoa(unresolvedWalk)
	t.Log(census)
	t.Log("достигнуто: " + uiNodeList(uiSortNodes(reached)))

	switch {
	case len(rels) == 0:
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: не прочитано НИ ОДНОГО манифеста под %s/. %s", uiRoot, census)
	case steps == 0:
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: в %s не разобрано НИ ОДНОГО шага с командой. %s", uiWorkflow, census)
	case len(declaring) == 0:
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: ни один пакет консоли не объявляет `lint` — предмета нет, "+
			"а значит гейт судит не то дерево. %s", census)
	}

	var missing []string
	for _, pkg := range declaring {
		if !reached[uiScriptNode{Pkg: pkg, Script: "lint"}] {
			missing = append(missing, uiScriptNode{Pkg: pkg, Script: "lint"}.String())
		}
	}
	if len(missing) > 0 {
		t.Fatalf("объявленную команду `lint` конвейер НЕ исполняет у %d пакет(ов): %s.\n"+
			"Пока конвейер зовёт линтер своей строкой, а объявленную команду не зовёт, её поломка "+
			"невидима ИМЕННО ТАМ, где её ищут: прогон зелен, команда не работает — и первым "+
			"читателем оказывается разработчик перед отправкой. Либо %s зовёт её, либо отступление "+
			"записывается с предикатом снятия, который сам роняет прогон, когда наступает.\n%s",
			len(missing), strings.Join(missing, ", "), uiWorkflow, census)
	}
}

// uiNodeList — перечень узлов одной строкой для переписи.
func uiNodeList(in []uiScriptNode) string {
	out := make([]string, 0, len(in))
	for _, n := range in {
		out = append(out, n.String())
	}
	return strings.Join(out, ", ")
}
