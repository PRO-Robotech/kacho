// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/authz/authzmetrics"
	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/schemaguard"
	"github.com/PRO-Robotech/corelib/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/send"
	"github.com/PRO-Robotech/kacho/services/notify/internal/probemigrations"
)

// runServe — композиционный корень пробы.
//
// Сборку серверов, цепочку звеньев, карту прав и стражи старта держит носитель
// (`corelib/servicehost`). Здесь — то, что принадлежит пробе: пул, лента,
// сужатель потока, диагностическая поверхность и объявление о себе.
func runServe(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	reg := newRegistry()
	p, err := assemble(ctx, cfg, logger, pool, reg)
	if err != nil {
		return err
	}
	defer p.close()

	desc, err := describe(cfg, logger, p.ports)
	if err != nil {
		return err
	}
	// Самоотчёт о посадке — после принятия дескриптора и до подъёма слушателей.
	observability.LogBootPosture(logger, bootPosture(cfg, p.ports.identity, desc.HostForm().String()))

	healthAgg := health.New(buildReadinessCheckers(pool,
		schemaguard.CheckFromFS(probemigrations.FS, schemaguard.PgxVersionReader(pool))))
	go func() {
		<-ctx.Done()
		healthAgg.SetShuttingDown()
	}()
	diagDesc, err := describeDiagnosticSurface(cfg.MetricsAddr, reg, healthAgg, desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	// Своя отмена: поверхность гасится ПОСЛЕ слушателей, а не вместе с ними.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	waitDiag, derr := servicehost.ServeSurface(diagCtx, diagDesc)
	if derr != nil {
		stopDiag()
		return fmt.Errorf("диагностическая поверхность: %w", derr)
	}

	serveErr := servicehost.Serve(ctx, desc,
		registerPublic,
		func(r grpc.ServiceRegistrar) { registerInternal(r, p.ports) },
	)

	stopDiag()
	if derr := waitDiag(); derr != nil {
		logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
		if serveErr == nil {
			serveErr = derr
		}
	}
	return serveErr
}

// probe — собранные части пробы до объявления о себе.
type probe struct {
	ports servePorts
	close func()
}

// assemble собирает ленту, глагол пробы, сужатель потока и звено идентичности и поднимает
// уборщиков. Объявление о себе (describe) строится из возвращённых портов.
// close гасит уборщиков (с ожиданием) и ребро сужателя — до закрытия пула.
func assemble(ctx context.Context, cfg config.Config, logger *slog.Logger,
	pool *pgxpool.Pool, reg prometheus.Registerer) (probe, error) {
	narrower, closeNarrower, err := buildNarrower(cfg)
	if err != nil {
		return probe{}, err
	}
	authzCache := &authzmetrics.Source{}
	registerAuthzCollectors(reg, authzCache, narrower)

	parts, err := buildFeed(cfg, pool, narrower, reg, logger)
	if err != nil {
		closeNarrower()
		return probe{}, err
	}
	identity, err := serviceIdentityAxis(cfg, parts)
	if err != nil {
		closeNarrower()
		return probe{}, err
	}
	stopSweeps, err := startFeedSweeps(ctx, pool, reg, logger)
	if err != nil {
		closeNarrower()
		return probe{}, err
	}
	return probe{
		ports: servePorts{
			parts:        parts,
			send:         send.New(pool, parts.source, logger.With(slog.String("component", "probe_send"))),
			identity:     identity,
			narrower:     narrower,
			authzObserve: authzCache.Install,
			metrics:      reg,
		},
		close: func() {
			stopSweeps()
			closeNarrower()
		},
	}, nil
}

// buildNarrower — сужатель потока подписки: видимость строки ленты
// спрашивается у владельца модели отношением reader на notification_feed —
// тем же, которого каталог прав требует у Claim и Ack. При выключенной
// доставке потока нет, и сужатель не строится.
func buildNarrower(cfg config.Config) (*listnarrow.Narrower, func(), error) {
	if !cfg.Notifications.On() {
		return nil, func() {}, nil
	}
	creds, err := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
	if err != nil {
		return nil, nil, fmt.Errorf("notify-probe→iam narrowing mTLS creds: %w", err)
	}
	conn, err := grpc.NewClient(cfg.AuthZIAMGRPCAddr, grpc.WithTransportCredentials(creds),
		grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return nil, nil, fmt.Errorf("notify-probe→iam narrowing edge %s: %w", cfg.AuthZIAMGRPCAddr, err)
	}
	n := listnarrow.New(narrowiam.New(conn), listnarrow.Config{
		Relations: authzfilter.PageRelations,
		Timeout:   cfg.AuthZCheckTimeout,
		CacheTTL:  cfg.AuthZCacheTTL,
	})
	return n, func() { _ = conn.Close() }, nil
}
