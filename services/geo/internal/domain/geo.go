// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package domain — сущности kacho-geo (Geography: Region / Zone).
//
// Domain-слой чистой архитектуры: чистый Go (только stdlib). Region/Zone —
// глобальные ресурсы платформенной топологии (оси размещения), владелец —
// kacho-geo (leaf-сервис). Они НЕ привязаны к Project/Account — cluster-scoped
// координаты. Публичная поверхность — lean read-discovery; сырой admin-флаг
// status и infra° — two-projection (только Internal). Другие сервисы ссылаются
// на region/zone по id (string, без cross-service FK) и валидируют через
// RegionService.Get / ZoneService.Get.
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// maxNameLen — верхняя граница display-name Region/Zone. Name — свободный
// admin-assigned ярлык ("RU Central 1", "Zone A"), не slug, поэтому валидируем
// только длину (charset-regex рассчитан на strict slug-ресурсы и отверг бы
// пробелы/uppercase).
const maxNameLen = 253

// maxIDLen — верхняя граница id Region/Zone (DNS-label-подобный slug, 63 симв.).
const maxIDLen = 63

// idFormat — slug-инвариант admin-assigned id: строчная буква в начале, далее
// hyphen-разделённые сегменты строчных alnum. id — канонический cross-service
// reference key И человеко-осмысленная placement-координата (THE ONE carve-out
// из 3-char-prefix+crockford-base32, module-geo rule 7).
var idFormat = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// countryCodeFormat — ISO-3166 alpha-2: ровно два uppercase-символа.
var countryCodeFormat = regexp.MustCompile(`^[A-Z]{2}$`)

// ValidateID проверяет slug-инвариант id ресурса (res ∈ {"region","zone"}).
// Пустой/слишком длинный/не-slug → конвенционный текст api-conventions.md
// "invalid <res> id '<value>'" (malformed ловится первым стейтментом RPC,
// НЕ персистится как PK/canonical reference и НЕ уходит в repo → NotFound).
func ValidateID(res, value string) error {
	if value == "" || len(value) > maxIDLen || !idFormat.MatchString(value) {
		return fmt.Errorf("invalid %s id '%s'", res, value)
	}
	return nil
}

// ValidateZoneCoupling энфорсит coupling `zone.id == regionId + "-" + <zoneSuffix>`
// (строго startsWith(regionId+"-"), НЕ голый startsWith — контрпример
// "ru-central10-a" под "ru-central1" → REJECT, т.к. следующий символ '0', не '-').
// Несоответствие → "zone id '<zoneID>' must be prefixed by its regionId '<regionID>'"
// (проверяется ДО любого FK-резолва). module-geo rule 7.
func ValidateZoneCoupling(zoneID, regionID string) error {
	if !strings.HasPrefix(zoneID, regionID+"-") {
		return fmt.Errorf("zone id '%s' must be prefixed by its regionId '%s'", zoneID, regionID)
	}
	return nil
}

// ValidateCountryCode проверяет формат countryCode: пустой — OK (опционально),
// непустой — ровно 2 uppercase-буквы (ISO-3166 alpha-2). Иначе конвенционный
// текст "countryCode must be an ISO-3166 alpha-2 code". LIVE-mutable, энфорсится
// и на Create, и на Update.
func ValidateCountryCode(value string) error {
	if value == "" {
		return nil
	}
	if !countryCodeFormat.MatchString(value) {
		return fmt.Errorf("countryCode must be an ISO-3166 alpha-2 code")
	}
	return nil
}

// ValidateName проверяет длину display-name (общий domain-инвариант Region/Zone).
// Required-check (пустой name → InvalidArgument) делает use-case на Create-пути:
// на Update пустой name означает «не менять поле» (COALESCE).
func ValidateName(field, value string) error {
	if utf8.RuneCountInString(value) > maxNameLen {
		return fmt.Errorf("%s exceeds %d characters", field, maxNameLen)
	}
	return nil
}

// GeoStatus — сырой admin maintenance-флаг Region/Zone (two-projection: только
// Internal). Ширина int32 совпадает с geov1.GeoStatus — конверсии domain↔proto
// точны. Fresh Region/Zone поднимаются GeoStatusDown (fail-safe).
type GeoStatus int32

// Значения GeoStatus (parity с proto-enum geo.v1.GeoStatus: UNSPECIFIED=0,
// UP=1, DOWN=2).
const (
	GeoStatusUnspecified GeoStatus = iota
	GeoStatusUp
	GeoStatusDown
)

// ZoneStatus — backward-compat alias канонического GeoStatus (Region и Zone
// делят единый статус-enum; исторически звался ZoneStatus у zone-call-sites).
type ZoneStatus = GeoStatus

// Backward-compat const-псевдонимы GeoStatus для zone-call-sites.
const (
	ZoneStatusUnspecified = GeoStatusUnspecified
	ZoneStatusUp          = GeoStatusUp
	ZoneStatusDown        = GeoStatusDown
)

// Validate проверяет, что статус — известное значение (UNSPECIFIED/UP/DOWN).
func (s GeoStatus) Validate() error {
	switch s {
	case GeoStatusUnspecified, GeoStatusUp, GeoStatusDown:
		return nil
	default:
		return fmt.Errorf("geo status %d is out of range", int32(s))
	}
}

// PlacementBlockedReason — почему derived openForPlacement° зоны false (только на
// Zone). Parity с proto-enum geo.v1.PlacementBlockedReason (UNSPECIFIED=0,
// NONE=1, ZONE_DOWN=2, REGION_DOWN=3).
type PlacementBlockedReason int32

// Значения PlacementBlockedReason.
const (
	PlacementBlockedReasonUnspecified PlacementBlockedReason = iota
	PlacementBlockedReasonNone
	PlacementBlockedReasonZoneDown
	PlacementBlockedReasonRegionDown
)

// RegionInfra — infra-sensitive проекция Region (two-projection: только Internal).
type RegionInfra struct {
	NumericInfraID int64  // immutable после Create
	CapacityHint   string // read-time rollup (не persisted), заполняется GetInternal
}

// ZoneInfra — infra-sensitive проекция Zone (two-projection: только Internal).
type ZoneInfra struct {
	NumericInfraID     int64    // immutable после Create
	HostClasses        []string // mutable; НИКОГДА на public
	FailureDomainCount int32    // mutable
	UnderlayAnchor     string   // mutable
	CapacityHint       string   // AMPLE|CONSTRAINED|FULL; mutable; НИКОГДА на public
}

// Region — глобальная placement-координата (REGIONAL/anycast). Домен kacho-geo.
type Region struct {
	ID          string
	Name        string
	CountryCode string
	Status      GeoStatus
	Infra       RegionInfra
	CreatedAt   time.Time
	// OpenZoneCount — read-time rollup числа зон с openForPlacement°=true
	// (advisory-hint; НЕ persisted). Заполняется read-path'ом; zero на write.
	OpenZoneCount int64
}

// OpenForPlacement — derived: единственный публичный placement-сигнал региона.
// = status==UP. АДМИНИСТРАТИВНАЯ availability, НЕ гарантия ёмкости. Region: когда
// false — причина ВСЕГДА REGION_DOWN by construction.
func (r Region) OpenForPlacement() bool { return r.Status == GeoStatusUp }

// Zone — placement-координата (ZONAL) внутри Region. region_id — class-A
// within-service FK RESTRICT.
type Zone struct {
	ID        string
	RegionID  string
	Name      string
	Status    GeoStatus
	Infra     ZoneInfra
	CreatedAt time.Time
	// RegionStatus — статус родит-региона как есть на момент read (JOIN); нужен
	// для деривации openForPlacement°/placementBlockedReason°. Zero на write-путях.
	RegionStatus GeoStatus
}

// OpenForPlacement — derived: единственный публичный placement-сигнал зоны.
// = zone.status==UP && region.status==UP. АДМИНИСТРАТИВНАЯ availability, НЕ
// гарантия ёмкости — Create МОЖЕТ упасть на ёмкости в schedule-time.
func (z Zone) OpenForPlacement() bool {
	return z.Status == GeoStatusUp && z.RegionStatus == GeoStatusUp
}

// PlacementBlockedReason — почему openForPlacement° false, в одном вызове.
// precedence: zone.status==DOWN⇒ZONE_DOWN; иначе region.status==DOWN⇒REGION_DOWN.
func (z Zone) PlacementBlockedReason() PlacementBlockedReason {
	if z.Status != GeoStatusUp {
		return PlacementBlockedReasonZoneDown
	}
	if z.RegionStatus != GeoStatusUp {
		return PlacementBlockedReasonRegionDown
	}
	return PlacementBlockedReasonNone
}

// CapacityUnavailableMessage — канонический ОБЕЗЛИЧЕННЫЙ текст public capacity-fail
// (module-geo rule 3; owner-контракт GEO-1-05). geo сам его НЕ эмитит (config-INSERT,
// нет scheduler'а) — но ВЛАДЕЕТ контрактом текста: НИКОГДА не встраивает host-class
// (`hostClasses` остаются только в GetInternal.infra°). Regression-lock:
// NotContains host-class-токен.
func CapacityUnavailableMessage(zoneID string) string {
	return fmt.Sprintf("zone %s has insufficient capacity for the requested configuration", zoneID)
}
