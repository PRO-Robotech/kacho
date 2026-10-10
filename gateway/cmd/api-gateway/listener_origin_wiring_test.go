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
// общего сервера — тот, что читает обе обёртки. Корень — весь пакет
// cmd/api-gateway без тестов: сервер и подъём слушателей вынесены из main.go в
// edge_listener.go (kacho#3125).
//
// Второе звено ConnContext (kacho#3028, C4) — состояние TLS соединения
// (linktls.WithConnState): за мультиплексором r.TLS пуст, и звено фронта,
// предъявившее сертификат, иначе было бы неотличимо от любого пира. Тем же
// доводом сервер gRPC несёт учётные данные linktls.ServerCredentials.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// parseRootPackage разбирает все не-тестовые файлы пакета корня. Пустой разбор
// — отказ: проба по пустому корню ничего не утверждает.
func parseRootPackage(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("состав пакета корня не читается: %v", err)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("%s: %v", name, rerr)
		}
		f, perr := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("%s не разбирается: %v", name, perr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("в пакете корня не найдено ни одного не-тестового файла")
	}
	return fset, files
}

func TestListenerOriginWiring_EveryHTTPListenerOfTheRootCarriesAnOriginWrapper(t *testing.T) {
	fset, files := parseRootPackage(t)
	var served, internal, external, connContext, linkState int
	var bare []string
	inspect := func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && key.Name == "ConnContext" {
				inner := n.Value
				if call, ok := n.Value.(*ast.CallExpr); ok && pkgFunc(call.Fun, "linktls") == "WithConnState" && len(call.Args) == 1 {
					linkState++
					inner = call.Args[0]
				}
				if originFunc(inner) == "ConnContext" {
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
	}
	for _, f := range files {
		ast.Inspect(f, inspect)
	}
	t.Logf("перепись пакета корня (файлов %d): httpSrv.Serve %d · InternalListener %d · ExternalListener %d · ConnContext %d · состояние TLS %d · без обёртки %d",
		len(files), served, internal, external, connContext, linkState, len(bare))
	if served == 0 {
		t.Fatal("в пакете корня не найдено ни одного httpSrv.Serve — проба судит пустоту")
	}
	if len(bare) > 0 {
		t.Fatalf("слушатель корня без обёртки происхождения — координаты церемонии на нём отказывают, "+
			"но и внешним он не помечен: %v", bare)
	}
	if internal != 1 || external == 0 || connContext != 1 {
		t.Fatalf("внутренних слушателей %d (ждали 1), внешних %d (ждали ≥1), ConnContext %d (ждали 1)", internal, external, connContext)
	}
	if linkState != 1 {
		t.Fatalf("ConnContext без linktls.WithConnState (%d) — звено фронта за мультиплексором не узнать по сертификату", linkState)
	}
}

// Сервер gRPC корня несёт учётные данные linktls: без них за мультиплексором
// peer.AuthInfo пуст, и сертификат звена на нативном пути не виден (C4).
func TestListenerOriginWiring_GRPCServerCarriesTheLinkCredentials(t *testing.T) {
	_, f := parseMain(t)
	var servers, withCreds int
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || pkgFunc(call.Fun, "proxy") != "NewServer" {
			return true
		}
		servers++
		for _, a := range call.Args {
			opt, ok := a.(*ast.CallExpr)
			if !ok || pkgFunc(opt.Fun, "grpc") != "Creds" || len(opt.Args) != 1 {
				continue
			}
			if inner, ok := opt.Args[0].(*ast.CallExpr); ok && pkgFunc(inner.Fun, "linktls") == "ServerCredentials" {
				withCreds++
			}
		}
		return true
	})
	t.Logf("перепись main.go: proxy.NewServer %d · с linktls.ServerCredentials %d", servers, withCreds)
	if servers == 0 {
		t.Fatal("proxy.NewServer в main.go не найден — проба судит пустоту")
	}
	if withCreds != servers {
		t.Fatalf("серверов gRPC %d, с учётными данными linktls %d", servers, withCreds)
	}
}

// pkgFunc — имя функции пакета pkg, на которую указывает выражение, либо "".
func pkgFunc(e ast.Expr, pkg string) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != pkg {
		return ""
	}
	return sel.Sel.Name
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
