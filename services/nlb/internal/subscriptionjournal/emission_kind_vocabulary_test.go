// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEveryEmissionNamesTheKindByTheCanonicalConstant — СЛОВО ВИДА ОБЪЯВЛЕНО ОДИН РАЗ.
//
// # Предмет
//
// Вид предмета журнала (`nlb_load_balancer` / `nlb_listener` / `nlb_target_group`)
// живёт константой общего пакета (`repo/kacho.OutboxResource*`), и её же читает
// объявление журнала подписки. Собственная копия того же значения в пакете
// use-case вреда во время исполнения не приносит — значения совпадают, — а вред
// приносит ПЕРЕПИСИ, и он уже наступал: предикат «где эмитится этот вид»,
// записанный по канонической константе, отвечал «точек нет» там, где их четыре,
// и по нему выходило, что обогащать у слушателя нечего (#1550).
//
// Цена ошибки не гипотетическая: тем же способом была занижена цена обогащения
// балансировщика — шапка этого пакета называла ПЯТЬ точек Go при семи, потому
// что две из них лежат в пакете слушателя и зовут копию.
//
// # Почему разбор, а не поиск по образцу
//
// Слова видов стоят в объяснениях — и в этом файле тоже. Проверка по подстроке
// краснела бы на собственном комментарии. Здесь судится УЗЕЛ: аргумент вида
// берётся позиционно из вызова `Emit` журнала и обязан быть обращением к
// константе общего пакета.
//
// # Чего проверка НЕ утверждает
//
// Она не утверждает, что нагрузка полна — это предмет разбора эмиттеров по видам
// (`TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`). Она утверждает, что
// СЛОВАРЬ видов один, то есть что перепись по канонической константе видит все
// точки эмиссии, а не часть.
func TestEveryEmissionNamesTheKindByTheCanonicalConstant(t *testing.T) {
	const (
		vocabularyPkg    = "kachorepo"
		vocabularyPrefix = "OutboxResource"
	)

	var findings []string
	res := inspectEmissions(t, func(e emission) {
		sel, ok := e.kind.(*ast.SelectorExpr)
		if ok {
			pkg, isIdent := sel.X.(*ast.Ident)
			if isIdent && pkg.Name == vocabularyPkg && strings.HasPrefix(sel.Sel.Name, vocabularyPrefix) {
				return
			}
		}
		findings = append(findings, e.pos)
	})

	kinds := make([]string, 0, len(res.byKind))
	for k := range res.byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	t.Logf("перепись: файлов осмотрено %d · вызовов Emit журнала найдено %d · слов вида различных %d %v",
		res.filesRead, res.emitsSeen, len(kinds), kinds)

	if res.filesRead == 0 {
		t.Fatal("не осмотрено ни одного файла — проверка беспредметна, а не пройдена")
	}
	if res.emitsSeen == 0 {
		t.Fatal("не найдено ни одной точки эмиссии журнала — предмета у проверки нет. Если " +
			"эмиссия переехала, проверка обязана покраснеть, а не молча одобрить любое дерево")
	}
	for _, pos := range findings {
		t.Errorf("%s: вид предмета назван НЕ канонической константой %s.%s*.\n"+
			"Второе объявление того же слова делает точку эмиссии невидимой переписи по канону: "+
			"предикат «где эмитится этот вид» ответит «нигде» там, где точка есть, и цена "+
			"обогащения будет занижена молча", pos, vocabularyPkg, vocabularyPrefix)
	}
}

// emission — одна точка эмиссии строки журнала: узлы аргументов вида и нагрузки.
type emission struct {
	pos     string
	kind    ast.Expr
	payload ast.Expr
}

type emissionCensus struct {
	filesRead int
	emitsSeen int
	byKind    map[string]int
}

// kindLiteralName — имя константы вида, как оно записано в аргументе, либо "" если
// аргумент не является обращением к именованной константе.
func kindLiteralName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := v.X.(*ast.Ident); ok {
			return pkg.Name + "." + v.Sel.Name
		}
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// inspectEmissions обходит не-тестовое дерево use-case'ов nlb и зовёт visit на
// КАЖДОМ вызове `Emit` журнала (шесть аргументов: ctx, вид, идентификатор,
// проект, род, нагрузка). Очередь регистрации прав зовёт свой `Emit` с тремя
// аргументами и под этот отбор не попадает by construction.
func inspectEmissions(t *testing.T, visit func(emission)) emissionCensus {
	t.Helper()

	const (
		emitName     = "Emit"
		emitArgCount = 6
		kindArgIdx   = 1
		payloadArg   = 5
	)
	roots := []string{
		filepath.Join("..", "apps", "kacho", "api"),
		filepath.Join("..", "apps", "kacho", "jobs"),
	}

	res := emissionCensus{byKind: map[string]int{}}
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				t.Fatalf("%s не разобран: %v — гейт судит по узлам, и неосмотренный файл его "+
					"молчания не оправдывает", path, parseErr)
			}
			res.filesRead++
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != emitName || len(call.Args) != emitArgCount {
					return true
				}
				res.emitsSeen++
				res.byKind[kindLiteralName(call.Args[kindArgIdx])]++
				visit(emission{
					pos:     fset.Position(call.Pos()).String(),
					kind:    call.Args[kindArgIdx],
					payload: call.Args[payloadArg],
				})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s не завершён: %v", root, err)
		}
	}
	return res
}
