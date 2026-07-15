// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
)

// OperationHandler реализует operationpb.OperationServiceServer для каталога
// kacho-storage: клиент поллит Get(operation_id) до done=true после async-мутации
// Volume/Snapshot. Регистрируется на обоих листенерах (public read-poll + internal).
//
// Get/Cancel энфорсят ВЛАДЕЛЬЦА операции (principal, создавший её) через
// ownership-scoped repo (GetOwned/CancelOwned — предикат в SQL WHERE, within-service
// инвариант на DB-уровне, без software TOCTOU). Чужой/несуществующий id → ОДИНАКОВЫЙ
// NotFound (no-leak existence-oracle, BOLA-защита прямого object-reference op-id).
type OperationHandler struct {
	operationpb.UnimplementedOperationServiceServer
	repo operations.Repo
}

// NewOperationHandler создаёт OperationHandler. repo — pgRepo (реализует
// operations.OwnedOperationRepo); если не реализует (wiring-ошибка) —
// ownership-вызовы возвращают INTERNAL (fail-closed, НЕ silent owner-bypass).
func NewOperationHandler(repo operations.Repo) *OperationHandler {
	return &OperationHandler{repo: repo}
}

// Get возвращает состояние операции (done/error/response) ТОЛЬКО её владельцу;
// чужой/несуществующий id → NotFound (no-leak).
func (h *OperationHandler) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	if req.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		return nil, status.Error(codes.Internal, "operation get failed")
	}
	owner := operations.OwnerFromPrincipal(operations.PrincipalFromContext(ctx))
	op, err := owned.GetOwned(ctx, req.GetOperationId(), owner)
	if err != nil {
		return nil, mapOpErr(err, req.GetOperationId(), "operation get failed")
	}
	return operationToProto(op), nil
}

// Cancel отменяет ещё не завершённую операцию ТОЛЬКО её владельцу (атомарный CAS-on-
// done под ownership-предикатом). Идемпотентно на уже-CANCELLED; на терминале
// SUCCESS/ERROR → FailedPrecondition; чужой/нет → NotFound (no-leak).
func (h *OperationHandler) Cancel(ctx context.Context, req *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	if req.GetOperationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		return nil, status.Error(codes.Internal, "operation cancel failed")
	}
	owner := operations.OwnerFromPrincipal(operations.PrincipalFromContext(ctx))
	op, err := owned.CancelOwned(ctx, req.GetOperationId(), owner)
	if err != nil {
		if errors.Is(err, operations.ErrAlreadyDone) {
			return nil, status.Errorf(codes.FailedPrecondition, "operation %s already completed", req.GetOperationId())
		}
		return nil, mapOpErr(err, req.GetOperationId(), "operation cancel failed")
	}
	return operationToProto(op), nil
}

// mapOpErr — repo-ошибка → gRPC-код. ErrNotFound (нет ИЛИ не владелец) → NotFound
// с эхо-id (no-leak). Прочее → фиксированный INTERNAL без leak'а pgx/SQL.
func mapOpErr(err error, id, internalMsg string) error {
	if errors.Is(err, operations.ErrNotFound) {
		return status.Errorf(codes.NotFound, "operation %s not found", id)
	}
	return status.Error(codes.Internal, internalMsg)
}

// operationToProto конвертирует corelib operations.Operation в proto-форму. oneof
// result (error|response) заполнен только при done. Timestamps усечены до секунд
// (единый apiconv-формат — микросекунды с БД не текут на wire).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	if op == nil {
		return nil
	}
	p := &operationpb.Operation{
		Id:                   op.ID,
		Description:          op.Description,
		CreatedAt:            timestamppb.New(op.CreatedAt.Truncate(time.Second)),
		CreatedBy:            op.CreatedBy,
		ModifiedAt:           timestamppb.New(op.ModifiedAt.Truncate(time.Second)),
		Done:                 op.Done,
		Metadata:             op.Metadata,
		PrincipalType:        op.Principal.Type,
		PrincipalId:          op.Principal.ID,
		PrincipalDisplayName: op.Principal.DisplayName,
	}
	if op.Error != nil {
		p.Result = &operationpb.Operation_Error{Error: op.Error}
	} else if op.Response != nil {
		p.Result = &operationpb.Operation_Response{Response: op.Response}
	}
	return p
}
