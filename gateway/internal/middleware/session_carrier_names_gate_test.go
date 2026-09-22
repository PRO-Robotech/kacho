// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_names_gate_test.go — гейт F4d-26 (Ф3-06): перечень гасимых
// имён носителя объявлен ОДИН раз, и у каждого имени есть ПРОИЗВОДИТЕЛЬ.
//
// # Предмет
//
// Край гасит носитель браузерной сессии в двух местах — путём отказа F4d-22
// (полоса личности) и обработчиком выхода. До Ф3 имя носителя стояло литералом
// в ДВУХ файлах (§1.8 приёмки: четыре объявления одного имени), и наше имя
// `kaname_session` не читал никто. Второе объявление расходится с первым молча:
// имя, добавленное в один перечень и забытое во втором, гасится отказом и не
// гасится выходом — у одного и того же браузера остаётся разное состояние.
//
// # Что судится — и ЧЕМ производитель отличается от гашения
//
// «Производитель имени» — ЧИТАТЕЛЬ этого имени на пути аутентификации (Д14
// приёмки): вызов, которому имя передано аргументом и который читает по нему
// носитель из запроса (`r.Cookie(<имя>)`, `strings.Contains(cookie, <имя>)`).
// Гашение (`http.SetCookie`) производителем НЕ является — оно и есть перечень.
// Имя, которое гасят и никто не читает, — носитель без предмета: край стирал бы
// у клиента печенье, которое никогда не признавал сессией.
//
// Законный близнец — имя носителя ПОСТАВЩИКА, пока его читатель жив (до S3,
// `kacho#1276`; заводит его множество носителей, называющее сторону
// `external`): читатель есть, гейт молчит. Снятие читателя делает имя находкой —
// так предикат истекает сам, а не по памяти.
//
// # Форма
//
// Судится ДЕРЕВО СИНТАКСИСА, не текст: имя в комментарии читателем не является,
// и предикат по подстроке краснел бы на собственном объяснении. Перепись печатает
// «имён в перечне N · имеющих производителя M · вторых объявлений K»; пустой
// обход — отказ, не ноль находок. Способность упасть доказана инъекцией в обе
// стороны на синтетике ниже.
package middleware

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// carrierNamesDeclaration — имя переменной, которая ЕСТЬ перечень. Названо
// здесь, чтобы переименование роняло гейт переписью («перечень не найден»), а
// не делало его тихо беспредметным.
const carrierNamesDeclaration = "sessionCarrierNames"

// carrierNameVerdict — вердикт по одному имени перечня.
type carrierNameVerdict struct {
	ident    string // константа-элемент перечня
	value    string // её строковое значение
	producer string // координата первого читателя, "" если читателя нет
}

// carrierNamesReport — перепись гейта.
type carrierNamesReport struct {
	names []carrierNameVerdict
	// duplicateLiterals — строковые литералы вне объявления, равные значению
	// имени из перечня: второе объявление предмета.
	duplicateLiterals []string
	filesRead         int
}

// judgeCarrierNames — судья гейта над разобранными файлами пакета (без проб).
//
// Отделён от обхода диска, чтобы инъекция подавала синтетику, а не правила
// дерево: доказательство способности падать не зависит от того, что лежит в
// каталоге в момент прогона.
func judgeCarrierNames(fset *token.FileSet, files map[string]*ast.File) (carrierNamesReport, error) {
	rep := carrierNamesReport{filesRead: len(files)}

	// 1. Константы пакета: имя → значение.
	consts := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							consts[n.Name] = strings.Trim(lit.Value, "`\"")
						}
					}
				}
			}
		}
	}

	// 2. Перечень: `var sessionCarrierNames = []string{A, B}` — элементы обязаны
	// быть идентификаторами констант, иначе имя не связать с читателем.
	var declFile string
	var elems []string
	for path, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if n.Name != carrierNamesDeclaration || i >= len(vs.Values) {
						continue
					}
					cl, ok := vs.Values[i].(*ast.CompositeLit)
					if !ok {
						return rep, errDeclShape(path, "перечень обязан быть составным литералом")
					}
					if declFile != "" {
						rep.duplicateLiterals = append(rep.duplicateLiterals,
							fset.Position(n.Pos()).String()+": второе объявление перечня "+carrierNamesDeclaration)
						continue
					}
					declFile = path
					for _, e := range cl.Elts {
						id, ok := e.(*ast.Ident)
						if !ok {
							return rep, errDeclShape(path, "элемент перечня обязан быть константой, а не литералом: "+
								fset.Position(e.Pos()).String())
						}
						elems = append(elems, id.Name)
					}
				}
			}
		}
	}
	if declFile == "" {
		return rep, errDeclShape("", "перечень "+carrierNamesDeclaration+" не найден")
	}

	// 3. Производители: вызов, которому имя передано аргументом, вне функции
	// гашения. Гашение — функция файла объявления, обходящая перечень; она
	// имени в аргументах не несёт (идёт по переменной цикла), поэтому явного
	// исключения не требуется — но и полагаться на это не стоит: функции файла
	// объявления из поиска читателей исключаются целиком.
	producers := map[string]string{}
	for path, f := range files {
		if path == declFile {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, a := range call.Args {
				id, ok := a.(*ast.Ident)
				if !ok {
					continue
				}
				if _, isElem := consts[id.Name]; !isElem {
					continue
				}
				if _, seen := producers[id.Name]; !seen {
					producers[id.Name] = fset.Position(call.Pos()).String()
				}
			}
			return true
		})
	}

	// 4. Вердикт по каждому имени + перепись вторых объявлений: строковый
	// литерал вне файла объявления, равный значению имени, — второе место об
	// одном предмете.
	values := map[string]string{}
	for _, e := range elems {
		v, ok := consts[e]
		if !ok {
			return rep, errDeclShape(declFile, "элемент перечня "+e+" не является строковой константой пакета")
		}
		values[v] = e
		rep.names = append(rep.names, carrierNameVerdict{ident: e, value: v, producer: producers[e]})
	}
	for path, f := range files {
		if path == declFile {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if _, dup := values[strings.Trim(lit.Value, "`\"")]; dup {
				rep.duplicateLiterals = append(rep.duplicateLiterals,
					fset.Position(lit.Pos()).String()+": литерал "+lit.Value+" — второе объявление имени носителя")
			}
			return true
		})
	}
	sort.Strings(rep.duplicateLiterals)
	return rep, nil
}

type declShapeError struct{ path, msg string }

func (e declShapeError) Error() string { return e.path + ": " + e.msg }

func errDeclShape(path, msg string) error { return declShapeError{path: path, msg: msg} }

// parseProdGoFiles разбирает все не-тестовые Go-файлы каталогов.
func parseProdGoFiles(t *testing.T, dirs ...string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("каталог %s не читается: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			p := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				t.Fatalf("%s не разбирается: %v", p, err)
			}
			files[p] = f
		}
	}
	return fset, files
}

// TestSessionCarrierNames_F3_06_EveryNameHasAProducerAndTheListIsDeclaredOnce —
// гейт F4d-26 на живом дереве края: пакеты middleware и handler (оба гасят
// носитель).
func TestSessionCarrierNames_F3_06_EveryNameHasAProducerAndTheListIsDeclaredOnce(t *testing.T) {
	fset, files := parseProdGoFiles(t, ".", "../handler")
	rep, err := judgeCarrierNames(fset, files)
	if err != nil {
		t.Fatalf("перечень имён носителя: %v", err)
	}
	if len(rep.names) == 0 {
		t.Fatal("имён в перечне 0 — обход пуст, и молчание этого гейта ничего не утверждает")
	}
	withProducer := 0
	for _, n := range rep.names {
		if n.producer == "" {
			t.Errorf("имя %s (%q) стоит в перечне гасимых, и его не читает ни один путь "+
				"аутентификации: край стирал бы у клиента печенье, которое никогда не "+
				"признавал сессией (F4d-26)", n.ident, n.value)
			continue
		}
		withProducer++
	}
	for _, d := range rep.duplicateLiterals {
		t.Errorf("второе объявление имени носителя: %s", d)
	}
	t.Logf("перепись: файлов прочитано %d · имён в перечне %d · имеющих производителя %d · вторых объявлений %d",
		rep.filesRead, len(rep.names), withProducer, len(rep.duplicateLiterals))
	// Наше имя обязано быть в перечне: без него отказ F4d-22 гасил бы только
	// носитель поставщика, и держатель нашей копии оставался бы с живым печеньем.
	found := false
	for _, n := range rep.names {
		if n.value == OurSessionCarrierName {
			found = true
		}
	}
	if !found {
		t.Fatalf("перечень гасимых имён не несёт нашего носителя %q", OurSessionCarrierName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ — синтетика, не дерево.

const carrierGateFixtureDecl = `package p
import "net/http"
const OurName = "our_session"
const TheirName = "their_session"
%s
var sessionCarrierNames = []string{OurName, TheirName%s}
func endCarriers(w http.ResponseWriter) {
	for _, c := range sessionCarrierNames { http.SetCookie(w, &http.Cookie{Name: c, MaxAge: -1}) }
}
`

const carrierGateFixtureReaders = `package p
import ("net/http"; "strings")
func lane(r *http.Request) bool {
	if _, err := r.Cookie(OurName); err == nil { return true }
	return strings.Contains(r.Header.Get("Cookie"), TheirName)
}
`

// syntheticDecl собирает файл объявления: extraConst — дополнительная
// константа (или пусто), extraElem — дополнительный элемент перечня (или пусто).
func syntheticDecl(extraConst, extraElem string) string {
	return fmt.Sprintf(carrierGateFixtureDecl, extraConst, extraElem)
}

func judgeSynthetic(t *testing.T, decl, readers string) carrierNamesReport {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, src := range map[string]string{"decl.go": decl, "readers.go": readers} {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("синтетика %s не разбирается: %v", name, err)
		}
		files[name] = f
	}
	rep, err := judgeCarrierNames(fset, files)
	if err != nil {
		t.Fatalf("судья на синтетике: %v", err)
	}
	return rep
}

// Инъекция: имя без производителя в перечне — красное С ИМЕНЕМ.
func TestSessionCarrierNamesGate_Injection_NameWithoutAProducerIsNamed(t *testing.T) {
	rep := judgeSynthetic(t, syntheticDecl(`const Orphan = "orphan_session"`, ", Orphan"), carrierGateFixtureReaders)
	var orphan *carrierNameVerdict
	for i := range rep.names {
		if rep.names[i].ident == "Orphan" {
			orphan = &rep.names[i]
		}
	}
	if orphan == nil {
		t.Fatal("внесённое имя не попало в перепись — судья не прочитал перечень")
	}
	if orphan.producer != "" {
		t.Fatalf("имя без читателя признано имеющим производителя (%s) — гейт не способен упасть", orphan.producer)
	}
	// Оба законных имени при этом — С производителем: инъекция роняет ТОЛЬКО
	// внесённое, а не всё подряд.
	for _, n := range rep.names {
		if n.ident != "Orphan" && n.producer == "" {
			t.Fatalf("законное имя %s объявлено сиротой рядом с внесённым — судья ловит форму, а не существо", n.ident)
		}
	}
}

// Близнец: имя поставщика при живом читателе `external` — молчит.
func TestSessionCarrierNamesGate_Twin_ProviderNameWithALiveReaderIsSilent(t *testing.T) {
	rep := judgeSynthetic(t, syntheticDecl("", ""), carrierGateFixtureReaders)
	if len(rep.names) != 2 {
		t.Fatalf("имён в перечне %d, ожидалось 2", len(rep.names))
	}
	for _, n := range rep.names {
		if n.producer == "" {
			t.Fatalf("имя %s с живым читателем объявлено сиротой — ложная находка отключила бы гейт первой же", n.ident)
		}
	}
	if len(rep.duplicateLiterals) != 0 {
		t.Fatalf("вторых объявлений на чистой синтетике %d: %v", len(rep.duplicateLiterals), rep.duplicateLiterals)
	}
}

// Инъекция: второе объявление — литерал того же имени в другом файле — находка.
func TestSessionCarrierNamesGate_Injection_ASecondDeclarationIsAFinding(t *testing.T) {
	readers := carrierGateFixtureReaders +
		"func logout(w http.ResponseWriter) { http.SetCookie(w, &http.Cookie{Name: \"their_session\", MaxAge: -1}) }\n"
	rep := judgeSynthetic(t, syntheticDecl("", ""), readers)
	if len(rep.duplicateLiterals) != 1 {
		t.Fatalf("второе объявление имени носителя не найдено: вторых объявлений %d", len(rep.duplicateLiterals))
	}
	if !strings.Contains(rep.duplicateLiterals[0], "their_session") {
		t.Fatalf("находка не называет имя: %q", rep.duplicateLiterals[0])
	}
}

// Пустой обход — отказ, а не ноль находок.
func TestSessionCarrierNamesGate_EmptyWalkIsARefusal(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "empty.go", "package p\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := judgeCarrierNames(fset, map[string]*ast.File{"empty.go": f}); err == nil {
		t.Fatal("судья на дереве без перечня обязан отказать, а не отчитаться нулём находок")
	}
}
