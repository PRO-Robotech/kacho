// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package zone_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// Зеркало проб региона (задача #718). Пара стоит намеренно: дыра была ОДНА на
// два ресурса одной схемы, и починка одного из них оставила бы класс живым.

// TestCreate_nameFormViolation_rejectedSynchronously — негодная форма имени зоны
// отвергается ДО записи, с названным полем в `FieldViolation`.
func TestCreate_nameFormViolation_rejectedSynchronously(t *testing.T) {
	for _, name := range []string{"Zone_A", "ZONE-A", "зона", "trailing-", strings.Repeat("z", 64)} {
		t.Run(name, func(t *testing.T) {
			mock := &repomock.ZoneRepo{InsertFunc: func(context.Context, *domain.Zone) (*domain.Zone, error) {
				t.Fatal("Insert не должен исполняться: форма имени судится ДО записи")
				return nil, nil
			}}
			uc, _ := newUC(mock)

			_, err := uc.Create(context.Background(), zone.CreateInput{
				ID: "ru-central1-a", RegionID: "ru-central1", Name: name,
			})
			if err == nil {
				t.Fatal("негодная форма имени принята")
			}
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

// TestCreate_nameFormValid_passesValidation — положительный контроль.
func TestCreate_nameFormValid_passesValidation(t *testing.T) {
	var inserted bool
	mock := &repomock.ZoneRepo{InsertFunc: func(_ context.Context, z *domain.Zone) (*domain.Zone, error) {
		inserted = true
		return z, nil
	}}
	uc, _ := newUC(mock)

	if _, err := uc.Create(context.Background(), zone.CreateInput{
		ID: "ru-central1-a", RegionID: "ru-central1", Name: "zone-a",
	}); err != nil {
		t.Fatalf("годное имя отвергнуто: %v", err)
	}
	if !inserted {
		t.Fatal("годное имя не дошло до записи")
	}
}
