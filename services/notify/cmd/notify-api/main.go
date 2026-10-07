// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command notify-api — развёртывание службы notify, обслуживающее запросы
// оператора и арендаторов (приёмка NTF-5 Р2; NTF-3 Р8): внутренний сервис
// извещений `InternalNoticeService`, их чтение арендатором `NoticeService`,
// опрос операций `OperationService` — на единственном внутреннем mTLS-слушателе
// носителя (форма «только внутренний», Х5). Секрета почты и SMTP-клиента у
// развёртывания нет; справочник службы доступа оно не зовёт (З1).
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/schemaguard"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/migrations"
)

const usage = "usage: notify-api {serve}"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	err := run(ctx, os.Args[1:], os.Stdout)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

// run — процесс целиком: разбор команды, страж конфигурации (до первого
// обращения к базе и до подъёма слушателей), подъём.
func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "serve":
		cfg, err := config.LoadAPI()
		if err != nil {
			return err
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		logger := observability.NewSlogger(out)
		slog.SetDefault(logger)
		return runServe(ctx, cfg, logger)
	default:
		return fmt.Errorf("unknown command %q (%s)", args[0], usage)
	}
}

// runServe — композиционный корень процесса: пул, диагностическая
// поверхность, удостоверения, затем носитель (serveAPI).
func runServe(ctx context.Context, cfg config.API, logger *slog.Logger) error {
	mode, err := cfg.Mode()
	if err != nil {
		return err
	}
	kanameCreds, err := grpcclient.TLSClientTransportCreds(grpcclient.TLSClient{
		Enable: true, CertFile: cfg.PeerTLSCertFile, KeyFile: cfg.PeerTLSKeyFile,
		CAFiles: []string{cfg.PeerTLSCAFile},
	})
	if err != nil {
		return fmt.Errorf("notify-api→kaname mTLS: %w", err)
	}

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return fmt.Errorf("пул базы %s: %w", cfg.DBName, err)
	}
	defer pool.Close()

	reg := newRegistry()
	agg := health.New([]health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: schemaguard.CheckerName, Check: schemaguard.CheckFromFS(migrations.FS, schemaguard.PgxVersionReader(pool))},
	})
	go func() {
		<-ctx.Done()
		agg.SetShuttingDown()
	}()
	diag, err := describeDiagnosticSurface(cfg.DiagAddr, reg, agg, mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	// Своя отмена: поверхность гасится ПОСЛЕ слушателя, а не вместе с ним.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	waitDiag, err := servicehost.ServeSurface(diagCtx, diag)
	if err != nil {
		stopDiag()
		return fmt.Errorf("диагностическая поверхность: %w", err)
	}

	serveErr := serveAPI(ctx, apiInputs{
		ListenAddr: ":" + cfg.InternalPort,
		ServerTLS: grpcsrv.TLSServer{Enable: true, CertFile: cfg.InternalServerCertFile,
			KeyFile: cfg.InternalServerKeyFile, ClientCAFiles: cfg.InternalServerClientCAFiles},
		TrustDomain:          cfg.AuthzTrustDomain,
		TrustedForwarderSANs: cfg.AuthzTrustedForwarderSANs,
		TrustAnyForwarder:    cfg.AuthzTrustAnyForwarder,
		Mode:                 mode,
		DBSSLMode:            coredb.SSLModeFromDSN(cfg.DSN()),
		Pool:                 pool,
		Now:                  time.Now,
		KanameAddr:           cfg.AuthzIAMGRPCAddr,
		KanameCreds:          kanameCreds,
		AuthzCacheTTL:        cfg.AuthzCacheTTL,
		AuthzCheckTimeout:    cfg.AuthzCheckTimeout,
		AuthzDenyBudget:      cfg.AuthzDenyBudgetPerSec,
		HandlingBudget:       cfg.HandlingBudget,
		ReminderLead:         cfg.NoticeReminderLead,
		ListFilterCacheTTL:   cfg.ListFilterCacheTTL,
		Metrics:              reg,
		Logger:               logger,
	})

	stopDiag()
	if derr := waitDiag(); derr != nil {
		logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
		if serveErr == nil {
			serveErr = derr
		}
	}
	return serveErr
}

var (
	buildVersion = ""
	buildCommit  = ""
)

// newRegistry — реестр величин процесса с метаданными сборки.
func newRegistry() *prometheus.Registry {
	version, commit := observability.NormalizeBuildStamp(buildVersion, buildCommit)
	reg := prometheus.NewRegistry()
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kacho_" + metricsPrefix + "_build_info",
		Help:        "Build metadata of the running notify-api binary (constant 1).",
		ConstLabels: prometheus.Labels{"version": version, "commit": commit},
	})
	buildInfo.Set(1)
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		buildInfo,
	)
	return reg
}
