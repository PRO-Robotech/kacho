// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
)

// seedInFlightOwnedOp пишет незавершённую (done=false) LRO-строку через РЕАЛЬНЫЙ
// pgRepo с creator-principal owner (из ctx). Без async-worker'а — строка остаётся
// in-flight, чтобы проверить ownership-scoped Cancel.
func seedInFlightOwnedOp(t *testing.T, ops operations.Repo, owner operations.Principal) string {
	t.Helper()
	ctx := operations.WithPrincipal(context.Background(), owner)
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix, "create region",
		&geov1.CreateRegionMetadata{RegionId: "region-owner"})
	require.NoError(t, err)
	require.NoError(t, ops.Create(ctx, op))
	return op.ID
}

// geo-op-owner-01: BOLA-гейт на РЕАЛЬНОМ pgRepo (ownership-предикат в SQL WHERE).
// Операция создана principal A; caller B (другой principal) НЕ может её ни
// прочитать, ни отменить — оба → NotFound (no-leak), и in-flight операция A
// остаётся неотменённой. Владелец A — читает и отменяет свою.
func TestOperationOwnerScoping_pgRepo(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	oh := handler.NewOperationHandler(ops)

	adminA := operations.Principal{Type: "user", ID: "usr_owner_A"}
	adminB := operations.Principal{Type: "user", ID: "usr_owner_B"}
	ctxA := operations.WithPrincipal(context.Background(), adminA)
	ctxB := operations.WithPrincipal(context.Background(), adminB)

	opID := seedInFlightOwnedOp(t, ops, adminA)

	// B не владелец → Get NotFound (no-leak).
	_, err := oh.Get(ctxB, &operationpb.GetOperationRequest{OperationId: opID})
	require.Equal(t, codes.NotFound, status.Code(err), "foreign Get must be NOT_FOUND")

	// B не владелец → Cancel NotFound, операция НЕ тронута.
	_, err = oh.Cancel(ctxB, &operationpb.CancelOperationRequest{OperationId: opID})
	require.Equal(t, codes.NotFound, status.Code(err), "foreign Cancel must be NOT_FOUND")

	// A владелец → видит свою in-flight операцию (done=false).
	got, err := oh.Get(ctxA, &operationpb.GetOperationRequest{OperationId: opID})
	require.NoError(t, err)
	require.False(t, got.GetDone(), "operation must remain in-flight after foreign cancel (integrity)")

	// A владелец → отменяет свою операцию.
	cancelled, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	require.NoError(t, err)
	require.True(t, cancelled.GetDone(), "owner Cancel must terminate the operation")
}

// geo-op-owner-02: Cancel УЖЕ ЗАВЕРШЁННОЙ УСПЕХОМ операции владельцем → на РЕАЛЬНОМ
// pgRepo CancelOwned видит done=true (не CANCELLED) → ErrAlreadyDone → handler
// маппит в FailedPrecondition; терминальное SUCCESS-состояние (response set, no
// error) НЕ перезаписывается (LRO terminal-state integrity, CAS-on-`done`).
func TestOperationCancel_TerminalSuccess_FailedPrecondition_pgRepo(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	oh := handler.NewOperationHandler(ops)

	owner := operations.Principal{Type: "user", ID: "usr_term_success"}
	ctx := operations.WithPrincipal(context.Background(), owner)
	opID := seedInFlightOwnedOp(t, ops, owner)

	resp, err := anypb.New(&geov1.Region{Id: "region-term"})
	require.NoError(t, err)
	require.NoError(t, ops.MarkDone(context.Background(), opID, resp), "finalize op as SUCCESS")

	_, err = oh.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: opID})
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"Cancel on terminal-SUCCESS op must be FAILED_PRECONDITION")

	got, err := ops.Get(context.Background(), opID)
	require.NoError(t, err)
	require.True(t, got.Done)
	require.Nil(t, got.Error, "terminal SUCCESS must not be overwritten to CANCELLED")
	require.NotNil(t, got.Response, "terminal SUCCESS response must survive the failed Cancel")
}

// geo-op-owner-03: идемпотентный re-cancel на РЕАЛЬНОМ pgRepo. Владелец отменяет
// in-flight операцию, затем повторно — второй Cancel возвращает ту же операцию
// (done=true) без ошибки (CancelOwned распознаёт уже-CANCELLED через error_code=
// Canceled, а не второй CAS-переход).
func TestOperationCancel_IdempotentReCancel_pgRepo(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	oh := handler.NewOperationHandler(ops)

	owner := operations.Principal{Type: "user", ID: "usr_idem"}
	ctx := operations.WithPrincipal(context.Background(), owner)
	opID := seedInFlightOwnedOp(t, ops, owner)

	first, err := oh.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: opID})
	require.NoError(t, err)
	require.True(t, first.GetDone())

	second, err := oh.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: opID})
	require.NoError(t, err, "re-cancel of already-CANCELLED op must be idempotent (no error)")
	require.True(t, second.GetDone())
	require.Equal(t, opID, second.GetId(), "re-cancel returns the same operation")
}

// geo-op-owner-04: race Cancel(владелец) vs MarkDone(worker) на ОДНОЙ in-flight
// операции. CancelOwned и MarkDone — оба атомарный CAS-on-`done` в одном
// UPDATE … RETURNING → ровно один перехватывает терминальный переход; проигравший
// видит уже-done строку. Инвариант: конечное состояние — РОВНО ОДНО терминальное
// (либо CANCELLED, либо SUCCESS), не смешанное; проигравший получает ошибку
// (Cancel→FailedPrecondition, MarkDone→ErrAlreadyDone). Ловит ослабление CAS-
// предиката, которое пустило бы обе мутации (last-writer-wins смешанный терминал).
func TestOperationCancelVsMarkDone_Race_ExactlyOneTerminal_pgRepo(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	oh := handler.NewOperationHandler(ops)

	owner := operations.Principal{Type: "user", ID: "usr_race"}
	ctx := operations.WithPrincipal(context.Background(), owner)
	opID := seedInFlightOwnedOp(t, ops, owner)

	resp, err := anypb.New(&geov1.Region{Id: "region-race"})
	require.NoError(t, err)

	var (
		wg                 sync.WaitGroup
		cancelErr, markErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, cancelErr = oh.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: opID})
	}()
	go func() {
		defer wg.Done()
		markErr = ops.MarkDone(context.Background(), opID, resp)
	}()
	wg.Wait()

	cancelWon := cancelErr == nil
	markWon := markErr == nil
	require.True(t, cancelWon != markWon,
		"exactly one terminal transition must win (cancelErr=%v markErr=%v)", cancelErr, markErr)

	got, err := ops.Get(context.Background(), opID)
	require.NoError(t, err)
	require.True(t, got.Done, "op must be terminal after the race")

	if cancelWon {
		require.ErrorIs(t, markErr, operations.ErrAlreadyDone,
			"MarkDone loser must observe already-done row")
		require.NotNil(t, got.Error, "cancel winner → CANCELLED terminal (error set)")
		require.Equal(t, int32(codes.Canceled), got.Error.GetCode())
		require.Nil(t, got.Response, "cancel winner must not carry a SUCCESS response")
	} else {
		require.Equal(t, codes.FailedPrecondition, status.Code(cancelErr),
			"Cancel loser must be FAILED_PRECONDITION")
		require.Nil(t, got.Error, "markdone winner → SUCCESS terminal (no error)")
		require.NotNil(t, got.Response, "markdone winner carries the SUCCESS response")
	}
}
