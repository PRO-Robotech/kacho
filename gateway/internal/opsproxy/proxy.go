// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package opsproxy реализует OpsProxy — фасад OperationService для api-gateway.
//
// Operation.id имеет 3-символьный domain-prefix (конвенция Kachō): "enp…" → vpc,
// "epd…" → compute, и т.д. OpsProxy парсит prefix → выбирает нужный
// backend-клиент → проксирует запрос. Клиент видит единый endpoint /operations/*.
//
// Маппинг префикса на backend:
//
//	"enp" → vpc               (операции по Network / RouteTable / SecurityGroup)
//	"e9b" → vpc               (операции по Subnet / Address)
//	"epd" → compute           (ВСЕ операции compute-домена: Instance/MachineType —
//	                           PrefixOperationCompute == PrefixInstance, см. kacho-corelib/ids.
//	                           Блочное хранение здесь БОЛЬШЕ НЕ значится: Volume/Snapshot/Image/
//	                           DiskType принадлежат kacho-storage и несут собственный op-префикс)
//	"iop" → iam               (ВСЕ операции iam-домена: Account/Project/User/SA/Group/Role/AccessBinding)
//	"nlb" → loadbalancer      (ВСЕ операции kacho-nlb: NetworkLoadBalancer/Listener/TargetGroup)
//	"rop" → registry          (ВСЕ операции kacho-registry: Registry/DeleteTag)
//	"sop" → storage           (ВСЕ операции kacho-storage: Volume/Snapshot)
//	"geo" → geo               (ВСЕ операции kacho-geo: Region/Zone admin CRUD)
//
// Префикс заведомо стабильный: ровно 3 символа, lowercase crockford-base32-friendly.
// Тело id (17 символов) — непрозрачно для proxy.
//
// Legacy-префиксы вида "<service>_<uuid>" принимаются на чтение для
// backward-compat (id могут еще лежать в БД после переходного периода) —
// см. legacyPrefix fallback ниже.
package opsproxy

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// Operation-id prefixes without an exported kacho-corelib constant reachable
// from the gateway. Pinned here as named constants (not bare map literals) and
// guarded by TestPrefixToBackend_* so a divergence is a test failure, giving the
// routing table a single named source of truth per prefix:
//
//   - prefixOperationVPCSubnet ("e9b"): vpc's secondary op-prefix
//     (Subnet/Address). It exists only as a validate-package literal in
//     kacho-vpc — no exported ids.* constant yet.
//   - prefixOperationIAM ("iop"): mirrors kaname domain.PrefixOperationIAM;
//     the gateway must not import kaname internal packages, so it is pinned
//     here.
//   - prefixOperationGeo ("geo"): mirrors kacho-geo lro.OperationPrefix; geo has
//     no exported ids.PrefixOperation* constant (its op-prefix lives as an
//     internal lro-package literal), so it is pinned here. Without this entry
//     every geo admin CRUD Operation.Get/Cancel routes to InvalidArgument and
//     geo internal-admin LROs are unpollable through the gateway.
const (
	prefixOperationVPCSubnet = "e9b"
	prefixOperationIAM       = "iop"
	prefixOperationGeo       = "geo"
)

// backendCallTimeout bounds every OperationService.Get/Cancel call OpsProxy
// makes to a backend. These are fast unary reads, so a short deadline matches
// the sibling unary clients (IAMSubjectClient uses 3-5s). Without it the raw
// request ctx carries no deadline and a wedged backend (half-open TCP, GC
// pause, overload) pins the gateway handler goroutine + HTTP/2 stream
// indefinitely — the exact hazard the "per-call deadline на КАЖДОМ внешнем
// вызове" invariant (architecture.md) guards against.
const backendCallTimeout = 5 * time.Second

// prefixToBackend — карта 3-символьного Operation-id префикса в имя
// backend-домена. Ключи биндятся на exported kacho-corelib константы
// (ids.PrefixOperation*) там, где они есть — единый источник истины: изменение
// префикса в corelib автоматически меняет здесь ключ (а TestPrefixToBackend_*
// ловит расхождение состава). Префиксы без corelib-константы (e9b/iop) — именные
// локальные консты выше.
var prefixToBackend = map[string]string{
	// vpc domain
	ids.PrefixOperationVPC:   "vpc", // enp: Network / RouteTable / SecurityGroup / vpc op-root
	prefixOperationVPCSubnet: "vpc", // e9b: Subnet / Address
	// compute domain
	ids.PrefixOperationCompute: "compute", // epd: все операции compute (Instance/MachineType — общий op-prefix)
	// iam domain
	prefixOperationIAM: "iam", // iop: все операции iam (Account/Project/User/SA/Group/Role/AccessBinding — общий op-prefix)
	// loadbalancer domain
	ids.PrefixOperationNLB: "loadbalancer", // nlb: все операции kacho-nlb (NetworkLoadBalancer/Listener/TargetGroup — общий op-prefix)
	// registry domain
	ids.PrefixOperationReg: "registry", // rop: все операции kacho-registry (Registry/DeleteTag)
	// storage domain
	ids.PrefixOperationStorage: "storage", // sop: все операции kacho-storage (Volume/Snapshot — общий op-prefix, декаплен от ресурса)
	// geo domain
	prefixOperationGeo: "geo", // geo: все операции kacho-geo (Region/Zone admin CRUD — lro.OperationPrefix)
}

// legacyPrefixToBackend — старые «<service>_<uuid>» Operation.id, все еще
// допустимые на чтение (например, если они закешированы в долгоживущих
// клиентах). Не используется при создании новых операций.
var legacyPrefixToBackend = map[string]string{
	"vpc": "vpc",
}

// OpsProxy реализует operationpb.OperationServiceServer, проксируя запросы
// к конкретному backend на основе domain-prefix в Operation.id.
type OpsProxy struct {
	operationpb.UnimplementedOperationServiceServer
	// backends — карта domain → OperationServiceClient данного backend.
	backends map[string]operationpb.OperationServiceClient
}

// New создает OpsProxy из карты долгоживущих ClientConn-ов.
// conns — карта domain → *grpc.ClientConn (те же соединения, что и proxy.Backends).
func New(conns map[string]*grpc.ClientConn) *OpsProxy {
	clients := make(map[string]operationpb.OperationServiceClient, len(conns))
	for domain, conn := range conns {
		clients[domain] = operationpb.NewOperationServiceClient(conn)
	}
	return &OpsProxy{backends: clients}
}

// resolveBackend парсит domain-prefix из Operation.id и возвращает либо клиент
// нужного backend, либо gRPC-ошибку:
//
//   - 20-символьный id с известным kacho-prefix → роутим в backend; его NotFound
//     пробрасываем как есть.
//   - 20-символьный id с известным kacho-prefix, но backend не подключен (defensively;
//     в prod не должно случаться) → NotFound «нет такой операции» (операции тут нет).
//     Текст берётся у ОБЩЕГО производителя `operations.NotFoundStatus` — того же,
//     которым отвечает владелец: обе полосы приходят клиенту на один адрес, и
//     различие хоть в один байт отличало бы «нет доступа» от «не существует» и
//     называло бы, какие backend'ы подключены (`security.md` §Hardening #6).
//     Своя запись этого текста здесь была, и она разошлась с владельцем
//     регистром одной буквы (#1370).
//   - legacy "<prefix>_<uuid>" с известным legacy-prefix → роутим.
//   - все остальное (malformed, неизвестный prefix) → InvalidArgument
//     "invalid operation id <X>" — валидные operation-id у Kachō имеют только
//     известные domain-префиксы (enp…/e9b…/epd…/iop…/nlb…/rop…/sop…/geo…) и legacy-формы.
func (p *OpsProxy) resolveBackend(id string) (operationpb.OperationServiceClient, error) {
	invalid := status.Errorf(codes.InvalidArgument, "invalid operation id %q", id)
	notFound := operations.NotFoundStatus(id)

	var domain string
	switch {
	case len(id) == 20:
		d, ok := prefixToBackend[id[:3]]
		if !ok {
			return nil, invalid
		}
		domain = d
	default:
		i := strings.Index(id, "_")
		if i <= 0 {
			return nil, invalid
		}
		d, ok := legacyPrefixToBackend[id[:i]]
		if !ok {
			return nil, invalid
		}
		domain = d
	}

	client, ok := p.backends[domain]
	if !ok {
		// id синтаксически валиден, но соответствующий backend не подключен —
		// для клиента это «такой операции тут нет».
		return nil, notFound
	}
	return client, nil
}

// Get проксирует OperationService.Get к нужному backend по prefix id.
// После получения операции проверяет право доступа вызывающего principal'а:
// только создавший операцию (principal_type + principal_id из Operation) может
// ее читать. Исключение — внутренний system/bootstrap caller (воркеры,
// cross-service реконсайл), которому разрешено читать любую операцию.
// Incoming metadata (x-kacho-principal-* set by restmux WithMetadata) должна
// доходить до backend через outgoing-ctx — иначе backend видит анонимный
// principal и его per-RPC authz возвращает NotFound/PermissionDenied. Pattern
// такой же как в server.go (Resolver) / shimproxy.go.
func (p *OpsProxy) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	client, err := p.resolveBackend(req.OperationId)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(principalmeta.OutgoingFromIncoming(ctx), backendCallTimeout)
	defer cancel()
	op, err := client.Get(callCtx, req)
	if err != nil {
		return nil, err
	}
	if err := checkOperationOwnership(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}

// Cancel проксирует OperationService.Cancel к нужному backend по prefix id.
// То же ownership-check что и Get — только создавший операцию может ее
// отменить, и те же требования по metadata propagation что и для Get.
//
// Порядок здесь несущий: отмена — МУТАЦИЯ, и она терминальна. Решение о доступе
// принимается ДО отправки Cancel владельцу (read → check → mutate), потому что
// проверка, выполненная по ответу мутации, отказать уже не может: строка
// переведена в CANCELLED, а вызывающий видит отказ на применённое действие.
// Backend'ы держат собственный ownership-предикат в SQL WHERE (CancelOwned) и
// остаются авторитетом; эта проверка — второй слой на краю, и она обязана быть
// расположена так, чтобы её отказ что-то предотвращал.
func (p *OpsProxy) Cancel(ctx context.Context, req *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	client, err := p.resolveBackend(req.OperationId)
	if err != nil {
		return nil, err
	}
	readCtx, readCancel := context.WithTimeout(principalmeta.OutgoingFromIncoming(ctx), backendCallTimeout)
	defer readCancel()
	existing, err := client.Get(readCtx, &operationpb.GetOperationRequest{OperationId: req.OperationId})
	if err != nil {
		return nil, err
	}
	if err := checkOperationOwnership(ctx, existing); err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(principalmeta.OutgoingFromIncoming(ctx), backendCallTimeout)
	defer cancel()
	op, err := client.Cancel(callCtx, req)
	if err != nil {
		return nil, err
	}
	// Пост-проверка остаётся: владелец в ответе обязан быть тем же, кого мы
	// авторизовали (страховка от подмены строки между двумя вызовами).
	if err := checkOperationOwnership(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}

// checkOperationOwnership — вправе ли вызывающий видеть и отменять эту операцию.
//
// РЕШЕНИЕ ЗДЕСЬ НЕ ПРИНИМАЕТСЯ. Край подаёт две личности — вызывающего и
// записанную в строке — санкционированному глаголу владельца полосы
// (`operations.CheckRecordedOwnership`) и возвращает его ответ. Там же
// производится и отказ.
//
// # Почему решение живёт не здесь
//
// Полоса владения по ПРОЧИТАННОЙ строке — вторая в дереве (первая у владельца:
// ключ из ctx в SQL `WHERE`). Пока правила были перечислены здесь, край был
// вторым ИСТОЧНИКОМ решения: он перечислял их сам, включая имя внутренней
// личности голыми строками — дважды, в двух соседних условиях. Сойтись двум
// перечислениям нечем, они не собираются вместе и друг друга не читают, и
// расхождение видит только тот, кто сравнит копии. Правила, отличия обеих полос
// и порядок условий описаны в доме — `pkg/operations`, и здесь намеренно НЕ
// пересказываются: два места об одном предмете расходятся молча (задача
// продукта #1399).
//
// # Что остаётся ответственностью КРАЯ
//
// Не решение, а ВХОД: чью личность считать личностью вызывающего (метаданные
// входящего запроса) и какую строку считать проверяемой. Нулевая строка —
// законный вход: геттеры контракта на ней отдают пустую пару, а «неизвестно
// чья» операция арендатору не читается — это решает дом, а не отдельный `if`
// здесь.
//
// Проверка остаётся ВТОРЫМ СЛОЕМ: авторитет — владелец, чей предикат стоит в
// SQL `WHERE` и применяется раньше. Радиус этой проверки и почему она
// расположена именно так — `gateway/docs/engineering/architecture/operations-proxy-ownership.md`.
func checkOperationOwnership(ctx context.Context, op *operationpb.Operation) error {
	callerID, callerType := principalFromContext(ctx)
	return operations.CheckRecordedOwnership(
		operations.Principal{Type: callerType, ID: callerID},
		operations.Principal{Type: op.GetPrincipalType(), ID: op.GetPrincipalId()},
	)
}

// principalFromContext извлекает (id, type) calling principal из incoming
// gRPC metadata (установленных grpc-gateway через WithMetadata callback или
// gRPC-auth-interceptor).
func principalFromContext(ctx context.Context) (id, ptype string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	if v := md.Get(principalmeta.MetaPrincipalID); len(v) > 0 {
		id = v[0]
	}
	if v := md.Get(principalmeta.MetaPrincipalType); len(v) > 0 {
		ptype = v[0]
	}
	return id, ptype
}
