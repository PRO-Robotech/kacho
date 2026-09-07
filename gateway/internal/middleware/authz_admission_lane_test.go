// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_admission_lane_test.go — КАЖДЫЙ допуск попадает в СВОЮ полосу.
//
// Предмет (#798). Счётчик решений края считал четыре полосы, но только для
// запросов, дошедших до принятия решения. Публичный путь короткозамыкается
// раньше, и его допуск не попадал НИ В ОДНУ полосу — число «решений в секунду»
// было занижено на весь публичный трафик.
//
// Тот же довод («пропущен, потому что путь публичен» и «разрешён, потому что
// права есть» — разные факты) применён последовательно: пять допусков, которые
// раньше сливались в `allow`, разведены по полосам механизма, каждым из которых
// запрос допускается БЕЗ ответа модели прав.
//
// Пробы утверждают РОСТ полосы, а не наличие серии: серия существует и стоит
// нулём при любом состоянии, поэтому её наличие не отличает работающий счётчик
// от неработающего.
package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestPublicHTTPPathGetsItsOwnLane — ядро #798.
func TestPublicHTTPPathGetsItsOwnLane(t *testing.T) {
	mw := buildAuthzMiddleware(t, buildCatalog(t, createEntry), &fakeChecker{allowed: true})
	served := false
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	before := mw.Metrics().Counts()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	after := mw.Metrics().Counts()

	require.True(t, served, "публичный путь обязан быть обслужен")
	require.Equal(t, before.PublicPath+1, after.PublicPath,
		"проход по публичному пути обязан вырастить СВОЮ полосу")
	require.Equal(t, before.Allowed, after.Allowed,
		"он не является решением «разрешено»: права не спрашивались, и смешивать эти "+
			"два факта нельзя — иначе путь, которому в списке не место, будет неотличим "+
			"от законной выдачи")
}

// TestDisabledAuthzIsDistinguishableFromNoTraffic — второе состояние того же
// рода: при выключенной проверке звено пропускает всё, накопитель собран, серии
// стоят нулями — и «проверка выключена» неотличимо от «трафика не было».
func TestDisabledAuthzIsDistinguishableFromNoTraffic(t *testing.T) {
	on := buildAuthzMiddleware(t, buildCatalog(t, createEntry), &fakeChecker{allowed: true})
	require.True(t, on.Metrics().Counts().Enforcing,
		"включённая проверка обязана объявлять себя включённой")

	off := buildAuthzMiddleware(t, buildCatalog(t, createEntry), &fakeChecker{allowed: true},
		func(c *middleware.AuthzMiddlewareConfig) { c.Enabled = false })
	require.False(t, off.Metrics().Counts().Enforcing,
		"выключенная проверка обязана быть отличима от отсутствия трафика — "+
			"нулями на полосах решений эти два состояния не различаются")
}

// TestAdmittedWithoutTheModelDoesNotCountAsAllow — законный близнец: полоса
// `allow` обязана считать ТОЛЬКО ответ владельца прав.
//
// Без этой пробы разведение полос ловило бы форму (появились новые счётчики), а
// не существо (старая полоса перестала вбирать чужое).
func TestAdmittedWithoutTheModelDoesNotCountAsAllow(t *testing.T) {
	t.Run("фиксированный список FQN — своя полоса", func(t *testing.T) {
		// Проверяющий отвечал бы ОТКАЗОМ, если бы его спросили: допуск здесь не
		// решение модели, и полоса обязана это показывать.
		mw := buildAuthzMiddleware(t, buildCatalog(t, createEntry), &fakeChecker{allowed: false})
		before := mw.Metrics().Counts()
		_, err := mw.Unary()(context.Background(), nil,
			&grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"},
			func(context.Context, any) (any, error) { return "ok", nil })
		require.NoError(t, err)
		after := mw.Metrics().Counts()
		require.Equal(t, before.Allowlist+1, after.Allowlist)
		require.Equal(t, before.Allowed, after.Allowed,
			"полоса allow обязана считать только ответ владельца прав")
	})

	t.Run("каталог снял вопрос модели — своя полоса", func(t *testing.T) {
		const exemptEntry = `{"fqn":"kacho.cloud.geo.v1.RegionService/List","permission":"<exempt>","risk_level":"LOW"}`
		mw := buildAuthzMiddleware(t, buildCatalog(t, exemptEntry), &fakeChecker{allowed: false})
		before := mw.Metrics().Counts()
		_, err := mw.Unary()(withTokenMD("usr_a", "user"), nil,
			&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.geo.v1.RegionService/List"},
			func(context.Context, any) (any, error) { return "ok", nil })
		require.NoError(t, err)
		after := mw.Metrics().Counts()
		require.Equal(t, before.Exempt+1, after.Exempt,
			"каталог снял вопрос модели — это СВОЙ факт, а не «права есть»")
		require.Equal(t, before.Allowed, after.Allowed)
	})

	t.Run("ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: ответ владельца прав растит именно allow", func(t *testing.T) {
		checker := &fakeChecker{allowed: true}
		router := &fakeRestRouter{m: map[string]string{
			"GET /iam/v1/accessBindings:listByScope": "kaname.cloud.iam.v1.AccessBindingService/ListByScope",
		}}
		mw := buildAuthzMiddleware(t, buildCatalog(t, listByScopeEntry), checker,
			func(c *middleware.AuthzMiddlewareConfig) { c.RestRouter = router })
		h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

		before := mw.Metrics().Counts()
		r := httptest.NewRequest(http.MethodGet,
			"/iam/v1/accessBindings:listByScope?resourceType=account&resourceId=acc_A", nil)
		r.Header.Set("X-Kacho-Principal-Id", "usr_owner")
		r.Header.Set("X-Kacho-Principal-Type", "user")
		r.Header.Set("X-Kacho-Token-Acr", "2")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		after := mw.Metrics().Counts()

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, before.Allowed+1, after.Allowed,
			"без этого контроля разведение полос могло бы опустошить allow целиком, "+
				"и «полос стало больше» читалось бы как «стало лучше»")
	})
}
