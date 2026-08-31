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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/PRO-Robotech/kacho/pkg/authz"
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
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/subscription"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktypebinding"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/handler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/storage/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
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

// subscriptionSubscribeFQN — полное имя общего глагола подписки.
//
// Оно записано строкой, а не выведено из сгенерённого дескриптора служб: носитель
// сверяет проводку с КАТАЛОГОМ ПРАВ, где ключ — та же строка, и вывод её из
// другого источника сделал бы сверку тождественно истинной при расхождении.
const subscriptionSubscribeFQN servicecontract.MethodFQN = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

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

	// Сервер потока изменений собирается ЗДЕСЬ — до подъёма любой поверхности.
	//
	// Раньше, а не рядом с регистрацией: его сборка умеет ОТКАЗАТЬ (величина
	// посадки, о которой никто не сказал), и отказ обязан наступить прежде, чем
	// процесс займёт порты и объявит себя поднявшимся. Отказ возвращается, а не
	// логируется: молчаливое умолчание обнаружилось бы первым запросом в бою.
	subscribeSrv, err := buildSubscriptionServer(cfg, narrower, logger)
	if err != nil {
		return err
	}

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
	// Приёмник читателя величин кеша вердиктов. Объявлен ДО дескриптора: кеш
	// собирает носитель контура, и читателя он отдаёт через поле дескриптора.
	var authzCache authzmetrics.Source

	// Приватный prometheus-реестр. Скрейпится ТОЛЬКО с cluster-internal
	// diagnostic-порта; ServiceMonitor чарта нацелен именно на него.
	//
	// Собирается ДО дескриптора: реестр — поле объявления, и без него дескриптор
	// не принимается. Регистрации коллекторов остаются ниже по тексту — им нужен
	// только сам реестр, а не порядок относительно объявления.
	svcMetrics := metrics.New()

	desc, err := describe(cfg, logger, narrower, pg.NewExistenceProbe(pool), authzCache.Install, svcMetrics.Registerer())
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

	if err = operations.ConfigureDefault(
		operations.WithLogger(logger),
	); err != nil {
		return fmt.Errorf("configure LRO worker: %w", err)
	}
	operations.Start()

	// Величины сужателя выходят из процесса ТОЛЬКО здесь. Полос четыре: одна
	// положительная и три — страница, ушедшая БЕЗ пообъектной проверки. Снимите
	// эту строку — и полосы исчезнут с поверхности, а не станут нулями; ровно это
	// ловит гейт дерева `TestEveryListNarrowConsumerRegistersItsCollector`.
	svcMetrics.RegisterListNarrow(func() listnarrow.Counts { return narrower.Counts() })
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
	svcMetrics.RegisterAuthzCache(map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    authzCache.Cache,
		authzmetrics.LaneNarrow: narrower.CacheStats,
	}, authzCache.Read)

	// ── use-cases (repo → use-case → handler). CQRS reader/writer связываются
	// раздельно (сейчас обе стороны — один pg-adapter). errStatus — transport-
	// mapper sentinel→gRPC, инжектится из handler-слоя (serviceerr.ToStatus). ──
	// Состояние рождения ресурса читается ИЗ ТОГО ЖЕ признака, что и проводка
	// сверщика: вид плоскости данных объявлен — ресурс рождается в намерении, и
	// пригодным его делает сверщик; не объявлен — сверять не с чем, и фиксация
	// записи сама есть готовность.
	//
	// Признак ОДИН на оба решения намеренно. Разведи их — и появится посадка, где
	// ресурс ждёт подтверждения от сверщика, которого никто не запускал: том
	// остаётся создаваемым навсегда при здоровом рапорте сервиса. Kachō —
	// платформа только управляющей плоскости, поэтому «плоскости данных нет» —
	// это штатная посадка, а не неполная.
	// dataPlane — ТОТ ЖЕ признак, из которого поднимается сверщик и берётся
	// состояние рождения ресурса. Одно решение на все три места: разведи их — и
	// появится посадка, где префикс требуется, а объекта, которому он даёт имя,
	// не будет никогда.
	dataPlane := cfg.BlockBackendKind != ""

	readyOnCommit := !dataPlane

	volumeRepo := pg.NewVolumeRepo(pool).
		WithProjectBytesLimit(cfg.ProjectProvisionedBytesLimit).
		WithReadyOnCommit(readyOnCommit)
	snapshotRepo := pg.NewSnapshotRepo(pool).WithReadyOnCommit(readyOnCommit)
	imageRepo := pg.NewImageRepo(pool).WithReadyOnCommit(readyOnCommit)
	diskTypeRepo := pg.NewDiskTypeRepo(pool)
	geoClient := clients.NewGeoClient(geoConn)
	iamClient := clients.NewIAMClient(iamConn)
	// Префикс установки — свойство РАЗВЁРТЫВАНИЯ, не ресурса: он отличает объекты
	// этого облака от объектов соседнего в общем кластере хранилища. Боевой страж
	// старта не пропускает посадку с бэкендом и без префикса.
	volumeUC := volume.New(volumeRepo, volumeRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus).
		WithDataPlane(dataPlane).
		WithInstallPrefix(cfg.BlockBackendInstallPrefix)
	snapshotUC := snapshot.New(snapshotRepo, iamClient, opsRepo, serviceerr.ToStatus).
		WithDataPlane(dataPlane).
		WithInstallPrefix(cfg.BlockBackendInstallPrefix).
		WithGeo(geoClient)
	imageUC := image.New(imageRepo, imageRepo, geoClient, iamClient, opsRepo, serviceerr.ToStatus).
		WithDataPlane(dataPlane).
		WithInstallPrefix(cfg.BlockBackendInstallPrefix)
	diskTypeUC := disktype.New(diskTypeRepo)
	storageBackendRepo := pg.NewStorageBackendRepo(pool)
	diskTypeBindingRepo := pg.NewDiskTypeBindingRepo(pool)
	storageBackendUC := storagebackend.New(storageBackendRepo)
	diskTypeBindingUC := disktypebinding.New(diskTypeBindingRepo, storageBackendRepo)

	volumeUC.WithListFilter(narrower).WithInstanceGate(narrower)
	snapshotUC.WithListFilter(narrower)
	imageUC.WithListFilter(narrower)

	// ── совещательная полоса учёта числа ресурсов (приёмка квот, DoD S4 п.1) ──
	// Величину назначает kacho-iam и разрешает старшинство областей У СЕБЯ
	// (`InternalLimitService.Resolve` на том же внутреннем соединении, которым мы
	// уже спрашиваем права, — нового ребра работа не заводит). Строку учёта
	// заводит владелец типа: ребро «владелец величин → владелец типа» замкнуло бы
	// цикл, запрещённый polyrepo.md.
	//
	// Без соединения с соседом полоса НЕ собирается, и это осознанно: спрашивать
	// величины не у кого. «Полоса не собрана» означает «нет РАННЕГО отказа», а не
	// «нет предела» — место по-прежнему занимает триггер в той же транзакции, что
	// вставка, поэтому исчерпание приезжает отказом операции, а «потолок не
	// назван» остаётся отказом. Молча снять учёт отсутствием соседа нельзя: ровно
	// так контроль и становится мёртвым, оставаясь на вид работающим.
	var quotaHandler *handler.QuotaHandler
	if authzConn != nil {
		limitClient := clients.NewLimitClient(authzConn)
		quotaGuard := quota.NewGuard(pg.NewQuotaRepo(pool), limitClient, iamClient)
		volumeUC.WithQuota(quotaGuard)
		snapshotUC.WithQuota(quotaGuard)
		imageUC.WithQuota(quotaGuard)
		// Чтение квот арендатором — та же полоса, что и ранний отказ: у чтения и
		// у полосы ровно два источника, и они одни и те же.
		quotaHandler = handler.NewQuotaHandler(quotaGuard)

		// Снимок величины обязан ДОГОНЯТЬ авторитет: без тянущего строка,
		// заведённая один раз, живёт со своей величиной вечно, и смена предела
		// администратором не доезжает до проекта никогда. Показывать арендатору
		// такой снимок значило бы громко назвать число, которое не догонит
		// назначенное, — поэтому чтение и тянущий едут вместе.
		stopQuotaSync, qerr := corequota.StartLimitSyncer(
			ctx, pool, limitClient, pg.QuotaSchema, corequota.Config{}, logger)
		if qerr != nil {
			return fmt.Errorf("start quota limit syncer: %w", qerr)
		}
		defer stopQuotaSync()
	} else {
		logger.Warn("resource-count quota advisory band NOT wired (authz.iam-addr empty) — " +
			"the charging trigger still holds the ceiling, but the tenant learns of exhaustion " +
			"from the operation instead of a synchronous refusal, and the limit snapshot will " +
			"NEVER catch up with the authority: an administrator raising or lowering a ceiling " +
			"has no effect on this process")
	}

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

	// ── сверщик плоскости данных ─────────────────────────────────────────────
	//
	// Единственный производитель готовности ресурса: операция фиксирует намерение и
	// завершается, а объявить ресурс готовым вправе только тот, кто УВИДЕЛ объект.
	// Без него ресурс остаётся создаваемым навсегда — поэтому вид бэкенда, объявленный
	// без работоспособного адаптера, роняет старт, а не молча заводит петлю, которая
	// каждый проход берёт работу и ничего не делает.
	if cfg.BlockBackendKind != "" {
		opener := clients.NewBackendOpener(blockBackendFactories(cfg.BlockBackendCallTimeout),
			clients.NewDirCredentials(cfg.BlockBackendCredentialsDir))
		if !opener.Supports(cfg.BlockBackendKind) {
			return fmt.Errorf("storage backend kind %q is configured but this build carries no "+
				"adapter for it (adapters present: %v): every volume registered on it would stay "+
				"in CREATING forever while the service reported itself healthy",
				cfg.BlockBackendKind, opener.Kinds())
		}
		dataPlane := reconciler.New(reconciler.NewStore(pool), opener, reconciler.Config{
			Interval:    cfg.BlockBackendReconcileInterval,
			Batch:       cfg.BlockBackendReconcileBatch,
			CallTimeout: cfg.BlockBackendCallTimeout,
			Logger:      logger,
		})
		go dataPlane.Run(ctx)
		logger.Info("storage data-plane reconciler started",
			"kind", cfg.BlockBackendKind, "interval", cfg.BlockBackendReconcileInterval,
			"batch", cfg.BlockBackendReconcileBatch)
	} else {
		// Названо вслух: без плоскости данных ресурсы остаются control-plane-записями.
		// Создать их при этом нельзя — класс не предлагается без действующей ревизии
		// привязки, а её нет без бэкенда, — поэтому «создаваемый навсегда» невозможен.
		logger.Info("storage data-plane reconciler is not started: no block backend configured")
	}

	// ── cluster-internal diagnostic HTTP (/healthz, /metrics). Пустой addr отключает. ──
	//
	// Эта поверхность НЕ входит в контур носителя: она не gRPC, у неё другая
	// цепочка, и втягивать её полями общего дескриптора значило бы превратить
	// дескриптор в свалку. До отдельной фазы она честно остаётся вне контура.
	// Готовность СТРОИТСЯ из именованных зависимостей и отдаётся отдельным путём
	// от живости; чарт пробирует именно её.
	healthAgg := health.New(buildReadinessCheckers(pool, authzConn))
	// Гашение переводит готовность в 503 ДО остановки слушателей: kubelet
	// перестаёт слать трафик, пока текущие вызовы дорабатывают.
	go func() {
		<-ctx.Done()
		healthAgg.SetShuttingDown()
	}()
	diagDesc, err := describeDiagnosticSurface(cfg.MetricsAddr, svcMetrics, healthAgg, desc.Spec().Mode, logger)
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

	opHandler := operationspb.NewHandler(opsRepo)
	serveErr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) {
			registerPublic(reg, volumeUC, snapshotUC, imageUC, diskTypeUC, quotaHandler, opHandler)
		},
		func(reg grpc.ServiceRegistrar) {
			registerInternal(reg, volumeUC, imageUC, diskTypeUC, storageBackendUC,
				diskTypeBindingUC, opHandler, subscribeSrv)
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
	authzObserve func(read func() authz.Metrics),
	metricsReg prometheus.Registerer,
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

	// Потолок темпа и одновременности НА ВЫЗЫВАЮЩЕГО: величины посадки там, где
	// она их назвала, и пол платформы там, где молчит. Сборка стоит ДО
	// дескриптора намеренно — негодный набор обязан назвать СЛУШАТЕЛЯ, а не
	// приехать в общий список находок безымянным.
	admission, err := servicecontract.AdmissionFromPosture(cfg.AdmissionPublic, cfg.AdmissionInternal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_STORAGE_ADMISSION_*: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-storage",
		Mode:    mode,
		Logger:  logger,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,
		// Приёмник величин кеша вердиктов: носитель строит кеш, а
		// диагностическую поверхность держит этот корень, и величины переходят
		// границу только здесь. Без него доля попаданий не выходит из процесса,
		// и «сколько даёт кеш» остаётся непроверяемым в обе стороны.
		AuthzObserve: authzObserve,

		// Реестр приходит из корня по той же причине: серии задержки заводит
		// носитель своими руками, а поверхность, которую скребут, держит корень.
		// Разбор решения — у `servicecontract.Spec.Metrics`.
		Metrics: metricsReg,

		// Верхняя граница обработки вызова. «Не применимо» у неё нет: вызов без
		// срока держит соединение из ограниченного пула столько, сколько
		// выполняется его запрос, и MaxConns таких вызовов отказывают весь сервис.
		// Величина и её обоснование — у ручки конфигурации.
		HandlingBudget: cfg.HandlingBudget,

		// Срок жизни подписки — ВЕЛИЧИНА.
		//
		// Здесь стояло изъятие «серверных стримов storage не служит», и оно было
		// верным ровно до этой правки: подписка на изменения ресурсов
		// (`pkg/subscription`, внутренний слушатель) отдаёт поток событий. Изъятие
		// самоистекает намеренно — заявление судится СЛУЖИМЫМ набором, а не
		// памятью автора: носитель снимает признак стрима с дескрипторов методов у
		// самих серверов (О11) и уронил бы старт, назвав метод подписки поимённо.
		//
		// Величина и почему она обязана превосходить границу обработки одиночного
		// вызова — у ручки конфигурации.
		StreamBudget: servicecontract.Value(cfg.SubscriptionStreamBudget),

		// Бюджет отказов объявляется ВЕЛИЧИНОЙ, а не изъятием: решение о доступе
		// storage принимает не у себя, а вопросом к kacho-iam, — то есть сетевой
		// сосед, которого шторм отказов может уронить, у него ЕСТЬ, и на том же
		// соединении живут пообъектный сужатель и регистрация владельца. Изъятие
		// («ронять некого») законно только у владельца модели, решающего в своём
		// процессе. Число и почему штатное чтение его не тратит — у ручки
		// конфигурации.
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
		// Сужателей ДВА, и объект у них ОДИН И ТОТ ЖЕ: за глаголом подписки, как и
		// за перечнем привязок, нет пообъектной проверки на крае, поэтому
		// откатываться не на что, а второй экземпляр означал бы, что поток сужается
		// не тем, чем сужаются списки.
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			listAttachmentsMethod:    narrower,
			subscriptionSubscribeFQN: narrower,
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
	quotaHandler *handler.QuotaHandler,
	opHandler operationpb.OperationServiceServer,
) {
	storagev1.RegisterVolumeServiceServer(reg, handler.NewVolumeHandler(volumeUC))
	storagev1.RegisterSnapshotServiceServer(reg, handler.NewSnapshotHandler(snapshotUC))
	storagev1.RegisterImageServiceServer(reg, handler.NewImageHandler(imageUC))
	storagev1.RegisterDiskTypeServiceServer(reg, handler.NewDiskTypeHandler(diskTypeUC))
	// Чтение квот выставляется, ТОЛЬКО когда полоса учёта собрана. Иначе метод
	// отвечал бы пустым набором на каждый запрос — то есть «квот нет», ровно то
	// утверждение, которое контракт запрещает делать. Незарегистрированный метод
	// отвечает `Unimplemented`, и это честно: возможности здесь действительно нет.
	if quotaHandler != nil {
		storagev1.RegisterQuotaServiceServer(reg, quotaHandler)
	}
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
	storageBackendUC *storagebackend.UseCase,
	diskTypeBindingUC *disktypebinding.UseCase,
	opHandler operationpb.OperationServiceServer,
	subscribe subscriptionv1.InternalSubscriptionServiceServer,
) {
	storagev1.RegisterInternalVolumeServiceServer(reg, handler.NewInternalVolumeHandler(volumeUC))
	storagev1.RegisterInternalImageServiceServer(reg, handler.NewInternalImageHandler(imageUC))
	storagev1.RegisterInternalDiskTypeServiceServer(reg, handler.NewInternalDiskTypeHandler(diskTypeUC))
	storagev1.RegisterInternalStorageBackendServiceServer(reg,
		handler.NewInternalStorageBackendHandler(storageBackendUC))
	storagev1.RegisterInternalDiskTypeBindingServiceServer(reg,
		handler.NewInternalDiskTypeBindingHandler(diskTypeBindingUC))
	operationpb.RegisterOperationServiceServer(reg, opHandler)

	// Поток изменений — ОБЩИЙ сервер (`pkg/subscription`), а не своя обёртка
	// вокруг него: владелец регистрирует его самого. Регистрация безусловна —
	// собирает сервер композиционный корень, и его сборка умеет ОТКАЗАТЬ, поэтому
	// до сюда нулевой указатель не доходит. Условная регистрация означала бы, что
	// подписка тихо отсутствует у процесса, чей дескриптор объявил ей срок жизни.
	//
	// ТОЛЬКО на внутреннем слушателе (ban #6): наружу поток проецирует край, у
	// которого своя ручка и свой периметр.
	subscriptionv1.RegisterInternalSubscriptionServiceServer(reg, subscribe)
}

// buildSubscriptionServer собирает ОБЩИЙ сервер потока изменений для журнала
// storage.
//
// Владелец приносит сюда ЖУРНАЛ и величины ПОСАДКИ — и ничего больше: курсор,
// граница устоявшегося, пределы, сужение по правам и порядок отказов принадлежат
// общему серверу. Появись здесь возможность принести своё вместо любого из них,
// механизм перестал бы быть общим, оставшись общим по имени.
//
// Сужатель — ТОТ ЖЕ объект, что сужает страницы списков: за глаголом подписки нет
// пообъектной проверки на крае (он `scope_filtered`), поэтому откатываться не на
// что, а второй экземпляр означал бы, что поток сужается не тем, чем сужаются
// списки.
//
// Отказ возвращается, а не логируется: величина посадки, о которой никто не
// сказал, не должна обнаруживаться первым запросом в бою.
func buildSubscriptionServer(
	cfg config.Config,
	listFilter *authzfilter.Narrower,
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
// подъёма поверхности, чтобы то, ЧТО отдаётся, можно было проверить без сети:
// расхождение между объявленным в чарте скрейпом и реально обслуживаемым путём
// иначе замечается только на живом Prometheus (см. diagnostic_metrics_test.go).
//
// Живость и готовность отдаются РАЗНЫМИ обработчиками. Прежде `/healthz` был
// здесь один и отвечал безусловным 200, а чарт пробировал им СЛОТ ГОТОВНОСТИ:
// под объявлял себя готовым, ничего не зная ни о базе, ни о канале к владельцу
// прав, — то есть kubelet начинал слать трафик до того, как сервис был способен
// ответить. Безусловные 200 остаются законным ответом ровно на один вопрос:
// «жив ли процесс».
func diagnosticMux(m *metrics.Metrics, agg *health.Aggregator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())
	mux.Handle("GET /metrics", m.Handler())
	return mux
}

// buildReadinessCheckers — ИМЕНОВАННЫЕ зависимости, без которых storage
// обслуживать не может. Живость их НЕ включает намеренно: блип зависимости
// обязан снять под из ротации, а не перезапустить процесс.
//
//   - database — собственный пул; без него не отвечает ни одно чтение;
//   - lro-worker — исполнитель операций поднят и готов забирать незавершённые.
//     Без него каждая мутация принимается и не исполняется НИКОГДА, а клиент
//     видит принятую операцию, которая не завершится;
//   - iam-authz — канал к владельцу прав. Включается ТОЛЬКО когда он
//     сконфигурирован: объявить зависимость, которой на этой посадке нет,
//     значило бы держать под вечно неготовым по причине, которой не существует.
func buildReadinessCheckers(pool *pgxpool.Pool, authzConn *grpc.ClientConn) []health.Checker {
	checkers := []health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: "lro-worker", Check: func(context.Context) error {
			if operations.Ready() {
				return nil
			}
			return errLROWorkerDown
		}},
	}
	if authzConn != nil {
		checkers = append(checkers, health.Checker{Name: "iam-authz", Check: func(context.Context) error {
			return authzConnHealth(authzConn)
		}})
	}
	return checkers
}

// authzConnHealth — состояние соединения с владельцем прав. Shutdown — отказ;
// прочие состояния (Idle/Connecting/Ready/TransientFailure) считаются рабочими:
// gRPC переподключается лениво, и снятие пода из ротации на кратком
// TransientFailure дало бы ложный флап на каждом перекате соседа.
func authzConnHealth(conn *grpc.ClientConn) error {
	if conn.GetState() == connectivity.Shutdown {
		return errIAMConnShutdown
	}
	return nil
}

// Сентинелы чекеров: причина «не готов» в ответе `/readyz` без раскрытия
// внутренних деталей наружу.
var (
	errLROWorkerDown   = errors.New("LRO dispatcher loop not running")
	errIAMConnShutdown = errors.New("connection to kacho-iam is shut down")
)

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
// поверхности (/healthz, /readyz, /metrics).
//
// Сервера, привязки порта и гашения здесь нет: их держит профиль не-gRPC
// поверхности (`pkg/servicehost.ServeSurface`). Корень отвечает на четыре
// вопроса — что обслуживается, откуда досягаемо, чем аутентифицировано и на
// сколько рассчитан каждый срок.
//
// Пустой эндпоинт перестал выключать поверхность МОЛЧА: теперь это объявленное
// выключение с причиной, и причина едет в журнал.
func describeDiagnosticSurface(endpoint string, m *metrics.Metrics, agg *health.Aggregator,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_STORAGE_METRICS_ADDR не задан профилем развёртывания: ни скрейпа, ни проб " +
				"живости и готовности на этой посадке нет — kubelet не узнает о неготовности " +
				"зависимостей ничего")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-storage",
		Name:    "диагностика (/healthz, /readyz, /metrics)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: diagnosticMux(m, agg),

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service и несёт " +
				"счётчики процесса, признак живости и имена зависимостей — ни секретов, ни " +
				"данных арендатора, ни " +
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
