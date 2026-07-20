// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestGetMalformedID — STOR-1-21: malformed img-id первым стейтментом → sync
// InvalidArgument "invalid image id '<X>'"; repo не вызывается.
func TestGetMalformedID(t *testing.T) {
	reader := &portmock.ImageReader{
		GetFunc: func(context.Context, string) (*domain.Image, error) {
			t.Fatal("reader.Get must not be called on malformed id")
			return nil, nil
		},
	}
	uc := image.New(reader, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, err := uc.Get(context.Background(), "not-an-img-id")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Get malformed code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "invalid image id 'not-an-img-id'" {
		t.Fatalf("Get malformed message = %q", got)
	}
}

// TestListRequiresProjectID — публичный List без projectId → sync InvalidArgument
// "projectId is required" (in-service backstop к gateway scope_extractor; audit-list-filter).
func TestListRequiresProjectID(t *testing.T) {
	reader := &portmock.ImageReader{
		ListFunc: func(context.Context, image.Pagination) ([]*domain.Image, string, error) {
			t.Fatal("reader.List must not be called when projectId is empty")
			return nil, "", nil
		},
	}
	uc := image.New(reader, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, _, err := uc.List(context.Background(), image.Pagination{PageSize: 50})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("List empty projectId code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "projectId is required" {
		t.Fatalf("List empty projectId message = %q", got)
	}
}

// TestListValidatePagination — STOR-1-32: pageSize>1000 → InvalidArgument (validate.PageSize,
// отвергается не clamp'ится) — ДО repo (reader.List не зовётся). Регрессия list-pagination.
func TestListValidatePagination(t *testing.T) {
	reader := &portmock.ImageReader{
		ListFunc: func(context.Context, image.Pagination) ([]*domain.Image, string, error) {
			t.Fatal("reader.List must not be called when page_size is invalid (validate before repo)")
			return nil, "", nil
		},
	}
	uc := image.New(reader, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, _, err := uc.List(context.Background(), image.Pagination{ProjectID: "prj-1", PageSize: 1001})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("List pageSize=1001 code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestCreateSourceExactlyOne — STOR-1-24 / F12: source oneof exactly-one. Оба →
// InvalidArgument (spoken-exclusion); ни одного → InvalidArgument "Image source is
// required". Sync-reject ДО peer-вызовов (geo/iam mocks с nil-func паникнули бы).
func TestCreateSourceExactlyOne(t *testing.T) {
	uc := image.New(&portmock.ImageReader{}, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)

	// оба источника → conflict.
	_, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "both",
		SourceSnapshot: "snp00000000000000000", SourceVolume: "vol00000000000000000",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("both sources code = %v, want InvalidArgument", status.Code(err))
	}

	// ни одного → required.
	_, err = uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "none",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no source code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "Image source is required" {
		t.Fatalf("no source message = %q, want 'Image source is required'", got)
	}
}

// TestCreatePeerValidatesRegion — STOR-1-20: Create валидирует region_id через GeoClient
// на request-path (cross-domain ref, fail-closed).
func TestCreatePeerValidatesRegion(t *testing.T) {
	sentinel := errors.New("geo unavailable")
	geo := &portmock.PeerClient{
		EnsureRegionFunc: func(_ context.Context, regionID string) error {
			if regionID != "ru-central1" {
				t.Fatalf("geo got region %q", regionID)
			}
			return sentinel
		},
	}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	uc := image.New(&portmock.ImageReader{}, &portmock.ImageWriter{}, geo, iam, nil, nil)
	_, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "img-a", SourceSnapshot: "snp00000000000000000",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Create err = %v, want geo sentinel", err)
	}
}

// TestCreatePeerValidatesProjectUnavailable — STOR-1-29: iam недоступен → Create
// fail-closed UNAVAILABLE (ресурс с непроверенным владельцем не создаётся).
func TestCreatePeerValidatesProjectUnavailable(t *testing.T) {
	geo := &portmock.PeerClient{EnsureRegionFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			return status.Error(codes.Unavailable, "iam project validation unavailable")
		},
	}
	uc := image.New(&portmock.ImageReader{}, &portmock.ImageWriter{}, geo, iam, nil, serviceerr.ToStatus)
	_, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-ghost", RegionID: "ru-central1", Name: "img-a", SourceSnapshot: "snp00000000000000000",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Create iam-down code = %v, want Unavailable", status.Code(err))
	}
}

// TestUpdateImmutableField — STOR-1-22: immutable-поле в маске → sync InvalidArgument
// "<field> is immutable after Image.Create" (immutable-switch ДО UpdateMask).
func TestUpdateImmutableField(t *testing.T) {
	uc := image.New(&portmock.ImageReader{}, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	for _, f := range []string{"region_id", "source_snapshot_id", "source_volume_id", "format"} {
		_, err := uc.Update(context.Background(), "img00000000000000000", []string{f}, "", "", nil)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Update mask=%s code=%v, want InvalidArgument", f, status.Code(err))
		}
		want := f + " is immutable after Image.Create"
		if got := status.Convert(err).Message(); got != want {
			t.Fatalf("Update mask=%s message=%q, want %q", f, got, want)
		}
	}
}

// TestCreateLROInsertsAndMarksDone — happy async: sync-фаза создаёт LRO-строку, worker
// вызывает writer.Insert, маршалит Image в Operation.response, done=true.
func TestCreateLROInsertsAndMarksDone(t *testing.T) {
	writer := &portmock.ImageWriter{
		InsertFunc: func(_ context.Context, i *domain.Image) (*domain.Image, error) {
			out := *i // id уже присвоен use-case'ом (ids.NewID) до Run
			out.Status = domain.ImageStatusReady
			return &out, nil
		},
	}
	geo := &portmock.PeerClient{EnsureRegionFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := image.New(&portmock.ImageReader{}, writer, geo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", Name: "img-a", RegionID: "ru-central1", SourceSnapshot: "snp00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	if op.Metadata == nil {
		t.Fatalf("op metadata nil, want CreateImageMetadata with imageId")
	}
	// STOR-1-20 инвариант: imageId присутствует в metadata СИНХРОННО (до done).
	var meta storagev1.CreateImageMetadata
	if uerr := op.Metadata.UnmarshalTo(&meta); uerr != nil {
		t.Fatalf("unmarshal metadata: %v", uerr)
	}
	if meta.GetImageId() == "" || meta.GetImageId()[:3] != domain.PrefixImage {
		t.Fatalf("metadata imageId = %q, want non-empty img- id present before done", meta.GetImageId())
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success terminal", done.Error)
	}
	var got storagev1.Image
	if uerr := done.Response.UnmarshalTo(&got); uerr != nil {
		t.Fatalf("unmarshal response: %v", uerr)
	}
	if got.GetId() != meta.GetImageId() {
		t.Fatalf("response image id = %q, want metadata id %q", got.GetId(), meta.GetImageId())
	}
	if got.GetStatus() != storagev1.Image_READY {
		t.Fatalf("response status = %v, want READY", got.GetStatus())
	}
	if got.GetPlacementType() != storagev1.Image_REGIONAL {
		t.Fatalf("response placement = %v, want REGIONAL", got.GetPlacementType())
	}
}

// TestCreateLROWriterErrorMarksError — error-путь: writer.Insert возвращает
// FailedPrecondition-sentinel → worker пишет его в Operation.error, done=true.
func TestCreateLROWriterErrorMarksError(t *testing.T) {
	sentinel := fmt.Errorf("%w: Snapshot snp00000000000000000 not found", ports.ErrFailedPrecondition)
	writer := &portmock.ImageWriter{
		InsertFunc: func(context.Context, *domain.Image) (*domain.Image, error) { return nil, sentinel },
	}
	geo := &portmock.PeerClient{EnsureRegionFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := image.New(&portmock.ImageReader{}, writer, geo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", SourceSnapshot: "snp00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Response != nil {
		t.Fatalf("op response = %v, want error terminal", done.Response)
	}
	if done.Error == nil || done.Error.GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op error = %v, want FailedPrecondition", done.Error)
	}
}
