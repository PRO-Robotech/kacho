// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "time"

// AttachmentMode — режим монтирования тома (READ_WRITE / READ_ONLY). Parity с
// proto storagev1.VolumeAttachment_Mode.
type AttachmentMode int32

// Значения AttachmentMode (UNSPECIFIED=0, READ_WRITE=1, READ_ONLY=2).
const (
	AttachmentModeUnspecified AttachmentMode = iota
	AttachmentModeReadWrite
	AttachmentModeReadOnly
)

// VolumeAttachment — связь Volume↔Instance (compute). Within-service строка
// volume_attachments; instance_id — cross-service ссылка (TEXT, без FK, резолвится
// compute'ом). instance_name — денормализованное output-only зеркало
// (source of truth = compute.Instance), не вход Create/Update.
//
// ProjectID / ZoneID — self-describing placement инстанса (из attach-payload,
// §3.2): storage сверяет их со СВОЕЙ строкой volumes атомарным CAS (zone/project
// coherence) и никогда не зовёт compute (ацикличность, INV-1). Хранятся в строке
// volume_attachments (колонки project_id/zone_id NOT NULL); на публичной проекции
// attachment (§1.1) их нет — только на internal attach-пути.
type VolumeAttachment struct {
	VolumeID     string
	InstanceID   string
	InstanceName string // output-only mirror
	ProjectID    string // self-describing placement (attach-CAS coherence)
	ZoneID       string // self-describing placement (attach-CAS coherence)
	DeviceName   string
	IsBoot       bool
	Mode         AttachmentMode
	AutoDelete   bool
	AttachedAt   time.Time
}
