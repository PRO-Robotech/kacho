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
	"testing"
)

// edgemetricsroute_test.go — обработчик экспозиции края провязан РОВНО на
// диагностическую поверхность и ни на один другой слушатель.
//
// # Предмет
//
// Край — единственный процесс платформы, чьи слушатели досягаемы снаружи
// кластера. Счётчики процесса на арендаторской поверхности — это сведения об
// инфраструктуре там, где им не место (`security.md` §«Инфра-чувствительные
// данные»), и заметить такое по ответу нельзя: `GET /metrics` на публичном
// слушателе отвечает `200` и выглядит как исправная диагностика.
//
// # Почему гейт, а не только сквозной кейс
//
// Сквозная проба утверждает то же самое обращением к внешнему адресу, но она
// зависит от поднятого стенда и от того, что маршрут кто-то попробовал. Здесь
// проверяется ДЕТЕРМИНИРОВАННАЯ половина: провязка одна, и она в объявлении
// диагностической поверхности. Вторая провязка появляется одной строкой, и без
// этого гейта её увидит только тот, кто пойдёт смотреть.

// metricsRoute — одно место, где край регистрирует маршрут экспозиции.
type metricsRoute struct {
	where string
	fn    string // функция, в которой сделана регистрация
}

// TestEdgeMetricsRouteIsMountedOnTheDiagnosticSurfaceOnly — маршрут экспозиции у
// края ровно один, и он в объявлении диагностической поверхности.
func TestEdgeMetricsRouteIsMountedOnTheDiagnosticSurfaceOnly(t *testing.T) {
	// Имя функции, объявляющей диагностическую поверхность края. Разъедется с
	// кодом — гейт упадёт на своей предпосылке (ниже), а не промолчит.
	const surfaceFn = "describeDiagnosticSurface"

	root := repoRoot(t)
	var routes []metricsRoute
	surfaceSeen := false
	scanned := 0

	walkOwnerRegisterGoFiles(t, root, []string{"gateway"}, func(rel string, body []byte) {
		scanned++
		src := string(body)
		if !strings.Contains(src, "/metrics") && !strings.Contains(src, surfaceFn) {
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Name.Name == surfaceFn {
				surfaceSeen = true
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				pattern, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || !strings.Contains(pattern, "/metrics") {
					return true
				}
				routes = append(routes, metricsRoute{
					where: rel + ":" + fmtLine(fset, call.Lparen),
					fn:    fn.Name.Name,
				})
				return true
			})
		}
	})

	t.Logf("осмотрено не-тестовых файлов Go края: %d; маршрутов экспозиции: %d", scanned, len(routes))
	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	// Предпосылка гейта: функция, которую он считает единственным законным
	// местом, существует. Переименуют — гейт обязан упасть, а не начать считать
	// законным всё подряд.
	if !surfaceSeen {
		t.Fatalf("в дереве края нет функции %s: предпосылка гейта отпала — либо объявление "+
			"поверхности переименовано, либо его больше нет, и тогда маршрут экспозиции "+
			"провязан неизвестно куда", surfaceFn)
	}
	if len(routes) == 0 {
		t.Fatal("край не регистрирует маршрут экспозиции ни в одном не-тестовом файле: " +
			"величины решений о доступе никуда не выходят")
	}

	var findings []string
	for _, r := range routes {
		if r.fn == surfaceFn {
			continue
		}
		findings = append(findings, r.where+" — маршрут экспозиции провязан в "+r.fn+
			"(), а не в "+surfaceFn+"(). У края слушатели досягаемы СНАРУЖИ кластера: "+
			"счётчики процесса на арендаторской поверхности — сведения об инфраструктуре там, "+
			"где им не место, и отличить это по ответу нельзя")
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("экспозиция края выставлена не только на диагностическую поверхность:\n  %s",
			strings.Join(findings, "\n  "))
	}
	if len(routes) > 1 {
		t.Fatalf("маршрутов экспозиции у края %d, а поверхность одна: вторая регистрация в том же "+
			"объявлении означает второй слушатель либо копию, которая разойдётся с первой молча",
			len(routes))
	}
}
