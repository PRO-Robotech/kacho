// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package domain — сущности kacho-geo (Geography: Region / Zone).
//
// Domain-слой чистой архитектуры: чистый Go (только stdlib). Region/Zone —
// глобальные ресурсы платформенной топологии, владелец — kacho-geo (leaf-сервис).
// Они НЕ привязаны к Project/Account — cluster-scoped топология. Другие сервисы
// ссылаются на region/zone по id (string, без cross-service FK) и валидируют через
// RegionService.Get / ZoneService.Get.
package domain

import (
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"
)

// maxNameLen — верхняя граница display-name Region/Zone. Name — свободный
// admin-assigned ярлык ("Region 1", "Zone A"), не slug, поэтому валидируем только
// длину (charset-regex из corelib рассчитан на strict slug-ресурсы и отверг бы
// пробелы/uppercase).
const maxNameLen = 253

// maxIDLen — верхняя граница id Region/Zone (DNS-label-подобный slug, 63 симв.).
const maxIDLen = 63

// idFormat — slug-инвариант admin-assigned id: строчная буква в начале, далее
// hyphen-разделённые сегменты строчных alnum (без ведущего/висящего/двойного
// дефиса, без uppercase/пробелов/пунктуации/подчёркиваний). id — канонический
// cross-service reference key (другие сервисы хранят его как TEXT и сверяют по
// RegionService.Get / ZoneService.Get), поэтому его форма — контракт, а не
// свободный ярлык (в отличие от Name).
var idFormat = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidateID проверяет slug-инвариант id ресурса Region/Zone (и region_id, того
// же namespace). Пустой id → "<field> is required"; слишком длинный или не-slug →
// InvalidArgument-текст. Вызывается из Region/Zone.Validate на Create-пути, чтобы
// малформ отвергался синхронно, а не персистился как PK/canonical reference.
func ValidateID(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxIDLen {
		return fmt.Errorf("%s exceeds %d characters", field, maxIDLen)
	}
	if !idFormat.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase slug (^[a-z][a-z0-9-]*$, hyphen-separated, e.g. region-1)", field)
	}
	return nil
}

// ZoneStatus — статус availability-zone. Ширина int32 точно совпадает с
// geov1.Zone_Status, поэтому конверсии domain↔proto точны (без сужения int→int32).
type ZoneStatus int32

// Значения ZoneStatus (parity с proto-enum geo.v1: UNSPECIFIED=0, UP=1, DOWN=2).
const (
	ZoneStatusUnspecified ZoneStatus = iota
	ZoneStatusUp
	ZoneStatusDown
)

// Validate проверяет, что статус — известное значение (UNSPECIFIED/UP/DOWN).
// Out-of-range статус → ошибка (оживляет CHECK-маппинг и не пишет мусор в БД).
func (s ZoneStatus) Validate() error {
	switch s {
	case ZoneStatusUnspecified, ZoneStatusUp, ZoneStatusDown:
		return nil
	default:
		return fmt.Errorf("zone status %d is out of range", int32(s))
	}
}

// ValidateName проверяет длину display-name (общий domain-инвариант Region/Zone).
// Используется и из Region/Zone.Validate, и из use-case при partial-Update, когда
// валидируется только заданное новое имя.
func ValidateName(field, value string) error {
	if utf8.RuneCountInString(value) > maxNameLen {
		return fmt.Errorf("%s exceeds %d characters", field, maxNameLen)
	}
	return nil
}

// Region — глобальный geography-ресурс (id = "region-1"). Admin-assigned,
// immutable PK. Домен kacho-geo.
type Region struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Validate проверяет domain-инварианты Region перед созданием/сохранением.
// id обязан быть непустым (admin-assigned PK); name — в пределах лимита длины.
func (r Region) Validate() error {
	if err := ValidateID("region id", r.ID); err != nil {
		return err
	}
	if err := ValidateName("region name", r.Name); err != nil {
		return err
	}
	return nil
}

// Zone — availability-zone (глобальный read-only справочник; id = "region-1-a").
// Принадлежит Region (region_id, FK RESTRICT на стороне БД).
type Zone struct {
	ID        string
	RegionID  string
	Name      string
	Status    ZoneStatus
	CreatedAt time.Time
}

// Validate проверяет domain-инварианты Zone перед созданием/сохранением.
// id и region_id обязаны быть непустыми; name — в пределах лимита длины;
// status — известное значение enum.
func (z Zone) Validate() error {
	if err := ValidateID("zone id", z.ID); err != nil {
		return err
	}
	if err := ValidateID("zone region_id", z.RegionID); err != nil {
		return err
	}
	if err := ValidateName("zone name", z.Name); err != nil {
		return err
	}
	if err := z.Status.Validate(); err != nil {
		return err
	}
	return nil
}
