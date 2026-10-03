// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// edgeunauthproducer.go — разбор для переписи производителей отказа `401` края
// (приёмка KA1, Р2; kacho#2958). Отделён от гейта, чтобы инъекция подавала ему
// синтетику, не трогая дерево.
//
// # Предмет
//
// Отказ «удостоверение не принято» обязан быть побайтово одним для всех причин
// на своей поверхности, и держится это ПОСТРОЕНИЕМ: производитель один —
// `gateway/internal/authnrefusal`. Второй производитель появляется одной строкой
// (`w.WriteHeader(http.StatusUnauthorized)` либо `status.Error(codes.Unauthenticated, …)`)
// и ничего не нарушает на вид: проба соседней полосы остаётся зелёной, а
// неразличимость причин теряется ровно на новой полосе.
//
// # Что считается производителем — узлы разбора, не слова
//
//   - селектор `http.StatusUnauthorized` вне сравнения (`==`, `!=`) и вне
//     перечня `case`: так пишется ответ, а не проверяется чужой;
//   - селектор `codes.Unauthenticated` аргументом вызова `status.Error`,
//     `status.Errorf`, `status.New`, `status.Newf`: так строится статус, а не
//     классифицируется чужой.
//
// Комментарий, строковый литерал и сравнение производителем не являются by
// construction. Форма, которой разбор не знает (`w.WriteHeader(401)` числом,
// своя константа со значением 401), им не видна — это граница, названная вслух;
// у дерева на дату гейта таких форм нет (`git grep -nE 'WriteHeader\(401\)'` в
// `gateway/` пуст).
//
// # Исключение одно, и оно не отказ
//
// Указание повысить уровень (Р3) — `401` с вызовом RFC 9470 держателю ГОДНОГО
// удостоверения. Его производители названы поимённо — файл и функция — и у
// каждого ожидаемое число мест. Запись без предмета (места нет) — находка: иначе
// исключение пережило бы своё основание молча.

// edgeUnauthHome — дом единственного производителя отказа; вне суда.
const edgeUnauthHome = "gateway/internal/authnrefusal/"

// edgeUnauthScope — где судится.
const edgeUnauthScope = "gateway/"

// edgeStepUpProducers — производители указания повысить уровень (Р3): файл#функция
// → ожидаемое число мест.
var edgeStepUpProducers = map[string]int{
	"gateway/internal/middleware/auth_stepup.go#enforceStepUpHTTP":          1,
	"gateway/internal/middleware/auth_stepup.go#enforceStepUpGRPCAssurance": 1,
	"gateway/internal/middleware/dpop_http_middleware.go#Wrap":              1,
}

// EdgeUnauthFinding — координата лишнего производителя либо исключения без
// предмета.
type EdgeUnauthFinding struct {
	File string
	Line int
	Func string
	What string
}

// EdgeUnauthCensus — объём осмотренного.
type EdgeUnauthCensus struct {
	// Files — разобранных файлов Go в области суда.
	Files int
	// Unparsed — не разобранных; они не судятся и названы отдельно.
	Unparsed int
	// Producers — найденных мест производства 401, включая исключённые.
	Producers int
	// StepUp — из них — указаний повысить уровень (Р3).
	StepUp int
}

// FindEdgeUnauthProducers находит производителей отказа 401 края вне его дома.
// sources — путь от корня репозитория → текст непроверочного файла Go.
func FindEdgeUnauthProducers(sources map[string]string) ([]EdgeUnauthFinding, EdgeUnauthCensus) {
	return findEdgeUnauthProducers(sources, edgeStepUpProducers)
}

func findEdgeUnauthProducers(sources map[string]string, allowed map[string]int) ([]EdgeUnauthFinding, EdgeUnauthCensus) {
	var (
		findings []EdgeUnauthFinding
		census   EdgeUnauthCensus
	)
	seen := map[string]int{}
	fset := token.NewFileSet()
	paths := make([]string, 0, len(sources))
	for rel := range sources {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if !strings.HasPrefix(rel, edgeUnauthScope) || strings.HasPrefix(rel, edgeUnauthHome) ||
			strings.HasSuffix(rel, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, rel, sources[rel], 0)
		if err != nil {
			census.Unparsed++
			continue
		}
		census.Files++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, pos := range edgeUnauthSites(fn.Body) {
				census.Producers++
				key := rel + "#" + fn.Name.Name
				if _, ok := allowed[key]; ok {
					census.StepUp++
					seen[key]++
					continue
				}
				findings = append(findings, EdgeUnauthFinding{
					File: rel, Line: fset.Position(pos).Line, Func: fn.Name.Name,
					What: "производитель отказа 401 вне authnrefusal",
				})
			}
		}
	}
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if seen[k] != allowed[k] {
			file, fn, _ := strings.Cut(k, "#")
			findings = append(findings, EdgeUnauthFinding{
				File: file, Func: fn,
				What: "исключение Р3 разошлось с деревом: мест " + strconv.Itoa(seen[k]) + ", объявлено " + strconv.Itoa(allowed[k]),
			})
		}
	}
	return findings, census
}

// edgeUnauthSites — позиции мест производства 401 в теле функции.
func edgeUnauthSites(body *ast.BlockStmt) []token.Pos {
	var out []token.Pos
	// Узлы, которые производителем не являются: сравнение и перечень case.
	exempt := map[ast.Node]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.EQL || v.Op == token.NEQ {
				exempt[v.X], exempt[v.Y] = true, true
			}
		case *ast.CaseClause:
			for _, e := range v.List {
				exempt[e] = true
			}
		case *ast.CallExpr:
			if edgeSelOf(v.Fun, "status", "Error", "Errorf", "New", "Newf") {
				for _, a := range v.Args {
					if edgeSelOf(a, "codes", "Unauthenticated") {
						out = append(out, a.Pos())
					}
				}
			}
		case *ast.SelectorExpr:
			if edgeSelOf(v, "http", "StatusUnauthorized") && !exempt[v] {
				out = append(out, v.Pos())
			}
		}
		return true
	})
	return out
}

func edgeSelOf(e ast.Expr, pkg string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != pkg {
		return false
	}
	for _, n := range names {
		if sel.Sel.Name == n {
			return true
		}
	}
	return false
}
