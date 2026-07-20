// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package zone_test

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// openInsert эмулирует repo.Insert: echo зоны + status родит-региона (JOIN) = UP.
func openInsert(_ context.Context, z *domain.Zone) (*domain.Zone, error) {
	z.RegionStatus = domain.GeoStatusUp
	return z, nil
}

func newUC(mock *repomock.ZoneRepo) (*zone.UseCase, *repomock.OpsRepo) {
	ops := repomock.NewOpsRepo()
	return zone.New(mock, mock, ops, serviceerr.ToStatus), ops
}

// TestCreate_couplingViolation_invalidArg — GEO-1-29: zone.id не префиксован своим
// regionId → синхронный InvalidArgument первым стейтментом (Insert не вызывается).
func TestCreate_couplingViolation_invalidArg(t *testing.T) {
	mock := &repomock.ZoneRepo{InsertFunc: func(context.Context, *domain.Zone) (*domain.Zone, error) {
		t.Fatal("Insert must not run on a coupling violation")
		return nil, nil
	}}
	uc, _ := newUC(mock)
	_, err := uc.Create(context.Background(), zone.CreateInput{ID: "ru-central1-a", RegionID: "eu-west1", Name: "Zone A"})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
	if msg := serviceerr.ToStatus(err).Error(); !strings.Contains(msg, "zone id 'ru-central1-a' must be prefixed by its regionId 'eu-west1'") {
		t.Fatalf("coupling text = %q", msg)
	}
}

// TestCreate_strictStartsWith_reject — GEO-1-30: 'ru-central10-a' под 'ru-central1' → REJECT.
func TestCreate_strictStartsWith_reject(t *testing.T) {
	uc, _ := newUC(&repomock.ZoneRepo{})
	_, err := uc.Create(context.Background(), zone.CreateInput{ID: "ru-central10-a", RegionID: "ru-central1", Name: "Zone A"})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("strict startsWith failed: err = %v, want ErrInvalidArg", err)
	}
}

// TestCreate_freshDOWN_warns — GEO-1-12: omit status → DOWN; done:true; response
// openForPlacement=false; metadata.warnings[0] дословно; response БЕЗ warnings.
func TestCreate_freshDOWN_warns(t *testing.T) {
	mock := &repomock.ZoneRepo{InsertFunc: openInsert}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), zone.CreateInput{ID: "ru-central1-d", RegionID: "ru-central1", Name: "RU Central 1 — Zone D"})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if !op.Done || op.Error != nil {
		t.Fatalf("op = %+v", op)
	}
	msg, _ := op.Response.UnmarshalNew()
	if msg.(*geov1.Zone).GetOpenForPlacement() {
		t.Error("fresh zone without status must be DOWN → openForPlacement=false")
	}
	meta, err := operations.MetadataFor[*geov1.CreateZoneMetadata](op)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	want := "zone ru-central1-d created but CLOSED to placement (status DOWN); no tenant can place here — Internal Update status=UP to open"
	if len(meta.GetWarnings()) != 1 || meta.GetWarnings()[0] != want {
		t.Fatalf("warnings = %v, want [%q]", meta.GetWarnings(), want)
	}
}

// TestCreate_openUP_noWarning — GEO-1-01/02: status=UP под UP-регионом → open, warnings пусты.
func TestCreate_openUP_noWarning(t *testing.T) {
	mock := &repomock.ZoneRepo{InsertFunc: openInsert}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), zone.CreateInput{ID: "ru-central1-a", RegionID: "ru-central1", Name: "Zone A", Status: domain.GeoStatusUp})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	msg, _ := op.Response.UnmarshalNew()
	if !msg.(*geov1.Zone).GetOpenForPlacement() {
		t.Error("zone UP under region UP → openForPlacement=true")
	}
	meta, _ := operations.MetadataFor[*geov1.CreateZoneMetadata](op)
	if len(meta.GetWarnings()) != 0 {
		t.Fatalf("open zone must carry empty warnings, got %v", meta.GetWarnings())
	}
}

// TestCreate_emptyName_invalidArg — GEO-1-38: пустой name → InvalidArgument.
func TestCreate_emptyName_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.ZoneRepo{})
	_, err := uc.Create(context.Background(), zone.CreateInput{ID: "ru-central1-a", RegionID: "ru-central1"})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
}

// TestCreate_absentRegion_FKopError — GEO-1-34 [PHASE-0-GATED]: несуществующий
// region_id остаётся FK-FAILED_PRECONDITION в op.error (НЕ pre-flight NOT_FOUND).
func TestCreate_absentRegion_FKopError(t *testing.T) {
	mock := &repomock.ZoneRepo{InsertFunc: func(context.Context, *domain.Zone) (*domain.Zone, error) {
		return nil, geoerrors.ErrFailedPrecondition // repo маппит FK 23503 → sentinel
	}}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), zone.CreateInput{ID: "eu-west1-a", RegionID: "eu-west1", Name: "Zone A"})
	if err != nil {
		t.Fatalf("Create accept err = %v (FK must land in op.error)", err)
	}
	if op.Error == nil || op.Error.GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op.Error = %v, want FAILED_PRECONDITION (gated FK fallback)", op.Error)
	}
}

// TestUpdate_immutableRegionId_invalidArg — GEO-1-32: mask=["regionId"] → синхронный
// InvalidArgument "regionId is immutable after Zone.Create" ДО UpdateMask.
func TestUpdate_immutableRegionId_invalidArg(t *testing.T) {
	mock := &repomock.ZoneRepo{UpdateFunc: func(context.Context, string, zone.UpdateParams) (*domain.Zone, error) {
		t.Fatal("writer.Update must not run for immutable regionId")
		return nil, nil
	}}
	uc, _ := newUC(mock)
	_, err := uc.Update(context.Background(), zone.UpdateInput{ID: "ru-central1-a", Mask: []string{"regionId"}})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
	if msg := serviceerr.ToStatus(err).Error(); !strings.Contains(msg, "regionId is immutable after Zone.Create") {
		t.Fatalf("immutable text = %q", msg)
	}
}

// TestUpdate_infraSubset_applied — GEO-1-04: mask=["infra.capacityHint","infra.hostClasses"]
// применяет ровно эти поля, остальные infra/поля НЕ трогаются.
func TestUpdate_infraSubset_applied(t *testing.T) {
	var got zone.UpdateParams
	mock := &repomock.ZoneRepo{UpdateFunc: func(_ context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
		got = p
		return &domain.Zone{ID: id, RegionID: "ru-central1", Status: domain.GeoStatusUp, RegionStatus: domain.GeoStatusUp}, nil
	}}
	uc, _ := newUC(mock)
	_, err := uc.Update(context.Background(), zone.UpdateInput{
		ID:    "ru-central1-a",
		Mask:  []string{"infra.capacityHint", "infra.hostClasses"},
		Infra: domain.ZoneInfra{CapacityHint: "CONSTRAINED", HostClasses: []string{"std-v3"}},
	})
	if err != nil {
		t.Fatalf("Update err = %v", err)
	}
	if got.CapacityHint == nil || *got.CapacityHint != "CONSTRAINED" {
		t.Fatalf("capacityHint param = %v, want &CONSTRAINED", got.CapacityHint)
	}
	if got.HostClasses == nil || len(*got.HostClasses) != 1 || (*got.HostClasses)[0] != "std-v3" {
		t.Fatalf("hostClasses param = %v", got.HostClasses)
	}
	if got.FailureDomainCount != nil || got.UnderlayAnchor != nil || got.Status != nil || got.Name != nil {
		t.Fatalf("unmasked fields leaked into params: %+v", got)
	}
}

// TestUpdate_immutableNumericInfraId_invalidArg — GEO-1-04: numericInfraId в mask → InvalidArgument.
func TestUpdate_immutableNumericInfraId_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.ZoneRepo{})
	_, err := uc.Update(context.Background(), zone.UpdateInput{ID: "ru-central1-a", Mask: []string{"infra.numericInfraId"}})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
	if msg := serviceerr.ToStatus(err).Error(); !strings.Contains(msg, "numericInfraId is immutable after Zone.Create") {
		t.Fatalf("immutable text = %q", msg)
	}
}

// TestUpdate_status_applied — GEO-1-15: mask=["status"], status=UP → param Status.
func TestUpdate_status_applied(t *testing.T) {
	var got zone.UpdateParams
	mock := &repomock.ZoneRepo{UpdateFunc: func(_ context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
		got = p
		return &domain.Zone{ID: id, RegionID: "ru-central1", Status: domain.GeoStatusUp, RegionStatus: domain.GeoStatusUp}, nil
	}}
	uc, _ := newUC(mock)
	if _, err := uc.Update(context.Background(), zone.UpdateInput{ID: "ru-central1-d", Mask: []string{"status"}, Status: domain.GeoStatusUp}); err != nil {
		t.Fatalf("Update err = %v", err)
	}
	if got.Status == nil || *got.Status != domain.GeoStatusUp {
		t.Fatalf("status param = %v, want &UP", got.Status)
	}
}

// TestGetInternal_malformedID_invalidArg — parity с Get.
func TestGetInternal_malformedID_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.ZoneRepo{})
	if _, err := uc.GetInternal(context.Background(), "ZZ!"); !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("GetInternal('ZZ!') err = %v, want ErrInvalidArg", err)
	}
}

func TestList_garbagePageSize_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.ZoneRepo{})
	if _, _, err := uc.List(context.Background(), zone.Pagination{PageSize: 1_000_000}); err == nil {
		t.Fatal("List(page_size too large) err = nil, want validation error")
	}
}
