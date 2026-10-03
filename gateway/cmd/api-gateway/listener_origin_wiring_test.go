// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listener_origin_wiring_test.go — каждый HTTP-слушатель корня несёт обёртку
// происхождения (возврат go-style GS-1 по kacho#2721).
//
// Метку «внешний» ставит только `listenerorigin.ExternalListener`, метку
// «внутренний» — только `listenerorigin.InternalListener`. Слушатель без
// обёртки не отдаёт ни пути `Internal*`, ни координат церемонии: оба читателя
// метки отказывают по умолчанию. Проба держит, что корень оборачивает каждый
// слушатель `httpSrv.Serve`, внутренний — ровно один, и что `ConnContext`
// общего сервера — тот, что читает обе обёртки.
package main

import (
	"go/ast"
	"testing"
)

func TestListenerOriginWiring_EveryHTTPListenerOfTheRootCarriesAnOriginWrapper(t *testing.T) {
	fset, f := parseMain(t)
	var served, internal, external, connContext int
	var bare []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && key.Name == "ConnContext" {
				if originFunc(n.Value) == "ConnContext" {
					connContext++
				} else {
					bare = append(bare, fset.Position(n.Pos()).String()+" ConnContext не listenerorigin.ConnContext")
				}
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Serve" || len(n.Args) != 1 {
				return true
			}
			if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "httpSrv" {
				return true
			}
			served++
			wrap, ok := n.Args[0].(*ast.CallExpr)
			switch {
			case ok && originFunc(wrap.Fun) == "InternalListener":
				internal++
			case ok && originFunc(wrap.Fun) == "ExternalListener":
				external++
			default:
				bare = append(bare, fset.Position(n.Pos()).String()+" httpSrv.Serve без обёртки происхождения")
			}
		}
		return true
	})
	t.Logf("перепись main.go: httpSrv.Serve %d · InternalListener %d · ExternalListener %d · ConnContext %d · без обёртки %d",
		served, internal, external, connContext, len(bare))
	if served == 0 {
		t.Fatal("в main.go не найдено ни одного httpSrv.Serve — проба судит пустоту")
	}
	if len(bare) > 0 {
		t.Fatalf("слушатель корня без обёртки происхождения — координаты церемонии на нём отказывают, "+
			"но и внешним он не помечен: %v", bare)
	}
	if internal != 1 || external == 0 || connContext != 1 {
		t.Fatalf("внутренних слушателей %d (ждали 1), внешних %d (ждали ≥1), ConnContext %d (ждали 1)", internal, external, connContext)
	}
}

// originFunc — имя функции пакета `listenerorigin`, на которую указывает
// выражение, либо пустая строка.
func originFunc(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "listenerorigin" {
		return ""
	}
	return sel.Sel.Name
}
