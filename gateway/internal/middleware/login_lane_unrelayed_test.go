// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// kacho#3178 — путь под приставкой полосы входа, которого нет в перечне
// ретрансляции, получает через край НАЗВАННЫЙ отказ: 404, код 5 и
// фиксированный текст края — одинаковый для любого такого пути (опечатка и
// «эта версия края путь не несёт» не различаются, и край не перечисляет пути
// службы), для анонимного и для аутентифицированного, без эха пути и без
// Set-Cookie; до обработчика (и до службы) запрос не доходит. Близнец —
// путь из перечня: проходит к обработчику, как прежде.
func TestLoginLaneUnrelayedPathGetsTheEdgesNamedRefusal(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, acbGetEntry), checker)
	reached := 0
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))
	serve := func(path string, authed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path+"?state=s3178", nil)
		if authed {
			r.Header.Set("X-Kacho-Principal-Id", "usr_x")
			r.Header.Set("X-Kacho-Principal-Type", "user")
			r.Header.Set("X-Kacho-Token-Acr", "2")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	want := `{"code":5,"message":"` + middleware.LoginLaneUnrelayedMessage + `"}` + "\n"
	for _, path := range []string{"/iam/v1/auth/no-such-verb", "/iam/v1/auth/login/extra", "/iam/v1/auth/"} {
		for _, authed := range []bool{false, true} {
			w := serve(path, authed)
			assert.Equal(t, http.StatusNotFound, w.Code, "%s authed=%t", path, authed)
			assert.Equal(t, want, w.Body.String(), "%s authed=%t", path, authed)
			assert.Empty(t, w.Header().Values("Set-Cookie"), "%s authed=%t", path, authed)
		}
	}
	require.Zero(t, reached, "путь вне перечня дошёл до обработчика")
	assert.Zero(t, checker.calls.Load())

	// Законный близнец: путь перечня проходит к обработчику.
	w := serve(middleware.LoginLanePathLogin, false)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, reached)
}

// Доклад об отказе пути вне перечня — окном, а не строкой на запрос: путь
// выбирает вызывающий, и строка на каждый такой запрос отдала бы ему объём
// журнала края. Первый отказ докладывается всегда, повторы в окне — итогом.
func TestLoginLaneUnrelayedRefusalIsReportedOncePerWindow(t *testing.T) {
	var buf bytes.Buffer
	mw := buildAuthzMiddleware(t, buildCatalog(t, acbGetEntry), &fakeChecker{allowed: true},
		func(c *middleware.AuthzMiddlewareConfig) { c.Logger = slog.New(slog.NewTextHandler(&buf, nil)) })
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/iam/v1/auth/no-such-verb", nil))
	}
	got := strings.Count(buf.String(), "path is not in the relay declaration")
	assert.Equal(t, 1, got, "строк доклада на 5 отказов в одном окне:\n%s", buf.String())
	assert.Contains(t, buf.String(), "login_lane_unrelayed_total=1", "первый доклад называет итог")
}
