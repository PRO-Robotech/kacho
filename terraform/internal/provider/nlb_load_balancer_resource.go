// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	nlbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const loadBalancersPath = "/nlb/v1/networkLoadBalancers"

// Балансировщик. Слушатели — ОТДЕЛЬНЫЕ ресурсы, ссылающиеся на него: контракт создания
// поля со списком слушателей не несёт вовсе (номер зарезервирован), поэтому описывать их
// здесь было бы обещанием возможности, которой нет.
type loadBalancerModel struct {
	ID                    types.String `tfsdk:"id"`
	ProjectID             types.String `tfsdk:"project_id"`
	RegionID              types.String `tfsdk:"region_id"`
	Name                  types.String `tfsdk:"name"`
	Description           types.String `tfsdk:"description"`
	Labels                types.Map    `tfsdk:"labels"`
	Placement             types.String `tfsdk:"placement"`
	SessionAffinity       types.String `tfsdk:"session_affinity"`
	AdminState            types.String `tfsdk:"admin_state"`
	DeletionProtection    types.Bool   `tfsdk:"deletion_protection"`
	CrossZoneEnabled      types.Bool   `tfsdk:"cross_zone_enabled"`
	SecurityGroupIDs      types.List   `tfsdk:"security_group_ids"`
	DisabledAnnounceZones types.List   `tfsdk:"disabled_announce_zones"`
	V4Source              types.Object `tfsdk:"v4_source"`
	V6Source              types.Object `tfsdk:"v6_source"`
	V4AddressID           types.String `tfsdk:"v4_address_id"`
	V6AddressID           types.String `tfsdk:"v6_address_id"`
	Type                  types.String `tfsdk:"type"`
	Status                types.String `tfsdk:"status"`
	CreatedAt             types.String `tfsdk:"created_at"`
}

// vipSourceModel — ОТКУДА берётся адрес. Это ВВОД, и он отделён от вывода намеренно.
//
// Прежде одно имя `v4_address_id` служило и тем, и другим: пользователь мог задать свой
// адрес, а край возвращал в него выделенный. Перегрузка стоила пересоздания балансировщика
// на КАЖДОЙ правке меток: вычисляемое значение планируется неизвестным, а «изменение
// требует замены» видит неизвестное как другое значение. Ввод и вывод — разные предметы,
// у них разные имена.
type vipSourceModel struct {
	AddressID types.String `tfsdk:"address_id"`
	SubnetID  types.String `tfsdk:"subnet_id"`
}

// vipSourceOf — источник из значения Terraform.
//
// Поле модели объявлено types.Object, а НЕ указателем на структуру, и это не стиль:
// указатель неспособен держать НЕИЗВЕСТНОЕ значение, а источник адреса ссылается на
// подсеть, которой в момент плана ещё нет. Провайдер отвечал на это «Value Conversion
// Error… target type cannot handle unknown values» — отказом, из которого вызывающий не
// узнаёт ни поля, ни причины.
func vipSourceOf(ctx context.Context, o types.Object) *vipSourceModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m vipSourceModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

type loadBalancerResource struct{ c *client.Client }

// NewNLBLoadBalancerResource — конструктор для реестра провайдера.
func NewNLBLoadBalancerResource() resource.Resource { return &loadBalancerResource{} }

func (r *loadBalancerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameNLBLoadBalancer
}

func (r *loadBalancerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Внутренняя ошибка провайдера",
			fmt.Sprintf("ожидался *client.Client, получено %T", req.ProviderData))
		return
	}
	r.c = c
}

func (r *loadBalancerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Сетевой балансировщик: где он стоит, откуда берёт адрес и кому " +
			"разрешено к нему обращаться. Слушатели заводятся отдельными ресурсами.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"region_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Регион. Группы целей, на которые сошлются слушатели, обязаны " +
					"быть в этом же регионе."},
			"name": schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString("")},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},

			"placement": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Посадка: `EXTERNAL_REGIONAL`, `INTERNAL_REGIONAL` или " +
					"`INTERNAL_ZONAL`. Задаёт и обращённость наружу, и зональность — отдельных " +
					"переключателей для них нет."},
			"session_affinity": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Как соединения закрепляются за целью: `FIVE_TUPLE` или " +
					"`CLIENT_IP_ONLY`."},
			"admin_state": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "`ADMIN_STATE_ENABLED` или `ADMIN_STATE_DISABLED` — " +
					"объявляет ли балансировщик свой адрес."},
			"deletion_protection": schema.BoolAttribute{Optional: true, Computed: true,
				Default: booldefault.StaticBool(false),
				MarkdownDescription: "Защита от удаления.\n\n" +
					":::warning Она остановит `terraform destroy`\n" +
					"Край отвергнет удаление, пока защита включена. Снимите её отдельным " +
					"apply — провайдер НЕ снимает её сам: тихо обходить защиту от удаления " +
					"значит делать её бессмысленной.\n:::"},
			"cross_zone_enabled": schema.BoolAttribute{Optional: true, Computed: true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Разрешено ли слать трафик в цели чужих зон."},
			"security_group_ids": schema.ListAttribute{Optional: true, Computed: true,
				ElementType:         types.StringType,
				MarkdownDescription: "Группы безопасности, применяемые к трафику балансировщика."},
			"disabled_announce_zones": schema.ListAttribute{Optional: true, Computed: true,
				ElementType:         types.StringType,
				MarkdownDescription: "Зоны, в которых адрес не объявляется."},

			"v4_source": vipSourceAttr("IPv4"),
			"v6_source": vipSourceAttr("IPv6"),

			"v4_address_id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Выделенный адрес IPv4 — ВЫВОД. Задаётся он через " +
					"`v4_source`."},
			"v6_address_id": schema.StringAttribute{Computed: true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Выделенный адрес IPv6 — ВЫВОД. Задаётся он через `v6_source`."},

			"type": schema.StringAttribute{Computed: true,
				MarkdownDescription: "`EXTERNAL` или `INTERNAL` — выводится краем из посадки."},
			"status":     schema.StringAttribute{Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

// vipSourceAttr — источник адреса одного семейства. Целиком пересоздающий: сменить адрес
// у живого балансировщика край не даёт.
func vipSourceAttr(family string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Optional: true,
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		MarkdownDescription: "Откуда взять адрес " + family + ": ровно одно из " +
			"`subnet_id` (край выделит адрес сам) или `address_id` (готовый адрес). " +
			"Выделенный адрес виден в выводе.",
		Attributes: map[string]schema.Attribute{
			"subnet_id":  schema.StringAttribute{Optional: true},
			"address_id": schema.StringAttribute{Optional: true},
		}}
}

// ValidateConfig ловит то, что край отверг бы уже после отправки: внутри источника ровно
// одно поле.
func (r *loadBalancerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg loadBalancerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, p := range []struct {
		family string
		src    *vipSourceModel
		attr   string
	}{
		{"IPv4", vipSourceOf(ctx, cfg.V4Source), "v4_source"},
		{"IPv6", vipSourceOf(ctx, cfg.V6Source), "v6_source"},
	} {
		if p.src == nil {
			continue
		}
		// Неизвестное считается ЗАДАННЫМ: ссылка на ещё не созданную подсеть приходит
		// именно неизвестной, и счесть её пустой значило бы отвергнуть верную настройку.
		n := 0
		if isSet(p.src.SubnetID) {
			n++
		}
		if isSet(p.src.AddressID) {
			n++
		}
		if n != 1 {
			resp.Diagnostics.AddError("Источник адреса "+p.family+" задан не однозначно",
				fmt.Sprintf("В %s задано полей: %d. Ровно одно из subnet_id и address_id "+
					"обязано присутствовать.", p.attr, n))
		}
	}
}

func vipSource(src *vipSourceModel) *nlbv1.VipSource {
	if src == nil {
		return nil
	}
	switch {
	case isSet(src.SubnetID):
		return &nlbv1.VipSource{Source: &nlbv1.VipSource_SubnetId{SubnetId: src.SubnetID.ValueString()}}
	case isSet(src.AddressID):
		return &nlbv1.VipSource{Source: &nlbv1.VipSource_AddressId{AddressId: src.AddressID.ValueString()}}
	}
	return nil
}

type lbWire struct {
	ID                    string            `json:"id"`
	ProjectID             string            `json:"projectId"`
	RegionID              string            `json:"regionId"`
	Name                  string            `json:"name"`
	Description           string            `json:"description"`
	Labels                map[string]string `json:"labels"`
	Placement             string            `json:"placement"`
	SessionAffinity       string            `json:"sessionAffinity"`
	AdminState            string            `json:"adminState"`
	DeletionProtection    bool              `json:"deletionProtection"`
	CrossZoneEnabled      bool              `json:"crossZoneEnabled"`
	SecurityGroupIDs      []string          `json:"securityGroupIds"`
	DisabledAnnounceZones []string          `json:"disabledAnnounceZones"`
	V4AddressID           string            `json:"v4AddressId"`
	V6AddressID           string            `json:"v6AddressId"`
	Type                  string            `json:"type"`
	Status                string            `json:"status"`
	CreatedAt             string            `json:"createdAt"`
}

// applyLoadBalancer раскладывает ответ края.
//
// Источники адреса (`v4_source`/`v6_source`) НЕ трогаются: край их не эхает — он
// возвращает уже ВЫДЕЛЕННЫЙ адрес. Затереть их прочитанным значило бы объявить, что
// пользователь источник не задавал, и предложить пересоздание на следующем плане.
func applyLoadBalancer(ctx context.Context, m *loadBalancerModel, raw []byte) error {
	var w lbWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.RegionID = types.StringValue(w.RegionID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.Placement = types.StringValue(w.Placement)
	m.SessionAffinity = types.StringValue(w.SessionAffinity)
	m.AdminState = types.StringValue(w.AdminState)
	m.DeletionProtection = types.BoolValue(w.DeletionProtection)
	m.CrossZoneEnabled = types.BoolValue(w.CrossZoneEnabled)
	m.SecurityGroupIDs = listFromStrings(ctx, w.SecurityGroupIDs)
	m.DisabledAnnounceZones = listFromStrings(ctx, w.DisabledAnnounceZones)
	m.V4AddressID = types.StringValue(w.V4AddressID)
	m.V6AddressID = types.StringValue(w.V6AddressID)
	m.Type = types.StringValue(w.Type)
	m.Status = types.StringValue(w.Status)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	return nil
}

func (r *loadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &nlbv1.CreateNetworkLoadBalancerRequest{
		ProjectId:             plan.ProjectID.ValueString(),
		RegionId:              plan.RegionID.ValueString(),
		Name:                  plan.Name.ValueString(),
		Description:           plan.Description.ValueString(),
		Labels:                mapFromTF(ctx, plan.Labels),
		Placement:             nlbv1.NetworkLoadBalancer_Placement(enumOf(plan.Placement, nlbv1.NetworkLoadBalancer_Placement_value)),
		SessionAffinity:       nlbv1.NetworkLoadBalancer_SessionAffinity(enumOf(plan.SessionAffinity, nlbv1.NetworkLoadBalancer_SessionAffinity_value)),
		AdminState:            nlbv1.NetworkLoadBalancer_AdminState(enumOf(plan.AdminState, nlbv1.NetworkLoadBalancer_AdminState_value)),
		DeletionProtection:    plan.DeletionProtection.ValueBool(),
		CrossZoneEnabled:      plan.CrossZoneEnabled.ValueBool(),
		SecurityGroupIds:      stringsFromTF(ctx, plan.SecurityGroupIDs),
		DisabledAnnounceZones: stringsFromTF(ctx, plan.DisabledAnnounceZones),
		V4Source:              vipSource(vipSourceOf(ctx, plan.V4Source)),
		V6Source:              vipSource(vipSourceOf(ctx, plan.V6Source)),
	}

	id, err := awaitCreate(ctx, r.c, loadBalancersPath, "networkLoadBalancerId", typeNameNLBLoadBalancer,
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание балансировщика не завершилось", err.Error())
		return
	}

	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт шесть сообщений об
	// «invalid result object» вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, loadBalancersPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Балансировщик создан, но не прочитан обратно", err.Error())
		return
	}
	if err := applyLoadBalancer(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := readByID(ctx, r.c, loadBalancersPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyLoadBalancer(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение балансировщика не удалось", err.Error())
		return
	}
	remove, title, detail := absenceDiagnostics(ctx, r.c, loadBalancersPath, client.ScopeProject,
		"Балансировщик", state.ID.ValueString(), state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *loadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &nlbv1.UpdateNetworkLoadBalancerRequest{NetworkLoadBalancerId: state.ID.ValueString()}
	var paths []string

	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueString()
		paths = append(paths, "name")
	}
	if !plan.Description.Equal(state.Description) {
		body.Description = plan.Description.ValueString()
		paths = append(paths, "description")
	}
	if !plan.Labels.Equal(state.Labels) {
		body.Labels = mapFromTF(ctx, plan.Labels)
		paths = append(paths, "labels")
	}
	if !plan.DeletionProtection.Equal(state.DeletionProtection) {
		body.DeletionProtection = plan.DeletionProtection.ValueBool()
		paths = append(paths, "deletion_protection")
	}
	if !plan.CrossZoneEnabled.Equal(state.CrossZoneEnabled) {
		body.CrossZoneEnabled = plan.CrossZoneEnabled.ValueBool()
		paths = append(paths, "cross_zone_enabled")
	}
	if !plan.SessionAffinity.Equal(state.SessionAffinity) {
		body.SessionAffinity = nlbv1.NetworkLoadBalancer_SessionAffinity(
			enumOf(plan.SessionAffinity, nlbv1.NetworkLoadBalancer_SessionAffinity_value))
		paths = append(paths, "session_affinity")
	}
	if !plan.AdminState.Equal(state.AdminState) {
		body.AdminState = nlbv1.NetworkLoadBalancer_AdminState(
			enumOf(plan.AdminState, nlbv1.NetworkLoadBalancer_AdminState_value))
		paths = append(paths, "admin_state")
	}
	if !plan.SecurityGroupIDs.Equal(state.SecurityGroupIDs) {
		body.SecurityGroupIds = stringsFromTF(ctx, plan.SecurityGroupIDs)
		paths = append(paths, "security_group_ids")
	}
	if !plan.DisabledAnnounceZones.Equal(state.DisabledAnnounceZones) {
		body.DisabledAnnounceZones = stringsFromTF(ctx, plan.DisabledAnnounceZones)
		paths = append(paths, "disabled_announce_zones")
	}

	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			loadBalancersPath+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение балансировщика не завершилось", err.Error())
			return
		}
	}

	raw, err := readByID(ctx, r.c, loadBalancersPath, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Балансировщик изменён, но не прочитан обратно", err.Error())
		return
	}
	plan.ID = state.ID
	if err := applyLoadBalancer(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		loadBalancersPath+"/"+state.ID.ValueString(), nil); err != nil {
		detail := err.Error()
		if state.DeletionProtection.ValueBool() {
			detail += "\n\nУ этого балансировщика включена защита от удаления. Снимите её " +
				"отдельным apply (deletion_protection = false) и повторите: провайдер не " +
				"снимает её сам — тихо обойдённая защита от удаления бессмысленна."
		}
		resp.Diagnostics.AddError("Удаление балансировщика не завершилось", detail)
		return
	}
}

func (r *loadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "балансировщик", ids.PrefixLoadBalancer, req, resp)
}
