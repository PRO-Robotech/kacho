// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// peer_transport_wiring_test.go — страж РАЗМЕЩЕНИЯ: у каждого исходящего ребра,
// которое поднимает композиционный корень, обязан быть держатель в загрузочной
// страже.
//
// Поведенческие замки (что именно отвергается) живут в peer_transport_test.go.
// Этот файл закрывает другой класс: поведенческий тест остаётся зелёным навсегда,
// а завтра здесь появится ЕЩЁ ОДНО ребро — со своим адресом и своей ручкой, — и
// страж о нём ничего не узнает. Ребро поднимется, insecure-creds не дадут ошибки,
// лог отчитается «edges wired», и контроль снова будет присутствовать, ничего не
// проверяя. Ровно так registry и жил до этой правки: три ручки, ноль держателей.
//
// Проверка читает ИСПОЛНЯЕМУЮ часть (разбор AST), а не текст: имя поля в
// комментарии или в строковом литерале держателем не является. Разбирается пара
// «адрес, creds» — условие `if cfg.<Addr> != ""` и вызов
// grpcclient.TLSClientTransportCreds(cfg.<Creds>) внутри него, — то есть ровно та
// форма, которой корень решает, дилить ли ребро.
//
// Предпосылка проверяется: ноль найденных рёбер — находка, а не «всё чисто».
// Объём осмотренного печатается, чтобы «ноль нарушений» было отличимо от «ноль
// прочитанного».

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

const (
	registryServeSrc     = "serve.go"
	registryCredsFn      = "TLSClientTransportCreds"
	registryGuardFn      = "requirePeerTransport"
	registryRunServeFn   = "runServe"
	registryEdgeMinCount = 3 // authz, project, geo — предпосылка гейта, см. ниже
)

type registryPeerEdge struct {
	addr  string
	creds string
}

// selectorName возвращает имя поля из выражения вида `cfg.Field`.
func selectorName(e ast.Expr) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, ok := sel.X.(*ast.Ident); !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// addrGuardField — поле адреса из условия `cfg.<Addr> != ""`.
func addrGuardField(cond ast.Expr) (string, bool) {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return "", false
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || lit.Value != `""` {
		return "", false
	}
	return selectorName(bin.X)
}

// dialledRegistryEdges — рёбра, поднимаемые композиционным корнем: условие на
// адрес + клиентские creds внутри его тела.
func dialledRegistryEdges(t *testing.T, path string) []registryPeerEdge {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse composition root %s: %v", path, err)
	}
	var root *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == registryRunServeFn && fn.Body != nil {
			root = fn
		}
	}
	if root == nil {
		t.Fatalf("%s: composition root %s not found — the gate's own premise is gone, "+
			"fix the gate rather than assume the tree is clean", path, registryRunServeFn)
	}
	var out []registryPeerEdge
	ast.Inspect(root.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		addr, ok := addrGuardField(ifs.Cond)
		if !ok {
			return true
		}
		ast.Inspect(ifs.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fn.Sel.Name != registryCredsFn {
				return true
			}
			if creds, ok := selectorName(call.Args[0]); ok {
				out = append(out, registryPeerEdge{addr: addr, creds: creds})
			}
			return true
		})
		return true
	})
	return out
}

// guardBodyFields — имена полей, которые читает тело загрузочной стражи.
func guardBodyFields(t *testing.T, path, fnName string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse boot guard %s: %v", path, err)
	}
	fields := map[string]bool{}
	var found bool
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != fnName || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if name, ok := selectorName(sel); ok {
					fields[name] = true
				}
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s: boot guard %s not found — the gate's own premise is gone", path, fnName)
	}
	return fields
}

// TestEveryDialledPeerEdgeIsHeldByTheBootGuard — ядро гейта.
func TestEveryDialledPeerEdgeIsHeldByTheBootGuard(t *testing.T) {
	if _, err := os.Stat(registryServeSrc); err != nil {
		t.Fatalf("composition root is not where this gate expects it (%s): %v", registryServeSrc, err)
	}
	edges := dialledRegistryEdges(t, registryServeSrc)
	// Предпосылка гейта: корень поднимает рёбра именно этой формой. Меньше
	// известного минимума — не «стало чище», а «гейт перестал читать корень».
	if len(edges) < registryEdgeMinCount {
		t.Fatalf("%s: found %d dialled peer edge(s), expected at least %d (authz, project, geo) — "+
			"either the composition root changed shape or this gate stopped reading it; "+
			"a gate that inspects less than its premise must fail, not pass",
			registryServeSrc, len(edges), registryEdgeMinCount)
	}
	held := guardBodyFields(t, registryServeSrc, registryGuardFn)

	var names []string
	for _, e := range edges {
		names = append(names, e.addr+"/"+e.creds)
		if !held[e.addr] {
			t.Errorf("edge %s/%s: the boot guard never reads %s, so it cannot tell whether this edge is "+
				"dialled at all — an edge the guard does not see is an edge whose transport nobody requires",
				e.addr, e.creds, e.addr)
		}
		if !held[e.creds] {
			t.Errorf("edge %s/%s: the boot guard never reads %s — unarmed client credentials degrade to "+
				"cleartext silently, so this edge would start unprotected and report itself wired",
				e.addr, e.creds, e.creds)
		}
	}
	sort.Strings(names)
	t.Logf("examined %d dialled peer edge(s) in %s against %s: %s",
		len(edges), registryServeSrc, registryGuardFn, strings.Join(names, ", "))
}
