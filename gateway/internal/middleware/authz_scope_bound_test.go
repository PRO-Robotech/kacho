// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
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

// authz_scope_bound_test.go — форма ScopeBound (kacho#2915, замысел NTF-1 §З14).
//
// Объект проверки строки `scope_extractor.bound_to_server` — экземпляр типа
// object_type, к которому ПРОЦЕСС, поднявший сервер, привязал его при подъёме
// (`servicecontract.Bound`). Запрос его не называет, и `from_request_field` у
// строки пуст законно. Край этот сервер не поднимает и привязки не знает: второго
// значения «имени модуля» в корне нет по замыслу. Поэтому единственный честный
// исход края — закрытый отказ. Прежний исход — пустое поле читалось как
// подстановка, и край спрашивал модель о `notification_feed:*`, то есть «о любой
// ленте», — ровно то, что форма ScopeBound заведена запретить.
//
// Строки ниже — дословные строки каталога для `Claim` (до и после правки
// генератора), а не синтетика иной формы.

const (
	boundClaimFQN = "corelib.notify.InternalNotificationFeedService/Claim"
	boundClaimRow = `{"fqn":"` + boundClaimFQN + `","permission":"platform.notification_feed.claim",` +
		`"required_relation":"reader","scope_extractor":{"object_type":"notification_feed",` +
		`"from_request_field":"","bound_to_server":true},"required_acr_min":"1"}`
	// Законный близнец: та же строка, источник идентификатора — поле запроса.
	// Отличается ровно источником идентификатора; край обязан спросить модель.
	requestScopedClaimRow = `{"fqn":"` + boundClaimFQN + `","permission":"platform.notification_feed.claim",` +
		`"required_relation":"reader","scope_extractor":{"object_type":"notification_feed",` +
		`"from_request_field":"subject"},"required_acr_min":"1"}`
)

// TestScopeBound_ExtractorNeverYieldsTheWildcard — извлекатель на строке
// ScopeBound не возвращает подстановку ни из proto, ни из HTTP: пустое
// `from_request_field` здесь означает «идентификатор не из запроса», а не
// «любой объект типа».
func TestScopeBound_ExtractorNeverYieldsTheWildcard(t *testing.T) {
	entry, ok := buildCatalog(t, boundClaimRow).Lookup(boundClaimFQN)
	require.True(t, ok)

	e := middleware.NewResourceExtractor(nil)
	id, resolved := e.ExtractFromProto(&iamv1.AuthorizeCheckRequest{Subject: "user:usr_x"}, entry)
	assert.False(t, id.IsWildcard(), "строка bound_to_server прочитана как подстановка: %q", id)
	assert.False(t, resolved, "у края нет привязки — идентификатор не разрешён, а не разрешён в `*`")

	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	hid, _ := e.ExtractFromHTTP(r, boundClaimFQN, entry)
	assert.False(t, hid.IsWildcard(), "HTTP-путь прочитал строку bound_to_server как подстановку: %q", hid)
}

// TestScopeBound_EdgeRefusesClosedWithoutAskingTheModel — аутентифицированный
// вызывающий, модель, готовая сказать «да» на что угодно, — и отказ без единого
// вопроса к модели: вопрос о `notification_feed:*` не задаётся вовсе.
func TestScopeBound_EdgeRefusesClosedWithoutAskingTheModel(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, boundClaimRow), checker)

	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	_, err := mw.Unary()(withTokenMD("svc_notify", "user"), &iamv1.AuthorizeCheckRequest{Subject: "user:usr_x"},
		&grpc.UnaryServerInfo{FullMethod: "/" + boundClaimFQN}, handler)

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "метод формы ScopeBound не обслуживается краем")
	assert.Equal(t, int64(0), checker.calls.Load(),
		"край не знает привязки и не вправе спрашивать модель о подстановке: %+v", checker.lastInput.Load())
}

// TestScopeBound_TwinWithRequestScopeAsksTheModel — законный близнец: та же
// строка с источником из запроса проходит обычной полосой (один Check), значит
// отказ выше вызван источником ScopeBound, а не FQN, правом или отношением.
func TestScopeBound_TwinWithRequestScopeAsksTheModel(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, requestScopedClaimRow), checker)

	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	_, err := mw.Unary()(withTokenMD("svc_notify", "user"), &iamv1.AuthorizeCheckRequest{Subject: "user:usr_x"},
		&grpc.UnaryServerInfo{FullMethod: "/" + boundClaimFQN}, handler)

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, int64(1), checker.calls.Load())
}
