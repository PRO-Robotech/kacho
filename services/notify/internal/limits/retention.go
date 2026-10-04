// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/retention"
)

// RetentionGrace — порог уборки окон сетки и суток потолка сверх их конца:
// аренда строки ленты. Освобождение резерва приходит не позже конца аренды
// строки, под которой резерв сделан (З21), поэтому окно, закончившееся раньше
// `now() − LeaseTTL`, освобождений уже не ждёт. Снятая раньше строка лишь
// обратила бы освобождение в пустой оператор, но порог держит и это.
const RetentionGrace = feed.LeaseTTL

const (
	// Окно закончилось: начало плюс длина окна раньше порога (часы базы).
	sweepNetSQL = `DELETE FROM recipient_net WHERE ctid IN (
SELECT ctid FROM recipient_net
WHERE window_start + CASE period WHEN 'hour' THEN interval '1 hour' ELSE interval '1 day' END
      < now() - make_interval(secs => $1)
LIMIT $2 FOR UPDATE SKIP LOCKED)`

	// Сутки закончились: конец суток UTC раньше порога.
	sweepDailySQL = `DELETE FROM global_daily WHERE day IN (
SELECT day FROM global_daily
WHERE (day + 1)::timestamp AT TIME ZONE 'UTC' < now() - make_interval(secs => $1)
LIMIT $2 FOR UPDATE SKIP LOCKED)`
)

// RetentionSubjects — предметы петли уборки notify (`corelib/retention`):
// строки окон сетки и сутки потолка, закончившиеся раньше порога, — в том числе
// ничьи строки прежнего ключа сетки после ротации (§6, З24). Ограда —
// строка-одиночка и уборке не подлежит.
func (l *Limiter) RetentionSubjects() []retention.Subject {
	return []retention.Subject{
		{Name: "recipient_net", Grace: RetentionGrace, Sweep: l.sweep(sweepNetSQL)},
		{Name: "global_daily", Grace: RetentionGrace, Sweep: l.sweep(sweepDailySQL)},
	}
}

func (l *Limiter) sweep(q string) retention.SweepFunc {
	return func(ctx context.Context, grace time.Duration, batch int) (int64, bool, error) {
		n, err := l.sweepBatch(ctx, q, grace, batch)
		if err != nil {
			return 0, false, fmt.Errorf("уборка лимитов: %w", err)
		}
		return n, n >= int64(batch), nil
	}
}

// sweepBatch — партия уборки: один оператор в транзакции с тем же серверным
// пределом, что у резерва и освобождения (SDR-К1): уборщик, замолчавший
// посреди партии, не держит строки дольше TxIdleTimeout.
func (l *Limiter) sweepBatch(ctx context.Context, q string, grace time.Duration, batch int) (n int64, err error) {
	tx, err := l.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	tag, err := tx.Exec(ctx, q, grace.Seconds(), batch)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}
