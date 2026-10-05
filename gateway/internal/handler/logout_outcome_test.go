// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_outcome_test.go — ИСХОД ВЫХОДА КРАЯ `POST /oauth/logout` — тот же, что
// у выхода полосы входа `POST /iam/v1/auth/logout`, на каждом предъявлении,
// которое путь принимает (kacho#2996), и выход на этом пути либо гасит носитель
// на сервере, либо отвечает отказом, — но никогда «вышли» при живой сессии
// (kacho#2959); каждый отказ пути — в форме `google.rpc.Status` (kacho#2956),
// неверный метод — `405` с кодом `12` и `Allow` (kaname#524, решение R36).
//
// # Откуда берутся ожидания
//
// Исходы выхода полосы входа объявляет служба (kaname
// `docs/content/api/auth-lane.mdx`, «Выход»; приёмка Ф3, Р4): `200 {}` — выход
// выполнен; `503` `{"code":14,"message":"logout not performed; try again later",
// "details":[]}` — хранилище не ответило, носитель цел, повторить можно.
// «Отвергнуто: гасить носитель и при отказе хранилища» — Ф3 Р4 дословно: именно
// это делал выход края, отвечая `200` с `warnings`.
//
// # Чего проба НЕ утверждает
//
// Она судит обработчик, а не боевую цепочку: «прежний носитель после выхода
// отвергнут» на пути запроса держит сквозная проба
// `gateway/internal/e2e/logout_ends_it_server_side_e2e_test.go`.
package handler_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Тела ответа — дословно. Выписаны здесь, а не взяты у продукта: проба, читающая
// ожидание у проверяемого, согласилась бы с любым его значением.
const (
	logoutDoneBody          = `{}`
	logoutNotPerformedBody  = `{"code":14,"message":"logout not performed; try again later","details":[]}`
	logoutMethodBody        = `{"code":12,"message":"method not allowed","details":[]}`
	logoutSessionOnPathBody = `{"code":9,"message":"this path ends access tokens; end the browser session with POST /iam/v1/auth/logout","details":[]}`
)

// presentation — одна форма предъявления удостоверения, которую путь принимает.
type presentation struct {
	name  string
	build func() *http.Request
}

func acceptedPresentations() []presentation {
	header := func(scheme string) func() *http.Request {
		return func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			r.Header.Set("Authorization", scheme+" tok-presented")
			return r
		}
	}
	return []presentation{
		{"Authorization: Bearer", header("Bearer")},
		{"Authorization: DPoP", header("DPoP")},
		{"форма token (RFC 7009)", func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/oauth/logout",
				strings.NewReader(url.Values{"token": {"tok-presented"}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return r
		}},
	}
}

func liveCaller() *fakeVerifier {
	return &fakeVerifier{caller: &handler.VerifiedCaller{Subject: "usr-00000000000000out", JTI: "jti-out-live"}}
}

func newOutcomeHandler(t *testing.T, cfg handler.LogoutHandlerConfig) *handler.LogoutHandler {
	t.Helper()
	cfg.Logger = newLogger()
	cfg.CallBudget = time.Second
	h, err := handler.NewLogoutHandler(cfg)
	require.NoError(t, err)
	return h
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// requireOutcome — статус, тело дословно, форма и ОТСУТСТВИЕ гашения носителя.
func requireOutcome(t *testing.T, rec *httptest.ResponseRecorder, status int, body, where string) {
	t.Helper()
	require.Equalf(t, status, rec.Code, "%s: статус; тело %s", where, rec.Body.String())
	require.Equalf(t, body, rec.Body.String(), "%s: тело не то дословно", where)
	require.Equalf(t, "application/json", rec.Header().Get("Content-Type"), "%s: Content-Type", where)
	require.Emptyf(t, rec.Result().Header.Values("Set-Cookie"),
		"%s: выход пути токенов браузерную сессию не трогает — гашение печенья без её конца на сервере есть «вышли» при живой сессии", where)
}

// TestLogout_2996_OutcomeIsTheLoginLaneOutcomeOnEveryAcceptedPresentation — на
// каждом принимаемом предъявлении: отзыв ответил → `200 {}`; отзыв не ответил →
// `503` полосы входа, и «вышли» не сказано.
func TestLogout_2996_OutcomeIsTheLoginLaneOutcomeOnEveryAcceptedPresentation(t *testing.T) {
	for _, p := range acceptedPresentations() {
		t.Run(p.name, func(t *testing.T) {
			rev := &recordingRevocations{}
			h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller(), Revocations: rev})
			requireOutcome(t, serve(h, p.build()), http.StatusOK, logoutDoneBody, p.name+", отзыв ответил")
			require.EqualValues(t, 1, rev.calls.Load(), "отзыв зовётся ровно один раз")
			require.Equal(t, "jti-out-live", rev.last.GetTokenJti())
			require.Equal(t, "usr-00000000000000out", rev.last.GetUserId())

			// Близнец отличается одним фактом: отзыв не ответил.
			failing := &recordingRevocations{err: errors.New("rpc error: code = Unavailable desc = dial kaname-internal:9091")}
			h = newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller(), Revocations: failing})
			rec := serve(h, p.build())
			requireOutcome(t, rec, http.StatusServiceUnavailable, logoutNotPerformedBody, p.name+", отзыв не ответил")
			require.NotContains(t, rec.Body.String(), "kaname", "текст отказа наружу не называет внутреннюю службу (kacho#3029)")
		})
	}
}

// TestLogout_2959_NoPathSaysLoggedOutWhileTheServerStillHonoursTheCarrier — выход
// либо выполнен на сервере, либо отказ. Три места, где обработчик отвечал «вышли»,
// не погасив ничего на сервере.
func TestLogout_2959_NoPathSaysLoggedOutWhileTheServerStillHonoursTheCarrier(t *testing.T) {
	t.Run("печенье нашей сессии на пути токенов", func(t *testing.T) {
		rev := &recordingRevocations{}
		h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller(), Revocations: rev})
		r := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
		r.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "opaque-session"})
		requireOutcome(t, serve(h, r), http.StatusBadRequest, logoutSessionOnPathBody, "печенье без токена")
		require.EqualValues(t, 0, rev.calls.Load(), "отказ ничего не отзывает")

		// И с токеном: путь, получивший носитель, которого он не гасит, не делает вид,
		// что вышли оба.
		r = acceptedPresentations()[0].build()
		r.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "opaque-session"})
		requireOutcome(t, serve(h, r), http.StatusBadRequest, logoutSessionOnPathBody, "печенье с токеном")
		require.EqualValues(t, 0, rev.calls.Load(), "отказ ничего не отзывает")
	})
	t.Run("токен предъявлен, проверяющего нет", func(t *testing.T) {
		rev := &recordingRevocations{}
		h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Revocations: rev})
		requireOutcome(t, serve(h, acceptedPresentations()[0].build()),
			http.StatusServiceUnavailable, logoutNotPerformedBody, "без проверяющего")
		require.EqualValues(t, 0, rev.calls.Load())
	})
	t.Run("токен проверен, отзыва нет", func(t *testing.T) {
		h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller()})
		requireOutcome(t, serve(h, acceptedPresentations()[0].build()),
			http.StatusServiceUnavailable, logoutNotPerformedBody, "без отзыва")
	})
	t.Run("близнец: ни носителя, ни токена", func(t *testing.T) {
		rev := &recordingRevocations{}
		h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller(), Revocations: rev})
		requireOutcome(t, serve(h, httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)),
			http.StatusOK, logoutDoneBody, "выход без носителя (Ф3-18)")
		require.EqualValues(t, 0, rev.calls.Load())
	})
}

// TestLogout_2956_524_EveryRefusalOfThePathIsAStatus — отказы пути в форме
// `google.rpc.Status`; неверный метод — `405`, код `12`, `Allow: POST`.
func TestLogout_2956_524_EveryRefusalOfThePathIsAStatus(t *testing.T) {
	h := newOutcomeHandler(t, handler.LogoutHandlerConfig{Verifier: liveCaller(), Revocations: &recordingRevocations{}})
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := serve(h, httptest.NewRequest(m, "/oauth/logout", nil))
		requireOutcome(t, rec, http.StatusMethodNotAllowed, logoutMethodBody, m)
		require.Equal(t, http.MethodPost, rec.Header().Get("Allow"), m)
	}
	// Отказ удостоверению — единый отказ края (KA1, Р2), тоже Status.
	refused := newOutcomeHandler(t, handler.LogoutHandlerConfig{
		Verifier: &fakeVerifier{err: errors.New("refused")}, Revocations: &recordingRevocations{}})
	rec := serve(refused, acceptedPresentations()[0].build())
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, ka1RefusalBody, strings.TrimSpace(rec.Body.String()))
}

// TestLogout_2996_HeaderNamesTheBodiesTheHandlerWrites — шапка обработчика
// называет фактические тела ответа: каждое тело, которое проба выше утверждает,
// процитировано в doc-комментарии `LogoutHandler`, и прежнего `ok:true` там нет.
func TestLogout_2996_HeaderNamesTheBodiesTheHandlerWrites(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "logout_handler.go", nil, parser.ParseComments)
	require.NoError(t, err)
	var doc string
	ast.Inspect(f, func(n ast.Node) bool {
		if g, ok := n.(*ast.GenDecl); ok && g.Tok == token.TYPE {
			for _, s := range g.Specs {
				if ts := s.(*ast.TypeSpec); ts.Name.Name == "LogoutHandler" && g.Doc != nil {
					doc = g.Doc.Text()
				}
			}
		}
		return true
	})
	require.NotEmpty(t, doc, "doc-комментарий LogoutHandler не найден — судить нечего")
	for _, body := range []string{logoutDoneBody, logoutNotPerformedBody, logoutMethodBody, logoutSessionOnPathBody} {
		require.Containsf(t, doc, "`"+body+"`", "шапка обработчика не называет тело %s", body)
	}
	require.NotContains(t, doc, "warnings", "шапка обещает тело, которого обработчик не пишет")
	require.NotContains(t, doc, "ok:true", "шапка обещает тело, которого обработчик не пишет")
}
