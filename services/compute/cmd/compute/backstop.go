// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// backstop.go — обвязка corelib outbox-backstop для kacho-compute (reconciler +
// metrics + fail-closed boot-gate). Добавляет наблюдаемость и страховку поверх
// register-drainer, не меняя co-commit-атомарность writer-tx (без отдельной
// миграции).
//
//   - reconciler: periodic RedrivePoisoned re-drives poisoned/exhausted intents
//     (with their original decoder-correct payload) back to claimable.
//   - metrics: a Collector scans backlog/oldest/poisoned; the drainer's
//     WithPoisonObserver bumps outbox_poisoned_total.
//   - boot-gate: KACHO_COMPUTE_REQUIRE_IAM refuses mutating Create + NotReady
//     until the IAM-connected register-drainer is up.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

const (
	// computeFGAOutboxTable / Channel — the register-outbox the drainer,
	// reconciler and metrics-collector all share.
	computeFGAOutboxTable   = "public.compute_fga_register_outbox"
	computeFGAOutboxChannel = "compute_fga_register_outbox"
)

// startBackstop wires the reconciler RedrivePoisoned pass + metrics Collector over
// the compute register-outbox. Both are best-effort observability/repair — a
// transient error is logged, never fatal. It returns their run-loops as supervised
// tasks (reconRun / colRun), wired into the runServe errgroup — not fire-and-forget.
func startBackstop(_ context.Context, pool *pgxpool.Pool, rec metrics.Recorder, logger *slog.Logger) (reconRun, colRun func(context.Context) error, err error) {
	rc, rerr := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           computeFGAOutboxTable,
		Channel:         computeFGAOutboxChannel,
	}, logger.With(slog.String("component", "fga-register-reconciler")))
	if rerr != nil {
		return nil, nil, rerr
	}

	// Per-direction breakdown: see metrics.RegisterOutboxDirections — the withdrawal
	// half of this queue has no symptom of its own when it stops arriving.
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{
		Table:      computeFGAOutboxTable,
		Directions: metrics.RegisterOutboxDirections(),
	})

	logger.Info("FGA register backstop started (reconciler + metrics)", "table", computeFGAOutboxTable)

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

// runReconciler runs the reconciler RedrivePoisoned pass on a periodic ticker:
// poisoned/exhausted register-intents are reset to claimable so the drainer
// re-delivers them with their ORIGINAL, decoder-correct tuple payload.
//
// Здесь стояло объяснение, почему два других прохода corelib — сверка с
// состоянием и сбор осиротевших — сознательно НЕ запускаются, и почему адаптер
// перечисления всё-таки провязан «на случай, если контракт обзаведётся хуком».
// Оба прохода сняты из corelib: их предикаты были недостижимы by construction —
// намерение пишется в очередь В ТОЙ ЖЕ транзакции, что и строка ресурса, — а
// адаптер, который никто не звал, был живым с виду механизмом, которого нет.
// Вместе с ними снят и адаптер этого сервиса.
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
