// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
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
