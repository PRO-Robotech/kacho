// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Проводка разрешителя осиротевших операций: доменный разрешитель storage +
// движок corelib.
//
// Живой исполнитель добирает только те операции, которые диспетчеризовал сам этот
// процесс. Всё, что пережило его смерть, остаётся done=false навсегда, и клиент,
// поллящий такую строку, не узнаёт исхода ни разу. Разрешитель при старте
// (RecoverAll — ДО приёма трафика) и периодическим проходом (Run) сверяет
// осиротевшие строки с закоммиченной реальностью ресурса и приводит их в терминал.
// Покрывает перекат/крах посреди работы, исчерпание бюджета терминальной записи и
// переполнение очереди исполнителя (там мутация не выполняется вовсе, а строка
// «в процессе» всё равно висит).
//
// Частичный индекс под запрос этого прохода схема storage несла с самого начала
// (миграция 0002, `ON kacho_storage.operations (modified_at) WHERE NOT done`) —
// разрешитель был заявлен раньше, чем провязан.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/operationresolver"
)

const (
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
// Serve) и возвращает объект, чей периодический Run вешается на фон.
//
// Ошибка стартового восстановления НЕ фатальна: это подстраховка, а не условие
// работоспособности, и ронять старт из-за временной недоступности БД значило бы
// менять один отказ на худший. Периодический проход добьёт позже.
func startLRORecovery(
	ctx context.Context,
	pool *pgxpool.Pool,
	readers operationresolver.Readers,
	logger *slog.Logger,
) *operations.Reconciler {
	resolver := operationresolver.New(readers, operationresolver.WithLogger(logger))
	reconciler := operations.NewReconciler(pool, resolver, operations.ReconcilerConfig{
		Schema:      config.DBSchema,
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
			"schema", config.DBSchema, "orphan_grace", reconcileOrphanGrace, "interval", reconcileInterval)
	}
	return reconciler
}
