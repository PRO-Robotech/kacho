// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package opsproxy_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
)

// countingOperationServer — mock backend, который СЧИТАЕТ вызовы Cancel.
// Утверждение «ownership отказал» ничего не говорит о том, применилась ли
// мутация: отказ, выданный ПОСЛЕ вызова backend'а, выглядит для клиента точно
// так же, как отказ, выданный ДО. Поэтому проба считает вызовы, а не коды.
type countingOperationServer struct {
	operationpb.UnimplementedOperationServiceServer
	ops         map[string]*operationpb.Operation
	getCalls    atomic.Int64
	cancelCalls atomic.Int64
}

func (m *countingOperationServer) Get(_ context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	m.getCalls.Add(1)
	op, ok := m.ops[req.OperationId]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "operation %q not found", req.OperationId)
	}
	return op, nil
}

func (m *countingOperationServer) Cancel(_ context.Context, req *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	m.cancelCalls.Add(1)
	op, ok := m.ops[req.OperationId]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "operation %q not found", req.OperationId)
	}
	return op, nil
}

func setupCountingBackend(t *testing.T, ops map[string]*operationpb.Operation) (*grpc.ClientConn, *countingOperationServer) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	impl := &countingOperationServer{ops: ops}
	srv := grpc.NewServer()
	operationpb.RegisterOperationServiceServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, impl
}

// TestOpsProxy_Cancel_DoesNotReachBackend_WhenOwnershipDenies — край обязан
// решать вопрос доступа ДО того, как отправит мутацию владельцу. Отмена — это
// мутация: как только backend её принял, операция терминальна, и последующий
// отказ края уже ничего не отменяет. Проба фиксирует НОЛЬ вызовов Cancel.
func TestOpsProxy_Cancel_DoesNotReachBackend_WhenOwnershipDenies(t *testing.T) {
	id := "iop0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "user",
		PrincipalId:   "usr_alice",
	}
	conn, impl := setupCountingBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"iam": conn})

	ctx := withPrincipalMD("usr_bob", "user")
	_, err := proxy.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: id})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PERMISSION_DENIED for a non-owner, got %v", err)
	}
	if n := impl.cancelCalls.Load(); n != 0 {
		t.Fatalf("backend Cancel must not be invoked when the edge denies ownership: got %d call(s)", n)
	}
}

// TestOpsProxy_Cancel_DoesNotReachBackend_WhenCallerIsAnonymous — та же
// гарантия для безымянного вызывающего.
func TestOpsProxy_Cancel_DoesNotReachBackend_WhenCallerIsAnonymous(t *testing.T) {
	id := "enp0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "user",
		PrincipalId:   "usr_alice",
	}
	conn, impl := setupCountingBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"vpc": conn})

	ctx := withPrincipalMD("anonymous", "system")
	_, err := proxy.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: id})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PERMISSION_DENIED for an anonymous caller, got %v", err)
	}
	if n := impl.cancelCalls.Load(); n != 0 {
		t.Fatalf("backend Cancel must not be invoked for an anonymous caller: got %d call(s)", n)
	}
}

// TestOpsProxy_Cancel_ReachesBackend_ForOwner — законная форма того же пути:
// владелец по-прежнему отменяет свою операцию, и ровно один раз. Без этой
// половины гейт ловил бы форму («Cancel не вызван»), а не существо.
func TestOpsProxy_Cancel_ReachesBackend_ForOwner(t *testing.T) {
	id := "iop0123456789abcdefg"
	op := &operationpb.Operation{
		Id:            id,
		PrincipalType: "user",
		PrincipalId:   "usr_alice",
	}
	conn, impl := setupCountingBackend(t, map[string]*operationpb.Operation{id: op})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"iam": conn})

	ctx := withPrincipalMD("usr_alice", "user")
	resp, err := proxy.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: id})
	if err != nil {
		t.Fatalf("owner must be able to cancel own operation: %v", err)
	}
	if resp.GetId() != id {
		t.Fatalf("expected op %q, got %q", id, resp.GetId())
	}
	if n := impl.cancelCalls.Load(); n != 1 {
		t.Fatalf("owner cancel must reach the backend exactly once: got %d", n)
	}
}
