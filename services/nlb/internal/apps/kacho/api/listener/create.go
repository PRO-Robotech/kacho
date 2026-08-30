// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
//	`nlb_load_balancer:<lb_id> UPDATED` + FGA-register-intent (project-hierarchy).
//	Триггер lb_status_recompute переводит LB INACTIVE→ACTIVE, если теперь есть
//	листенер И attached TG.
type CreateUseCase struct {
	// quota — совещательная полоса учёта числа ресурсов.
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции),
	// поэтому провязка обязательна на любом поднятом стенде; отсутствие
	// допустимо только там, где нет и соседа, у которого спрашивать величины.
	quota QuotaGuard

	repo    RepoFactory
	opsRepo OperationsRepo
	// registrar — sync-primary owner-tuple registrar (kacho-iam RegisterResource),
	// вызывается BEST-EFFORT после durable commit листенера. nil → только async
	// register-drainer. См. WithRegistrar.
	registrar Registrar
	// checkClient — object-scoped authz-gate для caller-supplied `targetGroupId`
	// (per-RPC interceptor скоупит только parent LB). nil НЕ означает «пропустить»:
	// отсутствие решателя — отказ (`Unavailable`). См. tg_ref.go.
	checkClient CheckClient
	logger      *slog.Logger
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

// WithRegistrar подключает sync-primary owner-tuple registrar. После durable
// commit листенера (+ его `fga_register_outbox`-intent'а) containment-tuple
// синхронно регистрируется в kacho-iam — grant создателя доступен сразу.
// BEST-EFFORT: сбой sync-Register логируется и глотается (durable intent +
// drainer — backstop), Operation.done НЕ гейтится (ban #9). Возвращает self.
func (u *CreateUseCase) WithRegistrar(r Registrar) *CreateUseCase {
	u.registrar = r
	return u
}

// WithCheckClient подключает object-scoped authz-gate для caller-supplied
// `targetGroupId` (`viewer` на `nlb_target_group:<id>`). Per-RPC interceptor
// авторизует caller'а только на parent LoadBalancer, поэтому TG — необойдённый
// caller-supplied объект (CWE-863). nil НЕ означает «пропустить»: отсутствие
// решателя — отказ (`Unavailable`). Возвращает self.
func (u *CreateUseCase) WithCheckClient(c CheckClient) *CreateUseCase {
	u.checkClient = c
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
	// (single authoritative targetGroupId). Sync-precheck owning-project scope +
	// object-scoped authz + existence + region-coherence with the parent LB — a
	// missing TG yields actionable guidance; the direct FK (0018/0023) is the atomic
	// race backstop.
	//
	// Ссылка на группу целей — ОДИН вход, `target_group_id` (задача продукта
	// #1596). `default_target_group_id` СНЯТ со входа и оставлен в запросе ровно
	// затем, чтобы клиент, который его шлёт, получил внятный отказ вместо
	// молчаливого отбрасывания на крае (`DiscardUnknown`) — та же идиома, что у
	// `type`/`placement_type` балансировщика. Отказ наступает ДО любой записи.
	if req.GetDefaultTargetGroupId() != "" {
		return nil, status.Error(codes.InvalidArgument, retiredDefaultTargetGroupMsg)
	}
	tgID := req.GetTargetGroupId()
	if err := u.prevalidateTargetGroup(ctx, tgID, string(lb.ProjectID), string(lb.RegionID)); err != nil {
		return nil, err
	}

	name, err := buildDomainName(req.GetName())
	if err != nil {
		return nil, err
	}
	// port is int64 on the wire; guard int32 overflow before the narrowing so an
	// out-of-range value can't alias onto a valid port and bypass LbPort.Validate
	// (api-conventions malformed-input discipline; gosec G115).
	//
	// The backend port is NOT read here and has no field of its own: it lives on
	// the wired TargetGroup and comes back as resolved_backend_port (NLB-1-25).
	port, err := domain.LbPortFromProto(req.GetPort())
	if err != nil {
		return nil, err
	}
	listener := domain.NewListener(
		lb.LoadBalancer,
		name,
		domain.LbProto(req.GetProtocol().String()),
		port,
	)
	listener.Description = domain.LbDescription(req.GetDescription())
	listener.Labels = domain.LabelsFromMap(req.GetLabels())
	// NLB-1b MIGRATE: wire the (already prechecked) authoritative targetGroupId.
	if tgID != "" {
		listener.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID(tgID))
	}
	// VIP-only LB-консолидация: листенер не аллоцирует адрес, поэтому терминальное
	// состояние Create — сразу ACTIVE (durable-handle/CREATING-фаза не нужна).
	listener.Status = domain.ListenerStatusActive

	if err := listener.Validate(); err != nil {
		return nil, err
	}

	// Учёт числа ресурсов: слушатель считается ДВАЖДЫ — в своём балансировщике
	// и в проекте. Порядок вопросов тот же, что порядок списания (V2-6:
	// сначала носитель-родитель), иначе ранний отказ называл бы не ту ось, по
	// которой откажет триггер.
	if u.quota != nil {
		if err := u.quota.AdmitCarrier(ctx,
			"loadbalancer.networkLoadBalancers.listeners",
			string(listener.LoadBalancerID),
			"loadbalancer.networkLoadBalancers.listeners"); err != nil {
			return nil, mapDomainErr(err)
		}
		if err := u.quota.Admit(ctx, string(listener.ProjectID), "loadbalancer.listeners"); err != nil {
			return nil, mapDomainErr(err)
		}
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
// wired targetGroupId references a TargetGroup of the parent LB's OWN project,
// authorized to the caller, and region-coherent with the LB. Unresolved (missing
// OR owned by another project — deliberately indistinguishable, see tg_ref.go) →
// actionable FAILED_PRECONDITION (guides the client to create the TG first or use
// the one-shot LB.Create); unauthorized → PermissionDenied; region mismatch →
// region-coherence FAILED_PRECONDITION. The composite FK (0018/0023) is the atomic
// backstop for the precheck→worker race.
func (u *CreateUseCase) prevalidateTargetGroup(ctx context.Context, tgID, lbProject, lbRegion string) error {
	if tgID == "" {
		return nil
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()
	tg, err := lookupWiredTargetGroup(ctx, rd.TargetGroups(), tgID, lbProject)
	if err != nil {
		if errors.Is(err, errTargetGroupUnresolved) {
			return status.Error(codes.FailedPrecondition,
				"listener requires an existing targetGroupId; create the TargetGroup first "+
					"(POST /nlb/v1/targetGroups) or use one-shot NetworkLoadBalancer.Create")
		}
		return mapDomainErr(err)
	}
	// Object-scoped authz AFTER the owning-project resolve — a cross-project id must
	// stay hidden as a miss (a PermissionDenied here would confirm it exists).
	if err := checkTargetGroupViewer(ctx, u.checkClient, tgID); err != nil {
		return err
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

// createInput — snapshot входов для async worker.
type createInput struct {
	listener domain.Listener
	lb       *parentLB
}

// doCreate — worker: одна writer-TX (внешнего side-effect нет). INSERT листенера
// (status='ACTIVE') + outbox CREATED + LB UPDATED + FGA-register-intent
// (project-hierarchy). Триггер lb_status_recompute сам переводит LB INACTIVE→ACTIVE при
// has_listener AND has_attached. Возвращает anypb.Any(Listener) при успехе.
func (u *CreateUseCase) doCreate(ctx context.Context, in createInput) (*anypb.Any, error) {
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
		kachorepo.OutboxResourceListener, string(created.ID), string(created.ProjectID),
		kachorepo.OutboxActionCreated, kachorepo.ListenerStatePayload(created),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit listener CREATED: %v", domain.ErrInternal, err))
	}
	// Запись родителя читается ЗАНОВО, после вставки: `in.lb` снят ДО неё, а
	// триггер пересчёта статуса срабатывает внутри самого оператора вставки —
	// значит снимок «до» объявлял бы прежний статус на строке, которую эта же
	// транзакция только что сдвинула. Разбор — в соседнем `helpers.go` этого пакета.
	lbAfter, err := w.LoadBalancers().Get(ctx, string(in.lb.ID))
	if err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: read parent load balancer after insert: %v", domain.ErrInternal, err))
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceLoadBalancer, string(lbAfter.ID), string(lbAfter.ProjectID),
		kachorepo.OutboxActionUpdated, kachorepo.LoadBalancerStatePayload(lbAfter),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit lb UPDATED: %v", domain.ErrInternal, err))
	}
	// Намерение строится ОДИН раз и доставляется обеими доставками — этой и
	// дренажом той же строки. Прежде его собирали дважды: очередь получала
	// одно значение, синхронный вызов — второе, построенное заново, и
	// разойтись они могли бы молча.
	intent := listenerRegisterIntent(created)
	intentVersion, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, intent)
	if err != nil {
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
	u.syncRegister(ctx, intent, intentVersion)

	return marshalListener(created)
}

// syncRegister — BEST-EFFORT sync owner-tuple регистрация после durable commit.
// Ошибка ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable fga_register_outbox-intent +
// register-drainer — at-least-once backstop; Operation.done НЕ гейтится (ban #9).
// nil registrar → no-op.
func (u *CreateUseCase) syncRegister(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent, intentVersion); err != nil {
		loggerOrDiscard(u.logger).Warn("Listener.Create sync owner-tuple registration incomplete; register-drainer will reconcile",
			"err", err, "listener_id", intent.ResourceID)
	}
}

// listenerRegisterIntent — FGA-register-intent для созданного Listener:
// project-hierarchy tuple, несущий labels + parent-project, чтобы kacho-iam
// обновил resource_mirror (γ-селекторы matchLabels). source_version штампует
// outbox-emitter из DB-clock.
//
// ONLY PROXY-REGISTRABLE TUPLES BELONG IN A DURABLE INTENT. kacho-iam's
// least-privilege proxy policy is declared in pkg/authz/proxytuple and decides the
// whole tuple, not the relation alone; privilege relations are writable only by the
// AccessBinding flow, never by a module proxy. So the creator (`admin`) and
// parent-link (`load_balancer`) tuples this intent used to carry were refused on
// EVERY delivery and never once landed in FGA — the model's own `load_balancer`
// is documented as a structural pointer, not an access cascade, and creator access
// comes from IAM's per-object materialisation (flat Contract-A), not from a
// module-written admin tuple.
//
// Carrying them cost the registration itself. A refusal from the model owner is
// TERMINAL: the applier maps it to drainer.ErrPermanent
// (clients/iam/register_applier.go) and the shared drainer classifies it the same
// way for every service (pkg/outbox/drainer/classify.go). Such a Create row
// therefore poisons on its FIRST attempt and drops out of the partition-head
// blocking set at once — it does not hold the listener's later intents (notably
// the labels-refresh that revokes an ARM_LABELS grant) for any deadline. It does
// stay undelivered until reconciler.RedrivePoisoned comes back for it, and the
// applier stops at the first rejection, so nothing after the rejected tuple ships.
// Ordering the project tuple first only ensured the mirror got written before the
// rejection; it did not let the row complete.
func listenerRegisterIntent(l *kachorepo.ListenerRecord) domain.FGARegisterIntent {
	id := string(l.ID)
	return domain.FGARegisterIntent{
		Kind:       "Listener",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeListener, id, string(l.ProjectID)),
		},
		Labels:          domain.LabelsToMap(l.Labels),
		ParentProjectID: string(l.ProjectID),
	}
}

// listenerUnregisterIntent — FGA-unregister-intent (project-hierarchy) для
// удалённого Listener (creator оставляется IAM-side GC).
//
// The tuple must be the project one for the same reason listenerMirrorIntent's
// must (see update.go): UnregisterResource runs the identical least-privilege
// rule (pkg/authz/proxytuple.ValidateTuple), and the parent-link relation
// `load_balancer` is not registrable. A retraction built from it is rejected before the resource_mirror
// DELETE, so the mirror row of a DELETED listener survives and the reconciler
// keeps re-materialising per-object grants from it — a deleted listener retaining
// its access. Parity with lbUnregisterIntent / tgUnregisterIntent, which already
// retract via the project tuple.
func listenerUnregisterIntent(listenerID, projectID string) domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:       "Listener",
		ResourceID: listenerID,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeListener, listenerID, projectID),
		},
	}
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateUseCase) WithQuotaGuard(g QuotaGuard) *CreateUseCase {
	u.quota = g
	return u
}
