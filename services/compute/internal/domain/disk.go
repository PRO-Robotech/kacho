// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"time"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// DiskStatus — состояние диска (control-plane: всегда READY после Create).
type DiskStatus int

// Значения DiskStatus зеркалят computev1.Disk_Status.
const (
	DiskStatusUnspecified DiskStatus = iota
	DiskStatusCreating
	DiskStatusReady
	DiskStatusError
	DiskStatusDeleting
)

// Disk — диск (zone-level ресурс). source = image|snapshot хранится в
// SourceImageID / SourceSnapshotID (взаимоисключающие; не FK — семантика Kachō
// допускает удаление source-ресурса).
//
// Сложные nested-поля (HardwareGeneration, KMSKey, DiskPlacementPolicy)
// хранятся как proto-указатели; repo сериализует их в JSONB через protojson.
type Disk struct {
	ID                  string
	ProjectID           string
	CreatedAt           time.Time
	Name                string
	Description         string
	Labels              map[string]string
	TypeID              string
	ZoneID              string
	Size                int64
	BlockSize           int64
	ProductIDs          []string
	Status              DiskStatus
	SourceImageID       string
	SourceSnapshotID    string
	DiskPlacementPolicy *computev1.DiskPlacementPolicy
	HardwareGeneration  *computev1.HardwareGeneration
	KMSKey              *computev1.KMSKey

	// InstanceIDs — output-only, ВСЕГДА пустой. Источником была таблица
	// `attached_disks`, дропнутая миграцией 0013 (storage-split): том↔Instance-
	// привязка теперь живёт в kacho-storage на Volume, а не на compute-Disk.
	// Писателя у поля нет; protoconv отдаёт его как пустой массив, пока wire-поле
	// Disk.instance_ids не будет снято вместе с самим ресурсом.
	InstanceIDs []string
}
