// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"time"
)

// ImageFormat — native Kachō single-tier image format (ban #2: не foreign-литерал
// qcow2/vmdk). Ширина int32 совпадает с storagev1.Image_Format для точной конверсии.
type ImageFormat int32

// Значения ImageFormat (parity с proto: UNSPECIFIED=0, STANDARD=1). Single-tier
// default — STANDARD; DB-CHECK format IN ('STANDARD').
const (
	ImageFormatUnspecified ImageFormat = iota
	ImageFormatStandard
)

// FormatStandard — единственный DB-текст формата (single-tier).
const FormatStandard = "STANDARD"

// ImagePlacementType — placement-семейство образа. Image — всегда REGIONAL (anycast):
// region-scoped, zone-независим (F10 OQ1). Ширина int32 == storagev1.Image_PlacementType.
type ImagePlacementType int32

// Значения ImagePlacementType (parity с proto: UNSPECIFIED=0, REGIONAL=1).
const (
	ImagePlacementUnspecified ImagePlacementType = iota
	ImagePlacementRegional
)

// ImageName — self-validating newtype display-name образа (skill evgeniy). Parity с
// VolumeName/SnapshotName: тот же lowercase-ASCII формат; пустое имя допустимо
// (partial UNIQUE не действует на пустую строку).
type ImageName string

// Validate проверяет формат имени образа (делегирует общий display-name-инвариант).
func (n ImageName) Validate() error {
	return validateDisplayName(string(n))
}

// errImageSourceRequired / errImageSourceConflict — контрактные тексты source-oneof
// exactly-one (F12): ни одного (blank DEFER) → required; оба → conflict.
var (
	errImageSourceRequired = errors.New("Image source is required") //nolint:staticcheck // контрактный текст
	errImageSourceConflict = errors.New("an image source must be either a snapshot or a volume, not both")
)

// ImageStatus — статус жизненного цикла Image (parity с proto storage.v1:
// UNSPECIFIED=0, CREATING=1, READY=2, DELETING=3, ERROR=4).
type ImageStatus int32

// Значения ImageStatus.
const (
	ImageStatusUnspecified ImageStatus = iota
	ImageStatusCreating
	ImageStatusReady
	ImageStatusDeleting
	ImageStatusError
)

// Validate проверяет, что статус — известное значение.
func (s ImageStatus) Validate() error {
	switch s {
	case ImageStatusUnspecified, ImageStatusCreating, ImageStatusReady,
		ImageStatusDeleting, ImageStatusError:
		return nil
	default:
		return errors.New("image status is out of range")
	}
}

// ImageStatusFromState маппит persisted state ({CREATING,READY,DELETING,ERROR}) в
// wire-Status. У образа нет derived-состояний — маппинг 1:1; Create → READY сразу
// (control-plane, durable Operation.done; ban #9 — не гейтит downstream).
func ImageStatusFromState(state string) ImageStatus {
	switch state {
	case "CREATING":
		return ImageStatusCreating
	case "READY":
		return ImageStatusReady
	case "DELETING":
		return ImageStatusDeleting
	case "ERROR":
		return ImageStatusError
	default:
		return ImageStatusUnspecified
	}
}

// Image — VM boot-образ (REGIONAL/anycast ресурс, привязан к region_id). Владелец —
// kacho-storage. Плоский ресурс. Публичная проекция lean (INV-1): infra-полей
// (blob-layout/bucket/engine-namespace/storage-node) на публичном Image нет — они
// живут в internal-проекции :9091 (будущий data-plane).
//
// Source — EXACTLY-ONE из SourceSnapshot / SourceVolume (F12): образ создаётся из
// снапшота ЛИБО тома напрямую (blank-upload DEFER). Оба immutable; same-DB FK →
// snapshots/volumes ON DELETE SET NULL (provenance). SizeBytes/MinDiskBytes derived
// из размера источника на Insert.
type Image struct {
	ID             string
	ProjectID      string
	Name           string
	Description    string
	Labels         map[string]string
	RegionID       string
	Placement      ImagePlacementType
	SourceSnapshot string
	SourceVolume   string
	SizeBytes      int64
	MinDiskBytes   int64
	Format         ImageFormat
	Status         ImageStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate проверяет domain-инварианты Image перед созданием. Порядок выдаёт
// контрактные тексты для input-негативов: name-формат → errIllegalName; source
// exactly-one → required/conflict (F12, STOR-1-24). Cross-service ссылки
// (region_id→geo, project_id→iam) валидируются peer-API на request-path.
func (i Image) Validate() error {
	if i.ProjectID == "" {
		return errors.New("image project_id is required")
	}
	if i.RegionID == "" {
		return errors.New("image region_id is required")
	}
	if err := ImageName(i.Name).Validate(); err != nil {
		return err
	}
	// source oneof exactly-one (F12): ни одного (blank DEFER) → required; оба → conflict.
	hasSnap := i.SourceSnapshot != ""
	hasVol := i.SourceVolume != ""
	switch {
	case hasSnap && hasVol:
		return errImageSourceConflict
	case !hasSnap && !hasVol:
		return errImageSourceRequired
	}
	return i.Status.Validate()
}
