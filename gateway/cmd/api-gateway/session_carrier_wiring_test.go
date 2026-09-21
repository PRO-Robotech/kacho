// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_wiring_test.go — гейт композиционного корня: читателей
// носителя браузерной сессии заводит МНОЖЕСТВО читателей, а не посадка.
//
// # Предмет и почему он сменился
//
// Прежний гейт (`own_lane_readers_wiring_test.go`) требовал обратного: каждый
// читатель — внутри ветки, называющей посадку (`identityposture.External` /
// `.Own`). Это ровно то устройство, которое делает переход НЕВЫРАЗИМЫМ:
// значений у посадки два, они взаимно исключают друг друга, и состояния «наш
// читается, и чужой ЕЩЁ читается» не существует. Перевод любого стенда с
// людьми означал бы мгновенную потерю входа у каждого, чья сессия жива.
//
// Предмет переведён на признак, ПРОИЗВОДИМЫЙ деревом: читатель заводится
// внутри ветки, чьё условие спрашивает МНОЖЕСТВО читателей
// (`carriers.ReadsOwn()` / `carriers.ReadsProvider()`). Множество выражает три
// состояния, посадка — два взаимоисключающих, и это единственная разница.
//
// # Что гейт судит и чего он не судит
//
// Судит: ВЛОЖЕННОСТЬ узла вызова конструктора в ветку решения, разбором
// исходника. Не судит текст: имена `own` и `external` стоят и в комментариях, и
// в текстах отказов.
//
// НЕ судит старшинство. Кто выигрывает при двух предъявленных носителях —
// свойство полосы, а не корня, и держит его
// `middleware/session_carrier_precedence_test.go`: множество нарочно устроено
// так, что порядок в нём непредставим.
//
// ОБЛАСТЬ ОБХОДА: композиционный корень (`main.go`) — для гейтов провязки; и
// непроверочные файлы ДВУХ пакетов, `internal/middleware` и `internal/config`,
// — для сверки перечня с деревом. Корни выписаны здесь и уходят параметром
// помощникам.
//
// ОСТАТОК: весь прочий модуль. Читатель носителя, заведённый НЕ в
// композиционном корне, этими гейтами не осматривается; имя конструктора,
// объявленное вне двух названных пакетов, опознано не будет. Свойство держится
// тем, что сборка зависимостей края живёт в одном корне (`arch-wiring-cmd-only`),
// и этой оговоркой — не обходом.
//
// Объявление стоит здесь потому, что МЕХАНИЗМ ЭТОТ СЛУЧАЙ НЕ ВИДИТ: корень
// уходит параметром помощнику, а такая форма — одна из одиннадцати, названных
// невидимыми в шапке `internal/repohygiene/gatescopedeclared_test.go`. Нашёл
// его человек, а не обход, и это ровно та цена частичного предиката, которая
// там объявлена.
//
// СУДИТ ЛИ ЭТОТ ФАЙЛ СОСЕДНИЙ ГЕЙТ — НЕТ, и вот число: из семнадцати файлов
// дельты он судит ОДИН, и это не он. Форма его обхода — каталог через параметр
// помощнику — первая в перечне невидимых. Сказано здесь потому, что молчание
// об этом и есть та разница между «зелено» и «проверено», против которой
// сосед заведён. Измерено на `9ae1ca86`, 2026-09-22.
package main

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

// carrierReaderConstructors — конструкторы читателей носителя, по одному на
// сторону, и КАЖДАЯ половина пары проверяется против дерева, а не только этого
// файла:
//
//   - имя конструктора обязано существовать в пакете полос
//     (`internal/middleware`). Переименование роняет гейт переписью, а не делает
//     его тихо беспредметным — прежде перечень держался сам на себе, и
//     переименованный читатель просто переставал осматриваться;
//   - имя вопроса обязано существовать методом множества
//     (`internal/config`). Сторона, добавленная множеству и забытая здесь, —
//     находка предпосылки, а не молчание.
//
// Перечень при этом остаётся объявленным: вывести «что есть читатель носителя»
// из дерева одним обходом нечем — читатель узнаётся по смыслу, а не по форме, —
// и это сказано вслух вместо того, чтобы выглядеть выведенным.
var carrierReaderConstructors = map[string]string{
	"NewKratosClient":  "ReadsProvider",
	"WithHumanSession": "ReadsOwn",
}

// TestSessionCarrierWiringGate_ItsRosterIsCheckedAgainstTheTree — ПРЕДПОСЫЛКА
// гейта провязки: и конструкторы, и вопросы множества существуют.
func TestSessionCarrierWiringGate_ItsRosterIsCheckedAgainstTheTree(t *testing.T) {
	lanes := identifiersDeclaredIn(t, "../../internal/middleware")
	set := identifiersDeclaredIn(t, "../../internal/config")

	sides := map[string]bool{}
	for constructor, question := range carrierReaderConstructors {
		if !lanes[constructor] {
			t.Errorf("конструктор читателя %q не объявлен в пакете полос — перечень гейта пережил "+
				"своё имя, и читатель перестал осматриваться молча", constructor)
		}
		if !set[question] {
			t.Errorf("вопрос множества %q не объявлен в пакете настройки — перечень гейта "+
				"спрашивает о стороне, которой нет", question)
		}
		sides[question] = true
	}
	// СТОРОНЫ ВЫВОДЯТСЯ ИЗ САМОГО МНОЖЕСТВА, а не выписываются. Прежде здесь
	// стоял литерал из двух имён, а над ним — утверждение о выведении: цикл не
	// мог узнать о третьей стороне и молчал бы о ней, то есть ровно о том
	// случае, ради которого заведён.
	questions := carrierSetQuestions(t, "../../internal/config")
	if len(questions) == 0 {
		t.Fatal("у множества не найдено ни одного вопроса вида Reads* — предикат перестал " +
			"опознавать свой предмет, и перечень гейта сверять не с чем")
	}
	for _, question := range questions {
		if !sides[question] {
			t.Errorf("множество отвечает на %q, а перечень гейта эту сторону не называет — её "+
				"читатель не осматривается", question)
		}
	}
	t.Logf("перепись: записей перечня %d · сторон У МНОЖЕСТВА %d (%s) · покрыто перечнем %d · "+
		"опознано имён в пакете полос %d · в пакете настройки %d",
		len(carrierReaderConstructors), len(questions), strings.Join(questions, ", "),
		len(sides), len(lanes), len(set))
}

// carrierSetQuestions — ВОПРОСЫ МНОЖЕСТВА, выведенные из его собственного
// объявления: методы с приёмником `SessionCarrierSet`, чьё имя начинается на
// `Reads`.
//
// Выводится, а не выписывается, и предмет вывода назван точно: сторона
// множества есть метод-вопрос о ней. Добавит кто-нибудь третью сторону —
// перечень гейта станет неполным в тот же прогон, а не когда заметят.
func carrierSetQuestions(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог %s не прочитан: %v", dir, err)
	}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("%s не разбирается: %v", name, err)
		}
		files++
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Reads") {
				continue
			}
			// Приёмник обязан быть именно множеством: метод `ReadsSomething` у
			// соседнего типа стороной носителя не является.
			id, ok := fn.Recv.List[0].Type.(*ast.Ident)
			if !ok || id.Name != carrierSetTypeName {
				continue
			}
			out = append(out, fn.Name.Name)
		}
	}
	if files == 0 {
		t.Fatalf("в %s не найдено непроверочных файлов Go", dir)
	}
	sort.Strings(out)
	return out
}

// carrierSetTypeName — тип множества читателей. Назван константой: его
// переименование обязано ронять вывод перепись, а не делать его беспредметным.
const carrierSetTypeName = "SessionCarrierSet"

// identifiersDeclaredIn — имена функций и методов, объявленные непроверочными
// файлами пакета.
func identifiersDeclaredIn(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог %s не прочитан: %v", dir, err)
	}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("%s не разбирается: %v", name, err)
		}
		files++
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				out[fn.Name.Name] = true
			}
		}
	}
	if files == 0 {
		t.Fatalf("в %s не найдено непроверочных файлов Go — предпосылка судила бы о непрочитанном", dir)
	}
	return out
}

// carrierDecisionBranchOf — РЕШЕНИЕ МНОЖЕСТВА, которым открыта самая внутренняя
// охватывающая позицию ветка `if`; "" если такой ветки нет.
//
// # Судится ФОРМА условия, а не наличие имени
//
// Прежняя редакция искала в условии имя метода и на этом останавливалась.
// Опознавание по имени обходится отрицанием: «если НЕ читаем нашу сторону —
// завести нашего читателя» несёт то же имя и означает ровно обратное, и гейт
// молчал бы. Дизъюнкция обходит его так же: «читаем нашу сторону ИЛИ что-то
// ещё» заводит читателя там, где множество его не называло.
//
// Законная форма названа положительно: условие раскладывается по `&&`, и среди
// слагаемых обязан стоять ГОЛЫЙ вызов `<множество>.ReadsOwn()` либо
// `.ReadsProvider()`. Прочие слагаемые допустимы — они только СУЖАЮТ: `&&` не
// способно завести читателя там, где множество его не назвало. Отрицание и
// дизъюнкция с участием множества — находка: первое переворачивает решение,
// вторая расширяет его.
func carrierDecisionBranchOf(f *ast.File, pos token.Pos, receiver string) string {
	decision := ""
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if pos <= ifs.Body.Lbrace || pos >= ifs.Body.Rbrace {
			return true
		}
		if d := lawfulSetDecision(ifs.Cond, receiver); d != "" {
			decision = d
		}
		return true
	})
	return decision
}

// lawfulSetDecision — имя метода множества, если условие имеет ЗАКОННУЮ форму;
// "" иначе, в том числе когда множество в условии есть, но форма незаконна.
func lawfulSetDecision(cond ast.Expr, receiver string) string {
	if usesSetUnlawfully(cond, receiver) {
		return ""
	}
	for _, term := range conjuncts(cond) {
		if name := bareSetCall(term, receiver); name != "" {
			return name
		}
	}
	return ""
}

// conjuncts раскладывает выражение по `&&`.
func conjuncts(e ast.Expr) []ast.Expr {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.LAND {
		return append(conjuncts(b.X), conjuncts(b.Y)...)
	}
	return []ast.Expr{e}
}

// carrierSetReceiver — ИМЯ ВЕЛИЧИНЫ, держащей множество читателей, выведенное
// из дерева: то, чему присвоен результат `ResolvedSessionCarriers`.
//
// Прежде признак решения смотрел только на ИМЯ МЕТОДА, и приёмник не проверял
// вовсе: `somethingElse.ReadsOwn()` прошёл бы как решение множества. Имя
// величины не выписывается здесь по той же причине, по которой не выписываются
// стороны, — оно берётся оттуда, где заводится.
func carrierSetReceiver(t *testing.T, f *ast.File) string {
	t.Helper()
	name := ""
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) == 0 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ResolvedSessionCarriers" {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok {
			name = id.Name
		}
		return true
	})
	if name == "" {
		t.Fatal("величина множества читателей не найдена: результат ResolvedSessionCarriers " +
			"никому не присваивается — признак решения не на чем основать")
	}
	return name
}

// bareSetCall — имя метода, если выражение есть ГОЛЫЙ вызов `<множество>.ReadsOwn()`
// или `.ReadsProvider()` без обёрток. Приёмник обязан быть названной величиной:
// чужой объект с тем же именем метода решением множества не является.
func bareSetCall(e ast.Expr, receiver string) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || (receiver != "" && id.Name != receiver) {
		return ""
	}
	switch sel.Sel.Name {
	case "ReadsOwn", "ReadsProvider":
		return sel.Sel.Name
	}
	return ""
}

// usesSetUnlawfully — участвует ли множество в ОТРИЦАНИИ либо в ДИЗЪЮНКЦИИ.
func usesSetUnlawfully(cond ast.Expr, receiver string) bool {
	bad := false
	ast.Inspect(cond, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op == token.NOT && mentionsSet(x.X, receiver) {
				bad = true
			}
		case *ast.BinaryExpr:
			if x.Op == token.LOR && (mentionsSet(x.X, receiver) || mentionsSet(x.Y, receiver)) {
				bad = true
			}
		}
		return true
	})
	return bad
}

// mentionsSet — упоминает ли выражение вызов множества.
func mentionsSet(e ast.Expr, receiver string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && bareSetCall(call, receiver) != "" {
			found = true
		}
		return true
	})
	return found
}

// TestSessionCarrierWiring_EveryReaderIsWiredByTheCarrierSet — «мест N ·
// заведено решением множества M · заведено посадкой K», требуется M = N, K = 0.
func TestSessionCarrierWiring_EveryReaderIsWiredByTheCarrierSet(t *testing.T) {
	fset, f := parseMain(t)
	receiver := carrierSetReceiver(t, f)

	total, bySet, byPosture := 0, 0, 0
	for callee, want := range carrierReaderConstructors {
		sites := wiringSites(fset, f, callee)
		if len(sites) == 0 {
			t.Fatalf("читатель %q не провязывается вовсе — молчание гейта ничего не утверждало бы", callee)
		}
		for _, s := range sites {
			total++
			decision := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, callee, s.pos), receiver)
			switch {
			case decision == want:
				bySet++
			default:
				if s.posture != "" {
					byPosture++
				}
				t.Errorf("читатель %q заведён не решением множества: %s (решение множества: %q, "+
					"ветка посадки: %q). Ожидалось условие %s(): посадка выражает два взаимоисключающих "+
					"состояния, а переход требует трёх — только чужой · оба · только наш",
					callee, s.pos, decision, s.posture, want)
			}
		}
	}
	t.Logf("перепись: мест провязки читателя носителя %d · заведено решением множества %d "+
		"(приёмник %q, выведен из дерева) · заведено веткой посадки %d · сторон носителя 2",
		total, bySet, receiver, byPosture)
}

// mustPosOf возвращает позицию вызова по её текстовой координате: wiringSites
// отдаёт координату, а вложенность судится по token.Pos.
func mustPosOf(t *testing.T, fset *token.FileSet, f *ast.File, callee, coord string) token.Pos {
	t.Helper()
	for _, pos := range f1bFindCall(f, callee) {
		if fset.Position(pos).String() == coord {
			return pos
		}
	}
	t.Fatalf("координата %s не найдена среди вызовов %q", coord, callee)
	return token.NoPos
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика, ТЕМ ЖЕ телом гейта.

const carrierWiringFixture = `package main
func wire(carriers config.SessionCarrierSet) {
	if carriers.ReadsProvider() {
		auth = auth.WithKratos(middleware.NewKratosClient(url))
	}
	if carriers.ReadsOwn() {
		auth = auth.WithHumanSession(ad)
	}
%s
}
`

// Дефект: читатель, заведённый ВЕТКОЙ ПОСАДКИ, — красное с координатой.
func TestSessionCarrierWiringGate_Injection_APostureBranchIsFound(t *testing.T) {
	fset, f := judgeCarrierFixture(t,
		"\tif lane == identityposture.External { who = who.WithKratos(middleware.NewKratosClient(url), lookup) }")
	var bare []string
	for _, s := range wiringSites(fset, f, "NewKratosClient") {
		if carrierDecisionBranchOf(f, mustPosOf(t, fset, f, "NewKratosClient", s.pos), "carriers") != "ReadsProvider" {
			bare = append(bare, s.pos)
		}
	}
	if len(bare) != 1 || !strings.HasPrefix(bare[0], "main.go:9:") {
		t.Fatalf("читатель под веткой посадки не назван координатой: %v", bare)
	}
}

// Близнец: читатель под решением множества — молчит. Без этой половины гейт
// краснел бы на законной провязке.
func TestSessionCarrierWiringGate_Twin_ASetDecisionIsSilent(t *testing.T) {
	fset, f := judgeCarrierFixture(t, "")
	for callee, want := range carrierReaderConstructors {
		for _, s := range wiringSites(fset, f, callee) {
			if got := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, callee, s.pos), "carriers"); got != want {
				t.Fatalf("законный читатель %q объявлен находкой: решение %q, ожидалось %q", callee, got, want)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ФОРМА УСЛОВИЯ: инъекция по КАЖДОМУ способу обойти опознавание по имени.
//
// Гейт провязки — единственный контроль, держащий свойство «читателей заводит
// множество, а не посадка». Пока он опознавал ветку по имени метода, обойти его
// можно было не убрав имя: отрицанием и дизъюнкцией. Инъекция подавала только
// ветку посадки — то есть проверяла один способ из трёх.

func TestSessionCarrierWiringGate_Injection_EveryEvasionOfTheNameCheckIsFound(t *testing.T) {
	cases := []struct {
		name     string
		cond     string
		receiver string
		lawful   bool
	}{
		{"голый вызов множества", "carriers.ReadsOwn()", "carriers", true},
		{"сужающее слагаемое рядом", `carriers.ReadsOwn() && url != "disabled"`, "carriers", true},
		{"сужающее слагаемое слева", `url != "disabled" && carriers.ReadsProvider()`, "carriers", true},
		{"ОТРИЦАНИЕ переворачивает решение", "!carriers.ReadsOwn()", "carriers", false},
		{"отрицание внутри конъюнкции", `carriers.ReadsProvider() && !carriers.ReadsOwn()`, "carriers", false},
		{"ДИЗЪЮНКЦИЯ расширяет решение", `carriers.ReadsOwn() || legacyFlag`, "carriers", false},
		{"дизъюнкция справа", `legacyFlag || carriers.ReadsOwn()`, "carriers", false},
		{"ветка посадки", "lane == identityposture.Own", "carriers", false},
		{"условие вовсе не о множестве", `url != "disabled"`, "carriers", false},
		// ПРИЁМНИК: то же имя метода у ЧУЖОГО объекта решением множества не
		// является. Без этих строк признак опознавал имя метода и пропускал
		// любой приёмник.
		{"ЧУЖОЙ приёмник с тем же именем метода", "somethingElse.ReadsOwn()", "carriers", false},
		{"чужой приёмник в конъюнкции", `somethingElse.ReadsProvider() && url != ""`, "carriers", false},
	}
	found := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package main\nfunc wire() {\n\tif " + tc.cond +
				" {\n\t\tauth = auth.WithHumanSession(ad)\n\t}\n}\n"
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "main.go", src, 0)
			if err != nil {
				t.Fatalf("синтетика не разбирается: %v", err)
			}
			sites := wiringSites(fset, f, "WithHumanSession")
			if len(sites) != 1 {
				t.Fatalf("мест %d, ожидалось 1", len(sites))
			}
			got := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, "WithHumanSession", sites[0].pos), tc.receiver)
			if tc.lawful && got == "" {
				t.Fatalf("законная форма %q объявлена находкой", tc.cond)
			}
			if !tc.lawful && got != "" {
				t.Fatalf("форма %q принята как решение множества (%q) — гейт обходится, не убирая "+
					"имени метода из условия", tc.cond, got)
			}
			if !tc.lawful {
				found++
			}
		})
	}
	t.Logf("перепись: форм условия проверено %d · законных 3 · найденных обходов %d "+
		"(включая чужой приёмник)", len(cases), found)
}

func judgeCarrierFixture(t *testing.T, extra string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", strings.Replace(carrierWiringFixture, "%s", extra, 1), 0)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	return fset, f
}

// ПЕРЕХОДНОЕ ОКНО ОБЪЯВЛЯЕТСЯ ТЕМ ЖЕ МНОЖЕСТВОМ, ЧТО И ЧИТАТЕЛИ.
//
// Окно решает о полномочии (в нём положительный пол второго фактора на чужой
// полосе не удовлетворяется), и объявить его чем-то ДРУГИМ, чем множество
// читателей, значило бы завести второй источник одного состояния: профиль
// назвал бы обе стороны, а окно осталось бы закрытым — молча.
func TestSessionCarrierWiring_TheTransitionalWindowIsDeclaredByTheSameSet(t *testing.T) {
	fset, f := parseMain(t)
	sites := f1bFindCall(f, "WithTransitionalCarrierWindow")
	if len(sites) == 0 {
		t.Fatal("окно не объявляется никому — молчание гейта ничего не утверждало бы")
	}
	// ПОТРЕБИТЕЛЕЙ ОКНА НЕСКОЛЬКО, И ЭТО ЗАКОННО: полос, читающих чужую
	// сессию, две, и каждая держит свой читатель окна — вложенные точки
	// предъявления. Запрещено не второе объявление, а ВТОРОЙ ИСТОЧНИК: величина
	// у всех обязана быть ОДНА, иначе полосы разойдутся молча, и разойдутся
	// ровно в ту сторону, где одна пускает то, что вторая отвергает.
	idents := map[string]bool{}
	arg := ""
	for _, pos := range sites {
		id := windowArgIdent(t, f, pos)
		idents[id] = true
		arg = id
	}
	if len(idents) != 1 {
		names := make([]string, 0, len(idents))
		for n := range idents {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("окно объявляется %d величинами (%s) — у потребителей разные источники, и они "+
			"разойдутся молча", len(idents), strings.Join(names, ", "))
	}

	// Вопрос об окне ОДИН и задаётся множеству читателей.
	asks := f1bFindCall(f, "IsTransitionalWindow")
	if len(asks) != 1 {
		t.Fatalf("вопросов «открыто ли окно» %d, ожидался 1: два вычисления одного состояния "+
			"разошлись бы молча", len(asks))
	}

	// Непустое значение присваивается величине ТОЛЬКО под этим вопросом.
	bare := assignmentsOutsideWindowBranch(fset, f, arg)
	for _, pos := range bare {
		t.Errorf("момент открытия окна присваивается вне ветки «открыто ли окно»: %s. Окно и "+
			"читатели разошлись бы: профиль назвал бы одну сторону, а окно осталось бы открытым",
			pos)
	}
	t.Logf("перепись: потребителей окна %d · различных величин %d · вопросов «открыто ли окно» %d · "+
		"непустых присваиваний вне ветки %d", len(sites), len(idents), len(asks), len(bare))
}

// windowArgIdent — имя величины, переданной объявлению окна.
func windowArgIdent(t *testing.T, f *ast.File, pos token.Pos) string {
	t.Helper()
	name := ""
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() != pos || len(call.Args) != 1 {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok {
			name = id.Name
		}
		return false
	})
	if name == "" {
		t.Fatal("окно объявляется не именованной величиной — судить о её происхождении нечем")
	}
	return name
}

// assignmentsOutsideWindowBranch — присваивания НЕПУСТОГО значения названной
// величине вне ветки, спрашивающей `IsTransitionalWindow`.
//
// Обнуление (`time.Time{}`) находкой не является: оно закрывает окно, а не
// открывает его, и стоять обязано именно снаружи.
func assignmentsOutsideWindowBranch(fset *token.FileSet, f *ast.File, ident string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != ident || i >= len(as.Rhs) {
				continue
			}
			if isZeroTimeLiteral(as.Rhs[i]) {
				continue
			}
			if !insideWindowQuestion(f, as.Pos()) {
				out = append(out, fset.Position(as.Pos()).String())
			}
		}
		return true
	})
	return out
}

// isZeroTimeLiteral — выражение вида `time.Time{}`.
func isZeroTimeLiteral(e ast.Expr) bool {
	cl, ok := e.(*ast.CompositeLit)
	if !ok || len(cl.Elts) != 0 {
		return false
	}
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time" && sel.Sel.Name == "Time"
}

// insideWindowQuestion — лежит ли позиция внутри ветки, чьё условие спрашивает
// `IsTransitionalWindow`.
func insideWindowQuestion(f *ast.File, pos token.Pos) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || pos <= ifs.Body.Lbrace || pos >= ifs.Body.Rbrace {
			return true
		}
		ast.Inspect(ifs.Cond, func(c ast.Node) bool {
			if sel, ok := c.(*ast.SelectorExpr); ok && sel.Sel.Name == "IsTransitionalWindow" {
				found = true
			}
			return true
		})
		return true
	})
	return found
}

// ─────────────────────────────────────────────────────────────────────────────
// НИ ОДНО СОСТОЯНИЕ НОСИТЕЛЯ НЕ ОСЛАБЛЯЕТ ПРИЁМ ТОКЕНА.
//
// Множество читателей отвечает на «чьё печенье мы читаем» и ни на что больше.
// Приём токена решает СВОЁ объявление (`KACHO_API_GATEWAY_TOKEN_ISSUERS`),
// разбираемое `config.TokenAcceptance`, и переходное состояние у него своё, с
// первого дня. Свойство судится ВЛОЖЕННОСТЬЮ: приём токена, оказавшийся внутри
// ветки решения о носителе, означал бы состояние носителя, в котором издателей
// принимается больше или проверяется меньше.

// tokenAcceptanceSites — места, где край решает, чей токен он принимает.
var tokenAcceptanceSites = []string{"TokenAcceptance", "NewJWTVerifier"}

func TestSessionCarrierWiring_NoCarrierStateReachesTokenAcceptance(t *testing.T) {
	fset, f := parseMain(t)
	receiver := carrierSetReceiver(t, f)
	seen := 0
	for _, callee := range tokenAcceptanceSites {
		positions := f1bFindCall(f, callee)
		if len(positions) == 0 {
			t.Fatalf("место приёма токена %q не найдено — молчание гейта ничего не утверждало бы", callee)
		}
		for _, pos := range positions {
			seen++
			if d := carrierDecisionBranchOf(f, pos, receiver); d != "" {
				t.Errorf("приём токена %q стоит внутри ветки решения о носителе (%s) в %s: "+
					"появилось бы состояние носителя, в котором издателей принимается больше "+
					"или проверяется меньше", callee, d, fset.Position(pos).String())
			}
		}
	}
	t.Logf("перепись: мест приёма токена осмотрено %d · внутри ветки решения о носителе 0", seen)
}

// ─────────────────────────────────────────────────────────────────────────────
// САМООТЧЁТ СТАРТА НАЗЫВАЕТ МОМЕНТ ОКНА.
//
// Величина, решающая, какие чужие сессии край ещё принимает, обязана быть
// наблюдаемой. Пока самоотчёт печатал только состав множества и посадку, два
// состояния были НЕРАЗЛИЧИМЫ на стенде: исправное окно и граница, которая
// ничего не отделяет, — клетка отвергнутых в обоих стоит нулём, а её смысл
// «ноль здоров» этого не различает.
//
// Гейт судит РАЗОБРАННЫЙ вызов самоотчёта: ключ, названный в нём, обязан быть
// величиной окна, а не строкой рядом.
func TestSessionCarrierWiring_TheBootSelfReportNamesTheWindowInstant(t *testing.T) {
	fset, f := parseMain(t)

	const reportMessage = "browser session carrier readers resolved"
	const windowKey = "transitional_window_opened_at"

	found, named := 0, false
	var coord string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || strings.Trim(lit.Value, `"`) != reportMessage {
			return true
		}
		found++
		coord = fset.Position(call.Pos()).String()
		for _, a := range call.Args[1:] {
			if l, ok := a.(*ast.BasicLit); ok && l.Kind == token.STRING &&
				strings.Trim(l.Value, `"`) == windowKey {
				named = true
			}
		}
		return true
	})

	if found != 1 {
		t.Fatalf("самоотчётов о читателях носителя %d, ожидался 1: второй разошёлся бы с первым молча",
			found)
	}
	if !named {
		t.Errorf("самоотчёт (%s) не называет %q — момент окна не наблюдается нигде, и «окно "+
			"исправно» неотличимо от «граница не отделяет ничего»", coord, windowKey)
	}
	t.Logf("перепись: самоотчётов о носителе %d · называющих момент окна %d", found, map[bool]int{true: 1}[named])
}

// ─────────────────────────────────────────────────────────────────────────────
// КОРЕНЬ ПЕРЕДАЁТ СТРАЖУ ВСЕ ПОЛЯ, ОТ КОТОРЫХ ЗАВИСЯТ ЕГО ОСИ.
//
// Оси стража включаются переданными величинами, и поле, забытое в корне, даёт
// НУЛЕВОЕ значение — ось молча не срабатывает. Сам страж теперь отказывает без
// часов, но этого мало: отказ увидит только тот, кто дошёл до стража с окном.
// Обход корня закрывает вторую половину — что поле вообще передают.
//
// Два контроля об одном предмете здесь не дублирование, а разные вопросы:
// страж спрашивает «могу ли я судить», обход — «спросили ли меня».
func TestSessionCarrierWiring_TheGuardIsGivenEveryFieldItsAxesNeed(t *testing.T) {
	fset, f := parseMain(t)

	// СОСТАВ ПОЛЕЙ ТИПА ВЫВОДИТСЯ ИЗ РАЗБОРА, а перечень ниже только приписывает
	// каждому полю ЕГО ОСЬ.
	//
	// Прежде перечень был выписан целиком, и утверждение «корень передаёт все
	// поля, от которых зависят оси» держалось им одним: шестое поле, прочитанное
	// новой осью, в перечень не попало бы, гейт остался бы зелёным, и ось
	// выключилась бы МОЛЧА. Это дословно тот дефект, который закрыт коммитом
	// `bce51658458` на одно касание раньше в этом же диффе — там ось выключалась
	// отсутствием ЗНАЧЕНИЯ, здесь выключилась бы отсутствием СТРОКИ в перечне.
	//
	// Довод прежней шапки «вывести из типа нечем» верен про то, КАКАЯ ось читает
	// поле — это живёт в теле стража, — и неверен про то, КАКИЕ ПОЛЯ у типа
	// есть: их называет объявление, и разбирается оно тем же `go/ast`, которым
	// проба уже пользуется. Сегодня поля осей = все поля типа, и расхождение
	// между перечнем и объявлением — находка.
	axisFields := map[string]string{
		"Now":            "ось «момент окна обязан быть фактом»",
		"Carriers":       "ось согласованности пары",
		"Posture":        "ось согласованности пары",
		"WindowOpenedAt": "ось обязательности момента",
		"ProviderURL":    "ось адреса объявленного читателя",
	}

	// Судятся ВСЕ конфигурации стража в корне, а не последняя найденная.
	// Прежде обход присваивал и перезаписывал, и появись вторая — судилась бы
	// только она, а первая прошла бы молча.
	// Перечень обязан покрывать ВСЕ поля типа, и это сверяется с объявлением.
	declared := structFieldsOf(t, f, "SessionCarrierConfig")
	if len(declared) == 0 {
		// Тип объявлен в непроверочном файле пакета — ищем и там.
		declared = structFieldsInPackage(t, ".", "SessionCarrierConfig")
	}
	if len(declared) == 0 {
		t.Fatal("объявление SessionCarrierConfig не найдено — сверять перечень не с чем, и " +
			"гейт судил бы о непрочитанном")
	}
	var uncovered []string
	for _, name := range declared {
		if _, ok := axisFields[name]; !ok {
			uncovered = append(uncovered, name)
		}
	}
	sort.Strings(uncovered)
	for _, name := range uncovered {
		t.Errorf("поле %s объявлено у SessionCarrierConfig и не названо в перечне осей: новая "+
			"ось, прочитавшая его, выключится МОЛЧА — гейт останется зелёным, потому что о поле "+
			"его никто не спросил", name)
	}

	var lits []*ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := cl.Type.(*ast.Ident); ok && id.Name == "SessionCarrierConfig" {
			lits = append(lits, cl)
		}
		return true
	})
	if len(lits) == 0 {
		t.Fatal("вызова стража с SessionCarrierConfig в корне не найдено — обход судил бы о " +
			"непроисходящем")
	}

	complete := 0
	for _, lit := range lits {
		given := map[string]bool{}
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if id, ok := kv.Key.(*ast.Ident); ok {
				given[id.Name] = true
			}
		}
		var missing []string
		for field, axis := range axisFields {
			if !given[field] {
				missing = append(missing, field+" ("+axis+")")
			}
		}
		sort.Strings(missing)
		for _, m := range missing {
			t.Errorf("корень не передаёт стражу поле %s: нулевое значение выключает ось МОЛЧА, и "+
				"снятие одной строки из корня возвращает закрытую находку при зелёном наборе (%s)",
				m, fset.Position(lit.Pos()).String())
		}
		if len(missing) == 0 {
			complete++
		}
	}
	t.Logf("перепись: конфигураций стража в корне %d · полей У ТИПА (выведено разбором) %d · "+
		"названо перечнем осей %d · не названо %d · конфигураций, передавших все поля, %d",
		len(lits), len(declared), len(axisFields), len(uncovered), complete)
}

// structFieldsOf — имена полей названной структуры, объявленной в этом файле.
func structFieldsOf(t *testing.T, f *ast.File, typeName string) []string {
	t.Helper()
	return fieldsOfStructIn(f, typeName)
}

// structFieldsInPackage — то же, но по НЕПРОВЕРОЧНЫМ файлам каталога: тип
// объявлен в продуктовом файле, а проба живёт рядом.
//
// ОБЛАСТЬ ОБХОДА: один каталог — тот, где лежит страж. ОСТАТОК: остальной
// модуль; тип, переехавший в другой пакет, здесь найден не будет, и об этом
// скажет отказ «объявление не найдено», а не молчание.
func structFieldsInPackage(t *testing.T, dir, typeName string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог %s не прочитан: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			continue
		}
		if fields := fieldsOfStructIn(parsed, typeName); len(fields) > 0 {
			return fields
		}
	}
	return nil
}

// fieldsOfStructIn — имена полей структуры в разобранном файле.
func fieldsOfStructIn(f *ast.File, typeName string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return false
		}
		for _, fld := range st.Fields.List {
			if len(fld.Names) == 0 {
				// ВСТРОЕННОЕ ПОЛЕ: список имён ПУСТ, и имя даёт ТИП. Прежде
				// цикл такое поле пропускал молча, и перепись печатала состав
				// без него — ложное число под словом «выведено». Ключом
				// составного литерала у встроенного поля служит имя типа, и
				// именно его обязан называть перечень осей.
				if name := embeddedFieldName(fld.Type); name != "" {
					out = append(out, name)
				}
				continue
			}
			for _, nm := range fld.Names {
				out = append(out, nm.Name)
			}
		}
		return false
	})
	sort.Strings(out)
	return out
}

// embeddedFieldName — имя, под которым встроенное поле стоит в составном
// литерале: последний идентификатор типа, без звёздочки и без квалификатора
// пакета.
//
// Формы перечислены и опробованы в `struct_fields_forms_test.go`; граница
// названа там же — продвинутые поля встроенного типа отсюда не видны.
func embeddedFieldName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return embeddedFieldName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr:
		// Встроенный обобщённый тип: имя даёт его основа.
		return embeddedFieldName(x.X)
	}
	return ""
}
