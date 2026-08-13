// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// flexInt64 — 64-битное целое, приходящее с края ЛИБО числом, ЛИБО строкой.
//
// Это не перестраховка. JSON не выражает 64-битное целое без потери точности,
// поэтому кодировщик protobuf отдаёт такие поля СТРОКОЙ ("8192"), и объявленное
// как int64 поле молча получило бы ноль на каждом размере: разбор упал бы, а
// ошибку разбора страницы легко списать на пустой каталог.
//
// Обе формы принимаются намеренно: форма зависит от настройки кодировщика на
// крае, а она — не наш контракт. Читать оба варианта стоит одной функции;
// угадать неверно стоит всех значений сразу.
type flexInt64 int64

// UnmarshalJSON принимает число, строку и null (null — это ноль, а не отказ:
// незаполненное краем поле означает «не объявлено»).
func (v *flexInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*v = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*v = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*v = flexInt64(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*v = flexInt64(n)
	return nil
}

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
// Значения, которых край не эхает (create_default_security_group), НЕ трогаются: их
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
