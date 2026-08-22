// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenpolicyquantity_injection_test.go — доказательство того, что
// TestTokenPolicyQuantityIsDeclaredOnce СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanDurationDeclarations), что и гейт.
//
// Вторая сторона пары обязательна и без неё гейт был бы вреден: величин у
// политики токенов много, и гейт, считающий «объявлений длительности больше
// одного», запретил бы политике объявлять вторую величину. Красное обязано
// приходить от ПОВТОРА ОДНОЙ величины, а не от числа объявлений.
package repohygiene

import (
	"testing"
	"time"
)

// quantityInjectedSecondSkew — ВТОРОЕ объявление допуска расхождения часов, и
// объявлено оно ДРУГИМ числом.
//
// Другое число здесь не для драматизма: два объявления одной величины
// расходятся не сразу, а при первой же правке одной стороны — и расходятся там,
// где расхождение не видно, потому что обе величины по отдельности выглядят
// разумными.
const quantityInjectedSecondSkew = `package clientassertion

import "time"

const (
	// allowedClockSkew — допуск расхождения часов этого проверяющего.
	allowedClockSkew = 30 * time.Second
	// maxAssertionLifetime — потолок длительности утверждения.
	maxAssertionLifetime = 10 * time.Minute
)
`

// quantityInjectedDistinctQuantities — законный близнец: РАЗНЫЕ величины, каждая
// объявлена по одному разу.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - `readTimeout` и `dialTimeout` — две длительности одного файла, к
//     стережённым величинам отношения не имеющие: гейт, считающий длительности,
//     объявил бы их находкой;
//   - `keyRemovalGrace` — величина, сложенная ТОЛЬКО из других имён: единицы
//     длительности она не называет вовсе, поэтому объявлением длительности не
//     является (слепая зона, названная в шапке разбора);
//   - `graceWithSlack` — та же величина, но с числом в слагаемых: единицу
//     называет, значит попадает в перепись — и попадает НЕРАЗОБРАННОЙ;
//   - `skewSeconds` — целое, а не длительность: числом величины оно не
//     объявляет.
const quantityInjectedDistinctQuantities = `package tokenpolicy

import "time"

const (
	ClockSkew            = 60 * time.Second
	MaxAssertionLifetime = 5 * time.Minute
	MaxTokenTTL          = 30 * time.Minute
	CacheCeiling         = time.Hour
	readTimeout          = 3 * time.Second
	dialTimeout          = 2 * time.Second
	keyRemovalGrace      = MaxTokenTTL + CacheCeiling
	graceWithSlack       = MaxTokenTTL + 15*time.Minute
	skewSeconds          = 60
)
`

// TestQuantityScannerFindsASecondDeclarationOfTheSameQuantity — сторона (а).
func TestQuantityScannerFindsASecondDeclarationOfTheSameQuantity(t *testing.T) {
	decls, census, err := ScanDurationDeclarations(
		"synthetic/clientassertion/policy.go", []byte(quantityInjectedSecondSkew))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.ValueSpecs == 0 {
		t.Fatalf("осмотрено ноль объявлений — разбирается не то дерево")
	}
	if len(decls) != 2 {
		t.Fatalf("объявлений длительности найдено %d, ожидалось 2: %+v", len(decls), decls)
	}

	for _, q := range tokenPolicyQuantities {
		var hit []DurationDeclaration
		for _, d := range decls {
			if q.Matches(d.Name) {
				hit = append(hit, d)
			}
		}
		if len(hit) != 1 {
			t.Fatalf("величина %q опознана в %d объявлениях синтетики, ожидалось 1: %+v",
				q.Name, len(hit), hit)
		}
		if hit[0].File == "" || hit[0].Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", hit[0])
		}
		if !hit[0].Resolved {
			t.Errorf("число величины %q не разобрано (%q) — тогда «объявлена числом» "+
				"нечем отличить от «объявлена выражением»", q.Name, hit[0].Expr)
		}
	}

	// Дефект внесён так, чтобы второе объявление НЕ СОВПАДАЛО с первым по
	// значению: расхождение обязано быть видно в тексте отказа.
	skew := decls[0]
	if skew.Nanos != int64(30*time.Second) {
		t.Fatalf("допуск синтетики разобран как %d нс, ожидалось %d — разбор считает не то",
			skew.Nanos, int64(30*time.Second))
	}
}

// TestQuantityScannerIsSilentOnDistinctQuantities — сторона (б): разные
// величины, объявленные каждая по одному разу, находкой не являются.
func TestQuantityScannerIsSilentOnDistinctQuantities(t *testing.T) {
	decls, census, err := ScanDurationDeclarations(
		"synthetic/tokenpolicy/policy.go", []byte(quantityInjectedDistinctQuantities))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.ValueSpecs < 8 {
		t.Fatalf("осмотрено объявлений %d — разбирается не то дерево", census.ValueSpecs)
	}

	for _, q := range tokenPolicyQuantities {
		var hit []DurationDeclaration
		for _, d := range decls {
			if q.Matches(d.Name) {
				hit = append(hit, d)
			}
		}
		if len(hit) != 1 {
			t.Fatalf("величина %q опознана в %d объявлениях законного близнеца, ожидалось 1 "+
				"(%+v).\n\nЛибо гейт краснеет на исправном месте, либо он не различает "+
				"величины между собой — и тогда его красное приходит от ЧИСЛА объявлений, "+
				"а не от повтора одной величины.", q.Name, len(hit), hit)
		}
	}

	// Целое, а не длительность, объявлением величины числом не является: иначе
	// вторым объявлением стала бы всякая настройка «в секундах».
	for _, d := range decls {
		if d.Name == "skewSeconds" {
			t.Fatalf("целое опознано объявлением длительности (%+v) — тогда настройка "+
				"«в секундах» становится вторым объявлением величины", d)
		}
	}

	// Величина, сложенная только из чужих имён, единицы длительности не
	// называет — объявлением длительности она не является, и это слепая зона,
	// названная в шапке разбора. Проба держит то утверждение честным: вырастет
	// разбор — здесь станет видно, что шапку пора править.
	for _, d := range decls {
		if d.Name == "keyRemovalGrace" {
			t.Fatalf("величина, сложенная только из чужих имён, признана объявлением "+
				"длительности (%+v) — разбор вырос, и шапка tokenpolicyquantity.go, "+
				"объявляющая это слепой зоной, стала ложной. Правь шапку вместе с разбором.", d)
		}
	}

	// Величина, у которой единица названа, но значение выводится из чужого
	// имени, в перепись попадает — и попадает НЕРАЗОБРАННОЙ: «объявлена числом»
	// обязано быть отличимо от «объявлена выражением».
	var grace *DurationDeclaration
	for i := range decls {
		if decls[i].Name == "graceWithSlack" {
			grace = &decls[i]
		}
	}
	if grace == nil {
		t.Fatalf("величина с названной единицей и выведенным слагаемым не попала в перепись " +
			"вовсе — тогда «объявлений длительности» насчитано меньше, чем есть, и перепись " +
			"занижает молчание")
	}
	if grace.Resolved {
		t.Fatalf("величина, выведенная из чужого имени (%q), разобрана как ЧИСЛО — тогда "+
			"смена слагаемого прошла бы мимо счётчика неразобранных", grace.Expr)
	}
	if census.Unresolved == 0 {
		t.Fatalf("счётчик неразобранных равен нулю при наличии выведенной величины — " +
			"молчание гейта нечем взвесить")
	}
}
