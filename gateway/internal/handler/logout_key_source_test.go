// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Источник проверочных ключей не ответил — это не приговор предъявителю, а сбой
// зависимости (#1194): проверяющий НЕ РЕШИЛ, годен ли токен. Выход в этом
// случае «не выполнен» (`503`, Ф3 Р4; kacho#2996 «на каждом предъявлении»), а
// не «предъявитель не принят» (`401`): ответ `401` велел бы клиенту выбросить
// годный токен, тогда как на сервере он по-прежнему жив.
//
// Близнец отличается одним фактом — проверяющий РЕШИЛ, что токен негоден: тогда
// единый отказ края `401`.

func logoutPresentations(tok string) map[string]func() *http.Request {
	return map[string]func() *http.Request{
		"Authorization: Bearer": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			r.Header.Set("Authorization", "Bearer "+tok)
			return r
		},
		"Authorization: DPoP": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			r.Header.Set("Authorization", "DPoP "+tok)
			return r
		},
		"форма token": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(url.Values{"token": {tok}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return r
		},
	}
}

func TestLogout_KeySourceUnanswered_IsNotPerformedOnEveryPresentation(t *testing.T) {
	undecided := map[string]error{
		"источник не ответил":     fmt.Errorf("%w: dial tcp: connection refused", middleware.ErrJWKSUnreachable),
		"источник ответил не тем": fmt.Errorf("%w: status=502", middleware.ErrJWKSFetchFailed),
	}
	for why, verr := range undecided {
		for name, req := range logoutPresentations("tok") {
			t.Run(why+"/"+name, func(t *testing.T) {
				rev := &recordingRevocations{}
				h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
					Logger: newLogger(), Verifier: &fakeVerifier{err: verr}, Revocations: rev, CallBudget: time.Second,
				})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req())
				assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
				assert.Equal(t, `{"code":14,"message":"logout not performed; try again later","details":[]}`, rec.Body.String())
				assert.Zero(t, rev.calls.Load(), "предъявитель не установлен — отзывать нечего")
				assert.NotContains(t, rec.Body.String(), "connection refused", "причина — в журнал, не в ответ")
			})
		}
	}
}

// Близнец: проверяющий решил — токен негоден (ключ, которого живой набор не
// публикует, — тоже «негоден», а не «источник молчит»).
func TestLogout_TokenRefusedByAnsweringKeySource_IsTheEdgeRefusal(t *testing.T) {
	refused := map[string]error{
		"подпись":            errors.New("signature mismatch"),
		"ключа нет в наборе": fmt.Errorf("%w: kid=x", middleware.ErrKeyNotFound),
	}
	for why, verr := range refused {
		t.Run(why, func(t *testing.T) {
			rev := &recordingRevocations{}
			h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
				Logger: newLogger(), Verifier: &fakeVerifier{err: verr}, Revocations: rev, CallBudget: time.Second,
			})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, logoutPresentations("tok")["Authorization: Bearer"]())
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
			assert.Zero(t, rev.calls.Load())
		})
	}
}

// deadlineVerifier запоминает срок, с которым его спросили.
type deadlineVerifier struct{ left time.Duration }

func (d *deadlineVerifier) Verify(ctx context.Context, _ string) (*handler.VerifiedCaller, error) {
	dl, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("asked without a deadline")
	}
	d.left = time.Until(dl)
	return &handler.VerifiedCaller{Subject: "usr", JTI: "jti"}, nil
}

// Проверка предъявителя ограничена той же ручкой бюджета, что и отзыв
// (KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET, приёмка KA1, Р4), а не константой:
// срок, через который клиент узнаёт «не выполнен», выбирает профиль.
func TestLogout_VerificationIsBoundedByTheCallBudget(t *testing.T) {
	const budget = 150 * time.Millisecond
	v := &deadlineVerifier{}
	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
		Logger: newLogger(), Verifier: v, Revocations: &recordingRevocations{}, CallBudget: budget,
	})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, logoutPresentations("tok")["Authorization: Bearer"]())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Greater(t, v.left, time.Duration(0))
	assert.LessOrEqual(t, v.left, budget, "проверке дан срок %s при бюджете %s", v.left, budget)
}
