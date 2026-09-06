// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
	"github.com/PRO-Robotech/kacho/pkg/retention"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/check"
	geoclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/iam"
	zotclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/zot"
	"github.com/PRO-Robotech/kacho/services/registry/internal/dataplane"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	"github.com/PRO-Robotech/kacho/services/registry/internal/handler"
	"github.com/PRO-Robotech/kacho/services/registry/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/registry/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/schemaguard"

	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// Сроки ПЛОСКОСТИ ДАННЫХ. Названы отдельно от диагностических намеренно: у этих
// двух поверхностей разные предметы, и общие числа читались бы как утверждение,
// что предмет один.
const (
	// dataplaneReadHeaderBudget — потолок чтения заголовка. Длиннее
	// диагностического: docker-клиент шлёт заголовки после рукопожатия TLS через
	// вход кластера, и жёсткая отсечка отрезала бы медленную, но исправную сеть.
	dataplaneReadHeaderBudget = 15 * time.Second
	// dataplaneIdleBudget — потолок простоя keep-alive соединения. Прежде его не
	// было вовсе: `IdleTimeout` не задавался, `ReadTimeout` тоже, а stdlib читает
	// такой ноль как «не ограничивать» — то есть брошенное docker-клиентом
	// соединение висело до конца жизни процесса.
	dataplaneIdleBudget = 120 * time.Second
	// dataplaneShutdownBudget — срок гашения: начатый push обязан дописаться.
	dataplaneShutdownBudget = 15 * time.Second
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
	// Стража круга отправителей живёт рядом с конфигурацией и срабатывает на ЛЮБОМ
	// non-breakglass старте — поэтому зовётся отдельно и до разбора режима.
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := validateSecurityConfig(cfg); err != nil {
		return err
	}
	// Хранилище слоёв аутентифицирует всех: без учётных данных сервис не смог бы в
	// него ходить — а значит хранилище открыто любому в сети подов.
	if err := requireZotCredentials(cfg.Posture(), cfg.ZotAddr, cfg.ZotUsername, cfg.ZotPassword); err != nil {
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
	//
	// Поверхность входит в контур ОТДЕЛЬНЫМ ПРОФИЛЕМ той же функции (решение
	// владельца XC-7, в-1): не gRPC, цепочка другая, полями общего дескриптора её
	// не втягивают. Корень приносит ОБЪЯВЛЕНИЕ; подъём, самоотчёт и гашение
	// принадлежат профилю.
	//
	// Посадка разбирается ЗДЕСЬ и уезжает в оба объявления — и в профиль
	// поверхности, и в дескриптор процесса. Двух разборов одной ручки не
	// заводится: разойтись им было бы нечем, но читателю пришлось бы это доказывать.
	mode, merr := servicecontract.ParseMode(cfg.AuthMode)
	if merr != nil {
		return fmt.Errorf("KACHO_REGISTRY_AUTH_MODE: %w", merr)
	}
	svcMetrics := metrics.New()
	// Готовность СТРОИТСЯ из именованных зависимостей и отдаётся отдельным путём
	// от живости; чарт пробирует именно её. Носитель канала к владельцу прав
	// приезжает ниже по тексту — до его установки готовность отвечает «не готов»,
	// а не «готов» (см. buildReadinessCheckers).
	var authzSlot health.Slot
	// Версия схемы читается из ВСТРОЕННОГО набора миграций — того же, что
	// применяет мигратор. Least-privilege serve-бинаря это не нарушает: набор
	// читается как встроенные байты, а у базы спрашивается ОДИН `SELECT`
	// применённой версии; схему serve-бинарь по-прежнему не меняет.
	healthAgg := health.New(buildReadinessCheckers(pool, cfg.AuthZIAMGRPCAddr != "", &authzSlot,
		schemaguard.CheckFromFS(migrations.FS, schemaguard.PgxVersionReader(pool))))
	// Гашение переводит готовность в 503 ДО остановки слушателей: kubelet
	// перестаёт слать трафик, пока текущие вызовы дорабатывают.
	go func() {
		<-ctx.Done()
		healthAgg.SetShuttingDown()
	}()
	diagDesc, derr := describeDiagnosticSurface(cfg.MetricsAddr, svcMetrics, healthAgg, mode, logger)
	if derr != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", derr)
	}
	// Собственный контекст поверхности: гасится ПОСЛЕ плоскости данных и обоих
	// gRPC-слушателей — скрейп и проба живости не должны исчезать раньше, чем
	// процесс закончил останавливаться.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	defer stopDiag()
	// Привязка порта синхронна: занятый адрес — ошибка посадки, и процесс не
	// вправе объявить себя поднявшимся, оставив её на код возврата.
	waitDiag, diagErr := servicehost.ServeSurface(diagCtx, diagDesc)
	if diagErr != nil {
		return fmt.Errorf("диагностическая поверхность: %w", diagErr)
	}

	// ── LRO-стек: общая operations-таблица (corelib) каталога kacho_registry.
	opsRepo := operations.NewRepo(pool, "kacho_registry")

	// Фоновая уборка терминальных строк таблицы операций.
	//
	// Строка заводится КАЖДОЙ мутацией — контракт объявляет мутации асинхронными,
	// и `Operation` возвращается вместо ресурса, — а снятия строк не было ни у
	// одного из восьми владельцев. Порог, предикат и расписание объявлены в
	// `pkg/operations` и `pkg/retention` ОДИН раз: восемь расписаний об одном
	// предмете разошлись бы молча.
	if _, err := operations.StartRetentionSweep(
		ctx, opsRepo, operations.DefaultRetentionConfig(),
		logger,
	); err != nil {
		return fmt.Errorf("фоновая уборка таблицы операций: %w", err)
	}

	// Фоновая уборка ДОСТАВЛЕННЫХ строк очереди регистрации (#1361).
	//
	// Дренаж помечает доставленную строку `sent_at` и не удаляет её никогда, а
	// заводится она в writer-транзакции КАЖДОЙ мутации: темп задаёт арендатор,
	// рост был монотонным и вечным.
	//
	// Ключ партиции — ТОТ ЖЕ, которым пользуются клейм дренажа и анти-джойн
	// реконсайлера, и он обязателен: без него уборка сняла бы доставленную
	// строку, которая одна и не даёт оживить отравленного предшественника, —
	// то есть вернула бы возможность отменить уже применённое снятие доступа.
	if _, err := outbox.StartQueueRetentionSweep(
		ctx, pool,
		outbox.QueueRetentionConfig{
			Table:           registerOutboxTable,
			PartitionColumn: reconciler.RegisterOutboxPartition,
		},
		retention.DefaultConfig(),
		logger.With(slog.String("component", "queue_retention_sweep")),
	); err != nil {
		return fmt.Errorf("фоновая уборка доставленных строк очереди: %w", err)
	}

	// Фоновая уборка РЕСУРСНОГО ЖУРНАЛА подписки (#1666). Строка в него пишется
	// триггером на каждой мутации реестра, темп задаёт арендатор, а снятия строк
	// не было ни на одном пути. Порог, предикат и обещание подписчику объявлены
	// в `pkg/subscription` ОДИН раз — здесь только провязка.
	if err := startJournalRetentionSweep(ctx, pool, cfg, logger); err != nil {
		return err
	}

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
			return fmt.Errorf("dial kaname internal: %w", err)
		}
		defer func() { _ = authzConn.Close() }()
		// Носитель зависимости, объявленной посадкой выше. До этой строки
		// готовность отвечает «не готов»: окно старта не зачитывается в готовность
		// молча.
		authzSlot.Install(func(context.Context) error { return authzConnHealth(authzConn) })
	}

	// ── ребро registry→iam PUBLIC (:9090, mTLS): ProjectService.Get (existence-
	// валидация project на Create). ОТДЕЛЬНЫЙ conn — ProjectService зарегистрирован
	// только на public :9090; вызов на :9091 (authzConn) вернул бы Unimplemented →
	// фикс. INTERNAL на Create. ServerName public dial-host'а (kaname.*) ≠ internal,
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
			return fmt.Errorf("dial kaname project: %w", err)
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
		syncReg, rerr := iamclient.NewSyncRegistrar(iamclient.NewRegisterResourceClient(iamConn))
		if rerr != nil {
			return fmt.Errorf("собрать синхронный registrar: %w", rerr)
		}
		registryUC.WithSyncRegistrar(syncReg)
	}

	// ── совещательная полоса учёта числа ресурсов ────────────────────────────
	// Величины живут у kaname на ВНУТРЕННЕМ слушателе (:9091) — админская
	// поверхность, которой на публичном нет и быть не должно. Зеркало аккаунта
	// берётся из УЖЕ существующего вызова к соседу за проектом (:9090), новым
	// ребром работа не обзаводится.
	//
	// Ребро registry→домен величин: ОДНО ребро, ДВЕ полосы — разрешение величины
	// на пути запроса и фоновая дельта, которой снимок догоняет авторитет.
	//
	// Адрес ОБЪЯВЛЕН ручкой, а не выведен из адреса авторизации. Ветки «полоса не
	// собирается, потому что соседа не задали» здесь БОЛЬШЕ НЕТ: незаданное
	// объявление отвергает страж старта, а объявленное отсутствие — законная
	// посадка, в которой тянущий не заводится, а состояние курсора называет
	// причину. Приёмка
	// `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
	// стадия S1.
	quotaEdge, stopQuotaEdge, qerr := buildQuotaAuthorityEdge(ctx, cfg, pool, iamAdapter, logger)
	if qerr != nil {
		return qerr
	}
	defer stopQuotaEdge()

	var quotaHandler *handler.QuotaHandler
	if quotaEdge.Guard != nil {
		registryUC.WithQuotaGuard(quotaEdge.Guard)
		// Чтение квот арендатором — та же полоса, что и ранний отказ: у чтения
		// и у полосы ровно два источника, и они одни и те же.
		quotaHandler = handler.NewQuotaHandler(quotaEdge.Guard)
	}
	logger.Info("quota_guard", "wired", quotaEdge.Guard != nil)

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
	// применяется через kaname fga-proxy (:9091, mTLS, идемпотентно, at-least-once,
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
		// Каждая ДОСТАВЛЕННАЯ строка инкрементит счётчик своего направления
		// (#1714). Прежде эту величину ставил скан как `count(*)` по живым
		// строкам — совпадая с объявленным «за всё время» ровно до тех пор,
		// пока строки не убираются. Наблюдатель считает СОБЫТИЕ доставки,
		// поэтому уборка на величину не влияет by construction.
		drainer.WithDeliveryObserver[domain.RegisterIntent](
			outboxmetrics.DeliveryObserver(registerOutboxTable, outboxmetrics.RegisterOutboxDirections(), svcMetrics)),
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
	// недоставленная регистрация оставляет объект без mirror-строки в kaname, а
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

	// per-repo authz-Check для ScopeFiltered RPC (ListRepositories/ListTags/DeleteTag):
	// звено решения о доступе их пропускает, handler сам Check'ает (call-gate +
	// row-filter + existence-hiding). Тот же conn к iam :9091, что и per-RPC звено.
	// authzConn==nil (breakglass) → nil authorizer → handler bypass.
	var listAuthz handler.Authorizer
	if authzConn != nil {
		listAuthz = check.NewIAMCheckClient(authzConn)
	}
	// Сужатель страницы — ТОТ ЖЕ объект, который решает по сужаемым методам, а не
	// собранный рядом второй: два сужателя были бы двумя предметами, расходящимися
	// молча. Порт `handler.Authorizer` его не отдаёт (и не должен — это порт
	// обработчика), поэтому берём у конкретного клиента; форма присваивания выше
	// оставлена дословно, её читает страж сужения списков
	// (`tools/auditlistfilter`).
	var pageNarrower servicecontract.ListNarrower
	if c, ok := listAuthz.(*check.IAMCheckClient); ok {
		pageNarrower = c.Narrower()
	}
	// Величины сужателя выходят из процесса ТОЛЬКО здесь. Полос четыре: одна
	// положительная и три — страница, ушедшая БЕЗ пообъектной проверки. Снимите
	// эту строку — и полосы исчезнут с поверхности, а не станут нулями; ровно это
	// ловит гейт дерева `TestEveryListNarrowConsumerRegistersItsCollector`.
	svcMetrics.RegisterListNarrow(func() listnarrow.Counts {
		if c, ok := listAuthz.(*check.IAMCheckClient); ok {
			return c.Narrower().Counts()
		}
		// Сужателя на этой посадке нет — полосы всё равно объявлены нулями:
		// «сужений не было» обязано быть отличимо от «коллектора нет».
		return listnarrow.Counts{}
	})

	// ── обработчики. Слушателей здесь больше нет: их поднимает носитель контура
	// (`pkg/servicehost`), и регистратор получает `grpc.ServiceRegistrar` —
	// интерфейс с единственным методом. Приделать сюда своё звено не к чему, и это
	// свойство ПОСТРОЕНИЯ, а не соглашение.
	registryHandler := handler.NewRegistryHandler(registryUC, listAuthz, cfg.AuthZCacheTTL)
	internalHandler := handler.NewInternalRegistryHandler(registryUC)
	opHandler := operationspb.NewHandler(opsRepo)

	// ── дескриптор процесса: ОБЪЯВЛЕНИЕ о себе. Порты (сверка существования и
	// проводка сужателя) приезжают сюда уже собранными — их предмет живёт в этом
	// корне, а судит их носитель против каталога прав на каждом старте.
	// Приёмник читателя величин кеша вердиктов звена. Объявлен ДО дескриптора:
	// кеш собирает носитель контура, и читателя он отдаёт через поле дескриптора.
	var authzCache authzmetrics.Source

	// ТРИ полосы, потому что окон положительных вердиктов у этого процесса три, и
	// два последних стоят ДРУГ ЗА ДРУГОМ на одном пути:
	//
	//   · окно звена решения — вопрос на ВЫЗОВ;
	//   · окно самого сервиса перед сужателем (`handler.cachedAuthorizer`) —
	//     вопрос на КАЖДЫЙ элемент страницы;
	//   · окно ОБЩЕГО сужателя (`pkg/listnarrow`) — то, что не поймал предыдущий.
	//
	// Сложить их в одну серию значило бы сделать невидимым то из них, которое не
	// попадает, — а это ровно то, ради которого величину и смотрят. Последнее до
	// #768 не считал никто, и «кеш сужателя даёт столько-то» было непроверяемо в
	// обе стороны.
	svcMetrics.RegisterAuthzCache(map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    authzCache.Cache,
		authzmetrics.LaneList:   registryHandler.VerdictCacheStats,
		authzmetrics.LaneNarrow: pageNarrower.CacheStats,
	}, authzCache.Read)

	// Поток изменений. Сужатель — тот же объект, что сужает страницы списков; его
	// отсутствие (аварийный режим) оставляет глагол невыставленным, а не
	// выставленным и не сужающим.
	var pageNarrowerImpl *listnarrow.Narrower
	if c, ok := listAuthz.(*check.IAMCheckClient); ok {
		pageNarrowerImpl = c.Narrower()
	}
	subscribeSrv, err := buildSubscriptionServer(cfg, pageNarrowerImpl, logger)
	if err != nil {
		return err
	}
	if subscribeSrv == nil {
		logger.Warn("поток изменений не поднят: сужателя списков нет на этой посадке — " +
			"глагол подписки не выставляется, чтобы не отдавать журнал без сужения")
	}

	desc, err := describe(cfg, mode, logger, servePorts{
		existence:    pg.NewExistenceProbe(pool),
		narrower:     pageNarrower,
		authzObserve: authzCache.Install,
		metricsReg:   svcMetrics.Registerer(),
	})
	if err != nil {
		return err
	}

	// ── ПЛОСКОСТЬ ДАННЫХ OCI (registry.kacho.local): Docker Registry v2 /
	// OCI token-auth flow перед zot.
	//
	// Это НЕ диагностика, и профиль их не смешивает: поверхность досягаема
	// СНАРУЖИ кластера и аутентифицирует КАЖДЫЙ запрос сама — проверка подписи
	// Bearer'а против зеркала ключей iam плюс вопрос владельцу модели прав. Обе
	// эти вещи она объявляет данными, и именно их пара делает объявление
	// судимым: снаружи досягаемая поверхность с объявленным ОТСУТСТВИЕМ
	// аутентификации профилем не принимается вовсе.
	//
	// Два срока у неё тоже свои, и оба — следствие потоковости: потолка чтения и
	// записи запроса НЕТ (слой образа едет минутами; потолок разорвал бы
	// исправную передачу), а срок гашения втрое длиннее диагностического —
	// начатый push обязан дописаться.
	//
	// Выключение поверхности — тоже ОБЪЯВЛЕНИЕ: пустой адрес превращается в
	// названную причину, а не в молчаливо пропущенную ветку. У этой поверхности
	// цена выключения особенно велика — docker push/pull перестаёт существовать
	// целиком, — и в журнале она теперь названа.
	dpAddr := servicecontract.Value(cfg.DataplaneAddr)
	var dpHandler http.Handler
	if cfg.DataplaneAddr == "" {
		dpAddr = servicecontract.NotApplicable[string](
			"KACHO_REGISTRY_DATAPLANE_ADDR не задан профилем развёртывания: docker push и pull " +
				"на этой посадке не обслуживаются вовсе, реестр остаётся только управляющей " +
				"поверхностью")
	} else {
		// registryRepo подаётся трижды и в трёх разных ролях: RepoRegistrar (эмит
		// интента + durable-признак существования), RepositoryPresence (чтение того же
		// признака ⊔ строки наложения — по нему выбирается глагол записи) и
		// RegistryLookup (owning-project реестра для containment scope интента).
		h, dperr := buildDataplaneHandler(cfg, authzConn, registryRepo, zotAdapter, registryRepo, registryRepo, pendingBlobRepo, pushGrantRepo, logger)
		if dperr != nil {
			return fmt.Errorf("build data-plane proxy: %w", dperr)
		}
		dpHandler = h
	}
	dpProfile, dperr := servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-registry",
		Name:    "плоскость данных OCI (docker push/pull)",
		Mode:    mode,
		Logger:  logger,

		Addr:    dpAddr,
		Handler: dpHandler,

		Reach: servicecontract.ReachExternal,
		Auth: servicecontract.Value[servicecontract.SurfaceAuthMech](
			"каждый запрос: подпись Bearer-токена против зеркала ключей проверки iam " +
				"(fail-closed при недоступности зеркала) плюс вопрос владельцу модели прав " +
				"по названному репозиторию, со скрытием существования"),

		ReadHeaderBudget: dataplaneReadHeaderBudget,
		RequestBudget: servicecontract.NotApplicable[time.Duration](
			"поверхность потоковая: слой образа едет минутами, и потолок чтения либо " +
				"записи разорвал бы исправную передачу по таймеру"),
		IdleBudget:     dataplaneIdleBudget,
		ShutdownBudget: dataplaneShutdownBudget,
	})
	if dperr != nil {
		return fmt.Errorf("профиль плоскости данных: %w", dperr)
	}
	if dpProfile.Enabled() {
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
	// Плоскость данных поднимается СРАЗУ и своим контекстом: её гашение идёт
	// первым, раньше gRPC-слушателей, и раньше диагностики.
	//
	// Прежде она поднималась `ListenAndServe` в горутине — то есть привязка порта
	// происходила ТАМ ЖЕ, где обслуживание, и занятый адрес попадал в журнал
	// строкой уровня Error, после чего процесс продолжал работать, объявляя себя
	// исправным при мёртвой плоскости данных. Профиль возвращает это отказом.
	dpCtx, stopDP := context.WithCancel(context.Background())
	waitDP, dpErr := servicehost.ServeSurface(dpCtx, dpProfile)
	if dpErr != nil {
		stopDP()
		return fmt.Errorf("плоскость данных: %w", dpErr)
	}

	// Порядок гашения того, что не входит в gRPC-контур. Носитель гасит свои два
	// слушателя сам; об этих трёх предметах он не знает и знать не должен — их
	// жизненный цикл принадлежит сервису.
	//
	// Порядок: отмена контекста → docker push/pull дописывается → дренаж
	// исполнителей операций → и ПОСЛЕДНЕЙ диагностика. Прежде диагностика
	// закрывалась ДО дренажа, то есть скрейп и проба живости исчезали, пока
	// процесс ещё останавливался; теперь она переживает остановку, как у
	// остальных сервисов платформы.
	//
	// Каждое ожидание — за возвратом ПРОФИЛЯ, а не за вызовом гашения: профиль
	// возвращается только после того, как порт освобождён.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		stopDP()
		if derr := waitDP(); derr != nil {
			logger.Error("плоскость данных остановлена с ошибкой", "err", derr)
		}
		// Дренируем in-flight LRO-worker'ы: SIGTERM не должен оставить async-мутацию
		// done=false навсегда (клиент завис бы в polling). Свежий ctx — request-ctx
		// уже отменён возвратом Operation клиенту.
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelDrain()
		if werr := operations.Wait(drainCtx); werr != nil {
			logger.Warn("LRO workers did not finish before shutdown timeout",
				"err", werr, "active", operations.Active())
		}
		stopDiag()
		if derr := waitDiag(); derr != nil {
			logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
		}
	}()

	serveErr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) { registerPublic(reg, registryHandler, quotaHandler, opHandler) },
		func(reg grpc.ServiceRegistrar) { registerInternal(reg, internalHandler, opHandler, subscribeSrv) },
	)
	cancel()
	<-shutdownDone
	return serveErr
}

// registerPublic — публичный слушатель :9090: tenant-facing RegistryService плюс
// опрос длительных операций.
func registerPublic(reg grpc.ServiceRegistrar, h registryv1.RegistryServiceServer,
	quotaHandler *handler.QuotaHandler, opHandler operationpb.OperationServiceServer) {
	registryv1.RegisterRegistryServiceServer(reg, h)
	// Чтение квот выставляется, ТОЛЬКО когда полоса учёта собрана. Иначе метод
	// отвечал бы пустым набором на каждый запрос — то есть «квот нет», ровно то
	// утверждение, которое контракт запрещает делать. Незарегистрированный метод
	// отвечает `Unimplemented`, и это честно: возможности здесь действительно нет.
	if quotaHandler != nil {
		registryv1.RegisterQuotaServiceServer(reg, quotaHandler)
	}
	operationpb.RegisterOperationServiceServer(reg, opHandler)
}

// registerInternal — cluster-internal слушатель :9091: админ-RPC реестра
// (GC/статистика) плюс тот же опрос операций.
//
// `InternalRegistryService` регистрируется ТОЛЬКО здесь: `Internal.*` не
// публикуется на внешнем endpoint. Разделение проверяется пробой через
// `grpc.Server.GetServiceInfo` — регрессия «Internal* уехал на публичный» ловится,
// а не остаётся на совести обзора.
func registerInternal(reg grpc.ServiceRegistrar, h registryv1.InternalRegistryServiceServer,
	opHandler operationpb.OperationServiceServer,
	subscribe subscriptionv1.InternalSubscriptionServiceServer) {
	registryv1.RegisterInternalRegistryServiceServer(reg, h)
	operationpb.RegisterOperationServiceServer(reg, opHandler)
	// Поток изменений — тоже Internal-глагол, и служится он ТОЛЬКО здесь.
	//
	// Ноль означает, что сужателя на этой посадке нет вовсе (аварийный режим), и
	// тогда глагол НЕ выставляется: за ним нет пообъектной проверки на крае, и
	// сервер без сужателя отдал бы весь журнал целиком. Незарегистрированный
	// метод отвечает `Unimplemented`, и это честно — возможности здесь
	// действительно нет; выставленный и не сужающий выглядел бы работающим.
	if subscribe != nil {
		subscriptionv1.RegisterInternalSubscriptionServiceServer(reg, subscribe)
	}
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
	if err := requireDataplaneTLSAck(cfg.Posture(), cfg.DataplaneTLSTerminatedExternally); err != nil {
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
		if authzConn == nil {
			return nil, errors.New("data-plane requires authz IAM conn (KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR)")
		}
		v, verr := buildTokenVerifier(cfg)
		if verr != nil {
			return nil, verr
		}
		verifier = v
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
//
// РЕПЛИКИ: на-реплику — проход — один условный оператор `DELETE … WHERE <отметка> <= <порог>`.
// Строки заперты самим оператором, поэтому вторая реплика уносит только
// остаток, а на пустой выборке не делает ничего; к соседям проход не ходит.
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

// requireDataplaneTLSAck — data-plane OCI-листенер (DataplaneAddr) обслуживает открытый
// HTTP; штатно TLS терминируется внешним ingress/mesh перед подом. По этому сокету
// транзитят bearer identity-JWT (Hydra-issued, реплеябельные в пределах TTL). В
// production/production-strict молчаливый plaintext-старт запрещён: если ingress
// ошибочно настроен на plaintext-passthrough, docker-login токены утекают в открытом
// виде (harvest+replay, CWE-319). Оператор обязан ЯВНО подтвердить внешнюю TLS-
// терминацию (KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY=true) — параллель
// Config.TokenAcceptance. В dev — no-op (как http:// JWKS и DB
// sslmode=disable). Вызывается только когда data-plane поднимается (DataplaneAddr!="").
func requireDataplaneTLSAck(mode servicecontract.Mode, tlsTerminatedExternally bool) error {
	if mode.IsProduction() && !tlsTerminatedExternally {
		return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY=true "+
			"(the data-plane serves plaintext HTTP and must sit behind external TLS termination; "+
			"bearer identity-JWTs would otherwise transit cleartext)", mode)
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
// (параллель requireDataplaneTLSAck / Config.TokenAcceptance).
//
// zotAddr пуст ⇒ хранилище не сконфигурировано, ходить некуда — гейт молчит.
// В dev — no-op (in-process фикстуры поднимают zot без аутентификации).
func requireZotCredentials(mode servicecontract.Mode, zotAddr, username, password string) error {
	if !mode.IsProduction() || strings.TrimSpace(zotAddr) == "" {
		return nil
	}
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_ZOT_USERNAME and "+
			"KACHO_REGISTRY_ZOT_PASSWORD (the layer store at %q must authenticate its callers; "+
			"without credentials it serves anyone that reaches its port, and the whole data-plane "+
			"authorization is one hop away)", mode, zotAddr)
	}
	return nil
}

// validateAuthMode разбирает KACHO_REGISTRY_AUTH_MODE (whitelist) и строгость
// DB-SSL. Режим не управляет authz/mTLS — ими управляет breakglass (см.
// validateSecurityConfig). `production-strict` дополнительно требует SSL до БД.
func validateAuthMode(cfg config.Config, logger *slog.Logger) error {
	// Словарь допустимых значений — НЕ свой: он объявлен в дереве один раз
	// (`servicecontract.Modes`), и отказ перечисляет ТОТ ЖЕ набор, что у остальных
	// шести стражей старта. Свой словарь здесь был, и он был одним из пяти.
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return fmt.Errorf("KACHO_REGISTRY_AUTH_MODE: %w", err)
	}
	switch mode {
	case servicecontract.ModeDev:
		if cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable" {
			logger.Warn("KACHO_REGISTRY_DB_SSLMODE=disable — DB plaintext (dev only)")
		}
	case servicecontract.ModeProductionStrict:
		// Перечень безопасных значений — тоже НЕ свой: он приходит из дома семантики
		// строки подключения (`pkg/db`), где объявлен один раз на всё дерево
		// (задача продукта #1464). Судится ИСХОД — режим строки, уходящей в пул.
		if sslMode := coredb.SSLModeFromDSN(cfg.DSN()); !coredb.SSLModeSecure(sslMode) {
			return fmt.Errorf("production-strict mode: KACHO_REGISTRY_DB_SSLMODE must be one of %s (got %q)",
				strings.Join(coredb.SecureSSLModes(), "|"), cfg.DBSSLMode)
		}
		logger.Warn("AuthMode=production-strict: DB SSL strictly validated")
	}
	return nil
}

// validateSecurityConfig — secure-by-default: операции без авторизации и mTLS
// запрещены. Per-RPC authz Check (адрес kaname) и mTLS на ОБОИХ листенерах
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
		if breakglassRefusedIn(cfg.Posture()) {
			return fmt.Errorf("production mode (%s): KACHO_REGISTRY_AUTHZ_BREAKGLASS must not be enabled "+
				"— it bypasses per-RPC authz Check and mTLS on both listeners; breakglass is a "+
				"non-production emergency escape only", cfg.Posture())
		}
		return nil
	}
	if cfg.AuthZIAMGRPCAddr == "" {
		return fmt.Errorf("%sauthz Check required on both listeners: set "+
			"KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR to the internal endpoint of kaname (:9091)%s",
			bootRefusalModePrefix(cfg.Posture()), breakglassBypassHint(cfg.Posture()))
	}
	if !cfg.PublicServerMTLS.Enable || !cfg.InternalServerMTLS.Enable {
		return fmt.Errorf("%smTLS required on both listeners: set "+
			"KACHO_REGISTRY_PUBLIC_SERVER_MTLS_ENABLE and KACHO_REGISTRY_INTERNAL_SERVER_MTLS_ENABLE=true%s",
			bootRefusalModePrefix(cfg.Posture()), breakglassBypassHint(cfg.Posture()))
	}
	return requirePeerTransport(cfg)
}

// breakglassRefusedIn — ЕДИНСТВЕННЫЙ предикат «в этом режиме breakglass отвергнут».
//
// Он читается обеими сторонами шва: стражем, который breakglass запрещает, и
// составителем совета, который его предлагает. Пока предикат был один (switch
// внутри стража), а совет — константой в тексте отказа, стороны разошлись молча:
// отказ рекомендовал ровно то, чем страж выше по этой же функции валит старт.
func breakglassRefusedIn(mode servicecontract.Mode) bool { return mode.IsProduction() }

// breakglassBypassHint — хвост «как обойти», приписываемый к отказу посадки.
//
// Зависит от режима, и это не косметика. Текст отказа — контракт ОПЕРАТОРА: по
// нему в три часа ночи выбирают следующий шаг, и другого источника в этот момент
// нет. В боевом режиме breakglass отвергается, поэтому совет его взвести называет
// заведомо неисполнимый шаг — цена ошибки измерена в полном цикле выкатки и
// ожидания раскатки, потраченном, пока сервис лежит (#1592).
//
// В боевом режиме хвоста нет ВОВСЕ — не «есть, но с оговоркой». Названная ручка
// читается как вариант независимо от того, что про неё написано рядом; предупредить
// о ней — работа отказа САМОГО breakglass-стража (он называет и ручку, и режим) и
// страницы установки, а не отказа о нехватке чего-то другого.
func breakglassBypassHint(mode servicecontract.Mode) string {
	if breakglassRefusedIn(mode) {
		return ""
	}
	return " (or set KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass — non-production only)"
}

// bootRefusalModePrefix — боевой отказ называет РЕЖИМ.
//
// Правила посадки от режима зависят, и без его имени оператор не отличит «здесь
// так нельзя» от «так нельзя нигде» — то есть не поймёт, почему тот же стенд
// поднимался вчера. Неизвестный режим сюда не доходит: `validateAuthMode` идёт
// в композиционном корне РАНЬШЕ и отвергает его по закрытому перечню.
func bootRefusalModePrefix(mode servicecontract.Mode) string {
	if breakglassRefusedIn(mode) {
		return fmt.Sprintf("production mode (%s): ", mode)
	}
	return ""
}

// requirePeerTransport — в любом боевом режиме транспорт КАЖДОГО поднимаемого
// исходящего ребра обязан быть проверяемым.
//
// Почему это отдельное измерение, а не следствие проверки листенеров. Проверка
// выше говорит о том, КАК с нами говорят; здесь — как говорим мы. Невзведённая
// ручка клиента не даёт ошибки сама по себе: grpcclient.TLSClientTransportCreds
// на Enable=false возвращает insecure-creds БЕЗ ошибки, поэтому процесс
// поднимается, честно печатает «registry→iam edges wired» с authz_mtls=false — и
// per-RPC Check уходит по открытому каналу. Контроль, от которого зависит
// решение о доступе, при этом присутствует и не отказывает ни разу за свою жизнь.
//
// Предикат активности — ТОТ ЖЕ, что читает проводка: composition root поднимает
// соединение ровно при непустом адресе (`if cfg.<Addr> != ""` в runServe).
// Поэтому «страж увидел ребро» ⟺ «ребро дилится»: незаданный адрес не порождает
// требования к транспорту, а заданный — порождает всегда. Связь стража с
// проводкой заперта peer_transport_wiring_test.go.
//
// Что покрыто: authz (:9091 — Check + fga-proxy регистрация владельца), project
// (:9090 — существование проекта и поиск аккаунта, вход в резолв области), geo
// (:9090 — регион реестра). Ребро JWKS сюда НЕ входит: оно ходит по HTTPS
// односторонним TLS и держится своей стражей (requireSecureKeySetURL).
//
// dev осознанно терпит невзведённый транспорт — только локальные фикстуры; на
// РАЗВЁРНУТОМ стенде dev-посадка запрещена отдельным правилом (production-mode
// ВЕЗДЕ), поэтому послабление не расширяет поверхность стенда.
func requirePeerTransport(cfg config.Config) error {
	if !cfg.Posture().IsProduction() {
		return nil
	}
	if cfg.AuthZIAMGRPCAddr != "" && !cfg.IAMAuthzMTLS.Enable {
		return errors.New("verified transport required on the registry→iam authz edge: set " +
			"KACHO_REGISTRY_IAM_AUTHZ_MTLS_ENABLE=true (with cert/key/CA) — the per-RPC authorization Check " +
			"and the owner-tuple registration travel over this connection, and unarmed client credentials " +
			"degrade to cleartext silently, so the process starts and reports authorization as enabled")
	}
	if cfg.IAMProjectGRPCAddr != "" && !cfg.IAMProjectMTLS.Enable {
		return errors.New("verified transport required on the registry→iam project edge: set " +
			"KACHO_REGISTRY_IAM_PROJECT_MTLS_ENABLE=true (with cert/key/CA) — project existence and the " +
			"account lookup that scopes a registry are decided on this connection, and unarmed client " +
			"credentials degrade to cleartext silently")
	}
	if cfg.GeoGRPCAddr != "" && !cfg.GeoMTLS.Enable {
		return errors.New("verified transport required on the registry→geo edge: set " +
			"KACHO_REGISTRY_GEO_MTLS_ENABLE=true (with cert/key/CA) — region existence for registry " +
			"placement is decided on this connection, and unarmed client credentials degrade to cleartext silently")
	}
	return nil
}

// servePorts — порты, которые сервис приносит носителю контура вместе с
// объявлением о себе. Их предмет живёт в этом корне (своя БД, свой клиент к
// владельцу модели), поэтому собрать их за сервис носитель не может; судит он их
// сам — против каталога прав, на каждом старте.
type servePorts struct {
	// existence — сверка «есть ли объект в МОЕЙ базе». Обязателен ровно потому,
	// что дескриптор объявляет скрытие существования: без него отказ на
	// существующем чужом реестре пришёл бы текстом, отличимым от промаха владельца.
	existence servicecontract.ExistenceProbe
	// narrower — ПРОВОДКА сужателя списочной выдачи. Перечень сужаемых методов
	// даёт каталог прав, а не это поле.
	narrower servicecontract.ListNarrower
	// authzObserve — приёмник читателя величин кеша вердиктов ЗВЕНА решения.
	// Кеш строит носитель контура, поэтому иначе его величины из процесса не
	// выходят.
	authzObserve func(read func() authz.Metrics)
	// metricsReg — реестр, в котором носитель заводит серии задержки
	// обслуженного вызова. Приходит из корня, потому что диагностическую
	// поверхность держит он: серии носитель заводит своими руками, и другого
	// пути к скребомому реестру у него нет. Разбор решения — у
	// `servicecontract.Spec.Metrics`.
	metricsReg prometheus.Registerer
}

// describe собирает ОБЪЯВЛЕНИЕ сервиса о себе.
//
// Стражей старта здесь нет ни одного — они живут в конструкторе дескриптора и в
// носителе. То, что осталось в композиционном корне (`validateAuthMode`,
// `validateSecurityConfig`, `requireZotCredentials`, `requirePeerTransport` и
// стражи плоскости данных), носителем НЕ выражается и потому не снимается: он
// судит боевую посадку, а эти четверо действуют в ЛЮБОМ режиме либо стерегут
// поверхность, которой у носителя нет вовсе (docker-листенер, хранилище слоёв).
//
// Прежде здесь собирались две цепочки звеньев и два сервера. Теперь сервис
// приносит ЗНАЧЕНИЯ, а порядок звеньев — один на все сервисы и правится как
// правка контура, а не как правка реестра.
func describe(cfg config.Config, mode servicecontract.Mode, logger *slog.Logger,
	ports servePorts) (servicecontract.Descriptor, error) {
	// Транспорт ребра решения о доступе строится ЗДЕСЬ, а проверяется
	// конструктором дескриптора — по ответу самого транспорта, а не по ручке:
	// сборщик на невзведённой ручке отдаёт незашифрованные креды БЕЗ ошибки.
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("registry→iam Check mTLS creds: %w", err)
	}
	publicCreds, err := grpcsrv.TLSServerTransportCreds(cfg.PublicServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(cfg.InternalServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("internal listener tls creds: %w", err)
	}

	// Потолок темпа и одновременности НА ВЫЗЫВАЮЩЕГО: величины посадки там, где
	// она их назвала, и пол платформы там, где молчит. Сборка стоит ДО
	// дескриптора намеренно — негодный набор обязан назвать СЛУШАТЕЛЯ, а не
	// приехать в общий список находок безымянным.
	admission, err := servicecontract.AdmissionFromPosture(cfg.AdmissionPublic, cfg.AdmissionInternal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_REGISTRY_ADMISSION_*: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-registry",
		Mode:    mode,
		Logger:  logger,

		// Круг отправителей чужой личности. Значение — то же, что читают стража
		// конфигурации и самоотчёт о посадке (`cfg.TrustedForwarders()`), поэтому
		// «страж пропустил» ⟺ «круг реально сужен» по построению. Законный
		// отправитель один — api-gateway, и он ходит на ОБА слушателя, поэтому круг
		// общий: внутренний периметр не освобождён.
		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_REGISTRY_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		// Домен доверия — ЗНАЧЕНИЕ: личность клиентского сертификата этот процесс
		// разбирает, и разбирает её ОТНОСИТЕЛЬНО домена. Читается из той же
		// функции, значение которой уезжает в пару звеньев извлечения личности,
		// поэтому «страж пропустил» ⟺ «домен реально объявлен».
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: "KACHO_REGISTRY_AUTHZ_TRUST_DOMAIN",

		Authz:     servicecontract.AuthzViaIAM,
		CheckEdge: servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		// Перевод вопроса в контракт службы доступа приносит СЕРВИС: носитель
		// принадлежит фундаменту и чужого контракта не знает (приёмка K3-1,
		// раздел 7.2).
		PeerCheck: authziam.NewCheckClient,
		// Окно кэша положительных вердиктов — оно же окно отзыва. Читается через
		// `check.CacheWindow`, потому что ручка реестра несёт landed-значение
		// «ноль = кэш выключен, отзыв немедленный», а носитель требует строго
		// положительной величины; обе стороны спрашивают ОДНУ функцию, поэтому
		// разойтись им нечем.
		CacheWindow: check.CacheWindow(cfg.AuthZCacheTTL),
		// Срок ОДНОГО вопроса о доступе. Та же величина, с какой реестр спрашивает
		// владельца модели на путях списка и плоскости данных (`check.CheckTimeout`),
		// и это один источник, а не два одинаковых числа.
		ClientBudget: check.CheckTimeout,

		// Приёмник величин кеша вердиктов: носитель строит кеш, а диагностическую
		// поверхность держит этот корень, и величины переходят границу только
		// здесь. Без него доля попаданий не выходит из процесса, и «сколько даёт
		// кеш» остаётся непроверяемым в обе стороны.
		AuthzObserve: ports.authzObserve,
		Metrics:      ports.metricsReg,

		// Верхняя граница обработки вызова. «Не применимо» у неё нет: вызов без
		// срока держит соединение из ограниченного пула столько, сколько
		// выполняется его запрос. Величина и её обоснование — у ручки конфигурации.
		HandlingBudget: cfg.HandlingBudget,
		// Серверных стримов реестр не служит: его gRPC-поверхность — одиночные
		// вызовы, а долгоживущее у него живёт на HTTP-поверхности OCI, которая в
		// контур не входит вовсе (её берёт отдельный профиль). Заявление
		// самоистекает: появится первая подписка — носитель уронит старт поимённо
		// по её методу, а проба ниже назовёт её раньше.
		// Срок жизни одного потока подписки. Прежде ось стояла ИЗЪЯТИЕМ
		// («серверных стримов registry не служит»), и изъятие самоистекло ровно
		// так, как обещал его собственный комментарий: реестр стал владельцем
		// журнала изменений и служит поток на внутреннем слушателе. Изъятие есть
		// заявление о дереве, и появившийся стрим сделал бы его ложью, оставив
		// срок жизни потока неназванным никем.
		StreamBudget: servicecontract.Value(cfg.SubscriptionStreamBudget),

		// Бюджет отказов объявляется ВЕЛИЧИНОЙ, а не изъятием: решение о доступе
		// реестр принимает не у себя, а вопросом к kaname, — то есть сетевой
		// сосед, которого шторм отказов может уронить, у него ЕСТЬ. До носителя
		// этой отсечки у реестра не было вовсе (поле не заполнялось, а механизм
		// читает неположительное как «ограничения нет»).
		DenyBudget: servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		// Ось потолка объявляется ВЕЛИЧИНОЙ, а не изъятием: слушатели выставлены
		// наружу, и «потолка не надо» означало бы, что один вызывающий вправе
		// занять сервис чтением. Изъятие законно только у внутрипроцессной
		// фикстуры, и на боевой посадке дескриптор его отвергает.
		Admission: servicecontract.Value(admission),

		DBSSLMode:     servicecontract.Value(cfg.DBSSLMode),
		PublicAddr:    ":" + cfg.GrpcPort,
		InternalAddr:  ":" + cfg.InternalGrpcPort,
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		// ── оси ──────────────────────────────────────────────────────────────

		// Что реестр эмитит владельцу прав. Четыре отношения, и все четыре
		// принимаются его закрытым набором: три иерархических (`project` реестра,
		// `parent` репозитория, `owner` создателя) плюс публичное чтение `v_get`,
		// которым материализуется публичный репозиторий. Перечень выведен из
		// единственного места, где интенты собираются
		// (`internal/domain/fga_intent.go`), а не выписан по памяти.
		Emits: servicecontract.Value([]proxytuple.Relation{
			proxytuple.RelationProject,
			proxytuple.RelationParent,
			proxytuple.RelationOwner,
			proxytuple.PublicReadRelation,
		}),
		// Типы объектов, которые реестр заводит у владельца прав. Ровно два:
		// сам реестр и репозиторий внутри него (`<regId>/<repo>` — глобально
		// уникальный идентификатор by construction, core rule #15).
		Registers: servicecontract.Value([]servicecontract.ObjectType{
			domain.FGAObjectTypeRegistry,
			domain.FGAObjectTypeRepository,
		}),

		// Проводка сужателя — на ДЕСЯТЬ методов `RegistryService`, которые каталог
		// прав объявляет `scope_filtered`. Перечень выписан ИЗ КАТАЛОГА, а не из
		// памяти, и носитель сверяет его с каталогом в ОБЕ стороны: метод,
		// объявленный сужаемым и оставшийся без проводки, — отказ старта (О3);
		// проводка на метод, которого каталог сужаемым не называет, — тоже (О4).
		//
		// За этими методами пообъектной проверки на крае НЕТ вовсе: их решение
		// принимается на уровне данных в `internal/handler/listauthz.go` (личность →
		// вопрос о правах → сокрытие существования), а пакетную половину этого
		// вопроса задаёт ровно тот сужатель, который здесь объявлен.
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			"/kacho.cloud.registry.v1.RegistryService/List":             ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/CreateRepository": ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/GetRepository":    ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/ListRepositories": ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/UpdateRepository": ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/RenameRepository": ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/DeleteRepository": ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/ListTags":         ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/DeleteTag":        ports.narrower,
			"/kacho.cloud.registry.v1.RegistryService/ListReferrers":    ports.narrower,
			// Поток изменений объявлен каталогом СУЖАЕМЫМ, и проводка ему нужна
			// та же: пообъектной проверки на крае за ним нет вовсе, поэтому
			// отсутствующий сужатель означает не «строже», а «без рубежа». Носитель
			// сверяет это в обе стороны и роняет старт поимённо по методу — что он
			// и сделал, когда проводки здесь ещё не было.
			"/kacho.cloud.subscription.InternalSubscriptionService/Subscribe": ports.narrower,
		}),

		// Форма отказа для типа, чьё существование скрывается. Каталог называет
		// таким `registry_registry` (строки `hide_existence` на
		// `RegistryService/Update` и `/Delete`), и форма обязана быть ДОСЛОВНО той,
		// которой отвечает звено решения о доступе: носитель сверяет её с
		// `authz.OwnerNotFoundFormat`, поэтому выписать «на глаз» нельзя — расхождение
		// роняет старт. Текст — промах владельца из его же слоя доступа к данным
		// (`internal/repo/kacho/pg/errmap.go`, ресурс «Registry»).
		HideExistence: servicecontract.Value(map[servicecontract.ObjectType]servicecontract.NotFoundFormat{
			domain.FGAObjectTypeRegistry: "Registry %s not found",
		}),

		// Происхождение намерений регистрации: их пишет ТА ЖЕ writer-транзакция,
		// что создаёт/меняет/удаляет строку (transactional outbox
		// `kacho_registry.registry_outbox`), — происхождение доказано записью, а не
		// выведено из часов в момент доставки.
		Delivery: servicecontract.Value(servicecontract.DeliveryWriterTransaction),

		// Порт сверки существования — обязателен, потому что скрытие объявлено выше.
		Existence: ports.existence,

		// Загрузочный гейт мутаций — ИЗЪЯТИЕ, и оно названо, а не умолчано: такого
		// гейта у реестра в дереве нет (`pkg/outbox/bootgate` его композиционный
		// корень не импортирует; на сегодня гейт несут vpc, compute и nlb). Принести
		// сюда чужой было бы не переводом на носитель, а новой защитой под видом
		// оформления — и вводить её следует своим изменением, со своей приёмкой и
		// своим замером окна, в котором путь доставки ещё не поднят.
		//
		// Заявление ИСТЕКАЕТ САМО: проба `TestRegistryBringsNoBootGateYet` спрашивает
		// дерево тем же именем механизма, и как только гейт появится в корне —
		// краснеет и требует объявить его здесь величиной.
		BootGate: servicecontract.NotApplicable[servicecontract.BootGate](
			"загрузочного гейта мутаций у реестра в дереве нет: пакет гейта его композиционный " +
				"корень не импортирует. Изъятие держится пробой, которая читает дерево, а не память автора"),
	})
}
