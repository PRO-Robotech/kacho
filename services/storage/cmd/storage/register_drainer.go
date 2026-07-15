// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

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
