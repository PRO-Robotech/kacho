// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// mapFromTF переводит карту меток в форму запроса. Неизвестное и пустое дают nil: край
// отличает «поле не задано» от «задано пустым», и слать пустую карту вместо отсутствия
// значило бы стирать метки при каждом создании.
func mapFromTF(ctx context.Context, m types.Map) map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := make(map[string]string, len(m.Elements()))
	_ = m.ElementsAs(ctx, &out, false)
	if len(out) == 0 {
		return nil
	}
	return out
}

// listFromStrings — список строк в значение Terraform. nil и пустой дают ПУСТОЙ список, а
// не null: край всегда возвращает массив, и null здесь означал бы расхождение на каждом
// плане.
func listFromStrings(ctx context.Context, in []string) types.List {
	if in == nil {
		in = []string{}
	}
	v, diags := types.ListValueFrom(ctx, types.StringType, in)
	if diags.HasError() {
		return types.ListNull(types.StringType)
	}
	return v
}

// mapToTF — карта меток из ответа края.
func mapToTF(ctx context.Context, in map[string]string) types.Map {
	if in == nil {
		in = map[string]string{}
	}
	v, diags := types.MapValueFrom(ctx, types.StringType, in)
	if diags.HasError() {
		return types.MapNull(types.StringType)
	}
	return v
}

// applyNetwork переносит ответ края в состояние.
//
// Значения, которых край не эхает, НЕ трогаются: их
// подстановка нулём означала бы «пользователь задал false» и приводила к пересозданию
// ресурса на следующем же плане.
func applyNetwork(ctx context.Context, m *networkModel, n *networkJSON) {
	m.ID = types.StringValue(n.ID)
	m.ProjectID = types.StringValue(n.ProjectID)
	m.Name = types.StringValue(n.Name)
	m.Description = types.StringValue(n.Description)
	m.Labels = mapToTF(ctx, n.Labels)
	m.CreatedAt = types.StringValue(n.CreatedAt)
	m.DefaultSecurityGroupID = types.StringValue(n.DefaultSecurityGroupID)
	m.DefaultRouteTableID = types.StringValue(n.DefaultRouteTableID)
	m.IPv4CidrBlocks = listFromStrings(ctx, n.IPv4CidrBlocks)
	m.IPv6CidrBlocks = listFromStrings(ctx, n.IPv6CidrBlocks)
}

// stringsFromTF — список строк из значения Terraform. Неизвестное и null дают nil:
// «не задано» и «задано пустым» — разные вещи, и слать пустой массив вместо отсутствия
// значило бы стирать супернет при каждом создании.
func stringsFromTF(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	_ = l.ElementsAs(ctx, &out, false)
	return out
}

// diffSets — что добавить и что убрать, чтобы из have получилось want.
func diffSets(have, want []string) (add, remove []string) {
	inHave := map[string]bool{}
	for _, v := range have {
		inHave[v] = true
	}
	inWant := map[string]bool{}
	for _, v := range want {
		inWant[v] = true
	}
	for _, v := range want {
		if !inHave[v] {
			add = append(add, v)
		}
	}
	for _, v := range have {
		if !inWant[v] {
			remove = append(remove, v)
		}
	}
	return add, remove
}

// fieldMask — маска обновления из путей. Пустую маску провайдер не отправляет никогда,
// поэтому функция вызывается только на непустом наборе.
func fieldMask(paths []string) *fieldmaskpb.FieldMask {
	return &fieldmaskpb.FieldMask{Paths: paths}
}

// enumOf — числовое значение перечисления по его имени из сгенерённой таблицы.
//
// Таблица берётся у СГЕНЕРЁННОГО типа, а не переписывается: своя копия разошлась бы с
// контрактом молча, и опечатка в имени варианта проходила бы как «значение не задано».
// Неизвестное имя даёт ноль — «не задано», и край отвечает на него своим отказом с
// перечнем допустимых, что вызывающему полезнее нашей догадки.
func enumOf(v types.String, table map[string]int32) int32 {
	if v.IsNull() || v.IsUnknown() {
		return 0
	}
	return table[v.ValueString()]
}

// sealUnknowns заменяет ВСЕ неизвестные значения модели на null.
//
// Зачем. Идентификатор пишется в состояние сразу после создания — иначе сорвавшееся
// обратное чтение оставило бы созданный ресурс вне управления навсегда. Но план несёт
// неизвестные значения вычисляемых полей, а Terraform требует, чтобы после apply
// неизвестных не осталось НИ ОДНОГО: он отвечает на каждое отдельным «invalid result
// object», и настоящая причина — одна строка про неудавшееся чтение — тонет среди них.
//
// null здесь честнее неизвестного: значение действительно не получено. Следующий план
// увидит расхождение с настройкой и предложит его закрыть — то есть ресурс останется
// управляемым, а не потеряется.
//
// Обход рефлексией, а не по полю на ресурс: полей десятки, ресурсов шесть, и забытое
// поле давало бы ровно ту же ошибку, только реже и в чужой отладке.
func sealUnknowns(model any) {
	sealValue(reflect.ValueOf(model))
}

func sealValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			sealValue(v.Elem())
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			sealValue(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanSet() {
				continue
			}
			if sealed, ok := nullOfUnknown(f.Interface()); ok {
				f.Set(reflect.ValueOf(sealed))
				continue
			}
			sealValue(f)
		}
	}
}

// nullOfUnknown — null того же типа, если значение неизвестно.
//
// Перечисление типов явное: обобщённого «null этого типа» у attr.Value нет, а выдумывать
// его через рефлексию значило бы получить молчаливый промах на первом же новом типе.
// Второе значение отличает «заменили» от «не наш тип» — без него ветка обхода не знала бы,
// идти ли внутрь.
func nullOfUnknown(x any) (any, bool) {
	switch v := x.(type) {
	case types.String:
		if v.IsUnknown() {
			return types.StringNull(), true
		}
		return nil, false
	case types.Int64:
		if v.IsUnknown() {
			return types.Int64Null(), true
		}
		return nil, false
	case types.Bool:
		if v.IsUnknown() {
			return types.BoolNull(), true
		}
		return nil, false
	case types.Map:
		if v.IsUnknown() {
			return types.MapNull(v.ElementType(context.Background())), true
		}
		return nil, false
	case types.List:
		if v.IsUnknown() {
			return types.ListNull(v.ElementType(context.Background())), true
		}
		return nil, false
	case types.Set:
		if v.IsUnknown() {
			return types.SetNull(v.ElementType(context.Background())), true
		}
		return nil, false
	}
	return nil, false
}

// Пересоздающие модификаторы для типов, у которых их нет в стандартном наборе фреймворка.
//
// Заведены не «для полноты»: у ключа служебной учётки КАЖДОЕ поле входа пересоздаёт
// ресурс, потому что изменяющей операции у выпущенного ключа не существует. Без этих
// модификаторов правка метки или срока прошла бы как изменение — и молча не применилась.
type mapRequiresReplace struct{}

func (mapRequiresReplace) Description(context.Context) string {
	return "изменение пересоздаёт ресурс"
}
func (m mapRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}
func (mapRequiresReplace) PlanModifyMap(_ context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

type listRequiresReplace struct{}

func (listRequiresReplace) Description(context.Context) string {
	return "изменение пересоздаёт ресурс"
}
func (l listRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return l.Description(ctx)
}
func (listRequiresReplace) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

type int64RequiresReplace struct{}

func (int64RequiresReplace) Description(context.Context) string {
	return "изменение пересоздаёт ресурс"
}
func (i int64RequiresReplace) MarkdownDescription(ctx context.Context) string {
	return i.Description(ctx)
}
func (int64RequiresReplace) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

type boolRequiresReplace struct{}

func (boolRequiresReplace) Description(context.Context) string {
	return "изменение пересоздаёт ресурс"
}
func (b boolRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return b.Description(ctx)
}
func (boolRequiresReplace) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

// stringsFromList — значение Terraform в набор строк.
//
// Неизвестное и null дают nil, а НЕ пустой набор: пустой набор — это осмысленное
// намерение («снять все»), и подставить его вместо «пользователь не сказал»
// значило бы отправить краю команду, которой не было. Разница видна ровно на
// наборе ключей входа, где пустой набор означает снятие доступа.
func stringsFromList(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	if diags := l.ElementsAs(ctx, &out, false); diags.HasError() {
		return nil
	}
	return out
}
