// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package subscription

// resolvecursor_internal_test.go — выбор курсора у подписчика БЕЗ позиции.
//
// Проба приехала из фундамента вместе с сервером (kacho#2601): её предмет —
// [Server.resolveCursor], а не наблюдение границы, и решает она вопрос сервера.
// В фундаменте она вела наблюдателя ВНУТРЕННИМ переходом (`h.observe`); здесь
// того же состояния приходится добиваться ЭКСПОРТИРОВАННЫМ путём — подставным
// источником через [Watermark.Advance], — потому что наблюдатель остался в
// другом модуле. Это не ослабление: состояние то же самое, а подаётся оно тем
// же способом, каким его подаёт настоящее наблюдение.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	corelibsub "github.com/PRO-Robotech/corelib/subscription"
)

// settledRow — ответ подставного источника на единственный вопрос
// [Watermark.Advance]: наибольший номер, наименьший номер и незавершённые
// писатели.
type settledRow struct {
	max, min int64
	writers  []string
}

func (r settledRow) Scan(dest ...any) error {
	if len(dest) != 3 {
		return pgx.ErrNoRows
	}
	maxTarget, ok := dest[0].(*int64)
	if !ok {
		return pgx.ErrNoRows
	}
	minTarget, ok := dest[1].(*int64)
	if !ok {
		return pgx.ErrNoRows
	}
	writersTarget, ok := dest[2].(*[]string)
	if !ok {
		return pgx.ErrNoRows
	}
	*maxTarget, *minTarget, *writersTarget = r.max, r.min, r.writers
	return nil
}

// settledSource — [Querier], отвечающий заготовленным наблюдением. Настоящего
// соединения здесь не нужно: предмет пробы — выбор курсора, а не разбор SQL.
type settledSource struct{ row settledRow }

func (s settledSource) QueryRow(context.Context, string, ...any) pgx.Row { return s.row }

// TestResolveCursorSeatsFromNowOnTheSettledBoundary — подписчик без позиции
// садится на границу, и садиться ему дают только на подтверждённую.
//
// Две другие ветви выбора курсора подтверждением не связаны, и это утверждается
// здесь же: «с начала» и «с названной позиции» не спрашивают границу вовсе.
func TestResolveCursorSeatsFromNowOnTheSettledBoundary(t *testing.T) {
	ctx := context.Background()
	s := &Server{}

	h := corelibsub.NewWatermark("probe_outbox", "sequence_no", nil)

	// Холодный наблюдатель: граница ещё НЕ подтверждена. Утверждается, чтобы
	// шаг ниже был отличим от этого состояния, — иначе «сел на 77» зеленело бы
	// и на реализации, которая границу вообще не спрашивает.
	if h.Established() {
		t.Fatal("холодный наблюдатель объявлен подтверждённым — его ноль означает отсутствие позиции")
	}

	if err := h.Advance(ctx, settledSource{row: settledRow{max: 77, min: 1}}); err != nil {
		t.Fatalf("наблюдение границы: %v", err)
	}
	if !h.Established() || h.Settled() != 77 {
		t.Fatalf("граница %d, подтверждена %v — ожидалось 77 и true", h.Settled(), h.Established())
	}

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
