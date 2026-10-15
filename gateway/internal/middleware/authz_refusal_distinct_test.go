// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// kacho#3177 — три причины, которые край отвечал одним 403 «permission
// denied», различимы по коду: промах каталога (у этой версии края нет записи о
// методе), запрос без обязательного поля скоупа и настоящая нехватка права.
// Три близнеца на одном методе меняют ровно один факт каждый; Check не
// зовётся ни на промахе, ни на пустом поле.

const distinctInviteEntry = `{"fqn":"kaname.cloud.iam.v1.UserService/Invite","permission":"iam.users.invite","required_relation":"editor","scope_extractor":{"object_type":"account","from_request_field":"account_id"},"required_acr_min":"1"}`

const inviteMethod = "/kaname.cloud.iam.v1.UserService/Invite"

func inviteCall(t *testing.T, mw *middleware.AuthzMiddleware, req *iamv1.InviteUserRequest) *status.Status {
	t.Helper()
	ctx := withTokenMD("usr_x", "user")
	_, err := mw.Unary()(ctx, req, &grpc.UnaryServerInfo{FullMethod: inviteMethod},
		func(context.Context, any) (any, error) { return "ok", nil })
	require.Error(t, err)
	st, _ := status.FromError(err)
	return st
}

func TestAuthz_ThreeRefusalsAreDistinguishableByCode(t *testing.T) {
	// Близнец: метод в каталоге, поле скоупа есть, права нет — прежний 403.
	deny := &fakeChecker{allowed: false, reasons: []string{"no path"}}
	st := inviteCall(t, buildAuthzMiddleware(t, buildCatalog(t, distinctInviteEntry), deny),
		&iamv1.InviteUserRequest{AccountId: "acc00000000000003177"})
	assert.Equal(t, codes.PermissionDenied, st.Code(), "нехватка права: %v", st)
	assert.Equal(t, "permission denied: iam.users.invite", st.Message())

	// Промах каталога — NotFound «unknown method», до Check.
	miss := &fakeChecker{allowed: true}
	st = inviteCall(t, buildAuthzMiddleware(t, buildCatalog(t, acbGetEntry), miss),
		&iamv1.InviteUserRequest{AccountId: "acc00000000000003177"})
	assert.Equal(t, codes.NotFound, st.Code(), "промах каталога: %v", st)
	assert.Equal(t, "unknown method: "+inviteMethod, st.Message())
	assert.Empty(t, st.Details(), "промах каталога не несёт причин отказа прав")
	assert.Zero(t, miss.calls.Load())

	// Пустое поле скоупа — InvalidArgument с фиксированным текстом, до Check.
	empty := &fakeChecker{allowed: true}
	st = inviteCall(t, buildAuthzMiddleware(t, buildCatalog(t, distinctInviteEntry), empty),
		&iamv1.InviteUserRequest{})
	assert.Equal(t, codes.InvalidArgument, st.Code(), "пустой account_id: %v", st)
	assert.Equal(t, "Illegal argument account_id: required", st.Message())
	assert.Zero(t, empty.calls.Load(), "пустой объект скоупа не уходит в Check")
}

// REST: промах каталога — 404 {code:5, message:"unknown method: /<fqn>"}.
func TestAuthz_HTTP_CatalogMissIsUnknownMethod(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	router := &fakeRestRouter{m: map[string]string{
		"POST /iam/v1/users:invite": "kaname.cloud.iam.v1.UserService/Invite",
	}}
	mw := buildAuthzMiddleware(t, buildCatalog(t, acbGetEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
		c.RestRouter = router
	})
	h := mw.HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("обработчик не должен исполниться на промахе каталога")
	}))
	r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", nil)
	r.Header.Set("X-Kacho-Principal-Id", "usr_x")
	r.Header.Set("X-Kacho-Principal-Type", "user")
	r.Header.Set("X-Kacho-Token-Acr", "2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(5), body["code"])
	assert.Equal(t, "unknown method: "+inviteMethod, body["message"])
	assert.Zero(t, checker.calls.Load())
}

// REST: users:invite без accountId — 400 {code:3} с фиксированным текстом, до
// Check; близнец с accountId — до Check доходит.
func TestAuthz_HTTP_InviteWithoutAccountIdIsInvalidArgument(t *testing.T) {
	call := func(body string) (*httptest.ResponseRecorder, *fakeChecker) {
		checker := &fakeChecker{allowed: true}
		mw := buildAuthzMiddleware(t, buildCatalog(t, distinctInviteEntry), checker, func(c *middleware.AuthzMiddlewareConfig) {
			c.RestRouter = &fakeRestRouter{m: map[string]string{
				"POST /iam/v1/users:invite": "kaname.cloud.iam.v1.UserService/Invite",
			}}
			c.Resources = middleware.NewResourceExtractor(map[string]string{
				"kaname.cloud.iam.v1.UserService/Invite": "/iam/v1/users:invite",
			})
		})
		h := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		r := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
		r.Header.Set("X-Kacho-Principal-Id", "usr_x")
		r.Header.Set("X-Kacho-Principal-Type", "user")
		r.Header.Set("X-Kacho-Token-Acr", "2")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w, checker
	}
	w, checker := call(`{"email":"a@example.test"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(3), body["code"])
	assert.Equal(t, "Illegal argument account_id: required", body["message"])
	assert.Zero(t, checker.calls.Load())

	w, checker = call(`{"accountId":"acc00000000000003177","email":"a@example.test"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(1), checker.calls.Load())
}
