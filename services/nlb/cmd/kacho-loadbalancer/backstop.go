// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// backstop.go — the corelib outbox backstop wiring for
// kacho-nlb (reconciler + metrics + fail-closed boot-gate). It adds
// observability + safety on TOP of the existing register-drainer WITHOUT
// changing co-commit atomicity (no migration).
//
//   - reconciler: periodic RedrivePoisoned re-drives poisoned/exhausted intents
//     (with their original decoder-correct payload) back to claimable.
//   - metrics: a Collector scans backlog/oldest/poisoned; the drainer's
//     WithPoisonObserver bumps outbox_poisoned_total.
//   - boot-gate: KACHO_NLB_REQUIRE_IAM refuses mutating Create + NotReady until
//     the IAM-connected register-drainer is up.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

const (
	// nlbFGAOutboxTable / Channel — the register-outbox the drainer, reconciler
	// and metrics-collector all share.
	nlbFGAOutboxTable   = "kacho_nlb.fga_register_outbox"
	nlbFGAOutboxChannel = "kacho_nlb_fga_register_outbox"
)

// startBackstop wires the reconciler RedrivePoisoned pass + metrics Collector over
// the nlb register-outbox. Both are best-effort observability/repair — a transient
// error is logged, never fatal. It returns their run-loops as supervised tasks
// (reconRun / colRun), wired into the runServe errgroup — not fire-and-forget.
func startBackstop(_ context.Context, pool *pgxpool.Pool, rec metrics.Recorder, logger *slog.Logger) (reconRun, colRun func(context.Context) error, err error) {
	rc, rerr := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           nlbFGAOutboxTable,
		Channel:         nlbFGAOutboxChannel,
	}, logger.With(slog.String("component", "fga-register-reconciler")))
	if rerr != nil {
		return nil, nil, rerr
	}

	// Per-direction breakdown: see metrics.RegisterOutboxDirections — the withdrawal
	// half of this queue has no symptom of its own when it stops arriving.
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{
		Table:      nlbFGAOutboxTable,
		Directions: metrics.RegisterOutboxDirections(),
	})

	logger.Info("fga_register_backstop_started", "table", nlbFGAOutboxTable)

	reconRun = func(ctx context.Context) error {
		runReconciler(ctx, rc, logger)
		return nil
	}
	colRun = func(ctx context.Context) error {
		col.Run(ctx, func(err error) {
			logger.Warn("outbox metrics scan failed", "err", err)
		})
		return nil
	}
	return reconRun, colRun, nil
}

// runReconciler runs the reconciler RedrivePoisoned pass on a periodic ticker
// (1.4-30): poisoned/exhausted register-intents are reset to claimable so the
// drainer re-delivers them with their ORIGINAL, decoder-correct tuple payload.
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
				logger.Warn("reconciler_redrive_poisoned_failed", "err", err)
			} else if n > 0 {
				logger.Info("reconciler_redrove_poisoned", "count", n)
			}
		}
	}
}
