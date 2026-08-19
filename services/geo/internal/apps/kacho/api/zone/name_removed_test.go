// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package zone_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// Зеркало проб региона. Пара стоит намеренно: поле-дубль было ОДНО на два
// ресурса одной схемы, и снятие у одного оставило бы класс живым у второго.

// TestUpdate_nameInMaskIsUnknownField — маска, назвавшая снятое поле,
// отвергается синхронно и до записи.
func TestUpdate_nameInMaskIsUnknownField(t *testing.T) {
	mock := &repomock.ZoneRepo{
		UpdateFunc: func(context.Context, string, zone.UpdateParams) (*domain.Zone, error) {
			t.Fatal("Update не должен исполняться: поля name у зоны нет")
			return nil, nil
		},
	}
	uc, _ := newUC(mock)

	_, err := uc.Update(context.Background(), zone.UpdateInput{
		ID: "ru-central1-a", Mask: []string{"name"},
	})
	if err == nil {
		t.Fatal("маска назвала снятое поле и была принята")
	}
	if code := grpcstatus.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
		t.Fatalf("код = %s, ожидался InvalidArgument", code)
	}
}

// TestUpdate_liveFieldInMaskStillApplies — положительный контроль к пробе выше.
func TestUpdate_liveFieldInMaskStillApplies(t *testing.T) {
	var applied bool
	mock := &repomock.ZoneRepo{
		UpdateFunc: func(_ context.Context, id string, p zone.UpdateParams) (*domain.Zone, error) {
			applied = true
			if p.Status == nil {
				t.Fatal("status назван маской, но до репозитория не доехал")
			}
			return &domain.Zone{ID: id, RegionID: "ru-central1", Status: *p.Status}, nil
		},
	}
	uc, _ := newUC(mock)

	if _, err := uc.Update(context.Background(), zone.UpdateInput{
		ID: "ru-central1-a", Mask: []string{"status"}, Status: domain.GeoStatusUp,
	}); err != nil {
		t.Fatalf("живое поле маски отвергнуто: %v", err)
	}
	if !applied {
		t.Fatal("живое поле маски не дошло до записи")
	}
}

// TestCreate_withoutNameSucceeds — создание больше НЕ требует подписи (прежде
// тот же вход отвергался текстом «zone name is required»).
func TestCreate_withoutNameSucceeds(t *testing.T) {
	var inserted bool
	mock := &repomock.ZoneRepo{InsertFunc: func(_ context.Context, z *domain.Zone) (*domain.Zone, error) {
		inserted = true
		return z, nil
	}}
	uc, _ := newUC(mock)

	if _, err := uc.Create(context.Background(), zone.CreateInput{
		ID: "ru-central1-a", RegionID: "ru-central1",
	}); err != nil {
		t.Fatalf("создание без подписи отвергнуто: %v", err)
	}
	if !inserted {
		t.Fatal("создание без подписи не дошло до записи")
	}
}
