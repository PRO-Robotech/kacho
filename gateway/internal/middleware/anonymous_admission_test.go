// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// anonymous_admission_test.go — отказные стороны пропуска анонима, которых
// приёмка не перечисляет поимённо, но без которых пропуск был бы шире, чем
// объявлен (anonymous_admission.go). Близнец каждой — SC-30: та же цепочка П3
// на строке «0», пропускающая анонима; отличие — ровно один факт.

import (
	"context"
	"io"
	"log/slog"
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

// Ступень пола не провязана — слою аутентификации нечем прочесть признак строки,
// и он отвергает анонима на строке «0» прежним отказом: пропуск без
// удостоверения не зависит от того, смонтирован ли следующий слой.
func TestAnonymousAdmission_UnwiredCatalogRefusesOnBothLanes(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "близнец SC-30: провязанный слой пропускает")

	wantNative, wantCode, wantBody := todayRefusal(t)
	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	p.auth = middleware.NewAuthInterceptor(middleware.AuthModeProduction, "secret", &fakeLookup{}, authTestLogger()).
		WithVerifier(rs256Verifier(t, p.jwks))

	n := p.native(scObj1, "")
	require.Error(t, n.err)
	assert.Equal(t, wantNative, statusBytes(t, n.err), "нативная — прежний отказ побайтово")
	assert.False(t, n.reached)
	r := p.rest(scObj1, "")
	assert.Equal(t, wantCode, r.code)
	assert.Equal(t, wantBody, r.body, "REST — прежний отказ побайтово")
	assert.False(t, r.reached)
	assert.Zero(t, p.checker.calls.Load(), "дублёр не спрошен")
}

// Носитель нашей сессии предъявлен, но не принят ни одной полосой — это
// предъявленное удостоверение, анонимом оно не становится (инвариант 1).
func TestAnonymousAdmission_PresentedSessionCarrierIsNeverAnonymous(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.Equal(t, http.StatusOK, twin.rest(scObj1, "").code, "близнец SC-30: без носителя пропускает")

	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	var reached bool
	h := p.auth.HTTP(p.mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})))
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks/"+scObj1, nil)
	req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "unresolved"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, reached)
	assert.Zero(t, p.checker.calls.Load(), "дублёр не спрошен")
}

// Каталоги двух звеньев разошлись (перечитывание): слой аутентификации видит
// строку «0», посредник прав — строку «1». Посредник отметку пропуска без
// признака своей строки не исполняет: прежний отказ побайтово, модель не
// спрошена.
func TestAnonymousAdmission_AuthzRechecksItsOwnRow(t *testing.T) {
	twin := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	require.NoError(t, twin.native(scObj1, "").err, "близнец SC-30: строки обоих звеньев «0»")

	wantNative, wantCode, wantBody := todayRefusal(t)
	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	p.mw = newP3(t, "1", p.checker).mw

	n := p.native(scObj1, "")
	require.Error(t, n.err)
	assert.Equal(t, wantNative, statusBytes(t, n.err))
	assert.False(t, n.reached)
	r := p.rest(scObj1, "")
	assert.Equal(t, wantCode, r.code)
	assert.Equal(t, wantBody, r.body)
	assert.False(t, r.reached)
	assert.Zero(t, p.checker.calls.Load(), "дублёр не спрошен")
}

// Потоковая полоса (проксируемый RPC края приходит ею): область конкретного
// объекта до чтения сообщения неразрешима, решение — отказ. Анониму он
// отдаётся прежним отказом без удостоверения, а не `PERMISSION_DENIED`:
// иначе ответ отличал бы метод с допуском от метода без него (Р7).
func TestAnonymousAdmission_StreamLaneRefusalIsTheUncredentialedOne(t *testing.T) {
	wantNative, _, _ := todayRefusal(t)
	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	info := &grpc.StreamServerInfo{FullMethod: "/" + scFQN, IsClientStream: true, IsServerStream: true}
	var reached bool
	err := p.auth.Stream()(nil, &ctxStream{ctx: ctx}, info, func(_ any, ss grpc.ServerStream) error {
		return p.mw.Stream()(nil, ss, info, func(any, grpc.ServerStream) error {
			reached = true
			return nil
		})
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, wantNative, statusBytes(t, err), "потоковая полоса — прежний отказ побайтово")
	assert.False(t, reached)

	// Близнец: та же полоса от вошедшего usr-B — отказ области, а не входа.
	md := metadata.MD{}
	md.Set("authorization", "Bearer "+p.usrBToken())
	err = p.auth.Stream()(nil, &ctxStream{ctx: metadata.NewIncomingContext(context.Background(), md)}, info,
		func(_ any, ss grpc.ServerStream) error {
			return p.mw.Stream()(nil, ss, info, func(any, grpc.ServerStream) error { return nil })
		})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "близнец: вошедший получает отказ области")
}

// ctxStream — серверный поток с заданным контекстом.
type ctxStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *ctxStream) Context() context.Context { return s.ctx }

// Строгая посадка со слоем доказательства владения (`RequireForAllRequests`)
// стоит между слоем аутентификации и посредником прав. Запрос, пропущенный
// слоем аутентификации по строке «0», она не отвергает — иначе допуск метода
// зависел бы от того, смонтирован ли этот слой. Близнец — тот же слой на
// запросе без отметки пропуска: прежний отказ.
func TestAnonymousAdmission_StrictProofOfPossessionLayerHonoursTheAdmission(t *testing.T) {
	p := newP3(t, "0", newTableChecker(map[string]bool{scAnon: true}))
	dpop, err := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
		ReplayCache:  middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{MaxEntries: 8, TTL: time.Minute}),
		IatFreshness: time.Minute,
	})
	require.NoError(t, err)
	strict, err := middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
		Revocation:            p.auth,
		Verifier:              stepUpVerifier(t, p.jwks),
		DPoP:                  dpop,
		StepUp:                middleware.NewStepUpGate(time.Now),
		Logger:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		APIDomain:             "api.kacho.cloud",
		RequireForAllRequests: true,
	})
	require.NoError(t, err)

	var reached bool
	h := p.auth.HTTP(strict.Wrap(p.mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vpc/v1/networks/"+scObj1, nil))
	assert.Equal(t, http.StatusOK, w.Code, "строгий слой пропускает отмеченный запрос: %s", w.Body.String())
	assert.True(t, reached)
	assert.EqualValues(t, 1, p.checker.calls.Load(), "решение вынес посредник прав")

	_, wantCode, wantBody := todayRefusal(t)
	w = httptest.NewRecorder()
	strict.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vpc/v1/networks/"+scObj1, nil))
	assert.Equal(t, wantCode, w.Code, "близнец: без отметки — прежний отказ")
	assert.Equal(t, wantBody, w.Body.String(), "близнец: прежний отказ побайтово")
}
