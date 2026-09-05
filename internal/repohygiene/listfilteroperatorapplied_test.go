// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listfilteroperatorapplied_test.go — гейт против владельца, который забирает из
// разобранного выражения ЗНАЧЕНИЕ и теряет вместе с ним ОПЕРАТОР.
//
// # Предмет (#460)
//
// `filter.Parse` возвращает узел из трёх частей: поле, оператор, значение.
// Владелец, который читает только `.Value` и строит предикат с зашитым `=`,
// отвечает РАВЕНСТВОМ на любой запрос — в том числе на запрос подстроки. Это не
// «ничего не нашлось»: это уверенный и неверный ответ, который вызывающий примет
// за результат поиска.
//
// Пока грамматика знала один оператор, находки быть не могло: зашитый `=`
// совпадал с разобранным by construction. Второй оператор (`CONTAINS`, строка
// поиска в консоли) сделал предмет живым, не тронув ни одной строки у владельцев,
// — правка была в грамматике, а неверными стали места, которых она не касалась.
// Диффом такое не видно ни с одной стороны.
//
// # Отличие от соседнего гейта
//
// `listfilterapplied_test.go` требует читать разобранное ПОЛЕ и срабатывает
// только при whitelist'е больше одного поля. Он про то, по КАКОЙ КОЛОНКЕ ищут.
// Этот — про то, КАКИМ ОТНОШЕНИЕМ, и он действует при whitelist'е любого
// размера: владельцу с единственным полем `name` терять оператор так же легко.
// Два разных вопроса об одном вызове, поэтому два гейта, а не один.
//
// # Что именно считается находкой
//
// Функция, которая читает `.Value` у разобранного узла и при этом НЕ применяет
// оператор — не зовёт `.ToSQL`/`.ToSQLOn` (они его применяют) и не читает `.Op`
// (значит, не решает про него явно: ни реализовать, ни отвергнуть).
//
// Разрешённых исходов у оператора ровно два, и оба видны синтаксически:
// применить его либо назвать отказом. Молча свести к равенству — не исход
// (`api-conventions.md` §«Принято-и-проигнорировано — ЗАПРЕЩЕНО»).
//
// # Косвенный разбор прослеживается
//
// Владелец не обязан звать `filter.Parse` сам: iam зовёт его через пакетный
// помощник, который лишь возвращает узел. Гейт, знающий только прямые вызовы,
// имел бы слепое пятно ровно там, где класс и прятался, поэтому проход
// двухфазный: сперва в пакете находятся помощники, чьё тело зовёт `filter.Parse`,
// затем вызовы этих помощников считаются такими же местами разбора.
//
// Сам помощник находкой не является: он ничего не решает, а передаёт узел
// дальше. Решает — и отвечает — тот, кто читает `.Value`.
//
// # Чего гейт НЕ утверждает
//
// Он не проверяет, что применённый оператор осмыслен для этой колонки, и не
// читает SQL. Это не выводится синтаксически и держится пробами владельцев.
// Объём осмотренного печатается, поэтому «ноль находок» отличимо от «ноль
// прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// opSite — функция, читающая значение разобранного выражения.
type opSite struct {
	file      string
	line      int
	fn        string
	honoursOp bool // зовёт ToSQL/ToSQLOn либо читает .Op
	viaHelper string
}

// collectFilterOperatorSites обходит не-тестовое дерево и возвращает места, где
// читается `.Value` разобранного выражения.
func collectFilterOperatorSites(t *testing.T, roots []string) (sites []opSite, filesRead int) {
	t.Helper()
	repo := repoRoot(t)

	type parsedFile struct {
		path string
		rel  string
		fset *token.FileSet
		f    *ast.File
	}
	var files []parsedFile
	// Помощники разбора: пакет → имя функции, чьё тело зовёт filter.Parse и
	// которая возвращает узел (то есть передаёт решение вызывающему).
	helpers := map[string]map[string]bool{}

	// Состав берётся из ИНДЕКСА, а не с диска: правила игнорирования на обход по
	// диску не действуют, и он видел бы то, чего в репозитории нет (`treewalkindex`).
	tree, err := treecorpus.NewTree(repo)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	wanted := func(rel string) bool {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return false
		}
		// pkg/filter — дом самой грамматики: его собственные чтения .Value не
		// являются применением оператора к чужому предикату.
		if strings.HasPrefix(rel, "pkg/filter/") {
			return false
		}
		for _, root := range roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return true
			}
		}
		return false
	}

	for _, rel := range tree.SortedFiles() {
		if !wanted(rel) {
			continue
		}
		path := filepath.Join(repo, rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			continue
		}
		filesRead++
		files = append(files, parsedFile{path, rel, fset, f})

		pkgKey := filepath.Dir(path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv != nil {
				continue
			}
			if !callsFilterParse(fd.Body) || !returnsFilterAST(fd.Type) {
				continue
			}
			if helpers[pkgKey] == nil {
				helpers[pkgKey] = map[string]bool{}
			}
			helpers[pkgKey][fd.Name.Name] = true
		}
	}

	for _, pf := range files {
		pkgKey := filepath.Dir(pf.path)
		for _, decl := range pf.f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// Сам помощник решения не принимает — он отдаёт узел дальше.
			if fd.Recv == nil && helpers[pkgKey][fd.Name.Name] {
				continue
			}
			via := ""
			if !callsFilterParse(fd.Body) {
				via = calledHelper(fd.Body, helpers[pkgKey])
				if via == "" {
					continue // выражение здесь не разбирают
				}
			}
			readsValue, honours := false, false
			var valueLine int
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					switch node.Sel.Name {
					case "Value":
						if !readsValue {
							valueLine = pf.fset.Position(node.Pos()).Line
						}
						readsValue = true
					case "Op":
						honours = true
					}
				case *ast.CallExpr:
					if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "ToSQL" || sel.Sel.Name == "ToSQLOn" {
							honours = true
						}
					}
				}
				return true
			})
			if !readsValue {
				continue
			}
			sites = append(sites, opSite{
				file: pf.rel, line: valueLine, fn: fd.Name.Name,
				honoursOp: honours, viaHelper: via,
			})
		}
	}
	return sites, filesRead
}

func callsFilterParse(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Parse" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "filter" {
			found = true
		}
		return true
	})
	return found
}

// returnsFilterAST — возвращает ли функция разобранный узел (`*filter.FilterAST`).
func returnsFilterAST(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	for _, res := range ft.Results.List {
		star, ok := res.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "FilterAST" {
			return true
		}
	}
	return false
}

// calledHelper — имя вызванного в теле помощника разбора, если он есть.
func calledHelper(body *ast.BlockStmt, pkgHelpers map[string]bool) string {
	out := ""
	if len(pkgHelpers) == 0 {
		return ""
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && pkgHelpers[id.Name] {
			out = id.Name
		}
		return true
	})
	return out
}

// TestFilterOwnerHonoursTheParsedOperator — владелец, читающий значение
// разобранного выражения, обязан применить его ОПЕРАТОР либо назвать
// неподдерживаемый отказом.
func TestFilterOwnerHonoursTheParsedOperator(t *testing.T) {
	sites, filesRead := collectFilterOperatorSites(t, []string{"services", "gateway", "pkg"})

	var findings []string
	honoured := 0
	for _, s := range sites {
		if s.honoursOp {
			honoured++
			continue
		}
		via := ""
		if s.viaHelper != "" {
			via = fmt.Sprintf(" (разбор через помощник %s)", s.viaHelper)
		}
		findings = append(findings, fmt.Sprintf(
			"%s:%d — функция %s читает значение разобранного выражения%s, "+
				"но не применяет его оператор: нет ни .ToSQL/.ToSQLOn, ни чтения .Op. "+
				"Предикат с зашитым `=` ответит РАВЕНСТВОМ на запрос подстроки — "+
				"уверенно и неверно. Исходов два: применить оператор либо отвергнуть "+
				"неподдерживаемый явно, назвав его",
			s.file, s.line, s.fn, via))
	}
	sort.Strings(findings)

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("осмотрено: %d не-тестовых файлов, %d мест чтения значения разобранного выражения, из них применяют оператор %d",
		filesRead, len(sites), honoured)

	// Проверка СВОЕЙ предпосылки. Гейт осмыслен, только если он вообще нашёл
	// владельцев; ноль мест означает, что разбор переехал или сломался обход, а не
	// что дерево чисто.
	if filesRead < 500 {
		t.Fatalf("прочитано всего %d файлов — обход не добрался до дерева, вердикт недействителен", filesRead)
	}
	if len(sites) == 0 {
		t.Fatal("не найдено ни одного места чтения разобранного выражения — " +
			"предпосылка гейта не выполняется, вердикт недействителен")
	}

	if len(findings) > 0 {
		t.Fatalf("владельцев, теряющих оператор фильтра: %d\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
