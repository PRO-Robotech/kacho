// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

type recordingRevocations struct {
	calls atomic.Int32
	last  *iamv1.RevokeRequest
	err   error
}

func (r *recordingRevocations) Revoke(_ context.Context, in *iamv1.RevokeRequest) error {
	r.calls.Add(1)
	r.last = in
	return r.err
}

// fakeVerifier stands in for the gateway JWKS access-token verifier. It returns
// a fixed caller identity, or an error to simulate an invalid/expired token.
type fakeVerifier struct {
	caller *handler.VerifiedCaller
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (*handler.VerifiedCaller, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.caller, nil
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLogout_POSTOnly(t *testing.T) {
	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second, Logger: newLogger()})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/oauth/logout", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestLogout_NeverEndsTheBrowserSessionCarrier — путь токенов печенье сессии не
// гасит ни на одном исходе (kacho#2959): гашение у клиента без конца сессии на
// сервере — «вышли» при живой сессии. Гашение остаётся у отказа полосы личности
// и у выхода службы (`POST /iam/v1/auth/logout`).
func TestLogout_NeverEndsTheBrowserSessionCarrier(t *testing.T) {
	ok := &recordingRevocations{}
	down := &recordingRevocations{err: errors.New("unreachable")}
	live := &fakeVerifier{caller: &handler.VerifiedCaller{Subject: "usr", JTI: "jti"}}
	cases := map[string]handler.LogoutHandlerConfig{
		"выход выполнен":     {Verifier: live, Revocations: ok},
		"выход не выполнен":  {Verifier: live, Revocations: down},
		"отказ предъявителю": {Verifier: &fakeVerifier{err: errors.New("refused")}, Revocations: ok},
	}
	for name, cfg := range cases {
		cfg.Logger, cfg.CallBudget = newLogger(), time.Second
		h, err := handler.NewLogoutHandler(cfg)
		require.NoError(t, err)
		for _, withCookie := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			req.Header.Set("Authorization", "Bearer valid.access.token")
			if withCookie {
				req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "opaque"})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Emptyf(t, rec.Result().Header.Values("Set-Cookie"), "%s, печенье %v: путь токенов выдал гашение", name, withCookie)
		}
	}
}

// TestLogout_RevokesOwnSubjectFromToken_IgnoresClientSubject — with a validated
// token the handler revokes the caller's own session, derived from the token's
// sub/jti, and IGNORES an attacker-supplied `subject`/`token_jti` in the body.
func TestLogout_RevokesOwnSubjectFromToken_IgnoresClientSubject(t *testing.T) {
	rev := &recordingRevocations{}
	h, _ := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
		Verifier: &fakeVerifier{caller: &handler.VerifiedCaller{
			Subject: "usr_alice_acc_a1b2",
			JTI:     "01HZQ8M5J7QTAEXAMPLEUUIDV7",
		}},
	})
	form := url.Values{
		// Attacker-supplied targets — must be ignored in favour of the token.
		"subject":    {"usr_victim_acc_zzzz"},
		"token_jti":  {"attacker-chosen-jti"},
		"revoke_all": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "DPoP some-access-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(1), rev.calls.Load())
	require.NotNil(t, rev.last)
	assert.Equal(t, "usr_alice_acc_a1b2", rev.last.UserId, "subject must come from validated token, not request body")
	assert.Equal(t, "01HZQ8M5J7QTAEXAMPLEUUIDV7", rev.last.TokenJti, "jti must come from validated token, not request body")
	assert.Equal(t, "user-logout", rev.last.Reason)
	assert.True(t, rev.last.RevokeAllUserTokens)
}

// TestLogout_InvalidToken_401 — a presented token that fails verification is a
// hard 401, and no revocation is attempted (no silent fallthrough to the body).
func TestLogout_InvalidToken_401(t *testing.T) {
	rev := &recordingRevocations{}
	h, _ := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
		Verifier:    &fakeVerifier{err: errors.New("bad signature")},
	})
	form := url.Values{"subject": {"usr_victim"}, "revoke_all": {"true"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer garbage.token.sig")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, int32(0), rev.calls.Load())
}

// TestLogout_RevocationFailure_IsNotPerformed — отзыв не ответил: выход «не
// выполнен» (`503`, Ф3 Р4), причина ошибки вызова наружу не уходит (kacho#3029).
func TestLogout_RevocationFailure_IsNotPerformed(t *testing.T) {
	rev := &recordingRevocations{err: errors.New("rpc error: code = Unavailable desc = iam unreachable")}
	h, _ := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
		Verifier:    &fakeVerifier{caller: &handler.VerifiedCaller{Subject: "usr", JTI: "jti"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
	req.Header.Set("Authorization", "Bearer valid.access.token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body, _ := io.ReadAll(rec.Result().Body)
	assert.NotContains(t, string(body), "iam unreachable")
	assert.NotContains(t, string(body), "warnings")
}

// TestLogout_MakesNoOutboundCallBeyondOurRecord — выход говорит с нашей службой
// доступа и ни с кем больше (#2734).
//
// Прежде у выхода был второй вызов — снятие сессии на стороне прежнего
// поставщика по его административному адресу. Поставщик снят, и у обработчика
// нет ни адреса, ни клиента, ни секрета для такого вызова: конфигурация
// обработчика их не несёт. Здесь это наблюдается исходом: запрос отзыва ушёл в
// нашу запись ровно один раз, ответ — `200 {}`.
func TestLogout_MakesNoOutboundCallBeyondOurRecord(t *testing.T) {
	rev := &recordingRevocations{}
	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
		Verifier:    &fakeVerifier{caller: &handler.VerifiedCaller{Subject: "usr_a", JTI: "jti-a"}},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
	req.Header.Set("Authorization", "Bearer valid.access.token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(1), rev.calls.Load(), "отзыв обязан уйти в нашу запись ровно один раз")
	body, _ := io.ReadAll(rec.Result().Body)
	assert.Equal(t, "{}", string(body), "успешный выход — `200 {}`, как у полосы входа")
}

func TestLogout_NoSubject_NoRevocationCall(t *testing.T) {
	rev := &recordingRevocations{}
	h, _ := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int32(0), rev.calls.Load())
}

func TestLogout_Construction_RequiresLogger(t *testing.T) {
	_, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second})
	require.Error(t, err)
}

// TestLogout_UnauthenticatedRevokeRejected — an unauthenticated caller supplying
// an arbitrary victim `subject` must NOT be able to revoke that user's sessions.
// Without a validated access token the endpoint must refuse the server-side
// revocation (401) and never touch iam.
func TestLogout_UnauthenticatedRevokeRejected(t *testing.T) {
	rev := &recordingRevocations{}
	h, _ := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger:      newLogger(),
		Revocations: rev,
		// No Verifier wired ⇒ no credential can be authenticated ⇒ fail closed.
	})
	form := url.Values{
		"subject":    {"usr_victim_acc_zzzz"},
		"revoke_all": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "unauth revoke of arbitrary subject must be 401")
	assert.Equal(t, int32(0), rev.calls.Load(), "must not revoke another user's tokens without auth")
}
