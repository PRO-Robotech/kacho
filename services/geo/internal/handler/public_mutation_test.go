// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

// public_mutation_test.go — административные глаголы каталога на ПУБЛИЧНЫХ
// RegionService/ZoneService (приёмка sub-phase-ADM-1-geo-placement-catalog-admin,
// §Р1–§Р4). Утверждается то, что отличает публичную поверхность от внутреннего
// близнеца, и ничего сверх того: блок infra° сюда не доезжает ни на создании, ни
// через маску правки, а предупреждение о закрытом создании не называет путей,
// недоступных вызывающему публичного API. Всё остальное (тексты отказов, полосы,
// Operation.error) — общий use-case, и утверждается он у обоих путей одинаково.

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// TestRegionHandler_ADM1GEO01_PublicCreateIsDoneWithPublicRegion — сценарий 01 на
// уровне службы: публичное создание отвечает Operation done=true, metadata несёт
// regionId, response — публичный Region; в хранилище уходит регион БЕЗ infra°.
func TestRegionHandler_ADM1GEO01_PublicCreateIsDoneWithPublicRegion(t *testing.T) {
	var stored *domain.Region
	mock := &repomock.RegionRepo{
		InsertFunc: func(_ context.Context, r *domain.Region) (*domain.Region, error) {
			stored = r
			return r, nil
		},
	}
	h := handler.NewRegionHandler(region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	op, err := h.Create(context.Background(), &geov1.CreatePublicRegionRequest{
		Id: "adm1g-x", CountryCode: "RU", Status: geov1.GeoStatus_UP,
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if !op.GetDone() || op.GetError() != nil {
		t.Fatalf("op = done %v error %v, want done=true without error", op.GetDone(), op.GetError())
	}
	meta := &geov1.CreateRegionMetadata{}
	if err := op.GetMetadata().UnmarshalTo(meta); err != nil || meta.GetRegionId() != "adm1g-x" {
		t.Fatalf("metadata = %v (%v), want regionId adm1g-x", meta, err)
	}
	if len(meta.GetWarnings()) != 0 {
		t.Fatalf("region created UP must carry no warnings, got %v", meta.GetWarnings())
	}
	msg, err := op.GetResponse().UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	r, ok := msg.(*geov1.Region)
	if !ok || r.GetId() != "adm1g-x" || r.GetCountryCode() != "RU" || !r.GetOpenForPlacement() {
		t.Fatalf("response = %#v, want public Region adm1g-x RU open", msg)
	}
	if stored == nil || stored.Infra != (domain.RegionInfra{}) {
		t.Fatalf("public Create stored infra %+v — the public input carries no infra°", stored)
	}
}

// TestRegionHandler_ADM1GEO01_ClosedWarningNamesNoInternalPath — §Р4:
// предупреждение о создании закрытым читает вызывающий публичного API, и ссылка
// на внутреннюю службу была бы ему ложной подсказкой.
func TestRegionHandler_ADM1GEO01_ClosedWarningNamesNoInternalPath(t *testing.T) {
	mock := &repomock.RegionRepo{
		InsertFunc: func(_ context.Context, r *domain.Region) (*domain.Region, error) { return r, nil },
	}
	h := handler.NewRegionHandler(region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	op, err := h.Create(context.Background(), &geov1.CreatePublicRegionRequest{Id: "adm1g-closed"})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	meta := &geov1.CreateRegionMetadata{}
	if err := op.GetMetadata().UnmarshalTo(meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if len(meta.GetWarnings()) != 1 {
		t.Fatalf("a region created without status is CLOSED and must say so: warnings = %v", meta.GetWarnings())
	}
	if w := meta.GetWarnings()[0]; strings.Contains(w, "Internal") || strings.Contains(w, "/internal") {
		t.Fatalf("warning names an internal path or service: %q", w)
	}
}

// TestZoneHandler_ADM1GEO02_PublicCreateStoresNoInfra — сценарий 02 на уровне
// службы: зона, созданная публичным глаголом, не несёт infra°.
func TestZoneHandler_ADM1GEO02_PublicCreateStoresNoInfra(t *testing.T) {
	var stored *domain.Zone
	mock := &repomock.ZoneRepo{
		InsertFunc: func(_ context.Context, z *domain.Zone) (*domain.Zone, error) {
			stored = z
			z.RegionStatus = domain.GeoStatusUp
			return z, nil
		},
	}
	h := handler.NewZoneHandler(zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	op, err := h.Create(context.Background(), &geov1.CreatePublicZoneRequest{
		Id: "adm1g-x-a", RegionId: "adm1g-x", Status: geov1.GeoStatus_UP,
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if !op.GetDone() || op.GetError() != nil {
		t.Fatalf("op = done %v error %v", op.GetDone(), op.GetError())
	}
	msg, err := op.GetResponse().UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if z, ok := msg.(*geov1.Zone); !ok || z.GetId() != "adm1g-x-a" || z.GetRegionId() != "adm1g-x" ||
		z.GetPlacementBlockedReason() != geov1.PlacementBlockedReason_NONE {
		t.Fatalf("response = %#v, want public Zone adm1g-x-a in adm1g-x, NONE", msg)
	}
	if stored == nil || stored.Infra.NumericInfraID != 0 || len(stored.Infra.HostClasses) != 0 ||
		stored.Infra.FailureDomainCount != 0 || stored.Infra.UnderlayAnchor != "" || stored.Infra.CapacityHint != "" {
		t.Fatalf("public Create stored infra %+v — the public input carries no infra°", stored)
	}
}

// maskViolation достаёт описание нарушения поля update_mask из статуса отказа.
func maskViolation(t *testing.T, err error) string {
	t.Helper()
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				if v.GetField() == "update_mask" {
					return v.GetDescription()
				}
			}
		}
	}
	return ""
}

// TestZoneHandler_ADM1GEO11_PublicMaskDoesNotReachInfra — сценарий 11, обе
// стороны оси. Публичная маска, назвавшая подполе infra°, отвергается синхронно
// общей проверкой маски; ТА ЖЕ маска на внутреннем близнеце принимается — то
// есть отказ порождён поверхностью, а не формой пути. Неизменяемый regionId —
// своим текстом; status — проходит.
func TestZoneHandler_ADM1GEO11_PublicMaskDoesNotReachInfra(t *testing.T) {
	var updates int
	mock := &repomock.ZoneRepo{
		UpdateFunc: func(_ context.Context, id string, _ zone.UpdateParams) (*domain.Zone, error) {
			updates++
			return &domain.Zone{ID: id, RegionID: "adm1g-m", Status: domain.GeoStatusDown, RegionStatus: domain.GeoStatusUp}, nil
		},
	}
	uc := zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus)
	pub := handler.NewZoneHandler(uc)
	intr := handler.NewInternalZoneHandler(uc)
	ctx := context.Background()

	_, err := pub.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.hostClasses"}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("public mask infra.hostClasses: want INVALID_ARGUMENT, got %v", err)
	}
	if got, want := maskViolation(t, err), "unknown field in update_mask: infra.hostClasses"; got != want {
		t.Fatalf("update_mask violation = %q, want %q", got, want)
	}
	if updates != 0 {
		t.Fatalf("a refused public mask reached the writer %d times", updates)
	}

	// Близнец по поверхности: тот же путь маски внутренним глаголом — принят.
	op, err := intr.Update(ctx, &geov1.UpdateZoneRequest{
		ZoneId: "adm1g-m-a", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.hostClasses"}},
	})
	if err != nil || op.GetError() != nil {
		t.Fatalf("internal twin with the same mask must be accepted: err=%v op.error=%v", err, op.GetError())
	}

	_, err = pub.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"regionId"}},
	})
	if st, _ := status.FromError(err); st.Code() != codes.InvalidArgument || st.Message() != "regionId is immutable after Zone.Create" {
		t.Fatalf("public mask regionId: got %v, want INVALID_ARGUMENT %q", err, "regionId is immutable after Zone.Create")
	}

	// Близнец по полю: status — изменяемое поле публичной маски.
	op, err = pub.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", Status: geov1.GeoStatus_DOWN,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	if err != nil || !op.GetDone() || op.GetError() != nil {
		t.Fatalf("public mask status: err=%v done=%v op.error=%v, want accepted", err, op.GetDone(), op.GetError())
	}
}

// TestRegionHandler_ADM1GEO11_PublicMaskNumericInfraIdIsImmutable — сценарий 11,
// региональная нога: путь infra.numericInfraId отвергается текстом
// неизменяемости раньше общей проверки; status/countryCode — проходят.
func TestRegionHandler_ADM1GEO11_PublicMaskNumericInfraIdIsImmutable(t *testing.T) {
	mock := &repomock.RegionRepo{
		UpdateFunc: func(_ context.Context, id string, _ region.UpdateParams) (*domain.Region, error) {
			return &domain.Region{ID: id, CountryCode: "KZ", Status: domain.GeoStatusUp}, nil
		},
	}
	h := handler.NewRegionHandler(region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	ctx := context.Background()

	_, err := h.Update(ctx, &geov1.UpdatePublicRegionRequest{
		RegionId: "adm1g-m", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.numericInfraId"}},
	})
	if st, _ := status.FromError(err); st.Code() != codes.InvalidArgument || st.Message() != "numericInfraId is immutable after Region.Create" {
		t.Fatalf("got %v, want INVALID_ARGUMENT %q", err, "numericInfraId is immutable after Region.Create")
	}

	_, err = h.Update(ctx, &geov1.UpdatePublicRegionRequest{
		RegionId: "adm1g-m", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.capacityHint"}},
	})
	if got, want := maskViolation(t, err), "unknown field in update_mask: infra.capacityHint"; status.Code(err) != codes.InvalidArgument || got != want {
		t.Fatalf("got %v (%q), want INVALID_ARGUMENT with %q", err, got, want)
	}

	op, err := h.Update(ctx, &geov1.UpdatePublicRegionRequest{
		RegionId: "adm1g-m", CountryCode: "KZ", Status: geov1.GeoStatus_UP,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status", "countryCode"}},
	})
	if err != nil || op.GetError() != nil {
		t.Fatalf("status+countryCode: err=%v op.error=%v, want accepted", err, op.GetError())
	}
}

// TestRegionHandler_ADM1GEO05_PublicDeleteIsDoneWithEmpty — сценарий 05 на
// уровне службы: удаление отвечает Operation done=true, response — Empty.
func TestRegionHandler_ADM1GEO05_PublicDeleteIsDoneWithEmpty(t *testing.T) {
	var deleted []string
	rmock := &repomock.RegionRepo{DeleteFunc: func(_ context.Context, id string) error { deleted = append(deleted, id); return nil }}
	zmock := &repomock.ZoneRepo{DeleteFunc: func(_ context.Context, id string) error { deleted = append(deleted, id); return nil }}
	rh := handler.NewRegionHandler(region.New(rmock, rmock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	zh := handler.NewZoneHandler(zone.New(zmock, zmock, repomock.NewOpsRepo(), serviceerr.ToStatus))

	zop, err := zh.Delete(context.Background(), &geov1.DeleteZoneRequest{ZoneId: "adm1g-x-a"})
	if err != nil || !zop.GetDone() || zop.GetError() != nil {
		t.Fatalf("zone delete: err=%v op=%v", err, zop)
	}
	rop, err := rh.Delete(context.Background(), &geov1.DeleteRegionRequest{RegionId: "adm1g-x"})
	if err != nil || !rop.GetDone() || rop.GetError() != nil {
		t.Fatalf("region delete: err=%v op=%v", err, rop)
	}
	if strings.Join(deleted, ",") != "adm1g-x-a,adm1g-x" {
		t.Fatalf("writer saw deletes %v", deleted)
	}
	if rop.GetResponse().GetTypeUrl() != "type.googleapis.com/google.protobuf.Empty" {
		t.Fatalf("delete response type = %q, want google.protobuf.Empty", rop.GetResponse().GetTypeUrl())
	}
}
