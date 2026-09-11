// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Config — корневая структура конфигурации kacho-vpc.
//
// Иерархия (YAML):
//
//	logger:        { level }
//	api-server:    { endpoint, internal-endpoint, graceful-shutdown }
//	metrics:       { enable }
//	healthcheck:   { enable }
//	repository:    { type, postgres }
//	authn:         { mode, tls }
//	authz:         { iam-endpoint, check-timeout, ... }
//	extapi:        { def-dial-duration, iam, geo }
//	dataplane:     { executor }
//
// Все секции — `mapstructure`-теги (viper по умолчанию использует mapstructure
// для Unmarshal). Default'ы — в defaults.go.
type Config struct {
	Logger      LoggerConfig      `mapstructure:"logger"`
	APIServer   APIServerConfig   `mapstructure:"api-server"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
	Healthcheck HealthcheckConfig `mapstructure:"healthcheck"`
	Repository  RepositoryConfig  `mapstructure:"repository"`
	AuthN       AuthNConfig       `mapstructure:"authn"`
	AuthZ       AuthZConfig       `mapstructure:"authz"`
	ExtAPI      ExtAPIConfig      `mapstructure:"extapi"`
	Network     NetworkConfig     `mapstructure:"network"`
	IAM         IAMConfig         `mapstructure:"iam"`
	Dataplane   DataplaneConfig   `mapstructure:"dataplane"`
}

// IAMConfig — секция iam: интеграция с kaname (fail-closed boot-gate +
// register-drainer owner-tuple publisher).
//
// Оба флага раньше читались ad-hoc через os.LookupEnv прямо в cmd/ (requireIAM /
// registerDrainerEnabled) с лёгким парсингом `v=="true" || v=="1"` — любое иное
// значение молча становилось false. Для security-свитча (Require) это fail-open:
// `KACHO_VPC_REQUIRE_IAM=yes` тихо ОТКЛЮЧАЛ gate. Теперь оба идут через
// типизированный Config — единый парсинг, YAML-override, и строгая bool-валидация
// на decode (нераспознанное значение → Load-ошибка, а не тихий false).
type IAMConfig struct {
	// Require — fail-closed boot-gate. true → мутирующий Create отвергается и
	// сервис отдаёт NotReady, пока register-drainer не подключится к kaname.
	// Default false (dev: Create разрешён, только Warn).
	// Legacy env: KACHO_VPC_REQUIRE_IAM. Новый ключ: iam.require /
	// KACHO_VPC_IAM__REQUIRE.
	Require bool `mapstructure:"require"`

	// RegisterDrainerEnabled — register-drainer default-on: дренит
	// kacho_vpc.fga_register_outbox в kaname (owner-tuple на каждый Create).
	// Без него созданные ресурсы не получат owner-tuple. Отключается только явным
	// false. Legacy env: KACHO_VPC_FGA_REGISTER_DRAINER_ENABLED. Новый ключ:
	// iam.register-drainer-enabled / KACHO_VPC_IAM__REGISTER_DRAINER_ENABLED.
	RegisterDrainerEnabled bool `mapstructure:"register-drainer-enabled"`
}

// AuthZConfig — секция authz.
//
// Ручки, снимающей проверку прав, здесь БОЛЬШЕ НЕТ: аварийный обход снят с
// контракта вместе с переводом vpc на носитель контура (имя снятой ручки
// называет `retired_knobs_test.go` — здесь оно намеренно не воспроизводится,
// иначе перечень снятого нашёл бы его как возвращённое). Носитель ставит звено
// решения о доступе ВСЕГДА — поля, которым его можно отменить, в дескрипторе
// процесса не существует, — поэтому ручка перестала бы что-либо менять и стала
// бы принятой-и-проигнорированной: оператор задаёт значение, чарт его
// доставляет, поведение не меняется, и узнать об этом можно только по
// последствиям. Снятие держит `retired_knobs_test.go`.
type AuthZConfig struct {
	// IAMEndpoint — gRPC адрес kaname internal-port'а (обычно
	// `kaname.kacho.svc:9091`). Обязателен на ЛЮБОЙ посадке:
	// ребро решения о доступе объявляется дескриптором явно, и незаданный адрес
	// отвергает конструктор дескриптора (не только в боевом режиме).
	IAMEndpoint string `mapstructure:"iam-endpoint"`

	// IAMTLS — TLS на peer-вызов в kaname.
	IAMTLS TLSClient `mapstructure:"iam-tls"`

	// CheckTimeout — таймаут на один Check-вызов (default 2s).
	CheckTimeout time.Duration `mapstructure:"check-timeout"`

	// DenyRateLimitPerSec — token-bucket per-Principal на denied-storm
	// (default 100).
	DenyRateLimitPerSec float64 `mapstructure:"deny-rate-limit-per-sec"`

	// CacheTTL — TTL positive-results кеша (default 5s).
	CacheTTL time.Duration `mapstructure:"cache-ttl"`

	// ListFilter — конфиг FGA-filtered List handlers.
	ListFilter ListFilterConfig `mapstructure:"list-filter"`

	// TrustedForwarderSANs — круг отправителей (личности сертификата,
	// SPIFFE-SAN), которым РАЗРЕШЕНО передавать личность конечного пользователя в
	// метаданных `x-kacho-principal-*`. Уезжает в grpcsrv.WithTrustedForwarders на
	// ОБОИХ листенерах (см. cmd/vpc/principal_chain.go).
	//
	// Почему это ручка, а не константа: список отличается от стенда к стенду
	// (пространства имён, имена служебных учёток), а перечислять его надо ПОЛНО —
	// иначе законный отправитель перестаёт обслуживаться. У vpc законных
	// отправителей ЧЕТЫРЕ, и все найдены по графу вызовов, а не по догадке
	// «наверное только шлюз»:
	//   - api-gateway — тенантский трафик на публичный :9090;
	//   - compute     — привязка/отвязка интерфейса и резолв подсети под личностью
	//     инициатора (services/compute/internal/clients/vpc_{nic,subnet}_client.go);
	//   - nlb         — резолв подсети/адреса/группы безопасности/интерфейса
	//     (services/nlb/internal/clients/vpc/*.go);
	// Канонические значения — в values.prod.
	//
	// Пусто допустимо ТОЛЬКО в dev (in-process фикстуры): contract corelib сужает
	// круг лишь на НЕПУСТОМ списке, поэтому пусто означает «доверяем любому пиру с
	// сертификатом внутреннего CA», а не «никому». В любом боевом режиме Validate()
	// отказывает в старте (fail-closed, зеркалит geo/compute/nlb/storage/registry/iam).
	//
	// ENV `KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS` (через запятую).
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
	// ENV `KACHO_VPC_AUTHZ__TRUST_DOMAIN`, ключ YAML `authz.trust-domain`.
	TrustDomainName string `mapstructure:"trust-domain"`
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
func (c Config) TrustDomain() grpcsrv.TrustDomain {
	return grpcsrv.NewTrustDomain(c.AuthZ.TrustDomainName)
}

// TrustedForwarders — круг отправителей, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/vpc/main.go), и стража старта (Validate), и самоотчёт о посадке
// (cmd/vpc/bootposture.go). Все трое спрашивают ОДИН объект и ОДИН его предикат,
// поэтому «стража пропустила» ⟺ «круг реально сужен» — по построению, а не по
// совпадению трёх одинаково написанных тел.
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZ.TrustedForwarderSANs...)
}

// ListFilterAuthorizeEndpoint — адрес, по которому РЕАЛЬНО поднимается соединение
// фильтра видимости (`AuthorizeService.BatchCheck`): своё поле
// `authz.list-filter.authorize-endpoint`, а если оно пусто — запасное
// `authz.iam-endpoint`. Пустая строка означает «ребро не дилится вовсе»
// (composition root тогда отдаёт nil-соединение, а фильтр вырождается в
// пропускающий).
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/vpc/main.go buildAuthorizeConn), и стража старта (ValidateListFilter,
// ValidatePeerTransport). Поэтому «стража увидела ребро» ⟺ «ребро дилится» — по
// построению, а не по совпадению двух одинаково написанных условий.
func (c Config) ListFilterAuthorizeEndpoint() string {
	if ep := strings.TrimSpace(c.AuthZ.ListFilter.AuthorizeEndpoint); ep != "" {
		return ep
	}
	return strings.TrimSpace(c.AuthZ.IAMEndpoint)
}

// ListFilterEdgeUsesMTLS — предикат «соединение фильтра видимости поднимается с
// проверяемым транспортом». true ⟺ composition root пойдёт по client-cert-пути;
// false ⟺ он возьмёт insecure-креденшелы.
//
// Ручек две, и любая из них включает один и тот же путь: собственная
// `authz.list-filter.authorize-tls.enable` и общая с ребром per-RPC Check
// `KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE` (личность клиента одна на оба ребра к iam).
// Тот же предикат читает и проводка, и стража — иначе они разойдутся, и ребро,
// несущее решение о видимости, поднимется незащищённым при довольной страже.
func (c Config) ListFilterEdgeUsesMTLS(m MTLSConfig) bool {
	return c.AuthZ.ListFilter.AuthorizeTLS.Enable || m.IAMAuthzMTLS.Enable
}

// ListFilterConfig — конфигурация FGA-filtered List.
//
// Source: yaml `authz.list-filter.{enabled,timeout-ms,cache-ttl,max-entries,fail-open}`.
//
// Версию модели авторизации сюда не передают: её закрепляет за собой kaname —
// единственный, кто ходит в хранилище прав, — и запрос BatchCheck поля под неё не
// несёт. Ручка `model-id` здесь принималась и не читалась никем, поэтому снята с
// контракта целиком (вместе с ключом чарта и мостом из окружения).
// ENV-override: `KACHO_VPC_AUTHZ__LIST_FILTER__ENABLED=true`, etc.
//
// Когда Enabled=true И authz.iam-endpoint выставлен → каждая List-RPC спрашивает
// kaname `AuthorizeService.BatchCheck` о видимости id прочитанной СТРАНИЦЫ
// (батчи ≤100). Прежний вопрос «перечисли все разрешенные ids»
// (`AuthorizeService.ListObjects`) снят — у него жесткий server-side предел без
// continuation-token'а, см. package-doc `internal/authzfilter`. Вместе с ним ушел
// и knob `max-results` (client-side trim уже усеченного ответа — cap не поднимал).
type ListFilterConfig struct {
	// Enabled — главный toggle. Default false (unfiltered behaviour).
	// В production: true.
	Enabled bool `mapstructure:"enabled"`

	// AuthorizeEndpoint — gRPC адрес kaname, по которому спрашивается
	// `AuthorizeService.BatchCheck`. Пустая строка → fallback на
	// AuthZConfig.IAMEndpoint; резолв — в ListFilterAuthorizeEndpoint выше, и на
	// этот fallback штатно опирается умолчание чарта.
	//
	// Слушатель здесь НЕ называется намеренно: AuthorizeService зарегистрирована на
	// ОБОИХ слушателях iam, поэтому законны оба адреса, а выбор делает профиль.
	// Состав слушателей описан в одном месте —
	// services/iam/cmd/kaname/grpc_register.go. Здесь стояло «адрес **public**
	// listener'а (AuthorizeService на :9090, в отличие от InternalIAMService на
	// :9091)»: вторая половина подразумевала, что на внутреннем слушателе службы нет,
	// и это неверно.
	AuthorizeEndpoint string `mapstructure:"authorize-endpoint"`

	// AuthorizeTLS — TLS на peer-вызов в kaname AuthorizeService.
	AuthorizeTLS TLSClient `mapstructure:"authorize-tls"`

	// TimeoutMs — таймаут одного BatchCheck-вызова (default 500ms):
	// per-call budget ≤100ms p95 + 5x safety margin.
	TimeoutMs int `mapstructure:"timeout-ms"`

	// CacheTTL — TTL positive entries в LRU-кэше (default 5s).
	CacheTTL time.Duration `mapstructure:"cache-ttl"`

	// MaxEntries — hard cap кэша (default 10000). LRU eviction.
	MaxEntries int `mapstructure:"max-entries"`

	// FailOpen — если true, FGA-error возвращает unfiltered list.
	// Default false (fail-closed). WARN-log + Critical-alert при включении.
	FailOpen bool `mapstructure:"fail-open"`

	// Ручки аварийного пропуска страницы здесь БОЛЬШЕ НЕТ — её имя называет
	// `retired_knobs_test.go` (здесь оно намеренно не воспроизводится, иначе
	// перечень снятого нашёл бы его как возвращённое). Предмет у неё был один —
	// «посадка без модели прав», — и он недостижим: `ValidateListFilter`
	// отказывает в старте и на выключенном фильтре, и на нерезолвимом адресе, на
	// ЛЮБОЙ посадке. Значит у поднявшегося процесса фильтр включён и соединение
	// есть, а вооружённая ручка не меняла ни одного исхода — то есть была принятой
	// и проигнорированной. Пропуск на отсутствующей модели остаётся невозможным по
	// построению: сужатель без соединения ОТКАЗЫВАЕТ (`pkg/listnarrow`), и снять
	// этот отказ настройкой нельзя.
}

// LoggerConfig — секция logger.
type LoggerConfig struct {
	// Level — один из FATAL|ERROR|WARN|INFO|DEBUG.
	Level string `mapstructure:"level"`
}

// APIServerConfig — секция api-server.
//
// Endpoint / InternalEndpoint поддерживают два формата:
//   - `tcp://0.0.0.0:9090` (полный URL-стиль, рекомендуется);
//   - `9090` (legacy: голый порт; работает для backward-compat
//     с старыми values.yaml, см. listenAddress в load.go).
type APIServerConfig struct {
	Endpoint         string        `mapstructure:"endpoint"`
	InternalEndpoint string        `mapstructure:"internal-endpoint"`
	GracefulShutdown time.Duration `mapstructure:"graceful-shutdown"`

	// RequestTimeout — верхняя граница на обработку одного RPC (server-side
	// deadline). Устанавливается deadline-interceptor'ом на обоих листенерах:
	// если у входящего ctx нет deadline (или он дальше этого лимита) — ctx
	// оборачивается context.WithDeadline(now+RequestTimeout). Более строгий
	// client-deadline уважается. 0 → interceptor не навешивается (без границы).
	//
	// Защита от bounded-pool exhaustion (CWE-770/400): без server-deadline
	// deadline-less RPC держат pooled-connection бесконечно; MaxConns таких
	// запросов исчерпывают pool → service-wide DoS. Дополняет DB-level
	// statement_timeout (repository.postgres.statement-timeout).
	RequestTimeout time.Duration `mapstructure:"request-timeout"`

	// RateLimit — сколько запросов в секунду и сколько одновременно вправе
	// задать ОДИН вызывающий каждому листенеру. Величина срока обработки выше
	// ограничивает ОДИН запрос; здесь ограничивается их ПОТОК, а это разные
	// защиты: тысяча запросов, каждый из которых уложился в срок, занимает базу
	// целиком. Подробности и полярность умолчания — ratelimit.go.
	RateLimit RateLimitConfig `mapstructure:"rate-limit"`

	// SubscriptionStreamBudget — СРОК ЖИЗНИ одного потока подписки.
	//
	// По истечении поток закрывается ЧИСТО, и клиент возобновляется со своей
	// позиции: обрыв здесь — штатное событие, а не отказ. Величина обязана
	// заметно превосходить границу обработки одиночного вызова (`RequestTimeout`
	// выше), иначе поток закрывался бы раньше, чем доезжает первое событие
	// догона, и подписчик читал бы штатное закрытие как «изменений нет».
	// Носитель контура судит это отношение сам и роняет старт на негодной
	// величине.
	SubscriptionStreamBudget time.Duration `mapstructure:"subscription-stream-budget"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этого процесса.
	//
	// Это АРИФМЕТИКА СОЕДИНЕНИЙ, а не вкус: каждый поток держит выделенное
	// соединение ВНЕ ПУЛА всё время своей жизни, поэтому
	//
	//	число реплик × этот потолок + пулы всех реплик ≤ max_connections базы
	//
	// Асимметрия, которая решает выбор величины: упереться в СВОЙ потолок —
	// чистый отказ ОДНОМУ вызывающему (`RESOURCE_EXHAUSTED`, повтор осмыслен);
	// упереться в предел БАЗЫ — отказ всему процессу и всем арендаторам сразу,
	// включая тех, кто подписок не открывал. Поэтому свой потолок держится
	// заведомо ниже предела базы.
	//
	// Поднимая величину, подними и предел базы — вместе, а не по одному: сегодня
	// их произведение не сверяет никто (гейт посадки считает только пул).
	SubscriptionMaxStreams int `mapstructure:"subscription-max-streams"`

	// SubscriptionIdlePoll — холостой перепрос журнала.
	//
	// Он не «на всякий случай»: ОТКАТИВШИЙСЯ писатель уведомления не шлёт, и
	// подтверждение границы устоявшегося приезжает именно этим перепросом. Ноль
	// означал бы, что горизонт не подтверждается никогда, и общий сервер такую
	// величину отвергает на подъёме.
	SubscriptionIdlePoll time.Duration `mapstructure:"subscription-idle-poll"`
}

// MetricsConfig — секция metrics: cluster-internal diagnostic HTTP-listener
// (/metrics + /healthz + /readyz). Endpoint пуст ИЛИ Enable=false → listener не
// поднимается (byte-identical back-compat).
type MetricsConfig struct {
	Enable bool `mapstructure:"enable"`
	// Endpoint — адрес diagnostic-listener'а (напр. ":9095"). Cluster-internal,
	// НЕ публикуется на external endpoint и НЕ проксируется api-gateway.
	Endpoint string `mapstructure:"endpoint"`
}

// MetricsEndpoint возвращает адрес diagnostic-listener'а, либо "" если метрики
// выключены (Enable=false) — composition root тогда не поднимает listener.
func (c Config) MetricsEndpoint() string {
	if !c.Metrics.Enable {
		return ""
	}
	return listenAddress(c.Metrics.Endpoint)
}

// HealthcheckConfig — секция healthcheck (placeholder под /healthz).
type HealthcheckConfig struct {
	Enable bool `mapstructure:"enable"`
}

// RepositoryConfig — секция repository. Single-backend (Postgres); `Type`
// оставлен как mapstructure-поле конфига, но продукт Postgres-only.
type RepositoryConfig struct {
	Type     string         `mapstructure:"type"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

// PostgresConfig — секция repository.postgres.
//
//	URL              — стандартный DSN postgres://user:pass@host:port/db (master).
//	SlaveURL         — DSN read-replica (опционально).
//	                   Пустая строка / совпадает с URL → Reader-TX идут на master
//	                   (fallback). Когда настроен — Reader использует slave-pool,
//	                   разгружая master от read-load (streaming replication,
//	                   `hot_standby=on` на реплике). Пароль читается из того же
//	                   `password-from-env` и подставляется в обе DSN.
//	MaxConns         — pgxpool max conns (одинаково для master и slave-pool);
//	                   0 = pgx default (max(4, NumCPU)).
//	SSLMode          — disable|require|verify-ca|verify-full (валидируется в Validate).
//	PasswordFromEnv  — имя ENV-переменной, из которой подтягивается пароль и
//	                   подставляется в URL и SlaveURL (legacy KACHO_VPC_DB_PASSWORD).
//	                   Пустая строка — пароль уже в URL (или sslmode=disable+no-password).
//
// Пароль в YAML/ConfigMap — нельзя (commit-able), поэтому он остается
// read-from-env через явный `password-from-env` мостик. Default —
// `KACHO_VPC_DB_PASSWORD` (backward-compat).
type PostgresConfig struct {
	URL      string `mapstructure:"url"`
	SlaveURL string `mapstructure:"slave-url"`
	MaxConns int    `mapstructure:"max-conns"`
	SSLMode  string `mapstructure:"ssl-mode"`
	// StatementTimeout — libpq `-c statement_timeout` для serving-пулов (master +
	// slave). Ограничивает длительность одного запроса на стороне сервера БД →
	// зависший/долгий запрос не держит pooled-connection бесконечно (защита от
	// bounded-pool exhaustion / DoS, CWE-770/400). Применяется ТОЛЬКО к serving
	// DSN (DSN/SlaveDSN), НЕ к MigrateDSN — миграции (index build / backfill)
	// могут легитимно превышать лимит. 0 → не задаётся (Postgres default = без лимита).
	StatementTimeout time.Duration `mapstructure:"statement-timeout"`
	// LockTimeout — libpq `-c lock_timeout` для serving-пулов: верхняя граница
	// ожидания блокировки (lock contention не должна пинить connection на весь
	// statement_timeout). Так же не применяется к MigrateDSN. 0 → не задаётся.
	LockTimeout     time.Duration `mapstructure:"lock-timeout"`
	PasswordFromEnv string        `mapstructure:"password-from-env"`
}

// AuthNConfig — секция authn.
//
// Mode — общий режим работы сервиса (см. mode.go). Под-секция TLS зарезервирована
// под будущий serving-TLS (key-file/cert-file на listener) — пока сервис
// слушает plain gRPC, поле наполняется через viper, но в runtime не используется.
type AuthNConfig struct {
	Mode Mode      `mapstructure:"mode"`
	TLS  TLSServer `mapstructure:"tls"`

	// TrustedForwarder — явное подтверждение оператора, что публичный listener
	// (:9090) стоит ЗА аутентифицированным forwarder'ом/service-mesh, который сам
	// терминирует идентичность клиента, и потому client-asserted x-kacho-*
	// metadata можно доверять БЕЗ server-mTLS на самом listener'е.
	//
	// Default false (fail-closed). В production (non-strict) публичный listener
	// выводит authz-principal'а именно из этой metadata; без server-mTLS ИЛИ без
	// этого явного подтверждения любой прямой вызов :9090 может подделать
	// произвольного principal'а (CWE-290). Поэтому ValidateServerMTLS в production
	// требует ЛИБО PublicServerMTLS.Enable, ЛИБО trusted-forwarder=true.
	//
	// production-strict игнорирует этот флаг — там server-mTLS обязателен всегда
	// (escape-hatch не действует).
	TrustedForwarder bool `mapstructure:"trusted-forwarder"`
}

// TLSServer — TLS-параметры server-side listener'а (зарезервировано).
type TLSServer struct {
	KeyFile    string   `mapstructure:"key-file"`
	CertFile   string   `mapstructure:"cert-file"`
	ServerName string   `mapstructure:"server-name"`
	CAFiles    []string `mapstructure:"ca-files"`
}

// ExtAPIConfig — секция extapi (peer-сервисы).
//
// Project-existence peer — kaname (ProjectService.Get); поддерживается
// только `extapi.iam`. zone_id валидируется через kacho-geo (`extapi.geo`) —
// leaf-домен Geography, а не kacho-compute.
type ExtAPIConfig struct {
	DefDialDuration time.Duration `mapstructure:"def-dial-duration"`
	IAM             PeerConfig    `mapstructure:"iam"`
	Geo             PeerConfig    `mapstructure:"geo"`
}

// PeerConfig — параметры одного peer-сервиса.
//
//	Endpoint      — host:port (без `dns:///` — префикс добавляется в dialer'е,
//	                если DNSLB=true).
//	TLS           — TLS-параметры клиента к peer'у.
//	DialDuration  — таймаут на установление conn (0 — extapi.def-dial-duration).
//	DNSLB         — включить gRPC client-side round_robin + dns:/// resolver.
type PeerConfig struct {
	Endpoint     string        `mapstructure:"endpoint"`
	TLS          TLSClient     `mapstructure:"tls"`
	DialDuration time.Duration `mapstructure:"dial-duration"`
	DNSLB        bool          `mapstructure:"dns-lb"`
}

// TLSClient — TLS-параметры client-side (для peer-gRPC).
type TLSClient struct {
	Enable     bool     `mapstructure:"enable"`
	ServerName string   `mapstructure:"server-name"`
	CAFiles    []string `mapstructure:"ca-files"`
}

// NetworkConfig — секция network (VPC-domain бизнес-настройки).
type NetworkConfig struct {
	ProjectCache ProjectCacheConfigStruct `mapstructure:"project-cache"`
}

// ProjectCacheConfigStruct — TTL+LRU кеш ProjectClient.Exists.
type ProjectCacheConfigStruct struct {
	PositiveTTL time.Duration `mapstructure:"positive-ttl"`
	NegativeTTL time.Duration `mapstructure:"negative-ttl"`
	MaxSize     int           `mapstructure:"max-size"`
}

// searchPathOpt — URL-encoded libpq-фрагмент `-c search_path=kacho_vpc,public`
// (без префикса `options=`). Первый сегмент libpq-`options`; после него могут
// дописываться serving-only тайм-ауты (statement_timeout / lock_timeout).
// Добавляется во все DSN автоматически, чтобы каждое соединение (pgxpool,
// dedicated pgx.Conn для LISTEN, goose-через-database/sql) видело таблицы
// kacho-vpc по unqualified-имени.
//
// Значение search_path — «kacho_vpc, public»:
//   - `kacho_vpc` впереди — наши таблицы (схема создается в baseline
//     `0001_initial.sql`, там же заданы все таблицы);
//   - `public` сзади — `btree_gist`-extension и built-in объекты Postgres,
//     которые extension/CREATE-команды по умолчанию создают там.
//
// Пробел в `-c search_path=…` обязан быть `%20`; знак `=` внутри значения —
// `%3D`; запятая — `%2C`. При смене схемы (ребрендинг / multi-tenant) — менять
// здесь и в `0001_initial.sql` одновременно.
const searchPathOpt = "-c%20search_path%3Dkacho_vpc%2Cpublic"

// pgOptionsParam собирает libpq `options=` для DSN. search_path — всегда;
// statement_timeout / lock_timeout — только при withTimeouts (serving-пулы),
// и только если соответствующий Duration > 0. search_path идёт первым, поэтому
// подстрока `options=-c%20search_path%3Dkacho_vpc%2Cpublic` всегда присутствует
// (обратная совместимость).
func (c Config) pgOptionsParam(withTimeouts bool) string {
	opt := searchPathOpt
	if withTimeouts {
		if ms := c.Repository.Postgres.StatementTimeout.Milliseconds(); ms > 0 {
			opt += fmt.Sprintf("%%20-c%%20statement_timeout%%3D%d", ms)
		}
		if ms := c.Repository.Postgres.LockTimeout.Milliseconds(); ms > 0 {
			opt += fmt.Sprintf("%%20-c%%20lock_timeout%%3D%d", ms)
		}
	}
	return "options=" + opt
}

// baseDSN — стандартный postgres DSN без pgxpool-параметров и БЕЗ serving-тайм-аутов;
// используется миграциями (database/sql.Open("pgx")). Делегирует composeDSN(URL,false).
func (c Config) baseDSN() string {
	return c.composeDSN(c.Repository.Postgres.URL, false)
}

// composeDSN добавляет к raw-DSN (master URL или slave URL) недостающие libpq-
// параметры: `sslmode=<mode>` (из PostgresConfig.SSLMode, default `disable`)
// и `options=-c search_path=kacho_vpc,public` (все VPC-таблицы живут в схеме
// `kacho_vpc`, поэтому каждое соединение должно установить корректный
// search_path).
//
// Если соответствующий параметр уже задан в raw-URL — не перетираем (упрощает
// override через прямой ENV/yaml). Для пустого raw возвращаем пустую строку
// — caller интерпретирует это как «slave не настроен».
func (c Config) composeDSN(raw string, withTimeouts bool) string {
	if raw == "" {
		return ""
	}
	mode := c.Repository.Postgres.SSLMode
	if mode == "" {
		mode = "disable"
	}
	if !strings.Contains(raw, "sslmode=") {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw = raw + sep + "sslmode=" + mode
	}
	// Append search_path (+ serving-тайм-ауты при withTimeouts) via libpq
	// `options` parameter, если еще не задан. Распознаем как `options=`, так и
	// URL-encoded `options%3D`. Если пользователь сам прописал `options=...` в
	// URL — оставляем его, не перетираем (упрощает override в dev/debug).
	if !strings.Contains(raw, "options=") && !strings.Contains(raw, "options%3D") {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw = raw + sep + c.pgOptionsParam(withTimeouts)
	}
	return raw
}

// SingleConnDSN — строка подключения для ОДИНОЧНОГО соединения (вне пула).
//
// Отличается от [Config.DSN] ровно отсутствием пуловых параметров, и различие
// несущее: `pool_max_conns` понимает pgxpool, а `pgx.Connect` — нет. Разбор
// строки на этом не спотыкается: неизвестный ключ уезжает серверу
// runtime-параметром в стартовом пакете, и Postgres отвечает FATAL уже на
// ПОДКЛЮЧЕНИИ. Отказ поэтому наступает не на сборке, а у каждого потребителя в
// бою, и выглядит тихо — источник «недоступен» и никогда ничем иным.
//
// Нужна тем, кто держит собственную сессию: `LISTEN` подписки не может жить в
// пуле — сессия вернулась бы в него вместе с подпиской.
func (c Config) SingleConnDSN() string {
	return c.composeDSN(c.Repository.Postgres.URL, true)
}

// DSN — connection string для pgxpool (поддерживает pool_max_conns).
// НЕ использовать для database/sql.Open("pgx") — pool_max_conns там FATAL.
func (c Config) DSN() string {
	dsn := c.composeDSN(c.Repository.Postgres.URL, true)
	if dsn == "" {
		return ""
	}
	if c.Repository.Postgres.MaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.Repository.Postgres.MaxConns)
	}
	return dsn
}

// SlaveDSN — connection string для slave-pool (read-replica). Пустая строка →
// реплика не настроена, caller использует master (Repository.New(master, nil)
// → Reader fallback на master).
//
// SlaveURL совпадает с URL — slave-pool тоже не создается (caller передаст
// nil), чтобы не плодить второй pool к той же физической БД.
func (c Config) SlaveDSN() string {
	slaveRaw := c.Repository.Postgres.SlaveURL
	if slaveRaw == "" || slaveRaw == c.Repository.Postgres.URL {
		return ""
	}
	dsn := c.composeDSN(slaveRaw, true)
	if dsn == "" {
		return ""
	}
	if c.Repository.Postgres.MaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.Repository.Postgres.MaxConns)
	}
	return dsn
}

// MigrateDSN — connection string для goose/database/sql (без pool_max_conns).
// Всегда master — goose не должен писать в реплику.
func (c Config) MigrateDSN() string { return c.baseDSN() }
