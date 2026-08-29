// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscription

// watermark_established_internal_test.go — «позиции ещё нет» отличимо от
// «позиция ноль» (kacho#1386).
//
// Граница устоявшегося начинает жизнь нулём и подтверждается НЕ ВСЕГДА первым
// же наблюдением: писатель, державший журнал в момент наблюдения, переносит
// подтверждение на следующий проход. Пока подтверждения нет, ноль означает
// отсутствие позиции — а тот, кто садится на границу, обязан различать эти два
// значения, иначе он садится в начало журнала.
//
// Проба разбирает ПЕРЕХОД состояния, а не запрос к базе: вход подаётся так же,
// как его подаёт наблюдение, и потому воспроизводим без Postgres. Что то же
// самое состояние производит НАСТОЯЩИЙ писатель, утверждает интеграционная
// проба (`coldwatermark_integration_test.go`).

import (
	"log/slog"
	"testing"
	"time"
)

func newProbeWatermark() *watermark {
	return &watermark{log: slog.Default(), now: time.Now}
}

// TestWatermarkEstablishedSeparatesNoPositionFromPositionZero — обе стороны.
func TestWatermarkEstablishedSeparatesNoPositionFromPositionZero(t *testing.T) {
	t.Run("непустой журнал под писателем — позиции ЕЩЁ НЕТ", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(5000, 1, []string{"3/17"})
		if h.established {
			t.Error("наблюдение объявлено состоявшимся, хотя писатель держит журнал: " +
				"ноль будет усвоен как позиция, и подписчик сядет в начало журнала")
		}
		if h.settled != 0 {
			t.Errorf("граница %d, ожидался 0 — подтверждать было нечем", h.settled)
		}
	})

	t.Run("писатель доистёк — наблюдение подтверждено", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(5000, 1, []string{"3/17"})
		h.observe(5000, 1, nil)
		if !h.established {
			t.Error("наблюдение не состоялось после ухода писателя")
		}
		if h.settled != 5000 {
			t.Errorf("граница %d, ожидалась 5000", h.settled)
		}
	})

	t.Run("непустой журнал без писателей — состоялось сразу", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(42, 1, nil)
		if !h.established || h.settled != 42 {
			t.Errorf("состоялось=%v граница=%d, ожидалось true/42", h.established, h.settled)
		}
	})

	// ЗАКОННЫЙ БЛИЗНЕЦ. У пустого журнала ноль — настоящая позиция: строк,
	// которые можно было бы пропустить, нет. Объяви его «ещё не позицией» — и
	// подписчик на пустом журнале не сядет НИКОГДА, то есть расход вылечился бы
	// отказом в обслуживании.
	t.Run("пустой журнал — ноль ЕСТЬ позиция", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(0, 0, nil)
		if !h.established {
			t.Error("пустой журнал объявлен неподтверждённым — подтверждать в нём нечего")
		}
		if h.settled != 0 {
			t.Errorf("граница %d, ожидался 0", h.settled)
		}
	})

	// Тот же близнец под писателем: номер ещё не выдан, максимума нет. Ждать
	// нечего — строка, которую он пишет, получит номер выше нуля и уедет
	// подписчику как событие ПОСЛЕ подписки.
	t.Run("пустой журнал под писателем — ноль ЕСТЬ позиция", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(0, 0, []string{"4/21"})
		if !h.established {
			t.Error("пустой журнал под писателем объявлен неподтверждённым — " +
				"его первая строка придёт событием, а не историей")
		}
	})

	// Состоявшееся наблюдение НЕ отзывается новым писателем: граница уже
	// известна, и следующий вопрос задаётся с неё, а не с нуля.
	t.Run("подтверждение не отзывается новым писателем", func(t *testing.T) {
		h := newProbeWatermark()
		h.observe(42, 1, nil)
		h.observe(90, 1, []string{"5/9"})
		if !h.established {
			t.Error("подтверждённое наблюдение отозвано появлением писателя")
		}
		if h.settled != 42 {
			t.Errorf("граница %d, ожидалась 42 — она не вправе двигаться до подтверждения", h.settled)
		}
	})
}

// TestResolveCursorSeatsFromNowOnTheSettledBoundary — подписчик без позиции
// садится на границу, и садиться ему дают только на подтверждённую.
//
// Две другие ветви выбора курсора подтверждением не связаны, и это утверждается
// здесь же: «с начала» и «с названной позиции» не спрашивают границу вовсе.
func TestResolveCursorSeatsFromNowOnTheSettledBoundary(t *testing.T) {
	s := &Server{}
	h := newProbeWatermark()
	h.observe(77, 1, nil)

	got, err := s.resolveCursor(Start{}, h, 0)
	if err != nil {
		t.Fatalf("выбор курсора: %v", err)
	}
	if got != 77 {
		t.Errorf("курсор %d, ожидался 77 — подписчик без позиции садится на границу", got)
	}

	got, err = s.resolveCursor(Start{FromBeginning: true}, h, 3)
	if err != nil {
		t.Fatalf("выбор курсора с начала: %v", err)
	}
	if got != 3 {
		t.Errorf("курсор %d, ожидался 3 — «с начала» садится на пол удержанного", got)
	}
}
