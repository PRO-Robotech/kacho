// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package region_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// Здесь стояли пробы формы имени (#718). Их предмет снят целиком: у региона
// больше нет подписи, дублировавшей идентификатор (#716), — а вместе с полем
// ушли и проверка формы, и ограничение таблицы.
//
// Пробы не «стали лишними» — они заменены на утверждение о СНЯТИИ, потому что
// снятое поле умеет вернуться тихо: маска правки принимает произвольные имена,
// и `name` в ней снова стал бы «известным полем» от одной строки в наборе.
// Проба ниже краснеет ровно на этой строке.

// TestUpdate_nameInMaskIsUnknownField — маска, назвавшая снятое поле,
// отвергается синхронно и до записи.
func TestUpdate_nameInMaskIsUnknownField(t *testing.T) {
	mock := &repomock.RegionRepo{
		UpdateFunc: func(context.Context, string, region.UpdateParams) (*domain.Region, error) {
			t.Fatal("Update не должен исполняться: поля name у региона нет")
			return nil, nil
		},
	}
	uc, _ := newUC(mock)

	_, err := uc.Update(context.Background(), region.UpdateInput{
		ID: "ru-central1", Mask: []string{"name"},
	})
	if err == nil {
		t.Fatal("маска назвала снятое поле и была принята")
	}
	if code := grpcstatus.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
		t.Fatalf("код = %s, ожидался InvalidArgument", code)
	}
}

// TestUpdate_liveFieldInMaskStillApplies — положительный контроль к пробе выше.
// Без него отрицание зеленело бы и на «отвергаем любую маску».
func TestUpdate_liveFieldInMaskStillApplies(t *testing.T) {
	var applied bool
	mock := &repomock.RegionRepo{
		UpdateFunc: func(_ context.Context, id string, p region.UpdateParams) (*domain.Region, error) {
			applied = true
			if p.Status == nil {
				t.Fatal("status назван маской, но до репозитория не доехал")
			}
			return &domain.Region{ID: id, Status: *p.Status}, nil
		},
	}
	uc, _ := newUC(mock)

	if _, err := uc.Update(context.Background(), region.UpdateInput{
		ID: "ru-central1", Mask: []string{"status"}, Status: domain.GeoStatusUp,
	}); err != nil {
		t.Fatalf("живое поле маски отвергнуто: %v", err)
	}
	if !applied {
		t.Fatal("живое поле маски не дошло до записи")
	}
}

// TestCreate_withoutNameSucceeds — создание больше НЕ требует подписи.
//
// Прежде тот же вход отвергался синхронно текстом «region name is required»:
// поле было обязательным. Проба закрепляет, что обязательность ушла вместе с
// полем, а не осталась требованием, которое нечем удовлетворить.
func TestCreate_withoutNameSucceeds(t *testing.T) {
	var inserted bool
	mock := &repomock.RegionRepo{InsertFunc: func(_ context.Context, r *domain.Region) (*domain.Region, error) {
		inserted = true
		return r, nil
	}}
	uc, _ := newUC(mock)

	if _, err := uc.Create(context.Background(), region.CreateInput{
		ID: "ru-central1", CountryCode: "RU",
	}); err != nil {
		t.Fatalf("создание без подписи отвергнуто: %v", err)
	}
	if !inserted {
		t.Fatal("создание без подписи не дошло до записи")
	}
}
