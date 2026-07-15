// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/check"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// runServe — composition root: единственное место wiring, без глобальных синглтонов.
func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	if err := validateAuthMode(cfg, logger); err != nil {
		return err
	}
	// Secure-by-default: per-RPC authz Check и mTLS на ОБОИХ листенерах
	// обязательны. Единственный способ запустить операции без авторизации и mTLS —
	// аварийный KACHO_GEO_AUTHZ_BREAKGLASS=true.
	if err := validateSecurityConfig(cfg); err != nil {
		return err
	}

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// ── observability: Prometheus-адаптер LRO-durability метрик (worker
	// terminal-write retries/failures + inflight, reconciler runs/errors/orphans).
	// Реальный Recorder заменяет NopRecorder default-registry — иначе исчезал
	// сигнал «async Region/Zone мутация не финализуется» (terminal-write
	// exhaustion оставляет durable done=false, клиент виснет в polling).
	metricsAdapter := metrics.New(buildVersion, buildCommit)

	// ── LRO-стек: общая operations-таблица (corelib) каталога kacho-geo.
	// Admin-мутации Region/Zone async — UseCase пишет LRO-строку и запускает
	// фоновый worker; клиент поллит OperationService.Get(id).
	opsRepo := operations.NewRepo(pool, "kacho_geo")

	// Подключаем Recorder+Logger к package-level default-registry LRO-worker'а и
	// поднимаем его dispatcher-loop ДО приёма трафика (ConfigureDefault→Start).
	// dispatch/drain через operations.Run/operations.Wait уже целятся в
	// default-registry — здесь добавляется только недостающая recorder/lifecycle
	// проводка.
	if err = startLROWorker(metricsAdapter, logger); err != nil {
		return err
	}

	// ── use-cases (repo → use-case → handler) ──────────────────────────────
	// CQRS-порты Reader/Writer связываются раздельно (сейчас обе стороны — один
	// pg-adapter поверх primary-pool; read-side можно позже перецепить на
	// read-replica pool, не трогая use-case). errStatus — transport-mapper
	// sentinel→gRPC-status, инжектится из handler-слоя (serviceerr.ToStatus): выбор
	// кода — transport-concern, use-case его не выбирает.
	regionRepo := pg.NewRegionRepo(pool)
	regionUC := region.New(regionRepo, regionRepo, opsRepo, serviceerr.ToStatus)
	zoneRepo := pg.NewZoneRepo(pool)
	zoneUC := zone.New(zoneRepo, zoneRepo, opsRepo, serviceerr.ToStatus)

	// ── durable LRO recovery: доменный resolver + corelib-reconciler поверх
	// schema kacho_geo. RecoverAll прогоняется ЗДЕСЬ (до приёма трафика) —
	// осиротевшие после краха процесса done=false строки разрешаются в терминал
	// по committed-реальности ресурса; периодический Run(ctx) ниже — backstop.
	// Это тот backstop, который обещает комментарий про shutdown-drain (worker
	// добирает только свои in-flight; crash mid-op закрывает reconciler).
	lroReconciler := startLRORecovery(ctx, pool, regionRepo, zoneRepo, metricsAdapter, logger)

	// ── authz: per-RPC OpenFGA Check на ОБОИХ листенерах (AuthN+AuthZ везде —
	// internal :9091 НЕ освобожден). Ребро geo→iam Check дозванивается в
	// kacho-iam internal (:9091) с client-cert (mTLS). Check обязателен —
	// validateSecurityConfig выше уже гарантировал, что без breakglass адрес
	// kacho-iam задан; при breakglass=true интерсептор пропускает все RPC.
	var authzConn *grpc.ClientConn
	if cfg.AuthZIAMGRPCAddr != "" {
		authzCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
		if cerr != nil {
			return fmt.Errorf("geo→iam Check mTLS creds: %w", cerr)
		}
		authzConn, err = grpc.NewClient(cfg.AuthZIAMGRPCAddr,
			grpc.WithTransportCredentials(authzCreds),
			grpcclient.KeepaliveDialOption(true))
		if err != nil {
			return fmt.Errorf("dial kacho-iam (authz): %w", err)
		}
		defer authzConn.Close()
	}

	authzIntr, aerr := check.NewInterceptor(check.Options{
		ServiceName: "kacho-geo",
		// authzIAMConn(nil) → ИСТИННЫЙ nil интерфейса, а не typed-nil
		// (*grpc.ClientConn)(nil), обёрнутый в interface (который != nil). Иначе
		// guard `if opts.IAMConn == nil` в check.NewInterceptor не сработал бы,
		// ErrIAMConnNotConfigured-ветка ниже стала бы мёртвой, а при ослаблении
		// upstream-guard'а клиент построился бы поверх nil-conn и паникнул на
		// первом Check (CWE-476).
		IAMConn:    authzIAMConn(authzConn),
		Breakglass: cfg.AuthZBreakglass,
		Logger:     logger,
	})

	// ── цепочки интерсепторов ──────────────────────────────────────────────
	// WithTrustedForwarders ограничивает форвард end-user principal'а allow-list'ом
	// SAN'ов (api-gateway SA): verified-но-не-форвардер peer (внутренний сервис со
	// своим валидным client-cert'ом) НЕ может выдать себя за пользователя. Пустой
	// allow-list (default) сохраняет прежнее «любой verified peer доверен» (dev
	// back-compat) — enforce задаётся конфигом в production.
	forwarders := cfg.AuthZTrustedForwarderSANs
	// Оба листенера получают ОДНУ И ТУ ЖЕ trust-aware principal-цепочку
	// (cert-identity → trusted-principal с allow-list форвардеров) — единый source
	// wiring'а через newPrincipalInterceptors, чтобы public и internal не могли
	// разъехаться по anti-spoof-гарантии при рефакторинге. WithTrustedForwarders
	// ограничивает форвард end-user principal'а allow-list'ом SAN'ов (api-gateway):
	//   - Public (:9090): без этого любой mTLS-verified peer мог выставить
	//     произвольный x-kacho-principal-* header и авторизоваться как чужой
	//     viewer-principal (principal-spoofing, CWE-290). Consumer'ы vpc/compute/nlb
	//     ходят сюда со СВОИМ cert'ом — их principal не форвардится, снимается.
	//   - Internal (:9091): тот же per-RPC authz, что и на public — internal не
	//     доверенный (defense-in-depth против lateral movement). Эскалация
	//     verified-но-не-форвардер peer'а до admin-CRUD Region/Zone (confused-deputy)
	//     закрыта тем же allow-list'ом. Единственный легитимный форвардер — api-gateway.
	publicPrincipalUnary, publicPrincipalStream := newPrincipalInterceptors(forwarders)
	internalPrincipalUnary, internalPrincipalStream := newPrincipalInterceptors(forwarders)

	// authzUnary/authzStream — nil, если authz-интерсептор не сконфигурирован
	// (недостижимо в non-breakglass posture: validateSecurityConfig гарантирует
	// адрес kacho-iam; при breakglass authzIntr пропускает все RPC).
	var authzUnary grpc.UnaryServerInterceptor
	var authzStream grpc.StreamServerInterceptor
	switch {
	case aerr == nil && authzIntr != nil:
		authzUnary = authzIntr.Unary()
		authzStream = authzIntr.Stream()
		if cfg.AuthZBreakglass {
			logger.Warn("BREAKGLASS active: per-RPC authz Check bypassed on BOTH listeners (emergency mode)")
		} else {
			logger.Info("authz interceptor enabled",
				"iam_endpoint", cfg.AuthZIAMGRPCAddr,
				"listeners", "public+internal")
		}
	case errors.Is(aerr, check.ErrIAMConnNotConfigured):
		// Недостижимо при штатной конфигурации: validateSecurityConfig уже отказал
		// бы старту (нет authz и нет breakglass). Defensive fail-closed.
		return errors.New("authz Check required: set KACHO_GEO_AUTHZ_IAM_GRPC_ADDR (or KACHO_GEO_AUTHZ_BREAKGLASS=true to bypass)")
	case aerr != nil:
		return fmt.Errorf("build authz interceptor: %w", aerr)
	}

	// ── финальные упорядоченные цепочки обоих листенеров ───────────────────
	// Порядок собирается в assembleUnaryChain/assembleStreamChain (единая точка,
	// под guard-тестом serve_interceptor_order_test.go) вместо императивного
	// append/prepend по четырём срезам: recovery ПЕРВЫМ (outermost), затем
	// principal-extract, затем authz. Оба листенера строятся идентично.
	publicUnary := assembleUnaryChain(recoveryUnaryInterceptor(logger), publicPrincipalUnary, authzUnary)
	publicStream := assembleStreamChain(recoveryStreamInterceptor(logger), publicPrincipalStream, authzStream)
	internalUnary := assembleUnaryChain(recoveryUnaryInterceptor(logger), internalPrincipalUnary, authzUnary)
	internalStream := assembleStreamChain(recoveryStreamInterceptor(logger), internalPrincipalStream, authzStream)

	// ── server-creds (mTLS обязателен на обоих листенерах, кроме breakglass —
	// это проверено validateSecurityConfig выше) ──
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
		grpc.ChainUnaryInterceptor(publicUnary...),
		grpc.ChainStreamInterceptor(publicStream...),
	)
	internalSrv := grpcsrv.NewServer(
		internalCreds,
		grpc.ChainUnaryInterceptor(internalUnary...),
		grpc.ChainStreamInterceptor(internalStream...),
	)

	// Регистрация сервисов: public read-only на :9090, admin-CRUD Internal* ТОЛЬКО
	// на cluster-internal :9091 (запрет #6 — Internal.* не на внешнем endpoint),
	// OperationService (LRO poll) на обоих. Выделено в registerServices, чтобы
	// разделение public/internal было под тестом (см. serve_registration_test.go).
	opHandler := handler.NewOperationHandler(opsRepo)
	registerServices(grpcSrv, internalSrv, regionUC, zoneUC, opHandler)

	listener, err := net.Listen("tcp", ":"+cfg.GrpcPort)
	if err != nil {
		return err
	}
	internalListener, err := net.Listen("tcp", ":"+cfg.InternalGrpcPort)
	if err != nil {
		_ = listener.Close()
		return err
	}
	logger.Info("kacho-geo listening",
		"public_mtls", cfg.PublicServerMTLS.Enable,
		"internal_mtls", cfg.InternalServerMTLS.Enable,
		"public_port", cfg.GrpcPort,
		"internal_port", cfg.InternalGrpcPort)

	// ── cluster-internal diagnostic-listener (/metrics). Пустой MetricsAddr явно
	// отключает его (back-compat). Ошибка привязки порта видна синхронно здесь; сам
	// Serve крутится на фоновой goroutine и гасится diagShutdown на ctx.Done().
	diagTask, diagShutdown, err := startDiagnosticListener(cfg.MetricsAddr, metricsAdapter, logger)
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

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		diagShutdown(context.Background())
		internalSrv.GracefulStop()
		grpcSrv.GracefulStop()
		// Дренируем in-flight LRO-worker'ы: SIGTERM не должен оставить async-мутацию
		// done=false навсегда (клиент завис бы в polling). Свежий ctx — request-ctx
		// уже отменён возвратом Operation клиенту.
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelDrain()
		if werr := operations.Wait(drainCtx); werr != nil {
			logger.Warn("LRO workers did not finish before shutdown timeout",
				"err", werr, "active", operations.Active())
		}
	}()

	// Периодический backstop-sweep reconciler'а: sweep осиротевших LRO каждые
	// geoReconcileInterval до отмены ctx (SIGTERM/SIGINT). Останавливается сам по
	// ctx.Done() — не требует отдельного drain'а.
	go lroReconciler.Run(ctx)

	// Ошибка internal-листенера захватывается (buffered chan) и учитывается в
	// exit-коде наравне с public: при фатальном крахе internal-листенера он зовёт
	// cancel(), shutdown-горутина делает grpcSrv.GracefulStop(), после чего
	// grpcSrv.Serve() по контракту grpc-go возвращает nil — если бы exit считался
	// только по public-ошибке, крах :9091 дал бы exit 0 (оркестратор не рестартит,
	// admin-плоскость тихо недоступна). serveResult ниже сводит обе ошибки.
	internalErrCh := make(chan error, 1)
	go func() {
		internalErrCh <- runInternalListener(internalSrv, internalListener, cancel, logger)
	}()

	serveErr := grpcSrv.Serve(listener)
	cancel()
	<-shutdownDone
	// internal-горутина разблокируется, когда её Serve вернётся (GracefulStop в
	// shutdown-горутине выше отработал до close(shutdownDone)).
	return serveResult(serveErr, <-internalErrCh)
}

// serveResult сводит exit-ошибку процесса из ошибок обоих листенеров: ошибка
// public-листенера приоритетна (первичный сигнал отказа edge-поверхности);
// иначе наверх идёт фатальная ошибка internal-листенера — чтобы её крах тоже дал
// non-zero exit (симметрия: public-путь возвращает свою Serve-ошибку, internal
// не должен глотаться после того как cancel()→GracefulStop обнулил public-ошибку).
func serveResult(publicErr, internalErr error) error {
	if publicErr != nil {
		return publicErr
	}
	return internalErr
}

// gracefulServer — минимальный контракт grpc-сервера, нужный runInternalListener
// (только Serve). Позволяет тестировать teardown-семантику без реального listen'а.
type gracefulServer interface {
	Serve(net.Listener) error
}

// runInternalListener обслуживает internal :9091 gRPC-сервер и зеркалит lifecycle
// public-листенера: фатальная (любая, кроме graceful grpc.ErrServerStopped) ошибка
// Serve сносит ВЕСЬ процесс через cancel() root-ctx (симметрично public-пути
// serve→cancel) И ВОЗВРАЩАЕТСЯ вызывающему. Возврат обязателен: после cancel()
// shutdown-горутина делает grpcSrv.GracefulStop() → public grpcSrv.Serve() отдаёт
// nil, поэтому без проброса этой ошибки в exit-код (serveResult) крах :9091 дал бы
// exit 0 — оркестратор не рестартит, admin-плоскость (InternalRegion/ZoneService,
// весь admin-CRUD) тихо недоступна. graceful-stop (ErrServerStopped, штатный
// GracefulStop) фатальным НЕ считается → возврат nil.
func runInternalListener(srv gracefulServer, lis net.Listener, cancel context.CancelFunc, logger *slog.Logger) error {
	if serr := srv.Serve(lis); serr != nil && !errors.Is(serr, grpc.ErrServerStopped) {
		logger.Error("internal grpc server stopped; tearing down process", "err", serr)
		cancel()
		return serr
	}
	return nil
}

// validateAuthMode разбирает KACHO_GEO_AUTH_MODE (whitelist) и строгость DB-SSL.
// Режим больше НЕ управляет authz/mTLS — ими управляет breakglass (см.
// validateSecurityConfig). `production-strict` дополнительно требует SSL до БД.
func validateAuthMode(cfg config.Config, logger *slog.Logger) error {
	switch cfg.AuthMode {
	case "dev":
		if cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable" {
			logger.Warn("KACHO_GEO_DB_SSLMODE=disable — DB plaintext (dev only)")
		}
		return nil
	case "production":
		// В production plaintext-соединение до БД запрещено: sslmode=disable (и
		// пустой → libpq-дефолт disable) отвергаем. Конкретный TLS-режим
		// (require|verify-ca|verify-full) — на усмотрение оператора; строгую
		// проверку сертификата требует production-strict ниже.
		if cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable" {
			return fmt.Errorf("production mode: KACHO_GEO_DB_SSLMODE must not be disable (got %q); use require|verify-ca|verify-full", cfg.DBSSLMode)
		}
		return nil
	case "production-strict":
		switch cfg.DBSSLMode {
		case "require", "verify-ca", "verify-full":
		default:
			return fmt.Errorf("production-strict mode: KACHO_GEO_DB_SSLMODE must be one of require|verify-ca|verify-full (got %q)", cfg.DBSSLMode)
		}
		logger.Warn("AuthMode=production-strict: DB SSL strictly validated")
		return nil
	default:
		return fmt.Errorf("unknown KACHO_GEO_AUTH_MODE=%q (allowed: dev, production, production-strict)", cfg.AuthMode)
	}
}

// validateSecurityConfig — secure-by-default: операции без авторизации и mTLS
// запрещены. Per-RPC authz Check (адрес kacho-iam) и mTLS на ОБОИХ листенерах
// обязательны; единственный способ запустить без них — аварийный
// KACHO_GEO_AUTHZ_BREAKGLASS=true. Без breakglass недостающий authz/mTLS — отказ старта.
//
// ⚠ ВНИМАНИЕ: breakglass=true — ПОЛНЫЙ обход authz+mTLS (emergency-only). На
// plaintext-листенере forged principal-header дает admin-доступ (mTLS не проверяет
// клиента, authz Check пропускается). Включать ТОЛЬКО при инциденте.
func validateSecurityConfig(cfg config.Config) error {
	if cfg.AuthZBreakglass {
		// Breakglass — аварийный ПОЛНЫЙ обход per-RPC authz Check + mTLS. В
		// production posture (production / production-strict) он НЕ honored: иначе
		// один env-флаг молча снял бы всю аутентификацию/авторизацию на
		// развёрнутом стенде, а forged x-kacho-principal-header на
		// plaintext-листенере дал бы admin Region/Zone CRUD (CWE-489
		// active-debug/emergency mode). Emergency-bypass допустим ТОЛЬКО вне
		// production (dev / локальный инцидент) — как и dev-anonymous, он под
		// запретом на любом production-деплое (security.md).
		switch cfg.AuthMode {
		case "production", "production-strict":
			return errors.New("production mode: KACHO_GEO_AUTHZ_BREAKGLASS must not be enabled — it bypasses per-RPC authz Check and mTLS entirely (forged x-kacho-principal headers reach admin Region/Zone CRUD); breakglass is a non-production emergency escape only")
		}
		return nil
	}
	if cfg.AuthZIAMGRPCAddr == "" {
		return errors.New("authz Check required on both listeners: set KACHO_GEO_AUTHZ_IAM_GRPC_ADDR (or KACHO_GEO_AUTHZ_BREAKGLASS=true to bypass)")
	}
	if !cfg.PublicServerMTLS.Enable || !cfg.InternalServerMTLS.Enable {
		return errors.New("mTLS required on both listeners: set KACHO_GEO_PUBLIC_SERVER_MTLS_ENABLE and KACHO_GEO_INTERNAL_SERVER_MTLS_ENABLE=true (or KACHO_GEO_AUTHZ_BREAKGLASS=true to bypass)")
	}
	// Secure-by-default: непустой allow-list доверенных форвардеров ОБЯЗАТЕЛЕН на
	// ЛЮБОМ non-breakglass старте (не только production). Пустой список означает
	// «доверять ЛЮБОМУ mTLS-verified peer'у как форвардеру principal'а» (см.
	// corelib grpcsrv.WithTrustedForwarders): любой внутренний под с валидным
	// client-cert'ом мог бы выставить произвольный x-kacho-principal-* header и
	// авторизоваться как чужой subject — principal-spoofing / confused-deputy до
	// admin-CRUD Region/Zone (:9091) или viewer-spoof на :9090. Раньше пустой
	// список молча разрешался в dev (config-default) — insecure-by-default gap:
	// оператор, забывший выставить production, отгружал spoofing-путь.
	//
	// Теперь trust-any — ЯВНЫЙ opt-in, не дефолт. Пустая строка в списке — НЕ
	// форвардер (WithTrustedForwarders отбрасывает "" → trust-any), поэтому
	// считаем только непустые.
	if countNonEmpty(cfg.AuthZTrustedForwarderSANs) == 0 {
		switch cfg.AuthMode {
		case "production", "production-strict":
			// В production trust-any недопустим ни при каких условиях — opt-in
			// НЕ honored, обязателен реальный SAN api-gateway.
			return errors.New("production mode: KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS must pin at least one trusted-forwarder SAN (api-gateway SA); an empty allow-list trusts ANY mTLS-verified peer to forward end-user principals (principal-spoofing / confused-deputy to admin Region/Zone CRUD). Set the api-gateway SAN")
		default:
			// dev: пустой allow-list разрешён ТОЛЬКО с явным dev-опт-ином. Без него —
			// fail-closed отказ старта (secure-by-default).
			if !cfg.AuthZTrustAnyForwarder {
				return errors.New("secure-by-default: empty KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS trusts ANY mTLS-verified peer to forward end-user principals (principal-spoofing / confused-deputy to admin Region/Zone CRUD). Pin the api-gateway SAN, or set KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER=true to explicitly opt into trust-any for local dev (or KACHO_GEO_AUTHZ_BREAKGLASS=true for emergency full bypass)")
			}
		}
	}
	return nil
}

// countNonEmpty возвращает число непустых строк в срезе. Пустые SAN'ы игнорируются
// corelib WithTrustedForwarders (отбрасывает "" → пустой allow-list → trust-any),
// поэтому production-гейт учитывает только непустые записи.
func countNonEmpty(ss []string) int {
	n := 0
	for _, s := range ss {
		if s != "" {
			n++
		}
	}
	return n
}

// authzIAMConn нормализует *grpc.ClientConn в grpc.ClientConnInterface без
// typed-nil-ловушки: при nil-conn возвращает ИСТИННЫЙ nil интерфейса (а не
// interface, обёртывающий (*grpc.ClientConn)(nil), который сравнением == nil даёт
// false). Благодаря этому guard `if opts.IAMConn == nil` в check.NewInterceptor
// корректно отдаёт ErrIAMConnNotConfigured, а не строит IAM-клиент поверх
// nil-conn (CWE-476: паника на первом Check при ослаблении upstream-guard'а).
func authzIAMConn(conn *grpc.ClientConn) grpc.ClientConnInterface {
	if conn == nil {
		return nil
	}
	return conn
}

// newPrincipalInterceptors собирает trust-aware principal-цепочку
// (cert-identity → trusted-principal с allow-list форвардеров) для ОДНОГО
// листенера. Единый source-of-truth wiring'а: оба листенера (public :9090 и
// internal :9091) строятся из него, поэтому anti-spoof-гарантия у них
// идентична by construction. forwarders (allow-list SAN'ов доверенных
// форвардеров) пробрасывается в WithTrustedForwarders — verified-но-не-форвардер
// peer не может форвардить произвольного principal'а.
func newPrincipalInterceptors(forwarders []string) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	unary := []grpc.UnaryServerInterceptor{
		grpcsrv.UnaryCertIdentityExtract(),
		grpcsrv.UnaryTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
	stream := []grpc.StreamServerInterceptor{
		grpcsrv.StreamCertIdentityExtract(),
		grpcsrv.StreamTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
	return unary, stream
}

// assembleUnaryChain строит финальную упорядоченную unary-цепочку ОДНОГО листенера
// и фиксирует load-bearing инвариант порядка в одном месте (под guard-тестом
// serve_interceptor_order_test.go): recovery ПЕРВЫМ (index 0, outermost — оборачивает
// все нижележащие интерсепторы и сам handler; иначе паника в интерсепторе/handler'е
// уронила бы процесс — DoS), затем principal-extract (cert-identity → trusted-
// principal), затем — если задан — authz Check ПОСЛЕ извлечения principal'а (Check
// обязан видеть уже извлечённого субъекта). authz==nil → слот отсутствует.
func assembleUnaryChain(recovery grpc.UnaryServerInterceptor, principal []grpc.UnaryServerInterceptor, authz grpc.UnaryServerInterceptor) []grpc.UnaryServerInterceptor {
	chain := make([]grpc.UnaryServerInterceptor, 0, len(principal)+2)
	chain = append(chain, recovery)
	chain = append(chain, principal...)
	if authz != nil {
		chain = append(chain, authz)
	}
	return chain
}

// assembleStreamChain — stream-аналог assembleUnaryChain (тот же инвариант порядка:
// recovery outermost → principal → authz).
func assembleStreamChain(recovery grpc.StreamServerInterceptor, principal []grpc.StreamServerInterceptor, authz grpc.StreamServerInterceptor) []grpc.StreamServerInterceptor {
	chain := make([]grpc.StreamServerInterceptor, 0, len(principal)+2)
	chain = append(chain, recovery)
	chain = append(chain, principal...)
	if authz != nil {
		chain = append(chain, authz)
	}
	return chain
}

// registerServices раскладывает сервисы по листенерам: public read-only
// (RegionService/ZoneService) — на publicSrv; admin-CRUD Internal*
// (InternalRegionService/InternalZoneService) — ТОЛЬКО на internalSrv (запрет #6:
// Internal.* не публикуется на внешнем endpoint); OperationService (LRO poll) — на
// обоих (клиент поллит результат admin-мутации через тот же mux, read-poll допустим
// и на public). Выделено, чтобы разделение public↔internal проверялось тестом через
// grpc.Server.GetServiceInfo (см. serve_registration_test.go) — регрессия «Internal*
// уехал на public» ловится, а не only-by-source-review.
func registerServices(
	publicSrv, internalSrv grpc.ServiceRegistrar,
	regionUC *region.UseCase,
	zoneUC *zone.UseCase,
	opHandler operationpb.OperationServiceServer,
) {
	// Публичные read-only сервисы на :9090.
	geov1.RegisterRegionServiceServer(publicSrv, handler.NewRegionHandler(regionUC))
	geov1.RegisterZoneServiceServer(publicSrv, handler.NewZoneHandler(zoneUC))
	// Admin CRUD сервисы ТОЛЬКО на cluster-internal :9091 (не на внешнем endpoint).
	geov1.RegisterInternalRegionServiceServer(internalSrv, handler.NewInternalRegionHandler(regionUC))
	geov1.RegisterInternalZoneServiceServer(internalSrv, handler.NewInternalZoneHandler(zoneUC))
	// OperationService (LRO poll) на ОБОИХ листенерах.
	operationpb.RegisterOperationServiceServer(publicSrv, opHandler)
	operationpb.RegisterOperationServiceServer(internalSrv, opHandler)
}
