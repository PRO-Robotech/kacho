// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
)

const (
	// fgaRegisterOutboxTable — schema-qualified таблица register-outbox (миграция 0006).
	fgaRegisterOutboxTable = "kacho_storage.fga_register_outbox"
	// fgaRegisterOutboxChannel — LISTEN/NOTIFY-канал (совпадает с trigger'ом 0006).
	fgaRegisterOutboxChannel = "kacho_storage_fga_register_outbox"
)

// startRegisterDrainer запускает corelib outbox/drainer поверх
// kacho_storage.fga_register_outbox: каждый pending owner-tuple intent переигрывается
// через InternalIAMService.RegisterResource / UnregisterResource (idempotent;
// Unavailable → retry с backoff; InvalidArgument → poison). Run-loop drainer'а владеет
// claim-CAS (FOR UPDATE SKIP LOCKED) для exactly-once между репликами. Дренаж живёт до
// ctx.Done() (graceful shutdown). iamConn — тот же mTLS-conn к kacho-iam :9091, что и
// authz-Check (RegisterResource Internal-only, ban #6).
func startRegisterDrainer(ctx context.Context, pool *pgxpool.Pool, iamConn *grpc.ClientConn, logger *slog.Logger) error {
	iamClient := iamv1.NewInternalIAMServiceClient(iamConn)
	d, err := drainer.New[clients.FGARegisterPayload](
		pool,
		drainer.Config{
			Table:   fgaRegisterOutboxTable,
			Channel: fgaRegisterOutboxChannel,
			// Order-preserving drain, per resource. Таблица несёт И fga.register, И
			// fga.unregister ОДНОГО ресурса, а материализация в iam версионирована
			// лишь ЧАСТИЧНО: source_version-LWW (resource_mirror UPSERT под
			// `source_version < EXCLUDED.source_version`) гейтит ТОЛЬКО ветку
			// ON CONFLICT DO UPDATE, а unregister делает ЖЁСТКИЙ DELETE без
			// tombstone. Переставленный STALE register сравнивать не с чем → ветка
			// INSERT → ВОСКРЕШЕНИЕ mirror-строки УДАЛЁННОГО Volume/Snapshot, которую
			// level-triggered реконсайлер iam вечно ре-материализует (самоисцеления
			// нет).
			//
			// Порядок ломается и БЕЗ конкурентности: claim сортирует
			// `ORDER BY (attempt_count, id)` → transiently-bumped register
			// (attempt>=1) уступает свежему unregister (attempt=0) даже при
			// ApplyConcurrency=1 (дефолт storage). PartitionColumn закрывает это на
			// CLAIM-уровне: строка не клеймится, пока в её партиции есть
			// ДОСТАВЛЯЕМЫЙ предшественник с меньшим id → per-resource FIFO
			// cross-batch и cross-replica; разные ресурсы дренятся параллельно.
			//
			// Ключ — resource_id: emitFGARegister пишет ОДНУ строку на один
			// FGA-объект и заполняет колонку id-половиной tuple.Object, глобально
			// уникальной by construction (core rule #15). Требует partial-index
			// миграции 0008 `(resource_id, id) WHERE sent_at IS NULL` под claim'овый
			// NOT EXISTS. Поведение зафиксировано corelib-тестом
			// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister.
			PartitionColumn: "resource_id",
		},
		clients.DecodeFGARegisterPayload,
		clients.NewIAMRegisterApplier(iamClient),
		logger.With(slog.String("component", "fga-register-drainer")),
	)
	if err != nil {
		return fmt.Errorf("build register-drainer: %w", err)
	}
	go func() {
		if rerr := d.Run(ctx); rerr != nil {
			logger.Error("register-drainer stopped", "err", rerr)
		}
	}()
	logger.Info("FGA register-drainer started", "table", fgaRegisterOutboxTable, "channel", fgaRegisterOutboxChannel)
	return nil
}
