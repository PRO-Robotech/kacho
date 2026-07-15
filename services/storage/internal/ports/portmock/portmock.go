// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package portmock — in-memory моки port-интерфейсов use-case-слоя kacho-storage
// (volume.Reader/Writer, volume.GeoClient/IAMClient, snapshot.Repo, disktype.Repo)
// на функциях-полях + in-memory operations.Repo (OpsRepo) с детерминированным
// AwaitOpDone-хелпером для async-LRO. Для unit-тестов use-case БЕЗ Postgres/grpc
// (иначе adapter протёк бы в use-case — architecture.md). Незаданное поле-функция →
// метод паникует (тест обязан задать нужный путь явно).
package portmock

import (
	"context"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
)

// VolumeReader — мок volume.Reader на функциях-полях.
type VolumeReader struct {
	GetFunc             func(ctx context.Context, id string) (*domain.Volume, error)
	ListFunc            func(ctx context.Context, p volume.Pagination) ([]*domain.Volume, string, error)
	GetInternalFunc     func(ctx context.Context, id string) (*domain.Volume, error)
	ListAttachmentsFunc func(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error)
}

func (m *VolumeReader) Get(ctx context.Context, id string) (*domain.Volume, error) {
	return m.GetFunc(ctx, id)
}
func (m *VolumeReader) List(ctx context.Context, p volume.Pagination) ([]*domain.Volume, string, error) {
	return m.ListFunc(ctx, p)
}
func (m *VolumeReader) GetInternal(ctx context.Context, id string) (*domain.Volume, error) {
	return m.GetInternalFunc(ctx, id)
}
func (m *VolumeReader) ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error) {
	return m.ListAttachmentsFunc(ctx, instanceIDs)
}

// VolumeWriter — мок volume.Writer на функциях-полях.
type VolumeWriter struct {
	InsertFunc func(ctx context.Context, v *domain.Volume) (*domain.Volume, error)
	UpdateFunc func(ctx context.Context, id string, u volume.VolumeUpdate) (*domain.Volume, error)
	DeleteFunc func(ctx context.Context, id string) error
	AttachFunc func(ctx context.Context, a *domain.VolumeAttachment) error
	DetachFunc func(ctx context.Context, volumeID, instanceID string) error
}

func (m *VolumeWriter) Insert(ctx context.Context, v *domain.Volume) (*domain.Volume, error) {
	return m.InsertFunc(ctx, v)
}
func (m *VolumeWriter) Update(ctx context.Context, id string, u volume.VolumeUpdate) (*domain.Volume, error) {
	return m.UpdateFunc(ctx, id, u)
}
func (m *VolumeWriter) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }
func (m *VolumeWriter) Attach(ctx context.Context, a *domain.VolumeAttachment) error {
	return m.AttachFunc(ctx, a)
}
func (m *VolumeWriter) Detach(ctx context.Context, volumeID, instanceID string) error {
	return m.DetachFunc(ctx, volumeID, instanceID)
}

// PeerClient — мок volume.GeoClient и volume.IAMClient / snapshot.IAMClient
// (идентичная EnsureZoneExists/EnsureProjectExists-форма на функциях-полях).
type PeerClient struct {
	EnsureZoneFunc    func(ctx context.Context, zoneID string) error
	EnsureProjectFunc func(ctx context.Context, projectID string) error
}

func (m *PeerClient) EnsureZoneExists(ctx context.Context, zoneID string) error {
	return m.EnsureZoneFunc(ctx, zoneID)
}
func (m *PeerClient) EnsureProjectExists(ctx context.Context, projectID string) error {
	return m.EnsureProjectFunc(ctx, projectID)
}

// SnapshotRepo — мок snapshot.Repo на функциях-полях.
type SnapshotRepo struct {
	GetFunc    func(ctx context.Context, id string) (*domain.Snapshot, error)
	ListFunc   func(ctx context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error)
	InsertFunc func(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, error)
	UpdateFunc func(ctx context.Context, id string, u snapshot.SnapshotUpdate) (*domain.Snapshot, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (m *SnapshotRepo) Get(ctx context.Context, id string) (*domain.Snapshot, error) {
	return m.GetFunc(ctx, id)
}
func (m *SnapshotRepo) List(ctx context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error) {
	return m.ListFunc(ctx, p)
}
func (m *SnapshotRepo) Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
	return m.InsertFunc(ctx, s)
}
func (m *SnapshotRepo) Update(ctx context.Context, id string, u snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
	return m.UpdateFunc(ctx, id, u)
}
func (m *SnapshotRepo) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }

// DiskTypeRepo — мок disktype.Repo на функциях-полях.
type DiskTypeRepo struct {
	GetFunc    func(ctx context.Context, id string) (*domain.DiskType, error)
	ListFunc   func(ctx context.Context, p disktype.Pagination) ([]*domain.DiskType, string, error)
	InsertFunc func(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error)
	UpdateFunc func(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (m *DiskTypeRepo) Get(ctx context.Context, id string) (*domain.DiskType, error) {
	return m.GetFunc(ctx, id)
}
func (m *DiskTypeRepo) List(ctx context.Context, p disktype.Pagination) ([]*domain.DiskType, string, error) {
	return m.ListFunc(ctx, p)
}
func (m *DiskTypeRepo) Insert(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error) {
	return m.InsertFunc(ctx, d)
}
func (m *DiskTypeRepo) Update(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error) {
	return m.UpdateFunc(ctx, id, name, description, zoneIDs, performanceTier)
}
func (m *DiskTypeRepo) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }

// ---- operations.Repo (in-memory, для async-LRO unit-тестов) ----

// OpsRepo — in-memory реализация kacho-corelib/operations.Repo. Async-worker
// (operations.Run) вызывает MarkDone/MarkError на этой строке; тест ждёт терминала
// через AwaitOpDone (детерминированный поллинг, не фиксированный time.Sleep).
type OpsRepo struct {
	mu  sync.Mutex
	ops map[string]*operations.Operation
}

// NewOpsRepo создаёт пустой OpsRepo.
func NewOpsRepo() *OpsRepo { return &OpsRepo{ops: make(map[string]*operations.Operation)} }

// Create сохраняет операцию (done=false).
func (r *OpsRepo) Create(_ context.Context, op operations.Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := op
	r.ops[op.ID] = &cp
	return nil
}

// CreateWithPrincipal сохраняет операцию с явным principal'ом (operations.Repo iface).
func (r *OpsRepo) CreateWithPrincipal(_ context.Context, op operations.Operation, p operations.Principal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := op
	cp.Principal = p
	r.ops[op.ID] = &cp
	return nil
}

// Get возвращает shallow-копию операции.
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

// List возвращает операции (фильтр по ResourceID, если задан).
func (r *OpsRepo) List(_ context.Context, f operations.ListFilter) ([]operations.Operation, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []operations.Operation
	for _, op := range r.ops {
		if f.ResourceID != "" && op.ResourceID != f.ResourceID {
			continue
		}
		out = append(out, *op)
	}
	return out, "", nil
}

// MarkDone помечает операцию завершённой с response (терминал success).
func (r *OpsRepo) MarkDone(_ context.Context, id string, resp *anypb.Any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Response = resp
	return nil
}

// MarkError помечает операцию завершённой с ошибкой (терминал error).
func (r *OpsRepo) MarkError(_ context.Context, id string, errStatus *status.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	op.Error = errStatus
	return nil
}

// Cancel помечает операцию завершённой (CANCELLED).
func (r *OpsRepo) Cancel(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return operations.ErrNotFound
	}
	op.Done = true
	return nil
}

// ---- await-helper для async Operation worker'ов ----

// TestingT — минимальный интерфейс из *testing.T для AwaitOpDone.
type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// AwaitOpDone детерминированно ждёт терминала операции (Operation.Done) — заменяет
// фиксированный time.Sleep: возвращает управление сразу как worker пометил done,
// падает через 2s (защита от зависшего теста). Поллинг с малым шагом — не «спать
// N секунд и надеяться».
func AwaitOpDone(t TestingT, r *OpsRepo, opID string) *operations.Operation {
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
		time.Sleep(5 * time.Millisecond)
	}
}

// Compile-time проверки соответствия портам.
var (
	_ volume.Reader      = (*VolumeReader)(nil)
	_ volume.Writer      = (*VolumeWriter)(nil)
	_ volume.GeoClient   = (*PeerClient)(nil)
	_ volume.IAMClient   = (*PeerClient)(nil)
	_ snapshot.IAMClient = (*PeerClient)(nil)
	_ snapshot.Repo      = (*SnapshotRepo)(nil)
	_ disktype.Repo      = (*DiskTypeRepo)(nil)
	_ operations.Repo    = (*OpsRepo)(nil)
)
