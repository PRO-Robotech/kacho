// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// password_change_required_test.go — Ф3-23: отказ по требованию сменить
// пароль стоит В РЕШЕНИИ ПО КАТАЛОГУ ПРАВ (Р8), на путях с записью каталога, до
// вопроса к модели; пути без записи каталога проходят.
package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// underSession — запрос, каким его оставляет полоса личности после НАШЕЙ
// сессии: личность и уровень выставлены; требование — контекстом.
func underSession(method, path string, pcr bool) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-r")
	r.Header.Set(principalmeta.HeaderTokenACR, "1")
	if pcr {
		r = r.WithContext(middleware.WithPasswordChangeRequired(r.Context()))
	}
	return r
}

func TestPasswordChangeRequired_F3_23_CataloguedPathIsRefusedBeforeTheModelIsAsked(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	router := &fakeRestRouter{m: map[string]string{
		"GET /vpc/v1/networks/net-1": "kacho.cloud.vpc.v1.NetworkService/Get",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, getEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	reached := 0
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, underSession(http.MethodGet, "/vpc/v1/networks/net-1", true))
	require.Equal(t, http.StatusForbidden, rec.Code, "глагол платформы под требованием обязан отвергаться 403: %s", rec.Body.String())
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Type   string `json:"@type"`
			Reason string `json:"reason"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 7, body.Code)
	require.Equal(t, "password change required before any other action", body.Message, "текст обязан называть следующий шаг")
	var reasons []string
	for _, d := range body.Details {
		if d.Reason != "" {
			reasons = append(reasons, d.Reason)
		}
	}
	require.Equal(t, []string{"PASSWORD_CHANGE_REQUIRED"}, reasons, "клиент ветвится по reason, не по прозе")
	require.Equal(t, int64(0), checker.calls.Load(), "модель прав не спрашивалась — отказ стоит ДО вопроса")
	require.Equal(t, 0, reached)
	require.Equal(t, uint64(1), mw.Metrics().Counts().PasswordChangeRequired, "клетка отказа по требованию")

	// Положительный контроль: та же сессия БЕЗ поля проходит (модель спрошена,
	// разрешила). Различие с близнецом одно — поле.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, underSession(http.MethodGet, "/vpc/v1/networks/net-1", false))
	require.Equal(t, http.StatusOK, rec.Code, "%s", rec.Body.String())
	require.Equal(t, 1, reached)
}

// Множество проходящих — ровно перечень путей без записи каталога: «кто я»,
// четыре глагола формы, пробы живости, выход полосы токенов.
func TestPasswordChangeRequired_F3_23_PathsWithoutACatalogEntryPass(t *testing.T) {
	mw := buildAuthzMiddleware(t, buildCatalog(t, getEntry), &fakeChecker{allowed: true})
	reached := map[string]int{}
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached[r.URL.Path]++; w.WriteHeader(http.StatusOK) }))
	paths := []string{"/iam/v1/auth/me", "/healthz", "/readyz", "/oauth/logout"}
	for _, rt := range middleware.LoginLaneRoutes() {
		paths = append(paths, rt.Path)
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, underSession(http.MethodGet, p, true))
		if rec.Code != http.StatusOK || reached[p] != 1 {
			t.Fatalf("%s под требованием обязан проходить (путь без записи каталога): %d", p, rec.Code)
		}
	}
	t.Logf("перепись: путей без записи каталога %d · прошли под требованием %d", len(paths), len(reached))
}
