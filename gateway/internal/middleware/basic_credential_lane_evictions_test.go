// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_lane_evictions_test.go — СКОРОСТЬ роста заполнения, а не
// только факт достижения потолка (#1221).
//
// # Чего не хватало объявленному до этого
//
// Заполнение было наблюдаемо двумя способами: потолок печатался при старте, а
// достижение — однократной записью в журнал. Оба отвечают на вопрос «дошли ли»
// и ни один — на вопрос «насколько близко и как быстро растём». Между тем
// решение, ради которого наблюдение и заводят (делать ли потолок ручкой),
// принимается именно по второму: потолок, достигнутый раз в сутки, и потолок,
// перемалывающий сотню записей в минуту, требуют разного, а однократная запись
// в журнале у них ОДНА И ТА ЖЕ.
//
// Отсюда третья величина — число вытеснений. Она монотонна, поэтому её
// производная по времени и есть искомая скорость.
package middleware_test

import (
	"context"
	"testing"
	"time"
)

// TestBasicLaneReportsEvictionsSoTheFillRateIsReadable — вытеснения выходят
// наружу отдельной величиной.
func TestBasicLaneReportsEvictionsSoTheFillRateIsReadable(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	capacity := lane.CacheStats().Capacity
	if capacity <= 0 {
		t.Fatalf("потолок не объявлен: %d", capacity)
	}

	// Пока потолок не превышен, вытеснять нечего: положительный контроль к
	// утверждению ниже — без него «вытеснений столько-то» зеленело бы на
	// счётчике, растущем на каждой записи.
	verify := func() {
		_, s := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), s); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	for i := 0; i < capacity; i++ {
		verify()
	}
	if got := lane.CacheStats().Evictions; got != 0 {
		t.Fatalf("потолок занят, но не превышен — вытеснений ожидалось 0, получено %d", got)
	}

	const over = 5
	for i := 0; i < over; i++ {
		verify()
	}
	st := lane.CacheStats()
	if st.Evictions != over {
		t.Errorf("сверх потолка предъявлено %d различных удостоверений, вытеснений %d — "+
			"по этой величине и читается скорость заполнения", over, st.Evictions)
	}
	if st.Entries != capacity {
		t.Errorf("записей %d при потолке %d: потолок не держится", st.Entries, capacity)
	}
}

// TestBasicLaneWindowTurnoverIsNotAnEviction — законный близнец.
//
// Оборот окна снимает записи штатно и к потолку отношения не имеет. Слитые в
// одно число, две причины сделали бы величину бесполезной ровно для того
// решения, ради которого она заведена.
func TestBasicLaneWindowTurnoverIsNotAnEviction(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	_, s := mintDistinct(t, auth)
	if _, err := lane.Verify(context.Background(), s); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	clock = clock.Add(100 * time.Hour)
	if _, err := lane.Verify(context.Background(), s); err != nil {
		t.Fatalf("Verify после окна: %v", err)
	}

	if got := lane.CacheStats().Evictions; got != 0 {
		t.Errorf("оборот окна засчитан вытеснением (%d) — величина отвечает не на "+
			"тот вопрос, продолжая называться этим именем", got)
	}
}
