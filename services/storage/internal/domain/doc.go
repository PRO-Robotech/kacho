// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package domain — сущности kacho-storage (Volume / VolumeAttachment / Snapshot /
// DiskType).
//
// Domain-слой чистой архитектуры: чистый Go (ТОЛЬКО stdlib + kacho-proto-safe
// типы). Без pgx / grpc / internal-зависимостей — dependency rule (architecture.md).
// Сущности self-validating (skill evgeniy): инвариант формы живёт на самом типе, а
// не размазан inline по use-case/handler.
//
// Владелец домена Storage — kacho-storage. Другие сервисы (compute) ссылаются на
// volume по id (string, без cross-service FK) и валидируют через
// VolumeService.Get / InternalVolumeService.Attach.
//
// Набор полей отражает proto storage.v1. Domain несёт инварианты ФОРМЫ
// (self-validating newtype'ы: name/size). Реляционные инварианты (placement-
// coherence zone, size-increase-only, attach-CAS) — на DB-уровне в repo
// (data-integrity.md), не в домене.
package domain

import (
	"fmt"
	"unicode/utf8"
)

// maxNameLen — верхняя граница display-name ресурсов storage. Name — свободный
// tenant-assigned ярлык, поэтому валидируется только длина.
const maxNameLen = 253

// validateName — общий domain-инвариант длины display-name.
func validateName(field, value string) error {
	if utf8.RuneCountInString(value) > maxNameLen {
		return fmt.Errorf("%s exceeds %d characters", field, maxNameLen)
	}
	return nil
}
