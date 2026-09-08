// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_deny_is_not_cached_test.go — край обязан кешировать ТОЛЬКО положительный
// вердикт.
//
// Срок жизни записи объявлен политикой `pkg/authz.RevocationPolicy` как ОКНО
// ОТЗЫВА: сколько субъект, у которого право уже отобрали, продолжает проходить.
// Это обоснование целиком про положительный вердикт. Пока край складывал в тот
// же кеш и ОТКАЗ, то же самое число работало второй, необъявленной стороной —
// как ЗАДЕРЖКА ВЫДАЧИ: первый же запрос, проигравший гонку материализации,
// сам записывал отказ и держал его весь срок, уже ПОСЛЕ того как право
// появилось у авторитетного источника.
//
// Общая библиотека (`pkg/authz.Cache`) это правило соблюдает и делает нарушение
// непредставимым: у записи нет поля «разрешено», сам факт живой записи означает
// «разрешено». Здесь проба утверждает то же свойство на крае — поведением, а не
// формой: авторитетный ответ сменился с «нет» на «да», и следующий запрос обязан
// пройти, не дожидаясь истечения записи.
package middleware_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// flippingChecker — авторитетный источник, чей вердикт меняется со временем:
// первые `denyFirst` ответов — отказ, дальше — разрешение. Так выглядит
// материализация owner-tuple: ресурс уже закоммичен, право появляется чуть позже.
type flippingChecker struct {
	calls     atomic.Int64
	denyFirst int64
}

func (f *flippingChecker) Check(_ context.Context, _ middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	n := f.calls.Add(1)
	if n <= f.denyFirst {
		return middleware.AuthzCheckResult{Allowed: false, DenyReasons: []string{"no path"}}, nil
	}
	return middleware.AuthzCheckResult{Allowed: true}, nil
}

// TestAuthz_DenyIsNotCached_FreshGrantAppliesImmediately — ОТРИЦАНИЕ.
// Отказ не переживает собственную причину: как только авторитетный источник
// начал отвечать «да», следующий запрос проходит.
func TestAuthz_DenyIsNotCached_FreshGrantAppliesImmediately(t *testing.T) {
	checker := &flippingChecker{denyFirst: 1}
	mw := buildAuthzMiddleware(t, buildCatalog(t, accountDeleteEntry), checker)

	handlerReached := false
	call := func() error {
		_, err := mw.Unary()(withTokenMD("usr_x", "user"), nil,
			&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.AccountService/Delete"},
			func(ctx context.Context, req any) (any, error) {
				handlerReached = true
				return nil, nil
			})
		return err
	}

	// Гонка материализации: право ещё не видно — отказ ожидаем и правилен.
	err := call()
	require.Error(t, err, "первый запрос обязан получить отказ: права ещё нет")
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.False(t, handlerReached)

	// Право материализовалось. Никакого ожидания срока жизни записи быть не должно.
	require.NoError(t, call(),
		"отказ пережил свою причину: край отдал закешированный «нет» после того, "+
			"как авторитетный источник начал отвечать «да»")
	assert.True(t, handlerReached, "хендлер обязан быть достигнут после выдачи права")
	assert.Equal(t, int64(2), checker.calls.Load(),
		"второй запрос обязан спросить авторитетный источник, а не кеш отказов")
}

// TestAuthz_AllowIsStillCached — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ (законный близнец).
// Снятие кеша отказов не имеет права снести кеш разрешений: иначе проба выше
// зеленела бы на полностью выключенном кеше, а объявленное окно отзыва потеряло
// бы предмет.
func TestAuthz_AllowIsStillCached(t *testing.T) {
	checker := &flippingChecker{denyFirst: 0} // всегда «да»
	mw := buildAuthzMiddleware(t, buildCatalog(t, accountDeleteEntry), checker)

	for i := 0; i < 3; i++ {
		_, err := mw.Unary()(withTokenMD("usr_y", "user"), nil,
			&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.AccountService/Delete"},
			func(ctx context.Context, req any) (any, error) { return nil, nil })
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), checker.calls.Load(),
		"положительный вердикт обязан кешироваться — это и есть объявленное окно отзыва")
}

// TestAuthz_DenyRepeatedAlwaysAsksAuthority — тот же предмет с другой стороны:
// пока авторитетный источник отвечает «нет», край обязан спрашивать его КАЖДЫЙ
// раз, а не отвечать из кеша. Иначе задержка выдачи возвращается через заднюю
// дверь при любом числе повторов.
func TestAuthz_DenyRepeatedAlwaysAsksAuthority(t *testing.T) {
	checker := &flippingChecker{denyFirst: 1 << 30} // всегда «нет»
	mw := buildAuthzMiddleware(t, buildCatalog(t, accountDeleteEntry), checker)

	for i := 0; i < 4; i++ {
		_, err := mw.Unary()(withTokenMD("usr_z", "user"), nil,
			&grpc.UnaryServerInfo{FullMethod: "/kaname.cloud.iam.v1.AccountService/Delete"},
			func(ctx context.Context, req any) (any, error) {
				t.Fatal("хендлер не должен быть достигнут на отказе")
				return nil, nil
			})
		require.Error(t, err)
	}
	assert.Equal(t, int64(4), checker.calls.Load(),
		"каждый отказ обязан быть свежим решением авторитетного источника")
}
