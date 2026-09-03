// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package restmux инициализирует REST-фасад grpc-gateway для api-gateway.
//
// Регистрирует активные сервисы Kachō + OperationService через OpsProxy.
// loadbalancer (kacho-nlb) — NetworkLoadBalancer / Listener / TargetGroup
// регистрируются на public mux (/nlb/v1/*). InternalResourceLifecycleService —
// streaming gRPC-direct only (нет HTTP-аннотаций; consumer'ы ходят в loadbalancer.kacho.svc:9091
// напрямую gRPC; через grpc-gateway не проксируется).
//
// # Split-mux pattern
//
// Внутри пакета поднимается ДВА grpc-gateway `runtime.ServeMux`-а с разными
// `protojson.MarshalOptions`:
//
//   - public mux   — `EmitUnpopulated=true`. Tenant-facing контракт: клиент
//     должен видеть поле даже если оно пустое (`description: ""`, `labels: {}`,
//     `cidrBlocks: []`, `defaultSecurityGroupId: ""`, и т.п.). Это часть
//     стабильного API.
//   - internal mux — `EmitUnpopulated=false`. Admin-ресурсы и internal-проекции
//     публичных ресурсов отдают много zero-полей. На внутренней admin/UI
//     поверхности этот шум вреден и сбивает админов.
//
// ПУБЛИЧНЫЕ RPC handlers регистрируются на ОБА mux'а — разница только в JSON
// маршалинге. АДМИНИСТРАТИВНЫЕ (Internal*) — ТОЛЬКО на internal mux: на public
// они были недостижимы by construction, а их отсутствие там делает public mux
// точной моделью «листенера, у которого административной поверхности нет».
// На эту модель опирается укрытие (ниже).
//
// Диспетчер выбирает нужный mux по ПАРЕ (HTTP-метод, путь) —
// `isInternalRoute`, см. также internal_routes.go:
//
//   - Любой REST-биндинг любого Internal*-сервиса (собирается из
//     proto-дескрипторов) → internal mux. Именно поэтому решение keyed на пару,
//     а не на путь: admin-CRUD каталога DiskType висит на ТОМ ЖЕ пути, что и
//     публичное чтение каталога (`/storage/v1/diskTypes`),
//     и отличается только методом — `GET` публичный, `POST`/`PATCH`/`DELETE`
//     admin-only.
//   - Любой путь, содержащий сегмент `/internal` (например
//     `/vpc/v1/networks/{id}/internal`, `/vpc/v1/networkInterfaces/{id}/internal`),
//     → internal mux.
//   - Admin-only ресурсы (kacho-only, не tenant-facing) → internal mux:
//     `/vpc/v1/addressPools`,
//     `/vpc/v1/networks/{id}/addressPoolBinding`.
//   - Все остальное → public mux.
//
// Запрос, классифицированный как internal, но пришедший на ВНЕШНИЙ листенер, до
// административного обработчика не доходит: он отдаётся public mux'у и получает
// ровно тот ответ, который этот листенер даёт на маршрут, которого у него нет
// (404, а на пути с публичным маршрутом под другим методом — 501). Ответ
// производит grpc-gateway, тот же, что и на любой обычный промах. Раньше здесь
// стоял отдельный `http.NotFound`, и его ответ отличался типом содержимого,
// телом и заголовком `X-Content-Type-Options` — то есть форма 404 отвечала на
// вопрос «есть ли здесь административный путь». Это existence-hiding без
// содержания; см. external_refusal_shape_test.go.
//
// Корневой `http.Handler` (диспетчер) экспонируется как `http.Handler`
// и передается в `httpMux.Handle("/", restHandler)` в `cmd/api-gateway/main.go`.
//
// # Активные сервисы
//
//   - iam.v1: Account, Project, User, ServiceAccount, Group, Role, AccessBinding
//   - vpc.v1: Network, Subnet, Address, RouteTable, SecurityGroup, Gateway, NetworkInterface
//   - vpc.v1 admin (kacho-only): AddressPool, InternalNetwork — обслуживаются
//     internal-портом vpc backend (9091).
//   - compute.v1: Disk, Image, Snapshot, Instance, DiskType
//     (Geography Region/Zone — отдельный leaf-сервис geo.v1.)
//   - compute.v1 admin (kacho-only): InternalDiskType — обслуживается
//     internal-портом compute backend (9091).
//   - storage.v1 (kacho-storage): Volume, Snapshot, DiskType — public RPC под
//     /storage/v1/* (volumes/snapshots CRUD + diskTypes read).
//   - storage.v1 admin (kacho-only): InternalVolume (Attach/Detach/
//     ListAttachments/GetInternal — default unbound-route, gRPC-direct/internal
//     REST only) + InternalDiskType (admin CRUD) — обслуживаются internal-портом
//     storage backend (9091).
//   - geo.v1: RegionService, ZoneService — public read под /geo/v1/regions,
//     /geo/v1/zones (geoAddr). Geography — leaf-сервис kacho-geo; обслуживается
//     ИСКЛЮЧИТЕЛЬНО geo.v1.
//   - geo.v1 admin (kacho-only): InternalRegionService, InternalZoneService — admin Region/Zone
//     CRUD на internal-порту geo backend (geoInternalAddr, 9091); cluster-internal only.
//   - loadbalancer.v1 (kacho-nlb): NetworkLoadBalancerService, ListenerService,
//     TargetGroupService — публичные RPC под /nlb/v1/*. InternalResourceLifecycleService —
//     streaming gRPC-direct only, REST не регистрируется (нет http-аннотаций).
//   - registry.v1 (kacho-registry): RegistryService — публичный control-plane реестра
//     под /registry/v1/* (registries CRUD + repositories/tags/DeleteTag).
//     InternalRegistryService (GC/stats admin, :9091) — без http-аннотаций → default
//     unbound-route, cluster-internal only. Data-plane OCI v2 — отдельный ingress.
//   - iam.v1: Account, Project, User (read+delete only), ServiceAccount, Group, Role, AccessBinding —
//     все RPC public под /iam/v1/*.
//   - iam.v1 admin (kacho-only): InternalUserService.Get — для admin tooling; зарегистрирован
//     в internal mux pro-forma (proto-аннотации `google.api.http` отсутствуют → real-трафик
//     идет только через gRPC-direct до kacho-iam:9091) + REST для UpsertFromIdentity.
//     InternalIAMService: LookupSubject и Check ИМЕЮТ http-аннотации, поэтому
//     RegisterInternalIAMServiceHandlerFromEndpoint (ниже, ветка internalMux) заводит им
//     REST-маршруты `/iam/v1/internal/iam:lookupSubject` и `:check` — они есть в
//     rest_route_table_gen.go и проходят через phaseInternalOriginExempt. Здесь стояло
//     «НЕ регистрируется в REST», и это противоречило коду двумя экранами ниже: два места
//     об одном предмете, из которых верно одно. `ListPermissions` в той же строке —
//     tombstone, RPC удалён (см. internal_iam_service.proto), поэтому регистрировать его
//     нечего вовсе. Строка была не устаревшей наполовину, а неверной в обеих половинах.
//     InternalModuleService (kacho#1991): Plan/Apply/Get/List под
//     `/iam/v1/internal/modules` — тот же префикс и тот же гейт, что у
//     InternalClusterService. Маршрутов не было вовсе по записанному решению
//     под-фазы (контракт заводился без http-аннотаций); теперь они есть, и
//     оживлённая ими ступень подтверждения личности у `Apply` — в
//     `authzguard.GatewayFrontedInternalRPCs`.
//   - operation (без v1!): OperationService (in-process OpsProxy)
package restmux

import (
	"context"
	"fmt"
	"net/http"

	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	computepb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	// geo.v1 — Region/Zone leaf-сервис kacho-geo.
	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	iampb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	quotapb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/quota/v1"

	// kacho-nlb (loadbalancer.v1) — public RPC под /nlb/v1/*.
	lbpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	// kacho-registry (registry.v1) — public RPC под /registry/v1/*.
	registrypb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	// kacho-storage (storage.v1) — public RPC под /storage/v1/*.
	storagepb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// buildPrincipalMetadata собирает outgoing gRPC-metadata личности для
// передозвона в backend (public ИЛИ internal mux).
//
// Строитель ОДИН на край и живёт в `principalmeta`: личность отправляют двое —
// этот мост и проекция потока подписки, которая дозванивается сама. Вторая
// копия разошлась бы с первой молча, потому что на обычном запросе обе кладут
// одно и то же, а различаются на кириллическом имени и на мостовой форме
// заголовка.
func buildPrincipalMetadata(r *http.Request) metadata.MD {
	return principalmeta.MetadataFromRequest(r)
}

// principalHeaderMatcher — grpc-gateway IncomingHeaderMatcher: решает, какой
// inbound HTTP header пересекает REST→gRPC мост и под каким metadata-ключом.
//
// Второй (независимый от strip'а) слой защиты от identity-подлога. Первый слой —
// `middleware.AuthInterceptor.HTTP`, который вычищает ВЕСЬ клиентский
// `x-kacho-`-namespace до того, как сюда дойдёт запрос. Здесь мы к тому же
// сужаем сам мост: `runtime.DefaultHeaderMatcher` бриджит ЛЮБОЙ заголовок с
// префиксом `Grpc-Metadata-` в одноимённую gRPC-метадату, поэтому
// `Grpc-Metadata-X-Kacho-<что-угодно>` доезжал бы до backend'а как
// `x-kacho-<что-угодно>`. Для зарезервированного namespace мост открыт только
// тому, что ставит сам gateway после проверенного credential'а
// (`principalmeta.IsGatewayProducedKey`) — всё остальное в этом namespace
// отбрасывается.
//
// Свойство, ради которого форма именно такая: новый identity-заголовок нельзя
// «забыть». Чтобы он поехал на backend, его нужно явно внести в
// gateway-produced-набор `principalmeta`; ни strip, ни мост его по умолчанию не
// пропустят. Прежняя форма (перечисление запрещённых имён) требовала помнить про
// каждое новое имя — так и появилась дыра с `x-kacho-admin`/`x-kacho-project-id`.
//
// Заголовки вне `x-kacho-` namespace обрабатываются штатным
// `runtime.DefaultHeaderMatcher` (permanent-HTTP + `Grpc-Metadata-`-бридж) —
// поведение для них не меняется.
func principalHeaderMatcher(key string) (string, bool) {
	if name, ok := principalmeta.KachoNamespaceKey(key); ok {
		// Ключ, который кладёт аннотатор, мост НЕ пропускает: у одного значения
		// один производитель. Пока пропускал и он, каждый такой ключ уезжал в
		// metadata трижды — одной копией от аннотатора и двумя от моста, по
		// одной на каждую форму заголовка (#930).
		//
		// Расхождение копий было бы ненаблюдаемым: потребитель читает первую,
		// а равенство остальных держалось не проверкой, а совпадением
		// источника — и переставало держаться в тот день, когда источников
		// стало два.
		if principalmeta.IsAnnotatorProducedKey(name) {
			return "", false
		}
		// Ключ, который край производит ДЛЯ СЕБЯ, мост не пропускает: у доводов
		// условия модели прав нет потребителя за краем, и отсутствия мостовой
		// формы для этого мало — префикс мост снимает сам, поэтому голая форма
		// пересекла бы его наравне с префиксованной.
		if principalmeta.IsEdgeOnlyKey(name) {
			return "", false
		}
		if principalmeta.IsGatewayProducedKey(name) {
			return name, true
		}
		return "", false
	}
	return runtime.DefaultHeaderMatcher(key)
}

// principalMetadata — grpc-gateway WithMetadata annotator: собирает outgoing
// gRPC-metadata из gateway-выставленных headers (см. buildPrincipalMetadata).
func principalMetadata(_ context.Context, r *http.Request) metadata.MD {
	return buildPrincipalMetadata(r)
}

// isInternalRoute решает, какой sub-mux обрабатывает запрос.
//
// Решение keyed на ПАРУ (HTTP-метод, path), а не на один path: часть
// Internal*-биндингов делит REST-путь с публичным чтением того же ресурса и
// отличается ТОЛЬКО методом (каталог DiskType в storage и compute: `GET` —
// публичный DiskTypeService, `POST`/`PATCH`/`DELETE` — admin-only
// InternalDiskTypeService на :9091). Классификация по одной строке пути такие
// пары не различает в принципе.
//
// Источник истины — REST-биндинги Internal*-сервисов из proto-дескрипторов
// (см. internal_routes.go); path-shaped правила ниже остаются как
// эшелонированная защита и покрывают формы, которых в дескрипторах нет
// (default unbound-route в gRPC-форме).
func isInternalRoute(method, path string) bool {
	return isInternalPath(path) || matchesInternalRESTBinding(method, path)
}

// isInternalPath — path-shaped правила классификации (метод-агностичные).
//
// Правила (в порядке проверки):
//  1. Любой path-сегмент `/internal` ИЛИ verb-suffix `:internal` → internal mux.
//     Покрывает `/vpc/v1/networks/{id}/internal`,
//     `/vpc/v1/networkInterfaces/{id}/internal`, `/vpc/v1/networks/{id}:internal`
//     (InternalNetworkService.GetNetwork) и любые будущие `*/internal` / `*:internal`.
//  2. `/vpc/v1/addressPools` → internal.
//  3. `/vpc/v1/networks/{id}/addressPoolBinding` → internal.
//  4. Все остальное → public.
func isInternalPath(path string) bool {
	// (1) any `/internal` segment.
	// strings.Contains покрывает оба варианта:
	//   /vpc/v1/networks/{id}/internal      (suffix)
	//   /vpc/v1/.../internal/...            (mid-path, гипотетически)
	// Защищаемся от ложного срабатывания на сегменте, начинающемся с
	// "internal" но не равном ему (никаких таких путей нет в Kachō, но на
	// будущее): требуем именно `/internal` как self-contained сегмент.
	if strings.Contains(path, "/internal/") || strings.HasSuffix(path, "/internal") ||
		strings.HasSuffix(path, ":internal") {
		// `:internal` verb-suffix covers InternalNetworkService.GetNetwork
		// (`/vpc/v1/networks/{id}:internal`) — an internal projection carrying
		// infra-sensitive Network fields. Without this it routed to the public
		// mux and would slip past the external-isolation gate for
		// infra-sensitive data.
		return true
	}

	// (2) /vpc/v1/addressPools[/...]
	if path == "/vpc/v1/addressPools" ||
		strings.HasPrefix(path, "/vpc/v1/addressPools/") ||
		strings.HasPrefix(path, "/vpc/v1/addressPools:") {
		return true
	}

	// (3) /vpc/v1/networks/{id}/addressPoolBinding
	if strings.HasPrefix(path, "/vpc/v1/networks/") &&
		strings.HasSuffix(path, "/addressPoolBinding") {
		return true
	}

	// (4) Internal*Service default unbound-route
	// (`/kacho.cloud.<domain>.v1.Internal<Name>Service/<Method>`). Сервисы без
	// `google.api.http`-аннотаций (InternalRegistryService: GC/stats admin) при
	// `generate_unbound_methods` получают default gRPC-style REST-путь — он не
	// содержит сегмента `/internal`, поэтому явно ловим его тем же предикатом,
	// что gRPC-роутер (HasInternalSuffix). Без этого admin-путь ушел бы на public
	// mux и просочился на external listener. Форма пути совпадает с
	// gRPC-FQN (ведущий "/"), которую HasInternalSuffix и разбирает; для обычных
	// REST-путей (`/registry/v1/registries`, `/vpc/v1/networks`) предикат ложен.
	if allowlist.HasInternalSuffix(path) {
		return true
	}

	return false
}

// NewMux создает grpc-gateway split-mux (public + internal) и регистрирует
// активные публичные сервисы плюс OperationService (через OpsProxy).
//
// Возвращает `http.Handler`-диспетчер, который на каждый request выбирает
// public или internal sub-mux на основании `isInternalRoute(r.Method, r.URL.Path)`.
//
// addrs — карта domain → адрес gRPC backend:
//
//	"iam"                  → kacho-iam.kacho.svc:9090
//	"iamInternal"          → kacho-iam.kacho.svc:9091
//	"vpc"                  → vpc.kacho.svc:9090
//	"vpcInternal"          → vpc.kacho.svc:9091 (admin internal-порт)
//	"compute"              → compute.kacho.svc:9090
//	"computeInternal"      → compute.kacho.svc:9091 (admin internal-порт)
//	"loadbalancer"         → kacho-nlb.kacho.svc:9090
//	"loadbalancerInternal" → kacho-nlb.kacho.svc:9091 (зарезервирован
//	                        под admin/internal REST, если в будущем добавятся http-аннотации;
//	                        сейчас InternalResourceLifecycleService — streaming gRPC-direct only)
//	"geo"                  → kacho-geo.kacho.svc:9090 (Region/Zone read)
//	"geoInternal"          → kacho-geo-internal.kacho.svc:9091 (admin Region/Zone CRUD)
//	"registry"             → kacho-registry.kacho.svc:9090 (RegistryService control-plane)
//	"registryInternal"     → kacho-registry.kacho.svc:9091 (InternalRegistryService GC/stats admin)
//	"storage"              → kacho-storage.kacho.svc:9090 (Volume/Snapshot/DiskType read)
//	"storageInternal"      → kacho-storage.kacho.svc:9091 (InternalVolume attach/detach + InternalDiskType admin)
//
// conns — карта domain → *grpc.ClientConn (нужна для OpsProxy);
// при nil — OperationService регистрируется через no-op Unimplemented (тесты).
//
// dialOpts — карта backend-key → transport-credentials grpc.DialOption.
// Ключи совпадают с `addrs` / `config.BackendAddrs()` (vpc/vpcInternal/compute/
// computeInternal/iam/iamInternal/loadbalancer/loadbalancerInternal). Для каждого
// backend'а REST-mux дозванивается с ЕГО per-edge creds: mTLS client-cert +
// корректный ServerName, когда mTLS на edge включен; insecure — когда нет.
//
// Per-edge creds обязательны: когда backend'ы работают в режиме
// `tls.RequireAndVerifyClientCert`, единый insecure dial обрывался бы на
// TLS-handshake → connection reset → 503. Composition-root
// (`cmd/api-gateway/main.go`) собирает dialOpts через `buildBackendDialCreds(cfg)`
// (те же per-edge creds, что gRPC-роутинг / authz-dial) — новой cert-обвязки не
// вводится.
//
// dialOpts может быть nil или не содержать ключ — тогда для этого backend'а
// используется insecure dial (dev backward-compat). enable=false на edge также
// дает insecure (creds-резолвер в main.go возвращает insecure-опцию).
func NewMux(
	ctx context.Context,
	addrs map[string]string,
	conns map[string]*grpc.ClientConn,
	dialOpts map[string]grpc.DialOption,
) (http.Handler, error) {
	// Boot-guard: таблица внутренних REST-маршрутов строится из proto-дескрипторов
	// (internal_routes.go). Пустая таблица означала бы, что дескрипторы не
	// слинкованы, и admin-поверхности молча уехали бы на публичный mux —
	// отказываемся стартовать, а не работаем без изоляции.
	if len(loadedInternalRoutes()) == 0 {
		return nil, fmt.Errorf("internal REST route table is empty: Internal*Service descriptors not linked — refusing to serve without external-listener isolation")
	}

	// JSON-marshallers — форма ответа задаётся в newPublicJSONPb /
	// newInternalJSONPb (strict_enum.go); отличаются они ТОЛЬКО `EmitUnpopulated`.
	//
	// Разбор тела: неизвестный КЛЮЧ отбрасывается, неизвестное ЗНАЧЕНИЕ
	// ПЕРЕЧИСЛЕНИЯ — отвергается. Это два разных «неизвестных», и у protojson
	// на них один флаг, поэтому второе отделено своим проходом (strict_enum.go).
	//
	// Ключ остаётся отбрасываемым (взвешено, не отложено):
	//
	//  1. СНЯТО 2026-07-28. Консоль собирала тело PATCH как «весь ответ GET +
	//     update_mask», поэтому в нём ехали `id`, `createdAt`, `status` и
	//     output-only зеркала — ничего из этого нет в Update-message, и набор
	//     ключей был не перечислим by construction. Это чинено: общий путь
	//     правки (шесть живых копий, не две) собирает тело ТОЛЬКО из полей
	//     маски, `_`-дискриминаторы снимаются рекурсивно на транспортном шве,
	//     и по всем девяти приложениям консоль больше не шлёт ни одного ключа
	//     вне контракта. Этот довод больше не действует.
	//  2. Конвенция update_mask (`api-conventions.md`) прямо предписывает:
	//     «mask пустой → immutable из тела silently игнорируются». В коде
	//     immutable-поля просто отсутствуют в Update-message, поэтому эта
	//     клауза РЕАЛИЗОВАНА именно `DiscardUnknown`. Переключение её отменяет —
	//     это продуктовое решение, а не правка края.
	//  3. Отказ ухудшил бы диагностику там, где она сейчас лучшая: пробы
	//     immutable-поля (`{"updateMask":"projectId","projectId":...}`) сегодня
	//     доходят до хендлера и получают контракт-тон
	//     `"project_id is immutable after Network.Create"`. protojson отверг бы
	//     тело раньше и сообщил бы лишь «unknown field», не отличая immutable
	//     от опечатки.
	//  4. Отказ ничего не даёт на ~половине найденного: authz-middleware
	//     оборачивает mux и отвечает `403` ДО разбора тела, поэтому в
	//     deny-кейсах тело не читается ни при каком значении флага.
	//
	// Цена оставшегося отбрасывания ключей измеряется гейтом
	// TestNewmanCollectionsSendNoUnknownRequestFields, который статически
	// сверяет КАЖДЫЙ запрос регрессионных suite'ов с контрактом RPC.
	//
	// Ни один из четырёх доводов на ЗНАЧЕНИЕ перечисления не переносился: маска
	// говорит про поля, а не про значения; диагностика на неизвестном значении
	// не «хуже», её просто не было — вызывающему отвечали `200`; и стоимость
	// была измерена (NLB-CR-VAL-INVALID-AFFINITY: `sessionAffinity` вне
	// словаря принимался, балансировщик создавался с умолчанием).
	publicMarshaler := newStrictEnumMarshaler(newPublicJSONPb())
	internalMarshaler := newStrictEnumMarshaler(newInternalJSONPb())

	publicMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, publicMarshaler),
		runtime.WithIncomingHeaderMatcher(principalHeaderMatcher),
		runtime.WithMetadata(principalMetadata),
	)
	internalMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, internalMarshaler),
		runtime.WithIncomingHeaderMatcher(principalHeaderMatcher),
		runtime.WithMetadata(principalMetadata),
	)

	// optsFor returns the dial-options for one backend-key: that backend's
	// per-edge transport credentials (mTLS client-cert + ServerName when the edge
	// is enabled, else insecure) plus the shared round-robin service-config. When
	// dialOpts has no entry for the key the dial falls back to insecure — dev
	// backward-compat.
	optsFor := func(backendKey string) []grpc.DialOption {
		transport, ok := dialOpts[backendKey]
		if !ok {
			transport = grpc.WithTransportCredentials(insecure.NewCredentials())
		}
		return []grpc.DialOption{
			transport,
			// Client-side round-robin; pair with `dns:///<headless-svc>:<port>` dial target.
			grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		}
	}

	// lbAddr обслуживает kacho-nlb (loadbalancer.v1). Внутреннего адреса
	// здесь больше нет: единственный маршрут, который его требовал, снят
	// вместе со своим потоком (#814).
	// registryAddr / registryInternalAddr обслуживают kacho-registry (registry.v1).
	var vpcAddr, vpcInternalAddr, computeAddr, computeInternalAddr, iamAddr, iamInternalAddr, lbAddr, geoAddr, geoInternalAddr, registryAddr, registryInternalAddr, storageAddr, storageInternalAddr string
	if addrs != nil {
		vpcAddr = addrs["vpc"]
		vpcInternalAddr = addrs["vpcInternal"]
		computeAddr = addrs["compute"]
		computeInternalAddr = addrs["computeInternal"]
		iamAddr = addrs["iam"]
		iamInternalAddr = addrs["iamInternal"]
		lbAddr = addrs["loadbalancer"]
		geoAddr = addrs["geo"]
		geoInternalAddr = addrs["geoInternal"]
		registryAddr = addrs["registry"]
		registryInternalAddr = addrs["registryInternal"]
		storageAddr = addrs["storage"]
		storageInternalAddr = addrs["storageInternal"]
	}

	// ПУБЛИЧНЫЙ handler регистрируется на ОБА mux'а (public + internal): диспетчер
	// по паре (метод, путь) — isInternalRoute — ниже выбирает, какой из них
	// фактически обработает конкретный запрос, и разница только в JSON-маршалинге.
	// Так нам не нужно заранее знать, какой RPC попадет на какой путь: grpc-gateway
	// сам разрулит, а мы лишь подсовываем правильный JSONPb.
	//
	// АДМИНИСТРАТИВНЫЙ handler регистрируется ТОЛЬКО на internalMux
	// (`if mux == internalMux && <домен>InternalAddr != ""`). Причин две, и вторая
	// несущая:
	//
	//  1. На publicMux эти маршруты недостижимы by construction: каждый
	//     REST-биндинг Internal*-сервиса классифицируется как внутренний той же
	//     таблицей дескрипторов (`matchesInternalRESTBinding`), поэтому диспетчер
	//     никогда не отдаёт его publicMux'у. Регистрация там была мёртвой.
	//  2. Именно это делает publicMux ТОЧНОЙ моделью «листенера, у которого
	//     административной поверхности нет». На неё опирается укрытие ниже: чтобы
	//     отказ внешнему вызывающему был неотличим от промаха, его обязан
	//     произвести ТОТ ЖЕ производитель, что и обычный промах, — а не вторая
	//     функция с похожим смыслом.
	muxes := []*runtime.ServeMux{publicMux, internalMux}

	for _, mux := range muxes {
		// --- vpc: Network + Subnet + Address + RouteTable + SecurityGroup + Gateway ---
		if err := vpcpb.RegisterNetworkServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register NetworkService: %w", err)
		}
		if err := vpcpb.RegisterSubnetServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register SubnetService: %w", err)
		}
		if err := vpcpb.RegisterAddressServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register AddressService: %w", err)
		}
		if err := vpcpb.RegisterRouteTableServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register RouteTableService: %w", err)
		}
		if err := vpcpb.RegisterSecurityGroupServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register SecurityGroupService: %w", err)
		}
		if err := vpcpb.RegisterGatewayServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register GatewayService: %w", err)
		}
		if err := vpcpb.RegisterNetworkInterfaceServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register NetworkInterfaceService: %w", err)
		}
		// CidrGroup — именованный набор префиксов, на который ссылается правило
		// группы безопасности. Публичный ресурс арендатора: инфра-полей у него
		// нет, поверхность — та же, что у сети.
		if err := vpcpb.RegisterCidrGroupServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register CidrGroupService: %w", err)
		}
		// Quota — сколько ресурсов каждого вида арендатору позволено и сколько
		// уже занято. Публичная поверхность и ТОЛЬКО чтение: величины меняет
		// администратор облака через iam.v1.InternalLimitService на внутреннем
		// слушателе. До этого сервиса вся поверхность квот была административной,
		// и арендатор, встретив отказ на пределе, не мог узнать ни своего
		// потолка, ни своего расхода — работающий предел был неотличим от сбоя.
		if err := vpcpb.RegisterQuotaServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register QuotaService: %w", err)
		}
		// AddressPool — административная поверхность на ПУБЛИЧНОМ бэкенде (ADM-1
		// S1). Регистрируется на ОБА mux'а, как любой публичный сервис.
		//
		// ЗАПРЕТ 6 НЕ СМЯГЧЁН: на внешний край выставлен `AddressPoolService`, а не
		// `InternalAddressPoolService`; предикат `HasInternalSuffix`, который ловит
		// второе, не тронут. Переехал ГЛАГОЛ, а не разрешение для `Internal.*`.
		// Закрывает не место вызова, а вызывающий без права: каждый RPC гейтится
		// `system_admin` @ `cluster` — отношением, которое подстановочный кортеж
		// `user:*` НЕ выполняет.
		//
		// Пути совпадают с внутренними НАМЕРЕННО (второго адреса у ресурса быть не
		// должно). Расщепление даёт диспетчер: снаружи обе его ветки ведут в
		// `publicMux`, поэтому виден публичный глагол; изнутри `runtime.ServeMux`
		// предваряет список, а внутренние регистрации идут ниже — поэтому виден
		// внутренний. Окно сосуществования закрывает стадия S3.
		if err := vpcpb.RegisterAddressPoolServiceHandlerFromEndpoint(ctx, mux, vpcAddr, optsFor("vpc")); err != nil {
			return nil, fmt.Errorf("register AddressPoolService: %w", err)
		}

		// --- vpc admin (AddressPool) — kacho-only, internal-port (9091) ---
		// Эти сервисы экспонируются через apiGW REST для UI/админ-tooling;
		// путь /vpc/v1/addressPools.
		if mux == internalMux && vpcInternalAddr != "" {
			if err := vpcpb.RegisterInternalAddressPoolServiceHandlerFromEndpoint(ctx, mux, vpcInternalAddr, optsFor("vpcInternal")); err != nil {
				return nil, fmt.Errorf("register InternalAddressPoolService: %w", err)
			}
			// GetNetwork → GET /vpc/v1/networks/{network_id}/internal — internal
			// projection of a Network (инфра-чувствительные поля); backs the
			// admin-UI "jsonint" tab.
			if err := vpcpb.RegisterInternalNetworkServiceHandlerFromEndpoint(ctx, mux, vpcInternalAddr, optsFor("vpcInternal")); err != nil {
				return nil, fmt.Errorf("register InternalNetworkService: %w", err)
			}
		}

		// --- compute: Instance ---
		// Block storage (Volume/Snapshot/Image/DiskType) is served by kacho-storage
		// under /storage/v1 — compute's duplicates are retired.
		// Geography (Region/Zone) обслуживается отдельным leaf-сервисом kacho-geo
		// (/geo/v1/regions, /geo/v1/zones; см. ниже), а не compute.v1.
		if err := computepb.RegisterInstanceServiceHandlerFromEndpoint(ctx, mux, computeAddr, optsFor("compute")); err != nil {
			return nil, fmt.Errorf("register compute InstanceService: %w", err)
		}
		// Quota — сколько ресурсов каждого вида арендатору позволено и сколько
		// уже занято. Публичная поверхность и ТОЛЬКО чтение: величины меняет
		// администратор облака через iam.v1.InternalLimitService на внутреннем
		// слушателе. До этого сервиса вся поверхность квот домена была
		// административной, и арендатор, встретив отказ на пределе, не мог узнать
		// ни своего потолка, ни своего расхода — работающий предел был неотличим
		// от сбоя.
		if err := computepb.RegisterQuotaServiceHandlerFromEndpoint(ctx, mux, computeAddr, optsFor("compute")); err != nil {
			return nil, fmt.Errorf("register compute QuotaService: %w", err)
		}
		// MachineTypeService — public read-only sizing catalog (GET /compute/v1/machineTypes[/{id}]);
		// cluster-viewer, parity с geo Region/Zone. Admin CRUD — InternalMachineTypeService
		// (internal-port block ниже; НЕ на external, ban #6).
		if err := computepb.RegisterMachineTypeServiceHandlerFromEndpoint(ctx, mux, computeAddr, optsFor("compute")); err != nil {
			return nil, fmt.Errorf("register compute MachineTypeService: %w", err)
		}
		// GuestAccessKeyService — публичные ключи входа в машину
		// (/compute/v1/guestAccessKeys). Только публичная половина ключа; закрытая
		// не покидает машину арендатора и в продукте не хранится.
		if err := computepb.RegisterGuestAccessKeyServiceHandlerFromEndpoint(ctx, mux, computeAddr, optsFor("compute")); err != nil {
			return nil, fmt.Errorf("register compute GuestAccessKeyService: %w", err)
		}
		// PlacementGroupService — правила взаимного размещения машин
		// (/compute/v1/placementGroups).
		if err := computepb.RegisterPlacementGroupServiceHandlerFromEndpoint(ctx, mux, computeAddr, optsFor("compute")); err != nil {
			return nil, fmt.Errorf("register compute PlacementGroupService: %w", err)
		}

		// --- compute admin — kacho-only, internal-port (9091) ---
		// Доступен только через cluster-internal REST listener для UI/admin-tooling.
		// Серверных стримов у compute больше нет: подписка на журнал изменений
		// снята вместе со своей поверхностью (#813) — подписчика у неё не было ни
		// одного дня. Прежде здесь стояла оговорка, почему этот стрим не
		// проксируется через REST; оговорка пережила бы свой предмет.
		// Admin Region/Zone обслуживает geo.v1.
		if mux == internalMux && computeInternalAddr != "" {
			// InternalMachineTypeService — admin CRUD над каталогом MachineType
			// (POST/PATCH/DELETE на /compute/v1/internal/machineTypes; async Operation,
			// system_admin). Путь несет сегмент `/internal/` → isInternalRoute 404-ит его
			// на external TLS listener, а gRPC-роутер блокирует Internal* через
			// HasInternalSuffix. Cluster-internal only (ban #6, parity с geo InternalRegion/Zone).
			if err := computepb.RegisterInternalMachineTypeServiceHandlerFromEndpoint(ctx, mux, computeInternalAddr, optsFor("computeInternal")); err != nil {
				return nil, fmt.Errorf("register compute InternalMachineTypeService: %w", err)
			}
		}

		// --- storage.v1 (kacho-storage): Volume + Snapshot + DiskType (public) ---
		// Public RPC под /storage/v1/*: volumes/snapshots CRUD (async Operation,
		// sop-prefix) + diskTypes read-only. Регистрируется условно по storageAddr —
		// backend еще может быть не задеплоен (поведение симметрично registry/geo/nlb).
		if storageAddr != "" {
			if err := storagepb.RegisterVolumeServiceHandlerFromEndpoint(ctx, mux, storageAddr, optsFor("storage")); err != nil {
				return nil, fmt.Errorf("register storage VolumeService: %w", err)
			}
			// Quota — сколько ресурсов каждого вида арендатору позволено и сколько
			// уже занято. Публичная поверхность и ТОЛЬКО чтение: величины меняет
			// администратор облака через iam.v1.InternalLimitService на внутреннем
			// слушателе. До этого сервиса вся поверхность квот домена была
			// административной, и арендатор, встретив отказ на пределе, не мог узнать
			// ни своего потолка, ни своего расхода — работающий предел был неотличим
			// от сбоя.
			if err := storagepb.RegisterQuotaServiceHandlerFromEndpoint(ctx, mux, storageAddr, optsFor("storage")); err != nil {
				return nil, fmt.Errorf("register storage QuotaService: %w", err)
			}
			if err := storagepb.RegisterSnapshotServiceHandlerFromEndpoint(ctx, mux, storageAddr, optsFor("storage")); err != nil {
				return nil, fmt.Errorf("register storage SnapshotService: %w", err)
			}
			if err := storagepb.RegisterDiskTypeServiceHandlerFromEndpoint(ctx, mux, storageAddr, optsFor("storage")); err != nil {
				return nil, fmt.Errorf("register storage DiskTypeService: %w", err)
			}
			// ImageService — public boot-image CRUD (POST/GET/PATCH/DELETE на
			// /storage/v1/images + GET .../operations; async Operation). StorageImage
			// `img`, выделен из compute Image. InternalImageService (infra-проекция) —
			// internal-port block ниже.
			if err := storagepb.RegisterImageServiceHandlerFromEndpoint(ctx, mux, storageAddr, optsFor("storage")); err != nil {
				return nil, fmt.Errorf("register storage ImageService: %w", err)
			}
		}

		// --- storage.v1 admin (InternalVolume + InternalDiskType) — kacho-only, internal-port (9091) ---
		// InternalVolumeService (Attach/Detach/ListAttachments/GetInternal) — без
		// google.api.http-аннотаций → grpc-gateway создает default unbound-route
		// POST /kacho.cloud.storage.v1.InternalVolumeService/<Method> (аналог iam
		// InternalUserService / registry InternalRegistryService). Несет placement/
		// инфра-чувствительные поля (security.md) → доступно ТОЛЬКО через
		// cluster-internal REST listener: dispatcher (isInternalRoute →
		// HasInternalSuffix) 404-ит эти пути на external TLS listener, а gRPC-роутер
		// блокирует Internal* через HasInternalSuffix. Data-plane consumer'ы могут
		// ходить напрямую gRPC до kacho-storage:9091.
		// InternalDiskTypeService (admin CRUD справочника DiskType) — POST/PATCH/DELETE
		// на /storage/v1/diskTypes (тот же collection-путь, что public read); гейтится
		// authz-каталогом (required_relation system_admin), как compute InternalDiskType.
		if mux == internalMux && storageInternalAddr != "" {
			if err := storagepb.RegisterInternalVolumeServiceHandlerFromEndpoint(ctx, mux, storageInternalAddr, optsFor("storageInternal")); err != nil {
				return nil, fmt.Errorf("register storage InternalVolumeService: %w", err)
			}
			if err := storagepb.RegisterInternalDiskTypeServiceHandlerFromEndpoint(ctx, mux, storageInternalAddr, optsFor("storageInternal")); err != nil {
				return nil, fmt.Errorf("register storage InternalDiskTypeService: %w", err)
			}
			// InternalImageService.GetInternal — full (infra) projection of an Image.
			// Без google.api.http-аннотаций → grpc-gateway создает default unbound-route
			// POST /kacho.cloud.storage.v1.InternalImageService/GetInternal (аналог
			// InternalVolumeService). Несет инфра-чувствительные поля (security.md) →
			// доступно ТОЛЬКО через cluster-internal REST listener: dispatcher
			// (isInternalRoute → HasInternalSuffix) 404-ит его на external TLS listener.
			if err := storagepb.RegisterInternalImageServiceHandlerFromEndpoint(ctx, mux, storageInternalAddr, optsFor("storageInternal")); err != nil {
				return nil, fmt.Errorf("register storage InternalImageService: %w", err)
			}
			// InternalStorageBackendService + InternalDiskTypeBindingService —
			// административная плоскость каталога хранения: где стоит кластер
			// данных и какой класс к нему привязан в какой зоне.
			//
			// Обе несут координату инфраструктуры (адрес кластера, имя пула,
			// пространство имён) и потому живут ТОЛЬКО здесь: на внешнем
			// слушателе их пути не обслуживаются вовсе — диспетчер
			// классифицирует по принадлежности RPC Internal*-сервису, а не по
			// форме пути.
			//
			// Ссылка на учётный материал (`credentials_ref`) — именно ссылка:
			// сам материал не проходит ни через этот край, ни через БД.
			if err := storagepb.RegisterInternalStorageBackendServiceHandlerFromEndpoint(ctx, mux, storageInternalAddr, optsFor("storageInternal")); err != nil {
				return nil, fmt.Errorf("register storage InternalStorageBackendService: %w", err)
			}
			if err := storagepb.RegisterInternalDiskTypeBindingServiceHandlerFromEndpoint(ctx, mux, storageInternalAddr, optsFor("storageInternal")); err != nil {
				return nil, fmt.Errorf("register storage InternalDiskTypeBindingService: %w", err)
			}
		}

		// --- geo.v1: Region + Zone (public read-only) ---
		// Geography (Region/Zone) — отдельный leaf-сервис kacho-geo.
		// RegionService/ZoneService — public read под /geo/v1/regions,
		// /geo/v1/zones. Регистрируется условно по geoAddr (graceful: kacho-geo
		// может быть еще не задеплоен — симметрично lbAddr/iamAddr выше).
		// Geography обслуживается ИСКЛЮЧИТЕЛЬНО geo.v1.
		if geoAddr != "" {
			if err := geopb.RegisterRegionServiceHandlerFromEndpoint(ctx, mux, geoAddr, optsFor("geo")); err != nil {
				return nil, fmt.Errorf("register geo RegionService: %w", err)
			}
			if err := geopb.RegisterZoneServiceHandlerFromEndpoint(ctx, mux, geoAddr, optsFor("geo")); err != nil {
				return nil, fmt.Errorf("register geo ZoneService: %w", err)
			}
		}

		// --- geo admin (InternalRegionService/InternalZoneService) — kacho-only, internal-port (9091) ---
		// Admin-CRUD справочников Region/Zone (POST/PATCH/DELETE на /geo/v1/regions,
		// /geo/v1/zones). Доступен ТОЛЬКО через cluster-internal REST listener для
		// UI/admin-tooling. На external TLS endpoint admin Region/Zone-
		// функции не светятся: gRPC-роутер блокирует Internal*-сервисы через
		// HasInternalSuffix, а authz-каталог гейтит эти RPC relation `system_admin`
		// на cluster-singleton. Мутации Region/Zone — это catalog-паттерн (sync-ответ
		// ресурсом, НЕ Operation; как InternalDiskType).
		if mux == internalMux && geoInternalAddr != "" {
			if err := geopb.RegisterInternalRegionServiceHandlerFromEndpoint(ctx, mux, geoInternalAddr, optsFor("geoInternal")); err != nil {
				return nil, fmt.Errorf("register geo InternalRegionService: %w", err)
			}
			if err := geopb.RegisterInternalZoneServiceHandlerFromEndpoint(ctx, mux, geoInternalAddr, optsFor("geoInternal")); err != nil {
				return nil, fmt.Errorf("register geo InternalZoneService: %w", err)
			}
		}

		// --- iam.v1: Account + Project + User (read+delete only) + ServiceAccount + Group + Role + AccessBinding ---
		// Public surface: все 7 сервисов под /iam/v1/*.
		// User не имеет публичного Create — User'ы создаются через
		// InternalUserService.UpsertFromIdentity (OIDC-callback в api-gateway);
		// display_name/email берётся от поставщика личности при следующем UpsertFromIdentity.
		if iamAddr != "" {
			// Квоты личности — сколько аккаунтов вызывающему позволено и сколько
			// уже занято. Публичная поверхность и ТОЛЬКО чтение о себе: величину
			// меняет администратор облака через iam.v1.InternalLimitService на
			// внутреннем слушателе. Обслуживает её kacho-iam, поэтому адрес тот же.
			if err := quotapb.RegisterIdentityQuotaServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam IdentityQuotaService: %w", err)
			}
			if err := iampb.RegisterAccountServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam AccountService: %w", err)
			}
			if err := iampb.RegisterProjectServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam ProjectService: %w", err)
			}
			if err := iampb.RegisterUserServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam UserService: %w", err)
			}
			if err := iampb.RegisterServiceAccountServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam ServiceAccountService: %w", err)
			}
			if err := iampb.RegisterGroupServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam GroupService: %w", err)
			}
			if err := iampb.RegisterRoleServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam RoleService: %w", err)
			}
			// MembershipService — чтение принадлежности человека аккаунту под
			// `/iam/v1/accounts/{accountId}/memberships[/{membershipId}]`.
			//
			// Регистрируется на ПУБЛИЧНОМ бэкенде, и это несущее свойство, а не
			// удобство: единственный гейт этих чтений — пообъектная проверка
			// ЭТОГО края по `viewer` @ `account` из пути. На cluster-internal
			// бэкенде края нет, поэтому туда служба не едет вовсе.
			if err := iampb.RegisterMembershipServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam MembershipService: %w", err)
			}
			// PermissionCatalogService.ListPermissionCatalog — public read under
			// GET /iam/v1/permissionCatalog: an authenticated-floor read (<exempt>
			// in the permission catalog — no FGA Check) that the UI calls to build its
			// role/permission palette. PUBLIC (external) on purpose, NOT an Internal*
			// service — registered here in the public iam block, not the iamInternalAddr
			// block; the gRPC-router allowlists it for the external listener.
			if err := iampb.RegisterPermissionCatalogServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam PermissionCatalogService: %w", err)
			}
			if err := iampb.RegisterAccessBindingServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam AccessBindingService: %w", err)
			}
			// LimitService — административная поверхность пределов на ПУБЛИЧНОМ
			// бэкенде (ADM-1 S1, #878). Пять глаголов управления величиной под
			// `/iam/v1/limits`, каждый гейтится `system_admin` @ `cluster`.
			//
			// ЗАПРЕТ 6 НЕ СМЯГЧЁН: наружу выставлен публичный `LimitService`, а не
			// `InternalLimitService`; предикат `HasInternalSuffix`, который ловит
			// второе, не тронут. Переезжает ГЛАГОЛ, а не разрешение для внутреннего
			// сервиса.
			//
			// ЧТО ЭТО ЧИНИТ. Величину назначает администратор облака, и назначает он
			// её через край. Пока глагол жил только внутренним, страница пределов
			// консоли получала 404 — отказ, неотличимый от «такого раздела нет
			// вовсе», при полностью исправном сервисе. Теперь отказ честен: 403 у
			// того, кому не положено, 200 у администратора.
			//
			// ДВА АДРЕСА ДО S3. Внутренний путь несёт сегмент `/internal/`, поэтому
			// публичный не совпадает с ним дословно — в отличие от пула адресов, где
			// оба глагола объявляли ОДИН путь и согласие их записей стерёг
			// `TestSharedRestPair_CannotChangeAnAccessDecision`. Здесь пары нет, и
			// согласие решения держит `TestLimits_AdminSurfaceIsReachableFromOutside`
			// — тем же предикатом `accessDecisionDiffers`, а не своей копией.
			if err := iampb.RegisterLimitServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam LimitService: %w", err)
			}
			// SAKeyService (ServiceAccount OAuth keys). Public under
			// /iam/v1/serviceAccounts/{id}/keys. Без этой регистрации grpc-gateway
			// не имеет REST-route → POST .../keys → 404, и SAKeyService.Issue/Revoke
			// недоступны.
			if err := iampb.RegisterSAKeyServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam SAKeyService: %w", err)
			}
			// UserTokenService (персональные API-токены пользователя). Public под
			// /iam/v1/users/{user_id}/tokens. Зеркалит SAKeyService: Issue/Revoke —
			// async Operation, List — sync. Без этой регистрации grpc-gateway не
			// имеет REST-route → POST .../tokens → 404.
			if err := iampb.RegisterUserTokenServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam UserTokenService: %w", err)
			}
			// AuthorizeService — tenant FGA check (POST /iam/v1/authorize:check).
			if err := iampb.RegisterAuthorizeServiceHandlerFromEndpoint(ctx, mux, iamAddr, optsFor("iam")); err != nil {
				return nil, fmt.Errorf("register iam AuthorizeService: %w", err)
			}
		}

		// --- iam.v1 admin (InternalUserService + InternalIAMService) —
		// kacho-only, internal-port (9091) ---
		// REST HTTP annotations on internal IAM proto RPCs (UpsertFromIdentity,
		// LookupSubject, ListPermissions, Check) make grpc-gateway create routes
		// for /iam/v1/internal/* paths.
		// These handlers are dispatched to the internal mux (isInternalRoute
		// returns true for any path containing /internal/); the authz middleware
		// lets them through via the public allowlist (no Bearer JWT required —
		// the IAM service enforces its own per-handler auth via authzguard
		// interceptor whitelist). External TLS listener never serves these
		// paths — the gRPC router's HasInternalSuffix blocks Internal* services
		// on the public listener.
		if mux == internalMux && iamInternalAddr != "" {
			if err := iampb.RegisterInternalUserServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalUserService: %w", err)
			}
			if err := iampb.RegisterInternalIAMServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalIAMService: %w", err)
			}
			// InternalClusterService — cluster-admin RBAC management
			// (Get / GrantAdmin / RevokeAdmin / ListAdmins) under
			// /iam/v1/internal/cluster/...  Internal-only;
			// isInternalRoute sends these paths to the internal sub-mux. Catalog gate
			// (`required_relation: admin`) enforces the FGA computed-alias
			// `system_admin OR emergency_admin` on `cluster:cluster_kacho_root`.
			if err := iampb.RegisterInternalClusterServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalClusterService: %w", err)
			}
			// InternalInteractiveClientService — lifecycle of the OAuth2 client
			// through which a HUMAN completes an interactive sign-in ceremony
			// (Get / List / Create / Update / Delete) under
			// /iam/v1/internal/interactiveClients.  Internal-only;
			// isInternalRoute sends these paths to the internal sub-mux and the
			// dispatcher 404s them on the external TLS listener, hiding
			// existence. Registering a client at the identity provider decides
			// where an authorization code may be delivered, so the catalog gate
			// requires `system_admin` on `cluster:cluster_kacho_root` and the
			// three mutations additionally carry the step-up floor acr=2.
			if err := iampb.RegisterInternalInteractiveClientServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalInteractiveClientService: %w", err)
			}
			// InternalLimitService — resource-count ceilings (issue #291):
			// Get / List / Create / Update / Delete plus the two owner-facing
			// reads Resolve / ListChangedSince, under /iam/v1/internal/limits.
			// Internal-only; isInternalRoute sends these paths to the internal
			// sub-mux and the dispatcher 404s them on the external TLS listener,
			// hiding existence. The five CRUD verbs are catalog-gated on
			// `system_admin` @ cluster:cluster_kacho_root (mutations additionally
			// at acr=2); the two reads carry the NARROW `quota_reader` relation,
			// because an owner service must not need the whole cluster read tier
			// to learn its tenant's ceiling.
			if err := iampb.RegisterInternalLimitServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalLimitService: %w", err)
			}
			// InternalOperationsService.ListIamOperations — cluster-wide IAM
			// operations dump for admin-UI under GET /iam/v1/internal/operations.
			// Internal-only; isInternalRoute routes /iam/v1/internal/* to
			// the internal sub-mux and the dispatcher 404s it on the external TLS
			// listener. The gRPC router's HasInternalSuffix also blocks the
			// InternalOperationsService suffix on the public listener.
			// admin-tier is enforced by the permission-catalog entry
			// (required_relation: system_admin, scope cluster:cluster_kacho_root, acr 2),
			// parity with InternalClusterService/*; the iam :9091 backend additionally
			// runs its own per-RPC authz-Check.
			if err := iampb.RegisterInternalOperationsServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalOperationsService: %w", err)
			}
			// InternalModuleService — Plan/Apply/Get/List над каталогом прав модуля
			// под /iam/v1/internal/modules (kacho#1991). Тот же префикс и тот же
			// гейт, что у InternalClusterService: system_admin на singleton-объекте
			// `cluster`, а у Apply вдобавок ступень подтверждения личности (acr 2).
			//
			// Здесь маршрута не было ВОВСЕ, и это было записанным решением
			// под-фазы: контракт заводился без http-аннотаций, чтобы таблица
			// маршрутов не сдвинулась вместе с ним. Глаголы были достижимы по
			// gRPC на внутреннем слушателе, но привычный оператору путь — REST
			// через этот мукс, как у всех соседних Internal*-служб.
			//
			// Плоскость от маршрута не меняется: приставка `Internal` держит
			// глаголы вне внешнего маршрутизатора by construction — allowlist
			// края считается из дескрипторов и утверждает
			// `AllowedMethods ∩ Internal* = ∅`, а isInternalRoute уводит
			// /iam/v1/internal/* на этот sub-mux. Обе стороны утверждаются
			// вычисляемыми гейтами (internal_binding_routability_test.go —
			// маршрут есть здесь; external_isolation_test.go — его нет там),
			// поэтому односторонним зелёным это закрыть нельзя.
			if err := iampb.RegisterInternalModuleServiceHandlerFromEndpoint(ctx, mux, iamInternalAddr, optsFor("iamInternal")); err != nil {
				return nil, fmt.Errorf("register iam InternalModuleService: %w", err)
			}
			// InternalBootstrapTokenService.MintBootstrapToken (#58) is DELIBERATELY
			// NOT registered here — do not add it back.
			//
			// It mints a Hydra-signed RS256 Bearer for a cluster `system_admin`
			// ServiceAccount, and its catalog permission is `<exempt>`. The authz
			// middleware admits an `<exempt>` Internal* RPC that arrives on the
			// cluster-internal listener WITHOUT extracting a principal
			// (phaseInternalOriginExempt), and this internal REST listener is plain
			// HTTP/1.1 — no TLS, no client cert (cmd/api-gateway/main.go). A REST route
			// would therefore be a CREDENTIAL-FREE control-plane takeover: any pod that
			// can reach the `internal-rest` Service port, or anyone holding a
			// port-forward, could POST an empty body and get a cluster-admin token.
			// Network position is not a credential (security.md — "internal = trusted"
			// is a forbidden assumption).
			//
			// The mint keeps exactly ONE door: a direct mTLS gRPC dial to kacho-iam
			// :9091, where authzguard.CallerPolicy checks the caller's verified
			// client-certificate SAN against an explicit allow-list
			// (`authn.bootstrap-mint.allowed-client-sans`). The proto carries no
			// `google.api.http` binding, and the grpc-gateway default unbound route is
			// unreachable because the handler is never registered on this mux.
			// Locked by restmux/bootstrap_token_no_rest_route_test.go.
		}

		// --- loadbalancer.v1 (kacho-nlb): NetworkLoadBalancer + Listener + TargetGroup ---
		// Public RPC под /nlb/v1/*. Регистрируется условно по lbAddr — backend еще
		// может быть не задеплоен в окружении (поведение симметрично vpcInternalAddr /
		// computeInternalAddr / iamAddr выше).
		if lbAddr != "" {
			if err := lbpb.RegisterNetworkLoadBalancerServiceHandlerFromEndpoint(ctx, mux, lbAddr, optsFor("loadbalancer")); err != nil {
				return nil, fmt.Errorf("register loadbalancer NetworkLoadBalancerService: %w", err)
			}
			// Quota — сколько ресурсов каждого вида арендатору позволено и сколько
			// уже занято. Публичная поверхность и ТОЛЬКО чтение: величины меняет
			// администратор облака через iam.v1.InternalLimitService на внутреннем
			// слушателе. До этого сервиса вся поверхность квот домена была
			// административной, и арендатор, встретив отказ на пределе, не мог узнать
			// ни своего потолка, ни своего расхода — работающий предел был неотличим
			// от сбоя.
			if err := lbpb.RegisterQuotaServiceHandlerFromEndpoint(ctx, mux, lbAddr, optsFor("loadbalancer")); err != nil {
				return nil, fmt.Errorf("register loadbalancer QuotaService: %w", err)
			}
			if err := lbpb.RegisterListenerServiceHandlerFromEndpoint(ctx, mux, lbAddr, optsFor("loadbalancer")); err != nil {
				return nil, fmt.Errorf("register loadbalancer ListenerService: %w", err)
			}
			if err := lbpb.RegisterTargetGroupServiceHandlerFromEndpoint(ctx, mux, lbAddr, optsFor("loadbalancer")); err != nil {
				return nil, fmt.Errorf("register loadbalancer TargetGroupService: %w", err)
			}
		}

		// --- loadbalancer.v1 admin (InternalResourceLifecycleService) — kacho-only, internal-port (9091) ---
		// InternalResourceLifecycleService.Subscribe — gRPC server-streaming для
		// подписки на CREATED/UPDATED/DELETED события (outbox).
		// В proto НЕТ `option (google.api.http)`, поэтому grpc-gateway не создает REST-routes —
		// consumer'ы (наблюдатели data-plane) дозваниваются по gRPC напрямую до
		// loadbalancer.kacho.svc:9091 через grpc-client. Регистрация здесь — pro-forma
		// reference (симметрично iam InternalUserService); если в будущем добавятся
		// http-аннотации, REST автоматически появится на internal mux.
		// HasInternalSuffix в gRPC-роутере (server.go Resolver / shimproxy.go)
		// блокирует попадание InternalResourceLifecycleService.* на external/TLS
		// endpoint.

		// --- registry.v1 (kacho-registry): RegistryService ---
		// Public control-plane реестра под /registry/v1/*: registries CRUD +
		// per-repo проекции (repositories/tags/DeleteTag). Регистрируется условно
		// по registryAddr — backend еще может быть не задеплоен (поведение
		// симметрично lbAddr / geoAddr выше). Data-plane OCI v2 (/v2/*) — отдельный
		// ingress, НЕ через api-gateway.
		if registryAddr != "" {
			// Quota — сколько ресурсов каждого вида арендатору позволено и сколько
			// уже занято. Публичная поверхность и ТОЛЬКО чтение: величины меняет
			// администратор облака через iam.v1.InternalLimitService на внутреннем
			// слушателе. До этого сервиса вся поверхность квот домена была
			// административной, и арендатор, встретив отказ на пределе, не мог узнать
			// ни своего потолка, ни своего расхода — работающий предел был неотличим
			// от сбоя.
			if err := registrypb.RegisterQuotaServiceHandlerFromEndpoint(ctx, mux, registryAddr, optsFor("registry")); err != nil {
				return nil, fmt.Errorf("register registry QuotaService: %w", err)
			}
			if err := registrypb.RegisterRegistryServiceHandlerFromEndpoint(ctx, mux, registryAddr, optsFor("registry")); err != nil {
				return nil, fmt.Errorf("register registry RegistryService: %w", err)
			}
		}

		// --- registry.v1 admin (InternalRegistryService) — kacho-only, internal-port (9091) ---
		// TriggerGarbageCollection (GC zot-стора) + GetRegistryStats (инфра-проекция
		// namespace: blob/размер — security.md). В proto НЕТ `google.api.http`, поэтому
		// grpc-gateway создает default unbound-route
		// POST /kacho.cloud.registry.v1.InternalRegistryService/<Method> (аналог iam
		// InternalUserService.Get). Доступно ТОЛЬКО через cluster-internal REST listener:
		// dispatcher (isInternalRoute → HasInternalSuffix) 404-ит эти пути на external
		// TLS listener, а gRPC-роутер блокирует Internal* через HasInternalSuffix.
		// Admin-tooling может ходить и напрямую gRPC до kacho-registry:9091.
		if mux == internalMux && registryInternalAddr != "" {
			if err := registrypb.RegisterInternalRegistryServiceHandlerFromEndpoint(ctx, mux, registryInternalAddr, optsFor("registryInternal")); err != nil {
				return nil, fmt.Errorf("register registry InternalRegistryService: %w", err)
			}
		}

		// --- OperationService (OpsProxy, in-process) ---
		// Не имеет отдельного backend — живет in-process как OpsProxy.
		// Регистрируем через RegisterOperationServiceHandlerServer (локальный вызов, без dial).
		var opsSrv operationpb.OperationServiceServer
		if conns != nil {
			opsSrv = opsproxy.New(conns)
		} else {
			opsSrv = operationpb.UnimplementedOperationServiceServer{}
		}
		if err := operationpb.RegisterOperationServiceHandlerServer(ctx, mux, opsSrv); err != nil {
			return nil, fmt.Errorf("register OperationService: %w", err)
		}
	}

	// Диспетчер по паре (HTTP-метод, путь). Решает, какому sub-mux'у скормить запрос. Сами
	// RPC-роуты внутри grpc-gateway-mux'ов идентичны — отличается только JSON
	// маршалинг ответа (EmitUnpopulated). Запрос НЕ переадресуется куда-то еще:
	// internal sub-mux обработает request тем же handler'ом, что и public, но
	// сожмет response пустых полей.
	dispatcher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isInternalRoute(r.Method, r.URL.Path) {
			// SECURITY: Internal* REST paths are cluster-internal-only.
			// When the request arrived on the advertised external TLS listener
			// (listenerorigin.IsExternal), the caller must get exactly what this
			// listener answers for a route it does not have — mirroring the gRPC
			// router, where the hidden method and a method that does not exist are
			// produced by one function (proxy.refuseRoute) and are therefore
			// byte-identical.
			//
			// The answer is produced by SERVING THE REQUEST ON publicMux, which
			// carries the public routes and nothing else. So grpc-gateway itself
			// decides the shape, exactly as it would if the admin surface had never
			// been registered: 404 where the path is unknown, 501 Method Not Allowed
			// where the path exists publicly under other methods (admin CRUD of the
			// DiskType catalogue shares its collection path with the public read).
			//
			// It must NOT be an http.NotFound written here. That was a SECOND
			// producer, and the two answers differed in Content-Type, in body and in
			// X-Content-Type-Options — so the shape of a 404 answered the question
			// «is there an admin path here», and the whole admin surface was
			// enumerable from outside without any credential.
			//
			// Internal-listener callers (UI / admin-tooling / port-forward / service
			// self-calls) are unmarked → served by internalMux as before.
			if listenerorigin.IsExternal(r.Context()) {
				publicMux.ServeHTTP(w, r)
				return
			}
			internalMux.ServeHTTP(w, r)
			return
		}
		publicMux.ServeHTTP(w, r)
	})

	return dispatcher, nil
}
