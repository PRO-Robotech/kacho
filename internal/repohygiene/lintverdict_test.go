// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lintverdict_test.go — вердикт линтера принадлежит ЭТОМУ дереву и не усечён
// молча.
//
// # Предмет
//
// Прогон линтера выглядит содержательным всегда: ненулевой код возврата и
// десятки строк. Две вещи делают этот вид ложным, и обе наблюдались:
//
//  1. КЭШ ОБЩИЙ НА МАШИНУ. Умолчание — `~/.cache/golangci-lint`, и ключ записи
//     складывается из содержимого пакета, а не из дерева, в котором он лежит.
//     Две рабочие копии одного модуля (worktree, каталог задачи, второй клон)
//     содержат одинаковые пакеты — значит вторая получает разбор, сделанный по
//     ФАЙЛАМ первой, и печатает находки с чужими путями (`../<чужое>/pkg/api/…`).
//     Замер на паре копий: та же команда, тот же пакет — в своей копии пути
//     `tools/…`, в соседней `../kc-b-envtest/tools/…`, сто находок из ста.
//     Дальше это не косметика: якорные исключения конфигурации (`^pkg/api/`,
//     `^ui-future/`) чужой путь НЕ матчат, поэтому исключённое всплывает обратно
//     находками, а чужая правка меняет вердикт здесь.
//
//  2. ПОТОЛКИ РЕЖУТ ВЫВОД БЕЗ СЛОВА ОБ ЭТОМ. `max-issues-per-linter` и
//     `max-same-issues` отбрасывают лишнее МОЛЧА — ни строки о том, что что-то
//     отброшено. Замер на синтетическом пакете: линтер порождает 119 находок,
//     при потолке 100 печатается «100 issues», про 19 не сказано ничего.
//     Отброшенные — РАЗНЫЕ находки, не повторы одной: молча исчезает та, ради
//     которой линтер и запускался.
//
// # Что здесь считается защитой
//
//   - у КАЖДОГО вызова `golangci-lint run` кэш переопределён на путь ВНУТРИ
//     этой рабочей копии (`GOLANGCI_LINT_CACHE`), заданный от корня checkout'а
//     (`$(CURDIR)`, `$(abspath …)`, `$PWD`, `${{ github.workspace }}`,
//     `$GITHUB_WORKSPACE`), а не от домашнего каталога и не абсолютным путём:
//     абсолютный и `$HOME`-путь снова общие на машину;
//   - оба потолка в `.golangci.yml` объявлены нулём (ноль = без ограничения).
//     Отсутствие записи — тоже находка: умолчания линтера ненулевые (3 и 50),
//     то есть «не написали» означает «режем молча».
//
// # Читается исполняемая часть, а не текст
//
// Слово `golangci-lint run` стоит в комментариях, ОБЪЯСНЯЮЩИХ эти правила, — в
// этом файле в том числе. Поэтому у сборочных файлов и скриптов читаются только
// строки рецептов/команд без комментария, а у конвейеров CI — РАЗОБРАННЫЙ
// документ, где комментария не существует как узла.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько отслеживаемых файлов каждого вида прочитал и сколько вызовов в них
// нашёл. Ноль прочитанных файлов и ноль найденных вызовов — провал, а не успех.
package repohygiene

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

const lintInvocation = "golangci-lint run"

// cacheEnvVar — ручка, которой golangci-lint переносит свой кэш.
const cacheEnvVar = "GOLANGCI_LINT_CACHE"

// checkoutRootTokens — способы назвать корень ЭТОЙ рабочей копии, не зная её
// абсолютного пути. Значение, собранное от одного из них, у каждой копии своё —
// в этом весь смысл; значение, собранное от `$HOME` или записанное абсолютным
// путём, снова общее на машину и защитой не является.
var checkoutRootTokens = []string{
	"$(CURDIR)", "${CURDIR}", "$(abspath", "$(PWD)", "${PWD}", "$PWD",
	"${{ github.workspace }}", "$GITHUB_WORKSPACE", "${GITHUB_WORKSPACE}",
}

// cachePinnedToCheckout — значение ручки привязано к корню этой рабочей копии.
func cachePinnedToCheckout(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if strings.Contains(v, "$HOME") || strings.Contains(v, "${HOME}") || strings.HasPrefix(v, "~") {
		return false
	}
	if strings.HasPrefix(v, "/") { // абсолютный путь — один и тот же у всех копий
		return false
	}
	for _, tok := range checkoutRootTokens {
		if strings.Contains(v, tok) {
			return true
		}
	}
	return false
}

// assignedCacheValue достаёт значение, присвоенное ручке в ИСПОЛНЯЕМОЙ части
// фрагмента (make-присваивание, export, префикс окружения перед командой).
// Возвращает пустую строку, если присваивания нет.
func assignedCacheValue(fragment string) string {
	for _, raw := range strings.Split(fragment, "\n") {
		kept, _ := shellExecutable(raw)
		i := strings.Index(kept, cacheEnvVar)
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(kept[i+len(cacheEnvVar):])
		rest = strings.TrimLeft(rest, ":?+") // make-присваивания :=, ?=, +=
		if !strings.HasPrefix(rest, "=") {
			continue // упоминание без присваивания (напр. `export X` отдельной строкой)
		}
		return strings.TrimSpace(strings.TrimPrefix(rest, "="))
	}
	return ""
}

// makeAssignsCacheSoftly — присваивание ручки в сборочном файле сделано `?=`.
//
// `?=` уступает окружению: экспортированная где-то выше переменная молча
// возвращает общий кэш машины, и защита остаётся на вид на месте. Гейт при этом
// зелёный — значение в файле правильное, просто до линтера доедет другое.
// Поэтому мягкое присваивание — тоже находка; переопределение остаётся
// возможным аргументом make, то есть заявленным явно.
func makeAssignsCacheSoftly(body string) bool {
	for _, raw := range strings.Split(stripMakeComments(body), "\n") {
		kept, _ := shellExecutable(raw)
		i := strings.Index(kept, cacheEnvVar)
		if i < 0 {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(kept[i+len(cacheEnvVar):]), "?=") {
			return true
		}
	}
	return false
}

// lintSite — одно место, где запускается линтер.
type lintSite struct {
	File string
	Line int
	Kind string // makefile | script | workflow
	Why  string // чем защита отсутствует
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 1: кэш линтера принадлежит рабочей копии.
// ─────────────────────────────────────────────────────────────────────────────

func TestLintRunCannotInheritAnotherCheckoutsCache(t *testing.T) {
	root := repoRoot(t)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открывается: %v", root, err)
	}
	defer func() { _ = r.Close() }()
	tracked := trackedPaths(t, root)

	var (
		read     = map[string]int{}
		sites    int
		findings []lintSite
		elsewhen []string
	)

	for _, rel := range tracked {
		body, ok := readTracked(r, rel)
		if !ok {
			continue
		}
		switch kindOfTrackedFile(rel, body) {
		case "makefile":
			read["makefile"]++
			n, f := lintSitesInMakefile(rel, body)
			sites += n
			findings = append(findings, f...)
		case "script":
			read["script"]++
			n, f := lintSitesInScript(rel, body)
			sites += n
			findings = append(findings, f...)
		case "workflow":
			read["workflow"]++
			n, f := lintSitesInWorkflow(t, rel, body)
			sites += n
			findings = append(findings, f...)
		default:
			if strings.Contains(body, lintInvocation) {
				elsewhen = append(elsewhen, rel)
			}
		}
	}

	census := "перепись: прочитано " + strconv.Itoa(read["makefile"]) + " сборочных файлов, " +
		strconv.Itoa(read["script"]) + " скриптов, " + strconv.Itoa(read["workflow"]) + " конвейеров CI; " +
		"вызовов линтера найдено " + strconv.Itoa(sites)
	t.Log(census)
	if len(elsewhen) > 0 {
		sort.Strings(elsewhen)
		// Не находка: в прозе и в комментариях команда называется, а не исполняется.
		t.Log("вне исполняемых видов команда упомянута в: " + strings.Join(elsewhen, ", "))
	}

	// Предпосылка гейта: ему было что осматривать. Ноль вызовов означает, что
	// либо дерево прочитано не то, либо предикат перестал узнавать вызов, —
	// и в обоих случаях «находок нет» неотличимо от «ничего не прочитано».
	if sites == 0 {
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: не найдено НИ ОДНОГО вызова %q. %s", lintInvocation, census)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.File + ":" + strconv.Itoa(f.Line) + " (" + f.Kind + ") — " + f.Why)
		}
		t.Fatalf("%d вызов(ов) линтера идут с кэшем, общим на машину. Кэш ключуется содержимым "+
			"пакета, а не деревом: соседняя рабочая копия того же модуля получает разбор, сделанный "+
			"по ЧУЖИМ файлам, и печатает находки с чужими путями — мимо якорных исключений "+
			"конфигурации, которые такой путь не матчат. Задай %s значением от корня этой копии.%s\n%s",
			len(findings), cacheEnvVar, b.String(), census)
	}
}

// lintSitesInMakefile — вызовы в рецептах сборочного файла.
//
// Переменные make действуют на весь файл, поэтому защита засчитывается либо у
// самой строки рецепта (префикс окружения), либо на уровне файла — присваивание
// ручки ВМЕСТЕ с `export` (без export дочерний процесс её не увидит).
func lintSitesInMakefile(rel, body string) (int, []lintSite) {
	fileValue := assignedCacheValue(stripMakeComments(body))
	exported := false
	for _, raw := range strings.Split(stripMakeComments(body), "\n") {
		kept, _ := shellExecutable(raw)
		f := strings.Fields(kept)
		if len(f) >= 2 && f[0] == "export" && strings.HasPrefix(f[1], cacheEnvVar) {
			exported = true
		}
	}

	var (
		count int
		out   []lintSite
	)
	for i, raw := range strings.Split(body, "\n") {
		if !strings.HasPrefix(raw, "\t") {
			continue // не рецепт
		}
		kept, _ := shellExecutable(raw)
		if !strings.Contains(kept, lintInvocation) {
			continue
		}
		count++
		switch {
		case cachePinnedToCheckout(assignedCacheValue(raw)):
		case exported && cachePinnedToCheckout(fileValue) && !makeAssignsCacheSoftly(body):
		case exported && cachePinnedToCheckout(fileValue):
			out = append(out, lintSite{rel, i + 1, "makefile",
				cacheEnvVar + " присвоена через `?=` — окружение молча перебьёт пин, и защита " +
					"останется на вид на месте. Нужно `:=`; переопределение остаётся аргументом make"})
		case fileValue != "" && !cachePinnedToCheckout(fileValue):
			out = append(out, lintSite{rel, i + 1, "makefile",
				cacheEnvVar + "=" + fileValue + " — значение общее на машину, а не своё у этой копии"})
		case fileValue != "" && !exported:
			out = append(out, lintSite{rel, i + 1, "makefile",
				cacheEnvVar + " присвоена, но не экспортирована — дочерний процесс её не увидит"})
		default:
			out = append(out, lintSite{rel, i + 1, "makefile", cacheEnvVar + " не задана"})
		}
	}
	return count, out
}

// lintSitesInScript — вызовы в shell-скрипте.
func lintSitesInScript(rel, body string) (int, []lintSite) {
	fileValue := assignedCacheValue(body)
	var (
		count int
		out   []lintSite
	)
	for i, raw := range strings.Split(body, "\n") {
		kept, _ := shellExecutable(raw)
		if !strings.Contains(kept, lintInvocation) {
			continue
		}
		count++
		if cachePinnedToCheckout(assignedCacheValue(raw)) || cachePinnedToCheckout(fileValue) {
			continue
		}
		out = append(out, lintSite{rel, i + 1, "script", cacheEnvVar + " не задана значением от корня копии"})
	}
	return count, out
}

// lintWorkflowDoc — ровно те узлы конвейера, которые нужны этому гейту.
type lintWorkflowDoc struct {
	Env  map[string]string `yaml:"env"`
	Jobs map[string]struct {
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			Run string            `yaml:"run"`
			Env map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// lintSitesInWorkflow — вызовы в шагах конвейера CI. Читается РАЗОБРАННЫЙ
// документ: в нём комментария не существует как узла, поэтому объяснение
// правила не может сойти за его исполнение.
func lintSitesInWorkflow(t *testing.T, rel, body string) (int, []lintSite) {
	t.Helper()
	var doc lintWorkflowDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("%s: конвейер не разбирается: %v", rel, err)
	}

	var (
		count int
		out   []lintSite
	)
	names := make([]string, 0, len(doc.Jobs))
	for name := range doc.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		job := doc.Jobs[name]
		for _, step := range job.Steps {
			if !strings.Contains(step.Run, lintInvocation) {
				continue
			}
			count++
			if cachePinnedToCheckout(step.Env[cacheEnvVar]) ||
				cachePinnedToCheckout(job.Env[cacheEnvVar]) ||
				cachePinnedToCheckout(doc.Env[cacheEnvVar]) ||
				cachePinnedToCheckout(assignedCacheValue(step.Run)) {
				continue
			}
			out = append(out, lintSite{rel, lineOfRun(body, step.Run), "workflow",
				"job " + name + ": " + cacheEnvVar + " не задана значением от корня копии"})
		}
	}
	return count, out
}

// lineOfRun — номер строки, с которой начинается тело шага, чтобы находка несла
// координату, а не только имя файла.
func lineOfRun(body, run string) int {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(run), "\n", 2)[0])
	if first == "" {
		return 0
	}
	for i, raw := range strings.Split(body, "\n") {
		if strings.Contains(raw, first) {
			return i + 1
		}
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 2: вывод линтера не режется молча.
// ─────────────────────────────────────────────────────────────────────────────

func TestLintVerdictIsNotSilentlyTruncated(t *testing.T) {
	root := repoRoot(t)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открывается: %v", root, err)
	}
	defer func() { _ = r.Close() }()
	rel := ".golangci.yml"
	body, ok := readTracked(r, rel)
	if !ok {
		t.Fatalf("ГЕЙТ НЕ ОТРАБОТАЛ: %s не прочитан — измерять нечего", rel)
	}

	var cfg struct {
		Issues struct {
			MaxSameIssues     *int `yaml:"max-same-issues"`
			MaxIssuesPerLiner *int `yaml:"max-issues-per-linter"`
		} `yaml:"issues"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("%s не разбирается: %v", rel, err)
	}

	check := func(name string, v *int) {
		t.Helper()
		if v == nil {
			t.Errorf("%s: %s не объявлен. Умолчания линтера НЕНУЛЕВЫЕ, поэтому «не написали» "+
				"означает «режем молча»: лишние находки отбрасываются без единой строки о том, "+
				"что что-то отброшено. Объяви 0 — это «без ограничения»", rel, name)
			return
		}
		if *v != 0 {
			t.Errorf("%s: %s = %d. Потолок отбрасывает находки МОЛЧА — в выводе не остаётся следа "+
				"ни от числа отброшенных, ни от их существования, а отбрасываются РАЗНЫЕ находки, "+
				"а не повторы одной. Измерено на синтетическом пакете: 119 порождённых находок, "+
				"«100 issues» в выводе, про 19 не сказано ничего. 0 = без ограничения",
				rel, name, *v)
		}
	}
	check("issues.max-same-issues", cfg.Issues.MaxSameIssues)
	check("issues.max-issues-per-linter", cfg.Issues.MaxIssuesPerLiner)
}

// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ ПРЕДИКАТА В ОБЕ СТОРОНЫ.
// ─────────────────────────────────────────────────────────────────────────────

// TestLintCachePredicateCutsBothWays — предикат обязан краснеть на настоящем
// дефекте и молчать на законной конструкции той же формы. Без второй половины
// он ловил бы форму, а не существо, и первый же ложный срабат его бы снял.
func TestLintCachePredicateCutsBothWays(t *testing.T) {
	t.Run("значение ручки", func(t *testing.T) {
		for _, tc := range []struct {
			value string
			want  bool
		}{
			{"$(CURDIR)/.cache/golangci-lint", true},
			{"$(abspath $(CURDIR)/../..)/.cache/golangci-lint", true},
			{"${{ github.workspace }}/.cache/golangci-lint", true},
			{"$GITHUB_WORKSPACE/.cache/golangci-lint", true},
			{"", false},                              // не задана — общий кэш машины
			{"/home/ci/.cache/golangci-lint", false}, // абсолютный — общий на машину
			{"$HOME/.cache/golangci-lint", false},    // домашний — общий на машину
			{"~/.cache/golangci-lint", false},        // он же
			{".cache/golangci-lint", false},          // зависит от каталога запуска, а не от копии
		} {
			if got := cachePinnedToCheckout(tc.value); got != tc.want {
				t.Errorf("cachePinnedToCheckout(%q) = %v, ожидалось %v", tc.value, got, tc.want)
			}
		}
	})

	t.Run("сборочный файл", func(t *testing.T) {
		defective := "lint:\n\tgolangci-lint run ./...\n"
		if n, f := lintSitesInMakefile("X/Makefile", defective); n != 1 || len(f) != 1 {
			t.Errorf("незащищённый вызов: найдено вызовов %d, находок %d — ожидалось 1 и 1", n, len(f))
		}

		legit := "GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint\nexport GOLANGCI_LINT_CACHE\n\nlint:\n\tgolangci-lint run ./...\n"
		if n, f := lintSitesInMakefile("X/Makefile", legit); n != 1 || len(f) != 0 {
			t.Errorf("защищённый вызов: найдено вызовов %d, находок %d — ожидалось 1 и 0", n, len(f))
		}

		// Мягкое присваивание: значение правильное, но окружение его перебьёт.
		soft := "GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint\nexport GOLANGCI_LINT_CACHE\n\nlint:\n\tgolangci-lint run ./...\n"
		if _, f := lintSitesInMakefile("X/Makefile", soft); len(f) != 1 {
			t.Errorf("мягкое присваивание `?=`: находок %d — ожидалась 1", len(f))
		}

		// Присвоена, но не экспортирована: дочерний процесс ручки не увидит,
		// то есть защита написана и не действует — это находка, а не стиль.
		noExport := "GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint\n\nlint:\n\tgolangci-lint run ./...\n"
		if _, f := lintSitesInMakefile("X/Makefile", noExport); len(f) != 1 {
			t.Errorf("присвоение без export: находок %d — ожидалась 1", len(f))
		}

		// Комментарий, ОБЪЯСНЯЮЩИЙ правило, вызовом не является: гейт, читающий
		// сырой текст, засчитал бы его и остался зелёным при снятой защите.
		onlyComment := "# запускать golangci-lint run только с локальным кэшем\nlint:\n\techo ok\n"
		if n, f := lintSitesInMakefile("X/Makefile", onlyComment); n != 0 || len(f) != 0 {
			t.Errorf("упоминание в комментарии: найдено вызовов %d, находок %d — ожидалось 0 и 0", n, len(f))
		}
	})

	t.Run("конвейер CI", func(t *testing.T) {
		defective := "jobs:\n  lint:\n    steps:\n      - run: golangci-lint run --timeout=10m\n"
		if n, f := lintSitesInWorkflow(t, "w.yml", defective); n != 1 || len(f) != 1 {
			t.Errorf("незащищённый шаг: вызовов %d, находок %d — ожидалось 1 и 1", n, len(f))
		}

		legitStep := "jobs:\n  lint:\n    steps:\n      - run: golangci-lint run --timeout=10m\n        env:\n          GOLANGCI_LINT_CACHE: ${{ github.workspace }}/.cache/golangci-lint\n"
		if n, f := lintSitesInWorkflow(t, "w.yml", legitStep); n != 1 || len(f) != 0 {
			t.Errorf("защита у шага: вызовов %d, находок %d — ожидалось 1 и 0", n, len(f))
		}

		legitJob := "jobs:\n  lint:\n    env:\n      GOLANGCI_LINT_CACHE: ${{ github.workspace }}/.cache/golangci-lint\n    steps:\n      - run: golangci-lint run\n"
		if _, f := lintSitesInWorkflow(t, "w.yml", legitJob); len(f) != 0 {
			t.Errorf("защита на уровне job: находок %d — ожидалось 0", len(f))
		}

		// Ручка задана, но домашним путём — снова общий кэш машины.
		fakeProtection := "jobs:\n  lint:\n    env:\n      GOLANGCI_LINT_CACHE: $HOME/.cache/golangci-lint\n    steps:\n      - run: golangci-lint run\n"
		if _, f := lintSitesInWorkflow(t, "w.yml", fakeProtection); len(f) != 1 {
			t.Errorf("домашний путь: находок %d — ожидалась 1", len(f))
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева.
// ─────────────────────────────────────────────────────────────────────────────

// trackedPaths — отслеживаемые git-ом пути. Единица счёта — элемент индекса, а
// не то, что лежит на диске: иначе объявление и поведение разъезжаются молча
// (артефакт сборки, чужой каталог, недоудалённый файл).
func trackedPaths(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — перепись невозможна, а без переписи «ноль находок» "+
			"неотличимо от «ноль прочитанного»", err)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// readTracked читает файл В ПРЕДЕЛАХ корня: имя, разрешение которого выходит
// наружу, не читается вовсе — иначе вердикт гейта стал бы свойством
// постороннего файла. Нечитаемое (нет в рабочем дереве) и двоичное в перепись
// не идут: исполняемых вызовов там нет по построению.
func readTracked(r *os.Root, rel string) (string, bool) {
	b, err := r.ReadFile(rel)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return "", false
	}
	return string(b), true
}

// kindOfTrackedFile — вид файла с точки зрения ЭТОГО гейта.
func kindOfTrackedFile(rel, body string) string {
	base := filepath.Base(rel)
	switch {
	case base == "Makefile" || strings.HasSuffix(rel, ".mk"):
		return "makefile"
	case strings.HasPrefix(rel, ".github/workflows/") &&
		(strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")):
		return "workflow"
	case strings.HasSuffix(rel, ".sh") || strings.HasPrefix(body, "#!"):
		return "script"
	}
	return ""
}
