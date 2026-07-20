// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package region_test

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

func echoInsert(_ context.Context, r *domain.Region) (*domain.Region, error) { return r, nil }

func newUC(mock *repomock.RegionRepo) (*region.UseCase, *repomock.OpsRepo) {
	ops := repomock.NewOpsRepo()
	return region.New(mock, mock, ops, serviceerr.ToStatus), ops
}

// TestGet_malformedID_invalidArg — не-slug id → синхронный InvalidArgument первым
// стейтментом, без round-trip в reader.Get.
func TestGet_malformedID_invalidArg(t *testing.T) {
	mock := &repomock.RegionRepo{GetFunc: func(context.Context, string) (*domain.Region, error) {
		t.Fatal("reader.Get must not be called for a malformed id")
		return nil, nil
	}}
	uc, _ := newUC(mock)
	if _, err := uc.Get(context.Background(), "ZZ!"); !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("Get('ZZ!') err = %v, want ErrInvalidArg", err)
	}
}

// TestCreate_freshDOWN_warnsLoud — GEO-1-13: omit status → DOWN (fail-safe, НЕ UP);
// Operation done:true немедленно; response.openForPlacement=false; metadata.warnings[0]
// дословно; response НЕ несёт warnings (geo-owned metadata).
func TestCreate_freshDOWN_warnsLoud(t *testing.T) {
	mock := &repomock.RegionRepo{InsertFunc: echoInsert}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), region.CreateInput{ID: "eu-west1", Name: "EU West 1", CountryCode: "NL"})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if !op.Done {
		t.Fatal("catalog Create must return Operation{done:true} synchronously")
	}
	if op.Error != nil {
		t.Fatalf("op.Error = %v", op.Error)
	}
	msg, _ := op.Response.UnmarshalNew()
	r := msg.(*geov1.Region)
	if r.GetOpenForPlacement() {
		t.Error("fresh region without status must be DOWN → openForPlacement=false")
	}
	meta, err := operations.MetadataFor[*geov1.CreateRegionMetadata](op)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	want := "region eu-west1 created but CLOSED to placement (status DOWN); no tenant can place here — Internal Update status=UP to open"
	if len(meta.GetWarnings()) != 1 || meta.GetWarnings()[0] != want {
		t.Fatalf("warnings = %v, want [%q]", meta.GetWarnings(), want)
	}
}

// TestCreate_explicitUP_open_noWarning — GEO-1-14/16: status=UP → open, warnings пусты.
func TestCreate_explicitUP_open_noWarning(t *testing.T) {
	mock := &repomock.RegionRepo{InsertFunc: echoInsert}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), region.CreateInput{ID: "ru-central1", Name: "RU Central 1", CountryCode: "RU", Status: domain.GeoStatusUp})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	msg, _ := op.Response.UnmarshalNew()
	if !msg.(*geov1.Region).GetOpenForPlacement() {
		t.Error("status=UP → openForPlacement=true")
	}
	meta, _ := operations.MetadataFor[*geov1.CreateRegionMetadata](op)
	if len(meta.GetWarnings()) != 0 {
		t.Fatalf("open region must carry empty warnings, got %v", meta.GetWarnings())
	}
}

// TestCreate_emptyName_invalidArg — GEO-1-38: пустой name → синхронный InvalidArgument.
func TestCreate_emptyName_invalidArg(t *testing.T) {
	mock := &repomock.RegionRepo{InsertFunc: func(context.Context, *domain.Region) (*domain.Region, error) {
		t.Fatal("Insert must not run when name is empty")
		return nil, nil
	}}
	uc, _ := newUC(mock)
	_, err := uc.Create(context.Background(), region.CreateInput{ID: "ru-central1", CountryCode: "RU"})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
	if got := serviceerr.ToStatus(err); grpcstatus.Code(got) != codes.InvalidArgument || got.Error() == "" {
		t.Fatalf("mapped = %v", got)
	}
}

// TestCreate_invalidCountryCode_invalidArg — GEO-1-39: countryCode "RUS" → InvalidArgument.
func TestCreate_invalidCountryCode_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.RegionRepo{})
	_, err := uc.Create(context.Background(), region.CreateInput{ID: "ru-central1", Name: "RU Central 1", CountryCode: "RUS"})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
}

// TestCreate_duplicate_opError_alreadyExists — GEO-1-36: repo 23505 → op.error ALREADY_EXISTS.
func TestCreate_duplicate_opError_alreadyExists(t *testing.T) {
	mock := &repomock.RegionRepo{InsertFunc: func(context.Context, *domain.Region) (*domain.Region, error) {
		return nil, geoerrors.ErrAlreadyExists
	}}
	uc, _ := newUC(mock)
	op, err := uc.Create(context.Background(), region.CreateInput{ID: "ru-central2", Name: "RU Central 1", Status: domain.GeoStatusUp})
	if err != nil {
		t.Fatalf("Create accept err = %v (dup must land in op.error, not sync)", err)
	}
	if op.Error == nil || op.Error.GetCode() != int32(codes.AlreadyExists) {
		t.Fatalf("op.Error = %v, want ALREADY_EXISTS", op.Error)
	}
}

// TestUpdate_immutableNumericInfraId_invalidArg — GEO-1-04-parity: infra.numericInfraId
// в mask → синхронный InvalidArgument ДО UpdateMask, конвенционный текст.
func TestUpdate_immutableNumericInfraId_invalidArg(t *testing.T) {
	mock := &repomock.RegionRepo{UpdateFunc: func(context.Context, string, region.UpdateParams) (*domain.Region, error) {
		t.Fatal("writer.Update must not run for an immutable-field reject")
		return nil, nil
	}}
	uc, _ := newUC(mock)
	_, err := uc.Update(context.Background(), region.UpdateInput{ID: "ru-central1", Mask: []string{"infra.numericInfraId"}})
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("err = %v, want ErrInvalidArg", err)
	}
	if msg := serviceerr.ToStatus(err).Error(); !strings.Contains(msg, "numericInfraId is immutable after Region.Create") {
		t.Fatalf("immutable text = %q", msg)
	}
}

// TestUpdate_unknownMaskField_invalidArg — неизвестное поле в mask → InvalidArgument.
func TestUpdate_unknownMaskField_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.RegionRepo{})
	_, err := uc.Update(context.Background(), region.UpdateInput{ID: "ru-central1", Mask: []string{"bogus"}})
	if err == nil {
		t.Fatal("unknown mask field must be rejected")
	}
}

// TestUpdate_countryCode_applied — GEO-1-39: mask=["countryCode"] с валидным кодом
// применяется; невалидный на Update тоже отвергается.
func TestUpdate_countryCode_applied(t *testing.T) {
	var got *string
	mock := &repomock.RegionRepo{UpdateFunc: func(_ context.Context, id string, p region.UpdateParams) (*domain.Region, error) {
		got = p.CountryCode
		return &domain.Region{ID: id, CountryCode: "DE", Status: domain.GeoStatusUp}, nil
	}}
	uc, _ := newUC(mock)
	if _, err := uc.Update(context.Background(), region.UpdateInput{ID: "ru-central1", Mask: []string{"countryCode"}, CountryCode: "DE"}); err != nil {
		t.Fatalf("Update err = %v", err)
	}
	if got == nil || *got != "DE" {
		t.Fatalf("countryCode param = %v, want &DE", got)
	}
	// невалидный countryCode на Update → InvalidArgument.
	if _, err := uc.Update(context.Background(), region.UpdateInput{ID: "ru-central1", Mask: []string{"countryCode"}, CountryCode: "RUS"}); !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("Update countryCode=RUS err = %v, want ErrInvalidArg", err)
	}
}

// TestDelete_FKRestrict_notEmptyText — GEO-1-18: FK RESTRICT → op.error
// FAILED_PRECONDITION "region <id> is not empty".
func TestDelete_FKRestrict_notEmptyText(t *testing.T) {
	mock := &repomock.RegionRepo{DeleteFunc: func(context.Context, string) error { return geoerrors.ErrFailedPrecondition }}
	uc, _ := newUC(mock)
	op, err := uc.Delete(context.Background(), "ru-central1")
	if err != nil {
		t.Fatalf("Delete accept err = %v", err)
	}
	if op.Error == nil || op.Error.GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op.Error = %v, want FAILED_PRECONDITION", op.Error)
	}
	if op.Error.GetMessage() != "region ru-central1 is not empty" {
		t.Fatalf("delete-nonempty text = %q", op.Error.GetMessage())
	}
}

// TestDelete_happy_done — успех → Operation done:true, response=Empty.
func TestDelete_happy_done(t *testing.T) {
	called := false
	mock := &repomock.RegionRepo{DeleteFunc: func(context.Context, string) error { called = true; return nil }}
	uc, _ := newUC(mock)
	op, err := uc.Delete(context.Background(), "ru-central1")
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if !op.Done || op.Error != nil || !called {
		t.Fatalf("delete op = %+v, called=%v", op, called)
	}
}

// TestGetInternal_malformedID_invalidArg — parity с Get.
func TestGetInternal_malformedID_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.RegionRepo{})
	if _, err := uc.GetInternal(context.Background(), "ZZ!"); !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("GetInternal('ZZ!') err = %v, want ErrInvalidArg", err)
	}
}

func TestList_garbagePageSize_invalidArg(t *testing.T) {
	uc, _ := newUC(&repomock.RegionRepo{})
	if _, _, err := uc.List(context.Background(), region.Pagination{PageSize: 1_000_000}); err == nil {
		t.Fatal("List(page_size too large) err = nil, want validation error")
	}
}
