// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Проводка разрешителя осиротевших операций registry: доменный разрешитель +
// движок corelib.
//
// Дренаж на SIGTERM (operations.Wait в serve.go) закрывает только штатное
// завершение и только то, что успело влезть в 15 секунд. Всё остальное —
// SIGKILL, OOM, вытеснение пода, исчерпание бюджета терминальной записи,
// переполнение очереди исполнителя — оставляло строку операции done=false
// НАВСЕГДА: живой исполнитель добирает лишь то, что диспетчеризовал сам этот
// процесс, а другого добирающего у сервиса не было.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/operationresolver"
)

const (
	// reconcileSchema — schema-квалификатор таблицы операций registry.
	reconcileSchema = "kacho_registry"
	// reconcileOrphanGrace — кандидат обязан быть старше этого окна, иначе
	// разрешитель добьёт ЖИВУЮ долгую работу. Строго больше предела исполнения
	// одной операции (corelib WithOperationTimeout, 4m). Инвариант заперт
	// repohygiene.TestOrphanGraceExceedsOperationTimeout.
	reconcileOrphanGrace = 5 * time.Minute
	// reconcileInterval — каденция периодического прохода-подстраховки.
	reconcileInterval = 30 * time.Second
	// reconcileBatchSize — размер пачки клейма за один проход.
	reconcileBatchSize = 100
)

// startLRORecovery строит разрешителя, прогоняет стартовое восстановление (ДО
// приёма трафика) и возвращает объект, чей периодический Run вешается на фон.
//
// Ошибка стартового восстановления НЕ фатальна: это подстраховка, а не условие
// работоспособности. Периодический проход добьёт позже.
func startLRORecovery(
	ctx context.Context,
	pool *pgxpool.Pool,
	readers operationresolver.Readers,
	logger *slog.Logger,
) *operations.Reconciler {
	resolver := operationresolver.New(readers, operationresolver.WithLogger(logger))
	reconciler := operations.NewReconciler(pool, resolver, operations.ReconcilerConfig{
		Schema:      reconcileSchema,
		OrphanGrace: reconcileOrphanGrace,
		BatchSize:   reconcileBatchSize,
		Interval:    reconcileInterval,
	},
		operations.WithReconcilerLogger(logger.With(slog.String("component", "lro-reconciler"))),
	)

	if err := reconciler.RecoverAll(ctx); err != nil {
		logger.Error("LRO startup-recovery failed; the periodic sweep will retry", "err", err)
	} else {
		logger.Info("LRO startup-recovery complete (orphaned operations resolved)",
			"schema", reconcileSchema, "orphan_grace", reconcileOrphanGrace, "interval", reconcileInterval)
	}
	return reconciler
}
