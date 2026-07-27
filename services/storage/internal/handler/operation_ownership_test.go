// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Наблюдаемый контракт: запрос БЕЗ принципала не является владельцем ничего.
//
// OperationService.Get/Cancel помечены в permission-map storage как Public:true —
// per-RPC ReBAC-Check для них короткозамкнут, поэтому единственный, кто решает
// доступ, — ownership-предикат. Если анонимный контекст приводится к
// «личности по умолчанию» {system, bootstrap}, он совпадает с этим предикатом
// на каждой операции, записанной системным принципалом (фоновый job, бутстрап,
// любой create-путь без auth-ctx), и анонимный вызывающий читает и отменяет
// чужие операции.

// fakeOwnedRepo — минимальный operations.Repo + OwnedOperationRepo. Хранит одну
// операцию и матчит её ТЕМ ЖЕ предикатом, что pgRepo: пара
// (principal_type, principal_id).
type fakeOwnedRepo struct {
	op        *operations.Operation
	owner     operations.Owner
	cancelled bool
}

func (r *fakeOwnedRepo) Create(context.Context, operations.Operation) error { return nil }
func (r *fakeOwnedRepo) CreateWithPrincipal(context.Context, operations.Operation, operations.Principal) error {
	return nil
}
func (r *fakeOwnedRepo) Get(context.Context, string) (*operations.Operation, error) {
	return r.op, nil
}
func (r *fakeOwnedRepo) List(context.Context, operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}
func (r *fakeOwnedRepo) MarkDone(context.Context, string, *anypb.Any) error         { return nil }
func (r *fakeOwnedRepo) MarkError(context.Context, string, *rpcstatus.Status) error { return nil }
func (r *fakeOwnedRepo) Cancel(context.Context, string) error                       { return nil }

func (r *fakeOwnedRepo) matches(owner operations.Owner) bool {
	return owner.PrincipalType == r.owner.PrincipalType && owner.PrincipalID == r.owner.PrincipalID
}

func (r *fakeOwnedRepo) GetOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	if r.op == nil || r.op.ID != id || !r.matches(owner) {
		return nil, operations.ErrNotFound
	}
	return r.op, nil
}

func (r *fakeOwnedRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	if r.op == nil || r.op.ID != id || !r.matches(owner) {
		return nil, operations.ErrNotFound
	}
	r.cancelled = true
	return r.op, nil
}

func (r *fakeOwnedRepo) ListOwned(context.Context, operations.ListFilter, operations.Owner) ([]operations.Operation, string, error) {
	return nil, "", nil
}

// systemOwnedRepo — репозиторий с одной операцией, записанной СИСТЕМНЫМ
// принципалом (ровно то, что пишет Create/CreateWithPrincipal, когда auth-ctx
// не было).
func systemOwnedRepo() *fakeOwnedRepo {
	return &fakeOwnedRepo{
		op:    &operations.Operation{ID: "opq00000000000000001", Description: "system-owned"},
		owner: operations.OwnerFromPrincipal(operations.SystemPrincipal()),
	}
}

// TestOperationGet_AnonymousContextGetsNotFound — запрос БЕЗ принципала на
// Get не должен получить операцию, записанную системным принципалом.
func TestOperationGet_AnonymousContextGetsNotFound(t *testing.T) {
	repo := systemOwnedRepo()
	h := NewOperationHandler(repo)

	got, err := h.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: repo.op.ID})
	if err == nil {
		t.Fatalf("anonymous Get returned an operation (%v) — default identity matched the owner predicate", got.GetId())
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Fatalf("anonymous Get: got code %v, want NotFound", code)
	}
	if msg := status.Convert(err).Message(); msg != "operation "+repo.op.ID+" not found" {
		t.Errorf("anonymous Get message = %q, want the no-leak not-found tone", msg)
	}
}

// TestOperationGet_ExplicitlyClearedPrincipalGetsNotFound — принципал, СНЯТЫЙ
// transport-слоем (scrub на недоверенном форвардере), тоже не владелец.
func TestOperationGet_ExplicitlyClearedPrincipalGetsNotFound(t *testing.T) {
	repo := systemOwnedRepo()
	h := NewOperationHandler(repo)

	ctx := operations.WithoutPrincipal(operations.WithPrincipal(context.Background(), operations.SystemPrincipal()))
	if _, err := h.Get(ctx, &operationpb.GetOperationRequest{OperationId: repo.op.ID}); status.Code(err) != codes.NotFound {
		t.Fatalf("cleared-principal Get: got %v, want NotFound", status.Code(err))
	}
}

// TestOperationCancel_AnonymousContextGetsNotFound — то же для Cancel: без
// принципала чужая in-flight операция не отменяется.
func TestOperationCancel_AnonymousContextGetsNotFound(t *testing.T) {
	repo := systemOwnedRepo()
	h := NewOperationHandler(repo)

	if _, err := h.Cancel(context.Background(), &operationpb.CancelOperationRequest{OperationId: repo.op.ID}); status.Code(err) != codes.NotFound {
		t.Fatalf("anonymous Cancel: got %v, want NotFound", status.Code(err))
	}
	if repo.cancelled {
		t.Error("anonymous Cancel actually cancelled the system-owned operation")
	}
}

// TestOperationGet_AuthenticatedOwnerStillServed — правка обязана СУЖАТЬ:
// подлинный владелец (принципал явно установлен) по-прежнему читает свою
// операцию.
func TestOperationGet_AuthenticatedOwnerStillServed(t *testing.T) {
	repo := &fakeOwnedRepo{
		op:    &operations.Operation{ID: "opq00000000000000002"},
		owner: operations.Owner{PrincipalType: "user", PrincipalID: "usr-1"},
	}
	h := NewOperationHandler(repo)

	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-1"})
	got, err := h.Get(ctx, &operationpb.GetOperationRequest{OperationId: repo.op.ID})
	if err != nil {
		t.Fatalf("owner Get: unexpected error %v (change must narrow, not break the owner)", err)
	}
	if got.GetId() != repo.op.ID {
		t.Errorf("owner Get returned %q, want %q", got.GetId(), repo.op.ID)
	}
}

// TestOperationGet_ExplicitSystemPrincipalStillServed — ЯВНО установленный
// системный принципал (доверенный internal-вызывающий) остаётся владельцем
// системных операций: сужаем анонимность, а не ломаем bootstrap-путь.
func TestOperationGet_ExplicitSystemPrincipalStillServed(t *testing.T) {
	repo := systemOwnedRepo()
	h := NewOperationHandler(repo)

	ctx := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())
	if _, err := h.Get(ctx, &operationpb.GetOperationRequest{OperationId: repo.op.ID}); err != nil {
		t.Fatalf("explicit system-principal Get: unexpected error %v", err)
	}
}
