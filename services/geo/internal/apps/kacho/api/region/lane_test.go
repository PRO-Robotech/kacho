// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lane_test.go — ПОЛОСА ПРЯМОГО ЧТЕНИЯ УТВЕРЖДАЕТСЯ ПАРОЙ: код И машинный токен.
//
// `api-conventions.md` §By-lane code-split требует, чтобы клиент отличал «я не
// нашёл СВОЁ» от «предусловие на ЧУЖОЕ не выполнено» ПО ТОКЕНУ, а не разбором
// прозы: тон сообщения стабилен, но не парсибелен. geo — leaf-владелец каталога
// размещения, и его промах арендатор видит НАПРЯМУЮ: Region/Zone Get и List —
// публичные read-RPC (project-scope EXEMPT, `security.md`), которые обязан
// читать всякий, кто размещает ресурс.
//
// Утверждать один код без токена значит не заметить смены полосы; утверждать
// один токен без кода — не заметить смены отображения на крае. Поэтому проба
// требует ОБЕ половины, а не любую из них (приёмка XC-1, сценарий XC-1-08).
package region_test

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

// laneOf возвращает пару «код + ErrorInfo» отказа. Деталь ищется среди details,
// а не берётся первой: порядок деталей контрактом не задан.
//
// Ошибка, статусом не являющаяся, — это доменный sentinel, который переведёт
// `ToStatus` у транспорта: детали полосы у него нет по построению, и `ok=false`
// здесь означает именно это, а не сбой пробы.
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

// wantDirectReadLane — общая проверка полосы прямого чтения: код NOT_FOUND,
// токен RESOURCE_NOT_FOUND, домен источника и координата ресурса.
func wantDirectReadLane(t *testing.T, err error, resourceType, id string) {
	t.Helper()
	code, info, isStatus := laneOf(err)
	if !isStatus {
		t.Fatalf("отказ не доехал до вызывающего статусом: %v — полоса собирается "+
			"там, где известен id, и обязана уже нести код", err)
	}
	if code != codes.NotFound {
		t.Errorf("код = %v, ждали NotFound (полоса прямого чтения: «не нашёл СВОЁ»)", code)
	}
	if info == nil {
		t.Fatal("отказ не несёт ErrorInfo: клиенту остаётся разбирать прозу сообщения, " +
			"тогда как контракт обещает машинный токен полосы")
	}
	if got := info.GetReason(); got != "RESOURCE_NOT_FOUND" {
		t.Errorf("reason = %q, ждали RESOURCE_NOT_FOUND", got)
	}
	if got := info.GetDomain(); got != "geo.kacho.cloud" {
		t.Errorf("domain = %q, ждали geo.kacho.cloud", got)
	}
	if got := info.GetMetadata()["resource_type"]; got != resourceType {
		t.Errorf("resource_type = %q, ждали %q", got, resourceType)
	}
	if got := info.GetMetadata()["resource_id"]; got != id {
		t.Errorf("resource_id = %q, ждали %q", got, id)
	}
}

func missingRegionRepo() *repomock.RegionRepo {
	miss := func(context.Context, string) (*domain.Region, error) { return nil, geoerrors.ErrNotFound }
	return &repomock.RegionRepo{GetFunc: miss, GetInternalFunc: miss}
}

// TestGet_absentRegion_directReadLane — XC-1-08: публичное чтение отсутствующего
// региона отвечает парой NOT_FOUND + RESOURCE_NOT_FOUND.
func TestGet_absentRegion_directReadLane(t *testing.T) {
	uc, _ := newUC(missingRegionRepo())
	_, err := uc.Get(context.Background(), "eu-west1")
	if err == nil {
		t.Fatal("Get отсутствующего региона обязан отказать")
	}
	wantDirectReadLane(t, err, "geo.region", "eu-west1")
}

// TestGetInternal_absentRegion_directReadLane — та же полоса на :9091. Внутренний
// листенер от контракта полос не освобождён: он тот же direct-read.
func TestGetInternal_absentRegion_directReadLane(t *testing.T) {
	uc, _ := newUC(missingRegionRepo())
	_, err := uc.GetInternal(context.Background(), "eu-west1")
	if err == nil {
		t.Fatal("GetInternal отсутствующего региона обязан отказать")
	}
	wantDirectReadLane(t, err, "geo.region", "eu-west1")
}

// TestGet_absentRegion_proseUnchanged — ЗАКОННЫЙ БЛИЗНЕЦ полосы: деталь
// добавляется, проза остаётся контрактным тоном. Без этой пробы полосу можно
// было бы «поставить», заодно переписав сообщение, — и сломать контракт,
// оставшись зелёным.
func TestGet_absentRegion_proseUnchanged(t *testing.T) {
	mock := &repomock.RegionRepo{
		GetFunc: func(context.Context, string) (*domain.Region, error) {
			return nil, fmt.Errorf("%w: Region eu-west1 not found", geoerrors.ErrNotFound)
		},
	}
	uc, _ := newUC(mock)
	_, err := uc.Get(context.Background(), "eu-west1")
	st, _ := status.FromError(err)
	if got := st.Message(); got != "Region eu-west1 not found" {
		t.Errorf("сообщение = %q, ждали контрактный тон «Region eu-west1 not found»", got)
	}
}

// TestGet_otherFailure_noLane — ОТРИЦАНИЕ В ПАРУ: ошибка НЕ этой полосы детали
// прямого чтения не получает. Иначе «полоса проставлена» означало бы «полоса
// проставлена всегда», то есть не означало бы ничего.
func TestGet_otherFailure_noLane(t *testing.T) {
	mock := &repomock.RegionRepo{
		GetFunc: func(context.Context, string) (*domain.Region, error) {
			return nil, geoerrors.ErrInternal
		},
	}
	uc, _ := newUC(mock)
	_, err := uc.Get(context.Background(), "eu-west1")
	if _, info, _ := laneOf(err); info != nil {
		t.Errorf("внутренняя ошибка получила деталь полосы %q — отказ без полосы "+
			"не вправе притворяться полосой контракта", info.GetReason())
	}
}
