// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
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

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
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
				assert.Empty(t, rec.Result().Header.Values("Set-Cookie"), "носитель не гасится")
				assert.NotContains(t, rec.Body.String(), "connection refused", "причина — в журнал, не в ответ")
				assert.NotContains(t, rec.Body.String(), "status=502", "причина — в журнал, не в ответ")
			})
		}
	}
}

// Близнец (KA1-36 строки (3), (4)): проверяющий решил — токен негоден (ключ,
// которого живой набор не публикует, — тоже «негоден», а не «источник
// молчит»). На каждом предъявлении отказ — единый отказ края `401` по Р2: тело
// и `WWW-Authenticate` равны KA1-10 (литералы харнесса `ka1stand`, а не
// производитель края), `Set-Cookie` нет, отзыв не зовётся.
func TestLogout_TokenRefusedByAnsweringKeySource_IsTheEdgeRefusal(t *testing.T) {
	refused := map[string]error{
		"подпись":            errors.New("signature mismatch"),
		"ключа нет в наборе": fmt.Errorf("%w: kid=x", middleware.ErrKeyNotFound),
	}
	for why, verr := range refused {
		for name, req := range logoutPresentations("tok") {
			t.Run(why+"/"+name, func(t *testing.T) {
				rev := &recordingRevocations{}
				h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
					Logger: newLogger(), Verifier: &fakeVerifier{err: verr}, Revocations: rev, CallBudget: time.Second,
				})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req())
				assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
				assert.Equal(t, ka1stand.RefusalBody, rec.Body.String())
				assert.Equal(t, ka1stand.RefusalChallenge, rec.Result().Header.Get("WWW-Authenticate"))
				assert.Empty(t, rec.Result().Header.Values("Set-Cookie"), "носитель не гасится")
				assert.Zero(t, rev.calls.Load())
				assert.NotContains(t, rec.Body.String(), verr.Error(), "причина — в журнал, не в ответ")
			})
		}
	}
}
