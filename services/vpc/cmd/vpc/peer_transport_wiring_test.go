// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// peer_transport_wiring_test.go — страж РАЗМЕЩЕНИЯ: у каждого исходящего ребра,
// которое поднимает композиционный корень, обязан быть держатель в загрузочной
// страже.
//
// Поведенческие замки (что именно отвергается) живут в
// internal/apps/kacho/config (ValidatePeerTransport и его пробы). Этот файл
// закрывает другой класс: поведенческий тест остаётся зелёным навсегда, а завтра
// здесь появится ЕЩЁ ОДНО ребро — со своей ручкой, — и страж о нём ничего не
// узнает. Ребро поднимется, невзведённые клиентские creds не дадут ошибки (они
// вырождаются в insecure БЕЗ ошибки), процесс отчитается «edge configured», и
// контроль снова будет присутствовать, ничего не проверяя.
//
// Что читается. Разбор AST композиционного корня, а не текст: имя ручки в
// комментарии или в строковом литерале держателем не является. Единица —
// ОБРАЩЕНИЕ ЗА КЛИЕНТСКИМИ CREDS ребра (`mtlsCfg.<Имя>ClientCreds`): именно оно
// решает, чем ребро будет говорить, и оно есть у КАЖДОГО ребра, включая те, что
// поднимаются не через общий dialPeer (регистрационное ребро дилится своим
// путём). Ручка ребра называется по тому же корню — `<Имя>MTLS`, — и от стража
// требуется читать её.
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
	vpcRootSrc     = "main.go"
	vpcGuardSrc    = "../../internal/apps/kacho/config/validate.go"
	vpcCredsSuffix = "ClientCreds"
	vpcKnobSuffix  = "MTLS"
	vpcGuardMethod = "ValidatePeerTransport"
)

// vpcDialledEdges — имена рёбер, за creds которых корень обращается к настройкам.
// Имя ребра — корень аксессора: `IAMProjectClientCreds` → `IAMProject`.
func vpcDialledEdges(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse composition root %s: %v", path, err)
	}
	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if !strings.HasSuffix(sel.Sel.Name, vpcCredsSuffix) {
			return true
		}
		// Обращение обязано идти к значению настроек, а не к произвольному
		// выражению: иначе ребро проехало бы мимо этого стража молча.
		if _, ok := sel.X.(*ast.Ident); !ok {
			return true
		}
		seen[strings.TrimSuffix(sel.Sel.Name, vpcCredsSuffix)] = true
		return true
	})
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// vpcGuardFields — имена полей, которые читает ТЕЛО загрузочной стражи. Берутся
// из AST метода, а не грепом по файлу: имя, упомянутое в комментарии рядом,
// держателем не является.
func vpcGuardFields(t *testing.T, path string) map[string]bool {
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
		if !ok || fn.Name.Name != vpcGuardMethod || fn.Recv == nil || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				fields[sel.Sel.Name] = true
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s: загрузочная стража %s не найдена — предпосылка самого стража исчезла, "+
			"чини стража, а не считай дерево чистым", path, vpcGuardMethod)
	}
	return fields
}

// TestEveryDialledPeerEdgeIsHeldByTheBootGuard — ядро гейта.
func TestEveryDialledPeerEdgeIsHeldByTheBootGuard(t *testing.T) {
	if _, err := os.Stat(vpcGuardSrc); err != nil {
		t.Fatalf("исходник загрузочной стражи не там, где его ждёт этот гейт (%s): %v", vpcGuardSrc, err)
	}
	edges := vpcDialledEdges(t, vpcRootSrc)
	if len(edges) == 0 {
		t.Fatalf("%s: не найдено ни одного обращения за клиентскими creds ребра — либо корень "+
			"сменил форму, либо гейт перестал его читать; гейт, ничего не осмотревший, обязан "+
			"падать, а не проходить", vpcRootSrc)
	}
	held := vpcGuardFields(t, vpcGuardSrc)

	for _, edge := range edges {
		knob := edge + vpcKnobSuffix
		if !held[knob] {
			t.Errorf("ребро %s: загрузочная стража ни разу не читает %s — ребро, которого стража "+
				"не видит, это ребро, чей транспорт никто не требует. Невзведённые клиентские "+
				"creds вырождаются в insecure БЕЗ ошибки, поэтому процесс поднимется и отчитается "+
				"о настроенном ребре", edge, knob)
		}
	}
	t.Logf("осмотрено: рёбер в %s — %d (%s), полей в теле %s — %d",
		vpcRootSrc, len(edges), strings.Join(edges, ", "), vpcGuardMethod, len(held))
}
