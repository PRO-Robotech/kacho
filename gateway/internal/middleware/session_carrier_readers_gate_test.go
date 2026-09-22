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
// ОБЛАСТЬ ОБХОДА: непроверочные файлы Go пакета полос (`internal/middleware`) —
// обе полосы, читающие браузерную сессию, живут здесь.
//
// ОСТАТОК: остальной модуль. Вне пакета имя носителя НАЗЫВАЕТСЯ (текст отказа
// старта, обход перечня гасимых), а не читается из запроса; читателем носителя
// это не является, и законные такие места названы отдельной пробой ниже —
// их 2, и обе координаты печатаются.
//
// Перепись начинается с области; пустой обход — отказ, а не ноль находок.
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
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
				// Признак — УПОМИНАНИЕ НОСИТЕЛЯ, а не два выписанных корня имени.
				// Прежде здесь стояли `OurSessionCarrier…` и
				// `SessionCarrierNames…`, и перепись молча упала с 2 до 1, когда
				// соседний вызов переименовался в `SessionCarrierEndings`: форма
				// вне словаря — ни красного, ни зелёного, а молчание. Ровно тот
				// класс, который эта ветка и чинит, в собственной пробе.
				if strings.Contains(sel.Sel.Name, "SessionCarrier") {
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
	// ПОРОГА ЗДЕСЬ НЕТ НАМЕРЕННО. Число — ПЕРЕПИСЬ, а не требование: сколько
	// раз чужой код законно называет носитель, решает дерево, и выписанный
	// порог пришлось бы править при каждом таком месте — то есть он стал бы
	// ещё одним выписанным знаменателем. Требование одно и по существу: хотя
	// бы одно живое упоминание, иначе сужение области оправдывает само себя.
	//
	// Усадку переписи читает тот, кто смотрит отчёт: перечень печатается
	// координатами, а не одним числом. Своим числом я уже ошибся — назвал два,
	// когда предикат знал одну форму из трёх, и перепись молча села после
	// переименования соседнего вызова.
	sort.Strings(places)
	t.Logf("перепись: законных квалифицированных упоминаний вне пакета полос %d (%s) — "+
		"имя НАЗЫВАЕТСЯ, а не читается", qualified, strings.Join(places, ", "))
}

// ─────────────────────────────────────────────────────────────────────────────
// ГАШЕНИЕ ПРОИЗВОДИТСЯ В ОДНОМ МЕСТЕ — ПО ВСЕМУ МОДУЛЮ.
//
// Мест, ОТКУДА носитель гасится, законно больше одного. Запрещено второе место,
// где СОБИРАЮТСЯ АТРИБУТЫ: совпадение держалось бы вниманием автора, а
// расхождение молчит — браузер сопоставляет печенье по имени, пути и домену, и
// гашение с другим путём он не находит. «Выйти» оставляет человека вошедшим, и
// ни одна проверка знака срока этого не видит.
//
// # ОБЛАСТЬ ВЫВЕДЕНА, А НЕ ВЫПИСАНА, и это правка предыдущей редакции
//
// Прежде обход шёл по трём ВЫПИСАННЫМ каталогам — 72 файла из 118, — а шапка
// обещала полноту. Найти второе гашение можно было, просто положив его в
// четвёртый каталог: обход туда не заглядывал, и обе половины переписи
// печатали ноль. Корень теперь один и выведенный — корень модуля, — и сузить
// его, не тронув модуль, нечем.
//
// # ПРЕДИКАТ ГАШЕНИЯ СУЖЕН ДО ПРИЗНАКА, КОТОРЫЙ НЕ ОБОЙТИ ЗАПИСЬЮ
//
// Прежний ждал ДВУХ примет разом: пустое значение литералом и срок унарным
// минусом. Обе обходятся записью, не меняя смысла: значение можно ОПУСТИТЬ
// (нулевое значение поля — та же пустая строка), а срок назвать КОНСТАНТОЙ.
//
// Признак теперь один и по существу: ПЕЧЕНЬЕ С ПУСТЫМ ЗНАЧЕНИЕМ. Пустое
// значение не выдают — его выдача не значила бы ничего; печенье с пустым
// значением есть гашение, какой бы записью ни был назван срок. Опущенное поле
// считается пустым, потому что таково нулевое значение.
func TestSessionCarrierEndings_AreBuiltInExactlyOnePlace(t *testing.T) {
	root := moduleRootOf(t)
	files := prodGoFilesUnder(t, root)
	if len(files) == 0 {
		t.Fatal("обход пуст — гейт судил бы о непрочитанном")
	}

	var sites, unresolved []string
	for _, path := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		consts := stringConstsOf(f)
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isHTTPCookieLit(cl) {
				return true
			}
			rel, _ := filepath.Rel(root, path)
			at := filepath.ToSlash(rel) + ":" + strconv.Itoa(fset.Position(cl.Pos()).Line)
			switch looksLikeAnEnding(cl, consts) {
			case valueEmpty:
				sites = append(sites, at)
			case valueUnknown:
				unresolved = append(unresolved, at)
			case valueNonEmpty:
			}
			return true
		})
	}
	sort.Strings(unresolved)
	sort.Strings(sites)
	if len(sites) != 1 {
		t.Errorf("мест сборки гасящего печенья %d, ожидалось 1 (%s): второе совпадало бы с первым "+
			"лишь вниманием автора, а расхождение молчит — браузер не сопоставит гашение с чужим "+
			"путём, и «выйти» оставит человека вошедшим при верном знаке срока",
			len(sites), strings.Join(sites, ", "))
	}
	// ТРЕТИЙ ИСХОД ПЕЧАТАЕТСЯ ЧИСЛОМ. Значение величиной, которую разбор не
	// разрешил, — не «выдача» и не «гашение»: это место, о котором гейт не
	// высказывается, и молча отнести его к выдаче значило бы вернуть обход.
	t.Logf("перепись: ОБЛАСТЬ ОБХОДА — весь модуль, корень выведен по go.mod (состав взят у "+
		"индекса); ОСТАТОК: файлы вне индекса и неразбираемые. Осмотрено непроверочных файлов "+
		"Go %d · мест сборки гасящего печенья %d (%s) · значений, не разрешённых разбором, %d%s",
		len(files), len(sites), strings.Join(sites, ", "), len(unresolved),
		func() string {
			if len(unresolved) == 0 {
				return ""
			}
			return " (" + strings.Join(unresolved, ", ") + ")"
		}())
}

// moduleRootOf — корень модуля, найденный по go.mod.
//
// Корень ВЫВЕДЕН, и это означает ровно одно: он не задан строкой в этом файле,
// а найден подъёмом до объявления модуля. Обещания «сузить нечем» здесь нет —
// механизма, который бы его держал, не существует: перенеси кто-нибудь go.mod
// либо подмени рабочий каталог, и корень станет другим. Держится это
// устройством модуля, а не проверкой.
func moduleRootOf(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod не найден выше %s — корень области не выводится", dir)
		}
		dir = parent
	}
}

// prodGoFilesUnder — НЕПРОВЕРОЧНЫЕ файлы Go модуля, ВЗЯТЫЕ У ИНДЕКСА.
//
// Не обходом диска: под корнем лежат каталоги, которых в репозитории нет —
// рабочие копии агентов, отчёты прогонов, сборочные каталоги. Обход диска
// сделал бы вердикт свойством чужого рабочего каталога, а не коммита, и ошибся
// бы в обе стороны — красным на файле, которого в репозитории нет, и молчанием
// в свежем checkout.
func prodGoFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева %s: %v — гейт не может назвать дерево, о котором говорит", root, err)
	}
	var out []string
	for rel := range tree.Files() {
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			out = append(out, filepath.Join(root, rel))
		}
	}
	sort.Strings(out)
	return out
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

// cookieValueKind — что разбор смог сказать о значении печенья.
type cookieValueKind int

const (
	// valueEmpty — значение ПУСТО: литерал "" либо константа, разрешённая
	// разбором в пустую строку, либо поле опущено (нулевое значение).
	valueEmpty cookieValueKind = iota
	// valueNonEmpty — значение непусто литералом: печенье выдаётся.
	valueNonEmpty
	// valueUnknown — значение приходит величиной, которую разбор разрешить не
	// смог. ТРЕТИЙ исход, а не «значит, выдача»: молча отнести его к выдаче
	// означало бы вернуть обход, закрытый на литерале, в форме константы.
	valueUnknown
)

// looksLikeAnEnding — ПЕЧЕНЬЕ С ПУСТЫМ ЗНАЧЕНИЕМ.
//
// Признак один и по существу: пустое значение не выдают — выдача пустого не
// значила бы ничего. Опущенное поле считается пустым, потому что таково нулевое
// значение.
//
// КОНСТАНТА, РАВНАЯ ПУСТОЙ СТРОКЕ, — ТО ЖЕ ПУСТОЕ ЗНАЧЕНИЕ. Прежняя редакция
// читала её как выдачу и потому не видела второго места гашения, названного
// константой: обход, закрытый на литерале, вернулся в форме имени. Константы
// пакета разрешаются разбором; неразрешённое остаётся ТРЕТЬИМ исходом и
// печатается переписью числом, а не приписывается к выдаче.
func looksLikeAnEnding(cl *ast.CompositeLit, consts map[string]string) cookieValueKind {
	for _, e := range cl.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Value" {
			continue
		}
		switch v := kv.Value.(type) {
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return valueUnknown
			}
			if strings.Trim(v.Value, `"`+"`") == "" {
				return valueEmpty
			}
			return valueNonEmpty
		case *ast.Ident:
			val, known := consts[v.Name]
			if !known {
				return valueUnknown
			}
			if val == "" {
				return valueEmpty
			}
			return valueNonEmpty
		default:
			return valueUnknown
		}
	}
	// Поле ОПУЩЕНО: нулевое значение поля есть пустая строка, то есть гашение.
	return valueEmpty
}

// stringConstsOf — строковые константы файла, имя → значение. Разрешаются
// только константы ЭТОГО файла: импортированная константа разбором отсюда не
// видна, и такое значение остаётся третьим исходом.
func stringConstsOf(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					out[n.Name] = strings.Trim(lit.Value, `"`+"`")
				}
			}
		}
	}
	return out
}
