// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// sgRuleAttrs — атрибуты ОДНОГО правила в схеме группы безопасности.
func sgRuleAttrs(t *testing.T) map[string]schema.Attribute {
	t.Helper()
	var resp resource.SchemaResponse
	NewVPCSecurityGroupResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("схема группы безопасности не собралась: %v", resp.Diagnostics.Errors())
	}
	rules, ok := resp.Schema.Attributes["rules"].(schema.SetNestedAttribute)
	if !ok {
		t.Fatalf("rules не набор вложенных объектов: %T", resp.Schema.Attributes["rules"])
	}
	return rules.NestedObject.Attributes
}

// Правило умеет назвать целью ИМЕНОВАННЫЙ НАБОР — третью ветвь цели.
//
// Без этого набор в провайдере заведён и бесполезен: сослаться на него из правила,
// описанного той же конфигурацией, нечем, и перечень пришлось бы по-прежнему копировать в
// каждое правило — то есть ровно то, ради устранения чего ресурс и существует.
func TestSecurityGroupRuleCanTargetACidrGroup(t *testing.T) {
	ctx := context.Background()

	// Предпосылка проверяется здесь же, на обеих сторонах контракта: ветвь есть в запросе
	// (её можно задать) и есть в проекции чтения (она приезжает обратно и удержится в
	// состоянии).
	if !hasContractField(&vpcv1.SecurityGroupRuleSpec{}, "cidr_group_id") {
		t.Fatal("предпосылка устарела: в контракте правила нет ветви cidr_group_id")
	}
	if !hasContractField(&vpcv1.SecurityGroupRule{}, "cidr_group_id") {
		t.Fatal("предпосылка устарела: проекция чтения правила не несёт cidr_group_id — " +
			"заданная цель не удержалась бы в состоянии")
	}

	attrs := sgRuleAttrs(t)
	a, ok := attrs["cidr_group_id"]
	if !ok {
		t.Fatal("схема правила не выставляет cidr_group_id: цель, которую край принимает, " +
			"конфигурацией не выражается")
	}
	if !a.IsOptional() {
		t.Error("cidr_group_id объявлен не задаваемым — цель нельзя назвать")
	}

	// Перевод в запрос выбирает ИМЕННО эту ветвь.
	m := sgRuleModel{
		Direction:       types.StringValue("INGRESS"),
		Ports:           types.ObjectNull(sgPortsObjectType().AttrTypes),
		CidrBlocks:      types.ObjectNull(sgCidrObjectType().AttrTypes),
		SecurityGroupID: types.StringNull(),
		CidrGroupID:     types.StringValue("cdg-0123456789abcdefg"),
	}
	spec, err := m.toProto(ctx, "rules[0]")
	if err != nil {
		t.Fatalf("правило с набором не собралось в запрос: %v", err)
	}
	arm, ok := spec.GetTarget().(*vpcv1.SecurityGroupRuleSpec_CidrGroupId)
	if !ok {
		t.Fatalf("выбрана не та ветвь цели: %T", spec.GetTarget())
	}
	if arm.CidrGroupId != "cdg-0123456789abcdefg" {
		t.Errorf("идентификатор набора не доехал: %q", arm.CidrGroupId)
	}

	// Парный положительный контроль: прежние ветви не сломаны. Без него проба выше
	// зеленела бы на переводе, который отправляет набор ВМЕСТО чего угодно.
	blocks, diags := types.ObjectValueFrom(ctx, sgCidrObjectType().AttrTypes, sgCidrModel{
		V4CidrBlocks: listFromStrings(ctx, []string{"203.0.113.0/24"}),
		V6CidrBlocks: types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("блоки не собрались: %v", diags.Errors())
	}
	m2 := m
	m2.CidrGroupID = types.StringNull()
	m2.CidrBlocks = blocks
	spec2, err := m2.toProto(ctx, "rules[0]")
	if err != nil {
		t.Fatalf("правило с блоками не собралось в запрос: %v", err)
	}
	if _, ok := spec2.GetTarget().(*vpcv1.SecurityGroupRuleSpec_CidrBlocks); !ok {
		t.Fatalf("ветвь блоков потеряна: %T", spec2.GetTarget())
	}
}

// Цель по-прежнему РОВНО ОДНА — теперь из трёх.
//
// Аннотация «ровно одна» рантаймом не читается, поэтому правило с двумя целями край
// принимает молча и сохраняет с одной из них: пользователь получил бы правило, которого не
// писал. Проверка провайдера — единственный барьер, и она обязана считать новую ветвь.
func TestSecurityGroupRuleTargetCountIncludesTheCidrGroup(t *testing.T) {
	ctx := context.Background()
	base := sgRuleModel{
		Direction:       types.StringValue("INGRESS"),
		Ports:           types.ObjectNull(sgPortsObjectType().AttrTypes),
		CidrBlocks:      types.ObjectNull(sgCidrObjectType().AttrTypes),
		SecurityGroupID: types.StringNull(),
		CidrGroupID:     types.StringNull(),
	}

	only := base
	only.CidrGroupID = types.StringValue("cdg-0123456789abcdefg")
	var resp resource.ValidateConfigResponse
	validateSGRuleTarget(ctx, &resp, "rules[0]", only)
	if resp.Diagnostics.HasError() {
		t.Errorf("правило с единственной целью-набором отвергнуто: %v", resp.Diagnostics.Errors())
	}

	both := only
	both.SecurityGroupID = types.StringValue("sgr01234567890abcde")
	resp = resource.ValidateConfigResponse{}
	validateSGRuleTarget(ctx, &resp, "rules[0]", both)
	if !resp.Diagnostics.HasError() {
		t.Error("правило с двумя целями принято: край сохранил бы одну, и правило перестало " +
			"бы быть тем, что написал вызывающий")
	}
}

// Цель-набор доезжает ОБРАТНО и различает правила.
//
// Потеряй разбор ответа эту ветвь — правило в состоянии осталось бы без цели: план объявлял
// бы вечное расхождение, а сохранение написания вызывающего склеило бы два РАЗНЫХ правила,
// отличающихся только набором.
func TestSecurityGroupRuleCidrGroupSurvivesTheRoundTrip(t *testing.T) {
	ctx := context.Background()

	w := sgRuleWire{Direction: "INGRESS", CidrGroupID: "cdg-0123456789abcdefg"}
	m, err := w.toModel(ctx)
	if err != nil {
		t.Fatalf("разбор правила: %v", err)
	}
	if m.CidrGroupID.ValueString() != "cdg-0123456789abcdefg" {
		t.Fatalf("цель-набор потеряна при чтении: %v", m.CidrGroupID)
	}

	other := m
	other.CidrGroupID = types.StringValue("cdg-abcdefghjkmnpqrst")
	if sgRuleKey(ctx, m) == sgRuleKey(ctx, other) {
		t.Error("правила с РАЗНЫМИ наборами дали один ключ — написание вызывающего сохранилось " +
			"бы поверх изменившейся цели, и правка не доехала бы до состояния")
	}
	// Парный положительный: одинаковые правила дают один ключ, иначе каждый план
	// пересобирал бы набор правил на неизменной инфраструктуре.
	if sgRuleKey(ctx, m) != sgRuleKey(ctx, w.mustModel(t, ctx)) {
		t.Error("одинаковые правила дали разные ключи")
	}
}

func (w sgRuleWire) mustModel(t *testing.T, ctx context.Context) sgRuleModel {
	t.Helper()
	m, err := w.toModel(ctx)
	if err != nil {
		t.Fatalf("разбор правила: %v", err)
	}
	return m
}
