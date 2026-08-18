// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package region_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// Предмет проб этого файла — измеренная дыра (задача #718).
//
// Миграция 715001 поставила `regions_name_check` / `zones_name_check` с единой
// формой имени дерева, но geo — ЕДИНСТВЕННЫЙ из пяти прошедших её сервисов, где
// парной проверки в сервисе не было: `domain.ValidateName` сторожит ДЛИНУ и
// только её. Имя `Region_One` проходило проверку сервиса и умирало на
// ограничении таблицы — то есть база оказывалась ПЕРВЫМ читателем ввода, а
// вызывающий получал отказ, не называющий ни поля, ни формы.
//
// Проверка формы обязана стоять на пути запроса, а ограничение таблицы —
// оставаться защитой последнего рубежа (`api-conventions.md` §Error-format).

// TestCreate_nameFormViolation_rejectedSynchronously — негодная форма имени
// отвергается ДО записи, с названным полем и формой в `FieldViolation`.
func TestCreate_nameFormViolation_rejectedSynchronously(t *testing.T) {
	for _, name := range []string{"Region_One", "RU-Central", "имя", "-leading-hyphen", strings.Repeat("a", 64)} {
		t.Run(name, func(t *testing.T) {
			mock := &repomock.RegionRepo{InsertFunc: func(context.Context, *domain.Region) (*domain.Region, error) {
				t.Fatal("Insert не должен исполняться: форма имени судится ДО записи")
				return nil, nil
			}}
			uc, _ := newUC(mock)

			_, err := uc.Create(context.Background(), region.CreateInput{
				ID: "ru-central1", Name: name, CountryCode: "RU",
			})
			if err == nil {
				t.Fatal("негодная форма имени принята")
			}
			// Утверждается НАБЛЮДАЕМОЕ — код и деталь на проводе, — а не
			// внутренний sentinel: форму судит канон дерева (`validate.Name`),
			// который отдаёт готовый статус с `FieldViolation`, и `ToStatus`
			// пробрасывает его как есть (тот же путь, что у `validate.PageSize`
			// в List). Утверждение о sentinel'е запрещало бы этот путь и тем
			// самым — саму деталь, ради которой проба и написана.
			st := grpcstatus.Convert(serviceerr.ToStatus(err))
			if st.Code() != codes.InvalidArgument {
				t.Fatalf("код = %s, ожидался InvalidArgument", st.Code())
			}
			var named bool
			for _, d := range st.Details() {
				br, ok := d.(*errdetails.BadRequest)
				if !ok {
					continue
				}
				for _, fv := range br.GetFieldViolations() {
					if fv.GetField() == "name" {
						named = true
					}
				}
			}
			if !named {
				t.Errorf("в details нет FieldViolation с полем name: %v", st.Details())
			}
		})
	}
}

// TestCreate_nameFormValid_passesValidation — положительный контроль: годное имя
// доходит до записи. Без него отрицание выше зеленело бы и на «отвергаем всё».
func TestCreate_nameFormValid_passesValidation(t *testing.T) {
	var inserted bool
	mock := &repomock.RegionRepo{InsertFunc: func(_ context.Context, r *domain.Region) (*domain.Region, error) {
		inserted = true
		return r, nil
	}}
	uc, _ := newUC(mock)

	if _, err := uc.Create(context.Background(), region.CreateInput{
		ID: "ru-central1", Name: "ru-central-1", CountryCode: "RU",
	}); err != nil {
		t.Fatalf("годное имя отвергнуто: %v", err)
	}
	if !inserted {
		t.Fatal("годное имя не дошло до записи")
	}
}

// TestUpdate_nameFormViolation_rejectedSynchronously — то же на правке.
//
// Пустое имя на правке означает «не менять поле» (COALESCE в репозитории), и
// проверка формы это НЕ отменяет: судится только то, что прислали.
func TestUpdate_nameFormViolation_rejectedSynchronously(t *testing.T) {
	mock := &repomock.RegionRepo{
		GetFunc: func(context.Context, string) (*domain.Region, error) {
			return &domain.Region{ID: "ru-central1", Name: "ru-central-1", CountryCode: "RU"}, nil
		},
		UpdateFunc: func(context.Context, string, region.UpdateParams) (*domain.Region, error) {
			t.Fatal("Update не должен исполняться: форма имени судится ДО записи")
			return nil, nil
		},
	}
	uc, _ := newUC(mock)

	_, err := uc.Update(context.Background(), region.UpdateInput{
		ID: "ru-central1", Name: "Region_One", Mask: []string{"name"},
	})
	if err == nil {
		t.Fatal("негодная форма имени принята на правке")
	}
	if code := grpcstatus.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
		t.Fatalf("код = %s, ожидался InvalidArgument", code)
	}
}
