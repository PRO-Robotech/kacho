// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/dberr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// awaitOpFromHandler поллит OperationHandler.Get до done=true (тот же контракт,
// что у клиента после async-мутации).
func awaitOpFromHandler(t *testing.T, oh *handler.OperationHandler, opID string) *operationpb.Operation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		op, err := oh.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: opID})
		if err == nil && op.GetDone() {
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not finish", opID)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRegionHandler_Get_notFound_mapsToNotFound(t *testing.T) {
	mock := &repomock.RegionRepo{
		GetFunc: func(_ context.Context, id string) (*domain.Region, error) {
			return nil, dberr.Wrap(stderrors.New("x"), "Region", id) // путь ErrInternal
		},
	}
	uc := region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewRegionHandler(uc)
	_, err := h.Get(context.Background(), &geov1.GetRegionRequest{RegionId: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("want INTERNAL for raw error, got %v", err)
	}
}

func TestRegionHandler_Get_emptyID_invalidArgument(t *testing.T) {
	h := handler.NewRegionHandler(region.New(&repomock.RegionRepo{}, &repomock.RegionRepo{}, repomock.NewOpsRepo(), serviceerr.ToStatus))
	_, err := h.Get(context.Background(), &geov1.GetRegionRequest{RegionId: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT for empty id, got %v", err)
	}
}

func TestRegionHandler_Get_happy(t *testing.T) {
	mock := &repomock.RegionRepo{
		GetFunc: func(_ context.Context, id string) (*domain.Region, error) {
			return &domain.Region{ID: id, Name: "Region 1"}, nil
		},
	}
	uc := region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewRegionHandler(uc)
	resp, err := h.Get(context.Background(), &geov1.GetRegionRequest{RegionId: "region-1"})
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if resp.GetId() != "region-1" || resp.GetName() != "Region 1" {
		t.Fatalf("Get resp = %+v", resp)
	}
}

func TestZoneHandler_Get_happy_statusMapped(t *testing.T) {
	mock := &repomock.ZoneRepo{
		GetFunc: func(_ context.Context, id string) (*domain.Zone, error) {
			return &domain.Zone{ID: id, RegionID: "region-1", Status: domain.ZoneStatusUp}, nil
		},
	}
	uc := zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewZoneHandler(uc)
	resp, err := h.Get(context.Background(), &geov1.GetZoneRequest{ZoneId: "region-1-a"})
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if resp.GetRegionId() != "region-1" || resp.GetStatus() != geov1.Zone_UP {
		t.Fatalf("Get resp = %+v", resp)
	}
}

// TestZoneHandler_Get_notFound_mapsToNotFound — ZoneRepo.Get отдаёт
// geoerrors.ErrNotFound → публичный ZoneService.Get маппит в codes.NotFound
// (parity с region Get-notFound; Zone.Get — отдельный код-путь).
func TestZoneHandler_Get_notFound_mapsToNotFound(t *testing.T) {
	mock := &repomock.ZoneRepo{
		GetFunc: func(_ context.Context, _ string) (*domain.Zone, error) {
			return nil, geoerrors.ErrNotFound
		},
	}
	uc := zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewZoneHandler(uc)
	_, err := h.Get(context.Background(), &geov1.GetZoneRequest{ZoneId: "no-such-zone"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NOT_FOUND for absent zone, got %v", err)
	}
}

// TestInternalRegionHandler_Delete_emptyID_syncInvalidArgument — пустой id
// отвергается синхронно (InvalidArgument), Operation не создаётся.
func TestInternalRegionHandler_Delete_emptyID_syncInvalidArgument(t *testing.T) {
	uc := region.New(&repomock.RegionRepo{}, &repomock.RegionRepo{}, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewInternalRegionHandler(uc)
	_, err := h.Delete(context.Background(), &geov1.DeleteRegionRequest{RegionId: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT for empty id, got %v", err)
	}
}

// TestInternalRegionHandler_Delete_failedPrecondition — FK RESTRICT (есть зоны)
// доезжает как Operation.error FAILED_PRECONDITION (async).
func TestInternalRegionHandler_Delete_failedPrecondition(t *testing.T) {
	ops := repomock.NewOpsRepo()
	mock := &repomock.RegionRepo{
		DeleteFunc: func(_ context.Context, _ string) error { return geoerrors.ErrFailedPrecondition },
	}
	uc := region.New(mock, mock, ops, serviceerr.ToStatus)
	h := handler.NewInternalRegionHandler(uc)
	oh := handler.NewOperationHandler(ops)
	op, err := h.Delete(context.Background(), &geov1.DeleteRegionRequest{RegionId: "region-1"})
	if err != nil {
		t.Fatalf("Delete accept err = %v", err)
	}
	if op.GetDone() {
		t.Fatalf("op must start done=false")
	}
	done := awaitOpFromHandler(t, oh, op.GetId())
	if done.GetError() == nil || done.GetError().GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op error = %v, want FAILED_PRECONDITION", done.GetError())
	}
}

// TestInternalZoneHandler_Create_happy — Create возвращает Operation; полл до
// done → response=Zone.
func TestInternalZoneHandler_Create_happy(t *testing.T) {
	ops := repomock.NewOpsRepo()
	mock := &repomock.ZoneRepo{
		InsertFunc: func(_ context.Context, z *domain.Zone) (*domain.Zone, error) { return z, nil },
	}
	uc := zone.New(mock, mock, ops, serviceerr.ToStatus)
	h := handler.NewInternalZoneHandler(uc)
	oh := handler.NewOperationHandler(ops)
	op, err := h.Create(context.Background(), &geov1.CreateZoneRequest{
		Id: "region-1-a", RegionId: "region-1", Status: geov1.Zone_UP, Name: "Region 1 A",
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	done := awaitOpFromHandler(t, oh, op.GetId())
	if done.GetError() != nil {
		t.Fatalf("op error = %v", done.GetError())
	}
	msg, err := done.GetResponse().UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	z, ok := msg.(*geov1.Zone)
	if !ok || z.GetId() != "region-1-a" || z.GetStatus() != geov1.Zone_UP {
		t.Fatalf("response = %+v", msg)
	}
}

// TestRegionHandler_List_happy_mapsItemsAndPropagatesToken — публичный
// RegionService.List маппит каждый domain.Region через protoconv и пробрасывает
// NextPageToken из use-case, а page_size/page_token из запроса — в use-case без
// подмены (guard против дропа токена / свопа size↔token в тонком handler-loop'е).
func TestRegionHandler_List_happy_mapsItemsAndPropagatesToken(t *testing.T) {
	var gotPage region.Pagination
	mock := &repomock.RegionRepo{
		ListFunc: func(_ context.Context, p region.Pagination) ([]*domain.Region, string, error) {
			gotPage = p
			return []*domain.Region{
				{ID: "region-1", Name: "Region 1"},
				{ID: "region-2", Name: "Region 2"},
			}, "next-tok", nil
		},
	}
	uc := region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewRegionHandler(uc)
	resp, err := h.List(context.Background(), &geov1.ListRegionsRequest{PageSize: 7, PageToken: "cursor"})
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	if got := len(resp.GetRegions()); got != 2 {
		t.Fatalf("len(regions) = %d, want 2", got)
	}
	if resp.GetRegions()[0].GetId() != "region-1" || resp.GetRegions()[0].GetName() != "Region 1" {
		t.Fatalf("region[0] = %+v", resp.GetRegions()[0])
	}
	if resp.GetNextPageToken() != "next-tok" {
		t.Fatalf("NextPageToken = %q, want next-tok", resp.GetNextPageToken())
	}
	if gotPage.PageSize != 7 || gotPage.PageToken != "cursor" {
		t.Fatalf("pagination passthrough = %+v, want {7 cursor}", gotPage)
	}
}

// TestRegionHandler_List_repoError_mapsToStatus — ошибка use-case/repo (напр.
// malformed page_token → ErrInvalidArg) доезжает через serviceerr.ToStatus как
// InvalidArgument (handler-loop не глотает ошибку).
func TestRegionHandler_List_repoError_mapsToStatus(t *testing.T) {
	mock := &repomock.RegionRepo{
		ListFunc: func(_ context.Context, _ region.Pagination) ([]*domain.Region, string, error) {
			return nil, "", geoerrors.ErrInvalidArg
		},
	}
	uc := region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewRegionHandler(uc)
	_, err := h.List(context.Background(), &geov1.ListRegionsRequest{PageSize: 10})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT for repo ErrInvalidArg, got %v", err)
	}
}

// TestZoneHandler_List_happy_mapsItemsAndPropagatesToken — тот же контракт для
// публичного ZoneService.List (маппинг RegionId/Status через protoconv +
// проброс NextPageToken и page_size/page_token).
func TestZoneHandler_List_happy_mapsItemsAndPropagatesToken(t *testing.T) {
	var gotPage zone.Pagination
	mock := &repomock.ZoneRepo{
		ListFunc: func(_ context.Context, p zone.Pagination) ([]*domain.Zone, string, error) {
			gotPage = p
			return []*domain.Zone{
				{ID: "region-1-a", RegionID: "region-1", Status: domain.ZoneStatusUp, Name: "Region 1 A"},
				{ID: "region-1-b", RegionID: "region-1", Status: domain.ZoneStatusUp, Name: "Region 1 B"},
			}, "z-next", nil
		},
	}
	uc := zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewZoneHandler(uc)
	resp, err := h.List(context.Background(), &geov1.ListZonesRequest{PageSize: 3, PageToken: "z-cursor"})
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	if got := len(resp.GetZones()); got != 2 {
		t.Fatalf("len(zones) = %d, want 2", got)
	}
	if resp.GetZones()[0].GetId() != "region-1-a" || resp.GetZones()[0].GetRegionId() != "region-1" ||
		resp.GetZones()[0].GetStatus() != geov1.Zone_UP {
		t.Fatalf("zone[0] = %+v", resp.GetZones()[0])
	}
	if resp.GetNextPageToken() != "z-next" {
		t.Fatalf("NextPageToken = %q, want z-next", resp.GetNextPageToken())
	}
	if gotPage.PageSize != 3 || gotPage.PageToken != "z-cursor" {
		t.Fatalf("pagination passthrough = %+v, want {3 z-cursor}", gotPage)
	}
}

// TestZoneHandler_List_repoError_mapsToStatus — parity с region: repo-ошибка →
// serviceerr.ToStatus (InvalidArgument).
func TestZoneHandler_List_repoError_mapsToStatus(t *testing.T) {
	mock := &repomock.ZoneRepo{
		ListFunc: func(_ context.Context, _ zone.Pagination) ([]*domain.Zone, string, error) {
			return nil, "", geoerrors.ErrInvalidArg
		},
	}
	uc := zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	h := handler.NewZoneHandler(uc)
	_, err := h.List(context.Background(), &geov1.ListZonesRequest{PageSize: 10})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT for repo ErrInvalidArg, got %v", err)
	}
}

// TestOperationHandler_Get_notFound — несуществующий operation_id → NOT_FOUND.
func TestOperationHandler_Get_notFound(t *testing.T) {
	oh := handler.NewOperationHandler(repomock.NewOpsRepo())
	_, err := oh.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: "geo-absent"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NOT_FOUND, got %v", err)
	}
}
