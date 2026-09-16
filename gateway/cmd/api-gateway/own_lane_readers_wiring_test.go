// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_readers_wiring_test.go — декларативные гейты по композиционному
// корню (приёмка Ф3, Ф3-12 и Ф3-45; гейт Ф1 §8).
//
// # Предмет
//
// Читателя носителя выбирает ПОСАДКА (`cfg.ResolvedIdentityProvider()`): под
// `own` — наша сессия, под `external` — сессия поставщика. Три места
// композиционного корня — полоса личности, отзыв на ней, маршрут «кто я» —
// заводились сегодня НАЛИЧИЕМ АДРЕСА поставщика (`kratosURL != "disabled"`), а
// не посадкой (§1.1 приёмки): под `own` читатель носителя поставщика оставался
// заведённым, и печенье поставщика продолжало становиться личностью.
//
// # Что судится — вложенность узлов, не текст
//
//   - читатель носителя ПОСТАВЩИКА — вызов `NewKratosClient`: заведён под
//     `own`, если не лежит внутри ветки `if`, чьё условие называет посадку
//     `external`. Требуется «мест N · заведено под own 0», N ≥ 1 (положительный
//     контроль: под `external` поставщик по-прежнему читается).
//     ЕДИНИЦА СЧЁТА НАЗВАНА, потому что она не та, что у приёмки: здесь «место»
//     — ВЫЗОВ КОНСТРУКТОРА читателя (их 2: полоса личности и маршрут «кто я»),
//     а «три места §1.1» приёмки — ПОТРЕБИТЕЛИ читателя: полоса личности, отзыв
//     на ней и «кто я». Первые два стоят за ОДНИМ конструктором — отзыв на
//     полосе читает тот же клиент, что и полоса, — поэтому 2 вызова обслуживают
//     три места, и перепись печатает обе величины, чтобы «2» не читалось как
//     «одно из трёх мест не осмотрено»;
//   - читатель НАШЕЙ сессии — вызов `WithHumanSession`: заведён под `external`,
//     если не лежит в ветке с посадкой `own`. N ≥ 2 (полоса и «кто я»);
//   - ретрансляция — вызов `NewLoginLaneRelay`: ровно 1, под `own`.
//
// Инъекция в обе стороны на синтетике: читатель без условия посадки — красное с
// координатой; читатель под `external` — молчит.
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

// TestOwnLane_F3_12_NoProviderCarrierReaderIsWiredUnderOwn — «мест N · заведено
// под own M», M = 0.
func TestOwnLane_F3_12_NoProviderCarrierReaderIsWiredUnderOwn(t *testing.T) {
	fset, f := parseMain(t)
	sites := wiringSites(fset, f, "NewKratosClient")
	if len(sites) == 0 {
		t.Fatal("читатель носителя поставщика не провязывается вовсе — под external посадка осталась бы без сессии, и молчание гейта ничего не утверждает")
	}
	underOwn := 0
	for _, s := range sites {
		if s.posture != "External" {
			underOwn++
			t.Errorf("читатель носителя поставщика заведён без условия посадки external: %s (ветка посадки: %q). "+
				"Под own печенье поставщика становилось бы личностью (Ф1-52).", s.pos, s.posture)
		}
	}
	t.Logf("перепись: мест (вызовов конструктора читателя поставщика) %d · заведено под own %d · "+
		"потребителей читателя по §1.1 приёмки 3 (полоса личности и отзыв на ней — за первым вызовом, «кто я» — за вторым)",
		len(sites), underOwn)
	if len(sites) != 2 {
		t.Errorf("вызовов конструктора %d, ожидалось 2: третий потребитель §1.1 (отзыв на полосе) читает клиент полосы, "+
			"и свой конструктор ему не полагается — новый вызов означает новый читатель носителя поставщика, чьё место в перечне не названо",
			len(sites))
	}
}

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
		t.Fatalf("ретрансляция четырёх глаголов заведена %d раз, ожидалось 1", len(relays))
	}
	if relays[0].posture != "Own" {
		t.Errorf("ретрансляция заведена вне ветки посадки own: %s (ветка: %q) — под external четыре глагола обязаны отвечать 404", relays[0].pos, relays[0].posture)
	}
	t.Logf("перепись: читателей нашей сессии %d (под own %d) · ретрансляций %d", len(readers), len(readers), len(relays))
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика.

const wiringFixture = `package main
import "github.com/PRO-Robotech/corelib/identityposture"
func wire(lane identityposture.Provider) {
	if lane == identityposture.External {
		auth = auth.WithKratos(middleware.NewKratosClient(url))
	}
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

// Инъекция: читатель поставщика БЕЗ условия посадки — красное с координатой.
func TestOwnLaneGate_Injection_AProviderReaderOutsideThePostureBranchIsNamed(t *testing.T) {
	fset, f := judgeWiringFixture(t, "\twho = who.WithKratos(middleware.NewKratosClient(url), lookup)")
	sites := wiringSites(fset, f, "NewKratosClient")
	if len(sites) != 2 {
		t.Fatalf("мест %d, ожидалось 2", len(sites))
	}
	var bare []string
	for _, s := range sites {
		if s.posture != "External" {
			bare = append(bare, s.pos)
		}
	}
	if len(bare) != 1 || !strings.HasPrefix(bare[0], "main.go:10:") {
		t.Fatalf("внесённый читатель без условия не назван координатой: %v", bare)
	}
}

// Близнец: читатель под `external` — молчит; ветка `else` посадкой не считается.
func TestOwnLaneGate_Twin_AProviderReaderUnderExternalIsSilent(t *testing.T) {
	fset, f := judgeWiringFixture(t, "")
	for _, s := range wiringSites(fset, f, "NewKratosClient") {
		if s.posture != "External" {
			t.Fatalf("законный читатель под external объявлен заведённым без посадки: %+v", s)
		}
	}
	// Читатель в ветке `else` посадки own — НЕ под external: «не own» есть
	// «external или не задано», и это не решение о посадке.
	fset, f = judgeWiringFixture(t, "\tif lane == identityposture.Own { _ = 1 } else { auth = auth.WithKratos(middleware.NewKratosClient(url)) }")
	sites := wiringSites(fset, f, "NewKratosClient")
	if sites[len(sites)-1].posture != "" {
		t.Fatalf("читатель в ветке else признан заведённым под посадкой: %+v", sites[len(sites)-1])
	}
}
