// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — viper-based YAML config для kacho-nlb.
//
// Иерархия секций — спека `docs/superpowers/specs/2026-05-23-kacho-nlb-design.md `:
//
//	logger / api-server / metrics / healthcheck / repository.postgres /
//	authn / extapi / authz.iam (+ cache, deny-budget) /
//	fga.register-drainer  / mtls (opt-in).
//
// ENV-binding через viper с делимитером `__`:
//
//	KACHO_NLB_API_SERVER__ENDPOINT              → api-server.endpoint
//	KACHO_NLB_REPOSITORY__POSTGRES__URL         → repository.postgres.url
//	KACHO_NLB_FGA__REGISTER_DRAINER__ENABLE     → fga.register-drainer.enable
//	KACHO_NLB_LOGGER__LEVEL                     → logger.level
//
// Defaults — в `defaults.go` (`RegisterDefaults`); validation — в `validate.go`
// (`Config.Validate`, `Mode` enum); loader — в `load.go` (`Load(path)`).
//
// **Anti-pattern protection :**
//   - НЕ envconfig в struct tags — только mapstructure через viper.
//   - НЕ defaults в struct tags — только в `RegisterDefaults`.
//   - НЕ `bool productionMode` — `Mode` enum (ModeDev / ModeProduction).
//   - НЕ `cfg.AuthMode` — `cfg.Mode` (общий режим работы, а не «auth mode»).
package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Config — корневой config kacho-nlb. Все вложенные структуры с mapstructure-тегами
// для viper-биндинга; defaults — в `defaults.go`, validation — в `validate.go`.
type Config struct {
	// Mode — общий режим работы (dev / production / production-strict).
	// Production-mode требует TLS на слушателе и на каждом ребре к соседу,
	// адресованное ребро nlb→iam, включённый fail-closed фильтр видимости
	// списков и безопасный sslmode у строки подключения к Postgres (сама
	// строка обязательна в ЛЮБОМ режиме). См. validate.go.
	//
	// Здесь стояло «требует не-пустую FGA endpoint» — снято как ложное
	// (kacho#1796): адреса внешнего движка отношений в описателе настроек
	// нет вовсе, движок снят целиком (#747), и стражу нечего было проверять.
	// Комментарий, обещающий отказ старта, читается как действующий контроль
	// и провоцирует починку кода под несуществующее требование.
	//
	// Хранится как строка в YAML (`mode: production` / `mode: dev`), мапится
	// на enum через `ParseMode`.
	ModeRaw string `mapstructure:"mode"`

	Logger      LoggerConfig      `mapstructure:"logger"`
	APIServer   APIServerConfig   `mapstructure:"api-server"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
	Healthcheck HealthcheckConfig `mapstructure:"healthcheck"`
	Repository  RepositoryConfig  `mapstructure:"repository"`
	Authn       AuthnConfig       `mapstructure:"authn"`
	ExtAPI      ExtAPIConfig      `mapstructure:"extapi"`
	Authz       AuthzConfig       `mapstructure:"authz"`
	FGA         FGAConfig         `mapstructure:"fga"`
	MTLS        MTLSConfig        `mapstructure:"mtls"`
	Jobs        JobsConfig        `mapstructure:"jobs"`
	Quota       QuotaConfig       `mapstructure:"quota"`
}

// QuotaConfig — секция quota: ОБЪЯВЛЕНИЕ домена величин.
//
// Прежде адрес домена величин ВЫВОДИЛСЯ из адреса внутреннего слушателя соседа
// по авторизации. Довод был верен ровно до тех пор, пока авторитет величин и
// авторитет авторизации — одна служба; уход модуля квотирования из службы
// доступа это условие снимает.
//
// Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// стадия S1, решение Д1.
type QuotaConfig struct {
	// Authority — где живёт домен величин. РОВНО ДВА законных значения:
	// адрес в форме host:port либо слово `not-deployed`. Незаданное значение —
	// отказ старта: умолчание означало бы выбор за оператора между «потолки
	// действуют» и «потолков нет», и выбор этот был бы невидим.
	//
	// Объявление ОДНО на обе полосы ребра — разрешение величины на пути запроса
	// и фоновую дельту: ребро одно, и два объявления о нём разошлись бы молча.
	//
	// ENV `KACHO_NLB_QUOTA__AUTHORITY`.
	Authority string `mapstructure:"authority"`
}

// Mode возвращает резолвленный enum-режим (после `Validate`).
func (c Config) Mode() ModeEnum {
	m, _ := ParseMode(c.ModeRaw)
	return m
}

// ─── Logger ─────────────────────────────────────────────────────────────────

type LoggerConfig struct {
	Level string `mapstructure:"level"` // FATAL|ERROR|WARN|INFO|DEBUG; default DEBUG
}

// ─── API-server ──────────────────────────────────────────────────────────────

type APIServerConfig struct {
	Endpoint          string        `mapstructure:"endpoint"`          // tcp://0.0.0.0:9090
	InternalEndpoint  string        `mapstructure:"internal-endpoint"` // tcp://0.0.0.0:9091
	GracefulShutdown  time.Duration `mapstructure:"graceful-shutdown"` // default 10s
	GRPCGatewayEnable bool          `mapstructure:"grpc-gw-enable"`    // false (gateway = separate svc)

	// HandlingBudget — верхняя граница обработки ОДНОГО вызова: носитель контура
	// (`pkg/servicehost`) ставит этот срок входящему контексту, если своего у него
	// нет либо он дальше границы. Более строгий срок вызывающего уважается — окно
	// не расширяется никогда.
	//
	// Величина обязательна и «не применимо» у неё нет: вызов без срока держит
	// соединение из ограниченного пула столько, сколько выполняется его запрос,
	// поэтому `pool_max_conns` таких вызовов отказывают весь сервис (CWE-770).
	//
	// 30s — тот же порядок, что у соседей: он обязан накрывать самую длинную
	// ЗАКОННУЮ обработку запроса nlb, а это страница списка предельного размера
	// (`page_size=1000`), чью фильтрацию по правам сужатель проводит волнами с
	// собственным бюджетом ≈6s (см. `authz.list-filter.timeout` и
	// `listnarrow.Budget`), плюс запросы к своей БД. Величина сознательно с
	// запасом: её предмет — не тонкая настройка задержки, а недопущение вызова
	// БЕЗ срока вовсе.
	HandlingBudget time.Duration `mapstructure:"handling-budget"`

	// SubscriptionStreamBudget — СРОК ЖИЗНИ одного потока подписки
	// (`pkg/subscription`, общий сервер потока изменений).
	//
	// По истечении поток закрывается ЧИСТО, и клиент возобновляется со своей
	// позиции: обрыв — штатное событие, а не отказ. Величина обязана заметно
	// превосходить границу обработки одиночного вызова выше, иначе поток
	// закрывался бы раньше, чем доезжает первое событие догона, и подписчик
	// читал бы штатное закрытие как «изменений нет». Носитель судит это
	// отношение сам и роняет старт на негодной величине.
	SubscriptionStreamBudget time.Duration `mapstructure:"subscription-stream-budget"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этого процесса.
	//
	// Это АРИФМЕТИКА СОЕДИНЕНИЙ, а не вкус: каждый поток держит выделенное
	// соединение ВНЕ ПУЛА всё время своей жизни, поэтому
	//
	//	число реплик × этот потолок + пулы всех реплик ≤ max_connections базы
	//
	// Величина выбрана от ПРЕДЕЛА БАЗЫ, а не от ожидаемого спроса: в боевом
	// профиле база этой службы принимает 100 соединений, пул ширины своей не
	// объявляет (берёт умолчание драйвера), и запас обязан остаться и ему, и
	// миграциям, и опросу готовности.
	//
	// Асимметрия, которая и решает выбор: упереться в СВОЙ потолок — чистый отказ
	// ОДНОМУ вызывающему (`RESOURCE_EXHAUSTED`, повтор осмыслен), упереться в
	// предел БАЗЫ — отказ всему процессу и всем арендаторам сразу, включая тех,
	// кто подписки не открывал. Поэтому свой потолок держится заведомо ниже.
	//
	// Превышение отвечает ОТКАЗОМ, а не молчаливой очередью: очередь превратила бы
	// исчерпание в неограниченное ожидание, неотличимое для клиента от
	// «событий нет».
	//
	// Поднимая величину, подними и предел базы — вместе, а не по одному: сегодня
	// их произведение не сверяет никто (гейт посадки считает только пул).
	SubscriptionMaxStreams int `mapstructure:"subscription-max-streams"`

	// SubscriptionIdlePoll — холостой перепрос журнала.
	//
	// Он не «на всякий случай»: ОТКАТИВШИЙСЯ писатель уведомления не шлёт, и
	// подтверждение границы устоявшегося приезжает именно этим перепросом.
	SubscriptionIdlePoll time.Duration `mapstructure:"subscription-idle-poll"`

	// RateLimit — ПОТОЛОК ТЕМПА и ОДНОВРЕМЕННОСТИ на вызывающего, по одному
	// набору на слушатель. Срок обработки выше ограничивает ОДИН запрос; здесь
	// ограничивается их ПОТОК, и это разные защиты.
	//
	// Молчание посадки означает ПОЛ ПЛАТФОРМЫ
	// (`grpcsrv.PlatformPublicAdmission` / `PlatformInternalAdmission`), а не
	// ноль: ноль механизм читает как «не ограничиваем», и слушатель выглядел бы
	// защищённым, ни разу не отказав. Числа живут в фундаменте одним местом —
	// вписанные сюда, они стали бы их седьмой прописью.
	//
	// Посадка вправе назвать свои величины (вёдра живут В ПРОЦЕССЕ: при N
	// репликах эффективный предел равен N × объявленного), но только ВЕСЬ набор
	// сразу — частичное объявление отвергается стартом с именем слушателя.
	RateLimit RateLimitConfig `mapstructure:"rate-limit"`
}

// RateLimitConfig — величины допуска обоих слушателей в том виде, в каком их
// объявляет файл настроек.
//
// Структура ручек — общая с фундаментом (`grpcsrv.AdmissionKnobs`): три
// семейства настроек платформы (viper у nlb/vpc/iam, env у остальных) читают
// ОДНИ И ТЕ ЖЕ четыре оси, и три копии тегов разъехались бы на первой же новой
// оси — молча, потому что незнакомый ключ viper игнорирует.
type RateLimitConfig struct {
	// Public — величины публичного слушателя, на ПРИНЦИПАЛА.
	Public grpcsrv.AdmissionKnobs `mapstructure:"public"`
	// Internal — величины внутреннего слушателя, на ЛИЧНОСТЬ СЕРТИФИКАТА.
	Internal grpcsrv.AdmissionKnobs `mapstructure:"internal"`
}

// ─── Metrics / Healthcheck ───────────────────────────────────────────────────

type MetricsConfig struct {
	Enable bool `mapstructure:"enable"`
	// Address — listen-адрес cluster-internal diagnostic HTTP-listener'а
	// (metrics + /healthz + /readyz). Default `:9101` (см. RegisterDefaults).
	// Пустая строка ИЛИ enable=false → listener не поднимается (back-compat).
	Address string `mapstructure:"address"`
}

type HealthcheckConfig struct {
	Enable bool `mapstructure:"enable"`
}

// ─── Repository ──────────────────────────────────────────────────────────────

type RepositoryConfig struct {
	Type     string         `mapstructure:"type"` // POSTGRES
	Postgres PostgresConfig `mapstructure:"postgres"`
}

// PostgresConfig — параметры подключения к `kacho_nlb`.
//
// Здесь нет `slave-url`: он объявлял read-replica для CQRS-Reader'а, но ни один
// путь его не читал — composition root всегда создавал ОДИН пул и передавал
// `Repository.New(pool, nil)`, то есть обещанной разгрузки чтений не существовало
// ни в одной посадке. Настройка снята с контракта, а не «подключена по-быстрому»:
// увести чтения на реплику значит изменить видимость собственных записей
// (read-your-writes) — это отдельное решение со своим acceptance, а не побочный
// эффект уборки. Реплика в repo-слое остаётся выразимой (`Repository.New` берёт
// второй пул), появится настройка — вместе с ней.
type PostgresConfig struct {
	URL string `mapstructure:"url"` // postgres://user:pass@host/kacho_nlb

	// MaxConns — ширина пула (0 → pgxpool default max(4, NumCPU)). Доезжает до
	// пула через DSN() как `pool_max_conns`; ширина решает, сколько
	// одновременных запросов сервис вообще способен обслужить.
	MaxConns int32 `mapstructure:"max-conns"`

	// ConnLifetime — потолок жизни одного соединения (0 → pgxpool default).
	// Доезжает до пула через DSN() как `pool_max_conn_lifetime`.
	ConnLifetime time.Duration `mapstructure:"conn-lifetime"`

	// PasswordFromEnv — имя ENV-переменной с паролем, который подставляется
	// в URL по shell-placeholder'у `$(<ИМЯ>)` на этапе Load (см.
	// load.go::expandPasswordFromEnv). Пароль — Secret в Helm, в ConfigMap
	// его держать нельзя; viper не понимает `$(VAR)` синтаксис, поэтому
	// expand делаем явно. Пустая строка — substitution отключён (URL
	// используется как есть). regression-fix.
	PasswordFromEnv string `mapstructure:"password-from-env"`
}

// DSN — connection string ДЛЯ ПУЛА: URL плюс `pool_*`-параметры, которые понимает
// только `pgxpool.ParseConfig`.
//
// Не использовать нигде, кроме создания pgxpool: `pool_max_conns` фатален для
// `database/sql` (cmd/migrator) и для одиночного `pgx.Conn` (LISTEN-feed
// lifecycle) — оба берут `Repository.Postgres.URL` как есть.
//
// Незаданные ручки не добавляют ничего: DSN пустого/недонастроенного конфига
// байт-в-байт равен URL, поэтому дефолты pgxpool остаются в силе.
func (c Config) DSN() string {
	dsn := c.Repository.Postgres.URL
	if dsn == "" {
		return ""
	}
	if c.Repository.Postgres.MaxConns > 0 {
		dsn += poolParamSep(dsn) + "pool_max_conns=" + strconv.FormatInt(int64(c.Repository.Postgres.MaxConns), 10)
	}
	if c.Repository.Postgres.ConnLifetime > 0 {
		dsn += poolParamSep(dsn) + "pool_max_conn_lifetime=" + c.Repository.Postgres.ConnLifetime.String()
	}
	return dsn
}

// poolParamSep — разделитель для дописываемого query-параметра.
func poolParamSep(dsn string) string {
	if strings.Contains(dsn, "?") {
		return "&"
	}
	return "?"
}

// ─── Authn (transport TLS) ───────────────────────────────────────────────────

// AuthnConfig — TLS-параметры серверной части (опционально). См.
type AuthnConfig struct {
	Type string         `mapstructure:"type"` // none | tls
	TLS  AuthnTLSConfig `mapstructure:"tls"`
}

type AuthnTLSConfig struct {
	KeyFile  string               `mapstructure:"key-file"`
	CertFile string               `mapstructure:"cert-file"`
	Client   AuthnTLSClientConfig `mapstructure:"client"`
}

type AuthnTLSClientConfig struct {
	Verify  string   `mapstructure:"verify"`   // skip | certs-required | verify
	CAFiles []string `mapstructure:"ca-files"` // PEM-bundle for client-cert verification
}

// ─── ExtAPI (peer gRPC clients: vpc / compute / iam) ─────────────────────────

type ExtAPIConfig struct {
	DefDialDuration time.Duration  `mapstructure:"def-dial-duration"` // default 10s
	VPC             ExtAPIEndpoint `mapstructure:"vpc"`
	Compute         ExtAPIEndpoint `mapstructure:"compute"`
	IAM             ExtAPIEndpoint `mapstructure:"iam"`
	// Geo — kacho-geo (Geography Region/Zone, leaf-owner; kacho-geo).
	// NetworkLoadBalancer.region_id / TargetGroup.region_id валидируются через
	// geo.RegionService.Get (sync precheck). Ребро nlb→geo заменяет прежнее
	// nlb→compute «ради region»; nlb→compute остаётся для InstanceService
	// (instance-resolve — НЕ geography). Addr биндится также из явной ENV
	// `KACHO_NLB_GEO_GRPC_ADDR` (BindEnv в defaults.go).
	Geo ExtAPIEndpoint `mapstructure:"geo"`
}

// ExtAPIEndpoint — параметры одного peer-сервиса. Public/Internal — два
// отдельных адреса (NLB зовёт internal-сервисы напрямую через cluster-internal
// :9091, не через api-gateway). TLS опционален.
type ExtAPIEndpoint struct {
	Addr         string        `mapstructure:"addr"`          // host:port (public RPC)
	InternalAddr string        `mapstructure:"internal-addr"` // host:port (internal :9091)
	DialDuration time.Duration `mapstructure:"dial-duration"` // 0 → берётся ExtAPI.DefDialDuration
	TLS          bool          `mapstructure:"tls"`           // production-mode требует true
}

// ─── Authz (FGA Check + cache + бюджет отказов) ──────────────────────────────

type AuthzConfig struct {
	IAM        AuthzIAMConfig        `mapstructure:"iam"`
	Cache      AuthzCacheConfig      `mapstructure:"cache"`
	ListFilter AuthzListFilterConfig `mapstructure:"list-filter"`

	// DenyBudgetPerSec — устойчивый темп (в секунду на принципала) тех проверок
	// прав, чей исход кэш НЕ поглощает: отказ, промах «нет пути», недоступность
	// модели. По исчерпании звено решения отвечает `ResourceExhausted`, не
	// обращаясь к kaname, — то есть сбрасывает шторм отказов с соседа.
	//
	// 100/с — та же величина, что до перевода на носитель стояла в композиционном
	// корне литералом (`DenyRateLimitPerSec: 100`). Штатную работу тенанта она не
	// задевает: положительные вердикты поглощает кэш (`authz.cache.ttl`), а сотня
	// ОТКАЗОВ в секунду от одного принципала — это уже перебор доступа, а не
	// работа.
	//
	// Ноль здесь запрещён и ловится `Validate`: механизм читает неположительное
	// значение как «ограничения нет», то есть отсечка выключилась бы МОЛЧА, ветка
	// `ResourceExhausted` стала бы недостижимой, а её счётчик — навсегда нулевым.
	// Ровно так эта величина однажды и пропала.
	DenyBudgetPerSec float64 `mapstructure:"deny-budget-per-sec"`

	// TrustedForwarderSANs — allow-list cert-identity SAN'ов, которым разрешено
	// форвардить end-user principal-metadata (обычно единственный — api-gateway SA).
	// Пробрасывается в grpcsrv.UnaryTrustedPrincipalExtract(WithTrustedForwarders)
	// на ОБОИХ листенерах. Пусто (default) → любой mTLS-verified peer доверен как
	// форвардер (паритет с insecure dev back-compat и kaname internal); задаётся в
	// production для defense-in-depth против confused-deputy (внутренний сервис со
	// своим валидным cert'ом не может выдать себя за пользователя). ENV
	// `KACHO_NLB_AUTHZ__TRUSTED_FORWARDER_SANS` (comma-separated).
	TrustedForwarderSANs []string `mapstructure:"trusted-forwarder-sans"`

	// TrustAnyForwarder — ЯВНЫЙ опт-ин «круг не сужаем», действующий ТОЛЬКО вне
	// боевого режима. Нужен для локальных in-process фикстур, где ни сертификатов,
	// ни шлюза нет.
	//
	// Он существует потому, что стража круга срабатывает на ЛЮБОМ старте, а не
	// только в боевом режиме: контроль, чья ветка на локальном стенде не
	// исполняется ни разу, обнаруживает «забыл выставить круг» только на боевом
	// профиле, где цена ошибки максимальна. Оставленный незаданным (false) = отказ
	// старта на пустом круге. В боевом режиме НЕ действует — иначе это была бы
	// ручка, снимающая защиту на развёрнутом стенде.
	TrustAnyForwarder bool `mapstructure:"trust-any-forwarder"`

	// TrustDomainName — ДОМЕН ДОВЕРИЯ установки: то, чьи сертификаты она признаёт своими.
	//
	// Круг отправителей выше называет, КОМУ позволено говорить за пользователя;
	// домен отвечает на предыдущий вопрос — чьи вообще предъявители наши. Пока он
	// был скомпилирован, установка меняла его только пересборкой: сертификаты
	// выпускаются под доменом из величины профиля, а принимающая сторона читала
	// литерал, и расходились они МОЛЧА — законный отправитель переставал
	// опознаваться, а отказ выглядел как вызов без личности.
	//
	// Умолчания нет намеренно: непустое умолчание сделало бы контроль на вид
	// включённым и увело бы установку, забывшую назвать свой домен, в чужой.
	// Пустая величина — отказ старта (Validate), а не «принимаем любой».
	//
	// ENV `KACHO_NLB_AUTHZ__TRUST_DOMAIN`, ключ YAML `authz.trust-domain`.
	TrustDomainName string `mapstructure:"trust-domain"`
}

// AuthzListFilterConfig — per-object filtered List (RBAC).
// Каждый публичный List<Resource> читает СТРАНИЦУ из своей БД и спрашивает
// iam.AuthorizeService.BatchCheck (viewer ∪ v_list) про id этой страницы, отдавая
// только доступные объекты; read==enforce, fail-closed (security.md). Endpoint
// iam (там AuthorizeService); по умолчанию переиспользуется conn, которым nlb
// уже зовёт iam (см. main.go buildListFilter), mTLS — через mtls.iam-register.
type AuthzListFilterConfig struct {
	// Enabled — master-switch. default true (D «применяется во всех доменах»).
	// false → use-case'ы получают nil-Filter → нефильтрованный project-scoped
	// passthrough (dev / graceful start без iam).
	Enabled bool `mapstructure:"enabled"`
	// Timeout — per-request deadline ОДНОГО iam.BatchCheck (default 500ms).
	Timeout time.Duration `mapstructure:"timeout"`
	// CacheTTL — TTL visibility-cache (default 5s; ≤10s).
	CacheTTL time.Duration `mapstructure:"cache-ttl"`
	// CacheMaxEntries — bound размера cache, в записях «(subject,type,id) видим»
	// (default 10000). ТОЛЬКО размер кеша: прежде та же величина уезжала ещё и в
	// ListObjects.max_results («cap видимых объектов»), связывая два несвязанных
	// смысла; per-object BatchCheck никакого cap'а не имеет.
	CacheMaxEntries int `mapstructure:"cache-max-entries"`
	// FailOpen — на FGA error: false (default) → Unavailable (fail-closed,
	// security.md); true → bypass + audit-warn (dev escape-hatch).
	FailOpen bool `mapstructure:"fail-open"`

	// Breakglass — аварийный режим: когда модели прав на этой посадке нет вовсе
	// (`enabled=false` либо соединение с kaname не собрано), списки отдаются
	// НЕсуженными вместо отказа.
	//
	// Он остаётся явным исключением, а не умолчанием: прежде «фильтр выключен» само
	// по себе означало сквозной проход, и вся защита держалась на загрузочном страже
	// — то есть существовала ровно до первой конфигурации, которая его не взвела.
	// Теперь пропуск требуется ОБЪЯВИТЬ, и каждое срабатывание считается и
	// называется (`listnarrow.Counts`).
	Breakglass bool `mapstructure:"breakglass"`
}

// AuthzIAMConfig — параметры ВЫЗОВА per-RPC Check. Адрес здесь НЕ живёт:
// соединение, по которому идёт Check, строится из `extapi.iam.internal-addr`
// (peerDialSpecs, conn `iam-internal`). Прежний ключ `authz.iam.addr` был снят с
// контракта: ни один путь кода его не читал — из него не строилось ни одно
// соединение, — а production-стража его требовала, то есть охраняла пустоту.
type AuthzIAMConfig struct {
	DialDeadline   time.Duration `mapstructure:"dial-deadline"`   // default 3s
	RequestTimeout time.Duration `mapstructure:"request-timeout"` // default 500ms
}

type AuthzCacheConfig struct {
	Enable bool          `mapstructure:"enable"` // default true (positive-only)
	TTL    time.Duration `mapstructure:"ttl"`    // default 5s (≤10s)
}

// ─── FGA (owner-tuple registration via IAM) ───────────────────────────

// FGAConfig — kacho-nlb НЕ пишет права напрямую.
// Прямой best-effort tuple-write (адрес прежнего движка, его стор и модель) удалён;
// вместо него
// owner-hierarchy tuple пишется register-intent'ом в `fga_register_outbox` в той же
// writer-tx (Вариант A), а register-drainer применяет его через kaname
// InternalIAMService.RegisterResource/UnregisterResource по mTLS.
type FGAConfig struct {
	// RegisterDrainer — register-drainer (fga_register_outbox → IAM
	// RegisterResource/UnregisterResource).: default-on (без него
	// созданные ресурсы не получат owner-tuple — деградация хуже текущей).
	RegisterDrainer FGARegisterDrainerConfig `mapstructure:"register-drainer"`

	// RequireIAM — fail-closed boot-gate. Когда true,
	// мутирующий Create отказывает (UNAVAILABLE) и readiness=NotReady, пока
	// register-drainer не подключён к IAM — ни один ресурс не создаётся без
	// доставляемого owner-tuple-intent'а. Default false (dev back-compat). В
	// production: true (единый канонический режим, N5). ENV
	// `KACHO_NLB_REQUIRE_IAM` (явный BindEnv в defaults.go).
	RequireIAM bool `mapstructure:"require-iam"`
}

// FGARegisterDrainerConfig — параметры corelib outbox/drainer на таблице
// `kacho_nlb.fga_register_outbox` (Вариант A). Drainer — внутренняя
// goroutine (не cross-cluster flag); под FOR UPDATE SKIP LOCKED claim одна
// реплика дренит каждую строку (exactly-once across pods).
type FGARegisterDrainerConfig struct {
	Enable       bool          `mapstructure:"enable"`        // default true
	BatchSize    int           `mapstructure:"batch-size"`    // default 32
	PollFallback time.Duration `mapstructure:"poll-fallback"` // default 30s (missed-NOTIFY safety)
	MaxAttempts  int           `mapstructure:"max-attempts"`  // default 10 (poison threshold)
	BackoffMin   time.Duration `mapstructure:"backoff-min"`   // default 1s
	BackoffMax   time.Duration `mapstructure:"backoff-max"`   // default 30s
}

// ─── mTLS (opt-in, per-edge) ───────────────────────────────────────────

// MTLSConfig — per-edge mTLS via corelib value-structs. enable=false
// (default) → insecure (dev backward-compat). Каждое ребро —
// независимый enable (rollback per-edge).
//
// ENV (viper, делимитер `.`→`__`; hyphen in a section name stays literal):
// mapstructure лоуэркейсит имена полей corelib-структур →
// `enable`/`certfile`/`keyfile`/`clientcafiles`/`cafiles`/`servername`:
//
//	KACHO_NLB_MTLS__SERVER__ENABLE                 → server listener mTLS
//	KACHO_NLB_MTLS__SERVER__CERTFILE / __KEYFILE / __CLIENTCAFILES
//	KACHO_NLB_MTLS__IAM-REGISTER__ENABLE           → nlb→iam internal :9091
//	KACHO_NLB_MTLS__IAM-REGISTER__CERTFILE / __KEYFILE / __CAFILES / __SERVERNAME
//	KACHO_NLB_MTLS__IAM-PROJECT__ENABLE            → nlb→iam public :9090
//	KACHO_NLB_MTLS__IAM-PROJECT__CERTFILE / __KEYFILE / __CAFILES / __SERVERNAME
//	KACHO_NLB_MTLS__VPC__*                         → nlb→vpc
//	KACHO_NLB_MTLS__COMPUTE__*                     → nlb→compute (Instance-resolve)
//	KACHO_NLB_MTLS__GEO__*                         → nlb→geo (RegionService.Get)
type MTLSConfig struct {
	// Server — server-cert на public+internal listener'ах (RequireAndVerify-
	// ClientCert при enable=true).
	Server grpcsrv.TLSServer `mapstructure:"server"`
	// IAMRegister — client-cert на ВНУТРЕННЕМ ребре nlb→iam-internal (9091):
	// InternalIAMService.Check (per-RPC authz-gate) + RegisterResource/
	// UnregisterResource (register-drainer). ServerName =
	// kaname-internal.* (фактический :9091 dial-host).:
	// этот же conn несёт Check, поэтому read/authz authz-ребро покрыто им.
	IAMRegister grpcclient.TLSClient `mapstructure:"iam-register"`
	// IAMProject — client-cert на ПУБЛИЧНОМ ребре nlb→iam (9090):
	// ProjectService.Get (existence + leaf-owner). ((b),
	// per-listener split): отдельное поле, потому что public dial-host =
	// kaname.* (≠ kaname-internal.*) и единый ServerName не может быть
	// корректен для обоих listener'ов под RequireAndVerifyClientCert (
	// latent-bug). ServerName = kaname.* (фактический :9090 dial-host).
	IAMProject grpcclient.TLSClient `mapstructure:"iam-project"`
	// VPC — client-cert на ребре nlb→vpc (Address/Subnet/NIC IPAM).
	VPC grpcclient.TLSClient `mapstructure:"vpc"`
	// Compute — client-cert на ребре nlb→compute (Instance-resolve; geography
	// region-валидация перенесена на ребро nlb→geo, см. Geo ниже).
	Compute grpcclient.TLSClient `mapstructure:"compute"`
	// Geo — client-cert на ребре nlb→geo (RegionService.Get, kacho-geo).
	Geo grpcclient.TLSClient `mapstructure:"geo"`
	// QuotaAuthority — client-cert на ребре nlb→домен величин (обе полосы:
	// InternalLimitService.Resolve на пути запроса и ListChangedSince фоновой
	// дельтой). Своё, а не заимствованное у authz-ребра: адрес домена величин
	// объявляется отдельно (`quota.authority`), и удостоверение обязано
	// следовать за адресом.
	QuotaAuthority grpcclient.TLSClient `mapstructure:"quota-authority"`
}

// ─── Jobs (background workers) ───────────────────────────────────────────────

// JobsConfig — конфигурация фоновых worker'ов (см. internal/apps/kacho/jobs).
type JobsConfig struct {
	TargetDrain TargetDrainConfig `mapstructure:"target-drain"`
	FreeIP      FreeIPConfig      `mapstructure:"free-ip"`
}

// TargetDrainConfig — параметры двухфазного drain runner.
type TargetDrainConfig struct {
	// Interval — период между тиками drain-runner'а. Default 10s
	// (см. RegisterDefaults). Должен быть > 0.
	Interval time.Duration `mapstructure:"interval"`
}

// FreeIPConfig — параметры free_ip_runner (реконсиляция застрявших
// БАЛАНСИРОВЩИКОВ: durable-handle create-сирота 'CREATING' + незавершённый
// Delete 'DELETING'). Реконсайлер сканирует load_balancers и только их —
// см. docs/architecture/15-free-ip-runner.md.
type FreeIPConfig struct {
	// Interval — период между тиками reconciler'а. Default 30s (сироты редки,
	// чаще не нужно). Должен быть > 0.
	Interval time.Duration `mapstructure:"interval"`
	// AgeThreshold — минимальный возраст строки (по updated_at), при котором она
	// считается «застрявшей»: свежий in-flight create/delete (моложе порога) не
	// трогается, пока легитимный worker дорабатывает. Default 5m. Должен быть > 0.
	AgeThreshold time.Duration `mapstructure:"age-threshold"`
}

// TrustDomain — домен доверия, который РЕАЛЬНО уезжает в пару звеньев извлечения
// личности на обоих слушателях.
//
// Единственный источник этой величины на процесс: проводка, стража старта и
// самоотчёт о посадке читают ОДИН объект и спрашивают его ОДИН предикат. Значит
// «страж пропустил» ⟺ «домен реально объявлен» — по построению, а не потому, что
// три автора написали одинаковые тела.
//
// Приведение написанного оператором (пробелы, схема, косые черты) живёт в
// конструкторе типа и здесь не повторяется: два места об одном предмете
// расходятся молча. См. grpcsrv.NewTrustDomain.
func (c *Config) TrustDomain() grpcsrv.TrustDomain {
	return grpcsrv.NewTrustDomain(c.Authz.TrustDomainName)
}

// TrustedForwarders — круг отправителей, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/kacho-loadbalancer/wiring.go), и стража старта (Validate), и самоотчёт о
// посадке (cmd/kacho-loadbalancer/bootposture.go). Все трое спрашивают ОДИН
// объект и ОДИН его предикат, поэтому «стража пропустила» ⟺ «круг реально
// сужен» — по построению. До ввода типа у nlb таких предикатов было ДВА: свой у
// стражи (пакет config) и свой у самоотчёта (пакет main).
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c *Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.Authz.TrustedForwarderSANs...)
}
