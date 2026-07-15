// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
)

// OperationHandler реализует operationpb.OperationServiceServer для каталога
// kacho-geo: клиент поллит Get(operation_id) до done=true после async-мутации
// Region/Zone. Регистрируется на обоих листенерах (public read-poll + internal).
//
// Get/Cancel энфорсят ВЛАДЕЛЬЦА операции. Владелец — principal, создавший
// операцию (колонки principal_type/principal_id LRO-строки, проставляются из
// доверенного ctx на Create). `operation_id` опакен, но это прямой
// object-reference (BOLA-поверхность): без owner-проверки любой caller,
// достижимый на любом листенере (OperationService помечен Public:true → per-RPC
// ReBAC-Check с него снят), узнав чужой op-id, прочитал бы результат чужой
// admin-мутации Region/Zone или ОТМЕНИЛ бы её in-flight (integrity/availability
// удар по control-plane). Поэтому ownership-предикат энфорсится тут через
// ownership-scoped repo (GetOwned/CancelOwned — предикат в SQL WHERE, within-
// service инвариант на DB-уровне, без software TOCTOU). Чужой/несуществующий id
// отдаёт ОДИНАКОВЫЙ NotFound (no-leak: «есть, но не твоя» неотличимо от «нет
// такой» — не создаём existence-oracle).
//
// В geo нет tenant/cluster-admin-обхода (все мутации и так требуют system_admin;
// операция принадлежит своему создателю-админу) — owner-скоуп строгий.
type OperationHandler struct {
	operationpb.UnimplementedOperationServiceServer
	repo operations.Repo
}

// NewOperationHandler создает OperationHandler. В проде repo — pgRepo, который
// реализует operations.OwnedOperationRepo; если не реализует (ошибка wiring'а) —
// ownership-вызовы возвращают INTERNAL (fail-closed, НЕ silent-bypass owner-гейта).
func NewOperationHandler(repo operations.Repo) *OperationHandler {
	return &OperationHandler{repo: repo}
}

// Get возвращает текущее состояние операции (done/error/response) ТОЛЬКО её
// владельцу; чужой/несуществующий id → NotFound (no-leak).
func (h *OperationHandler) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	if req.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		// Wiring-инвариант нарушен: fail-closed, не отдаём операцию без owner-гейта.
		return nil, status.Error(codes.Internal, "operation get failed")
	}
	owner := operations.OwnerFromPrincipal(operations.PrincipalFromContext(ctx))
	op, err := owned.GetOwned(ctx, req.GetOperationId(), owner)
	if err != nil {
		return nil, mapOpErr(err, req.GetOperationId(), "operation get failed")
	}
	return operationToProto(op), nil
}

// Cancel отменяет ещё не завершённую операцию (done=true, code=CANCELLED) ТОЛЬКО
// её владельцу; чужой/несуществующий id → NotFound (no-leak). Идемпотентно на
// уже-CANCELLED; на терминале SUCCESS/ERROR → FailedPrecondition.
func (h *OperationHandler) Cancel(ctx context.Context, req *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	if req.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		return nil, status.Error(codes.Internal, "operation cancel failed")
	}
	owner := operations.OwnerFromPrincipal(operations.PrincipalFromContext(ctx))
	// CancelOwned — атомарный CAS-on-`done` под ownership-предикатом в одном
	// UPDATE … RETURNING: терминальное состояние читается тем же стейтментом,
	// reload-Get не нужен (TOCTOU/second-writer-wins исключён).
	op, err := owned.CancelOwned(ctx, req.GetOperationId(), owner)
	if err != nil {
		if errors.Is(err, operations.ErrAlreadyDone) {
			return nil, status.Errorf(codes.FailedPrecondition, "operation %s already completed", req.GetOperationId())
		}
		return nil, mapOpErr(err, req.GetOperationId(), "operation cancel failed")
	}
	return operationToProto(op), nil
}

// mapOpErr — маппинг repo-ошибки в gRPC-код. ErrNotFound (нет записи ИЛИ не
// владелец) → NotFound с эхо-id (no-leak). Прочее → фиксированный INTERNAL без
// leak'а pgx/SQL-detail наружу.
func mapOpErr(err error, id, internalMsg string) error {
	if errors.Is(err, operations.ErrNotFound) {
		return status.Errorf(codes.NotFound, "operation %s not found", id)
	}
	return status.Error(codes.Internal, internalMsg)
}
