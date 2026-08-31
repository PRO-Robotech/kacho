// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-loadbalancer — API-сервер kacho-nlb (gRPC public :9090 +
// internal :9091). Composition root (workspace CLAUDE.md «Чистая архитектура»):
// единственное место, где собираются adapter'ы (pgxpool, gRPC clients, peer-
// клиенты) и пробрасываются в handler-слой.
//
// Поддерживает один subcommand `serve`. Миграции — в отдельном binary
// `cmd/migrator` (один CLI use-case = один binary).
//
// # Чего этот корень БОЛЬШЕ НЕ делает
//
// Он не собирает серверы, не выстраивает цепочки звеньев, не строит карту прав и
// не держит собственного звена решения о доступе: всё это переехало в носитель
// контура (`pkg/servicehost`), которому сервис приносит ОБЪЯВЛЕНИЕ о себе —
// дескриптор (`describe.go`). Оба слушателя поднимает `servicehost.Serve`, он же
// гасит их по отмене контекста.
//
// Здесь остаётся домен: пул, слой доступа к данным, use-cases, peer-клиенты,
// фоновые loop'ы под супервизором, диагностический слушатель и readiness.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
	"github.com/PRO-Robotech/kacho/pkg/retention"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients"
	computeclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/compute"
	geoclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	nlbmetrics "github.com/PRO-Robotech/kacho/services/nlb/internal/observability/metrics"

	// dto/type2pb init регистрирует все DTO трансферы (domain ↔ proto) в реестре.
	// Импортируется здесь (composition root), чтобы registry был полон до старта
	// gRPC server'ов; handler'ы вызывают dto.Transfer и предполагают, что
	// каждая зарегистрированная пара уже в map'е.
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	_ "github.com/PRO-Robotech/kacho/services/nlb/internal/dto/type2pb"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/schemaguard"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// peerClients — composition root bundle типизированных адаптеров к peer-сервисам.
// Use-case'ы принимают эти port-интерфейсы через конструкторы (Clean
// Architecture: composition root — единственное место, где известны
// конкретные реализации).
type peerClients struct {
	// IAM
	Project  iamclient.ProjectClient
	Check    iamclient.CheckClient
	Register iamclient.RegisterResourceClient // FGA-proxy (register-drainer)
	// Limit — резолв разрешённых величин учёта (InternalLimitService.Resolve).
	// nil → совещательная полоса не собирается, и ранний отказ по квоте не
	// производится; место при этом по-прежнему занимает триггер.
	Limit *iamclient.LimitClient
	// Geo (Region/Zone-валидация — ребро nlb→geo, kacho-geo)
	Region geoclient.RegionClient
	Zone   geoclient.ZoneClient
	// ZoneRegion — авторитетный zone→region резолв (тот же geo-conn). Регион
	// НИКОГДА не выводится из имени зоны — только вызовом владельца Geography.
	ZoneRegion *geoclient.ZoneRegionClient
	// Compute (Instance-resolve — НЕ geography, ребро nlb→compute остаётся)
	Instance computeclient.InstanceClient
	// VPC
	Subnet           vpcclient.SubnetClient
	NetworkInterface vpcclient.NetworkInterfaceClient
	Address          vpcclient.AddressClient
	InternalAddress  vpcclient.InternalAddressClient
	// SecurityGroup — NLB-1b MIGRATE peer-validate of LB security_group_ids.
	SecurityGroup vpcclient.SecurityGroupClient
	// ListFilter — per-object filtered List (RBAC; iam
	// AuthorizeService.BatchCheck). nil → use-case'ы делают unfiltered passthrough.
	ListFilter *authzfilter.Narrower
}

func main() {
	root := &cobra.Command{
		Use:          "kacho-loadbalancer",
		Short:        "kacho-nlb API server (L4 NLB control-plane)",
		SilenceUsage: true,
	}
	root.AddCommand(newServeCmd())
	if err := root.Execute(); err != nil {
		// cobra сама печатает текст ошибки; нам остаётся exit-code.
		os.Exit(1)
	}
}

// newServeCmd запускает API-сервер kacho-nlb. Принимает --config path к YAML
// (опционально; defaults + ENV сами по себе достаточны для dev). Под капотом —
// composition root: config.Load → slog → pgxpool → ops repo → peer-clients
// stubs → public+internal gRPC servers → parallel.ExecAbstract.
func newServeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "run gRPC public/internal servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(configPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "",
		"path to kacho-nlb config.yaml (optional; defaults + ENV used if empty)")
	return cmd
}

// runServe — собственно serve composition root.
func runServe(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)
	logger.Info("kacho-loadbalancer starting",
		"mode", cfg.Mode().String(),
		"endpoint", cfg.APIServer.Endpoint,
		"internal_endpoint", cfg.APIServer.InternalEndpoint,
	)

	// Context: SIGTERM / SIGINT триггерит cancel → graceful stop.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Fail-closed boot-gate: при KACHO_NLB_REQUIRE_IAM=true мутирующий Create
	// отвергается (UNAVAILABLE), а готовность пода остаётся NotReady, пока дренаж
	// регистраций не подключён к kacho-iam, — ни один ресурс не создаётся без
	// доставляемого намерения о владельце. Стартует НЕ подключённым;
	// SetConnected(true) вызывается ниже, когда дренаж собран с живым пиром.
	//
	// Строится ДО дескриптора: гейт — ось дескриптора (`Spec.BootGate`), и
	// исполняет его теперь носитель, а не собственное звено сервиса.
	bootGate := bootgate.New(bootgate.Config{RequireIAM: cfg.FGA.RequireIAM, Service: "kacho-nlb"})

	// Peer-gRPC clients (corlib client-builder) — ДО дескриптора: из соединения с
	// внутренним листенером kacho-iam строится сужатель списочной выдачи, а он
	// объявляется осью дескриптора (`Spec.Narrowers`).
	peerConns, peers, err := dialPeers(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("dial peers: %w", err)
	}
	defer closeAll(peerConns, logger)

	// Самоотчёт о посадке — ПОСЛЕ приёма дескриптора и ДО подъёма слушателей.
	// Гейт посадки обязан утверждать на этом наблюдаемом факте, а не на хранимом
	// конфиге: правка настроек без переката пода оставляет процесс с прежним
	// окружением, и «под Ready» доказательством посадки не является.

	// pgxpool. Строится из cfg.DSN() — URL плюс `pool_max_conns` /
	// `pool_max_conn_lifetime`: только там ширина пула, объявленная в конфиге,
	// доезжает до самого пула (см. config.DSN; migrator и LISTEN-feed берут URL
	// как есть — `pool_*` для них фатален). Ширина решает, сколько одновременных
	// запросов сервис обслужит, поэтому настройка обязана быть читаемой, а не
	// декоративной; гейт — cmd/kacho-loadbalancer/pool_wiring_test.go.
	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return fmt.Errorf("open pgxpool: %w", err)
	}
	defer pool.Close()

	// Дескриптор процесса. Все отказы старта, являющиеся свойствами объявленного
	// (круг отправителей, ребро решения о доступе, окна и бюджеты, боевая посадка,
	// незаявленная ось), отрабатывают ЗДЕСЬ — до единого слушателя.
	//
	// После пула, а не до него: порт сверки существования живёт НА пуле, и принести
	// его раньше значило бы принести порт, отвечающий «соединения нет». Открытие
	// пула обратимо (`defer` выше) и дешевле ложной сверки.
	// Приёмник читателя величин кеша вердиктов. Объявлен ДО дескриптора: кеш
	// собирает носитель контура, и читателя он отдаёт через поле дескриптора.
	var authzCache authzmetrics.Source

	// Prometheus observability adapter: приватный реестр, питает outbox-recorder,
	// LRO-worker/reconciler recorder и diagnostic /metrics. Заменяет in-memory
	// MemRecorder (метрики не экспортировались наружу) и NopRecorder LRO-worker'а.
	//
	// Собирается ДО дескриптора: реестр — поле объявления, и без него дескриптор
	// не принимается. Регистрации коллекторов остаются ниже по тексту — им нужен
	// только сам адаптер, а не порядок относительно объявления.
	metricsAdapter := nlbmetrics.New(buildVersion, buildCommit)

	desc, err := describe(cfg, logger, peers.ListFilter, bootGate, kachopg.NewExistenceProbe(pool), authzCache.Install, metricsAdapter.Registerer())
	if err != nil {
		return err
	}

	// Самоотчёт о посадке — ПОСЛЕ принятия дескриптора: до него он описывал бы
	// намерение, а не посадку, которую процесс действительно занял. Гейт посадки
	// читает эту строку у живого процесса, поэтому печатать её раньше отказов
	// значило бы отчитываться за старт, который может не состояться.
	observability.LogBootPosture(logger, bootPosture(cfg))

	// CQRS-Repository. Реплики нет ни в одной посадке — второй пул не создаётся,
	// поэтому Reader идёт на тот же master-пул, что и Writer (`New(pool, nil)`).
	// Это и есть причина, по которой read-TX не удерживается через ожидание
	// соседа: пул общий с мутациями (см. api/*/list.go readPage). Use-case'ы,
	// зарегистрированные на handler-слое в следующих 'ах, получают этот
	// repo через port-интерфейсы (`internal/repo/kacho.Repository`).
	repo := kachopg.New(pool, nil)
	defer repo.Close()

	// Operations LRO repo (общая таблица operations в kacho_nlb schema).
	// Используется всеми use-case'ами мутирующих RPC через worker'ы
	// `operations.Run(ctx, opsRepo, opID, fn)` (kacho-corelib pattern) и
	// напрямую — OperationService.Get/Cancel (см. ниже).
	opsRepo := operations.NewRepo(pool, "kacho_nlb")

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
			Table:           nlbFGAOutboxTable,
			PartitionColumn: reconciler.RegisterOutboxPartition,
		},
		retention.DefaultConfig(),
		logger.With(slog.String("component", "queue_retention_sweep")),
	); err != nil {
		return fmt.Errorf("фоновая уборка доставленных строк очереди: %w", err)
	}

	// Фоновая уборка РЕСУРСНОГО ЖУРНАЛА подписки (#1735).
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

	// Снимок величины квоты обязан ДОГОНЯТЬ авторитет: без тянущего строка учёта,
	// заведённая один раз, живёт со своей величиной вечно, и смена предела
	// администратором не доезжает до проекта никогда. Показывать арендатору такой
	// снимок значило бы громко назвать число, которое не догонит назначенное, —
	// поэтому чтение квот и тянущий едут вместе.
	//
	// Без соседа величин тянущий НЕ собирается, и это названо вслух: «полосы нет»
	// обязано быть отличимо от «полоса есть и молчит».
	if peers.Limit != nil {
		stopQuotaSync, qerr := corequota.StartLimitSyncer(
			ctx, pool, peers.Limit, kachopg.QuotaSchema, corequota.Config{}, logger)
		if qerr != nil {
			return fmt.Errorf("start quota limit syncer: %w", qerr)
		}
		defer stopQuotaSync()
	} else {
		logger.Warn("resource-count quota: no internal iam endpoint, the limit snapshot will " +
			"NEVER catch up with the authority — the charging trigger still enforces, but an " +
			"administrator raising or lowering a ceiling has no effect on this process")
	}

	// peers — типизированные clients потребляются handler'ами. Композиционный root
	// владеет gRPC-conn'ами и закрывает их через defer выше — peers держит ссылки
	// на stub'ы поверх этих conn'ов, отдельного Close не требуется.
	//
	// Звена решения о доступе здесь БОЛЬШЕ НЕТ: его строит носитель по объявленному
	// ребру (`Spec.CheckEdge`), окну кэша, сроку вопроса и бюджету отказов. Прежде
	// оно собиралось тут же, и бюджет отказов стоял литералом рядом с конструктором;
	// теперь это ручка `authz.deny-budget-per-sec`, которую видно в обзоре и можно
	// сузить на конкретной посадке. peers.Check остаётся — его зовут ОБРАБОТЧИКИ
	// (пообъектная проверка целевой группы в Listener/LoadBalancer), а это другой
	// путь, чем per-RPC гейт.

	// Величины сужателя выходят из процесса ТОЛЬКО здесь. Полос четыре: одна
	// положительная и три — страница, ушедшая БЕЗ пообъектной проверки. Снимите
	// эту строку — и полосы исчезнут с поверхности, а не станут нулями; ровно это
	// ловит гейт дерева `TestEveryListNarrowConsumerRegistersItsCollector`.
	metricsAdapter.RegisterListNarrow(func() listnarrow.Counts { return peers.ListFilter.Counts() })
	// Доля попаданий кеша положительных вердиктов. Источник устанавливается ПОЗЖЕ
	// — кеш строит носитель контура, — поэтому коллектор регистрируется сейчас и
	// до установки отвечает нулями: исчезновение серий на это окно сообщило бы
	// собирателю не «попаданий не было», а ничего.
	//
	// ПОЛОС ДВЕ, потому что окон положительных вердиктов у этого процесса два:
	// окно звена решения (вопрос на ВЫЗОВ) и окно общего сужателя (вопрос на
	// КАЖДЫЙ элемент страницы, а страница контрактно бывает до тысячи). Через
	// второе проходит БОЛЬШЕ вопросов, чем через первое, и до #768 его не считал
	// никто: «кеш сужателя даёт столько-то» было непроверяемо в обе стороны.
	metricsAdapter.RegisterAuthzCache(map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    authzCache.Cache,
		authzmetrics.LaneNarrow: peers.ListFilter.CacheStats,
	}, authzCache.Read)
	var outboxRec metrics.Recorder = metricsAdapter
	var lroRec operations.Recorder = metricsAdapter

	// LRO worker (default-registry) поднимается ДО приёма трафика: ConfigureDefault
	// подключает Prometheus-Recorder (live terminal-write/inflight метрики — раньше
	// NopRecorder), Start делает Ready=true без единой мутации (нет
	// readiness-deadlock «NotReady → нет Run → worker не стартует»).
	if err := startLROWorker(lroRec, logger); err != nil {
		return fmt.Errorf("start LRO worker: %w", err)
	}

	// Supervised background loop'ы (errgroup): LRO-reconciler, target-drain,
	// free-ip, fga-register-drainer + outbox-backstop. Собираются здесь
	// (drainer/backstop-ресурсы + bootGate.SetConnected как side-effect), но
	// запускаются errgroup'ом перед Serve.
	background, err := assembleBackgroundWorkers(ctx, backgroundDeps{
		pool:            pool,
		repo:            repo,
		lroRec:          lroRec,
		outboxRec:       outboxRec,
		bootGate:        bootGate,
		peers:           peers,
		cfg:             cfg,
		logger:          logger,
		freeIPPoisonObs: metricsAdapter.IncFreeIPPoisoned,
	})
	if err != nil {
		return err
	}

	// Регистраторы обработчиков. Носитель зовёт КАЖДЫЙ ровно один раз и передаёт
	// ему `grpc.ServiceRegistrar` — интерфейс с единственным методом, поэтому
	// сервера у корня не остаётся ни в каком виде и приделать к нему своё звено
	// нельзя. Разделение public/internal сохраняется: `Internal.*` регистрируется
	// ТОЛЬКО на внутреннем слушателе (ban #6).
	//
	// Синхронный регистратор владельца собирается ЗДЕСЬ, а не внутри регистрации:
	// его сборка умеет отказать, а у регистратора носителя возврата ошибки нет —
	// и это правильно, отказ обязан случиться до подъёма слушателей.
	syncRegistrar, err := buildSyncRegistrar(peers)
	if err != nil {
		return err
	}
	// Общий сервер потока изменений. Строится ДО подъёма слушателей: его сборка
	// умеет отказать (негодное объявление журнала, невыбранная величина посадки,
	// неработающий сужатель), а отказ обязан случиться раньше первого принятого
	// соединения, а не первым запросом в бою.
	subscribeSrv, err := buildSubscriptionServer(cfg, peers.ListFilter, logger)
	if err != nil {
		return err
	}
	wiring := grpcWiring{
		repo:          repo,
		opsRepo:       opsRepo,
		peers:         peers,
		pool:          pool,
		cfg:           cfg,
		logger:        logger,
		syncRegistrar: syncRegistrar,
		// Учёт числа ресурсов: совещательная полоса. Собирается ЗДЕСЬ, до подъёма
		// слушателей, потому что ей нужны репозиторий и оба соседа сразу.
		quotaGuard: buildQuotaGuard(repo, peers),
		// Поток изменений: собран выше, потому что его сборка умеет отказать, а
		// регистратор носителя возврата ошибки не имеет — и не должен.
		subscription: subscribeSrv,
	}
	logger.Info("quota_guard", "wired", wiring.quotaGuard != nil)

	// Dependency-aware readiness: /readyz отражает здоровье database / register-
	// drainer (= IAM-достижимость в nlb) / lro-worker; /healthz — только живость
	// процесса (защита от restart-storm). Результат зеркалится в dependency_up
	// Prometheus-gauge.
	healthAgg := health.New(
		// Версия схемы читается из ВСТРОЕННОГО набора миграций — того же, что
		// применяет мигратор. Least-privilege serve-бинаря это не нарушает: набор
		// читается как встроенные байты, а у базы спрашивается ОДИН `SELECT`
		// применённой версии; схему serve-бинарь по-прежнему не меняет.
		buildReadinessCheckers(pool, bootGate, schemaguard.CheckFromFS(migrations.FS, schemaguard.PgxVersionReader(pool))),
		health.WithResultObserver(metricsAdapter.SetDependencyUp),
	)
	// Diagnostic HTTP-listener (cluster-internal): /metrics + /healthz + /readyz.
	// metrics.enable=false ИЛИ пустой metrics.address → не поднимается (back-compat).
	//
	// Поверхность входит в контур ОТДЕЛЬНЫМ ПРОФИЛЕМ той же функции (решение
	// владельца XC-7, в-1): не gRPC, цепочка другая, полями общего дескриптора её
	// не втягивают. Корень приносит ОБЪЯВЛЕНИЕ; подъём, самоотчёт и гашение
	// принадлежат профилю.
	diagAddr := ""
	if cfg.Metrics.Enable {
		diagAddr = cfg.Metrics.Address
	}
	diagDesc, err := describeDiagnosticSurface(diagAddr, metricsAdapter, healthAgg,
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
	// этой отмены, а не одновременно с ней: kubelet перестаёт слать трафик до
	// того, как соединения начнут закрываться. Одним контекстом на всё этот
	// порядок был бы неразличим.
	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()
	// serveDone закрывается, когда носитель вернул управление. Нужен сторожу
	// срока штатного завершения ниже.
	serveDone := make(chan struct{})

	// Единый shutdown-триггер (sync.Once): флипает готовность, отменяет фоновые
	// loop'ы, затем просит носитель погасить слушатели. Вызывается из
	// shutdown-waiter (SIGTERM), из краха любого supervised-task'а и из
	// superviseBackground при неожиданном выходе.
	var shutdownOnce sync.Once
	shutdownCh := make(chan struct{})
	triggerShutdown := func() {
		shutdownOnce.Do(func() {
			healthAgg.SetShuttingDown()
			close(shutdownCh)
			cancel()
			stopServe()
			// Сторож срока штатного завершения. Носитель гасит слушатели ТОЛЬКО
			// мягко (GracefulStop) — принудительного `Stop()` по истечении срока у
			// него нет, и принести его сервису нечем: сервера корень не получает ни
			// в каком виде. Поэтому величина `api-server.graceful-shutdown`
			// сохраняет свой предмет — срок, за который завершение обязано
			// уложиться, — но её нарушение сегодня НАБЛЮДАЕТСЯ, а не исполняется.
			// Молчаливой альтернативой было бы зависшее завершение, неотличимое от
			// штатного, и ручка без читателя вдобавок.
			go func() {
				select {
				case <-serveDone:
				case <-time.After(cfg.APIServer.GracefulShutdown):
					logger.Error("graceful stop exceeded its budget; listeners are still draining",
						"budget", cfg.APIServer.GracefulShutdown,
						"knob", "api-server.graceful-shutdown",
						"note", "the carrier stops listeners gracefully only; there is no forced Stop behind this budget")
				}
			}()
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
	// ОБА gRPC-слушателя — носитель контура. Он поднимает их с одной цепочкой
	// звеньев, прогоняет отказы старта, которым нужен служимый набор RPC, и
	// обслуживает до отмены serveCtx. Исход внутреннего слушателя учитывается
	// наравне с публичным — это его свойство, а не наше.
	g.Go(func() error {
		serr := servicehost.Serve(serveCtx, desc,
			func(reg grpc.ServiceRegistrar) { registerPublic(reg, wiring) },
			func(reg grpc.ServiceRegistrar) { registerInternal(reg, wiring) },
		)
		close(serveDone)
		if serr != nil {
			logger.Error("grpc listeners stopped", "err", serr)
			triggerShutdown()
			return fmt.Errorf("grpc: %w", serr)
		}
		triggerShutdown()
		return nil
	})
	// shutdown-waiter: SIGTERM/SIGINT (ctx) ИЛИ крах любого task'а (shutdownCh) →
	// triggerShutdown → дрейн LRO worker'ов → гашение diagnostic-listener'а
	// последним (флип /readyz→503 успевает отработать до закрытия порта).
	g.Go(func() error {
		select {
		case <-ctx.Done():
		case <-shutdownCh:
		}
		logger.Info("shutdown signal received")
		triggerShutdown()
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		if werr := operations.Wait(drainCtx); werr != nil {
			logger.Warn("operations workers did not finish in time",
				"err", werr, "active", operations.Active())
		}
		// Гашение поверхности — последним действием остановки. Её возврата ждёт
		// сам errgroup: профиль возвращается только после того, как порт освобождён.
		stopDiag()
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}
	logger.Info("kacho-loadbalancer stopped cleanly")
	return nil
}

// peerDialSpec — декларативная единица wiring одного peer-conn'а: имя, dial-addr,
// legacy system-trust TLS-bool и per-edge mTLS-config (grpcclient.TLSClient).
// mtls имеет приоритет над tls в dialOne (см. dialPeers): при mtls.Enable=true dial
// предъявляет client-cert и верифицирует server по CA + server_name.
//
// Вынесен из dialPeers как чистая (без side-effect'ов) проекция wiring'а — это
// единственный testable seam, фиксирующий контракт «каждое cross-service ребро
// предъявляет СВОИ per-edge mTLS-creds» (nlb→vpc / nlb→compute зеркалят
// nlb→iam mtls.iam-register). Регрессия к zero-value (insecure) TLSClient на vpc/
// compute ребре ловится тестом peerDialSpecs (cmd/.../dialpeers_mtls_test.go).
type peerDialSpec struct {
	name string               // лог-имя ребра (iam-public / vpc-internal / compute / …)
	addr string               // host:port (уже резолвнутый firstNonEmpty)
	tls  bool                 // legacy system-trust TLS (перебивается mtls при Enable)
	mtls grpcclient.TLSClient // per-edge client-cert config (приоритет над tls)
}

// peerDialSpecs строит таблицу peer-conn'ов из config'а. Чистая функция:
// никаких dial'ов / I/O — только маппинг cfg → peerDialSpec. Порядок conn'ов:
//   - iam-public  (9090, ProjectService.Get)        ← cfg.MTLS.IAMProject
//   - iam-internal(9091, Check + Register)           ← cfg.MTLS.IAMRegister
//   - geo         (9090, RegionService.Get)          ← cfg.MTLS.Geo
//   - compute     (9090, InstanceService.Get)        ← cfg.MTLS.Compute
//   - vpc-public  (9090, Address/Subnet/NIC)         ← cfg.MTLS.VPC
//   - vpc-internal(9091, InternalAddressService)     ← cfg.MTLS.VPC
//
// Per-listener split для iam (iam-public≠iam-internal по ServerName) обязателен под
// RequireAndVerifyClientCert (latent-bug). vpc-public и vpc-internal
// дилят один Service `vpc` (SAN serverHosts=[vpc] покрывает оба порта) → общий
// cfg.MTLS.VPC. Адрес каждого ребра — firstNonEmpty(public, internal) и наоборот,
// чтобы single-addr dev-config продолжал работать.
func peerDialSpecs(cfg *config.Config) []peerDialSpec {
	return []peerDialSpec{
		{
			name: "iam-public",
			addr: firstNonEmpty(cfg.ExtAPI.IAM.Addr, cfg.ExtAPI.IAM.InternalAddr),
			tls:  cfg.ExtAPI.IAM.TLS,
			mtls: cfg.MTLS.IAMProject,
		},
		{
			name: "iam-internal",
			addr: firstNonEmpty(cfg.ExtAPI.IAM.InternalAddr, cfg.ExtAPI.IAM.Addr),
			tls:  cfg.ExtAPI.IAM.TLS,
			mtls: cfg.MTLS.IAMRegister,
		},
		{
			name: "geo",
			addr: firstNonEmpty(cfg.ExtAPI.Geo.Addr, cfg.ExtAPI.Geo.InternalAddr),
			tls:  cfg.ExtAPI.Geo.TLS,
			mtls: cfg.MTLS.Geo,
		},
		{
			name: "compute",
			addr: firstNonEmpty(cfg.ExtAPI.Compute.Addr, cfg.ExtAPI.Compute.InternalAddr),
			tls:  cfg.ExtAPI.Compute.TLS,
			mtls: cfg.MTLS.Compute,
		},
		{
			name: "vpc-public",
			addr: firstNonEmpty(cfg.ExtAPI.VPC.Addr, cfg.ExtAPI.VPC.InternalAddr),
			tls:  cfg.ExtAPI.VPC.TLS,
			mtls: cfg.MTLS.VPC,
		},
		{
			name: "vpc-internal",
			addr: firstNonEmpty(cfg.ExtAPI.VPC.InternalAddr, cfg.ExtAPI.VPC.Addr),
			tls:  cfg.ExtAPI.VPC.TLS,
			mtls: cfg.MTLS.VPC,
		},
	}
}

// dialPeers открывает gRPC connections к vpc/compute/iam через corlib client-builder
// и собирает типизированные adapter'ы (Clean Architecture outbound adapters).
//
// Возвращает:
//   - clients.Conn — для defer'нутого Close в composition root.
//   - *peerClients   — bundle типизированных port-интерфейсов для use-case'ов.
//
// Соединения открываются только если соответствующий addr задан в config;
// иначе adapter в peers остаётся nil (graceful dev-startup без peer-сервисов;
// use-case'ы при отсутствующем adapter'е возвращают Unavailable).
//
// Внутренняя топология:
//   - kacho-iam: один conn на InternalAddr — ProjectService.Get живёт и
//     на public, и (через scope-filter) на internal; InternalIAMService.{Check,
//     RegisterResource, UnregisterResource} — только на internal. Используем
//     internal listener.
//   - kacho-geo: один conn на public Addr — RegionService.Get (region-валидация,
//
// kacho-geo). Geography выделена из compute в leaf-сервис geo.
//   - kacho-compute: один conn на public Addr — InstanceService.Get
//     (instance-resolve; НЕ geography).
//   - kacho-vpc: ДВА conn'а. public (Addr) — AddressService / OperationService;
//     internal (InternalAddr) — InternalAddressService.{Set,Clear}Reference,
//     SubnetService / NetworkInterfaceService живут на public, но edge consumer
//     (NLB) использует public Addr для них тоже.
//
// Internal-vs-external инвариант: Internal.* НЕ публикуется на external
// TLS endpoint.
func dialPeers(
	ctx context.Context, cfg *config.Config, logger *slog.Logger,
) ([]clients.Conn, *peerClients, error) {
	var conns []clients.Conn
	// dialOne opens one peer conn. mtls (per-edge grpcclient.TLSClient)
	// takes precedence over the legacy `useTLS` system-trust bool: when
	// mtls.Enable=true the dial presents a client-cert and verifies the server
	// against the configured CA + server_name. mtls.Enable=false → insecure /
	// legacy TLS (dev backward-compat). A mTLS cred-build error is
	// fail-closed (no silent insecure downgrade).
	dialOne := func(name, addr string, useTLS bool, mtls grpcclient.TLSClient) (clients.Conn, error) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			logger.Info("peer not configured — skip", "peer", name)
			return nil, nil
		}
		var mtlsCreds credentials.TransportCredentials
		if mtls.Enable {
			c, cerr := grpcclient.TLSClientTransportCreds(mtls)
			if cerr != nil {
				return nil, fmt.Errorf("build mTLS creds for %s: %w", name, cerr)
			}
			mtlsCreds = c
		}
		cc, err := clients.Build(ctx, clients.BuildOptions{
			Endpoint:    addr,
			TLS:         useTLS,
			MTLSCreds:   mtlsCreds,
			DialTimeout: peerDialDuration(cfg),
		})
		if err != nil {
			return nil, fmt.Errorf("dial %s @ %q: %w", name, addr, err)
		}
		conns = append(conns, cc)
		logger.Info("peer connected", "peer", name, "addr", addr, "tls", useTLS, "mtls", mtls.Enable)
		return cc, nil
	}

	peers := &peerClients{}

	// Dial every peer-conn from the declarative spec table (peerDialSpecs). The
	// table is the single source of truth for per-edge mTLS wiring: each spec
	// carries its OWN grpcclient.TLSClient (iam-public→IAMProject, iam-internal→
	// IAMRegister, compute→Compute, vpc-public/internal→VPC). dialOne applies the
	// mtls precedence (Enable=true → client-cert + server verify; fail-closed on
	// cred-build error). conns are appended for defer'd Close in the composition
	// root. A dial error closes everything opened so far and propagates.
	//
	// Топология (см. peerDialSpecs doc):
	//   - kacho-iam: два conn'а ПЕР-LISTENER. PUBLIC (9090) — ProjectService.Get;
	//     INTERNAL (9091) — InternalIAMService.{Check,RegisterResource,Unregister}.
	//     Раздельные mTLS-поля (IAMProject vs IAMRegister) обязательны: единый
	//     ServerName не корректен для обоих listener'ов под
	//     RequireAndVerifyClientCert (latent-bug). До split'а
	//     оба шли на INTERNAL → ProjectService Unimplemented ("project lookup failed").
	//   - kacho-geo: один conn (9090) — RegionService.Get (region-валидация).
	//   - kacho-compute: один conn (9090) — InstanceService.Get (instance-resolve).
	//   - kacho-vpc: два conn'а — public (Address/Subnet/NIC/Operation) + internal
	//     (InternalAddressService). Оба предъявляют cfg.MTLS.VPC (vpc Service `vpc`,
	//     SAN serverHosts=[vpc] покрывает оба порта).
	dialedConns := make(map[string]clients.Conn, 6)
	for _, spec := range peerDialSpecs(cfg) {
		cc, derr := dialOne(spec.name, spec.addr, spec.tls, spec.mtls)
		if derr != nil {
			closeAll(conns, logger)
			return nil, nil, derr
		}
		dialedConns[spec.name] = cc
	}

	iamPublicConn := dialedConns["iam-public"]
	iamInternalConn := dialedConns["iam-internal"]
	geoConn := dialedConns["geo"]
	computeConn := dialedConns["compute"]
	vpcPublicConn := dialedConns["vpc-public"]
	vpcInternalConn := dialedConns["vpc-internal"]

	if iamPublicConn != nil {
		peers.Project = iamclient.NewProjectClient(iamPublicConn)
	}
	if iamInternalConn != nil {
		// Same per-call timeout source as the authz interceptor
		// (check.Options.CheckTimeout below) — handler-side direct Check
		// calls (attach_target_group.go, move.go) run OUTSIDE the
		// interceptor's own bounded ctx, so the client must bound itself.
		peers.Check = iamclient.NewCheckClientWithTimeout(iamInternalConn, cfg.Authz.IAM.RequestTimeout)
		// FGA-proxy: register-drainer applies owner-tuple intents through
		// InternalIAMService.RegisterResource / UnregisterResource (Internal-only
		// :9091). Replaces the former direct WriteCreatorTuple (Issue N5).
		peers.Register = iamclient.NewRegisterResourceClient(iamInternalConn)
		// Учёт числа ресурсов: резолв разрешённых величин у владельца величин.
		// ВНУТРЕННИЙ слушатель — величины админская поверхность, на внешнем их
		// нет и быть не должно (`security.md` §Internal-vs-external).
		peers.Limit = iamclient.NewLimitClient(iamInternalConn)
	}
	// report the per-listener mTLS state of the iam read/authz edges
	// (mirror of the register-drainer fga_register_drainer_started "mtls" log).
	// iam-project (9090, ProjectService.Get) and iam-internal (9091, Check) are
	// the read/authz edges; each enables independently with its own ServerName.
	logger.Info("iam_read_authz_mtls",
		"project_mtls", cfg.MTLS.IAMProject.Enable,
		"project_server_name", cfg.MTLS.IAMProject.ServerName,
		"authz_mtls", cfg.MTLS.IAMRegister.Enable,
		"authz_server_name", cfg.MTLS.IAMRegister.ServerName)

	// RBAC (issue): per-object filtered List. Каждый публичный
	// List<Resource> читает СТРАНИЦУ из своей БД и спрашивает
	// iam.AuthorizeService.BatchCheck (viewer ∪ v_list) про id ЭТОЙ страницы,
	// оставляя только доступные объекты; read==enforce (та же relation, что per-RPC
	// Check на Get), fail-closed. nil → use-case'ы получают unfiltered passthrough
	// (disabled / нет iam conn). Перечисления «все разрешённые id» больше нет — оно
	// упиралось в жёсткий предел прежнего движка прав (1000 объектов на тип в его
	// сторе, без продолжения) и молча прятало собственные ресурсы тенанта
	// (см. internal/authzfilter package-doc).
	// AuthorizeService зарегистрирован и на iam INTERNAL listener
	// (9091) — service→service per-object list-filter ходит по тому же mTLS-edge, что
	// InternalIAMService.Check (reuse iamInternalConn; mTLS — mtls.iam-register). :9091
	// энфорсит CallerPolicy (verified module-cert), аноним fail-closed — authN+authZ на
	// каждом вызове (НЕ public :9090, где сервис→сервис без JWT отклонился бы).
	peers.ListFilter = buildListFilter(cfg, iamInternalConn, logger)

	// kacho-geo — один conn на public listener (RegionService.Get — публичный
	// read-only Geography-справочник). Ребро nlb→geo (kacho-geo) заменило
	// прежнюю region-валидацию через nlb→compute.
	if geoConn != nil {
		peers.Region = geoclient.NewRegionClient(geoConn)
		peers.Zone = geoclient.NewZoneClient(geoConn)
		peers.ZoneRegion = geoclient.NewZoneRegionClient(geoConn)
	}

	// kacho-compute — один conn на public listener (InstanceService.Get —
	// instance-resolve для TargetGroup-таргетов; НЕ geography).
	if computeConn != nil {
		peers.Instance = computeclient.NewInstanceClient(computeConn)
	}

	// kacho-vpc — public (Address/Subnet/NIC/Operation) + internal (InternalAddressService).
	if vpcPublicConn != nil {
		// Subnet-adapter несёт zone→region резолвер (geo) для заполнения
		// denormalised Subnet.RegionID у ZONAL-подсети — placement-coherence
		// region-precheck (ребро nlb→geo). geoConn nil → nil resolver → RegionID
		// ZONAL пуст (region-precheck пропускается, REGIONAL всё равно заполняется).
		var zoneRegion vpcclient.ZoneRegionResolver
		if peers.ZoneRegion != nil {
			zoneRegion = peers.ZoneRegion
		}
		peers.Subnet = vpcclient.NewSubnetClientWithZoneRegion(vpcPublicConn, zoneRegion)
		peers.NetworkInterface = vpcclient.NewNetworkInterfaceClient(vpcPublicConn)
		peers.Address = vpcclient.NewAddressClient(vpcPublicConn)
		peers.SecurityGroup = vpcclient.NewSecurityGroupClient(vpcPublicConn)
	}
	if vpcPublicConn != nil && vpcInternalConn != nil {
		peers.InternalAddress = vpcclient.NewInternalAddressClient(vpcPublicConn, vpcInternalConn)
	}

	return conns, peers, nil
}

// firstNonEmpty — first non-empty string из аргументов; "" если все пусты.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// peerDialDuration — берёт DialDuration из per-peer если задан, иначе
// общий DefDialDuration. Для NLB-stub'а пока используется единый default.
func peerDialDuration(cfg *config.Config) time.Duration {
	if cfg.ExtAPI.DefDialDuration > 0 {
		return cfg.ExtAPI.DefDialDuration
	}
	return 10 * time.Second
}

func closeAll(conns []clients.Conn, logger *slog.Logger) {
	for _, cc := range conns {
		if err := cc.Close(); err != nil {
			logger.Warn("close peer conn", "err", err)
		}
	}
}

// buildListFilter собирает per-object List-filter (RBAC).
// Возвращает nil (→ use-case'ы делают unfiltered project-scoped
// passthrough), если list-filter выключен в конфиге ИЛИ iam conn недоступен
// (graceful start без iam). Иначе — FGAFilter поверх iam.AuthorizeService.BatchCheck
// (conn — iamInternalConn, тот же, которым nlb зовёт InternalIAMService.Check;
// mTLS — через mtls.iam-register). read==enforce (relation viewer), fail-closed
// (FailOpen=false).
//
// Ребро именно ВНУТРЕННЕЕ, и это не деталь: iam-register и iam-project —
// намеренно РАЗНЫЕ поля конфигурации (у листенеров разные dial-host'ы, единый
// ServerName не может быть корректен для обоих), поэтому назвать не то поле в
// godoc или в загрузочном логе значит отправить оператора крутить ручку, которая
// на это соединение не влияет.
func buildListFilter(cfg *config.Config, iamConn clients.Conn, logger *slog.Logger) *authzfilter.Narrower {
	lf := cfg.Authz.ListFilter
	// Выключенный фильтр БОЛЬШЕ НЕ ОЗНАЧАЕТ сквозной проход: сужатель собирается
	// всегда и ОТКАЗЫВАЕТ, пока ему не с кем говорить. Пропуск возможен только
	// объявленным аварийным режимом, и каждое его срабатывание считается.
	breakglass := !lf.Enabled || iamConn == nil
	if breakglass {
		logger.Warn("list_filter_has_no_model",
			"enabled", lf.Enabled, "iam_conn", iamConn != nil, "breakglass", lf.Breakglass)
		iamConn = nil
	}
	f := authzfilter.New(iamConn, authzfilter.Config{
		Timeout:               lf.Timeout,
		CacheTTL:              lf.CacheTTL,
		CacheMaxEntries:       lf.CacheMaxEntries,
		SoftPassOnPeerFailure: lf.FailOpen,
		Breakglass:            breakglass && lf.Breakglass,
	}).WithLogger(logger)
	logger.Info("list_filter_wired",
		// per_call_timeout гейтит ОДИН BatchCheck; operation_budget — потолок всей
		// фильтрации страницы (выводится из per-call и веера). Логируем все три:
		// иначе по конфигу не видно, какое число реально ограничивает запрос.
		"per_call_timeout", lf.Timeout,
		"operation_budget", f.Budget(),
		"worst_case_depth_waves", f.WorstCaseDepth(),
		"cache_ttl", lf.CacheTTL,
		"cache_max_entries", lf.CacheMaxEntries,
		"soft_pass_on_peer_failure", lf.FailOpen,
		"narrows", f.Narrows(),
		// mtls.iam-register — ручка, которая реально закрывает ЭТО соединение
		// (iamInternalConn). mtls.iam-project управляет другим, публичным ребром.
		"iam_authz_mtls", cfg.MTLS.IAMRegister.Enable)
	return f
}
