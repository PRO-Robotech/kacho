// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/H-BF/corlib/pkg/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// CreateUseCase инициирует создание Listener'а.
//
// VIP консолидирован на LoadBalancer (anycast active-active): листенер — «порт на
// VIP LB», собственной аллокации адреса больше не делает. Поэтому Create —
// чистый INSERT строки-листенера (FK на LB), без acquireVIP-саги и без обращения
// к vpc.
//
// Sync (handler-thread, до возврата Operation клиенту):
//  1. Required: load_balancer_id.
//  2. LB.Get (same project + status != DELETING) — NotFound иначе.
//  3. domain.Listener builder + Validate (name regex, port range, protocol, labels).
//  4. opsRepo.CreateWithPrincipal(op, principal).
//  5. operations.Run(callerCtx, opsRepo, op.ID, worker) — fire-and-trigger.
//
// Async worker — одна writer-TX (внешнего side-effect нет):
//
//	INSERT listener (status='ACTIVE') + outbox `nlb_listener:<id> CREATED` +
//	`nlb_load_balancer:<lb_id> UPDATED` + FGA-register-intent (creator +
//	parent-link). Триггер lb_status_recompute переводит LB INACTIVE→ACTIVE, если
//	теперь есть листенер И attached TG.
type CreateUseCase struct {
	repo    RepoFactory
	opsRepo OperationsRepo
	// registrar — sync-primary owner-tuple registrar (kacho-iam RegisterResource),
	// вызывается BEST-EFFORT после durable commit листенера. nil → только async
	// register-drainer. См. WithRegistrar.
	registrar Registrar
	// internalAddrs — NLB-1b F5 VIP acquire/release (AllocateInternalIP[v6] для auto
	// subnet_id; AttachExisting для BYO address_id; FreeIP/ClearReference на откате).
	// nil → VIP-анкер не поддерживается (Create с address_id/subnet_id → Unavailable).
	internalAddrs InternalAddressClient
	// subnetClient — NLB-1b F5 placement/zone-coherence peer-validate (NLB-1-32/33).
	// nil → coherence-precheck пропускается (минимальный existence через acquire).
	subnetClient SubnetClient
	logger       *slog.Logger
}

// NewCreateUseCase — конструктор. Зависимости — port-интерфейсы (composition
// root wires в `cmd/kacho-loadbalancer/main.go`). logger допускается nil.
func NewCreateUseCase(
	repo RepoFactory,
	opsRepo OperationsRepo,
	logger *slog.Logger,
) *CreateUseCase {
	return &CreateUseCase{
		repo:    repo,
		opsRepo: opsRepo,
		logger:  logger,
	}
}

// WithVIP подключает vpc-клиенты для VIP-анкера листенера (NLB-1b F5): аллокация/
// линк VIP на Create и placement/zone-coherence peer-validate. Без них Create с
// address_id/subnet_id fail-closed'ит (`Unavailable`); Create без анкера (legacy
// VIP-on-LB fallback) работает и без клиентов. Возвращает self для chaining.
func (u *CreateUseCase) WithVIP(internalAddrs InternalAddressClient, subnetClient SubnetClient) *CreateUseCase {
	u.internalAddrs = internalAddrs
	u.subnetClient = subnetClient
	return u
}

// WithRegistrar подключает sync-primary owner-tuple registrar. После durable
// commit листенера (+ его `fga_register_outbox`-intent'а) containment-tuple
// синхронно регистрируется в kacho-iam — grant создателя доступен сразу.
// BEST-EFFORT: сбой sync-Register логируется и глотается (durable intent +
// drainer — backstop), Operation.done НЕ гейтится (ban #9). Возвращает self.
func (u *CreateUseCase) WithRegistrar(r Registrar) *CreateUseCase {
	u.registrar = r
	return u
}

// Run — sync validation + Operation creation + async worker spawn. Возвращает
// Operation клиенту до завершения worker'а; клиент поллит OperationService.Get.
func (u *CreateUseCase) Run(ctx context.Context, req *lbv1.CreateListenerRequest) (*operations.Operation, error) {
	lbID := req.GetLoadBalancerId()
	if lbID == "" {
		return nil, status.Error(codes.InvalidArgument, "load_balancer_id required")
	}
	// Malformed referenced load_balancer_id → sync InvalidArgument first (before
	// repo.Get), not NotFound (api-conventions malformed-id discipline).
	if err := validateLoadBalancerRefID(lbID); err != nil {
		return nil, err
	}

	// Sync read parent LB. Verifies LB exists, not DELETING; пробрасывает
	// project_id/region_id для denormalisation и семейства для vestigial ip_version.
	lb, err := u.fetchParentLB(ctx, lbID)
	if err != nil {
		return nil, err
	}

	// NLB-1b MIGRATE (F4/NLB-1-23): the listener wires to a TargetGroup DIRECTLY
	// (single authoritative targetGroupId). Sync-precheck existence +
	// region-coherence with the parent LB — a missing TG yields actionable guidance;
	// the direct FK (0018) is the atomic race backstop. target_group_id takes
	// precedence over the legacy default_target_group_id (both coexist until CONTRACT).
	tgID := req.GetTargetGroupId()
	if tgID == "" {
		tgID = req.GetDefaultTargetGroupId()
	}
	if err := u.prevalidateTargetGroup(ctx, tgID, string(lb.RegionID)); err != nil {
		return nil, err
	}

	// NLB-1b F5 (MIGRATE): VIP-анкер листенера — address_id (BYO) ⊕ subnet_id (auto),
	// взаимоисключающие immutable-инпуты. Отсутствие обоих → без собственного VIP
	// (optional-first: fallback на VIP LB). foreign vpc id → existence/placement
	// peer-validate, НЕ nlb-prefix-check (B4).
	vipAnchor, err := u.resolveVIPAnchor(ctx, req, lb.LoadBalancer)
	if err != nil {
		return nil, err
	}

	name, err := buildDomainName(req.GetName())
	if err != nil {
		return nil, err
	}
	// Port/target_port are int64 on the wire; guard int32 overflow before the
	// narrowing so an out-of-range value can't alias onto a valid port and bypass
	// LbPort.Validate (api-conventions malformed-input discipline; gosec G115).
	port, err := domain.LbPortFromProto(req.GetPort())
	if err != nil {
		return nil, err
	}
	targetPort, err := domain.LbPortFromProto(req.GetTargetPort())
	if err != nil {
		return nil, err
	}
	listener := domain.NewListener(
		lb.LoadBalancer,
		name,
		domain.LbProto(req.GetProtocol().String()),
		port,
		targetPort,
		listenerIPVersion(lb.LoadBalancer),
	)
	listener.Description = domain.LbDescription(req.GetDescription())
	listener.Labels = domain.LabelsFromMap(req.GetLabels())
	listener.ProxyProtocolV2 = req.GetProxyProtocolV2()
	// NLB-1b MIGRATE: wire the (already prechecked) authoritative targetGroupId.
	if tgID != "" {
		listener.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID(tgID))
	}
	// NLB-1b F5: persist the VIP-anchor discriminator (vip_origin + subnet_id).
	// address_id/allocated_address are filled by the worker after the VIP is
	// resolved (acquire → INSERT-with-VIP). Терминальное состояние — ACTIVE (VIP,
	// если есть, аллоцируется ДО durable INSERT, поэтому CREATING-фаза не нужна).
	listener.VipOrigin = vipAnchor.origin
	if vipAnchor.subnetID != "" {
		listener.SubnetID = option.MustNewOption(domain.SubnetID(vipAnchor.subnetID))
	}
	listener.Status = domain.ListenerStatusActive

	if err := listener.Validate(); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Create listener %s", string(name)),
		&lbv1.CreateListenerMetadata{
			ListenerId:     string(listener.ID),
			LoadBalancerId: lbID,
		},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	in := createInput{
		listener: listener,
		lb:       lb,
		// Acting subject FGA-id inline (parity с loadbalancer/targetgroup):
		// `<type>:<id>` либо "" для anonymous/system (creator-tuple пропускается).
		fgaOwner:  domain.FGASubjectFromPrincipal(principal.Type, principal.ID),
		vipAnchor: vipAnchor,
	}
	// Durable commit → op done сразу. Owner-tuple Listener материализуется
	// eventually-consistent (writer-TX fga_register_outbox intent → register-
	// drainer → kacho-iam RegisterResource → reconciler backstop); Operation.done
	// означает durability ресурса, не видимость owner-tuple в FGA.
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doCreate(workerCtx, in)
	})
	return &op, nil
}

// parentLB — snapshot полей LB, нужных Listener-Create worker'у.
type parentLB struct {
	domain.LoadBalancer
}

// fetchParentLB — sync Get parent LB. NotFound — LB не существует;
// FailedPrecondition — LB.Status == DELETING; Internal — repo failure.
func (u *CreateUseCase) fetchParentLB(ctx context.Context, lbID string) (*parentLB, error) {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()
	rec, err := rd.LoadBalancers().Get(ctx, lbID)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if rec.Status == domain.LBStatusDeleting {
		return nil, status.Errorf(codes.FailedPrecondition,
			"NetworkLoadBalancer %s is being deleted", lbID)
	}
	return &parentLB{LoadBalancer: rec.LoadBalancer}, nil
}

// prevalidateTargetGroup — NLB-1b MIGRATE (F4/NLB-1-23): sync precheck that the
// wired targetGroupId references an EXISTING TargetGroup region-coherent with the
// parent LB. Missing → actionable FAILED_PRECONDITION (guides the client to create
// the TG first or use the one-shot LB.Create); region mismatch → region-coherence
// FAILED_PRECONDITION. The direct FK (0018) is the atomic backstop for races.
func (u *CreateUseCase) prevalidateTargetGroup(ctx context.Context, tgID, lbRegion string) error {
	if tgID == "" {
		return nil
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()
	tg, err := rd.TargetGroups().Get(ctx, tgID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return status.Error(codes.FailedPrecondition,
				"listener requires an existing targetGroupId; create the TargetGroup first "+
					"(POST /nlb/v1/targetGroups) or use one-shot NetworkLoadBalancer.Create")
		}
		return mapDomainErr(err)
	}
	if string(tg.RegionID) != lbRegion {
		return status.Errorf(codes.FailedPrecondition,
			"target group region %s does not match listener region %s", tg.RegionID, lbRegion)
	}
	return nil
}

// buildDomainName — обёртка над domain.LbName с верхним sync-маппингом proto
// → domain newtype. Возвращает gRPC InvalidArgument из Validate.
func buildDomainName(raw string) (domain.LbName, error) {
	n := domain.LbName(raw)
	if err := n.Validate(); err != nil {
		return "", err
	}
	return n, nil
}

// listenerIPVersion — вестигиальное значение для NOT NULL колонки listeners.ip_version
// (снята с proto-листенера; колонка удаляется поздней миграцией). Берётся первое
// семейство родительского LB, иначе IPV4 — листенер обслуживает VIP LB всех его
// семейств одновременно.
func listenerIPVersion(lb domain.LoadBalancer) domain.IPVersion {
	for _, f := range lb.IPFamilies {
		if f == domain.IPVersionV4 || f == domain.IPVersionV6 {
			return f
		}
	}
	return domain.IPVersionV4
}

// createInput — snapshot входов для async worker.
type createInput struct {
	listener  domain.Listener
	lb        *parentLB
	fgaOwner  string
	vipAnchor vipAnchor
}

// doCreate — worker. Без VIP-анкера: одна writer-TX (INSERT ACTIVE + outbox +
// FGA-intent). С VIP-анкером (NLB-1b F5): acquire VIP externally (SetReference
// owner=nlb_listener:<id>) ДО durable INSERT, затем та же INSERT-TX с уже
// заполненными address_id/allocated_address (partial-UNIQUE `(region,ip,port,proto)`
// — атомарный backstop → 23505 → ALREADY_EXISTS). На любом pre-commit сбое —
// best-effort worker-компенсация releaseVIP (auto→FreeIP, byo→ClearReference),
// зеркалит recycle-on-delete. Триггер lb_status_recompute сам переводит LB
// INACTIVE→ACTIVE при has_listener AND has_attached.
func (u *CreateUseCase) doCreate(ctx context.Context, in createInput) (*anypb.Any, error) {
	if !in.vipAnchor.present() {
		return u.doInsert(ctx, in)
	}
	// NLB-1b F5: acquire the VIP anchor externally BEFORE the durable INSERT.
	alloc, err := u.acquireVIP(ctx, in)
	if err != nil {
		return nil, err
	}
	in.listener.AddressID = option.MustNewOption(domain.AddressID(alloc.addressID))
	in.listener.AllocatedAddress = domain.IPAddress(alloc.address)
	any, err := u.doInsert(ctx, in)
	if err != nil {
		// Best-effort worker compensation: release the acquired VIP so a failed
		// Create (dup port, VIP conflict, commit failure) does not leak the lease.
		u.compensateVIP(ctx, alloc.addressID, in.vipAnchor.origin == domain.VipOriginBYO)
		return nil, err
	}
	return any, nil
}

// doInsert — durable INSERT-TX: INSERT листенера (status='ACTIVE') + outbox CREATED
// + LB UPDATED + FGA-register-intent (creator + parent-link) атомарно. Возвращает
// anypb.Any(Listener) при успехе; ошибка означает pre-commit failure (компенсируемо).
func (u *CreateUseCase) doInsert(ctx context.Context, in createInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()

	listener := in.listener
	created, err := w.Listeners().Insert(ctx, &listener)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		outboxResourceTypeListener, string(created.ID), string(created.ProjectID),
		outboxActionCreated, listenerPayloadMap(created),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit listener CREATED: %v", domain.ErrInternal, err))
	}
	if err := w.Outbox().Emit(ctx,
		outboxResourceTypeLoadBalancer, string(in.lb.ID), string(in.lb.ProjectID),
		outboxActionUpdated, lbUpdatedPayloadMap(string(in.lb.ID), string(in.lb.ProjectID), string(in.lb.RegionID), "listener_created"),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit lb UPDATED: %v", domain.ErrInternal, err))
	}
	if err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister,
		listenerRegisterIntent(created, in.fgaOwner)); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: fga register-intent emit: %v", domain.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	committed = true

	// Sync-primary owner-tuple registration (после durable commit листенера + его
	// fga_register_outbox-intent'а): containment-grant виден сразу, закрывая
	// async-only окно. BEST-EFFORT — сбой логируется и глотается (durable intent
	// + register-drainer — backstop); Operation.done НЕ гейтится (ban #9).
	u.syncRegister(ctx, listenerRegisterIntent(created, in.fgaOwner))

	return marshalListener(created)
}

// syncRegister — BEST-EFFORT sync owner-tuple регистрация после durable commit.
// Ошибка ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable fga_register_outbox-intent +
// register-drainer — at-least-once backstop; Operation.done НЕ гейтится (ban #9).
// nil registrar → no-op.
func (u *CreateUseCase) syncRegister(ctx context.Context, intent domain.FGARegisterIntent) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent); err != nil {
		loggerOrDiscard(u.logger).Warn("Listener.Create sync owner-tuple registration incomplete; register-drainer will reconcile",
			"err", err, "listener_id", intent.ResourceID)
	}
}

// listenerRegisterIntent — FGA-register-intent для созданного Listener:
//
//	<subject> #admin @nlb_listener:<id>                                 (creator)
//	nlb_network_load_balancer:<lb_id> #load_balancer @nlb_listener:<id>  (parent-link)
//
// creator-tuple пропускается на пустом subject (system-initiated). Листенер
// резолвит проект через parent-link → LB-иерархию (своего project-tuple нет).
// Несёт labels + parent-project, чтобы kacho-iam обновил resource_mirror
// (γ-селекторы matchLabels). source_version штампует outbox-emitter из DB-clock.
func listenerRegisterIntent(l *kachorepo.ListenerRecord, subject string) domain.FGARegisterIntent {
	id := string(l.ID)
	// project-tuple идёт ПЕРВЫМ — он даёт видимость Listener через project (как у
	// LoadBalancer/TargetGroup: reconciler материализует v_*-relation по
	// parent-project). Дренер применяет tuples по порядку и short-circuit'ит на
	// первом отказе, а creator (relation admin) и parent-link (load_balancer)
	// iam-proxy отвергает (allowedProxyRelations = {project, account, parent,
	// owner}). Раньше первым шёл creator(admin) → падал сразу → НИ ОДИН tuple не
	// применялся → Listener был невидим в authz-filtered List. Теперь project-
	// tuple успевает примениться до отказа admin — Listener виден.
	tuples := []domain.FGATuple{
		domain.FGAProjectTuple(domain.FGAObjectTypeListener, id, string(l.ProjectID)),
	}
	if subject != "" {
		tuples = append(tuples, domain.FGACreatorTuple(subject, domain.FGAObjectTypeListener, id))
	}
	tuples = append(tuples, domain.FGAParentLinkTuple(
		domain.FGAObjectTypeLoadBalancer, string(l.LoadBalancerID),
		domain.FGARelationLoadBalancer,
		domain.FGAObjectTypeListener, id,
	))
	return domain.FGARegisterIntent{
		Kind:            "Listener",
		ResourceID:      id,
		Tuples:          tuples,
		Labels:          domain.LabelsToMap(l.Labels),
		ParentProjectID: string(l.ProjectID),
	}
}

// listenerUnregisterIntent — FGA-unregister-intent (parent-link) для удалённого
// Listener (creator оставляется IAM-side GC).
func listenerUnregisterIntent(listenerID, lbID string) domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:       "Listener",
		ResourceID: listenerID,
		Tuples: []domain.FGATuple{
			domain.FGAParentLinkTuple(
				domain.FGAObjectTypeLoadBalancer, lbID,
				domain.FGARelationLoadBalancer,
				domain.FGAObjectTypeListener, listenerID,
			),
		},
	}
}
