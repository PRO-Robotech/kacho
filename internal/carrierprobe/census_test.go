// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package carrierprobe

// census_test.go — в дереве ОДНО определение предиката эфемерного порта и ОДНО
// определение стража предусловия, и их зовут ВСЕ композиционные корни
// (kacho#2749).
//
// Предикаты жили двумя посимвольно одинаковыми копиями в пробах двух служб из
// шести; правка вносилась в одну копию из двух — дважды за одну полосу. У
// остальных четырёх стража не было вовсе. Перепись отвечает на оба вопроса
// разом: сколько определений и кто зовёт.
//
// Композиционный корень ВЫВОДИТСЯ из дерева, а не выписывается: это пакет
// процесса, чей НЕ-пробный код зовёт `servicehost.Serve`. Седьмая служба
// попадёт в перепись сама. Состав дерева — из индекса git.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

const treeRoot = "../.."

// definitionName — имя, под которым в дереве заводили копию предиката или
// стража. Узел разбора — объявление функции; упоминание в комментарии или
// тексте определением не является.
var definitionName = regexp.MustCompile(`(?i)(kernelassigned|ephemeral)`)

type carrierCensus struct {
	files       int
	roots       []string          // каталоги композиционных корней
	definitions map[string]string // "файл:строка" → имя функции
	guardCalls  map[string]bool   // корень → зовёт страж
	serveCalls  map[string]bool   // корень → зовёт суждение об исходе подъёма
	// Пробы, поднимающие носитель: каждая функция проб, зовущая
	// servicehost.Serve, обязана звать и страж, и суждение об исходе. Корень,
	// где страж позван в ОДНОЙ пробе из двух, иначе числился бы покрытым.
	serveSites, unguarded, unjudged []string
}

// callsSelector — в файле есть вызов pkg.Name(…) узлом разбора.
func callsSelector(f ast.Node, pkg, name string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == pkg {
			found = true
		}
		return true
	})
	return found
}

// censusOf — перепись по данному составу файлов: пути абсолютные, root — корень.
func censusOf(root string, files []string) (carrierCensus, error) {
	c := carrierCensus{definitions: map[string]string{}, guardCalls: map[string]bool{}, serveCalls: map[string]bool{}}
	roots := map[string]bool{}
	parsed := map[string]*ast.File{}
	for _, p := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return c, err
		}
		c.files++
		parsed[p] = f
		rel, _ := filepath.Rel(root, p)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			// Пробы (`Test…`) — не определения предиката: они его зовут.
			if !ok || strings.HasPrefix(fd.Name.Name, "Test") || !definitionName.MatchString(fd.Name.Name) {
				continue
			}
			c.definitions[rel+":"+strconv.Itoa(fset.Position(fd.Pos()).Line)] = fd.Name.Name
		}
		if !strings.HasSuffix(p, "_test.go") && strings.Contains(rel, "/cmd/") && callsSelector(f, "servicehost", "Serve") {
			roots[filepath.Dir(p)] = true
		}
	}
	for p, f := range parsed {
		d := filepath.Dir(p)
		if !roots[d] || !strings.HasSuffix(p, "_test.go") {
			continue
		}
		if callsSelector(f, "carrierprobe", "RequireKernelAssigned") {
			c.guardCalls[d] = true
		}
		if callsSelector(f, "carrierprobe", "RequireRaised") {
			c.serveCalls[d] = true
		}
		rel, _ := filepath.Rel(root, p)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !callsSelector(fd, "servicehost", "Serve") {
				continue
			}
			site := rel + ":" + fd.Name.Name
			c.serveSites = append(c.serveSites, site)
			// Страж зовут либо целиком, либо его суждение напрямую — так делает
			// проба, которая САМА подаёт фиксированный порт и утверждает отказ.
			if !callsSelector(fd, "carrierprobe", "RequireKernelAssigned") && !callsSelector(fd, "carrierprobe", "Refusal") {
				c.unguarded = append(c.unguarded, site)
			}
			if !callsSelector(fd, "carrierprobe", "RequireRaised") && !callsSelector(fd, "carrierprobe", "Classify") {
				c.unjudged = append(c.unjudged, site)
			}
		}
	}
	sort.Strings(c.serveSites)
	sort.Strings(c.unguarded)
	sort.Strings(c.unjudged)
	for d := range roots {
		c.roots = append(c.roots, d)
	}
	sort.Strings(c.roots)
	return c, nil
}

// treeFiles — Go-файлы под services/*/cmd и в самом общем пакете, из индекса git.
func treeFiles(t *testing.T) (string, []string) {
	t.Helper()
	root, err := filepath.Abs(treeRoot)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, pat := range []string{"services/*/cmd/*/*.go", "internal/carrierprobe/*.go"} {
		files, err := treecorpus.Glob(filepath.Join(root, pat))
		if err != nil {
			t.Fatalf("состав дерева по образцу %s не прочитан: %v", pat, err)
		}
		out = append(out, files...)
	}
	return root, out
}

func TestOneEphemeralPredicateAndOneGuardCalledByEveryCompositionRoot(t *testing.T) {
	root, files := treeFiles(t)
	c, err := censusOf(root, files)
	if err != nil {
		t.Fatal(err)
	}
	outside, inside := 0, map[string]bool{}
	for at, name := range c.definitions {
		if strings.HasPrefix(at, "internal/carrierprobe/") {
			inside[name] = true
		}
		if !strings.HasPrefix(at, "internal/carrierprobe/") {
			outside++
			t.Errorf("%s: %s — копия предиката эфемерного порта или стража вне общего пакета "+
				"internal/carrierprobe; копии расходятся молча, правку вносят в одну из двух", at, name)
		}
	}
	guards, serves := 0, 0
	for _, d := range c.roots {
		rel, _ := filepath.Rel(root, d)
		if c.guardCalls[d] {
			guards++
		} else {
			t.Errorf("%s: композиционный корень, пробы которого не зовут carrierprobe.RequireKernelAssigned — "+
				"вердикт его пробы носителя зависит от того, что ещё поднято на машине", rel)
		}
		if c.serveCalls[d] {
			serves++
		} else {
			t.Errorf("%s: пробы корня не судят исход подъёма через carrierprobe.RequireRaised — "+
				"занятый порт здесь неотличим от отказа носителя", rel)
		}
	}
	for _, site := range c.unguarded {
		t.Errorf("%s: проба поднимает носитель без стража carrierprobe.RequireKernelAssigned", site)
	}
	for _, site := range c.unjudged {
		t.Errorf("%s: проба поднимает носитель и не судит исход через carrierprobe", site)
	}
	t.Logf("осмотрено файлов %d · композиционных корней %d · зовут страж %d · судят исход подъёма %d · "+
		"проб, поднимающих носитель, %d (без стража %d, без суждения об исходе %d) · "+
		"определений предиката и стража %d (вне общего пакета %d)",
		c.files, len(c.roots), guards, serves, len(c.serveSites), len(c.unguarded), len(c.unjudged),
		len(c.definitions), outside)
	if len(c.serveSites) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни одной пробы, поднимающей носитель — перепись не видит своего предмета")
	}
	// Внутри общего пакета — ровно предикат и страж, по одному.
	if len(inside) != 2 || !inside["KernelAssigned"] || !inside["RequireKernelAssigned"] {
		t.Errorf("в общем пакете определения предиката и стража не те: %v (ждали KernelAssigned и "+
			"RequireKernelAssigned, по одному)", inside)
	}
	if len(c.roots) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: композиционных корней не найдено среди %d файлов — предикат поиска "+
			"перестал узнавать дерево", c.files)
	}
}

// Самопроверка переписи на синтетическом дереве: копия предиката в пробе
// службы — находка; корень без стража — находка; корень со стражем — молчание;
// вызов в комментарии — не вызов.
func TestCarrierCensusSelfCheck(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	main := "package main\n\nimport \"github.com/PRO-Robotech/corelib/servicehost\"\n\nfunc run() { _ = servicehost.Serve }\nfunc serve() { servicehost.Serve(nil, nil, nil, nil) }\n"
	files := []string{
		write("services/a/cmd/a/main.go", main),
		write("services/a/cmd/a/carrier_test.go", "package main\n\nfunc portIsKernelAssigned(p string) bool { return p == \"0\" }\n"),
		write("services/b/cmd/b/main.go", main),
		write("services/b/cmd/b/carrier_test.go", "package main\n\nimport \"github.com/PRO-Robotech/kacho/internal/carrierprobe\"\n\n"+
			"func x() { carrierprobe.RequireKernelAssigned(nil, nil); carrierprobe.RequireRaised(nil, \"b\", servicehost.Serve(nil, nil, nil, nil)) }\n"+
			"func y() { carrierprobe.RequireRaised(nil, \"b\", servicehost.Serve(nil, nil, nil, nil)) }\n"),
		write("services/c/cmd/c/main.go", main),
		write("services/c/cmd/c/carrier_test.go", "package main\n\n// carrierprobe.RequireKernelAssigned(t, spec)\n"),
		write("internal/carrierprobe/carrierprobe.go", "package carrierprobe\n\nfunc KernelAssigned(addr string) bool { return false }\n"),
	}
	c, err := censusOf(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.roots) != 3 {
		t.Fatalf("корней %d, ждали 3: %v", len(c.roots), c.roots)
	}
	if len(c.definitions) != 2 {
		t.Fatalf("определений %d, ждали 2 (копия в a и общее): %v", len(c.definitions), c.definitions)
	}
	if _, ok := c.definitions["services/a/cmd/a/carrier_test.go:3"]; !ok {
		t.Fatalf("копия в пробе службы не названа координатой: %v", c.definitions)
	}
	b := filepath.Join(root, "services/b/cmd/b")
	cc := filepath.Join(root, "services/c/cmd/c")
	if !c.guardCalls[b] || !c.serveCalls[b] {
		t.Fatalf("корень b зовёт оба, а перепись этого не видит: %+v", c)
	}
	if c.guardCalls[cc] || c.guardCalls[filepath.Join(root, "services/a/cmd/a")] {
		t.Fatalf("вызов в комментарии либо его отсутствие засчитаны за вызов: %+v", c.guardCalls)
	}
	// Корень b зовёт страж в одной пробе из двух: вторая — находка, хотя корень
	// «покрыт».
	if len(c.serveSites) != 2 || len(c.unguarded) != 1 || c.unguarded[0] != "services/b/cmd/b/carrier_test.go:y" || len(c.unjudged) != 0 {
		t.Fatalf("пробы, поднимающие носитель, разобраны не так: сайты %v, без стража %v, без суждения %v",
			c.serveSites, c.unguarded, c.unjudged)
	}
}
