// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// verdict_cache_test.go — сколько раз страница спрашивает хранилище прав.
//
// Контрактная страница — до тысячи элементов, и на прямом пути фильтра КАЖДЫЙ
// элемент шёл отдельным вопросом к общему для всей платформы хранилищу прав, без
// кеша (у интерсептора кеш есть, но прямые вызовы его не проходят). Числа ниже —
// предмет проверки, а не иллюстрация.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type countingAuthorizer struct {
	calls   atomic.Int64
	allowed bool
}

func (c *countingAuthorizer) Check(context.Context, string, string, string) (bool, error) {
	c.calls.Add(1)
	return c.allowed, nil
}

// TestVerdictCache_CollapsesRepeatedQuestions — повторный вопрос про тот же объект
// не уходит в хранилище прав: страница из ста элементов, каждый спрошенный дважды
// (первая и вторая страницы того же списка), стоит ста обращений, а не двухсот.
func TestVerdictCache_CollapsesRepeatedQuestions(t *testing.T) {
	inner := &countingAuthorizer{allowed: true}
	az := newCachedAuthorizer(inner, time.Minute)

	const objects = 100
	for range 2 {
		for i := range objects {
			ok, err := az.Check(context.Background(), "service_account:sva-1", relationVList,
				repositoryObjectRef("reg-A", "app-"+string(rune('a'+i%26))+string(rune('a'+i/26))))
			require.NoError(t, err)
			require.True(t, ok)
		}
	}
	require.Equal(t, int64(objects), inner.calls.Load(),
		"обращений к хранилищу прав %d при %d различных объектах, спрошенных дважды",
		inner.calls.Load(), objects)
}

// TestVerdictCache_DoesNotCacheDenials — отказ НЕ кешируется: свежая выдача
// материализуется асинхронно, и закешированный отказ продлевал бы окно, в котором
// владелец не видит собственный ресурс.
func TestVerdictCache_DoesNotCacheDenials(t *testing.T) {
	inner := &countingAuthorizer{allowed: false}
	az := newCachedAuthorizer(inner, time.Minute)

	for range 3 {
		ok, err := az.Check(context.Background(), "service_account:sva-1", relationVList,
			repositoryObjectRef("reg-A", "app"))
		require.NoError(t, err)
		require.False(t, ok)
	}
	require.Equal(t, int64(3), inner.calls.Load(),
		"отказ закеширован — свежая выдача осталась бы невидимой на срок кеша")
}

// TestVerdictCache_Disabled — при выключенном сроке обёртки нет вовсе: у выключенного
// состояния не должно быть собственного поведения.
func TestVerdictCache_Disabled(t *testing.T) {
	inner := &countingAuthorizer{allowed: true}
	require.Same(t, Authorizer(inner), newCachedAuthorizer(inner, 0))
	require.Nil(t, newCachedAuthorizer(nil, time.Minute))
}

// CheckMany — та же дверь, выведенная из Check (см. manyFromOne). Счётчик поэтому
// считает ВОПРОСЫ, а не запросы: проба про окно вердиктов, а не про цену страницы.
func (c *countingAuthorizer) CheckMany(
	ctx context.Context, subject, relation, objectType string, objectIDs []string,
) ([]string, error) {
	return manyFromOne{one: c.Check}.checkMany(ctx, subject, relation, objectType, objectIDs)
}
