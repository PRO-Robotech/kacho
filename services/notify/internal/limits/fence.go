// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// lockNotAvailable — SQLSTATE исхода по `lock_timeout` (55P03).
const lockNotAvailable = "55P03"

const (
	fenceLockTimeoutSQL = "SET LOCAL lock_timeout = '%dms'"
	fenceLockSQL        = `LOCK TABLE recipient_key_fence IN EXCLUSIVE MODE`
	fenceWriteSQL       = `INSERT INTO recipient_key_fence (singleton, fingerprint) VALUES (true, $1)
ON CONFLICT (singleton) DO UPDATE SET fingerprint = EXCLUDED.fingerprint
WHERE recipient_key_fence.fingerprint <> EXCLUDED.fingerprint`
)

// WriteFence — запись ограды ключа сетки при старте реплики: после миграций и
// до первого `Claim`, собственной короткой транзакцией из пяти операторов
// (З24, CX1-68 (д), CX1-69): серверный предел (два `SET LOCAL`),
// `lock_timeout` = [FenceLockTimeout], `LOCK TABLE … IN EXCLUSIVE MODE`,
// запись отпечатка в строку-одиночку; затем коммит. Действующий ключ задаёт
// последняя стартовавшая реплика.
//
// Табличный замок, а не строчный: `EXCLUSIVE` встаёт в очередь за резервами,
// чей `FOR SHARE` ограды уже исполнен, а новые резервы ждут за ним и читают
// новый отпечаток. Время ожидания замка числом пишется в журнал старта и при
// успехе, и при отказе; ключа и отпечатка журнал не несёт. Отказ по пределу —
// ошибка `55P03`, ограда прежняя; что держало замок, запись не утверждает.
func (l *Limiter) WriteFence(ctx context.Context) error {
	var wait time.Duration
	err := l.writeFenceTx(ctx, &wait)
	if err == nil {
		l.log.Info("ограда ключа сетки записана", "wait", wait, "limit", FenceLockTimeout)
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == lockNotAvailable {
		l.log.Error("ограда ключа сетки не записана: замок не получен за предел",
			"wait", wait, "limit", FenceLockTimeout, "sqlstate", pg.Code)
		return fmt.Errorf("ограда ключа сетки не записана: замок не получен за предел %s: %w", FenceLockTimeout, err)
	}
	l.log.Error("ограда ключа сетки не записана", "wait", wait, "err", err.Error())
	return fmt.Errorf("ограда ключа сетки не записана: %w", err)
}

func (l *Limiter) writeFenceTx(ctx context.Context, wait *time.Duration) (err error) {
	tx, err := l.begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if _, err := tx.Exec(ctx, fmt.Sprintf(fenceLockTimeoutSQL, FenceLockTimeout.Milliseconds())); err != nil {
		return err
	}
	start := time.Now()
	_, err = tx.Exec(ctx, fenceLockSQL)
	*wait = time.Since(start)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, fenceWriteSQL, l.fingerprint); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
