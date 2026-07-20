// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"google.golang.org/protobuf/proto"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

// insResource / volResource / plgResource — human-labels для malformed-id ошибок
// (`corevalidate.ResourceID`: `invalid <label> id '<X>'`, api-conventions).
const (
	insResource = "instance"
	volResource = "volume"
	plgResource = "placement group"
	saResource  = "service account"
)

// bootSourceStorageImage / bootSourceRegistryImage — owner-дискриминаторы
// bootSource.type (COMP-1 F3/B13): storage.image (OS/disk-образ) vs registry.image
// (OCI-контейнер). bare imageId никогда не двусмыслен между owner'ами.
const (
	bootSourceStorageImage  = "storage.image"
	bootSourceRegistryImage = "registry.image"
)

// nextBootReason — statusReason для next-boot-deferred изменений (COMP-1 F10).
const nextBootReason = "takes effect on next boot"

// CreateInstanceReq — запрос на создание Instance (COMP-1 redesign). Sizing —
// единственный канал MachineTypeID; ОС — единственный вход BootSource; kind гейтит
// один из VMSpec/ContainerSpec. Launch-*Specs (NIC/Volume/ssh) приняты и структурно
// валидированы, но НЕ материализуются (сага — COMP-2).
type CreateInstanceReq struct {
	ProjectID   string
	Name        string
	Description string
	Labels      map[string]string
	ZoneID      string
	Metadata    map[string]string
	Hostname    string

	InstanceKind        domain.InstanceKind
	MachineTypeID       string
	CPUGuaranteePercent int32
	BootSource          domain.BootSource
	ServiceAccountID    string
	PlacementGroupID    string

	VMSpec        *domain.VMSpec
	ContainerSpec *domain.ContainerSpec

	// Launch-*Specs (SKELETON — структурная валидация формы, materialize → COMP-2).
	NetworkInterfaceSpecs  []NetworkInterfaceSpec
	SecondaryVolumeSpecs   []SecondaryVolumeSpec
	SSHPublicKeys          []string
	UseDefaultNetwork      bool
	AssignExternalAddress  bool
	AcknowledgeUnreachable bool
}

// NetworkInterfaceSpec — форма NIC-spec на входе Create (F6 skeleton).
type NetworkInterfaceSpec struct {
	SubnetID         string
	SecurityGroupIDs []string
}

// SecondaryVolumeSpec — форма secondary-Volume-spec на входе Create (F6 skeleton).
type SecondaryVolumeSpec struct {
	SizeGiB      int64
	VolumeTypeID string
	MountPath    string
	AutoDelete   bool
}

// UpdateInstanceReq — запрос на обновление Instance (COMP-1 redesign,
// mutability-классы F10).
type UpdateInstanceReq struct {
	InstanceID          string
	Name                string
	Description         string
	Labels              map[string]string
	ServiceAccountID    string
	MachineTypeID       string
	CPUGuaranteePercent int32
	PlacementGroupID    string
	SSHPublicKeys       []string
	VMSpec              *domain.VMSpec
	UpdateMask          []string
}

// AttachDiskReq — параметры подключения существующего storage-Volume к инстансу.
type AttachDiskReq struct {
	VolumeID   string
	DeviceName string
	Mode       int32 // computev1.AttachedDiskSpec_Mode
	IsBoot     bool
	AutoDelete bool
}

// InstanceService — бизнес-логика управления ВМ + state-машина. Компьют держит
// НОЛЬ local attach-state: том↔Instance-привязка живёт в kacho-storage
// (storageClient → InternalVolumeService), NIC↔Instance — в kacho-vpc (nicClient).
type InstanceService struct {
	repo InstanceRepo
	// machineTypes — sync-каталог sizing (COMP-1 F2/F7). Резолвит machineTypeId
	// (mt-slug ИЛИ стабильное имя) в effectiveResources + family + status.
	machineTypes MachineTypeRepo
	// zones — existence-check zone_id (авторитет — kacho-geo).
	zones         ZoneRegistry
	projectClient ProjectClient
	// nicClient — compute→kacho-vpc InternalNetworkInterfaceService. Может быть nil.
	nicClient NicClient
	// storageClient — compute→kacho-storage InternalVolumeService (volume-attach
	// саги + batched mirror-read). Может быть nil (edge не сконфигурирован):
	// мутации fail-closed Unavailable, read-mirror грациозно опускается.
	storageClient StorageClient
	opsRepo       operations.Repo
	// ownerRegistrar — sync-registrar owner-tuple (best-effort post-commit
	// window-оптимизация; register-drainer — at-least-once backstop). nil = только
	// drainer. Подключается в composition-root через WithOwnerRegistrar.
	ownerRegistrar OwnerRegistrar
}

// NewInstanceService создаёт InstanceService.
func NewInstanceService(repo InstanceRepo, machineTypes MachineTypeRepo, zones ZoneRegistry, projectClient ProjectClient, nicClient NicClient, storageClient StorageClient, opsRepo operations.Repo) *InstanceService {
	return &InstanceService{
		repo: repo, machineTypes: machineTypes, zones: zones, projectClient: projectClient,
		nicClient: nicClient, storageClient: storageClient, opsRepo: opsRepo,
	}
}

// WithOwnerRegistrar подключает sync-registrar owner-tuple: немедленная post-commit
// (best-effort) регистрация owner-tuple, сужающая eventual-consistency-окно до того,
// как register-drainer опросит outbox. nil-safe. Вызывается ОДИН раз из
// composition-root до приёма трафика (single-threaded boot).
func (s *InstanceService) WithOwnerRegistrar(registrar OwnerRegistrar) *InstanceService {
	s.ownerRegistrar = registrar
	return s
}

// Get возвращает Instance по ID. NIC- и volume-зеркала подтягиваются из kacho-vpc /
// kacho-storage (source of truth) с graceful-degrade — недоступность owner'а НЕ
// роняет Get (consumer грациозно переживает недоступность owner'а).
func (s *InstanceService) Get(ctx context.Context, id string) (*domain.Instance, error) {
	// malformed-id первым стейтментом (COMP-1 F8/F22): sync InvalidArgument до repo;
	// well-formed-но-нет → NotFound через repo.Get. Покрывает и Update/Delete-хендлеры
	// (они зовут Get для ownership-guard первым).
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstanceHyphen, id); err != nil {
		return nil, err
	}
	in, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	s.applyNicMirror(ctx, in)
	s.applyVolumeMirror(ctx, in)
	return in, nil
}

// List возвращает список ВМ. project_id обязателен. NIC- и volume-зеркала
// резолвятся ОДНИМ batched-вызовом каждый (не N+1) с graceful-degrade.
func (s *InstanceService) List(ctx context.Context, f InstanceFilter, p Pagination) ([]*domain.Instance, string, error) {
	if f.ProjectID == "" {
		return nil, "", status.Error(codes.InvalidArgument, "project_id required")
	}
	out, next, err := s.repo.List(ctx, f, p)
	if err != nil {
		return nil, "", err
	}
	s.applyNicMirrorBatch(ctx, out)
	s.applyVolumeMirrorBatch(ctx, out)
	return out, next, nil
}

// ValidateCreateInstanceReq — синхронная pre-flight валидация Create-запроса (формат/
// структура/guard'ы; COMP-1 F1/F2/F3/F5/F6). Чистая (без DB/peer/каталог-вызовов) —
// выделена для fuzz. Порядок: kind-oneof → sizing-канал → bootSource-grammar →
// name/desc/labels → cpuGuarantee → ref-форматы → net-runbook → volume-specs →
// unreachable-guard.
func ValidateCreateInstanceReq(req CreateInstanceReq) error {
	if req.ProjectID == "" {
		return status.Error(codes.InvalidArgument, "project_id required")
	}
	if req.ZoneID == "" {
		return status.Error(codes.InvalidArgument, "zone_id required")
	}
	// F1 — instanceKind — сильный первый required-дискриминатор; kind-oneof XOR.
	if !req.InstanceKind.Valid() {
		return invalidArg("instance_kind", "instanceKind is required")
	}
	switch req.InstanceKind {
	case domain.InstanceKindVM:
		if req.ContainerSpec != nil {
			return invalidArg("container_spec", "containerSpec is not allowed when instanceKind is VM")
		}
	case domain.InstanceKindContainer:
		if req.VMSpec != nil {
			return invalidArg("vm_spec", "vmSpec is not allowed when instanceKind is CONTAINER")
		}
	}
	// F2 — machineTypeId — единственный канал sizing (обязателен; резолв — doCreate).
	if req.MachineTypeID == "" {
		return invalidArg("machine_type_id", "machineTypeId is required")
	}
	// F3 — bootSource grammar + type-whitelist + output-field-reject.
	if err := validateBootSource(req.BootSource); err != nil {
		return err
	}
	if err := corevalidate.NameCompute("name", req.Name); err != nil {
		return err
	}
	if err := corevalidate.Description("description", req.Description); err != nil {
		return err
	}
	if err := corevalidate.Labels("labels", req.Labels); err != nil {
		return err
	}
	if !domain.ValidCPUGuaranteePercent(req.CPUGuaranteePercent) {
		return invalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
	}
	// F4 — serviceAccountId own-side format-check (existence peer-validate → COMP-2).
	if req.ServiceAccountID != "" {
		// "sva" — iam service-account prefix; ResourceID family-agnostic (accepts canon).
		if err := corevalidate.ResourceID(saResource, "sva", req.ServiceAccountID); err != nil {
			return err
		}
	}
	// OQ4 — placementGroupId format-only passthrough (existence/coherence → COMP-3).
	if req.PlacementGroupID != "" {
		if err := corevalidate.ResourceID(plgResource, "plg", req.PlacementGroupID); err != nil {
			return err
		}
	}
	// F6 — launch net-spec: networkInterfaceSpecs ИЛИ useDefaultNetwork (одно обязательно).
	if len(req.NetworkInterfaceSpecs) == 0 && !req.UseDefaultNetwork {
		return status.Errorf(codes.FailedPrecondition,
			"needs an existing subnet+SG in zone %s; discover via SubnetService.List / SecurityGroupService.List, create via SubnetService.Create — or set useDefaultNetwork:true",
			req.ZoneID)
	}
	for i := range req.NetworkInterfaceSpecs {
		if req.NetworkInterfaceSpecs[i].SubnetID == "" {
			return invalidArg("network_interface_specs.subnet_id", "networkInterfaceSpecs[].subnetId is required")
		}
	}
	// F6 — secondaryVolumeSpecs structural: sizeGiB>0 (human-scale GiB, не байты).
	for i := range req.SecondaryVolumeSpecs {
		if req.SecondaryVolumeSpecs[i].SizeGiB <= 0 {
			return invalidArg("secondary_volume_specs.size_gib", "secondaryVolumeSpecs[].sizeGiB must be > 0")
		}
	}
	// F5 — unreachable-guard: VM без ssh И без external → FAILED_PRECONDITION (снимается
	// acknowledgeUnreachable). CONTAINER exempt (нужен NIC для egress, не ssh/external).
	if req.InstanceKind == domain.InstanceKindVM &&
		len(req.SSHPublicKeys) == 0 && !req.AssignExternalAddress && !req.AcknowledgeUnreachable {
		return status.Error(codes.FailedPrecondition,
			"VM will be RUNNING but unreachable (no sshPublicKeys and no external address); set acknowledgeUnreachable:true to proceed")
	}
	return nil
}

// Create инициирует создание Instance (COMP-1: durable-персист без materialize).
func (s *InstanceService) Create(ctx context.Context, req CreateInstanceReq) (*operations.Operation, error) {
	if err := ValidateCreateInstanceReq(req); err != nil {
		return nil, err
	}

	instanceID := ids.NewHyphenID(ids.PrefixInstanceHyphen)
	// Operation.done = durability ресурса (row закоммичен в doCreate). Owner-tuple
	// материализуется eventually-consistent — sync-registrar (window-оптимизация) +
	// register-drainer/reconciler backstop, НЕ гейтит op.done.
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Create instance %s", req.Name),
		&computev1.CreateInstanceMetadata{InstanceId: instanceID},
		func(ctx context.Context) (*anypb.Any, error) {
			return s.doCreate(ctx, instanceID, req)
		})
}

func (s *InstanceService) doCreate(ctx context.Context, instanceID string, req CreateInstanceReq) (*anypb.Any, error) {
	if err := checkProject(ctx, s.projectClient, req.ProjectID); err != nil {
		return nil, err
	}
	if err := s.zones.GetZone(ctx, req.ZoneID); err != nil {
		return nil, mapZoneRefErr(err, req.ZoneID)
	}
	// F2/F7 — резолв machineTypeId (mt-slug ИЛИ стабильное имя) в каталоге → canonical
	// mt-slug + effectiveResources; RETIRED/unknown → FailedPrecondition.
	mt, err := s.resolveMachineType(ctx, req.MachineTypeID)
	if err != nil {
		return nil, err
	}

	bs := req.BootSource
	bs.ImageKind = imageKindFor(bs.Type) // server-derived B13 discriminator (F3)
	in := &domain.Instance{
		ID:          instanceID,
		ProjectID:   req.ProjectID,
		CreatedAt:   time.Now().UTC(),
		Name:        req.Name,
		Description: req.Description,
		Labels:      req.Labels,
		ZoneID:      req.ZoneID,
		// OQ1 — resting-status PROVISIONING persisted (durable; launch-сага NIC/Volume +
		// переход к RUNNING — COMP-2). Operation.done = durability, не materialize (ban 9).
		Status:              domain.InstanceStatusProvisioning,
		Metadata:            req.Metadata,
		Hostname:            req.Hostname,
		FQDN:                fqdn(instanceID, req.Hostname),
		CPUGuaranteePercent: req.CPUGuaranteePercent,
		InstanceKind:        req.InstanceKind,
		MachineTypeID:       mt.ID, // canonical mt- slug (F2/F6 echo)
		EffectiveResources:  mt.EffectiveResources,
		BootSource:          bs,
		PlacementGroupID:    req.PlacementGroupID,
		ServiceAccountID:    req.ServiceAccountID,
		VMSpec:              req.VMSpec,
		ContainerSpec:       req.ContainerSpec,
	}
	// Self-validating domain invariant на persistence-границе (last-line guard;
	// формат уже проверен sync ValidateCreateInstanceReq).
	if err := in.Validate(); err != nil {
		return nil, invalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
	}
	created, err := s.repo.Insert(ctx, in)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	// Sync-register owner-tuple post-commit (best-effort window-оптимизация); durable
	// outbox-intent (writer-tx repo.Insert) + drainer — at-least-once backstop.
	syncRegisterOwner(ctx, s.ownerRegistrar, "Instance", created.ID, created.ProjectID, created.Labels)
	return anypb.New(protoconv.Instance(created))
}

// resolveMachineType резолвит machineTypeId (mt-slug ИЛИ стабильное имя) в каталоге
// (COMP-1 F2/F7). unknown → FailedPrecondition "machine type <ref> not found";
// RETIRED → FailedPrecondition (не запускается на Create; DEPRECATED — можно).
func (s *InstanceService) resolveMachineType(ctx context.Context, ref string) (*domain.MachineType, error) {
	var mt *domain.MachineType
	if strings.HasPrefix(ref, ids.PrefixMachineTypeHyphen+"-") {
		got, err := s.machineTypes.Get(ctx, ref)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, status.Errorf(codes.FailedPrecondition, "machine type %s not found", ref)
			}
			return nil, mapRepoErr(err)
		}
		mt = got
	} else {
		list, _, err := s.machineTypes.List(ctx, MachineTypeFilter{Name: ref}, Pagination{PageSize: 2})
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if len(list) == 0 {
			return nil, status.Errorf(codes.FailedPrecondition, "machine type %s not found", ref)
		}
		mt = list[0]
	}
	if mt.Status == domain.MachineTypeStatusRetired {
		return nil, status.Errorf(codes.FailedPrecondition, "machine type %s is retired and cannot be used on Create", ref)
	}
	return mt, nil
}

// instanceUpdateKnown — known-set маски Update (snake_case proto/spec-пути; COMP-1
// F10). instance_kind/zone_id immutable, bootSource Reinstall-only — НЕ в наборе
// (спец-reject срабатывает первым). ssh_public_keys/vm_spec — next-boot deferred;
// machine_type_id/cpu_guarantee_percent/placement_group_id — STOPPED-gated.
var instanceUpdateKnown = map[string]struct{}{
	"name": {}, "description": {}, "labels": {}, "service_account_id": {},
	"machine_type_id": {}, "cpu_guarantee_percent": {}, "placement_group_id": {},
	"ssh_public_keys": {}, "vm_spec": {},
}

// instanceStoppedGatedMask — маска-поля, требующие STOPPED (sizing/placement, F10).
var instanceStoppedGatedMask = map[string]struct{}{
	"machine_type_id": {}, "cpu_guarantee_percent": {}, "placement_group_id": {},
}

// Update обновляет Instance (COMP-1 mutability-классы, F10). immutable-reject и
// Reinstall-only-reject срабатывают ДО UpdateMask; STOPPED-gate (sizing/placement) —
// sync FAILED_PRECONDITION (в COMP-1 STOPPED недостижим ⇒ always-reject).
func (s *InstanceService) Update(ctx context.Context, req UpdateInstanceReq) (*operations.Operation, error) {
	if req.InstanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstanceHyphen, req.InstanceID); err != nil {
		return nil, err
	}
	if err := validateInstanceUpdate(req); err != nil {
		return nil, err
	}
	// STOPPED-gate (F10): sizing/placement маска на не-STOPPED инстансе → sync
	// FAILED_PRECONDITION. Sync repo.Get для статуса ДО Operation (в COMP-1 инстанс
	// никогда не STOPPED ⇒ always-reject; COMP-2 добавит достижимость через Stop).
	if maskIntersects(req.UpdateMask, instanceStoppedGatedMask) {
		in, err := s.repo.Get(ctx, req.InstanceID)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if in.Status != domain.InstanceStatusStopped {
			return nil, status.Error(codes.FailedPrecondition, "instance must be STOPPED to change sizing or placement")
		}
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Update instance %s", req.InstanceID),
		&computev1.UpdateInstanceMetadata{InstanceId: req.InstanceID},
		func(ctx context.Context) (*anypb.Any, error) {
			in, err := s.repo.Get(ctx, req.InstanceID)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			updates := req.UpdateMask
			if len(updates) == 0 {
				// пустая маска = full-object PATCH mutable-полей (LIVE-mutable only —
				// STOPPED-gated/next-boot применяются лишь при явной маске).
				updates = []string{"name", "description", "labels", "service_account_id"}
			}
			labelsInMask := false
			nextBoot := false
			changed := make([]string, 0, len(updates)+1)
			for _, f := range updates {
				switch f {
				case "name":
					in.Name = req.Name
					changed = append(changed, "name")
				case "description":
					in.Description = req.Description
					changed = append(changed, "description")
				case "labels":
					in.Labels = req.Labels
					labelsInMask = true
					changed = append(changed, "labels")
				case "service_account_id":
					in.ServiceAccountID = req.ServiceAccountID
					changed = append(changed, "service_account_id")
				case "machine_type_id":
					in.MachineTypeID = req.MachineTypeID
					changed = append(changed, "machine_type_id")
				case "cpu_guarantee_percent":
					in.CPUGuaranteePercent = req.CPUGuaranteePercent
					changed = append(changed, "cpu_guarantee_percent")
				case "placement_group_id":
					in.PlacementGroupID = req.PlacementGroupID
					changed = append(changed, "placement_group_id")
				case "vm_spec":
					in.VMSpec = req.VMSpec
					nextBoot = true
					changed = append(changed, "vm_spec")
				case "ssh_public_keys":
					// next-boot deferred: ключи не персистятся на durable-row в COMP-1
					// (launch-spec skeleton) — фиксируется только deferral-marker.
					nextBoot = true
				}
			}
			if nextBoot {
				in.StatusReason = nextBootReason
				changed = append(changed, "status_reason")
			}
			updated, err := s.repo.Update(ctx, in, labelsInMask, changed)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(protoconv.Instance(updated))
		})
}

func validateInstanceUpdate(req UpdateInstanceReq) error {
	// immutable-switch + Reinstall-only ДО corevalidate.UpdateMask (known-set не несёт
	// этих полей → UpdateMask отверг бы их generic "unknown field" вместо конвенционных
	// текстов). camelCase в сообщении — часть контракта (api-conventions).
	for _, f := range req.UpdateMask {
		switch f {
		case "instance_kind":
			return invalidArg(f, "instanceKind is immutable after Instance.Create")
		case "zone_id":
			return invalidArg(f, "zoneId is immutable after Instance.Create")
		case "boot_source":
			return invalidArg(f, "bootSource cannot be changed via Update; use Reinstall")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", req.UpdateMask, instanceUpdateKnown); err != nil {
		return err
	}
	for _, f := range req.UpdateMask {
		switch f {
		case "name":
			if err := corevalidate.NameCompute("name", req.Name); err != nil {
				return err
			}
		case "description":
			if err := corevalidate.Description("description", req.Description); err != nil {
				return err
			}
		case "labels":
			if err := corevalidate.Labels("labels", req.Labels); err != nil {
				return err
			}
		case "cpu_guarantee_percent":
			if !domain.ValidCPUGuaranteePercent(req.CPUGuaranteePercent) {
				return invalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
			}
		case "placement_group_id":
			if req.PlacementGroupID != "" {
				if err := corevalidate.ResourceID(plgResource, "plg", req.PlacementGroupID); err != nil {
					return err
				}
			}
		case "service_account_id":
			if req.ServiceAccountID != "" {
				if err := corevalidate.ResourceID(saResource, "sva", req.ServiceAccountID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// maskIntersects сообщает, пересекается ли маска с набором полей.
func maskIntersects(mask []string, set map[string]struct{}) bool {
	for _, f := range mask {
		if _, ok := set[f]; ok {
			return true
		}
	}
	return false
}

// UpdateMetadata обновляет map metadata (delete + upsert).
func (s *InstanceService) UpdateMetadata(ctx context.Context, instanceID string, del []string, upsert map[string]string) (*operations.Operation, error) {
	if instanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Update instance %s metadata", instanceID),
		&computev1.UpdateInstanceMetadataMetadata{InstanceId: instanceID},
		func(ctx context.Context) (*anypb.Any, error) {
			updated, err := s.repo.MergeMetadata(ctx, instanceID, del, upsert)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(protoconv.Instance(updated))
		})
}

// Start/Stop/Restart — state-машина (DB-уровневый atomic CAS).
func (s *InstanceService) Start(ctx context.Context, id string) (*operations.Operation, error) {
	return s.lifecycle(ctx, id, "Start", domain.InstanceStatusStopped, domain.InstanceStatusRunning,
		"Instance is not stopped", &computev1.StartInstanceMetadata{InstanceId: id})
}

// Stop переводит ВМ RUNNING→STOPPED.
func (s *InstanceService) Stop(ctx context.Context, id string) (*operations.Operation, error) {
	return s.lifecycle(ctx, id, "Stop", domain.InstanceStatusRunning, domain.InstanceStatusStopped,
		"Instance is not running", &computev1.StopInstanceMetadata{InstanceId: id})
}

// Restart перезапускает RUNNING ВМ (single atomic CAS RUNNING→RUNNING).
func (s *InstanceService) Restart(ctx context.Context, id string) (*operations.Operation, error) {
	return s.lifecycle(ctx, id, "Restart", domain.InstanceStatusRunning, domain.InstanceStatusRunning,
		"Instance is not running", &computev1.RestartInstanceMetadata{InstanceId: id})
}

func (s *InstanceService) lifecycle(ctx context.Context, id, action string, from, to domain.InstanceStatus, precondMsg string, meta protoreflectMessage) (*operations.Operation, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("%s instance %s", action, id), meta,
		func(ctx context.Context) (*anypb.Any, error) {
			updated, err := s.repo.SetStatusCAS(ctx, id, from, to)
			if err != nil {
				return nil, mapLifecycleErr(err, precondMsg)
			}
			return anypb.New(protoconv.Instance(updated))
		})
}

// mapLifecycleErr маппит ошибку SetStatusCAS: CAS-промах → FailedPrecondition
// с precondMsg; остальное — стандартный mapRepoErr.
func mapLifecycleErr(err error, precondMsg string) error {
	if errors.Is(err, ErrFailedPrecondition) {
		return status.Error(codes.FailedPrecondition, precondMsg)
	}
	return mapRepoErr(err)
}

// AttachDisk подключает storage-Volume к ВМ (async сага → kacho-storage).
//
// Sync-фаза: malformed instance/volume-id первым стейтментом (sec.3.1). Async-worker:
// compute-local CAS-гейт (GateForAttach: state ∈ {RUNNING, STOPPED} + self-describing
// zone/project/name) → storage.Attach (fail-closed Unavailable, идемпотентный replay,
// zone/project-coherence + attach-CAS на стороне storage). Компьют attach-строку
// локально НЕ пишет (storage — владелец привязки; ацикличность).
func (s *InstanceService) AttachDisk(ctx context.Context, instanceID string, req AttachDiskReq) (*operations.Operation, error) {
	if instanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	// Malformed-id первым стейтментом (sync InvalidArgument, до Operation).
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstance, instanceID); err != nil {
		return nil, err
	}
	if req.VolumeID == "" {
		return nil, invalidArg("volume_id", "volume_id is required")
	}
	if err := corevalidate.ResourceID(volResource, ids.PrefixVolume, req.VolumeID); err != nil {
		return nil, err
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Attach disk to instance %s", instanceID),
		&computev1.AttachInstanceDiskMetadata{InstanceId: instanceID, VolumeId: req.VolumeID},
		func(ctx context.Context) (*anypb.Any, error) {
			zoneID, projectID, name, err := s.repo.GateForAttach(ctx, instanceID)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			if s.storageClient == nil {
				return nil, status.Error(codes.Unavailable, "volume service unavailable")
			}
			if _, err := s.storageClient.Attach(ctx, VolumeAttachSpec{
				VolumeID:       req.VolumeID,
				InstanceID:     instanceID,
				InstanceName:   name,
				InstanceZoneID: zoneID,
				ProjectID:      projectID,
				DeviceName:     req.DeviceName,
				IsBoot:         req.IsBoot,
				Mode:           VolumeAttachMode(req.Mode),
				AutoDelete:     req.AutoDelete,
			}); err != nil {
				return nil, err // storage-client уже нормализовал (leak-guard + contract codes)
			}
			return s.reloadWithMirror(ctx, instanceID)
		})
}

// DetachDisk отвязывает том (по volume_id ЛИБО device_name; boot нельзя).
// Идемпотентно: том не привязан → done no-op. Привязку резолвит storage
// (compute local attach-state нет) — источник истины для volume_id/is_boot.
func (s *InstanceService) DetachDisk(ctx context.Context, instanceID, volumeID, deviceName string) (*operations.Operation, error) {
	if instanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	// oneof exactly_one: ровно одно из volume_id / device_name.
	if (volumeID == "") == (deviceName == "") {
		return nil, invalidArg("disk", "exactly one of volume_id or device_name is required")
	}
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstance, instanceID); err != nil {
		return nil, err
	}
	if err := corevalidate.ResourceID(volResource, ids.PrefixVolume, volumeID); err != nil {
		return nil, err
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Detach disk from instance %s", instanceID),
		&computev1.DetachInstanceDiskMetadata{InstanceId: instanceID, VolumeId: volumeID},
		func(ctx context.Context) (*anypb.Any, error) {
			if s.storageClient == nil {
				return nil, status.Error(codes.Unavailable, "volume service unavailable")
			}
			atts, err := s.storageClient.ListAttachments(ctx, []string{instanceID})
			if err != nil {
				return nil, err // fail-closed (Unavailable) — не роняем detach в INTERNAL
			}
			var target *VolumeAttachmentInfo
			for i := range atts {
				a := &atts[i]
				if (volumeID != "" && a.VolumeID == volumeID) || (deviceName != "" && a.DeviceName == deviceName) {
					target = a
					break
				}
			}
			if target == nil {
				// Уже отвязан — идемпотентный no-op.
				return s.reloadWithMirror(ctx, instanceID)
			}
			if target.IsBoot {
				return nil, status.Error(codes.FailedPrecondition, "boot volume cannot be detached")
			}
			if err := s.storageClient.Detach(ctx, target.VolumeID, instanceID); err != nil {
				return nil, err
			}
			return s.reloadWithMirror(ctx, instanceID)
		})
}

// SimulateMaintenanceEvent — no-op (control-plane: done-операция с самой ВМ).
func (s *InstanceService) SimulateMaintenanceEvent(ctx context.Context, id string) (*operations.Operation, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Simulate maintenance event for instance %s", id),
		&computev1.SimulateInstanceMaintenanceEventMetadata{InstanceId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			in, err := s.repo.Get(ctx, id)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(protoconv.Instance(in))
		})
}

// Delete инициирует удаление ВМ (delete-сага, M2).
//
// Порядок (crash-safe, идемпотентный): (1) гейт instance→DELETING (конкурентный
// AttachDisk-гейт видит DELETING и падает — attach-vs-delete race); (2) release всех
// NIC-привязок через kacho-vpc (fail-closed Unavailable — не оставляем dangling);
// (3) release всех volume-привязок через kacho-storage (fail-closed); (4) строка
// инстанса удаляется ПОСЛЕДНЕЙ. Списки привязок пересчитываются из storage/vpc на
// каждом прогоне (self-describing) → replay идемпотентен: уже снятая привязка
// возвращается пустым списком, повторный Detach — no-op. Crash после любого шага
// оставляет консистентное состояние (строка инстанса ещё жива → привязки резолвятся).
//
// NB: удаление auto_delete-томов (storage Volume.Delete) вынесено в отдельный
// storage-side инкремент (acceptance sec.0.3) — здесь привязки лишь СНИМАЮТСЯ
// (detach), что закрывает найденный go-review NIC/volume-leak.
//
// Degraded-path (Noop-клиенты, KACHO_COMPUTE_SKIP_PEER_VALIDATION / несконфигурированное
// vpc/storage-ребро): NoopNicClient.ListByInstance и NoopStorageClient.ListAttachments
// возвращают пустой список, поэтому шаги (2)/(3) НЕ находят привязок и ничего не
// снимают — при неподнятых peer'ах NIC/volume-привязки НЕ освобождаются (их и не
// отследить без живого owner'а). Освобождение гарантировано только при сконфигурированных
// реальных клиентах.
func (s *InstanceService) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Delete instance %s", id),
		&computev1.DeleteInstanceMetadata{InstanceId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			// (1) gate → DELETING. Уже удалён (crash-replay) → идемпотентный success.
			if _, err := s.repo.MarkDeleting(ctx, id); err != nil {
				if errors.Is(err, ErrNotFound) {
					return anypb.New(&emptypb.Empty{})
				}
				return nil, mapRepoErr(err)
			}
			// (2) release NICs (fail-closed).
			if s.nicClient != nil {
				nics, err := s.nicClient.ListByInstance(ctx, []string{id})
				if err != nil {
					return nil, mapNicErr(err)
				}
				for i := range nics {
					if err := s.nicClient.Detach(ctx, nics[i].NICID, id); err != nil {
						return nil, mapNicErr(err)
					}
				}
			}
			// (3) release volumes (fail-closed).
			if s.storageClient != nil {
				vols, err := s.storageClient.ListAttachments(ctx, []string{id})
				if err != nil {
					return nil, err
				}
				for i := range vols {
					if err := s.storageClient.Detach(ctx, vols[i].VolumeID, id); err != nil {
						return nil, err
					}
				}
			}
			// (4) delete instance row LAST.
			if err := s.repo.Delete(ctx, id); err != nil {
				if errors.Is(err, ErrNotFound) {
					return anypb.New(&emptypb.Empty{})
				}
				return nil, mapRepoErr(err)
			}
			return anypb.New(&emptypb.Empty{})
		})
}

// GetSerialPortOutput — sync RPC: синтетический текст (control-plane).
func (s *InstanceService) GetSerialPortOutput(ctx context.Context, id string) (string, error) {
	in, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", mapRepoErr(err)
	}
	return fmt.Sprintf("[control-plane] serial port output for instance %s (status=%s) is not available (control-plane only).\n", in.ID, instanceStatusName(in.Status)), nil
}

// ListOperations возвращает операции для конкретной ВМ.
func (s *InstanceService) ListOperations(ctx context.Context, id string, p Pagination) ([]operations.Operation, string, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, "", mapRepoErr(err)
	}
	return s.opsRepo.List(ctx, operations.ListFilter{ResourceID: id, PageSize: p.PageSize, PageToken: p.PageToken})
}

// mirrorReadTimeout — верхняя граница best-effort mirror-READ (Get/List NIC/volume
// зеркала) на КАЖДЫЙ peer-вызов. Зеркало output-only (data-integrity: source of
// truth = kacho-vpc/kacho-storage), graceful-degrade на ЛЮБОЙ ошибке — поэтому НЕ
// должно крутить полный retry.OnUnavailable (MaxElapsed=30s): против Unavailable
// peer'а это вешало Get/List на ~30s/зеркало (×2 nic+volume = ~55s/RPC — доминирующий
// bottleneck instance-суита; disk/image/snapshot без зеркал —
// быстрые). Короткий bound → быстрый degrade: peer up — read в ms (bound не
// срабатывает); peer down/blip — зеркало опускается, следующий read перечитает.
// Мутации (attach/detach/release-сага, worker fn) сохраняют полный 30s retry —
// fail-closed для них корректен (down-peer ⇒ Unavailable/leak-safety), их НЕ трогаем.
const mirrorReadTimeout = 3 * time.Second

// ---- mirrors (read-only проекции attach-состояния из storage/vpc) ----

// reloadWithMirror перечитывает инстанс и накладывает NIC/volume-зеркала — общий
// хвост attach/detach-саг, возвращающий свежий Instance-снимок.
func (s *InstanceService) reloadWithMirror(ctx context.Context, id string) (*anypb.Any, error) {
	in, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	s.applyNicMirror(ctx, in)
	s.applyVolumeMirror(ctx, in)
	return anypb.New(protoconv.Instance(in))
}

// applyVolumeMirror заполняет in.AttachedDisks read-only зеркалом из kacho-storage
// (source of truth). Graceful-degrade: nil-client или ошибка storage → зеркало
// опускается (Get/List не падают).
func (s *InstanceService) applyVolumeMirror(ctx context.Context, in *domain.Instance) {
	if s.storageClient == nil || in == nil {
		return
	}
	// best-effort read → короткий bound, не 30s retry.OnUnavailable (mirrorReadTimeout).
	ctx, cancel := context.WithTimeout(ctx, mirrorReadTimeout)
	defer cancel()
	atts, err := s.storageClient.ListAttachments(ctx, []string{in.ID})
	if err != nil {
		return
	}
	in.AttachedDisks = volumeMirror(atts)
}

// applyVolumeMirrorBatch — batched (не N+1) зеркало для List: один ListAttachments
// по всем id, затем раскладка по инстансам. Graceful-degrade как applyVolumeMirror.
func (s *InstanceService) applyVolumeMirrorBatch(ctx context.Context, list []*domain.Instance) {
	if s.storageClient == nil || len(list) == 0 {
		return
	}
	instIDs := make([]string, 0, len(list))
	for _, in := range list {
		instIDs = append(instIDs, in.ID)
	}
	// best-effort read → короткий bound, не 30s retry.OnUnavailable (mirrorReadTimeout).
	ctx, cancel := context.WithTimeout(ctx, mirrorReadTimeout)
	defer cancel()
	atts, err := s.storageClient.ListAttachments(ctx, instIDs)
	if err != nil {
		return
	}
	byInstance := make(map[string][]VolumeAttachmentInfo, len(list))
	for _, a := range atts {
		byInstance[a.InstanceID] = append(byInstance[a.InstanceID], a)
	}
	for _, in := range list {
		in.AttachedDisks = volumeMirror(byInstance[in.ID])
	}
}

// volumeMirror конвертирует storage volume-attachments в domain.AttachedDisk
// (read-only зеркало), boot первым, затем по device_name — детерминированный порядок.
func volumeMirror(atts []VolumeAttachmentInfo) []domain.AttachedDisk {
	if len(atts) == 0 {
		return nil
	}
	sorted := make([]VolumeAttachmentInfo, len(atts))
	copy(sorted, atts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsBoot != sorted[j].IsBoot {
			return sorted[i].IsBoot // boot первым
		}
		return sorted[i].DeviceName < sorted[j].DeviceName
	})
	out := make([]domain.AttachedDisk, 0, len(sorted))
	for i := range sorted {
		a := &sorted[i]
		out = append(out, domain.AttachedDisk{
			DiskID:     a.VolumeID,
			IsBoot:     a.IsBoot,
			Mode:       domain.AttachedDiskMode(a.Mode),
			DeviceName: a.DeviceName,
			AutoDelete: a.AutoDelete,
		})
	}
	return out
}

// ---- helpers ----

// protoreflectMessage — alias для proto.Message (operations.New принимает его).
type protoreflectMessage = proto.Message

// validateBootSource — grammar + type-whitelist + output-field-reject (COMP-1 F3).
// На входе допустимы только Type/ID; tag/digest живут ВНУТРИ ID; bare-untagged → 400
// с грамматикой в тексте; name/resolvedDigest/materializedVolume/imageKind —
// output-only (на вход не принимаются, COMP-1-11).
func validateBootSource(bs domain.BootSource) error {
	if bs.Type == "" && bs.ID == "" {
		return invalidArg("boot_source", "bootSource is required")
	}
	if bs.Type != bootSourceStorageImage && bs.Type != bootSourceRegistryImage {
		return invalidArg("boot_source.type",
			"bootSource.type must be one of storage.image, registry.image")
	}
	if bs.Name != "" || bs.ResolvedDigest != "" || bs.MaterializedVolume != nil || bs.ImageKind != domain.ImageKindUnspecified {
		return invalidArg("boot_source",
			"bootSource name/resolvedDigest/materializedVolume are output-only and must not be set on input")
	}
	if bs.ID == "" {
		return invalidArg("boot_source.id", "bootSource.id is required")
	}
	if !hasTagOrDigest(bs.ID) {
		return invalidArg("boot_source.id",
			"bootSource.id needs a tag or digest, e.g. 'img-<base32>:<tag>' or 'img-<base32>@sha256:<hex>'; use ImageCatalog item.bootSource")
	}
	return nil
}

// hasTagOrDigest — id несёт tag (":" в последнем path-сегменте) ИЛИ digest
// ("@sha256:"). bare "img-<b32>" / "repo/name" без tag/digest → false (→ 400).
func hasTagOrDigest(id string) bool {
	if strings.Contains(id, "@sha256:") {
		return true
	}
	seg := id
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		seg = id[i+1:]
	}
	return strings.Contains(seg, ":")
}

// imageKindFor — server-derived B13 imageKind по bootSource.type (COMP-1 F3).
func imageKindFor(bsType string) domain.ImageKind {
	switch bsType {
	case bootSourceStorageImage:
		return domain.ImageKindStorageImage
	case bootSourceRegistryImage:
		return domain.ImageKindOCIImage
	default:
		return domain.ImageKindUnspecified
	}
}

func fqdn(id, hostname string) string {
	if hostname != "" {
		return hostname + ".kacho.internal"
	}
	return id + ".auto.internal"
}

func instanceStatusName(s domain.InstanceStatus) string {
	if v, ok := computev1.Instance_Status_name[int32(s)]; ok { // #nosec G115 -- s — domain.InstanceStatus (малый enum, зеркалит proto); индекс в Instance_Status_name
		return v
	}
	return "STATUS_UNSPECIFIED"
}
