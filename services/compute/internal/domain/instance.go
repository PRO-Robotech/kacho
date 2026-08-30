// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"time"
)

// MaxCPUGuaranteePercent — верхняя граница cpu_guarantee_percent (нижняя — 0).
const MaxCPUGuaranteePercent = 100

// ErrInvalidCPUGuaranteePercent — cpu_guarantee_percent вне допустимого [0,100].
var ErrInvalidCPUGuaranteePercent = errors.New("cpu_guarantee_percent out of range")

// ValidCPUGuaranteePercent сообщает, лежит ли v в допустимом диапазоне [0,100]
// (0 = best-effort/burstable, 1..100 = гарантированный baseline per vCPU).
func ValidCPUGuaranteePercent(v int32) bool { return v >= 0 && v <= MaxCPUGuaranteePercent }

// InstanceStatus — состояние ВМ (control-plane: детерминированная state-машина).
// Значения зеркалят computev1.Instance_Status.
type InstanceStatus int

// Значения InstanceStatus.
const (
	InstanceStatusUnspecified InstanceStatus = iota
	InstanceStatusProvisioning
	InstanceStatusRunning
	InstanceStatusStopping
	InstanceStatusStopped
	InstanceStatusStarting
	InstanceStatusRestarting
	InstanceStatusUpdating
	InstanceStatusError
	InstanceStatusCrashed
	InstanceStatusDeleting
)

// InstanceKind — сильный первый дискриминатор (COMP-1 F1). VM XOR CONTAINER, гейтит
// ровно один вложенный spec; immutable после Create. Зеркалит computev1.InstanceKind.
type InstanceKind int32

// Значения InstanceKind.
const (
	InstanceKindUnspecified InstanceKind = iota // INSTANCE_KIND_UNSPECIFIED
	InstanceKindVM                              // VM
	InstanceKindContainer                       // CONTAINER
)

// Valid сообщает, что kind — конкретный (VM или CONTAINER), не UNSPECIFIED.
func (k InstanceKind) Valid() bool { return k == InstanceKindVM || k == InstanceKindContainer }

// ImageKind — формальный дискриминатор источника ОС (COMP-1 F3/B13): storage.image
// (OS/disk-образ) vs registry.image (OCI-артефакт). Зеркалит computev1.ImageKind.
type ImageKind int32

// Значения ImageKind.
const (
	ImageKindUnspecified  ImageKind = iota // IMAGE_KIND_UNSPECIFIED
	ImageKindStorageImage                  // STORAGE_IMAGE
	ImageKindOCIImage                      // OCI_IMAGE
)

// MetadataOption — vendor-agnostic состояние metadata-endpoint (COMP-1 F9). Зеркалит
// computev1.MetadataOption.
type MetadataOption int32

// Значения MetadataOption.
const (
	MetadataOptionUnspecified MetadataOption = iota // METADATA_OPTION_UNSPECIFIED
	MetadataOptionEnabled                           // ENABLED
	MetadataOptionDisabled                          // DISABLED
)

// RestartPolicy — политика перезапуска CONTAINER-job (COMP-1 F1). Зеркалит
// computev1.RestartPolicy.
type RestartPolicy int32

// Значения RestartPolicy.
const (
	RestartPolicyUnspecified RestartPolicy = iota // RESTART_POLICY_UNSPECIFIED
	RestartPolicyNever                            // NEVER
	RestartPolicyOnFailure                        // ON_FAILURE
	RestartPolicyAlways                           // ALWAYS
)

// AttachedDiskMode — режим подключения диска (зеркалит computev1.AttachedDisk_Mode).
type AttachedDiskMode int

// Значения AttachedDiskMode.
const (
	AttachedDiskModeUnspecified AttachedDiskMode = iota
	AttachedDiskModeReadOnly
	AttachedDiskModeReadWrite
)

// AttachedDisk — output-only зеркало volume-привязки (COMP-2; пусто в COMP-1).
type AttachedDisk struct {
	VolumeID   string
	IsBoot     bool
	Mode       AttachedDiskMode
	DeviceName string
	AutoDelete bool
	AttachedAt time.Time
}

// OneToOneNat — конфигурация one-to-one NAT на NIC (output-only зеркало из kacho-vpc).
//
// Поле `DNSRecords []byte` снято вместе с DNS-поверхностью контракта: у него не было ни
// читателя, ни писателя во всём дереве сервиса, а сериализовалось оно в колонку
// `instance_network_interfaces.primary_v*_nat` (JSONB) с `omitempty` — то есть никогда
// не появлялось и в персисте. Домен DNS у платформы отсутствует; появится — придёт своим
// доменом, а не байтовым полем в зеркале NAT.
type OneToOneNat struct {
	Address   string `json:"address,omitempty"`
	AddressID string `json:"address_id,omitempty"`
	Ephemeral bool   `json:"ephemeral,omitempty"`
	IPVersion int32  `json:"ip_version,omitempty"`
}

// NetworkInterface — output-only зеркало NIC-привязки (source of truth = kacho-vpc;
// материализуется launch-сагой COMP-2, пусто в COMP-1).
type NetworkInterface struct {
	Index              string
	NICID              string
	MACAddress         string
	SubnetID           string
	PrimaryV4Address   string
	PrimaryV4AddressID string
	PrimaryV4Nat       *OneToOneNat
	PrimaryV6Address   string
	PrimaryV6Nat       *OneToOneNat
	SecurityGroupIDs   []string
}

// MaterializedVolume — output-only зеркало boot-Volume (COMP-2; пусто в COMP-1).
type MaterializedVolume struct {
	VolumeID     string `json:"volume_id,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	SizeGiB      int64  `json:"size_gib,omitempty"`
	VolumeTypeID string `json:"volume_type_id,omitempty"`
}

// BootSource — единый вход ОС (COMP-1 F3). На входе принимаются ТОЛЬКО Type/ID;
// Name/ResolvedDigest/MaterializedVolume/ImageKind — output-only и на входе
// отвергаются (resolve/materialize сага COMP-2). tag/digest живут ВНУТРИ ID.
//
// ImageKind стоит в этом перечне четвёртым намеренно: прежняя редакция называла
// его входом («+ImageKind роутинг»), тогда как значение выводит сервер из Type
// (imageKindFor), а присланное отвергается синхронно. Два места об одном поле
// расходились, и верным было второе — то, которое исполняется.
type BootSource struct {
	Type               string              `json:"type"`
	ID                 string              `json:"id"`
	Name               string              `json:"name,omitempty"`
	ResolvedDigest     string              `json:"resolved_digest,omitempty"`
	ImageKind          ImageKind           `json:"image_kind,omitempty"`
	MaterializedVolume *MaterializedVolume `json:"materialized_volume,omitempty"`
}

// VMSpec — конфигурация VM (instance_kind = VM).
type VMSpec struct {
	UserData         string         `json:"user_data,omitempty"`
	MetadataEndpoint MetadataOption `json:"metadata_endpoint,omitempty"`

	// Поля «требуется ли сессионный токен» здесь НЕТ, и это решение, а не
	// пропуск: токен обязателен by construction, а ручка, которой можно
	// отключить защиту, однажды будет отключена. Снято вместе с полем контракта
	// (номер и имя зарезервированы навсегда).
}

// ContainerPort — объявление порта контейнера.
type ContainerPort struct {
	ContainerPort int32  `json:"container_port,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

// ContainerSpec — конфигурация CONTAINER-job (instance_kind = CONTAINER). ExitCode —
// output-only (терминальный SUCCEEDED/FAILED).
type ContainerSpec struct {
	Command       []string          `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Ports         []ContainerPort   `json:"ports,omitempty"`
	RestartPolicy RestartPolicy     `json:"restart_policy,omitempty"`
	ExitCode      int32             `json:"exit_code,omitempty"`
}

// Instance — вычислительный ресурс (COMP-1 redesign). Плоская durable control-plane-
// запись. InstanceKind гейтит один из VMSpec/ContainerSpec; MachineTypeID —
// единственный канал sizing (EffectiveResources — output-зеркало каталога);
// BootSource — единственный вход ОС. Инфра-чувствительные placement-поля НЕ здесь
// (two-projection). NetworkInterfaces/AttachedDisks — output-only зеркала (COMP-2).
type Instance struct {
	ID          string
	ProjectID   string
	CreatedAt   time.Time
	Name        string
	Description string
	Labels      map[string]string
	ZoneID      string

	Status       InstanceStatus
	StatusReason string

	Hostname string
	FQDN     string

	CPUGuaranteePercent int32

	InstanceKind       InstanceKind
	MachineTypeID      string
	EffectiveResources EffectiveResources
	BootSource         BootSource
	PlacementGroupID   string
	ServiceAccountID   string

	// VMSpec set при kind=VM; ContainerSpec — при kind=CONTAINER (взаимоисключающе).
	VMSpec        *VMSpec
	ContainerSpec *ContainerSpec

	// RegionID — регион зоны машины, РЕЗОЛВНУТЫЙ У ВЛАДЕЛЬЦА Geography.
	//
	// Своей колонки у него нет и не должно быть: регион зоны знает geo, и
	// хранить его копию значило бы завести второй источник истины, который
	// разъедется молча. Поле транзиентное — заполняется на пути запроса ради
	// когерентности с региональной группой размещения и на wire не выходит.
	RegionID string

	// Ключи входа гостя — ссылками по неизменяемому идентификатору. Материал
	// ключа здесь не лежит и лежать не может: ключ — отдельный ресурс, и его
	// срок жизни не совпадает со сроком жизни машины.
	GuestAccessKeyIDs []string

	// Output-only зеркала (материализуются launch-сагами COMP-2; пусто в COMP-1).
	NetworkInterfaces []NetworkInterface
	AttachedDisks     []AttachedDisk
}

// Validate проверяет доменные инварианты Instance (self-validating domain):
// cpu_guarantee_percent обязан лежать в [0,100] (зеркалит DB-CHECK); kind конкретен.
func (i *Instance) Validate() error {
	if !ValidCPUGuaranteePercent(i.CPUGuaranteePercent) {
		return ErrInvalidCPUGuaranteePercent
	}
	return nil
}

// BootDiskMirror возвращает boot attached-disk зеркало (is_boot=true) или nil.
func (i *Instance) BootDiskMirror() *AttachedDisk {
	for idx := range i.AttachedDisks {
		if i.AttachedDisks[idx].IsBoot {
			return &i.AttachedDisks[idx]
		}
	}
	return nil
}

// GuestAccessKey — публичная половина ключа, с которым арендатор входит в
// машину.
//
// Закрытая половина здесь не хранится никогда: её место — у арендатора, а
// хранилище закрытых ключей есть отдельный домен со своей моделью угроз.
type GuestAccessKey struct {
	ID          string
	ProjectID   string
	Name        string
	PublicKey   string
	Fingerprint string
	Labels      map[string]string
	CreatedAt   time.Time
}

// PlacementStrategy — что группа размещения делает с машинами.
type PlacementStrategy int32

// Значения зеркалят перечисление контракта; строковые имена совпадают с
// ограничением схемы поэлементно.
const (
	PlacementStrategyUnspecified PlacementStrategy = 0
	PlacementStrategySpread      PlacementStrategy = 1
	PlacementStrategyPack        PlacementStrategy = 2
)

// PlacementAnchorType — какой координатой закреплена группа.
type PlacementAnchorType int32

const (
	PlacementTypeUnspecified PlacementAnchorType = 0
	PlacementTypeZonal       PlacementAnchorType = 1
	PlacementTypeRegional    PlacementAnchorType = 2
)

// PlacementGroup — правило взаимного размещения машин.
//
// Числового параметра разнесения здесь нет: он описывает нашу раскладку железа,
// а не намерение арендатора. Намерений ровно два, и оба выразимы стратегией.
type PlacementGroup struct {
	ID            string
	ProjectID     string
	Name          string
	Description   string
	Labels        map[string]string
	CreatedAt     time.Time
	Strategy      PlacementStrategy
	PlacementType PlacementAnchorType
	ZoneID        string
	RegionID      string
}

// StrategyName переводит стратегию в имя, которое принимает схема.
// Неизвестное значение даёт пустую строку — её отвергнет ограничение схемы, а
// не молча запишет.
func (s PlacementStrategy) StrategyName() string {
	switch s {
	case PlacementStrategySpread:
		return "SPREAD"
	case PlacementStrategyPack:
		return "PACK"
	}
	return ""
}

// ParsePlacementStrategy — обратное отображение; ok=false на неизвестном.
func ParsePlacementStrategy(name string) (PlacementStrategy, bool) {
	switch name {
	case "SPREAD":
		return PlacementStrategySpread, true
	case "PACK":
		return PlacementStrategyPack, true
	}
	return PlacementStrategyUnspecified, false
}

// PlacementTypeName переводит якорь в имя, которое принимает схема.
func (p PlacementAnchorType) PlacementTypeName() string {
	switch p {
	case PlacementTypeZonal:
		return "ZONAL"
	case PlacementTypeRegional:
		return "REGIONAL"
	}
	return ""
}

// ParsePlacementType — обратное отображение; ok=false на неизвестном.
func ParsePlacementType(name string) (PlacementAnchorType, bool) {
	switch name {
	case "ZONAL":
		return PlacementTypeZonal, true
	case "REGIONAL":
		return PlacementTypeRegional, true
	}
	return PlacementTypeUnspecified, false
}
