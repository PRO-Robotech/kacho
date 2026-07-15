// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protoconv_test

import (
	"testing"
	"time"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// TestRegion_TruncatesToSecond — created_at усекается до секунд (единый формат
// Kachō), остальные поля переносятся 1-в-1.
func TestRegion_TruncatesToSecond(t *testing.T) {
	created := time.Date(2026, 7, 5, 12, 30, 45, 987654321, time.UTC)
	got := protoconv.Region(&domain.Region{ID: "region-1", Name: "Region One", CreatedAt: created})

	if got.GetId() != "region-1" || got.GetName() != "Region One" {
		t.Fatalf("field mismatch: %+v", got)
	}
	if !got.GetCreatedAt().AsTime().Equal(created.Truncate(time.Second)) {
		t.Errorf("created_at not truncated to second: got %v", got.GetCreatedAt().AsTime())
	}
	if got.GetCreatedAt().AsTime().Nanosecond() != 0 {
		t.Errorf("sub-second component leaked: %d ns", got.GetCreatedAt().AsTime().Nanosecond())
	}
}

// TestZone_FieldsAndStatus — все поля Zone (включая status enum) проецируются,
// created_at усекается до секунд.
func TestZone_FieldsAndStatus(t *testing.T) {
	created := time.Date(2026, 7, 5, 12, 30, 45, 500000000, time.UTC)
	got := protoconv.Zone(&domain.Zone{
		ID: "region-1-a", RegionID: "region-1", Name: "Zone A",
		Status: domain.ZoneStatusUp, CreatedAt: created,
	})

	if got.GetId() != "region-1-a" || got.GetRegionId() != "region-1" || got.GetName() != "Zone A" {
		t.Fatalf("field mismatch: %+v", got)
	}
	if got.GetStatus() != geov1.Zone_UP {
		t.Errorf("status mismatch: got %v want UP", got.GetStatus())
	}
	if got.GetCreatedAt().AsTime().Nanosecond() != 0 {
		t.Errorf("sub-second component leaked: %d ns", got.GetCreatedAt().AsTime().Nanosecond())
	}
}
