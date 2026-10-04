// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/retention"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// sweepStopBound — сколько останов ждёт текущий проход уборки: партия —
// один оператор под серверным пределом TxStatementTimeout, проход — не больше
// retention.DefaultMaxBatchesPerPass партий; ждать дольше одного оператора с
// запасом останову незачем — недоделанная партия откатывается сервером.
const sweepStopBound = limits.TxStatementTimeout + 5*time.Second

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

	reg := newRegistry()

	// Ограда ключа сетки — после миграций (init-контейнер точки наката) и до
	// первого `Claim`: действующий ключ задаёт последняя стартовавшая реплика
	// (З24, CX1-68). Отказ записи, включая предел ожидания замка, — отказ
	// старта ненулевым кодом; журнал называет время ожидания и не несёт ни
	// ключа, ни отпечатка.
	lim, err := limits.New(limits.Options{
		Pool:       pool,
		Key:        cfg.RecipientKey().Bytes(),
		Grid:       cfg.Grid(),
		Now:        time.Now,
		Registerer: reg,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("сетка лимитов: %w", err)
	}
	if err := lim.WriteFence(ctx); err != nil {
		return err
	}
	// Уборка окон сетки и суток потолка, закончившихся раньше порога (§6, З24).
	sweeper, err := retention.New(retention.DefaultConfig(), lim.RetentionSubjects(), logger)
	if err != nil {
		return fmt.Errorf("уборка лимитов: %w", err)
	}
	sweeper.Start(ctx)

	agg := health.New([]health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
	})
	go func() {
		<-ctx.Done()
		agg.SetShuttingDown()
	}()

	diag, err := describeDiagnosticSurface(cfg.DiagAddr, reg, agg, desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	wait, err := servicehost.ServeSurface(ctx, diag)
	if err != nil {
		return fmt.Errorf("диагностическая поверхность: %w", err)
	}

	<-ctx.Done()
	logger.Info("останов по сигналу")
	if !sweeper.Wait(sweepStopBound) {
		logger.Warn("петля уборки лимитов не завершилась за предел останова", "bound", sweepStopBound)
	}
	return wait()
}
