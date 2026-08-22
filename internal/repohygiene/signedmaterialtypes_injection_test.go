// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// signedmaterialtypes_injection_test.go — доказательство того, что
// TestSignedMaterialTypesAreDeclaredOnceAndDistinct СПОСОБЕН упасть, и падает он
// на существе.
//
// Инъекция подаёт синтетику в ТЕ ЖЕ функции, что судят дерево:
// signedTypesDistinctnessDefects — правила различности, ScanDeclaredStringValues
// — разбор объявлений.
//
// Инъекция здесь не украшение: значения приходят из политики и в пробе не
// мутируются, поэтому «зелено» о способности гейта упасть не говорит ничего.
package repohygiene

import (
	"strings"
	"testing"
)

// TestSignedTypesRulesCatchACollision — сторона (а): совпадение двух значений
// становится находкой, и находка называет координату.
func TestSignedTypesRulesCatchACollision(t *testing.T) {
	defects := signedTypesDistinctnessDefects([]string{"at+jwt", "at+jwt"})
	if len(defects) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(defects), defects)
	}
	if !strings.Contains(defects[0], signedTypesOwnerFile) {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %q", defects[0])
	}
	if !strings.Contains(defects[0], "at+jwt") {
		t.Errorf("находка не называет совпавшее значение: %q", defects[0])
	}

	// Пустой тип — тот же класс с другой стороны: «тип не назван» не является
	// значением типа, и проверяющий, требующий своего типа явно, принял бы
	// отсутствие.
	empty := signedTypesDistinctnessDefects([]string{"at+jwt", ""})
	if len(empty) != 1 {
		t.Fatalf("на пустом типе находок %d, ожидалась 1: %v", len(empty), empty)
	}
	if !strings.Contains(empty[0], "ПУСТЫМ") {
		t.Errorf("находка не называет, чем тип негоден: %q", empty[0])
	}

	// Совпадение ТРЁХ значений обязано дать три пары, а не одну: гейт,
	// останавливающийся на первой, объявил бы остальные разрешёнными.
	three := signedTypesDistinctnessDefects([]string{"x", "x", "x"})
	if len(three) != 3 {
		t.Fatalf("на трёх совпавших значениях находок %d, ожидалось 3 пары: %v", len(three), three)
	}
}

// TestSignedTypesRulesAreSilentOnDistinctValues — сторона (б): два разных
// значения, объявленных в том же месте, находкой не являются.
func TestSignedTypesRulesAreSilentOnDistinctValues(t *testing.T) {
	defects := signedTypesDistinctnessDefects([]string{"at+jwt", "client-authentication+jwt"})
	if len(defects) != 0 {
		t.Fatalf("законные значения объявлены находкой (%v) — гейт краснел бы на исправной "+
			"политике", defects)
	}
	// И на большем числе видов тоже: правило судит ПАРЫ, а не длину перечня.
	more := signedTypesDistinctnessDefects([]string{"at+jwt", "client-authentication+jwt", "dpop+jwt"})
	if len(more) != 0 {
		t.Fatalf("три разных значения объявлены находкой (%v) — тогда красное приходит от "+
			"ЧИСЛА видов, а не от их совпадения", more)
	}
}

// signedTypesInjectedSecondDeclaration — ВТОРОЕ объявление значения константой,
// рядом с законными соседями.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - `hdr.Typ != "at+jwt"` — УПОТРЕБЛЕНИЕ значения: сравнение вторым его
//     объявлением не является, и гейт, считающий литералы, запретил бы величину
//     употреблять;
//   - `otherType = "dpop+jwt"` — объявление ЧУЖОГО значения: к видам
//     подписанного этой политики отношения не имеет;
//   - `builtType = "at" + "+jwt"` — значение, собранное из частей: слепая зона,
//     названная в шапке разбора.
const signedTypesInjectedSecondDeclaration = `package registrytokenwire

const registryTokenType = "at+jwt"

const otherType = "dpop+jwt"

var builtType = "at" + "+jwt"

func check(hdr header) bool {
	return hdr.Typ != "at+jwt"
}
`

// TestSignedTypesScannerFindsASecondDeclaration — разбор отличает объявление от
// употребления.
func TestSignedTypesScannerFindsASecondDeclaration(t *testing.T) {
	values := []string{"at+jwt", "client-authentication+jwt"}
	decls, census, err := ScanDeclaredStringValues(
		"synthetic/registrytokenwire/minter.go", []byte(signedTypesInjectedSecondDeclaration), values)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.ValueSpecs == 0 {
		t.Fatalf("осмотрено ноль спецификаций — разбирается не то дерево")
	}
	if len(decls) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1: %+v\n\nЛибо употребление спутано с "+
			"объявлением — тогда гейт запрещает величину употреблять; либо чужое значение "+
			"принято за наше.", len(decls), decls)
	}
	d := decls[0]
	if d.File == "" || d.Line == 0 {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", d)
	}
	if d.Name != "registryTokenType" || d.Value != "at+jwt" || d.Kind != "const" {
		t.Errorf("находка описана неверно: %+v", d)
	}

	// Слепая зона названа, а не спрятана: значение, собранное из частей, разбор
	// не видит. Проба держит утверждение шапки честным.
	for _, x := range decls {
		if x.Name == "builtType" {
			t.Fatalf("значение, собранное из частей, признано объявлением (%+v) — разбор "+
				"вырос, и шапка signedmaterialtypes.go, объявляющая это слепой зоной, стала "+
				"ложной. Правь шапку вместе с разбором.", x)
		}
	}
}

// TestSignedTypeDebtRulesCatchABareEntry — правила ведомости обязаны краснеть на
// голой записи и МОЛЧАТЬ на полной.
func TestSignedTypeDebtRulesCatchABareEntry(t *testing.T) {
	lawful := signedTypeDebtEntry{
		File:  "services/x/config.go",
		Name:  "TokenTypePlatform",
		Why:   "ожидаемый тип на принимающей стороне",
		Until: "файл читает значение из pkg/tokenpolicy",
	}
	if bad := signedTypeDebtDefects([]signedTypeDebtEntry{lawful}); len(bad) != 0 {
		t.Fatalf("полная запись объявлена дефектной: %v", bad)
	}

	noWhy := lawful
	noWhy.Why = "  "
	noUntil := lawful
	noUntil.Until = ""
	noCoord := lawful
	noCoord.File = ""

	for name, entry := range map[string]signedTypeDebtEntry{
		"без обоснования": noWhy,
		"без предиката":   noUntil,
		"без координаты":  noCoord,
	} {
		if bad := signedTypeDebtDefects([]signedTypeDebtEntry{entry}); len(bad) == 0 {
			t.Errorf("запись %s принята правилами ведомости — тогда ведомость перестаёт "+
				"отличаться от списка прощённых", name)
		}
	}

	if bad := signedTypeDebtDefects([]signedTypeDebtEntry{lawful, lawful}); len(bad) == 0 {
		t.Error("дубль в ведомости не найден — записей на одну меньше, чем кажется")
	}
}

// TestSignedTypeDebtStaleRuleCatchesAnEntryWithoutSubject — запись, которой
// больше нечего исключать, роняет прогон.
func TestSignedTypeDebtStaleRuleCatchesAnEntryWithoutSubject(t *testing.T) {
	entries := []signedTypeDebtEntry{
		{File: "a.go", Name: "A", Why: "w", Until: "u"},
		{File: "b.go", Name: "B", Why: "w", Until: "u"},
	}
	live := map[string]bool{"a.go\x00A": true}
	stale := signedTypeDebtStale(entries, live)
	if len(stale) != 1 {
		t.Fatalf("просроченных записей %d, ожидалась 1: %v", len(stale), stale)
	}
	if !strings.Contains(stale[0], "b.go") {
		t.Fatalf("названа не та запись: %q", stale[0])
	}
	// Законный близнец: запись, которой ещё есть что исключать, молчит.
	if got := signedTypeDebtStale(entries[:1], live); len(got) != 0 {
		t.Fatalf("живая запись объявлена просроченной: %v", got)
	}
	// И пустая ведомость молчит: пустота есть цель, а не поломка.
	if got := signedTypeDebtStale(nil, live); len(got) != 0 {
		t.Fatalf("пустая ведомость объявлена просроченной: %v", got)
	}
}
