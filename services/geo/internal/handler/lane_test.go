// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lane_test.go — ПОЛОСА ДОЕЗЖАЕТ ДО ВЫЗЫВАЮЩЕГО, А НЕ ТОЛЬКО ПРОСТАВЛЯЕТСЯ.
//
// Проба use-case утверждает, что полоса собрана; она НЕ утверждает, что деталь
// переживёт транспорт. Между ними стоит `serviceerr.ToStatus`, который на других
// ветках ПЕРЕСОБИРАЕТ статус — а пересборка детали теряет. Свойство «деталь
// доехала» есть свойство ЦЕПОЧКИ, и утверждать его надо на цепочке.
//
// Класс не выдуманный: тот же файл `serviceerr.go` предупреждает о нём в своём
// комментарии применительно к деталям `BadRequest` от валидатора.
package handler_test

import (
	"context"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// laneReason — токен полосы из деталей отказа; пусто, если детали нет.
func laneReason(err error) (codes.Code, string) {
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return st.Code(), info.GetReason()
		}
	}
	return st.Code(), ""
}

// TestRegionHandler_Get_absent_laneSurvivesTransport — публичный RPC отдаёт пару
// NOT_FOUND + RESOURCE_NOT_FOUND, а не один код.
func TestRegionHandler_Get_absent_laneSurvivesTransport(t *testing.T) {
	mock := &repomock.RegionRepo{GetFunc: func(context.Context, string) (*domain.Region, error) {
		return nil, geoerrors.ErrNotFound
	}}
	h := handler.NewRegionHandler(region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	_, err := h.Get(context.Background(), &geov1.GetRegionRequest{RegionId: "eu-west1"})
	code, reason := laneReason(err)
	if code != codes.NotFound {
		t.Errorf("код = %v, ждали NotFound", code)
	}
	if reason != "RESOURCE_NOT_FOUND" {
		t.Errorf("reason = %q, ждали RESOURCE_NOT_FOUND — деталь не пережила транспорт", reason)
	}
}

// TestZoneHandler_Get_absent_laneSurvivesTransport — то же у зоны: контракт
// полос один на оба ресурса каталога.
func TestZoneHandler_Get_absent_laneSurvivesTransport(t *testing.T) {
	mock := &repomock.ZoneRepo{GetFunc: func(context.Context, string) (*domain.Zone, error) {
		return nil, geoerrors.ErrNotFound
	}}
	h := handler.NewZoneHandler(zone.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	_, err := h.Get(context.Background(), &geov1.GetZoneRequest{ZoneId: "eu-west1-a"})
	code, reason := laneReason(err)
	if code != codes.NotFound {
		t.Errorf("код = %v, ждали NotFound", code)
	}
	if reason != "RESOURCE_NOT_FOUND" {
		t.Errorf("reason = %q, ждали RESOURCE_NOT_FOUND — деталь не пережила транспорт", reason)
	}
}

// TestRegionHandler_Get_internalError_carriesNoLane — ОТРИЦАНИЕ В ПАРУ: отказ,
// полосой не являющийся, деталь через транспорт не приобретает.
func TestRegionHandler_Get_internalError_carriesNoLane(t *testing.T) {
	mock := &repomock.RegionRepo{GetFunc: func(context.Context, string) (*domain.Region, error) {
		return nil, geoerrors.ErrInternal
	}}
	h := handler.NewRegionHandler(region.New(mock, mock, repomock.NewOpsRepo(), serviceerr.ToStatus))
	_, err := h.Get(context.Background(), &geov1.GetRegionRequest{RegionId: "eu-west1"})
	if code, reason := laneReason(err); reason != "" {
		t.Errorf("отказ %v получил деталь полосы %q", code, reason)
	}
}
