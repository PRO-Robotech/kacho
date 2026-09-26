// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_alwayson_test.go — the per-RPC authentication floor has to be applied by
// the layer EVERY request passes through, and the token context it depends on has
// to be written there too.
//
// Both used to live only in the sender-constrained-token middleware, which mounts
// behind a feature toggle. A toggle is a legitimate home for verifying a proof of
// possession — that is a property of SOME tokens. Whether the caller
// authenticated strongly enough for THIS call is a property of every token, and
// the same is true of the token context the cluster-internal arm reads: with a
// single producer inside an unmounted middleware, the header never left the
// gateway, so the internal floor evaluated an absent value on every request.
//
// The neighbouring stepup_wiring_test.go drives the middleware directly and
// proves the gate CAN fire. That is a different statement from "it runs", and the
// difference is exactly what went unnoticed. These cases drive the always-mounted
// authN layer instead.
//
// «КАЖДЫЙ ЗАПРОС» ЗНАЧИТ ОБЕ ПОЛОСЫ (#1201). Прежняя редакция этого файла
// утверждала «слой, через который проходит КАЖДЫЙ запрос», а подавала ТОЛЬКО
// подписанного предъявителя. «Каждый запрос» и «каждый запрос ЭТОЙ полосы» —
// разные утверждения, и разница между ними и была дефектом: браузер ходит по
// сессии, а на той полосе пол не спрашивался вовсе. Заявление файла теперь
// исполнимо, поэтому оно и проверяется: случаи ниже подают ОБЕ полосы носителя
// личности — браузерную сессию (НАШУ) и подписанного предъявителя.
//
// Соседний stepup_lane_parity_test.go спрашивает другое — «решал ли кто-нибудь,
// что полосы различаются», — и сравнивает их попарно. Здесь утверждается
// поведение КАЖДОЙ полосы поимённо, включая исходы, которых у второй нет:
// уровень уверенности, который край перевести не может.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// alwaysOnAuth wires the authN interceptor the way the composition root must:
// a JWKS verifier plus the step-up arm backed by the embedded catalog and the
// generated REST route table.
func alwaysOnAuth(t *testing.T, fix *jwksFixture) *middleware.AuthInterceptor {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "",
		&countingLookup{subj: middleware.Subject{Type: "user", ID: "should-not-be-used"}},
		authTestLogger(),
	).
		WithVerifier(rs256Verifier(t, fix)).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
}

// alwaysOnClaims — an issuer-shaped human token at the given assurance level.
func alwaysOnClaims(acr string) jwt.MapClaims {
	c := issuerClaims("user", "usr_alice_acc_a1b2")
	c["acr"] = acr
	return c
}

// serveREST runs one request through the interceptor's HTTP arm and reports what
// the backend saw.
func serveREST(t *testing.T, auth *middleware.AuthInterceptor, method, url, token string) (*httptest.ResponseRecorder, *http.Request, bool) {
	t.Helper()
	var seen *http.Request
	hit := false
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		seen = r
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen, hit
}

// callUnary runs one call through the interceptor's gRPC arm.
func callUnary(t *testing.T, auth *middleware.AuthInterceptor, fullMethod, token string) error {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+token))
	_, err := auth.Unary()(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: fullMethod},
		func(context.Context, any) (any, error) { return nil, nil })
	return err
}

// A credential-mint RPC declares the raised floor. A human token below it must be
// refused by the always-mounted layer, with the challenge that tells the client
// which ceremony to run.
func TestStepUpAlwaysOn_SensitiveRPC_BelowFloor_Refused(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, alwaysOnClaims("1")))

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"the floor must be applied by the layer every request passes through, not by an unmounted middleware")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_user_authentication")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `acr_values="2"`)
	assert.False(t, hit, "the backend must not be reached")
}

// Routine resource work is not a posture change: the same token passes, and the
// token context travels on so the cluster-internal arm has something to read.
func TestStepUpAlwaysOn_RoutineRPC_Passes_AndForwardsTokenContext(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	rec, seen, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/vpc/v1/networks",
		fix.sign(t, alwaysOnClaims("1")))

	require.True(t, hit, "routine resource work carries no raised floor")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", seen.Header.Get("X-Kacho-Token-Acr"),
		"the internal floor reads this header; with no producer that runs, it evaluates an absent value forever")
	assert.Equal(t, "1", seen.Header.Get("Grpc-Metadata-X-Kacho-Token-Acr"))
}

// A machine has no interactive ceremony and can never present one. The shared
// rule exempts it; the always-mounted arm must not re-derive a stricter one.
func TestStepUpAlwaysOn_MachinePrincipal_Exempt(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	claims := issuerClaims("service_account", "sva_deployer_a1b2")
	delete(claims, "acr")

	rec, _, hit := serveREST(t, auth, http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens",
		fix.sign(t, claims))

	assert.True(t, hit, "a service principal is exempt from the interactive floor")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// The native gRPC surface is the same edge. It resolves the method directly, so
// there is no routing excuse for leaving it unguarded.
func TestStepUpAlwaysOn_GRPC_SensitiveRPC_BelowFloor_Refused(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	err := callUnary(t, auth, "/kaname.cloud.iam.v1.UserTokenService/Issue",
		fix.sign(t, alwaysOnClaims("1")))
	require.Error(t, err, "the native surface must apply the same floor")
	assert.Contains(t, err.Error(), "insufficient_user_authentication")
}

// And the same token reaches a routine method on that surface.
func TestStepUpAlwaysOn_GRPC_RoutineRPC_Passes(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	err := callUnary(t, auth, "/kacho.cloud.vpc.v1.NetworkService/Create",
		fix.sign(t, alwaysOnClaims("1")))
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА СЕССИИ. Тот же слой, тот же каталог, тот же пол — другой носитель.

// sessionAtLevel — ответ НАШЕЙ службы о живой сессии названного уровня.
//
// Уровень назван НА ОСИ КАТАЛОГА («1», «2», «3»): наша сессия объявляет его
// сама, перевода со словаря чужого поставщика нет (Ф11 Р7). Пустая строка
// означает «служба уровня не назвала» — ответ без поля, а не с пустым полем.
//
// ПРЕЖДЕ ЗДЕСЬ СТОЯЛ HTTP-ДУБЛЁР ЧУЖОГО ПОСТАВЩИКА с его словарём («aal1»,
// «aal2»). Он снят вместе с поставщиком; предмет случаев ниже — ПОЛ на полосе
// сессии — от смены носителя не меняется, и полоса, на которой он меряется,
// сегодня одна.
type sessionAtLevelReader struct{ level string }

func (r sessionAtLevelReader) ResolveHumanSession(
	_ context.Context, _ string,
) (middleware.HumanSession, bool, error) {
	return middleware.HumanSession{
		UserID:          "usr_alice_acc_a1b2",
		Email:           "alice@example.test",
		DisplayName:     "Alice A",
		AuthenticatedAt: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		AssuranceLevel:  r.level,
		EmailVerified:   true,
	}, true, nil
}

// alwaysOnSessionAuth — тот же композиционный корень, что alwaysOnAuth, плюс
// смонтированная полоса сессии. Резолвер субъекта здесь ИСПОЛЬЗУЕТСЯ (у сессии
// нет claim'ов), поэтому он несёт настоящую личность, а не заглушку.
func alwaysOnSessionAuth(t *testing.T, level string) *middleware.AuthInterceptor {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "",
		&countingLookup{subj: middleware.Subject{
			Type: "user", ID: "usr_alice_acc_a1b2", DisplayName: "Alice A",
		}},
		authTestLogger(),
	).
		WithHumanSession(sessionAtLevelReader{level: level}).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
}

// serveSession runs one browser request (cookie, no Bearer) through the same
// always-mounted layer.
func serveSession(t *testing.T, auth *middleware.AuthInterceptor, method, url string) (*httptest.ResponseRecorder, *http.Request, bool) {
	t.Helper()
	var seen *http.Request
	hit := false
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		seen = r
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, url, nil)
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: t.Name()})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen, hit
}

// Адреса: глагол с поднятым полом, глагол с полом AAL1 и глагол БЕЗ пола.
// Последний — единственный положительный контроль, годный для полосы, чей
// уровень край перевести не может: 317 записей каталога из 342 несут
// положительный пол, поэтому «обычный» глагол таким контролем НЕ является.
const (
	sessionElevatedRoute = "https://api.kacho.cloud/iam/v1/users/usr-abc/tokens" // пол "2"
	sessionRoutineRoute  = "https://api.kacho.cloud/vpc/v1/networks"             // пол "1"
	sessionNoFloorRoute  = "https://api.kacho.cloud/iam/v1/projects"             // пола нет
)

// Человек вошёл ПАРОЛЕМ (aal1) и просит чеканку удостоверения (пол "2"). Отказ
// обязан прийти от того же слоя и с тем же вызовом, что предъявителю. Именно
// этот случай в проде проходил: браузер ходит по сессии, а пол на ней не
// спрашивался.
func TestStepUpAlwaysOn_SessionLane_SensitiveRPC_BelowFloor_Refused(t *testing.T) {
	auth := alwaysOnSessionAuth(t, "1")

	rec, _, hit := serveSession(t, auth, http.MethodPost, sessionElevatedRoute)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"пол применяет слой, через который проходит каждый запрос — включая браузерный")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "insufficient_user_authentication")
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `acr_values="2"`)
	assert.False(t, hit, "backend не должен быть достигнут")
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ полосы: предъявивший второй фактор (aal2) тот же
// глагол ПРОХОДИТ, и его уровень едет вперёд ко второму замку. Без этого случая
// отказ выше зеленел бы на полосе, которая просто отвергает всё.
func TestStepUpAlwaysOn_SessionLane_AtFloor_Passes_AndForwardsAssurance(t *testing.T) {
	auth := alwaysOnSessionAuth(t, "2")

	rec, seen, hit := serveSession(t, auth, http.MethodPost, sessionElevatedRoute)

	require.True(t, hit, "предъявивший второй фактор проходит поднятый пол")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "2", seen.Header.Get("X-Kacho-Token-Acr"),
		"внутренний замок (iam authzguard.ACRFloor) читает этот заголовок; "+
			"без производителя он вечно вычисляет отсутствующее значение")
	assert.Equal(t, "2", seen.Header.Get("Grpc-Metadata-X-Kacho-Token-Acr"))
}

// Уровень, которого край не знает, — НЕ «достаточно». Мягкий проход здесь дал бы
// контроль, не отказавший ни разу за свою жизнь.
func TestStepUpAlwaysOn_SessionLane_UnknownAssurance_FailsClosed(t *testing.T) {
	auth := alwaysOnSessionAuth(t, "уровень-вне-оси")

	rec, _, hit := serveSession(t, auth, http.MethodPost, sessionElevatedRoute)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"нераспознанный уровень не удовлетворяет положительный пол")
	assert.False(t, hit)
}

// Служба, не назвавшая уровня ВОВСЕ, — тот же исход: обойти пол отсутствием
// поля в ответе нельзя.
func TestStepUpAlwaysOn_SessionLane_NoAssurance_FailsClosed(t *testing.T) {
	auth := alwaysOnSessionAuth(t, "")

	rec, _, hit := serveSession(t, auth, http.MethodPost, sessionElevatedRoute)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"отсутствующий уровень не удовлетворяет положительный пол")
	assert.False(t, hit)
}

// ГРАНИЦА fail-closed, и она существенна: закрыт ПОЛ, а не аутентификация.
// Глагол, которому каталог пола не объявил, проходит и с непереводимым уровнем —
// иначе «строгость» означала бы отказ каждому браузерному на всём каталоге.
func TestStepUpAlwaysOn_SessionLane_UnknownAssurance_NoFloorRPC_StillPasses(t *testing.T) {
	auth := alwaysOnSessionAuth(t, "уровень-вне-оси")

	rec, seen, hit := serveSession(t, auth, http.MethodGet, sessionNoFloorRoute)

	require.True(t, hit, "fail-closed закрывает пол, а не полосу целиком")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, seen.Header.Get("X-Kacho-Token-Acr"),
		"уровня, за который полоса не может поручиться, она и не утверждает: "+
			"пустое ранжируется нулём и у второго замка тоже не пройдёт пол")
	assert.Equal(t, "user", seen.Header.Get("X-Kacho-Principal-Type"),
		"личность резолвится: отказ пола — не отказ аутентификации")
}

// Пол AAL1 на обычной работе с ресурсами держится и на этой полосе: вошедший
// паролем работает, а уровень вне оси сессии — нет.
func TestStepUpAlwaysOn_SessionLane_RoutineRPC_Level1Passes_OffAxisRefused(t *testing.T) {
	pass := alwaysOnSessionAuth(t, "1")
	recPass, seenPass, hitPass := serveSession(t, pass, http.MethodPost, sessionRoutineRoute)
	require.True(t, hitPass, "уровень «1» удовлетворяет пол AAL1")
	assert.Equal(t, http.StatusOK, recPass.Code)
	assert.Equal(t, "1", seenPass.Header.Get("X-Kacho-Token-Acr"))

	refuse := alwaysOnSessionAuth(t, "0")
	recRefuse, _, hitRefuse := serveSession(t, refuse, http.MethodPost, sessionRoutineRoute)
	assert.Equal(t, http.StatusUnauthorized, recRefuse.Code, "уровень «0» вне оси сессии и ранжируется нулём")
	assert.False(t, hitRefuse)
}
