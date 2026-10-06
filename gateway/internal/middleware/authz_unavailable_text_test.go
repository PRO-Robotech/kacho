// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_unavailable_text_test.go — ОТКАЗ «РЕШЕНИЕ О ПРАВАХ НЕ ПОЛУЧЕНО» НЕ
// НАЗЫВАЕТ ВНУТРЕННЮЮ СЛУЖБУ (kacho#3029). Пара `503` / `14` прежняя (повторить,
// fail-closed); текст — дословно, на REST и на унарном gRPC. Потоковый
// производитель пишет тот же текст той же константой; класс по всему дереву
// края держит гейт `TestEdgeRefusalNamesNoInternalService`.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const authzUndecidedText = "authorization could not be decided; try again later"

func TestAuthz_3029_UndecidedRefusalNamesNoInternalService(t *testing.T) {
	down := &fakeChecker{returnErr: errors.New("dial tcp: lookup kaname-internal: no such host")}
	router := &fakeRestRouter{m: map[string]string{"POST /vpc/v1/networks": "kacho.cloud.vpc.v1.NetworkService/Create"}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, createEntry), down, func(c *middleware.AuthzMiddlewareConfig) { c.RestRouter = router })

	h := mw.HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach handler") }))
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", nil)
	r.Header.Set("X-Kacho-Principal-Id", "usr_x")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, `{"code":14,"message":"`+authzUndecidedText+`","details":[]}`, rec.Body.String())

	_, err := mw.Unary()(withTokenMD("usr_x", "user"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Create"},
		func(context.Context, any) (any, error) { return "ok", nil })
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unavailable, st.Code())
	require.Equal(t, authzUndecidedText, st.Message())
}
