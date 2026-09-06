// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// Аноним не аутентифицирован.
//
// Два сцепленных свойства держат дыру открытой:
//
//  1. В режиме `production` (дефолт кода И чарта) пустой Bearer не отвергается —
//     запросу выдаётся личность «система/аноним».
//  2. Эта личность ИМЕНОВАНА, поэтому извлечение субъекта на ней срабатывает
//     (через диагностическую запасную ветку по `sub`), и гейт «не
//     аутентифицирован» не срабатывает — ни на каталогизированном RPC, ни на
//     RPC, освобождённом от проверки прав.
//
// Замки — на наблюдаемом: запрос без credential'а не проходит authN, а
// аутентифицированный вызывающий БЕЗ выдач по-прежнему проходит (это разные
// вещи: «неизвестно кто» ≠ «известно кто, но без прав»; на втором держится
// публичное чтение географии).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// anonymousMarkerCtx — ровно то, что auth-интерсептор кладёт в incoming
// metadata запросу без Bearer'а (см. injectAnonymous).
func anonymousMarkerCtx() context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		principalmeta.MetaPrincipalType:    "system",
		principalmeta.MetaPrincipalID:      "anonymous",
		principalmeta.MetaPrincipalDisplay: "",
	}))
}

// TestAuth_Production_NoBearer_Unauthenticated — режим production обязан
// отвергать запрос без credential'а, а не выдавать ему личность анонима.
func TestAuth_Production_NoBearer_Unauthenticated(t *testing.T) {
	auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "secret", &fakeLookup{}, authTestLogger())
	called := false
	handler := func(_ context.Context, _ any) (any, error) {
		called = true
		return nil, nil
	}
	_, err := auth.Unary()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/test/Method"}, handler)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code(),
		"запрос без Bearer'а в production обязан получить UNAUTHENTICATED(16)")
	assert.False(t, called, "handler не должен вызываться для запроса без credential'а")
}

// TestSubjectExtractor_AnonymousMarker_NotASubject — именованный маркер
// анонимности не является субъектом. Пока он им является, любой гейт вида
// «субъект извлёкся ⇒ аутентифицирован» проходит на анонимном запросе.
func TestSubjectExtractor_AnonymousMarker_NotASubject(t *testing.T) {
	e := middleware.NewSubjectExtractor(true)
	subj, ok := e.Extract(&middleware.VerifiedToken{
		Subject: "anonymous",
		ExtClaims: map[string]any{
			"kacho_principal_type": "system",
			"kacho_principal_id":   "anonymous",
		},
	})
	assert.False(t, ok, "маркер анонимности не должен резолвиться в субъект")
	assert.Empty(t, subj.FGA, "на маркере анонимности не должно возникать FGA-субъекта")
}

// TestAuthz_GRPC_AnonymousMarker_CataloguedRPC_Unauthenticated — на
// каталогизированном RPC анонимный запрос обязан получить UNAUTHENTICATED и
// НИКОГДА не доходить до проверки прав.
func TestAuthz_GRPC_AnonymousMarker_CataloguedRPC_Unauthenticated(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, createEntry), checker)
	reached := false
	_, err := mw.Unary()(anonymousMarkerCtx(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Create"},
		func(context.Context, any) (any, error) { reached = true; return "ok", nil })

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code(),
		"именованный аноним обязан читаться как отсутствие credential'а")
	assert.False(t, reached, "handler не должен вызываться на анонимном запросе")
	assert.Zero(t, checker.calls.Load(), "проверка прав на анонимном запросе не запускается")
}

// TestAuthz_GRPC_AnonymousMarker_ExemptRPC_Unauthenticated — RPC, освобождённый
// от проверки ПРАВ, не освобождён от АУТЕНТИФИКАЦИИ.
func TestAuthz_GRPC_AnonymousMarker_ExemptRPC_Unauthenticated(t *testing.T) {
	checker := &fakeChecker{allowed: false}
	exemptListEntry := `{"fqn":"kacho.cloud.geo.v1.ZoneService/List","permission":"<exempt>"}`
	mw := buildAuthzMiddleware(t, buildCatalog(t, exemptListEntry), checker)
	reached := false
	_, err := mw.Unary()(anonymousMarkerCtx(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.geo.v1.ZoneService/List"},
		func(context.Context, any) (any, error) { reached = true; return "ok", nil })

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code(),
		"освобождение от проверки прав не освобождает от аутентификации")
	assert.False(t, reached, "handler не должен вызываться на анонимном запросе")
}

// TestAuthz_GRPC_AuthenticatedWithoutGrants_ExemptRPC_Allowed — обратная
// сторона того же решения: аутентифицированный вызывающий БЕЗ единой выдачи
// проходит освобождённый RPC. Это законная безымянная поверхность (публичное
// чтение географии), и сужение анонимности её ломать не должно.
func TestAuthz_GRPC_AuthenticatedWithoutGrants_ExemptRPC_Allowed(t *testing.T) {
	checker := &fakeChecker{allowed: false}
	exemptListEntry := `{"fqn":"kacho.cloud.geo.v1.ZoneService/List","permission":"<exempt>"}`
	mw := buildAuthzMiddleware(t, buildCatalog(t, exemptListEntry), checker)
	reached := false
	_, err := mw.Unary()(withTokenMD("usr-nogrants", "user"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.geo.v1.ZoneService/List"},
		func(context.Context, any) (any, error) { reached = true; return "ok", nil })

	require.NoError(t, err)
	assert.True(t, reached, "аутентифицированный вызывающий без выдач обязан проходить освобождённый RPC")
}
