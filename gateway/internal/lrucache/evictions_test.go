// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// evictions_test.go — вытеснение ПОД ДАВЛЕНИЕМ ПОТОЛКА считается, а истечение
// окна — нет.
//
// # Почему это две разные величины, а не одна
//
// Обе снимают запись, и на этом сходство кончается. Истечение окна — штатный
// оборот: запись отжила свой срок, потолок тут ни при чём, и её число растёт
// ровно с оборотом кэша. Вытеснение под давлением потолка означает другое: места
// не хватило, и вытесненный ключ оплатит вызов к авторитету заново.
//
// Величина заводится ради ОДНОГО решения — делать ли потолок ручкой (#1221).
// Слитая с истечением, она отвечала бы «да» всегда, потому что оборот идёт и на
// пустом кэше: сигнал утонул бы в шуме собственного знаменателя.
package lrucache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/lrucache"
)

// TestEvictionsCountCapacityPressureOnly — давление потолка считается.
func TestEvictionsCountCapacityPressureOnly(t *testing.T) {
	clock := time.Unix(0, 0)
	c := lrucache.New[string, int](3, time.Minute, func() time.Time { return clock })

	require.Zero(t, c.Evictions(), "свежий кэш ничего не вытеснял")

	// Три записи занимают потолок ровно, четвёртая и пятая вытесняют.
	for _, k := range []string{"a", "b", "c"} {
		c.Put(k, 1)
	}
	require.Zero(t, c.Evictions(),
		"потолок занят, но не превышен — вытеснять было нечего")

	c.Put("d", 1)
	c.Put("e", 1)
	require.Equal(t, uint64(2), c.Evictions(),
		"две записи сверх потолка обязаны дать ровно два вытеснения")
	require.Equal(t, 3, c.Len(), "потолок держится")
}

// TestExpiryIsNotAnEviction — законный близнец: оборот окна вытеснением НЕ
// считается.
//
// Без этой пробы величина зеленела бы на счётчике, считающем всякое снятие
// записи, — то есть отвечала бы на другой вопрос, продолжая называться этим
// именем.
func TestExpiryIsNotAnEviction(t *testing.T) {
	clock := time.Unix(0, 0)
	c := lrucache.New[string, int](100, time.Second, func() time.Time { return clock })

	c.Put("a", 1)
	clock = clock.Add(2 * time.Second)

	if _, ok := c.Get("a"); ok {
		t.Fatal("запись обязана была отжить окно")
	}
	require.Zero(t, c.Evictions(),
		"истечение окна — штатный оборот, а не нехватка места: смешав их, "+
			"величина перестала бы отвечать на вопрос о потолке")
}

// TestReplacingAKeyIsNotAnEviction — второй законный близнец: перезапись
// существующего ключа места не отнимает.
func TestReplacingAKeyIsNotAnEviction(t *testing.T) {
	clock := time.Unix(0, 0)
	c := lrucache.New[string, int](2, time.Minute, func() time.Time { return clock })

	c.Put("a", 1)
	c.Put("a", 2)
	c.Put("a", 3)
	require.Zero(t, c.Evictions(), "перезапись ключа не вытесняет ничего")
	require.Equal(t, 1, c.Len())
}

// TestInvalidationIsNotAnEviction — третий близнец: сброс по отзыву снимает
// записи намеренно, и считать это нехваткой места значило бы объявлять отзыв
// поводом поднять потолок.
func TestInvalidationIsNotAnEviction(t *testing.T) {
	clock := time.Unix(0, 0)
	c := lrucache.New[string, int](10, time.Minute, func() time.Time { return clock })

	c.Put("a", 1)
	c.Put("b", 1)
	c.Invalidate()
	require.Zero(t, c.Evictions())

	c.Put("a", 1)
	c.InvalidateKey("a")
	require.Zero(t, c.Evictions())

	c.Put("b", 1)
	c.InvalidateWhere(func(string) bool { return true })
	require.Zero(t, c.Evictions(),
		"снятие по отзыву — решение, а не давление потолка")
}
