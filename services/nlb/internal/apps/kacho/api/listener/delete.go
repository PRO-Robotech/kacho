// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// DeleteUseCase — async Delete Listener.
//
// Sync (handler-thread):
//  1. listener_id required.
//  2. Listener.Get — NotFound иначе.
//  3. opsRepo.CreateWithPrincipal + operations.Run.
//
// Async worker:
//  1. Listener.SetStatusCAS(<current> → DELETING) — атомарный transient marker;
//     parallel UPDATE/DELETE losses race fast.
//  2. repo.Writer.Listeners.Delete + 2× outbox emit (`nlb_listener:<id> DELETED`
//     + `nlb_load_balancer:<lb_id> UPDATED`).
//  3. ops.MarkDone(response=Empty).
//
// VIP листенер НЕ освобождает: адрес принадлежит родительскому LoadBalancer'у
// (один anycast-VIP на семейство) и освобождается его собственным Delete /
// create-compensation / free_ip_runner'ом (тот сканирует ТОЛЬКО load_balancers).
// Прежняя release-ветка листенера читала `listeners.address_id`, который ни один
// production-путь никогда не заполнял (единственные писатели `SetVIP`/
// `SetAllocatedAddress` не имели вызывающих) — ветка была недостижима и снята
// вместе с колонками (миграция 0028).
type DeleteUseCase struct {
	repo    RepoFactory
	opsRepo OperationsRepo
	logger  *slog.Logger
}

// NewDeleteUseCase — конструктор.
func NewDeleteUseCase(
	repo RepoFactory,
	opsRepo OperationsRepo,
	logger *slog.Logger,
) *DeleteUseCase {
	return &DeleteUseCase{
		repo:    repo,
		opsRepo: opsRepo,
		logger:  logger,
	}
}

// Run — sync validate + spawn worker.
func (u *DeleteUseCase) Run(ctx context.Context, req *lbv1.DeleteListenerRequest) (*operations.Operation, error) {
	id := req.GetListenerId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "listener_id required")
	}
	if err := validateListenerID(id); err != nil {
		return nil, err
	}

	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.Listeners().Get(ctx, id)
	if err != nil {
		_ = rd.Close()
		return nil, mapDomainErr(err)
	}
	_ = rd.Close()

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Delete listener %s", string(cur.Name)),
		&lbv1.DeleteListenerMetadata{
			ListenerId:     string(cur.ID),
			LoadBalancerId: string(cur.LoadBalancerID),
		},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	snap := *cur
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doDelete(workerCtx, &snap)
	})
	return &op, nil
}

// doDelete — worker flow.
func (u *DeleteUseCase) doDelete(ctx context.Context, cur *kachorepo.ListenerRecord) (*anypb.Any, error) {
	listenerID := string(cur.ID)
	lbID := string(cur.LoadBalancerID)
	projectID := string(cur.ProjectID)
	regionID := string(cur.RegionID)

	// Step 1: mark DELETING (atomic CAS — protects against parallel writers).
	// We accept any non-DELETING current status. Mutex (CAS-style) writes
	// `status='DELETING'` если был ACTIVE / CREATING / UPDATING; если уже
	// DELETING — повторный Delete-worker идемпотентно проходит дальше.
	if cur.Status != domain.ListenerStatusDeleting {
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
		_, err = w.Listeners().SetStatusCAS(ctx, listenerID, cur.Status, domain.ListenerStatusDeleting)
		if err != nil {
			// CAS-miss (e.g. parallel writer already moved to DELETING) →
			// proceed anyway (read current status); else propagate.
			if !errors.Is(err, domain.ErrFailedPrecondition) {
				return nil, mapDomainErr(err)
			}
		}
		if err := w.Outbox().Emit(ctx,
			outboxResourceTypeListener, listenerID, projectID,
			outboxActionUpdated, listenerPayloadMap(cur),
		); err != nil {
			return nil, mapDomainErr(fmt.Errorf("%w: outbox emit listener UPDATED: %v", domain.ErrInternal, err))
		}
		if err := w.Commit(); err != nil {
			return nil, mapDomainErr(err)
		}
		committed = true
	}

	// Step 2: DELETE listener row + 2× outbox emit + Commit atomically.
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
	if err := w.Listeners().Delete(ctx, listenerID); err != nil {
		// ErrNotFound — idempotent (двойной Delete): продолжаем, emit DELETED
		// для consumers (idempotency).
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, mapDomainErr(err)
		}
	}
	if err := w.Outbox().Emit(ctx,
		outboxResourceTypeListener, listenerID, projectID,
		outboxActionDeleted, listenerPayloadMap(cur),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit listener DELETED: %v", domain.ErrInternal, err))
	}
	if err := w.Outbox().Emit(ctx,
		outboxResourceTypeLoadBalancer, lbID, projectID,
		outboxActionUpdated, lbUpdatedPayloadMap(lbID, projectID, regionID, "listener_deleted"),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit lb UPDATED: %v", domain.ErrInternal, err))
	}
	// FGA-unregister-intent (parent-link) in the SAME tx as the Delete —
	// register-drainer removes the parent-link tuple via IAM.UnregisterResource.
	if err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventUnregister,
		listenerUnregisterIntent(listenerID, lbID)); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: fga unregister-intent emit: %v", domain.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	committed = true

	any, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return any, nil
}
