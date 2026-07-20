// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"regexp"
	"time"
	"unicode/utf8"
)

// ID-префиксы ресурсов домена Storage (3-char, ids.NewID). Тип ресурса читается
// по префиксу. op-root storage = "sop" (opsproxy маршрутизирует Operation.Get по
// первым 3 символам op-id).
const (
	PrefixVolume    = "vol"
	PrefixSnapshot  = "snp"
	PrefixImage     = "img"
	PrefixDiskType  = "dtp"
	PrefixOperation = "sop"
)

// maxDisplayNameLen — верхняя граница tenant display-name (1..63, §1.1) для Volume и
// Snapshot. Формат зеркалит DB-CHECK volumes_name_check / snapshots_name_check.
const maxDisplayNameLen = 63

// displayNameRe — допустимый charset tenant display-name (Volume/Snapshot): lowercase,
// начинается с буквы, далее [-a-z0-9], заканчивается буквой/цифрой. Точная копия DB-CHECK
// *_name_check (self-validating newtype энфорсит тот же инвариант в домене, не полагаясь
// только на backstop CHECK). Общий для VolumeName и SnapshotName (parity).
var displayNameRe = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)

// errIllegalName / errIllegalSize — контрактные тексты валидации (часть контракта
// Kachō, §1.7; assert'ятся behaviour-level в newman/integration). serviceerr
// срезает sentinel-префикс, клиент видит именно эту строку.
var (
	//nolint:staticcheck // ST1005: контрактный текст Kachō (§1.7, "Illegal argument <field>") — капитализация нормативна
	errIllegalName = errors.New("Illegal argument name")
	//nolint:staticcheck // ST1005: контрактный текст Kachō (§1.7) — капитализация нормативна
	errIllegalSize = errors.New("Illegal argument size_bytes")
	// errSourceConflict — том нельзя засеять одновременно из snapshot и image (F9).
	errSourceConflict = errors.New("a volume is seeded from either a snapshot or an image, not both")
)

// VolumeName — self-validating newtype display-name тома (skill evgeniy: инвариант
// формы живёт на типе). Пустое имя допустимо (partial UNIQUE не действует на ”;
// два безымянных тома в проекте легальны, S1-06).
type VolumeName string

// Validate проверяет формат имени. Пусто → ok; иначе 1..63 lowercase из
// допустимого charset. Любое нарушение → фиксированный errIllegalName.
func (n VolumeName) Validate() error {
	return validateDisplayName(string(n))
}

// validateDisplayName — общий self-validating инвариант tenant display-name
// (Volume/Snapshot): пусто → ok; иначе 1..63 lowercase из допустимого charset. Любое
// нарушение → фиксированный контрактный errIllegalName ("Illegal argument name").
func validateDisplayName(v string) error {
	if v == "" {
		return nil
	}
	if utf8.RuneCountInString(v) > maxDisplayNameLen {
		return errIllegalName
	}
	if !displayNameRe.MatchString(v) {
		return errIllegalName
	}
	return nil
}

// VolumeStatus — статус жизненного цикла Volume. Ширина int32 совпадает с
// storagev1.Volume_Status, поэтому конверсии domain↔proto точны.
type VolumeStatus int32

// Значения VolumeStatus (parity с proto-enum storage.v1:
// UNSPECIFIED=0, CREATING=1, AVAILABLE=2, IN_USE=3, DELETING=4, ERROR=5).
const (
	VolumeStatusUnspecified VolumeStatus = iota
	VolumeStatusCreating
	VolumeStatusAvailable
	VolumeStatusInUse
	VolumeStatusDeleting
	VolumeStatusError
)

// Validate проверяет, что статус — известное значение.
func (s VolumeStatus) Validate() error {
	switch s {
	case VolumeStatusUnspecified, VolumeStatusCreating, VolumeStatusAvailable,
		VolumeStatusInUse, VolumeStatusDeleting, VolumeStatusError:
		return nil
	default:
		return errors.New("volume status is out of range")
	}
}

// DeriveStatus вычисляет wire-Status из persisted state + наличия attachment
// (§1.3, фикс дрейфа B3): READY+attachment → IN_USE, READY без attach → AVAILABLE,
// остальные state отображаются 1:1. Единственный источник derive — не хранится
// отдельной колонкой.
func DeriveStatus(state string, attached bool) VolumeStatus {
	switch state {
	case "CREATING":
		return VolumeStatusCreating
	case "DELETING":
		return VolumeStatusDeleting
	case "ERROR":
		return VolumeStatusError
	case "READY":
		if attached {
			return VolumeStatusInUse
		}
		return VolumeStatusAvailable
	default:
		return VolumeStatusUnspecified
	}
}

// Volume — блочный том (zonal-ресурс, привязан к zone_id и disk_type_id).
// Владелец — kacho-storage. Плоский ресурс (без K8s-envelope). Публичная проекция
// lean (INV-7): только tenant-facing поля, инфра-полей (backend-LUN/pool/node) на
// публичном Volume нет — они живут в internal-проекции :9091 (будущий data-plane).
//
// Attachments — output-only derive-on-read из volume_attachments (source of truth
// для attach-state); Status — derived (см. DeriveStatus). Оба заполняются repo на
// чтении, на вход Create/Update не принимаются.
type Volume struct {
	ID             string
	ProjectID      string
	Name           string
	Description    string
	Labels         map[string]string
	ZoneID         string
	DiskTypeID     string
	SizeBytes      int64
	BlockSize      int64
	SourceSnapshot string
	// SourceImage — id образа (Image), из которого материализован boot-Volume (F9).
	// Immutable; same-DB FK → images ON DELETE SET NULL (provenance, не live-dependency).
	// Взаимоисключение с SourceSnapshot: том засевается из ОДНОГО источника.
	SourceImage string
	Status      VolumeStatus
	Attachments []VolumeAttachment // output-only (0..1: PK volume_id → ≤1 attach)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate проверяет domain-инварианты Volume перед созданием. Порядок выдаёт
// контрактные тексты для input-негативов S1-11: name-формат → errIllegalName;
// size_bytes<=0 → errIllegalSize (DB-backstop CHECK size_bytes>0). Cross-service
// ссылки (zone_id→geo, project_id→iam) валидируются peer-API на request-path, а не
// здесь (existence — не domain-инвариант формы).
func (v Volume) Validate() error {
	if v.ProjectID == "" {
		return errors.New("volume project_id is required")
	}
	if v.ZoneID == "" {
		return errors.New("volume zone_id is required")
	}
	if v.DiskTypeID == "" {
		return errors.New("volume disk_type_id is required")
	}
	if err := VolumeName(v.Name).Validate(); err != nil {
		return err
	}
	if v.SizeBytes <= 0 {
		return errIllegalSize
	}
	// Взаимоисключение источников (F9, STOR-1-19): том засевается ЛИБО из snapshot,
	// ЛИБО из image, не из обоих (spoken-exclusion). Backstop — нет (нельзя выразить
	// одним DB-CHECK, оба поля независимо nullable), поэтому энфорсим в домене.
	if v.SourceSnapshot != "" && v.SourceImage != "" {
		return errSourceConflict
	}
	return v.Status.Validate()
}
