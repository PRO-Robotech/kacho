// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/protobuf/types/known/anypb"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Polling параметры для in-flight VPC operations (Create/Delete Address).
// kacho-vpc control-plane операции завершаются за ~1с (no real data-plane).
const (
	vpcOpPollInterval = 50 * time.Millisecond
	vpcOpPollTimeout  = 15 * time.Second
)

// Бюджет OWN-LANE read-your-writes ретрая: адрес уже закоммичен в vpc, но его
// per-object owner-tuple материализуется eventually-consistent (outbox → drainer →
// iam → FGA), поэтому непосредственно следующий SetAddressReference / Delete по
// СВОЕМУ адресу может кратко получить hide-existence NOT_FOUND / PERMISSION_DENIED.
// 12 × 500ms ≈ 6s покрывает штатное окно и остаётся заметно внутри
// vpcOpPollTimeout; бюджет ОГРАНИЧЕН (fail-open в честный UNAVAILABLE), это
// клиентский ретрай, а не серверный confirm-барьер (ban #9).
const (
	defaultAddressVisibilityRetries  = 12
	defaultAddressVisibilityInterval = 500 * time.Millisecond
)

// DefaultInternalAddressCallTimeout — per-call deadline применяемый к КАЖДОМу
// outbound gRPC-вызову этого client'а (Create/Delete/Get на AddressService,
// Set/ClearAddressReference на InternalAddressService, Operation.Get на poll-
// итерации), когда client построен без явного timeout'а. Тот же класс
// проблемы, что DefaultAddressGetTimeout (address_client.go): без него
// зависший (не отвечающий, не Unavailable) vpc-peer парковал бы вызывающую
// горутину навсегда — в частности free_ip_runner.reconcileOne держит
// FOR UPDATE SKIP LOCKED row-lock + tx через ровно эти вызовы (round-6 audit
// finding: leak/MEDIUM free_ip_runner + sibling finding 2). Каждый sibling-
// метод обязан применять один и тот же configured-timeout (architecture.md).
const DefaultInternalAddressCallTimeout = 5 * time.Second

// AllocateExternalIPRequest — параметры аллокации внешнего VIP.
type AllocateExternalIPRequest struct {
	ProjectID string // folder owning the Address row
	Name      string // resource name (unique within ProjectID; suffix runId in tests)
	// ZoneID — зона, из пула которой берётся IP. ПУСТО = anycast: адрес
	// зоне-независим и резолвится из zone-independent пула. Для VIP
	// REGIONAL-балансировщика (а EXTERNAL всегда REGIONAL) это ЕДИНСТВЕННАЯ
	// допустимая форма.
	ZoneID string
	Owner  AddressOwner
}

// AllocateInternalIPRequest — параметры аллокации внутреннего VIP под Listener.
type AllocateInternalIPRequest struct {
	ProjectID string
	Name      string
	SubnetID  string // обязательный scope для internal allocation
	Owner     AddressOwner
}

// AllocateResponse — результат аллокации IP (auto-alloc флоу).
type AllocateResponse struct {
	AddressID string
	Value     string // resolved IP в строковой форме
	PoolID    string // pool_id для external (пусто для internal)
}

// AttachExistingRequest — параметры link-привязки принесённого tenant'ом Address к
// owner-ресурсу (LoadBalancer VIP). Server-side привязка идёт через
// InternalAddressService.SetAddressReference (атомарный CAS в vpc); mismatch /
// not-found мапится в generic InvalidArgument (анти-oracle). Owned=false —
// tenant-owned адрес (link): release снимает только референс, адрес уцелевает.
type AttachExistingRequest struct {
	AddressID string
	Owner     AddressOwner
	Owned     bool
}

// InternalAddressClient — port-интерфейс для service-слоя.
// Каждый метод выполняет атомарную операцию VPC IPAM и устанавливает
// referrer на новосозданном/изменённом Address-ресурсе.
type InternalAddressClient interface {
	// AllocateExternalIP создаёт внешний Address (auto-alloc IP из дефолтного
	// pool в zone) + atomic SetReference. Семантика ошибок:
	//   - FailedPrecondition (pool exhausted / zone unavailable) → domain.ErrFailedPrecondition
	//   - InvalidArgument                                       → domain.ErrInvalidArg
	//   - Unavailable/DeadlineExceeded                          → domain.ErrUnavailable
	AllocateExternalIP(ctx context.Context, req AllocateExternalIPRequest) (*AllocateResponse, error)

	// AllocateInternalIP создаёт внутренний Address в указанной subnet +
	// atomic SetReference.
	AllocateInternalIP(ctx context.Context, req AllocateInternalIPRequest) (*AllocateResponse, error)

	// AllocateExternalIPv6 — как AllocateExternalIP, но аллоцирует внешний
	// IPv6-VIP (external_ipv6 pool-IPAM). Контракт request/response и семантика
	// ошибок идентичны AllocateExternalIP.
	AllocateExternalIPv6(ctx context.Context, req AllocateExternalIPRequest) (*AllocateResponse, error)

	// AllocateInternalIPv6 — как AllocateInternalIP, но аллоцирует внутренний
	// IPv6-VIP из subnet.v6_cidr_blocks. Контракт идентичен AllocateInternalIP.
	AllocateInternalIPv6(ctx context.Context, req AllocateInternalIPRequest) (*AllocateResponse, error)

	// AttachExisting привязывает принесённый tenant'ом Address к owner-ресурсу
	// через InternalAddressService.SetAddressReference. Семантика ошибок
	// (анти-oracle: не подтверждаем чужой ownership/семейство/существование):
	//   - AlreadyExists (address занят другим referrer)  → domain.ErrFailedPrecondition
	//   - NotFound / InvalidArgument / PermissionDenied  → generic domain.ErrInvalidArg
	//                                                       "Illegal argument addressId"
	//   - Unavailable/DeadlineExceeded                   → domain.ErrUnavailable
	// Возвращает resolved-значение привязанного Address (Get после успеха).
	AttachExisting(ctx context.Context, req AttachExistingRequest) (*AllocateResponse, error)

	// FreeIP освобождает Address (idempotent через AddressService.Delete →
	// NotFound трактуется как успех). ClearReference вызывается автоматически
	// kacho-vpc при Delete.
	FreeIP(ctx context.Context, addressID string) error

	// SetReference — атомарный CAS Set used_by=owner на существующем Address.
	// owned помечает референс как owned (auto-alloc, lifecycle связан) либо
	// used_by (linked, tenant-owned). Семантика ошибок:
	//   - AlreadyExists (address уже занят другим owner) → domain.ErrFailedPrecondition
	//   - NotFound                                       → domain.ErrInvalidArg
	//   - Unavailable/DeadlineExceeded                   → domain.ErrUnavailable
	SetReference(ctx context.Context, addressID string, owner AddressOwner, owned bool) error

	// ClearReference — снимает used_by с Address (Listener.Delete release BYO).
	// Идемпотентно: NotFound → успех. Unavailable/DeadlineExceeded → domain.ErrUnavailable.
	ClearReference(ctx context.Context, addressID string) error
}

// internalAddressClient — реализация InternalAddressClient через gRPC.
//
// Использует ТРИ generated stub'а:
//   - AddressServiceClient        (public)  — Create / Delete (auto-alloc flow).
//   - InternalAddressServiceClient (internal) — SetReference / ClearReference.
//   - OperationServiceClient      (public)  — poll Operation на Create/Delete.
type internalAddressClient struct {
	addrs    vpcpb.AddressServiceClient
	internal vpcpb.InternalAddressServiceClient
	ops      operationpb.OperationServiceClient
	// logger — для наблюдаемости best-effort компенсаций (напр. failed
	// compensating FreeIP → leaked Address в kacho-vpc). Никогда не nil:
	// конструкторы дефолтят на slog.Default() (main.go делает slog.SetDefault).
	logger  *slog.Logger
	timeout time.Duration

	// visibilityRetries / visibilityInterval — бюджет OWN-LANE read-your-writes
	// ретрая (см. linkOwnAddress). Тесты сжимают cadence.
	visibilityRetries  int
	visibilityInterval time.Duration
}

// NewInternalAddressClient оборачивает grpc-conn'ы в typed adapter.
//
// publicConn — kacho-vpc public listener (`:9090`); содержит AddressService +
// OperationService.
// internalConn — kacho-vpc internal listener (`:9091`); содержит
// InternalAddressService (SetReference / ClearReference — не публикуются на
// external endpoint, Internal-only).
// Per-call timeout — DefaultInternalAddressCallTimeout.
func NewInternalAddressClient(publicConn, internalConn grpc.ClientConnInterface) InternalAddressClient {
	return NewInternalAddressClientWithTimeout(publicConn, internalConn, DefaultInternalAddressCallTimeout)
}

// NewInternalAddressClientWithTimeout — как NewInternalAddressClient, но с
// явным per-call timeout'ом (применяется к КАЖДОМУ outbound-вызову клиента).
// timeout<=0 → DefaultInternalAddressCallTimeout.
func NewInternalAddressClientWithTimeout(
	publicConn, internalConn grpc.ClientConnInterface, timeout time.Duration,
) InternalAddressClient {
	if publicConn == nil || internalConn == nil {
		return nil
	}
	return &internalAddressClient{
		addrs:    vpcpb.NewAddressServiceClient(publicConn),
		internal: vpcpb.NewInternalAddressServiceClient(internalConn),
		ops:      operationpb.NewOperationServiceClient(publicConn),
		logger:   slog.Default(),
		timeout:  resolveInternalAddressTimeout(timeout),
	}
}

// NewInternalAddressClientFromStubs — конструктор для тестов.
func NewInternalAddressClientFromStubs(
	addrs vpcpb.AddressServiceClient,
	internal vpcpb.InternalAddressServiceClient,
	ops operationpb.OperationServiceClient,
) InternalAddressClient {
	return NewInternalAddressClientFromStubsWithTimeout(addrs, internal, ops, DefaultInternalAddressCallTimeout)
}

// NewInternalAddressClientFromStubsWithTimeout — как
// NewInternalAddressClientFromStubs, но с явным per-call timeout'ом
// (используется тестами concurrency/timeout-фиксов).
func NewInternalAddressClientFromStubsWithTimeout(
	addrs vpcpb.AddressServiceClient,
	internal vpcpb.InternalAddressServiceClient,
	ops operationpb.OperationServiceClient,
	timeout time.Duration,
) InternalAddressClient {
	if addrs == nil || internal == nil || ops == nil {
		return nil
	}
	return &internalAddressClient{
		addrs: addrs, internal: internal, ops: ops,
		logger: slog.Default(), timeout: resolveInternalAddressTimeout(timeout),
	}
}

func resolveInternalAddressTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultInternalAddressCallTimeout
	}
	return d
}

// withCallTimeout — деривирует ctx с client'ом configured per-call timeout'ом.
// Caller обязан вызвать возвращённый cancel (defer). Единая точка для ВСЕХ
// outbound-вызовов этого client'а (Create/Delete/Get/SetReference/
// ClearReference/Operation.Get) — architecture.md "все sibling-методы клиента
// обязаны применять один и тот же configured-timeout".
func (c *internalAddressClient) withCallTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

// AllocateExternalIP — см. контракт InternalAddressClient.AllocateExternalIP.
func (c *internalAddressClient) AllocateExternalIP(
	ctx context.Context, req AllocateExternalIPRequest,
) (*AllocateResponse, error) {
	if err := validateExternalReq(req); err != nil {
		return nil, err
	}
	createReq := &vpcpb.CreateAddressRequest{
		ProjectId: req.ProjectID,
		Name:      req.Name,
		AddressSpec: &vpcpb.CreateAddressRequest_ExternalIpv4AddressSpec{
			ExternalIpv4AddressSpec: &vpcpb.ExternalIpv4AddressSpec{ZoneId: req.ZoneID},
		},
	}
	return c.allocFromCreate(ctx, createReq, req.Owner, func(a *vpcpb.Address) string {
		return a.GetExternalIpv4Address().GetAddress()
	})
}

// AllocateExternalIPv6 — см. контракт InternalAddressClient.AllocateExternalIPv6.
// Зеркало AllocateExternalIP для external_ipv6: AddressService.Create с
// external-IPv6-spec (vpc аллоцирует v6-VIP из EXTERNAL_PUBLIC v6-pool в writer-TX).
func (c *internalAddressClient) AllocateExternalIPv6(
	ctx context.Context, req AllocateExternalIPRequest,
) (*AllocateResponse, error) {
	if err := validateExternalReq(req); err != nil {
		return nil, err
	}
	createReq := &vpcpb.CreateAddressRequest{
		ProjectId: req.ProjectID,
		Name:      req.Name,
		AddressSpec: &vpcpb.CreateAddressRequest_ExternalIpv6AddressSpec{
			ExternalIpv6AddressSpec: &vpcpb.ExternalIpv6AddressSpec{ZoneId: req.ZoneID},
		},
	}
	return c.allocFromCreate(ctx, createReq, req.Owner, func(a *vpcpb.Address) string {
		return a.GetExternalIpv6Address().GetAddress()
	})
}

// AllocateInternalIP — см. контракт InternalAddressClient.AllocateInternalIP.
func (c *internalAddressClient) AllocateInternalIP(
	ctx context.Context, req AllocateInternalIPRequest,
) (*AllocateResponse, error) {
	if err := validateInternalReq(req); err != nil {
		return nil, err
	}
	createReq := &vpcpb.CreateAddressRequest{
		ProjectId: req.ProjectID,
		Name:      req.Name,
		AddressSpec: &vpcpb.CreateAddressRequest_InternalIpv4AddressSpec{
			InternalIpv4AddressSpec: &vpcpb.InternalIpv4AddressSpec{
				Scope: &vpcpb.InternalIpv4AddressSpec_SubnetId{SubnetId: req.SubnetID},
			},
		},
	}
	return c.allocFromCreate(ctx, createReq, req.Owner, func(a *vpcpb.Address) string {
		return a.GetInternalIpv4Address().GetAddress()
	})
}

// AllocateInternalIPv6 — см. контракт InternalAddressClient.AllocateInternalIPv6.
// Зеркало AllocateInternalIP для internal_ipv6: адрес из subnet.v6_cidr_blocks.
func (c *internalAddressClient) AllocateInternalIPv6(
	ctx context.Context, req AllocateInternalIPRequest,
) (*AllocateResponse, error) {
	if err := validateInternalReq(req); err != nil {
		return nil, err
	}
	createReq := &vpcpb.CreateAddressRequest{
		ProjectId: req.ProjectID,
		Name:      req.Name,
		AddressSpec: &vpcpb.CreateAddressRequest_InternalIpv6AddressSpec{
			InternalIpv6AddressSpec: &vpcpb.InternalIpv6AddressSpec{
				Scope: &vpcpb.InternalIpv6AddressSpec_SubnetId{SubnetId: req.SubnetID},
			},
		},
	}
	return c.allocFromCreate(ctx, createReq, req.Owner, func(a *vpcpb.Address) string {
		return a.GetInternalIpv6Address().GetAddress()
	})
}

// allocFromCreate — общий хвост per-family auto-alloc: AddressService.Create
// (vpc аллоцирует IP нужной family в writer-TX) + atomic SetReference сразу
// после Create (used_by=<owner> до commit Listener.Create). readIP извлекает
// family-specific resolved-адрес из ответа. pool_id не expose'ится через
// Create-response (для NLB-флоу не критично — pool tracking отдельный enhancement).
func (c *internalAddressClient) allocFromCreate(
	ctx context.Context,
	createReq *vpcpb.CreateAddressRequest,
	owner AddressOwner,
	readIP func(*vpcpb.Address) string,
) (*AllocateResponse, error) {
	addr, err := c.createAddressAndWait(ctx, createReq)
	if err != nil {
		return nil, err
	}
	// auto-alloc → owned=true (адрес заказан LB неявно, lifecycle связан).
	// OWN-LANE: адрес только что закоммичен нами, поэтому его невидимость —
	// материализационное окно, а не «плохой id» (см. linkOwnAddress).
	if err := c.linkOwnAddress(ctx, addr.GetId(), owner); err != nil {
		// Компенсация: возвращаем half-allocated адрес в пул. Не маскируем
		// исходную ошибку (она важнее для caller'а). Если компенсация тоже
		// падает — адрес orphaned в kacho-vpc; логируем Warn, чтобы leaked-адрес
		// был наблюдаем и реконсайлируем (иначе — silent IP leak).
		if freeErr := c.freeOwnAddress(ctx, addr.GetId()); freeErr != nil {
			c.logger.Warn("address_compensation_free_failed",
				"address_id", addr.GetId(),
				"owner_kind", owner.Kind,
				"owner_id", owner.ID,
				"set_reference_err", err.Error(),
				"free_err", freeErr.Error(),
			)
		}
		return nil, err
	}
	return &AllocateResponse{AddressID: addr.GetId(), Value: readIP(addr)}, nil
}

// linkOwnAddress — SetReference на адресе, который МЫ ЖЕ только что создали
// (auto-alloc хвост `allocFromCreate`), с ограниченным read-your-writes ретраем.
//
// Почему отдельная полоса от публичного `SetReference`. Тот обслуживает BYO-lane,
// где address_id пришёл ОТ КЛИЕНТА: там NOT_FOUND действительно значит «этот id не
// резолвится» и намеренно схлопывается в generic `ErrInvalidArg` (анти-oracle — не
// подтверждаем чужой ownership). Здесь id сминтили мы сами, а `AddressService.Create`
// уже закоммитил строку — значит NOT_FOUND (hide-existence) / PERMISSION_DENIED от
// vpc НЕ МОЖЕТ означать «нет такого адреса». Это отказ per-object authz-Check, пока
// owner-tuple свежего `vpc_address:<id>` ещё не материализовался (data-integrity.md:
// материализация eventually-consistent — outbox → drainer → iam → FGA).
//
// Наблюдённый инцидент (CI 30135586348, лог kacho-vpc — четыре случая, каждый убил
// здоровый LoadBalancer.Create):
//
//	{"msg":"authz_hide_existence","rpc":".../InternalAddressService/SetAddressReference",
//	 "relation":"v_update","object":"vpc_address:adrj251yyhebawpehh6h"}
//
// Прежде эта ошибка классифицировалась как `ErrInvalidArg`, и use-case (у которого не
// оставалось полосы) отвечал capacity-непрозрачным «could not allocate load balancer
// address» — фактически ЛОЖЬЮ: адрес был выделен, ёмкость ни при чём.
//
// Лечится КЛИЕНТСКИМ bounded-retry (api-conventions.md: «создал→сразу мутирую»
// закрывается bounded client-retry, НЕ серверным confirm-барьером, ban #9).
// Ретраим ТОЛЬКО own-lane и ТОЛЬКО коды невидимости; ёмкость/AlreadyExists/
// InvalidArgument не ретраятся и сохраняют прежнюю классификацию. Бюджет исчерпан →
// `ErrUnavailable` (честная transient-полоса: `allocAcquireErr` отдаёт UNAVAILABLE,
// ничего не раскрывая), НИКОГДА не capacity-текст.
func (c *internalAddressClient) linkOwnAddress(
	ctx context.Context, addressID string, owner AddressOwner,
) error {
	attempts, interval := c.visibilityBudget(1)

	var lastErr error
	for i := range attempts {
		rerr := c.setAddressReferenceRaw(ctx, addressID, owner, true)
		if rerr == nil {
			return nil
		}
		if !ownResourceInvisible(rerr) {
			// Не полоса невидимости (ёмкость / AlreadyExists / InvalidArgument /
			// peer down) — классифицируем как раньше, без ретрая.
			return mapSetReferenceErr(addressID, rerr)
		}
		lastErr = rerr
		c.logger.Debug("address_owner_tuple_not_visible_yet",
			"address_id", addressID,
			"owner_kind", owner.Kind,
			"owner_id", owner.ID,
			"attempt", i+1,
			"err", rerr.Error(),
		)
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: vpc set address reference %s: %w", domain.ErrUnavailable, addressID, ctx.Err())
		case <-time.After(interval):
		}
	}

	c.logger.Warn("address_owner_tuple_never_materialized",
		"address_id", addressID,
		"owner_kind", owner.Kind,
		"owner_id", owner.ID,
		"attempts", attempts,
		"err", lastErr.Error(),
	)
	return fmt.Errorf("%w: vpc set address reference %s: owner-tuple not visible after %d attempts: %w",
		domain.ErrUnavailable, addressID, attempts, lastErr)
}

// freeOwnAddress — компенсирующий Delete адреса, который МЫ ЖЕ только что создали,
// с тем же ограниченным read-your-writes ретраем, что и linkOwnAddress.
//
// Обычный `FreeIP` трактует NOT_FOUND как «уже удалён» и возвращает nil — верно для
// произвольного address_id, но НЕВЕРНО здесь: адрес создан нами мгновение назад, значит
// NOT_FOUND — это hide-existence deny того же не-материализовавшегося owner-tuple
// (в инциденте deny на Delete приходил через ~10ms после deny на SetAddressReference,
// на тот же объект). Проглотить его = превратить возвращаемый lease в МОЛЧАЛИВУЮ утечку
// пула (data-integrity.md «Lease-recycle-on-delete»), невидимую даже для
// `address_compensation_free_failed`.
//
// Поэтому ретраим Delete по полосе невидимости, а невозвращённый lease уходит наверх
// ошибкой — вызывающий логирует его как утечку.
func (c *internalAddressClient) freeOwnAddress(ctx context.Context, addressID string) error {
	// Половинный бюджет: этот путь исполняется УЖЕ ПОСЛЕ того, как linkOwnAddress
	// израсходовал свой (окно почти наверняка закрылось либо сломано по-крупному), а
	// суммарная задержка неуспешной Create не должна выесть polling-бюджет клиента.
	attempts, interval := c.visibilityBudget(2)

	var lastErr error
	for i := range attempts {
		rerr := c.deleteAddressRaw(ctx, addressID)
		if rerr == nil {
			return nil
		}
		if !ownResourceInvisible(rerr) {
			return mapAllocErr(addressID, rerr)
		}
		lastErr = rerr
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: vpc address delete %s: %w", domain.ErrUnavailable, addressID, ctx.Err())
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("%w: vpc address delete %s: owner-tuple not visible after %d attempts: %w",
		domain.ErrUnavailable, addressID, attempts, lastErr)
}

// deleteAddressRaw — AddressService.Delete + ожидание Operation, СЫРОЙ gRPC-status
// наружу (без idempotent-NotFound трактовки `FreeIP`): own-lane сам решает, что
// NOT_FOUND значит.
func (c *internalAddressClient) deleteAddressRaw(ctx context.Context, addressID string) error {
	callCtx, cancel := c.withCallTimeout(ctx)
	var op *operationpb.Operation
	err := retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		var rerr error
		op, rerr = c.addrs.Delete(auth.PropagateOutgoing(ctx), &vpcpb.DeleteAddressRequest{AddressId: addressID})
		return rerr
	})
	cancel()
	if err != nil {
		return err
	}
	if op == nil {
		return nil
	}
	_, err = c.waitOperation(ctx, op)
	return err
}

// visibilityBudget — бюджет own-lane read-your-writes ретрая, делённый на `divisor`
// (1 — полный). Тесты сжимают cadence через поля клиента; нули → дефолты. Всегда
// возвращает минимум 1 попытку (сам вызов).
func (c *internalAddressClient) visibilityBudget(divisor int) (int, time.Duration) {
	attempts := c.visibilityRetries
	if attempts <= 0 {
		attempts = defaultAddressVisibilityRetries
	}
	if divisor > 1 {
		attempts /= divisor
	}
	if attempts < 1 {
		attempts = 1
	}
	interval := c.visibilityInterval
	if interval <= 0 {
		interval = defaultAddressVisibilityInterval
	}
	return attempts, interval
}

// ownResourceInvisible — сырой gRPC-код означает «мой свежий ресурс ещё не виден
// per-object authz». vpc отвечает hide-existence NOT_FOUND там, где скрывает
// существование, и PERMISSION_DENIED там, где не скрывает; на own-lane обе формы
// значат одно и то же. Классификация ПО КОДУ, не по прозе сообщения.
func ownResourceInvisible(rerr error) bool {
	st, ok := status.FromError(rerr)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.NotFound, codes.PermissionDenied:
		return true
	default:
		return false
	}
}

// validateExternalReq — общая sync-валидация аргументов external-alloc (v4/v6).
//
// ZoneID НЕ обязателен и намеренно: пустая зона — anycast-аллокация из
// зоне-независимого пула (единственная форма для REGIONAL-балансировщика, а
// EXTERNAL всегда REGIONAL). vpc принимает пустой `zone_id` на external-spec
// ровно с этим смыслом (address/create.go validateExternalZone). Требование
// непустой зоны здесь и заставляло use-case выдумывать зону, пиня anycast-VIP к
// одной зоне.
func validateExternalReq(req AllocateExternalIPRequest) error {
	switch {
	case req.ProjectID == "":
		return fmt.Errorf("%w: project_id is empty", domain.ErrInvalidArg)
	case req.Owner.Kind == "" || req.Owner.ID == "":
		return fmt.Errorf("%w: owner is empty", domain.ErrInvalidArg)
	}
	return nil
}

// validateInternalReq — общая sync-валидация аргументов internal-alloc (v4/v6).
func validateInternalReq(req AllocateInternalIPRequest) error {
	switch {
	case req.ProjectID == "":
		return fmt.Errorf("%w: project_id is empty", domain.ErrInvalidArg)
	case req.SubnetID == "":
		return fmt.Errorf("%w: subnet_id is empty", domain.ErrInvalidArg)
	case req.Owner.Kind == "" || req.Owner.ID == "":
		return fmt.Errorf("%w: owner is empty", domain.ErrInvalidArg)
	}
	return nil
}

// FreeIP — см. контракт InternalAddressClient.FreeIP.
func (c *internalAddressClient) FreeIP(ctx context.Context, addressID string) error {
	if addressID == "" {
		return fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	}

	callCtx, cancel := c.withCallTimeout(ctx)
	var op *operationpb.Operation
	err := retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		var rerr error
		op, rerr = c.addrs.Delete(auth.PropagateOutgoing(ctx), &vpcpb.DeleteAddressRequest{AddressId: addressID})
		if rerr != nil {
			if st, ok := status.FromError(rerr); ok && st.Code() == codes.NotFound {
				// Idempotent: уже удалён.
				op = nil
				return nil
			}
			return rerr
		}
		return nil
	})
	cancel()
	if err != nil {
		return mapAllocErr(addressID, err)
	}
	if op == nil {
		return nil
	}
	if _, err := c.waitOperation(ctx, op); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return nil
		}
		return mapAllocErr(addressID, err)
	}
	return nil
}

// SetReference — см. контракт InternalAddressClient.SetReference.
func (c *internalAddressClient) SetReference(
	ctx context.Context, addressID string, owner AddressOwner, owned bool,
) error {
	switch {
	case addressID == "":
		return fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	case owner.Kind == "":
		return fmt.Errorf("%w: owner.Kind is empty", domain.ErrInvalidArg)
	case owner.ID == "":
		return fmt.Errorf("%w: owner.ID is empty", domain.ErrInvalidArg)
	}

	if rerr := c.setAddressReferenceRaw(ctx, addressID, owner, owned); rerr != nil {
		return mapSetReferenceErr(addressID, rerr)
	}
	return nil
}

// setAddressReferenceRaw — сам RPC SetAddressReference (retry.OnUnavailable +
// per-call deadline), возвращающий СЫРОЙ gRPC-status. Единственная точка вызова;
// классификацию делают вызывающие — BYO-lane через `mapSetReferenceErr`
// (анти-oracle), own-lane через `linkOwnAddress` (по коду, а не по прозе).
func (c *internalAddressClient) setAddressReferenceRaw(
	ctx context.Context, addressID string, owner AddressOwner, owned bool,
) error {
	callCtx, cancel := c.withCallTimeout(ctx)
	defer cancel()
	return retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		_, rerr := c.internal.SetAddressReference(auth.PropagateOutgoing(ctx), &vpcpb.SetAddressReferenceRequest{
			AddressId:    addressID,
			ReferrerType: owner.Kind,
			ReferrerId:   owner.ID,
			ReferrerName: owner.Name,
			Owned:        owned,
		})
		return rerr
	})
}

// mapSetReferenceErr — BYO-lane классификация SetAddressReference: address_id
// пришёл ОТ КЛИЕНТА, поэтому NOT_FOUND намеренно схлопывается в generic
// `ErrInvalidArg` (не подтверждаем чужой ownership — анти-oracle).
func mapSetReferenceErr(addressID string, rerr error) error {
	st, ok := status.FromError(rerr)
	if !ok {
		return fmt.Errorf("vpc set address reference %q: %w", addressID, rerr)
	}
	switch st.Code() {
	case codes.AlreadyExists:
		return fmt.Errorf("%w: address %s already used by another resource", domain.ErrFailedPrecondition, addressID)
	case codes.NotFound:
		return fmt.Errorf("%w: address %s not found", domain.ErrInvalidArg, addressID)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: vpc set address reference %s: %s", domain.ErrInvalidArg, addressID, st.Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		// fail-closed для мутации (api-conventions.md); также покрывает
		// DeadlineExceeded от per-call c.withCallTimeout на зависшем peer'е.
		return fmt.Errorf("%w: vpc set address reference %s: %s", domain.ErrUnavailable, addressID, st.Message())
	default:
		return fmt.Errorf("vpc set address reference %q: %w", addressID, rerr)
	}
}

// ClearReference — см. контракт InternalAddressClient.ClearReference.
func (c *internalAddressClient) ClearReference(ctx context.Context, addressID string) error {
	if addressID == "" {
		return fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	}

	callCtx, cancel := c.withCallTimeout(ctx)
	defer cancel()
	return retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		_, rerr := c.internal.ClearAddressReference(auth.PropagateOutgoing(ctx), &vpcpb.ClearAddressReferenceRequest{
			AddressId: addressID,
		})
		if rerr == nil {
			return nil
		}
		st, ok := status.FromError(rerr)
		if !ok {
			return fmt.Errorf("vpc clear address reference %q: %w", addressID, rerr)
		}
		switch st.Code() {
		case codes.NotFound:
			// Idempotent: уже снят / address удалён.
			return nil
		case codes.InvalidArgument:
			return fmt.Errorf("%w: vpc clear address reference %s: %s", domain.ErrInvalidArg, addressID, st.Message())
		case codes.Unavailable, codes.DeadlineExceeded:
			// fail-closed для мутации (api-conventions.md); также покрывает
			// DeadlineExceeded от per-call c.withCallTimeout на зависшем peer'е.
			return fmt.Errorf("%w: vpc clear address reference %s: %s", domain.ErrUnavailable, addressID, st.Message())
		default:
			return fmt.Errorf("vpc clear address reference %q: %w", addressID, rerr)
		}
	})
}

// AttachExisting — см. контракт InternalAddressClient.AttachExisting.
func (c *internalAddressClient) AttachExisting(
	ctx context.Context, req AttachExistingRequest,
) (*AllocateResponse, error) {
	switch {
	case req.AddressID == "":
		return nil, fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	case req.Owner.Kind == "" || req.Owner.ID == "":
		return nil, fmt.Errorf("%w: owner is empty", domain.ErrInvalidArg)
	}

	// Атомарный CAS-referrer в vpc (та же tx, что и запись used_by). Mismatch /
	// not-found → generic InvalidArgument (анти-oracle: не раскрываем чужой
	// ownership/семейство/несуществование адреса).
	setCtx, setCancel := c.withCallTimeout(ctx)
	err := retry.OnUnavailable(setCtx, func(ctx context.Context) error {
		_, rerr := c.internal.SetAddressReference(auth.PropagateOutgoing(ctx), &vpcpb.SetAddressReferenceRequest{
			AddressId:    req.AddressID,
			ReferrerType: req.Owner.Kind,
			ReferrerId:   req.Owner.ID,
			Owned:        req.Owned,
		})
		if rerr == nil {
			return nil
		}
		st, ok := status.FromError(rerr)
		if !ok {
			return fmt.Errorf("vpc set address reference %q: %w", req.AddressID, rerr)
		}
		switch st.Code() {
		case codes.AlreadyExists:
			return fmt.Errorf("%w: address %s already used by another resource", domain.ErrFailedPrecondition, req.AddressID)
		case codes.NotFound, codes.InvalidArgument, codes.PermissionDenied:
			return fmt.Errorf("%w: Illegal argument addressId", domain.ErrInvalidArg)
		case codes.Unavailable, codes.DeadlineExceeded:
			// fail-closed для мутации (api-conventions.md); также покрывает
			// DeadlineExceeded от per-call c.withCallTimeout на зависшем peer'е.
			return fmt.Errorf("%w: vpc set address reference %s: %s", domain.ErrUnavailable, req.AddressID, st.Message())
		default:
			return fmt.Errorf("vpc set address reference %q: %w", req.AddressID, rerr)
		}
	})
	setCancel()
	if err != nil {
		return nil, err
	}

	// Привязка прошла → адрес наш; читаем resolved-значение.
	addr, err := c.resolveAddressValue(ctx, req.AddressID)
	if err != nil {
		return nil, err
	}
	return &AllocateResponse{AddressID: req.AddressID, Value: addr}, nil
}

// resolveAddressValue — Get Address + извлечение resolved IP-строки (любое
// семейство). Используется после успешной BYO-привязки.
func (c *internalAddressClient) resolveAddressValue(ctx context.Context, addressID string) (string, error) {
	callCtx, cancel := c.withCallTimeout(ctx)
	defer cancel()
	var resp *vpcpb.Address
	if err := retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		var rerr error
		resp, rerr = c.addrs.Get(auth.PropagateOutgoing(ctx), &vpcpb.GetAddressRequest{AddressId: addressID})
		return rerr
	}); err != nil {
		return "", mapAllocErr(addressID, err)
	}
	switch {
	case resp.GetInternalIpv4Address() != nil:
		return resp.GetInternalIpv4Address().GetAddress(), nil
	case resp.GetInternalIpv6Address() != nil:
		return resp.GetInternalIpv6Address().GetAddress(), nil
	case resp.GetExternalIpv4Address() != nil:
		return resp.GetExternalIpv4Address().GetAddress(), nil
	case resp.GetExternalIpv6Address() != nil:
		return resp.GetExternalIpv6Address().GetAddress(), nil
	}
	return "", nil
}

// createAddressAndWait вызывает AddressService.Create + poll Operation до
// done=true. Возвращает созданный Address. Маппит ошибки в sentinel'ы.
func (c *internalAddressClient) createAddressAndWait(
	ctx context.Context, req *vpcpb.CreateAddressRequest,
) (*vpcpb.Address, error) {
	createCtx, createCancel := c.withCallTimeout(ctx)
	var op *operationpb.Operation
	err := retry.OnUnavailable(createCtx, func(ctx context.Context) error {
		var rerr error
		op, rerr = c.addrs.Create(auth.PropagateOutgoing(ctx), req)
		return rerr
	})
	createCancel()
	if err != nil {
		return nil, mapCreateAllocErr(err)
	}
	resp, err := c.waitOperation(ctx, op)
	if err != nil {
		return nil, mapCreateAllocErr(err)
	}
	if resp == nil {
		return nil, fmt.Errorf("vpc create address: operation %s returned no response", op.GetId())
	}
	addr := &vpcpb.Address{}
	if err := resp.UnmarshalTo(addr); err != nil {
		return nil, fmt.Errorf("vpc create address: unmarshal operation response: %w", err)
	}
	return addr, nil
}

// waitOperation поллит OperationService.Get до done=true. Возвращает
// Operation.response (`*anypb.Any`) либо смаппленную gRPC-status ошибку.
func (c *internalAddressClient) waitOperation(
	ctx context.Context, op *operationpb.Operation,
) (*anypb.Any, error) {
	if op.GetDone() {
		return operationResult(op)
	}
	deadline := time.Now().Add(vpcOpPollTimeout)
	ticker := time.NewTicker(vpcOpPollInterval)
	defer ticker.Stop()
	id := op.GetId()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
		var got *operationpb.Operation
		getCtx, getCancel := c.withCallTimeout(ctx)
		err := retry.OnUnavailable(getCtx, func(ctx context.Context) error {
			var rerr error
			got, rerr = c.ops.Get(auth.PropagateOutgoing(ctx), &operationpb.GetOperationRequest{OperationId: id})
			return rerr
		})
		getCancel()
		if err != nil {
			return nil, err
		}
		if got.GetDone() {
			return operationResult(got)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vpc operation %s did not finish within %s", id, vpcOpPollTimeout)
		}
	}
}

// operationResult извлекает либо response (success), либо google.rpc.Status
// (failure → gRPC error).
func operationResult(op *operationpb.Operation) (*anypb.Any, error) {
	if e := op.GetError(); e != nil {
		return nil, status.ErrorProto(e)
	}
	return op.GetResponse(), nil
}

// mapCreateAllocErr — маппер CREATE-полосы (AddressService.Create + poll его
// Operation), где адреса ЕЩЁ НЕТ.
//
// Поэтому NOT_FOUND от vpc на этой полосе НИКОГДА не означает «адрес не найден»
// — он всегда указывает на ССЫЛАЕМЫЙ объект: caller-supplied `subnet_id`
// (`assertSubnetOwned` в vpc отвечает `NotFound "Subnet <id> not found"` и на
// отсутствующую, и на чужую подсеть — без existence-oracle) либо инфра-объект
// (AddressPool underlay-зоны для public-VIP). Раньше обе ветки шли через
// `mapAllocErr("", err)`, чей NotFound-арм форматирует `"address %s not found"`
// с ПУСТЫМ id → наружу уходило `"address  not found"` под sentinel'ом
// ErrInvalidArg. Оба факта неверны (architecture.md doc-truthfulness), а
// мисклассификация не давала use-case'у отличить «твоя ссылка не резолвится» от
// «ёмкость исчерпана» — обе схлопывались в непрозрачное
// `"could not allocate load balancer address"`.
//
// Отдаём `domain.ErrNotFound` (отдельная полоса) и СОХРАНЯЕМ текст peer'а — он
// идёт в server-log; наружу его не эхает никто (loadbalancer.allocAcquireErr
// маппит полосу в фиксированный контрактный текст). Остальные коды — как в
// `mapAllocErr` (единый маппер, без дрейфа).
func mapCreateAllocErr(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return fmt.Errorf("%w: vpc address allocate: %s", domain.ErrNotFound, st.Message())
	}
	return mapAllocErr("", err)
}

// mapAllocErr транслирует gRPC-status в domain-sentinel-ошибки для allocate-
// флоу (Delete Address operation, Set/ClearReference, Get) — полос, работающих
// над СУЩЕСТВУЮЩИМ address_id. Там NOT_FOUND действительно значит «этот
// address_id не резолвится» и намеренно мапится в generic ErrInvalidArg
// («Illegal argument addressId»), чтобы не подтверждать чужой ownership
// (анти-oracle). CREATE-полоса использует `mapCreateAllocErr`.
func mapAllocErr(addressID string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("vpc address allocate %q: %w", addressID, err)
	}
	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: address %s not found", domain.ErrInvalidArg, addressID)
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: vpc address allocate: %s", domain.ErrFailedPrecondition, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: vpc address allocate: %s", domain.ErrFailedPrecondition, st.Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("%w: vpc address allocate: %s", domain.ErrUnavailable, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: vpc address allocate: %s", domain.ErrInvalidArg, st.Message())
	default:
		return fmt.Errorf("vpc address allocate: %w", err)
	}
}
