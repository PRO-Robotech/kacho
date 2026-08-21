// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_token_checks_test.go — Ф1б-07 и Ф1б-08: край ИСПОЛНЯЕТ обязательные
// проверки, а не только объявляет их.
//
// Гейт по дереву судит ОБЪЯВЛЕНИЕ состава: константа, названная и не
// исполняемая, разбору неотличима от исполняемой. Правдивость держит эта проба —
// она подаёт токен, у которого не хватает РОВНО ОДНОГО признака, и рядом тот же
// токен с признаком, который обязан приниматься.
package middleware

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/golang-jwt/jwt/v5"
)

// TestF1b07_TokenTypeIsRequiredOnOurLaneAndCheckedOnBoth — тип обязателен на
// НАШЕЙ полосе; несовпадение отвергается на любой.
func TestF1b07_TokenTypeIsRequiredOnOurLaneAndCheckedOnBoth(t *testing.T) {
	v, ours, legacy := f1bTwoIssuerVerifier(t)
	now := time.Now()

	noTyp := ours.mint("ours-1", "", f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), noTyp); err == nil {
		t.Fatalf("наш токен БЕЗ заголовка типа принят — производитель типа мы сами, и его " +
			"отсутствие означало бы, что мы не выпускаем того, что требуем")
	}

	wrongTyp := ours.mint("ours-1", "dpop+jwt", f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), wrongTyp); err == nil {
		t.Fatalf("наш токен с НЕСОВПАДАЮЩИМ типом принят")
	}

	okTyp := ours.mint("ours-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), okTyp); err != nil {
		t.Fatalf("наш токен с ожидаемым типом отвергнут: %v", err)
	}

	// Полоса ПРЕЖНЕГО издателя своего поведения не меняет: отсутствие типа
	// принимается, НЕСОВПАДЕНИЕ — нет.
	legacyNoTyp := legacy.mint("legacy-1", "", f1bClaims(f1bLegacyIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), legacyNoTyp); err != nil {
		t.Fatalf("токен прежнего издателя без типа отвергнут: %v — полоса меняет поведение "+
			"там, где менять его фаза не собиралась", err)
	}
	legacyWrongTyp := legacy.mint("legacy-1", "at+jwt", f1bClaims(f1bLegacyIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), legacyWrongTyp); err == nil {
		t.Fatalf("токен прежнего издателя с НЕСОВПАДАЮЩИМ типом принят — отсутствие типа и " +
			"несовпадение типа разные вещи, и различает их только эта ветка")
	}
}

// TestF1b08_ExpiryIsRequired — срок ПРИСУТСТВУЕТ и не истёк. Разбор, не встретив
// срока, сам не возразит: токен без срока живёт вечно, и на положительном пути
// это невидимо.
func TestF1b08_ExpiryIsRequired(t *testing.T) {
	v, ours, _ := f1bTwoIssuerVerifier(t)
	now := time.Now()

	noExp := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute)
	delete(noExp, "exp")
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, noExp)); err == nil {
		t.Fatalf("токен БЕЗ срока принят — он живёт вечно, и заметить это на положительном пути нельзя")
	}

	expired := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now.Add(-2*time.Hour), time.Minute)
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, expired)); err == nil {
		t.Fatalf("токен с ИСТЁКШИМ сроком принят")
	}

	live := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute)
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, live)); err != nil {
		t.Fatalf("токен с ДЕЙСТВУЮЩИМ сроком отвергнут: %v", err)
	}
}

// TestF1b08_KeyIDFormIsBoundedBeforeUse — форма идентификатора ключа ограничена
// ДО обращения к источнику: он приходит от предъявителя.
func TestF1b08_KeyIDFormIsBoundedBeforeUse(t *testing.T) {
	v, ours, _ := f1bTwoIssuerVerifier(t)
	now := time.Now()

	for _, bad := range []string{
		"",
		strings.Repeat("k", 400),
		"kid/../../etc/passwd",
		"kid\nX-Injected: 1",
	} {
		claims := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute)
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		tok.Header["kid"] = bad
		tok.Header["typ"] = PlatformTokenType
		raw, err := tok.SignedString(ours.signer["ours-1"])
		if err != nil {
			t.Fatalf("подпись: %v", err)
		}
		before := ours.Requests
		if _, verr := v.Verify(context.Background(), raw); verr == nil {
			t.Fatalf("идентификатор ключа негодной формы (%q) принят", bad)
		}
		if ours.Requests != before {
			t.Fatalf("идентификатор ключа негодной формы (%q) стоил обращения к источнику "+
				"набора — форма обязана ограничиваться ДО использования", bad)
		}
	}

	// Положительный контроль: законный идентификатор резолвится.
	ok := ours.mint("ours-1", PlatformTokenType, f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute))
	if _, err := v.Verify(context.Background(), ok); err != nil {
		t.Fatalf("законный идентификатор ключа не резолвится: %v — тогда отрицания выше "+
			"зеленеют на разборе, отвергающем всё", err)
	}
}

// TestF1b08_ClockSkewIsAssertedOnBothSides — допуск на расхождение часов
// утверждается ОБЕИМИ сторонами, и часы пробы — ВХОД, а не системное время.
//
// Прежняя редакция двигала не часы, а утверждения токена, и попадала в допуск
// широкими полями: такая проба не различает «допуск ровно такой» от «допуск
// вдвое шире». Здесь двигаются часы проверяющего, а токен неподвижен, — тогда
// граница утверждается там, где она объявлена.
func TestF1b08_ClockSkewIsAssertedOnBothSides(t *testing.T) {
	ours, legacy := newF1bKeySet(t), newF1bKeySet(t)
	ours.addEC("ours-1")
	legacy.addRSA("legacy-1")

	// Часы — управляемые: проверяющий смотрит на них, а не на системное время.
	var at time.Time
	v, err := NewJWTVerifier(JWTVerifierConfig{
		Issuers: []IssuerKeySet{
			{Issuer: f1bPlatformIssuer, KeySetURL: ours.URL(), TokenTypes: []string{PlatformTokenType}},
			{Issuer: f1bLegacyIssuer, KeySetURL: legacy.URL(),
				TokenTypes: []string{LegacyTokenType}, TolerateAbsentTokenType: true},
		},
		ExpectedAudience: f1bAudience,
		Clock:            func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("построение проверяющего: %v", err)
	}

	issued := time.Now()
	tok := ours.mint("ours-1", PlatformTokenType,
		f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", issued, time.Minute))

	// Обе стороны допуска по СРОКУ: токен истёк, но не дальше допуска — принят;
	// на секунду дальше допуска — отвергнут.
	at = issued.Add(time.Minute).Add(tokenpolicy.ClockSkew - 2*time.Second)
	if _, verr := v.Verify(context.Background(), tok); verr != nil {
		t.Fatalf("истёкший ВНУТРИ допуска отвергнут: %v — допуск уже объявленного", verr)
	}
	at = issued.Add(time.Minute).Add(tokenpolicy.ClockSkew + 2*time.Second)
	if _, verr := v.Verify(context.Background(), tok); verr == nil {
		t.Fatalf("истёкший ЗА пределом допуска принят — допуск шире объявленного")
	}

	// Обе стороны допуска по МОМЕНТУ ВСТУПЛЕНИЯ В СИЛУ: часы отстают.
	at = issued.Add(-tokenpolicy.ClockSkew + 2*time.Second)
	if _, verr := v.Verify(context.Background(), tok); verr != nil {
		t.Fatalf("«из будущего» ВНУТРИ допуска отвергнут: %v", verr)
	}
	at = issued.Add(-tokenpolicy.ClockSkew - 2*time.Second)
	if _, verr := v.Verify(context.Background(), tok); verr == nil {
		t.Fatalf("«из будущего» ЗА пределом допуска принят")
	}

	// Положительный контроль: в момент выпуска токен принимается. Без него все
	// четыре утверждения зеленели бы на проверяющем, отвергающем всё.
	at = issued
	if _, verr := v.Verify(context.Background(), tok); verr != nil {
		t.Fatalf("токен отвергнут в момент собственного выпуска: %v", verr)
	}
}

// TestF1b08_AudienceIsRequiredAndChecked — незаданный адресат означает «любой».
func TestF1b08_AudienceIsRequiredAndChecked(t *testing.T) {
	v, ours, _ := f1bTwoIssuerVerifier(t)
	now := time.Now()

	other := f1bClaims(f1bPlatformIssuer, "https://registry.kacho.test", "usr-1", now, time.Minute)
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, other)); err == nil {
		t.Fatalf("токен, адресованный ДРУГОМУ контуру, принят — путаница адресатов есть " +
			"самостоятельная возможность ровно тогда, когда один подписант обслуживает два контура")
	}

	none := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute)
	delete(none, "aud")
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, none)); err == nil {
		t.Fatalf("токен БЕЗ адресата принят")
	}

	ok := f1bClaims(f1bPlatformIssuer, f1bAudience, "usr-1", now, time.Minute)
	if _, err := v.Verify(context.Background(), ours.mint("ours-1", PlatformTokenType, ok)); err != nil {
		t.Fatalf("токен, адресованный ЭТОЙ поверхности, отвергнут: %v", err)
	}
}
