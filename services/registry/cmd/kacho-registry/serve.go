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
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/check"
	geoclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
	zotclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/zot"
	"github.com/PRO-Robotech/kacho/services/registry/internal/dataplane"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	"github.com/PRO-Robotech/kacho/services/registry/internal/handler"
	"github.com/PRO-Robotech/kacho/services/registry/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/registry/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
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
	// обязательны. Единственный способ запустить без авторизации и mTLS —
	// аварийный KACHO_REGISTRY_AUTHZ_BREAKGLASS=true.
	if err := validateSecurityConfig(cfg); err != nil {
		return err
	}
	// Хранилище слоёв аутентифицирует всех: без учётных данных сервис не смог бы в
	// него ходить — а значит хранилище открыто любому в сети подов.
	if err := requireZotCredentials(cfg.AuthMode, cfg.ZotAddr, cfg.ZotUsername, cfg.ZotPassword); err != nil {
		return err
	}
	// Самоотчёт о security-posture: ПОСЛЕ boot-guard'ов (validateAuthMode +
	// validateSecurityConfig — конфиг уже ПРИНЯТ процессом) и ДО подъёма
	// листенеров. Production-posture гейт обязан утверждать на этом наблюдаемом
	// факте, а не на хранимом конфиге (см. observability.BootPosture).
	observability.LogBootPosture(logger, bootPosture(cfg))

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// ── наблюдаемость: приватный prometheus-реестр + diagnostic-листенер. У
	// сервиса не было ни одной метрики, при том что именно его очередь
	// регистраций — та, на которой класс «очередь не доставила ни одной строки за
	// всю жизнь, и узнать это было неоткуда» наблюдался вживую. Ошибка привязки
	// порта фатальна для старта: молча поднятый сервис без наблюдаемости — ровно
	// та форма без содержания, которую мы ловим в коде.
	svcMetrics := metrics.New()
	diagTask, diagShutdown, derr := startDiagnosticListener(cfg.MetricsAddr, svcMetrics, logger)
	if derr != nil {
		return fmt.Errorf("start diagnostic listener: %w", derr)
	}
	if diagTask != nil {
		go func() {
			if serr := diagTask(); serr != nil {
				logger.Error("diagnostic listener stopped", "err", serr)
			}
		}()
	}

	// ── LRO-стек: общая operations-таблица (corelib) каталога kacho_registry.
	opsRepo := operations.NewRepo(pool, "kacho_registry")

	// ── ребро registry→iam INTERNAL (:9091, mTLS): per-RPC authz Check +
	// fga-proxy RegisterResource/UnregisterResource (Internal-only). При breakglass
	// conn может быть nil (интерсептор пропускает всё; клиенты отвечают Unavailable).
	var authzConn *grpc.ClientConn
	if cfg.AuthZIAMGRPCAddr != "" {
		authzCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
		if cerr != nil {
			return fmt.Errorf("registry→iam authz mTLS creds: %w", cerr)
		}
		authzConn, err = grpc.NewClient(cfg.AuthZIAMGRPCAddr,
			grpc.WithTransportCredentials(authzCreds),
			grpcclient.KeepaliveDialOption(true))
		if err != nil {
			return fmt.Errorf("dial kacho-iam internal: %w", err)
		}
		defer func() { _ = authzConn.Close() }()
	}

	// ── ребро registry→iam PUBLIC (:9090, mTLS): ProjectService.Get (existence-
	// валидация project на Create). ОТДЕЛЬНЫЙ conn — ProjectService зарегистрирован
	// только на public :9090; вызов на :9091 (authzConn) вернул бы Unimplemented →
	// фикс. INTERNAL на Create. ServerName public dial-host'а (kacho-iam.*) ≠ internal,
	// поэтому раздельные mTLS-creds (IAMProjectMTLS vs IAMAuthzMTLS) обязательны.
	var projectConn *grpc.ClientConn
	if cfg.IAMProjectGRPCAddr != "" {
		projectCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.IAMProjectMTLS)
		if cerr != nil {
			return fmt.Errorf("registry→iam project mTLS creds: %w", cerr)
		}
		projectConn, err = grpc.NewClient(cfg.IAMProjectGRPCAddr,
			grpc.WithTransportCredentials(projectCreds),
			grpcclient.KeepaliveDialOption(true))
		if err != nil {
			return fmt.Errorf("dial kacho-iam project: %w", err)
		}
		defer func() { _ = projectConn.Close() }()
	}
	// geoConn — PUBLIC :9090 kacho-geo (RegionService.Get — новое ребро registry→geo,
	// REG-1 F4). Отдельный conn/mTLS (ServerName kacho-geo dial-host'а). Пусто → nil
	// conn → RegionExists fail-closed Unavailable (Create не создаст реестр).
	var geoConn *grpc.ClientConn
	if cfg.GeoGRPCAddr != "" {
		geoCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.GeoMTLS)
		if cerr != nil {
			return fmt.Errorf("registry→geo mTLS creds: %w", cerr)
		}
		geoConn, err = grpc.NewClient(cfg.GeoGRPCAddr,
			grpc.WithTransportCredentials(geoCreds),
			grpcclient.KeepaliveDialOption(true))
		if err != nil {
			return fmt.Errorf("dial kacho-geo region: %w", err)
		}
		defer func() { _ = geoConn.Close() }()
	}
	logger.Info("registry→iam edges wired",
		"authz_addr", cfg.AuthZIAMGRPCAddr, "authz_mtls", cfg.IAMAuthzMTLS.Enable,
		"project_addr", cfg.IAMProjectGRPCAddr, "project_mtls", cfg.IAMProjectMTLS.Enable,
		"geo_addr", cfg.GeoGRPCAddr, "geo_mtls", cfg.GeoMTLS.Enable)

	// ── adapters (порты use-case): pgx-repo, zot data/registry-API, iam-клиент ──
	// iamConn — internal :9091 (Check-интерсептор + fga-proxy register-drainer).
	// projectIAMConn — public :9090 (ProjectService.Get). Их conn'ы РАЗДЕЛЬНЫ.
	var iamConn grpc.ClientConnInterface
	if authzConn != nil {
		iamConn = authzConn
	}
	var projectIAMConn grpc.ClientConnInterface
	if projectConn != nil {
		projectIAMConn = projectConn
	}
	registryRepo := pg.NewRegistryRepo(pool)
	// pendingBlobRepo — durable per-repo учёт загруженных блобов (registry_pending_blob,
	// REG-33 Defect A): blob PUT-finalize пишет строку, push-time blob HEAD/GET раскрывает
	// только-что-загруженный слой ДО появления манифеста (REG-37 сохранён).
	pendingBlobRepo := pg.NewPendingBlobRepo(pool, cfg.PendingBlobTTL)
	// pushGrantRepo — durable per-subject push-ownership кеш (registry_push_grant, REG-33
	// immediate-pull): успешный manifest-PUT пишет строку, pull-path раскрывает repo
	// толкавшему, пока async register-on-first-push не материализовал per-repo v_get в FGA
	// (иначе собственный немедленный pull толкавшего упрётся в v_get-deny → 404). Ключ по
	// subject → раскрывается только собственный только-что-запушенный repo (REG-37 сохранён).
	pushGrantRepo := pg.NewPushGrantRepo(pool, cfg.PushGrantTTL)
	zotAdapter := zotclient.New(cfg.ZotAddr, zotclient.WithBasicAuth(cfg.ZotUsername, cfg.ZotPassword))
	iamAdapter := iamclient.New(projectIAMConn)
	var geoIAMConn grpc.ClientConnInterface
	if geoConn != nil {
		geoIAMConn = geoConn
	}
	geoAdapter := geoclient.New(geoIAMConn)
	// repoConfigRepo — config-overlay Repository (repository_configs, RG-1): durable
	// overlay-строки (survives-empty) + ACTIVE-guard + transactional-outbox owner/public-grant.
	repoConfigRepo := pg.NewRepositoryConfigRepo(pool)

	// ── use-case (CQRS repo + config-overlay + zot + iam + geo + repo-registrar + LRO) ──
	registryUC := registry.New(registryRepo, registryRepo, repoConfigRepo, zotAdapter, iamAdapter, geoAdapter, registryRepo, opsRepo, cfg.EndpointBase)

	// ── sync-registrar: применяет register-type owner/parent/public-grant tuple СРАЗУ
	// после durable-commit (immediate materialization; repo/registry GET не 404-ит в окне,
	// пока async register-drainer не догнал под burst create). Тот же iamConn (:9091, mTLS)
	// + порт, что drainer; register-drainer остаётся at-least-once backstop'ом. nil iamConn
	// (breakglass/dev-insecure) → sync-путь пропускается (syncReg остаётся nil).
	if iamConn != nil {
		registryUC.WithSyncRegistrar(iamclient.NewSyncRegistrar(iamclient.NewRegisterResourceClient(iamConn)))
	}

	// ── разрешитель осиротевших операций (durable LRO recovery) ───────────────
	// Дренаж на SIGTERM ниже закрывает только штатное завершение. Всё остальное —
	// SIGKILL, OOM, вытеснение, исчерпание бюджета терминальной записи, переполнение
	// очереди исполнителя — оставляло строку операции «в процессе» навсегда, и клиент
	// не узнавал исхода ни разу. См. recovery.go.
	lroReconciler := startLRORecovery(ctx, pool, operationresolver.Readers{
		Registry:   registryRepo,
		RepoConfig: repoConfigRepo,
		Proto:      registryUC.ProtoRegistry,
	}, logger)
	go lroReconciler.Run(ctx)

	// ── register-drainer: owner-tuple register/unregister intent из registry_outbox
	// применяется через kacho-iam fga-proxy (:9091, mTLS, идемпотентно, at-least-once,
	// exactly-once claim FOR UPDATE SKIP LOCKED между репликами). iam недоступен →
	// intent durable + retry (owner-tuple не теряется). Без него созданные реестры не
	// получат owner/project-tuple → невидимы в authz-filtered List.
	regDrainer, derr := drainer.New[domain.RegisterIntent](
		pool,
		drainer.Config{
			Table:   registerOutboxTable,
			Channel: registerOutboxChannel,
			// Order-preserving drain, per resource. registry_outbox — это
			// register-outbox (несмотря на имя): он несёт И fga.register, И
			// fga.unregister ОДНОГО объекта (Registry Create/Update/Delete;
			// Repository register-on-first-push / unregister-on-last-tag;
			// adopt-owner/public-grant overlay). Материализация в iam версионирована
			// лишь ЧАСТИЧНО: source_version-LWW (resource_mirror UPSERT под
			// `source_version < EXCLUDED.source_version`) гейтит ТОЛЬКО ветку
			// ON CONFLICT DO UPDATE, а unregister делает ЖЁСТКИЙ DELETE без
			// tombstone. Переставленный STALE register сравнивать не с чем → ветка
			// INSERT → ВОСКРЕШЕНИЕ mirror-строки УДАЛЁННОГО реестра/репозитория,
			// которую level-triggered реконсайлер iam вечно ре-материализует (для
			// репозитория это переживший удаление последнего тега pull-grant).
			//
			// Порядок ломается и БЕЗ конкурентности: claim сортирует
			// `ORDER BY (attempt_count, id)` → transiently-bumped register
			// (attempt>=1) уступает свежему unregister (attempt=0) даже при
			// ApplyConcurrency=1 (дефолт registry). Per-repo
			// pg_advisory_xact_lock в emitRepoIntent НЕ помогает — он сериализует
			// WRITE-сторону (монотонный source_version), а не порядок claim'а.
			//
			// Ключ — resource_id: emitFGAIntent штампует его из
			// RegisterIntent.ResourceID, а tuple'ы одной строки всегда целятся в
			// РОВНО ОДИН FGA-объект с этим id — "<regId>" для registry_registry и
			// "<regId>/<repo>" для registry_repository (оба глобально уникальны by
			// construction, core rule #15) → «одна партиция» == «один объект
			// iam-mirror», реестр не делит партицию со своими репозиториями.
			// Требует partial-index миграции 0008 `(resource_id, id) WHERE sent_at IS
			// NULL` под claim'овый NOT EXISTS. Поведение зафиксировано corelib-тестом
			// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister.
			PartitionColumn: "resource_id",
		},
		iamclient.DecodeRegisterIntent,
		iamclient.NewRegisterApplier(iamclient.NewRegisterResourceClient(iamConn)),
		logger,
		// Отравление — СОБЫТИЕ, и после ужесточения классификации отказа в правах
		// до терминального оно реально достижимо: строка, которую владелец прав
		// отверг, больше не ретраится вечно, а выбывает. Считаем её монотонным
		// счётчиком, иначе единственный след — WARN в логе.
		drainer.WithPoisonObserver[domain.RegisterIntent](func() {
			svcMetrics.IncPoisoned(registerOutboxTable)
		}),
	)
	if derr != nil {
		return fmt.Errorf("build register-drainer: %w", derr)
	}
	go func() {
		if rerr := regDrainer.Run(ctx); rerr != nil && !errors.Is(rerr, context.Canceled) {
			logger.Error("register-drainer stopped", "err", rerr)
		}
	}()
	// Отравление обязано быть паузой, а не потерей: без периодического redrive
	// недоставленная регистрация оставляет объект без mirror-строки в kacho-iam, а
	// значит без owner-tuple и без материализованных глаголов — невидимым в
	// authz-фильтрованном List до ручной правки БД. См. redrive_backstop.go.
	if rerr := startRedriveBackstop(ctx, pool, logger); rerr != nil {
		return fmt.Errorf("start redrive backstop: %w", rerr)
	}
	// Скан СОСТОЯНИЯ той же очереди. Дренаж выше сообщает о событиях и делает это
	// в лог; «в очереди лежит N строк, старейшей M секунд» не производил никто, и
	// застрявшая очередь молчала ровно так же, как пустая (см. diagnostics.go).
	go runRegisterOutboxMetrics(ctx, pool, svcMetrics, logger)
	// Сверка «у объекта прав есть живой ресурс». Держит само свойство, а не
	// отсутствие одной его причины, и разбирает уже накопленное — снимать которое
	// больше некому: оно не привязано ни к какому будущему удалению
	// (см. orphan_sweep_backstop.go).
	startOrphanSweepBackstop(ctx, registryRepo, logger)

	// ── authz: per-RPC OpenFGA Check на ОБОИХ листенерах (AuthN+AuthZ везде —
	// internal :9091 НЕ освобождён, security.md). Check обязателен —
	// validateSecurityConfig уже гарантировал наличие адреса kacho-iam без breakglass.
	authzIntr, aerr := check.NewInterceptor(check.Options{
		ServiceName: "kacho-registry",
		IAMConn:     authzConn,
		Breakglass:  cfg.AuthZBreakglass,
		CacheTTL:    cfg.AuthZCacheTTL,
		Logger:      logger,
	})

	// ── цепочки интерсепторов ──
	// ОБА листенера: cert-identity → trusted-principal (anti-spoof) → authz Check.
	// Public (:9090) не освобождён от доверенной пары так же, как internal (:9091)
	// не освобождён от per-RPC authz: публичный листенер — обычный Service внутри
	// пространства имён, и дозвониться до него может любой под (см. identityUnary).
	publicUnary := identityUnary(cfg)
	publicStream := identityStream(cfg)
	internalUnary := identityUnary(cfg)
	internalStream := identityStream(cfg)

	switch {
	case aerr == nil && authzIntr != nil:
		publicUnary = append(publicUnary, authzIntr.Unary())
		publicStream = append(publicStream, authzIntr.Stream())
		internalUnary = append(internalUnary, authzIntr.Unary())
		internalStream = append(internalStream, authzIntr.Stream())
		if cfg.AuthZBreakglass {
			logger.Warn("BREAKGLASS active: per-RPC authz Check bypassed on BOTH listeners (emergency mode)")
		} else {
			logger.Info("authz interceptor enabled",
				"iam_endpoint", cfg.AuthZIAMGRPCAddr,
				"listeners", "public+internal",
				"cache_ttl", cfg.AuthZCacheTTL)
		}
	case errors.Is(aerr, check.ErrIAMConnNotConfigured):
		// Недостижимо при штатной конфигурации: validateSecurityConfig уже отказал
		// бы старту. Defensive fail-closed.
		return errors.New("authz Check required: set KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR (or KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass)")
	case aerr != nil:
		return fmt.Errorf("build authz interceptor: %w", aerr)
	}

	// ── server-creds (mTLS обязателен на обоих листенерах, кроме breakglass) ──
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

	// per-repo authz-Check для ScopeFiltered RPC (ListRepositories/ListTags/DeleteTag):
	// interceptor их пропускает, handler сам Check'ает (call-gate + row-filter +
	// existence-hiding). Тот же conn к iam :9091, что и per-RPC interceptor.
	// authzConn==nil (breakglass) → nil authorizer → handler bypass (как interceptor).
	var listAuthz handler.Authorizer
	if authzConn != nil {
		listAuthz = check.NewIAMCheckClient(authzConn)
	}

	// Публичный control-plane RegistryService на :9090.
	registryv1.RegisterRegistryServiceServer(grpcSrv, handler.NewRegistryHandler(registryUC, listAuthz, cfg.AuthZCacheTTL))
	// Admin InternalRegistryService ТОЛЬКО на cluster-internal :9091.
	registryv1.RegisterInternalRegistryServiceServer(internalSrv, handler.NewInternalRegistryHandler(registryUC))
	// OperationService (LRO poll) на ОБОИХ листенерах: async-мутации идут на public
	// и internal, клиент поллит результат через тот же mux. Read-RPC гейтятся authz.
	opHandler := handler.NewOperationHandler(opsRepo)
	operationpb.RegisterOperationServiceServer(grpcSrv, opHandler)
	operationpb.RegisterOperationServiceServer(internalSrv, opHandler)

	// ── data-plane OCI auth-proxy (registry.kacho.local): отдельный HTTP-листенер,
	// Docker Registry v2 / OCI token-auth flow перед zot. per-request JWKS-verify +
	// InternalIAMService.Check + existence-hiding + stream-proxy. Отдельно от gRPC.
	var dpServer *http.Server
	if cfg.DataplaneAddr != "" {
		// registryRepo подаётся трижды и в трёх разных ролях: RepoRegistrar (эмит
		// интента + durable-признак существования), RepositoryPresence (чтение того же
		// признака ⊔ строки наложения — по нему выбирается глагол записи) и
		// RegistryLookup (owning-project реестра для containment scope интента).
		dpHandler, dperr := buildDataplaneHandler(cfg, authzConn, registryRepo, zotAdapter, registryRepo, registryRepo, pendingBlobRepo, pushGrantRepo, logger)
		if dperr != nil {
			return fmt.Errorf("build data-plane proxy: %w", dperr)
		}
		dpServer = &http.Server{
			Addr:              cfg.DataplaneAddr,
			Handler:           dpHandler,
			ReadHeaderTimeout: 15 * time.Second,
		}
		// TTL-sweeper'ы REG-33: подметают протухшие строки. Реюзают ctx-lifecycle сервиса;
		// интервал = TTL (роста таблицы не более двух окон). TTL≤0 → трекинг выключен.
		//   - pending-blob (> PendingBlobTTL): registry_pending_blob (Defect A).
		//   - push-grant (> PushGrantTTL):     registry_push_grant (immediate-pull).
		if cfg.PendingBlobTTL > 0 {
			go runStaleSweeper(ctx, pendingBlobRepo, cfg.PendingBlobTTL, "pending-blob", logger)
		}
		if cfg.PushGrantTTL > 0 {
			go runStaleSweeper(ctx, pushGrantRepo, cfg.PushGrantTTL, "push-grant", logger)
		}
	}

	listener, err := net.Listen("tcp", ":"+cfg.GrpcPort)
	if err != nil {
		return err
	}
	internalListener, err := net.Listen("tcp", ":"+cfg.InternalGrpcPort)
	if err != nil {
		_ = listener.Close()
		return err
	}
	logger.Info("kacho-registry listening",
		"public_mtls", cfg.PublicServerMTLS.Enable,
		"internal_mtls", cfg.InternalServerMTLS.Enable,
		"public_port", cfg.GrpcPort,
		"internal_port", cfg.InternalGrpcPort,
		"dataplane_addr", cfg.DataplaneAddr,
		"zot_addr", cfg.ZotAddr)

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		internalSrv.GracefulStop()
		grpcSrv.GracefulStop()
		// Graceful drain data-plane HTTP: перестаёт принимать новые, дожидается
		// in-flight docker push/pull в пределах bounded-таймаута.
		if dpServer != nil {
			dpCtx, cancelDP := context.WithTimeout(context.Background(), 15*time.Second)
			if serr := dpServer.Shutdown(dpCtx); serr != nil {
				logger.Warn("data-plane proxy shutdown", "err", serr)
			}
			cancelDP()
		}
		// Diagnostic-листенер закрывается вместе с остальными: скрейп в момент
		// остановки не должен висеть на полузакрытом соединении.
		diagCtx, cancelDiag := context.WithTimeout(context.Background(), 5*time.Second)
		diagShutdown(diagCtx)
		cancelDiag()
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

	go func() {
		if serr := internalSrv.Serve(internalListener); serr != nil && !errors.Is(serr, grpc.ErrServerStopped) {
			logger.Error("internal grpc server stopped", "err", serr)
		}
	}()

	if dpServer != nil {
		go func() {
			if serr := dpServer.ListenAndServe(); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
				logger.Error("data-plane proxy stopped", "err", serr)
			}
		}()
	}

	serveErr := grpcSrv.Serve(listener)
	cancel()
	<-shutdownDone
	return serveErr
}

// buildDataplaneHandler собирает data-plane OCI auth-proxy (fail-closed). Штатно:
// JWKS-verify Hydra-issued identity-JWT (RS256/ES256) + per-request
// InternalIAMService.Check + zot stream-proxy. breakglass → bypass AuthN+AuthZ
// (аварийный режим, как gRPC-листенеры).
func buildDataplaneHandler(cfg config.Config, authzConn *grpc.ClientConn, repoReg dataplane.RepoRegistrar, backend dataplane.Backend, presence dataplane.RepositoryPresence, regLookup dataplane.RegistryLookup, uploads dataplane.UploadRecorder, pushGrants dataplane.PushGrantRecorder, logger *slog.Logger) (http.Handler, error) {
	// Plaintext data-plane обязан стоять за внешней TLS-терминацией (bearer
	// identity-JWT не должны транзитить открытым текстом). В проде — явный ack
	// оператора; проверяется независимо от breakglass (риск открытого сокета
	// ортогонален обходу authz).
	if err := requireDataplaneTLSAck(cfg.AuthMode, cfg.DataplaneTLSTerminatedExternally); err != nil {
		return nil, err
	}

	forwarder, err := dataplane.NewZotForwarder(cfg.ZotAddr, logger,
		dataplane.WithZotBasicAuth(cfg.ZotUsername, cfg.ZotPassword))
	if err != nil {
		return nil, err
	}

	var verifier dataplane.TokenVerifier
	var authorizer dataplane.Authorizer
	if cfg.AuthZBreakglass {
		logger.Warn("BREAKGLASS active: data-plane AuthN+AuthZ bypassed (emergency mode)")
	} else {
		if cfg.IAMJWKSURL == "" {
			return nil, errors.New("data-plane requires KACHO_REGISTRY_IAM_JWKS_URL (or KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass)")
		}
		if err := requireSecureJWKSURL(cfg.AuthMode, cfg.IAMJWKSURL); err != nil {
			return nil, err
		}
		if err := requireIssuerPinned(cfg.AuthMode, cfg.HydraIssuer); err != nil {
			return nil, err
		}
		if authzConn == nil {
			return nil, errors.New("data-plane requires authz IAM conn (KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR)")
		}
		verifier = jwks.New(cfg.IAMJWKSURL, cfg.ServiceAud, cfg.HydraIssuer)
		authorizer = check.NewIAMCheckClient(authzConn)
	}

	return dataplane.New(verifier, authorizer, backend, presence, forwarder, repoReg, regLookup, uploads, pushGrants,
		cfg.TokenRealm, cfg.ServiceAud, logger).
		// Anonymous public pull (RG-1 D-7): resolve the iam-issued anon principal id to
		// the FGA wildcard user:*. Empty → anon disabled (secure-by-default).
		WithAnonymousSubject(cfg.AnonymousSubjectID), nil
}

// staleSweeper — узкий порт TTL-подметания durable-таблиц REG-33 (registry_pending_blob,
// registry_push_grant). Реализуется pg.PendingBlobRepo / pg.PushGrantRepo.
type staleSweeper interface {
	SweepStale(ctx context.Context) (int64, error)
}

// runStaleSweeper периодически (interval) подметает протухшие строки до отмены ctx
// (SIGTERM). Ошибка sweep'а логируется под именем name и не роняет сервис (гигиена, не
// критичный путь). Первый тик — через interval (свежий старт таблицы пуст).
func runStaleSweeper(ctx context.Context, sweeper staleSweeper, interval time.Duration, name string, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			n, err := sweeper.SweepStale(sweepCtx)
			cancel()
			if err != nil {
				logger.Warn(name+" sweep failed", "err", err)
				continue
			}
			if n > 0 {
				logger.Info(name+" sweep", "deleted", n)
			}
		}
	}
}

// requireSecureJWKSURL — в production/production-strict JWKS-endpoint (единственный
// trust-anchor верификации identity-JWT data-plane: jwks.Verifier тянет из него
// публичные ключи) обязан быть https://. Plaintext-HTTP допускает MITM-подмену
// JWKS-документа на пути к iam-JWKS-эндпоинту → forge-токен под любой subject →
// полный обход data-plane AuthN. В dev (и breakglass — там verifier не поднимается)
// http:// допустим, симметрично DB sslmode=disable.
func requireSecureJWKSURL(authMode, jwksURL string) error {
	switch authMode {
	case "production", "production-strict":
		u, err := url.Parse(jwksURL)
		if err != nil {
			return fmt.Errorf("invalid KACHO_REGISTRY_IAM_JWKS_URL %q: %w", jwksURL, err)
		}
		if !strings.EqualFold(u.Scheme, "https") {
			return fmt.Errorf("AuthMode=%s requires https:// KACHO_REGISTRY_IAM_JWKS_URL "+
				"(JWKS trust anchor must not be fetched over plaintext; got scheme %q)", authMode, u.Scheme)
		}
	}
	return nil
}

// requireDataplaneTLSAck — data-plane OCI-листенер (DataplaneAddr) обслуживает открытый
// HTTP; штатно TLS терминируется внешним ingress/mesh перед подом. По этому сокету
// транзитят bearer identity-JWT (Hydra-issued, реплеябельные в пределах TTL). В
// production/production-strict молчаливый plaintext-старт запрещён: если ingress
// ошибочно настроен на plaintext-passthrough, docker-login токены утекают в открытом
// виде (harvest+replay, CWE-319). Оператор обязан ЯВНО подтвердить внешнюю TLS-
// терминацию (KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY=true) — параллель
// requireSecureJWKSURL/requireIssuerPinned. В dev — no-op (как http:// JWKS и DB
// sslmode=disable). Вызывается только когда data-plane поднимается (DataplaneAddr!="").
func requireDataplaneTLSAck(authMode string, tlsTerminatedExternally bool) error {
	switch authMode {
	case "production", "production-strict":
		if !tlsTerminatedExternally {
			return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY=true "+
				"(the data-plane serves plaintext HTTP and must sit behind external TLS termination; "+
				"bearer identity-JWTs would otherwise transit cleartext)", authMode)
		}
	}
	return nil
}

// requireZotCredentials — в production/production-strict сервис обязан
// предъявляться своему хранилищу слоёв (zot) HTTP-Basic'ом.
//
// Смысл гейта не в «шифровании пароля», а в том, что учётные данные —
// НАБЛЮДАЕМЫЙ признак включённой аутентификации хранилища. zot тенантов не
// различает: если реестр ходит в него анонимно, значит хранилище обслуживает
// анонимных, и тогда любой процесс, дозвонившийся до его порта, перечисляет
// репозитории всех тенантов, выгружает чужие слои, подменяет образ под чужим
// тегом и удаляет содержимое — минуя проверку подписи docker-Bearer'а, per-request
// Check, сокрытие существования и запрет разрушительного DELETE. Один хоп в объезд
// всей плоскости данных, поэтому молчаливый старт в такой посадке запрещён
// (параллель requireDataplaneTLSAck / requireSecureJWKSURL / requireIssuerPinned).
//
// zotAddr пуст ⇒ хранилище не сконфигурировано, ходить некуда — гейт молчит.
// В dev — no-op (in-process фикстуры поднимают zot без аутентификации).
func requireZotCredentials(authMode, zotAddr, username, password string) error {
	switch authMode {
	case "production", "production-strict":
		if strings.TrimSpace(zotAddr) == "" {
			return nil
		}
		if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
			return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_ZOT_USERNAME and "+
				"KACHO_REGISTRY_ZOT_PASSWORD (the layer store at %q must authenticate its callers; "+
				"without credentials it serves anyone that reaches its port, and the whole data-plane "+
				"authorization is one hop away)", authMode, zotAddr)
		}
	}
	return nil
}

// requireIssuerPinned — в production/production-strict issuer (iss) identity-JWT
// обязан быть закреплён (KACHO_REGISTRY_HYDRA_ISSUER непустой). jwks.Verifier пропускает
// iss-проверку при пустом issuer (issuer-pinning опционален) — тогда data-plane принял бы
// любой токен, подписанный ключом из того же JWKS и несущий aud ⊇ ServiceAud, независимо
// от того, КТО его выпустил (federation-out на другой RP, разделяющий Hydra/JWKS, дал бы
// доступ к OCI data-plane). В проде issuer-pinning не должен молча отсутствовать — параллель
// requireSecureJWKSURL. В dev (и breakglass — verifier не поднимается) пустой iss допустим.
func requireIssuerPinned(authMode, issuer string) error {
	switch authMode {
	case "production", "production-strict":
		if issuer == "" {
			return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_HYDRA_ISSUER "+
				"(issuer pinning must not be silently omitted; a token from any relying "+
				"party sharing the JWKS+aud would otherwise authenticate)", authMode)
		}
	}
	return nil
}

// validateAuthMode разбирает KACHO_REGISTRY_AUTH_MODE (whitelist) и строгость
// DB-SSL. Режим не управляет authz/mTLS — ими управляет breakglass (см.
// validateSecurityConfig). `production-strict` дополнительно требует SSL до БД.
func validateAuthMode(cfg config.Config, logger *slog.Logger) error {
	switch cfg.AuthMode {
	case "dev":
		if cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable" {
			logger.Warn("KACHO_REGISTRY_DB_SSLMODE=disable — DB plaintext (dev only)")
		}
		return nil
	case "production":
		return nil
	case "production-strict":
		switch cfg.DBSSLMode {
		case "require", "verify-ca", "verify-full":
		default:
			return fmt.Errorf("production-strict mode: KACHO_REGISTRY_DB_SSLMODE must be one of require|verify-ca|verify-full (got %q)", cfg.DBSSLMode)
		}
		logger.Warn("AuthMode=production-strict: DB SSL strictly validated")
		return nil
	default:
		return fmt.Errorf("unknown KACHO_REGISTRY_AUTH_MODE=%q (allowed: dev, production, production-strict)", cfg.AuthMode)
	}
}

// validateSecurityConfig — secure-by-default: операции без авторизации и mTLS
// запрещены. Per-RPC authz Check (адрес kacho-iam) и mTLS на ОБОИХ листенерах
// обязательны; обойти их можно ТОЛЬКО в dev через
// KACHO_REGISTRY_AUTHZ_BREAKGLASS=true.
//
// ⚠ ВНИМАНИЕ: breakglass=true — ПОЛНЫЙ обход authz+mTLS (emergency-only, dev-only).
// В любом production-режиме он ОТКАЗЫВАЕТ старту (см. ниже).
func validateSecurityConfig(cfg config.Config) error {
	if cfg.AuthZBreakglass {
		// Registry был единственным сервисом, где breakglass не гейтился вообще:
		// ни fatal (как geo/nlb), ни даже WARN (как compute/vpc) — `return nil`
		// первым стейтментом снимал и authz-Check, и mTLS на ОБОИХ листенерах в
		// ЛЮБОМ режиме, включая production-strict. Теперь — fail-closed
		// (security.md «AuthN+AuthZ ВЕЗДЕ», core rule #16); в dev обход сохранён.
		switch cfg.AuthMode {
		case "production", "production-strict":
			return fmt.Errorf("production mode (%s): KACHO_REGISTRY_AUTHZ_BREAKGLASS must not be enabled "+
				"— it bypasses per-RPC authz Check and mTLS on both listeners; breakglass is a "+
				"non-production emergency escape only", cfg.AuthMode)
		}
		return nil
	}
	if cfg.AuthZIAMGRPCAddr == "" {
		return errors.New("authz Check required on both listeners: set KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR (or KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass)")
	}
	if !cfg.PublicServerMTLS.Enable || !cfg.InternalServerMTLS.Enable {
		return errors.New("mTLS required on both listeners: set KACHO_REGISTRY_PUBLIC_SERVER_MTLS_ENABLE and KACHO_REGISTRY_INTERNAL_SERVER_MTLS_ENABLE=true (or KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass)")
	}
	return requireTrustedForwarders(cfg)
}

// requireTrustedForwarders — в любом боевом режиме круг отправителей чужой
// личности обязан быть сужен.
//
// Оба листенера строят цепочку CertIdentityExtract →
// TrustedPrincipalExtract(WithTrustedForwarders(cfg.TrustedForwarders())).
// Контракт corelib (pkg/grpcsrv principalIsTrusted) сужает круг ТОЛЬКО на непустом
// списке; на пустом он отвечает «доверяем» ЛЮБОМУ пиру, прошедшему проверку
// сертификата, и переданная в метаданных личность становится субъектом проверки
// прав (pkg/authz subject_extract). То есть на пустом списке сосед со своим
// законным сертификатом (compute, nlb, vpc, storage, оператор) читает, меняет и
// удаляет чужие реестры и репозитории от имени жертвы, а на внутреннем листенере
// ещё и дёргает административные RPC. Внутренний периметр у нас объявлен
// НЕдоверенным, сетевой политики на поды registry нет, и слой TLS имена не сверяет
// — сужает только этот список.
//
// Проверяем результат TrustedForwarders(), а не длину сырого поля: там же, где
// сужение реально произойдёт, отбрасываются пустые записи, поэтому `SANS=","` не
// может пройти гейт и вернуть дыру.
//
// dev осознанно терпит пусто (in-process фикстуры) — но только там: на РАЗВЁРНУТОМ
// стенде dev-посадка запрещена отдельным правилом (production-mode ВЕЗДЕ).
func requireTrustedForwarders(cfg config.Config) error {
	switch cfg.AuthMode {
	case "production", "production-strict":
	default:
		return nil
	}
	if len(cfg.TrustedForwarders()) == 0 {
		return errors.New("trusted-forwarder allow-list required: set KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS " +
			"(empty → any certificate-verified peer may forward an end-user identity, so a neighbouring " +
			"service can act as any tenant; pin the api-gateway SAN)")
	}
	return nil
}

// identityUnary / identityStream — цепочка извлечения личности вызывающего,
// ОДНА на оба листенера.
//
// Пара, а не одиночный извлекатель: сначала классифицируется транспорт и
// снимается личность клиентского сертификата (CertIdentityExtract), и только
// потом переданная в метаданных личность конечного пользователя принимается —
// и только от пира, чья личность сертификата перечислена оператором.
//
// Почему это обязательно и на ПУБЛИЧНОМ листенере. Прежде он монтировал
// grpcsrv.UnaryPrincipalExtract, который читает x-kacho-principal-* безусловно;
// его собственный godoc разрешает такое лишь там, куда не дозвонится
// неконтролируемый пир. У registry дозванивается любой: сетевой политики на его
// поды нет (в отличие от vpc/nlb), :9090 — обычный Service пространства имён, а
// клиентский сертификат всем соседям выдаёт один и тот же внутренний центр.
//
// Список отправителей приходит ТОЛЬКО из конфигурации и никогда не задаётся здесь
// литералом: пустой список для corelib означает не «никому», а «любому пиру с
// проверенным сертификатом» (pkg/grpcsrv principalIsTrusted сужает круг лишь на
// непустом списке), и переданная личность становится субъектом проверки прав.
// Боевой режим на пустом списке не стартует (validateSecurityConfig).
//
// Законный отправитель один — api-gateway, и он ходит на ОБА листенера
// (KACHO_API_GATEWAY_REGISTRY_GRPC :9090 и ..._REGISTRY_INTERNAL_GRPC :9091),
// поэтому список общий: внутренний периметр не освобождён.
func identityUnary(cfg config.Config) []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		grpcsrv.UnaryCertIdentityExtract(),
		grpcsrv.UnaryTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(cfg.TrustedForwarders()...)),
	}
}

func identityStream(cfg config.Config) []grpc.StreamServerInterceptor {
	return []grpc.StreamServerInterceptor{
		grpcsrv.StreamCertIdentityExtract(),
		grpcsrv.StreamTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(cfg.TrustedForwarders()...)),
	}
}
