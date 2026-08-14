// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Обе пробы этого файла ПЕРЕНЕСЕНЫ с ресурса машины, снятого при слиянии как дубль
// (два воплощения одного `kacho_compute_instance` сосуществовать не могут). Их
// предмет дубля пережил: он про свойства ЖИВОГО ресурса, а не про снятый файл.

// TestStringsFromList_UnsaidIsNotEmpty — «пользователь не сказал» и «снять всё»
// обязаны различаться.
//
// Разница видна ровно на наборе ключей входа: пустой набор при названной маске
// означает снятие всего доступа к машине. Подставив его вместо «не сказал», мы
// отправили бы краю команду, которой не было, — и правка описания снимала бы
// доступ.
func TestStringsFromList_UnsaidIsNotEmpty(t *testing.T) {
	ctx := context.Background()

	t.Run("null — это «не сказал», а не «пусто»", func(t *testing.T) {
		if got := stringsFromList(ctx, types.ListNull(types.StringType)); got != nil {
			t.Errorf("получено %#v, ожидался nil: «не сказал» и «снять всё» неразличимы", got)
		}
	})

	t.Run("неизвестное — тоже «не сказал»", func(t *testing.T) {
		if got := stringsFromList(ctx, types.ListUnknown(types.StringType)); got != nil {
			t.Errorf("получено %#v, ожидался nil: о неизвестном нельзя утверждать ни «задано», ни «пусто»", got)
		}
	})

	// Положительный контроль в паре: пустой ЗАДАННЫЙ список — осмысленное
	// намерение и обязан доехать непустым набором нулевой длины, отличимым от nil.
	t.Run("заданный пустой список — это «снять всё»", func(t *testing.T) {
		empty, diags := types.ListValueFrom(ctx, types.StringType, []string{})
		if diags.HasError() {
			t.Fatalf("не собрать пустой список: %v", diags)
		}
		got := stringsFromList(ctx, empty)
		if got == nil {
			t.Fatal("заданный пустой список отдан как nil — «снять всё» стало неотличимо от «не сказал»")
		}
		if len(got) != 0 {
			t.Errorf("получено %#v, ожидался набор нулевой длины", got)
		}
	})

	t.Run("непустой список доезжает целиком", func(t *testing.T) {
		l, diags := types.ListValueFrom(ctx, types.StringType, []string{"gak-a", "gak-b"})
		if diags.HasError() {
			t.Fatalf("не собрать список: %v", diags)
		}
		got := stringsFromList(ctx, l)
		if len(got) != 2 || got[0] != "gak-a" || got[1] != "gak-b" {
			t.Errorf("получено %#v — состав или порядок искажены", got)
		}
	})
}

// TestApplyInstance_AbsentOptionalsStayNull — необязательные ссылки, которых край
// не вернул, остаются null.
//
// Записав в них пустую строку, мы объявили бы «пользователь задал пустую
// группу», и план расходился бы после каждого применения.
func TestApplyInstance_AbsentOptionalsStayNull(t *testing.T) {
	ctx := context.Background()

	t.Run("край не вернул группу — атрибут остаётся null", func(t *testing.T) {
		var m computeInstanceModel
		if err := applyInstance(ctx, &m, []byte(`{"id":"ins-1","name":"м","bootSource":{"type":"IMAGE","id":"img-1"}}`)); err != nil {
			t.Fatalf("ответ края не разобран: %v", err)
		}
		if !m.PlacementGroupID.IsNull() {
			t.Errorf("группа = %v, ожидался null", m.PlacementGroupID)
		}
		if !m.ServiceAccountID.IsNull() {
			t.Errorf("учётка = %v, ожидался null", m.ServiceAccountID)
		}
	})

	// Положительный контроль: возвращённые краем значения ДОЕЗЖАЮТ. Без него
	// проба выше зеленела бы на реализации, не пишущей эти поля никогда.
	t.Run("край вернул значения — они доезжают", func(t *testing.T) {
		var m computeInstanceModel
		err := applyInstance(ctx, &m, []byte(
			`{"id":"ins-1","name":"м","bootSource":{"type":"IMAGE","id":"img-1"},"placementGroupId":"plg-x","serviceAccount":{"id":"sva-y"}}`))
		if err != nil {
			t.Fatalf("ответ края не разобран: %v", err)
		}
		if m.PlacementGroupID.ValueString() != "plg-x" {
			t.Errorf("группа = %v, ожидалась plg-x", m.PlacementGroupID)
		}
		if m.ServiceAccountID.ValueString() != "sva-y" {
			t.Errorf("учётка = %v, ожидалась sva-y", m.ServiceAccountID)
		}
	})

	t.Run("набор ключей приходит списком, а не null", func(t *testing.T) {
		var m computeInstanceModel
		if err := applyInstance(ctx, &m, []byte(`{"id":"ins-1","name":"м","bootSource":{"type":"IMAGE","id":"img-1"}}`)); err != nil {
			t.Fatalf("ответ края не разобран: %v", err)
		}
		if m.GuestAccessKeyIDs.IsNull() {
			t.Error("набор ключей null — край всегда возвращает массив, и null дал бы " +
				"расхождение плана на каждом применении")
		}
	})

	// Положительный контроль к предыдущей: набор, названный краем, доезжает
	// составом. Без него «не null» зеленело бы на реализации, всегда кладущей пустой.
	t.Run("названный краем набор доезжает составом", func(t *testing.T) {
		var m computeInstanceModel
		err := applyInstance(ctx, &m, []byte(
			`{"id":"ins-1","name":"м","bootSource":{"type":"IMAGE","id":"img-1"},"guestAccessKeyIds":["gak-a","gak-b"]}`))
		if err != nil {
			t.Fatalf("ответ края не разобран: %v", err)
		}
		if got := stringsFromList(ctx, m.GuestAccessKeyIDs); len(got) != 2 ||
			got[0] != "gak-a" || got[1] != "gak-b" {
			t.Errorf("набор = %#v — состав или порядок искажены", got)
		}
	})
}
