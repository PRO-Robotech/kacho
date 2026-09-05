// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"github.com/PRO-Robotech/kacho/pkg/identityposture"

	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// Config хранит конфигурацию api-gateway.
// Переменные окружения:
//
//	KACHO_API_GATEWAY_LISTEN_ADDR         — адрес для cmux listener (default: :8080)
//	KACHO_API_GATEWAY_TLS_LISTEN_ADDR     — адрес для TLS listener (default: пусто — TLS отключен)
//	KACHO_API_GATEWAY_TLS_CERT_FILE       — путь к TLS-сертификату (PEM)
//	KACHO_API_GATEWAY_TLS_KEY_FILE        — путь к TLS-приватному ключу (PEM)
//	KACHO_API_GATEWAY_VPC_GRPC            — адрес backend vpc
//	KACHO_API_GATEWAY_COMPUTE_GRPC        — адрес backend compute (public, port 9090)
//	KACHO_API_GATEWAY_COMPUTE_INTERNAL_GRPC — адрес backend compute internal-port (9091)
//	KACHO_API_GATEWAY_IAM_GRPC            — адрес backend iam (public, port 9090)
//	KACHO_API_GATEWAY_IAM_INTERNAL_GRPC   — адрес backend iam internal-port (9091)
//	KACHO_API_GATEWAY_NLB_GRPC            — адрес backend kacho-nlb (public, port 9090)
//	KACHO_API_GATEWAY_NLB_INTERNAL_GRPC   — адрес backend kacho-nlb internal-port (9091)
//	KACHO_API_GATEWAY_GEO_GRPC            — адрес backend kacho-geo (public, port 9090)
//	KACHO_API_GATEWAY_GEO_INTERNAL_GRPC   — адрес backend kacho-geo internal-port (9091)
//	KACHO_API_GATEWAY_REGISTRY_GRPC          — адрес backend kacho-registry (public, port 9090)
//	KACHO_API_GATEWAY_REGISTRY_INTERNAL_GRPC — адрес backend kacho-registry internal-port (9091)
//	KACHO_API_GATEWAY_STORAGE_GRPC           — адрес backend kacho-storage (public, port 9090)
//	KACHO_API_GATEWAY_STORAGE_INTERNAL_GRPC  — адрес backend kacho-storage internal-port (9091)
//	KACHO_APP_ENV                            — deployment-env label (keys the prod authz guard)
//	KACHO_API_GATEWAY_KRATOS_PUBLIC_URL      — Ory Kratos public API base ("disabled" turns it off)
//	KACHO_API_GATEWAY_ADMISSION_PUBLIC_*     — потолок темпа/одновременности внешнего
//	                                           слушателя (READ_PER_SEC, MUTATION_PER_SEC,
//	                                           BURST_FACTOR, IN_FLIGHT; молчание — пол платформы)
//	KACHO_API_GATEWAY_METRICS_ADDR           — cluster-internal диагностическая поверхность
//	                                           (GET /metrics, default :9095; пустая строка —
//	                                           объявленное выключение с причиной в журнале)
//
// TLS требуется для совместимости с CLI-клиентами, жестко ожидающими TLS-endpoint.
// Когда TLS_LISTEN_ADDR пустой — TLS не запускается; plain-cmux на ListenAddr.
type Config struct {
	ListenAddr    string `envconfig:"KACHO_API_GATEWAY_LISTEN_ADDR"          default:":8080"`
	TLSListenAddr string `envconfig:"KACHO_API_GATEWAY_TLS_LISTEN_ADDR"      default:""`
	// InternalRESTAddr — dedicated cluster-internal admin REST listener. It is
	// the ONLY listener wrapped with listenerorigin.InternalListener, so it is
	// the ONLY listener on which Internal* REST paths (/vpc/v1/addressPools,
	// `:internal` projections, InternalRegistry/Cluster/Operations admin) are
	// served. Every other listener — the plaintext cmux listener the ingress
	// targets AND the external TLS listener — is external (fail-closed) and 404s
	// Internal* REST. The ingress MUST NOT target this port;
	// admin-UI / port-forward / cluster-internal tooling reach it via the
	// dedicated `internal-rest` Service port. Empty → the internal REST listener
	// is disabled (Internal* REST unreachable via the gateway entirely).
	InternalRESTAddr string `envconfig:"KACHO_API_GATEWAY_INTERNAL_REST_ADDR"  default:":8081"`
	TLSCertFile      string `envconfig:"KACHO_API_GATEWAY_TLS_CERT_FILE"        default:""`
	TLSKeyFile       string `envconfig:"KACHO_API_GATEWAY_TLS_KEY_FILE"         default:""`
	VPCAddr          string `envconfig:"KACHO_API_GATEWAY_VPC_GRPC"              default:"vpc.kacho.svc:9090"`
	// VPCInternalAddr — admin-only internal-port (9091) of vpc backend.
	// Routes AddressPool RESTful endpoints (kacho-only admin).
	VPCInternalAddr string `envconfig:"KACHO_API_GATEWAY_VPC_INTERNAL_GRPC" default:"vpc.kacho.svc:9091"`
	// ComputeAddr — public gRPC backend of kacho-compute (Disk/Image/Snapshot/Instance/DiskType/Zone).
	ComputeAddr string `envconfig:"KACHO_API_GATEWAY_COMPUTE_GRPC" default:"compute.kacho.svc:9090"`
	// ComputeInternalAddr — admin-only internal-port (9091) of compute backend.
	// Routes InternalDiskType RESTful endpoints (kacho-only admin).
	ComputeInternalAddr string `envconfig:"KACHO_API_GATEWAY_COMPUTE_INTERNAL_GRPC" default:"compute.kacho.svc:9091"`
	// IAMAddr — public gRPC backend of kaname (Account/Project/User/ServiceAccount/Group/Role/AccessBinding).
	// Все RPC под /iam/v1/*.
	IAMAddr string `envconfig:"KACHO_API_GATEWAY_IAM_GRPC" default:"iam.kacho.svc:9090"`
	// IAMInternalAddr — admin-only internal-port (9091) of iam backend.
	// InternalUserService.Get для admin tooling (gRPC-direct; REST-routing no-op,
	// proto-аннотации `google.api.http` отсутствуют — handler регистрируется в mux
	// pro-forma, реальный трафик идет по gRPC) + REST для
	// InternalUserService.UpsertFromIdentity (OIDC-callback).
	// InternalIAMService.LookupSubject/Check — REST-маршруты ЕСТЬ на внутреннем mux
	// (`/iam/v1/internal/iam:lookupSubject` и `:check`, см. restmux/mux.go и
	// middleware/rest_route_table_gen.go). Auth-interceptor при этом ходит на :9091
	// напрямую по gRPC — одно другого не отменяет, а прежняя редакция этой строки
	// выводила из второго первое и утверждала, что маршрутов нет. `ListPermissions`
	// оттуда же выведен: RPC удалён (tombstone), регистрировать нечего.
	IAMInternalAddr string `envconfig:"KACHO_API_GATEWAY_IAM_INTERNAL_GRPC" default:"iam.kacho.svc:9091"`

	// NLBAddr — public gRPC backend of kacho-nlb (NetworkLoadBalancer/Listener/TargetGroup).
	// Public RPC под /nlb/v1/*. При пустом значении nlb-handlers не регистрируются
	// (graceful — позволяет деплоить api-gateway до kacho-nlb pod'a).
	NLBAddr string `envconfig:"KACHO_API_GATEWAY_NLB_GRPC" default:"kacho-nlb.kacho.svc:9090"`

	// NLBInternalAddr — admin-only internal-port (9091) of kacho-nlb backend.
	// InternalResourceLifecycleService.Subscribe — gRPC server-streaming для
	// подписки на CREATED/UPDATED/DELETED события (data-plane consumer'ы дозваниваются
	// напрямую). Регистрируется в REST mux pro-forma (как iam InternalUserService),
	// реальный трафик идет через gRPC-direct. Internal-only, cluster-internal listener.
	NLBInternalAddr string `envconfig:"KACHO_API_GATEWAY_NLB_INTERNAL_GRPC" default:"kacho-nlb.kacho.svc:9091"`

	// GeoAddr — public gRPC backend of kacho-geo (RegionService/ZoneService read).
	// Public RPC под /geo/v1/*. Geography — отдельный leaf-сервис kacho-geo. При
	// пустом значении geo-handlers не регистрируются (graceful — позволяет
	// деплоить api-gateway до kacho-geo pod'a).
	// The geo k8s Service is "kacho-geo" — the bare "geo.kacho.svc"
	// host does NOT resolve (NXDOMAIN) → the grpc resolver returns no addresses →
	// "no children to pick from" 503 on every /geo/v1/* request. Target the real
	// Service name (mirrors kaname / kacho-nlb).
	GeoAddr string `envconfig:"KACHO_API_GATEWAY_GEO_GRPC" default:"kacho-geo.kacho.svc:9090"`

	// GeoInternalAddr — admin-only internal-port (9091) of kacho-geo backend.
	// Routes InternalRegionService/InternalZoneService admin-CRUD endpoints
	// (kacho-only). Cluster-internal listener only.
	// Separate Service "kacho-geo-internal" (mirrors kaname-internal).
	GeoInternalAddr string `envconfig:"KACHO_API_GATEWAY_GEO_INTERNAL_GRPC" default:"kacho-geo-internal.kacho.svc:9091"`

	// RegistryAddr — public gRPC backend of kacho-registry (RegistryService:
	// control-plane реестра). Public RPC под /registry/v1/*. При пустом значении
	// registry-handlers не регистрируются (graceful — позволяет деплоить
	// api-gateway до kacho-registry pod'a). Data-plane OCI v2 (/v2/*) — отдельный
	// ingress, НЕ через api-gateway.
	RegistryAddr string `envconfig:"KACHO_API_GATEWAY_REGISTRY_GRPC" default:"kacho-registry.kacho.svc:9090"`

	// RegistryInternalAddr — admin-only internal-port (9091) of kacho-registry
	// backend. Routes InternalRegistryService (TriggerGarbageCollection/
	// GetRegistryStats) — GC zot-стора + инфра-статистика namespace. Cluster-internal
	// listener only. Same host, internal port (mirrors iam/nlb).
	RegistryInternalAddr string `envconfig:"KACHO_API_GATEWAY_REGISTRY_INTERNAL_GRPC" default:"kacho-registry.kacho.svc:9091"`

	// StorageAddr — public gRPC backend of kacho-storage (VolumeService/
	// SnapshotService/DiskTypeService). Public RPC под /storage/v1/*. При пустом
	// значении storage-handlers не регистрируются (graceful — позволяет деплоить
	// api-gateway до kacho-storage pod'a; симметрично registry/geo/nlb).
	StorageAddr string `envconfig:"KACHO_API_GATEWAY_STORAGE_GRPC" default:"kacho-storage.kacho.svc:9090"`

	// StorageInternalAddr — admin-only internal-port (9091) of kacho-storage
	// backend. Routes InternalVolumeService (Attach/Detach/ListAttachments/
	// GetInternal — placement/инфра-поля) + InternalDiskTypeService (admin CRUD
	// справочника DiskType). Cluster-internal listener only. Same host, internal
	// port (mirrors iam/nlb/registry).
	StorageInternalAddr string `envconfig:"KACHO_API_GATEWAY_STORAGE_INTERNAL_GRPC" default:"kacho-storage.kacho.svc:9091"`

	// --- Проекция потока изменений в браузер (kacho#1020) ---

	// SubscriptionOwners — ЗАКРЫТЫЙ перечень владельцев журналов, чей поток край
	// проецирует. Имена — ключи домена, те же, что у карты внутренних адресов
	// (`compute`, `loadbalancer`, `vpc`, …); разделитель — запятая.
	//
	// ПУСТО означает «владелец не объявлен», а НЕ «все домены». Пустое значение,
	// прочитанное как «не сужаем», уже стоило этому дереву круга отправителей,
	// который никого не сужал; здесь оно означает ровно то, что сказано: ручка
	// отвечает `501` с названной причиной, а не открывает поток к домену,
	// который глагола не служит.
	//
	// Умолчание чарта края называет всех владельцев, служащих глагол (kacho#1388);
	// пустым значение остаётся только там, где профиль выключает возможность явно.
	SubscriptionOwners string `envconfig:"KACHO_API_GATEWAY_SUBSCRIPTION_OWNERS" default:""`

	// SubscriptionStreamBudget — срок жизни ОДНОГО потока проекции.
	//
	// Обязан быть МЕНЬШЕ предела чтения посредника перед краем
	// (`ingress.proxyReadTimeout`, сегодня 120 с): иначе поток рвёт посредник, и
	// клиент читает это как сетевой сбой, а не как чистое закрытие по сроку,
	// после которого он возобновляется со своей позиции. Согласие двух величин
	// держит декларативная проба чарта, а не эта фраза.
	SubscriptionStreamBudget time.Duration `envconfig:"KACHO_API_GATEWAY_SUBSCRIPTION_STREAM_BUDGET" default:"90s"`

	// SubscriptionHeartbeat — период служебного кадра поддержания связи.
	//
	// Молчащая подписка — обычный её режим, а посредник закрывает соединение, по
	// которому дольше своего предела ничего не шло. Кадр заодно мешает
	// буферизации ответа промежуточным звеном.
	SubscriptionHeartbeat time.Duration `envconfig:"KACHO_API_GATEWAY_SUBSCRIPTION_HEARTBEAT" default:"20s"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этой реплики.
	//
	// Арифметика, а не вкус: число реплик края × потолок обязано помещаться в
	// потолок потоков владельца, иначе исчерпание наступает у владельца — то
	// есть у всех арендаторов сразу, а не у того, кто его вызвал.
	SubscriptionMaxStreams int `envconfig:"KACHO_API_GATEWAY_SUBSCRIPTION_MAX_STREAMS" default:"64"`

	// SubscriptionMaxStreamsPerSubject — потолок потоков ОДНОГО субъекта.
	//
	// Потолок реплики защищает процесс, этот — арендаторов друг от друга: без
	// него один субъект занимает потолок реплики целиком, и остальные получают
	// отказ, не имея ни одного собственного потока. Консоль открывает поток на
	// вкладку, поэтому случай не умозрительный.
	SubscriptionMaxStreamsPerSubject int `envconfig:"KACHO_API_GATEWAY_SUBSCRIPTION_MAX_STREAMS_PER_SUBJECT" default:"8"`

	// AdvertisedEndpointAddr — host:port that the api-gateway advertises through
	// the endpoint-discovery RPC. External clients dial this address. Defaults to
	// api.kacho.local:443.
	AdvertisedEndpointAddr string `envconfig:"KACHO_API_GATEWAY_ADVERTISED_ENDPOINT" default:"api.kacho.local:443"`

	// AuthNMode — режим auth-interceptor:
	//   - "dev": backwards-compat. Без Bearer = anonymous; невалидный Bearer =
	//     fallback anonymous. С валидным Bearer + subject в kaname = real Principal.
	//   - "production" (default): Bearer обязателен. Невалидный или unknown subject =
	//     Unauthenticated.
	//   - "production-strict": то же что production + reject missing Bearer.
	//
	// Дефолт — production (secure-by-default, core rule #16): незаданный env НЕ должен
	// поднимать edge в anonymous-fallback posture. dev — явный opt-in dev-профиля.
	AuthNMode string `envconfig:"KACHO_API_GATEWAY_AUTHN_MODE" default:"production"`

	// IdentityProvider — ПОСАДКА ЛИЧНОСТИ, объявленная профилем: проверяет ли
	// человека внешний поставщик удостоверений (`external`) или наша
	// собственная чеканка (`own`). Задача #1125.
	//
	// УМОЛЧАНИЯ НЕТ НАМЕРЕННО, и это отличает поле от всех соседних. Каждое из
	// двух возможных умолчаний неверно по-своему: `external` заставило бы
	// профиль, поля не объявивший, требовать адресов поставщика, которых у него
	// нет; `own` МОЛЧА сняло бы эти требования с профиля, который просто забыли
	// обновить. Умолчание живёт в ПРОФИЛЕ, а не здесь.
	//
	// Значение разбирается ОБЩИМ словарём (pkg/identityposture): служба прав и
	// край читают одно и то же поле, и второй словарь разошёлся бы с первым на
	// первом же новом значении — молча, потому что обе стороны компилируются.
	IdentityProvider string `envconfig:"KACHO_API_GATEWAY_IDENTITY_PROVIDER" default:""`

	// AuthNDevSecret — HMAC-secret для подписи dev-JWT (mode=dev).
	// Если пуст — Bearer-токены в dev-режиме игнорируются (всегда anonymous).
	// Production / production-strict — нужен Hydra JWKS.
	AuthNDevSecret string `envconfig:"KACHO_API_GATEWAY_AUTHN_DEV_SECRET" default:""`

	// --- composition-root settings (previously read via ad-hoc os.Getenv in
	// main.go; centralised here so they carry documented defaults + appear in the
	// single Config env contract) ---

	// AppEnv — deployment-environment label. Keys the fail-fast production authz
	// guard (validateProductionAuthzConfig) and relaxed-posture warnings. Only the
	// explicit dev-class labels "dev" / "local" / "test" tolerate a relaxed
	// posture; every other value — including an empty/unset label (the default) —
	// is production-class and fails closed, so a forgotten KACHO_APP_ENV cannot
	// skip the guard (secure-by-default, CWE-1188). Emitted from the helm overlay
	// via extraEnv.
	AppEnv string `envconfig:"KACHO_APP_ENV" default:""`

	// KratosPublicURL — base URL of the Ory Kratos public API (session /whoami).
	// The sentinel "disabled" turns Kratos session-auth off entirely. Default is
	// the cluster-internal kratos-public Service.
	KratosPublicURL string `envconfig:"KACHO_API_GATEWAY_KRATOS_PUBLIC_URL" default:"http://kacho-umbrella-kratos-public.kacho.svc:80"`

	// MetricsAddr — адрес cluster-internal ДИАГНОСТИЧЕСКОЙ поверхности края
	// (`GET /metrics`).
	//
	// Умолчание НЕПУСТО, как у семи остальных процессов платформы, и это
	// решение, а не копирование: пустое умолчание означало бы, что поверхность
	// поднимается только там, где профиль развёртывания о ней вспомнил, — а
	// забытая ручка неотличима от посадки, где сбора нет намеренно. Выключение
	// требует ЯВНО объявленной пустоты: присвоить этой ручке пустую строку в
	// профиле. Тогда причина едет в журнал и в самоотчёт процесса
	// (см. `describeDiagnosticSurface`), то есть снятие названо словами, а
	// забытое слов не имеет.
	//
	// Поверхность НЕ несёт `/healthz` и `/readyz`: они остаются на внешнем
	// слушателе, где на них нацелены пробы пода. Их переезд — отдельный предмет
	// со своим риском, а дублирование дало бы два места об одном предмете.
	MetricsAddr string `envconfig:"KACHO_API_GATEWAY_METRICS_ADDR" default:":9095"`

	// --- ПОТОЛОК ТЕМПА И ОДНОВРЕМЕННОСТИ на вызывающего, по слушателю ---
	//
	// Величины — свойство ПЛАТФОРМЫ, а не края: арендатору обещан один пол на
	// весь продукт, и «сколько мне можно» не должно зависеть от того, во что он
	// упёрся первым. Поэтому числа живут в фундаменте
	// (`grpcsrv.PlatformPublicAdmission` / `PlatformInternalAdmission`), а здесь
	// стоит только ОТСТУПЛЕНИЕ посадки от них.
	//
	// Три состояния, а не два. Посадка вправе молчать (берётся пол платформы) и
	// вправе назвать ВЕСЬ набор осей (берётся он); назвать ЧАСТЬ она не вправе —
	// такой вход выглядит настройкой и не ограничивает по незаполненной оси, а
	// оператор считает предел выставленным. Частичный набор отвергается при
	// старте (`grpcsrv.AdmissionKnobs.Resolve`), а не дополняется полом.
	//
	// Имена переменных выводятся из тега родительского поля и осей ручки:
	// KACHO_API_GATEWAY_ADMISSION_{PUBLIC,INTERNAL}_{READ_PER_SEC,
	// MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}.

	// AdmissionPublic — величины ВНЕШНЕГО gRPC-слушателя. Ключ ведра — личность
	// конечного пользователя: за краем сидит арендатор, и предел объявлен на него.
	AdmissionPublic grpcsrv.AdmissionKnobs `envconfig:"KACHO_API_GATEWAY_ADMISSION_PUBLIC"`

	// ВНУТРЕННЕГО gRPC-СЛУШАТЕЛЯ У КРАЯ НЕТ — ручек его посадки тоже (задача #1024).
	//
	// Здесь стояли адрес слушателя, его величины допуска и четыре ручки mTLS с
	// кругом доверенных отправителей. Всё это сторожило ОДНУ службу — ту, которой
	// iam гасил кэш решений края. Направление развёрнуто: соединение открывает
	// потребитель, и модулей, зовущих край, не осталось ни одного.
	//
	// Ручка, сторожащая порт без единого метода, — не запас: она объявляет
	// поверхность, которой нет, и первый же читатель профиля решит, что край
	// принимает вызовы внутрь. Внутренний REST-мультиплексор края (`InternalRESTAddr`)
	// — другой предмет и другой порт, и запрет #6 держится на нём по-прежнему.

	// --- RESERVED: KACHO_API_GATEWAY_OIDC_* (снято с контракта) ---
	// Пять ключей — _ISSUER, _EXTERNAL_ISSUER, _CLIENT_ID, _CLIENT_SECRET,
	// _REDIRECT_URI — питали обработчик интерактивного входа края, снятый вместе
	// с провайдером, чьи пути он адресовал. Их не объявлял НИ ОДИН профиль
	// развёртывания, а секрет наполнял Job, удалённый тикетом KAC-127.
	//
	// Имена ЗАРЕЗЕРВИРОВАНЫ и не переиспользуются: оператор, у которого они
	// остались в своём окружении, обязан получить «ключ ничего не делает»
	// молчанием, а не другое поведение под прежним именем. Церемонию проводит
	// консоль входа развёрнутого провайдера; заводить её заново — отдельная
	// работа со своей приёмкой, и тогда ключи объявляются НОВЫМИ именами.

	// --- AuthN core (DPoP / JWT / mTLS-bound / step-up / BCL) ---

	// APIDomain — публичный домен kacho-api (используется для построения canonical
	// `htu` в DPoP-валидации и для resolve issuer/audience). НЕ хардкод. Default
	// меняется в production helm-values.
	APIDomain string `envconfig:"KACHO_API_DOMAIN" default:"api.kacho.cloud"`

	// HydraIssuer — issuer URL Ory Hydra; используется как expected `iss` в
	// access tokens + base URL для JWKS fetch (`{HydraIssuer}/.well-known/jwks.json`).
	// Пустой → derived as `https://hydra.{APIDomain}`.
	HydraIssuer string `envconfig:"KACHO_HYDRA_ISSUER" default:""`

	// HydraJWKSURL — explicit JWKS endpoint; пустой → derived from HydraIssuer.
	HydraJWKSURL string `envconfig:"KACHO_HYDRA_JWKS_URL" default:""`

	// HydraIntrospectionURL — token-introspection endpoint on the identity
	// provider's ADMIN API (`{admin}/admin/oauth2/introspect`). Never derived:
	// the admin API is a different Service and port from the public issuer, so
	// there is nothing to derive it from. Empty ⇒ the revocation check is not
	// configured, and a production-class gateway refuses to start (see the boot
	// guard in cmd/api-gateway/revocation_validation.go).
	HydraIntrospectionURL string `envconfig:"KACHO_HYDRA_INTROSPECTION_URL" default:""`

	// HydraAdminURL — base URL of the identity provider's ADMIN API, used by the
	// logout handler to kill the provider-side session
	// (`DELETE /admin/oauth2/auth/sessions/login`). Never derived, same reason.
	// Empty ⇒ the session kill is disabled, and a production-class gateway
	// refuses to start.
	//
	// THE ADMIN API AUTHENTICATES NOBODY. Ory Hydra's admin API has no
	// authentication of its own — anyone who can reach it can mint clients, read
	// sessions and introspect tokens. Its only protection is that it is not
	// routable, so the two addresses above must always name a cluster-internal
	// Service and that Service must never be published (no ingress, no
	// LoadBalancer, no NodePort). Enforced offline by
	// deploy/tests/helm/admin-hop-transport-test.sh.
	HydraAdminURL string `envconfig:"KACHO_HYDRA_ADMIN_URL" default:""`

	// HydraAdminCAFile — path to the PEM bundle the gateway verifies the ADMIN
	// API's certificate against, when that hop is served over TLS.
	//
	// Why it exists: since the revocation check moved onto the authN layer, the
	// admin hop carries the caller's LIVE bearer on every introspection cache
	// miss, not just administrative calls. Over plaintext that bearer is
	// readable by anything on the path. Moving the hop to https only helps if
	// the certificate is VERIFIED, and an in-cluster provider certificate comes
	// from the internal CA — which this process does not trust by default (its
	// default pool is the system roots).
	//
	// Empty ⇒ no anchor, default transport. Set ⇒ the bundle becomes the ONLY
	// trust anchor for the hop, and a bundle that cannot be read or holds no
	// certificate REFUSES THE START (cmd/api-gateway/admin_hop_client.go):
	// falling back to the system roots would read as configured while verifying
	// nothing.
	HydraAdminCAFile string `envconfig:"KACHO_HYDRA_ADMIN_CA_FILE" default:""`

	// HydraJWKSCAFile — то же для ХОПА ЗА КЛЮЧАМИ ВЕРИФИКАЦИИ.
	//
	// Зачем. По этому хопу едет материал, которым край проверяет ПОДПИСЬ каждого
	// предъявителя. Подменивший его в пути подменяет и решение о доступе: дальше край
	// добросовестно верит собственному ответу. Требование то же, что у
	// административного хопа, и по той же причине — сертификат внутрикластерного
	// адреса выписан внутренним центром, которого в корнях процесса по умолчанию нет.
	//
	// ЧЕГО НЕ БЫЛО. Ручка АДРЕСА у этого хопа существовала, ручки ДОВЕРИЯ — нет,
	// поэтому «перевести хоп на защищённый транспорт» было недостижимо: клиент шёл
	// транспортом по умолчанию и отвергал внутренний сертификат. Обходов было два, и
	// оба хуже проблемы: увести край НАПРЯМУЮ к провайдеру мимо фасада (ровно тот
	// обход, который у края уже однажды находили и чинили) либо снять проверку
	// сертификата — то есть объявить защиту и не выполнять её.
	//
	// Пусто ⇒ якоря нет, транспорт по умолчанию (незащищённый внутрикластерный адрес
	// связки не требует). Задано ⇒ связка становится ЕДИНСТВЕННЫМ якорем доверия
	// хопа, а нечитаемая связка или связка без сертификата ОТКАЗЫВАЮТ В СТАРТЕ
	// (cmd/api-gateway/admin_hop_client.go): откат к системным корням читался бы как
	// настроенная проверка, не проверяя при этом ничего.
	HydraJWKSCAFile string `envconfig:"KACHO_HYDRA_JWKS_CA_FILE" default:""`

	// ─── Объявление приёма токена (Ф1б, задача #926) ──────────────────────
	//
	// Издатель на крае — МНОЖЕСТВО: платформа чеканит свои токены сама
	// (задача #897), а прежний издатель на переходе остаётся. У каждого
	// принимаемого издателя СВОЯ запись источника ключей: один набор на двоих
	// означал бы, что ключ одного проверяет токен другого.
	//
	// Разбор и стражи — tokenissuers.go; там же объяснено, почему «не
	// объявлено» отличается от «объявлено пустым».

	// TokenIssuers — принимаемые издатели через запятую.
	//
	// Не задано ⇒ строится ОДНА запись из HydraIssuer/HydraJWKSURL —
	// сегодняшняя посадка. Задано, но элементов ноль (`","`, пробелы) ⇒ ОТКАЗ
	// В СТАРТЕ, безусловный: пустой перечень означает «принимаем любого
	// издателя». Страж считает ЭЛЕМЕНТЫ, а не длину строки.
	TokenIssuers string `envconfig:"KACHO_API_GATEWAY_TOKEN_ISSUERS" default:""`

	// TokenIssuerKeySets — привязка «издатель=адрес набора», записи через
	// запятую.
	//
	// Адрес ОБЪЯВЛЯЕТСЯ и НИКОГДА не выводится из издателя: издатель приходит
	// от предъявителя (недоверенный вход), а производный адрес получался бы у
	// ВСЯКОГО издателя — состояние «записи нет» не наступало бы никогда, и
	// страж старта остался бы в тексте, не имея возможности упасть.
	TokenIssuerKeySets string `envconfig:"KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS" default:""`

	// PlatformTokenIssuer — издатель, под которым чеканит НАША платформа.
	//
	// Выбирает полосу приёма: строгий тип `at+jwt` (RFC 9068) и чтение отзыва
	// на предъявлении. Непусто, но вне TokenIssuers ⇒ отказ в старте.
	// Пусто ⇒ наша чеканка краем не принимается — это и есть выключатель
	// перехода, и откат состоит в его снятии, а не в перекатке образа.
	PlatformTokenIssuer string `envconfig:"KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER" default:""`

	// PlatformTokenRevocationURL — НАШ авторитет отзыва (RFC 7662).
	//
	// Обязателен, когда наш издатель принимается: прежний провайдер о наших
	// токенах не знает by construction, и его ответ на наш токен есть
	// утверждение о чужом предмете. Задаётся явно, никогда не выводится.
	//
	// ИСХОД при незаданном названо здесь, чтобы его не приходилось выводить:
	// наш издатель принимается, адрес пуст ⇒ ОТКАЗ В СТАРТЕ (TokenAcceptance →
	// requirePlatformRevocationAuthority → os.Exit в композиционном корне).
	// МЯГКОГО ПРОХОДА на этой полосе нет и не было НИ РАЗУ — в отличие от полосы
	// прежнего провайдера, где пустой адрес даёт предупреждение и неподключённое
	// чтение. Разница намеренная: там отзывы чужие и провайдер их проверяет сам,
	// здесь производитель отзыва МЫ, и «спросить некого» означало бы «выпустили
	// то, что не умеем отозвать».
	PlatformTokenRevocationURL string `envconfig:"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL" default:""`

	// PlatformTokenRevocationCAFile — якорь доверия хопа к нашему авторитету
	// отзыва.
	//
	// По этому хопу едет ПРЕДЪЯВЛЕННЫЙ токен, поэтому сертификат авторитета
	// проверяется против этой связки, а САМ край предъявляет себя парой из
	// двух ручек ниже. Пусто ⇒ транспорт по умолчанию; задано ⇒ связка становится
	// ЕДИНСТВЕННЫМ якорем, а нечитаемая связка ОТКАЗЫВАЕТ В СТАРТЕ: откат к
	// системным корням читался бы как настроенная проверка, не проверяя ничего.
	PlatformTokenRevocationCAFile string `envconfig:"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE" default:""`

	// PlatformTokenRevocationCertFile / PlatformTokenRevocationKeyFile —
	// КЛИЕНТСКАЯ ПАРА этого хопа: то, чем край предъявляет СЕБЯ авторитету.
	//
	// Комментарий выше утверждал, что хоп «идёт под клиентским сертификатом
	// края», ещё когда предъявлять было нечем: ручки не существовало, транспорт
	// нёс только якорь доверия. Авторитет отзыва выставлен на слушателе,
	// который сертификат ЗАПРАШИВАЕТ, и отвечает опознавательным словом тому,
	// кто проверенной цепочки не дал, — то есть контроль был собран, провязан,
	// исполнялся на каждом запросе и не мог ответить «действует» НИ РАЗУ.
	//
	// Пусто ⇒ хоп идёт без сертификата (профиль, где авторитет его не
	// спрашивает, законен). Задано наполовину или нечитаемо ⇒ ОТКАЗ В СТАРТЕ:
	// объявить личность и не предъявлять её — то же самое, что не объявлять,
	// только незаметно.
	PlatformTokenRevocationCertFile string `envconfig:"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE" default:""`
	PlatformTokenRevocationKeyFile  string `envconfig:"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE" default:""`

	// JWKSCacheTTL — TTL для JWKS cache (sec); RFC рекомендация 5–60 min.
	JWKSCacheTTLSeconds int `envconfig:"KACHO_JWKS_CACHE_TTL_SECONDS" default:"300"`

	// JWKSFetchTimeout — таймаут на single JWKS fetch (sec).
	JWKSFetchTimeoutSeconds int `envconfig:"KACHO_JWKS_FETCH_TIMEOUT_SECONDS" default:"5"`

	// IdempotencyStoreKind — где живут записи однократности `Idempotency-Key`:
	// "memory" (в памяти процесса) либо "postgres" (общее хранилище флота).
	//
	// В памяти — законно РОВНО для флота из одной реплики: повтор, попавший в
	// соседний под, записи не находит и уходит к downstream. Пару «вид хранилища
	// ↔ FleetSize» сверяет отказ в старте
	// (cmd/api-gateway/idempotency_validation.go), поэтому объявление посадки и
	// объявление процесса больше не могут разойтись молча (#694).
	IdempotencyStoreKind string `envconfig:"KACHO_IDEMPOTENCY_STORE" default:"memory"`

	// IdempotencyDSN — адрес общего хранилища однократности. Задаётся ЯВНО:
	// производить его из адреса соседа запрещено — такой адрес всегда непуст,
	// поэтому хранилище выглядело бы настроенным и вело в никуда.
	IdempotencyDSN string `envconfig:"KACHO_IDEMPOTENCY_DSN" default:""`

	// FleetSize — верхняя граница числа реплик края, объявленная профилем
	// посадки: максимум автомасштабирования, если оно включено, иначе число
	// реплик развёртывания. Рендерится чартом ИЗ ТОГО ЖЕ значения, что питает
	// автомасштабирование, — иначе это было бы второе объявление об одном
	// предмете, а расходятся такие пары молча.
	FleetSize int `envconfig:"KACHO_GATEWAY_FLEET_SIZE" default:"1"`

	// DPoPReplayCacheSize — LRU capacity для DPoP-replay (entries).
	DPoPReplayCacheSize int `envconfig:"KACHO_DPOP_REPLAY_CACHE_SIZE" default:"100000"`

	// DPoPReplayCacheTTLSeconds — TTL для DPoP-replay entries (sec). Должен быть
	// ≥ 2× iat-freshness-window (60s × 2 = 120s default).
	DPoPReplayCacheTTLSeconds int `envconfig:"KACHO_DPOP_REPLAY_CACHE_TTL_SECONDS" default:"120"`

	// DPoPIatFreshnessSeconds — допустимое отклонение DPoP `iat` от now() (sec).
	// RFC 9449 рекомендация 60s.
	DPoPIatFreshnessSeconds int `envconfig:"KACHO_DPOP_IAT_FRESHNESS_SECONDS" default:"60"`

	// JWTClockSkewSeconds — допустимый clock skew для JWT `exp`/`nbf` (sec).
	JWTClockSkewSeconds int `envconfig:"KACHO_JWT_CLOCK_SKEW_SECONDS" default:"30"`

	// IntrospectionCacheTTLSeconds — TTL для introspection-cache entries (sec).
	IntrospectionCacheTTLSeconds int `envconfig:"KACHO_INTROSPECTION_CACHE_TTL_SECONDS" default:"5"`

	// IntrospectionCacheSize — LRU capacity для introspection cache (entries).
	IntrospectionCacheSize int `envconfig:"KACHO_INTROSPECTION_CACHE_SIZE" default:"10000"`

	// IntrospectionTimeoutMs — hard budget for ONE round-trip to the provider's
	// introspection endpoint (ms). This is a blocking step on the request path,
	// so the number is what an unwell provider may cost a request-handling
	// goroutine. A healthy round-trip is a single intra-cluster POST over one
	// indexed lookup — tens of milliseconds — and the cache above means a given
	// token pays it at most once per TTL. 1s is roughly ten times the healthy
	// case: room for a cold connection or a stalled lookup, while a brown-out
	// cannot pin the gateway's capacity waiting on answers no caller is still
	// there to receive.
	IntrospectionTimeoutMs int `envconfig:"KACHO_INTROSPECTION_TIMEOUT_MS" default:"1000"`

	// HookSharedSecret — shared secret для Hydra→kaname back-channel logout
	// (RFC 8254). Также используется как HMAC для CAEP push payload integrity.
	HookSharedSecret string `envconfig:"KANAME_HOOK_TOKEN" default:""`

	// AuthNEnableDPoP — feature toggle; true → требовать DPoP/mTLS-bound для
	// tokens с `cnf` claim, валидировать. False → skip DPoP проверки (legacy
	// dev mode без sender-constrained tokens).
	AuthNEnableDPoP bool `envconfig:"KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP" default:"false"`

	// AuthNEnforceStepUp — apply the per-RPC authentication floor
	// (`required_acr_min` from the permission catalog) on the authN layer every
	// request passes through.
	//
	// It is a SEPARATE knob from AuthNEnableDPoP on purpose. That one mounts the
	// proof-of-possession validators — a property of SOME tokens, which issuance
	// does not yet mint, so demanding it would refuse every machine credential.
	// How strongly the caller authenticated is a property of EVERY token. Sharing
	// one toggle is how the floor came to be declared per RPC, mirrored into the
	// identity service, and applied by nothing on every deployed stand.
	//
	// Default false so an in-process fixture may run without it. A
	// production-class environment does not get that choice: the boot guard
	// refuses to start when the catalog declares a floor and this is off, so
	// every deployable profile states it.
	AuthNEnforceStepUp bool `envconfig:"KACHO_API_GATEWAY_AUTHN_ENFORCE_STEP_UP" default:"false"`

	// AuthNRequireMachineTokenBinding — true → a token whose principal is a
	// MACHINE (kacho_principal_type=service_account) MUST be sender-constrained
	// (RFC 7800 `cnf`: DPoP `jkt` or mTLS `x5t#S256`); an unbound machine token
	// is rejected 401. The human/interactive path is unaffected.
	//
	// Enforced by AuthInterceptor — the one authN layer that always runs —
	// rather than by DPoPMiddleware / CnfBindingInterceptor, which sit behind
	// AuthNEnableDPoP and are therefore unmounted on a default deployment.
	//
	// Default false: the identity provider does not yet register OAuth2 clients
	// that mint bound tokens, so switching this on before issuance lands would
	// reject every service-account token. Sequence: enable issuance
	// (kaname sa-key `bindDpop`) → confirm minted machine tokens carry `cnf`
	// → set this true.
	AuthNRequireMachineTokenBinding bool `envconfig:"KACHO_API_GATEWAY_AUTHN_REQUIRE_MACHINE_TOKEN_BINDING" default:"false"`

	// --- AuthZ core (per-RPC enforcement) ---

	// AuthZEnabled — master toggle for the per-RPC AuthZ middleware. When
	// false (default), the middleware mounts as a pass-through (compat with
	// dev environments that have no IAM AuthorizeService to ask).
	AuthZEnabled bool `envconfig:"KACHO_API_GATEWAY_AUTHZ_ENABLED" default:"false"`

	// AuthZFailOpen — when true, transient IAM-Check failures (Unavailable
	// / DeadlineExceeded) PASS the request through (logged ERROR). Default
	// false (fail-closed); only flip to true in dev / staging emergencies.
	AuthZFailOpen bool `envconfig:"KACHO_API_GATEWAY_AUTHZ_FAIL_OPEN" default:"false"`

	// IAMAuthorizeURL — gRPC address of kaname AuthorizeService. Empty
	// → derives from IAMAddr (public iam endpoint, port 9090). Отдельный env
	// позволяет вынести AuthorizeService на свой pod ради HA.
	IAMAuthorizeURL string `envconfig:"KACHO_API_GATEWAY_IAM_AUTHORIZE_URL" default:""`

	// AuthZCacheTTLSeconds — decision-cache TTL (sec). Default 5s.
	AuthZCacheTTLSeconds int `envconfig:"KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS" default:"5"`

	// AuthZCacheMaxEntries — LRU cap. Default 10000.
	AuthZCacheMaxEntries int `envconfig:"KACHO_API_GATEWAY_AUTHZ_CACHE_MAX_ENTRIES" default:"10000"`

	// AuthZCheckTimeoutMs — hard timeout per AuthorizeService.Check (ms).
	// Default 200ms.
	AuthZCheckTimeoutMs int `envconfig:"KACHO_API_GATEWAY_AUTHZ_CHECK_TIMEOUT_MS" default:"200"`

	// AuthZPermissionCatalogFile — runtime override path for the catalog
	// JSON. Empty → use the embedded asset (build-time pinned). ConfigMap
	// mounts go here; SIGHUP triggers reload.
	AuthZPermissionCatalogFile string `envconfig:"KACHO_API_GATEWAY_PERMISSION_CATALOG_FILE" default:""`

	// AuthZOverridesFile — file-based per-route overrides (allow/deny).
	// Empty → no overrides. SIGHUP reload.
	AuthZOverridesFile string `envconfig:"KACHO_API_GATEWAY_AUTHZ_OVERRIDES_FILE" default:""`

	// AuthZTrustedXForwardedFor — honour X-Forwarded-For / X-Real-IP when
	// computing the `client_ip` Condition context value. True for typical
	// k8s ingress topology (api-gateway sits behind an L7 LB that strips
	// client-supplied values). Flip to false when running api-gateway
	// directly on the wire.
	AuthZTrustedXForwardedFor bool `envconfig:"KACHO_API_GATEWAY_AUTHZ_TRUSTED_XFF" default:"true"`

	// AuthZTrustedProxyCount — number of trusted reverse-proxy hops in front of
	// the gateway. X-Forwarded-For is read from the RIGHT: the client IP is the
	// entry the outermost trusted proxy recorded (parts[len-N]), so a
	// client-forged leftmost XFF cannot drive `client_ip` / `source_ip_in_range`.
	// Default 1 (single k8s ingress). Set 0 to ignore forwarded headers entirely
	// and treat the TCP peer as authoritative. Only consulted when
	// AuthZTrustedXForwardedFor is true.
	AuthZTrustedProxyCount int `envconfig:"KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_COUNT" default:"1"`

	// SubjectChangePollInterval — how often the subject-change watcher polls
	// kaname InternalIAMService.PollSubjectChanges to flush the authz
	// decision cache on sibling replicas that did not process the mutation.
	// Default 2s. Omit the env var (or set 0) to use the built-in default.
	SubjectChangePollInterval time.Duration `envconfig:"KACHO_API_GATEWAY_SUBJECT_CHANGE_POLL_INTERVAL" default:"2s"`

	// --- per-edge backend-dial mTLS ---
	//
	// Backward-compat default = OFF: all *_ENABLE false, cert/key/ca empty ⇒
	// every backend dial is insecure, identical to current dev. When an edge is
	// enabled the gateway presents the shared "api-gateway" client-cert
	// (CertFile/KeyFile), verifies the backend server-cert against CAFile, and
	// checks the server-cert SAN against the per-edge ServerName (or the dial-host
	// when unset). enable=true with missing cert material ⇒ fail-fast at startup,
	// never a silent insecure fallback.
	//
	// One shared client cert/key/ca across all edges (one "api-gateway" module
	// identity); per-edge ENABLE + SERVER_NAME give independent rollback.
	MTLSClientCertFile string `envconfig:"KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE" default:""`
	MTLSClientKeyFile  string `envconfig:"KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE"  default:""`
	MTLSCAFile         string `envconfig:"KACHO_API_GATEWAY_MTLS_CA_FILE"          default:""`

	MTLSVPCEnable      bool `envconfig:"KACHO_API_GATEWAY_MTLS_VPC_ENABLE"      default:"false"`
	MTLSComputeEnable  bool `envconfig:"KACHO_API_GATEWAY_MTLS_COMPUTE_ENABLE"  default:"false"`
	MTLSIAMEnable      bool `envconfig:"KACHO_API_GATEWAY_MTLS_IAM_ENABLE"      default:"false"`
	MTLSNLBEnable      bool `envconfig:"KACHO_API_GATEWAY_MTLS_NLB_ENABLE"      default:"false"`
	MTLSGeoEnable      bool `envconfig:"KACHO_API_GATEWAY_MTLS_GEO_ENABLE"      default:"false"`
	MTLSRegistryEnable bool `envconfig:"KACHO_API_GATEWAY_MTLS_REGISTRY_ENABLE" default:"false"`
	MTLSStorageEnable  bool `envconfig:"KACHO_API_GATEWAY_MTLS_STORAGE_ENABLE"  default:"false"`

	// Per-edge SNI/server-name overrides. Empty ⇒ derive from the dial-addr host.
	MTLSVPCServerName      string `envconfig:"KACHO_API_GATEWAY_MTLS_VPC_SERVER_NAME"      default:""`
	MTLSComputeServerName  string `envconfig:"KACHO_API_GATEWAY_MTLS_COMPUTE_SERVER_NAME"  default:""`
	MTLSIAMServerName      string `envconfig:"KACHO_API_GATEWAY_MTLS_IAM_SERVER_NAME"      default:""`
	MTLSNLBServerName      string `envconfig:"KACHO_API_GATEWAY_MTLS_NLB_SERVER_NAME"      default:""`
	MTLSGeoServerName      string `envconfig:"KACHO_API_GATEWAY_MTLS_GEO_SERVER_NAME"      default:""`
	MTLSRegistryServerName string `envconfig:"KACHO_API_GATEWAY_MTLS_REGISTRY_SERVER_NAME" default:""`
	MTLSStorageServerName  string `envconfig:"KACHO_API_GATEWAY_MTLS_STORAGE_SERVER_NAME"  default:""`

	// Hybrid external listener: when true, the external TLS listener
	// (TLSListenAddr) runs with tls.VerifyClientCertIfGiven and the internal CA
	// (MTLSCAFile) as ClientCAs — an OPTIONAL client cert. A browser (no cert)
	// handshakes and takes the JWT path; a client presenting a valid Kachō cert
	// is verified so the AuthInterceptor can derive a principal from its SPIFFE
	// SAN (no JWT required). Default false ⇒ ClientAuth stays NoClientCert,
	// behaviour unchanged. Internal service listeners are NOT affected by this
	// flag (they stay strict RequireAndVerifyClientCert).
	HybridMTLSExternal bool `envconfig:"KACHO_API_GATEWAY_HYBRID_MTLS_EXTERNAL" default:"false"`
}

// DPoPReplayTTL — сколько живёт запись о предъявленном доказательстве.
//
// Метод, а не выражение по месту: величину читают ДВОЕ — страж однократности
// (сколько держать запись) и сборщик (с каким шагом её убирать), — и оба обязаны
// брать её из одного места. Два одинаковых выражения по разным файлам разошлись
// бы молча, и уборка стала бы либо реже жизни строки, либо чаще, чем нужно.
func (c Config) DPoPReplayTTL() time.Duration {
	return time.Duration(c.DPoPReplayCacheTTLSeconds) * time.Second
}

// TLSEnabled возвращает true, если TLS-listener должен быть запущен.
// Требует одновременно TLS_LISTEN_ADDR + TLS_CERT_FILE + TLS_KEY_FILE.
func (c Config) TLSEnabled() bool {
	return c.TLSListenAddr != "" && c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// AdvertisedEndpoint returns the host:port to advertise through the
// endpoint-discovery RPC.
func (c Config) AdvertisedEndpoint() string {
	return c.AdvertisedEndpointAddr
}

// HybridMTLSEnabled reports whether the external TLS listener should accept an
// optional client cert. When false (default) the listener stays NoClientCert
// (JWT-only authN).
func (c Config) HybridMTLSEnabled() bool {
	return c.HybridMTLSExternal
}

// ExternalListenerClientAuth applies the hybrid client-auth policy to the
// external listener's *tls.Config and returns it. When hybrid is disabled it is a
// no-op (ClientAuth stays NoClientCert). When enabled it sets
// tls.VerifyClientCertIfGiven with the internal CA (MTLSCAFile) as ClientCAs, so
// a browser without a cert still handshakes (JWT path) while a client that DOES
// present a cert has it verified against the trust anchor — the AuthInterceptor
// then derives the principal from the verified cert's SPIFFE SAN.
//
// Fail-fast: hybrid enabled with no readable CA file is an error (a listener that
// cannot verify any client cert would silently degrade every cert client to the
// JWT path — the operator must know).
func (c Config) ExternalListenerClientAuth(base *tls.Config) (*tls.Config, error) {
	if base == nil {
		base = &tls.Config{}
	}
	if !c.HybridMTLSExternal {
		return base, nil
	}
	if c.MTLSCAFile == "" {
		return nil, fmt.Errorf(
			"hybrid mTLS external listener enabled but client-CA missing " +
				"(KACHO_API_GATEWAY_MTLS_CA_FILE)")
	}
	caPEM, err := os.ReadFile(c.MTLSCAFile)
	if err != nil {
		return nil, fmt.Errorf("read hybrid client-CA %q: %w", c.MTLSCAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("hybrid client-CA %q: no certificates parsed", c.MTLSCAFile)
	}
	base.ClientAuth = tls.VerifyClientCertIfGiven
	base.ClientCAs = pool
	return base, nil
}

// ResolvedHydraIssuer returns the Hydra issuer URL, deriving it from APIDomain
// when explicitly unset. Trailing slash is stripped.
func (c Config) ResolvedHydraIssuer() string {
	iss := c.HydraIssuer
	if iss == "" {
		iss = "https://hydra." + c.APIDomain
	}
	for len(iss) > 0 && iss[len(iss)-1] == '/' {
		iss = iss[:len(iss)-1]
	}
	return iss
}

// ResolvedHydraJWKSURL returns the JWKS endpoint, deriving from issuer when
// not explicitly set.
func (c Config) ResolvedHydraJWKSURL() string {
	if c.HydraJWKSURL != "" {
		return c.HydraJWKSURL
	}
	return c.ResolvedHydraIssuer() + "/.well-known/jwks.json"
}

// ResolvedHydraIntrospectionURL returns the token-introspection endpoint, or the
// empty string when none is configured.
//
// It is deliberately NOT derived from the issuer. Introspection is served by the
// identity provider's ADMIN API — a different Service and port from the public
// issuer, reachable only inside the cluster — so an issuer-derived address names
// a server that does not serve this endpoint. Aiming a revocation check at a
// guessed address is worse than having none: the check runs, never gets an
// answer, and the caller cannot distinguish that from "the token is fine".
//
// Empty means the revocation check is not configured. The composition root
// refuses to start a production-class gateway in that state.
func (c Config) ResolvedHydraIntrospectionURL() string {
	return strings.TrimSpace(c.HydraIntrospectionURL)
}

// IdentityProviderKnob — имя ручки посадки личности НА КРАЕ. Объявлено один
// раз: его называют текст отказа старта и документация профиля; две копии
// разошлись бы на той, которую забыли поправить.
const IdentityProviderKnob = "KACHO_API_GATEWAY_IDENTITY_PROVIDER"

// ResolvedIdentityProvider разбирает объявленную посадку личности.
//
// Незаданное значение возвращается как «не задано» БЕЗ ошибки: отказ старта
// производит страж, называя ручку и оба законных значения. Отказ здесь назвал
// бы то же самое вторым текстом.
func (c Config) ResolvedIdentityProvider() (identityposture.Provider, error) {
	raw := c.IdentityProvider
	if strings.TrimSpace(raw) == "" {
		return identityposture.Unset, nil
	}
	return identityposture.Parse(IdentityProviderKnob, raw)
}

// ResolvedHydraAdminURL returns the admin API base, or the empty string when
// none is configured. Same rule and same reason as the introspection endpoint
// above: the admin API is not the issuer, and a guessed base sends the logout
// handler's provider-side session kill to whatever answers on the issuer host.
func (c Config) ResolvedHydraAdminURL() string {
	return strings.TrimSpace(c.HydraAdminURL)
}

// ExpectedAudience returns the audience value injected in tokens for this
// API gateway — `https://{APIDomain}`. Used as the expected `aud` during JWT
// validation.
func (c Config) ExpectedAudience() string {
	return "https://" + c.APIDomain
}

// ResolvedIAMAuthorizeURL returns the AuthorizeService address, deriving it
// from IAMAddr when the explicit IAMAuthorizeURL is unset.
func (c Config) ResolvedIAMAuthorizeURL() string {
	if c.IAMAuthorizeURL != "" {
		return c.IAMAuthorizeURL
	}
	return c.IAMAddr
}

// internalBackendKeySuffix — как называется ключ ВНУТРЕННЕГО адреса домена в
// карте соединений края.
//
// Объявлен ЗДЕСЬ, рядом с самой картой, а не у того, кто им пользуется: суффикс
// есть свойство карты, и вторая его копия разошлась бы с ней молча — обе непусты,
// обе выглядят действующими, а ведут в разные ключи.
const internalBackendKeySuffix = "Internal"

// InternalBackendKey — ключ внутреннего адреса домена в карте соединений.
//
// Единственный способ получить этот ключ. Подписка живёт только на внутреннем
// слушателе владельца, поэтому край резолвит владельца именно им.
func InternalBackendKey(domain string) string {
	return domain + internalBackendKeySuffix
}

// DomainsWithInternalBackend — домены, у которых край ЗНАЕТ внутренний адрес.
//
// Это и есть множество имён, принимаемых в `KACHO_API_GATEWAY_SUBSCRIPTION_OWNERS`
// и в параметре `owner` ручки потока: имя вне множества дозвониться не может, и
// страж старта отвергает его.
//
// # Почему имя домена, а не каталог сервиса в дереве
//
// Ключ совпадает с пакетом контракта (`kacho.cloud.<домен>.v1`), по которому
// маршрутизирует gRPC-роутер, — то есть с тем написанием, которое видит клиент.
// Каталог сервиса в дереве может зваться иначе (`services/nlb` против домена
// `loadbalancer`), и это написание наружу не выходит вовсе.
func (c Config) DomainsWithInternalBackend() []string {
	addrs := c.BackendAddrs()
	names := make([]string, 0, len(addrs)/2)
	for key := range addrs {
		if _, ok := addrs[InternalBackendKey(key)]; ok {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

// BackendAddrs возвращает карту domain → адрес для инициализации Backends.
// "iam" / "iamInternal" — kaname public (9090) / internal (9091) endpoints.
// "loadbalancer" / "loadbalancerInternal" — kacho-nlb public / internal endpoints.
// Domain-ключ "loadbalancer" совпадает с proto-package `kacho.cloud.loadbalancer.v1.*`,
// по которому gRPC-роутер (server.go Resolver / shimproxy.go) выбирает backend.
// "geo" / "geoInternal" — kacho-geo public / internal endpoints. Domain-ключ
// "geo" совпадает с proto-package `kacho.cloud.geo.v1.*` (та же маршрутизация).
// "quota" — пакет общей формы ответа о квотах; его единственную службу
// обслуживает kaname (см. комментарий у ключа).
func (c Config) BackendAddrs() map[string]string {
	return map[string]string{
		"vpc":             c.VPCAddr,
		"vpcInternal":     c.VPCInternalAddr,
		"compute":         c.ComputeAddr,
		"computeInternal": c.ComputeInternalAddr,
		"iam":             c.IAMAddr,
		"iamInternal":     c.IAMInternalAddr,
		// "quota" — пакет ОБЩЕЙ формы ответа о квотах, и в нём объявлена ровно
		// одна служба: чтение квот, носителем которых является личность. Её
		// обслуживает kaname, поэтому адрес тот же, что у "iam".
		//
		// Служба живёт не в `iam.v1` потому, что общая форма ответа уже зависит
		// от `iam.v1` (область назначенной величины), и обратная ссылка замкнула
		// бы пакеты друг на друга — это отвергает `buf lint`, а не вкус.
		// Роутер выбирает backend по сегменту пакета, поэтому ключ обязан быть
		// здесь: без него метод разрешался бы в никуда, оставаясь в allow-list.
		"quota":                c.IAMAddr,
		"loadbalancer":         c.NLBAddr,
		"loadbalancerInternal": c.NLBInternalAddr,
		"geo":                  c.GeoAddr,
		"geoInternal":          c.GeoInternalAddr,
		"registry":             c.RegistryAddr,
		"registryInternal":     c.RegistryInternalAddr,
		"storage":              c.StorageAddr,
		"storageInternal":      c.StorageInternalAddr,
	}
}

// EdgeTLSClient assembles the corelib grpcclient.TLSClient value-struct for a
// backend edge ("vpc" | "compute" | "iam" | "nlb" | "geo" | "registry" | "storage"),
// deriving the server-name from the dial address host when no per-edge override
// is set.
//
// Contract:
//   - edge disabled ⇒ {Enable:false}; cert material is NOT consulted (insecure
//     dial; dev backward-compat). The returned struct is safe to pass to
//     grpcclient.TLSClientCreds.
//   - edge enabled ⇒ {Enable:true, CertFile, KeyFile, CAFiles, ServerName}; if any
//     of cert/key/ca is empty the call FAILS (fail-fast), never a silent insecure
//     fallback. PEM validity itself is enforced later by grpcclient.TLSClientCreds.
//   - unknown edge ⇒ error (programming error).
func (c Config) EdgeTLSClient(edge, dialAddr string) (grpcclient.TLSClient, error) {
	enable, serverNameOverride, err := c.edgeMTLS(edge)
	if err != nil {
		return grpcclient.TLSClient{}, err
	}
	if !enable {
		return grpcclient.TLSClient{Enable: false}, nil
	}

	// Fail-fast: enabled edge demands the full shared cert material. A silent
	// insecure fallback here would defeat the security contract.
	if c.MTLSClientCertFile == "" || c.MTLSClientKeyFile == "" || c.MTLSCAFile == "" {
		return grpcclient.TLSClient{}, fmt.Errorf(
			"mtls %s enabled but client cert/key/ca missing "+
				"(KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE/_KEY_FILE/_CA_FILE)", edge)
	}

	serverName := serverNameOverride
	if serverName == "" {
		serverName = hostFromAddr(dialAddr)
	}
	if serverName == "" {
		return grpcclient.TLSClient{}, fmt.Errorf(
			"mtls %s enabled but server_name could not be derived from dial addr %q "+
				"(set KACHO_API_GATEWAY_MTLS_%s_SERVER_NAME)", edge, dialAddr, strings.ToUpper(edge))
	}

	return grpcclient.TLSClient{
		Enable:     true,
		CertFile:   c.MTLSClientCertFile,
		KeyFile:    c.MTLSClientKeyFile,
		CAFiles:    []string{c.MTLSCAFile},
		ServerName: serverName,
	}, nil
}

// edgeMTLS resolves the per-edge enable flag + server-name override.
func (c Config) edgeMTLS(edge string) (enable bool, serverName string, err error) {
	switch edge {
	case "vpc":
		return c.MTLSVPCEnable, c.MTLSVPCServerName, nil
	case "compute":
		return c.MTLSComputeEnable, c.MTLSComputeServerName, nil
	case "iam":
		return c.MTLSIAMEnable, c.MTLSIAMServerName, nil
	case "nlb":
		return c.MTLSNLBEnable, c.MTLSNLBServerName, nil
	case "geo":
		return c.MTLSGeoEnable, c.MTLSGeoServerName, nil
	case "registry":
		return c.MTLSRegistryEnable, c.MTLSRegistryServerName, nil
	case "storage":
		return c.MTLSStorageEnable, c.MTLSStorageServerName, nil
	default:
		return false, "", fmt.Errorf("unknown mtls edge %q", edge)
	}
}

// hostFromAddr returns the host portion of a "host:port" dial address (or the
// input unchanged when it has no port).
func hostFromAddr(addr string) string {
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return addr
	}
	return host
}

// Load читает конфигурацию из переменных окружения.
func Load() (Config, error) {
	var cfg Config
	if err := corecfg.Load(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SubscriptionOwnerNames разбирает объявленный перечень владельцев журналов.
//
// Разделитель — запятая, пустые элементы отбрасываются. Отбрасывание не
// косметика: вырожденное значение (одинокая запятая) даёт непустую строку и
// НОЛЬ имён, и без него «объявлен» решалось бы по длине строки, а не по числу
// имён — тот же разрыв, что однажды уже дал круг отправителей, непустой для
// стража и пустой для транспорта.
func (c Config) SubscriptionOwnerNames() []string {
	names := make([]string, 0, 4)
	for _, raw := range strings.Split(c.SubscriptionOwners, ",") {
		if name := strings.TrimSpace(raw); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// Posture — посадка процесса, разобранная ОБЩИМ словарём.
//
// Словарь допустимых написаний объявлен в дереве один раз
// (`servicecontract.Modes`). Пока край сравнивал строку на месте и приводил её к
// своему типу БЕЗ разбора, написание вне перечня не совпадало ни с одной
// константой: процесс поднимался, объявляя посадку, которой не существует, и
// каждый свич уходил в ветку `default`.
//
// Неразобранное значение читается как БОЕВАЯ посадка. Это не «умолчание»: старт
// на нём отвергает страж посадки (`describePosture`), который называет ручку и
// перечисляет словарь; а до отказа послаблений быть не должно — иначе опечатка в
// профиле тихо открывала бы то, что закрывает боевой режим.
func (c Config) Posture() servicecontract.Mode { return PostureOf(c.AuthNMode) }

// PostureOf — та же посадка, но по ЗНАЧЕНИЮ ручки, а не по конфигурации целиком.
//
// Второе написание одного предмета заведено осознанно, и вот зачем. Место сборки
// компонента обязано НАЗЫВАТЬ ручки, от которых компонент зависит: гейт края
// (`gateway/deploy/producerless_input_test.go`) читает аргументы конструктора и
// спрашивает, может ли хоть один профиль его настроить. Метод на конфигурации
// зависимость прячет — в аргументе видно `cfg.Posture()`, а не `AuthNMode`, — и
// компонент, настраиваемый ровно этой ручкой, выглядел бы ненастраиваемым ничем.
//
// Реализация ОДНА: метод делегирует сюда. Двух разборов одной ручки не
// заводится — разойтись им было бы нечем, но читателю пришлось бы это доказывать.
func PostureOf(raw string) servicecontract.Mode {
	mode, err := servicecontract.ParseMode(raw)
	if err != nil {
		return servicecontract.ModeProduction
	}
	return mode
}
