// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Полная цепочка живой эскалации, локнутая на наблюдаемом уровне:
//
//	client → `Grpc-Metadata-X-Kacho-Admin: true`
//	      → api-gateway REST→gRPC bridge (пробрасывал любой Grpc-Metadata-*)
//	      → compute TenantCtx.Admin = true на ПУБЛИЧНОМ листенере
//	      → OperationService.Get снимал ownership-предикат
//	      → Operation.response несёт созданный ресурс целиком → чужой ресурс прочитан.
//
// Здесь проверяется последнее звено: даже если заголовок каким-то образом снова
// доедет до сервиса, чужая операция читаться не должна. «Шлюз почистит» —
// недостаточное обоснование: gateway не единственный способ дотянуться до
// листенера.

// opForgedAdminCtx — ctx честного пользователя usr-B, которому вдобавок
// проставили TenantCtx.Admin (ровно то, что делал подложенный заголовок).
func opForgedAdminCtx(id string) context.Context {
	return context.WithValue(opUserCtx(id), tenantCtxKey{}, TenantCtx{Admin: true})
}

func TestOperationHandler_ForgedAdmin_CannotReadForeignOperation(t *testing.T) {
	repo := newFakeOwnedOpsRepo()
	victim := seedInFlight(repo, "user", "usr-A")
	h := NewOperationHandler(repo)

	_, err := h.Get(opForgedAdminCtx("usr-B"),
		&operationpb.GetOperationRequest{OperationId: victim.ID})

	require.Error(t, err, "foreign operation must not be readable with a forged admin flag")
	st, _ := grpcstatus.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	// Байт-идентично «нет такой операции» — «есть, но не твоя» неотличимо.
	assert.Equal(t, "operation "+victim.ID+" not found", st.Message())
}

func TestOperationHandler_ForgedAdmin_CannotCancelForeignOperation(t *testing.T) {
	repo := newFakeOwnedOpsRepo()
	victim := seedInFlight(repo, "user", "usr-A")
	h := NewOperationHandler(repo)

	_, err := h.Cancel(opForgedAdminCtx("usr-B"),
		&operationpb.CancelOperationRequest{OperationId: victim.ID})

	require.Error(t, err, "foreign in-flight operation must not be cancellable with a forged admin flag")
	st, _ := grpcstatus.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "operation "+victim.ID+" not found", st.Message())

	// Владелец предъявляется исключительно из ctx-principal'а: repo видит
	// usr-B, а не подставленного admin'а / владельца операции.
	require.Len(t, repo.cancelOwners, 1)
	assert.Equal(t, operations.Owner{PrincipalType: "user", PrincipalID: "usr-B"}, repo.cancelOwners[0])
}

// TestOperationHandler_OwnerStillWorks — регрессия: собственная операция
// владельца по-прежнему читается и отменяется (фикс не режет happy-path).
func TestOperationHandler_OwnerStillWorks(t *testing.T) {
	repo := newFakeOwnedOpsRepo()
	mine := seedInFlight(repo, "user", "usr-A")
	h := NewOperationHandler(repo)

	got, err := h.Get(opUserCtx("usr-A"), &operationpb.GetOperationRequest{OperationId: mine.ID})
	require.NoError(t, err)
	assert.Equal(t, mine.ID, got.Id)

	cancelled, err := h.Cancel(opUserCtx("usr-A"), &operationpb.CancelOperationRequest{OperationId: mine.ID})
	require.NoError(t, err)
	assert.True(t, cancelled.Done)
}

// TestOperationService_NotOnInternalListener — почему admin-обхода в
// OperationHandler не должно быть в принципе (паритет с kacho-vpc): сервис
// выставлен ТОЛЬКО на публичном листенере, где Admin недостижим by construction.
// Ветка admin-bypass была бы одновременно мёртвой и, при любой ошибке в
// заголовочной гигиене выше, живым обходом ownership.
//
// Гейт держится за то, что публичный interceptor Admin не выдаёт — assert'им это
// на цепочке, которой обслуживается OperationService.
func TestOperationService_PublicChainYieldsNoAdmin(t *testing.T) {
	var seen TenantCtx
	_, err := TenantUnaryInterceptor(false, false)(
		forgedAdminCtx(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.operation.OperationService/Get"},
		func(c context.Context, _ any) (any, error) {
			seen = TenantFromCtx(c)
			return nil, nil
		})
	require.NoError(t, err)
	assert.False(t, seen.Admin)
	assert.True(t, seen.IsAnonymous(),
		"forged admin/project headers must not satisfy the production-mode AuthN gate either")
}
