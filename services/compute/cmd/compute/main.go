// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command compute — API-сервер kacho-compute (gRPC public :9090 + internal :9091).
//
// # Чего этот корень БОЛЬШЕ НЕ делает
//
// Он не собирает серверы, не выстраивает цепочки звеньев, не строит карту прав,
// не держит собственного звена решения о доступе и собственного загрузочного
// гейта мутаций: всё это переехало в носитель контура (`pkg/servicehost`),
// которому сервис приносит ОБЪЯВЛЕНИЕ о себе — дескриптор (`describe.go`). Оба
// слушателя поднимает `servicehost.Serve`, он же гасит их по отмене контекста.
//
// Здесь остаётся домен: пул, слой доступа к данным, use-cases, peer-клиенты,
// фоновые loop'ы под супервизором, диагностический слушатель и готовность.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/retention"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/guestaccesskey"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/machinetype"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/nodeownership"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/placementgroup"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/realization"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/handler"
	computemetrics "github.com/PRO-Robotech/kacho/services/compute/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/compute/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: compute serve")
	}
	cmd := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	switch cmd {
	case "serve":
		if err := runServe(cfg); err != nil {
			log.Fatal(err)
		}
	case "migrate":
		// Миграции вынесены в отдельный least-privilege binary kacho-migrator —
		// runtime serve-образ не несёт embed-миграции и деструктивный `migrate down`.
		log.Fatal("migrations are not handled by this binary — use the kacho-migrator CLI ({up|down|status})")
	default:
		log.Fatalf("unknown command %q (this binary only serves the API; migrations live in `kacho-migrator`)", cmd)
	}
}

// services — собранный набор бизнес-сервисов (composition-point).
type services struct {
	machineType    *machinetype.MachineTypeService
	instance       *instance.InstanceService
	guestAccessKey *guestaccesskey.Service
	realization    *realization.Service
	nodeOwnership  *nodeownership.Service
	placementGroup *placementgroup.Service
	// quota — арендаторское чтение квот. ТОЛЬКО чтение: величины назначает
	// администратор облака на внутреннем слушателе владельца величин.
	quota *handler.QuotaHandler
}

func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	// Стража круга отправителей живёт рядом с конфигурацией и срабатывает на ЛЮБОМ
	// non-breakglass старте — поэтому зовётся здесь, до разбора режима, а не
	// внутри его боевых веток.
	if verr := cfg.Validate(); verr != nil {
		return verr
	}

	productionMode, err := validateAuthMode(cfg, logger)
	if err != nil {
		return err
	}

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	opsRepo := operations.NewRepo(pool, "public")

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

	// Фоновая уборка ДОСТАВЛЕННЫХ строк очереди регистрации.
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
			Table:           computeFGAOutboxTable,
			PartitionColumn: reconciler.RegisterOutboxPartition,
		},
		retention.DefaultConfig(),
		logger.With(slog.String("component", "queue_retention_sweep")),
	); err != nil {
		return fmt.Errorf("фоновая уборка доставленных строк очереди: %w", err)
	}

	// Фоновая уборка РЕСУРСНОГО ЖУРНАЛА подписки.
	//
	// Строка в него пишется на КАЖДОЙ мутации ресурса владельца, то есть темп
	// задаёт арендатор, а снятия строк не было ни на одном пути: рост был
	// монотонным и вечным.
	//
	// Петля СВОЯ, а не запись в реестре уборки таблицы операций: пороги у двух
	// предметов выводятся из РАЗНЫХ читателей (оператор, разбирающий отказавшую
	// мутацию, против подписчика, возобновляющегося с позиции). Расписание при
	// этом одно и берётся из одного места — разошлись бы два литерала, а не два
	// вызова одной функции.
	//
	// Пул, а не одиночное соединение подписки: уборка — обычный оператор, ей
	// выделенная сессия не нужна, а сессия подписки занята `LISTEN`.
	if _, err := subscription.StartJournalRetentionSweep(
		ctx, pool, subscriptionjournal.Journal(),
		retention.DefaultConfig(),
		logger.With(slog.String("component", "journal_retention_sweep")),
	); err != nil {
		return fmt.Errorf("фоновая уборка ресурсного журнала: %w", err)
	}

	projectClient, geoZones, subnetPlacement, nicClient, storageClient, closers, err := dialPeers(cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	// Регион спрашивается у ТОГО ЖЕ владельца Geography, что и зона, — поэтому
	// вторая половина берётся у уже собранного носителя, а не собирается заново
	// вторым соединением. Носитель, её не несущий, — отказ в старте, а не тихая
	// деградация: группа с региональным якорем иначе заводилась бы против
	// региона, существование которого никто не подтвердил.
	geoRegions, ok := geoZones.(placementgroup.RegionRegistry)
	if !ok {
		return fmt.Errorf("носитель Geography не отвечает на вопрос о регионе: " +
			"группа размещения с региональным якорем не может быть проверена")
	}

	// Fail-closed boot-gate: when KACHO_COMPUTE_REQUIRE_IAM=true, mutating Create is
	// refused and readiness is NotReady until the register-drainer is IAM-connected.
	// Starts NOT connected; SetConnected(true) fires once the drainer dial succeeds.
	bootGate := bootgate.New(bootgate.Config{RequireIAM: cfg.RequireIAM, Service: "kacho-compute"})

	// Prometheus observability adapter: приватный реестр, питает outbox-recorder,
	// LRO-worker/reconciler recorder и diagnostic /metrics. Заменяет in-memory
	// MemRecorder (метрики не экспортировались) и NopRecorder LRO-worker'а.
	metricsAdapter := computemetrics.New(buildVersion, buildCommit)
	var outboxRec metrics.Recorder = metricsAdapter
	var lroRec operations.Recorder = metricsAdapter

	// ── соединение с моделью прав ─────────────────────────────────────────────
	// Одно на два предмета: пообъектное сужение видимости и синхронная регистрация
	// владельца ресурса. ТРЕТИЙ предмет — вопрос о правах — с этого соединения
	// снят: его задаёт носитель по ребру, объявленному дескриптором, и транспорт
	// того же адреса он собирает у себя (см. describe.go). Собственного звена
	// решения о доступе у корня больше нет.
	var authzConn *grpc.ClientConn
	if cfg.AuthZIAMGRPCAddr != "" {
		// authz-conn → iam-internal:9091 — idle-prone (между всплесками активных
		// стримов нет) → idle=true: пинги держат conn тёплым.
		//
		// Предъявляет client-cert mTLS через cfg.IAMAuthzMTLS (enable=false →
		// insecure dev; enable=true без валидного cert-trio → startup error,
		// fail-closed).
		authzCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
		if cerr != nil {
			return fmt.Errorf("compute→iam list-filter/register mTLS creds: %w", cerr)
		}
		authzConn, err = dialPeerCreds(cfg.AuthZIAMGRPCAddr, authzCreds, true)
		if err != nil {
			return fmt.Errorf("dial kacho-iam (authz): %w", err)
		}
		defer authzConn.Close()
		logger.Info("compute→iam read/authz mTLS state",
			"project_get_mtls", cfg.IAMProjectMTLS.Enable,
			"authz_check_listfilter_mtls", cfg.IAMAuthzMTLS.Enable,
		)
	}

	// Резолв величин квоты идёт на ВНУТРЕННИЙ слушатель kacho-iam — по тому же
	// адресу, что уже объявлен оператором для проверки прав.
	//
	// Это НЕ вывод адреса из чужого: адрес внутреннего контура объявлен
	// оператором явно, и здесь он переиспользуется. Своей ручки у резолва нет
	// именно потому, что второй адрес того же слушателя разошёлся бы с первым
	// молча — и контроль ушёл бы в никуда, выглядя включённым.
	//
	// Пустой адрес (dev без соседа) → полоса не собирается. Это означает
	// «раннего отказа и материализации нет», а НЕ «предела нет»: место занимает
	// триггер, и при отсутствии строк учёта он отвергает вставку «потолок не
	// назван». На любом поднятом стенде адрес обязан быть задан — иначе домен
	// перестаёт принимать создание, и это видно с первой же мутации, а не тихо.
	var quotaLimits quota.LimitResolver
	if authzConn != nil {
		limitClient := clients.NewLimitClient(authzConn)
		quotaLimits = limitClient
		logger.Info("resource-count quota: limits resolver wired",
			"endpoint", cfg.AuthZIAMGRPCAddr, "service", "compute")

		// Снимок величины обязан ДОГОНЯТЬ авторитет: без тянущего строка,
		// заведённая один раз, живёт со своей величиной вечно, и смена предела
		// администратором не доезжает до проекта никогда. Показывать арендатору
		// такой снимок значило бы громко назвать число, которое не догонит
		// назначенное, — поэтому чтение и тянущий едут вместе.
		stopQuotaSync, qerr := corequota.StartLimitSyncer(
			ctx, pool, limitClient, repo.QuotaSchema, corequota.Config{}, logger)
		if qerr != nil {
			return fmt.Errorf("start quota limit syncer: %w", qerr)
		}
		defer stopQuotaSync()
	} else {
		logger.Warn("resource-count quota: no internal kacho-iam endpoint, limits resolver is OFF " +
			"and the limit snapshot will NEVER catch up with the authority. " +
			"The charging trigger still enforces, so creates are refused with " +
			"\"no ceiling stated\" until the endpoint is configured, and an administrator " +
			"raising or lowering a ceiling has no effect on this process")
	}

	// Сборка use-case'ов идёт ПОСЛЕ объявления резолва величин: полоса учёта —
	// их зависимость, а её источник — соединение внутреннего контура выше.
	svcs := buildServices(pool, projectClient, quotaLimits, geoZones, geoRegions, subnetPlacement, nicClient, storageClient, opsRepo)

	// Пообъектный сужатель: он же уезжает ПРОВОДКОЙ в дескриптор, поэтому строится
	// ДО него и ТЕМ ЖЕ объектом, что сужает строки в обработчиках. Собери его
	// второй раз — носитель сверял бы с каталогом экземпляр, которого на пути
	// запроса нет.
	listFilter := buildListFilter(cfg, authzConn, logger)

	// Общий сервер потока изменений. Строится ДО подъёма слушателей: его сборка
	// умеет отказать (негодное объявление журнала, невыбранная величина посадки,
	// неработающий сужатель), а отказ обязан случиться раньше первого принятого
	// соединения, а не первым запросом в бою.
	//
	// Сужатель — ТОТ ЖЕ объект, что сужает строки в обработчиках списков: за этим
	// методом нет пообъектной проверки на крае (он `scope_filtered`), поэтому
	// откатываться не на что, и второй экземпляр сужателя означал бы, что поток
	// сужается не тем, чем сужаются списки.
	subscribeSrv, err := buildSubscriptionServer(cfg, listFilter, logger)
	if err != nil {
		return err
	}
	// Величины сужателя выходят из процесса ТОЛЬКО здесь. Полос четыре: одна
	// положительная и три — страница, ушедшая БЕЗ пообъектной проверки. Снимите
	// эту строку — и полосы исчезнут с поверхности, а не станут нулями; ровно это
	// ловит гейт дерева `TestEveryListNarrowConsumerRegistersItsCollector`.
	var authzCache authzmetrics.Source
	metricsAdapter.RegisterListNarrow(func() listnarrow.Counts { return listFilter.Counts() })
	// Доля попаданий кеша положительных вердиктов. Источник устанавливается ПОЗЖЕ
	// — кеш строит носитель контура, — поэтому коллектор регистрируется сейчас и
	// до установки отвечает нулями: исчезновение серий на это окно сообщило бы
	// собирателю не «попаданий не было», а ничего.
	//
	// ПОЛОС ДВЕ, потому что окон положительных вердиктов у этого процесса два:
	// окно звена решения (вопрос на ВЫЗОВ) и окно общего сужателя (вопрос на
	// КАЖДЫЙ элемент страницы, а страница контрактно бывает до тысячи). Через
	// второе проходит БОЛЬШЕ вопросов, чем через первое, и прежде его не считал
	// никто: «кеш сужателя даёт столько-то» было непроверяемо в обе стороны.
	metricsAdapter.RegisterAuthzCache(map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    authzCache.Cache,
		authzmetrics.LaneNarrow: listFilter.CacheStats,
	}, authzCache.Read)

	// ── объявление о себе ─────────────────────────────────────────────────────
	//
	// Дескриптор собирается ПОСЛЕ пула и ДО фоновых проходов. После пула — потому
	// что порт сверки существования живёт НА пуле, и принести его раньше значило бы
	// принести порт, отвечающий «соединения нет». Открытие пула обратимо (`defer`
	// выше) и дешевле ложной сверки.
	desc, err := describe(cfg, logger, listFilter, bootGate, repo.NewExistenceProbe(pool), authzCache.Install, metricsAdapter.Registerer())
	if err != nil {
		return err
	}

	// Самоотчёт о посадке — ПОСЛЕ приёма дескриптора (то есть после всех отказов
	// старта, являющихся свойствами объявленного) и ДО подъёма слушателей. Гейт
	// посадки обязан утверждать на этом наблюдаемом факте, а не на хранимом
	// конфиге: правка настроек без переката пода оставляет процесс с прежним
	// окружением, и «под Ready» доказательством посадки не является.
	observability.LogBootPosture(logger, bootPosture(cfg))

	// background — фоновые loop'ы под супервизором (errgroup): неожиданный exit
	// флипает readiness в shutting-down и триггерит graceful-shutdown (не
	// fire-and-forget). Заполняется ниже (LRO-reconciler, register-drainer,
	// outbox-backstop), запускается в supervised errgroup перед Serve.
	type bgWorker struct {
		name string
		run  func(context.Context) error
	}
	var background []bgWorker

	// LRO worker (default-registry) поднимается ДО приёма трафика: ConfigureDefault
	// подключает Prometheus-Recorder (live terminal-write/inflight метрики — раньше
	// NopRecorder), Start делает Ready()=true без единой мутации (нет
	// readiness-deadlock «NotReady → нет Run → worker не стартует»).
	if err := startLROWorker(lroRec, logger); err != nil {
		return fmt.Errorf("start LRO worker: %w", err)
	}

	// Durable LRO recovery: доменный resolver + corelib-reconciler поверх schema
	// public. RecoverAll прогоняется ДО приёма трафика (осиротевшие операции
	// умершего worker'а — backlog-overflow, terminal-write retry exhausted,
	// shutdown, crash mid-op — разрешаются в терминал); периодический Run — backstop
	// под супервизором.
	lroReaders := operationresolver.Readers{
		Instance: repo.NewInstanceRepo(pool),
	}
	lroReconciler := startLRORecovery(ctx, pool, lroReaders, lroRec, logger)
	background = append(background, bgWorker{"lro-reconciler", func(c context.Context) error {
		lroReconciler.Run(c)
		return nil
	}})

	// Добиватель начатых удалений. Разрешитель выше по контракту рабочую функцию
	// НЕ перезапускает — он лишь приводит статус операции к закоммиченной
	// реальности. Значит удаление, прерванное крахом, доводить было некому:
	// машина оставалась в DELETING навсегда, удерживая интерфейсы и тома у
	// владельцев, которые снятия не запрашивают. См. stuck_delete_finisher.go.
	background = append(background, bgWorker{"stuck-delete-finisher", func(c context.Context) error {
		runStuckDeleteFinisher(c, svcs.instance, logger)
		return nil
	}})

	// Сканер состояния ЖУРНАЛА АУДИТА. Провязан безусловно и намеренно: журнал
	// наполняется каждой мутацией репозитория, независимо от того, включён ли
	// дренаж очереди регистраций ниже. У журнала теперь есть доставка (вывоз
	// следующей задачей), поэтому величины сканера читаются как обычно: глубина
	// падает, возраст головы ограничен сверху.
	background = append(background, bgWorker{"audit-outbox-metrics", func(c context.Context) error {
		runAuditOutboxMetrics(c, pool, outboxRec, logger)
		return nil
	}})

	// Вывоз журнала аудита в приёмник. Строится ДО запуска задач: ошибка сборки
	// обязана останавливать старт, а не всплывать фоном — журнал, который не
	// вывозится, снаружи неотличим от журнала, в котором нечего вывозить.
	auditShipper, ashErr := buildAuditShipper(pool, outboxRec, logger)
	if ashErr != nil {
		return fmt.Errorf("audit shipper wiring: %w", ashErr)
	}
	background = append(background, bgWorker{"audit-shipper", auditShipper.Run})

	// register-drainer — applies FGA owner-tuple register/unregister intents
	// (compute_fga_register_outbox, written transactionally by repo.Insert/Delete)
	// via kacho-iam InternalIAMService.RegisterResource/UnregisterResource over the
	// (optionally mTLS) compute→iam edge. Idempotent + retry-on-Unavailable; the
	// owner-tuple is never lost. Default-on; without it created resources get no
	// per-resource FGA tuple. Drainer Run-loop + outbox backstop (reconciler +
	// metrics collector) идут под супервизором, а не fire-and-forget.
	if cfg.FGARegisterDrainerEnabled {
		drainRun, drainCloser, derr := startRegisterDrainer(cfg, pool, outboxRec, logger)
		if derr != nil {
			return fmt.Errorf("start register-drainer: %w", derr)
		}
		defer drainCloser()
		background = append(background, bgWorker{"fga-register-drainer", drainRun})
		// Drainer dial established → IAM-register delivery path is up: open the
		// boot-gate + start the reconciler/metrics backstop.
		bootGate.SetConnected(true)
		reconRun, colRun, berr := startBackstop(ctx, pool, outboxRec, logger)
		if berr != nil {
			return fmt.Errorf("start outbox backstop: %w", berr)
		}
		background = append(background,
			bgWorker{"fga-register-reconciler", reconRun},
			bgWorker{"outbox-metrics-collector", colRun},
		)
	} else {
		logger.Warn("FGA register-drainer DISABLED (KACHO_COMPUTE_FGA_REGISTER_DRAINER_ENABLED=false) — " +
			"created resources will not get their per-resource FGA owner-tuple registered in IAM")
	}

	// sync-registrar owner-tuple (window-оптимизация): немедленная post-commit
	// регистрация owner-tuple Instance через InternalIAMService.RegisterResource,
	// сужающая eventual-consistency-окно до poll'а register-drainer'а. register-drainer
	// остаётся at-least-once backstop'ом. Активен только когда authzConn сконфигурирован
	// (production/authz-on) и drainer включён; иначе — только drainer.
	if authzConn != nil && cfg.FGARegisterDrainerEnabled {
		reg, closeReg, rerr := buildSyncRegistrar(cfg, logger)
		if rerr != nil {
			return fmt.Errorf("build owner-tuple sync registrar: %w", rerr)
		}
		defer closeReg()
		svcs.instance.WithOwnerRegistrar(reg)
		svcs.guestAccessKey.WithOwnerRegistrar(reg)
		svcs.placementGroup.WithOwnerRegistrar(reg)
		logger.Info("owner-tuple sync-registrar enabled (Instance/GuestAccessKey Create)")
	}

	// Dependency-aware readiness: /readyz отражает здоровье критичных зависимостей
	// (database / register-drainer / lro-worker / iam-authz), /healthz — только
	// живость процесса (защита от restart-storm). Результат зеркалится в
	// dependency_up Prometheus-gauge.
	healthAgg := health.New(
		buildReadinessCheckers(pool, bootGate, authzConn),
		health.WithResultObserver(metricsAdapter.SetDependencyUp),
	)
	// Диагностическая поверхность (cluster-internal): /metrics + /healthz + /readyz.
	//
	// Входит в контур ОТДЕЛЬНЫМ ПРОФИЛЕМ той же функции (решение владельца XC-7,
	// в-1): не gRPC, цепочка другая, полями общего дескриптора её не втягивают.
	// Корень приносит ОБЪЯВЛЕНИЕ; подъём, самоотчёт и гашение принадлежат профилю.
	diagDesc, err := describeDiagnosticSurface(cfg.MetricsAddr, metricsAdapter, healthAgg,
		desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	// Собственный контекст поверхности: она гасится ПОСЛЕДНЕЙ — после обоих
	// gRPC-слушателей и после дренажа исполнителей операций, — чтобы переброс
	// /readyz в 503 успел отработать до закрытия порта.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	defer stopDiag()

	// Отдельный контекст слушателей. Носитель гасит оба слушателя по отмене СВОЕГО
	// контекста, поэтому флип готовности в shutting_down обязан произойти РАНЬШЕ
	// этой отмены, а не одновременно с ней: kubelet перестаёт слать трафик до того,
	// как соединения начнут закрываться. Одним контекстом на всё этот порядок был бы
	// неразличим.
	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()

	// Единый shutdown-триггер (sync.Once): флипает readiness в shutting_down (kubelet
	// перестаёт слать трафик ДО гашения слушателей), отменяет ctx (фоновые loop'ы
	// выходят), затем просит носитель погасить слушатели. Вызывается из
	// shutdown-waiter (SIGTERM), из краха любого supervised-task'а и из
	// superviseBackground при неожиданном exit'е.
	var shutdownOnce sync.Once
	shutdownCh := make(chan struct{})
	triggerShutdown := func() {
		shutdownOnce.Do(func() {
			healthAgg.SetShuttingDown()
			close(shutdownCh)
			cancel()
			stopServe()
		})
	}

	var g errgroup.Group
	// Фоновые loop'ы под супервизором: неожиданный exit (ctx ещё жив) флипает
	// readiness и триггерит shutdown; штатный возврат после ctx-cancel → nil.
	for _, bg := range background {
		g.Go(func() error {
			return superviseBackground(ctx, bg.name, bg.run, triggerShutdown, logger)
		})
	}
	// Привязка порта — ЗДЕСЬ, до постановки задачи: занятый адрес есть ошибка
	// посадки, и узнать о ней надо до того, как процесс объявит себя поднявшимся.
	// Прежде подъём целиком уезжал в задачу супервизора, и отказ привязки
	// становился кодом возврата процесса, успевшего сколько угодно проработать.
	//
	// Ожидание ставится задачей ВСЕГДА, даже когда поверхность объявлена
	// выключенной: тогда оно сразу возвращается, а причина уже названа в журнале.
	// Условная постановка вернула бы то самое молчание, ради устранения которого
	// выключение стало объявлением.
	waitDiag, diagErr := servicehost.ServeSurface(diagCtx, diagDesc)
	if diagErr != nil {
		stopDiag()
		return fmt.Errorf("диагностическая поверхность: %w", diagErr)
	}
	g.Go(func() error {
		if derr := waitDiag(); derr != nil {
			logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
			triggerShutdown()
			return fmt.Errorf("диагностическая поверхность: %w", derr)
		}
		return nil
	})
	// ОБА gRPC-слушателя — носитель контура. Он поднимает их с ОДНОЙ парой цепочек,
	// прогоняет отказы старта, которым нужен служимый набор RPC, и обслуживает до
	// отмены serveCtx. Исход внутреннего слушателя учитывается наравне с публичным —
	// это его свойство, а не наше.
	//
	// Регистраторы оборачиваются рубежом СВОЕГО слушателя (`internal/handler`):
	// `x-kacho-admin` и `x-kacho-project-id` читаются только на внутреннем, и это
	// свойство ПОСТРОЕНИЯ — публичный конструктор параметра «слушатель» не имеет.
	// Разбор, почему рубеж живёт там, а не осью дескриптора, — в шапке
	// `internal/handler/listener_scope.go`.
	g.Go(func() error {
		serr := servicehost.Serve(serveCtx, desc,
			func(reg grpc.ServiceRegistrar) {
				registerPublicServices(handler.PublicRegistrar(reg, productionMode), svcs, opsRepo, listFilter)
			},
			func(reg grpc.ServiceRegistrar) {
				registerInternalServices(handler.InternalRegistrar(reg, productionMode), svcs, subscribeSrv)
			},
		)
		if serr != nil {
			logger.Error("grpc listeners stopped", "err", serr)
			triggerShutdown()
			return fmt.Errorf("grpc: %w", serr)
		}
		triggerShutdown()
		return nil
	})
	// shutdown-waiter: SIGTERM/SIGINT (ctx) ИЛИ краш любого task'а (shutdownCh) →
	// triggerShutdown → дрейн LRO worker'ов → гашение diagnostic-listener'а последним
	// (probe-flip /readyz→503 успевает отработать до закрытия порта).
	g.Go(func() error {
		select {
		case <-ctx.Done():
		case <-shutdownCh:
		}
		triggerShutdown()
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		if werr := operations.Wait(drainCtx); werr != nil {
			logger.Warn("operations workers did not finish in time", "err", werr, "active", operations.Active())
		}
		// Гашение поверхности — последним действием остановки. Её возврата ждёт
		// сам errgroup: профиль возвращается только после того, как порт освобождён.
		stopDiag()
		return nil
	})

	return g.Wait()
}

// validateAuthMode разбирает KACHO_COMPUTE_AUTH_MODE (whitelist), для
// production-strict валидирует cross-service TLS + DB sslmode, логирует insecure
// dev-defaults. Зеркалит kacho-vpc/cmd/vpc/main.go::validateAuthMode.
func validateAuthMode(cfg config.Config, logger *slog.Logger) (productionMode bool, err error) {
	// Breakglass в production — ОТКАЗ СТАРТА, и проверяется ПЕРВЫМ, чтобы причина
	// не тонула в жалобах на другие рёбра (breakglass как раз их и снимал).
	//
	// Раньше здесь был WARN «зеркалим kacho-vpc warn-not-reject», тогда как geo и nlb
	// отвергали: одна и та же настройка означала «сервис поднят вообще без
	// авторизации» в одних сервисах и «сервис не поднимется» в других. WARN не
	// защищает — leftover breakglass после инцидента переживает рестарт и оставляет
	// ОБА листенера без per-RPC InternalIAMService.Check (object-self
	// v_get/v_update/v_delete и cross-tenant Check не оцениваются, остаётся только
	// AuthN). Это прямое нарушение security.md «AuthN+AuthZ ВЕЗДЕ» и kacho core rule «production-mode обязателен ВЕЗДЕ»
	// («production-mode boot-guard fail-closed → refuse-to-start»). В dev аварийный
	// обход остаётся доступным — там он и задуман.
	if cfg.AuthZBreakglass && cfg.Posture().IsProduction() {
		return false, fmt.Errorf("production mode (%s): KACHO_COMPUTE_AUTHZ_BREAKGLASS must not be enabled "+
			"— it bypasses ALL per-RPC authz Check on both listeners (every RPC allowed without IAM "+
			"authorization); breakglass is a non-production emergency escape only", cfg.AuthMode)
	}
	// Тот же класс, что breakglass выше, и такой же отказ старта: SkipPeerValidation
	// снимает НЕ одну проверку, а ВСЕ кросс-сервисные разом. dialPeers при нём
	// подменяет каждый peer-клиент no-op-заглушкой, для которой любой чужой
	// идентификатор «существует», — отваливаются существование проекта, существование
	// зоны, когерентность размещения подсети интерфейса и высвобождение интерфейса и
	// тома на удалении. Это аварийный dev-выключатель; на развёрнутом стенде он
	// означает, что ресурсы создаются со ссылками в никуда и с межзональными
	// интерфейсами, а высвобождение чужих ресурсов просто не происходит.
	//
	// Проверяется ДО разбора режима, рядом с breakglass, по той же причине: иначе
	// причина утонула бы в жалобах на рёбра, требование mTLS с которых этот же
	// выключатель и снимает (см. insecureEdgesInProductionStrict).
	if cfg.SkipPeerValidation && cfg.Posture().IsProduction() {
		return false, fmt.Errorf("production mode (%s): KACHO_COMPUTE_SKIP_PEER_VALIDATION must not be enabled "+
			"— it disables EVERY cross-service check at once (project existence, zone existence, NIC subnet "+
			"placement coherence, NIC/volume release on Delete): peers are replaced by no-op stubs for which "+
			"any foreign id \"exists\"; skipping peer validation is a non-production escape only", cfg.AuthMode)
	}

	// Словарь допустимых значений — НЕ свой: он объявлен в дереве один раз
	// (`servicecontract.Modes`), и отказ ниже перечисляет ТОТ ЖЕ набор, что у
	// остальных шести стражей старта. Свой словарь здесь был, и он был одним из
	// пяти; копии не собираются вместе и друг друга не читают, поэтому расхождение
	// приходило молча.
	//
	// Разбор ЗДЕСЬ, а свич — по ЗНАЧЕНИЮ: ветки посадки перестают быть вторым
	// перечислением словаря, и появление четвёртой посадки заметит компилятор, а
	// не оператор на выкатке.
	mode, merr := servicecontract.ParseMode(cfg.AuthMode)
	if merr != nil {
		return false, fmt.Errorf("KACHO_COMPUTE_AUTH_MODE: %w", merr)
	}
	switch mode {
	case servicecontract.ModeDev:
		productionMode = false
	case servicecontract.ModeProduction:
		productionMode = true
		// Fail-closed listener gate: оба server-листенера (public :9090 / internal
		// :9091) принимают forwarded x-kacho-principal-* и доверяют ему на
		// plaintext-транспорте (principalIsTrusted: "insecure listener → principal
		// accepted"). Без server-mTLS любой, кто дозвонился на порт в обход
		// api-gateway, форжит x-kacho-principal-id: usr_<victim> и проходит FGA-Check
		// как жертва (CWE-290 subject spoofing → tenant crossing). Поэтому production
		// ОТКАЗЫВАЕТСЯ стартовать с plaintext-листенерами (раньше это гейтилось только
		// в production-strict). Прочие peer-рёбра (project/geo/vpc/storage/register)
		// остаются послаблением plain production (mesh-encrypted) — их строгий mTLS
		// требует production-strict. Ребро проверки прав из этого послабления
		// ВЫВЕДЕНО, см. requireAuthzEdgeTransport ниже.
		if terr := insecureListenersInProduction(cfg); terr != nil {
			return false, terr
		}
		if terr := requireAuthzEdgeTransport(cfg); terr != nil {
			return false, terr
		}
		if terr := requireDBSSLMode(cfg); terr != nil {
			return false, terr
		}
		if terr := requireListFilter(cfg); terr != nil {
			return false, terr
		}
		logger.Warn("AuthMode=production: anonymous rejected + server-mTLS listeners + SSL DB + forwarder allow-list + per-object List-filter required")
	case servicecontract.ModeProductionStrict:
		productionMode = true
		// TLS-check on the actually-dialed transport edges (per-edge mTLS value-
		// structs). The former server-auth-only IAM/AUTHZ bool knobs wired no live
		// dial, so gating on them gave false assurance while every cross-service edge
		// + both listeners could still run plaintext — they have been removed.
		if terr := insecureEdgesInProductionStrict(cfg); terr != nil {
			return false, terr
		}
		if terr := requireDBSSLMode(cfg); terr != nil {
			return false, terr
		}
		if terr := requireListFilter(cfg); terr != nil {
			return false, terr
		}
		logger.Warn("AuthMode=production-strict: anonymous rejected + per-edge mTLS+SSL + forwarder allow-list + per-object List-filter strictly validated")
	}
	if !productionMode {
		if terr := insecureEdgesInProductionStrict(cfg); terr != nil {
			logger.Warn("insecure cross-service/listener transport (dev only)", "detail", terr.Error())
		}
		if cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable" {
			logger.Warn("KACHO_COMPUTE_DB_SSLMODE=disable — DB plaintext (dev only)")
		}
	}
	// breakglass в production сюда не доходит — отвергнут в самом начале функции.
	return productionMode, nil
}

// requireAuthzEdgeTransport — транспорт ребра, несущего РЕШЕНИЕ о доступе, обязан
// быть verified в ЛЮБОМ боевом режиме, а не только в строгом.
//
// Ребро compute→iam (per-RPC InternalIAMService.Check) несёт и вердикт о доступе,
// и переданную личность вызывающего. Невзведённая ручка не даёт ошибки сама по
// себе: grpcclient.TLSClientTransportCreds на Enable=false возвращает
// insecure-creds БЕЗ ошибки — процесс поднимается, отчитывается «authz enabled», и
// каждый Check уходит по открытому каналу.
//
// Прежде требование стояло только в production-strict, и это было записанным
// послаблением. Оно снято: обычный production не является более слабой посадкой —
// это та же посадка, а страж, не срабатывающий в ней, есть контроль, чья ветка не
// исполнялась ни разу за свою жизнь.
//
// Страж читает ТЕ ЖЕ предикаты, что и проводка: соединение поднимается ровно
// когда задан адрес (main.go: `if cfg.AuthZIAMGRPCAddr != ""`), а аварийный режим
// снимает проверку целиком. Поэтому «страж увидел ребро» ⟺ «ребро дилится» — по
// построению, а не по совпадению двух одинаково написанных условий.
func requireAuthzEdgeTransport(cfg config.Config) error {
	if cfg.AuthZIAMGRPCAddr == "" || cfg.AuthZBreakglass {
		return nil
	}
	if cfg.IAMAuthzMTLS.Enable {
		return nil
	}
	return fmt.Errorf("production mode: verified transport required on the compute→iam authz Check edge " +
		"— set KACHO_COMPUTE_IAM_AUTHZ_MTLS_ENABLE=true (with cert/key/CA). Without it the per-RPC " +
		"authorization Check and the forwarded end-user principal travel over cleartext gRPC: the client " +
		"credentials silently degrade to insecure, so the process starts and reports authz as enabled")
}

// requireDBSSLMode — DB-канал в любом production-режиме обязан быть TLS.
// sslmode=disable гонит KACHO_COMPUTE_DB_PASSWORD и все данные строк открытым
// текстом по сети (CWE-319) — допустимо только в dev.
//
// Перечень безопасных значений — НЕ свой: он приходит из дома семантики строки
// подключения (`pkg/db`), где объявлен один раз на всё дерево. Судится ИСХОД — режим той строки, что уходит в пул: у compute он
// деривится из ручки (`baseDSN` подставляет `disable` на пустой), поэтому
// сегодня совпадает с ней, но спрашивать надо строку. Страж, читающий ручку,
// расходится с пулом молча при первом же изменении сборки DSN — так и вышло у
// двух соседних сервисов, где режим приходит ещё и из сырого URL.
func requireDBSSLMode(cfg config.Config) error {
	if mode := coredb.SSLModeFromDSN(cfg.DSN()); coredb.SSLModeSecure(mode) {
		return nil
	}
	return fmt.Errorf("production mode: KACHO_COMPUTE_DB_SSLMODE must be one of %s (got %q)",
		strings.Join(coredb.SecureSSLModes(), "|"), cfg.DBSSLMode)
}

// requireListFilter — в любом production-режиме per-object FGA-фильтр обязан быть
// активен.
//
// Предмет стражи ШИРЕ публичных List'ов, хотя имя историческое. Под ней все RPC,
// помеченные `ScopeFiltered` в `internal/check/permission_map.go`, — то есть те, у
// которых per-RPC Check снят и сужение живёт на уровне данных. Для List'а страж —
// вторая линия: под ним остаётся Check на проектном ярусе, поэтому выключение
// даёт over-show, но не полный обход.
//
// Прежде здесь стоял случай, где второй линии НЕТ по построению, — поток журнала
// изменений: его запрос не называл ни одного ресурса, единого объекта для одного
// вопроса не существовало, и выключенный фильтр означал бы отсутствие авторизации
// вовсе. Поток снят, поэтому у стражи сегодня остаются только List'ы, и
// сказанное про «полного обхода не даёт» относится ко всем её подопечным.
// Совпадение вердикта стражи и способности подопечных открыться проверяется в обе
// стороны — `scope_filtered_boot
//
// Условия: master-switch включён (ListFilterEnabled=true), задан
// authz-endpoint (AuthZIAMGRPCAddr непуст — иначе authzConn=nil → buildListFilter
// вернёт nil → handler'ы делают bypass фильтра) И фильтр отказывает fail-closed
// (ListFilterFailOpen=false). Per-RPC FGA Check гейтит List лишь на project-tier
// `viewer`; сужение страницы до per-object `viewer ∪ v_list` делает ТОЛЬКО этот
// фильтр. С выключенным фильтром любой principal с project-tier viewer видит ВСЕ
// Instance проекта (блочное хранение ушло из compute миграцией 0021 — Disk/Image/
// Snapshot тут больше не значатся), включая объекты без per-object гранта
// (over-show / BOLA-lite, CWE-862 / OWASP A01). Fail-closed зеркалит requireDBSSLMode /
// requireDBSSLMode (project-rule security.md → make audit-list-filter).
//
// Ручка отказа — часть предмета стражи, а не соседняя настройка. Стража, знающая
// только про наличие фильтра, охраняет его присутствие и не охраняет его
// поведение: с ListFilterFailOpen=true фильтр построен и включён, но на ЛЮБОЙ
// ошибке обращения к iam отдаёт страницу НЕотфильтрованной (authzfilter.handleErr).
// Тогда недоступности соседа достаточно, чтобы получить ровно тот over-show, от
// которого фильтр и защищает, — причём молча, одним WARN'ом в лог. Аварийный
// degraded-режим остаётся доступен в dev; в production он не живёт, симметрично
// AuthZBreakglass.
func requireListFilter(cfg config.Config) error {
	if !cfg.ListFilterEnabled {
		return fmt.Errorf("production mode requires KACHO_COMPUTE_LIST_FILTER_ENABLED=true " +
			"(false → public List bypasses the per-object FGA allow-list; project-tier viewer sees every resource → BOLA-lite)")
	}
	if cfg.AuthZIAMGRPCAddr == "" {
		return fmt.Errorf("production mode requires KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR (list-filter authorize endpoint) to be set " +
			"(empty → authzConn nil → public List bypasses the per-object FGA filter)")
	}
	if cfg.ListFilterFailOpen {
		return fmt.Errorf("production mode requires KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN=false " +
			"(true → any authz error returns the page UNFILTERED, so an unreachable peer alone bypasses the " +
			"per-object allow-list this gate exists to enforce); the degraded mode is a non-production escape only")
	}
	return nil
}

// insecureListenersInProduction — non-nil ошибка, если хотя бы один из двух
// server-листенеров запущен без mTLS. Оба принимают forwarded principal-identity;
// plaintext на любом даёт subject-spoofing (см. вызов в боевой ветке свича). Это
// подмножество insecureEdgesInProductionStrict (только листенеры, без peer-рёбер).
func insecureListenersInProduction(cfg config.Config) error {
	var insecure []string
	if !cfg.PublicServerMTLS.Enable {
		insecure = append(insecure, "PUBLIC_SERVER_MTLS_ENABLE")
	}
	if !cfg.InternalServerMTLS.Enable {
		insecure = append(insecure, "INTERNAL_SERVER_MTLS_ENABLE")
	}
	if len(insecure) == 0 {
		return nil
	}
	return fmt.Errorf("production mode requires server-mTLS on both listeners (forwarded principal is trusted on plaintext → subject spoofing); insecure (Enable=false): %s", strings.Join(insecure, ", "))
}

// insecureEdgesInProductionStrict возвращает non-nil ошибку, перечисляющую
// КАЖДОЕ реально дозваниваемое transport-ребро, у которого per-edge mTLS
// выключен. nil ⇒ все провода защищены. Гейт production-strict строится на этом
// (а не на удалённых server-auth-only bool-флагах): каждое перечисленное ребро несёт forwarded
// x-kacho-principal-* identity и/или DB/registration-payload, поэтому plaintext
// на любом из них компрометирует authorization-subject на проводе (CWE-319).
//
//   - оба server-listener'а (PublicServerMTLS / InternalServerMTLS) — принимают
//     forwarded principal, всегда обязательны;
//   - project/geo/vpc-subnet peer-рёбра (IAMProjectMTLS / GeoMTLS / VPCMTLS) —
//     дозваниваются на request-path каждой мутации, кроме SkipPeerValidation;
//   - vpc NIC-attach (VPCNicMTLS) и storage volume-attach (StorageMTLS) —
//     internal :9091 сагами Create/Delete, активны при заданном адресе;
//   - authz Check-ребро (IAMAuthzMTLS) — per-RPC FGA-gate, активно кроме breakglass;
//   - register-drainer ребро (IAMRegisterMTLS) — реплеит FGA-registration в iam,
//     активно при FGARegisterDrainerEnabled.
//
// Заявление о полноте здесь — не риторика: оно проверяется механически. Гейт
// TestEdgeCensus_GuardCoversEveryDialedEdge переписывает по синтаксису main.go
// каждое место, где composition root резолвит TLS-креды, и требует, чтобы
// отключение любого такого ребра роняло эту функцию. Обратный гейт
// (TestEdgeCensus_GuardDemandsNothingUndialed) не даёт остаться требованию на
// провод, который сняли. Руками этот перечень поддерживать больше не нужно — и,
// что важнее, нельзя разойтись с кодом молча: два ребра (NIC-attach и
// volume-attach) уже выпадали из него именно так.
func insecureEdgesInProductionStrict(cfg config.Config) error {
	var insecure []string
	if !cfg.PublicServerMTLS.Enable {
		insecure = append(insecure, "PUBLIC_SERVER_MTLS_ENABLE")
	}
	if !cfg.InternalServerMTLS.Enable {
		insecure = append(insecure, "INTERNAL_SERVER_MTLS_ENABLE")
	}
	if !cfg.SkipPeerValidation {
		if !cfg.IAMProjectMTLS.Enable {
			insecure = append(insecure, "IAM_PROJECT_MTLS_ENABLE")
		}
		if !cfg.GeoMTLS.Enable {
			insecure = append(insecure, "GEO_MTLS_ENABLE")
		}
		// vpc SubnetService.Get — placement-валидация NIC-спеки на Instance.Create;
		// несёт forwarded principal, значит plaintext на нём компрометирует subject.
		if cfg.VPCGRPCAddr != "" && !cfg.VPCMTLS.Enable {
			insecure = append(insecure, "VPC_MTLS_ENABLE")
		}
		// vpc InternalNetworkInterfaceService — NIC-attach/release сага (:9091).
		// Несёт forwarded principal и привязку интерфейса к машине; провод живой
		// ровно тогда, когда задан адрес (иначе dialPeers ставит no-op-клиент).
		if cfg.VPCInternalGRPCAddr != "" && !cfg.VPCNicMTLS.Enable {
			insecure = append(insecure, "VPC_NIC_MTLS_ENABLE")
		}
		// storage InternalVolumeService — attach/detach тома (:9091). Payload
		// самоописывающийся (несёт project/instance/zone), поэтому plaintext на
		// нём — это ещё и подмена привязки тома, не только subject'а.
		if cfg.StorageInternalGRPCAddr != "" && !cfg.StorageMTLS.Enable {
			insecure = append(insecure, "STORAGE_MTLS_ENABLE")
		}
	}
	if !cfg.AuthZBreakglass && !cfg.IAMAuthzMTLS.Enable {
		insecure = append(insecure, "IAM_AUTHZ_MTLS_ENABLE")
	}
	if cfg.FGARegisterDrainerEnabled && !cfg.IAMRegisterMTLS.Enable {
		insecure = append(insecure, "IAM_REGISTER_MTLS_ENABLE")
	}
	if len(insecure) == 0 {
		return nil
	}
	return fmt.Errorf("production-strict mode requires per-edge mTLS on all live transport edges; insecure (Enable=false): %s", strings.Join(insecure, ", "))
}

// dialPeers открывает gRPC-клиенты к peer-сервисам (kacho-iam — public :9090 для
// project-existence-check; kacho-geo — public :9090 для zone_id-валидации Instance)
// либо возвращает no-op-заглушки при KACHO_COMPUTE_SKIP_PEER_VALIDATION=true.
//
// project-existence-check идёт в kacho-iam.ProjectService.Get.
//
// zone_id-валидация Instance идёт через geo.v1.ZoneService.Get (clients.GeoClient);
// Geography (Region/Zone) принадлежит kacho-geo — compute их больше не обслуживает,
// а лишь валидирует свой zone_id как consumer.
func dialPeers(cfg config.Config, logger *slog.Logger) (ports.ProjectAccountClient, instance.ZoneRegistry, instance.SubnetRegistry, instance.NicClient, instance.StorageClient, []*grpc.ClientConn, error) {
	if cfg.SkipPeerValidation {
		logger.Warn("KACHO_COMPUTE_SKIP_PEER_VALIDATION=true — cross-service existence-check disabled (dev/test only)")
		return clients.NoopProjectClient{}, clients.NoopGeoClient{}, clients.NoopSubnetClient{}, clients.NoopNicClient{}, clients.NoopStorageClient{}, nil, nil
	}
	// iam (public ProjectService.Get) — активно используется на request-path каждой
	// мутации → idle=false (трафик есть, idle-пинги не нужны; keepalive всё равно
	// ставится для half-open-detection при паузах).
	//
	// compute→iam ProjectService.Get (:9090) предъявляет client-cert mTLS
	// через cfg.IAMProjectMTLS (enable=false → insecure dev; enable=true без
	// валидного cert-trio → startup error, fail-closed). Заменяет удалённый
	// server-auth-only bool-флаг, который не предъявлял cert (был бы отвергнут iam с
	// required client-cert).
	iamCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMProjectMTLS)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("compute→iam ProjectService.Get mTLS creds: %w", err)
	}
	iamConn, err := dialPeerCreds(cfg.IAMGRPCAddr, iamCreds, false)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("dial iam: %w", err)
	}
	// compute→geo zone_id-валидация Instance (geo.v1.ZoneService.Get,
	// public :9090). Per-edge client-cert mTLS через cfg.GeoMTLS (enable=false →
	// insecure dev; enable=true без валидного cert-trio → startup error,
	// fail-closed) — паритет с compute→iam ребром.
	geoCreds, err := grpcclient.TLSClientTransportCreds(cfg.GeoMTLS)
	if err != nil {
		_ = iamConn.Close()
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("compute→geo ZoneService.Get mTLS creds: %w", err)
	}
	geoConn, err := dialPeerCreds(cfg.GeoGRPCAddr, geoCreds, false)
	if err != nil {
		_ = iamConn.Close()
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("dial geo: %w", err)
	}
	logger.Info("compute→geo resource-validation mTLS state",
		"geo_zone_validate_mtls", cfg.GeoMTLS.Enable,
	)

	conns := []*grpc.ClientConn{iamConn, geoConn}

	// compute→vpc InternalNetworkInterfaceService (NIC-attach saga, S4, :9091
	// internal). Пустой addr → NIC-ребро не сконфигурировано (NoopNicClient:
	// attach fail-closed Unavailable, зеркало опускается). Per-edge client-cert
	// mTLS через cfg.VPCNicMTLS (enable=false → insecure dev; enable=true без
	// валидного cert-trio → startup error, fail-closed) — паритет с geo/iam рёбрами.
	var nicClient instance.NicClient = clients.NoopNicClient{}
	if cfg.VPCInternalGRPCAddr != "" {
		vpcCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.VPCNicMTLS)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("compute→vpc InternalNetworkInterfaceService mTLS creds: %w", cerr)
		}
		vpcConn, cerr := dialPeerCreds(cfg.VPCInternalGRPCAddr, vpcCreds, false)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("dial vpc: %w", cerr)
		}
		conns = append(conns, vpcConn)
		nicClient = clients.NewVPCNicClient(vpcConn)
		logger.Info("compute→vpc NIC-attach mTLS state", "vpc_nic_attach_mtls", cfg.VPCNicMTLS.Enable)
	} else {
		logger.Warn("KACHO_COMPUTE_VPC_INTERNAL_GRPC_ADDR empty — NIC-attach edge disabled (NoopNicClient)")
	}

	// compute→vpc SubnetService.Get (публичный :9090) — peer-валидация placement'а
	// подсети NIC-спеки на Instance.Create: машина создаётся в своей зоне, и её
	// интерфейсы обязаны быть в той же зоне (REGIONAL/anycast подсеть исключена из
	// зональной проверки by construction). Читается под идентичностью вызывающего
	// (auth.PropagateOutgoing) — недоступная тенанту подсеть неотличима от
	// несуществующей. Пустой addr → ребро не сконфигурировано: nil-клиент, и
	// Create с NIC-спеками fail-closed Unavailable (coherence неверифицируема).
	var subnetClient instance.SubnetRegistry
	if cfg.VPCGRPCAddr != "" {
		subnetCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.VPCMTLS)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("compute→vpc SubnetService.Get mTLS creds: %w", cerr)
		}
		subnetConn, cerr := dialPeerCreds(cfg.VPCGRPCAddr, subnetCreds, false)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("dial vpc public: %w", cerr)
		}
		conns = append(conns, subnetConn)
		subnetClient = clients.NewVPCSubnetClient(subnetConn)
		logger.Info("compute→vpc subnet placement mTLS state", "vpc_subnet_mtls", cfg.VPCMTLS.Enable)
	} else {
		logger.Warn("KACHO_COMPUTE_VPC_GRPC_ADDR empty — NIC-spec placement coherence unverifiable (Create fails closed)")
	}

	// compute→storage InternalVolumeService (volume-attach saga, :9091 internal).
	// Пустой addr → storage-ребро не сконфигурировано (NoopStorageClient: attach
	// fail-closed Unavailable, зеркало опускается). Per-edge client-cert mTLS через
	// cfg.StorageMTLS (enable=false → insecure dev; enable=true без валидного cert-trio
	// → startup error, fail-closed) — паритет с vpc/geo/iam рёбрами.
	var storageClient instance.StorageClient = clients.NoopStorageClient{}
	if cfg.StorageInternalGRPCAddr != "" {
		storageCreds, cerr := grpcclient.TLSClientTransportCreds(cfg.StorageMTLS)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("compute→storage InternalVolumeService mTLS creds: %w", cerr)
		}
		storageConn, cerr := dialPeerCreds(cfg.StorageInternalGRPCAddr, storageCreds, false)
		if cerr != nil {
			for _, c := range conns {
				_ = c.Close()
			}
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("dial storage: %w", cerr)
		}
		conns = append(conns, storageConn)
		storageClient = clients.NewStorageClient(storageConn)
		logger.Info("compute→storage volume-attach mTLS state", "storage_volume_attach_mtls", cfg.StorageMTLS.Enable)
	} else {
		logger.Warn("KACHO_COMPUTE_STORAGE_INTERNAL_GRPC_ADDR empty — volume-attach edge disabled (NoopStorageClient)")
	}

	return clients.NewProjectClient(iamConn), clients.NewGeoClient(geoConn), subnetClient, nicClient, storageClient, conns, nil
}

// peerKeepalive — keepalive-параметры для peer-conn. idle=true для
// преимущественно-idle conn'ов (authz → iam-internal): PermitWithoutStream держит
// conn тёплым пингами без активных стримов, прямо лечит half-open-столл.
func peerKeepalive(idle bool) keepalive.ClientParameters {
	return grpcclient.KeepaliveParams(idle)
}

// peerDialOptsCreds — seam-функция (тестируемая): собирает []grpc.DialOption из
// готовых transport-creds + keepalive по idle. Единая точка для всех peer-dial'ов:
// per-edge client-cert mTLS iam/geo-рёбер резолвят creds через corelib
// grpcclient.TLSClientTransportCreds и подают их сюда. grpc.NewClient не отдаёт
// опции назад — тест инспектирует именно этот набор.
func peerDialOptsCreds(creds credentials.TransportCredentials, idle bool) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpcclient.KeepaliveDialOption(idle),
	}
}

// dialPeerCreds открывает gRPC-conn к peer-сервису, предъявляя готовые transport-
// creds (per-edge client-cert mTLS). Используется для compute→iam read/authz
// рёбер: creds резолвятся из cfg.IAMProjectMTLS / cfg.IAMAuthzMTLS через corelib
// grpcclient (enable=false → insecure, dev backward-compat).
func dialPeerCreds(addr string, creds credentials.TransportCredentials, idle bool) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, peerDialOptsCreds(creds, idle)...)
}

// buildServices создаёт все repo'ы поверх pool и собирает из них бизнес-сервисы.
//
// existence-check zone_id (Instance/Disk Create, Disk Relocate) идёт в kacho-geo
// через geoZones (instance.ZoneRegistry, реализован clients.GeoClient). compute
// больше НЕ обслуживает Region/Zone — Geography (Region/Zone) принадлежит
// kacho-geo; локальные таблицы `zones`/`regions` сняты миграцией
// 0011_drop_geography. Режим KACHO_COMPUTE_SKIP_PEER_VALIDATION учтён на уровне
// dialPeers (NoopProjectClient/NoopGeoClient — любой project/зона «существует»).
// storageClient — compute→kacho-storage InternalVolumeService edge (volume-attach
// saga). Wired here at the composition root; the Instance use-case consumes it in a
// follow-up cutover slice (attach-state moves from the local attached_disks table to
// storage). Threaded now so the peer-conn/config plumbing lands additively.
func buildServices(pool *pgxpool.Pool, projectClient ports.ProjectAccountClient, quotaLimits quota.LimitResolver, geoZones instance.ZoneRegistry, geoRegions placementgroup.RegionRegistry, subnets instance.SubnetRegistry, nicClient instance.NicClient, storageClient instance.StorageClient, opsRepo operations.Repo) *services {
	instanceRepo := repo.NewInstanceRepo(pool)
	machineTypeRepo := repo.NewMachineTypeRepo(pool)

	// Полоса учёта собирается ЗДЕСЬ, потому что здесь живёт пул: материализация
	// пишет строки учёта, а решение принимает триггер в той же транзакции, что
	// вставка. Зависимости, требующие соединений (резолв величин и аккаунт
	// проекта), приходят параметрами — их владелец composition root.
	//
	// nil-резолв означает «раннего отказа нет», а НЕ «предела нет»: разбор — у
	// объявления quotaLimits в runServe.
	var quotaGuard *quota.Guard
	if quotaLimits != nil {
		quotaGuard = quota.NewGuard(repo.NewQuotaRepo(pool), quotaLimits, projectClient, "compute")
	}

	return &services{
		// Чтение квот арендатором — та же полоса, что и ранний отказ: у чтения и
		// у полосы ровно два источника, и они одни и те же.
		quota:       quotaHandlerOrNil(quotaGuard),
		machineType: machinetype.NewMachineTypeService(machineTypeRepo, opsRepo),
		instance: instance.NewInstanceService(instanceRepo, machineTypeRepo, geoZones, subnets, projectClient, nicClient, storageClient, opsRepo).
			WithQuotaGuard(quotaGuard),
		guestAccessKey: guestaccesskey.NewService(
			repo.NewGuestAccessKeyRepo(pool), opsRepo, projectClient, nil).
			WithQuotaGuard(quotaGuard),
		realization:   realization.NewService(instanceRepo),
		nodeOwnership: nodeownership.NewService(instanceRepo),
		placementGroup: placementgroup.NewService(
			repo.NewPlacementGroupRepo(pool), opsRepo, projectClient, geoZones, geoRegions).
			WithQuotaGuard(quotaGuard),
	}
}

// registerPublicServices — публичные RPC + OperationService на внешний listener
// (:9090, проксируется api-gateway).
//
// List handlers получают listFilter (FGA filter); может быть nil — тогда
// FGA-фильтрация на List отключена (dev/breakglass). Catalog (DiskType) — public
// read, FGA bypass not needed (handler skips). Region/Zone serving снят —
// Geography принадлежит kacho-geo.
func registerPublicServices(srv grpc.ServiceRegistrar, svcs *services, opsRepo operations.Repo, listFilter *authzfilter.Narrower) {
	computev1.RegisterMachineTypeServiceServer(srv, handler.NewMachineTypeHandler(svcs.machineType))
	computev1.RegisterInstanceServiceServer(srv, handler.NewInstanceHandler(svcs.instance, listFilter))
	computev1.RegisterGuestAccessKeyServiceServer(srv, handler.NewGuestAccessKeyHandler(svcs.guestAccessKey, listFilter))
	computev1.RegisterPlacementGroupServiceServer(srv, handler.NewPlacementGroupHandler(svcs.placementGroup, listFilter))
	// Чтение квот выставляется, ТОЛЬКО когда полоса учёта собрана. Иначе метод
	// отвечал бы пустым набором на каждый запрос — то есть «квот нет», ровно то
	// утверждение, которое контракт запрещает делать (`ListQuotasResponse`:
	// пустой массив зарезервирован за состоянием, которого этот сервис не
	// сообщает). Незарегистрированный метод отвечает `Unimplemented`, и это
	// честно: возможности здесь действительно нет.
	if svcs.quota != nil {
		computev1.RegisterQuotaServiceServer(srv, svcs.quota)
	}
	operationpb.RegisterOperationServiceServer(srv, operationspb.NewHandler(opsRepo))
}

// buildListFilter собирает сужатель страницы (пообъектная видимость через iam
// `AuthorizeService.BatchCheck`) для публичных List и для потока журнала изменений.
//
// Выключенный фильтр БОЛЬШЕ НЕ ОЗНАЧАЕТ сквозной проход: сужатель собирается всегда
// и ОТКАЗЫВАЕТ, пока ему не с кем говорить. Пропуск возможен только объявленным
// аварийным режимом, и каждое его срабатывание считается и называется.
func buildListFilter(cfg config.Config, authzConn *grpc.ClientConn, logger *slog.Logger) *authzfilter.Narrower {
	breakglass := !cfg.ListFilterEnabled || authzConn == nil
	var conn grpc.ClientConnInterface
	if !breakglass {
		conn = authzConn
	} else {
		logger.Warn("list filter has no rights model to ask — every list REFUSES and the change "+
			"stream refuses to start, unless the emergency bypass is armed",
			"enabled", cfg.ListFilterEnabled, "authz_conn", authzConn != nil,
			"breakglass", cfg.ListFilterBreakglass)
	}
	cacheMax := cfg.ListFilterCacheMaxEntries
	if cacheMax <= 0 {
		cacheMax = 10000
	}
	f := authzfilter.New(conn, authzfilter.Config{
		Timeout:               time.Duration(cfg.ListFilterTimeoutMs) * time.Millisecond,
		CacheTTL:              time.Duration(cfg.ListFilterCacheTTLMs) * time.Millisecond,
		CacheMaxEntries:       cacheMax,
		SoftPassOnPeerFailure: cfg.ListFilterFailOpen,
		Breakglass:            breakglass && cfg.ListFilterBreakglass,
	}).WithLogger(logger)
	logger.Info("list filter wired",
		// per_call_timeout_ms gates ONE BatchCheck; operation_budget caps the whole
		// page filter (derived from per-call and the fan-out). All three are logged:
		// otherwise the config alone does not reveal which number actually bounds a
		// request.
		"per_call_timeout_ms", cfg.ListFilterTimeoutMs,
		"operation_budget", f.Budget(),
		"worst_case_depth_waves", f.WorstCaseDepth(),
		"cache_ttl_ms", cfg.ListFilterCacheTTLMs,
		"cache_max_entries", cacheMax,
		"soft_pass_on_peer_failure", cfg.ListFilterFailOpen,
		"narrows", f.Narrows(),
	)
	return f
}

// startRegisterDrainer dials the kacho-iam internal endpoint over the
// compute→iam edge (mTLS opt-in via cfg.IAMRegisterClientCreds — enable=false →
// insecure dev) and starts a corelib outbox/drainer over
// compute_fga_register_outbox. Each pending intent is replayed through
// InternalIAMService.RegisterResource / UnregisterResource by the applier
// (idempotent; Unavailable → retry with backoff; InvalidArgument → poison). The
// drainer Run-loop owns claim-CAS + advisory-lock for exactly-once across replicas
// (corelib W1.1). Returns a closer that shuts the dial-conn; nil error on success.
//
// The drainer dials the iam-internal :9091 listener — RegisterResource is an
// Internal-only RPC; the addr is derived from AuthZIAMGRPCAddr (the
// existing iam-internal endpoint compute already uses for Check) and falls back to
// IAMGRPCAddr when unset.
//
// Возвращает run-функцию drainer'а (d.Run, вешается на супервизор в runServe — не
// fire-and-forget) и closer dial-conn'а; conn живёт всё время работы run и
// закрывается defer'ом в runServe после g.Wait.
func startRegisterDrainer(cfg config.Config, pool *pgxpool.Pool, rec metrics.Recorder, logger *slog.Logger) (run func(context.Context) error, closer func(), err error) {
	addr := cfg.AuthZIAMGRPCAddr
	if addr == "" {
		addr = cfg.IAMGRPCAddr
	}

	creds, cerr := cfg.IAMRegisterClientCreds()
	if cerr != nil {
		return nil, nil, fmt.Errorf("compute→iam register mTLS creds: %w", cerr)
	}
	// idle-prone edge (register-drainer is mostly waiting on NOTIFY) → keepalive
	// idle pings keep the conn warm.
	conn, cerr := grpc.NewClient(addr, creds, grpcclient.KeepaliveDialOption(true))
	if cerr != nil {
		return nil, nil, fmt.Errorf("dial kacho-iam (register-drainer): %w", cerr)
	}

	applier := clients.NewIAMRegisterApplier(conn)
	d, derr := drainer.New[fgaintent.Payload](
		pool,
		drainer.Config{
			Table:   computeFGAOutboxTable,
			Channel: computeFGAOutboxChannel,
			// Parallel apply of a claim-batch's RegisterResource calls: a sequential
			// drainer ceilings at ~1/apply_latency (~0.2–0.5 tuple/s when iam
			// RegisterResource times out under write-burst) — an order of magnitude
			// below the create-worker's ~6.7/s, so the outbox backlog diverges and
			// v_list never materialises in the list read-your-writes window. Exactly-once
			// is unchanged (claim-tx holds each row's FOR UPDATE SKIP LOCKED lock;
			// applies are external gRPC, no extra pool conns). The iam applier is a
			// grpc.ClientConn-backed client → safe for concurrent invocation.
			ApplyConcurrency: cfg.FGARegisterApplyConcurrency,
			// Order-preserving drain, per resource. This table carries BOTH
			// fga.register AND fga.unregister of the SAME resource, and iam's
			// materialisation is only PARTIALLY versioned: source_version-LWW
			// (resource_mirror UPSERT guarded by `source_version <
			// EXCLUDED.source_version`) protects the ON-CONFLICT-UPDATE branch ONLY,
			// while unregister is a hard DELETE leaving no tombstone. A reordered
			// STALE register therefore has nothing to compare against, takes the
			// INSERT branch and RESURRECTS the mirror row of a DELETED resource; the
			// level-triggered reconciler then re-materialises its owner-tuple forever
			// (no self-healing). Reorder is NOT introduced by ApplyConcurrency: the
			// claim orders by (attempt_count, id), so a transiently-bumped register
			// (attempt>=1) loses to a fresh unregister (attempt=0) even at
			// ApplyConcurrency=1 and lands in a later batch. LWW therefore does NOT
			// make this stream order-insensitive — it only covers register↔register.
			//
			// PartitionColumn makes the claim partition-head-only: a row is never
			// claimed while a DELIVERABLE same-resource predecessor with a smaller id
			// is unsent, so per-resource FIFO holds cross-batch AND cross-replica and
			// at most one row per resource is in flight — throughput across DIFFERENT
			// resources is untouched, so the ApplyConcurrency backlog fix above still
			// stands. resource_id is the object key: every emitter writes one row per
			// FGA object stamped with that object's id, globally unique by
			// construction. Requires migration 0018's partial index (resource_id, id)
			// WHERE sent_at IS NULL for the claim's NOT EXISTS. Behaviour pinned by
			// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister and
			// clients.TestRegisterDrainer_PartitionHead_UnregisterThenStaleRegister.
			PartitionColumn: "resource_id",
		},
		func(b []byte) (fgaintent.Payload, error) {
			p, decErr := fgaintent.Decode(b)
			if decErr != nil {
				// Malformed payload — permanent poison, never retried.
				return fgaintent.Payload{}, errors.Join(drainer.ErrPermanent, decErr)
			}
			return p, nil
		},
		applier.Apply,
		logger.With("component", "fga-register-drainer"),
		// Each poisoned row bumps outbox_poisoned_total{table=…}.
		drainer.WithPoisonObserver[fgaintent.Payload](func() {
			rec.IncPoisoned(computeFGAOutboxTable)
		}),
		// Каждая ДОСТАВЛЕННАЯ строка инкрементит счётчик своего направления
		// Прежде эту величину ставил скан как `count(*)` по живым
		// строкам — совпадая с объявленным «за всё время» ровно до тех пор,
		// пока строки не убираются. Наблюдатель считает СОБЫТИЕ доставки,
		// поэтому уборка на величину не влияет by construction.
		drainer.WithDeliveryObserver[fgaintent.Payload](
			metrics.DeliveryObserver(computeFGAOutboxTable, metrics.RegisterOutboxDirections(), rec)),
	)
	if derr != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("build register-drainer: %w", derr)
	}

	logger.Info("FGA register-drainer started",
		"iam_addr", addr, "mtls", cfg.IAMRegisterMTLS.Enable)

	return d.Run, func() { _ = conn.Close() }, nil
}

// buildSyncRegistrar дилит kacho-iam internal :9091 (InternalIAMService.
// RegisterResource) тем же compute→iam fga-proxy ребром (mTLS opt-in через
// cfg.IAMRegisterClientCreds — enable=false → insecure dev) и собирает синхронный
// owner-tuple registrar (owner-tuple op-gating P4). Отдельный dial-conn
// (idle-keepalive: registrar срабатывает лишь на Create-всплесках); возвращает
// closer. Addr выводится из AuthZIAMGRPCAddr (тот же iam-internal, что для Check),
// fallback — IAMGRPCAddr. Зеркалит kacho-vpc buildSyncRegistrar.
func buildSyncRegistrar(cfg config.Config, logger *slog.Logger) (*ownerregister.Registrar, func(), error) {
	addr := cfg.AuthZIAMGRPCAddr
	if addr == "" {
		addr = cfg.IAMGRPCAddr
	}
	creds, cerr := cfg.IAMRegisterClientCreds()
	if cerr != nil {
		return nil, nil, fmt.Errorf("compute→iam sync-register mTLS creds: %w", cerr)
	}
	conn, cerr := grpc.NewClient(addr, creds, grpcclient.KeepaliveDialOption(true))
	if cerr != nil {
		return nil, nil, fmt.Errorf("dial kacho-iam (sync registrar): %w", cerr)
	}
	logger.Info("owner-tuple sync-registrar dialed", "iam_addr", addr, "mtls", cfg.IAMRegisterMTLS.Enable)
	// Форма доставки — ОДНА на все сервисы (pkg/ownerregister): своего
	// регистратора у compute больше нет, потому что своего в нём и не было —
	// только копия, разошедшаяся с соседями по маркеру версии.
	reg, rerr := ownerregister.New(iamv1.NewInternalIAMServiceClient(conn))
	if rerr != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("собрать синхронный registrar: %w", rerr)
	}
	return reg, func() { _ = conn.Close() }, nil
}

// registerInternalServices — kacho-only/admin RPC на internal listener (:9091).
//
// «Не маршрутизируется наружу» — про api-gateway: Internal*-сервисы отсекаются его
// allow-list'ом (`HasInternalSuffix`), поэтому тенантский REST сюда не доходит. Это
// НЕ значит «сюда доходит только шлюз»: у kacho-compute нет NetworkPolicy ни в одном
// профиле развёртывания (шаблона нет; у kacho-vpc он есть и намеренно выключен в
// отладочном профиле), поэтому на уровне сети порт открыт всякому поду namespace'а.
// Прежняя редакция этого комментария ссылалась на сетевую политику как на
// действующее ограничение — ссылка была ложной, и именно она делала отсутствие
// авторизации у потока журнала похожим на осознанный размен.
//
// Что здесь РЕАЛЬНО ограничивает доступ: mTLS на листенере, allow-list законных
// отправителей чужой личности, per-RPC Check на каждом замапленном RPC
// (`internal/check/permission_map.go`), а для потока журнала изменений — сужение по
// правам вызывающего на КАЖДУЮ отдаваемую строку. Сетевая политика была бы
// эшелонированием поверх этого, а не заменой ему.
func registerInternalServices(
	srv grpc.ServiceRegistrar,
	svcs *services,
	subscribe subscriptionv1.InternalSubscriptionServiceServer,
) {
	// Поток изменений — ОБЩИЙ сервер (`pkg/subscription`), а не своя обёртка
	// вокруг него: владелец регистрирует его самого. Регистрация безусловна —
	// собирает сервер композиционный корень, и его сборка умеет ОТКАЗАТЬ, поэтому
	// до сюда нулевой указатель не доходит. Условная регистрация означала бы, что
	// подписка тихо отсутствует у процесса, чей дескриптор объявил ей срок жизни.
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, subscribe)
	computev1.RegisterInternalMachineTypeServiceServer(srv, handler.NewInternalMachineTypeHandler(svcs.machineType))
	computev1.RegisterInternalRealizationServiceServer(srv, handler.NewInternalRealizationHandler(svcs.realization))
	computev1.RegisterInternalNodeOwnershipServiceServer(srv, handler.NewInternalNodeOwnershipHandler(svcs.nodeOwnership))
}

// quotaHandlerOrNil возвращает обработчик чтения квот ЛИБО настоящий nil.
//
// Возврат `*handler.QuotaHandler(nil)` в поле структуры был бы не тем же самым:
// проверка `svcs.quota != nil` на типизированном nil ИСТИННА, и метод
// зарегистрировался бы, чтобы отвечать отказом на первом же вызове. Решение
// принимается здесь, где тип ещё конкретен.
func quotaHandlerOrNil(g *quota.Guard) *handler.QuotaHandler {
	if g == nil {
		return nil
	}
	return handler.NewQuotaHandler(g)
}

// buildSubscriptionServer собирает ОБЩИЙ сервер потока изменений для журнала compute.
//
// Владелец приносит сюда ЖУРНАЛ и величины ПОСАДКИ — и ничего больше: курсор,
// граница устоявшегося, пределы, сужение по правам и порядок отказов
// принадлежат общему серверу. Появись здесь возможность принести своё вместо
// любого из них, механизм перестал бы быть общим, оставшись общим по имени.
//
// Отказ возвращается, а не логируется: величина посадки, о которой никто не
// сказал, не должна обнаруживаться первым запросом в бою.
func buildSubscriptionServer(
	cfg config.Config,
	listFilter *listnarrow.Narrower,
	logger *slog.Logger,
) (subscriptionv1.InternalSubscriptionServiceServer, error) {
	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		return nil, err
	}
	dsn := cfg.SingleConnDSN()
	// Страж посадки: параметр ПУЛА в строке одиночного соединения означает отказ
	// на подключении, а не на сборке, — и потому обязан быть пойман здесь, а не
	// первой подпиской в бою. Предикат один на дерево (coredb.PoolParamFromDSN):
	// он отдаёт ИМЯ ключа, поэтому отказ называет ручку, а не строку подключения,
	// которая несёт пароль базы.
	if key := coredb.PoolParamFromDSN(dsn); key != "" {
		return nil, fmt.Errorf("поток изменений: строка подключения несёт параметр пула %q: "+
			"вне пула это неизвестный PG-параметр и FATAL при подключении, "+
			"а отказ наступил бы не на сборке, а у каждой подписки в бою", key)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal: subscriptionjournal.Journal(),
		// Выделенное соединение вне пула: `LISTEN` требует своей сессии, а сессия
		// из пула вернулась бы в него вместе с подпиской.
		DSN:          dsn,
		Narrower:     listFilter,
		ProjectGate:  gate,
		MaxStreams:   cfg.SubscriptionMaxStreams,
		StreamBudget: cfg.SubscriptionStreamBudget,
		IdlePoll:     cfg.SubscriptionIdlePoll,
		Logger:       logger,
	})
	if err != nil {
		return nil, fmt.Errorf("subscription server: %w", err)
	}
	return srv, nil
}
