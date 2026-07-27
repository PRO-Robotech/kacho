// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package opsproxy_test

// Аноним не владеет операциями.
//
// Проверка владения сравнивает пару (тип, id) вызывающего с парой, записанной
// в операции. Пара «система/аноним» непуста, поэтому она проходит гейт «личность
// не извлеклась» и затем СОВПАДАЕТ САМА С СОБОЙ: один безымянный запрос читает и
// отменяет операции другого безымянного запроса. Ключ владения у них общий по
// построению — их столько же, сколько имён, то есть одно на всех.

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
)

// TestOpsProxy_Get_AnonymousCallerCannotReadAnonymousOwnedOp — операция,
// записанная одним безымянным запросом, недоступна другому безымянному запросу.
func TestOpsProxy_Get_AnonymousCallerCannotReadAnonymousOwnedOp(t *testing.T) {
	id := "iop0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "system",
		PrincipalId:   "anonymous",
	}
	iamConn := setupMockBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"iam": iamConn})

	ctx := withPrincipalMD("anonymous", "system")
	_, err := proxy.Get(ctx, &operationpb.GetOperationRequest{OperationId: id})
	if err == nil {
		t.Fatal("аноним не владелец: чтение операции, записанной другим анонимом, обязано быть отвергнуто")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Errorf("ожидался PERMISSION_DENIED, получено %s", st.Code())
	}
}

// TestOpsProxy_Cancel_AnonymousCallerCannotCancelAnonymousOwnedOp — то же
// правило на отмене: безымянный вызывающий не отменяет чужую операцию.
func TestOpsProxy_Cancel_AnonymousCallerCannotCancelAnonymousOwnedOp(t *testing.T) {
	id := "enp0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "system",
		PrincipalId:   "anonymous",
	}
	vpcConn := setupMockBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"vpc": vpcConn})

	ctx := withPrincipalMD("anonymous", "system")
	_, err := proxy.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: id})
	if err == nil {
		t.Fatal("аноним не владелец: отмена операции, записанной другим анонимом, обязана быть отвергнута")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Errorf("ожидался PERMISSION_DENIED, получено %s", st.Code())
	}
}

// TestOpsProxy_Get_TenantCannotReadAnonymousOwnedOp — операция с безымянным
// владельцем не становится общедоступной и для обычного тенанта: настоящий
// владелец неизвестен, значит fail-closed (как и на системном владельце).
func TestOpsProxy_Get_TenantCannotReadAnonymousOwnedOp(t *testing.T) {
	id := "iop0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "system",
		PrincipalId:   "anonymous",
	}
	iamConn := setupMockBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"iam": iamConn})

	ctx := withPrincipalMD("usr_tenant", "user")
	_, err := proxy.Get(ctx, &operationpb.GetOperationRequest{OperationId: id})
	if err == nil {
		t.Fatal("операция с безымянным владельцем не должна читаться тенантом")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Errorf("ожидался PERMISSION_DENIED, получено %s", st.Code())
	}
}

// TestOpsProxy_Get_OwnerStillReadsOwnOperation — сужаем анонимность, а не
// владение: настоящий владелец по-прежнему читает свою операцию.
func TestOpsProxy_Get_OwnerStillReadsOwnOperation(t *testing.T) {
	id := "iop0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "user",
		PrincipalId:   "usr_owner",
	}
	iamConn := setupMockBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"iam": iamConn})

	ctx := withPrincipalMD("usr_owner", "user")
	resp, err := proxy.Get(ctx, &operationpb.GetOperationRequest{OperationId: id})
	if err != nil {
		t.Fatalf("владелец обязан читать свою операцию: %v", err)
	}
	if resp.Id != id {
		t.Errorf("ожидался %q, получено %q", id, resp.Id)
	}
}
