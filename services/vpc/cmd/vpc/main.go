// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/H-BF/corlib/pkg/parallel"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/safeconv"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/retention"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	addressapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/address"
	addresspoolapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	cidrgroupapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/cidrgroup"
	gatewayapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway"
	networkapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	niapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/networkinterface"
	quotaapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/quota"
	routetableapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/routetable"
	sgapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/securitygroup"
	subnetapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/addressref"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/networkinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/nicinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/clients"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto" // регистрирует DTO-трансферы (init); boot-check ниже
	"github.com/PRO-Robotech/kacho/services/vpc/internal/handler"
	vpcmetrics "github.com/PRO-Robotech/kacho/services/vpc/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/cqrsadapter"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// configPathEnv — путь к YAML-конфигу. Пустое значение допустимо (defaults +
// ENV-override). Helm chart выставляет KACHO_VPC_CONFIG_PATH=/etc/kacho-vpc/config.yaml.
const configPathEnv = "KACHO_VPC_CONFIG_PATH"

func main() {
	// kacho-vpc — single-purpose binary: только обслуживает API. Миграции живут в
	// отдельном `cmd/migrator` (cobra-based; сам накат — internal/migratorrun).
	// Subcommand-проверка — в switch ниже.

	cfg, err := config.Load(os.Getenv(configPathEnv))
	if err != nil {
		log.Fatalf("config load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validate: %v", err)
	}
	// S3 boot-guard: data-level list-filter обязан быть включён и резолвим на ЛЮБОЙ
	// развёрнутой посадке, включая dev (core rule #16 стенды не делит). Список
	// ScopeFiltered-методов страж читает из карты САМ: пока его передавали отсюда,
	// пустой аргумент снимал все проверки, и «забыли передать» было неотличимо от
	// «защищать нечего».
	if err := cfg.ValidateListFilter(); err != nil {
		log.Fatalf("config validate (list-filter): %v", err)
	}
	// S5 boot-guard: профиль возможностей исполнителя датаплейна. Контур принимает
	// от арендатора то, что исполнять будет НЕ он (адресные диапазоны, правила со
	// ссылкой на именованный набор, ограничение полосы), и возможностей исполнителя
	// вывести не может — их объявляет посадка. Несущее предусловие — пересечение
	// адресов между арендаторами: диапазоны у vpc уникальны лишь в пределах сети по
	// построению, поэтому боевая посадка, не объявившая изоляцию одинаковых адресов
	// разных арендаторов, здесь и останавливается.
	if err := cfg.ValidateExecutorProfile(); err != nil {
		log.Fatalf("config validate (dataplane executor profile): %v", err)
	}
	// S6 boot-guard: перечень адресных диапазонов, которые платформа держит ЗА
	// СОБОЙ (служебные адреса узлов, адреса служб внутри подсети, точка получения
	// метаданных экземпляра). Подсеть арендатора поверх такого диапазона проходит
	// все проверки контура и не работает, причём симптом выглядит сетевым. Перечень
	// зависит от посадки, поэтому объявляется настройкой — а у настройки есть
	// состояние «не задана», и оно НЕ безобидно: пустой перечень означает «не
	// сужаем», а не «нечего сужать», то есть проверка на пути запроса исполняется на
	// каждом создании подсети и не отвергает ничего. Боевая посадка здесь и
	// останавливается.
	if err := cfg.ValidateReservedPrefixes(); err != nil {
		log.Fatalf("config validate (dataplane reserved prefixes): %v", err)
	}
	// S7 boot-guard: величины допуска запросов на каждом листенере. Стоимость
	// запроса здесь высокая по построению — три строки в базе на мутацию, до
	// полной страницы объектов с проверкой прав партиями на чтение, — поэтому
	// неограниченный темп бьёт не в сеть, а в базу, и один вызывающий занимает
	// процесс, обслуживающий всех. Величина исполняется ведром В ПРОЦЕССЕ (при N
	// репликах эффективный предел равен N × объявленного), значит её объявляет
	// посадка; а у настройки есть состояние «не задана», и оно НЕ безобидно:
	// нулевые величины означают «не ограничиваем», а не «ограничивать нечего».
	// Боевая посадка здесь и останавливается.
	if err := cfg.ValidateRequestRateLimits(); err != nil {
		log.Fatalf("config validate (request rate limits): %v", err)
	}

	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "serve":
			// no-op: продолжаем в runServe
		case "migrate":
			log.Fatal("migrations are not handled by this binary — use the kacho-migrator CLI ({up|down|status|create})")
		default:
			log.Fatalf("unknown command %q (this binary only serves the API; migrations live in `kacho-migrator`)", os.Args[1])
		}
	}

	if err := runServe(cfg); err != nil {
		log.Fatal(err)
	}
}

// services — собранный набор бизнес-сервисов (один composition-point вместо
// россыпи локальных переменных в runServe). Заполняется buildServices,
// используется register{Public,Internal}Services. Каждый ресурс представлен
// готовым use-case-handler'ом, а не «толстым» сервисом.
type services struct {
	networkHandler           *networkapp.Handler
	subnetHandler            *subnetapp.Handler
	addressHandler           *addressapp.Handler
	addressAllocate          *addressapp.AllocateUseCase
	addressRefService        *addressref.Service
	routeTableHandler        *routetableapp.Handler
	securityGroupHandler     *sgapp.Handler
	gatewayHandler           *gatewayapp.Handler
	addressPoolHandler       *addresspoolapp.Handler
	addressPoolPublic        *addresspoolapp.PublicHandler
	networkInternal          *networkinternal.Service
	networkInterfaceHandler  *niapp.Handler
	networkInterfaceInternal *nicinternal.Service
	// cidrGroupHandler — именованные наборы префиксов: предмет, на который
	// ссылается правило группы безопасности вместо своей копии перечня.
	cidrGroupHandler *cidrgroupapp.Handler
	// quotaHandler — арендаторское чтение квот. ТОЛЬКО чтение: величины
	// назначает администратор облака на внутреннем слушателе iam. nil означает
	// «внутреннего адреса соседа нет», и тогда сервис этого RPC не выставляет —
	// а не выставляет отвечающий пустотой (см. регистрацию ниже).
	quotaHandler *quotaapp.Handler
}

func runServe(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// logger.level из конфига уважается (валидность проверена в cfg.Validate()).
	logger := observability.NewSloggerLevel(os.Stdout, cfg.SlogLevel())
	slog.SetDefault(logger)

	// Boot-time self-check DTO-реестра: fail-fast, если blank-import
	// internal/dto/toproto потерян и init()-регистрации не отработали (иначе —
	// codes.Internal «no transfer registered» на первом же валидном Get/List).
	dto.MustBeRegistered()

	// Логируем insecure dev-defaults.
	for _, w := range cfg.InsecureDevWarnings() {
		logger.Warn(w)
	}
	if cfg.AuthN.Mode == config.ModeProduction {
		logger.Warn("authn.mode=production: anonymous callers will be rejected (M5 fail-closed)")
	}
	if cfg.AuthN.Mode == config.ModeProductionStrict {
		logger.Warn("authn.mode=production-strict: anonymous rejected + TLS+SSL strictly validated")
	}
	// Здесь стояло утверждение, что аварийный пропуск страницы в боевом режиме
	// «сюда не доходит, потому что Config.Validate() отвергает такой конфиг».
	// Условия про этот пропуск в Config.Validate НЕ БЫЛО ни в одной редакции:
	// страж отвергал выключенный фильтр и нерезолвимый адрес, но не саму ручку, —
	// то есть комментарий называл несуществующую защиту. Это хуже обычного
	// расхождения: он ОТВЕТИЛ читателю на вопрос «закрыта ли эта ручка», поэтому
	// проверять никто не шёл. Ручка снята целиком (её имя — в
	// `retired_knobs_test.go`), и запрет держится тем, что предмета больше нет:
	// сужатель без соединения отказывает по построению, снять отказ настройкой
	// нельзя.

	// Профиль возможностей исполнителя — в журнал старта, ОДНОЙ строкой и целиком.
	//
	// Он уже прошёл стража (S5, до этой точки), и печатается здесь не ради проверки,
	// а ради наблюдаемости: гейт посадки читает то, что процесс сам объявил при
	// старте, а не хранимый ConfigMap (правка ConfigMap не перекатывает под, и
	// процесс живёт с boot-time окружением). Семейства печатаются НОРМАЛИЗОВАННЫМ
	// значением — тем же, что читал страж, — иначе журнал показывал бы «v4,,» там,
	// где страж видел «не объявлено».
	//
	// Рядом с объявленным полезным размером кадра печатается ОБЕЩАНИЕ ПРОДУКТА
	// (`product_payload_floor_bytes`): это единственная гарантия профиля, у которой
	// есть нижняя граница, обещанная арендатору, и читающий журнал обязан видеть обе
	// величины в одной строке — иначе объявленное число нечем сопоставить с тем, на
	// что арендатор рассчитывает. Стенд с объявлением НИЖЕ обещания страж (S5, выше)
	// до этой точки уже не пустил; строка ниже — наблюдаемость, а не проверка.
	logger.Info("dataplane executor profile declared",
		"overlapping_tenant_addresses", cfg.Dataplane.Executor.OverlappingTenantAddresses,
		"state_tracking_families", cfg.StateTrackingFamilies().String(),
		"named_set_reference_in_rule", cfg.Dataplane.Executor.NamedSetReferenceInRule,
		"guaranteed_payload_bytes", cfg.Dataplane.Executor.GuaranteedPayloadBytes,
		"product_payload_floor_bytes", domain.GuaranteedPayloadFloorBytes,
		"guaranteed_bandwidth_per_interface_mbps", cfg.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps,
		"connection_limit_per_interface", cfg.Dataplane.Executor.ConnectionLimitPerInterface,
		"connection_rate_limit_per_interface_per_second", cfg.Dataplane.Executor.ConnectionRateLimitPerInterfacePerSecond,
		"connection_rate_burst_per_interface", cfg.Dataplane.Executor.ConnectionRateBurstPerInterface,
		"tenant_settable_bandwidth_limit", cfg.Dataplane.Executor.TenantSettableBandwidthLimit,
	)

	// Per-edge opt-in mTLS-конфиг из env (KACHO_VPC_*). enable=false на ребре →
	// insecure (dev backward-compat). Используется для ребер vpc→iam
	// register-drainer, vpc→geo (zone_id), фильтра видимости и для public/internal
	// server-листенеров.
	//
	// Грузится и проверяется ДО первого соединения — включая соединение с БД.
	// Это чистая проверка конфигурации, ей нечего ждать от окружения, а отказ,
	// наступающий позже открытия пула, наблюдался бы как ошибка БД: отказ старта
	// обязан называть свою причину независимо от того, поднята ли база.
	mtlsCfg, err := config.LoadMTLS()
	if err != nil {
		return fmt.Errorf("load mTLS config: %w", err)
	}
	// Fail-closed boot-гардрейл S2: production-strict требует server-mTLS на обоих
	// листенерах. MTLSConfig грузится вне viper-Config, поэтому проверка — здесь,
	// ДО привязки листенеров (отказ старта вместо insecure-listener'а).
	if err := cfg.ValidateServerMTLS(mtlsCfg); err != nil {
		return fmt.Errorf("config validate (server mTLS): %w", err)
	}
	// Fail-closed boot-гардрейл S4: production требует verified transport на КАЖДОМ
	// исходящем ребре — authz Check (:9091), ProjectService.Get (:9090), vpc→geo,
	// owner-tuple register и фильтр видимости (AuthorizeService.BatchCheck). Иначе
	// dialPeer откатился бы в insecure creds, и решение о доступе (или о том, что
	// вызывающий увидит) уехало бы по незащищённому транспорту. Проверка — здесь,
	// ДО cross-service dial'ов.
	if err := cfg.ValidatePeerTransport(mtlsCfg); err != nil {
		return fmt.Errorf("config validate (peer transport): %w", err)
	}

	pool, err := coredb.NewPool(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	// Slave-pool (read-replica). Если slave-url настроен и отличается от master URL
	// — отдельный pgxpool для read-TX'ов; иначе slavePool = nil и kachopg.New()
	// делает fallback на master. Код во всех use-case'ах уже разделен на
	// Reader/Writer, так что переключение на реальную реплику — это только wiring.
	var slavePool *pgxpool.Pool
	if slaveDSN := cfg.SlaveDSN(); slaveDSN != "" {
		slavePool, err = coredb.NewPool(ctx, slaveDSN)
		if err != nil {
			return fmt.Errorf("new slave pool: %w", err)
		}
		defer slavePool.Close()
		logger.Info("kacho-vpc CQRS slave-pool enabled (read-replica)",
			"slave_url_masked", maskDSN(cfg.Repository.Postgres.SlaveURL))
	} else {
		logger.Info("kacho-vpc CQRS slave-pool disabled — Reader-TX fallback to master")
	}

	// Schema = `kacho_vpc`. cfg.DSN() уже несет `options=-c search_path=kacho_vpc,public`
	// — unqualified-references из repo-кода резолвятся в kacho_vpc. operations-repo
	// дополнительно передает схему явно для квалификации SQL-операций.
	opsRepo := operations.NewRepo(pool, "kacho_vpc")

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

	// Prometheus observability adapter: приватный реестр, питает outbox-recorder,
	// reconciler-recorder и diagnostic /metrics. Заменяет in-memory MemRecorder —
	// метрики теперь экспортируются наружу (scrape).
	metricsAdapter := vpcmetrics.New(buildVersion, buildCommit)

	// Cross-service gRPC dial — через единый builder: retries=3 / dialTimeout=10s /
	// keepalive=30s / TLS / опц. dns:///+round_robin.
	//
	// Ребро vpc→iam ProjectService.Get — клиентский mTLS. При
	// KACHO_VPC_IAM_PROJECT_MTLS_ENABLE=true дилим с corelib client-cert creds
	// (предъявляем kacho-vpc-client-tls, проверяем iam server-cert против internal-CA +
	// ServerName=kacho-iam, fail-closed на плохой тройке); enable=false → insecure/
	// server-auth путь через clients.Build (dev backward-compat). Обязателен, когда
	// kacho-iam требует и проверяет client-cert — иначе TLS-handshake этого dial падает.
	iamPeer := cfg.ExtAPI.IAM
	iamConn, err := dialPeer(ctx, "vpc→iam project", mtlsCfg.IAMProjectMTLS.Enable,
		mtlsCfg.IAMProjectClientCreds, false, clients.BuildOptions{
			Endpoint: iamPeer.Endpoint,
			TLS:      iamPeer.TLS.Enable,
			DNSLB:    iamPeer.DNSLB,
		})
	if err != nil {
		return fmt.Errorf("dial iam: %w", err)
	}
	defer iamConn.Close()
	logger.Info("vpc→iam ProjectService.Get edge configured",
		"endpoint", iamPeer.Endpoint, "mtls", mtlsCfg.IAMProjectMTLS.Enable)
	// TTL+LRU кеш: снимает gRPC-hop в kacho-iam из hot-path Network.Create при
	// burst-нагрузке. См. internal/clients/project_cache.go.
	rawProjectClient := clients.NewProjectClient(iamConn)
	projectClient := clients.NewCachedProjectClient(rawProjectClient, clients.ProjectCacheConfig{
		PositiveTTL: cfg.Network.ProjectCache.PositiveTTL,
		NegativeTTL: cfg.Network.ProjectCache.NegativeTTL,
		MaxSize:     cfg.Network.ProjectCache.MaxSize,
	})
	logger.Info("project existence cache enabled",
		"positive_ttl", cfg.Network.ProjectCache.PositiveTTL,
		"negative_ttl", cfg.Network.ProjectCache.NegativeTTL,
		"max_size", cfg.Network.ProjectCache.MaxSize)

	// Geography (Region/Zone) — leaf-домен kacho-geo: VPC валидирует zone_id вызовом
	// geo.v1.ZoneService.Get (Subnet.Create / AddressPool.Create). Когда per-edge
	// mTLS включен (KACHO_VPC_GEO_MTLS_ENABLE=true) — дилим geo с corelib client-cert
	// creds (fail-closed); иначе insecure/one-way-TLS путь через clients.Build (dev
	// backward-compat).
	geoConn, err := dialPeer(ctx, "vpc→geo", mtlsCfg.GeoMTLS.Enable,
		mtlsCfg.GeoClientCreds, false, clients.BuildOptions{
			Endpoint: cfg.ExtAPI.Geo.Endpoint,
			TLS:      cfg.ExtAPI.Geo.TLS.Enable,
		})
	if err != nil {
		return fmt.Errorf("dial geo: %w", err)
	}
	defer geoConn.Close()
	geoClient := clients.NewGeoZoneClient(geoConn)
	geoRegionClient := clients.NewGeoRegionClient(geoConn)

	// authz internal IAM conn: cfg.AuthZ.IAMEndpoint → **internal** listener kacho-iam
	// (:9091), единственный, что обслуживает InternalIAMService.Check. Пустой endpoint
	// → nil conn (dev / no-authz: per-RPC gate пропускается).
	//
	// Этот conn обслуживает per-RPC authz-gate — и ТОЛЬКО его. У фильтра видимости
	// соединение своё (authorizeConn, см. buildAuthorizeConn), и адрес у него свой:
	// отсюда он наследуется лишь тогда, когда authz.list-filter.authorize-endpoint не
	// задан. Здесь стояло «общий conn … обслуживает и per-RPC gate, и list-filter», а
	// рядом — «пустой endpoint ⇒ list-filter тоже обязан быть выключен». Оба
	// утверждения неверны с тех пор, как у фильтра появился собственный дозвон.
	//
	// Ребро vpc→iam Check — клиентский mTLS. При KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE=true дилим с
	// corelib client-cert creds (ServerName=kacho-iam-internal — SAN dial-host'а :9091;
	// fail-closed на плохой тройке); enable=false → insecure/server-auth путь через
	// clients.Build (dev).
	var authzConn clients.Conn
	if cfg.AuthZ.IAMEndpoint != "" {
		authzConn, err = dialPeer(ctx, "vpc→iam authz", mtlsCfg.IAMAuthzMTLS.Enable,
			mtlsCfg.IAMAuthzClientCreds, false, clients.BuildOptions{
				Endpoint: cfg.AuthZ.IAMEndpoint,
				TLS:      cfg.AuthZ.IAMTLS.Enable,
			})
		if err != nil {
			return fmt.Errorf("dial kacho-iam (authz): %w", err)
		}
		defer authzConn.Close()
		logger.Info("vpc→iam Check edge configured (per-RPC gate)",
			"endpoint", cfg.AuthZ.IAMEndpoint, "mtls", mtlsCfg.IAMAuthzMTLS.Enable)
	}

	// Per-page фильтр видимости для List. Каждый List RPC читает СТРАНИЦУ из своей
	// БД и спрашивает kacho-iam AuthorizeService.BatchCheck, какие её id видимы
	// caller'у (viewer ∪ v_list; read == enforce). Единичный Get фильтр НЕ
	// использует — его авторизует прямой per-object Check в authz-interceptor'е.
	//
	// Соединение у фильтра СВОЁ (authorizeConn), отдельное от ребра per-RPC
	// InternalIAMService.Check (authzConn). Как резолвится его адрес и почему
	// слушатель iam здесь не называется — godoc buildAuthorizeConn; тут это не
	// пересказывается. nil-фильтр (выключен / нет endpoint) → use-case'ы делают
	// нефильтрованный list.
	var authorizeConn clients.Conn
	if cfg.AuthZ.ListFilter.Enabled {
		authorizeConn, err = buildAuthorizeConn(ctx, cfg, mtlsCfg, logger)
		if err != nil {
			return err
		}
		if authorizeConn != nil {
			defer authorizeConn.Close()
		}
	}
	listFilter := buildListFilter(cfg, authorizeConn, logger)
	// Величины сужателя выходят из процесса ТОЛЬКО здесь. Полос четыре: одна
	// положительная и три — страница, ушедшая БЕЗ пообъектной проверки. Снимите
	// эту строку — и полосы исчезнут с поверхности, а не станут нулями; ровно это
	// ловит гейт дерева `TestEveryListNarrowConsumerRegistersItsCollector`.
	metricsAdapter.RegisterListNarrow(func() listnarrow.Counts { return listFilter.Counts() })

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
	var authzCache authzmetrics.Source
	metricsAdapter.RegisterAuthzCache(map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    authzCache.Cache,
		authzmetrics.LaneNarrow: listFilter.CacheStats,
	}, authzCache.Read)

	// Sync-primary owner-tuple registrar (Decision 2): create-flow синхронно
	// регистрирует owner-tuple в kacho-iam после commit — грант доступен сразу, без
	// гонки с async register-drainer'ом. Тот же iam-internal endpoint :9091 +
	// register-creds, что и у drainer'а. Пустой endpoint / drainer disabled → nil
	// (dev/no-iam: остается только async-путь).
	var syncRegistrar fgaregister.Registrar
	if cfg.IAM.RegisterDrainerEnabled && cfg.AuthZ.IAMEndpoint != "" {
		reg, closeReg, rerr := buildSyncRegistrar(cfg.AuthZ.IAMEndpoint, mtlsCfg)
		if rerr != nil {
			return fmt.Errorf("build sync owner-tuple registrar: %w", rerr)
		}
		defer closeReg()
		syncRegistrar = reg
	}

	// Учёт числа ресурсов арендатора. Приёмка
	// `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
	// (APPROVED, раунд 2), DoD S2 п.3 и п.5.
	//
	// Величины живут у kacho-iam и разрешаются ТАМ: старшинство PROJECT >
	// ACCOUNT > DEFAULT требует знать аккаунт проекта, а владелец типа его не
	// знает (у него только зеркало, заводимое из того же обращения).
	//
	// Резолв идёт на ВНУТРЕННИЙ слушатель соседа — тот же `authzConn`, что
	// обслуживает per-RPC Check: `InternalLimitService` выставлен ровно там. Это
	// НЕ вывод адреса из чужого (`security.md` §Hardening п.9), а использование
	// уже объявленного оператором адреса внутреннего контура: своей ручки у
	// резолва нет именно потому, что второй адрес того же слушателя разошёлся бы
	// с первым молча.
	//
	// Пустой endpoint (dev без соседа) → полоса не собирается. Это означает
	// «раннего отказа нет», а НЕ «предела нет»: место по-прежнему занимает
	// триггер в writer-транзакции, и исчерпание приезжает отказом операции.
	// Различие наблюдаемо, и на любом поднятом стенде адрес обязан быть задан.
	var quotaLimits quota.LimitResolver
	if authzConn != nil {
		limitClient := clients.NewLimitClient(authzConn)
		quotaLimits = limitClient
		logger.Info("resource-count quota: limits resolver wired",
			"endpoint", cfg.AuthZ.IAMEndpoint, "service", "vpc")

		// Снимок величины обязан ДОГОНЯТЬ авторитет: без тянущего строка,
		// заведённая один раз, живёт со своей величиной вечно, и смена предела
		// администратором не доезжает до проекта никогда.
		stopQuotaSync, qerr := startQuotaLimitSyncer(ctx, pool, limitClient, "kacho_vpc", logger)
		if qerr != nil {
			return fmt.Errorf("start quota limit syncer: %w", qerr)
		}
		defer stopQuotaSync()
	} else {
		logger.Warn("resource-count quota: no internal kacho-iam endpoint, advisory band is OFF " +
			"and the limit snapshot will NEVER catch up with the authority. " +
			"Limits are still enforced by the charging trigger, but the tenant learns of " +
			"exhaustion from the operation instead of a synchronous refusal, and an " +
			"administrator raising or lowering a ceiling has no effect on this process")
	}

	svcs := buildServices(pool, slavePool, projectClient, geoClient, geoRegionClient, listFilter, opsRepo, syncRegistrar, quotaLimits, projectClient, cfg, logger)

	// Сервер потока изменений — ОБЩИЙ (`pkg/subscription`), а не свой. Форма
	// подписки объявлена однажды на всю платформу, и владелец журнала приносит
	// сюда только объявление СВОЕГО журнала: где он лежит, каким каналом будит,
	// как его строка становится событием. Курсор, граница устоявшегося, пределы,
	// сужение по правам и порядок отказов принадлежат серверу.
	subscribe, subErr := buildSubscriptionServer(cfg, listFilter, logger)
	if subErr != nil {
		return subErr
	}

	// Fail-closed boot-gate: при KACHO_VPC_REQUIRE_IAM мутирующий Create отвергается,
	// а readiness = NotReady, пока register-drainer не подключен к IAM. Стартует
	// неподключенным; SetConnected(true) срабатывает ниже, как только dial drainer'а
	// успешен.
	bootGate := bootgate.New(bootgate.Config{RequireIAM: cfg.IAM.Require, Service: "kacho-vpc"})
	// ── объявление о себе ────────────────────────────────────────────────────
	//
	// Дескриптор собирается ПОСЛЕ пула: порт сверки существования живёт НА пуле, и
	// собрать его без пула означало бы принести порт, который на первом же вопросе
	// ответит «соединения нет». Probe читает МАСТЕР (авторитетно, без
	// replica-lag false-absent): с реплики он на ограниченное, но не измеряемое
	// отсюда время отвечал бы «объекта нет» про уже созданный объект, а этот ответ
	// превращает отказ в правах в 404.
	//
	// Сужатель и загрузочный гейт уезжают ТЕМИ ЖЕ объектами, что провязаны в
	// use-case'ах и в пробе готовности пода.
	//
	// И ДО фоновых проходов: дескриптор судит конфигурацию, а не окружение,
	// поэтому его отказ обязан наступить раньше, чем процесс поднимет дренаж
	// регистраций и соседние соединения. Открытие пула обратимо (defer выше) и
	// дешевле ложной сверки существования — это единственное, что стоит перед ним.
	desc, err := describe(cfg, mtlsCfg, logger, listFilter, bootGate, kachopg.NewExistenceProbe(pool), authzCache.Install, metricsAdapter.Registerer())
	if err != nil {
		return fmt.Errorf("describe kacho-vpc: %w", err)
	}

	// Самоотчёт о посадке — ПОСЛЕ того, как дескриптор ПРИНЯТ (конфиг прошёл все
	// отказы, которые являются его свойствами), и ДО подъёма слушателей. До приёма
	// дескриптора отчёт описывал бы намерение, а не посадку: часть измерений (круг
	// отправителей, транспорт обоих слушателей, ребро решения о доступе) судит
	// именно конструктор, и напечатать их раньше значило бы напечатать то, что ещё
	// может не пройти. Гейт посадки обязан утверждать на этом наблюдаемом факте, а
	// не на хранимом конфиге: правка настроек без переката пода оставляет процесс
	// с прежним окружением, и «под Ready» доказательством посадки не является.
	observability.LogBootPosture(logger, bootPosture(cfg, mtlsCfg))

	// Prometheus-backed outbox-recorder: backlog/oldest/poisoned register-outbox
	// экспортируются на /metrics (заменяет in-memory MemRecorder). Тот же adapter
	// — operations.Recorder для reconciler'а ниже.
	outboxRec := metricsAdapter

	// register-drainer: применяет FGA owner-tuple register/unregister intents
	// (kacho_vpc.fga_register_outbox, записанные транзакционно в writer-TX ресурса)
	// через kacho-iam InternalIAMService.RegisterResource по ребру vpc→iam (mTLS
	// opt-in). Default-on: без него созданные ресурсы не получают owner-tuple →
	// per-resource Check DENY. Дилит iam-internal listener :9091 (cfg.AuthZ.IAMEndpoint
	// — RegisterResource Internal-only, ban #6). Пустой endpoint → drainer не стартует
	// (dev / no-iam).
	if cfg.IAM.RegisterDrainerEnabled {
		if cfg.AuthZ.IAMEndpoint == "" {
			logger.Warn("FGA register-drainer NOT started — authz.iam-endpoint unset " +
				"(no kacho-iam internal endpoint to apply register-intents); intents stay durable until configured")
		} else {
			closeDrainer, derr := startRegisterDrainer(ctx, cfg.AuthZ.IAMEndpoint, mtlsCfg, pool, outboxRec, logger)
			if derr != nil {
				return fmt.Errorf("start register-drainer: %w", derr)
			}
			defer closeDrainer()
			// Dial drainer'а установлен → путь доставки IAM-register работает:
			// открываем boot-gate. reconciler + metrics-collector стартуют рядом.
			bootGate.SetConnected(true)
			if berr := startBackstop(ctx, pool, outboxRec, logger); berr != nil {
				return fmt.Errorf("start outbox backstop: %w", berr)
			}
		}
	} else {
		logger.Warn("FGA register-drainer DISABLED (KACHO_VPC_FGA_REGISTER_DRAINER_ENABLED=false) — " +
			"register-intents accumulate in fga_register_outbox unapplied")
	}

	logger.Info("kacho-vpc listener mTLS",
		"public_mtls", mtlsCfg.PublicServerMTLS.Enable,
		"internal_mtls", mtlsCfg.InternalServerMTLS.Enable)
	// SEC: production без public-mTLS допущен boot-гардрейлом только под явный
	// trusted-forwarder ack — принимаем client-asserted x-kacho-* principal'а по
	// незашифрованному :9090. Громко предупреждаем, что безопасность целиком
	// зависит от аутентифицирующего forwarder'а/mesh перед listener'ом.
	if cfg.AuthN.Mode == config.ModeProduction && !mtlsCfg.PublicServerMTLS.Enable && cfg.AuthN.TrustedForwarder {
		logger.Warn("public :9090 listener trusts client-asserted principal WITHOUT server-mTLS "+
			"(authn.trusted-forwarder=true) — the public endpoint MUST be reachable only via an "+
			"authenticated forwarder/service-mesh that terminates client identity; direct network "+
			"access to :9090 allows principal spoofing / cross-tenant authz bypass",
			"mode", cfg.AuthN.Mode.String())
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
	diagDesc, err := describeDiagnosticSurface(cfg.MetricsEndpoint(), metricsAdapter, healthAgg,
		desc.Spec().Mode, logger)
	if err != nil {
		return fmt.Errorf("профиль диагностической поверхности: %w", err)
	}
	// Собственный контекст поверхности: она гасится ПОСЛЕДНЕЙ — после обоих
	// gRPC-слушателей и после дренажа исполнителей операций, — чтобы переброс
	// /readyz в 503 успел отработать до закрытия порта.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	defer stopDiag()

	// Durable LRO recovery: доменный resolver + corelib-reconciler поверх schema
	// kacho_vpc. RecoverAll прогоняется ДО приема трафика; периодический Run —
	// backstop до отмены ctx.
	startLRORecovery(ctx, pool, kachopg.New(pool, slavePool), metricsAdapter, logger)

	// Явно поднимаем package-level default-registry LRO-worker'а ДО приема трафика:
	// readiness lro-worker зеленый без единой мутации (нет boot-deadlock), а
	// live-worker метрики (terminal-write retries/failures, inflight gauge) текут в
	// тот же Prometheus-adapter — раньше эти серии были мертвы (NopRecorder).
	if err := startLROWorker(metricsAdapter, logger); err != nil {
		return fmt.Errorf("start LRO worker: %w", err)
	}

	gracefulTimeout := cfg.APIServer.GracefulShutdown
	if gracefulTimeout <= 0 {
		gracefulTimeout = 10 * time.Second
	}

	// Ограничитель допуска запросов ЗДЕСЬ больше не собирается: его провязал
	// носитель контура (`pkg/servicehost`) по оси дескриптора. Пока проводка
	// принадлежала этому корню, она существовала ровно у одного сервиса из
	// семи — и «провязал» было неотличимо от «не провязал» без сплошной
	// переписи (задачи #692, #771). Величины по-прежнему объявляет посадка
	// (`api-server.rate-limit`), и необъявленную боевую посадку по-прежнему не
	// поднимает страж S7 выше.

	// Отдельный контекст слушателей. Носитель гасит ОБА слушателя по отмене СВОЕГО
	// контекста, поэтому флип готовности в shutting_down обязан произойти РАНЬШЕ
	// этой отмены, а не одновременно с ней: kubelet перестаёт слать трафик до
	// того, как соединения начнут закрываться. Одним контекстом на всё этот
	// порядок был бы неразличим.
	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()
	// serveDone закрывается, когда носитель вернул управление. Нужен сторожу
	// срока штатного завершения ниже.
	serveDone := make(chan struct{})

	// Параллельный запуск слушателей + shutdown-waiter через `parallel.ExecAbstract`
	// (`github.com/H-BF/corlib/pkg/parallel`). Failure-isolation: первая ошибка /
	// SIGTERM / SIGINT триггерит гашение ВСЕГО — умерший носитель не оставляет
	// диагностический слушатель крутиться.
	//
	// shutdownCh закрывается ВНУТРИ triggerShutdown — он будит shutdown-waiter не
	// только по SIGTERM/SIGINT (ctx.Done), но и когда гашение инициировано крахом
	// слушателей. Без этого waiter висел бы на `<-ctx.Done()` навечно (ctx —
	// только сигнальный), и `parallel.ExecAbstract` никогда бы не вернулся —
	// процесс-зомби.
	shutdownCh := make(chan struct{})
	var shutdownOnce sync.Once
	triggerShutdown := func() {
		shutdownOnce.Do(func() {
			// Readiness флипает в shutting_down ДО гашения — kubelet перестаёт
			// слать трафик, пока in-flight RPC дренируются.
			healthAgg.SetShuttingDown()
			close(shutdownCh)
			stopServe()
			// Сторож срока штатного завершения. Носитель гасит слушатели ТОЛЬКО
			// мягко (GracefulStop) — принудительного `Stop()` по истечении срока у
			// него нет, и принести его сервису нечем: сервера корень не получает ни
			// в каком виде. Поэтому величина `api-server.graceful-shutdown`
			// сохраняет свой предмет — срок, за который завершение обязано
			// уложиться, — но её нарушение теперь НАБЛЮДАЕТСЯ, а не исполняется.
			// Молчаливой альтернативой было бы зависшее завершение, неотличимое от
			// штатного, и ручка без читателя вдобавок.
			go func() {
				select {
				case <-serveDone:
				case <-time.After(gracefulTimeout):
					logger.Error("graceful stop exceeded its budget; listeners are still draining",
						"budget", gracefulTimeout,
						"knob", "api-server.graceful-shutdown",
						"note", "the carrier stops listeners gracefully only; there is no forced Stop behind this budget")
				}
			}()
		})
	}

	tasks := []func() error{
		// ОБА gRPC-слушателя — носитель контура. Он поднимает их с ОДНОЙ цепочкой
		// звеньев, прогоняет отказы старта, которым нужен служимый набор RPC, и
		// обслуживает до отмены serveCtx. Исход внутреннего слушателя учитывается
		// наравне с публичным — это его свойство, а не наше.
		func() error {
			serr := servicehost.Serve(serveCtx, desc,
				func(reg grpc.ServiceRegistrar) {
					registerPublicServices(reg, svcs, opsRepo)
				},
				func(reg grpc.ServiceRegistrar) {
					registerInternalServices(reg, svcs, subscribe)
				},
			)
			close(serveDone)
			triggerShutdown()
			if serr != nil {
				logger.Error("grpc listeners stopped", "err", serr)
				return fmt.Errorf("grpc: %w", serr)
			}
			return nil
		},
		// shutdown waiter: SIGTERM/SIGINT (ctx) ИЛИ краш слушателей (shutdownCh) →
		// гашение + дрейн LRO worker'ов. select по обоим каналам, иначе при крахе
		// слушателей waiter висел бы на ctx навечно.
		func() error {
			select {
			case <-ctx.Done():
			case <-shutdownCh:
			}
			triggerShutdown()
			drainCtx, cancelDrain := context.WithTimeout(context.Background(), 3*gracefulTimeout)
			defer cancelDrain()
			if err := operations.Wait(drainCtx); err != nil {
				logger.Warn("operations workers did not finish in time",
					"err", err, "active", operations.Active())
			}
			// Диагностическая поверхность гасится последней (после дренажа LRO
			// worker'ов), чтобы переброс /readyz в 503 успел отработать до закрытия
			// порта. Ждать её возврата здесь не нужно: он ждётся самим набором задач —
			// профиль возвращается только после того, как порт освобождён.
			stopDiag()
			return nil
		},
	}
	// Привязка порта — ЗДЕСЬ, до постановки задачи: занятый адрес есть ошибка
	// посадки, и узнать о ней надо до того, как процесс объявит себя поднявшимся.
	// Прежде подъём целиком уезжал в задачу супервизора, и отказ привязки
	// становился кодом возврата процесса, успевшего сколько угодно проработать.
	//
	// Ожидание ставится задачей ВСЕГДА, даже когда поверхность объявлена
	// выключенной: тогда оно сразу возвращается, а причина уже названа в журнале.
	// Условная постановка вернула бы то самое молчание, ради устранения которого
	// выключение стало объявлением. Крах поверхности триггерит graceful-stop всех
	// через triggerShutdown/shutdownCh.
	waitDiag, diagErr := servicehost.ServeSurface(diagCtx, diagDesc)
	if diagErr != nil {
		stopDiag()
		return fmt.Errorf("диагностическая поверхность: %w", diagErr)
	}
	tasks = append(tasks, func() error {
		if derr := waitDiag(); derr != nil {
			logger.Error("диагностическая поверхность остановлена с ошибкой", "err", derr)
			triggerShutdown()
			return fmt.Errorf("диагностическая поверхность: %w", derr)
		}
		return nil
	})

	// ExecAbstract(taskCount, maxConcurrency, fn): запускает все задачи
	// параллельно; собирает первую ошибку. maxConcurrency=len(tasks)-1 дает
	// схему «1 + (N-1)» — основная горутина + N-1 дополнительных, все
	// задачи реально параллельны (см. corlib/pkg/parallel/exec-in-parallel.go).
	err = parallel.ExecAbstract(len(tasks), safeconv.IntToInt32(len(tasks)-1), func(i int) error {
		return tasks[i]()
	})
	cancel()
	return err
}

// buildAuthorizeConn — дилит endpoint kacho-iam AuthorizeService для per-object
// List/Get-фильтра. Соединение ОТДЕЛЬНОЕ от ребра per-RPC
// InternalIAMService.Check (его держит authzConn): у него свой адрес и свой
// транспорт. Endpoint = authz.list-filter.authorize-endpoint, с fallback на
// authz.iam-endpoint, если не задан. Пусто → (nil, nil): caller логирует warn, а
// фильтр деградирует в passthrough.
//
// НА КАКОЙ СЛУШАТЕЛЬ iam приходит этот адрес — свойство ПРОФИЛЯ, а не этого кода:
// AuthorizeService зарегистрирована на ОБОИХ слушателях iam, поэтому законны и
// публичный адрес, и внутренний. Состав слушателей описан в ОДНОМ месте —
// services/iam/cmd/kacho-iam/grpc_register.go (registerPublicServices /
// registerInternalServices), — и здесь он намеренно не пересказывается: пересказ
// разошёлся бы с ним молча.
//
// Здесь и на месте вызова стояло «AuthorizeService — PUBLIC-проекция :9090, а
// listener InternalIAMService.Check (:9091) её не обслуживает». Вторая половина
// ложна безусловно: регистрация на внутреннем слушателе есть. Первая верна лишь
// там, где профиль задал authorize-endpoint явно; при пустом значении адрес
// наследуется от authz.iam-endpoint, чьё умолчание в чарте — внутренний
// kacho-iam-internal:9091. Снято потому, что по этому комментарию делали вывод
// «публичных служб на внутреннем слушателе нет», и вывод был неверен.
//
// mTLS — opt-in через authz.list-filter.authorize-tls; когда выключен, переиспользует
// тот же vpc→iam authz client-cert, что и ребро Check (IAMAuthzMTLS), чтобы одна
// client-identity покрывала оба ребра. enable=false на обоих → insecure/server-auth
// dev-путь, и именно эту комбинацию в любом боевом режиме отвергает boot-гардрейл S4
// (ValidatePeerTransport) — адрес и предикат транспорта здесь и там читаются ОДНИМИ И
// ТЕМИ ЖЕ методами Config, чтобы страж и проводка не могли разойтись.
func buildAuthorizeConn(ctx context.Context, cfg config.Config, mtlsCfg config.MTLSConfig, logger *slog.Logger) (clients.Conn, error) {
	endpoint := cfg.ListFilterAuthorizeEndpoint()
	if endpoint == "" {
		logger.Warn("authz.list-filter.enabled=true but neither authorize-endpoint nor iam-endpoint set — per-object list-filter disabled")
		return nil, nil
	}
	useMTLS := cfg.ListFilterEdgeUsesMTLS(mtlsCfg)
	conn, err := dialPeer(ctx, "vpc→iam authorize", useMTLS,
		mtlsCfg.IAMAuthzClientCreds, false, clients.BuildOptions{
			Endpoint: endpoint,
			TLS:      cfg.AuthZ.ListFilter.AuthorizeTLS.Enable,
		})
	if err != nil {
		return nil, fmt.Errorf("dial kacho-iam (authorize/list-filter): %w", err)
	}
	logger.Info("per-object list-filter authorize edge configured", "endpoint", endpoint, "mtls", useMTLS)
	return conn, nil
}

// dialPeer собирает cross-service gRPC conn для одного edge. useMTLS=true → per-edge
// client-cert creds через credsFn (fail-closed на плохой тройке); useMTLS=false →
// insecure/one-way-TLS путь через clients.Build (dev backward-compat). keepalive
// управляет idle-keepalive пингами (для idle-склонных ребер). endpoint берется из
// opts.Endpoint (opts.TLS/DNSLB игнорируются на mTLS-пути — creds несут TLS сами).
func dialPeer(
	ctx context.Context,
	label string,
	useMTLS bool,
	credsFn func() (grpc.DialOption, error),
	keepalive bool,
	opts clients.BuildOptions,
) (clients.Conn, error) {
	if useMTLS {
		creds, err := credsFn()
		if err != nil {
			return nil, fmt.Errorf("%s mTLS creds: %w", label, err)
		}
		return grpc.NewClient(opts.Endpoint, creds, grpcclient.KeepaliveDialOption(keepalive))
	}
	return clients.Build(ctx, opts)
}

// buildListFilter — сужатель списочной страницы (`AuthorizeService.BatchCheck` на
// идентификаторах прочитанной страницы). Get его НЕ получает — единичное чтение
// авторизует прямой пообъектный Check в интерсепторе.
//
// Выключенный фильтр БОЛЬШЕ НЕ ОЗНАЧАЕТ сквозной проход. Прежняя редакция отдавала
// здесь nil, и use-case'ы трактовали его как «сужение выключено, страницу отдать»:
// посадка без модели показывала каждому участнику проекта каждую его строку, а у
// помеченных scope-filtered RPC пропадала единственная пообъектная авторизация
// вовсе. Теперь сужатель собирается ВСЕГДА и отказывает, пока ему не с кем говорить.
//
// Настройки, снимающей этот отказ, НЕ СУЩЕСТВУЕТ. Прежде здесь читалась ручка
// аварийного пропуска (имя — в `retired_knobs_test.go`), и её предмет был
// недостижим: на выключенном фильтре и на нерезолвимом адресе процесс не
// поднимается — `ValidateListFilter` отказывает на любой посадке, а адрес там и
// здесь резолвится ОДНИМ методом Config. Отказ на отсутствующей модели остаётся
// свойством самого сужателя (`pkg/listnarrow`: нет соединения ⇒ PermissionDenied),
// а не следствием того, что ручку не тронули.
func buildListFilter(cfg config.Config, conn clients.Conn, logger *slog.Logger) *authzfilter.Narrower {
	if !cfg.AuthZ.ListFilter.Enabled || conn == nil {
		// Обе половины условия — то, чему страж старта не даёт случиться; журнал
		// нужен на случай, если сужатель собирают мимо стража (проба, будущий
		// второй вызывающий): «страница отказывает» обязано быть названо, а не
		// выведено потом из отказов на чтении.
		logger.Warn("per-object list-filter has no rights model to ask — every list will REFUSE "+
			"(no configuration waives this: the emergency bypass knob is retired)",
			"enabled", cfg.AuthZ.ListFilter.Enabled, "authorize_conn", conn != nil)
		conn = nil
	}
	f := authzfilter.New(conn, authzfilter.Config{
		Timeout:               time.Duration(cfg.AuthZ.ListFilter.TimeoutMs) * time.Millisecond,
		CacheTTL:              cfg.AuthZ.ListFilter.CacheTTL,
		CacheMaxEntries:       cfg.AuthZ.ListFilter.MaxEntries,
		SoftPassOnPeerFailure: cfg.AuthZ.ListFilter.FailOpen,
	}).WithLogger(logger)
	logger.Info("per-object list-filter wired",
		// per_call_timeout_ms гейтит ОДИН BatchCheck; operation_budget — потолок
		// всей фильтрации страницы (выводится из per-call и веера). Логируем все
		// три: иначе по конфигу не видно, какое число реально ограничивает запрос.
		"per_call_timeout_ms", cfg.AuthZ.ListFilter.TimeoutMs,
		"operation_budget", f.Budget(),
		"worst_case_depth_waves", f.WorstCaseDepth(),
		"cache_ttl", cfg.AuthZ.ListFilter.CacheTTL,
		"max_entries", cfg.AuthZ.ListFilter.MaxEntries,
		"soft_pass_on_peer_failure", cfg.AuthZ.ListFilter.FailOpen,
		"narrows", f.Narrows(),
	)
	return f
}

// startRegisterDrainer — дилит internal endpoint kacho-iam по ребру vpc→iam (mTLS
// opt-in через mtlsCfg.IAMRegisterClientCreds — enable=false → insecure dev) и
// запускает corelib outbox/drainer поверх kacho_vpc.fga_register_outbox. Каждый
// pending intent переигрывается через InternalIAMService.RegisterResource /
// UnregisterResource (idempotent; Unavailable → retry с backoff; InvalidArgument →
// poison). Run-loop drainer'а владеет claim-CAS для exactly-once между репликами.
// Возвращает closer, закрывающий dial-conn.
//
// buildSyncRegistrar дилит iam-internal :9091 (RegisterResource) тем же
// register-creds, что и register-drainer, и собирает синхронный owner-tuple
// registrar (Decision 2). Отдельный dial-conn (idle-keepalive); возвращает closer.
func buildSyncRegistrar(iamAddr string, mtlsCfg config.MTLSConfig) (*clients.SyncRegistrar, func(), error) {
	creds, err := mtlsCfg.IAMRegisterClientCreds()
	if err != nil {
		return nil, nil, fmt.Errorf("vpc→iam register mTLS creds: %w", err)
	}
	conn, err := grpc.NewClient(iamAddr, creds, grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return nil, nil, fmt.Errorf("dial kacho-iam (sync registrar): %w", err)
	}
	reg, err := clients.NewSyncRegistrar(iamv1.NewInternalIAMServiceClient(conn))
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("собрать синхронный registrar: %w", err)
	}
	return reg, func() { _ = conn.Close() }, nil
}

// iamAddr — listener iam-internal :9091; RegisterResource Internal-only (ban #6).
func startRegisterDrainer(ctx context.Context, iamAddr string, mtlsCfg config.MTLSConfig, pool *pgxpool.Pool, rec metrics.Recorder, logger *slog.Logger) (func(), error) {
	creds, err := mtlsCfg.IAMRegisterClientCreds()
	if err != nil {
		return nil, fmt.Errorf("vpc→iam register mTLS creds: %w", err)
	}
	// idle-склонное ребро (drainer почти все время ждет NOTIFY) → keepalive idle pings.
	conn, err := grpc.NewClient(iamAddr, creds, grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return nil, fmt.Errorf("dial kacho-iam (register-drainer): %w", err)
	}

	iamClient := iamv1.NewInternalIAMServiceClient(conn)
	d, err := drainer.New[clients.FGARegisterPayload](
		pool,
		drainer.Config{
			Table:   fgaRegisterOutboxTable,
			Channel: fgaRegisterOutboxChannel,
			// Order-preserving drain, per resource. Эта таблица несёт И fga.register,
			// И fga.unregister ОДНОГО ресурса, а материализация в iam версионирована
			// лишь ЧАСТИЧНО: source_version-LWW (resource_mirror UPSERT под
			// `source_version < EXCLUDED.source_version`) гейтит ТОЛЬКО ветку
			// ON CONFLICT DO UPDATE, а unregister делает ЖЁСТКИЙ DELETE без
			// tombstone. Переставленный STALE register сравнивать не с чем → он
			// попадает в ветку INSERT и ВОСКРЕШАЕТ mirror-строку УДАЛЁННОГО ресурса;
			// level-triggered реконсайлер iam читает mirror как источник истины и
			// вечно ре-материализует owner-tuple (самоисцеления нет).
			//
			// Порядок ломается и БЕЗ конкурентности: claim сортирует
			// `ORDER BY (attempt_count, id)`, поэтому transiently-bumped register
			// (attempt>=1 после блипа iam) уступает свежему unregister (attempt=0) —
			// уже при ApplyConcurrency=1 (дефолт vpc) и тем более попадает в БОЛЕЕ
			// РАННИЙ батч. PartitionColumn закрывает это на CLAIM-уровне: строка не
			// клеймится, пока в её партиции есть ДОСТАВЛЯЕМЫЙ (sent_at IS NULL AND
			// attempt_count < MaxAttempts) предшественник с меньшим id → per-resource
			// FIFO держится cross-batch и cross-replica; разные ресурсы дренятся
			// параллельно как раньше.
			//
			// Ключ — resource_id (миграция 0008): emitter пишет ОДНУ строку на один
			// FGA-объект и заполняет колонку id-половиной tuple.Object, глобально
			// уникальной by construction (core rule #15) → «одна партиция» == «один
			// объект iam-mirror». Требует partial-index миграции 0018
			// `(resource_id, id) WHERE sent_at IS NULL` под claim'овый NOT EXISTS.
			// Поведение зафиксировано corelib-тестом
			// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister.
			PartitionColumn: "resource_id",
		},
		clients.DecodeFGARegisterPayload,
		clients.NewIAMRegisterApplier(iamClient),
		logger.With(slog.String("component", "fga-register-drainer")),
		// Каждая отравленная строка инкрементит outbox_poisoned_total{table=…},
		// чтобы потерянная доставка owner-tuple была alertable, а не тихим Warn.
		drainer.WithPoisonObserver[clients.FGARegisterPayload](func() {
			rec.IncPoisoned(fgaRegisterOutboxTable)
		}),
		// Каждая ДОСТАВЛЕННАЯ строка инкрементит счётчик своего направления
		// (#1714). Прежде эту величину ставил скан как `count(*)` по живым
		// строкам — совпадая с объявленным «за всё время» ровно до тех пор,
		// пока строки не убираются. Наблюдатель считает СОБЫТИЕ доставки,
		// поэтому уборка на величину не влияет by construction.
		drainer.WithDeliveryObserver[clients.FGARegisterPayload](
			metrics.DeliveryObserver(fgaRegisterOutboxTable, metrics.RegisterOutboxDirections(), rec)),
	)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("build register-drainer: %w", err)
	}

	go func() {
		if rerr := d.Run(ctx); rerr != nil {
			logger.Error("register-drainer stopped", "err", rerr)
		}
	}()
	logger.Info("FGA register-drainer started",
		"iam_addr", iamAddr, "mtls", mtlsCfg.IAMRegisterMTLS.Enable)

	return func() { _ = conn.Close() }, nil
}

// buildServices создает все repo'ы поверх pool и собирает из них бизнес-сервисы.
//
// Группа правил по умолчанию создаётся БЕЗУСЛОВНО: настройки, которая бы это
// отменяла, больше нет, и предупреждения о ней тоже — оно объявляло состояние,
// которое сегодня недостижимо.
//
// slavePool — опц. read-replica pool; nil → kachopg.New делает fallback и Reader-TX
// идут на master.
func buildServices(pool, slavePool *pgxpool.Pool, projectClient repo.ProjectClient, geoClient repo.ZoneRegistry, regionClient repo.RegionRegistry, listFilter *authzfilter.Narrower, opsRepo operations.Repo, registrar fgaregister.Registrar, quotaLimits quota.LimitResolver, quotaAccounts quota.AccountLocator, cfg config.Config, logger *slog.Logger) *services {
	// Прямой write-side FGA убран: каждый Create/Delete ресурса эмитит FGA
	// owner-tuple register/unregister INTENT в своей writer-TX (один commit, без
	// dual-write); register-drainer применяет каждый intent через kacho-iam
	// InternalIAMService.RegisterResource по mTLS. Писателя кортежей прав в
	// use-case'ах больше нет вовсе: права пишет владелец, и только он.

	// Все VPC-ресурсы (Network/Subnet/Address/RouteTable/SecurityGroup/Gateway/
	// NetworkInterface) работают через `kacho.Repository` (Reader/Writer split).
	// pgxpool-impl — `internal/repo/kacho/pg`. Admin-сервисы и peer-port'ы
	// use-case-пакетов получают тонкие adapter'ы поверх kachoRepo из пакета
	// `internal/repo/cqrsadapter`.
	kachoRepo := kachopg.New(pool, slavePool)

	// Adapter'ы под узкие port-интерфейсы admin/peer-сервисов. Каждый adapter
	// открывает свежую Reader/Writer-TX на каждый вызов (read на slave-pool, если он
	// настроен; write — на master).
	// Совещательная полоса учёта собирается ЗДЕСЬ, потому что здесь живёт
	// репозиторий: материализация пишет строки учёта своей writer-транзакцией до
	// мутации. Зависимости, требующие соединений (резолв величин и аккаунт
	// проекта), приходят параметрами — их владелец composition root.
	//
	// nil-резолв означает «раннего отказа нет», а не «предела нет»: разбор — у
	// объявления quotaLimits выше.
	var quotaGuard *quota.Guard
	if quotaLimits != nil && quotaAccounts != nil {
		quotaGuard = quota.NewGuard(kachoRepo, quotaLimits, quotaAccounts, "vpc")
	}

	networkAdapter := cqrsadapter.NewNetwork(kachoRepo)
	subnetAdapter := cqrsadapter.NewSubnet(kachoRepo)
	addressAdapter := cqrsadapter.NewAddress(kachoRepo)
	routeTableAdapter := cqrsadapter.NewRouteTable(kachoRepo)
	sgAdapter := cqrsadapter.NewSecurityGroup(kachoRepo)
	niAdapter := cqrsadapter.NewNetworkInterface(kachoRepo)

	// Шов с исполнителем датаплейна. Адаптер работает НАПРЯМУЮ с пулом, а не
	// через `kacho.Repository`: его предмет — проекция намерения и таблицы
	// ресурсов, читаемые в ОДНОМ снимке (REPEATABLE READ), и разложить это по
	// per-resource читателям значило бы читать курсор и тела разными
	// транзакциями — то есть отдавать пару, которой ни в один момент не
	// существовало.

	// AddressPool — admin-only use-case-структура (см.
	// `internal/apps/kacho/api/addresspool/`). Composition root собирает use-case'ы +
	// ResolverService под единый Handler. Все use-case'ы работают через `kachoRepo`
	// (CQRS-Repository) — каждый mutate открывает писатель, делает DML + outbox emit в
	// одной TX. Узкие read-port'ы Address/Subnet/Network удовлетворяются adapter'ами
	// поверх kachoRepo (cqrsadapter.Address / Subnet / Network).
	addressPoolResolver := addresspoolapp.NewResolverService(
		kachoRepo, addressAdapter, subnetAdapter,
	)
	//
	// Use-case'ы собираются ОДИН раз и передаются в ОБА транспорта — внутренний
	// (ответ ресурсом, :9091) и публичный (ответ `Operation`, :9090). Именно так
	// «два пути записи делают одно» держится построением: разойтись валидацией,
	// умолчаниями или набором заполняемых полей им нечем — тело одно.
	// Собрать второй набор здесь значило бы завести дубль, который разъедется
	// молча и станет наблюдаемым только когда консоль уже на публичном пути, а
	// оператор ещё на внутреннем.
	poolCreate := addresspoolapp.NewCreateAddressPoolUseCase(kachoRepo, geoClient)
	poolUpdate := addresspoolapp.NewUpdateAddressPoolUseCase(kachoRepo)
	poolDelete := addresspoolapp.NewDeleteAddressPoolUseCase(kachoRepo)
	poolBind := addresspoolapp.NewBindAsNetworkDefaultUseCase(kachoRepo, networkAdapter)
	poolUnbind := addresspoolapp.NewUnbindNetworkDefaultUseCase(kachoRepo)
	poolAddCidr := addresspoolapp.NewAddCidrBlocksUseCase(kachoRepo)
	poolRemoveCidr := addresspoolapp.NewRemoveCidrBlocksUseCase(kachoRepo)

	addressPoolHandler := addresspoolapp.NewHandler(
		poolCreate,
		poolUpdate,
		poolDelete,
		addresspoolapp.NewGetAddressPoolUseCase(kachoRepo),
		addresspoolapp.NewListAddressPoolsUseCase(kachoRepo),
		poolBind,
		poolUnbind,
		addresspoolapp.NewGetPoolUtilizationUseCase(kachoRepo),
		addresspoolapp.NewListPoolAddressesUseCase(kachoRepo),
		poolAddCidr,
		poolRemoveCidr,
	)
	addressPoolPublicHandler := addresspoolapp.NewPublicHandler(
		addressPoolHandler,
		addresspoolapp.NewAsyncMutations(
			opsRepo,
			poolCreate,
			poolUpdate,
			poolDelete,
			poolBind,
			poolUnbind,
			poolAddCidr,
			poolRemoveCidr,
		),
	)

	addressRefSvc := addressref.NewService(addressAdapter)

	// Network — use-case-структура. Все use-case'ы работают через kachoRepo (CQRS);
	// checkNetworkEmpty / default-SG cleanup в Network.Delete получают subnet/RT/SG
	// adapter'ы, отделенные от writer-TX (каждый открывает свою TX).
	// defaultSGInline=true (default) — при Network.Create в одной writer-TX создается
	// inline default SG и Network.default_security_group_id заполняется атомарно.
	netCreateUC := networkapp.NewCreateNetworkUseCase(kachoRepo, projectClient, opsRepo).
		WithLogger(logger).WithRegistrar(registrar).WithQuotaGuard(quotaGuard)
	netUpdateUC := networkapp.NewUpdateNetworkUseCase(kachoRepo, opsRepo).WithRegistrar(registrar)
	netDeleteUC := networkapp.NewDeleteNetworkUseCase(kachoRepo, subnetAdapter, routeTableAdapter, sgAdapter, opsRepo)
	// Per-page FGA-фильтр (listFilter) питает ТОЛЬКО List; Get авторизуется
	// прямым per-object Check'ом в interceptor'е. listFilter == nil → passthrough.
	netGetUC := networkapp.NewGetNetworkUseCase(kachoRepo)
	netListUC := networkapp.NewListNetworksUseCase(kachoRepo, listFilter)
	netAddCidrUC := networkapp.NewAddCidrBlocksUseCase(kachoRepo, opsRepo)
	netRemoveCidrUC := networkapp.NewRemoveCidrBlocksUseCase(kachoRepo, opsRepo)
	netListSubUC := networkapp.NewListSubnetsUseCase(kachoRepo, subnetAdapter)
	netListSGUC := networkapp.NewListSecurityGroupsUseCase(kachoRepo, sgAdapter)
	netListRTUC := networkapp.NewListRouteTablesUseCase(kachoRepo, routeTableAdapter)
	netListOpsUC := networkapp.NewListOperationsUseCase(opsRepo)
	netHandler := networkapp.NewHandler(
		netCreateUC, netUpdateUC, netDeleteUC,
		netGetUC, netListUC, netAddCidrUC, netRemoveCidrUC,
		netListSubUC, netListSGUC, netListRTUC, netListOpsUC,
	)

	// Gateway use-case'ы работают через CQRS-Repository (kachoRepo) — конструктор
	// принимает Repository, каждый use-case открывает Reader/Writer внутри.
	gwCreateUC := gatewayapp.NewCreateGatewayUseCase(kachoRepo, projectClient, opsRepo).WithRegistrar(registrar).
		WithQuotaGuard(quotaGuard)
	gwHandler := gatewayapp.NewHandler(
		gwCreateUC,
		gatewayapp.NewUpdateGatewayUseCase(kachoRepo, opsRepo).WithRegistrar(registrar),
		gatewayapp.NewDeleteGatewayUseCase(kachoRepo, opsRepo),
		gatewayapp.NewGetGatewayUseCase(kachoRepo),
		gatewayapp.NewListGatewaysUseCase(kachoRepo, listFilter),
		gatewayapp.NewListOperationsUseCase(opsRepo),
	)

	// RouteTable use-case'ы работают через CQRS-Repository. routeTableAdapter
	// передается Network.Delete для child-check.
	rtCreateUC := routetableapp.NewCreateRouteTableUseCase(kachoRepo, projectClient, opsRepo).WithRegistrar(registrar).
		WithQuotaGuard(quotaGuard)
	rtHandler := routetableapp.NewHandler(
		rtCreateUC,
		routetableapp.NewUpdateRouteTableUseCase(kachoRepo, opsRepo).WithRegistrar(registrar),
		routetableapp.NewDeleteRouteTableUseCase(kachoRepo, opsRepo),
		routetableapp.NewGetRouteTableUseCase(kachoRepo),
		routetableapp.NewListRouteTablesUseCase(kachoRepo, listFilter),
		routetableapp.NewListOperationsUseCase(opsRepo),
	)

	// Subnet use-case'ы работают через CQRS-Repository (kachoRepo). niAdapter
	// передается в Delete для precondition-check «нет привязанных NIC».
	// Перечень служебных диапазонов читается ОДНИМ методом настроек — тем же,
	// который спрашивает страж старта (cfg.ValidateReservedPrefixes выше). Поэтому
	// «страж пропустил» ⟺ «путь запроса сверяется с тем же перечнем»; своя сборка
	// значения здесь дала бы два места об одном предмете. Оба глагола, объявляющих
	// диапазон подсети (Create и :addCidrBlocks), получают его — второй не менее
	// важен: без него обход занимает один дополнительный запрос. Провязку держит
	// гейт reserved_prefixes_wiring_test.go.
	reservedPrefixes := cfg.ReservedPrefixes()
	subnetCreateUC := subnetapp.NewCreateSubnetUseCase(kachoRepo, projectClient, geoClient, regionClient, opsRepo).
		WithRegistrar(registrar).
		WithReservedPrefixes(reservedPrefixes).
		WithQuotaGuard(quotaGuard)
	subnetHandler := subnetapp.NewHandler(
		subnetCreateUC,
		subnetapp.NewUpdateSubnetUseCase(kachoRepo, opsRepo).WithRegistrar(registrar),
		subnetapp.NewDeleteSubnetUseCase(kachoRepo, niAdapter, opsRepo),
		subnetapp.NewGetSubnetUseCase(kachoRepo),
		subnetapp.NewListSubnetsUseCase(kachoRepo, listFilter),
		subnetapp.NewAddCidrBlocksUseCase(kachoRepo, opsRepo).WithReservedPrefixes(reservedPrefixes),
		subnetapp.NewRemoveCidrBlocksUseCase(kachoRepo, opsRepo),
		subnetapp.NewListUsedAddressesUseCase(kachoRepo, addressAdapter),
		subnetapp.NewListOperationsUseCase(opsRepo),
	)

	// Address — use-case-структура. Composition с AddressPoolService для IPAM cascade
	// resolve. Internal Allocate UC отделен — принимается
	// InternalAddressAllocateHandler через узкий port.
	//
	// Все Address use-cases работают через CQRS-Repository (`kachoRepo`). IPAM
	// atomicity (Insert + Allocate + Outbox) гарантируется одной writer-TX в
	// `CreateAddressUseCase.doCreate` / `AllocateUseCase.*`. subnetAdapter — peer-port
	// для SubnetReader (Get + AddressesBySubnet), удовлетворяется тем же kachoRepo
	// через cqrsadapter.
	addressCreateUC := addressapp.NewCreateAddressUseCase(kachoRepo, subnetAdapter, projectClient, opsRepo, addressPoolResolver).
		WithRegistrar(registrar).
		WithZoneRegistry(geoClient).
		WithQuotaGuard(quotaGuard)
	addressUpdateUC := addressapp.NewUpdateAddressUseCase(kachoRepo, opsRepo).WithRegistrar(registrar)
	addressDeleteUC := addressapp.NewDeleteAddressUseCase(kachoRepo, opsRepo)
	addressGetUC := addressapp.NewGetAddressUseCase(kachoRepo)
	addressGetByValueUC := addressapp.NewGetByValueUseCase(kachoRepo)
	addressListUC := addressapp.NewListAddressesUseCase(kachoRepo, listFilter)
	addressListBySubnetUC := addressapp.NewListBySubnetUseCase(kachoRepo, subnetAdapter)
	addressListOpsUC := addressapp.NewListOperationsUseCase(opsRepo)
	addressAllocateUC := addressapp.NewAllocateUseCase(kachoRepo, addressPoolResolver)
	addressReleaseUC := addressapp.NewReleaseOwnedAddressUseCase(kachoRepo)
	addressHandler := addressapp.NewHandler(
		addressCreateUC, addressUpdateUC, addressDeleteUC,
		addressGetUC, addressGetByValueUC, addressListUC, addressListBySubnetUC, addressListOpsUC,
	).WithLeaseReleaser(addressReleaseUC)

	// SecurityGroup — use-case-структура. Split-endpoint Update / UpdateRules /
	// UpdateRule (OCC через xmin в repo). Все DML + outbox-emit идут в одной writer-TX.
	// sgAdapter (cqrsadapter поверх kachoRepo) передается в Network use-case'ы для
	// checkNetworkEmpty / default-SG cleanup при Network.Delete (отдельная TX от
	// Network writer'а).
	sgCreateUC := sgapp.NewCreateSecurityGroupUseCase(kachoRepo, networkAdapter, projectClient, opsRepo).
		WithSGReader(sgAdapter).WithRegistrar(registrar).WithQuotaGuard(quotaGuard)
	sgHandler := sgapp.NewHandler(
		sgCreateUC,
		sgapp.NewUpdateSecurityGroupUseCase(kachoRepo, opsRepo).WithSGReader(sgAdapter).WithRegistrar(registrar),
		// sgAdapter (SecurityGroupReader) — same-network-валидация SG-target-правил
		// на UpdateRules/UpdateRule.
		sgapp.NewUpdateRulesUseCase(kachoRepo, opsRepo, sgAdapter),
		sgapp.NewUpdateRuleUseCase(kachoRepo, opsRepo, sgAdapter),
		sgapp.NewDeleteSecurityGroupUseCase(kachoRepo, opsRepo),
		sgapp.NewGetSecurityGroupUseCase(kachoRepo),
		sgapp.NewListSecurityGroupsUseCase(kachoRepo, listFilter),
		sgapp.NewListOperationsUseCase(kachoRepo, opsRepo),
	)

	// NetworkInterface — use-case-структура. Все use-case'ы работают через
	// CQRS-Repository (`kachoRepo`). У NIC нет Move RPC (NIC привязан к Subnet).
	// Address-attach/detach идёт через writer-TX (`w.Addresses()`) внутри Create/
	// Update — отдельный addressAdapter в эти UC больше не передаётся.
	//
	// Ограничение полосы, задаваемое арендатором, принимается ровно тогда, когда
	// посадка объявила умение исполнителя, и в промежутке, верхний край которого —
	// её же объявленная гарантия. Правило собирается ОДИН раз и раздаётся обоим
	// путям: собери его дважды — и стенд однажды получит поле, которое нельзя
	// задать при создании и можно дописать изменением. Пустоту промежутка при
	// объявленном умении не пускает страж старта (`cfg.ValidateExecutorProfile`
	// выше), поэтому здесь остаётся чтение настроек, а не своя арифметика.
	bandwidthPolicy := domain.NewBandwidthLimitPolicy(
		cfg.Dataplane.Executor.TenantSettableBandwidthLimit,
		cfg.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps,
	)
	niCreateUC := niapp.NewCreateNetworkInterfaceUseCase(kachoRepo, projectClient, opsRepo).
		WithRegistrar(registrar).
		WithBandwidthLimitPolicy(bandwidthPolicy).
		WithQuotaGuard(quotaGuard)
	niHandler := niapp.NewHandler(
		niCreateUC,
		niapp.NewUpdateNetworkInterfaceUseCase(kachoRepo, opsRepo).
			WithRegistrar(registrar).
			WithBandwidthLimitPolicy(bandwidthPolicy),
		niapp.NewDeleteNetworkInterfaceUseCase(kachoRepo, opsRepo),
		niapp.NewGetNetworkInterfaceUseCase(kachoRepo),
		niapp.NewListNetworkInterfacesUseCase(kachoRepo, listFilter),
		niapp.NewListOperationsUseCase(opsRepo),
	)

	// CidrGroup — use-case-структура. Форма ровно та же, что у сети: чтение
	// синхронно, мутации через операцию, состав правится глаголами. Потолок
	// состава и отсутствие затирания живут в writer'е репозитория (условный
	// инкремент счётчика под блокировкой строки), а не в этих use-case'ах.
	cgHandler := cidrgroupapp.NewHandler(
		cidrgroupapp.NewCreateCidrGroupUseCase(kachoRepo, projectClient, opsRepo).
			WithLogger(logger).WithRegistrar(registrar).WithQuotaGuard(quotaGuard),
		cidrgroupapp.NewUpdateCidrGroupUseCase(kachoRepo, opsRepo).WithRegistrar(registrar),
		cidrgroupapp.NewDeleteCidrGroupUseCase(kachoRepo, opsRepo),
		cidrgroupapp.NewGetCidrGroupUseCase(kachoRepo),
		cidrgroupapp.NewListCidrGroupsUseCase(kachoRepo, listFilter),
		cidrgroupapp.NewAddCidrBlocksUseCase(kachoRepo, opsRepo),
		cidrgroupapp.NewRemoveCidrBlocksUseCase(kachoRepo, opsRepo),
		cidrgroupapp.NewListOperationsUseCase(opsRepo),
	)

	return &services{
		networkHandler:          netHandler,
		subnetHandler:           subnetHandler,
		addressHandler:          addressHandler,
		addressAllocate:         addressAllocateUC,
		addressRefService:       addressRefSvc,
		routeTableHandler:       rtHandler,
		securityGroupHandler:    sgHandler,
		gatewayHandler:          gwHandler,
		addressPoolHandler:      addressPoolHandler,
		addressPoolPublic:       addressPoolPublicHandler,
		networkInternal:         networkinternal.NewService(networkAdapter, sgAdapter),
		networkInterfaceHandler: niHandler,
		// InternalNetworkInterfaceService — NIC↔Instance attach-CAS (:9091, §3a).
		// Работает напрямую через CQRS-Repository (kachoRepo): attach/detach — writer-TX
		// с атомарным CAS, ListByInstance — batched reader. geoClient резолвит зону
		// инстанса в регион для региональной полосы placement-coherence (anycast-
		// подсеть зоны не несёт) — регион из имени зоны не выводится. listFilter даёт
		// per-object видимость для ListByInstance: инстансы называет вызывающий, и
		// per-RPC Check тут семантически невозможен (ScopeFiltered, см. PermissionMap);
		// в production его наличие гарантирует boot-guard ValidateListFilter.
		networkInterfaceInternal: nicinternal.NewService(kachoRepo).
			WithZoneRegistry(geoClient).
			WithListFilter(listFilter),
		cidrGroupHandler: cgHandler,
		quotaHandler:     quotaHandlerOrNil(quotaGuard),
	}
}

// quotaHandlerOrNil возвращает обработчик чтения квот ЛИБО настоящий nil.
//
// Возврат `*quotaapp.Handler(nil)` в поле структуры был бы не тем же самым:
// проверка `svcs.quotaHandler != nil` на типизированном nil ИСТИННА, и метод
// зарегистрировался бы, чтобы упасть на первом же вызове. Тот же класс уже
// стоил паники на пути создания ресурса, поэтому решение принимается здесь, где
// тип ещё конкретен.
func quotaHandlerOrNil(g *quota.Guard) *quotaapp.Handler {
	if g == nil {
		return nil
	}
	return quotaapp.NewHandler(g)
}

// registerPublicServices — публичные RPC + OperationService на внешний listener.
func registerPublicServices(srv grpc.ServiceRegistrar, svcs *services, opsRepo operations.Repo) {
	vpcv1.RegisterNetworkServiceServer(srv, svcs.networkHandler)
	vpcv1.RegisterSubnetServiceServer(srv, svcs.subnetHandler)
	vpcv1.RegisterAddressServiceServer(srv, svcs.addressHandler)
	vpcv1.RegisterRouteTableServiceServer(srv, svcs.routeTableHandler)
	vpcv1.RegisterSecurityGroupServiceServer(srv, svcs.securityGroupHandler)
	vpcv1.RegisterGatewayServiceServer(srv, svcs.gatewayHandler)
	vpcv1.RegisterNetworkInterfaceServiceServer(srv, svcs.networkInterfaceHandler)
	vpcv1.RegisterCidrGroupServiceServer(srv, svcs.cidrGroupHandler)
	// AddressPoolService — административная поверхность пула на ПУБЛИЧНОМ
	// слушателе под правом `system_admin` @ `cluster` (ADM-1 S1). Не нарушение
	// запрета 6: `Internal*`-сервис на внешний край не выставлен и предикат,
	// который это ловит, не тронут — переехал ГЛАГОЛ, а не разрешение.
	vpcv1.RegisterAddressPoolServiceServer(srv, svcs.addressPoolPublic)
	// Чтение квот выставляется, ТОЛЬКО когда полоса учёта собрана. Иначе метод
	// отвечал бы пустым набором на каждый запрос — то есть «квот нет», ровно то
	// утверждение, которое контракт запрещает делать (`ListQuotasResponse`:
	// пустой массив зарезервирован за состоянием, которого этот сервис не
	// сообщает). Незарегистрированный метод отвечает `Unimplemented`, и это
	// честно: возможности здесь действительно нет.
	if svcs.quotaHandler != nil {
		vpcv1.RegisterQuotaServiceServer(srv, svcs.quotaHandler)
	}
	operationpb.RegisterOperationServiceServer(srv, operationspb.NewHandler(opsRepo))
}

// registerInternalServices — kacho-only/admin RPC на internal listener.
func registerInternalServices(srv grpc.ServiceRegistrar, svcs *services, subscribe subscriptionv1.InternalSubscriptionServiceServer) {
	// `WithOwnedCreator` — путь `CreateOwnedAddress`: создание адреса, СРАЗУ
	// привязанного к владельцу, одной writer-TX. Реализуется публичным
	// транспортным handler'ом адреса, чтобы разбор тела создания оставался
	// единственным на оба пути.
	// `WithLeaseReleaser` — путь `ReleaseOwnedAddress`: снятие аренды по
	// предъявлению владения ею, одной writer-TX, с НАЗВАННЫМ исходом. Право
	// анкорится на проекте (как у создания аренды), поэтому пообъектной пробы
	// существования у глагола нет, а значит нет и полосы скрытия, из которой
	// вызывающий выводил бы «работа сделана».
	vpcv1.RegisterInternalAddressServiceServer(srv,
		handler.NewInternalAddressAllocateHandler(svcs.addressAllocate, svcs.addressRefService).
			WithOwnedCreator(svcs.addressHandler).
			WithLeaseReleaser(svcs.addressHandler))
	vpcv1.RegisterInternalAddressPoolServiceServer(srv, svcs.addressPoolHandler)
	vpcv1.RegisterInternalNetworkServiceServer(srv, handler.NewInternalNetworkHandler(svcs.networkInternal))
	// InternalNetworkInterfaceService — NIC↔Instance attach-CAS (:9091, ban #6): не на
	// external mux (INV-2). Регистрируется на internalSrv → та же authz-Check-цепочка
	// интерсепторов (internalUnary + authzIntr), что и прочие internal RPC (INV-2a).
	vpcv1.RegisterInternalNetworkInterfaceServiceServer(srv, handler.NewInternalNetworkInterfaceHandler(svcs.networkInterfaceInternal))
	// Поток изменений — ТОЛЬКО на внутреннем слушателе (:9091, ban #6). Глагол
	// объявлен сужаемым по правам вызывающего, то есть за ним нет пообъектной
	// проверки на крае: сужение делает сам владелец на каждой строке. Выставить
	// его наружу значило бы отдать журнал домена внешнему периметру.
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, subscribe)
}

// buildSubscriptionServer — общий сервер потока над журналом vpc.
//
// Соединение он держит ВНЕ пула: `LISTEN` требует своей сессии, а сессия из пула
// вернулась бы в него вместе с подпиской. Поэтому сюда едет строка соединения, а
// не пул.
func buildSubscriptionServer(
	cfg config.Config, listFilter *authzfilter.Narrower, logger *slog.Logger,
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
		Journal:      subscriptionjournal.Journal(),
		DSN:          dsn,
		Narrower:     listFilter,
		ProjectGate:  gate,
		MaxStreams:   cfg.APIServer.SubscriptionMaxStreams,
		StreamBudget: cfg.APIServer.SubscriptionStreamBudget,
		IdlePoll:     cfg.APIServer.SubscriptionIdlePoll,
		Logger:       logger,
	})
	if err != nil {
		return nil, fmt.Errorf("subscription server: %w", err)
	}
	return srv, nil
}

// maskDSN отдает DSN с замаскированным паролем — для безопасного логирования
// slave-URL. Возвращает оригинальную строку, если она не парсится как URL.
// Если password не найден, ничего не меняет (DSN без пароля — нормальная
// dev-конфигурация sslmode=disable).
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if _, hasPwd := u.User.Password(); !hasPwd {
		return dsn
	}
	u.User = url.UserPassword(u.User.Username(), "***")
	return u.String()
}
