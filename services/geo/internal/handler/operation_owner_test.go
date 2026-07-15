// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// seedOwnedOp кладёт в мок незавершённую операцию, созданную principal'ом owner
// (через ctx-WithPrincipal → NewFromContext переносит principal в op.Principal,
// мок сохраняет его). Возвращает op-id.
func seedOwnedOp(t *testing.T, ops *repomock.OpsRepo, owner operations.Principal) string {
	t.Helper()
	ctx := operations.WithPrincipal(context.Background(), owner)
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix, "create region",
		&geov1.CreateRegionMetadata{RegionId: "region-1"})
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if err := ops.Create(ctx, op); err != nil {
		t.Fatalf("ops.Create: %v", err)
	}
	return op.ID
}

var (
	adminA = operations.Principal{Type: "user", ID: "usr_adminA"}
	adminB = operations.Principal{Type: "user", ID: "usr_adminB"}
)

// TestOperationHandler_Get_foreignPrincipal_NotFound — BOLA-гейт: caller B не
// владелец операции A → NotFound (no-leak, неотличимо от «нет такой»), НЕ отдаёт
// чужую операцию.
func TestOperationHandler_Get_foreignPrincipal_NotFound(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxB := operations.WithPrincipal(context.Background(), adminB)
	_, err := oh.Get(ctxB, &operationpb.GetOperationRequest{OperationId: opID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("foreign Get: want NOT_FOUND, got %v", err)
	}
}

// TestOperationHandler_Get_owner_ok — владелец A читает свою операцию.
func TestOperationHandler_Get_owner_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Get(ctxA, &operationpb.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Get err = %v", err)
	}
	if op.GetId() != opID {
		t.Fatalf("owner Get id = %q, want %q", op.GetId(), opID)
	}
}

// TestOperationHandler_Cancel_foreignPrincipal_NotFound — caller B не может
// отменить in-flight операцию A → NotFound (integrity-гейт control-plane).
func TestOperationHandler_Cancel_foreignPrincipal_NotFound(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxB := operations.WithPrincipal(context.Background(), adminB)
	_, err := oh.Cancel(ctxB, &operationpb.CancelOperationRequest{OperationId: opID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("foreign Cancel: want NOT_FOUND, got %v", err)
	}
	// Операция A должна остаться НЕ отменённой (foreign cancel — no-op).
	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Get(ctxA, &operationpb.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Get after foreign cancel err = %v", err)
	}
	if op.GetDone() {
		t.Fatalf("operation was cancelled by foreign principal — BOLA integrity breach")
	}
}

// TestOperationHandler_Cancel_owner_ok — владелец A отменяет свою in-flight
// операцию (done=true, CANCELLED).
func TestOperationHandler_Cancel_owner_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Cancel err = %v", err)
	}
	if !op.GetDone() {
		t.Fatalf("owner Cancel: op must be done=true")
	}
}

// TestOperationHandler_Cancel_terminalSuccess_FailedPrecondition — владелец A
// отменяет УЖЕ ЗАВЕРШЁННУЮ УСПЕХОМ (done=true, response set, no error) операцию →
// FailedPrecondition ("already completed"). Проверяет негативную ветку публичного
// RPC (rule #12): CancelOwned → ErrAlreadyDone → codes.FailedPrecondition, а
// терминальное SUCCESS-состояние НЕ перезаписывается на CANCELLED.
func TestOperationHandler_Cancel_terminalSuccess_FailedPrecondition(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)

	// Финализируем как SUCCESS (worker.MarkDone) — терминал, не CANCELLED.
	resp, err := anypb.New(&geov1.Region{Id: "region-1"})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	if err := ops.MarkDone(context.Background(), opID, resp); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	_, err = handler.NewOperationHandler(ops).Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Cancel on terminal-SUCCESS op: want FAILED_PRECONDITION, got %v", err)
	}

	// Терминальное состояние не тронуто: остаётся SUCCESS (response set, no error).
	got, gerr := ops.Get(context.Background(), opID)
	if gerr != nil {
		t.Fatalf("Get after failed Cancel: %v", gerr)
	}
	if got.Error != nil {
		t.Fatalf("terminal SUCCESS was overwritten to error — LRO terminal-state integrity breach")
	}
	if got.Response == nil {
		t.Fatalf("terminal SUCCESS response was cleared — integrity breach")
	}
}

// TestOperationHandler_Cancel_idempotentReCancel_ok — владелец A отменяет
// in-flight операцию, затем ПОВТОРНО её отменяет: второй Cancel идемпотентен —
// возвращает ту же операцию (done=true) без ошибки (CancelOwned распознаёт уже-
// CANCELLED). Проверяет идемпотентную ветку, ранее не покрытую.
func TestOperationHandler_Cancel_idempotentReCancel_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)
	ctxA := operations.WithPrincipal(context.Background(), adminA)

	first, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("first Cancel err = %v", err)
	}
	if !first.GetDone() {
		t.Fatalf("first Cancel: op must be done=true")
	}

	second, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("re-Cancel must be idempotent (no error), got %v", err)
	}
	if !second.GetDone() {
		t.Fatalf("re-Cancel: op must remain done=true")
	}
	if second.GetId() != opID {
		t.Fatalf("re-Cancel returned id %q, want same op %q", second.GetId(), opID)
	}
}

// bareOpsRepo реализует ТОЛЬКО operations.Repo (не OwnedOperationRepo) — модель
// wiring-ошибки. Handler обязан fail-closed (INTERNAL), а не молча пропустить
// ownership-проверку.
type bareOpsRepo struct{ operations.Repo }

// TestOperationHandler_Get_repoWithoutOwned_failClosed — если repo не
// реализует OwnedOperationRepo → INTERNAL (не silent-bypass ownership).
func TestOperationHandler_Get_repoWithoutOwned_failClosed(t *testing.T) {
	oh := handler.NewOperationHandler(bareOpsRepo{Repo: repomock.NewOpsRepo()})
	_, err := oh.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("bare repo Get: want INTERNAL (fail-closed), got %v", err)
	}
	_, err = oh.Cancel(context.Background(), &operationpb.CancelOperationRequest{OperationId: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("bare repo Cancel: want INTERNAL (fail-closed), got %v", err)
	}
}
