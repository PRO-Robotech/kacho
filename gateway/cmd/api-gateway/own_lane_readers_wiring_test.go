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
// # ЧИТАТЕЛЬ ОСТАЛСЯ ОДИН, И ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА СНЯТА С ПРЕДМЕТОМ
//
// Чужой поставщик снят целиком: его читателя в дереве нет ни одного. Случай
// «читатель поставщика не заведён под own» вместе с ним стал БЕСПРЕДМЕТНЫМ —
// не выполненным, а неизмеримым: его перепись обязана была требовать N ≥ 1
// мест, и на нуле она честно краснела «молчание гейта ничего не утверждает».
// Оставить его значило бы либо держать красное о снятом предмете, либо снять
// проверку предпосылки — то есть получить гейт, зелёный на пустом обходе.
//
// Требование «у каждого читателя есть ветка посадки» исполняется тем, что
// читатель остался один и он наш, а его ветка посадки проверяется ниже.
//
// # Что судится — вложенность узлов, не текст
//
//   - читатель НАШЕЙ сессии — вызов `WithHumanSession`: обязан лежать внутри
//     ветки `if`, чьё условие называет посадку `own`. N ≥ 2 (полоса личности и
//     маршрут «кто я»);
//   - ретрансляция — вызов `NewLoginLaneRelay`: ровно 1, под `own`.
//
// Инъекция в обе стороны на синтетике: читатель без условия посадки — красное с
// координатой; читатель под названной посадкой — молчит. Синтетика намеренно
// НЕ опирается на живой корень: опирайся она на него, доказательство исчезало
// бы вместе с каждой правкой провязки.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// postureBranchOf — самая внутренняя ветка `if`, охватывающая позицию, чьё
// условие называет посадку `identityposture.<Name>`; "" если такой ветки нет.
func postureBranchOf(f *ast.File, pos token.Pos) string {
	posture := ""
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if pos <= ifs.Body.Lbrace || pos >= ifs.Body.Rbrace {
			// Ветка else не считается веткой посадки: else «не own» есть
			// «external или не задано», и это не решение.
			return true
		}
		if p := postureNamedIn(ifs.Cond); p != "" {
			posture = p
		}
		return true
	})
	return posture
}

// postureNamedIn — какую посадку называет условие: селектор
// `identityposture.Own` / `identityposture.External`, единственный законный
// способ назвать её в дереве (`corelib/identityposture`).
func postureNamedIn(cond ast.Expr) string {
	found := ""
	ast.Inspect(cond, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "identityposture" {
			switch sel.Sel.Name {
			case "Own", "External":
				found = sel.Sel.Name
			}
		}
		return true
	})
	return found
}

// wiringSite — одно место провязки читателя.
type wiringSite struct {
	pos     string
	posture string // "Own" | "External" | ""
}

// wiringSites — места вызова названного метода/функции и посадка каждого.
func wiringSites(fset *token.FileSet, f *ast.File, callee string) []wiringSite {
	var out []wiringSite
	for _, pos := range f1bFindCall(f, callee) {
		out = append(out, wiringSite{pos: fset.Position(pos).String(), posture: postureBranchOf(f, pos)})
	}
	return out
}

func parseMain(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("композиционный корень не разбирается: %v", err)
	}
	return fset, f
}

// ЗДЕСЬ СТОЯЛ TestOwnLane_F3_12_NoProviderCarrierReaderIsWiredUnderOwn —
// перепись «мест N · заведено под own M», M = 0, по вызовам конструктора
// читателя носителя ЧУЖОГО поставщика. Случай снят вместе со своим предметом:
// таких вызовов в дереве ноль, и его собственная проверка предпосылки это и
// сказала — «читатель не провязывается вовсе, молчание гейта ничего не
// утверждает». Единственные исходы у такого случая — снять с предметом либо
// снять проверку предпосылки; второе дало бы гейт, зелёный на пустом обходе.

// TestOwnLane_F3_45_OurReaderAndTheRelayAreWiredUnderOwnOnly — наш читатель
// (полоса и «кто я») и ретрансляция заведены под `own` и не заведены под
// `external`.
func TestOwnLane_F3_45_OurReaderAndTheRelayAreWiredUnderOwnOnly(t *testing.T) {
	fset, f := parseMain(t)
	readers := wiringSites(fset, f, "WithHumanSession")
	if len(readers) < 2 {
		t.Fatalf("читателей нашей сессии провязано %d, ожидалось не меньше 2 (полоса личности и «кто я»)", len(readers))
	}
	for _, s := range readers {
		if s.posture != "Own" {
			t.Errorf("читатель нашей сессии заведён вне ветки посадки own: %s (ветка: %q)", s.pos, s.posture)
		}
	}
	relays := wiringSites(fset, f, "NewLoginLaneRelay")
	if len(relays) != 1 {
		t.Fatalf("ретрансляция глаголов формы заведена %d раз, ожидалось 1", len(relays))
	}
	if relays[0].posture != "Own" {
		t.Errorf("ретрансляция заведена вне ветки посадки own: %s (ветка: %q) — под external глаголы формы обязаны отвечать 404", relays[0].pos, relays[0].posture)
	}
	t.Logf("перепись: читателей нашей сессии %d (под own %d) · ретрансляций %d", len(readers), len(readers), len(relays))
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика.

// wiringFixture — СИНТЕТИКА, а не живой корень. Предмет инъекции — предикат
// «лежит ли вызов внутри ветки названной посадки», и опирайся он на дерево,
// доказательство менялось бы вместе с каждой правкой провязки — и исчезло бы
// ровно тогда, когда предикат достиг цели.
//
// Вызов в фикстуре назван ЖИВЫМ именем (`WithHumanSession`): прежняя редакция
// изображала здесь читателя чужого поставщика, снятого из дерева, и синтетика,
// именующая снятое, — утверждение, пережившее свой предмет.
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
	// Читатель в ветке `else` посадки own — НЕ под own: «не own» есть «external
	// или не задано», и это не решение о посадке.
	fset, f = judgeWiringFixture(t, "\tif lane == identityposture.Own { _ = 1 } else { auth = auth.WithHumanSession(ad) }")
	sites := wiringSites(fset, f, "WithHumanSession")
	if sites[len(sites)-1].posture != "" {
		t.Fatalf("читатель в ветке else признан заведённым под посадкой: %+v", sites[len(sites)-1])
	}
}
