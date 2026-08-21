// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_declared_checks_test.go — Ф1б-01: край ОБЪЯВЛЯЕТ состав проверок токена и
// объявление сходится с единственным перечнем (`pkg/tokenpolicy`).
//
// Пока состав живёт у каждой поверхности свой, различие между поверхностями НЕ
// ВЫРАЖЕНО и потому не может покраснеть ни у одной: одна перестанет требовать
// срок, другая тип, и спросить об этом будет нечего.
package middleware

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

func TestF1b01_EdgeDeclaresTheMandatoryCheckComposition(t *testing.T) {
	v := newTwoIssuerVerifierForDeclaration(t)

	declared := v.DeclaredChecks()
	if len(declared) == 0 {
		t.Fatalf("край не объявил ни одной проверки — сверять состав не с чем, "+
			"и молчание об этом не является утверждением (перечень обязательных: %v)",
			tokenpolicy.MandatoryChecks())
	}
	if missing := tokenpolicy.MissingChecks(declared); len(missing) > 0 {
		t.Fatalf("край объявляет состав, но в нём НЕ ХВАТАЕТ обязательных проверок — %d: %v\n"+
			"Объявлено: %v\n"+
			"Каждая недостающая — признак, который край не спрашивает у предъявителя. "+
			"На положительном пути её отсутствие невидимо.",
			len(missing), missing, declared)
	}
}

// TestF1b01_RevocationIsDeclaredOnlyWhenARecordReadsIt — объявление правдиво ПО
// ПОСТРОЕНИЮ, а не по добросовестности: отзыв попадает в состав тогда и только
// тогда, когда хотя бы одна запись издателя его включила.
func TestF1b01_RevocationIsDeclaredOnlyWhenARecordReadsIt(t *testing.T) {
	withRevocation := newTwoIssuerVerifierForDeclaration(t)
	if !containsCheck(withRevocation.DeclaredChecks(), tokenpolicy.CheckRevocation) {
		t.Fatalf("запись объявила чтение отзыва, но состав его не называет — "+
			"объявление разошлось с построением; объявлено: %v", withRevocation.DeclaredChecks())
	}

	legacyOnly := newLegacyOnlyVerifierForDeclaration(t)
	if containsCheck(legacyOnly.DeclaredChecks(), tokenpolicy.CheckRevocation) {
		t.Fatalf("ни одна запись не читает отзыв, а состав его называет — это контроль, " +
			"объявленный без читателя: он не отказал бы ни разу за свою жизнь")
	}
}

func containsCheck(list []tokenpolicy.Check, want tokenpolicy.Check) bool {
	for _, c := range list {
		if c == want {
			return true
		}
	}
	return false
}
