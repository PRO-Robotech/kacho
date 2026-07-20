// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package repomock — in-memory моки портов region/zone Repo и LRO operations.Repo
// для unit-тестов use-case (без Postgres; иначе adapter протек бы в use-case).
package repomock

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// RegionRepo — мок region.Repo на функциях-полях.
type RegionRepo struct {
	GetFunc         func(ctx context.Context, id string) (*domain.Region, error)
	GetInternalFunc func(ctx context.Context, id string) (*domain.Region, error)
	ListFunc        func(ctx context.Context, p region.Pagination) ([]*domain.Region, string, error)
	InsertFunc      func(ctx context.Context, r *domain.Region) (*domain.Region, error)
	UpdateFunc      func(ctx context.Context, id string, p region.UpdateParams) (*domain.Region, error)
	DeleteFunc      func(ctx context.Context, id string) error
}

// Get реализует region.Repo.
func (m *RegionRepo) Get(ctx context.Context, id string) (*domain.Region, error) {
	return m.GetFunc(ctx, id)
}

// GetInternal реализует region.Repo.
func (m *RegionRepo) GetInternal(ctx context.Context, id string) (*domain.Region, error) {
	return m.GetInternalFunc(ctx, id)
}

// List реализует region.Repo.
func (m *RegionRepo) List(ctx context.Context, p region.Pagination) ([]*domain.Region, string, error) {
	return m.ListFunc(ctx, p)
}

// Insert реализует region.Repo.
func (m *RegionRepo) Insert(ctx context.Context, r *domain.Region) (*domain.Region, error) {
	return m.InsertFunc(ctx, r)
}

// Update реализует region.Repo.
func (m *RegionRepo) Update(ctx context.Context, id string, p region.UpdateParams) (*domain.Region, error) {
	return m.UpdateFunc(ctx, id, p)
}

// Delete реализует region.Repo.
func (m *RegionRepo) Delete(ctx context.Context, id string) error {
	return m.DeleteFunc(ctx, id)
}

var _ region.Repo = (*RegionRepo)(nil)

// ZoneRepo — мок zone.Repo на функциях-полях.
type ZoneRepo struct {
	GetFunc         func(ctx context.Context, id string) (*domain.Zone, error)
	GetInternalFunc func(ctx context.Context, id string) (*domain.Zone, error)
	ListFunc        func(ctx context.Context, p zone.Pagination) ([]*domain.Zone, string, error)
	InsertFunc      func(ctx context.Context, z *domain.Zone) (*domain.Zone, error)
	UpdateFunc      func(ctx context.Context, id string, p zone.UpdateParams) (*domain.Zone, error)
	DeleteFunc      func(ctx context.Context, id string) error
}

// Get реализует zone.Repo.
func (m *ZoneRepo) Get(ctx context.Context, id string) (*domain.Zone, error) {
	return m.GetFunc(ctx, id)
}

// GetInternal реализует zone.Repo.
func (m *ZoneRepo) GetInternal(ctx context.Context, id string) (*domain.Zone, error) {
	return m.GetInternalFunc(ctx, id)
}

// List реализует zone.Repo.
func (m *ZoneRepo) List(ctx context.Context, p zone.Pagination) ([]*domain.Zone, string, error) {
	return m.ListFunc(ctx, p)
}

// Insert реализует zone.Repo.
func (m *ZoneRepo) Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error) {
	return m.InsertFunc(ctx, z)
}

// Update реализует zone.Repo.
func (m *ZoneRepo) Update(ctx context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
	return m.UpdateFunc(ctx, id, p)
}

// Delete реализует zone.Repo.
func (m *ZoneRepo) Delete(ctx context.Context, id string) error {
	return m.DeleteFunc(ctx, id)
}

var _ zone.Repo = (*ZoneRepo)(nil)

// OpsRepo — потокобезопасный in-memory operations.Repo для unit-тестов async
// use-case (без Postgres). Worker зовёт Create (sync) → MarkDone/MarkError
// (async); тест дожидается завершения через AwaitOpDone (поллит Get).
type OpsRepo struct {
	mu  sync.Mutex
	ops map[string]*operations.Operation
}

// NewOpsRepo создаёт пустой in-memory LRO-репозиторий.
func NewOpsRepo() *OpsRepo { return &OpsRepo{ops: make(map[string]*operations.Operation)} }

// Create сохраняет новую операцию (done=false). Principal резолвится тем же
// приоритетом, что и pgRepo.Create (op.Principal → ctx → SystemPrincipal), чтобы
// ownership-scoped GetOwned/CancelOwned в тестах вели себя как прод: строка
// хранит creator-principal, а не zero-value.
func (r *OpsRepo) Create(ctx context.Context, op operations.Operation) error {
	if op.Principal == (operations.Principal{}) {
		op.Principal = operations.PrincipalFromContext(ctx) // SystemPrincipal-fallback без auth-ctx
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := op
	r.ops[op.ID] = &cp
	return nil
}

// CreateWithPrincipal — то же, но principal передан явно (мок его сохраняет для
// ownership-предиката GetOwned/CancelOwned).
func (r *OpsRepo) CreateWithPrincipal(_ context.Context, op operations.Operation, p operations.Principal) error {
	op.Principal = p
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := op
	r.ops[op.ID] = &cp
	return nil
}

// Get возвращает операцию по id (ErrNotFound если нет).
func (r *OpsRepo) Get(_ context.Context, id string) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return nil, operations.ErrNotFound
	}
	cp := *op
	return &cp, nil
}

// List в моке не используется (пустой результат).
func (r *OpsRepo) List(_ context.Context, _ operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}

// ListOwned возвращает операции owner'а. Ownership-предикат ТОТ ЖЕ, что у GetOwned
// (ownerMatches) — иначе мок разошёлся бы с pgRepo, где предикат живёт в SQL WHERE,
// и unit-тест перестал бы ловить утечку чужих операций в tenant-facing List.
// Пагинация/фильтры не моделируются (как и в List): порядок map-обхода
// недетерминирован, поэтому keyset-семантику проверяет integration-тест на pgRepo.
func (r *OpsRepo) ListOwned(_ context.Context, _ operations.ListFilter, owner operations.Owner) ([]operations.Operation, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]operations.Operation, 0, len(r.ops))
	for _, op := range r.ops {
		if ownerMatches(op, owner) {
			out = append(out, *op)
		}
	}
	return out, "", nil
}

// MarkDone финализирует операцию с response.
func (r *OpsRepo) MarkDone(_ context.Context, id string, response *anypb.Any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Response = response
	op.ModifiedAt = time.Now().UTC()
	return nil
}

// MarkError финализирует операцию с ошибкой (google.rpc.Status).
func (r *OpsRepo) MarkError(_ context.Context, id string, errStatus *status.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Error = errStatus
	op.ModifiedAt = time.Now().UTC()
	return nil
}

// Cancel помечает незавершённую операцию отменённой.
func (r *OpsRepo) Cancel(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	if op.Done {
		return operations.ErrAlreadyDone
	}
	op.Done = true
	op.Error = &status.Status{Code: 1, Message: "operation cancelled"}
	return nil
}

// ownerMatches зеркалит ownerPredicateSQL corelib'а: match по паре
// (principal_type, principal_id). Account-ветка предиката в geo инертна (каталог
// cluster-scoped, без account-metadata — операции пишутся с account_id=NULL), а
// operations.Operation вообще не проецирует account_id, поэтому в моке её нет.
func ownerMatches(op *operations.Operation, owner operations.Owner) bool {
	return op.Principal.Type == owner.PrincipalType && op.Principal.ID == owner.PrincipalID
}

// GetOwned возвращает операцию по id ТОЛЬКО если она принадлежит owner. Нет
// такой ИЛИ не владелец → ErrNotFound (no-leak, неотличимо). Зеркалит
// pgRepo.GetOwned.
func (r *OpsRepo) GetOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || !ownerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	cp := *op
	return &cp, nil
}

// CancelOwned атомарно (под mutex'ом) отменяет операцию owner'а. Идемпотентно на
// уже-CANCELLED (→ тот же Operation); на терминале SUCCESS/ERROR → ErrAlreadyDone;
// чужая/нет → ErrNotFound. Зеркалит pgRepo.CancelOwned.
func (r *OpsRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || !ownerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	if op.Done {
		// Уже CANCELLED (error_code=1 == codes.Canceled) → идемпотентно OK.
		if op.Error != nil && op.Error.GetCode() == 1 {
			cp := *op
			return &cp, nil
		}
		return nil, operations.ErrAlreadyDone // терминал SUCCESS/ERROR
	}
	op.Done = true
	op.Error = &status.Status{Code: 1, Message: "operation cancelled"}
	op.ModifiedAt = time.Now().UTC()
	cp := *op
	return &cp, nil
}

var (
	_ operations.Repo               = (*OpsRepo)(nil)
	_ operations.OwnedOperationRepo = (*OpsRepo)(nil)
)

// AwaitOpDone детерминированно ждёт Done==true вместо time.Sleep (worker
// финализирует операцию в отдельной goroutine). Таймаут 2s.
func AwaitOpDone(t *testing.T, r *OpsRepo, opID string) *operations.Operation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		op, err := r.Get(context.Background(), opID)
		if err == nil && op.Done {
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not finish within 2s", opID)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
