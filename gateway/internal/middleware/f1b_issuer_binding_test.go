// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_issuer_binding_test.go — Ф1б-06: развязка «издатель ↔ набор ключей» на
// крае, и Ф1б-03: издатель без объявленной записи даёт ОТКАЗ, а не перебор.
//
// Принять двух издателей, имея один набор ключей, значило бы разрешить ключу
// одного проверять токен другого — то есть отменить ровно ту защиту, ради
// которой развязка и делается. Контуров два и подписантов два, поэтому это не
// теория, а конструкция, которая получается сама, если её не запретить.
package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// f1bTwoIssuerVerifier — проверяющий с двумя РАЗНЫМИ наборами ключей.
func f1bTwoIssuerVerifier(t *testing.T) (*JWTVerifier, *f1bKeySet, *f1bKeySet) {
	t.Helper()
	ours, legacy := newF1bKeySet(t), newF1bKeySet(t)
	ours.addEC("ours-1")
	legacy.addRSA("legacy-1")
	v, err := NewJWTVerifier(JWTVerifierConfig{
		Issuers: []IssuerKeySet{
			{Issuer: f1bPlatformIssuer, KeySetURL: ours.URL(), TokenTypes: []string{PlatformTokenType}},
			{Issuer: f1bLegacyIssuer, KeySetURL: legacy.URL(), TokenTypes: []string{LegacyTokenType}, TolerateAbsentTokenType: true},
		},
		ExpectedAudience: f1bAudience,
	})
	if err != nil {
		t.Fatalf("построение проверяющего: %v", err)
	}
	return v, ours, legacy
}

func TestF1b06_KeyOfOneIssuerDoesNotVerifyATokenClaimingTheOther(t *testing.T) {
	v, ours, legacy := f1bTwoIssuerVerifier(t)
	now := time.Now()

	// Подписан НАШИМ ключом, объявляет ПРЕЖНЕГО издателя.
	//
	// Тип берётся ПРИЕМЛЕМЫЙ для полосы прежнего издателя намеренно: с чужим для
	// неё типом отказ приходил бы от сверки типа, и эта половина оставалась бы
	// зелёной при полностью сломанном выборе записи. Утверждается поэтому не
	// «хоть какой-нибудь отказ», а что отказ пришёл НЕ от типа: единственное, что
	// теперь может отвергнуть этот токен, — отсутствие нашего ключа в наборе
	// прежнего издателя.
	crossA := ours.mint("ours-1", LegacyTokenType,
		f1bClaims(f1bLegacyIssuer, f1bAudience, "usr-1", now, time.Minute))
	_, errA := v.Verify(context.Background(), crossA)
	if errA == nil {
		t.Fatalf("токен, подписанный ключом НАШЕГО издателя и объявляющий ПРЕЖНЕГО, принят — " +
			"ключ одного издателя проверил токен другого")
	}
	if errors.Is(errA, ErrUnexpectedTokenType) {
		t.Fatalf("отказ пришёл от сверки ТИПА, а не от развязки наборов (%v) — на таком входе "+
			"проба зелена и при сломанном выборе записи", errA)
	}
	if !errors.Is(errA, ErrKeyNotFound) {
		t.Fatalf("отказ пришёл не от разрешения ключа в наборе ВЫБРАННОЙ записи (%v) — "+
			"утверждение о развязке сказано о чём-то другом", errA)
	}

	// Зеркально: подписан ключом прежнего, объявляет нашего.
	crossB := legacy.mint("legacy-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), crossB); err == nil {
		t.Fatalf("зеркальный случай принят — токен, подписанный ключом ПРЕЖНЕГО издателя и " +
			"объявляющий НАШЕГО, обязан отвергаться")
	}

	// ОБА законных сочетания принимаются. Без этой половины оба отрицания
	// зеленеют на проверяющем, отвергающем всё.
	okOurs := ours.mint("ours-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), okOurs); err != nil {
		t.Fatalf("законное сочетание НАШ ключ / НАШ издатель отвергнуто: %v", err)
	}
	okLegacy := legacy.mint("legacy-1", LegacyTokenType, f1bClaims(f1bLegacyIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), okLegacy); err != nil {
		t.Fatalf("законное сочетание ПРЕЖНИЙ ключ / ПРЕЖНИЙ издатель отвергнуто: %v", err)
	}
}

// TestF1b06_SnapshotsAreSeparatePerRecord — снимок каждой записи СВОЙ: обращение
// за набором одного издателя не наполняет снимок другого.
func TestF1b06_SnapshotsAreSeparatePerRecord(t *testing.T) {
	v, ours, legacy := f1bTwoIssuerVerifier(t)
	now := time.Now()

	tok := ours.mint("ours-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("законный токен нашей записи отвергнут: %v", err)
	}
	if ours.Requests == 0 {
		t.Fatalf("к источнику НАШЕЙ записи не обращались вовсе — проверка шла не по ней")
	}
	if legacy.Requests != 0 {
		t.Fatalf("обращение за набором НАШЕГО издателя задело источник ПРЕЖНЕГО (%d обращений) — "+
			"снимок общий, а обязан быть свой у каждой записи", legacy.Requests)
	}
}

// TestF1b03_IssuerWithoutADeclaredRecordIsRefused — издатель, записи не
// имеющий, даёт ОТКАЗ; и отказ этот наступает БЕЗ обращения к какому-либо
// источнику: перебора записей подряд не бывает.
func TestF1b03_IssuerWithoutADeclaredRecordIsRefused(t *testing.T) {
	v, ours, legacy := f1bTwoIssuerVerifier(t)
	now := time.Now()

	stranger := ours.mint("ours-1", PlatformTokenType,
		f1bClaims("https://issuer.example.invalid", f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), stranger); err == nil {
		t.Fatalf("токен издателя, для которого записи источника нет, принят")
	}
	if ours.Requests != 0 || legacy.Requests != 0 {
		t.Fatalf("отказ по неизвестному издателю стоил обращения к источникам "+
			"(наш %d, прежний %d) — записи перебирались подряд", ours.Requests, legacy.Requests)
	}

	// Положительный контроль: издатель ИЗ перечня резолвится.
	known := ours.mint("ours-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), known); err != nil {
		t.Fatalf("издатель из перечня не резолвится: %v — тогда отрицание выше зеленеет на "+
			"разборе, отвергающем всё", err)
	}
}

// TestF1b03_IssuerIsNeverPartOfAnAddress — объявленный издатель НЕДОВЕРЕННЫЙ
// вход: произвольная строка в нём не превращается в адрес и не уезжает наружу.
func TestF1b03_IssuerIsNeverPartOfAnAddress(t *testing.T) {
	v, ours, _ := f1bTwoIssuerVerifier(t)
	now := time.Now()

	for _, hostile := range []string{
		"../../../etc/passwd",
		"http://169.254.169.254/latest/meta-data/",
		"https://iam.kacho.test\n\rX-Injected: 1",
		"https://iam.kacho.test/../evil",
	} {
		tok := ours.mint("ours-1", PlatformTokenType, f1bClaims(hostile, f1bAudience, "usr-1", now, time.Minute))
		_, err := v.Verify(context.Background(), tok)
		if err == nil {
			t.Fatalf("издатель %q принят — произвольная строка резолвилась в запись", hostile)
		}
		if strings.Contains(err.Error(), hostile) {
			t.Fatalf("текст отказа несёт объявленный предъявителем издатель дословно (%q) — "+
				"недоверенный вход уехал наружу", hostile)
		}
	}
}
