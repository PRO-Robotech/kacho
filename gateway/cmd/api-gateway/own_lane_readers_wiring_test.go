// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_readers_wiring_test.go — декларативный гейт по композиционному
// корню: РЕТРАНСЛЯЦИЯ ГЛАГОЛОВ ФОРМЫ заводится посадкой и только ею.
//
// # Что этот файл судил раньше и почему больше не судит
//
// Он требовал, чтобы ЧИТАТЕЛЬ НОСИТЕЛЯ стоял внутри ветки, называющей посадку
// (Ф3-12, Ф3-45). Требование было верным ровно до тех пор, пока переход между
// источниками личности считался мгновенным: у посадки два взаимоисключающих
// значения, и состояния «наш носитель читается, и чужой ЕЩЁ читается» в такой
// форме не существует. Перевод стенда с людьми означал бы мгновенную потерю
// входа у каждого, чья чужая сессия жива.
//
// Предмет СНЯТ ВМЕСТЕ СО СВОИМ ТРЕБОВАНИЕМ, одним изменением: читателей теперь
// заводит множество (`config.SessionCarrierSet`), и судит их
// `session_carrier_wiring_test.go` — тем же разбором, с переписью и инъекцией в
// обе стороны. Оставить здесь прежние утверждения значило бы держать в дереве
// два правила об одном предмете, из которых верно одно.
//
// # Что осталось и почему это ДРУГОЙ предмет
//
// Ретрансляция глаголов формы входа — предмет ПОСАДКИ, а не множества. Форма
// входа принадлежит той чеканке, которая личность ВЫДАЁТ; множество говорит
// лишь о том, чьё печенье край ещё согласен прочитать. Под `external` глаголы
// формы обязаны отвечать 404 краем, и это требование пережило смену предмета
// соседей нетронутым.
//
// Распознаватель посадки (`postureBranchOf`) тоже остаётся: его зовёт и новый
// гейт — чтобы НАЗВАТЬ в тексте находки ветку посадки, в которой читатель
// оказался по ошибке.
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

// TestOwnLane_F3_45_TheLoginLaneRelayIsWiredUnderTheOwnPostureOnly — ровно одна
// ретрансляция, и она под `own`.
func TestOwnLane_F3_45_TheLoginLaneRelayIsWiredUnderTheOwnPostureOnly(t *testing.T) {
	fset, f := parseMain(t)
	relays := wiringSites(fset, f, "NewLoginLaneRelay")
	if len(relays) != 1 {
		t.Fatalf("ретрансляция глаголов формы заведена %d раз, ожидалось 1", len(relays))
	}
	if relays[0].posture != "Own" {
		t.Errorf("ретрансляция заведена вне ветки посадки own: %s (ветка: %q) — под external глаголы "+
			"формы обязаны отвечать 404", relays[0].pos, relays[0].posture)
	}
	// Читателей носителя этот гейт НЕ судит: их предмет переехал вместе со
	// сменой устройства перехода. Перепись называет обе величины, чтобы «1»
	// не читалось как «осмотрено одно место из трёх».
	t.Logf("перепись: ретрансляций %d (под own %d) · читателей носителя здесь не судится — "+
		"их судит session_carrier_wiring_test.go", len(relays), 1)
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

// Распознаватель ветки посадки, инъекция: читатель поставщика вне ветки `if`,
// называющей посадку, получает ПУСТУЮ посадку и называется координатой. Вердикта
// о читателе здесь нет: его выносит session_carrier_wiring_test.go по решению
// множества, а посадку этот распознаватель лишь называет в тексте находки.
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

// Близнец распознавателя: читатель в ветке `lane == identityposture.External`
// распознаётся под этой посадкой; ветка `else` посадкой не считается.
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
