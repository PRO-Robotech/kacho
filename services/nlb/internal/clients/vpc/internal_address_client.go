// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"errors"
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
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Polling параметры для in-flight VPC operations (Create/Delete Address).
// kacho-vpc control-plane операции завершаются за ~1с (no real data-plane).
const (
	vpcOpPollInterval = 50 * time.Millisecond
	vpcOpPollTimeout  = 15 * time.Second
)

// Бюджет ожидания окна видимости снят с полосы АВТО-аллокации и у неё не
// восстановлен: адрес рождается СРАЗУ привязанным к владельцу
// (`createOwnedAddressAndWait`), второго решения о доступе на пути нет — ждать
// нечего. Полоса BYO-привязки устроена иначе и свой бюджет несёт: адрес принесён
// арендатором и существует задолго до вызова, поэтому отдельное решение о
// доступе к нему принимается всегда (см. `attachWithVisibilityBudget`). Разница
// не в аппетите к ретраю, а в том, устранима ли зависимость по существу.

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
// InternalAddressService.SetAddressReference (атомарный CAS в vpc); полосы
// ответа владельца разведены по sentinel'ам — см. контракт `AttachExisting`.
// Owned=false — tenant-owned адрес (link): release снимает только референс,
// адрес уцелевает.
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
	// через InternalAddressService.SetAddressReference. Полосы ответа владельца
	// (анти-oracle сохраняется ПРОЗОЙ у вызывающего, а не схлопыванием полос —
	// api-conventions.md §By-lane code-split):
	//   - AlreadyExists (address занят другим referrer)  → domain.ErrFailedPrecondition
	//   - NotFound / PermissionDenied                    → domain.ErrNotFound: адрес
	//     сейчас не резолвится у владельца. ПЕРЕХОДНОЕ состояние, пока
	//     материализуется пообъектный доступ к свежесозданному адресу; повтор
	//     закрывает его (ban #9 — барьера на стороне сервера не заводим)
	//   - InvalidArgument                                → domain.ErrInvalidArg
	//     (владелец счёл ссылку негодной — повтором не лечится)
	//   - FailedPrecondition                             → domain.ErrFailedPrecondition
	//   - Unavailable/DeadlineExceeded                   → domain.ErrUnavailable
	// Возвращает resolved-значение привязанного Address (Get после успеха).
	AttachExisting(ctx context.Context, req AttachExistingRequest) (*AllocateResponse, error)

	// ReleaseLease снимает аренду адреса ПО ПРЕДЪЯВЛЕНИЮ ВЛАДЕНИЯ ею и
	// возвращает НАЗВАННЫЙ владельцем исход.
	//
	// Заменяет пару `ClearReference` + `FreeIP`. Та пара спрашивала владельца
	// пообъектно — про сам адрес, — а на пообъектном вопросе ответ «не найдено»
	// НЕ несёт утверждения «аренды нет»: тем же ответом владелец намеренно
	// отвечает на промах чужого проекта и на опрос операции без ключа владельца.
	// Пара читала его как «работа сделана» и возвращала успех, после которого
	// строка потребителя сносилась, а координаты аренды не оставалось ни у кого.
	//
	// Здесь вопрос задан так, что этого ответа не бывает: право анкорится на
	// проекте, исход приезжает ПОЛЕМ. Ни один код ошибки больше не читается как
	// доказательство отсутствия аренды.
	//
	// Полосы отказа разведены носителем `pkg/peer`, корзины «прочее» у них нет —
	// она выбирала бы политику повтора за вызывающего. ЛЮБОЙ отказ означает
	// «работа НЕ сделана», аренду не трогаем:
	//   - Denied/StateRefused          → domain.ErrFailedPrecondition, терминально
	//   - Malformed                    → domain.ErrInvalidArg
	//   - Unavailable/DeadlineExceeded → domain.ErrUnavailable — ЕДИНСТВЕННАЯ
	//     повторяемая полоса (реконсайлер оставляет строку на следующий тик)
	//   - Missing                      → domain.ErrFailedPrecondition: глагол этой
	//     полосы не производит, значит мы говорим не с тем глаголом — настройка,
	//     а не «уже снято»
	//   - ответ не классифицирован     → domain.ErrInternal + строка журнала
	ReleaseLease(ctx context.Context, req ReleaseLeaseRequest) (LeaseOutcome, error)

	// SetReference — атомарный CAS Set used_by=owner на существующем Address.
	// owned помечает референс как owned (auto-alloc, lifecycle связан) либо
	// used_by (linked, tenant-owned). Семантика ошибок:
	//   - AlreadyExists (address уже занят другим owner) → domain.ErrFailedPrecondition
	//   - NotFound                                       → domain.ErrInvalidArg
	//   - Unavailable/DeadlineExceeded                   → domain.ErrUnavailable
	SetReference(ctx context.Context, addressID string, owner AddressOwner, owned bool) error

}

// OwnerKindLoadBalancer — значение `referrer_type`, под которым балансировщик
// владеет арендой адреса у vpc.
//
// Живёт ЗДЕСЬ, а не у вызывающего, потому что это значение на ПРОВОДЕ: им
// заводится аренда и им же она предъявляется при снятии. Разойдись эти два
// места — сверка владения не совпала бы НИ РАЗУ, и снятие отвечало бы «аренда
// не твоя» на собственную аренду. Один источник вместо двух — не стиль, а
// условие того, что полоса вообще работает.
const OwnerKindLoadBalancer = "network_load_balancer"

// ReleaseLeaseRequest — предъявление владения арендой.
//
// `owned` здесь нет намеренно: ветку «удалить адрес» либо «оставить адрес
// арендатора» выбирает ВЛАДЕЛЕЦ по своей колонке. Прежде это решение принимал
// потребитель по собственной копии признака (`vip_origin`), и три места,
// принимавших его, спрашивали по-разному.
type ReleaseLeaseRequest struct {
	ProjectID string
	AddressID string
	Owner     AddressOwner
}

// LeaseOutcome — исход, НАЗВАННЫЙ владельцем.
type LeaseOutcome string

const (
	// LeaseReleased — ЭТИМ вызовом: ссылка снята, адрес удалён, аренда в пуле.
	LeaseReleased LeaseOutcome = "RELEASED"
	// LeaseAlreadyReleased — аренда снята ранее (законный повтор).
	LeaseAlreadyReleased LeaseOutcome = "ALREADY_RELEASED"
	// LeaseDetached — ЭТИМ вызовом: адрес арендатора, ссылка снята, адрес жив.
	LeaseDetached LeaseOutcome = "DETACHED"
	// LeaseAlreadyDetached — ссылки этого потребителя уже не было.
	LeaseAlreadyDetached LeaseOutcome = "ALREADY_DETACHED"
)

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
	// linkAttempts / linkBackoff — бюджет окна видимости на link-CAS. Поля, а не
	// константы, чтобы проба сжимала каденцию, не подменяя саму петлю: подменённая
	// петля утверждала бы о своей копии, а не о том, что исполняется в проде.
	// Ноль = значения по умолчанию.
	linkAttempts int
	linkBackoff  time.Duration
}

// linkBudget — фактический бюджет окна видимости этого клиента.
func (c *internalAddressClient) linkBudget() (int, time.Duration) {
	attempts, backoff := c.linkAttempts, c.linkBackoff
	if attempts <= 0 {
		attempts = linkVisibilityAttempts
	}
	if backoff <= 0 {
		backoff = linkVisibilityBackoff
	}
	return attempts, backoff
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

// allocFromCreate — общий хвост per-family auto-alloc: ОДИН вызов
// `InternalAddressService.CreateOwnedAddress` (vpc вставляет строку, аллоцирует
// IP нужной family и проставляет referrer `used_by=<owner>` в ОДНОЙ writer-TX).
// readIP извлекает family-specific resolved-адрес из ответа. pool_id не
// expose'ится через Create-response (для NLB-флоу не критично — pool tracking
// отдельный enhancement).
//
// ЗДЕСЬ БЫЛА ПАРА «создать, затем привязать» — и она зависела от окна
// материализации. Второй вызов гейтился на объекте, которого в начале операции
// не существовало: доступ создателя к своему свежему адресу появляется не
// мгновенно, поэтому привязка крутила ограниченный ретрай, а на исчерпании
// бюджета отдавала честный transient-отказ — на СВОЙ ЖЕ адрес, выделенный
// мгновение назад. Замер прогона CI 31002239590: 8 таких отказов на пути
// создания балансировщика утянули 44 каскадных утверждения. Поднимать бюджет
// значило бы маскировать; вместо этого снята сама зависимость — решение о
// доступе принимается один раз и на project'е, который существует давно.
//
// Компенсации на этом пути больше нет by construction: отказ внутри одной
// транзакции откатывает и вставку, и аллокацию, и привязку, поэтому
// half-allocated адреса, который надо было бы возвращать в пул, не возникает.
func (c *internalAddressClient) allocFromCreate(
	ctx context.Context,
	createReq *vpcpb.CreateAddressRequest,
	owner AddressOwner,
	readIP func(*vpcpb.Address) string,
) (*AllocateResponse, error) {
	// auto-alloc → owned=true (адрес заказан LB неявно, lifecycle связан).
	addr, err := c.createOwnedAddressAndWait(ctx, &vpcpb.CreateOwnedAddressRequest{
		Address:      createReq,
		ReferrerType: owner.Kind,
		ReferrerId:   owner.ID,
		ReferrerName: owner.Name,
		Owned:        true,
	})
	if err != nil {
		return nil, err
	}
	// Аллокация, которая не может НАЗВАТЬ выделенное, — неудавшаяся аллокация
	// (#467). Пустой идентификатор доезжал до строки балансировщика и там
	// становился неотличим от «этого семейства нет»: освобождение молча ничего не
	// делало, строка удалялась, и аренда оставалась висеть на подсети навсегда —
	// вернуть её было уже нечем и некому. Отказ здесь разворачивает создание
	// целиком (вся вставка, аллокация и привязка идут одной транзакцией у vpc),
	// поэтому платы за него нет: невыделённый адрес возвращать не нужно.
	id := addr.GetId()
	if id == "" {
		return nil, fmt.Errorf("%w: vpc allocated an address without an id", domain.ErrUnavailable)
	}
	return &AllocateResponse{AddressID: id, Value: readIP(addr)}, nil
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

// ReleaseLease — см. контракт InternalAddressClient.ReleaseLease.
//
// Ни одна ветка здесь не превращает ответ владельца в успех: успех приезжает
// ТОЛЬКО как названный исход в поле. Это и есть предмет метода — код ошибки
// перестаёт быть источником суждения о том, что стало с арендой.
func (c *internalAddressClient) ReleaseLease(
	ctx context.Context, req ReleaseLeaseRequest,
) (LeaseOutcome, error) {
	switch {
	case req.ProjectID == "":
		return "", fmt.Errorf("%w: project_id is empty", domain.ErrInvalidArg)
	case req.AddressID == "":
		return "", fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	case req.Owner.Kind == "" || req.Owner.ID == "":
		return "", fmt.Errorf("%w: owner is empty", domain.ErrInvalidArg)
	}

	callCtx, cancel := c.withCallTimeout(ctx)
	defer cancel()
	var resp *vpcpb.ReleaseOwnedAddressResponse
	err := retry.OnUnavailable(callCtx, func(ctx context.Context) error {
		var rerr error
		resp, rerr = c.internal.ReleaseOwnedAddress(auth.PropagateOutgoing(ctx),
			&vpcpb.ReleaseOwnedAddressRequest{
				ProjectId:    req.ProjectID,
				AddressId:    req.AddressID,
				ReferrerType: req.Owner.Kind,
				ReferrerId:   req.Owner.ID,
			})
		return rerr
	})
	if err != nil {
		return "", c.mapReleaseLeaseErr(req.AddressID, err)
	}
	out, ok := leaseOutcomeFromProto(resp.GetOutcome())
	if !ok {
		// Владелец не назвал исход. Это НЕ «наверное сделано»: неназванный исход
		// разбирается как отказ, иначе мы вернулись бы ровно к выводу, который
		// метод и устраняет.
		return "", fmt.Errorf("%w: vpc release lease %s: outcome is unnamed", domain.ErrFailedPrecondition, req.AddressID)
	}
	return out, nil
}

// leaseOutcomeFromProto — закрытое отображение. Неизвестное значение (в том
// числе `OUTCOME_UNSPECIFIED`) возвращает ok=false, а не корзину «прочее».
func leaseOutcomeFromProto(o vpcpb.ReleaseOwnedAddressResponse_Outcome) (LeaseOutcome, bool) {
	switch o {
	case vpcpb.ReleaseOwnedAddressResponse_RELEASED:
		return LeaseReleased, true
	case vpcpb.ReleaseOwnedAddressResponse_ALREADY_RELEASED:
		return LeaseAlreadyReleased, true
	case vpcpb.ReleaseOwnedAddressResponse_DETACHED:
		return LeaseDetached, true
	case vpcpb.ReleaseOwnedAddressResponse_ALREADY_DETACHED:
		return LeaseAlreadyDetached, true
	default:
		return "", false
	}
}

// mapReleaseLeaseErr — полосы отказа снятия аренды, разведённые ОБЩИМ носителем
// `pkg/peer` (тем же, что у привязки и у публичного чтения адреса).
//
// Корзины «прочее» здесь нет намеренно. Она не нейтральна: она ВЫБИРАЕТ политику
// повтора за вызывающего — и на этой полосе выбирала бы терминальную для всего,
// что в её список не попало. Цена наблюдалась на стенде: недоступность модели
// прав приезжала кодом отказа, падала в корзину, а реконсайлер считает
// транзиентным ровно один sentinel — перебой в доли секунды изолировал
// балансировщик как отравленный, что на пути освобождения аренды необратимо.
//
// ОТСУТСТВИЕ ИСХОДА `nil` — предмет этого метода. У соседа по файлу полоса
// `OutcomeMissing` законно означала «снимать нечего»; здесь её нет и быть не
// может: глагол `NOT_FOUND` не производит, поэтому «нет ресурса» приезжает
// ПОЛЕМ ответа, а не кодом ошибки. Ни один код здесь не читается как
// доказательство снятой аренды.
func (c *internalAddressClient) mapReleaseLeaseErr(addressID string, rerr error) error {
	switch peer.Classify(rerr) {
	case peer.OutcomeOK:
		// Носитель счёл ответ успешным, а мы попали сюда только с ошибкой —
		// значит ответ не тот, за который себя выдаёт. Успехом это не называем.
		return fmt.Errorf("%w: vpc release lease %s: refusal classified as success",
			domain.ErrInternal, addressID)
	case peer.OutcomeMissing:
		// Глагол этой полосы не производит. Получить её можно, только говоря не с
		// тем глаголом (владелец не перекатан, поверхность не та) — это
		// НАСТРОЙКА, а не «аренды уже нет». Терминально и громко.
		return fmt.Errorf("%w: address %s: owner does not serve the release verb",
			domain.ErrFailedPrecondition, addressID)
	case peer.OutcomeDenied:
		// Решение владельца. Терминально: повтор идентичного запроса его не
		// изменит. Текст ФИКСИРОВАН — прозу чужого решения о доступе наружу не
		// несём (её pass-through был отдельной находкой на этой же полосе).
		return fmt.Errorf("%w: address %s lease cannot be released", domain.ErrFailedPrecondition, addressID)
	case peer.OutcomeStateRefused:
		// Предъявленное владение не подтвердилось: аренда чужая либо адрес из
		// другого проекта. Аренду НЕ трогаем.
		return fmt.Errorf("%w: address %s lease is not held as presented", domain.ErrFailedPrecondition, addressID)
	case peer.OutcomeMalformed:
		return fmt.Errorf("%w: vpc release lease %s: %s", domain.ErrInvalidArg, addressID, peer.PeerMessage(rerr))
	case peer.OutcomeUnavailable:
		// Единственная повторяемая полоса: владелец не установил НИЧЕГО.
		// fail-closed для мутации — недоступность не есть «уже снято».
		return fmt.Errorf("%w: vpc release lease %s: %s", domain.ErrUnavailable, addressID, peer.PeerMessage(rerr))
	}
	// Ответ, которому носитель полосы не назначил, — СОСТОЯНИЕ «не понят», а не
	// третья политика повтора. Стоит ПОСЛЕ switch, а не веткой в нём: полоса,
	// добавленная в носитель завтра, попадёт сюда — в самый тихий из осмысленных
	// исходов, а не в тихий успех.
	c.logger.Error("vpc release lease: peer answer not classified",
		"address_id", addressID, "err", rerr)
	return fmt.Errorf("%w: vpc release lease %s: peer answer not classified",
		domain.ErrInternal, addressID)
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
// per-call deadline), возвращающий СЫРОЙ gRPC-status; классификацию делает
// вызывающий — `SetReference` через `mapSetReferenceErr` (анти-oracle).
//
// Здесь стояло «единственная точка вызова» и ссылка на `linkOwnAddress` как на
// own-полосу. Оба утверждения ложны и пережили свой предмет: функции с таким
// именем в дереве нет ВОВСЕ, а вызовов самого RPC в этом файле два — этот и
// встроенный в `AttachExisting`, которая классифицирует свой ответ сама.
// Own-полоса этот RPC не зовёт с тех пор, как пара «создать, затем привязать»
// свелась к одному `CreateOwnedAddress` в одной транзакции (см. `allocFromCreate`),
// — именно ради снятия зависимости от окна материализации.
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

	// Атомарный CAS-referrer в vpc (та же tx, что и запись used_by). Полосы
	// ответа владельца разведены по sentinel'ам — см. контракт метода.
	err := c.attachWithVisibilityBudget(ctx, req)
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

// linkVisibilityBudget — сколько попыток и с какой паузой закрывать окно, в
// котором пообъектный доступ к УЖЕ СУЩЕСТВУЮЩЕМУ адресу ещё не материализовался.
//
// Замер стенда 2026-08-16 (прогон e2e 31965970965): адрес создан в 19:07:20.129,
// `v_get` виден в .237, `v_update` — только к .939, то есть окно ≈0.7 с и набор
// глаголов объекта в нём виден ЧАСТИЧНО. Бюджет взят с запасом к измеренному, но
// конечным: невидимость, которая не закрылась за него, отвечает своей полосой, а
// не ждёт дальше.
const (
	linkVisibilityAttempts = 12
	linkVisibilityBackoff  = 500 * time.Millisecond
)

// attachWithVisibilityBudget — link-CAS с ОГРАНИЧЕННЫМ повтором ровно одной
// полосы: «адрес сейчас не резолвится у владельца».
//
// Почему повтор здесь законен, хотя у соседней полосы авто-аллокации его сняли.
// Там зависимость от окна убрали ПО СУЩЕСТВУ: адрес рождается сразу привязанным,
// второго решения о доступе на пути нет. Здесь так нельзя by construction —
// адрес принесён арендатором и существует ЗАДОЛГО до нашего вызова, поэтому
// решение о доступе к нему принимается отдельно и всегда. Это ровно тот случай,
// для которого конвенция и предписывает ограниченный повтор ВЫЗЫВАЮЩЕГО
// (api-conventions.md: «создал→сразу мутирую» закрывается повтором клиента, а не
// серверным барьером — ban #9).
//
// Повторяется ТОЛЬКО полоса промаха. Негодная ссылка, проигранный CAS и
// состояние ресурса терминальны — повтор идентичного запроса их не изменит и
// лишь оттянул бы отказ на весь бюджет (data-integrity.md §«Межсервисное
// намерение»: отказ в правах НЕ временный). Недоступность соседа остаётся за
// `retry.OnUnavailable` внутри одной попытки.
func (c *internalAddressClient) attachWithVisibilityBudget(
	ctx context.Context, req AttachExistingRequest,
) error {
	attempts, backoff := c.linkBudget()
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: vpc set address reference %s: %s",
					domain.ErrUnavailable, req.AddressID, ctx.Err())
			case <-time.After(backoff):
			}
		}
		err = c.setAddressReferenceOnce(ctx, req)
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	return err
}

// setAddressReferenceOnce — одна попытка link-CAS: сам RPC плюс классификация
// ответа владельца.
func (c *internalAddressClient) setAddressReferenceOnce(
	ctx context.Context, req AttachExistingRequest,
) error {
	setCtx, setCancel := c.withCallTimeout(ctx)
	defer setCancel()
	return retry.OnUnavailable(setCtx, func(ctx context.Context) error {
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
		// Проигранный CAS — вне закрытого набора носителя (`AlreadyExists` полосы
		// не имеет: у соседа это не отказ ссылки, а состояние нашей же попытки).
		if st.Code() == codes.AlreadyExists {
			return fmt.Errorf("%w: address %s already used by another resource", domain.ErrFailedPrecondition, req.AddressID)
		}
		// Остальное классифицирует носитель — тот же, что у публичного
		// `AddressClient.Get` (`mapAddressErr`). Прежде здесь стоял свой разбор
		// кодов, и он сводил промах, отказ в правах и негодную ссылку в ОДИН
		// sentinel «аргумент незаконен»: окно материализации пообъектного доступа
		// становилось неотличимо от настоящей ошибки ввода — ни для клиентского
		// повтора (ban #9), ни для разбора красноты.
		switch peer.Classify(rerr) {
		case peer.OutcomeMissing, peer.OutcomeDenied:
			// anti-oracle: «нет адреса» и «не виден» — один sentinel и один текст.
			return fmt.Errorf("%w: address %s not found", domain.ErrNotFound, req.AddressID)
		case peer.OutcomeStateRefused:
			return fmt.Errorf("%w: address %s state does not allow linking", domain.ErrFailedPrecondition, req.AddressID)
		case peer.OutcomeMalformed:
			return fmt.Errorf("%w: vpc set address reference %s: %s", domain.ErrInvalidArg, req.AddressID, peer.PeerMessage(rerr))
		case peer.OutcomeUnavailable:
			// fail-closed для мутации (api-conventions.md); также покрывает
			// DeadlineExceeded от per-call c.withCallTimeout на зависшем peer'е.
			return fmt.Errorf("%w: vpc set address reference %s: %s", domain.ErrUnavailable, req.AddressID, peer.PeerMessage(rerr))
		}
		return fmt.Errorf("vpc set address reference %q: %w", req.AddressID, rerr)
	})
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

// createOwnedAddressAndWait — создать адрес, СРАЗУ привязанный к владельцу, и
// дождаться завершения его операции. ОДНА мутация на внутреннем листенере вместо
// прежней пары «создать, затем привязать»: на пути нет второго решения о доступе,
// а значит нет и зависимости от окна материализации свежесозданного объекта.
// Прежний двухшаговый путь удалён вместе со своим бюджетом ожидания — оставленный
// мёртвым, он читался бы как действующая альтернатива.
func (c *internalAddressClient) createOwnedAddressAndWait(
	ctx context.Context, req *vpcpb.CreateOwnedAddressRequest,
) (*vpcpb.Address, error) {
	createCtx, createCancel := c.withCallTimeout(ctx)
	var op *operationpb.Operation
	err := retry.OnUnavailable(createCtx, func(ctx context.Context) error {
		var rerr error
		op, rerr = c.internal.CreateOwnedAddress(auth.PropagateOutgoing(ctx), req)
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
		return nil, fmt.Errorf("vpc create owned address: operation %s returned no response", op.GetId())
	}
	addr := &vpcpb.Address{}
	if err := resp.UnmarshalTo(addr); err != nil {
		return nil, fmt.Errorf("vpc create owned address: unmarshal operation response: %w", err)
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
	// Исчерпанный бюджет ожидания — это «сосед не ответил», а не «неизвестно
	// что». Ошибка приезжает сюда НЕ gRPC-статусом (её родил истёкший per-call
	// контекст поверх повторов на UNAVAILABLE), поэтому без явной ветки она
	// попадала в неклассифицированную и теряла retryable-полосу: вызывающий
	// получал «прочее» там, где верно «повтори позже».
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: vpc address allocate: peer did not answer within the call budget", domain.ErrUnavailable)
	}
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
