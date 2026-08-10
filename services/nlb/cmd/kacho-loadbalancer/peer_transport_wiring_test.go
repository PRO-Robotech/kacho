// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// peer_transport_wiring_test.go — страж РАЗМЕЩЕНИЯ: у каждого исходящего ребра,
// которое поднимает композиционный корень, обязан быть держатель в загрузочной
// страже.
//
// Поведенческие замки (что именно отвергается) живут в
// internal/apps/kacho/config (Validate и его пробы), а форму самой таблицы рёбер
// держит dialpeers_mtls_test.go. Этот файл закрывает другой класс: оба остаются
// зелёными навсегда, а завтра в таблице появится ЕЩЁ ОДНО ребро — со своей
// ручкой, — и страж о нём ничего не узнает. Ребро поднимется, невзведённые
// клиентские creds не дадут ошибки (они вырождаются в insecure БЕЗ ошибки),
// процесс отчитается «peers dialled», и контроль снова будет присутствовать,
// ничего не проверяя.
//
// Что читается. Разбор AST, а не текст: имя ручки в комментарии или в строковом
// литерале держателем не является. Единица — поле `mtls:` в объявлении таблицы
// рёбер (`peerDialSpecs`): именно оно решает, чем ребро будет говорить. От
// стражи требуется читать ТО ЖЕ поле — где угодно в её пакете конфигурации, но в
// исполняемой части.
//
// Предпосылка проверяется: ноль найденных рёбер — находка, а не «всё чисто».
// Объём осмотренного печатается, чтобы «ноль нарушений» было отличимо от «ноль
// прочитанного».

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

const (
	nlbRootSrc      = "main.go"
	nlbGuardDir     = "../../internal/apps/kacho/config"
	nlbSpecsFn      = "peerDialSpecs"
	nlbMTLSSelector = "MTLS"
)

// nlbDialledEdges — имена ручек транспорта из объявления таблицы рёбер:
// `mtls: cfg.MTLS.<Имя>` → `<Имя>`.
func nlbDialledEdges(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse composition root %s: %v", path, err)
	}
	seen := map[string]bool{}
	var found bool
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != nlbSpecsFn || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "mtls" {
				return true
			}
			// Значение обязано быть `<что-то>.MTLS.<Имя>`: ручка приходит из
			// настроек, а не из литерала. Иначе ребро проехало бы мимо стража.
			sel, ok := kv.Value.(*ast.SelectorExpr)
			if !ok {
				t.Errorf("%s: ребро с ручкой транспорта не в форме `cfg.MTLS.<Имя>` — "+
					"страж не сможет быть сопоставлен с ним, значит либо сохрани форму, "+
					"либо расширь этот гейт", path)
				return true
			}
			outer, ok := sel.X.(*ast.SelectorExpr)
			if !ok || outer.Sel.Name != nlbMTLSSelector {
				t.Errorf("%s: ручка ребра взята не из раздела %s (получено %s) — "+
					"сопоставить её со стражем нечем", path, nlbMTLSSelector, sel.Sel.Name)
				return true
			}
			seen[sel.Sel.Name] = true
			return true
		})
	}
	if !found {
		t.Fatalf("%s: объявление таблицы рёбер %s не найдено — предпосылка самого стража "+
			"исчезла, чини стража, а не считай дерево чистым", path, nlbSpecsFn)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// nlbGuardMTLSFields — ручки транспорта, которые читает ИСПОЛНЯЕМАЯ часть
// загрузочной стражи: селекторы вида `<что-то>.MTLS.<Имя>` в не-тестовых
// исходниках пакета конфигурации. Комментарии и строковые литералы в AST не
// попадают by construction.
func nlbGuardMTLSFields(t *testing.T, dir string) (map[string]bool, int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("предпосылка гейта нарушена: пакет загрузочной стражи %s не читается: %v", dir, err)
	}
	fields := map[string]bool{}
	read := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		read++
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			outer, ok := sel.X.(*ast.SelectorExpr)
			if !ok || outer.Sel.Name != nlbMTLSSelector {
				return true
			}
			fields[sel.Sel.Name] = true
			return true
		})
	}
	if read == 0 {
		t.Fatalf("предпосылка гейта нарушена: в %s не прочитано ни одного исходника", dir)
	}
	return fields, read
}

// TestEveryDialledPeerEdgeIsHeldByTheBootGuard — ядро гейта.
func TestEveryDialledPeerEdgeIsHeldByTheBootGuard(t *testing.T) {
	edges := nlbDialledEdges(t, nlbRootSrc)
	if len(edges) == 0 {
		t.Fatalf("%s: в таблице %s не найдено ни одного ребра с ручкой транспорта — либо корень "+
			"сменил форму, либо гейт перестал его читать; гейт, ничего не осмотревший, обязан "+
			"падать, а не проходить", nlbRootSrc, nlbSpecsFn)
	}
	held, filesRead := nlbGuardMTLSFields(t, nlbGuardDir)

	for _, edge := range edges {
		if !held[edge] {
			t.Errorf("ребро %s: загрузочная стража ни разу не читает MTLS.%s — ребро, которого "+
				"стража не видит, это ребро, чей транспорт никто не требует. Невзведённые "+
				"клиентские creds вырождаются в insecure БЕЗ ошибки, поэтому процесс поднимется "+
				"и отчитается о настроенных рёбрах", edge, edge)
		}
	}
	t.Logf("осмотрено: рёбер в %s.%s — %d (%s); исходников стражи прочитано — %d, ручек в них — %d",
		nlbRootSrc, nlbSpecsFn, len(edges), strings.Join(edges, ", "), filesRead, len(held))
}
