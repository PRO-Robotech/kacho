// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"fmt"
	"time"
)

// SnapshotName — self-validating newtype display-name снапшота (skill evgeniy). Parity
// с VolumeName: тот же lowercase-ASCII proto-pattern (CS1-S3-03). Пустое имя допустимо
// (partial UNIQUE не действует на пустую строку). uppercase / non-ASCII (кириллица
// "снимок") / >63 / invalid-char → фиксированный "Illegal argument name" (не
// length-only, как admin DiskType — tenant-ресурс несёт строгий формат).
type SnapshotName string

// Validate проверяет формат имени снапшота (делегирует общий display-name-инвариант).
func (n SnapshotName) Validate() error {
	return validateDisplayName(string(n))
}

// SnapshotStatus — статус жизненного цикла Snapshot (parity с proto storage.v1:
// UNSPECIFIED=0, CREATING=1, READY=2, DELETING=3, ERROR=4).
type SnapshotStatus int32

// Значения SnapshotStatus.
const (
	SnapshotStatusUnspecified SnapshotStatus = iota
	SnapshotStatusCreating
	SnapshotStatusReady
	SnapshotStatusDeleting
	SnapshotStatusError
)

// Validate проверяет, что статус — известное значение.
func (s SnapshotStatus) Validate() error {
	switch s {
	case SnapshotStatusUnspecified, SnapshotStatusCreating, SnapshotStatusReady,
		SnapshotStatusDeleting, SnapshotStatusError:
		return nil
	default:
		return fmt.Errorf("snapshot status %d is out of range", int32(s))
	}
}

// SnapshotStatusFromState маппит persisted state ({CREATING,READY,DELETING,ERROR})
// в wire-Status. У снапшота нет derived-состояний (нет attach) — маппинг 1:1;
// Create переводит state→READY сразу (control-plane, §1.4).
func SnapshotStatusFromState(state string) SnapshotStatus {
	switch state {
	case "CREATING":
		return SnapshotStatusCreating
	case "READY":
		return SnapshotStatusReady
	case "DELETING":
		return SnapshotStatusDeleting
	case "ERROR":
		return SnapshotStatusError
	default:
		return SnapshotStatusUnspecified
	}
}

// Snapshot — снимок Volume в точке времени. Владелец — kacho-storage.
// source_volume_id — within-service ссылка на volumes(id) (FK, реализует
// rpc-implementer в миграции).
type Snapshot struct {
	ID             string
	ProjectID      string
	Name           string
	Description    string
	Labels         map[string]string
	SourceVolumeID string
	SizeBytes      int64
	Status         SnapshotStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// ZoneID — СОБСТВЕННЫЙ якорь размещения снимка.
	//
	// Прежде размещения у снимка не было вовсе: зона добиралась через исходный
	// том, а эта ссылка обнуляется при его удалении — снимок обязан переживать
	// источник. Проверка когерентности при засеве из такого снимка вырождалась в
	// тождественно истинную, пропуская ровно тот случай, ради которого писалась.
	//
	// Пустая зона — честное «размещение неизвестно» для строк, доставшихся от
	// прежней схемы. Она ЗАПРЕЩАЕТ засев: доказать когерентность нечем, а
	// придумать размещение задним числом нельзя.
	ZoneID string

	StatusReason StatusReason

	// SeededVolumeIDs — тома, засеянные из этого источника. Output-only,
	// выводится на чтении, на вход не принимается.
	SeededVolumeIDs []string
	Backend         Placement
	Observation     Observation
}

// Validate проверяет domain-инварианты Snapshot перед созданием/сохранением.
func (s Snapshot) Validate() error {
	if s.ProjectID == "" {
		return fmt.Errorf("snapshot project_id is required")
	}
	if s.SourceVolumeID == "" {
		return fmt.Errorf("snapshot source_volume_id is required")
	}
	if err := SnapshotName(s.Name).Validate(); err != nil {
		return err
	}
	return s.Status.Validate()
}
