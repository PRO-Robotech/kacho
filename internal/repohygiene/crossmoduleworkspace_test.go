// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// crossmoduleworkspace_test.go — МЕХАНИЗМ КРОСС-МОДУЛЬНОЙ РАЗРАБОТКИ НАЗВАН И
// ИСПОЛНИМ: у дерева с несколькими модулями есть отслеживаемый ОБРАЗЕЦ рабочего
// пространства, и он называет ВСЕ модули этого дерева.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Дерево несёт больше одного модуля Go, и служба резолвит фундамент
// ОПУБЛИКОВАННОЙ версией (псевдоверсией), а не подменой пути: `replace` на
// внутренний модуль запрещён (`polyrepo.md`), и запрет держится тем, что при
// клоне одного репозитория `replace ../` не резолвится вовсе.
//
// Тогда локальная кросс-модульная разработка возможна ровно одним средством —
// рабочим пространством Go (`go.work`): оно переводит резолв на дерево, само
// игнорируется git и в индекс не попадает.
//
// Средство, названное правилом и не имеющее ОБРАЗЦА в дереве, — обещание. Его
// цена не в неудобстве: каждый, кому оно понадобится, соберёт своё, и половина
// соберёт неверно (перечислит один модуль из двух, положит файл в подкаталог,
// заведёт `replace` вместо `use`).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ ДЕЛАЕТ — И ЭТО СКАЗАНО ПРЯМО
//
// Он НЕ проверяет, что разработчик прогнал сборку и ПОД рабочим пространством, и
// БЕЗ него. Это свойство ПРОГОНА, а не дерева: под пространством резолв идёт на
// дерево, в конвейере — на пин, и оба вердикта настоящие, но о разном. Гейт
// дерева о прогоне не утверждает ничего by construction; требование двойного
// прогона живёт в документе (`docs/architecture/cross-module-development.md`) и
// держится вниманием. Названо здесь, чтобы наличие гейта не читалось шире.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕЧЕНЬ МОДУЛЕЙ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// Модули берутся из СОСТАВА дерева (`go.mod` в индексе). Выписанный перечень
// разошёлся бы с деревом на первой же вынесенной службе — молча, потому что
// образец продолжал бы существовать, называя вчерашнее множество.
//
// Сверка двусторонняя: модуль без записи `use` (пространство его не видит, и
// правка фундамента до него не доедет) и запись `use` без модуля (координата
// пережила свой предмет) — обе находки.
package repohygiene

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// workspaceExampleName — имя отслеживаемого образца рабочего пространства.
//
// `go.work.example` (а не `go.work`) намеренно: сам `go.work` меняет смысл
// `./...` для всякого, кто окажется в дереве, поэтому он игнорируется, а в
// индексе лежит образец, который берут копией.
const workspaceExampleName = "go.work.example"

// workspaceFinding — одно расхождение образца с деревом.
type workspaceFinding struct {
	Kind string // "нет-образца" | "модуль-без-use" | "use-без-модуля" | "не-отслеживается"
	What string
	Why  string
}

// parseWorkspaceUses — каталоги, названные директивами `use` образца.
//
// Разбор судит ДИРЕКТИВУ, а не строку: `use` встречается и в прозе шапки
// образца, поэтому комментарии срезаются до разбора, а блочная форма
// (`use ( … )`) читается наравне с однострочной. Иначе гейт краснел бы на
// собственном объяснении — класс, который корпус ловит у распознавателей.
func parseWorkspaceUses(src string) []string {
	var out []string
	inBlock := false
	for _, raw := range strings.Split(src, "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			out = append(out, normalizeUse(line))
			continue
		}
		rest, ok := strings.CutPrefix(line, "use")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "(" {
			inBlock = true
			continue
		}
		if rest != "" {
			out = append(out, normalizeUse(rest))
		}
	}
	sort.Strings(out)
	return out
}

// normalizeUse приводит запись `use` к каталогу ОТ КОРНЯ дерева: `.`, `./x`, `x`
// и `"./x"` суть одна координата, и различать их значило бы считать формой то,
// что является записью.
func normalizeUse(s string) string {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `"`))
	s = path.Clean(strings.TrimPrefix(s, "./"))
	if s == "" {
		s = "."
	}
	return s
}

// checkCrossModuleWorkspace — ТЕЛО гейта. Вынесено, чтобы инъекция звала то же,
// что исполняется на дереве.
func checkCrossModuleWorkspace(modules []string, exampleSrc string, examplePresent bool) []workspaceFinding {
	var out []workspaceFinding

	if !examplePresent {
		out = append(out, workspaceFinding{
			Kind: "нет-образца",
			What: workspaceExampleName,
			Why: "модулей в дереве больше одного, а образца рабочего пространства нет: " +
				"средство локальной кросс-модульной разработки названо и не предъявлено",
		})
		return out
	}

	uses := map[string]bool{}
	for _, u := range parseWorkspaceUses(exampleSrc) {
		uses[u] = true
	}
	have := map[string]bool{}
	for _, m := range modules {
		have[m] = true
	}

	for _, m := range modules {
		if !uses[m] {
			out = append(out, workspaceFinding{
				Kind: "модуль-без-use",
				What: m,
				Why: "модуль дерева не назван директивой `use`: под этим пространством " +
					"резолв до него не доедет, и правка фундамента останется невидимой",
			})
		}
	}
	var named []string
	for u := range uses {
		named = append(named, u)
	}
	sort.Strings(named)
	for _, u := range named {
		if !have[u] {
			out = append(out, workspaceFinding{
				Kind: "use-без-модуля",
				What: u,
				Why:  "директива `use` называет каталог, в котором модуля дерева нет: координата пережила свой предмет",
			})
		}
	}
	return out
}

// TestCrossModuleWorkspaceExampleNamesEveryModule — гейт на дереве.
func TestCrossModuleWorkspaceExampleNamesEveryModule(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	var modules []string
	for _, rel := range tree.Tree.SortedFiles() {
		if path.Base(rel) == "go.mod" {
			modules = append(modules, path.Dir(rel))
		}
	}
	sort.Strings(modules)

	// Предпосылка гейта. «Модулей ноль» означает, что состав прочитан неверно:
	// корневой `go.mod` в этом дереве есть всегда, и его отсутствие — не
	// законное состояние, а слепой обход.
	if len(modules) == 0 {
		t.Fatal("обход пуст: объявлений модуля в составе дерева не найдено ни одного — вердикт беспредметен")
	}

	// Один модуль — предмета у гейта нет, и это ЗАКОННОЕ состояние дерева, а не
	// отказ: кросс-модульной разработки при одном модуле не бывает. Перепись
	// ниже называет это числом, поэтому «ноль находок» отличимо от «ноль
	// прочитанного».
	examplePresent := tree.hasFile(workspaceExampleName)
	src := ""
	if examplePresent {
		b, err := os.ReadFile(filepath.Join(root, workspaceExampleName))
		if err != nil {
			t.Fatalf("образец %s есть в составе, но не прочитан: %v — это отказ, а не пропуск",
				workspaceExampleName, err)
		}
		src = string(b)
	}

	var found []workspaceFinding
	if len(modules) > 1 {
		found = checkCrossModuleWorkspace(modules, src, examplePresent)
	}

	t.Logf("осмотрено: состав %d файлов -> модулей %d (%s); образец %s: %s; директив use %d; находок %d",
		tree.count(), len(modules), strings.Join(modules, ", "),
		workspaceExampleName, map[bool]string{true: "есть", false: "НЕТ"}[examplePresent],
		len(parseWorkspaceUses(src)), len(found))

	for _, f := range found {
		t.Errorf("%s %q — %s.\n"+
			"  ЧТО ДЕЛАТЬ: привести %s к составу дерева: по одной директиве `use` на каждый "+
			"каталог с `go.mod`, включая корневой (`.`).",
			f.Kind, f.What, f.Why, workspaceExampleName)
	}
}

// TestCrossModuleWorkspaceItselfStaysOutOfTheIndex — рабочее пространство не
// уезжает в индекс, а его ОБРАЗЕЦ — уезжает.
//
// Две половины одного свойства, и порознь каждая бесполезна: отслеживаемый
// `go.work` молча поменял бы смысл `./...` для всех, а игнорируемый образец
// нельзя было бы взять копией — его бы просто не было в свежем клоне.
func TestCrossModuleWorkspaceItselfStaysOutOfTheIndex(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	if tree.count() == 0 {
		t.Fatal("обход пуст: состав дерева не прочитан — вердикт беспредметен")
	}

	for _, rel := range []string{"go.work", "go.work.sum"} {
		if tree.hasFile(rel) {
			t.Errorf("%s лежит в индексе — он меняет смысл `./...` для всякого, кто окажется "+
				"в дереве, и делает это молча. В индексе место ОБРАЗЦА (%s), а не самого "+
				"пространства.", rel, workspaceExampleName)
		}
	}

	var modules int
	for _, rel := range tree.Tree.SortedFiles() {
		if path.Base(rel) == "go.mod" {
			modules++
		}
	}
	if modules > 1 && !tree.hasFile(workspaceExampleName) {
		t.Errorf("модулей в дереве %d, а образца %s в индексе нет: взять копией нечего",
			modules, workspaceExampleName)
	}

	t.Logf("осмотрено: состав %d файлов; модулей %d; go.work в индексе: %v; %s в индексе: %v",
		tree.count(), modules, tree.hasFile("go.work"), workspaceExampleName, tree.hasFile(workspaceExampleName))
}
