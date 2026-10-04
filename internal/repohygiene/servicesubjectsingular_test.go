// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// servicesubjectsingular_test.go — NTF1-M08 (гейт) и УК1: служебный
// вызывающий называется ОДНОЙ функцией, а строку `service:<имя>` производит
// ОДНА функция.
//
// # Предмет
//
//   - Читатель второго носителя (`grpcsrv.ServiceNameFromContext`) — ровно
//     одна функция, `authz.CallerSubject`. Её зовут извлекатель звена прав
//     (`authz.defaultSubjectExtractor`) и субъект сужения списков
//     (`listnarrow.SubjectFromContext`). Вторая функция, читающая носитель, —
//     второе решение «кто вызывает», и звено прав со списком могли бы
//     разойтись в ответе (NTF1-M08: «у listnarrow и authz одна функция
//     извлечения служебного принципала»).
//   - Производитель строки `service:<имя>` — ровно одна функция,
//     `authz.ServiceSubject` (УК1). Формы склейки, которые гейт опознаёт узлом:
//     строковое константное выражение (литерал, константа, сложение таких) со
//     значением, начинающимся с `service:`; `authz.ServiceSubjectType` операндом
//     сложения со значением, начинающимся с `:`.
//
// # Область
//
// corelib на пине и kacho. kaname судит своих писателей субъекта собственным
// гейтом (`internal/check/service_subject_writer.go` kaname, NTF1-M10): его
// распознаватель держит литерал `service:` как образец формы, и чужой гейт,
// читающий kaname, объявил бы находкой сам распознаватель.
//
// # Чего гейт не видит
//
// Склейку форматной строкой с отдельным словом типа (`fmt.Sprintf("%s:%s",
// authz.ServiceSubjectType, n)`) и чтение носителя через значение-функцию,
// сохранённое в другом пакете. Остаток держится ревью диффа.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
	"testing"
)

const (
	grpcsrvPkg    = corelibModulePath + "/grpcsrv"
	authzPkg      = corelibModulePath + "/authz"
	listnarrowPkg = corelibModulePath + "/listnarrow"

	// serviceCarrierReader — единственный читатель второго носителя.
	serviceCarrierReader = authzPkg + ".CallerSubject"
	// serviceSubjectProducer — единственный производитель `service:<имя>`.
	serviceSubjectProducer = authzPkg + ".ServiceSubject"
)

// serviceSubjectExtractors — извлекатели, обязанные звать читателя, а не
// читать носитель сами: звено прав и сужение списков.
var serviceSubjectExtractors = []string{
	authzPkg + ".defaultSubjectExtractor",
	listnarrowPkg + ".SubjectFromContext",
}

// carrierReport — исход гейта по корпусу.
type carrierReport struct {
	census       string
	files, funcs int
	readers      map[string][]string // функция → координаты обращений к носителю
	producers    map[string][]string // функция → координаты склеек `service:`
	routed       map[string]bool     // извлекатель → зовёт читателя
	findings     []string
}

// judgeServiceSubjectSingular — NTF1-M08 (гейт) и УК1 по корпусу.
func judgeServiceSubjectSingular(c *pinnedCorpus) carrierReport {
	r := carrierReport{
		census: c.census(), readers: map[string][]string{}, producers: map[string][]string{},
		routed: map[string]bool{},
	}
	consts := packageStringConsts(c)
	extractor := map[string]bool{}
	for _, e := range serviceSubjectExtractors {
		extractor[e] = true
	}
	for _, f := range c.files {
		r.files++
		names, dot := pinnedImportNames(f.file, nil)
		for _, d := range dot {
			if d == grpcsrvPkg || d == authzPkg {
				r.findings = append(r.findings, fmt.Sprintf("%s: точечный импорт %q — обращение к носителю "+
					"и склейка субъекта невидимы селекторному разбору", c.pos(f, f.file.Package), d))
			}
		}
		inPkg := func(e ast.Expr, pkg, name string) bool {
			if id, ok := e.(*ast.Ident); ok {
				return f.pkgPath == pkg && id.Name == name
			}
			p, n := selectorTarget(e, names)
			return p == pkg && n == name
		}
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			r.funcs++
			owner := funcOwner(f.pkgPath, fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Ident, *ast.SelectorExpr:
					if inPkg(x.(ast.Expr), grpcsrvPkg, "ServiceNameFromContext") {
						r.readers[owner] = append(r.readers[owner], c.pos(f, x.Pos()))
					}
					if extractor[owner] && inPkg(x.(ast.Expr), authzPkg, "CallerSubject") {
						r.routed[owner] = true
					}
					if _, isSel := x.(*ast.SelectorExpr); isSel {
						return false
					}
				case *ast.BinaryExpr:
					if x.Op != token.ADD {
						return true
					}
					if v, ok := foldString(x, f.pkgPath, names, consts); ok {
						if strings.HasPrefix(v, "service:") {
							r.producers[owner] = append(r.producers[owner], c.pos(f, x.Pos()))
						}
						return false
					}
					if inPkg(x.X, authzPkg, "ServiceSubjectType") {
						if v, ok := foldString(x.Y, f.pkgPath, names, consts); ok && strings.HasPrefix(v, ":") {
							r.producers[owner] = append(r.producers[owner], c.pos(f, x.Pos()))
						}
					}
				case *ast.BasicLit:
					if v, ok := foldString(x, f.pkgPath, names, consts); ok && strings.HasPrefix(v, "service:") {
						r.producers[owner] = append(r.producers[owner], c.pos(f, x.Pos()))
					}
				}
				return true
			})
		}
	}
	for _, owner := range sortedKeys(r.readers) {
		if owner == serviceCarrierReader {
			continue
		}
		r.findings = append(r.findings, fmt.Sprintf("%s: второй читатель носителя службы — %s обращается к "+
			"grpcsrv.ServiceNameFromContext сам, а не зовёт %s; звено прав и сужение списков могут "+
			"разойтись в ответе «кто вызывает» (NTF1-M08)", strings.Join(r.readers[owner], ", "), owner,
			serviceCarrierReader))
	}
	if len(r.readers[serviceCarrierReader]) != 1 {
		r.findings = append(r.findings, fmt.Sprintf("читатель %s обращается к носителю %d раз, ожидалось 1 — "+
			"образец гейта вне его зрения либо раздвоен", serviceCarrierReader, len(r.readers[serviceCarrierReader])))
	}
	for _, e := range serviceSubjectExtractors {
		if !r.routed[e] {
			r.findings = append(r.findings, fmt.Sprintf("извлекатель %s не зовёт %s — функция извлечения "+
				"служебного принципала у звена прав и у сужения списков не одна (NTF1-M08)", e, serviceCarrierReader))
		}
	}
	for _, owner := range sortedKeys(r.producers) {
		if owner == serviceSubjectProducer {
			continue
		}
		r.findings = append(r.findings, fmt.Sprintf("%s: второй производитель строки service:<имя> — %s "+
			"склеивает субъект службы сам, а не зовёт %s (УК1)", strings.Join(r.producers[owner], ", "), owner,
			serviceSubjectProducer))
	}
	if len(r.producers[serviceSubjectProducer]) != 1 {
		r.findings = append(r.findings, fmt.Sprintf("производитель %s склеивает субъект %d раз, ожидалось 1 — "+
			"образец гейта вне его зрения", serviceSubjectProducer, len(r.producers[serviceSubjectProducer])))
	}
	sort.Strings(r.findings)
	return r
}

// String — перепись для журнала пробы.
func (r carrierReport) String() string {
	routed := make([]string, 0, len(r.routed))
	for e := range r.routed {
		routed = append(routed, e)
	}
	sort.Strings(routed)
	return fmt.Sprintf("NTF1-M08/УК1: %s · файлов %d · функций %d · читателей носителя %d %v · "+
		"извлекателей через читателя %d из %d %v · производителей service: %d %v · находок %d",
		r.census, r.files, r.funcs, len(r.readers), sortedKeys(r.readers), len(r.routed),
		len(serviceSubjectExtractors), routed, len(r.producers), sortedKeys(r.producers), len(r.findings))
}

// TestNTF1M08_ServicePrincipalHasOneExtractor — NTF1-M08 (гейт) и УК1 по
// corelib на пине и kacho.
func TestNTF1M08_ServicePrincipalHasOneExtractor(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	c := pinnedTreesCorpus(t, root, "corelib", "kacho")
	if len(c.broken) > 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: файлов, не прошедших разбор, %d: %s", len(c.broken), c.broken[0])
	}
	r := judgeServiceSubjectSingular(c)
	t.Log(r.String())
	if r.files == 0 || r.funcs == 0 {
		t.Fatalf("пустой обход — не вердикт: %s", r.String())
	}
	for _, f := range r.findings {
		t.Errorf("NTF1-M08 · %s", f)
	}
}

// TestNTF1M08Injection — вторая функция извлечения и второй производитель в
// дереве kacho; близнецы — вызов единственных функций.
func TestNTF1M08Injection(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := pinnedTreesCorpus(t, root, "corelib", "kacho")
	if control := judgeServiceSubjectSingular(base); len(control.findings) != 0 {
		t.Fatalf("контроль не чист — инъекция не отличима от дерева: %v", control.findings)
	}
	const rel = "services/notify/cmd/notify-probe/internal/authzfilter/zz_m08_inject.go"
	cases := []struct {
		name, src, want string
	}{
		{"порча: вторая функция читает носитель службы", `package authzfilter
import (
	"context"
	"github.com/PRO-Robotech/corelib/grpcsrv"
)
func ownCaller(ctx context.Context) string {
	n, _ := grpcsrv.ServiceNameFromContext(ctx)
	return string(n)
}`, "второй читатель носителя службы"},
		{"порча: литерал service: склеен вручную", `package authzfilter
func subj(n string) string { return "service:" + n }`, "второй производитель строки service:<имя>"},
		{"порча: слово типа операндом сложения", `package authzfilter
import "github.com/PRO-Robotech/corelib/authz"
func subj2(n string) string { return authz.ServiceSubjectType + ":" + n }`, "второй производитель строки service:<имя>"},
		{"близнец: субъект через единственную функцию вызывающего", `package authzfilter
import (
	"context"
	"github.com/PRO-Robotech/corelib/authz"
)
func viaOne(ctx context.Context) string {
	c, _ := authz.CallerSubject(ctx)
	return c.Subject()
}`, ""},
		{"близнец: слово типа в сравнении, не в склейке", `package authzfilter
import "github.com/PRO-Robotech/corelib/authz"
func isSvc(t string) bool { return t == authz.ServiceSubjectType }`, ""},
	}
	for _, tc := range cases {
		r := judgeServiceSubjectSingular(injectedCorpus(t, base, rel, tc.src))
		got := strings.Join(r.findings, "\n")
		switch {
		case tc.want == "" && len(r.findings) != 0:
			t.Errorf("%s: законный близнец объявлен находкой: %s", tc.name, got)
		case tc.want != "" && !strings.Contains(got, tc.want):
			t.Errorf("%s: находки %q нет: %s", tc.name, tc.want, got)
		case tc.want != "" && !strings.Contains(got, "kacho:"+rel+":"):
			t.Errorf("%s: находка без координаты инъекции: %s", tc.name, got)
		}
		t.Logf("%s → находок %d: %s", tc.name, len(r.findings), got)
	}
}
