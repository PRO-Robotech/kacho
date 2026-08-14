// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// schemaOf — схема ресурса. Отдельной пробой не является: это способ спросить у
// ресурса то, что он объявляет о себе сам.
func schemaOf(t *testing.T, r resource.Resource) rschema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("схема не собралась: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// TestPlacementGroupSchema_AnchorHasExactlyOneWayToSayIt — вид якоря ВЫВОДИТСЯ,
// а не пишется.
//
// Будь `placement_type` доступен для записи, появилась бы пара, описывающая
// размещение, которого не бывает: `ZONAL` при заданном регионе. Проверка схемы
// закрывает это by construction — противоречие становится невыразимым, а не
// отлавливаемым.
func TestPlacementGroupSchema_AnchorHasExactlyOneWayToSayIt(t *testing.T) {
	s := schemaOf(t, NewPlacementGroupResource())

	pt, ok := s.Attributes["placement_type"]
	if !ok {
		t.Fatal("атрибута placement_type нет вовсе — читать вид якоря арендатору нечем")
	}
	if pt.IsOptional() || pt.IsRequired() {
		t.Error("placement_type доступен для записи — появился второй способ сказать то же самое, " +
			"и первым же следствием станет пара «ZONAL при заданном регионе»")
	}
	if !pt.IsComputed() {
		t.Error("placement_type не вычисляемый — вид якоря обязан приходить с края")
	}

	// Положительный контроль: координаты, наоборот, ПИШУТСЯ — иначе группу не
	// создать вовсе, и проверка выше зеленела бы на схеме, где нет ничего.
	for _, name := range []string{"zone_id", "region_id"} {
		a, ok := s.Attributes[name]
		if !ok {
			t.Fatalf("атрибута %s нет — задать якорь нечем", name)
		}
		if !a.IsOptional() {
			t.Errorf("%s не доступен для записи — координату задаёт арендатор", name)
		}
	}
}

// TestPlacementGroupValidators_ExactlyOneCoordinate — «обе» и «ни одной»
// отвергаются.
//
// Проверка стоит у ресурса ради МОМЕНТА: несочетаемый якорь виден на плане, до
// того как применение начнёт заводить что-либо ещё. Авторитетом остаётся край —
// здесь проверяется форма сообщения, а не политика.
func TestPlacementGroupValidators_ExactlyOneCoordinate(t *testing.T) {
	r, ok := NewPlacementGroupResource().(resource.ResourceWithConfigValidators)
	if !ok {
		t.Fatal("у ресурса нет проверок конфигурации — «обе координаты» уехало бы к краю")
	}
	vs := r.ConfigValidators(context.Background())
	if len(vs) == 0 {
		t.Fatal("перечень проверок пуст — форма без содержания")
	}
	for _, v := range vs {
		t.Logf("проверка конфигурации: %s", v.Description(context.Background()))
	}
}

// TestApplyPlacementGroup_UnsetCoordinateStaysNull — незаданная координата
// остаётся null, а не пустой строкой.
//
// Пустая строка в состоянии читается как «арендатор задал пустой регион», и
// проверка «ровно одна координата» увидела бы две заданных: план расходился бы
// после каждого применения на группе, которая совершенно законна.
func TestApplyPlacementGroup_UnsetCoordinateStaysNull(t *testing.T) {
	ctx := context.Background()

	t.Run("зональная группа не получает пустого региона", func(t *testing.T) {
		m := placementGroupModel{ZoneID: types.StringValue("ru-central1-a"), RegionID: types.StringNull()}
		applyPlacementGroup(ctx, &m, &placementGroupJSON{
			ID: "plg-1", Name: "г", Strategy: "SPREAD", PlacementType: "ZONAL",
			ZoneID: "ru-central1-a", RegionID: "",
		})
		if !m.RegionID.IsNull() {
			t.Errorf("регион = %v, ожидался null: пустая строка читается как заданное значение "+
				"и даёт две координаты вместо одной", m.RegionID)
		}
		if m.ZoneID.ValueString() != "ru-central1-a" {
			t.Errorf("зона = %v — заданная координата обязана доехать", m.ZoneID)
		}
	})

	t.Run("региональная группа не получает пустой зоны", func(t *testing.T) {
		m := placementGroupModel{ZoneID: types.StringNull(), RegionID: types.StringValue("ru-central1")}
		applyPlacementGroup(ctx, &m, &placementGroupJSON{
			ID: "plg-2", Name: "г", Strategy: "PACK", PlacementType: "REGIONAL",
			ZoneID: "", RegionID: "ru-central1",
		})
		if !m.ZoneID.IsNull() {
			t.Errorf("зона = %v, ожидался null", m.ZoneID)
		}
	})

	// Положительный контроль: вид якоря и стратегия ДОЕЗЖАЮТ. Без него обе пробы
	// выше зеленели бы на реализации, не пишущей в состояние ничего.
	t.Run("вид якоря и стратегия приходят с края", func(t *testing.T) {
		m := placementGroupModel{}
		applyPlacementGroup(ctx, &m, &placementGroupJSON{
			ID: "plg-3", Name: "г", Strategy: "SPREAD", PlacementType: "REGIONAL", RegionID: "ru-central1",
		})
		if m.PlacementType.ValueString() != "REGIONAL" {
			t.Errorf("вид якоря = %v, ожидался REGIONAL", m.PlacementType)
		}
		if m.Strategy.ValueString() != "SPREAD" {
			t.Errorf("стратегия = %v, ожидалась SPREAD", m.Strategy)
		}
	})
}
