// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
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

// listAttachmentsMethod — единственный метод storage, чья авторизация переехала на
// уровень ДАННЫХ (`scope_filtered` в каталоге прав): инстансы называет вызывающий,
// а ответ касается томов с разными владельцами, поэтому единичного объекта, про
// который можно спросить заранее, у него нет by construction.
//
// Имя стоит здесь ОДИН раз и уезжает проводкой сужателя в дескриптор. Перечня
// сужаемых методов дескриптор не объявляет: его даёт каталог, и носитель сверяет
// проводку с ним в обе стороны (О3/О4) — потерянная проводка и лишняя одинаково
// роняют старт.
const listAttachmentsMethod servicecontract.MethodFQN = "/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments"

// runServe — composition root.
//
// # Что этот корень БОЛЬШЕ НЕ делает
//
// Он не собирает серверы, не выстраивает цепочку звеньев, не строит карту прав и
// не пишет собственных стражей старта на то, что знает носитель. Всё это переехало
// в `pkg/servicehost`, и переехало не ради красоты: пока сборка жила здесь, каждый
// из семи сервисов держал СВОЮ, и порядок звеньев совпадал ровно настолько,
// насколько авторы написали одинаковое. У storage расхождение было наблюдаемым:
// журнал доступа стоял только на unary-цепочке, поэтому стрим-вызов не оставлял в
// нём ни строки. Носитель ведёт журнал на обеих полосах.
//
// Здесь остаётся то, что действительно принадлежит домену: пул, слой доступа к
// данным, use-cases, соседи, сужатель видимости, дренаж регистраций, разрешитель
// осиротевших операций, диагностический слушатель и ОБЪЯВЛЕНИЕ о себе — дескриптор.
func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	// ── остаток собственного стража: измерения, которых носитель не знает ──
	// Транспорт исходящих рёбер к geo и iam, включённость сужателя и его
	// degraded-ручка. Всё прочее (режим, круг отправителей, sslmode, транспорт
	// обоих слушателей, ребро решения о доступе) судит конструктор дескриптора.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("insecure configuration refused: %w", err)
	}

	// ── peer-клиенты (runtime cross-domain edges) ─────────────────────────
	// Поднимаются ДО дескриптора: `grpc.NewClient` не блокирует до первого RPC,
	// поэтому это лишь сборка креденшелов, а сужатель, который уезжает в
	// дескриптор проводкой, обязан быть ТЕМ ЖЕ объектом, что провязан в use-cases.
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
	// ── соединение с моделью прав ─────────────────────────────────────────
	// Одно на три предмета: вопрос о правах (его задаёт носитель по ребру,
	// объявленному дескриптором), пообъектное сужение видимости и регистрация
	// владельца ресурса. Дескриптор получает СВОЙ транспорт того же адреса —
	// собирается он в describe(), где его и судит конструктор.
	authzConn, err := dialPeer(cfg.AuthZIAMGRPCAddr, cfg.IAMClientMTLS, logger, "iam-authz")
	if err != nil {
		return err
	}
	if authzConn != nil {
		defer authzConn.Close()
	}

	// ── пообъектный сужатель публичного List (анти-over-show) ─────────────
	// Per-RPC Check гейтит List на project-tier `viewer` — «вправе ли листать ЭТОТ
	// проект». Сужение страницы до объектов, на которые есть грант (пообъектный
	// `viewer` батчем по прочитанной странице — то же отношение, что энфорсит Get),
	// делает ТОЛЬКО этот сужатель. Без него любой член проекта видел КАЖДЫЙ
	// том/снимок/образ проекта.
	//
	// Тот же сужатель отвечает и на ВТОРОЙ вопрос запросов Attach/Detach — про
	// ИНСТАНС, в чей набор привязок пишется строка (`v_update` / `v_update ∪
	// v_delete` на `compute_instance`, см. volume.requireInstanceControl). Это не
	// «фильтр списка», а гейт мутации, поэтому порт отдельный
	// (authzfilter.ObjectGate) и мягкий проход к нему не применяется; вопрос идёт в
	// ту же модель, вызова в compute не происходит, ацикличность держится.
	//
	// Нулевой указатель раскладывается в НУЛЕВЫЕ интерфейсы намеренно: typed-nil в
	// интерфейсе не равен nil, и проверки вида `filter == nil` у потребителей молча
	// перестали бы срабатывать.
	narrower := buildListFilter(cfg, authzConn, logger)

	// Самоотчёт о посадке — ПОСЛЕ приёма дескриптора и ДО подъёма слушателей.
	// Гейт посадки обязан утверждать на этом наблюдаемом факте, а не на хранимом
	// конфиге: правка настроек без переката пода оставляет процесс с прежним
	// окружением, и «под Ready» доказательством посадки не является.
	// ── БД + LRO-стек ─────────────────────────────────────────────────────
	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// ── объявление о себе ─────────────────────────────────────────────────
	//
	// Дескриптор собирается СРАЗУ ПОСЛЕ пула и ДО фоновых проходов. Прежняя
	// редакция строила его раньше пула, объясняя это тем, что отказ обязан прийти
	// «до того, как процесс открыл пул»; после того как объявлено скрытие
	// существования, так больше нельзя: порт сверки живёт НА пуле, и собрать его
	// без пула означало бы принести порт, который на первом же вопросе ответит
	// «соединения нет». Открытие пула обратимо (`defer` выше) и дешевле ложной
	// сверки; существенная половина прежнего довода — «до фоновых проходов» —
	// сохранена, а они начинаются ниже.
	//
	// Проводка сужателя уезжает ТЕМ ЖЕ объектом, что провязан в use-cases ниже:
	// иначе носитель сверял бы с каталогом не то, что реально сужает страницу.
	desc, err := describe(cfg, logger, narrower, pg.NewExistenceProbe(pool))
	if err != nil {
		return err
	}

	// Самоотчёт о посадке — ПОСЛЕ того, как дескриптор принят (конфиг прошёл все
	// отказы, которые являются его свойствами), и ДО подъёма слушателей. Гейт
	// посадки обязан утверждать на этом наблюдаемом факте, а не на хранимом
	// конфиге: правка настроек без переката пода оставляет процесс с прежним
	// окружением, и «под Ready» доказательством посадки не является.
	observability.LogBootPosture(logger, bootPosture(cfg))
	if cfg.AuthMode == "dev" && (cfg.DBSSLMode == "" || cfg.DBSSLMode == "disable") {
		logger.Warn("KACHO_STORAGE_DB_SSLMODE=disable — DB plaintext (dev only; never on a deployed stand)")
	}

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
	geoClient := clients.NewGeoClient(geoConn)
	iamClient := clients.NewIAMClient(iamConn)
	volumeUC := volume.New(volumeRepo, volumeRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus)
	snapshotUC := snapshot.New(snapshotRepo, iamClient, opsRepo, serviceerr.ToStatus)
	imageUC := image.New(imageRepo, imageRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus)
	diskTypeUC := disktype.New(diskTypeRepo)

	volumeUC.WithListFilter(narrower).WithInstanceGate(narrower)
	snapshotUC.WithListFilter(narrower)
	imageUC.WithListFilter(narrower)

	// ── FGA owner-tuple register-drainer + sync-registrar (SEC-D, анти-BOLA) ──
	// Volume/Snapshot/Image Create/Delete эмитят register/unregister-intent в
	// kacho_storage.fga_register_outbox (writer-TX). register-drainer применяет их
	// через kacho-iam RegisterResource/UnregisterResource (тот же :9091 mTLS-conn,
	// что и вопрос о правах — RegisterResource Internal-only, ban #6). sync-registrar
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
		// Форма доставки — ОДНА на все сервисы (pkg/ownerregister): у storage больше
		// нет своего регистратора, потому что своего в нём ничего и не было —
		// только копия, разошедшаяся с соседями по маркеру версии.
		syncRegistrar, rerr := ownerregister.New(iamv1.NewInternalIAMServiceClient(authzConn))
		if rerr != nil {
			return fmt.Errorf("собрать синхронный registrar: %w", rerr)
		}
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
	// ни разу. Не зависит ни от kacho-iam, ни от дренажа регистраций: это сверка со
	// СВОЕЙ БД. См. recovery.go.
	lroReconciler := startLRORecovery(ctx, pool, operationresolver.Readers{
		Volume:   volumeRepo,
		Snapshot: snapshotRepo,
		Image:    imageRepo,
	}, logger)
	go lroReconciler.Run(ctx)

	// ── cluster-internal diagnostic HTTP (/healthz, /metrics). Пустой addr отключает. ──
	//
	// Эта поверхность НЕ входит в контур носителя: она не gRPC, у неё другая
	// цепочка, и втягивать её полями общего дескриптора значило бы превратить
	// дескриптор в свалку. До отдельной фазы она честно остаётся вне контура.
	diagDesc, err := describeDiagnosticSurface(cfg.MetricsAddr, svcMetrics, desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	// Собственный контекст поверхности: гасить её надо ПОСЛЕ обоих gRPC-слушателей
	// и после дренажа исполнителей операций — иначе проба живости и скрейп исчезают
	// раньше, чем процесс закончил останавливаться.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	// Привязка порта синхронна: занятый адрес — ошибка посадки, и процесс не
	// вправе объявить себя поднявшимся, оставив её на код возврата.
	waitDiag, diagErr := servicehost.ServeSurface(diagCtx, diagDesc)
	if diagErr != nil {
		stopDiag()
		return fmt.Errorf("диагностическая поверхность: %w", diagErr)
	}

	opHandler := handler.NewOperationHandler(opsRepo)
	serveErr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) {
			registerPublic(reg, volumeUC, snapshotUC, imageUC, diskTypeUC, opHandler)
		},
		func(reg grpc.ServiceRegistrar) {
			registerInternal(reg, volumeUC, imageUC, diskTypeUC, opHandler)
		},
	)

	// Дренаж in-flight LRO — ПОСЛЕ того, как оба слушателя погашены: новые мутации
	// уже не принимаются, а начатые обязаны дописать свой исход. Без него строка
	// операции остаётся done=false навсегда, и клиент, поллящий её, не узнаёт
	// исхода ни разу.
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), lroDrainTimeout)
	defer cancelDrain()
	if werr := operations.Wait(drainCtx); werr != nil {
		logger.Warn("LRO workers did not finish before shutdown timeout",
			"err", werr, "active", operations.Active())
	}

	// Диагностическая поверхность гасится ПОСЛЕДНЕЙ и её возврата ЖДУТ: профиль
	// возвращается только после того, как порт освобождён, а без ожидания процесс
	// завершался бы, оставляя это неизвестным. Прежде здесь стоял `defer` с
	// контекстом БЕЗ срока — то есть остановка ждала последнего скрейпа
	// неограниченно.
	stopDiag()
	if derr := waitDiag(); derr != nil {
		logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
		if serveErr == nil {
			serveErr = derr
		}
	}
	return serveErr
}

// describe собирает ОБЪЯВЛЕНИЕ сервиса о себе.
//
// Стражей старта здесь нет ни одного — они живут в конструкторе дескриптора и в
// носителе. Остаток собственного стража storage (`config.Validate`) судит только
// то, чего носитель не знает: транспорт исходящих рёбер к geo и iam, включённость
// сужателя и его degraded-ручку.
//
// Сужатель передаётся АРГУМЕНТОМ, а не собирается здесь заново: проводка,
// объявленная дескриптором, обязана быть тем же объектом, что сужает страницу в
// use-cases. Собери её здесь второй раз — носитель сверил бы с каталогом
// экземпляр, которого на пути запроса нет, и «проводка есть» перестало бы
// означать «страница сужается».
// hideExistenceForms — формы отказа для типов storage, которые рантайм скрывает.
//
// Взяты у производителя (`authz.OwnerNotFoundFormat`), а не выписаны: носитель
// сверяет объявленное с тем, чем звено РЕАЛЬНО отвечает, и расхождение роняет
// старт. Собственная копия формы прошла бы конструктор и упала бы на подъёме —
// то есть позже и дороже.
//
// Отсутствие типа в таблице здесь — не «пропустить», а паника композиционного
// корня: значит перечень типов разошёлся с таблицей промахов, и поднимать процесс
// с неполной картой скрытия нельзя.
func hideExistenceForms() map[servicecontract.ObjectType]servicecontract.NotFoundFormat {
	out := map[servicecontract.ObjectType]servicecontract.NotFoundFormat{}
	for _, ot := range []string{"storage_volume", "storage_snapshot", "storage_image"} {
		form, ok := authz.OwnerNotFoundFormat(ot)
		if !ok {
			panic("storage: у типа " + ot + " нет голоса владельца в таблице промахов — " +
				"перечень скрывающих типов разошёлся с pkg/authz")
		}
		out[servicecontract.ObjectType(ot)] = servicecontract.NotFoundFormat(form)
	}
	return out
}

func describe(
	cfg config.Config,
	logger *slog.Logger,
	narrower servicecontract.ListNarrower,
	existence servicecontract.ExistenceProbe,
) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_STORAGE_AUTH_MODE: %w", err)
	}
	// Транспорт ребра решения о доступе строится ЗДЕСЬ, а проверяется конструктором
	// дескриптора — по ответу самого транспорта, а не по ручке. Сборщик на
	// невзведённой ручке отдаёт незашифрованные креды БЕЗ ошибки, поэтому «ручка
	// выглядит как угодно» этой проверке безразлично.
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMClientMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("storage→iam Check mTLS creds: %w", err)
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
		Service: "kacho-storage",
		Mode:    mode,
		Logger:  logger,

		Forwarders: cfg.TrustedForwarders(),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,

		// Верхняя граница обработки вызова. «Не применимо» у неё нет: вызов без
		// срока держит соединение из ограниченного пула столько, сколько
		// выполняется его запрос, и MaxConns таких вызовов отказывают весь сервис.
		// Величина и её обоснование — у ручки конфигурации.
		HandlingBudget: cfg.HandlingBudget,

		// Срок жизни подписки — изъятие: storage не служит НИ ОДНОГО серверного
		// стрима. Все его RPC (тенантские Volume/Snapshot/Image/DiskType, опрос
		// операций, внутренняя привязка тома) отдают единичный ответ, поэтому
		// накрывать сроком нечего.
		//
		// Заявление судится СЛУЖИМЫМ набором, а не памятью автора: носитель
		// снимает признак стрима с дескрипторов методов у самих серверов (О11).
		// Появится у storage первая подписка — процесс не поднимется и назовёт
		// её метод.
		StreamBudget: servicecontract.NotApplicable[time.Duration](
			"серверных стримов storage не служит: ни один его метод не отдаёт поток событий, " +
				"все ответы единичные"),

		// Бюджет отказов объявляется ВЕЛИЧИНОЙ, а не изъятием: решение о доступе
		// storage принимает не у себя, а вопросом к kacho-iam, — то есть сетевой
		// сосед, которого шторм отказов может уронить, у него ЕСТЬ, и на том же
		// соединении живут пообъектный сужатель и регистрация владельца. Изъятие
		// («ронять некого») законно только у владельца модели, решающего в своём
		// процессе. Число и почему штатное чтение его не тратит — у ручки
		// конфигурации.
		DenyBudget: servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		DBSSLMode:     cfg.DBSSLMode,
		PublicAddr:    ":" + cfg.GrpcPort,
		InternalAddr:  ":" + cfg.InternalGrpcPort,
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		// ── оси ──────────────────────────────────────────────────────────────
		//
		// Эмиссия: одно отношение иерархии владения — `project:<id> #project
		// @storage_<res>:<id>`. Имя берётся у ПРИНИМАЮЩЕЙ стороны
		// (`pkg/authz/proxytuple`), которая владеет закрытым набором принимаемых
		// отношений: второе написание чужого закрытого набора расходится молча, и
		// расходится там, где это не видно — отказ в правах дренаж читает как
		// временный, и очередь встаёт головой партиции навсегда.
		Emits: servicecontract.Value([]proxytuple.Relation{proxytuple.RelationProject}),

		// Регистрируемые типы — три ресурса storage, у каждого свой пообъектный
		// тип в модели прав. `volume_attachments` своего типа не заводит: право
		// видеть привязку вытекает из права на ИНСТАНС и на ТОМ, а не из
		// собственного объекта.
		Registers: servicecontract.Value([]servicecontract.ObjectType{
			"storage_volume", "storage_snapshot", "storage_image",
		}),

		// Проводка сужателя — ровно на тот метод, который каталог объявляет
		// сужаемым. Перечень сужаемых методов здесь НЕ объявляется: его даёт
		// каталог, и носитель сверяет проводку с ним в обе стороны.
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			listAttachmentsMethod: narrower,
		}),

		// Скрытия существования у storage нет: ни одна строка каталога его домена
		// не несёт этой полосы, поэтому «есть-но-не твой» отвечает обычным отказом
		// в доступе, а не голосом владельца. Заявление СУДИТСЯ каталогом на каждом
		// старте (О5): появится первая такая строка — оно станет находкой, и
		// вспоминать о нём никому не придётся.
		// Скрытие существования у storage ЕСТЬ, хотя ни один его RPC не помечен
		// аннотацией: рантайм скрывает и ПО ФОРМЕ — чтение объекта глаголом `v_get`
		// у типа, за который есть чем говорить. Такими оказываются три `/Get`
		// (Volume, Snapshot, Image), и у всех трёх типов голос владельца в таблице
		// промахов есть.
		//
		// Прежняя редакция объявляла ось неприменимой словами «отказ на чужом
		// объекте приходит отказом в доступе, а не промахом владельца». Вторая
		// половина была ЛОЖНОЙ, и заявление проходило лишь потому, что судья читал
		// аннотацию, а рантайм — форму. Сведи предикаты — и оно падает тремя
		// находками, по одной на тип.
		//
		// Формы НЕ выписаны здесь, а взяты у того, кто ими отвечает: выписанная
		// копия разошлась бы с производителем ровно тем способом, который отказ О5
		// и ловит, — и дескриптор бы это расхождение узаконил.
		HideExistence: servicecontract.Value(hideExistenceForms()),

		// Порт сверки существования обязателен, раз скрытие объявлено выше: без него
		// отказ «есть, но не твой» и промах «нет такого» пришли бы одной формой, и
		// различить их снаружи стало бы невозможно — то есть скрытие превратилось бы
		// в оракул наоборот. Конструктор дескриптора это и требует.
		Existence: existence,

		// Происхождение доставки — writer-транзакция: намерение регистрации
		// пишется строкой fga_register_outbox в ТОЙ ЖЕ транзакции, что вставляет
		// ресурс (один commit, без dual-write). Самая узкая семантика из
		// существующих: происхождение доказано записью, а не выведено из часов в
		// момент доставки.
		Delivery: servicecontract.Value(servicecontract.DeliveryWriterTransaction),

		// Загрузочный гейт мутаций — изъятие, и предикат его снятия ВНЕШНИЙ:
		// гейта (`pkg/outbox/bootgate`) в дереве storage нет ни одного вызова.
		// Заявление истекает пробой TestStorageBringsNoBootGateYet, которая
		// спрашивает дерево, а не память автора: появится провязка — проба
		// покраснеет и потребует принести гейт сюда.
		//
		// Почему это не «дыра, которую прикрыли словом»: доставка у storage
		// доказана записью (см. Delivery), поэтому окно «ресурс создан, а
		// намерение не доставлено» ограничено дренажом и подстраховано
		// redrive-проходом, а не разомкнуто. Гейт сузил бы это окно до нуля ценой
		// отказа в создании — решение отдельной фазы, и принимать его молча, по
		// ходу переезда контура, нельзя.
		BootGate: servicecontract.NotApplicable[servicecontract.BootGate](
			"загрузочного гейта мутаций у storage нет: очередь регистраций поднимается дренажом и " +
				"redrive-подстраховкой, а отвергать создание до её подъёма — отдельное продуктовое " +
				"решение, не принятое. Принести гейт, не приняв решения, значило бы отвергать создание " +
				"по причине, которой никто не выбирал"),
	})
}

// registerPublic — публичный слушатель :9090: тенантские Volume/Snapshot/Image/
// DiskType плюс опрос операций.
func registerPublic(
	reg grpc.ServiceRegistrar,
	volumeUC *volume.UseCase,
	snapshotUC *snapshot.UseCase,
	imageUC *image.UseCase,
	diskTypeUC *disktype.UseCase,
	opHandler operationpb.OperationServiceServer,
) {
	storagev1.RegisterVolumeServiceServer(reg, handler.NewVolumeHandler(volumeUC))
	storagev1.RegisterSnapshotServiceServer(reg, handler.NewSnapshotHandler(snapshotUC))
	storagev1.RegisterImageServiceServer(reg, handler.NewImageHandler(imageUC))
	storagev1.RegisterDiskTypeServiceServer(reg, handler.NewDiskTypeHandler(diskTypeUC))
	operationpb.RegisterOperationServiceServer(reg, opHandler)
}

// registerInternal — cluster-internal слушатель :9091: координация привязки томов,
// инфра-проекция образа, admin-CRUD типов дисков плюс тот же опрос операций.
//
// `Internal*` регистрируется ТОЛЬКО здесь: на внешнем endpoint эти службы не
// публикуются (ban #6). Разделение проверяется через `grpc.Server.GetServiceInfo` —
// регрессия «Internal* уехал на public» ловится пробой, а не остаётся на совести
// обзора.
func registerInternal(
	reg grpc.ServiceRegistrar,
	volumeUC *volume.UseCase,
	imageUC *image.UseCase,
	diskTypeUC *disktype.UseCase,
	opHandler operationpb.OperationServiceServer,
) {
	storagev1.RegisterInternalVolumeServiceServer(reg, handler.NewInternalVolumeHandler(volumeUC))
	storagev1.RegisterInternalImageServiceServer(reg, handler.NewInternalImageHandler(imageUC))
	storagev1.RegisterInternalDiskTypeServiceServer(reg, handler.NewInternalDiskTypeHandler(diskTypeUC))
	operationpb.RegisterOperationServiceServer(reg, opHandler)
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

// Сроки диагностической поверхности. Названы константами, а не вписаны в
// объявление: они одинаковы у всех диагностических поверхностей платформы, и
// разъехавшиеся числа читались бы как осознанная разница.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)

// describeDiagnosticSurface — ОБЪЯВЛЕНИЕ cluster-internal диагностической
// поверхности (/healthz, /metrics).
//
// Сервера, привязки порта и гашения здесь нет: их держит профиль не-gRPC
// поверхности (`pkg/servicehost.ServeSurface`). Корень отвечает на четыре
// вопроса — что обслуживается, откуда досягаемо, чем аутентифицировано и на
// сколько рассчитан каждый срок.
//
// Пустой эндпоинт перестал выключать поверхность МОЛЧА: теперь это объявленное
// выключение с причиной, и причина едет в журнал.
func describeDiagnosticSurface(endpoint string, m *metrics.Metrics, mode servicecontract.Mode,
	logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_STORAGE_METRICS_ADDR не задан профилем развёртывания: ни скрейпа, ни пробы " +
				"живости на этой посадке нет")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-storage",
		Name:    "диагностика (/healthz, /metrics)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: diagnosticMux(m),

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service и несёт " +
				"счётчики процесса и признак живости — ни секретов, ни данных арендатора, ни " +
				"сведений о размещении на проводе нет (security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}

// buildListFilter собирает пообъектный сужатель видимости (kacho-iam
// AuthorizeService.BatchCheck по id ПРОЧИТАННОЙ страницы).
//
// Возвращается КОНКРЕТНЫЙ тип: один и тот же сужатель обслуживает обе двери
// механизма — видимость страницы и гейт мутации по названному объекту.
//
// Выключенный сужатель НЕ ОЗНАЧАЕТ сквозной проход: он собирается всегда и
// ОТКАЗЫВАЕТ, пока ему не с кем говорить. Пропуск возможен только объявленным
// аварийным режимом, и каждое его срабатывание считается и называется.
//
// Логируются ВСЕ три числа таймингов: per-call дедлайн гейтит ОДИН BatchCheck,
// operation_budget — сужение всей страницы (выводится из per-call и параллелизма),
// worst_case_depth — сколько волн он покрывает. По одному конфигу не видно, какое
// из них реально ограничивает запрос.
func buildListFilter(cfg config.Config, authzConn *grpc.ClientConn, logger *slog.Logger) *authzfilter.Narrower {
	breakglass := !cfg.ListFilterEnabled || authzConn == nil
	var conn grpc.ClientConnInterface
	if !breakglass {
		conn = authzConn
	} else {
		logger.Warn("list filter has no rights model to ask — every list and every object gate "+
			"will REFUSE unless the emergency bypass is armed",
			"enabled", cfg.ListFilterEnabled, "authz_conn", authzConn != nil,
			"breakglass", cfg.ListFilterBreakglass)
	}
	f := authzfilter.New(conn, authzfilter.Config{
		Timeout:               time.Duration(cfg.ListFilterTimeoutMs) * time.Millisecond,
		CacheTTL:              time.Duration(cfg.ListFilterCacheTTLMs) * time.Millisecond,
		CacheMaxEntries:       cfg.ListFilterCacheMaxEntries,
		SoftPassOnPeerFailure: cfg.ListFilterFailOpen,
		Breakglass:            breakglass && cfg.ListFilterBreakglass,
	}).WithLogger(logger)
	logger.Info("list filter wired",
		"iam_authorize_endpoint", cfg.AuthZIAMGRPCAddr,
		"per_call_timeout_ms", cfg.ListFilterTimeoutMs,
		"operation_budget", f.Budget(),
		"worst_case_depth_waves", f.WorstCaseDepth(),
		"cache_ttl_ms", cfg.ListFilterCacheTTLMs,
		"cache_max_entries", cfg.ListFilterCacheMaxEntries,
		"soft_pass_on_peer_failure", cfg.ListFilterFailOpen,
		"narrows", f.Narrows())
	return f
}
