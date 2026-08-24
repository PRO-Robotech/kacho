// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// auth_keysource_unavailable_test.go — недоступность ИСТОЧНИКА КЛЮЧЕЙ есть сбой
// зависимости, а не приговор удостоверению вызывающего (задача #1194).
//
// # Предмет
//
// `UNAUTHENTICATED (16)` означает «твоё удостоверение негодно»: исход
// терминальный, повтор бессмыслен, штатная реакция клиента — выбросить токен и
// пройти вход заново. `UNAVAILABLE (14)` означает «повтори позже».
//
// Пока край отвечал 16 на недостижимый набор ключей, недоступность соседа
// заставляла КАЖДОГО клиента переаутентифицироваться — то есть добавляла
// нагрузку ровно на тот компонент, который лежит, и объявляла негодным исправный
// токен. Отказ при этом был и остаётся правильным: спорна не строгость, а
// классификация.
//
// # Три исхода, а не два, и они РАЗВЕДЕНЫ
//
//	источник ключей не ответил   → 503 / 14 (UNAVAILABLE)      — «повтори позже»
//	удостоверение негодно        → 401 / 16 (UNAUTHENTICATED)  — «войди заново»
//	прав нет                     → 403 /  7 (PERMISSION_DENIED) — слой прав, не здесь
//
// # Почему проба ПАРНАЯ
//
// Отрицание без положительного контроля вырождается в «отвечать 503 на всё»:
// такая правка зеленела бы и на дереве, где негодный токен тоже получает 503, —
// то есть на снятой аутентификации. Поэтому рядом с каждой половиной «источник
// не ответил» стоит половина «токен действительно негоден», и граница между ними
// закреплена отдельно: `kid`, которого в ЖИВОМ наборе нет, — это негодный токен,
// а не сбой источника.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// deadKeySetVerifier — проверяющий, чей источник ключей НЕ ОТВЕЧАЕТ (транспорт).
func deadKeySetVerifier(t *testing.T) *middleware.JWTVerifier {
	t.Helper()
	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{
			Issuer:                  testIssuer,
			KeySetURL:               "http://127.0.0.1:1/.well-known/jwks.json",
			TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
			TolerateAbsentTokenType: true,
		}},
		ExpectedAudience: testAudience,
		JWKSFetchTimeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	return v
}

// refusingKeySet заставляет фикстуру отвечать ровно так, как отвечает наш
// публикатор набора, когда не может его собрать: 502 с опознавательным словом.
// Это НЕ выдуманная форма — `services/iam/internal/handler/jwksproxyhttp`
// отказывает именно так, и именно этот ответ получил край в прогоне 32677391418.
func refusingKeySet(fix *jwksFixture) {
	fix.overrides = func(w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"jwks_keyset_unavailable"}`))
		return true
	}
}

func grpcCodeOf(t *testing.T, auth *middleware.AuthInterceptor, token string) (codes.Code, bool) {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+token))
	reached := false
	handler := func(context.Context, any) (any, error) { reached = true; return nil, nil }
	_, err := auth.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/iam/WhoAmI"}, handler)
	require.Error(t, err, "отказ обязан остаться отказом — правится классификация, не строгость")
	st, ok := status.FromError(err)
	require.True(t, ok)
	return st.Code(), reached
}

func restRefusal(t *testing.T, auth *middleware.AuthInterceptor, token string) (int, float64, bool) {
	t.Helper()
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	auth.HTTP(next).ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body),
		"тело отказа обязано быть разбираемым JSON: %q", rec.Body.String())
	raw, ok := body["code"]
	require.True(t, ok, "в теле отказа нет поля code — клиенту ключеваться не на что: %q", rec.Body.String())
	num, ok := raw.(float64)
	require.True(t, ok, "поле code не число: %#v", raw)
	return rec.Code, num, reached
}

func TestKeySourceUnanswerableIsUnavailableNotUnauthenticated(t *testing.T) {
	// ── ПОЛОВИНА ПЕРВАЯ: источник ключей не ответил ─────────────────────────
	t.Run("grpc: источник ключей недостижим", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		token := fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2"))
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(deadKeySetVerifier(t))

		code, reached := grpcCodeOf(t, auth, token)
		assert.Equal(t, codes.Unavailable, code,
			"недоступность источника ключей — сбой зависимости, а не негодность удостоверения")
		assert.False(t, reached, "fail-closed: обработчик не исполняется")
	})

	t.Run("grpc: источник ответил, но не набором", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		token := fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2"))
		refusingKeySet(fix)
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(rs256Verifier(t, fix))

		code, reached := grpcCodeOf(t, auth, token)
		assert.Equal(t, codes.Unavailable, code)
		assert.False(t, reached)
	})

	t.Run("rest: источник ключей недостижим", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		token := fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2"))
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(deadKeySetVerifier(t))

		httpStatus, grpcCode, reached := restRefusal(t, auth, token)
		// Утверждается ПАРА: край своего отображения ошибок не несёт, поэтому
		// статус — механическое следствие кода (api-conventions.md).
		assert.Equal(t, http.StatusServiceUnavailable, httpStatus)
		assert.Equal(t, float64(codes.Unavailable), grpcCode)
		assert.False(t, reached)
	})

	t.Run("rest: источник ответил, но не набором", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		token := fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2"))
		refusingKeySet(fix)
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(rs256Verifier(t, fix))

		httpStatus, grpcCode, reached := restRefusal(t, auth, token)
		assert.Equal(t, http.StatusServiceUnavailable, httpStatus)
		assert.Equal(t, float64(codes.Unavailable), grpcCode)
		assert.False(t, reached)
	})

	// ── ПОЛОВИНА ВТОРАЯ (положительный контроль): токен действительно негоден ─
	// Без неё правка выше вырождается в «отвечать 503 на всё», то есть в снятую
	// аутентификацию, и ни одно утверждение первой половины этого не заметит.
	t.Run("grpc: негодная подпись остаётся UNAUTHENTICATED", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		other := newJWKSFixture(t, "RS256")
		other.kid = fix.kid // тот же kid, ЧУЖОЙ ключ → подпись не сходится
		token := other.sign(t, hydraClaims("user", "usr_evil"))
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(rs256Verifier(t, fix))

		code, reached := grpcCodeOf(t, auth, token)
		assert.Equal(t, codes.Unauthenticated, code)
		assert.False(t, reached)
	})

	t.Run("rest: истёкший токен остаётся 401/16", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		claims := hydraClaims("user", "usr_alice_acc_a1b2")
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
		claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
		token := fix.sign(t, claims)
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(rs256Verifier(t, fix))

		httpStatus, grpcCode, reached := restRefusal(t, auth, token)
		assert.Equal(t, http.StatusUnauthorized, httpStatus)
		assert.Equal(t, float64(codes.Unauthenticated), grpcCode)
		assert.False(t, reached)
	})

	// ── ГРАНИЦА между половинами, и она не самоочевидна ─────────────────────
	// Живой источник ответил набором, а подписант назвал ключ, которого в наборе
	// НЕТ. Источник тут ни при чём — негоден токен, и ответ обязан остаться
	// терминальным. Ошибка в эту сторону тише всех остальных: она превратила бы
	// подделку с произвольным `kid` в «повтори позже».
	t.Run("rest: неизвестный kid при ЖИВОМ наборе — это негодный токен, а не сбой", func(t *testing.T) {
		fix := newJWKSFixture(t, "RS256")
		other := newJWKSFixture(t, "RS256")
		other.kid = "test-kid-nobody-publishes"
		token := other.sign(t, hydraClaims("user", "usr_evil"))
		auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "", &countingLookup{}, authTestLogger()).
			WithVerifier(rs256Verifier(t, fix))

		httpStatus, grpcCode, reached := restRefusal(t, auth, token)
		assert.Equal(t, http.StatusUnauthorized, httpStatus)
		assert.Equal(t, float64(codes.Unauthenticated), grpcCode)
		assert.False(t, reached)
	})
}
