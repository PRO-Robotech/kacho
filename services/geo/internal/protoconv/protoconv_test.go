// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protoconv_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// fieldNames возвращает set proto-имён полей message-дескриптора.
func fieldNames(m proto.Message) map[string]struct{} {
	fields := m.ProtoReflect().Descriptor().Fields()
	out := make(map[string]struct{}, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		out[string(fields.Get(i).Name())] = struct{}{}
	}
	return out
}

// TestPublicZone_TwoProjection_NoStatusNoInfra — GEO-1-02/33: публичный Zone
// СТРУКТУРНО не несёт status/infra (two-projection через РАЗНЫЕ messages: поля
// физически отсутствуют в дескрипторе, сильнее runtime-omit).
func TestPublicZone_TwoProjection_NoStatusNoInfra(t *testing.T) {
	names := fieldNames(&geov1.Zone{})
	// Обязаны присутствовать (lean public-проекция).
	for _, must := range []string{"id", "region_id", "name", "open_for_placement", "placement_blocked_reason", "created_at"} {
		if _, ok := names[must]; !ok {
			t.Errorf("public Zone missing required field %q", must)
		}
	}
	// Обязаны ОТСУТСТВОВАТЬ (admin-plane / infra / ось).
	for _, absent := range []string{"status", "infra", "numeric_infra_id", "host_classes", "failure_domain_count", "underlay_anchor", "capacity_hint", "placement_type", "placement_scope"} {
		if _, ok := names[absent]; ok {
			t.Errorf("public Zone MUST NOT carry field %q (two-projection leak)", absent)
		}
	}
}

// TestPublicRegion_TwoProjection_NoStatusNoInfra — GEO-1-03/33.
func TestPublicRegion_TwoProjection_NoStatusNoInfra(t *testing.T) {
	names := fieldNames(&geov1.Region{})
	for _, must := range []string{"id", "name", "country_code", "open_for_placement", "open_zone_count_hint", "created_at"} {
		if _, ok := names[must]; !ok {
			t.Errorf("public Region missing required field %q", must)
		}
	}
	for _, absent := range []string{"status", "infra", "numeric_infra_id", "capacity_hint", "placement_blocked_reason", "placement_type", "placement_scope"} {
		if _, ok := names[absent]; ok {
			t.Errorf("public Region MUST NOT carry field %q (two-projection leak)", absent)
		}
	}
}

// TestZone_Derived — public Zone несёт derived openForPlacement°/placementBlockedReason°;
// created_at усечён до секунд.
func TestZone_Derived(t *testing.T) {
	created := time.Date(2026, 7, 5, 12, 30, 45, 500000000, time.UTC)
	got := protoconv.Zone(&domain.Zone{
		ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A",
		Status: domain.GeoStatusUp, RegionStatus: domain.GeoStatusUp, CreatedAt: created,
	})
	if got.GetId() != "ru-central1-a" || got.GetRegionId() != "ru-central1" || got.GetName() != "Zone A" {
		t.Fatalf("field mismatch: %+v", got)
	}
	if !got.GetOpenForPlacement() {
		t.Error("both UP → openForPlacement must be true")
	}
	if got.GetPlacementBlockedReason() != geov1.PlacementBlockedReason_NONE {
		t.Errorf("reason = %v, want NONE", got.GetPlacementBlockedReason())
	}
	if got.GetCreatedAt().AsTime().Nanosecond() != 0 {
		t.Errorf("sub-second leaked: %d ns", got.GetCreatedAt().AsTime().Nanosecond())
	}
	// zone DOWN → reason ZONE_DOWN.
	down := protoconv.Zone(&domain.Zone{ID: "ru-central1-d", RegionID: "ru-central1", Status: domain.GeoStatusDown, RegionStatus: domain.GeoStatusUp})
	if down.GetOpenForPlacement() || down.GetPlacementBlockedReason() != geov1.PlacementBlockedReason_ZONE_DOWN {
		t.Errorf("zone DOWN → openForPlacement=false + ZONE_DOWN, got open=%v reason=%v", down.GetOpenForPlacement(), down.GetPlacementBlockedReason())
	}
}

// TestRegion_Derived — public Region: country_code + derived openForPlacement° +
// openZoneCountHint° rollup; created_at усечён.
func TestRegion_Derived(t *testing.T) {
	created := time.Date(2026, 7, 5, 12, 30, 45, 987654321, time.UTC)
	got := protoconv.Region(&domain.Region{ID: "ru-central1", Name: "RU Central 1", CountryCode: "RU", Status: domain.GeoStatusUp, OpenZoneCount: 2, CreatedAt: created})
	if got.GetCountryCode() != "RU" || !got.GetOpenForPlacement() || got.GetOpenZoneCountHint() != 2 {
		t.Fatalf("region projection mismatch: %+v", got)
	}
	if got.GetCreatedAt().AsTime().Nanosecond() != 0 {
		t.Errorf("sub-second leaked: %d ns", got.GetCreatedAt().AsTime().Nanosecond())
	}
}

// TestInternalProjections_CarryStatusInfra — GEO-1-01/04: Internal-проекции несут
// status + полный infra° (readable-плоскость).
func TestInternalProjections_CarryStatusInfra(t *testing.T) {
	iz := protoconv.InternalZone(&domain.Zone{
		ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A", Status: domain.GeoStatusUp,
		Infra: domain.ZoneInfra{NumericInfraID: 10402, HostClasses: []string{"std-v3", "mem-v2"}, FailureDomainCount: 3, UnderlayAnchor: "fd00:ru1a::/48", CapacityHint: "AMPLE"},
	})
	if iz.GetStatus() != geov1.GeoStatus_UP || iz.GetInfra().GetNumericInfraId() != 10402 ||
		len(iz.GetInfra().GetHostClasses()) != 2 || iz.GetInfra().GetCapacityHint() != "AMPLE" {
		t.Fatalf("InternalZone projection mismatch: %+v", iz)
	}
	ir := protoconv.InternalRegion(&domain.Region{ID: "ru-central1", Name: "RU Central 1", CountryCode: "RU", Status: domain.GeoStatusUp, Infra: domain.RegionInfra{NumericInfraID: 900}})
	if ir.GetStatus() != geov1.GeoStatus_UP || ir.GetInfra().GetNumericInfraId() != 900 {
		t.Fatalf("InternalRegion projection mismatch: %+v", ir)
	}
}
