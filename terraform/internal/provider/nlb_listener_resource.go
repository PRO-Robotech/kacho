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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	nlbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const listenersPath = "/nlb/v1/listeners"

// Слушатель: порт балансировщика и группа целей, в которую он раздаёт.
type listenerModel struct {
	ID                  types.String `tfsdk:"id"`
	ProjectID           types.String `tfsdk:"project_id"`
	LoadBalancerID      types.String `tfsdk:"load_balancer_id"`
	Name                types.String `tfsdk:"name"`
	Description         types.String `tfsdk:"description"`
	Labels              types.Map    `tfsdk:"labels"`
	Protocol            types.String `tfsdk:"protocol"`
	Port                types.Int64  `tfsdk:"port"`
	TargetGroupID       types.String `tfsdk:"target_group_id"`
	ResolvedBackendPort types.Int64  `tfsdk:"resolved_backend_port"`
	Status              types.String `tfsdk:"status"`
	Substatus           types.String `tfsdk:"substatus"`
	CreatedAt           types.String `tfsdk:"created_at"`
}

type listenerResource struct{ c *client.Client }

// NewNLBListenerResource — конструктор для реестра провайдера.
func NewNLBListenerResource() resource.Resource { return &listenerResource{} }

func (r *listenerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameNLBListener
}

func (r *listenerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *listenerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Слушатель балансировщика: порт, протокол и группа целей.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"load_balancer_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Балансировщик-владелец. Изменение пересоздаёт слушателя."},
			"project_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Проект. Наследуется от балансировщика, задавать его нельзя."},
			"name": schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString("")},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},

			"protocol": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "`TCP` или `UDP`. Изменение пересоздаёт слушателя: край " +
					"протокол не меняет."},
			"port": schema.Int64Attribute{Required: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				MarkdownDescription: "Порт, который слушает балансировщик, `1`–`65535`. " +
					"Изменение пересоздаёт слушателя."},
			"target_group_id": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Группа целей. Обязана быть в том же регионе, что и " +
					"балансировщик, — иначе край откажет."},

			// Backend-порта у слушателя нет и задавать его нечем: величина живёт на
			// группе целей, а здесь видна эхом. Прежде рядом стоял `target_port` —
			// поле, объявленное необязательным и отвергавшее единственную форму «не
			// задавал», поэтому провайдер требовал его явно. Край снял поле с
			// контракта (kacho#231), и требование ушло вместе с ним.
			"resolved_backend_port": schema.Int64Attribute{Computed: true,
				MarkdownDescription: "Порт, на который трафик уходит в цели, — эхо `port` " +
					"привязанной группы целей. Нужен другой — привяжите другую группу."},
			"status": schema.StringAttribute{Computed: true},
			"substatus": schema.StringAttribute{Computed: true,
				MarkdownDescription: "`OK` или `MISCONFIGURED` — второе означает, что слушатель " +
					"создан, но раздавать ему некуда."},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

type listenerWire struct {
	ID                  string            `json:"id"`
	ProjectID           string            `json:"projectId"`
	LoadBalancerID      string            `json:"loadBalancerId"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Labels              map[string]string `json:"labels"`
	Protocol            string            `json:"protocol"`
	Port                any               `json:"port"`
	TargetGroupID       string            `json:"targetGroupId"`
	ResolvedBackendPort any               `json:"resolvedBackendPort"`
	Status              string            `json:"status"`
	Substatus           string            `json:"substatus"`
	CreatedAt           string            `json:"createdAt"`
}

func applyListener(ctx context.Context, m *listenerModel, raw []byte) error {
	var w listenerWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.LoadBalancerID = types.StringValue(w.LoadBalancerID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.Protocol = types.StringValue(w.Protocol)
	m.Port = types.Int64Value(numOf(w.Port))
	m.TargetGroupID = types.StringValue(w.TargetGroupID)
	m.ResolvedBackendPort = types.Int64Value(numOf(w.ResolvedBackendPort))
	m.Status = types.StringValue(w.Status)
	m.Substatus = types.StringValue(w.Substatus)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	return nil
}

func (r *listenerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listenerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &nlbv1.CreateListenerRequest{
		LoadBalancerId: plan.LoadBalancerID.ValueString(),
		Name:           plan.Name.ValueString(),
		Description:    plan.Description.ValueString(),
		Labels:         mapFromTF(ctx, plan.Labels),
		Protocol:       nlbv1.Listener_Protocol(enumOf(plan.Protocol, nlbv1.Listener_Protocol_value)),
		Port:           plan.Port.ValueInt64(),
		TargetGroupId:  plan.TargetGroupID.ValueString(),
	}

	id, err := awaitCreate(ctx, r.c, listenersPath, "listenerId", typeNameNLBListener,
		plan.LoadBalancerID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание слушателя не завершилось", err.Error())
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

	raw, err := readByID(ctx, r.c, listenersPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Слушатель создан, но не прочитан обратно", err.Error())
		return
	}
	if err := applyListener(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listenerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listenerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := readByID(ctx, r.c, listenersPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyListener(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение слушателя не удалось", err.Error())
		return
	}
	// Область подтверждения — ПРОЕКТ: списочный запрос слушателей требует именно его
	// (`load_balancer_id` в нём необязателен). Спрашивать не в той области значит получить
	// отказ и принять его за отсутствие.
	remove, title, detail := absenceDiagnostics(ctx, r.c, listenersPath, client.ScopeProject,
		"Слушатель", state.ID.ValueString(), state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *listenerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listenerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &nlbv1.UpdateListenerRequest{ListenerId: state.ID.ValueString()}
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
	if !plan.TargetGroupID.Equal(state.TargetGroupID) {
		body.TargetGroupId = plan.TargetGroupID.ValueString()
		paths = append(paths, "target_group_id")
	}

	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			listenersPath+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение слушателя не завершилось", err.Error())
			return
		}
	}

	raw, err := readByID(ctx, r.c, listenersPath, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Слушатель изменён, но не прочитан обратно", err.Error())
		return
	}
	plan.ID = state.ID
	if err := applyListener(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listenerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state listenerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		listenersPath+"/"+state.ID.ValueString(), nil); err != nil {
		resp.Diagnostics.AddError("Удаление слушателя не завершилось", err.Error())
		return
	}
}

func (r *listenerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "слушатель", ids.PrefixListener, req, resp)
}
