// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package portmock содержит in-memory fake-реализации port-интерфейсов из
// `internal/ports` плюс helper'ы для ожидания async-Operation'ов. Используется
// unit-тестами use-case-пакетов (`internal/apps/kacho/api/<resource>`) и
// `internal/handler`.
//
// Зависит только от `internal/ports`, `internal/domain` и `kacho-corelib/operations`
// — НЕ от use-case-пакетов, поэтому их white-box тесты могут его импортировать
// без import-cycle.
package portmock

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/status"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ---- InstanceRepo ----

// InstanceRepo — in-memory InstanceRepo.
type InstanceRepo struct {
	mu   sync.Mutex
	data map[string]*domain.Instance
	// deletingSince — момент входа строки в DELETING. Держится отдельной картой,
	// а не полем domain-сущности: это координата саги, а не свойство машины, и в
	// публичной проекции ресурса ей делать нечего.
	deletingSince map[string]time.Time
	// LastUpdateEmitLabels — последнее значение emitLabelsRegister, переданное в
	// Update (epic RSAB β, D-β6). nil — Update ещё не вызывался. Позволяет
	// use-case-тесту проверить решение «labels ∈ mask → эмитить register-intent».
	LastUpdateEmitLabels *bool
}

// NewInstanceRepo создаёт пустой InstanceRepo.
func NewInstanceRepo() *InstanceRepo {
	return &InstanceRepo{
		data:          make(map[string]*domain.Instance),
		deletingSince: make(map[string]time.Time),
	}
}

// Seed добавляет ВМ напрямую.
func (r *InstanceRepo) Seed(in *domain.Instance) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[in.ID] = in
}

// Get возвращает ВМ по id. Отдаёт shallow-КОПИЮ (не live-указатель) — зеркалит
// pg-адаптер, где каждый Get — свежий scan строки: конкурентные worker'ы,
// заполняющие read-only NIC-зеркало (applyNicMirror пишет in.NetworkInterfaces),
// не делят один *domain.Instance (иначе data-race на общем указателе, чего в
// проде нет).
func (r *InstanceRepo) Get(_ context.Context, id string) (*domain.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.data[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	cp := *in
	return &cp, nil
}

// List возвращает ВМ по проекту (без authz-измерения — см. ports.InstanceFilter).
func (r *InstanceRepo) List(_ context.Context, f ports.InstanceFilter, _ ports.Pagination) ([]*domain.Instance, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Instance
	for _, in := range r.data {
		if f.ProjectID != "" && in.ProjectID != f.ProjectID {
			continue
		}
		out = append(out, in)
	}
	return out, "", nil
}

// Insert вставляет строку ВМ (без привязок — storage-split).
func (r *InstanceRepo) Insert(_ context.Context, in *domain.Instance) (*domain.Instance, []ownerregister.Registration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.Name != "" {
		for _, x := range r.data {
			if x.ProjectID == in.ProjectID && x.Name == in.Name {
				return nil, nil, ports.ErrAlreadyExists
			}
		}
	}
	r.data[in.ID] = in
	return in, mockRegistrations(in), nil
}

// Update обновляет ВМ. Записывает emitLabelsRegister в LastUpdateEmitLabels
// (epic RSAB β, D-β6) для проверки use-case-тестом.
func (r *InstanceRepo) Update(_ context.Context, in *domain.Instance, emitLabelsRegister bool, _ []string) (*domain.Instance, []ownerregister.Registration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	flag := emitLabelsRegister
	r.LastUpdateEmitLabels = &flag
	if _, ok := r.data[in.ID]; !ok {
		return nil, nil, ports.ErrNotFound
	}
	r.data[in.ID] = in
	if !emitLabelsRegister {
		return in, nil, nil
	}
	return in, mockRegistrations(in), nil
}

// mockRegistrations — строка доставки, которую реальный repo вернул бы из
// writer-транзакции.
//
// ПОЧЕМУ ДУБЛЁР ОБЯЗАН ШТАМПОВАТЬ ВЕРСИЮ. Общий регистратор регистрацию без
// версии ОТВЕРГАЕТ. Дублёр, отдающий ноль, молча превратил бы каждую пробу
// доставки в «ничего не доставлено» — зелёное отрицание на мёртвом пути, то
// есть ровно тот дефект, ради которого дублёра и подставляют. Значение
// намеренно НЕ похоже на «сейчас»: подставное, отличимое от настоящего, не даёт
// спутать проброс с выдумыванием на месте.
func mockRegistrations(in *domain.Instance) []ownerregister.Registration {
	tuple, ok := fgaintent.ProjectHierarchyTuple("Instance", in.ID, in.ProjectID)
	if !ok {
		return nil
	}
	fgaStampMu.Lock()
	fgaStampSeq++
	seq := fgaStampSeq
	fgaStampMu.Unlock()
	return []ownerregister.Registration{{
		Tuple: ownerregister.Tuple{
			SubjectID: tuple.SubjectID,
			Relation:  tuple.Relation,
			Object:    tuple.Object,
		},
		TraceID:         in.ID,
		Labels:          in.Labels,
		ParentProjectID: in.ProjectID,
		// Цепь предков собирается ТЕМ ЖЕ вызовом, что в проде. Дублёр, молча
		// отдающий доставку без предков, снисходительнее настоящего ровно в том,
		// ради чего его подставляют: проба зеленела бы на форме, которой в
		// writer-транзакции не бывает.
		ParentChain:   ownerregister.ParentChain(nil, in.ProjectID, ""),
		SourceVersion: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Millisecond),
	}}
}

var (
	fgaStampMu  sync.Mutex
	fgaStampSeq int64
)

// SetStatusCAS — in-memory CAS: атомарно переводит status из expected в next.
// Если row не существует → ErrNotFound; если текущий status != expected →
// ErrFailedPrecondition (mirrors DB-уровень в repo.InstanceRepo.SetStatusCAS).
func (r *InstanceRepo) SetStatusCAS(_ context.Context, id string, expected, next domain.InstanceStatus) (*domain.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.data[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	if in.Status != expected {
		return nil, fmt.Errorf("%w: state transition not allowed from current status", ports.ErrFailedPrecondition)
	}
	in.Status = next
	return in, nil
}

// GateForAttach — CAS-гейт attach-саги: инстанс ∈ {RUNNING, STOPPED} → возвращает
// zone/project/name; иначе FailedPrecondition; нет инстанса → NotFound.
func (r *InstanceRepo) GateForAttach(_ context.Context, id string) (string, string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.data[id]
	if !ok {
		return "", "", "", fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
	}
	if in.Status != domain.InstanceStatusRunning && in.Status != domain.InstanceStatusStopped {
		return "", "", "", fmt.Errorf("%w: Instance must be RUNNING or STOPPED", ports.ErrFailedPrecondition)
	}
	return in.ZoneID, in.ProjectID, in.Name, nil
}

// MarkDeleting переводит инстанс в DELETING (идемпотентно). Нет инстанса → NotFound.
func (r *InstanceRepo) MarkDeleting(_ context.Context, id string) (*domain.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.data[id]
	if !ok {
		return nil, fmt.Errorf("%w: Instance %s not found", ports.ErrNotFound, id)
	}
	if in.Status != domain.InstanceStatusDeleting {
		in.Status = domain.InstanceStatusDeleting
		// Отметка ставится ТОЛЬКО на фактическом переходе: повторный вызов не
		// вправе омолодить строку, иначе застрявшая машина вечно моложе отсрочки.
		r.deletingSince[id] = time.Now()
	}
	cp := *in
	return &cp, nil
}

// ListStuckDeleting — машины в DELETING, вошедшие туда раньше чем olderThan назад.
func (r *InstanceRepo) ListStuckDeleting(_ context.Context, olderThan time.Duration) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var out []string
	for id, in := range r.data {
		if in.Status != domain.InstanceStatusDeleting {
			continue
		}
		since, ok := r.deletingSince[id]
		if !ok || since.After(cutoff) {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// MergeMetadata атомарно применяет delete+upsert дельту (под r.mu — зеркалит
// row-level-lock атомарность DB-адаптера: read+merge+write под одним локом).
func (r *InstanceRepo) MergeMetadata(_ context.Context, id string, del []string, upsert map[string]string) (*domain.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.data[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	md := map[string]string{}
	for k, v := range in.Metadata {
		md[k] = v
	}
	for _, k := range del {
		delete(md, k)
	}
	for k, v := range upsert {
		md[k] = v
	}
	in.Metadata = md
	return in, nil
}

// Delete удаляет строку ВМ (финальный шаг delete-саги; привязки уже сняты в
// use-case через storage/vpc Detach). Нет инстанса → NotFound.
func (r *InstanceRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[id]; !ok {
		return ports.ErrNotFound
	}
	delete(r.data, id)
	return nil
}

// ---- MachineTypeRepo ----

// MachineTypeRepo — in-memory MachineTypeRepo (COMP-1 F7). Enforces UNIQUE(name)
// on Insert (mirrors the DB-backstop → ports.ErrAlreadyExists) and supports the
// name=/family=/minGpus= filters. No cursor pagination (unit-level).
type MachineTypeRepo struct {
	mu   sync.Mutex
	data map[string]*domain.MachineType
}

// NewMachineTypeRepo создаёт пустой MachineTypeRepo.
func NewMachineTypeRepo() *MachineTypeRepo {
	return &MachineTypeRepo{data: make(map[string]*domain.MachineType)}
}

// Seed добавляет запись напрямую (для теста-фикстуры), минуя Insert-валидацию.
func (r *MachineTypeRepo) Seed(mt *domain.MachineType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[mt.ID] = mt
}

// Get возвращает machine-type по id.
func (r *MachineTypeRepo) Get(_ context.Context, id string) (*domain.MachineType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mt, ok := r.data[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return mt, nil
}

// List возвращает machine-type с whitelist-фильтрами (name=/family=/minGpus=).
func (r *MachineTypeRepo) List(_ context.Context, f ports.MachineTypeFilter, _ ports.Pagination) ([]*domain.MachineType, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.MachineType
	for _, mt := range r.data {
		if f.Name != "" && mt.Name != f.Name {
			continue
		}
		if f.Family != domain.MachineTypeFamilyUnspecified && mt.Family != f.Family {
			continue
		}
		if f.MinGPUs > 0 && mt.EffectiveResources.GPUs < f.MinGPUs {
			continue
		}
		out = append(out, mt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, "", nil
}

// Insert вставляет machine-type (UNIQUE(name) → ErrAlreadyExists).
func (r *MachineTypeRepo) Insert(_ context.Context, mt *domain.MachineType) (*domain.MachineType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.data {
		if existing.Name == mt.Name {
			return nil, ports.ErrAlreadyExists
		}
	}
	r.data[mt.ID] = mt
	return mt, nil
}

// Update обновляет machine-type (id отсутствует → ErrNotFound).
func (r *MachineTypeRepo) Update(_ context.Context, id string, u ports.MachineTypeUpdate) (*domain.MachineType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.data[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	// Мок обязан вести себя как настоящий репозиторий: применять ТОЛЬКО названные
	// колонки. Мок, применяющий всё, скрыл бы ровно тот дефект, ради которого набор
	// изменений и заведён.
	next := *cur
	if u.Description != nil {
		next.Description = *u.Description
	}
	if u.Family != nil {
		next.Family = *u.Family
	}
	if u.EffectiveResources != nil {
		next.EffectiveResources = *u.EffectiveResources
	}
	if u.AvailableZones != nil {
		next.AvailableZones = *u.AvailableZones
	}
	if u.Status != nil {
		next.Status = *u.Status
	}
	if u.LabelsSet {
		next.Labels = u.Labels
	}
	r.data[id] = &next
	return &next, nil
}

// Delete удаляет machine-type (id отсутствует → ErrNotFound).
func (r *MachineTypeRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[id]; !ok {
		return ports.ErrNotFound
	}
	delete(r.data, id)
	return nil
}

var _ ports.MachineTypeRepo = (*MachineTypeRepo)(nil)

// ---- ZoneRegistry ----

// ZoneRegistry — in-memory ports.ZoneRegistry (zone_id existence-check для
// Disk/Instance Create + Disk Relocate). В проде реализуется clients.GeoClient
// (geo.v1.ZoneService.Get) — Geography принадлежит kacho-geo.
type ZoneRegistry struct {
	mu   sync.Mutex
	data map[string]struct{} // set of known zoneIDs (existence-check)
	// Regions — явное zone→region соответствие (регион из имени НЕ выводится).
	Regions map[string]string
	// DefaultRegion — регион зон, не перечисленных в Regions ("" → "ru-central1").
	DefaultRegion string
	// Err — принудительная ошибка zone→region резолва (недоступность geo).
	Err error
}

// NewZoneRegistry создаёт ZoneRegistry с seed-зонами (ru-central1-{a,b,d} по умолчанию).
func NewZoneRegistry(ids ...string) *ZoneRegistry {
	r := &ZoneRegistry{data: make(map[string]struct{})}
	if len(ids) == 0 {
		ids = []string{"ru-central1-a", "ru-central1-b", "ru-central1-d"}
	}
	for _, id := range ids {
		r.data[id] = struct{}{}
	}
	return r
}

// GetZone — реализация ports.ZoneRegistry: existence-check зоны по id
// (nil если зона засеяна, ErrNotFound при отсутствии).
func (r *ZoneRegistry) GetZone(_ context.Context, zoneID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[zoneID]; !ok {
		return ports.ErrNotFound
	}
	return nil
}

// RegionOfZone — реализация ports.ZoneRegistry: авторитетный регион зоны.
// Регион из имени зоны НЕ выводится, поэтому фейк задаёт соответствие явно —
// Regions[zone], иначе DefaultRegion ("ru-central1"). Err имитирует недоступность
// geo (fail-closed). Неизвестная зона → ErrNotFound, как у GetZone.
func (r *ZoneRegistry) RegionOfZone(_ context.Context, zoneID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return "", r.Err
	}
	if _, ok := r.data[zoneID]; !ok {
		return "", ports.ErrNotFound
	}
	if region, ok := r.Regions[zoneID]; ok {
		return region, nil
	}
	if r.DefaultRegion != "" {
		return r.DefaultRegion, nil
	}
	return "ru-central1", nil
}

// ---- SubnetRegistry ----

// SubnetPlacement — алиас на placement-проекцию подсети (удобство фикстур).
type SubnetPlacement = ports.SubnetPlacement

// SubnetRegistry — in-memory ports.SubnetRegistry. В проде реализуется
// clients.VPCSubnetClient (vpc.v1.SubnetService.Get) — подсеть принадлежит
// kacho-vpc. Незасеянный id → ErrNotFound (hide-existence); Err имитирует
// недоступность vpc (fail-closed).
type SubnetRegistry struct {
	mu   sync.Mutex
	data map[string]ports.SubnetPlacement
	// Err — принудительная ошибка резолва (недоступность peer'а).
	Err error
	// calls — сколько раз спросили. Кратность повторяемого поля запроса напрямую
	// умножает обращения к соседу, поэтому «сколько раз спросили» — предмет
	// утверждения теста, а не диагностика.
	calls int
}

// Calls — число обращений к реестру подсетей с момента создания.
func (r *SubnetRegistry) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// NewSubnetRegistry создаёт SubnetRegistry. Без аргументов засевает подсети,
// которыми пользуются общие Create-фикстуры (их placement обязан быть ЯВНЫМ —
// зона подсети теперь сверяется с зоной инстанса): sub-abc/sub-a/e9bsub в
// ru-central1-a, sub-b в ru-central1-b.
func NewSubnetRegistry(placements ...ports.SubnetPlacement) *SubnetRegistry {
	r := &SubnetRegistry{data: make(map[string]ports.SubnetPlacement)}
	if len(placements) == 0 {
		placements = []ports.SubnetPlacement{
			{ID: "sub-abc", ProjectID: "prj-acme", PlacementType: ports.SubnetPlacementZonal, ZoneID: "ru-central1-a"},
			{ID: "sub-a", ProjectID: "prj-acme", PlacementType: ports.SubnetPlacementZonal, ZoneID: "ru-central1-a"},
			{ID: "e9bsub", ProjectID: "prj-acme", PlacementType: ports.SubnetPlacementZonal, ZoneID: "ru-central1-a"},
			{ID: "sub-b", ProjectID: "prj-acme", PlacementType: ports.SubnetPlacementZonal, ZoneID: "ru-central1-b"},
		}
	}
	for _, p := range placements {
		r.data[p.ID] = p
	}
	return r
}

// Seed добавляет/переопределяет placement подсети.
func (r *SubnetRegistry) Seed(p ports.SubnetPlacement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[p.ID] = p
}

// GetSubnet — реализация ports.SubnetRegistry.
func (r *SubnetRegistry) GetSubnet(_ context.Context, subnetID string) (*ports.SubnetPlacement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.Err != nil {
		return nil, r.Err
	}
	p, ok := r.data[subnetID]
	if !ok {
		return nil, ports.ErrNotFound
	}
	out := p
	return &out, nil
}

// ---- ProjectClient ----

// ProjectClient — fake ProjectClient. OK задаёт результат Exists().
type ProjectClient struct{ OK bool }

// Exists возвращает ProjectClient.OK.
func (c *ProjectClient) Exists(_ context.Context, _ string) (bool, error) { return c.OK, nil }

// ---- OwnerRegistrar ----

// RegisterCall — одна запись вызова OwnerRegistrar.Register (для проверок в тестах).
type RegisterCall struct {
	Kind       string
	ResourceID string
	ProjectID  string
	Labels     map[string]string
}

// OwnerRegistrar — in-memory fake ports.OwnerRegistrar: записывает каждый
// Register-вызов, позволяя service-тесту проверить, что Create синхронно
// регистрирует owner-tuple (window-оптимизация). Err инъектит ошибку регистрара —
// Create обязан пережить её (best-effort: durable outbox-intent + drainer backstop).
type OwnerRegistrar struct {
	mu    sync.Mutex
	calls []RegisterCall
	// Err — инъецируемая ошибка (nil = успех). best-effort: Create не проваливается.
	Err error
}

// NewOwnerRegistrar создаёт пустой OwnerRegistrar.
func NewOwnerRegistrar() *OwnerRegistrar { return &OwnerRegistrar{} }

// Register записывает вызов и возвращает инъецированную Err.
func (r *OwnerRegistrar) Register(_ context.Context, kind, resourceID, projectID string, labels map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, RegisterCall{Kind: kind, ResourceID: resourceID, ProjectID: projectID, Labels: labels})
	return r.Err
}

// Calls возвращает копию записанных Register-вызовов (thread-safe снимок).
func (r *OwnerRegistrar) Calls() []RegisterCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RegisterCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// ---- NicClient ----

// NicClient — in-memory fake ports.NicClient. Models the kacho-vpc side of the
// NIC↔Instance binding: a single-slot-per-NIC map with atomic (mutex-serialised)
// attach — enough to unit-test the compute saga-worker (auto-index, in-use CAS,
// idempotent replay, mirror-read) without a live kacho-vpc. AttachErrs / Err inject
// the peer error paths (zone-coherence FailedPrecondition, Unavailable fail-closed).
type NicClient struct {
	mu    sync.Mutex
	byNic map[string]ports.NicAttachment // nicID → current binding
	// AttachErrs — per-NIC injected Attach error (zone-coherence, in-use, …).
	AttachErrs map[string]error
	// Err — global injected error for Attach/Detach/ListByInstance (e.g. Unavailable).
	Err error
	// ListErr — injected error for ListByInstance only (mirror graceful-degrade test).
	ListErr error
	// LastListCtx — ctx, полученный последним ListByInstance-вызовом (mirror-read
	// bound test: mirror обязан нести короткий per-call deadline, не 30s retry-storm).
	LastListCtx context.Context
}

// NewNicClient создаёт пустой fake NicClient.
func NewNicClient() *NicClient {
	return &NicClient{byNic: make(map[string]ports.NicAttachment), AttachErrs: make(map[string]error)}
}

// SeedZoneMismatch помечает NIC как zone-incoherent — Attach вернёт
// FailedPrecondition (S4-03), зеркалит kacho-vpc zone-coherence CAS-промах.
func (c *NicClient) SeedZoneMismatch(nicID, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AttachErrs[nicID] = grpcstatus.Error(grpccodes.FailedPrecondition, msg)
}

// Attach атомарно (под mutex) привязывает NIC к инстансу: auto-index (первый
// свободный слот при spec.Index==0), in-use-CAS (чужой инстанс → FailedPrecondition
// "NetworkInterface is in use"), идемпотентный replay (already-ours → OK).
func (c *NicClient) Attach(_ context.Context, spec ports.NicAttachSpec) (*ports.NicAttachment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return nil, c.Err
	}
	if e := c.AttachErrs[spec.NICID]; e != nil {
		return nil, e
	}
	if ex, ok := c.byNic[spec.NICID]; ok {
		if ex.InstanceID != spec.InstanceID {
			return nil, grpcstatus.Error(grpccodes.FailedPrecondition, "NetworkInterface is in use")
		}
		cp := ex
		return &cp, nil // idempotent replay: already ours
	}
	used := make(map[int32]bool)
	for _, a := range c.byNic {
		if a.InstanceID == spec.InstanceID {
			used[a.Index] = true
		}
	}
	idx := spec.Index
	for used[idx] {
		idx++
	}
	att := ports.NicAttachment{
		NICID: spec.NICID, InstanceID: spec.InstanceID, Index: idx,
		SubnetID: "sub-fake", PrimaryV4Address: fmt.Sprintf("10.0.0.%d", idx+2),
		MACAddress: fmt.Sprintf("00:11:22:33:44:%02d", idx),
	}
	c.byNic[spec.NICID] = att
	cp := att
	return &cp, nil
}

// Detach идемпотентно снимает привязку NIC↔instance (already-free → OK).
func (c *NicClient) Detach(_ context.Context, nicID, instanceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return c.Err
	}
	if ex, ok := c.byNic[nicID]; ok && ex.InstanceID == instanceID {
		delete(c.byNic, nicID)
	}
	return nil
}

// ListByInstance — batched read of NIC-привязок по instance-ids.
func (c *NicClient) ListByInstance(ctx context.Context, instanceIDs []string) ([]ports.NicAttachment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastListCtx = ctx
	if c.ListErr != nil {
		return nil, c.ListErr
	}
	if c.Err != nil {
		return nil, c.Err
	}
	want := make(map[string]struct{}, len(instanceIDs))
	for _, id := range instanceIDs {
		want[id] = struct{}{}
	}
	var out []ports.NicAttachment
	for _, a := range c.byNic {
		if _, ok := want[a.InstanceID]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// ---- StorageClient ----

// StorageClient — in-memory fake ports.StorageClient. Models the kacho-storage side
// of the volume↔Instance attachment: a single-slot-per-volume map with atomic
// (mutex-serialised) attach — enough to unit-test the compute saga-worker (in-use CAS,
// idempotent replay, mirror-read) without a live kacho-storage. AttachErrs / Err inject
// the peer error paths (zone/project-coherence FailedPrecondition, Unavailable fail-closed).
type StorageClient struct {
	mu    sync.Mutex
	byVol map[string]ports.VolumeAttachmentInfo // volumeID → current attachment
	// AttachErrs — per-volume injected Attach error (zone/project-coherence, in-use, …).
	AttachErrs map[string]error
	// Err — global injected error for Attach/Detach/ListAttachments (e.g. Unavailable).
	Err error
	// ListErr — injected error for ListAttachments only (mirror graceful-degrade test).
	ListErr error
	// LastListCtx — ctx, полученный последним ListAttachments-вызовом (mirror-read
	// bound test: mirror обязан нести короткий per-call deadline, не 30s retry-storm).
	LastListCtx context.Context
}

// NewStorageClient создаёт пустой fake StorageClient.
func NewStorageClient() *StorageClient {
	return &StorageClient{
		byVol:      make(map[string]ports.VolumeAttachmentInfo),
		AttachErrs: make(map[string]error),
	}
}

// SeedZoneMismatch помечает volume как zone-incoherent — Attach вернёт
// FailedPrecondition, зеркалит kacho-storage zone-coherence CAS-промах.
func (c *StorageClient) SeedZoneMismatch(volumeID, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AttachErrs[volumeID] = grpcstatus.Error(grpccodes.FailedPrecondition, msg)
}

// Attach атомарно (под mutex) привязывает volume к инстансу: in-use-CAS (чужой
// инстанс → FailedPrecondition "Volume is in use"), идемпотентный replay
// (already-ours → OK).
func (c *StorageClient) Attach(_ context.Context, spec ports.VolumeAttachSpec) (*ports.VolumeAttachmentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return nil, c.Err
	}
	if e := c.AttachErrs[spec.VolumeID]; e != nil {
		return nil, e
	}
	if ex, ok := c.byVol[spec.VolumeID]; ok {
		if ex.InstanceID != spec.InstanceID {
			return nil, grpcstatus.Error(grpccodes.FailedPrecondition, "Volume is in use")
		}
		cp := ex
		return &cp, nil // idempotent replay: already ours
	}
	att := ports.VolumeAttachmentInfo{
		VolumeID:     spec.VolumeID,
		InstanceID:   spec.InstanceID,
		InstanceName: spec.InstanceName,
		DeviceName:   spec.DeviceName,
		IsBoot:       spec.IsBoot,
		Mode:         spec.Mode,
		AutoDelete:   spec.AutoDelete,
	}
	c.byVol[spec.VolumeID] = att
	cp := att
	return &cp, nil
}

// Detach идемпотентно снимает привязку volume↔instance (already-free → OK).
func (c *StorageClient) Detach(_ context.Context, volumeID, instanceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return c.Err
	}
	if ex, ok := c.byVol[volumeID]; ok && ex.InstanceID == instanceID {
		delete(c.byVol, volumeID)
	}
	return nil
}

// ListAttachments — batched read volume-привязок по instance-ids.
func (c *StorageClient) ListAttachments(ctx context.Context, instanceIDs []string) ([]ports.VolumeAttachmentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastListCtx = ctx
	if c.ListErr != nil {
		return nil, c.ListErr
	}
	if c.Err != nil {
		return nil, c.Err
	}
	want := make(map[string]struct{}, len(instanceIDs))
	for _, id := range instanceIDs {
		want[id] = struct{}{}
	}
	var out []ports.VolumeAttachmentInfo
	for _, a := range c.byVol {
		if _, ok := want[a.InstanceID]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

var _ ports.StorageClient = (*StorageClient)(nil)

// ---- operations.Repo ----

// OpsRepo — in-memory реализация kacho-corelib/operations.Repo.
type OpsRepo struct {
	mu  sync.Mutex
	ops map[string]*operations.Operation
}

// NewOpsRepo создаёт пустой OpsRepo.
func NewOpsRepo() *OpsRepo { return &OpsRepo{ops: make(map[string]*operations.Operation)} }

// Create сохраняет операцию.
func (r *OpsRepo) Create(_ context.Context, op operations.Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := op
	r.ops[op.ID] = &cp
	return nil
}

// CreateWithPrincipal сохраняет операцию + principal (operations.Repo iface).
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

// List возвращает операции (для ListOperations — фильтрует по ResourceID).
func (r *OpsRepo) List(_ context.Context, f operations.ListFilter) ([]operations.Operation, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []operations.Operation
	for _, op := range r.ops {
		if f.ResourceID != "" && extractResourceID(op) != f.ResourceID {
			continue
		}
		out = append(out, *op)
	}
	return out, "", nil
}

// MarkDone помечает операцию завершённой с response.
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

// MarkError помечает операцию завершённой с ошибкой.
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

// Cancel помечает операцию завершённой.
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

// ---- operations.OwnedOperationRepo (ownership-scoped Get/Cancel) ----
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

// CancelOwned атомарно отменяет операцию owner'а; терминальное состояние в
// возврате (без reload-Get). Идемпотентно на уже-CANCELLED.
func (r *OpsRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || !opsOwnerMatches(op, owner) {
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
	op.Error = &status.Status{Code: 1, Message: "operation cancelled"}
	cp := *op
	return &cp, nil
}

// ListOwned — ownership-scoped листинг: зеркалит List, но AND-ит
// ownership-предикат (чужие строки не возвращаются, симметрично GetOwned).
func (r *OpsRepo) ListOwned(_ context.Context, f operations.ListFilter, owner operations.Owner) ([]operations.Operation, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []operations.Operation
	for _, op := range r.ops {
		if !opsOwnerMatches(op, owner) {
			continue
		}
		if f.ResourceID != "" && extractResourceID(op) != f.ResourceID {
			continue
		}
		out = append(out, *op)
	}
	return out, "", nil
}

var _ operations.OwnedOperationRepo = (*OpsRepo)(nil)

// extractResourceID — денормализованный resource_id строки операции.
//
// Возвращает ЯВНО заданное Operation.ResourceID — ровно ту колонку, по которой
// фильтрует настоящий репозиторий (reflection-fallback по метаданным mock'у не
// нужен: фикстура ставит поле сама). Раньше здесь стоял безусловный "", то есть
// фильтр по ресурсу в mock'е не применялся вовсе — под такой фикстурой тест
// «список этого ресурса» ничего про фильтр не утверждал.
func extractResourceID(op *operations.Operation) string {
	if op == nil {
		return ""
	}
	return op.ResourceID
}

// ---- await-helpers для async Operation worker'ов ----

// TestingT — минимальный интерфейс из *testing.T/*testing.B для await-helper'ов.
type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// AwaitOpDone детерминированно ждёт завершения worker-горутины (Operation.Done).
// Заменяет фиксированный time.Sleep. Падает через 2s.
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

// AwaitAllOpsDone ждёт пока все ops в repo станут Done. Падает через 2s.
func AwaitAllOpsDone(t TestingT, r *OpsRepo) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		allDone := true
		var stuckID string
		for id, op := range r.ops {
			if !op.Done {
				allDone = false
				stuckID = id
				break
			}
		}
		r.mu.Unlock()
		if allDone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not finish within 2s", stuckID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Compile-time проверки соответствия port-интерфейсам.
var (
	_ ports.InstanceRepo  = (*InstanceRepo)(nil)
	_ ports.ZoneRegistry  = (*ZoneRegistry)(nil)
	_ ports.ProjectClient = (*ProjectClient)(nil)
	_ operations.Repo     = (*OpsRepo)(nil)
)
