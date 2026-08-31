// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

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

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/ownersync"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/peercheck"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
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
	GuestAccessKeyIDs      []string
	SecondaryVolumeSpecs   []SecondaryVolumeSpec
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
	VMSpec              *domain.VMSpec
	GuestAccessKeyIDs   []string
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
	// quota — полоса учёта числа ресурсов (порт QuotaGuard). Материализация
	// строк учёта висит на ней, поэтому на поднятом стенде провязка обязательна.
	quota QuotaGuard
	repo  InstanceRepo
	// machineTypes — sync-каталог sizing (COMP-1 F2/F7). Резолвит machineTypeId
	// (mt-slug ИЛИ стабильное имя) в effectiveResources + family + status.
	machineTypes MachineTypeRepo
	// zones — existence-check zone_id + авторитетный zone→region (авторитет — kacho-geo).
	zones ZoneRegistry
	// subnets — peer-валидация подсети NIC-спеки (авторитет — kacho-vpc):
	// placement-coherence зоны инстанса и его интерфейсов на request-path.
	// nil → coherence неверифицируема → Create fail-closed Unavailable.
	subnets       SubnetRegistry
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
func NewInstanceService(repo InstanceRepo, machineTypes MachineTypeRepo, zones ZoneRegistry, subnets SubnetRegistry, projectClient ProjectClient, nicClient NicClient, storageClient StorageClient, opsRepo operations.Repo) *InstanceService {
	return &InstanceService{
		repo: repo, machineTypes: machineTypes, zones: zones, subnets: subnets, projectClient: projectClient,
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
	// (они зовут Get первым — ради existence/NotFound-контракта).
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstanceHyphen, id); err != nil {
		return nil, err
	}
	in, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
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

// validateGuestAccessKeyIDs проверяет форму ссылок на ключи входа.
//
// Принадлежность проекту здесь НЕ проверяется: это вопрос к данным, и он решён
// внутри самой вставки связи. Проверка перед вставкой защищала бы ровно тот
// путь, который через неё проходит.
func validateGuestAccessKeyIDs(ids []string) error {
	if len(ids) > domain.MaxGuestAccessKeysPerInstance {
		return serviceerr.InvalidArg("guest_access_key_ids",
			fmt.Sprintf("guestAccessKeyIds must contain at most %d entries (got %d)",
				domain.MaxGuestAccessKeysPerInstance, len(ids)))
	}
	for _, id := range ids {
		if err := corevalidate.ResourceID("GuestAccessKey", "gak", id); err != nil {
			return err
		}
	}
	return nil
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
		return serviceerr.InvalidArg("instance_kind", "instanceKind is required")
	}
	switch req.InstanceKind {
	case domain.InstanceKindVM:
		if req.ContainerSpec != nil {
			return serviceerr.InvalidArg("container_spec", "containerSpec is not allowed when instanceKind is VM")
		}
	case domain.InstanceKindContainer:
		// ОТВЕРГАЕТСЯ ПО ВИДУ, а не по источнику ОС — и это ровно то, что
		// объявляет контракт значения (`InstanceKind.CONTAINER`: несоздаваем,
		// отказ синхронный и по имени поля).
		//
		// Прежде отказ висел ТОЛЬКО на `boot_source.type = registry.image`, а
		// связки «вид ↔ источник ОС» в проверке входа не было вовсе. Пара
		// «вид CONTAINER + образ хранилища» проходила целиком и создавала
		// машину: вид «контейнер» с корневой файловой системой из образа диска —
		// ресурс, не описываемый ни одной ветвью модели. Контракт при этом
		// называл поле, которого отказ не называл.
		//
		// Причина невозможности — на стороне ВЛАДЕЛЬЦА образа, и она проверяема:
		// у `registry.Repository` нет неизменяемого идентификатора, он адресуется
		// парой (реестр, имя), а имя меняется отдельным глаголом. Ссылка,
		// зафиксированная в машине, стала бы ложной после чужого переименования.
		//
		// Из трёх законных исходов это второй. Первый (реализовать) закрыт
		// фактом о владельце; третий (снять с контракта) отбросил бы решённое
		// направление, у которого назван и владелец, и предикат возврата, —
		// ветвь открывается вместе с неизменяемым идентификатором репозитория.
		//
		// Проверка XOR `vmSpec ↔ вид` в этой ветви НЕ стоит: до неё нет
		// достижимого пути, а ветвь, которой код никогда не производит, —
		// документация несуществующего поведения.
		return serviceerr.InvalidArg("instance_kind",
			"instanceKind CONTAINER is not creatable yet: a registry image has no durable address today")
	}
	// F2 — machineTypeId — единственный канал sizing (обязателен; резолв — doCreate).
	if req.MachineTypeID == "" {
		return serviceerr.InvalidArg("machine_type_id", "machineTypeId is required")
	}
	// F3 — bootSource grammar + type-whitelist + output-field-reject.
	if err := validateBootSource(req.BootSource); err != nil {
		return err
	}
	// Форма имени; пустое на создании законно и означает «назови сам» — умолчание
	// подставит NameOrDefault в Create, когда id уже сгенерирован.
	if err := corevalidate.NameOnCreate("name", req.Name); err != nil {
		return err
	}
	if err := corevalidate.Description("description", req.Description); err != nil {
		return err
	}
	if err := corevalidate.Labels("labels", req.Labels); err != nil {
		return err
	}
	// Свободная карта данных машины несёт СВОЙ бюджет: правка сливается в уже
	// накопленное, поэтому потолок размера сообщения хранимое не ограничивает —
	// карта росла бы от вызова к вызову, и каждая правка навсегда клала бы весь
	// выросший блоб ещё и в две неподчищаемые служебные таблицы.
	if !domain.ValidCPUGuaranteePercent(req.CPUGuaranteePercent) {
		return serviceerr.InvalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
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
	// Ключи входа: формат СВОЕГО идентификатора проверяется здесь, первым
	// стейтментом. Без проверки непригодная строка уехала бы в связь и вернулась
	// отказом «ключ не этого проекта» — утверждением о принадлежности строки,
	// которая ключом быть не может.
	if err := validateGuestAccessKeyIDs(req.GuestAccessKeyIDs); err != nil {
		return err
	}
	// F6 — launch net-spec: networkInterfaceSpecs ИЛИ useDefaultNetwork (одно обязательно).
	if len(req.NetworkInterfaceSpecs) == 0 && !req.UseDefaultNetwork {
		return status.Errorf(codes.FailedPrecondition,
			"needs an existing subnet+SG in zone %s; discover via SubnetService.List / SecurityGroupService.List, create via SubnetService.Create — or set useDefaultNetwork:true",
			req.ZoneID)
	}
	// Кратность списка интерфейсов ограничена ЗДЕСЬ, до фазы обращений к соседу:
	// каждый элемент стоит одного вызова к владельцу подсети, поэтому длина списка
	// напрямую умножает внешние round-trip'ы и время удержания слота ОБЩЕГО пула
	// исполнителей. Проверка сверх предела не должна стоить ни одного внешнего
	// вызова — иначе предел защищает от роста строки, но не от нагрузки
	// (см. domain.MaxNetworkInterfaceSpecsPerInstance).
	if len(req.NetworkInterfaceSpecs) > domain.MaxNetworkInterfaceSpecsPerInstance {
		return serviceerr.InvalidArg("network_interface_specs",
			fmt.Sprintf("networkInterfaceSpecs must contain at most %d entries (got %d)",
				domain.MaxNetworkInterfaceSpecsPerInstance, len(req.NetworkInterfaceSpecs)))
	}
	for i := range req.NetworkInterfaceSpecs {
		if req.NetworkInterfaceSpecs[i].SubnetID == "" {
			return serviceerr.InvalidArg("network_interface_specs.subnet_id", "networkInterfaceSpecs[].subnetId is required")
		}
	}
	// F6 — secondaryVolumeSpecs: кратность (предел объявлен контрактом) + structural
	// sizeGiB>0 (human-scale GiB, не байты).
	if len(req.SecondaryVolumeSpecs) > domain.MaxSecondaryVolumeSpecsPerInstance {
		return serviceerr.InvalidArg("secondary_volume_specs",
			fmt.Sprintf("secondaryVolumeSpecs must contain at most %d entries (got %d)",
				domain.MaxSecondaryVolumeSpecsPerInstance, len(req.SecondaryVolumeSpecs)))
	}
	for i := range req.SecondaryVolumeSpecs {
		if req.SecondaryVolumeSpecs[i].SizeGiB <= 0 {
			return serviceerr.InvalidArg("secondary_volume_specs.size_gib", "secondaryVolumeSpecs[].sizeGiB must be > 0")
		}
	}
	// F5 — unreachable-guard: VM без external-адреса → FAILED_PRECONDITION (снимается
	// acknowledgeUnreachable). CONTAINER exempt (нужен NIC для egress, не external).
	//
	// Прежде страж снимался ещё и непустым sshPublicKeys — то есть списком ключей,
	// который compute никуда не доставляет. Условие пропускало ровно тот случай, от
	// которого защищает: машина оставалась недостижимой, а вызывающий получал
	// подтверждение обратного. Поле снято с приёма
	// (handler.RejectUnsupportedCreateFields), вместе с ним снят и этот терм.
	if req.InstanceKind == domain.InstanceKindVM &&
		!req.AssignExternalAddress && !req.AcknowledgeUnreachable {
		return status.Error(codes.FailedPrecondition,
			"VM will be RUNNING but unreachable (no external address); set acknowledgeUnreachable:true to proceed")
	}
	return nil
}

// Create инициирует создание Instance (COMP-1: durable-персист без materialize).
func (s *InstanceService) Create(ctx context.Context, req CreateInstanceReq) (*operations.Operation, error) {
	if err := ValidateCreateInstanceReq(req); err != nil {
		return nil, err
	}

	instanceID := ids.NewHyphenID(ids.PrefixInstanceHyphen)
	// Пустое имя не доживает до записи: ресурса без имени не бывает.
	req.Name = corevalidate.NameOrDefault(req.Name, instanceID)
	// Operation.done = durability ресурса (row закоммичен в doCreate). Owner-tuple
	// материализуется eventually-consistent — sync-registrar (window-оптимизация) +
	// register-drainer/reconciler backstop, НЕ гейтит op.done.
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Create instance %s", req.Name),
		&computev1.CreateInstanceMetadata{InstanceId: instanceID},
		func(ctx context.Context) (*anypb.Any, error) {
			return s.doCreate(ctx, instanceID, req)
		})
}

func (s *InstanceService) doCreate(ctx context.Context, instanceID string, req CreateInstanceReq) (*anypb.Any, error) {
	if err := peercheck.Project(ctx, s.projectClient, req.ProjectID); err != nil {
		return nil, err
	}

	// Учёт числа ресурсов. Здесь же материализуются строки учёта, если проект их
	// ещё не имеет, — момент, когда владелец типа впервые узнаёт о проекте.
	//
	// ПОЧЕМУ ВНУТРИ ОПЕРАЦИИ, А НЕ ДО НЕЁ, как у остальных двух видов. Проверка
	// проекта у машины живёт в асинхронной половине, а резолв величин обязан
	// идти ПОСЛЕ неё: на несуществующем проекте отказ обязан называть проект, а
	// не пределы. Значит отказ учёта приезжает в `Operation.error` —
	// авторитетной полосой, которую приёмка и называет несущей; синхронная
	// совещательная полоса у машины отсутствует осознанно, а не забыта.
	if s.quota != nil {
		if err := s.quota.Admit(ctx, req.ProjectID, "compute.instance"); err != nil {
			return nil, err
		}
	}
	if err := s.zones.GetZone(ctx, req.ZoneID); err != nil {
		return nil, serviceerr.MapZoneRefErr(err, req.ZoneID)
	}
	// Регион зоны резолвится У ВЛАДЕЛЬЦА и только когда он нужен — то есть когда
	// машина названа группой размещения. Спрашивать его всегда значило бы платить
	// лишним обращением к соседу на КАЖДОМ создании ради ветки, которой чаще
	// всего нет.
	//
	// Выводить регион из имени зоны запрещено: имена произвольны, выводимой связи
	// между ними нет, а строковая деривация молча отдаёт пустоту и превращает
	// проверку когерентности в тождественно-истинную.
	var regionID string
	if req.PlacementGroupID != "" {
		var rerr error
		if regionID, rerr = s.zones.RegionOfZone(ctx, req.ZoneID); rerr != nil {
			return nil, serviceerr.MapZoneRefErr(rerr, req.ZoneID)
		}
	}
	// Машина создаётся в своей зоне — её интерфейсы обязаны быть в той же зоне.
	// Проверяется ЗДЕСЬ, на пути создания (до Insert), а не откладывается до
	// launch-саги: иначе инстанс с интерфейсом в чужой зоне становится durable.
	if err := s.checkNicSpecPlacement(ctx, req); err != nil {
		return nil, err
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
		Hostname:            req.Hostname,
		FQDN:                fqdn(instanceID, req.Hostname),
		CPUGuaranteePercent: req.CPUGuaranteePercent,
		InstanceKind:        req.InstanceKind,
		MachineTypeID:       mt.ID, // canonical mt- slug (F2/F6 echo)
		EffectiveResources:  mt.EffectiveResources,
		BootSource:          bs,
		PlacementGroupID:    req.PlacementGroupID,
		RegionID:            regionID,
		ServiceAccountID:    req.ServiceAccountID,
		VMSpec:              req.VMSpec,
		ContainerSpec:       req.ContainerSpec,
		GuestAccessKeyIDs:   req.GuestAccessKeyIDs,
	}
	// Self-validating domain invariant на persistence-границе (last-line guard;
	// формат уже проверен sync ValidateCreateInstanceReq).
	if err := in.Validate(); err != nil {
		return nil, serviceerr.InvalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
	}
	created, regs, err := s.repo.Insert(ctx, in)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Sync-register owner-tuple post-commit (best-effort window-оптимизация); durable
	// outbox-intent (writer-tx repo.Insert) + drainer — at-least-once backstop.
	ownersync.Register(ctx, s.ownerRegistrar, regs)
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
			if errors.Is(err, ports.ErrNotFound) {
				return nil, status.Errorf(codes.FailedPrecondition, "machine type %s not found", ref)
			}
			return nil, serviceerr.MapRepoErr(err)
		}
		mt = got
	} else {
		list, _, err := s.machineTypes.List(ctx, MachineTypeFilter{Name: ref}, Pagination{PageSize: 2})
		if err != nil {
			return nil, serviceerr.MapRepoErr(err)
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
// F10). instance_kind/zone_id/boot_source immutable — НЕ в наборе
// (спец-reject срабатывает первым). vm_spec — next-boot deferred;
// machine_type_id/cpu_guarantee_percent/placement_group_id — STOPPED-gated.
// ssh_public_keys в наборе НЕТ: поле не принимается вовсе
// (handler.RejectUnsupportedUpdateFields) — раньше оно числилось «отложенным до
// следующей загрузки», хотя загружать было нечего.
var instanceUpdateKnown = map[string]struct{}{
	"name": {}, "description": {}, "labels": {}, "service_account_id": {},
	"machine_type_id": {}, "cpu_guarantee_percent": {}, "placement_group_id": {},
	"vm_spec": {}, "guest_access_key_ids": {},
}

// instanceStoppedGatedMask — маска-поля, требующие STOPPED (sizing/placement, F10).
var instanceStoppedGatedMask = map[string]struct{}{
	"machine_type_id": {}, "cpu_guarantee_percent": {}, "placement_group_id": {},
}

// instanceFullPatchFields — поля, применяемые при ПУСТОЙ маске (full-object PATCH,
// api-conventions): только LIVE-mutable — STOPPED-gated и next-boot поля меняются
// лишь по явной маске. Один список на применение и на валидацию: разъехавшись, они
// дают ровно тот дефект, ради которого список и заведён — поле применяется, но не
// проверяется.
//
// Набора ключей входа здесь НЕТ намеренно: пустая маска означала бы «снять все
// ключи», то есть правка описания молча лишала бы машину доступа. Замена набора
// требует, чтобы её назвали.
var instanceFullPatchFields = []string{"name", "description", "labels", "service_account_id"}

// instanceUpdatedFields — поля, которые ЭТОТ запрос изменит: замаскированные, а при
// пустой маске — LIVE-mutable набор (full-object PATCH).
func instanceUpdatedFields(mask []string) []string {
	if len(mask) == 0 {
		return instanceFullPatchFields
	}
	return mask
}

// Update обновляет Instance (COMP-1 mutability-классы, F10). immutable-reject (в том
// числе boot_source) срабатывает ДО UpdateMask; STOPPED-gate (sizing/placement) —
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
			return nil, serviceerr.MapRepoErr(err)
		}
		if in.Status != domain.InstanceStatusStopped {
			return nil, status.Error(codes.FailedPrecondition, "instance must be STOPPED to change sizing or placement")
		}
	}
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Update instance %s", req.InstanceID),
		&computev1.UpdateInstanceMetadata{InstanceId: req.InstanceID},
		func(ctx context.Context) (*anypb.Any, error) {
			in, err := s.repo.Get(ctx, req.InstanceID)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			// пустая маска = full-object PATCH mutable-полей (LIVE-mutable only —
			// STOPPED-gated/next-boot применяются лишь при явной маске).
			updates := instanceUpdatedFields(req.UpdateMask)
			labelsInMask := false
			nextBoot := false
			changed := make([]string, 0, len(updates)+1)
			for _, f := range updates {
				switch f {
				case "name":
					// Пишем ровно тогда, когда общая функция говорит «пиши»: имя,
					// которого запрос не присылал, не пишется — иначе полный PATCH
					// описания снимал бы имя молча. Отказ здесь невозможен: тот же
					// вызов уже прошёл в validateInstanceUpdate до начала записи.
					applyName, _ := corevalidate.NameOnUpdate("name", req.UpdateMask, req.Name)
					if !applyName {
						continue
					}
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
					// Регион зоны машины — тот же авторитетный резолв, что на
					// создании, и по той же причине: без него региональная группа
					// сверялась бы с пустой строкой, то есть не сверялась бы.
					if req.PlacementGroupID != "" {
						reg, rerr := s.zones.RegionOfZone(ctx, in.ZoneID)
						if rerr != nil {
							return nil, serviceerr.MapZoneRefErr(rerr, in.ZoneID)
						}
						in.RegionID = reg
					}
					changed = append(changed, "placement_group_id")
				case "vm_spec":
					in.VMSpec = req.VMSpec
					nextBoot = true
					changed = append(changed, "vm_spec")
				case "guest_access_key_ids":
					in.GuestAccessKeyIDs = req.GuestAccessKeyIDs
					changed = append(changed, "guest_access_key_ids")
				}
			}
			if nextBoot {
				in.StatusReason = nextBootReason
				changed = append(changed, "status_reason")
			}
			updated, regs, err := s.repo.Update(ctx, in, labelsInMask, changed)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			// Смена меток меняет ПРОЕКЦИЮ, которую читает селектор владельца прав,
			// поэтому доставляется на пути запроса — ровно как регистрация на Create.
			// Durable-intent (writer-tx repo.Update) остаётся at-least-once backstop'ом,
			// но ждать только его значит отдать ОТЗЫВ по снятию метки глубине очереди
			// (замер соседнего сервиса, 2026-08-05: 188–365 с при клиентском бюджете
			// чтения-своих-записей 15 с). Update без меток в маске проекции не меняет —
			// звать владельца прав незачем.
			// Ветка «метки в маске» остаётся ЯВНОЙ, хотя набор доставки и так
			// пуст, когда меток в маске нет: перепись гейта
			// `TestLabelMirrorIsDeliveredNotOnlyQueued` знает площадки каждого
			// сервиса по этому признаку, и без неё compute выпал бы из переписи
			// МОЛЧА — а вердикт про «доставку зеркала меток» перестал бы быть
			// про то дерево, что есть.
			if labelsInMask {
				ownersync.Register(ctx, s.ownerRegistrar, regs)
			}
			return anypb.New(protoconv.Instance(updated))
		})
}

func validateInstanceUpdate(req UpdateInstanceReq) error {
	// immutable-switch ДО corevalidate.UpdateMask (known-set не несёт
	// этих полей → UpdateMask отверг бы их generic "unknown field" вместо конвенционных
	// текстов). camelCase в сообщении — часть контракта (api-conventions).
	for _, f := range req.UpdateMask {
		switch f {
		case "instance_kind":
			return serviceerr.InvalidArg(f, "instanceKind is immutable after Instance.Create")
		case "zone_id":
			return serviceerr.InvalidArg(f, "zoneId is immutable after Instance.Create")
		case "boot_source":
			// Текст ОТКАЗА не имеет права называть операцию, которой нет. Прежний
			// отправлял вызывающего к Reinstall — такого RPC нет ни в контракте, ни в
			// маршрутах, ни в коде: слово встречалось только здесь и в трёх пинах на
			// этот же текст. Поле неизменяемо, и сменить его нечем, поэтому тон —
			// общий контрактный (api-conventions «<field> is immutable after
			// <Resource>.Create»), как у двух соседних веток.
			return serviceerr.InvalidArg(f, "bootSource is immutable after Instance.Create")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", req.UpdateMask, instanceUpdateKnown); err != nil {
		return err
	}
	// Проверяются именно ПРИМЕНЯЕМЫЕ поля, а не содержимое маски: при пустой маске
	// маска пуста, а применяется весь LIVE-mutable набор, поэтому цикл по маске не
	// проверял бы ничего из того, что запрос записывает. immutable-check и known-set
	// выше остаются на самой маске — пустая маска не называет ни immutable-поля, ни
	// неизвестного.
	for _, f := range instanceUpdatedFields(req.UpdateMask) {
		switch f {
		case "name":
			// Решение об имени принимает ЕДИНСТВЕННАЯ функция дерева: пять исходов
			// маски и значения. Пустое имя при ПУСТОЙ маске означает «не прислали»
			// (в proto3 пропущенное и пустое поле неразличимы), при названной маске —
			// «сними имя», а снять его нельзя.
			if _, err := corevalidate.NameOnUpdate("name", req.UpdateMask, req.Name); err != nil {
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
				return serviceerr.InvalidArg("cpu_guarantee_percent", "cpuGuaranteePercent must be between 0 and 100")
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
		case "guest_access_key_ids":
			if err := validateGuestAccessKeyIDs(req.GuestAccessKeyIDs); err != nil {
				return err
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
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("%s instance %s", action, id), meta,
		func(ctx context.Context) (*anypb.Any, error) {
			updated, err := s.repo.SetStatusCAS(ctx, id, from, to)
			if err != nil {
				return nil, mapLifecycleErr(err, precondMsg)
			}
			return anypb.New(protoconv.Instance(updated))
		})
}

// mapLifecycleErr маппит ошибку SetStatusCAS: CAS-промах → FailedPrecondition
// с precondMsg; остальное — стандартный serviceerr.MapRepoErr.
func mapLifecycleErr(err error, precondMsg string) error {
	if errors.Is(err, ports.ErrFailedPrecondition) {
		return status.Error(codes.FailedPrecondition, precondMsg)
	}
	return serviceerr.MapRepoErr(err)
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
		return nil, serviceerr.InvalidArg("volume_id", "volume_id is required")
	}
	if err := corevalidate.ResourceID(volResource, ids.PrefixVolume, req.VolumeID); err != nil {
		return nil, err
	}
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Attach disk to instance %s", instanceID),
		&computev1.AttachInstanceDiskMetadata{InstanceId: instanceID, VolumeId: req.VolumeID},
		func(ctx context.Context) (*anypb.Any, error) {
			zoneID, projectID, name, err := s.repo.GateForAttach(ctx, instanceID)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
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
		return nil, serviceerr.InvalidArg("disk", "exactly one of volume_id or device_name is required")
	}
	if err := corevalidate.ResourceID(insResource, ids.PrefixInstance, instanceID); err != nil {
		return nil, err
	}
	if err := corevalidate.ResourceID(volResource, ids.PrefixVolume, volumeID); err != nil {
		return nil, err
	}
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Detach disk from instance %s", instanceID),
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
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Simulate maintenance event for instance %s", id),
		&computev1.SimulateInstanceMaintenanceEventMetadata{InstanceId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			in, err := s.repo.Get(ctx, id)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
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
	return lro.RunOp(ctx, s.opsRepo, fmt.Sprintf("Delete instance %s", id),
		&computev1.DeleteInstanceMetadata{InstanceId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			// (1) gate → DELETING. Уже удалён (crash-replay) → идемпотентный success.
			if _, err := s.repo.MarkDeleting(ctx, id); err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return anypb.New(&emptypb.Empty{})
				}
				return nil, serviceerr.MapRepoErr(err)
			}
			// (2)-(4) — общая часть с добивателем (releaseAndDelete): одна
			// реализация на два вызывающих. Две копии шагов саги разъехались бы
			// ровно там, где расхождение не наблюдаемо — на пути после краха.
			if err := s.releaseAndDelete(ctx, id); err != nil {
				return nil, err
			}
			return anypb.New(&emptypb.Empty{})
		})
}

// releaseAndDelete — шаги (2)-(4) delete-саги: снять привязки интерфейсов, снять
// привязки томов, удалить строку ПОСЛЕДНЕЙ.
//
// Списки привязок пересчитываются у владельцев на каждом прогоне (self-describing),
// поэтому повтор идемпотентен: уже снятая привязка возвращается пустым списком.
// Именно это делает шаги пригодными и для штатного пути, и для добивателя — их
// не нужно писать дважды.
//
// Порядок «строка последней» — не стиль: списки привязок резолвятся ПО машине, и
// без её строки не останется ничего, по чему их можно найти. Отказ владельца
// поэтому обязан оставить строку на месте, а не проглотиться.
func (s *InstanceService) releaseAndDelete(ctx context.Context, id string) error {
	if s.nicClient != nil {
		nics, err := s.nicClient.ListByInstance(ctx, []string{id})
		if err != nil {
			return mapNicErr(err)
		}
		for i := range nics {
			if err := s.nicClient.Detach(ctx, nics[i].NICID, id); err != nil {
				return mapNicErr(err)
			}
		}
	}
	if s.storageClient != nil {
		vols, err := s.storageClient.ListAttachments(ctx, []string{id})
		if err != nil {
			return err
		}
		for i := range vols {
			if err := s.storageClient.Detach(ctx, vols[i].VolumeID, id); err != nil {
				return err
			}
		}
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		return serviceerr.MapRepoErr(err)
	}
	return nil
}

// FinishStuckDeletes доводит до конца удаления, которые начал умерший процесс, и
// возвращает id доделанных машин.
//
// # Почему это обязано существовать
//
// Порядок delete-саги crash-safe: строка живёт до последнего шага, привязки
// резолвятся у владельцев на каждом прогоне, повтор идемпотентен. Повторять,
// однако, было НЕКОМУ. Разрешитель осиротевших операций по своему контракту
// рабочую функцию не перезапускает — он приводит статус операции в соответствие
// закоммиченной реальности: видит строку на месте и помечает операцию
// прерванной. Машина остаётся в DELETING навсегда, а её интерфейсы и тома —
// занятыми у владельцев, которые о случившемся не узнают: снятие привязки
// инициирует потребитель, владелец его не запрашивает.
//
// Наблюдаемое следствие — занятый том не присоединить к другой машине, занятый
// интерфейс удерживает адрес из ограниченного пула. Ошибок при этом нет ни у
// кого: удаление «прошло», просто не до конца.
//
// # Отсрочка
//
// grace отсекает удаления, которые прямо сейчас доделывает законный исполнитель:
// иначе он и добиватель снимали бы одни и те же привязки наперегонки.
//
// # Многорепличность
//
// Проход берётся ОДНОЙ репликой — TryClaimStuckDeleteSweep, замок прохода в базе
// сервиса. Второе возвращаемое значение говорит, состоялся ли проход: проигрыш
// замка есть штатный исход, а не отказ.
//
// Здесь стояло обратное решение: «отдельной блокировки не нужно, каждый шаг
// идемпотентен». Про КОРРЕКТНОСТЬ это верно и остаётся верным — повторное снятие
// привязки у владельца есть no-op, отсутствующая строка есть успех, — поэтому
// дубль ничего не портил. Неверен был вывод о ЦЕНЕ: выборка детерминирована, две
// реплики берут одни и те же пятьдесят машин и зовут по каждой двух соседей.
// Предел партии, обоснованный бережностью к соседям, умножался на число реплик —
// а компонент масштабируется по загрузке, то есть умножение включается само и
// именно под нагрузкой.
//
// # Отказ соседа
//
// Отказ владельца привязки ВОЗВРАЩАЕТСЯ вызывающему и прерывает проход: строка
// обязана уцелеть до успешного снятия. Следующий проход начнёт заново —
// самоисцеление при возвращении соседа.
func (s *InstanceService) FinishStuckDeletes(ctx context.Context, grace time.Duration) ([]string, bool, error) {
	release, ok, err := s.repo.TryClaimStuckDeleteSweep(ctx)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		// Проход исполняет другая реплика. Не ошибка и не работа — пропуск тика.
		return nil, false, nil
	}
	defer release(ctx)

	stuck, err := s.repo.ListStuckDeleting(ctx, grace)
	if err != nil {
		return nil, true, err
	}
	finished := make([]string, 0, len(stuck))
	for _, id := range stuck {
		if err := s.releaseAndDelete(ctx, id); err != nil {
			// Уже доделанное возвращаем вместе с ошибкой: вызывающий обязан видеть
			// и то, что сделано, и то, на чём проход встал.
			return finished, true, err
		}
		finished = append(finished, id)
	}
	return finished, true, nil
}

// GetSerialPortOutput — sync RPC: синтетический текст (control-plane).
func (s *InstanceService) GetSerialPortOutput(ctx context.Context, id string) (string, error) {
	in, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", serviceerr.MapRepoErr(err)
	}
	return fmt.Sprintf("[control-plane] serial port output for instance %s (status=%s) is not available (control-plane only).\n", in.ID, instanceStatusName(in.Status)), nil
}

// ListOperations возвращает операции вызывающего над конкретной ВМ.
//
// Право на список — не право на чтение: строка операции несёт ресурс целиком в
// Response и личность инициатора, поэтому выдача сужена до операций самого
// вызывающего (operations.ListForCaller — предикат владения внутри SQL WHERE).
// Кросс-принципальная история ресурса на этой поверхности не выдаётся.
func (s *InstanceService) ListOperations(ctx context.Context, id string, p Pagination) ([]operations.Operation, string, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	return operations.ListForCaller(ctx, s.opsRepo,
		operations.ListFilter{ResourceID: id, PageSize: p.PageSize, PageToken: p.PageToken})
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
		return nil, serviceerr.MapRepoErr(err)
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
			VolumeID:   a.VolumeID,
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
		return serviceerr.InvalidArg("boot_source", "bootSource is required")
	}
	if bs.Type != bootSourceStorageImage && bs.Type != bootSourceRegistryImage {
		return serviceerr.InvalidArg("boot_source.type",
			"bootSource.type must be one of storage.image, registry.image")
	}
	if bs.Name != "" || bs.ResolvedDigest != "" || bs.MaterializedVolume != nil || bs.ImageKind != domain.ImageKindUnspecified {
		// Текст перечисляет ВСЕ ЧЕТЫРЕ поля, которые условие выше отвергает.
		// Прежняя редакция называла три и молчала о четвёртом — `imageKind`,
		// том самом, который вызывающий вероятнее всего и заполнил: он объявлен
		// в контракте без пометки output-only и со словарём значений, то есть
		// выглядит входом. Отказ, не назвавший присланное поле, не
		// восстанавливает следующий шаг: вызывающий снимает три названных,
		// которых не слал, и получает тот же отказ снова.
		return serviceerr.InvalidArg("boot_source",
			"bootSource name/resolvedDigest/materializedVolume/imageKind are output-only and must not be set on input")
	}
	if bs.ID == "" {
		return serviceerr.InvalidArg("boot_source.id", "bootSource.id is required")
	}

	// Ветви разведены ПО ВЛАДЕЛЬЦУ, и это не косметика: у двух источников разные
	// формы идентификатора, потому что разные владельцы адресуют по-разному.
	//
	// Прежняя редакция требовала тег или дайджест от КАЖДОГО идентификатора и
	// отсылала за примером к каталогу, которого в дереве нет ни одного вхождения.
	// Для образа хранилища это требование неисполнимо by construction: у его
	// контракта нет ни поля тега, ни поля дайджеста — то есть форма, которую
	// проверка требовала, не производится владельцем вовсе.
	switch bs.Type {
	case bootSourceStorageImage:
		// Образ хранилища адресуется своим неизменяемым идентификатором, и
		// только им: он неизменяем на всю жизнь ресурса, поэтому повторный
		// запуск через месяц берёт тот же образ без дополнительной фиксации.
		if err := corevalidate.ResourceID("Image", "img", bs.ID); err != nil {
			return err
		}
		return nil

	case bootSourceRegistryImage:
		// ОТВЕРГАЕТСЯ ЯВНО, а не принимается молча.
		//
		// Durable-координата образа из реестра сегодня невыразима by construction:
		// у репозитория нет неизменяемого идентификатора, он адресуется парой
		// (реестр, имя), а имя переименовывается отдельным глаголом. Ссылка,
		// зафиксированная в машине, стала бы ложной после чужого переименования —
		// то есть запуск через месяц взял бы другой образ либо не нашёл ничего.
		//
		// Из трёх законных исходов это второй: принять и не разрешить значило бы
		// пообещать возможность, которой нет; снять с контракта — потерять
		// объявленное направление, у которого есть решение и предикат возврата.
		// Ветвь открывается, когда у репозитория появится неизменяемый
		// идентификатор.
		return serviceerr.InvalidArg("boot_source.type",
			"bootSource.type registry.image is not accepted yet: a registry image has no durable address today")
	}
	return nil
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

// nicPlacementBudget — потолок ВСЕЙ фазы проверки размещения интерфейсов.
//
// Арифметика следует из контракта, а не выбрана «на глаз»: предел кратности
// (domain.MaxNetworkInterfaceSpecsPerInstance = 8) × per-call дедлайн ребра
// compute→vpc (3s) = 24s worst-case стены на здоровом, но медленном соседе.
// Потолок обязан быть ЗАПАСОМ над ней, чтобы в здоровом режиме первым срабатывал
// per-call дедлайн, а бюджет оставался ограничителем на деградацию. При изменении
// предела кратности или per-call дедлайна это число пересчитывается.
const nicPlacementBudget = 30 * time.Second

// checkNicSpecPlacement — placement-coherence зоны инстанса и подсетей его
// сетевых интерфейсов (data-integrity.md, placement-coherence). Подсеть — чужой
// ресурс (владелец kacho-vpc), поэтому это peer-validate на request-path, а не
// локальная догадка:
//
//   - ZONAL-подсеть   → её zone_id обязан совпадать с zone_id инстанса;
//   - REGIONAL (anycast) подсеть → зоны у неё НЕТ by construction, зональная
//     проверка исключена; остаётся региональная: region_id подсети обязан
//     совпадать с регионом ЗОНЫ инстанса. Регион зоны берётся у владельца
//     Geography (geo), из имени зоны он не выводится;
//   - подсеть не резолвится (нет / нет доступа) → FAILED_PRECONDITION с тем же
//     hide-existence текстом, что и настоящий miss (анти-BOLA);
//   - vpc/geo недоступны → UNAVAILABLE (fail-closed на мутации).
func (s *InstanceService) checkNicSpecPlacement(ctx context.Context, req CreateInstanceReq) error {
	if len(req.NetworkInterfaceSpecs) == 0 {
		return nil
	}
	if s.subnets == nil {
		return status.Error(codes.Unavailable, "subnet lookup unavailable")
	}
	// Бюджет ВСЕЙ фазы, а не отдельного вызова. Per-call дедлайн ограничивает ОДИН
	// хоп; их последовательность он не ограничивает, а под деградировавшим соседом
	// каждый элемент ещё и оплачивается повторами. С пределом кратности
	// (domain.MaxNetworkInterfaceSpecsPerInstance) и склейкой повторов худший случай
	// по-прежнему складывается из нескольких обращений подряд, и без потолка он
	// доходит до предела исполнения операции — то есть слот ОБЩЕГО пула держится
	// полные четыре минуты за работу, которая уже не удастся.
	//
	// Потолок обязан НЕ срабатывать в здоровом режиме: 8 разных подсетей × 3s
	// per-call = 24s стены, поэтому 30s оставляет запас и первым срабатывает
	// per-call дедлайн, а не бюджет.
	ctx, cancel := context.WithTimeout(ctx, nicPlacementBudget)
	defer cancel()
	// Регион зоны инстанса резолвится лениво — он нужен только anycast-подсети.
	instRegion := ""
	// Одна и та же подсеть спрашивается у соседа ОДИН раз: ответ на «где лежит эта
	// подсеть» от повторения вопроса не меняется, а каждый вопрос — это round-trip,
	// который сосед ещё и авторизует у себя. Кратность запроса ограничена в
	// ValidateCreateInstanceReq, склейка убирает оставшийся множитель: стоимость
	// определяется числом РАЗНЫХ подсетей, а не длиной списка.
	asked := make(map[string]struct{}, len(req.NetworkInterfaceSpecs))
	for i := range req.NetworkInterfaceSpecs {
		subnetID := req.NetworkInterfaceSpecs[i].SubnetID
		if _, seen := asked[subnetID]; seen {
			continue
		}
		asked[subnetID] = struct{}{}
		sn, err := s.subnets.GetSubnet(ctx, subnetID)
		if err != nil {
			// Задокументированное dev-послабление KACHO_COMPUTE_SKIP_PEER_VALIDATION
			// (тот же класс, что NoopGeoClient «любая зона существует»); на
			// развёрнутом стенде флаг выключен и маркер не возникает.
			if errors.Is(err, ports.ErrPeerValidationSkipped) {
				return nil
			}
			return mapSubnetRefErr(err, subnetID)
		}
		if sn.PlacementType == ports.SubnetPlacementRegional {
			if instRegion == "" {
				r, rerr := s.zones.RegionOfZone(ctx, req.ZoneID)
				if rerr != nil {
					return status.Error(codes.Unavailable, "zone region lookup unavailable")
				}
				instRegion = r
			}
			if sn.RegionID != instRegion {
				return status.Error(codes.FailedPrecondition,
					"NetworkInterface subnet must be in the same region as the instance")
			}
			continue
		}
		if sn.ZoneID != req.ZoneID {
			return status.Errorf(codes.FailedPrecondition,
				"NetworkInterface subnet is in zone %s, instance zone is %s", sn.ZoneID, req.ZoneID)
		}
	}
	return nil
}

// mapSubnetRefErr — peer-ошибка резолва подсети NIC-спеки → gRPC-status.
// Не найдено / нет доступа → FAILED_PRECONDITION тем же текстом, что настоящий
// miss (hide-existence, анти-BOLA); недоступность vpc → UNAVAILABLE (fail-closed
// на мутации), непрозрачным текстом — детали peer'а наружу не идут.
func mapSubnetRefErr(err error, subnetID string) error {
	if errors.Is(err, ports.ErrNotFound) {
		return status.Errorf(codes.FailedPrecondition, "Subnet %s not found", subnetID)
	}
	return status.Error(codes.Unavailable, "subnet lookup unavailable")
}

// QuotaGuard — полоса учёта числа ресурсов.
//
// Порт объявлен здесь, у вызывающего; реализация — `apps/kacho/shared/quota`.
//
// Полоса НЕ ПРИНИМАЕТ решения: между её ответом и вставкой помещается чужая
// запись, и место занимает атомарное списание триггера в writer-транзакции
// — чтение с последующим сравнением не даёт
// блокировки строки. Она существует ради материализации строк учёта на промахе — без неё
// триггеру нечего списывать.
type QuotaGuard interface {
	Admit(ctx context.Context, projectID, kind string) error
}

// WithQuotaGuard подключает полосу учёта.
func (s *InstanceService) WithQuotaGuard(g QuotaGuard) *InstanceService {
	s.quota = g
	return s
}
