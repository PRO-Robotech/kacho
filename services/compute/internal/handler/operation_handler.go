// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// OperationHandler реализует operationpb.OperationServiceServer.
//
// Get/Cancel энфорсят владельца операции: владелец — principal, создавший её
// (колонки principal_type/principal_id записи operations). `operation_id` опакен,
// но это прямой объект-референс: без проверки владельца любой аутентифицированный
// caller, узнав чужой id, прочитал бы чужой ресурс (Operation.response несёт его
// целиком, напр. созданный Instance) или отменил бы чужую in-flight мутацию.
// Поэтому ownership-предикат энфорсится тут через ownership-scoped repo
// (GetOwned/CancelOwned, предикат в SQL WHERE). Чужой/несуществующий id отдаёт
// одинаковый NotFound (no-leak: «есть, но не твоя» неотличимо от «нет такой»).
//
// Admin-обхода предиката здесь НЕТ (паритет с kacho-vpc): OperationService
// выставлен ТОЛЬКО на public listener (registerPublicServices), а публичный
// листенер admin-полномочий не выдаёт вообще (tenant_interceptor.go). Прежняя
// ветка `if TenantFromCtx(ctx).Admin { … }` снимала предикат по флагу, который
// приходил из клиентского заголовка `x-kacho-admin`, ретранслированного шлюзом
// через `Grpc-Metadata-`-мост — то есть была не admin-функцией, а живым обходом
// владения: Operation.response несёт созданный ресурс целиком, поэтому чужой
// ресурс становился читаемым по одному лишь угаданному/подсмотренному op-id.
// Владелец операции резолвится ИСКЛЮЧИТЕЛЬНО из доверенного ctx-principal'а.
type OperationHandler struct {
	operationpb.UnimplementedOperationServiceServer
	repo operations.Repo
}

// NewOperationHandler создаёт OperationHandler. В проде repo — pgRepo, который
// реализует operations.OwnedOperationRepo; если не реализует (ошибка wiring'а) —
// ownership-вызовы возвращают INTERNAL (fail-closed, не silent-bypass).
func NewOperationHandler(repo operations.Repo) *OperationHandler {
	return &OperationHandler{repo: repo}
}

// Get возвращает операцию по id — только её владельцу.
func (h *OperationHandler) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		return nil, status.Error(codes.Internal, "operation get failed")
	}
	// Анонимный запрос владельцем не является. PrincipalFromContext на ctx без
	// принципала отдаёт системный fallback {system, bootstrap}, который совпадает
	// с ownership-предикатом на КАЖДОЙ операции, записанной системным путём;
	// OwnerFromContext вместо этого сообщает об отсутствии ключа, и мы отдаём тот
	// же no-leak NotFound, что и на чужой операции.
	owner, hasOwner := operations.OwnerFromContext(ctx)
	if !hasOwner {
		return nil, status.Errorf(codes.NotFound, "operation %s not found", req.OperationId)
	}
	op, err := owned.GetOwned(ctx, req.OperationId, owner)
	if err != nil {
		return nil, mapOpGetErr(err, req.OperationId)
	}
	return operationToProto(op), nil
}

// Cancel отменяет операцию (если ещё не завершена) — только её владельцу.
func (h *OperationHandler) Cancel(ctx context.Context, req *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id required")
	}
	owned, ok := operations.AsOwned(h.repo)
	if !ok {
		return nil, status.Error(codes.Internal, "operation cancel failed")
	}

	// Owner-ключ резолвится ИСКЛЮЧИТЕЛЬНО из доверенного ctx-principal'а
	// (anti-spoof).
	// Анонимный запрос владельцем не является. PrincipalFromContext на ctx без
	// принципала отдаёт системный fallback {system, bootstrap}, который совпадает
	// с ownership-предикатом на КАЖДОЙ операции, записанной системным путём;
	// OwnerFromContext вместо этого сообщает об отсутствии ключа, и мы отдаём тот
	// же no-leak NotFound, что и на чужой операции.
	owner, hasOwner := operations.OwnerFromContext(ctx)
	if !hasOwner {
		return nil, status.Errorf(codes.NotFound, "operation %s not found", req.OperationId)
	}

	// Атомарный CancelOwned возвращает терминальное состояние в RETURNING —
	// отдельный reload-Get после отмены не нужен.
	op, err := owned.CancelOwned(ctx, req.OperationId, owner)
	if err != nil {
		if errors.Is(err, operations.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "operation %s not found", req.OperationId)
		}
		if errors.Is(err, operations.ErrAlreadyDone) {
			return nil, status.Errorf(codes.FailedPrecondition, "operation %s already completed", req.OperationId)
		}
		return nil, status.Error(codes.Internal, "operation cancel failed")
	}
	return operationToProto(op), nil
}

// mapOpGetErr — маппинг repo-ошибки Get'а в gRPC-код. ErrNotFound (нет записи ИЛИ
// не владелец) → NotFound с эхо-id (no-leak). Прочее → фиксированный INTERNAL без
// leak'а pgx/SQL-detail наружу.
func mapOpGetErr(err error, id string) error {
	if errors.Is(err, operations.ErrNotFound) {
		return status.Errorf(codes.NotFound, "operation %s not found", id)
	}
	return status.Error(codes.Internal, "operation get failed")
}
