// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// Матчеры заголовков HTTP/2 библиотеки cmux. Слушатели края распознают gRPC
// матчером cmuxh2.MatchHeaderFieldSendSettings: ресурсы соединения до
// аутентификации у него ограничены явно.
// Перечень сверен с библиотекой: TestUnboundedCmuxMatcherListNamesRealFunctions
// держит, что каждое имя существует, и не даёт ему пережить свой предмет.
var unboundedCmuxHeaderMatchers = map[string]bool{
	"HTTP2HeaderField":                        true,
	"HTTP2HeaderFieldPrefix":                  true,
	"HTTP2MatchHeaderFieldSendSettings":       true,
	"HTTP2MatchHeaderFieldPrefixSendSettings": true,
}

// edgeMatcherCalls — строки вызовов матчеров заголовков cmux и число вызовов
// cmuxh2.MatchHeaderFieldSendSettings в файле. Предмет опознаётся узлом
// разбора (селектор вызова), а не словом: комментарий и строковый литерал не
// считаются.
func edgeMatcherCalls(t *testing.T, rel string, body []byte) (unbounded []int, bounded int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch {
		case pkg.Name == "cmux" && unboundedCmuxHeaderMatchers[sel.Sel.Name]:
			unbounded = append(unbounded, fset.Position(call.Pos()).Line)
		case pkg.Name == "cmuxh2" && sel.Sel.Name == "MatchHeaderFieldSendSettings":
			bounded++
		}
		return true
	})
	return unbounded, bounded
}

// TestEdgeListenersUseTheBoundedGRPCMatcher — ни один прод-файл шлюза не
// распознаёт gRPC матчером заголовков cmux; распознаёт его
// cmuxh2.MatchHeaderFieldSendSettings. Предпосылка — ограниченный матчер
// вызван хотя бы раз: иначе «ноль находок» значил бы, что слушатели края
// перестали делить порт, и запрет надо пересмотреть, а не держать молча.
func TestEdgeListenersUseTheBoundedGRPCMatcher(t *testing.T) {
	root := gatewayTreeRoot(t)
	sources, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав gateway/: %v", err)
	}

	var hits []string
	scanned, bounded := 0, 0
	for _, path := range sources {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("относительный путь для %s: %v", path, relErr)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") || hasPathSegment(rel, "docs", "testdata") {
			continue
		}
		scanned++
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		lines, n := edgeMatcherCalls(t, rel, body)
		bounded += n
		for _, line := range lines {
			hits = append(hits, rel+":"+strconv.Itoa(line))
		}
	}

	if scanned == 0 {
		t.Fatalf("гейт не прочитал ни одного прод-файла в %s — предпосылка обхода сломана", root)
	}
	t.Logf("осмотрено прод-файлов шлюза: %d · вызовов ограниченного матчера: %d · матчеров заголовков cmux: %d",
		scanned, bounded, len(hits))

	if len(hits) > 0 {
		t.Errorf("gRPC на слушателе края распознаётся матчером заголовков cmux: %s\n\n"+
			"Распознавай cmuxh2.MatchHeaderFieldSendSettings — он ограничивает ресурсы "+
			"соединения до аутентификации.", strings.Join(hits, ", "))
	}
	if bounded == 0 {
		t.Errorf("ограниченный матчер cmuxh2.MatchHeaderFieldSendSettings не вызван ни разу — " +
			"слушатели края больше не делят порт? пересмотри запрет")
	}
}

// Инъекция в обе стороны на синтетике: дефект находится со своей строкой,
// законный близнец той же формы молчит, упоминание в комментарии и литерале
// предметом не считается.
func TestEdgeGRPCMatcherGateInjection(t *testing.T) {
	const defective = `package main

import "github.com/soheilhy/cmux"

func split(m cmux.CMux) {
	_ = m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	_ = m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
}
`
	const twin = `package main

import (
	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/kacho/gateway/internal/cmuxh2"
)

// cmux.HTTP2MatchHeaderFieldSendSettings здесь не вызывается.
var note = "cmux.HTTP2MatchHeaderFieldSendSettings"

func split(m cmux.CMux) {
	_ = m.MatchWithWriters(
		cmuxh2.MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	_ = m.Match(cmux.HTTP2(), cmux.Any())
}
`
	lines, bounded := edgeMatcherCalls(t, "defective.go", []byte(defective))
	if len(lines) != 2 || lines[0] != 7 || lines[1] != 9 || bounded != 0 {
		t.Fatalf("дефект: найдено строк %v (ждали [7 9]), ограниченных %d (ждали 0)", lines, bounded)
	}
	lines, bounded = edgeMatcherCalls(t, "twin.go", []byte(twin))
	if len(lines) != 0 || bounded != 1 {
		t.Fatalf("законный близнец: найдено строк %v (ждали ни одной), ограниченных %d (ждали 1)", lines, bounded)
	}
}

// TestUnboundedCmuxMatcherListNamesRealFunctions — каждое имя перечня есть в
// библиотеке (иначе тут не собралось бы), и перечень не длиннее проверенного:
// запись, которой нечего запрещать, — находка.
func TestUnboundedCmuxMatcherListNamesRealFunctions(t *testing.T) {
	real := map[string]any{
		"HTTP2HeaderField":                        cmux.HTTP2HeaderField,
		"HTTP2HeaderFieldPrefix":                  cmux.HTTP2HeaderFieldPrefix,
		"HTTP2MatchHeaderFieldSendSettings":       cmux.HTTP2MatchHeaderFieldSendSettings,
		"HTTP2MatchHeaderFieldPrefixSendSettings": cmux.HTTP2MatchHeaderFieldPrefixSendSettings,
	}
	for name := range unboundedCmuxHeaderMatchers {
		if _, ok := real[name]; !ok {
			t.Errorf("в перечне %q, которого проба не сверила с библиотекой", name)
		}
	}
	if len(unboundedCmuxHeaderMatchers) != len(real) {
		t.Errorf("перечень %d имён, сверено %d", len(unboundedCmuxHeaderMatchers), len(real))
	}
}
