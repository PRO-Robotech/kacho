// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import "github.com/PRO-Robotech/kacho/services/compute/internal/ports"

// Port-интерфейсы и связанные value-объекты вынесены в leaf-пакет
// `internal/ports` — это позволяет переиспользовать общий test-helper
// `internal/ports/portmock` без import-cycle. Здесь — type-alias'ы для
// удобства: service-код и adapter'ы (`internal/repo`, `internal/clients`)
// ссылаются на `service.*` имена. Зеркалит kacho-vpc/internal/service/ports.go.

type (
	// Pagination — постраничная навигация.
	Pagination = ports.Pagination

	// InstanceFilter — фильтр для списка ВМ.
	InstanceFilter = ports.InstanceFilter

	// InstanceRepo — port-интерфейс репозитория ВМ.
	InstanceRepo = ports.InstanceRepo
	// DiskTypeRepo — port-интерфейс репозитория типов дисков.
	DiskTypeRepo = ports.DiskTypeRepo
	// MachineTypeRepo — port-интерфейс каталога machine-type (COMP-1 F7).
	MachineTypeRepo = ports.MachineTypeRepo
	// MachineTypeFilter — фильтр списка machine-type (name=/family=/minGpus=).
	MachineTypeFilter = ports.MachineTypeFilter

	// ProjectClient — port для проверки существования Project.
	ProjectClient = ports.ProjectClient
	// ZoneRegistry — port existence-check zone_id (Disk/Instance Create, Disk Relocate)
	// + авторитетный zone→region резолв (placement-coherence с anycast-подсетью).
	ZoneRegistry = ports.ZoneRegistry
	// SubnetRegistry — peer-валидация подсети NIC-спеки через kacho-vpc
	// (placement-coherence зоны инстанса и его интерфейсов).
	SubnetRegistry = ports.SubnetRegistry
	// SubnetPlacement — placement-проекция vpc.Subnet (ZONAL zone / REGIONAL region).
	SubnetPlacement = ports.SubnetPlacement

	// NicClient — port для compute→kacho-vpc InternalNetworkInterfaceService
	// (NIC↔Instance attach/detach + batched mirror-read).
	NicClient = ports.NicClient
	// NicAttachSpec — self-describing NIC-attach payload.
	NicAttachSpec = ports.NicAttachSpec
	// NicAttachment — NIC↔Instance binding + addressing mirror.
	NicAttachment = ports.NicAttachment

	// StorageClient — port для compute→kacho-storage InternalVolumeService
	// (volume↔Instance attach/detach + batched mirror-read).
	StorageClient = ports.StorageClient

	// OwnerRegistrar — синхронная post-commit регистрация owner-tuple (sync-registrar
	// window-оптимизация); реализуется clients.SyncRegistrar поверх
	// InternalIAMService.RegisterResource.
	OwnerRegistrar = ports.OwnerRegistrar
	// VolumeAttachSpec — self-describing volume-attach payload.
	VolumeAttachSpec = ports.VolumeAttachSpec
	// VolumeAttachmentInfo — volume↔Instance attachment mirror.
	VolumeAttachmentInfo = ports.VolumeAttachmentInfo
	// VolumeAttachMode — access mode of a volume attachment.
	VolumeAttachMode = ports.VolumeAttachMode
)
