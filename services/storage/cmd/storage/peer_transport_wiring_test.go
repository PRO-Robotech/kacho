// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// peer_transport_wiring_test.go — страж РАЗМЕЩЕНИЯ: у каждого исходящего ребра,
// которое поднимает композиционный корень, обязан быть держатель в загрузочной
// страже.
//
// Поведенческие замки (что именно отвергается) живут в
// internal/config/peer_transport_test.go. Этот файл закрывает другой класс:
// поведенческий тест остаётся зелёным навсегда, а завтра здесь появится ЕЩЁ
// ОДНО ребро — с новым адресом и новой ручкой, — и страж о нём ничего не узнает.
// Ребро поднимется, insecure-creds не дадут ошибки, процесс отчитается «peer edge
// configured», и контроль снова будет присутствовать, ничего не проверяя.
//
// Проверка читает ИСПОЛНЯЕМУЮ часть (разбор AST), а не текст: имя поля в
// комментарии или в строковом литерале держателем не является. Разбирается пара
// «адрес, creds» из каждого вызова dialPeer — это ровно тот предикат, по которому
// проводка решает, дилить ли ребро (dialPeer возвращает nil на пустом адресе), —
// и требуется, чтобы ОБА имени встречались в исходнике стража.
//
// Предпосылка проверяется: ноль найденных вызовов — находка, а не «всё чисто».
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
	storageServeSrc    = "serve.go"
	storageGuardSrc    = "../../internal/config/validate.go"
	storageDialFn      = "dialPeer"
	storageGuardMethod = "Validate"
)

// peerEdge — одно исходящее ребро, каким его видит композиционный корень.
type peerEdge struct {
	addr  string // имя поля конфигурации с адресом (предикат «дилится ли»)
	creds string // имя поля конфигурации с клиентскими creds
}

// dialledEdges — все вызовы dialPeer(cfg.<Addr>, cfg.<Creds>, …) в корне.
func dialledEdges(t *testing.T, path string) []peerEdge {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse composition root %s: %v", path, err)
	}
	var out []peerEdge
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != storageDialFn || len(call.Args) < 2 {
			return true
		}
		addr, aok := selectorField(call.Args[0])
		creds, cok := selectorField(call.Args[1])
		if !aok || !cok {
			t.Errorf("%s: a %s(…) call whose address/credentials are not plain config fields — "+
				"the guard cannot be matched against it, so either keep the form or extend this gate",
				path, storageDialFn)
			return true
		}
		out = append(out, peerEdge{addr: addr, creds: creds})
		return true
	})
	return out
}

// selectorField возвращает имя поля из выражения вида `cfg.Field`.
func selectorField(e ast.Expr) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, ok := sel.X.(*ast.Ident); !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// guardFields — имена полей, которые читает тело загрузочной стражи. Берутся из
// AST метода, а не грепом по файлу: имя, упомянутое в комментарии рядом, держателем
// не является.
func guardFields(t *testing.T, path string) map[string]bool {
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
		if !ok || fn.Name.Name != storageGuardMethod || fn.Recv == nil || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if name, ok := selectorField(sel); ok {
				fields[name] = true
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s: boot guard %s not found — the gate's own premise is gone, "+
			"fix the gate rather than assume the tree is clean", path, storageGuardMethod)
	}
	return fields
}

// TestEveryDialledPeerEdgeIsHeldByTheBootGuard — ядро гейта.
func TestEveryDialledPeerEdgeIsHeldByTheBootGuard(t *testing.T) {
	if _, err := os.Stat(storageGuardSrc); err != nil {
		t.Fatalf("boot guard source is not where this gate expects it (%s): %v", storageGuardSrc, err)
	}
	edges := dialledEdges(t, storageServeSrc)
	if len(edges) == 0 {
		t.Fatalf("%s: no %s(…) call found — either the composition root changed shape or this gate "+
			"stopped reading it; a gate that inspects nothing must fail, not pass",
			storageServeSrc, storageDialFn)
	}
	held := guardFields(t, storageGuardSrc)

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
				"cleartext silently, so this edge would start unprotected and report itself configured",
				e.addr, e.creds, e.creds)
		}
	}
	sort.Strings(names)
	t.Logf("examined %d dialled peer edge(s) in %s against %s.%s: %s",
		len(edges), storageServeSrc, strings.TrimPrefix(storageGuardSrc, "../../"), storageGuardMethod,
		strings.Join(names, ", "))
}
