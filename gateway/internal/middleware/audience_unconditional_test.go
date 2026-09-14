// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// audience_unconditional_test.go — АДРЕСАТ СУЖАЕТ БЕЗУСЛОВНО (задача #2567).
//
// # Предмет
//
// `aud` — единственное, чем токен говорит, КАКОЙ УСТАНОВКЕ он адресован.
// Проверка, сужающая только при непустом ожидаемом значении, на незаявленном
// адресате выглядит включённой и не отвергает ничего: пустое означает «не
// сужаем», а не «запрещаем» (`security.md` §«Пустой список — это „не сужаем",
// а НЕ „запрещаем"»).
//
// # Что из этого следует для соседней установки
//
// Край с незаявленным адресатом принимает токен, выпущенный ДЛЯ ДРУГОЙ
// установки: подпись у него настоящая, издатель объявлен принимаемым, срок не
// вышел — отвергнуть его больше нечему.
//
// # Форма утверждения
//
// Исходов у полосы ровно ДВА, и каждый её закрывает: конструктор отказал (см.
// собственный довод `NewJWTVerifier` — «отказ вместо построения на всяком
// состоянии, которое при пустом значении означает „не сужаем"») ЛИБО проверка
// отвергла чужой адресат. Третьего — «построился и принял» — не бывает.
// Утверждается поэтому дизъюнкция, а не один из двух: сузив её до одного
// исхода, проба судила бы выбранный способ починки, а не свойство.
package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// foreignInstallationAudience — адресат ДРУГОЙ установки того же продукта.
// Подпись, издатель и срок у такого токена настоящие; неверен только адресат.
const foreignInstallationAudience = "https://api.other-tenant.example"

// undeclaredAudienceRecords — годная запись приёма: неверно здесь ровно одно —
// адресат не объявлен. Одно-фактность против положительного близнеца ниже.
func undeclaredAudienceRecords(fix *jwksFixture) []middleware.IssuerKeySet {
	return []middleware.IssuerKeySet{{
		Issuer:                  testIssuer,
		KeySetURL:               fix.url,
		TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
		TolerateAbsentTokenType: true,
	}}
}

// TestUndeclaredAudienceNeverAcceptsAForeignInstallationsToken — НЕСУЩЕЕ.
func TestUndeclaredAudienceNeverAcceptsAForeignInstallationsToken(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")

	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: undeclaredAudienceRecords(fix),
		// ExpectedAudience НЕ ОБЪЯВЛЕН — ровно то состояние, которое давал
		// выведенный адресат на посадке, домен не объявившей.
	})
	if err != nil {
		t.Logf("исход: конструктор отказал на незаявленном адресате (%v)", err)
		return
	}

	claims := standardClaims()
	claims["aud"] = []any{foreignInstallationAudience}
	_, verr := v.Verify(context.Background(), fix.sign(t, claims))
	require.Error(t, verr,
		"край построился с НЕЗАЯВЛЕННЫМ адресатом и принял токен, адресованный %q — "+
			"то есть выпущенный для другой установки. Ни конструктор, ни проверка полосу "+
			"не закрыли: пустое ожидаемое значение означает «не сужаем»",
		foreignInstallationAudience)
	t.Logf("исход: проверка отвергла чужой адресат (%v)", verr)
}

// TestDeclaredAudienceAcceptsOurOwnToken — ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ.
//
// Без него утверждение выше зеленело бы на проверяющем, отвергающем всё: «чужой
// токен не принят» верно и для края, который не принимает никакого.
func TestDeclaredAudienceAcceptsOurOwnToken(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")

	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers:          undeclaredAudienceRecords(fix),
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err, "объявленный адресат обязан строиться")

	_, verr := v.Verify(context.Background(), fix.sign(t, standardClaims()))
	require.NoError(t, verr, "свой токен при объявленном адресате обязан приниматься")
}

// TestDeclaredAudienceStillRefusesAForeignInstallationsToken — второй
// положительный близнец: сужение действует и после того, как адресат объявлен.
func TestDeclaredAudienceStillRefusesAForeignInstallationsToken(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")

	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers:          undeclaredAudienceRecords(fix),
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	claims := standardClaims()
	claims["aud"] = []any{foreignInstallationAudience}
	_, verr := v.Verify(context.Background(), fix.sign(t, claims))
	require.Error(t, verr, "объявленный адресат обязан отвергать чужой")
}
