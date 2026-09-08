// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// CreateTargetGroupUseCase — async Create TG.
//
// Sync part:
//   - required: project_id, region_id, health_check;
//   - domain.TargetGroup.Validate (name regex, HC oneof + bounds, dereg/slow_start ranges, per-target oneof + bogon-check);
//   - sync duplicate-name check (project_id+name) → AlreadyExists;
//   - operations.New + opsRepo.CreateWithPrincipal → return Operation.
//
// Async worker:
//   - peer-check project_id (iam ProjectService.Get);
//   - peer-check region_id (geo RegionService.Get);
//   - Writer-TX → Insert TG (+ inline targets) + outbox CREATED +
//     FGARegisterOutbox.Emit(fga.register) → Commit (Вариант A: owner-
//     hierarchy + creator tuple intent written in the SAME tx as Insert — no
//     dual-write; register-drainer applies it through kaname).
//
// Note про inline targets (+): per-target peer-resolve
// (instance/nic/ip_ref existence + region match) делается AddTargets'ом, не
// здесь — говорит «если instance не существует,
// worker rolls back TX и TG не создаётся». Делегируем работу: после Insert
// TG в той же transaction раскрываем targets через AddTargets-логику peer-validate
// inline (worker уже зашёл в TX); чтобы избежать TX-pollution валидацией peer-
// gRPC-вызовов (long IO внутри открытой DB-TX) — peer-validate делаем ДО открытия
// Writer-TX, а сам Insert (включая targets) — в single Writer-TX.
type CreateTargetGroupUseCase struct {
	// quota — совещательная полоса учёта числа ресурсов.
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции),
	// поэтому провязка обязательна на любом поднятом стенде; отсутствие
	// допустимо только там, где нет и соседа, у которого спрашивать величины.
	quota QuotaGuard

	repo          Repo
	opsRepo       OpsRepo
	projectClient ProjectClient
	regionClient  RegionClient
	// registrar — sync-primary owner-tuple registrar (kaname RegisterResource),
	// вызывается BEST-EFFORT после durable commit TG. nil → только async
	// register-drainer. См. WithRegistrar.
	registrar Registrar
	logger    *slog.Logger
}

// NewCreateTargetGroupUseCase конструктор.
func NewCreateTargetGroupUseCase(
	repo Repo, opsRepo OpsRepo,
	pc ProjectClient, rc RegionClient,
	logger *slog.Logger,
) *CreateTargetGroupUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreateTargetGroupUseCase{
		repo: repo, opsRepo: opsRepo,
		projectClient: pc, regionClient: rc,
		logger: logger,
	}
}

// WithRegistrar подключает sync-primary owner-tuple registrar. После durable
// commit TG (+ его `fga_register_outbox`-intent'а) те же owner/containment-tuple'ы
// синхронно регистрируются в kaname — grant создателя доступен сразу.
// BEST-EFFORT: сбой sync-Register логируется и глотается (durable intent +
// drainer — backstop), Operation.done НЕ гейтится (ban #9). Возвращает self.
func (u *CreateTargetGroupUseCase) WithRegistrar(r Registrar) *CreateTargetGroupUseCase {
	u.registrar = r
	return u
}

// Execute — sync validate + ops insert + spawn worker.
func (u *CreateTargetGroupUseCase) Execute(
	ctx context.Context, req *lbv1.CreateTargetGroupRequest,
) (*operations.Operation, error) {
	// ---- Sync validation ----
	if req.GetProjectId() == "" {
		return nil, errInvalidArg("project_id", "required")
	}
	if req.GetRegionId() == "" {
		return nil, errInvalidArg("region_id", "required")
	}
	if req.GetHealthCheck() == nil {
		return nil, errInvalidArg("health_check", "required")
	}

	tg := domain.NewTargetGroup(
		domain.ProjectID(req.GetProjectId()),
		domain.RegionID(req.GetRegionId()),
		domain.LbName(req.GetName()),
		domain.LbDescription(req.GetDescription()),
		domain.LabelsFromMap(req.GetLabels()),
	)
	hc, err := healthCheckFromPb(req.GetHealthCheck())
	if err != nil {
		return nil, mapDomainErr(err)
	}
	tg.HealthCheck = hc
	tg.Port = domain.LbPort(req.GetPort())
	inlineTargets, err := targetsFromPbForWrite("targets", req.GetTargets())
	if err != nil {
		return nil, err
	}
	tg.Targets = inlineTargets
	// Defaults via builder уже выставлены — override только если caller прислал
	// значение (proto message-nil === «не задано»; NLB-1c B8 Duration).
	if d := req.GetDeregistrationDelay(); d != nil {
		tg.DeregistrationDelay = domain.LbDuration(d.AsDuration())
	}
	if d := req.GetSlowStart(); d != nil {
		tg.SlowStart = domain.LbDuration(d.AsDuration())
	}
	if err := tg.Validate(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Sync duplicate-name check (best-effort UX; UNIQUE-violation в worker'е —
	// атомарный backstop).
	//
	// Условия «имя непусто» здесь нет — см. тот же разбор в create.go балансировщика:
	// tg.Validate() строкой выше отвергает пустое имя, и ветка недостижима.
	if err := u.assertNameUnique(ctx, string(tg.ProjectID), string(tg.Name)); err != nil {
		return nil, err
	}

	// Учёт числа ресурсов: ранний отказ ДО создания операции (см. разбор в
	// пакете `apps/kacho/quota`).
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(tg.ProjectID), "loadbalancer.targetGroups"); err != nil {
			return nil, mapDomainErr(err)
		}
	}

	// ---- Operation row ----
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Create TargetGroup %s", tg.Name),
		&lbv1.CreateTargetGroupMetadata{TargetGroupId: string(tg.ID)},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	// Durable commit → op done сразу. Owner-tuple TargetGroup материализуется
	// eventually-consistent (writer-TX fga_register_outbox intent → register-
	// drainer → kaname RegisterResource → reconciler backstop); Operation.done
	// означает durability ресурса, не видимость owner-tuple в FGA.
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doCreate(workerCtx, tg, principal)
	})
	return &op, nil
}

// doCreate — async worker: peer-check + Writer-TX + outbox + FGA-register-intent
// + Commit (intent in the same tx, applied async by register-drainer).
func (u *CreateTargetGroupUseCase) doCreate(
	ctx context.Context, tg domain.TargetGroup, principal operations.Principal,
) (*anypb.Any, error) {
	// 1. Peer-check project_id.
	if u.projectClient != nil {
		if _, err := u.projectClient.Get(ctx, string(tg.ProjectID)); err != nil {
			return nil, peerErrToStatus(err, "project", string(tg.ProjectID))
		}
	}
	// 2. Peer-check region_id.
	if u.regionClient != nil {
		if _, err := u.regionClient.Get(ctx, string(tg.RegionID)); err != nil {
			return nil, peerErrToStatus(err, "region", string(tg.RegionID))
		}
	}

	// 3. Writer-TX: Insert TG (+ inline targets) + outbox CREATED + Commit.
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	created, err := w.TargetGroups().Insert(ctx, &tg)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceTargetGroup, string(created.ID), string(created.ProjectID),
		kachorepo.OutboxActionCreated, kachorepo.TargetGroupStatePayload(created),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// FGA-register-intent (project-hierarchy) in the SAME tx.
	// Намерение строится ОДИН раз и доставляется обеими доставками — этой и
	// дренажом той же строки. Прежде его собирали дважды: очередь получала
	// одно значение, синхронный вызов — второе, построенное заново, и
	// разойтись они могли бы молча.
	intent := tgRegisterIntent(created)
	intentVersion, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, intent)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Sync-primary owner-tuple registration (после durable commit TG + его
	// fga_register_outbox-intent'а): grant создателя виден сразу, закрывая
	// async-only окно. BEST-EFFORT — сбой логируется и глотается (durable intent
	// + register-drainer — backstop); Operation.done НЕ гейтится (ban #9).
	u.syncRegister(ctx, intent, intentVersion)

	// 4. Marshal response.
	return marshalTargetGroup(created)
}

// syncRegister — BEST-EFFORT sync owner-tuple регистрация после durable commit.
// Ошибка ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable fga_register_outbox-intent +
// register-drainer — at-least-once backstop; Operation.done НЕ гейтится (ban #9).
// nil registrar → no-op.
func (u *CreateTargetGroupUseCase) syncRegister(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent, intentVersion); err != nil {
		u.logger.Warn("TargetGroup.Create sync owner-tuple registration incomplete; register-drainer will reconcile",
			"err", err, "target_group_id", intent.ResourceID)
	}
}

// assertNameUnique — sync precheck дубликата (project_id, name).
func (u *CreateTargetGroupUseCase) assertNameUnique(ctx context.Context, projectID, name string) error {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()

	existing, _, err := rd.TargetGroups().List(ctx,
		kachorepo.TargetGroupFilter{ProjectID: projectID, Name: kachorepo.ExactName(name)},
		kachorepo.Pagination{},
	)
	if err != nil {
		return mapDomainErr(err)
	}
	if len(existing) > 0 {
		return status.Errorf(codes.AlreadyExists,
			"TargetGroup '%s' already exists in project %s", name, projectID)
	}
	return nil
}

// tgRegisterIntent builds the FGA-register-intent for a created
// TargetGroup: the project-hierarchy tuple, carrying tenant labels +
// parent-project so kaname feeds its resource_mirror (γ selector matchLabels /
// containment). source_version is stamped by the outbox emitter from the DB clock
// inside the writer-tx.
//
// A durable intent carries ONLY proxy-registrable tuples. kaname's
// least-privilege policy accepts the ownership/parent relations declared in
// pkg/authz/proxytuple and reserves
// privilege relations for the AccessBinding flow, so the creator (`admin`) tuple
// this used to append was refused on every delivery.
//
// A refusal from the model owner is TERMINAL: the applier maps it to
// drainer.ErrPermanent (clients/iam/register_applier.go) and the shared drainer
// classifies it the same way for every service (pkg/outbox/drainer/classify.go).
// So such a row poisons on its FIRST attempt and leaves the partition-head
// blocking set at once — it does not hold later intents for this target group for
// any deadline. What it costs is the registration: the applier stops at the first
// rejection, so nothing after the rejected tuple ships, and the row stays
// undelivered until reconciler.RedrivePoisoned comes back for it. Creator access
// is materialised per-object by IAM's reconciler (flat Contract-A), not by a
// module-written admin tuple.
func tgRegisterIntent(tg *kachorepo.TargetGroupRecord) domain.FGARegisterIntent {
	id := string(tg.ID)
	return domain.FGARegisterIntent{
		Kind:       "TargetGroup",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, id, string(tg.ProjectID)),
		},
		Labels:          domain.LabelsToMap(tg.Labels),
		ParentProjectID: string(tg.ProjectID),
	}
}

// tgMirrorIntent builds the mirror-feed register-intent for an
// UPDATED TargetGroup: the project-hierarchy tuple (re-register is idempotent in
// IAM) carrying the refreshed labels + parent so kaname updates its
// resource_mirror. No creator tuple — Update never re-assigns ownership; this is a
// pure labels-refresh feed. source_version is stamped by the outbox emitter.
func tgMirrorIntent(tg *kachorepo.TargetGroupRecord) domain.FGARegisterIntent {
	id := string(tg.ID)
	return domain.FGARegisterIntent{
		Kind:       "TargetGroup",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, id, string(tg.ProjectID)),
		},
		Labels:          domain.LabelsToMap(tg.Labels),
		ParentProjectID: string(tg.ProjectID),
	}
}

// tgUnregisterIntent builds the FGA-unregister-intent (project-hierarchy)
// for a deleted/moved TargetGroup.
func tgUnregisterIntent(id, projectID string) domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:       "TargetGroup",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, id, projectID),
		},
	}
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateTargetGroupUseCase) WithQuotaGuard(g QuotaGuard) *CreateTargetGroupUseCase {
	u.quota = g
	return u
}
