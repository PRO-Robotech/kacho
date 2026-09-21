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
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	// Стороны множества выводятся ИЗ НЕГО: сторона, добавленная множеству и
	// забытая здесь, обязана быть находкой.
	for _, question := range []string{"ReadsOwn", "ReadsProvider"} {
		if set[question] && !sides[question] {
			t.Errorf("множество отвечает на %q, а перечень гейта эту сторону не называет — её "+
				"читатель не осматривается", question)
		}
	}
	t.Logf("перепись: записей перечня %d · сторон покрыто %d · опознано в пакете полос %d · "+
		"в пакете настройки %d", len(carrierReaderConstructors), len(sides), len(lanes), len(set))
}

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
func carrierDecisionBranchOf(f *ast.File, pos token.Pos) string {
	decision := ""
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if pos <= ifs.Body.Lbrace || pos >= ifs.Body.Rbrace {
			return true
		}
		if d := lawfulSetDecision(ifs.Cond); d != "" {
			decision = d
		}
		return true
	})
	return decision
}

// lawfulSetDecision — имя метода множества, если условие имеет ЗАКОННУЮ форму;
// "" иначе, в том числе когда множество в условии есть, но форма незаконна.
func lawfulSetDecision(cond ast.Expr) string {
	if usesSetUnlawfully(cond) {
		return ""
	}
	for _, term := range conjuncts(cond) {
		if name := bareSetCall(term); name != "" {
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

// bareSetCall — имя метода, если выражение есть ГОЛЫЙ вызов `X.ReadsOwn()` или
// `X.ReadsProvider()` без обёрток.
func bareSetCall(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	switch sel.Sel.Name {
	case "ReadsOwn", "ReadsProvider":
		return sel.Sel.Name
	}
	return ""
}

// usesSetUnlawfully — участвует ли множество в ОТРИЦАНИИ либо в ДИЗЪЮНКЦИИ.
func usesSetUnlawfully(cond ast.Expr) bool {
	bad := false
	ast.Inspect(cond, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op == token.NOT && mentionsSet(x.X) {
				bad = true
			}
		case *ast.BinaryExpr:
			if x.Op == token.LOR && (mentionsSet(x.X) || mentionsSet(x.Y)) {
				bad = true
			}
		}
		return true
	})
	return bad
}

// mentionsSet — упоминает ли выражение вызов множества.
func mentionsSet(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && bareSetCall(call) != "" {
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

	total, bySet, byPosture := 0, 0, 0
	for callee, want := range carrierReaderConstructors {
		sites := wiringSites(fset, f, callee)
		if len(sites) == 0 {
			t.Fatalf("читатель %q не провязывается вовсе — молчание гейта ничего не утверждало бы", callee)
		}
		for _, s := range sites {
			total++
			decision := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, callee, s.pos))
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
	t.Logf("перепись: мест провязки читателя носителя %d · заведено решением множества %d · "+
		"заведено веткой посадки %d · сторон носителя 2 (наша, чужая)", total, bySet, byPosture)
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
		if carrierDecisionBranchOf(f, mustPosOf(t, fset, f, "NewKratosClient", s.pos)) != "ReadsProvider" {
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
			if got := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, callee, s.pos)); got != want {
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
		name   string
		cond   string
		lawful bool
	}{
		{"голый вызов множества", "carriers.ReadsOwn()", true},
		{"сужающее слагаемое рядом", `carriers.ReadsOwn() && url != "disabled"`, true},
		{"сужающее слагаемое слева", `url != "disabled" && carriers.ReadsProvider()`, true},
		{"ОТРИЦАНИЕ переворачивает решение", "!carriers.ReadsOwn()", false},
		{"отрицание внутри конъюнкции", `carriers.ReadsProvider() && !carriers.ReadsOwn()`, false},
		{"ДИЗЪЮНКЦИЯ расширяет решение", `carriers.ReadsOwn() || legacyFlag`, false},
		{"дизъюнкция справа", `legacyFlag || carriers.ReadsOwn()`, false},
		{"ветка посадки", "lane == identityposture.Own", false},
		{"условие вовсе не о множестве", `url != "disabled"`, false},
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
			got := carrierDecisionBranchOf(f, mustPosOf(t, fset, f, "WithHumanSession", sites[0].pos))
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
	t.Logf("перепись: форм условия проверено %d · законных 3 · найденных обходов %d", len(cases), found)
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
	if len(sites) != 1 {
		t.Fatalf("объявлений переходного окна %d, ожидалось 1: второе разошлось бы с первым молча",
			len(sites))
	}
	// Величина, которой объявляется окно.
	arg := windowArgIdent(t, f, sites[0])

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
	t.Logf("перепись: объявлений окна %d · вопросов «открыто ли окно» %d · непустых присваиваний "+
		"вне ветки %d", len(sites), len(asks), len(bare))
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
	seen := 0
	for _, callee := range tokenAcceptanceSites {
		positions := f1bFindCall(f, callee)
		if len(positions) == 0 {
			t.Fatalf("место приёма токена %q не найдено — молчание гейта ничего не утверждало бы", callee)
		}
		for _, pos := range positions {
			seen++
			if d := carrierDecisionBranchOf(f, pos); d != "" {
				t.Errorf("приём токена %q стоит внутри ветки решения о носителе (%s) в %s: "+
					"появилось бы состояние носителя, в котором издателей принимается больше "+
					"или проверяется меньше", callee, d, fset.Position(pos).String())
			}
		}
	}
	t.Logf("перепись: мест приёма токена осмотрено %d · внутри ветки решения о носителе 0", seen)
}
