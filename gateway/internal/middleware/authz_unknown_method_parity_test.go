// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// kacho#3177: промах каталога у аутентифицированного отвечается ТЕМ ЖЕ
// ответом, что маршрут, который внешний слушатель не ведёт (proxy refuseRoute
// для Internal*). Разойдись они хоть в одном байте — по ответу отличался бы
// скрытый метод от несуществующего, и различимый отказ стал бы перечислителем
// Internal*. Сверяются два производителя на одном и том же методе: код,
// текст и детали.
func TestAuthz_CatalogMissAnswersExactlyLikeTheRefusedInternalRoute(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalNetworkService/Get"
	require.True(t, proxy.IsInternalRoute(method), "предпосылка: метод — Internal*")

	next := func(context.Context, any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: method}

	_, routeErr := proxy.UnaryRefuseInternalRoute()(context.Background(), nil, info, next)
	require.Error(t, routeErr)
	route, _ := status.FromError(routeErr)

	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, buildCatalog(t, acbGetEntry), checker)
	_, missErr := mw.Unary()(withTokenMD("usr_x", "user"), nil, info, next)
	require.Error(t, missErr)
	miss, _ := status.FromError(missErr)

	assert.Equal(t, route.Code(), miss.Code(), "код промаха каталога = код неведомого маршрута")
	assert.Equal(t, route.Message(), miss.Message(), "текст промаха каталога = текст неведомого маршрута")
	assert.Equal(t, len(route.Details()), len(miss.Details()), "деталей у обоих ответов поровну")
	assert.Zero(t, checker.calls.Load(), "промах каталога Check не зовёт")
}
