// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"fmt"
	"log/slog"

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
//     dual-write; register-drainer applies it through kacho-iam).
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
	repo          Repo
	opsRepo       OpsRepo
	projectClient ProjectClient
	regionClient  RegionClient
	logger        *slog.Logger

	// confirmer — read-after-register проба owner-tuple (owner-tuple opgate). При
	// non-nil Create-op становится `done=true, response` только после подтверждения
	// owner-tuple TargetGroup в FGA (окно 403 на немедленной мутации создателя закрыто).
	// nil → confirm-gate выключен (прежнее поведение — op done сразу после worker-fn).
	confirmer OwnerTupleConfirmer

	// dispatch — точка запуска async Create-worker'а с confirm-gate. Дефолт —
	// operations.RunWithConfirm; тест инжектит Worker с коротким deadline (OTG-05).
	dispatch confirmDispatcher
}

// OwnerTupleConfirmer — read-after-register проба owner-tuple для confirm-gate
// Create-op (owner-tuple opgate). confirmed=true, когда owner-tuple созданной
// TargetGroup эффективен в FGA для creator'а (gateway scope_extractor Check немедленной
// мутации `creator #v_update lb_target_group:<id>` вернёт ALLOW). Реализация —
// check.NewTargetGroupOwnerConfirmer (reuse iamclient.CheckClient, без нового ребра).
// nil → confirm-gate выключен.
type OwnerTupleConfirmer interface {
	Confirm(ctx context.Context, creator operations.Principal, resourceID string) (bool, error)
}

// confirmDispatcher — сигнатура диспетча Create-op с confirm-gate (owner-tuple
// opgate). Совпадает с operations.RunWithConfirm; confirm==nil ≡ operations.Run.
type confirmDispatcher func(ctx context.Context, opsRepo operations.Repo, opID string,
	fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc)

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
		logger:   logger,
		dispatch: operations.RunWithConfirm,
	}
}

// WithConfirmer подключает read-after-register confirmer owner-tuple (owner-tuple
// opgate): Create-op достигает success-`done` только после подтверждения owner-tuple
// в FGA — окно 403 на немедленной мутации создателя закрыто. Nil → confirm-gate выключен.
func (u *CreateTargetGroupUseCase) WithConfirmer(c OwnerTupleConfirmer) *CreateTargetGroupUseCase {
	u.confirmer = c
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
	tg.Targets = targetsFromPb(req.GetTargets())
	// Defaults via builder уже выставлены — override только если caller прислал
	// non-zero значение (proto numeric zero === «не задано»).
	if v := req.GetDeregistrationDelaySeconds(); v != 0 {
		tg.DeregistrationDelaySeconds = v
	}
	if v := req.GetSlowStartSeconds(); v != 0 {
		tg.SlowStartSeconds = v
	}
	if err := tg.Validate(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Sync duplicate-name check (best-effort UX; UNIQUE-violation в worker'е —
	// атомарный backstop).
	if string(tg.Name) != "" {
		if err := u.assertNameUnique(ctx, string(tg.ProjectID), string(tg.Name)); err != nil {
			return nil, err
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

	// Confirm-gate owner-tuple (owner-tuple opgate): при подключённом confirmer op
	// достигает success-`done` только после read-after-register подтверждения
	// owner-tuple TargetGroup. creator = principal. nil confirmer → confirm=nil →
	// RunWithConfirm ≡ Run (back-compat).
	var confirm operations.ConfirmFunc
	if u.confirmer != nil {
		tgID := string(tg.ID)
		confirm = func(cctx context.Context) (bool, error) {
			return u.confirmer.Confirm(cctx, principal, tgID)
		}
	}
	u.dispatch(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doCreate(workerCtx, tg, principal)
	}, confirm)
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
		kachorepo.OutboxActionCreated, tgOutboxPayload(created),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// FGA-register-intent (project-hierarchy + creator) in the SAME tx.
	if err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister,
		tgRegisterIntent(created, principal)); err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	// 4. Marshal response.
	return marshalTargetGroup(created)
}

// assertNameUnique — sync precheck дубликата (project_id, name).
func (u *CreateTargetGroupUseCase) assertNameUnique(ctx context.Context, projectID, name string) error {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()

	existing, _, err := rd.TargetGroups().List(ctx,
		kachorepo.TargetGroupFilter{ProjectID: projectID, Name: name},
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
// TargetGroup: project-hierarchy tuple plus, for an authenticated (non-system)
// principal, a creator (admin) tuple (skipped on empty subject).
func tgRegisterIntent(tg *kachorepo.TargetGroupRecord, principal operations.Principal) domain.FGARegisterIntent {
	id := string(tg.ID)
	tuples := []domain.FGATuple{
		domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, id, string(tg.ProjectID)),
	}
	if subject := domain.FGASubjectFromPrincipal(principal.Type, principal.ID); subject != "" {
		tuples = append(tuples, domain.FGACreatorTuple(subject, domain.FGAObjectTypeTargetGroup, id))
	}
	// carry tenant labels + parent-project so kacho-iam feeds its
	// resource_mirror (γ selector matchLabels / containment). source_version is
	// stamped by the outbox emitter from the DB clock inside the writer-tx.
	return domain.FGARegisterIntent{
		Kind:            "TargetGroup",
		ResourceID:      id,
		Tuples:          tuples,
		Labels:          domain.LabelsToMap(tg.Labels),
		ParentProjectID: string(tg.ProjectID),
	}
}

// tgMirrorIntent builds the mirror-feed register-intent for an
// UPDATED TargetGroup: the project-hierarchy tuple (re-register is idempotent in
// IAM) carrying the refreshed labels + parent so kacho-iam updates its
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
