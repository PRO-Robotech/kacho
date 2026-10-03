// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// runServe — подъём процесса после стража конфигурации. Каждая ошибка
// возвращается наверх и останавливает процесс ДО подъёма поверхности.
func runServe(cfg config.Config, logger *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	desc, err := describe(cfg, logger)
	if err != nil {
		return err
	}
	if err := checkPeerTLS(cfg); err != nil {
		return err
	}

	observability.LogBootPosture(logger, bootPosture(cfg, desc))

	// Пул ленив: соединение открывается первым запросом, поэтому база,
	// недоступная на старте, — неготовность (/readyz), а не отказ подъёма.
	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return fmt.Errorf("пул базы %s: %w", cfg.DBName, err)
	}
	defer pool.Close()

	agg := health.New([]health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
	})
	go func() {
		<-ctx.Done()
		agg.SetShuttingDown()
	}()

	diag, err := describeDiagnosticSurface(cfg.DiagAddr, newRegistry(), agg, desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	wait, err := servicehost.ServeSurface(ctx, diag)
	if err != nil {
		return fmt.Errorf("диагностическая поверхность: %w", err)
	}

	<-ctx.Done()
	logger.Info("останов по сигналу")
	return wait()
}
