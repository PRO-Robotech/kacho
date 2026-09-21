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
	"strings"
	"testing"
)

// carrierReaderConstructors — конструкторы читателей носителя, по одному на
// сторону. Перечень закрыт: новый читатель, не названный здесь, гейтом не
// осматривается, и это названо вслух — свойство держится тем, что читателей
// носителя ровно два, и переписью ниже.
var carrierReaderConstructors = map[string]string{
	"NewKratosClient":  "ReadsProvider",
	"WithHumanSession": "ReadsOwn",
}

// carrierDecisionBranchOf — имя метода множества, которым спрашивает условие
// самой внутренней ветки `if`, охватывающей позицию; "" если такой ветки нет.
//
// Ветка `else` решением не считается по той же причине, что у прежнего гейта:
// «не наш» есть «чужой или никакой», и это не ответ на вопрос «читаем ли мы
// чужой носитель».
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
		ast.Inspect(ifs.Cond, func(c ast.Node) bool {
			call, ok := c.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "ReadsOwn", "ReadsProvider":
				decision = sel.Sel.Name
			}
			return true
		})
		return true
	})
	return decision
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
