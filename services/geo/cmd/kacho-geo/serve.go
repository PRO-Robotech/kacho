// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// runServe — composition root.
//
// # Что этот корень БОЛЬШЕ НЕ делает
//
// Он не собирает серверы, не выстраивает цепочку звеньев, не строит карту прав
// и не пишет собственных стражей старта. Всё это переехало в носитель
// (`pkg/servicehost`), и переехало не ради красоты: пока сборка жила здесь,
// каждый из семи сервисов держал СВОЮ, и порядок звеньев совпадал ровно
// настолько, насколько авторы написали одинаковое.
//
// Здесь остаётся то, что действительно принадлежит домену: пул, слой доступа к
// данным, use-cases, разрешитель осиротевших операций, диагностический
// слушатель и ОБЪЯВЛЕНИЕ о себе — дескриптор.
func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	desc, err := describe(cfg, logger)
	if err != nil {
		return err
	}

	// Самоотчёт о посадке — ПОСЛЕ того, как дескриптор принят (конфиг прошёл все
	// отказы, которые являются его свойствами), и ДО подъёма слушателей. Гейт
	// посадки обязан утверждать на этом наблюдаемом факте, а не на хранимом
	// конфиге: правка настроек без переката пода оставляет процесс с прежним
	// окружением, и «под Ready» доказательством посадки не является.
	observability.LogBootPosture(logger, bootPosture(cfg))

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// ── observability: Prometheus-адаптер метрик разрешителя осиротевших
	// операций. Исполнителя длительных операций здесь нет и не заводится:
	// мутации каталога — конфиг-INSERT, операция завершается СИНХРОННО
	// (shared/syncop), поэтому диспетчеризовать в worker нечего, а его ряды
	// вечно стояли бы на нуле и читались как «отказов нет».
	metricsAdapter := metrics.New(buildVersion, buildCommit)

	// ── LRO-стек: общая operations-таблица (corelib) каталога kacho-geo.
	// Admin-мутации Region/Zone пишут строку операции и сразу её финализируют
	// (Operation{done:true}); клиент разворачивает .response, поллить не нужно.
	opsRepo := operations.NewRepo(pool, "kacho_geo")

	// ── use-cases (repo → use-case → handler) ──────────────────────────────
	// CQRS-порты Reader/Writer связываются раздельно (сейчас обе стороны — один
	// pg-adapter поверх primary-pool; read-side можно позже перецепить на
	// read-replica pool, не трогая use-case). errStatus — transport-mapper
	// sentinel→gRPC-status, инжектится из handler-слоя (serviceerr.ToStatus):
	// выбор кода — transport-concern, use-case его не выбирает.
	regionRepo := pg.NewRegionRepo(pool)
	regionUC := region.New(regionRepo, regionRepo, opsRepo, serviceerr.ToStatus)
	zoneRepo := pg.NewZoneRepo(pool)
	zoneUC := zone.New(zoneRepo, zoneRepo, opsRepo, serviceerr.ToStatus)

	// ── durable LRO recovery: доменный resolver + corelib-reconciler поверх
	// schema kacho_geo. Сирота возможна и без асинхронного пути: синхронное
	// завершение пишется ДВУМЯ стейтментами (Create → MarkDone/MarkError), и
	// падение процесса между ними оставляет durable done=false строку. RecoverAll
	// прогоняется ЗДЕСЬ (до приёма трафика) — такие строки разрешаются в терминал
	// по committed-реальности ресурса; периодический Run(ctx) ниже — backstop.
	lroReconciler := startLRORecovery(ctx, pool, regionRepo, zoneRepo, metricsAdapter, logger)
	go lroReconciler.Run(ctx)

	// ── cluster-internal diagnostic-listener (/metrics). Пустой MetricsAddr явно
	// отключает его (back-compat). Ошибка привязки порта видна синхронно здесь;
	// сам Serve крутится на фоновой goroutine и гасится diagShutdown на ctx.Done().
	//
	// Эта поверхность НЕ входит в контур носителя: она не gRPC, у неё другая
	// цепочка, и втягивать её полями общего дескриптора значило бы превратить
	// дескриптор в свалку. Её берёт отдельный профиль той же функции — работа
	// отдельной фазы, и до неё поверхность честно остаётся вне контура.
	diagTask, diagShutdown, err := startDiagnosticListener(cfg.MetricsAddr, metricsAdapter, logger)
	if err != nil {
		return fmt.Errorf("diagnostic listener: %w", err)
	}
	if diagTask != nil {
		go func() {
			if derr := diagTask(); derr != nil {
				logger.Error("diagnostic listener stopped", "err", derr)
			}
		}()
	}
	defer func() { diagShutdown(context.Background()) }()

	opHandler := handler.NewOperationHandler(opsRepo)
	return servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) { registerPublic(reg, regionUC, zoneUC, opHandler) },
		func(reg grpc.ServiceRegistrar) { registerInternal(reg, regionUC, zoneUC, opHandler) },
	)
}

// describe собирает ОБЪЯВЛЕНИЕ сервиса о себе.
//
// Стражей старта здесь нет ни одного — они все живут в конструкторе дескриптора
// и в носителе. Это не перенос ради переноса: до него geo нёс два собственных
// стража (разбор режима и боевая посадка), и оба были написаны заново в каждом
// из семи сервисов, расходясь тем, что именно каждый считает обязательным.
func describe(cfg config.Config, logger *slog.Logger) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_GEO_AUTH_MODE: %w", err)
	}
	// Транспорт ребра решения о доступе строится ЗДЕСЬ, а проверяется
	// конструктором дескриптора — по ответу самого транспорта, а не по ручке.
	// Сборщик на невзведённой ручке отдаёт незашифрованные креды БЕЗ ошибки,
	// поэтому «ручка выглядит как угодно» этой проверке безразлично.
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("geo→iam Check mTLS creds: %w", err)
	}
	publicCreds, err := grpcsrv.TLSServerTransportCreds(cfg.PublicServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(cfg.InternalServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("internal listener tls creds: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-geo",
		Mode:    mode,
		Logger:  logger,

		Forwarders: cfg.TrustedForwarders(),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,

		// Верхняя граница обработки вызова. «Не применимо» у неё нет: вызов без
		// срока держит соединение из ограниченного пула столько, сколько
		// выполняется его запрос, и MaxConns таких вызовов отказывают весь
		// сервис. Величина и её обоснование — у ручки конфигурации.
		HandlingBudget: cfg.HandlingBudget,

		// Срок жизни подписки — изъятие: geo не служит НИ ОДНОГО серверного
		// стрима. Обе его службы отдают единичный ответ (справочник Region/Zone
		// плюс опрос операций), поэтому накрывать сроком нечего.
		//
		// Заявление ИСТЕКАЕТ САМО и судится не памятью автора, а служимым
		// набором: носитель снимает признак стрима с дескрипторов методов у
		// самих серверов (О11). Появится у geo первая подписка — процесс не
		// поднимется и назовёт её метод.
		StreamBudget: servicecontract.NotApplicable[time.Duration](
			"серверных стримов geo не служит: RegionService/ZoneService и их Internal*-пара " +
				"отдают единичный ответ, подписок в домене нет"),

		// Бюджет отказов объявляется ВЕЛИЧИНОЙ, а не изъятием: решение о доступе
		// geo принимает не у себя, а вопросом к kacho-iam, — то есть сетевой
		// сосед, которого шторм отказов может уронить, у него ЕСТЬ. Изъятие
		// («ронять некого») законно только у владельца модели, решающего в своём
		// процессе. Число и почему штатное чтение справочника его не тратит — у
		// ручки конфигурации.
		DenyBudget: servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		DBSSLMode:     cfg.DBSSLMode,
		PublicAddr:    ":" + cfg.GrpcPort,
		InternalAddr:  ":" + cfg.InternalGrpcPort,
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		// ── оси: у geo все четыре пусты, и каждая пустота ОБЪЯСНЕНА ──────────
		//
		// Объяснения здесь не отговорка: каждое из них СУДИТСЯ каталогом прав
		// или соседней осью на каждом старте. Как только у домена появится
		// первая строка нужной полосы, соответствующее заявление станет
		// находкой, и вспоминать о нём никому не придётся.
		Emits: servicecontract.NotApplicable[[]proxytuple.Relation](
			"глобальный справочник оси размещения: Region и Zone — admin-curated каталог " +
				"кластера, пообъектных грантов у него нет by construction, поэтому кортежей " +
				"владельцу прав geo не эмитит"),
		Registers: servicecontract.NotApplicable[[]servicecontract.ObjectType](
			"своих типов объектов модели прав geo не заводит: его admin-CRUD гейтится " +
				"кластерным отношением на singleton, а не пообъектным на Region/Zone"),
		Narrowers: servicecontract.NotApplicable[map[servicecontract.MethodFQN]servicecontract.ListNarrower](
			"ни один метод geo не сужается по правам: публичные Get/List Region и Zone — " +
				"глобальный справочник, который обязан читать каждый аутентифицированный тенант"),
		HideExistence: servicecontract.NotApplicable[map[servicecontract.ObjectType]servicecontract.NotFoundFormat](
			"скрывать нечего: справочник виден всем аутентифицированным целиком, поэтому " +
				"«есть-но-не-твой» у geo не бывает состоянием"),
		Delivery: servicecontract.NotApplicable[servicecontract.DeliveryProvenance](
			"происхождение доставки объявляет тот, кто доставляет; geo не эмитит намерений " +
				"регистрации вовсе (см. Emits)"),

		// Загрузочный гейт мутаций — изъятие, и у него ДВА независимых предмета,
		// каждый из которых сам по себе делает гейт бессмысленным:
		//
		//  1. доставлять нечего — geo не эмитит намерений регистрации (см. Emits),
		//     а гейт отвергает создание именно до подъёма пути доставки. Принести
		//     его сюда — находка, и это отказ старта конструктора дескриптора;
		//  2. отвергать нечего — все мутации geo живут на `InternalRegionService` /
		//     `InternalZoneService`, а под гейт подпадает `/Create` служб, ИМЯ
		//     которых не начинается с `Internal` (servicehost.IsGatedMutation).
		//     То есть даже принесённый гейт не сработал бы ни разу.
		//
		// Заявление ИСТЕКАЕТ САМО по второму предмету: проба
		// `TestGeoServesNoGatedMutation` спрашивает ТЕМ ЖЕ предикатом, которым
		// гейт исполняется, оба зарегистрированных слушателя. Появится у geo
		// тенантское `Create` — проба покраснеет и назовёт метод, и вспоминать об
		// этом изъятии никому не придётся.
		BootGate: servicecontract.NotApplicable[servicecontract.BootGate](
			"очереди регистраций у geo нет (см. Emits), а все его мутации — админ-CRUD каталога " +
				"на Internal*-службах, которые под гейт не подпадают by construction: отвергать " +
				"было бы нечего, и гейт стоял бы проводкой без предмета"),
	})
}

// registerPublic — публичный слушатель :9090: read-only справочник плюс опрос
// операций.
func registerPublic(reg grpc.ServiceRegistrar, regionUC *region.UseCase, zoneUC *zone.UseCase,
	opHandler operationpb.OperationServiceServer) {
	geov1.RegisterRegionServiceServer(reg, handler.NewRegionHandler(regionUC))
	geov1.RegisterZoneServiceServer(reg, handler.NewZoneHandler(zoneUC))
	operationpb.RegisterOperationServiceServer(reg, opHandler)
}

// registerInternal — cluster-internal слушатель :9091: admin-CRUD плюс тот же
// опрос операций.
//
// Admin-CRUD (`InternalRegionService` / `InternalZoneService`) регистрируется
// ТОЛЬКО здесь: `Internal.*` не публикуется на внешнем endpoint. Разделение
// проверяется тестом через `grpc.Server.GetServiceInfo` — регрессия «Internal*
// уехал на public» ловится, а не остаётся на совести обзора.
func registerInternal(reg grpc.ServiceRegistrar, regionUC *region.UseCase, zoneUC *zone.UseCase,
	opHandler operationpb.OperationServiceServer) {
	geov1.RegisterInternalRegionServiceServer(reg, handler.NewInternalRegionHandler(regionUC))
	geov1.RegisterInternalZoneServiceServer(reg, handler.NewInternalZoneHandler(zoneUC))
	operationpb.RegisterOperationServiceServer(reg, opHandler)
}
