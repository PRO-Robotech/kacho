// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-registry, загружается из переменных
// окружения через corelib config.LoadPrefixed("KACHO_REGISTRY"). Поля с
// абсолютным тегом читаются как есть; вложенные per-edge TLS-структуры
// (grpcclient.TLSClient / grpcsrv.TLSServer) получают независимые имена
// KACHO_REGISTRY_<EDGE>_<NAME> — префикс на каждое ребро, без общего TLS-синглтона.
package config

import (
	"fmt"
	"net"
	"net/url"
	"time"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// envPrefix — корневой сегмент env-имён kacho-registry (KACHO_<DOMAIN>).
const envPrefix = "KACHO_REGISTRY"

// Config — конфигурация kacho-registry.
type Config struct {
	DBHost     string `envconfig:"KACHO_REGISTRY_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_REGISTRY_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_REGISTRY_DB_USER" default:"registry"`
	DBPassword string `envconfig:"KACHO_REGISTRY_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_REGISTRY_DB_NAME" default:"kacho_registry"`
	// DBSSLMode — sslmode для DSN. dev по умолчанию `disable`; в проде обязателен
	// require|verify-ca|verify-full.
	DBSSLMode string `envconfig:"KACHO_REGISTRY_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx-пула (0 = дефолт pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_REGISTRY_DB_MAX_CONNS" default:"0"`
	// DBStatementTimeout — server-side statement_timeout для pool-соединений (libpq
	// value: "30s" / "30000"). Backstop против runaway-запроса, держащего pooled-conn
	// весь client-контролируемый срок (CWE-770; pool-saturation soft-DoS). "0"/"" —
	// backstop отключён. Ставится ТОЛЬКО на pool-DSN, не на migrator-DSN (DDL не
	// клампится). Все read-пути keyset-пагинированы и индексированы, поэтому 30s с
	// запасом.
	DBStatementTimeout string `envconfig:"KACHO_REGISTRY_DB_STATEMENT_TIMEOUT" default:"30s"`

	// GrpcPort — публичный control-plane листенер (RegistryService).
	GrpcPort string `envconfig:"KACHO_REGISTRY_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — cluster-internal листенер (InternalRegistryService).
	// Не выставляется на внешнем endpoint api-gateway — только cluster-internal.
	InternalGrpcPort string `envconfig:"KACHO_REGISTRY_INTERNAL_PORT" default:"9091"`

	// AuthMode — fail-closed режим: dev | production | production-strict.
	//
	// Дефолт — production (secure-by-default, core rule #16): незаданный env НЕ должен
	// поднимать сервис в insecure-posture. dev — явный opt-in локальных фикстур и
	// dev-профиля стенда (values.dev.yaml выставляет его явно).
	AuthMode string `envconfig:"KACHO_REGISTRY_AUTH_MODE" default:"production"`

	// AuthZIAMGRPCAddr — internal endpoint kaname (:9091) для per-RPC Check
	// (ребро registry→iam authz) И для fga-proxy RegisterResource/UnregisterResource
	// (Internal-only). Пусто + Breakglass=false → интерсептор НЕ подключается.
	AuthZIAMGRPCAddr string `envconfig:"KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR" default:""`

	// IAMProjectGRPCAddr — PUBLIC endpoint kaname (:9090) для ProjectService.Get
	// (existence-валидация project на Create). ProjectService зарегистрирован ТОЛЬКО
	// на public :9090; на internal :9091 (AuthZIAMGRPCAddr) его НЕТ — вызов там
	// возвращает Unimplemented. Поэтому project-ребро держит СОБСТВЕННЫЙ conn на :9090,
	// отдельный от authz/register-ребра на :9091 (единый conn на :9091 давал
	// Unimplemented на Get → фикс. INTERNAL на Create ещё до insert'а).
	IAMProjectGRPCAddr string `envconfig:"KACHO_REGISTRY_IAM_PROJECT_GRPC_ADDR" default:"kaname.kacho.svc:9090"`
	// GeoGRPCAddr — PUBLIC endpoint kacho-geo (:9090) для RegionService.Get
	// (existence-валидация Registry.region_id на Create — новое ребро registry→geo,
	// REG-1 F4). RegionService — публичный read-only справочник Geography на :9090.
	// Пусто → geo-client nil → RegionExists отвечает Unavailable (Create fail-closed).
	GeoGRPCAddr string `envconfig:"KACHO_REGISTRY_GEO_GRPC_ADDR" default:"kacho-geo.kacho.svc:9090"`
	// AuthZTrustedForwarderSANs — allow-list личностей клиентского сертификата
	// (SPIFFE-SAN), которым разрешено ПЕРЕДАВАТЬ личность конечного пользователя в
	// метаданных x-kacho-principal-*. Уезжает в ОБА листенера через
	// grpcsrv.WithTrustedForwarders (см. cmd/kacho-registry/serve.go).
	//
	// Почему это ручка, а не константа: contract corelib (pkg/grpcsrv
	// principalIsTrusted) сужает круг отправителей ТОЛЬКО когда список непуст; на
	// пустом он отвечает «доверяем» любому пиру, прошедшему проверку сертификата.
	// Внутренний периметр у нас объявлен НЕдоверенным, сетевой политики у registry
	// нет вовсе, а клиентский сертификат всем соседям выдаёт один и тот же
	// внутренний центр — то есть пустой список означает: любой под кластера
	// присылает заголовки личности жертвы, и решение о правах принимается от её
	// имени (pkg/authz subject_extract читает ровно эту личность).
	//
	// Формат — список через запятую. Законный отправитель ОДИН — api-gateway: по
	// графу импортов заглушки registry вне самого сервиса импортирует только
	// gateway/internal/restmux, и он же держит адреса ОБОИХ листенеров
	// (KACHO_API_GATEWAY_REGISTRY_GRPC :9090 и ..._REGISTRY_INTERNAL_GRPC :9091).
	// Каноническое значение — в values.prod.
	//
	// Пусто допустимо ТОЛЬКО в dev (in-process фикстуры); в любом боевом режиме
	// validateSecurityConfig отказывает в старте (fail-closed, зеркалит
	// geo/compute/nlb/storage).
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS"`

	// AuthZTrustAnyForwarder — ЯВНЫЙ опт-ин «круг не сужаем», действующий ТОЛЬКО
	// вне боевого режима. Нужен для локальных in-process фикстур, где ни
	// сертификатов, ни шлюза нет.
	//
	// Он существует потому, что стража круга срабатывает на ЛЮБОМ старте, а не
	// только в боевом режиме: контроль, чья ветка на локальном стенде не
	// исполняется ни разу, обнаруживает «забыл выставить круг» только на боевом
	// профиле, где цена ошибки максимальна. Оставленный незаданным (false) =
	// отказ старта на пустом круге. В боевом режиме НЕ действует — иначе это была
	// бы ручка, снимающая защиту на развёрнутом стенде.
	AuthZTrustAnyForwarder bool `envconfig:"KACHO_REGISTRY_AUTHZ_TRUST_ANY_FORWARDER" default:"false"`

	// AuthZBreakglass — аварийный режим: пропускать все RPC без Check + WARN
	// (только dev / break-glass).
	AuthZBreakglass bool `envconfig:"KACHO_REGISTRY_AUTHZ_BREAKGLASS" default:"false"`

	// AuthZCacheTTL — TTL positive-кеша authz-Check gRPC-интерсептора (ОБА
	// листенера). Ограничивает окно, в течение которого отозванный (revoked)
	// субъект держит закешированный allow ПОСЛЕ удаления AccessBinding: registry НЕ
	// читает журнал смены субъекта у iam (это делает ТОЛЬКО край, и делает он это
	// для СВОЕГО кэша) и db-per-service ⇒ LISTEN/NOTIFY от iam сюда не доходит →
	// revoke-окно = этот TTL + async fga-drain. Короткий дефолт (2s) держит окно
	// узким; 0 → positive-кеш выключен (каждый gRPC-RPC — живой IAM Check,
	// немедленный revoke). data-plane /v2/ (OCI-proxy) authz НЕ кеширует (прямой
	// per-request Check), поэтому этот knob влияет только на control-plane gRPC. См. #33.
	AuthZCacheTTL time.Duration `envconfig:"KACHO_REGISTRY_AUTHZ_CACHE_TTL" default:"2s"`

	// AuthZDenyBudgetPerSec — устойчивый темп (в секунду на принципала) проверок,
	// чей исход кэш НЕ поглощает: отказ, сокрытие существования, промах «нет
	// пути», недоступность модели. По исчерпании звено отвечает
	// `ResourceExhausted`, не обращаясь к kaname, — то есть сбрасывает шторм
	// отказов с соседа.
	//
	// До носителя контура registry этой отсечки НЕ ИМЕЛ вовсе: поле
	// `DenyRateLimitPerSec` не заполнялось, а механизм читает неположительное
	// значение как «ограничения нет». Величина 100 не выдумана — это то же число,
	// которое платформа уже выбрала для того же механизма (литерал в
	// композиционном корне nlb и умолчание ручки vpc/geo).
	//
	// Почему отсечка нужна и реестру: бюджет тратят ТОЛЬКО непоглощаемые кэшем
	// исходы, поэтому законное чтение своих реестров её не видит вовсе. Платит
	// ровно тот, кто штурмует отказами чужие идентификаторы, — и платит за него
	// не kaname.
	AuthZDenyBudgetPerSec float64 `envconfig:"KACHO_REGISTRY_AUTHZ_DENY_BUDGET_PER_SEC" default:"100"`

	// AdmissionPublic / AdmissionInternal — ПОТОЛОК ТЕМПА и ОДНОВРЕМЕННОСТИ на
	// вызывающего, по одному набору на слушатель. Не путать с бюджетом отказов
	// выше: тот сбрасывает шторм ОТКАЗОВ с владельца модели, этот ограничивает
	// ПОТОК запросов к нам самим, и стоимость запроса здесь высокая по
	// построению — мутация есть три строки в базе, чтение есть до тысячи
	// объектов на страницу с проверкой прав партиями.
	//
	// # Почему у ручек НЕТ умолчаний в тегах
	//
	// Полы у слушателей РАЗНЫЕ, поэтому умолчание пришлось бы написать дважды —
	// то есть завести седьмую пропись чисел, которые уже названы в фундаменте
	// (grpcsrv.PlatformPublicAdmission / PlatformInternalAdmission). Молчание
	// посадки разрешается там же, где живут числа: пустой набор означает ПОЛ
	// ПЛАТФОРМЫ, а не ноль. Ноль механизм читает как «не ограничиваем», и
	// слушатель выглядел бы защищённым, ни разу не отказав.
	//
	// # Что посадка вправе сказать
	//
	// Ничего (берётся пол) либо ВЕСЬ набор из четырёх осей. Частичное
	// объявление отвергается стартом с именем слушателя: оператор, задавший темп
	// и забывший одновременность, считает предел выставленным, а незаполненная
	// ось не ограничивает ничего. Своя величина осмысленна потому, что вёдра
	// живут В ПРОЦЕССЕ: при N репликах эффективный предел равен N × объявленного,
	// и запас у стенда из одной реплики и из двадцати разный.
	//
	// Имена: KACHO_REGISTRY_ADMISSION_{PUBLIC,INTERNAL}_{READ_PER_SEC,
	// MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}.
	AdmissionPublic   grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_PUBLIC"`
	AdmissionInternal grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_INTERNAL"`

	// HandlingBudget — верхняя граница обработки ОДНОГО gRPC-вызова (серверный
	// срок). Более строгий срок вызывающего уважается; окно не расширяется никогда.
	//
	// 30s — то же число, что платформа выбрала для той же величины у vpc и geo.
	// Это ПОТОЛОК, а не цель: он обязан с запасом накрывать вопрос о правах
	// (`check.CheckTimeout`, 2s) плюс запрос к своей БД (её собственный backstop —
	// DBStatementTimeout, тоже 30s), а предмет его — не задержка, а вызов БЕЗ
	// срока, который держит соединение из ограниченного пула столько, сколько
	// выполняется его запрос: MaxConns таких вызовов отказывают весь сервис
	// (CWE-770).
	//
	// Величина относится к ДВУМ gRPC-листенерам. Плоскость данных (docker
	// push/pull) в контур носителя не входит и держит свои сроки: подмешивать
	// сюда потоковую передачу слоёв значило бы рвать выгрузку большого образа по
	// границе, выведенной из совсем другой арифметики.
	//
	// «Не применимо» у величины нет и быть не может — сказать «границы не надо»
	// значит сказать «мой процесс вправе держать чужой ресурс сколько угодно».
	// Неположительное значение отвергает конструктор дескриптора.
	HandlingBudget time.Duration `envconfig:"KACHO_REGISTRY_HANDLING_BUDGET" default:"30s"`

	// ── ПОТОК ИЗМЕНЕНИЙ (общий сервер подписки, `pkg/subscription`) ──────────
	//
	// Три величины ПОСАДКИ, а не журнала: журнал говорит, где он лежит и как его
	// строка становится событием, а сколько потоков держать и сколько они живут —
	// свойство процесса и его базы.

	// SubscriptionStreamBudget — СРОК ЖИЗНИ одного потока подписки.
	//
	// По истечении поток закрывается ЧИСТО, и клиент возобновляется со своей
	// позиции: обрыв здесь — штатное событие, а не отказ. Величина обязана
	// заметно превосходить границу обработки одиночного вызова выше, иначе поток
	// закрывался бы раньше, чем доезжает первое событие догона, и подписчик
	// читал бы штатное закрытие как «изменений нет». Носитель судит это
	// отношение сам и роняет старт на негодной величине.
	//
	// Час — то же число, что выбрано для той же величины у соседнего владельца
	// журнала: у неё нет предмета тонкой настройки, её предмет — чтобы поток не
	// жил вечно, занимая соединение вне пула.
	SubscriptionStreamBudget time.Duration `envconfig:"KACHO_REGISTRY_SUBSCRIPTION_STREAM_BUDGET" default:"1h"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этого процесса.
	//
	// Это АРИФМЕТИКА СОЕДИНЕНИЙ, а не вкус: каждый поток держит выделенное
	// соединение ВНЕ ПУЛА всё время своей жизни, поэтому
	//
	//	число реплик × этот потолок + пулы всех реплик ≤ max_connections базы
	//
	// Асимметрия, которая и решает выбор: упереться в СВОЙ потолок — чистый отказ
	// ОДНОМУ вызывающему (`RESOURCE_EXHAUSTED`, повтор осмыслен), упереться в
	// предел БАЗЫ — отказ всему процессу и всем арендаторам сразу, включая тех,
	// кто подписки не открывал. Поэтому свой потолок держится заведомо ниже.
	//
	// Превышение отвечает ОТКАЗОМ, а не молчаливой очередью: очередь превратила
	// бы исчерпание в неограниченное ожидание, неотличимое для клиента от
	// «событий нет».
	//
	// Поднимая величину, подними и предел базы — вместе, а не по одному: их
	// произведение сегодня не сверяет никто.
	SubscriptionMaxStreams int `envconfig:"KACHO_REGISTRY_SUBSCRIPTION_MAX_STREAMS" default:"16"`

	// SubscriptionIdlePoll — холостой перепрос журнала.
	//
	// Он не «на всякий случай»: ОТКАТИВШИЙСЯ писатель уведомления не шлёт, и
	// подтверждение границы устоявшегося приезжает именно этим перепросом. Ноль
	// означал бы поток, живущий одними уведомлениями, — то есть поток, который
	// после отката писателя не сдвинет свою позицию никогда.
	SubscriptionIdlePoll time.Duration `envconfig:"KACHO_REGISTRY_SUBSCRIPTION_IDLE_POLL" default:"2s"`

	// ZotAddr — internal HTTP-endpoint zot-бэкенда (data/registry-API). zot
	// никогда не публично достижим; клиент ходит на cluster-internal endpoint.
	ZotAddr string `envconfig:"KACHO_REGISTRY_ZOT_ADDR" default:""`

	// ZotUsername / ZotPassword — учётные данные, которыми и адаптер плоскости
	// управления, и форвардер плоскости данных предъявляются zot (HTTP Basic
	// поверх htpasswd zot).
	//
	// zot — хранилище слоёв, а не «доверенный внутренний компонент»: он не имеет
	// собственного понятия о тенантах, поэтому любой процесс, дозвонившийся до его
	// порта БЕЗ учётных данных, читает, подменяет и удаляет содержимое всех
	// тенантов, обходя весь per-request контроль плоскости данных одним хопом
	// («internal = trusted» — запрещённое допущение, security.md §1.4). Пусто в
	// production/production-strict при заданном ZotAddr → requireZotCredentials
	// отказывает в старте.
	ZotUsername string `envconfig:"KACHO_REGISTRY_ZOT_USERNAME" default:""`
	ZotPassword string `envconfig:"KACHO_REGISTRY_ZOT_PASSWORD" default:""`

	// PendingBlobTTL — freshness-окно durable per-repo учёта загруженных блобов
	// (registry_pending_blob, REG-33 Defect A). На blob PUT-finalize пишется строка
	// (registry_id, repo, digest); push-time blob HEAD/GET раскрывает блоб, если он
	// загружен в ЭТОТ repo не старше этого TTL (до появления в манифесте — там
	// BlobInRepo уже true). Достаточно пережить одно push-окно (обычно секунды-минуты);
	// дефолт 1h щедр даже для больших образов по медленному каналу. Строки старше TTL
	// подметает sweeper (интервал = TTL). 0 → трекинг фактически выключен (не задавать
	// в проде — REG-33 не закрыт).
	PendingBlobTTL time.Duration `envconfig:"KACHO_REGISTRY_PENDING_BLOB_TTL" default:"1h"`

	// PushGrantTTL — freshness-окно durable per-subject push-ownership кеша
	// (registry_push_grant, REG-33 immediate-pull). На успешном manifest-PUT пишется
	// строка (registry_id, repo, subject); pull-path раскрывает repo толкавшему, если он
	// запушил его не старше этого TTL, ПОКА async register-on-first-push не материализовал
	// per-repo v_get в FGA (иначе собственный немедленный `docker pull` толкавшего упрётся
	// в v_get-deny → 404). Запись — лишь мост на окно материализации.
	//
	// Revoke-safety — TTL обязан быть КОРОТКИМ: это backstop worst-case-обхода revoke. Пока
	// строка свежа, pull-path раскрывает repo на КАЖДЫЙ v_get-deny — включая ПОСЛЕ revoke
	// (v_get denies, но свежий push-grant → allow). Прежний дефолт 1h позволял отозванному
	// субъекту тянуть repo (и чужой контент, залитый в него другими) до 1h после revoke —
	// реальный stale/cross-tenant-ish access leak. Первичный ограничитель — delete-on-
	// materialized (pull-path на первом же реальном v_get-allow удаляет строку → окно ~0);
	// TTL ловит случай, когда толкавший после материализации ни разу не пул(нул). Дефолт
	// 60s: щедрый буфер над материализацией (эмпирически ~10-15s), но worst-case обход revoke
	// ≤60s, а не 1h. Sweeper подметает строки (интервал = TTL). 0 → fallback выключен (не
	// задавать в проде — REG-33 immediate-pull не закрыт).
	PushGrantTTL time.Duration `envconfig:"KACHO_REGISTRY_PUSH_GRANT_TTL" default:"60s"`

	// MetricsAddr — адрес cluster-internal diagnostic-листенера (/healthz,
	// /metrics). Отдельный порт и от gRPC, и от data-plane НАМЕРЕННО: внутренняя
	// кардинальность не публикуется ни на tenant-facing поверхности, ни на
	// docker-эндпоинте (security.md, инфра-чувствительные данные — только
	// internal). Пусто → листенер не поднимается.
	//
	// Наблюдаемости у сервиса не было вовсе — ни этого адреса, ни серий; при том
	// что именно на его очереди регистраций класс «очередь не доставила ни одной
	// строки за всю жизнь и это было неоткуда узнать» и наблюдался вживую.
	MetricsAddr string `envconfig:"KACHO_REGISTRY_METRICS_ADDR" default:":9095"`

	// ===== data-plane OCI auth-proxy (registry.kacho.local) =====

	// DataplaneAddr — адрес data-plane HTTP-листенера (Docker Registry v2 / OCI).
	// Отдельный порт от gRPC :9090/:9091. Пусто → data-plane не поднимается.
	DataplaneAddr string `envconfig:"KACHO_REGISTRY_DATAPLANE_ADDR" default:":8080"`
	// ===== приём токена: издатель — МНОЖЕСТВО, у каждого СВОЯ запись источника =====

	// TokenIssuers — перечень ПРИНИМАЕМЫХ издателей identity-JWT, через запятую.
	//
	// Издатель перестал быть скаляром: платформа чеканит свои токены сама, а
	// прежний издатель на переходе остаётся. Страж старта считает ЭЛЕМЕНТЫ, а не
	// длину строки: «,» и «  ,  ,» — непустые строки с нулём элементов, и такое
	// значение означает «не сужаем», то есть «принимаем любого издателя».
	//
	// Пусто в production/production-strict → отказ в старте.
	TokenIssuers string `envconfig:"KACHO_REGISTRY_TOKEN_ISSUERS" default:""`

	// TokenIssuerKeySets — привязка «издатель → адрес его набора проверочных
	// ключей», перечислением: `<издатель>=<адрес>` через запятую.
	//
	// Адрес ОБЪЯВЛЯЕТСЯ и НИКОГДА не выводится из издателя. Издатель приходит от
	// предъявителя — это недоверенный вход; кроме прямого вреда, производный адрес
	// получался бы у ВСЯКОГО издателя, и состояние «записи источника нет» не
	// наступало бы никогда: страж старта остался бы в тексте, не имея возможности
	// упасть.
	//
	// Издатель, объявленный принимаемым, но не имеющий записи, → отказ в старте.
	// Вырожденная запись (адрес пуст либо не является абсолютным адресом) → отказ
	// в старте: «источника нет», выданное за «источник объявлен».
	TokenIssuerKeySets string `envconfig:"KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS" default:""`

	// PlatformTokenIssuer — издатель, под которым чеканит НАША платформа.
	//
	// Он же выбирает полосу приёма: токен нашей чеканки несёт тип `at+jwt`
	// (RFC 9068) и проходит чтение отзыва на предъявлении; полоса прежнего
	// издателя сохраняет сегодняшнее поведение (тип `JWT`, отзыв не читается) —
	// она вне области этой под-фазы.
	//
	// Пусто → наш издатель не принимается вовсе (и тогда чтение отзыва не
	// требуется). Непусто, но вне TokenIssuers → отказ в старте: чеканить под
	// издателем, которого приёмная сторона не принимает, значит выдавать токены,
	// негодные с первого же запроса.
	PlatformTokenIssuer string `envconfig:"KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER" default:""`

	// TokenRevocationURL — ОБЪЯВЛЕННЫЙ адрес авторитета отзыва (интроспекция по
	// форме RFC 7662) для токенов нашей чеканки.
	//
	// Задаётся явно и НИКОГДА не выводится из адреса соседней службы: выведенный
	// адрес всегда непуст, поэтому контроль выглядел бы включённым, ведя в никуда,
	// и ни один профиль развёртывания не обязан был бы ничего задавать, чтобы это
	// заметить.
	//
	// Наш издатель принимается, а адрес не задан → отказ в старте: контроль,
	// действующий только на выдаче, отзывом не является.
	TokenRevocationURL string `envconfig:"KACHO_REGISTRY_TOKEN_REVOCATION_URL" default:""`

	// TokenRevocationMTLS — учётные данные ребра «реестр → авторитет отзыва»,
	// по той же дисциплине «на ребро», что и остальные исходящие рёбра.
	//
	// Ребро СВОЁ, а не общее с загрузкой набора ключей: набор несёт только
	// публичный материал и потому доступен без аутентификации по
	// задокументированному исключению, а маршруту отзыва ПРИСЫЛАЮТ предъявленный
	// токен — на проводе оказывается удостоверение. Распространить исключение
	// молча значило бы принять запрещённое допущение «внутренний периметр
	// доверенный»: авторитет отвергает вызывающего, чью цепочку транспорт не
	// проверил.
	//
	// Якорь объявлен и непригоден → отказ в СТАРТЕ: откат на системные корни
	// всегда «работает», поэтому ошибка в якоре стала бы ненаблюдаемой.
	TokenRevocationMTLS grpcclient.TLSClient `envconfig:"TOKEN_REVOCATION_MTLS"`

	// TokenRealm — realm для WWW-Authenticate; docker сам идёт туда за Bearer-токеном.
	// Остаётся token-шимом (kaname /iam/token): docker предъявляет SA-key шиму,
	// шим брокерит токен у Hydra. Для data-plane realm — непрозрачный указатель на
	// auth-сервер клиента, поэтому Hydra-переключение его не меняет.
	TokenRealm string `envconfig:"KACHO_REGISTRY_TOKEN_REALM" default:"https://api.kacho.local/iam/token"`
	// ServiceAud — expected audience identity-JWT (наш service) + значение service=
	// в WWW-Authenticate. Токен обязан нести aud ⊇ ServiceAud (federation-out на
	// другие RP registry-доступа не даёт).
	ServiceAud string `envconfig:"KACHO_REGISTRY_SERVICE_AUD" default:"registry.kacho.local"`
	// DataplaneTLSTerminatedExternally — оператор подтверждает, что data-plane
	// OCI-листенер (открытый HTTP, DataplaneAddr) стоит за внешней TLS-терминацией
	// (ingress/mesh). В production/production-strict обязателен true — иначе
	// buildDataplaneHandler (requireDataplaneTLSAck) отклоняет старт: bearer
	// identity-JWT (реплеябельные в пределах TTL) не должны транзитить открытым текстом
	// (CWE-319). Параллель Config.TokenAcceptance. В dev игнорируется.
	DataplaneTLSTerminatedExternally bool `envconfig:"KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY" default:"false"`

	// AnonymousSubjectID — the anonymous principal id (the iam-issued anon Hydra client
	// id, kaname AnonymousClientID) the data-plane resolves to the FGA wildcard
	// `user:*` for anonymous public pull (RG-1 D-7). A VALID anon Bearer whose sub
	// equals this id reads only PUBLIC repos (repo `user:* v_get` tuple) and can never
	// write (B03/B14). Пусто (default) → anonymous pull DISABLED (secure-by-default:
	// анонимный /token не сконфигурирован ⇒ никакой токен не резолвится в user:*).
	// MUST match kaname's configured AnonymousClientID and be a RESERVED id (no real
	// principal shares it).
	AnonymousSubjectID string `envconfig:"KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID" default:""`

	// EndpointBase — tenant-facing base OCI-endpoint namespace. Output-only поле
	// Registry.endpoint = "<EndpointBase>/<id>". Это tenant-facing ingress-host;
	// инфра-адрес zot наружу не раскрывается (infra-sensitive, не на публичной поверхности).
	EndpointBase string `envconfig:"KACHO_REGISTRY_ENDPOINT_BASE" default:"registry.kacho.local"`

	// ===== per-edge mTLS =====

	// IAMAuthzMTLS — client-creds для ребра registry→iam internal (:9091): Check + fga-proxy.
	// ServerName = kaname-internal.* (реальный dial-host :9091).
	IAMAuthzMTLS grpcclient.TLSClient `envconfig:"IAM_AUTHZ_MTLS"`

	// IAMProjectMTLS — client-creds для ребра registry→iam public (:9090): ProjectService.Get.
	// Отдельное поле от IAMAuthzMTLS, потому что ServerName public dial-host'а
	// (kaname.*) ≠ internal (kaname-internal.*): единый ServerName некорректен
	// для обоих листенеров под RequireAndVerifyClientCert.
	IAMProjectMTLS grpcclient.TLSClient `envconfig:"IAM_PROJECT_MTLS"`

	// GeoMTLS — client-creds для ребра registry→geo public (:9090): RegionService.Get.
	// ServerName = kacho-geo public dial-host'а (REG-1 F4 новое ребро).
	GeoMTLS grpcclient.TLSClient `envconfig:"GEO_MTLS"`

	// PublicServerMTLS — server-creds для публичного листенера (:9090).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`

	// InternalServerMTLS — server-creds для cluster-internal листенера (:9091).
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`
}

// TrustedForwarders — круг отправителей, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/kacho-registry/serve.go), и стража старта (Validate), и самоотчёт о
// посадке (cmd/kacho-registry/bootposture.go). Все трое спрашивают ОДИН объект и
// ОДИН его предикат, поэтому «стража пропустила» ⟺ «круг реально сужен» — по
// построению, а не по совпадению трёх одинаково написанных тел.
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZTrustedForwarderSANs...)
}

// Пары `PublicServerCreds` / `InternalServerCreds`, отдававших `grpc.ServerOption`,
// здесь больше нет: слушатели поднимает носитель контура, и транспорт он берёт
// из дескриптора в виде `credentials.TransportCredentials`
// (`grpcsrv.TLSServerTransportCreds`, см. `cmd/kacho-registry/describe`).
// Оставленные, они были бы прод-функциями без прод-читателя — тем самым мёртвым
// кодом, который выглядит работающим.

// searchPathOption — libpq `-c` startup-опция: каждое соединение видит схему
// kacho_registry без отдельного SET search_path на каждый стейтмент.
const searchPathOption = "-c search_path=kacho_registry,public"

// baseDSN — стандартный postgres DSN (годится и для pgxpool, и для database/sql).
// userinfo/host собираются через net/url (url.UserPassword + net.JoinHostPort) —
// пароль/пользователь percent-энкодятся, поэтому URL-значимые символы (@ / : ? #)
// в секрете не «раскусывают» DSN (CWE-116). extraOptions добавляются к libpq
// `options` (несколько `-c`-флагов в одной опции — второй `options=` перезаписал бы
// первый).
func (c Config) baseDSN(extraOptions ...string) string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	options := searchPathOption
	for _, o := range extraOptions {
		if o != "" {
			options += " " + o
		}
	}
	q := url.Values{}
	q.Set("sslmode", mode)
	q.Set("options", options)
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUser, c.DBPassword),
		Host:     net.JoinHostPort(c.DBHost, c.DBPort),
		Path:     "/" + c.DBName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// DSN — строка подключения для pgxpool (поддерживает pool_max_conns +
// statement_timeout backstop). НЕ для database/sql (pool_max_conns → неизвестный
// PG-параметр → FATAL).
func (c Config) DSN() string {
	var extra []string
	if c.DBStatementTimeout != "" && c.DBStatementTimeout != "0" {
		// Каждый GUC в libpq `options` — отдельный `-c key=value` флаг.
		extra = append(extra, "-c statement_timeout="+c.DBStatementTimeout)
	}
	dsn := c.baseDSN(extra...)
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// SingleConnDSN — строка подключения для ОДИНОЧНОГО соединения вне пула
// (`pgx.Connect`), а не для пула.
//
// # Зачем отдельная форма, а не `DSN()`
//
// `DSN()` дописывает `pool_max_conns`, и её собственный комментарий говорит, чем
// это кончается вне пула: неизвестный PG-параметр → FATAL при подключении.
// Разбор строки такую ошибку НЕ ловит — неизвестные ключи уезжают серверу как
// runtime-параметры, — поэтому отказ наступает на ПОДКЛЮЧЕНИИ, а не на сборке.
// У долгоживущего потока подписки это выглядит особенно тихо: сервер поднялся,
// глагол выставлен, а каждая подписка отвечает «источник недоступен» и никогда
// ничем иным.
//
// # Чем отличается от `MigrateDSN()`
//
// Тем, что `statement_timeout` ЗДЕСЬ ОСТАЁТСЯ. Ограничитель одиночного оператора
// нужен всякому читателю: чтение журнала — обычный запрос, и без backstop'а он
// держал бы соединение столько, сколько выполняется. У миграций он снят
// осознанно (долгий DDL не клампится), и это ДРУГОЕ решение — не переносить.
//
// Ожидание уведомления ему не подвластно by construction: ждёт не оператор, а
// протокол, и `statement_timeout` на него не действует.
func (c Config) SingleConnDSN() string {
	var extra []string
	if c.DBStatementTimeout != "" && c.DBStatementTimeout != "0" {
		extra = append(extra, "-c statement_timeout="+c.DBStatementTimeout)
	}
	return c.baseDSN(extra...)
}

// MigrateDSN — строка подключения для goose/database/sql (без pgxpool-параметров и
// без statement_timeout — долгий DDL не клампится).
func (c Config) MigrateDSN() string {
	return c.baseDSN()
}

// Load загружает конфигурацию из переменных окружения.
func Load() (Config, error) {
	var c Config
	err := corecfg.LoadPrefixed(envPrefix, &c)
	return c, err
}
