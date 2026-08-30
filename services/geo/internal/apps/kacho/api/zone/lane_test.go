// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lane_test.go — ПОЛОСА ПРЯМОГО ЧТЕНИЯ УТВЕРЖДАЕТСЯ ПАРОЙ: код И машинный токен.
//
// Зеркало пробы региона: контракт полос один на оба ресурса каталога, и
// расхождение между ними было бы тем самым «продукт противоречит себе»,
// которое разбор клиентом находит первым. Разбор — `api-conventions.md`
// §By-lane code-split, приёмка XC-1 (сценарий XC-1-08).
package zone_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// laneOf — код и ErrorInfo отказа. Ошибка, статусом не являющаяся, — доменный
// sentinel, который переведёт `ToStatus` у транспорта: детали у него нет по
// построению, и это возвращается третьим значением, а не падением пробы.
func laneOf(err error) (codes.Code, *errdetails.ErrorInfo, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return codes.Unknown, nil, false
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return st.Code(), info, true
		}
	}
	return st.Code(), nil, true
}

func wantDirectReadLane(t *testing.T, err error, id string) {
	t.Helper()
	code, info, isStatus := laneOf(err)
	if !isStatus {
		t.Fatalf("отказ не доехал до вызывающего статусом: %v", err)
	}
	if code != codes.NotFound {
		t.Errorf("код = %v, ждали NotFound (полоса прямого чтения)", code)
	}
	if info == nil {
		t.Fatal("отказ не несёт ErrorInfo: клиенту остаётся разбирать прозу, " +
			"тогда как контракт обещает машинный токен полосы")
	}
	if got := info.GetReason(); got != "RESOURCE_NOT_FOUND" {
		t.Errorf("reason = %q, ждали RESOURCE_NOT_FOUND", got)
	}
	if got := info.GetDomain(); got != "geo.kacho.cloud" {
		t.Errorf("domain = %q, ждали geo.kacho.cloud", got)
	}
	if got := info.GetMetadata()["resource_type"]; got != "geo.zone" {
		t.Errorf("resource_type = %q, ждали geo.zone", got)
	}
	if got := info.GetMetadata()["resource_id"]; got != id {
		t.Errorf("resource_id = %q, ждали %q", got, id)
	}
}

func missingZoneRepo() *repomock.ZoneRepo {
	miss := func(context.Context, string) (*domain.Zone, error) { return nil, geoerrors.ErrNotFound }
	return &repomock.ZoneRepo{GetFunc: miss, GetInternalFunc: miss}
}

// TestGet_absentZone_directReadLane — публичное чтение отсутствующей зоны
// отвечает парой NOT_FOUND + RESOURCE_NOT_FOUND.
func TestGet_absentZone_directReadLane(t *testing.T) {
	uc, _ := newUC(missingZoneRepo())
	_, err := uc.Get(context.Background(), "eu-west1-a")
	if err == nil {
		t.Fatal("Get отсутствующей зоны обязан отказать")
	}
	wantDirectReadLane(t, err, "eu-west1-a")
}

// TestGetInternal_absentZone_directReadLane — та же полоса на :9091.
func TestGetInternal_absentZone_directReadLane(t *testing.T) {
	uc, _ := newUC(missingZoneRepo())
	_, err := uc.GetInternal(context.Background(), "eu-west1-a")
	if err == nil {
		t.Fatal("GetInternal отсутствующей зоны обязан отказать")
	}
	wantDirectReadLane(t, err, "eu-west1-a")
}

// TestGet_absentZone_proseUnchanged — ЗАКОННЫЙ БЛИЗНЕЦ: деталь добавляется,
// проза остаётся контрактным тоном.
func TestGet_absentZone_proseUnchanged(t *testing.T) {
	mock := &repomock.ZoneRepo{GetFunc: func(context.Context, string) (*domain.Zone, error) {
		return nil, fmt.Errorf("%w: Zone eu-west1-a not found", geoerrors.ErrNotFound)
	}}
	uc, _ := newUC(mock)
	_, err := uc.Get(context.Background(), "eu-west1-a")
	st, _ := status.FromError(err)
	if got := st.Message(); got != "Zone eu-west1-a not found" {
		t.Errorf("сообщение = %q, ждали контрактный тон «Zone eu-west1-a not found»", got)
	}
}

// TestGet_otherFailure_noLane — ОТРИЦАНИЕ В ПАРУ: ошибка НЕ этой полосы детали
// не получает.
func TestGet_otherFailure_noLane(t *testing.T) {
	mock := &repomock.ZoneRepo{GetFunc: func(context.Context, string) (*domain.Zone, error) {
		return nil, geoerrors.ErrInternal
	}}
	uc, _ := newUC(mock)
	_, err := uc.Get(context.Background(), "eu-west1-a")
	if _, info, _ := laneOf(err); info != nil {
		t.Errorf("внутренняя ошибка получила деталь полосы %q", info.GetReason())
	}
}
