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
	DiskID     string
	IsBoot     bool
	Mode       AttachedDiskMode
	DeviceName string
	AutoDelete bool
	AttachedAt time.Time
}

// OneToOneNat — конфигурация one-to-one NAT на NIC (output-only зеркало из kacho-vpc).
type OneToOneNat struct {
	Address    string `json:"address,omitempty"`
	AddressID  string `json:"address_id,omitempty"`
	Ephemeral  bool   `json:"ephemeral,omitempty"`
	IPVersion  int32  `json:"ip_version,omitempty"`
	DNSRecords []byte `json:"dns_records,omitempty"`
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

// BootSource — единый вход ОС (COMP-1 F3). На входе — только Type/ID (+ImageKind
// роутинг); Name/ResolvedDigest/MaterializedVolume — output-only (resolve/materialize
// сага COMP-2). tag/digest живут ВНУТРИ ID.
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
	UserData              string         `json:"user_data,omitempty"`
	MetadataEndpoint      MetadataOption `json:"metadata_endpoint,omitempty"`
	MetadataTokenRequired bool           `json:"metadata_token_required,omitempty"`
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

	// Metadata — legacy free-form map (только на FULL view; back-compat).
	Metadata map[string]string
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
