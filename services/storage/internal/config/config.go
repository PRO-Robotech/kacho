// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-storage из переменных окружения через corelib
// config.LoadPrefixed("KACHO_STORAGE"). Per-edge TLS-структуры (grpcclient.TLSClient
// / grpcsrv.TLSServer) получают независимые имена KACHO_STORAGE_<EDGE>_<NAME> —
// префикс на каждое ребро, без общего TLS-синглтона на процесс.
package config

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// envPrefix — корневой сегмент env-имён kacho-storage (KACHO_<DOMAIN>).
const envPrefix = "KACHO_STORAGE"

// DBSchema — Postgres-схема (database-per-service): своя БД kacho_storage.
const DBSchema = "kacho_storage"

// Config — конфигурация kacho-storage.
type Config struct {
	DBHost     string `envconfig:"KACHO_STORAGE_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_STORAGE_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_STORAGE_DB_USER" default:"storage"`
	DBPassword string `envconfig:"KACHO_STORAGE_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_STORAGE_DB_NAME" default:"kacho_storage"`
	// DBSSLMode — sslmode для DSN. dev по умолчанию disable; в проде обязателен
	// require|verify-ca|verify-full.
	DBSSLMode string `envconfig:"KACHO_STORAGE_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx-пула (0 = дефолт pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_STORAGE_DB_MAX_CONNS" default:"0"`

	// GrpcPort — публичный листенер (Volume/Snapshot/DiskType Service).
	GrpcPort string `envconfig:"KACHO_STORAGE_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — cluster-internal листенер (InternalVolume/InternalDiskType).
	// Не выставляется на внешнем endpoint api-gateway — только cluster-internal.
	InternalGrpcPort string `envconfig:"KACHO_STORAGE_INTERNAL_PORT" default:"9091"`
	// MetricsAddr — адрес cluster-internal diagnostic HTTP-listener'а (/healthz,
	// /metrics). Пустое значение явно отключает listener (back-compat).
	MetricsAddr string `envconfig:"KACHO_STORAGE_METRICS_ADDR" default:":9095"`

	// AuthMode — fail-closed режим: dev | production | production-strict.
	AuthMode string `envconfig:"KACHO_STORAGE_AUTH_MODE" default:"production"`

	// ===== peer-адреса (runtime cross-domain edges) =====

	// GeoGRPCAddr — public endpoint kacho-geo для валидации zone_id
	// (ребро storage→geo, ZoneService.Get). Пусто → GeoClient fail-closed.
	GeoGRPCAddr string `envconfig:"KACHO_STORAGE_GEO_GRPC_ADDR" default:""`
	// IAMGRPCAddr — endpoint kacho-iam для валидации project_id
	// (ребро storage→iam, ProjectService.Get). Пусто → IAMClient fail-closed.
	IAMGRPCAddr string `envconfig:"KACHO_STORAGE_IAM_GRPC_ADDR" default:""`
	// AuthZIAMGRPCAddr — internal endpoint kacho-iam для per-RPC Check
	// (ребро storage→iam authz, InternalIAMService.Check). Пусто → authz-интерсептор
	// не подключается (грациозный dev-старт без kacho-iam). Тот же endpoint несёт
	// InternalIAMService.RegisterResource/UnregisterResource (FGA-proxy, Internal-only
	// :9091) — его переиспользует register-drainer + sync-registrar.
	AuthZIAMGRPCAddr string `envconfig:"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR" default:""`

	// AuthZTrustedForwarderSANs — allow-list личностей сертификата (SPIFFE-SAN),
	// которым разрешено ПЕРЕДАВАТЬ личность конечного пользователя в метаданных
	// x-kacho-principal-*. Пробрасывается в оба листенера через
	// grpcsrv.WithTrustedForwarders (см. cmd/storage/serve.go).
	//
	// Почему это ручка, а не константа, и почему её отсутствие было дырой: contract
	// corelib (pkg/grpcsrv principalIsTrusted) сужает круг отправителей ТОЛЬКО когда
	// список непуст; на пустом он отвечает «доверяем» любому пиру, прошедшему
	// проверку сертификата. Внутренний периметр у нас объявлен НЕдоверенным, поэтому
	// пустой список означает: любой сосед со своим законным клиентским сертификатом
	// присылает заголовки личности жертвы, и решение о правах принимается от её
	// имени. Раньше поля не было вовсе, а serve.go передавал литеральный пустой
	// список — то есть сузить круг было нечем.
	//
	// Формат — список через запятую. Законных отправителей ДВА, и оба обязаны быть
	// в списке (иначе ломается рабочий путь):
	//   - api-gateway — передаёт личность пользователя на публичный :9090;
	//   - compute — передаёт её же на внутренний :9091 (привязка/отвязка тома идёт
	//     под личностью того, кто её инициировал, см. compute
	//     internal/clients/storage_client.go → pkg/auth.PropagateOutgoing).
	// Канонические значения — в values.prod.
	//
	// Пусто допустимо ТОЛЬКО в dev (in-process фикстуры); в любом боевом режиме
	// Validate() отказывает в старте (fail-closed, зеркалит geo/compute/nlb).
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS"`

	// FGARegisterDrainerEnabled — включает register-drainer owner-tuple'ов (SEC-D):
	// применяет fga_register_outbox-intents через kacho-iam RegisterResource/
	// UnregisterResource по ребру storage→iam (AuthZIAMGRPCAddr, mTLS). Default true;
	// без него созданные Volume/Snapshot не получают owner-tuple → анти-BOLA
	// scope_extractor не резолвит target→project. false → intents копятся
	// неприменёнными (dev/degraded). Требует непустой AuthZIAMGRPCAddr.
	FGARegisterDrainerEnabled bool `envconfig:"KACHO_STORAGE_FGA_REGISTER_DRAINER_ENABLED" default:"true"`

	// ===== per-object filtered List =====
	//
	// Все ListFilter* — production-edition: configurable, без хардкода. Адрес
	// authorize-эндпоинта переиспользует AuthZIAMGRPCAddr (kacho-iam internal :9091,
	// AuthorizeService.BatchCheck) и те же per-edge creds IAMClientMTLS.
	//
	// NB: knob'а «размер allow-list» здесь НЕТ и быть не может: видимость
	// спрашивается per-object по УЖЕ прочитанной странице, поэтому усечь её нечем —
	// полнота и стоимость задаются page_size, а не ручкой (перечисление всех
	// разрешённых объектов даёт усечение сверх жёсткого предела ListObjects).

	// ListFilterEnabled — master-switch. true → List спрашивает iam.BatchCheck про
	// id прочитанной страницы. false → фильтра нет (passthrough); production
	// boot-guard такую посадку ЗАПРЕЩАЕТ (см. Validate).
	ListFilterEnabled bool `envconfig:"KACHO_STORAGE_LIST_FILTER_ENABLED" default:"true"`

	// ListFilterTimeoutMs — per-call deadline ОДНОГО iam.BatchCheck (страница >100 id
	// режется на батчи ≤100, у каждого свой deadline), НЕ бюджет всей фильтрации:
	// бюджет операции выводится в authzfilter.NewFGAFilter из контракта (максимум
	// страницы ÷ предел батча ÷ параллелизм). 1000ms — реалистичный допуск на ОДИН
	// хоп: батчи идут ограниченным fan-out'ом, поэтому worst-case глубина на
	// предельной странице (page_size=1000) — 4 волны, а не 20 последовательных хопов.
	ListFilterTimeoutMs int `envconfig:"KACHO_STORAGE_LIST_FILTER_TIMEOUT_MS" default:"1000"`

	// ListFilterCacheTTLMs — TTL ПОЛОЖИТЕЛЬНОГО per-object вердикта. Короткий (5s),
	// чтобы revoke access-binding был виден ≤5s. Отрицательные вердикты не кешируются
	// вовсе (иначе свежий грант / только что созданный свой ресурс были бы невидимы
	// весь TTL).
	ListFilterCacheTTLMs int `envconfig:"KACHO_STORAGE_LIST_FILTER_CACHE_TTL_MS" default:"5000"`

	// ListFilterCacheMaxEntries — bound кеша, вытеснение LRU. Кеш НИКАК не
	// ограничивает видимость: страница проверяется целиком независимо от его размера.
	ListFilterCacheMaxEntries int `envconfig:"KACHO_STORAGE_LIST_FILTER_CACHE_MAX_ENTRIES" default:"10000"`

	// ListFilterFailOpen — degraded mode. true → на ошибке iam страница отдаётся
	// НЕотфильтрованной (+ audit-WARN); false → Unavailable. Default false
	// (fail-closed = secure); true — только break-glass.
	ListFilterFailOpen bool `envconfig:"KACHO_STORAGE_LIST_FILTER_FAIL_OPEN" default:"false"`

	// ListFilterBreakglass — аварийный режим: когда модели прав на этой посадке нет
	// вовсе (фильтр выключен либо эндпоинт не задан), списки и гейт объекта отдают
	// проход вместо отказа.
	//
	// Он остаётся явным исключением, а не умолчанием: прежде «фильтр выключен» само
	// по себе означало сквозной проход, и вся защита держалась на загрузочном страже
	// — то есть существовала ровно до первой конфигурации, которая его не взвела.
	// Теперь пропуск требуется ОБЪЯВИТЬ, и каждое срабатывание считается и
	// называется (`listnarrow.Counts`).
	ListFilterBreakglass bool `envconfig:"KACHO_STORAGE_LIST_FILTER_BREAKGLASS" default:"false"`

	// ===== per-edge mTLS =====

	// GeoClientMTLS — client-creds ребра storage→geo (:9090).
	GeoClientMTLS grpcclient.TLSClient `envconfig:"GEO_CLIENT_MTLS"`
	// IAMClientMTLS — client-creds ребра storage→iam (:9090 / :9091 authz).
	IAMClientMTLS grpcclient.TLSClient `envconfig:"IAM_CLIENT_MTLS"`
	// PublicServerMTLS — server-creds публичного листенера (:9090).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`
	// InternalServerMTLS — server-creds cluster-internal листенера (:9091).
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`
}

// TrustedForwarders — список личностей сертификата, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/storage/serve.go), и стража старта (Validate), и самоотчёт о посадке
// (cmd/storage/bootposture.go). Поэтому «стража пропустила» ⟺ «круг отправителей
// реально сужен» — по построению, а не по совпадению; разъехаться им нечем.
//
// Отбрасывает пустые записи, потому что их отбрасывает и corelib
// (WithTrustedForwarders пропускает только s != ""): список из одних пустых строк
// (`SANS=","`) там вырождается в пустое множество, то есть снова «доверяем любому».
// Считать такую строку заполненной значило бы пропустить дыру через гейт.
//
// Пробелы по краям срезаются — и это НЕ зеркало corelib, а осознанное расхождение:
// corelib сравнивает личность сертификата побайтово (CertIdentity отдаёт SAN как
// есть), поэтому запись " spiffe://…" не совпала бы там ни с одним сертификатом.
// Без среза оператор, написавший список через «запятая-пробел», получил бы не
// отказ старта, а молчаливый отказ в обслуживании законному отправителю. Круг
// доверенных от этого не расширяется: в него попадают ровно те строки, которые
// оператор перечислил, — срезаются только окружающие пробелы.
func (c Config) TrustedForwarders() []string {
	out := make([]string, 0, len(c.AuthZTrustedForwarderSANs))
	for _, s := range c.AuthZTrustedForwarderSANs {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// PublicServerCreds возвращает grpc.ServerOption для публичного листенера (:9090).
func (c Config) PublicServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.PublicServerMTLS)
}

// InternalServerCreds возвращает grpc.ServerOption для internal-листенера (:9091).
func (c Config) InternalServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.InternalServerMTLS)
}

// schemaOptionsParam — URL-encoded libpq options=-c search_path=kacho_storage,public.
// Каждое соединение (pgxpool + goose database/sql) видит схему без отдельного SET.
const schemaOptionsParam = "options=-c%20search_path%3Dkacho_storage%2Cpublic"

// baseDSN — стандартный postgres DSN (для pgxpool и database/sql), несёт search_path
// kacho_storage через libpq options.
func (c Config) baseDSN() string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, mode, schemaOptionsParam,
	)
}

// DSN — строка подключения для pgxpool (поддерживает pool_max_conns). НЕ для
// database/sql (pool_max_conns → неизвестный PG-параметр → FATAL).
func (c Config) DSN() string {
	dsn := c.baseDSN()
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// MigrateDSN — строка подключения для goose/database/sql (без pgxpool-параметров).
func (c Config) MigrateDSN() string {
	return c.baseDSN()
}

// Load загружает конфигурацию из переменных окружения.
func Load() (Config, error) {
	var c Config
	err := corecfg.LoadPrefixed(envPrefix, &c)
	return c, err
}
