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

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// MoveTargetGroupUseCase — cross-project move.
//
// Sync prechecks:
//   - same-project ("destination project is the same as source") → InvalidArgument;
//   - ReferencingListenerIDs non-empty → FailedPrecondition с фиксированным текстом
//     `"target group is referenced by N listener(s); repoint them before moving"`;
//   - destination project exists (peer ProjectClient.Get) — InvalidArgument если NotFound.
//
// Worker:
//   - Writer-TX → MoveProject (UPDATE project_id) + outbox MOVED + outbox
//     UPDATED + FGA-register(dst project) + FGA-unregister(src project) → Commit
//     (Вариант A: project-rewrite in the SAME tx as MoveProject).
//
// Destination-project authorization (`editor on project:<dst>`) — handler-side
// Check via checkClient: the per-RPC interceptor authorizes
// only the source TG, so the caller's grant on the destination is verified here.
type MoveTargetGroupUseCase struct {
	repo          Repo
	opsRepo       OpsRepo
	projectClient ProjectClient
	checkClient   CheckClient
	logger        *slog.Logger
}

// NewMoveTargetGroupUseCase конструктор. checkClient авторизует caller'а на
// destination project (`editor on project:<dst>`). nil НЕ означает «пропустить»:
// отсутствие решателя — отказ (`Unavailable`), см. shared.AuthorizeObject.
func NewMoveTargetGroupUseCase(
	repo Repo, opsRepo OpsRepo,
	pc ProjectClient, checkClient CheckClient, logger *slog.Logger,
) *MoveTargetGroupUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &MoveTargetGroupUseCase{
		repo: repo, opsRepo: opsRepo,
		projectClient: pc, checkClient: checkClient, logger: logger,
	}
}

// Execute — sync prechecks + ops insert + spawn worker.
func (u *MoveTargetGroupUseCase) Execute(
	ctx context.Context, req *lbv1.MoveTargetGroupRequest,
) (*operations.Operation, error) {
	id := req.GetTargetGroupId()
	if id == "" {
		return nil, errInvalidArg("target_group_id", "required")
	}
	if err := validateTargetGroupID(id); err != nil {
		return nil, err
	}
	dst := req.GetDestinationProjectId()
	if dst == "" {
		return nil, errInvalidArg("destination_project_id", "required")
	}

	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.TargetGroups().Get(ctx, id)
	if err != nil {
		_ = rd.Close()
		return nil, mapDomainErr(err)
	}
	if string(cur.ProjectID) == dst {
		_ = rd.Close()
		return nil, status.Error(codes.InvalidArgument,
			"destination project is the same as source")
	}
	lstIDs, err := rd.TargetGroups().ReferencingListenerIDs(ctx, id)
	if err != nil {
		_ = rd.Close()
		return nil, mapDomainErr(err)
	}
	if len(lstIDs) > 0 {
		_ = rd.Close()
		return nil, status.Errorf(codes.FailedPrecondition,
			"target group is referenced by %d listener(s); repoint them before moving", len(lstIDs))
	}
	_ = rd.Close()

	// Peer-check destination project.
	if u.projectClient != nil {
		if _, err := u.projectClient.Get(ctx, dst); err != nil {
			return nil, peerErrToStatus(err, "project", dst)
		}
	}

	// Destination-project authorization (CWE-862/863): the
	// per-RPC interceptor authorizes the caller on the SOURCE TG only; the caller
	// must ALSO hold `editor` on the destination project, else it could inject
	// the TG into a victim's project.
	if err := u.authorizeDestination(ctx, dst); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Move TargetGroup %s -> %s", id, dst),
		&lbv1.MoveTargetGroupMetadata{
			TargetGroupId:        id,
			DestinationProjectId: dst,
		},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}
	srcProject := string(cur.ProjectID)
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doMove(workerCtx, id, srcProject, dst)
	})
	return &op, nil
}

// authorizeDestination авторизует caller'а на destination project
// (`editor on project:<dst>`). Fail-closed посадка (решателя нет / вызывающего
// нельзя назвать → отказ, никогда не пропуск) живёт в `shared.AuthorizeObject` —
// одно правило на все пообъектные решения сервиса.
func (u *MoveTargetGroupUseCase) authorizeDestination(ctx context.Context, dst string) error {
	return shared.AuthorizeObject(ctx, u.checkClient,
		domain.FGARelationEditor,
		domain.FGAObjectRef(domain.FGAObjectTypeProject, dst),
		fmt.Sprintf("caller is not authorized (editor) on destination project %s", dst))
}

// doMove — worker: Writer-TX → MoveProject + outbox MOVED + UPDATED → Commit → FGA rewrite.
func (u *MoveTargetGroupUseCase) doMove(ctx context.Context, id, srcProject, dstProject string) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	moved, err := w.TargetGroups().MoveProject(ctx, id, dstProject)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceTargetGroup, string(moved.ID), string(moved.ProjectID),
		kachorepo.OutboxActionMoved, tgMovedPayload(string(moved.ID), srcProject, dstProject),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// UPDATED — для downstream watchers, не подписанных на MOVED.
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceTargetGroup, string(moved.ID), string(moved.ProjectID),
		kachorepo.OutboxActionUpdated, tgOutboxPayload(moved),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// project-rewrite as unregister(src) THEN register(dst) in the SAME tx.
	//
	// ORDER IS PART OF THE CONTRACT, not style. Both intents are about the SAME
	// object, so they land in one drainer partition (resource_id) and are applied
	// in emission order, and the emitter stamps each with a strictly newer
	// source_version than the previous intent of this tx. The surviving state must
	// therefore be emitted LAST: unregister takes down the source scope, register
	// then puts the destination scope in place. Swap them and the unregister —
	// a hard DELETE in IAM, gated `source_version <= tombstone` — carries the newer
	// version and removes the projection the register has just written, leaving the
	// moved target group with no access at all while the Move itself reports success.
	//
	// unregister(src) stays bare (IAM uses only object + source_version on
	// unregister). register(dst) must carry full tgMirrorIntent semantics (Labels
	// from the moved record + ParentProjectID=dst) so the kacho-iam resource_mirror
	// feeding the γ label/parent selector is re-created intact.
	if _, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventUnregister,
		tgUnregisterIntent(id, srcProject)); err != nil {
		return nil, mapDomainErr(err)
	}
	if _, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister,
		tgMirrorIntent(moved)); err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	return marshalTargetGroup(moved)
}
