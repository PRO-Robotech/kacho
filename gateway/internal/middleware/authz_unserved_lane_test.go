// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_unserved_lane_test.go — отказ «этот слушатель такого не обслуживает»
// попадает в СВОЮ полосу.
//
// Тот же довод, что у соседнего `authz_admission_lane_test.go` (#798), только с
// другого знака. Там разводили ДОПУСКИ: «пропущен, потому что путь публичен» и
// «разрешён, потому что права есть» — разные факты. Здесь разводится ОТКАЗ:
// «отвергнут, потому что этот слушатель такого не обслуживает» и «отвергнут,
// потому что модель сказала нет» — тоже разные факты, и слить их значило бы
// записать перебор внешней поверхности в решения о правах.
//
// Проба утверждает РОСТ полосы, а не наличие серии: серия существует и стоит
// нулём при любом состоянии, поэтому её наличие не отличает работающий счётчик
// от неработающего.
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestUnservedRefusalGetsItsOwnLane — укрытый отказ растит свою полосу и НЕ
// растит полосу решений модели.
func TestUnservedRefusalGetsItsOwnLane(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	rr := middleware.NewRestRouter()
	mw := buildAuthzMiddleware(t, buildCatalog(t, internalCheckExempt), checker,
		func(c *middleware.AuthzMiddlewareConfig) {
			c.RestRouter = rr
			c.Resources = middleware.NewResourceExtractor(rr.PathTemplates())
		})

	served := false
	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	// Внешнее происхождение — умолчание fail-closed: маркера нет.
	before := mw.Metrics().Counts()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/iam/v1/internal/iam:check", nil))
	after := mw.Metrics().Counts()

	require.False(t, served, "необслуживаемый путь не смеет дойти до обработчика")
	require.Equal(t, http.StatusNotFound, rec.Code, "ответ — тот же, что на любом промахе")

	require.Equal(t, before.Unserved+1, after.Unserved,
		"отказ «этот слушатель такого не обслуживает» обязан вырастить СВОЮ полосу")
	require.Equal(t, before.Denied, after.Denied,
		"он не является решением «отказано»: модель не спрашивали, и слить эти два факта "+
			"значило бы записать перебор внешней поверхности в решения о правах")
	require.Equal(t, before.Allowed, after.Allowed, "и решением «разрешено» тоже не является")
	require.Zero(t, checker.calls.Load(), "модель прав не смеет быть спрошена вовсе")
}

// TestServedPathDoesNotGrowTheUnservedLane — законный близнец: путь, который
// слушатель ОБСЛУЖИВАЕТ, эту полосу не трогает. Без него «полоса выросла»
// не отличалось бы от полосы, растущей на всём подряд.
func TestServedPathDoesNotGrowTheUnservedLane(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	rr := middleware.NewRestRouter()
	mw := buildAuthzMiddleware(t, buildCatalog(t, getEntry), checker,
		func(c *middleware.AuthzMiddlewareConfig) {
			c.RestRouter = rr
			c.Resources = middleware.NewResourceExtractor(rr.PathTemplates())
		})

	h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	before := mw.Metrics().Counts()
	r := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks/net_probe0000000001", nil)
	r.Header.Set("X-Kacho-Principal-Id", "usr_probe")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	h.ServeHTTP(httptest.NewRecorder(), r)
	after := mw.Metrics().Counts()

	require.Equal(t, before.Unserved, after.Unserved,
		"обслуживаемый путь не смеет растить полосу необслуживаемых")
}
