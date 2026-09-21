// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_readers_gate_test.go — ПРЕДИКАТ ПРИСУТСТВИЯ НОСИТЕЛЯ ОДИН НА
// СТОРОНУ В ПАКЕТЕ ПОЛОС, и это держится обходом, а не шапкой файла.
//
// # ОБЛАСТЬ НАЗВАНА ТОЧНО, И ОНА РАВНА ИСПОЛНЯЕМОЙ
//
// Прежняя редакция объявляла находкой «всякое чтение имени носителя вне этого
// файла», а исполняла два сужения сразу: обход шёл по одному каталогу, и
// предикат опознавал только НЕКВАЛИФИЦИРОВАННОЕ имя — из чужого пакета оно
// приходит другой формой узла. Объявление было шире исполняемого, и строка
// переписи печатала «вне дома — 0» о знаменателе, поставленном так, чтобы ноль
// получился.
//
// Выбран не рост обхода, а СУЖЕНИЕ ОБЪЯВЛЕНИЯ — потому что широкое было бы
// ложным по существу, а не только по охвату: вне пакета имя встречается в
// другом качестве. Чужой код его НАЗЫВАЕТ — в тексте отказа старта, в обходе
// перечня гасимых, — и читателем носителя от этого не становится; объявить это
// находкой значило бы запретить объяснять оператору, о каком печенье речь.
// Законный квалифицированный близнец в дереве есть, и он назван пробой ниже.
//
// # Почему гейт заведён именно здесь
//
// `session_carrier_readers.go` УТВЕРЖДАЕТ о дереве: «предикат один на сторону,
// и обе полосы зовут именно его». Утверждение о дереве, за которым не стоит
// обхода, живёт ровно до следующей правки — и прожило меньше: третья копия
// (`r.Cookie(OurSessionCarrierName)` внутри `meFromOwnSession`) осталась в том
// же изменении, которым объявлялась единственность, и нашлась на ревью, а не
// прогоном.
//
// Цена расхождения копий названа в шапке предиката и не косметическая: в
// состоянии, где живы оба читателя, разные ответы на «наш ли это запрос»
// означают, что действующая личность зависит от задетой полосы.
//
// # Что судится
//
// ВЫЗОВ, которому имя носителя передано АРГУМЕНТОМ: так записаны обе законные
// формы чтения — `r.Cookie(<имя>)` и `strings.Contains(<заголовок>, <имя>)`.
// Судится разобранный исходник, не текст: имена стоят и в комментариях, и в
// текстах отказов.
//
// Гашение (`EndSessionCarriers`) под предикат не попадает: оно идёт по
// переменной цикла и имени в аргументах не несёт — то же различие, которым
// пользуется гейт перечня имён.
//
// Перепись печатает «файлов ПАКЕТА ПОЛОС осмотрено N · мест чтения M · вне
// объявления K» — знаменатель назван областью, а не умолчанием; пустой обход —
// отказ, а не ноль находок.
package middleware

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

// carrierReadersHome — файл, которому принадлежат предикаты присутствия.
// Назван константой: переименование обязано ронять гейт переписью, а не делать
// его тихо беспредметным.
const carrierReadersHome = "session_carrier_readers.go"

// carrierNameIdents — константы имён носителя. Перечень ВЫВОДИТСЯ из перечня
// гасимых имён, а не выписывается: имя, добавленное к продукту и забытое здесь,
// читалось бы где угодно, и гейт молчал бы.
func carrierNameIdents(files map[string]*ast.File) []string {
	values := map[string]bool{}
	for _, v := range SessionCarrierNames() {
		values[v] = true
	}
	var out []string
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if values[strings.Trim(lit.Value, "`\"")] {
						out = append(out, n.Name)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// carrierReadSites — места, где имя носителя передано вызову аргументом.
// Ключ — файл, значение — координаты.
func carrierReadSites(fset *token.FileSet, files map[string]*ast.File, idents []string) map[string][]string {
	named := map[string]bool{}
	for _, id := range idents {
		named[id] = true
	}
	out := map[string][]string{}
	for path, f := range files {
		base := filepath.Base(path)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, a := range call.Args {
				if id, ok := a.(*ast.Ident); ok && named[id.Name] {
					out[base] = append(out[base], fset.Position(call.Pos()).String())
				}
			}
			return true
		})
	}
	return out
}

// parseCarrierPackage разбирает НЕПРОВЕРОЧНЫЕ файлы пакета.
func parseCarrierPackage(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог пакета не прочитан: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s не разбирается: %v", name, err)
		}
		files[name] = f
	}
	if len(files) == 0 {
		t.Fatal("обход пуст: непроверочных файлов Go в пакете не найдено — гейт судил бы о непрочитанном")
	}
	return fset, files
}

func TestSessionCarrierReaders_OnePredicatePerSideAndItLivesInOneFile(t *testing.T) {
	fset, files := parseCarrierPackage(t)
	idents := carrierNameIdents(files)
	if len(idents) != len(SessionCarrierNames()) {
		t.Fatalf("констант имён носителя найдено %d (%v), имён в перечне %d — предикат перестал "+
			"опознавать свой предмет", len(idents), idents, len(SessionCarrierNames()))
	}

	sites := carrierReadSites(fset, files, idents)
	total, outside := 0, 0
	var strays []string
	for file, positions := range sites {
		total += len(positions)
		if file == carrierReadersHome {
			continue
		}
		outside += len(positions)
		strays = append(strays, positions...)
	}
	if total == 0 {
		t.Fatal("мест чтения носителя не найдено ни одного — обход ничего не осмотрел, и молчание " +
			"гейта ничего не утверждает")
	}
	if len(sites[carrierReadersHome]) == 0 {
		t.Fatalf("в %s нет ни одного чтения носителя — предикаты уехали, а гейт сторожит пустое место",
			carrierReadersHome)
	}
	sort.Strings(strays)
	for _, s := range strays {
		t.Errorf("носитель читается вне %s: %s. Предикат объявлен ОДНИМ на сторону, и копия его "+
			"расходится молча: в состоянии, где живы оба читателя, разные ответы на «наш ли это "+
			"запрос» означают, что действующая личность зависит от задетой полосы",
			carrierReadersHome, s)
	}
	t.Logf("перепись: ОБЛАСТЬ — пакет полос (internal/middleware); непроверочных файлов "+
		"осмотрено %d · констант имён %d (%s) · мест чтения носителя %d · из них вне %s — %d. "+
		"ВНЕ ОБЛАСТИ: чужие пакеты, где имя НАЗЫВАЕТСЯ (текст отказа, обход перечня гасимых) — "+
		"читателем носителя это не является",
		len(files), len(idents), strings.Join(idents, ", "), total, carrierReadersHome, outside)
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика, ТЕМ ЖЕ телом гейта.

func judgeCarrierReadFixture(t *testing.T, src string) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "elsewhere.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	return carrierReadSites(fset, map[string]*ast.File{"elsewhere.go": f},
		[]string{"OurSessionCarrierName", "providerSessionCarrierName"})
}

// Дефект: вторая копия чтения вне дома предикатов — красное с координатой.
func TestSessionCarrierReadersGate_Injection_ASecondReadIsFound(t *testing.T) {
	got := judgeCarrierReadFixture(t, `package middleware
func me(r *http.Request) {
	c, _ := r.Cookie(OurSessionCarrierName)
	_ = c
}
`)
	if len(got["elsewhere.go"]) != 1 || !strings.HasPrefix(got["elsewhere.go"][0], "elsewhere.go:3:") {
		t.Fatalf("внесённая копия чтения не названа координатой: %v", got)
	}
}

// Близнец первый: ПРОЗА, называющая имя константы. Комментарий чтением не
// является — иначе гейт краснел бы на шапке, которая его же и объясняет.
func TestSessionCarrierReadersGate_Twin_ProseNamingTheConstantIsSilent(t *testing.T) {
	got := judgeCarrierReadFixture(t, `package middleware
// Носитель читается предикатом ourSessionCarrierOf по имени OurSessionCarrierName.
func nothing() {}
`)
	if len(got) != 0 {
		t.Fatalf("проза объявлена находкой: %v", got)
	}
}

// Близнец второй: ВЫЗОВ ОБЩЕГО ПРЕДИКАТА — законная форма, ради которой всё и
// заводилось. Имени носителя он в аргументах не несёт, и гейт обязан молчать.
func TestSessionCarrierReadersGate_Twin_CallingTheSharedPredicateIsSilent(t *testing.T) {
	got := judgeCarrierReadFixture(t, `package middleware
func me(r *http.Request) {
	if bearer, ours := ourSessionCarrierOf(r); ours {
		_ = bearer
	}
	_ = providerSessionCarrierPresented(r)
}
`)
	if len(got) != 0 {
		t.Fatalf("законный вызов общего предиката объявлен находкой: %v", got)
	}
}

// Законный близнец ОБЛАСТИ: квалифицированное имя в ЧУЖОМ пакете. Такие места
// в дереве есть — страж старта называет наше печенье в тексте отказа, а
// ретранслятор обходит перечень гасимых, — и находкой они не являются.
//
// Проба судит не «гейт их пропустил» (он их и не смотрит), а то, что они
// СУЩЕСТВУЮТ: сужение области обязано опираться на живой предмет, иначе оно
// оправдывает само себя. Исчезнут — пробу пересмотрит то же изменение.
func TestSessionCarrierReaders_TheNarrowedScopeHasALivingLawfulTwinOutside(t *testing.T) {
	roots := []string{"../handler", "../../cmd/api-gateway"}
	qualified := 0
	var places []string
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("каталог %s не прочитан: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("%s не разбирается: %v", name, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "middleware" {
					return true
				}
				if strings.HasPrefix(sel.Sel.Name, "OurSessionCarrier") ||
					strings.HasPrefix(sel.Sel.Name, "SessionCarrierNames") {
					qualified++
					places = append(places, fset.Position(sel.Pos()).String())
				}
				return true
			})
		}
	}
	if qualified == 0 {
		t.Fatal("квалифицированных упоминаний имени носителя вне пакета полос не найдено — " +
			"сужение области больше не опирается на живой предмет и оправдывает само себя; " +
			"пересмотреть область вместе с этим изменением")
	}
	sort.Strings(places)
	t.Logf("перепись: законных квалифицированных упоминаний вне пакета полос %d (%s) — "+
		"имя НАЗЫВАЕТСЯ, а не читается", qualified, strings.Join(places, ", "))
}

// ─────────────────────────────────────────────────────────────────────────────
// ГАШЕНИЕ ПРОИЗВОДИТСЯ В ОДНОМ МЕСТЕ.
//
// Мест, ОТКУДА носитель гасится, законно больше одного — путь отказа полосы,
// обработчик выхода, ответ глагола выхода на ретрансляции. Запрещено второе
// место, где СОБИРАЮТСЯ АТРИБУТЫ: совпадение атрибутов держалось бы вниманием
// автора, а расхождение молчит — браузер сопоставляет печенье по имени, пути и
// домену, и гашение с другим путём он не находит. «Выйти» оставляет человека
// вошедшим, и ни одна проверка знака срока этого не видит.
//
// Судится литерал `http.Cookie` с ПУСТЫМ значением и отрицательным сроком:
// это и есть форма гашения. Выдача печенья (непустое значение) предметом не
// является — её производит служба, а край её лишь пропускает.
func TestSessionCarrierEndings_AreBuiltInExactlyOnePlace(t *testing.T) {
	roots := map[string]string{
		"пакет полос":           ".",
		"обработчики":           "../handler",
		"композиционный корень": "../../cmd/api-gateway",
	}
	var sites []string
	filesRead := 0
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("каталог %s не прочитан: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("%s не разбирается: %v", name, err)
			}
			filesRead++
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || !isHTTPCookieLit(cl) {
					return true
				}
				if !looksLikeAnEnding(cl) {
					return true
				}
				sites = append(sites, fset.Position(cl.Pos()).String())
				return true
			})
		}
	}
	if filesRead == 0 {
		t.Fatal("обход пуст — гейт судил бы о непрочитанном")
	}
	sort.Strings(sites)
	if len(sites) != 1 {
		t.Errorf("мест сборки гасящего печенья %d, ожидалось 1 (%s): второе совпадало бы с первым "+
			"лишь вниманием автора, а расхождение молчит — браузер не сопоставит гашение с чужим "+
			"путём, и «выйти» оставит человека вошедшим при верном знаке срока",
			len(sites), strings.Join(sites, ", "))
	}
	t.Logf("перепись: файлов осмотрено %d (пакет полос · обработчики · композиционный корень) · "+
		"мест сборки гасящего печенья %d", filesRead, len(sites))
}

// isHTTPCookieLit — литерал типа `http.Cookie`.
func isHTTPCookieLit(cl *ast.CompositeLit) bool {
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Cookie" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "http"
}

// looksLikeAnEnding — несёт ли литерал форму ГАШЕНИЯ: пустое значение и
// отрицательный срок. Выдача (непустое значение) предметом не является.
func looksLikeAnEnding(cl *ast.CompositeLit) bool {
	emptyValue, negativeAge := false, false
	for _, e := range cl.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Value":
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING &&
				strings.Trim(lit.Value, `"`+"`") == "" {
				emptyValue = true
			}
		case "MaxAge":
			if u, ok := kv.Value.(*ast.UnaryExpr); ok && u.Op == token.SUB {
				negativeAge = true
			}
		}
	}
	return emptyValue && negativeAge
}
