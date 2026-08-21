// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Обвязка outbox-backstop поверх register-drainer: добавляет observability и
// safety, не меняя co-commit-атомарность register-intent в writer-TX.
//
//   - reconciler: периодический RedrivePoisoned — переотправляет отравленные
//     register-intents через тот же kacho_vpc.fga_register_outbox, что дренит drainer.
//   - metrics: Collector сканирует backlog/oldest/poisoned; WithPoisonObserver
//     drainer'а инкрементит outbox_poisoned_total.
//   - boot-gate: KACHO_VPC_REQUIRE_IAM отказывает мутирующему Create и отдает
//     NotReady, пока register-drainer не подключен к IAM.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

const (
	// fgaRegisterOutboxTable / Channel — register-outbox kacho-vpc, общий для
	// drainer, reconciler и metrics-collector (одна таблица, один путь доставки).
	fgaRegisterOutboxTable   = "kacho_vpc.fga_register_outbox"
	fgaRegisterOutboxChannel = "kacho_vpc_fga_register_outbox"
)

// startBackstop — собирает reconciler + metrics Collector поверх vpc
// register-outbox и крутит их в фоне, пока ctx не отменен. Оба — best-effort
// observability/repair: транзиентная ошибка скана/прохода логируется, но не
// фатальна. Интервалы reconcile/metrics — разумные production-каденции по умолчанию.
func startBackstop(ctx context.Context, pool *pgxpool.Pool, rec metrics.Recorder, logger *slog.Logger) error {
	rc, err := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           fgaRegisterOutboxTable,
		Channel:         fgaRegisterOutboxChannel,
	}, logger.With(slog.String("component", "fga-register-reconciler")))
	if err != nil {
		return err
	}

	go runReconciler(ctx, rc, logger)

	// Directions splits the same series by direction of the queue. Without it the
	// table-wide numbers stay healthy while withdrawals never arrive: grants drain
	// continuously, so the depth is small and the head is young no matter what the
	// other half is doing, and "it works" reads exactly like "it was never revoked".
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{
		Table:      fgaRegisterOutboxTable,
		Directions: metrics.RegisterOutboxDirections(),
	})
	go col.Run(ctx, func(err error) {
		logger.Warn("outbox metrics scan failed", "err", err)
	})

	logger.Info("FGA register backstop started (reconciler + metrics)", "table", fgaRegisterOutboxTable)
	return nil
}

// runReconciler гоняет проход RedrivePoisoned на периодическом тикере: отравленные/
// исчерпанные register-intents (sent_at NULL, attempt_count >= MaxAttempts)
// сбрасываются в claimable, чтобы drainer переотправил их с ОРИГИНАЛЬНЫМ,
// корректным для декодера tuple-payload. Re-drive — рабочий backstop для уже
// атомарного сервиса.
//
// Здесь стояло объяснение, почему два других прохода corelib — сверка с
// состоянием и сбор осиротевших — сознательно НЕ запускаются, и почему адаптер
// перечисления всё-таки провязан «на случай, если контракт обзаведётся хуком».
// Оба прохода сняты из corelib (#760): их предикаты были недостижимы by
// construction — намерение пишется в очередь В ТОЙ ЖЕ транзакции, что и строка
// ресурса, — а адаптер, который никто не звал, был живым с виду механизмом,
// которого нет. Вместе с ними снят и адаптер этого сервиса.
//
// РЕПЛИКИ: на-реплику — проход — один условный оператор возврата отравленных строк.
// Строки заперты самим оператором, повтор идемпотентен, к соседям проход
// не ходит; вторая реплика приводит к тому же состоянию.
func runReconciler(ctx context.Context, rc *reconciler.Reconciler, logger *slog.Logger) {
	const interval = 5 * time.Minute
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if n, err := rc.RedrivePoisoned(ctx); err != nil {
				logger.Warn("reconciler redrive-poisoned failed", "err", err)
			} else if n > 0 {
				logger.Info("reconciler re-drove poisoned intents", "count", n)
			}
		}
	}
}
