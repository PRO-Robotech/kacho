// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Дублёры репозитория операций для проб ЭТОГО сервиса.
//
// Переехали сюда из снятой пробы обработчика операции: сам обработчик сведён в
// `pkg/operations/operationspb` и проверяется там одной суитой, а здешние пробы
// утверждают СВОЙ предмет — что репозиторий этого сервиса несёт предикат
// владения и что подделанный заголовок его не снимает.

package handler

import (
	"context"
	"sync"
	"time"

	genstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// fakeOwnedOpsRepo — тестовый double, реализующий operations.Repo И
// operations.OwnedOperationRepo. Хранит операции вместе с principal'ом и
// применяет ownership-предикат в GetOwned/CancelOwned (зеркало pgRepo-семантики
// из corelib). Дополнительно записывает owner каждого owned-вызова — это нужно,
// чтобы доказать, что владелец резолвится ИСКЛЮЧИТЕЛЬНО из ctx-principal'а
// (anti-spoof), а не из тела/заголовков запроса.

type fakeOwnedOpsRepo struct {
	mu           sync.Mutex
	ops          map[string]*operations.Operation
	getOwners    []operations.Owner
	cancelOwners []operations.Owner
	forceErr     error // если задана — repo-вызовы возвращают её (симуляция INTERNAL)
}

func newFakeOwnedOpsRepo() *fakeOwnedOpsRepo {
	return &fakeOwnedOpsRepo{ops: make(map[string]*operations.Operation)}
}

func (f *fakeOwnedOpsRepo) seed(op *operations.Operation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *op
	f.ops[op.ID] = &cp
}

func ownerMatches(op *operations.Operation, owner operations.Owner) bool {
	return op.Principal.Type == owner.PrincipalType && op.Principal.ID == owner.PrincipalID
}

// ---- operations.OwnedOperationRepo ----

func (f *fakeOwnedOpsRepo) GetOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getOwners = append(f.getOwners, owner)
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	op, ok := f.ops[id]
	if !ok || !ownerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	cp := *op
	return &cp, nil
}

func (f *fakeOwnedOpsRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelOwners = append(f.cancelOwners, owner)
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	op, ok := f.ops[id]
	if !ok || !ownerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	if op.Done {
		if op.Error != nil && op.Error.GetCode() == 1 {
			cp := *op
			return &cp, nil // идемпотентно: уже CANCELLED
		}
		return nil, operations.ErrAlreadyDone // terminal SUCCESS/ERROR
	}
	op.Done = true
	op.Error = &genstatus.Status{Code: 1, Message: "operation cancelled"}
	op.ModifiedAt = time.Now().UTC()
	cp := *op
	return &cp, nil
}

// ---- operations.Repo ----

func (f *fakeOwnedOpsRepo) Create(_ context.Context, op operations.Operation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := op
	f.ops[op.ID] = &cp
	return nil
}

func (f *fakeOwnedOpsRepo) CreateWithPrincipal(_ context.Context, op operations.Operation, p operations.Principal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	op.Principal = p
	cp := op
	f.ops[op.ID] = &cp
	return nil
}

func (f *fakeOwnedOpsRepo) Get(_ context.Context, id string) (*operations.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	op, ok := f.ops[id]
	if !ok {
		return nil, operations.ErrNotFound
	}
	cp := *op
	return &cp, nil
}

func (f *fakeOwnedOpsRepo) List(_ context.Context, _ operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}

func (f *fakeOwnedOpsRepo) ListOwned(_ context.Context, _ operations.ListFilter, owner operations.Owner) ([]operations.Operation, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceErr != nil {
		return nil, "", f.forceErr
	}
	var out []operations.Operation
	for _, op := range f.ops {
		if !ownerMatches(op, owner) {
			continue
		}
		out = append(out, *op)
	}
	return out, "", nil
}

func (f *fakeOwnedOpsRepo) MarkDone(_ context.Context, id string, resp *anypb.Any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	op, ok := f.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Response = resp
	return nil
}

func (f *fakeOwnedOpsRepo) MarkError(_ context.Context, id string, st *genstatus.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	op, ok := f.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Error = st
	return nil
}

func (f *fakeOwnedOpsRepo) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	op, ok := f.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	if op.Done {
		return operations.ErrAlreadyDone
	}
	op.Done = true
	op.Error = &genstatus.Status{Code: 1, Message: "operation cancelled"}
	return nil
}

var (
	_ operations.Repo               = (*fakeOwnedOpsRepo)(nil)
	_ operations.OwnedOperationRepo = (*fakeOwnedOpsRepo)(nil)
)

// opUserCtx — ctx с доверенным principal'ом user/<id> (как после JWT-валидации
// на gateway, проброшенной через principal-extract).

func opUserCtx(id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: id, DisplayName: "test"})
}

// opSACtx — ctx с principal service_account/<id>.

func opSACtx(id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "service_account", ID: id, DisplayName: "test"})
}

// seedInFlight — кладёт in-flight (done=false) операцию владельца (type,id).

func seedInFlight(repo *fakeOwnedOpsRepo, principalType, principalID string) *operations.Operation {
	op := &operations.Operation{
		ID:          ids.NewID(ids.PrefixOperationCompute),
		Description: "unit-test op",
		Principal:   operations.Principal{Type: principalType, ID: principalID, DisplayName: "test"},
	}
	repo.seed(op)
	return op
}

// nonexistentOpID — well-formed (epd-prefix) operation-id, заведомо не созданный.

const nonexistentOpID = "epd00000000000000000"

// --- Валидация аргумента ---
