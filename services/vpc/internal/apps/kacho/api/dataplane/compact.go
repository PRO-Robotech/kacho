// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// IntentCompactor — уплотнение снятых намерений.
type IntentCompactor interface {
	// Compact удаляет снятые намерения старше retention и возвращает, сколько
	// удалено и каким стал горизонт продолжения.
	Compact(ctx context.Context, retention time.Duration) (removed, horizon int64, err error)
}

// Compactor — фоновая работа, без которой журнал намерения растёт вечно.
//
// # Зачем это существует
//
// Снятое намерение обязано полежать: исполнитель, отставший на несколько минут,
// должен узнать об удалении объекта, а не просто не увидеть его. Но лежать
// вечно оно не может — тогда таблица растёт на каждый удалённый ресурс и не
// убывает никогда.
//
// Уплотнение поднимает горизонт: позиция ниже него могла пропустить снятие,
// след которого стёрт, и потому больше не годится для продолжения. Именно
// поэтому у исхода «твоя ревизия слишком стара» ЕСТЬ производитель — без
// уплотнения та ветвь потока была бы недостижимой, то есть описывала бы
// контракт, которого код никогда не производит.
type Compactor struct {
	store     IntentCompactor
	log       *slog.Logger
	retention time.Duration
	every     time.Duration
}

// NewCompactor собирает фоновую работу.
func NewCompactor(store IntentCompactor, log *slog.Logger) *Compactor {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Compactor{
		store:     store,
		log:       log,
		retention: TombstoneRetention,
		every:     CompactInterval,
	}
}

// Run ведёт уплотнение до отмены контекста.
//
// Отказ уплотнения — НЕ повод останавливаться: журнал продолжает расти, но
// доставка намерения от этого не ломается. Отказ громкий (ERROR) и с числом
// подряд идущих отказов: «уплотнение не проходит месяц» обязано быть заметно,
// иначе горизонт стоит на месте, таблица растёт, и узнают об этом по месту на
// диске.
func (c *Compactor) Run(ctx context.Context) {
	ticker := time.NewTicker(c.every)
	defer ticker.Stop()
	var failures int
	for {
		select {
		case <-ctx.Done():
			c.log.Info("dataplane intent compaction stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
		}
		removed, horizon, err := c.store.Compact(ctx, c.retention)
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			continue
		case err != nil:
			failures++
			c.log.Error("dataplane intent compaction failed",
				"err", err, "consecutive_failures", failures, "retention", c.retention)
			continue
		}
		failures = 0
		// Печатается ВСЕГДА, включая ноль удалённых: «уплотнять было нечего» и
		// «уплотнение не запускалось» иначе неразличимы.
		c.log.Info("dataplane intent compaction done",
			"removed", removed, "horizon_revision", horizon, "retention", c.retention)
	}
}
