// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/handler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// lroDrainTimeout — граница graceful-дренажа in-flight LRO-worker'ов на SIGTERM
// (не оставляем async-мутацию done=false навсегда — клиент завис бы в polling).
const lroDrainTimeout = 30 * time.Second

// runServe — composition root: ЕДИНСТВЕННОЕ место wiring (без глобальных синглтонов
// вне cmd). Поднимает pgxpool, LRO-worker, peer-клиентов, два gRPC-листенера
// (public :9090 + internal :9091) с идентичными interceptor-цепочками, health и
// diagnostic HTTP, затем graceful shutdown.
func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	// ── БД + LRO-стек ─────────────────────────────────────────────────────
	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// Общая operations-таблица (corelib) каталога kacho_storage. Admin/tenant
	// async-мутации пишут LRO-строку; фоновый worker финализирует; клиент поллит
	// OperationService.Get(id).
	opsRepo := operations.NewRepo(pool, config.DBSchema)
	if err = operations.ConfigureDefault(
		operations.WithLogger(logger),
	); err != nil {
		return fmt.Errorf("configure LRO worker: %w", err)
	}
	operations.Start()

	// ── peer-клиенты (runtime cross-domain edges) ─────────────────────────
	geoConn, err := dialPeer(cfg.GeoGRPCAddr, cfg.GeoClientMTLS, logger, "geo")
	if err != nil {
		return err
	}
	if geoConn != nil {
		defer geoConn.Close()
	}
	iamConn, err := dialPeer(cfg.IAMGRPCAddr, cfg.IAMClientMTLS, logger, "iam")
	if err != nil {
		return err
	}
	if iamConn != nil {
		defer iamConn.Close()
	}
	geoClient := clients.NewGeoClient(geoConn)
	iamClient := clients.NewIAMClient(iamConn)

	// ── use-cases (repo → use-case → handler). CQRS reader/writer связываются
	// раздельно (сейчас обе стороны — один pg-adapter). errStatus — transport-
	// mapper sentinel→gRPC, инжектится из handler-слоя (serviceerr.ToStatus). ──
	volumeRepo := pg.NewVolumeRepo(pool)
	snapshotRepo := pg.NewSnapshotRepo(pool)
	diskTypeRepo := pg.NewDiskTypeRepo(pool)
	volumeUC := volume.New(volumeRepo, volumeRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus)
	snapshotUC := snapshot.New(snapshotRepo, iamClient, opsRepo, serviceerr.ToStatus)
	diskTypeUC := disktype.New(diskTypeRepo)

	// ── authz: per-RPC InternalIAMService.Check на ОБОИХ листенерах (AuthN+AuthZ
	// везде — internal :9091 НЕ освобождён, security.md). Ребро storage→iam Check
	// дозванивается в kacho-iam internal (:9091, mTLS). Пустой AuthZIAMGRPCAddr →
	// authz-интерсептор не подключается (грациозный dev-старт без kacho-iam);
	// production ОБЯЗАН задать адрес (security-долг иначе). ──
	authzConn, err := dialPeer(cfg.AuthZIAMGRPCAddr, cfg.IAMClientMTLS, logger, "iam-authz")
	if err != nil {
		return err
	}
	if authzConn != nil {
		defer authzConn.Close()
	}
	var authzUnary grpc.UnaryServerInterceptor
	var authzStream grpc.StreamServerInterceptor
	if authzConn != nil {
		authzIntr, aerr := check.NewInterceptor(check.Options{
			ServiceName: "kacho-storage",
			IAMConn:     authzConn,
			Logger:      logger,
		})
		if aerr != nil {
			return fmt.Errorf("build authz interceptor: %w", aerr)
		}
		authzUnary = authzIntr.Unary()
		authzStream = authzIntr.Stream()
		logger.Info("authz interceptor enabled", "iam_authz_endpoint", cfg.AuthZIAMGRPCAddr, "listeners", "public+internal")
	} else {
		logger.Warn("authz Check NOT configured (KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR empty) — dev only; production MUST enable per-RPC Check")
	}

	// ── FGA owner-tuple register-drainer + sync-registrar (SEC-D, анти-BOLA) ──
	// Volume/Snapshot Create/Delete эмитят register/unregister-intent в
	// kacho_storage.fga_register_outbox (writer-TX). register-drainer применяет их
	// через kacho-iam RegisterResource/UnregisterResource (тот же :9091 mTLS-conn,
	// что и authz-Check — RegisterResource Internal-only, ban #6). sync-registrar
	// регистрирует owner-tuple сразу после Create-commit (immediate анти-BOLA-резолв,
	// без гонки с async drainer'ом; drainer — at-least-once backstop). authzConn nil
	// (dev/no-iam) или drainer выключен → путь пропускается, intents durable.
	if cfg.FGARegisterDrainerEnabled && authzConn != nil {
		if derr := startRegisterDrainer(ctx, pool, authzConn, logger); derr != nil {
			return fmt.Errorf("start register-drainer: %w", derr)
		}
		syncRegistrar := clients.NewSyncRegistrar(iamv1.NewInternalIAMServiceClient(authzConn))
		volumeUC.WithRegistrar(syncRegistrar)
		snapshotUC.WithRegistrar(syncRegistrar)
	} else {
		logger.Warn("FGA register-drainer NOT started (disabled or authz.iam-addr empty) — " +
			"owner-tuple register-intents stay durable in fga_register_outbox until configured")
	}

	// ── interceptor-цепочки обоих листенеров (recovery→logging→principal→authz).
	// forwarders — allow-list SAN'ов доверенных форвардеров (production пинит
	// api-gateway SAN); пустой в dev-скелете. ──
	forwarders := []string{}
	publicCreds, err := cfg.PublicServerCreds()
	if err != nil {
		return fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := cfg.InternalServerCreds()
	if err != nil {
		return fmt.Errorf("internal listener tls creds: %w", err)
	}

	grpcSrv := grpcsrv.NewServer(
		publicCreds,
		grpc.ChainUnaryInterceptor(unaryChain(logger, forwarders, authzUnary)...),
		grpc.ChainStreamInterceptor(streamChain(logger, forwarders, authzStream)...),
	)
	internalSrv := grpcsrv.NewServer(
		internalCreds,
		grpc.ChainUnaryInterceptor(unaryChain(logger, forwarders, authzUnary)...),
		grpc.ChainStreamInterceptor(streamChain(logger, forwarders, authzStream)...),
	)

	// ── регистрация сервисов по листенерам + health на обоих ───────────────
	opHandler := handler.NewOperationHandler(opsRepo)
	registerServices(grpcSrv, internalSrv, volumeUC, snapshotUC, diskTypeUC, opHandler)
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)
	healthpb.RegisterHealthServer(internalSrv, healthSrv)

	// ── listeners ──────────────────────────────────────────────────────────
	listener, err := net.Listen("tcp", ":"+cfg.GrpcPort)
	if err != nil {
		return err
	}
	internalListener, err := net.Listen("tcp", ":"+cfg.InternalGrpcPort)
	if err != nil {
		_ = listener.Close()
		return err
	}
	logger.Info("kacho-storage listening",
		"public_mtls", cfg.PublicServerMTLS.Enable,
		"internal_mtls", cfg.InternalServerMTLS.Enable,
		"public_port", cfg.GrpcPort,
		"internal_port", cfg.InternalGrpcPort)

	// ── cluster-internal diagnostic HTTP (/healthz). Пустой addr отключает. ──
	diagTask, diagShutdown, err := startDiagnosticListener(cfg.MetricsAddr, logger)
	if err != nil {
		_ = listener.Close()
		_ = internalListener.Close()
		return fmt.Errorf("diagnostic listener: %w", err)
	}
	if diagTask != nil {
		go func() {
			if derr := diagTask(); derr != nil {
				logger.Error("diagnostic listener stopped", "err", derr)
			}
		}()
	}

	// ── graceful shutdown: gRPC GracefulStop обоих листенеров + drain LRO ──
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		diagShutdown(context.Background())
		internalSrv.GracefulStop()
		grpcSrv.GracefulStop()
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), lroDrainTimeout)
		defer cancelDrain()
		if werr := operations.Wait(drainCtx); werr != nil {
			logger.Warn("LRO workers did not finish before shutdown timeout",
				"err", werr, "active", operations.Active())
		}
	}()

	// internal-листенер на фоновой goroutine; фатальный крах :9091 сносит процесс
	// (cancel root-ctx) и учитывается в exit-коде наравне с public.
	internalErrCh := make(chan error, 1)
	go func() {
		internalErrCh <- runInternalListener(internalSrv, internalListener, cancel, logger)
	}()

	serveErr := grpcSrv.Serve(listener)
	cancel()
	<-shutdownDone
	return serveResult(serveErr, <-internalErrCh)
}

// registerServices раскладывает сервисы по листенерам: public (Volume/Snapshot/
// DiskType) — на :9090; Internal* (InternalVolume/InternalDiskType) — ТОЛЬКО на
// cluster-internal :9091 (ban #6); OperationService (LRO poll) — на обоих.
func registerServices(
	publicSrv, internalSrv grpc.ServiceRegistrar,
	volumeUC *volume.UseCase,
	snapshotUC *snapshot.UseCase,
	diskTypeUC *disktype.UseCase,
	opHandler operationpb.OperationServiceServer,
) {
	storagev1.RegisterVolumeServiceServer(publicSrv, handler.NewVolumeHandler(volumeUC))
	storagev1.RegisterSnapshotServiceServer(publicSrv, handler.NewSnapshotHandler(snapshotUC))
	storagev1.RegisterDiskTypeServiceServer(publicSrv, handler.NewDiskTypeHandler(diskTypeUC))
	storagev1.RegisterInternalVolumeServiceServer(internalSrv, handler.NewInternalVolumeHandler(volumeUC))
	storagev1.RegisterInternalDiskTypeServiceServer(internalSrv, handler.NewInternalDiskTypeHandler(diskTypeUC))
	operationpb.RegisterOperationServiceServer(publicSrv, opHandler)
	operationpb.RegisterOperationServiceServer(internalSrv, opHandler)
}

// dialPeer лениво создаёт *grpc.ClientConn к peer-сервису (per-edge mTLS). Пустой
// addr → (nil, nil): ребро не сконфигурировано, клиент fail-closed (dev-скелет).
// grpc.NewClient не блокирует до первого RPC — peer может быть недоступен на старте.
func dialPeer(addr string, tls grpcclient.TLSClient, logger *slog.Logger, name string) (*grpc.ClientConn, error) {
	if addr == "" {
		logger.Warn("peer edge not configured; client fail-closed", "peer", name)
		return nil, nil
	}
	creds, err := grpcclient.TLSClientCreds(tls)
	if err != nil {
		return nil, fmt.Errorf("storage→%s mTLS creds: %w", name, err)
	}
	conn, err := grpc.NewClient(addr, creds, grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return nil, fmt.Errorf("dial kacho-%s: %w", name, err)
	}
	logger.Info("peer edge configured", "peer", name, "addr", addr)
	return conn, nil
}

// startDiagnosticListener поднимает cluster-internal HTTP-listener (/healthz).
// Пустой addr → (nil, no-op): отключён. net.Listen синхронный — ошибка привязки
// видна вызывающему сразу.
func startDiagnosticListener(addr string, logger *slog.Logger) (task func() error, shutdown func(context.Context), err error) {
	if addr == "" {
		return nil, func(context.Context) {}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	lis, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		return nil, nil, lerr
	}
	logger.Info("kacho-storage diagnostic listener", "endpoint", addr, "paths", "/healthz")
	task = func() error {
		if serr := srv.Serve(lis); serr != nil && serr != http.ErrServerClosed {
			return serr
		}
		return nil
	}
	shutdown = func(sctx context.Context) { _ = srv.Shutdown(sctx) }
	return task, shutdown, nil
}

// runInternalListener обслуживает internal :9091 и зеркалит lifecycle public-
// листенера: фатальная (не graceful) ошибка Serve сносит процесс через cancel()
// И возвращается вызывающему, чтобы её крах дал non-zero exit (serveResult).
func runInternalListener(srv gracefulServer, lis net.Listener, cancel context.CancelFunc, logger *slog.Logger) error {
	if serr := srv.Serve(lis); serr != nil && !errors.Is(serr, grpc.ErrServerStopped) {
		logger.Error("internal grpc server stopped; tearing down process", "err", serr)
		cancel()
		return serr
	}
	return nil
}

// gracefulServer — минимальный контракт grpc-сервера, нужный runInternalListener.
type gracefulServer interface {
	Serve(net.Listener) error
}

// serveResult сводит exit-ошибку процесса: ошибка public-листенера приоритетна;
// иначе наверх идёт фатальная ошибка internal-листенера (её крах тоже даёт
// non-zero exit — симметрия public/internal).
func serveResult(publicErr, internalErr error) error {
	if publicErr != nil {
		return publicErr
	}
	return internalErr
}
