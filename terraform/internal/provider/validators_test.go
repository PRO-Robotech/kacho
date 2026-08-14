// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestOneOf_RefusesOutsideTheSetAndPassesInside — отрицание строго в паре с
// положительным.
//
// Без положительного контроля проверка, отвергающая вообще всё, выглядела бы
// работающей: «недопустимое отвергнуто» верно и у неё.
func TestOneOf_RefusesOutsideTheSetAndPassesInside(t *testing.T) {
	v := oneOf("SPREAD", "PACK")
	ctx := context.Background()

	check := func(in types.String) validator.StringResponse {
		var resp validator.StringResponse
		v.ValidateString(ctx, validator.StringRequest{
			Path: path.Root("strategy"), ConfigValue: in,
		}, &resp)
		return resp
	}

	t.Run("значение вне набора отвергается и называет допустимые", func(t *testing.T) {
		resp := check(types.StringValue("SPREAD_2"))
		if !resp.Diagnostics.HasError() {
			t.Fatal("значение вне набора принято")
		}
		text := resp.Diagnostics.Errors()[0].Detail()
		for _, want := range []string{"SPREAD", "PACK", "SPREAD_2"} {
			if !strings.Contains(text, want) {
				t.Errorf("отказ не называет %q: %s\n"+
					"Читатель обязан узнать из текста и что он написал, и что допустимо.", want, text)
			}
		}
	})

	t.Run("значение из набора проходит", func(t *testing.T) {
		if resp := check(types.StringValue("PACK")); resp.Diagnostics.HasError() {
			t.Errorf("законное значение отвергнуто: %v", resp.Diagnostics)
		}
	})

	// Обязательность — ответственность схемы, а о неизвестном на плане нельзя
	// утверждать ничего: отвергнув его, мы отвергали бы координату, приходящую
	// выходом другого ресурса.
	t.Run("null и неизвестное пропускаются", func(t *testing.T) {
		if resp := check(types.StringNull()); resp.Diagnostics.HasError() {
			t.Error("null отвергнут — это подменило бы собой Required у схемы")
		}
		if resp := check(types.StringUnknown()); resp.Diagnostics.HasError() {
			t.Error("неизвестное отвергнуто — значение ещё не получено, судить не о чем")
		}
	})
}

// coordinateConfig — конфигурация из двух координат, какой её видит проверка.
//
// Схема настоящая — та, что объявляет ресурс группы: подставная приняла бы
// больше настоящей и сделала бы невидимым ровно тот случай, ради которого
// проверка написана.
func coordinateConfig(t *testing.T, zone, region *string) tfsdk.Config {
	t.Helper()
	s := schemaOf(t, NewPlacementGroupResource())

	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		switch name {
		case "zone_id":
			vals[name] = optionalString(typ, zone)
		case "region_id":
			vals[name] = optionalString(typ, region)
		default:
			vals[name] = tftypes.NewValue(typ, nil)
		}
	}
	return tfsdk.Config{Raw: tftypes.NewValue(objType, vals), Schema: s}
}

func optionalString(typ tftypes.Type, v *string) tftypes.Value {
	if v == nil {
		return tftypes.NewValue(typ, nil)
	}
	if *v == unknownMarker {
		return tftypes.NewValue(typ, tftypes.UnknownValue)
	}
	return tftypes.NewValue(typ, *v)
}

// unknownMarker — просьба подставить НЕИЗВЕСТНОЕ значение вместо строки.
const unknownMarker = "\x00unknown"

// TestExactlyOneOf_ZeroAndBothAreBothRefusals — «ни одной» и «обе» суть разные
// отказы, и оба отказы.
//
// Проверяется на НАСТОЯЩЕЙ схеме ресурса и через тот же путь, каким её зовёт
// каркас, — иначе проба закрепляла бы ответ функции, а не поведение ресурса.
func TestExactlyOneOf_ZeroAndBothAreBothRefusals(t *testing.T) {
	ctx := context.Background()
	zone, region := "ru-central1-a", "ru-central1"

	r, ok := NewPlacementGroupResource().(resource.ResourceWithConfigValidators)
	if !ok {
		t.Fatal("у ресурса нет проверок конфигурации — «обе координаты» уехало бы к краю")
	}
	vs := r.ConfigValidators(ctx)
	if len(vs) == 0 {
		t.Fatal("перечень проверок пуст — форма без содержания")
	}

	run := func(z, rg *string) *resource.ValidateConfigResponse {
		resp := &resource.ValidateConfigResponse{}
		for _, v := range vs {
			v.ValidateResource(ctx, resource.ValidateConfigRequest{
				Config: coordinateConfig(t, z, rg),
			}, resp)
		}
		return resp
	}

	t.Run("ни одной координаты — отказ", func(t *testing.T) {
		if resp := run(nil, nil); !resp.Diagnostics.HasError() {
			t.Error("конфигурация без якоря принята — группа без координаты не описывает размещения")
		}
	})

	t.Run("обе координаты — отказ, называющий обе", func(t *testing.T) {
		resp := run(&zone, &region)
		if !resp.Diagnostics.HasError() {
			t.Fatal("приняты обе координаты — это описывает размещение, которого не бывает")
		}
		text := resp.Diagnostics.Errors()[0].Detail()
		for _, want := range []string{"zone_id", "region_id"} {
			if !strings.Contains(text, want) {
				t.Errorf("отказ не называет %q: %s", want, text)
			}
		}
	})

	// Положительные контроли в паре: обе законные формы проходят. Без них оба
	// отрицания зеленели бы на проверке, отвергающей вообще всё.
	t.Run("только зона — проходит", func(t *testing.T) {
		if resp := run(&zone, nil); resp.Diagnostics.HasError() {
			t.Errorf("зональная группа отвергнута: %v", resp.Diagnostics)
		}
	})
	t.Run("только регион — проходит", func(t *testing.T) {
		if resp := run(nil, &region); resp.Diagnostics.HasError() {
			t.Errorf("региональная группа отвергнута: %v", resp.Diagnostics)
		}
	})

	// Неизвестное значение: проверка ВОЗДЕРЖИВАЕТСЯ.
	//
	// Это не тонкость на будущее, а воспроизведение реального дефекта. Каркас
	// зовёт проверку на конфигурацию блока ДО развёртки `for_each`, где каждое
	// поле — выражение над `each.value`, то есть неизвестно ВСЁ сразу. Прежняя
	// редакция считала неизвестное заданным, находила две координаты там, где ни
	// одной не вычислено, и роняла план на модуле, где групп ноль.
	//
	// Поймала это модульная проба terraform, а не размышление, — поэтому
	// опровергнутая гипотеза записана здесь рядом с её опровержением.
	t.Run("обе координаты неизвестны — воздержаться, а не отвергнуть", func(t *testing.T) {
		unknown := unknownMarker
		if resp := run(&unknown, &unknown); resp.Diagnostics.HasError() {
			t.Errorf("отвергнута конфигурация, о которой судить нечем: %v\n"+
				"Так выглядит КАЖДЫЙ блок с for_each до развёртки — включая тот, "+
				"у которого ноль экземпляров.", resp.Diagnostics)
		}
	})

	t.Run("одна известна, вторая неизвестна — воздержаться", func(t *testing.T) {
		unknown := unknownMarker
		if resp := run(&zone, &unknown); resp.Diagnostics.HasError() {
			t.Errorf("отвергнута пара «литерал + неизвестное»: исход зависит от значения, "+
				"которого ещё нет, и решает его край: %v", resp.Diagnostics)
		}
	})
}

// TestExactlyOneOf_DescribesItselfByItsPaths — описание проверки называет предмет.
//
// Оно попадает в документацию схемы и в вывод отладки; безымянное «ровно один из»
// не сообщает, из чего именно, и читателю приходится идти в код.
func TestExactlyOneOf_DescribesItselfByItsPaths(t *testing.T) {
	d := exactlyOneOf(path.Root("zone_id"), path.Root("region_id")).Description(context.Background())
	for _, want := range []string{"zone_id", "region_id"} {
		if !strings.Contains(d, want) {
			t.Errorf("описание проверки не называет %q: %s", want, d)
		}
	}
}
