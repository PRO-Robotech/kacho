// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package repomock — in-memory моки port-интерфейсов use-case-слоя kacho-storage
// (volume.Reader/Writer, volume.GeoClient/IAMClient, snapshot.Repo, disktype.Repo)
// на функциях-полях + in-memory operations.Repo (OpsRepo) с детерминированным
// AwaitOpDone-хелпером для async-LRO. Для unit-тестов use-case БЕЗ Postgres/grpc
// (иначе adapter протёк бы в use-case — architecture.md). Незаданное поле-функция →
// метод паникует (тест обязан задать нужный путь явно).
package repomock

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
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
	ChangeDiskTypeFn func(ctx context.Context, id, diskTypeID string) (*domain.Volume, error)
	InsertFunc       func(ctx context.Context, v *domain.Volume) (*domain.Volume, error)
	UpdateFunc       func(ctx context.Context, id string, u volume.VolumeUpdate) (*domain.Volume, error)
	DeleteFunc       func(ctx context.Context, id string) error
	AttachFunc       func(ctx context.Context, a *domain.VolumeAttachment) error
	DetachFunc       func(ctx context.Context, volumeID, instanceID string) error
}

// Insert — zoneRegionID (регион зоны тома) энфорсится в SQL-CAS реального repo;
// мок его не моделирует и передаёт вызов дальше без него.
func (m *VolumeWriter) Insert(ctx context.Context, v *domain.Volume, _ string) (*domain.Volume, []ownerregister.Registration, error) {
	res, err := m.InsertFunc(ctx, v)
	if err != nil {
		return nil, nil, err
	}
	return res, mockRegistrations(fgaregister.VolumeItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *VolumeWriter) Update(ctx context.Context, id string, u volume.VolumeUpdate) (*domain.Volume, []ownerregister.Registration, error) {
	res, err := m.UpdateFunc(ctx, id, u)
	if err != nil {
		return nil, nil, err
	}
	if !u.LabelsSet {
		return res, nil, nil
	}
	return res, mockRegistrations(fgaregister.VolumeItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *VolumeWriter) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }
func (m *VolumeWriter) Attach(ctx context.Context, a *domain.VolumeAttachment) error {
	return m.AttachFunc(ctx, a)
}
func (m *VolumeWriter) Detach(ctx context.Context, volumeID, instanceID string) error {
	return m.DetachFunc(ctx, volumeID, instanceID)
}

// PeerClient — мок volume.GeoClient / image.GeoClient и volume.IAMClient /
// snapshot.IAMClient / image.IAMClient (EnsureZoneExists/EnsureRegionExists/
// EnsureProjectExists на функциях-полях).
type PeerClient struct {
	EnsureZoneFunc    func(ctx context.Context, zoneID string) error
	RegionOfZoneFunc  func(ctx context.Context, zoneID string) (string, error)
	EnsureRegionFunc  func(ctx context.Context, regionID string) error
	EnsureProjectFunc func(ctx context.Context, projectID string) error
	ZonesOfRegionFunc func(ctx context.Context, regionID string) ([]string, error)
}

func (m *PeerClient) EnsureZoneExists(ctx context.Context, zoneID string) error {
	return m.EnsureZoneFunc(ctx, zoneID)
}

// RegionOfZone — авторитетный регион зоны. Регион из имени зоны НЕ выводится,
// поэтому фейк обязан его назвать: незаданный RegionOfZoneFunc → "region-1"
// (дефолт фикстур), что делает соответствие явным, а не выводимым.
func (m *PeerClient) RegionOfZone(ctx context.Context, zoneID string) (string, error) {
	if m.RegionOfZoneFunc != nil {
		return m.RegionOfZoneFunc(ctx, zoneID)
	}
	return "region-1", nil
}

func (m *PeerClient) EnsureRegionExists(ctx context.Context, regionID string) error {
	return m.EnsureRegionFunc(ctx, regionID)
}

// ZonesOfRegion — зоны региона по данным владельца Geography. Как и RegionOfZone,
// принадлежность зоны региону НЕ выводится из имени, поэтому фейк обязан её
// назвать: незаданный ZonesOfRegionFunc → зоны дефолтного региона фикстур.
func (m *PeerClient) ZonesOfRegion(ctx context.Context, regionID string) ([]string, error) {
	if m.ZonesOfRegionFunc != nil {
		return m.ZonesOfRegionFunc(ctx, regionID)
	}
	return []string{"region-1-a", "region-1-b"}, nil
}
func (m *PeerClient) EnsureProjectExists(ctx context.Context, projectID string) error {
	return m.EnsureProjectFunc(ctx, projectID)
}

// ImageReader — мок image.Reader на функциях-полях.
type ImageReader struct {
	GetFunc         func(ctx context.Context, id string) (*domain.Image, error)
	ListFunc        func(ctx context.Context, p image.Pagination) ([]*domain.Image, string, error)
	GetInternalFunc func(ctx context.Context, id string) (*domain.Image, error)
}

func (m *ImageReader) Get(ctx context.Context, id string) (*domain.Image, error) {
	return m.GetFunc(ctx, id)
}
func (m *ImageReader) List(ctx context.Context, p image.Pagination) ([]*domain.Image, string, error) {
	return m.ListFunc(ctx, p)
}
func (m *ImageReader) GetInternal(ctx context.Context, id string) (*domain.Image, error) {
	return m.GetInternalFunc(ctx, id)
}

// ImageWriter — мок image.Writer на функциях-полях.
type ImageWriter struct {
	InsertFunc   func(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, error)
	UpdateFunc   func(ctx context.Context, id string, u image.ImageUpdate) (*domain.Image, error)
	DeleteFunc   func(ctx context.Context, id string) error
	RegisterFunc func(ctx context.Context, i *domain.Image) (*domain.Image, error)
}

func (m *ImageWriter) Insert(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, []ownerregister.Registration, error) {
	res, err := m.InsertFunc(ctx, i, regionZones)
	if err != nil {
		return nil, nil, err
	}
	return res, mockRegistrations(fgaregister.ImageItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *ImageWriter) Update(ctx context.Context, id string, u image.ImageUpdate) (*domain.Image, []ownerregister.Registration, error) {
	res, err := m.UpdateFunc(ctx, id, u)
	if err != nil {
		return nil, nil, err
	}
	if !u.LabelsSet {
		return res, nil, nil
	}
	return res, mockRegistrations(fgaregister.ImageItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *ImageWriter) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }

// Register — регистрация образа, внесённого в хранилище вне облака. Владение
// регистрируется той же транзакцией, что и строка, поэтому мок возвращает
// регистрацию так же, как Insert: проба, ждущая её, не должна зеленеть на моке,
// который её не отдаёт.
func (m *ImageWriter) Register(ctx context.Context, i *domain.Image) (*domain.Image, []ownerregister.Registration, error) {
	res, err := m.RegisterFunc(ctx, i)
	if err != nil {
		return nil, nil, err
	}
	return res, mockRegistrations(fgaregister.ImageItem(res.ProjectID, res.ID, res.Labels)), nil
}

// SnapshotRepo — мок snapshot.Repo на функциях-полях.
type SnapshotRepo struct {
	CopyFn     func(ctx context.Context, s *domain.Snapshot, sourceID, targetZone string) (*domain.Snapshot, error)
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
func (m *SnapshotRepo) Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, []ownerregister.Registration, error) {
	res, err := m.InsertFunc(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	return res, mockRegistrations(fgaregister.SnapshotItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *SnapshotRepo) Update(ctx context.Context, id string, u snapshot.SnapshotUpdate) (*domain.Snapshot, []ownerregister.Registration, error) {
	res, err := m.UpdateFunc(ctx, id, u)
	if err != nil {
		return nil, nil, err
	}
	if !u.LabelsSet {
		return res, nil, nil
	}
	return res, mockRegistrations(fgaregister.SnapshotItem(res.ProjectID, res.ID, res.Labels)), nil
}
func (m *SnapshotRepo) Delete(ctx context.Context, id string) error { return m.DeleteFunc(ctx, id) }

// DiskTypeRepo — мок disktype.Repo на функциях-полях.
type DiskTypeRepo struct {
	GetFunc    func(ctx context.Context, id string) (*domain.DiskType, error)
	ListFunc   func(ctx context.Context, p disktype.Pagination) ([]*domain.DiskType, string, error)
	InsertFunc func(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error)
	UpdateFunc func(ctx context.Context, id string, u disktype.DiskTypeUpdate) (*domain.DiskType, error)
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
func (m *DiskTypeRepo) Update(ctx context.Context, id string, u disktype.DiskTypeUpdate) (*domain.DiskType, error) {
	return m.UpdateFunc(ctx, id, u)
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

// ---- operations.OwnedOperationRepo ----
//
// Зеркалит SQL-предикат pgRepo: доступ только владельцу (пара principal
// type/id); чужой/несуществующий id → ErrNotFound (no-leak).

func opsOwnerMatches(op *operations.Operation, owner operations.Owner) bool {
	return op.Principal.Type == owner.PrincipalType && op.Principal.ID == owner.PrincipalID
}

// GetOwned возвращает операцию только если она принадлежит owner.
func (r *OpsRepo) GetOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || !opsOwnerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	cp := *op
	return &cp, nil
}

// CancelOwned отменяет операцию owner'а; чужая/нет → ErrNotFound, терминальная →
// ErrAlreadyDone.
func (r *OpsRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || !opsOwnerMatches(op, owner) {
		return nil, operations.ErrNotFound
	}
	if op.Done {
		return nil, operations.ErrAlreadyDone
	}
	op.Done = true
	cp := *op
	return &cp, nil
}

// ListOwned — те же фильтры, что у List, AND предикат владения.
func (r *OpsRepo) ListOwned(_ context.Context, f operations.ListFilter, owner operations.Owner) ([]operations.Operation, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []operations.Operation
	for _, op := range r.ops {
		if !opsOwnerMatches(op, owner) {
			continue
		}
		if f.ResourceID != "" && op.ResourceID != f.ResourceID {
			continue
		}
		out = append(out, *op)
	}
	return out, "", nil
}

var _ operations.OwnedOperationRepo = (*OpsRepo)(nil)

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

// ChangeDiskType — подстановка смены класса. Дублёр обязан выполнять контракт
// настоящего: незаданная функция ОТКАЗЫВАЕТ, а не отвечает успехом. Молча принявший
// вызов дублёр прятал бы ровно тот путь, ради которого его подставляют.
func (w *VolumeWriter) ChangeDiskType(ctx context.Context, id, diskTypeID string) (*domain.Volume, error) {
	if w.ChangeDiskTypeFn != nil {
		return w.ChangeDiskTypeFn(ctx, id, diskTypeID)
	}
	return nil, errors.New("repomock: ChangeDiskTypeFn is not set")
}

// Copy — подстановка копирования снимка. Незаданная функция ОТКАЗЫВАЕТ: дублёр,
// молча отвечающий успехом там, где настоящий делает работу, прячет ровно тот путь,
// ради которого его подставляют.
func (r *SnapshotRepo) Copy(ctx context.Context, s *domain.Snapshot, sourceID, targetZone string) (*domain.Snapshot, error) {
	if r.CopyFn != nil {
		return r.CopyFn(ctx, s, sourceID, targetZone)
	}
	return nil, errors.New("repomock: CopyFn is not set")
}

// Compile-time проверки соответствия портам.
var (
	_ volume.Reader      = (*VolumeReader)(nil)
	_ volume.Writer      = (*VolumeWriter)(nil)
	_ volume.GeoClient   = (*PeerClient)(nil)
	_ volume.IAMClient   = (*PeerClient)(nil)
	_ snapshot.IAMClient = (*PeerClient)(nil)
	_ snapshot.Repo      = (*SnapshotRepo)(nil)
	_ image.Reader       = (*ImageReader)(nil)
	_ image.Writer       = (*ImageWriter)(nil)
	_ image.GeoClient    = (*PeerClient)(nil)
	_ image.IAMClient    = (*PeerClient)(nil)
	_ disktype.Repo      = (*DiskTypeRepo)(nil)
	_ operations.Repo    = (*OpsRepo)(nil)
)

// ── строки доставки, которые мок отдаёт вместо БД ──────────────────────────

// fgaMockStampBase / fgaStampSeq — детерминированный монотонный штамп вместо
// часов БД. In-memory-часов writer-транзакции у мока нет, поэтому штамп идёт
// шагом от фиксированной точки.
//
// ПОЧЕМУ МОК ОБЯЗАН ШТАМПОВАТЬ, А НЕ ОТДАВАТЬ НУЛЬ. Дублёр, снисходительнее
// настоящего, делает невидимым именно тот дефект, ради которого его
// подставляют: общий регистратор ОТВЕРГАЕТ регистрацию без версии, и мок,
// отдающий ноль, молча превратил бы каждую пробу доставки в «ничего не
// доставлено» — то есть в зелёное отрицание на полностью мёртвом пути.
// Значение при этом намеренно НЕ похоже на «сейчас»: подставное, отличимое от
// настоящего, не даёт спутать проброс с выдумыванием на месте.
var (
	fgaMockStampBase = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	fgaStampMu       sync.Mutex
	fgaStampSeq      int64
)

func nextFGAStamp() time.Time {
	fgaStampMu.Lock()
	defer fgaStampMu.Unlock()
	fgaStampSeq++
	return fgaMockStampBase.Add(time.Duration(fgaStampSeq) * time.Millisecond)
}

// mockRegistrations — строка доставки, которую реальный repo вернул бы из
// writer-транзакции для того же item'а. Содержимое берётся из ТОГО ЖЕ
// конструктора item'а, что и в проде: разойтись они не могут.
func mockRegistrations(item fgaregister.Item) []ownerregister.Registration {
	object := item.Tuple.Object
	id := object
	if i := strings.IndexByte(object, ':'); i >= 0 {
		id = object[i+1:]
	}
	return []ownerregister.Registration{{
		// Все три поля берутся у уже собранного кортежа: это ПЕРЕСБОРКА, а не
		// решение о тройке — решает её конструктор домена, который дублёр и
		// зовёт. Тем же признаком её отличает гейт сверки намерения с приёмной
		// стороной, поэтому объект тоже берётся селектором, а не через local.
		Tuple: ownerregister.Tuple{
			SubjectID: item.Tuple.SubjectID,
			Relation:  item.Tuple.Relation,
			Object:    item.Tuple.Object,
		},
		TraceID:         id,
		Labels:          item.Labels,
		ParentProjectID: item.ParentProjectID,
		// Цепь предков собирается ТЕМ ЖЕ вызовом, что в проде: дублёр обязан
		// выполнять контракт настоящего, иначе он снисходительнее продукта.
		ParentChain:   ownerregister.ParentChain(nil, item.ParentProjectID, ""),
		SourceVersion: nextFGAStamp(),
	}}
}
