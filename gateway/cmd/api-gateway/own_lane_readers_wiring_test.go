// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_readers_wiring_test.go — декларативные гейты по композиционному
// корню (приёмка Ф3, Ф3-12 и Ф3-45; гейт Ф1 §8).
//
// # Предмет
//
// Читателя носителя выбирала ПОСАДКА (`cfg.ResolvedIdentityProvider()`): под
// `own` — наша сессия, под `external` — сессия поставщика. Три места
// композиционного корня — полоса личности, отзыв на ней, маршрут «кто я» —
// заводились НАЛИЧИЕМ АДРЕСА поставщика, а не посадкой (§1.1 приёмки): под
// `own` читатель носителя поставщика оставался заведённым, и печенье
// поставщика продолжало становиться личностью.
//
// # ЧИТАТЕЛЬ ОСТАЛСЯ ОДИН, И ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА СНЯТА С ПРЕДМЕТОМ (#2792)
//
// Читатель чужой сессии снят целиком: его конструктора в дереве нет. Случай
// «читатель поставщика не заведён под own» вместе с ним стал БЕСПРЕДМЕТНЫМ —
// не выполненным, а неизмеримым: его перепись требовала N ≥ 1 мест и на нуле
// честно краснела «молчание гейта ничего не утверждает». Оставить его значило
// бы либо держать красное о снятом предмете, либо снять проверку предпосылки —
// то есть получить гейт, зелёный на пустом обходе.
//
// Требование «у каждого читателя есть ветка посадки» исполняется тем, что
// читатель остался один и он наш, а его ветка посадки проверяется ниже.
//
// # Что судится — вложенность узлов, не текст
//
//   - читатель НАШЕЙ сессии — вызов `WithHumanSession`: обязан лежать внутри
//     ветки `if` или `case`, чьё условие утверждает посадку `own`, и никакое
//     замыкание не отделяет его от этой ветки (postureBranchOf). N ≥ 2 (полоса
//     личности и маршрут «кто я»);
//   - ретрансляция — вызов `NewLoginLaneRelay`: по провязке на КАЖДУЮ объявленную
//     цель, под `own` (множество, а не константа — relay_wiring_test.go);
//   - страж адреса цели — вызов `validateLoginLaneConfig`: во всём пакете ровно
//     один, внутри `prepareRelayTarget`; а `prepareRelayTarget` позван только
//     корнем, по вызову на каждую объявленную цель, и каждый под `own`. Своей
//     ветки по посадке у стража нет (#2873), поэтому путь к нему и есть то, что
//     отличает `own` от прочего.
//
// Инъекция в обе стороны на синтетике: читатель без условия посадки — красное с
// координатой; читатель под названной посадкой — молчит. Синтетика намеренно
// НЕ опирается на живой корень: опирайся она на него, доказательство исчезало
// бы вместе с каждой правкой провязки.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// postureBranchOf — посадка, под которой стоит позиция: на пути к ней есть
// решение own — тело ветки `if` или ветка `case`, чьё условие УТВЕРЖДАЕТ own, —
// и между этим решением и позицией нет замыкания. Замыкание решение снимает:
// где его позовут, судья не прослеживает. Иначе posture пуст, а why называет
// увиденное с координатой (kacho#2890).
func postureBranchOf(fset *token.FileSet, f *ast.File, pos token.Pos) (posture, why string) {
	at := fset.Position(pos).String()
	path := enclosingNodes(f, pos)
	decided := -1
	var decision ast.Node
	for i, n := range path {
		if decidesOwn(path, i, pos) {
			decided, decision = i, n
		}
	}
	if decided < 0 {
		forms := executionForms(fset, path, pos)
		if len(forms) == 0 {
			forms = []string{"ни одной формы ветвления"}
		}
		return "", "ни одна ветка if или case, утверждающая посадку own, не охватывает " + at +
			"; на пути: " + strings.Join(forms, " → ")
	}
	for _, n := range path[decided+1:] {
		if lit, ok := n.(*ast.FuncLit); ok {
			return "", fmt.Sprintf("замыкание %T у %s отделяет %s от решения own у %s — где его позовут, судья не "+
				"прослеживает", lit, fset.Position(lit.Pos()), at, fset.Position(decision.Pos()))
		}
	}
	return "Own", ""
}

// decidesOwn — решает ли path[i] посадку own для позиции: тело ветки if с
// условием, утверждающим own, либо тело ветки case, чей единственный вариант —
// own у switch по значению или утверждение own у switch без тега. Ветка else и
// ветка default решением не считаются: «не own» — не решение о посадке.
func decidesOwn(path []ast.Node, i int, pos token.Pos) bool {
	switch x := path[i].(type) {
	case *ast.IfStmt:
		return within(x.Body, pos) && assertsOwn(x.Cond)
	case *ast.CaseClause:
		if pos <= x.Colon || len(x.List) != 1 || i < 2 {
			return false
		}
		sw, ok := path[i-2].(*ast.SwitchStmt)
		if !ok {
			return false
		}
		if sw.Tag == nil {
			return assertsOwn(x.List[0])
		}
		return isOwnSelector(x.List[0])
	}
	return false
}

// assertsOwn — условие истинно только под посадкой own: сравнение `==` с
// `identityposture.Own` либо конъюнкция, один из членов которой его утверждает.
// Отрицание, `!=` и дизъюнкция own не утверждают.
func assertsOwn(cond ast.Expr) bool {
	switch x := cond.(type) {
	case *ast.ParenExpr:
		return assertsOwn(x.X)
	case *ast.BinaryExpr:
		switch x.Op {
		case token.EQL:
			return isOwnSelector(x.X) || isOwnSelector(x.Y)
		case token.LAND:
			return assertsOwn(x.X) || assertsOwn(x.Y)
		}
	}
	return false
}

// isOwnSelector — селектор `identityposture.Own`, единственный законный способ
// назвать посадку в дереве (`corelib/identityposture`). Второго законного
// значения в словаре фундамента нет: `external` снята выпуском v1.10.0-rc.3
// (corelib#30), и её имени край не читает (#2873). Вне `own` корень ветвится
// сравнением с `own`, и ветка, названная иным значением, посадкой не считается.
func isOwnSelector(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Own" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "identityposture"
}

// wiringSite — одно место провязки читателя.
type wiringSite struct {
	pos     string
	posture string // "Own" | ""
	why     string // чем посадка own не доказана; "" при "Own"
}

// wiringSites — места вызова названного метода/функции и посадка каждого.
func wiringSites(fset *token.FileSet, f *ast.File, callee string) []wiringSite {
	var out []wiringSite
	for _, pos := range f1bFindCall(f, callee) {
		posture, why := postureBranchOf(fset, f, pos)
		out = append(out, wiringSite{pos: fset.Position(pos).String(), posture: posture, why: why})
	}
	return out
}

func parseMain(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("композиционный корень не разбирается: %v", err)
	}
	return fset, f
}

// ЗДЕСЬ СТОЯЛ TestOwnLane_F3_12_NoProviderCarrierReaderIsWiredUnderOwn —
// перепись «мест N · заведено под own M», M = 0, по вызовам конструктора
// читателя носителя ЧУЖОГО поставщика. Случай снят вместе со своим предметом
// (#2792): таких вызовов в дереве ноль, и его собственная проверка предпосылки
// это и сказала бы — «читатель не провязывается вовсе, молчание гейта ничего
// не утверждает». Единственные исходы у такого случая — снять с предметом либо
// снять проверку предпосылки; второе дало бы гейт, зелёный на пустом обходе.

// TestOwnLane_F3_45_OurReaderAndTheRelayAreWiredUnderOwnOnly — наш читатель
// (полоса и «кто я»), ретрансляция и страж её адреса заведены под `own` и не
// заведены вне этой ветки. Своей ветки по посадке у стража адреса нет: он
// отвергает незаданный адрес всегда, и отличает `own` от прочего ровно место
// вызова.
func TestOwnLane_F3_45_OurReaderAndTheRelayAreWiredUnderOwnOnly(t *testing.T) {
	fset, f := parseMain(t)
	readers := wiringSites(fset, f, "WithHumanSession")
	if len(readers) < 2 {
		t.Fatalf("читателей нашей сессии провязано %d, ожидалось не меньше 2 (полоса личности и «кто я»)", len(readers))
	}
	for _, s := range readers {
		if s.posture != "Own" {
			t.Errorf("читатель нашей сессии заведён вне ветки посадки own: %s (%s)", s.pos, s.why)
		}
	}
	// Ретрансляция судится МНОЖЕСТВОМ, а не константой (замысел LINE-A-1 §5.1б
	// п. 2а, §7 инв. 36): провязок столько, сколько объявлено целей, каждая под
	// `own`, ось различения — цель. Предмет и его инъекции — relay_wiring_test.go.
	relays := judgeRelayWiring(fset, f, middleware.RelayTargets())
	for _, finding := range relays.findings {
		t.Error(finding)
	}
	// Страж адреса цели своей ветки по посадке не имеет (#2873): `own` от
	// прочего отличает МЕСТО вызова. Целей две, и страж зовётся не из корня
	// напрямую, а из prepareRelayTarget — по вызову на цель; судится весь путь.
	pkg := parsePackage(t, fset)
	guards := judgeRelayGuardPath(fset, rootOf(t, fset, pkg), pkg, middleware.RelayTargets())
	for _, finding := range guards.findings {
		t.Error(finding)
	}
	t.Logf("перепись: читателей нашей сессии %d (под own %d) · провязок ретранслятора %d · целей объявлено %d · "+
		"файлов пакета прочитано %d · вызовов стража %d · подготовок цели в корне %d",
		len(readers), len(readers), len(relays.sites), len(middleware.RelayTargets()),
		guards.files, len(guards.guards), len(guards.prepares))
}

// guardSite — вызов стража цели и функция пакета, в теле которой он стоит.
type guardSite struct {
	pos string
	fn  string
}

// prepareSite — вызов подготовки цели в корне: посадка ветки и названная цель.
type prepareSite struct {
	pos     string
	posture string
	why     string // чем посадка own не доказана; "" при "Own"
	serves  string // имя селектора цели (`RelayTargetForm`), "" если не названа
}

// relayGuardPath — вердикт о пути к стражу целей ретрансляции.
type relayGuardPath struct {
	files    int
	guards   []guardSite
	prepares []prepareSite
	findings []string
}

// enclosingFunc — имя объявления функции, в теле которого стоит позиция.
func enclosingFunc(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && pos > fd.Body.Lbrace && pos < fd.Body.Rbrace {
			return fd.Name.Name
		}
	}
	return ""
}

// plainCalls — вызовы функции пакета по голому имени.
func plainCalls(f *ast.File, callee string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == callee {
				out = append(out, call)
			}
		}
		return true
	})
	return out
}

// judgeRelayGuardPath — страж позван ровно раз и только из prepareRelayTarget;
// prepareRelayTarget позван только корнем, по вызову на цель закрытого перечня,
// каждый под `own`. Пустой перечень файлов пакета — находка, а не молчание.
func judgeRelayGuardPath(fset *token.FileSet, root *ast.File, pkg []*ast.File, targets []middleware.RelayTarget) relayGuardPath {
	var out relayGuardPath
	out.files = len(pkg)
	if len(pkg) == 0 {
		out.findings = append(out.findings, "файлов пакета не прочитано — путь к стражу судить не по чему, и это не зелёный")
		return out
	}
	for _, f := range pkg {
		for _, call := range plainCalls(f, "validateLoginLaneConfig") {
			out.guards = append(out.guards, guardSite{pos: fset.Position(call.Pos()).String(), fn: enclosingFunc(f, call.Pos())})
		}
		if f == root {
			continue
		}
		for _, call := range plainCalls(f, "prepareRelayTarget") {
			out.findings = append(out.findings, fset.Position(call.Pos()).String()+
				": подготовка цели позвана мимо корня — ветка посадки own её места не накрывает")
		}
	}
	inPrepare := 0
	for _, g := range out.guards {
		if g.fn != "prepareRelayTarget" {
			out.findings = append(out.findings, g.pos+": страж цели позван мимо prepareRelayTarget (в "+strconv.Quote(g.fn)+
				") — его место вызова ветка посадки own не судит")
			continue
		}
		inPrepare++
	}
	switch {
	case len(out.guards) == 0:
		out.findings = append(out.findings, "страж цели не позван ни разу — адрес ни одной цели при старте не судится")
	case inPrepare > 1:
		out.findings = append(out.findings, "страж цели позван в prepareRelayTarget "+strconv.Itoa(inPrepare)+" раз, ожидался 1")
	}

	want := map[string]bool{}
	for _, tg := range targets {
		if name := relayTargetSelector(tg); name != "" {
			want[name] = true
		}
	}
	if len(want) == 0 {
		out.findings = append(out.findings, "закрытый перечень целей пуст — судить нечего, и это не зелёный")
		return out
	}
	seen := map[string]string{}
	for _, call := range plainCalls(root, "prepareRelayTarget") {
		posture, why := postureBranchOf(fset, root, call.Pos())
		s := prepareSite{pos: fset.Position(call.Pos()).String(), posture: posture, why: why}
		if len(call.Args) >= 2 {
			if sel, ok := call.Args[1].(*ast.SelectorExpr); ok {
				s.serves = sel.Sel.Name
			}
		}
		out.prepares = append(out.prepares, s)
		switch {
		case s.serves == "":
			out.findings = append(out.findings, s.pos+": подготовка цели без названной цели — ось различения потеряна")
		case !want[s.serves]:
			out.findings = append(out.findings, s.pos+": подготовка цели "+s.serves+", которой нет в закрытом перечне")
		case seen[s.serves] != "":
			out.findings = append(out.findings, s.pos+": лишняя подготовка цели "+s.serves+" — первая стоит в "+seen[s.serves])
		default:
			seen[s.serves] = s.pos
		}
		if s.posture != "Own" {
			out.findings = append(out.findings, s.pos+": страж цели позван вне ветки посадки own ("+s.why+
				") — вне own он отверг бы старт края, которому ретрансляция не нужна")
		}
	}
	missing := []string{}
	for name := range want {
		if seen[name] == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		out.findings = append(out.findings, "объявленная цель "+name+" не подготовлена корнем — её адрес при старте не судится")
	}
	return out
}

// rootOf — композиционный корень среди разобранных файлов пакета.
func rootOf(t *testing.T, fset *token.FileSet, pkg []*ast.File) *ast.File {
	t.Helper()
	for _, f := range pkg {
		if filepath.Base(fset.Position(f.Pos()).Filename) == "main.go" {
			return f
		}
	}
	t.Fatal("среди файлов пакета нет main.go — корень судить не по чему")
	return nil
}

// parsePackage — не-тестовые файлы пакета корня, разобранные в тот же набор
// позиций, что и корень.
func parsePackage(t *testing.T, fset *token.FileSet) []*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("файлы пакета не перечисляются: %v", err)
	}
	var out []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("%s не разбирается: %v", name, perr)
		}
		out = append(out, f)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика.

// wiringFixture — СИНТЕТИКА, а не живой корень. Предмет инъекции — предикат
// «лежит ли вызов внутри ветки названной посадки», и опирайся он на дерево,
// доказательство менялось бы вместе с каждой правкой провязки — и исчезло бы
// ровно тогда, когда предикат достиг цели.
const wiringFixture = `package main
import "github.com/PRO-Robotech/corelib/identityposture"
func wire(lane identityposture.Provider) {
	if lane == identityposture.Own {
		auth = auth.WithHumanSession(ad)
	}
%s
}
`

func judgeWiringFixture(t *testing.T, extra string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", strings.Replace(wiringFixture, "%s", extra, 1), 0)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	return fset, f
}

// Инъекция: читатель БЕЗ условия посадки — красное с координатой.
func TestOwnLaneGate_Injection_AReaderOutsideThePostureBranchIsNamed(t *testing.T) {
	fset, f := judgeWiringFixture(t, "\twho = who.WithHumanSession(ad)")
	sites := wiringSites(fset, f, "WithHumanSession")
	if len(sites) != 2 {
		t.Fatalf("мест %d, ожидалось 2", len(sites))
	}
	var bare []string
	for _, s := range sites {
		if s.posture != "Own" {
			bare = append(bare, s.pos)
		}
	}
	if len(bare) != 1 || !strings.HasPrefix(bare[0], "main.go:7:") {
		t.Fatalf("внесённый читатель без условия не назван координатой: %v", bare)
	}
}

// Близнец: читатель под названной посадкой — молчит; ветка `else` посадкой не
// считается.
func TestOwnLaneGate_Twin_AReaderUnderTheNamedPostureIsSilent(t *testing.T) {
	fset, f := judgeWiringFixture(t, "")
	for _, s := range wiringSites(fset, f, "WithHumanSession") {
		if s.posture != "Own" {
			t.Fatalf("законный читатель под own объявлен заведённым без посадки: %+v", s)
		}
	}
	// Читатель в ветке `else` посадки own — НЕ под own: «не own» есть
	// «external или не задано», и это не решение о посадке.
	fset, f = judgeWiringFixture(t, "\tif lane == identityposture.Own { _ = 1 } else { auth = auth.WithHumanSession(ad) }")
	sites := wiringSites(fset, f, "WithHumanSession")
	if sites[len(sites)-1].posture != "" {
		t.Fatalf("читатель в ветке else признан заведённым под посадкой: %+v", sites[len(sites)-1])
	}
}

// postureFixture — синтетика судьи посадки: одно тело функции, в нём одно
// место провязки читателя под одной формой ветвления.
const postureFixture = `package main
import "github.com/PRO-Robotech/corelib/identityposture"
func wire(lane identityposture.Provider) {
%s
}
`

// Судья посадки видит решение own в `if` И в ветке `case`, а замыкание между
// решением и местом провязки решение снимает: где его позовут, судья не
// прослеживает. Условие обязано УТВЕРЖДАТЬ own, а не называть: отрицание и
// дизъюнкция own не утверждают. Каждая строка — один факт против близнеца
// `if lane == identityposture.Own { … }`.
func TestOwnLaneGate_PostureIsSeenInEveryBranchForm(t *testing.T) {
	const call = "auth = auth.WithHumanSession(ad)"
	judged, underOwn := 0, 0
	defer func() {
		t.Logf("перепись: форм судимо %d · под own %d · вне own %d", judged, underOwn, judged-underOwn)
	}()
	for _, tc := range []struct {
		name, body, posture, why string
	}{
		{"близнец: ветка if", "if lane == identityposture.Own {\n" + call + "\n}", "Own", ""},
		{"близнец: own справа", "if identityposture.Own == lane {\n" + call + "\n}", "Own", ""},
		{"близнец: конъюнкция", "if lane == identityposture.Own && ours != nil {\n" + call + "\n}", "Own", ""},
		{"близнец: ветка case у switch по посадке", "switch lane {\ncase identityposture.Own:\n" + call + "\n}", "Own", ""},
		{"близнец: ветка case у switch без тега", "switch {\ncase lane == identityposture.Own:\n" + call + "\n}", "Own", ""},
		{"близнец: замыкание охватывает решение", "wireOwn := func() {\nif lane == identityposture.Own {\n" + call + "\n}\n}\n_ = wireOwn", "Own", ""},
		{"замыкание между решением и местом", "if lane == identityposture.Own {\nwireReader = func() {\n" + call + "\n}\n}", "", "*ast.FuncLit"},
		{"отрицание own", "if lane != identityposture.Own {\n" + call + "\n}", "", "ни одна ветка"},
		{"отрицание own унарным !", "if !(lane == identityposture.Own) {\n" + call + "\n}", "", "ни одна ветка"},
		{"дизъюнкция с own", "if lane == identityposture.Own || legacy {\n" + call + "\n}", "", "ни одна ветка"},
		{"ветка else", "if lane == identityposture.Own {\n} else {\n" + call + "\n}", "", "ни одна ветка"},
		{"ветка default", "switch lane {\ncase identityposture.Own:\ndefault:\n" + call + "\n}", "", "ни одна ветка"},
		{"ветка case на два значения", "switch lane {\ncase identityposture.Own, other:\n" + call + "\n}", "", "ни одна ветка"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "main.go", strings.Replace(postureFixture, "%s", tc.body, 1), 0)
			if err != nil {
				t.Fatalf("синтетика не разбирается: %v", err)
			}
			sites := wiringSites(fset, f, "WithHumanSession")
			if len(sites) != 1 {
				t.Fatalf("мест %d, ожидалось 1", len(sites))
			}
			s := sites[0]
			judged++
			if s.posture == "Own" {
				underOwn++
			}
			if s.posture != tc.posture {
				t.Fatalf("посадка места %s — %q, ожидалась %q (%s)", s.pos, s.posture, tc.posture, s.why)
			}
			if tc.posture == "" && (!strings.Contains(s.why, tc.why) || !strings.Contains(s.why, s.pos)) {
				t.Fatalf("находка не называет %q и координату %s: %s", tc.why, s.pos, s.why)
			}
			if tc.posture != "" && s.why != "" {
				t.Fatalf("под own находки быть не должно: %s", s.why)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Путь к стражу целей — инъекция в обе стороны на синтетике.

const guardPathRoot = `package main
import "github.com/PRO-Robotech/corelib/identityposture"
func main() {
	if lane == identityposture.Own {
		a, _, _ := prepareRelayTarget(cfg, middleware.RelayTargetForm, cfg.LoginLaneURL)
		b, _, _ := prepareRelayTarget(cfg, middleware.RelayTargetIssuance, cfg.IAMIssuanceURL)
%s
	}
%s
}
`

const guardPathHelper = `package main
func prepareRelayTarget(cfg config.Config, serves middleware.RelayTarget, raw string) (int, int, error) {
	if err := validateLoginLaneConfig(LoginLaneConfig{URL: raw}); err != nil {
		return 0, 0, err
	}
	return 0, 0, nil
}
%s
`

func judgeGuardPathFixture(t *testing.T, inside, outside, helper string) relayGuardPath {
	t.Helper()
	fset := token.NewFileSet()
	root, err := parser.ParseFile(fset, "main.go", strings.Replace(strings.Replace(guardPathRoot, "%s", inside, 1), "%s", outside, 1), 0)
	if err != nil {
		t.Fatalf("синтетика корня не разбирается: %v", err)
	}
	h, err := parser.ParseFile(fset, "login_lane_transport.go", strings.Replace(guardPathHelper, "%s", helper, 1), 0)
	if err != nil {
		t.Fatalf("синтетика помощника не разбирается: %v", err)
	}
	return judgeRelayGuardPath(fset, root, []*ast.File{root, h}, middleware.RelayTargets())
}

func TestRelayGuardPath_Twin_EachTargetPreparedOnceUnderOwnIsSilent(t *testing.T) {
	got := judgeGuardPathFixture(t, "", "", "")
	if len(got.findings) != 0 {
		t.Fatalf("законный путь к стражу дал находки: %v", got.findings)
	}
	if len(got.guards) != 1 || len(got.prepares) != 2 {
		t.Fatalf("перепись законного пути: стражей %d (ожидался 1), подготовок %d (ожидалось 2)", len(got.guards), len(got.prepares))
	}
}

// Прямой вызов стража в корне, пусть и под own, — находка с координатой:
// ровно та форма, которую прежний гейт считал законной при одной цели.
func TestRelayGuardPath_Injection_ADirectGuardCallIsNamed(t *testing.T) {
	got := judgeGuardPathFixture(t, "\t\t_ = validateLoginLaneConfig(LoginLaneConfig{})", "", "")
	if len(got.findings) != 1 || !strings.HasPrefix(got.findings[0], "main.go:7:") || !strings.Contains(got.findings[0], "мимо prepareRelayTarget") {
		t.Fatalf("прямой вызов стража не назван координатой: %v", got.findings)
	}
}

func TestRelayGuardPath_Injection_APreparationOutsideOwnIsNamed(t *testing.T) {
	got := judgeGuardPathFixture(t, "", "\tc, _, _ := prepareRelayTarget(cfg, middleware.RelayTargetForm, cfg.Other)", "")
	var outside, extra bool
	for _, f := range got.findings {
		outside = outside || (strings.HasPrefix(f, "main.go:9:") && strings.Contains(f, "вне ветки посадки own"))
		extra = extra || strings.Contains(f, "лишняя подготовка цели RelayTargetForm")
	}
	if !outside || !extra || len(got.findings) != 2 {
		t.Fatalf("подготовка вне own не названа координатой: %v", got.findings)
	}
}

func TestRelayGuardPath_Injection_AMissingTargetAndAHelperCallerAreFound(t *testing.T) {
	src := strings.Replace(guardPathRoot, "\t\tb, _, _ := prepareRelayTarget(cfg, middleware.RelayTargetIssuance, cfg.IAMIssuanceURL)\n", "", 1)
	fset := token.NewFileSet()
	root, err := parser.ParseFile(fset, "main.go", strings.Replace(strings.Replace(src, "%s", "", 1), "%s", "", 1), 0)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	h, err := parser.ParseFile(fset, "login_lane_transport.go", strings.Replace(guardPathHelper, "%s",
		"func other() { _, _, _ = prepareRelayTarget(cfg, middleware.RelayTargetIssuance, x) }", 1), 0)
	if err != nil {
		t.Fatalf("синтетика помощника не разбирается: %v", err)
	}
	got := judgeRelayGuardPath(fset, root, []*ast.File{root, h}, middleware.RelayTargets())
	var missing, bypass bool
	for _, f := range got.findings {
		missing = missing || strings.Contains(f, "цель RelayTargetIssuance не подготовлена корнем")
		bypass = bypass || (strings.HasPrefix(f, "login_lane_transport.go:8:") && strings.Contains(f, "мимо корня"))
	}
	if !missing || !bypass {
		t.Fatalf("недостающая цель либо вызов подготовки мимо корня не найдены: %v", got.findings)
	}
}

func TestRelayGuardPath_Injection_NoGuardAndNoFilesAreNotSilent(t *testing.T) {
	got := judgeRelayGuardPath(token.NewFileSet(), nil, nil, middleware.RelayTargets())
	if len(got.findings) != 1 || !strings.Contains(got.findings[0], "файлов пакета не прочитано") {
		t.Fatalf("пустой обход пакета молчит: %v", got.findings)
	}
	fset := token.NewFileSet()
	root, _ := parser.ParseFile(fset, "main.go", strings.Replace(strings.Replace(guardPathRoot, "%s", "", 1), "%s", "", 1), 0)
	h, _ := parser.ParseFile(fset, "login_lane_transport.go", "package main\nfunc prepareRelayTarget() {}\n", 0)
	got = judgeRelayGuardPath(fset, root, []*ast.File{root, h}, middleware.RelayTargets())
	var none bool
	for _, f := range got.findings {
		none = none || strings.Contains(f, "страж цели не позван ни разу")
	}
	if !none {
		t.Fatalf("снятый вызов стража не найден: %v", got.findings)
	}
}
