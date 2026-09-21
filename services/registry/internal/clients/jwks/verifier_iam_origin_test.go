// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package jwks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Эти пробы фиксируют поведение проверяющего плоскости данных ПОСЛЕ сведения набора
// ключей к одному адресу: набор скачивается из зеркала (а не у прежнего издателя
// напрямую), а привязка издателя остаётся на ПРЕЖНЕМ издателе. verifier.go
// origin-agnostic (I7): он не знает и не должен знать, что за URL стоит за адресом
// набора. Поэтому эти случаи — замок поведения на развязке (адрес набора ⟂ издатель,
// I5), fail-closed (I3) и происхождении скачивания (I8): они ловят будущую регрессию,
// если кто-то заодно перенаправит привязку издателя на зеркало или сломает
// fail-closed. testLegacyIss / testAud / newJWKSServer / mintRS256 /
// legacyIdentityClaims — общая оснастка из verifier_test.go (тот же пакет).

// iamJWKSServer — семантический синоним: подставное зеркало набора ключей, отдающее
// зеркалированные ключи прежнего издателя (идентификатор и алгоритм байт-в-байт его).
// Для проверяющего это просто адрес набора.
func iamJWKSServer(t *testing.T, mirroredKids ...string) *jwksServer {
	t.Helper()
	return newJWKSServer(t, mirroredKids...)
}

// RJU-09 — счастливый путь: набор скачивается с адреса зеркала, токен, подписанный
// ПРЕЖНИМ издателем (iss = прежний, kid = legacy-kid-1), проверяется, привязка издателя
// совпала → проверка проходит. Доказывает I8 (скачивание идёт с объявленного адреса
// зеркала) + I5 (издатель прежнего проходит).
func TestJWKS_Verify_IAMOrigin_LegacyIssuerPinned(t *testing.T) {
	iam := iamJWKSServer(t, "legacy-kid-1")
	// Проверяющий направлен на адрес зеркала; привязка издателя — на прежнего
	// (ручки раздельные).
	v := newTestVerifier(t, iam.srv.URL, testAud, testLegacyIss)
	tok := iam.mintRS256(t, "legacy-kid-1", legacyIdentityClaims("cid-ci", time.Now().Add(time.Hour)))

	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "токен прежнего издателя обязан проверяться набором, скачанным с зеркала")
	require.Equal(t, "cid-ci", sub)
	require.GreaterOrEqual(t, iam.fetch.Load(), int32(1),
		"JWKS must be fetched from the configured iam origin")
}

// RJU-11 — fail-closed: iam-JWKS-эндпоинт недоступен и нужного ключа нет в кэше →
// Verify падает и оборачивает ErrInvalidToken (data-plane маппит ЛЮБУЮ ошибку Verify
// в HTTP 401 invalid_token). Никогда не fail-open. Доказывает I3.
func TestJWKS_Verify_IAMOrigin_Unreachable_FailClosed(t *testing.T) {
	iam := iamJWKSServer(t, "legacy-kid-1")
	tok := iam.mintRS256(t, "legacy-kid-1", legacyIdentityClaims("cid-ci", time.Now().Add(time.Hour)))
	iam.srv.Close() // iam JWKS-proxy недоступен, кэш verifier'а пуст (cold)

	v := newTestVerifier(t, iam.srv.URL, testAud, testLegacyIss)
	_, err := v.Verify(context.Background(), tok)
	require.Error(t, err, "iam JWKS unreachable + cold cache must fail closed (never allow)")
	require.ErrorIs(t, err, ErrInvalidToken,
		"fail-closed Verify error must map to 401 invalid_token")
}

// RJU-13/n2 — привязка издателя остаётся на ПРЕЖНЕМ НЕСМОТРЯ на смену адреса набора
// на зеркало (развязка ручек, I5): токен подписан годным зеркалированным ключом из
// набора зеркала, но несёт iss = адрес зеркала (как если бы кто-то ошибочно
// перенаправил издателя на зеркало) → ОТВЕРГАЕТСЯ. Ловит регрессию «заодно
// перепривязать издателя на зеркало».
func TestJWKS_Verify_IAMOrigin_IssuerMismatch_Rejected(t *testing.T) {
	iam := iamJWKSServer(t, "legacy-kid-1")
	v := newTestVerifier(t, iam.srv.URL, testAud, testLegacyIss) // привязка — на прежнем издателе
	claims := legacyIdentityClaims("cid-ci", time.Now().Add(time.Hour))
	claims["iss"] = iam.srv.URL // iss указывает на зеркало, а не на прежнего издателя — расхождение
	tok := iam.mintRS256(t, "legacy-kid-1", claims)

	_, err := v.Verify(context.Background(), tok)
	require.Error(t, err, "iss-mismatch must be rejected even though JWKS now comes from iam")
	require.ErrorIs(t, err, ErrInvalidToken)
}
