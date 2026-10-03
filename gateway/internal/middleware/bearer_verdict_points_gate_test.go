// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bearer_verdict_points_gate_test.go — перепись точек чтения вердикта отзыва на
// полосе ПОДПИСАННОГО предъявителя (kacho#2742).
//
// # Предмет
//
// Точка предъявления — функция, которая сама проверяет подпись токена
// (`<…>.verifier.Verify(…)`) И выставляет по нему личность (прямо либо через
// функцию того же пакета: setPrincipalHeaders, injectPrincipal,
// injectVerifiedTokenHeaders, authorizeViaLookup). Каждая такая точка обязана
// читать вердикт отзыва ОБЩИМ словарём — revocationCheck (прямо либо через
// refuseRevokedHTTP): иначе отзыв, исполняемый на одной поверхности, обходится
// другой. Прежде это держалось поимённым перечнем двух сквозных проб, и третья
// поверхность прошла бы всё зелёное — так и прошла схема `DPoP`.
//
// Свойства переписи — те же пять, что у соседней (presentation_points_gate_test.go):
// обход дерева синтаксиса; печать объёма осмотренного; отказ на пустом обходе;
// названное ожидаемое число точек; инъекция в обе стороны.
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

const bearerVerifyField = "verifier"

var (
	bearerIdentityWriters = map[string]bool{
		"setPrincipalHeaders": true, "injectPrincipal": true,
		"injectVerifiedTokenHeaders": true, "authorizeViaLookup": true,
	}
	bearerVerdictReaders = map[string]bool{"revocationCheck": true, "refuseRevokedHTTP": true}
)

type bearerPoint struct {
	fn, pos   string
	readsCall bool
}

// judgeBearerVerdictPoints — судья над разобранными файлами одного пакета.
func judgeBearerVerdictPoints(fset *token.FileSet, files []*ast.File) []bearerPoint {
	type fnInfo struct {
		calls            map[string]bool
		verifies, writes bool
		reads            bool
		pos              token.Pos
	}
	fns := map[string]*fnInfo{}
	key := func(fd *ast.FuncDecl) string {
		if fd.Recv != nil && len(fd.Recv.List) == 1 {
			t := fd.Recv.List[0].Type
			if st, ok := t.(*ast.StarExpr); ok {
				t = st.X
			}
			if id, ok := t.(*ast.Ident); ok {
				return id.Name + "." + fd.Name.Name
			}
		}
		return fd.Name.Name
	}
	byName := map[string][]string{} // имя функции → ключи (метод может совпадать по имени у разных типов)
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			info := &fnInfo{calls: map[string]bool{}, pos: fd.Pos()}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
					if inner, ok := fun.X.(*ast.SelectorExpr); ok && name == "Verify" && inner.Sel.Name == bearerVerifyField {
						info.verifies = true
					}
				case *ast.Ident:
					name = fun.Name
				}
				switch {
				case bearerIdentityWriters[name]:
					info.writes = true
				case bearerVerdictReaders[name]:
					info.reads = true
				}
				info.calls[name] = true
				return true
			})
			k := key(fd)
			fns[k] = info
			byName[fd.Name.Name] = append(byName[fd.Name.Name], k)
		}
	}
	var reach func(k string, seen map[string]bool, pick func(*fnInfo) bool) bool
	reach = func(k string, seen map[string]bool, pick func(*fnInfo) bool) bool {
		info, ok := fns[k]
		if !ok || seen[k] {
			return false
		}
		seen[k] = true
		if pick(info) {
			return true
		}
		for callee := range info.calls {
			for _, ck := range byName[callee] {
				if reach(ck, seen, pick) {
					return true
				}
			}
		}
		return false
	}
	var out []bearerPoint
	for k, info := range fns {
		if !info.verifies || !reach(k, map[string]bool{}, func(i *fnInfo) bool { return i.writes }) {
			continue
		}
		out = append(out, bearerPoint{fn: k, pos: fset.Position(info.pos).String(),
			readsCall: reach(k, map[string]bool{}, func(i *fnInfo) bool { return i.reads })})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fn < out[j].fn })
	return out
}

func TestBearerPresentationPointsReadTheVerdictThroughTheSharedDictionary(t *testing.T) {
	fset, byPkg := parseGatewayInternal(t)
	var files []*ast.File
	read := 0
	for dir, fs := range byPkg {
		if dir != "../middleware" {
			read += len(fs)
			continue
		}
		for _, f := range fs {
			files = append(files, f)
			read++
		}
	}
	if len(files) == 0 {
		t.Fatalf("файлов пакета middleware прочитано 0 (всего %d) — обход пуст", read)
	}
	points := judgeBearerVerdictPoints(fset, files)
	with := 0
	names := make([]string, 0, len(points))
	for _, p := range points {
		names = append(names, p.fn)
		if !p.readsCall {
			t.Errorf("точка предъявления %s (%s) проверяет подпись и выставляет личность, НЕ читая вердикт отзыва общим словарём (revocationCheck) — отозванный токен на этой поверхности проходит", p.fn, p.pos)
			continue
		}
		with++
	}
	t.Logf("перепись: файлов middleware %d · точек предъявления подписанного %d · читающих вердикт %d · %v", len(files), len(points), with, names)
	// Точек ТРИ: нативная поверхность (authorize), REST по схеме Bearer
	// (tryBearerJWT) и REST по схеме DPoP (DPoPMiddleware.Wrap). Число названо:
	// четвёртая поверхность — предмет решения, а не побочный эффект.
	if len(points) != 3 {
		t.Fatalf("точек предъявления подписанного %d, ожидалось 3: %v", len(points), names)
	}
}

// Инъекция в обе стороны: третья поверхность, не читающая вердикт, названа;
// поверхность, читающая его через функцию пакета, и читатель вне точки — молчат.
func TestBearerVerdictPointsInjection(t *testing.T) {
	src := `package p
type V struct{ verifier X }
func (v *V) good() { t := v.verifier.Verify(); if v.refuseRevokedHTTP(t) { return }; setPrincipalHeaders() }
func (v *V) viaHelper() { t := v.verifier.Verify(); v.ask(t); v.inject() }
func (v *V) ask(t T) { revocationCheck(t) }
func (v *V) inject() { injectPrincipal() }
func (v *V) bad() { t := v.verifier.Verify(); injectVerifiedTokenHeaders(t) }
func (v *V) bindingOnly() { v.verifier.Verify() }
func readerAlone() { revocationCheck(nil) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pts := judgeBearerVerdictPoints(fset, []*ast.File{f})
	got := map[string]bool{}
	for _, p := range pts {
		got[p.fn] = p.readsCall
	}
	want := map[string]bool{"V.good": true, "V.viaHelper": true, "V.bad": false}
	if len(got) != len(want) {
		t.Fatalf("точки распознаны неверно: %v, ожидалось %v (проверка без выставления личности — не точка)", got, want)
	}
	for k, v := range want {
		if r, ok := got[k]; !ok || r != v {
			t.Errorf("%s: читает=%v (есть=%v), ожидалось %v", k, r, ok, v)
		}
	}
}
