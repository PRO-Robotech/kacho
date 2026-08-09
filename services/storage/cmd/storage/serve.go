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

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/handler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/storage/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
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

	// ── secure-by-default boot-guard (#56) ────────────────────────────────
	// В production/production-strict refuse-to-start при insecure-конфиге: без
	// mTLS на обоих листенерах, без per-RPC authz Check (пустой AuthZIAMGRPCAddr)
	// или с plaintext-DB (sslmode=disable). Ранее AuthMode был dead-code →
	// storage единственным boot'ился insecure с одним WARN. Fail-closed ДО listen
	// (security.md «AuthN+AuthZ ВЕЗДЕ + любой деплой — production-mode»).
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("insecure configuration refused: %w", err)
	}
	// Самоотчёт о security-posture (эталон контракта; добавлено поле `service`,
	// чтобы все восемь сервисов были идентичны по форме — см.
	// observability.BootPosture).
	observability.LogBootPosture(logger, bootPosture(cfg))
	if cfg.AuthMode == "dev" && (cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable") {
		logger.Warn("KACHO_STORAGE_DB_SSLMODE=disable — DB plaintext (dev only; never on a deployed stand)")
	}

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

	// Приватный prometheus-реестр. Скрейпится ТОЛЬКО с cluster-internal
	// diagnostic-порта; ServiceMonitor чарта нацелен именно на него.
	svcMetrics := metrics.New()

	// ── use-cases (repo → use-case → handler). CQRS reader/writer связываются
	// раздельно (сейчас обе стороны — один pg-adapter). errStatus — transport-
	// mapper sentinel→gRPC, инжектится из handler-слоя (serviceerr.ToStatus). ──
	volumeRepo := pg.NewVolumeRepo(pool)
	snapshotRepo := pg.NewSnapshotRepo(pool)
	imageRepo := pg.NewImageRepo(pool)
	diskTypeRepo := pg.NewDiskTypeRepo(pool)
	volumeUC := volume.New(volumeRepo, volumeRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus)
	snapshotUC := snapshot.New(snapshotRepo, iamClient, opsRepo, serviceerr.ToStatus)
	imageUC := image.New(imageRepo, imageRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus)
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
			CacheTTL:    cfg.AuthZCacheTTL,
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

	// ── per-object list-filter публичного List (анти-over-show) ───────────────
	// Per-RPC Check выше гейтит List на project-tier `viewer` — «вправе ли листать
	// ЭТОТ проект». Сужение страницы до объектов, на которые есть грант
	// (per-object `viewer` батчем по прочитанной странице — то же отношение, что
	// энфорсит Get), делает ТОЛЬКО этот фильтр.
	// Без него любой член проекта видел КАЖДЫЙ том/снимок/образ проекта. Тот же
	// authzConn (kacho-iam internal :9091, mTLS) — там живёт AuthorizeService.
	// Production boot-guard (config.Validate) не пускает старт с выключенным
	// фильтром, поэтому nil здесь возможен только в dev.
	//
	// Тот же клиент модели прав отвечает и на ВТОРОЙ вопрос запросов Attach/Detach —
	// про ИНСТАНС, в чей набор привязок пишется строка (`v_update` / `v_update ∪
	// v_delete` на `compute_instance`, см. volume.requireInstanceControl). Это не
	// «фильтр списка», а гейт мутации, поэтому порт отдельный (authzfilter.ObjectGate)
	// и fail-open к нему не применяется; вопрос идёт в ту же модель, вызова в compute
	// не происходит, ацикличность держится.
	//
	// Нулевой указатель раскладывается в НУЛЕВЫЕ интерфейсы намеренно: typed-nil в
	// интерфейсе не равен nil, и проверки вида `filter == nil` у потребителей
	// молча перестали бы срабатывать.
	fgaFilter := buildListFilter(cfg, authzConn, logger)
	var (
		listFilter   authzfilter.Filter
		instanceGate authzfilter.ObjectGate
	)
	if fgaFilter != nil {
		listFilter, instanceGate = fgaFilter, fgaFilter
	}
	volumeUC.WithListFilter(listFilter).WithInstanceGate(instanceGate)
	snapshotUC.WithListFilter(listFilter)
	imageUC.WithListFilter(listFilter)

	// ── FGA owner-tuple register-drainer + sync-registrar (SEC-D, анти-BOLA) ──
	// Volume/Snapshot Create/Delete эмитят register/unregister-intent в
	// kacho_storage.fga_register_outbox (writer-TX). register-drainer применяет их
	// через kacho-iam RegisterResource/UnregisterResource (тот же :9091 mTLS-conn,
	// что и authz-Check — RegisterResource Internal-only, ban #6). sync-registrar
	// регистрирует owner-tuple сразу после Create-commit (immediate анти-BOLA-резолв,
	// без гонки с async drainer'ом; drainer — at-least-once backstop). authzConn nil
	// (dev/no-iam) или drainer выключен → путь пропускается, intents durable.
	if cfg.FGARegisterDrainerEnabled && authzConn != nil {
		if derr := startRegisterDrainer(ctx, pool, authzConn, svcMetrics, logger); derr != nil {
			return fmt.Errorf("start register-drainer: %w", derr)
		}
		// Отравление обязано быть паузой, а не потерей: без периодического
		// redrive недоставленная регистрация оставляет ресурс без mirror-строки в
		// kacho-iam, а значит без owner-tuple и без материализованных глаголов —
		// невидимым для authz до ручной правки БД. См. redrive_backstop.go.
		if derr := startRedriveBackstop(ctx, pool, logger); derr != nil {
			return fmt.Errorf("start redrive backstop: %w", derr)
		}
		syncRegistrar := clients.NewSyncRegistrar(iamv1.NewInternalIAMServiceClient(authzConn))
		volumeUC.WithRegistrar(syncRegistrar)
		snapshotUC.WithRegistrar(syncRegistrar)
		imageUC.WithRegistrar(syncRegistrar)
	} else {
		logger.Warn("FGA register-drainer NOT started (disabled or authz.iam-addr empty) — " +
			"owner-tuple register-intents stay durable in fga_register_outbox until configured")
	}

	// ── разрешитель осиротевших операций (durable LRO recovery) ───────────────
	// Стартовое восстановление прогоняется ДО приёма трафика, периодический проход —
	// подстраховка. Без него строка операции, пережившая смерть процесса (перекат,
	// OOM, исчерпание бюджета терминальной записи) или так и не дождавшаяся места в
	// очереди исполнителя, остаётся «в процессе» НАВСЕГДА, и клиент не узнаёт исхода
	// ни разу. Частичный индекс под запрос этого прохода схема несла с самого начала
	// (миграция 0002) — разрешитель был заявлен раньше, чем провязан. См. recovery.go.
	//
	// Не зависит ни от kacho-iam, ни от дренажа регистраций: это сверка со СВОЕЙ БД.
	lroReconciler := startLRORecovery(ctx, pool, operationresolver.Readers{
		Volume:   volumeRepo,
		Snapshot: snapshotRepo,
		Image:    imageRepo,
	}, logger)
	go lroReconciler.Run(ctx)

	// ── interceptor-цепочки обоих листенеров (recovery→logging→principal→authz).
	//
	// forwarders — круг отправителей, которым разрешено ПЕРЕДАВАТЬ личность
	// конечного пользователя (x-kacho-principal-*). Значение приходит из
	// конфигурации и НИКОГДА не задаётся здесь литералом: пустой список для corelib
	// означает не «никому», а «кому угодно, у кого есть валидный клиентский
	// сертификат» (pkg/grpcsrv principalIsTrusted сужает круг только на непустом
	// списке), и переданная личность становится субъектом проверки прав. Боевой
	// режим на пустом списке не стартует (config.Validate).
	//
	// Законных отправителей два, оба пинятся в values.prod: api-gateway (публичный
	// :9090) и compute (внутренний :9091 — привязка тома идёт под личностью того,
	// кто её инициировал). Список общий для обоих листенеров: внутренний периметр
	// не освобождён.
	forwarders := cfg.TrustedForwarders()
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

	// ── регистрация сервисов по листенерам ─────────────────────────────────
	// health (grpc.health.v1.Health, SERVING) + reflection уже регистрируются
	// внутри grpcsrv.NewServer для КАЖДОГО сервера (pkg/grpcsrv/server.go) — как у
	// vpc/compute/nlb/registry. Повторная RegisterHealthServer здесь роняла процесс
	// на старте: "duplicate service registration for grpc.health.v1.Health".
	opHandler := handler.NewOperationHandler(opsRepo)
	registerServices(grpcSrv, internalSrv, volumeUC, snapshotUC, imageUC, diskTypeUC, opHandler)

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

	// ── cluster-internal diagnostic HTTP (/healthz, /metrics). Пустой addr отключает. ──
	diagTask, diagShutdown, err := startDiagnosticListener(cfg.MetricsAddr, svcMetrics, logger)
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
// Image/DiskType) — на :9090; Internal* (InternalVolume/InternalImage/
// InternalDiskType) — ТОЛЬКО на cluster-internal :9091 (ban #6); OperationService
// (LRO poll) — на обоих. Каждый зарегистрированный здесь RPC ОБЯЗАН иметь запись в
// check.PermissionMap (иначе authz-интерсептор fail-closed'ит его «rpc not mapped»).
func registerServices(
	publicSrv, internalSrv grpc.ServiceRegistrar,
	volumeUC *volume.UseCase,
	snapshotUC *snapshot.UseCase,
	imageUC *image.UseCase,
	diskTypeUC *disktype.UseCase,
	opHandler operationpb.OperationServiceServer,
) {
	storagev1.RegisterVolumeServiceServer(publicSrv, handler.NewVolumeHandler(volumeUC))
	storagev1.RegisterSnapshotServiceServer(publicSrv, handler.NewSnapshotHandler(snapshotUC))
	storagev1.RegisterImageServiceServer(publicSrv, handler.NewImageHandler(imageUC))
	storagev1.RegisterDiskTypeServiceServer(publicSrv, handler.NewDiskTypeHandler(diskTypeUC))
	storagev1.RegisterInternalVolumeServiceServer(internalSrv, handler.NewInternalVolumeHandler(volumeUC))
	storagev1.RegisterInternalImageServiceServer(internalSrv, handler.NewInternalImageHandler(imageUC))
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

// diagnosticMux — маршруты cluster-internal diagnostic-листенера. Вынесен из
// startDiagnosticListener, чтобы то, ЧТО отдаётся, можно было проверить без сети:
// расхождение между объявленным в чарте скрейпом и реально обслуживаемым путём
// иначе замечается только на живом Prometheus (см. diagnostic_metrics_test.go).
func diagnosticMux(m *metrics.Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", m.Handler())
	return mux
}

// startDiagnosticListener поднимает cluster-internal HTTP-listener
// (/healthz, /metrics). Пустой addr → (nil, no-op): отключён. net.Listen
// синхронный — ошибка привязки видна вызывающему сразу.
func startDiagnosticListener(addr string, m *metrics.Metrics, logger *slog.Logger) (task func() error, shutdown func(context.Context), err error) {
	if addr == "" {
		return nil, func(context.Context) {}, nil
	}
	srv := &http.Server{Addr: addr, Handler: diagnosticMux(m), ReadHeaderTimeout: 5 * time.Second}
	lis, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		return nil, nil, lerr
	}
	logger.Info("kacho-storage diagnostic listener", "endpoint", addr, "paths", "/healthz,/metrics")
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

// buildListFilter собирает per-object фильтр видимости публичного List
// (kacho-iam AuthorizeService.BatchCheck по id ПРОЧИТАННОЙ страницы).
//
// nil (⇒ use-case делает passthrough) возможен только в dev: production boot-guard
// (config.Validate) требует и ListFilterEnabled=true, и непустой AuthZIAMGRPCAddr,
// поэтому «тихо выключить фильтр на развёрнутом стенде» нельзя.
//
// Логируются ВСЕ три числа таймингов: per-call дедлайн гейтит ОДИН BatchCheck,
// operation_budget — фильтрацию всей страницы (выводится из per-call и
// параллелизма), worst_case_depth — сколько волн он покрывает. По одному конфигу не
// видно, какое из них реально ограничивает запрос.
// Возвращается КОНКРЕТНЫЙ тип, а не интерфейс: один и тот же клиент модели прав
// обслуживает два разных порта (видимость страницы и гейт мутации по названному
// объекту), и вызывающий раскладывает его сам. Возврат интерфейса заставил бы
// приводить типы либо строить второй клиент к тому же эндпоинту.
func buildListFilter(cfg config.Config, authzConn *grpc.ClientConn, logger *slog.Logger) *authzfilter.FGAFilter {
	if !cfg.ListFilterEnabled {
		logger.Warn("list filter DISABLED (KACHO_STORAGE_LIST_FILTER_ENABLED=false) — " +
			"public List returns every row of the project regardless of per-object grants; dev only")
		return nil
	}
	if authzConn == nil {
		logger.Warn("list filter requested but KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR is unset — disabled")
		return nil
	}
	f := authzfilter.NewFGAFilter(
		authzfilter.NewIAMAuthorizeClient(authzConn),
		authzfilter.Config{
			Enabled:         true,
			Timeout:         time.Duration(cfg.ListFilterTimeoutMs) * time.Millisecond,
			CacheTTL:        time.Duration(cfg.ListFilterCacheTTLMs) * time.Millisecond,
			CacheMaxEntries: cfg.ListFilterCacheMaxEntries,
			FailOpen:        cfg.ListFilterFailOpen,
		},
	).WithLogger(logger)
	logger.Info("list filter enabled",
		"iam_authorize_endpoint", cfg.AuthZIAMGRPCAddr,
		"per_call_timeout_ms", cfg.ListFilterTimeoutMs,
		"batch_parallelism", f.Parallelism(),
		"operation_budget", f.Budget(),
		"worst_case_depth_waves", f.WorstCaseDepth(),
		"cache_ttl_ms", cfg.ListFilterCacheTTLMs,
		"cache_max_entries", cfg.ListFilterCacheMaxEntries,
		"fail_open", cfg.ListFilterFailOpen)
	return f
}
