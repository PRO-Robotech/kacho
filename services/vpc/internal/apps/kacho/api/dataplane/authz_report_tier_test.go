// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/authz"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// authz_report_tier_test.go — право ПИСАТЬ отчёт не совпадает с правом его
// ЧИТАТЬ (APPLY-17).
//
// # Предмет
//
// Подтверждение решает, что платформа считает применённым: кто до него
// дотянулся, тот пишет арендатору «применено» на ресурс, которого сеть не
// видела. Право читать поток намерения — другое: оно даёт видеть, а не решать.
// Совпади ярусы, любой, кому позволено смотреть, объявлял бы применённым что
// угодно.
//
// # Почему проба ведёт себя как МОДЕЛЬ, а не как переключатель
//
// Соседние пробы этого пакета гоняют звено решения с ответом «да всем» либо
// «нет всем»: их предмет — попал ли метод в карту и закрывает ли отказ
// обработчик. Здесь предмет другой — РАЗНИЦА между ярусами, и её нельзя увидеть
// на клиенте, который отвечает одинаково на любой вопрос. Поэтому подставная
// модель отвечает по отношению: у принципала есть набор отношений, и «да»
// получает только тот вопрос, который в него попадает.

// tieredDecisionLink — звено решения поверх модели, знающей ЯРУСЫ.
//
// Карта прав берётся та же, что у процесса (`check.PermissionMap`), а не
// собирается литералом: своя копия утверждала бы о себе.
func tieredDecisionLink(t *testing.T, granted map[string]bool, asked *[]string) *authz.Interceptor {
	t.Helper()
	return authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-vpc-test",
		Map:         check.PermissionMap(),
		Client: authz.CheckClientFunc(func(_ context.Context, _, relation, _ string) (bool, error) {
			*asked = append(*asked, relation)
			return granted[relation], nil
		}),
	})
}

// TestReadTierCannotWriteTheApplyReport — читатель потока не пишет отчёт.
func TestReadTierCannotWriteTheApplyReport(t *testing.T) {
	// Принципал уровня чтения: у него есть отношение читателя и нет админского.
	readTier := map[string]bool{"system_viewer": true}

	// ── он НЕ записывает отчёт ───────────────────────────────────────────────
	var asked []string
	intr := tieredDecisionLink(t, readTier, &asked)
	called := false
	_, err := intr.Unary()(
		principalCtx("service_account", "sva_dataplane_reader"),
		struct{}{},
		&grpc.UnaryServerInfo{FullMethod: reportMethod},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	require.Error(t, err, "читатель потока записал подтверждение применения")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "обработчик подтверждения вызван при отказе модели")
	require.Len(t, asked, 1, "решение о доступе не спрашивалось вовсе")
	assert.Equal(t, "system_admin", asked[0],
		"подтверждение спрошено не админским отношением — ярусы совпали")

	// ── ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: тот же принципал открывает поток ─────────────
	// Без него «отказано» относилось бы к принципалу вообще, а не к глаголу, и
	// проба зеленела бы на модели, которая не разрешает ничего.
	asked = nil
	streamCalled := false
	err = intr.Stream()(nil,
		&fakeServerStream{ctx: principalCtx("service_account", "sva_dataplane_reader")},
		&grpc.StreamServerInfo{FullMethod: watchMethod, IsServerStream: true},
		func(any, grpc.ServerStream) error { streamCalled = true; return nil },
	)
	require.NoError(t, err, "читателю потока отказано в чтении потока — отказ выше относится к принципалу, а не к глаголу")
	assert.True(t, streamCalled)
	require.Len(t, asked, 1)
	assert.Equal(t, "system_viewer", asked[0])
}

// TestAdminTierWritesTheApplyReport — админский ярус отчёт записывает.
//
// Вторая половина пары: без неё «читателю отказано» было бы верно и в мире, где
// отчёт не записывает никто.
func TestAdminTierWritesTheApplyReport(t *testing.T) {
	var asked []string
	intr := tieredDecisionLink(t, map[string]bool{"system_admin": true}, &asked)

	called := false
	_, err := intr.Unary()(
		principalCtx("service_account", "sva_dataplane_admin"),
		struct{}{},
		&grpc.UnaryServerInfo{FullMethod: reportMethod},
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)
	require.NoError(t, err)
	assert.True(t, called, "админский ярус не дошёл до обработчика подтверждения")
	require.Len(t, asked, 1)
	assert.Equal(t, "system_admin", asked[0])
}
