// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cachedtreeverdict_test.go — вердикт проверки ДЕРЕВА не подаётся из кеша
// `go test`.
//
// # Предмет
//
// `go test` кеширует успешный результат пакета по содержимому пакета и его
// импортов. Проверка дерева судит не пакет, а РЕПОЗИТОРИЙ, и состав берёт из
// индекса git подпроцессом — инструменту невидимым. Правка в чужом каталоге кеш
// не инвалидирует, и над красным деревом печатается `ok (cached)`.
//
// Класс, замеры и дискриминатор — pkg/treecorpus, cachedverdict.go. Здесь
// не пересказывается: два места об одном предмете разошлись бы на первой правке.
//
// # Что держит ЭТОТ гейт
//
// Он держит не сам класс (класс держит страж), а то, что страж ОСТАЁТСЯ на
// месте, — по трём осям сразу:
//
//	ось 1  каждый пакет проб под internal/repohygiene и pkg/treecorpus
//	       объявляет TestMain, доходящий до стража;
//	ось 2  каждая экспортированная функция treecorpus, достигающая реального
//	       индекса, доходит до стража — то есть третий конструктор, заведённый
//	       мимо, находка, а не тихая дыра;
//	ось 3  каждый рецепт Makefile дерева, вызывающий `go test`, несёт отключение
//	       кеша.
//
// Ось 3 сегодня зелена целиком, и это сказано прямо: она держит свойство ВПЕРЁД,
// а её прохождение свидетельством ничего не является.
//
// # Названная слепая зона
//
// Область стража — два корня выше. Проверки дерева живут и вне их; сколько
// пакетов вне области спрашивают состав дерева, гейт ПЕЧАТАЕТ числом на каждом
// прогоне. Ноль там будет означать закрытый остаток, а не «не искали».

package repohygiene

import (
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// guardRoots — корни, пакеты которых обязаны нести стража. Перечень назван, а не
// выведен из дерева: «пакет судит дерево» — свойство разбора, а не раскладки, и
// требовать стража от ВСЕХ таких пакетов сразу значило бы посадить гейт красным.
// Остаток при этом не спрятан — он печатается числом (см. слепую зону в шапке).
var guardRoots = []string{
	"internal/repohygiene",
	"pkg/treecorpus",
}

// guardName — имя стража. Одно место на весь файл: ось 1 и ось 2 обязаны
// спрашивать про ОДНО и то же имя, иначе разойдутся молча.
const guardName = "CachedVerdictRefusal"

// realIndexReader — функция treecorpus, читающая индекс НАСТОЯЩЕГО репозитория.
// Синтетическое дерево идёт мимо неё и стража не требует.
const realIndexReader = "listFilesCached"

func TestTreeVerdictIsNeverServedFromTheTestCache(t *testing.T) {
	root := repoRoot(t)

	pkgs, pkgsRead := collectTestMainFacts(t, root)
	ctors, ctorsRead := collectTreecorpusFacts(t, root)
	recipes, mkRead, mkFiles := collectMakefileRecipes(t, root)
	outside := countGuardlessTreeReaders(t, root)

	t.Logf("перепись: пакетов проб под областью %d, экспортированных функций treecorpus %d, "+
		"файлов Makefile %d (рецептов с прогоном %d); пакетов ВНЕ области, спрашивающих состав "+
		"дерева, %d", pkgsRead, ctorsRead, mkFiles, mkRead, outside)

	if pkgsRead == 0 || ctorsRead == 0 || mkFiles == 0 {
		t.Fatalf("обход пуст (пакетов %d, функций %d, файлов Makefile %d) — «ноль находок» "+
			"означало бы «ноль прочитанного»", pkgsRead, ctorsRead, mkFiles)
	}

	var findings []string
	findings = append(findings, judgeGuardedPackages(pkgs)...)
	findings = append(findings, judgeGuardedConstructors(ctors)...)
	findings = append(findings, judgeMakefileRecipes(recipes)...)

	if len(findings) > 0 {
		t.Errorf("страж кешированного вердикта снят или обойдён — найдено %d:\n  %s\n\n"+
			"Без него проверка дерева отвечает `ok (cached)` над деревом, где она красная: "+
			"состав она берёт подпроцессом, о котором `go test` не знает.\n"+
			"Разбор — pkg/treecorpus, cachedverdict.go.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// ─────────────────────────── ось 1: пакеты проб ───────────────────────────

// testMainFacts — что известно про ОДИН пакет проб.
type testMainFacts struct {
	pkgDir           string
	declaresTestMain bool
	reachesGuard     bool
}

func judgeGuardedPackages(pkgs []testMainFacts) []string {
	var out []string
	for _, p := range pkgs {
		switch {
		case !p.declaresTestMain:
			out = append(out, p.pkgDir+": пакет проб без TestMain — прогон с отбором `-run` "+
				"по одной проверке подался бы из кеша")
		case !p.reachesGuard:
			out = append(out, p.pkgDir+": TestMain есть, но до "+guardName+" не доходит — "+
				"форма стража без содержания")
		}
	}
	return out
}

// ────────────────────── ось 2: конструкторы treecorpus ──────────────────────

// constructorFacts — что известно про ОДНУ экспортированную функцию treecorpus.
type constructorFacts struct {
	name         string
	reachesIndex bool
	reachesGuard bool
}

func judgeGuardedConstructors(ctors []constructorFacts) []string {
	var out []string
	for _, c := range ctors {
		if c.reachesIndex && !c.reachesGuard {
			out = append(out, "treecorpus."+c.name+": читает индекс настоящего репозитория "+
				"и не зовёт "+guardName+" — вердикт вызывающего можно подать из кеша")
		}
	}
	return out
}

// ───────────────────────── ось 3: рецепты Makefile ─────────────────────────

// makeRecipe — ОДНА логическая строка Makefile, вызывающая прогон проб.
type makeRecipe struct {
	file string
	line int
	text string
}

// cacheDisablingFlags — флаги, при которых `go test` кеш не применяет. Перечень
// закрытый: `-count` в любой форме, а также режимы, где кеша нет by construction.
var cacheDisablingFlags = []string{"-count=", "-count ", "-bench", "-fuzz", "-exec"}

func judgeMakefileRecipes(recipes []makeRecipe) []string {
	var out []string
	for _, r := range recipes {
		if !recipeDisablesCache(r.text) {
			out = append(out, r.file+":"+itoa(r.line)+": прогон проб без отключения кеша — "+
				"цель отдала бы `ok (cached)` над деревом, где проверка красная")
		}
	}
	return out
}

func recipeDisablesCache(text string) bool {
	for _, f := range cacheDisablingFlags {
		if strings.Contains(text, f) {
			return true
		}
	}
	return false
}

// recipeInvokesGoTest — строка вызывает прогон проб. Форма вызова бывает трёх
// видов: прямая и через переменную, которой Makefile называет инструмент.
func recipeInvokesGoTest(text string) bool {
	for _, form := range []string{"go test ", "$(GO) test ", "${GO} test "} {
		if strings.Contains(text, form) {
			return true
		}
	}
	return false
}

// makefileLogicalLines — логические строки: комментарии сняты, продолжения через
// обратную косую склеены. Гейт обязан читать исполняемую часть: строка `#   go
// test ./…` в разборе самого Makefile прогоном не является.
func makefileLogicalLines(text string) (lines []makeRecipe) {
	raw := strings.Split(text, "\n")
	var acc strings.Builder
	start := 0
	for i, ln := range raw {
		if acc.Len() == 0 {
			if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
				continue
			}
			start = i + 1
		}
		cont := strings.HasSuffix(ln, `\`)
		acc.WriteString(strings.TrimSuffix(ln, `\`))
		acc.WriteString(" ")
		if cont {
			continue
		}
		lines = append(lines, makeRecipe{line: start, text: acc.String()})
		acc.Reset()
	}
	if acc.Len() > 0 {
		lines = append(lines, makeRecipe{line: start, text: acc.String()})
	}
	return lines
}

// ──────────────────────────── сбор фактов из дерева ────────────────────────────

func collectTestMainFacts(t *testing.T, root string) ([]testMainFacts, int) {
	t.Helper()
	byDir := map[string]*testMainFacts{}
	for _, r := range guardRoots {
		files, err := treecorpus.UnderWithSuffix(filepath.Join(root, r), "_test.go")
		if err != nil {
			t.Fatalf("состав %s: %v", r, err)
		}
		for _, f := range files {
			rel, relErr := filepath.Rel(root, f)
			if relErr != nil {
				t.Fatalf("относительный путь %s: %v", f, relErr)
			}
			dir := filepath.ToSlash(filepath.Dir(rel))
			if byDir[dir] == nil {
				byDir[dir] = &testMainFacts{pkgDir: dir}
			}
			declares, guarded := testMainFactsFromAST(parseGo(t, f, mustRead(t, f)))
			byDir[dir].declaresTestMain = byDir[dir].declaresTestMain || declares
			byDir[dir].reachesGuard = byDir[dir].reachesGuard || guarded
		}
	}
	var out []testMainFacts
	for _, v := range byDir {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pkgDir < out[j].pkgDir })
	return out, len(out)
}

func collectTreecorpusFacts(t *testing.T, root string) ([]constructorFacts, int) {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "pkg/treecorpus"), ".go")
	if err != nil {
		t.Fatalf("состав pkg/treecorpus: %v", err)
	}
	calls := map[string]map[string]bool{}
	exported := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file := parseGo(t, f, mustRead(t, f))
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			calls[fd.Name.Name] = calleeNames(fd.Body)
			if ast.IsExported(fd.Name.Name) {
				exported[fd.Name.Name] = true
			}
		}
	}
	var out []constructorFacts
	for name := range exported {
		out = append(out, constructorFacts{
			name:         name,
			reachesIndex: reaches(calls, name, realIndexReader),
			reachesGuard: reaches(calls, name, guardName),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, len(out)
}

func collectMakefileRecipes(t *testing.T, root string) ([]makeRecipe, int, int) {
	t.Helper()
	all, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	var recipes []makeRecipe
	files := 0
	for _, f := range all {
		base := filepath.Base(f)
		if base != "Makefile" && !strings.HasSuffix(base, ".mk") {
			continue
		}
		files++
		src, readErr := os.ReadFile(f) // #nosec G304 — путь из индекса git
		if readErr != nil {
			t.Fatalf("чтение %s: %v", f, readErr)
		}
		rel, relErr := filepath.Rel(root, f)
		if relErr != nil {
			t.Fatalf("относительный путь %s: %v", f, relErr)
		}
		for _, ln := range makefileLogicalLines(string(src)) {
			if !recipeInvokesGoTest(ln.text) {
				continue
			}
			ln.file = filepath.ToSlash(rel)
			recipes = append(recipes, ln)
		}
	}
	return recipes, len(recipes), files
}

// countGuardlessTreeReaders — сколько пакетов ВНЕ области стража спрашивают
// состав дерева. Это НЕ находка, а названный остаток: он печатается числом,
// чтобы «ноль» когда-нибудь означал закрытый долг, а не отсутствие обхода.
func countGuardlessTreeReaders(t *testing.T, root string) int {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(root, "_test.go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	dirs := map[string]bool{}
	for _, f := range files {
		rel, relErr := filepath.Rel(root, f)
		if relErr != nil {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if underGuardRoots(dir) {
			continue
		}
		src, readErr := os.ReadFile(f) // #nosec G304 — путь из индекса git
		if readErr != nil {
			continue
		}
		if strings.Contains(string(src), "pkg/treecorpus") {
			dirs[dir] = true
		}
	}
	return len(dirs)
}

func underGuardRoots(dir string) bool {
	for _, r := range guardRoots {
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return true
		}
	}
	return false
}

// ─────────────────────────────── разбор ───────────────────────────────

// testMainFactsFromAST — объявлен ли в ЭТОМ файле TestMain и доходит ли он до
// стража.
//
// Вынесено отдельной функцией ради инъекции, и не из аккуратности: объявление
// TestMain встречается в этом дереве ВНУТРИ строковых литералов — синтетические
// пакеты фикстур соседних гейтов содержат его дословно. Поиск по тексту нашёл бы
// их и объявил пакет прикрытым, тогда как настоящего TestMain у него нет. Разбор
// судит узел объявления by construction.
func testMainFactsFromAST(file *ast.File) (declares, reachesGuard bool) {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != "TestMain" || fd.Body == nil {
			continue
		}
		declares = true
		if calleeNames(fd.Body)[guardName] {
			reachesGuard = true
		}
	}
	return declares, reachesGuard
}

func calleeNames(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := ce.Fun.(type) {
		case *ast.Ident:
			out[fn.Name] = true
		case *ast.SelectorExpr:
			out[fn.Sel.Name] = true
		}
		return true
	})
	return out
}

func reaches(calls map[string]map[string]bool, from, target string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		for c := range calls[name] {
			if c == target || walk(c) {
				return true
			}
		}
		return false
	}
	return walk(from)
}
