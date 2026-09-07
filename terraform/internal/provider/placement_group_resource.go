// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const placementGroupsPath = "/compute/v1/placementGroups"

type placementGroupModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Labels        types.Map    `tfsdk:"labels"`
	Strategy      types.String `tfsdk:"strategy"`
	PlacementType types.String `tfsdk:"placement_type"`
	ZoneID        types.String `tfsdk:"zone_id"`
	RegionID      types.String `tfsdk:"region_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

type placementGroupResource struct{ c *client.Client }

// NewPlacementGroupResource — конструктор для реестра провайдера.
func NewPlacementGroupResource() resource.Resource { return &placementGroupResource{} }

func (r *placementGroupResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameComputePlacementGroup
}

func (r *placementGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ConfigValidators закрывает то, что схема выразить не может: якорь размещения —
// РОВНО ОДНА координата.
//
// Проверка стоит здесь, а не только у края, ради МОМЕНТА: несочетаемый якорь
// виден на плане, до того как применение начнёт создавать что-либо ещё. Это
// проверка ФОРМЫ сообщения, а не решение о доступе, — политика остаётся за
// краем, и он же остаётся авторитетом: обе координаты пустые он отвергнет и без
// нас.
func (r *placementGroupResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		exactlyOneOf(
			path.Root("zone_id"),
			path.Root("region_id"),
		),
	}
}

func (r *placementGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Группа размещения — правило взаимного размещения машин: РАЗНЕСТИ их " +
			"(`SPREAD`, чтобы отказ одного куска железа не унёс всю группу) либо СБЛИЗИТЬ " +
			"(`PACK`, чтобы машины видели друг друга коротким путём).\n\n" +
			"Числа доменов отказа здесь нет намеренно: оно описывало бы раскладку железа, " +
			"а не намерение арендатора.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор группы.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт группу."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Человекочитаемое имя, уникальное в проекте. Косметическое."},
			"description": schema.StringAttribute{Optional: true, Computed: true},
			"labels":      schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"strategy": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Validators: []validator.String{oneOf("SPREAD", "PACK")},
				MarkdownDescription: "`SPREAD` — разнести, `PACK` — сблизить.\n\n" +
					"Изменение пересоздаёт группу: смена стратегии — это другое обещание " +
					"о размещении уже стоящих машин, и выдать его правкой поля значило бы " +
					"объявить исполненным то, что исполнено не будет."},
			"zone_id": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				MarkdownDescription: "Зона — для ЗОНАЛЬНОЙ группы (машины в одной зоне). " +
					"Задаётся ровно одна координата: `zone_id` ИЛИ `region_id`."},
			"region_id": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				MarkdownDescription: "Регион — для РЕГИОНАЛЬНОЙ группы (машины в одном регионе, зоны разные). " +
					"Задаётся ровно одна координата: `zone_id` ИЛИ `region_id`."},
			"placement_type": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Вид якоря (`ZONAL` / `REGIONAL`) — ВЫВОДИТСЯ из заданной координаты, " +
					"а не пишется отдельно.\n\n" +
					"Отдельное поле дало бы второй способ сказать то же самое, и первым же " +
					"следствием стала бы пара, описывающая размещение, которого не бывает " +
					"(`ZONAL` при заданном регионе). Один факт — одно место.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"created_at": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *placementGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan placementGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.CreatePlacementGroupRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
		Strategy:    computev1.PlacementGroup_Strategy(computev1.PlacementGroup_Strategy_value[plan.Strategy.ValueString()]),
		ZoneId:      plan.ZoneID.ValueString(),
		RegionId:    plan.RegionID.ValueString(),
	}
	// Вид якоря выводится из заданной координаты — здесь и нигде больше.
	if plan.ZoneID.ValueString() != "" {
		body.PlacementType = computev1.PlacementGroup_ZONAL
	} else {
		body.PlacementType = computev1.PlacementGroup_REGIONAL
	}

	raw, _ := json.Marshal(map[string]string{
		"projectId": body.ProjectId, "name": body.Name,
		"strategy": body.Strategy.String(), "placementType": body.PlacementType.String(),
		"zoneId": body.ZoneId, "regionId": body.RegionId,
	})
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameComputePlacementGroup, plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, placementGroupsPath, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание группы не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Край отверг создание группы", out.Message)
		return
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ на создание группы не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Создание группы не завершилось", err.Error())
		return
	}
	id, ok := done.MetadataString("placementGroupId")
	if !ok || id == "" {
		resp.Diagnostics.AddError("Край не сообщил идентификатор группы",
			"Операция успешна, но метаданные не содержат placementGroupId — записывать в состояние нечего.")
		return
	}

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	g, rerr := r.readPlacementGroup(ctx, id, true)
	if rerr != nil {
		resp.Diagnostics.AddError("Группа создана, но не прочитана обратно", rerr.Error())
		return
	}
	applyPlacementGroup(ctx, &plan, g)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *placementGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state placementGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.readPlacementGroup(ctx, state.ID.ValueString(), false)
	if err == nil {
		applyPlacementGroup(ctx, &state, g)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение группы не удалось", err.Error())
		return
	}

	verdict, verr := r.c.ConfirmAbsence(ctx, placementGroupsPath, client.ScopeProject,
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к проекту утрачен",
			"Группа "+state.ID.ValueString()+" не читается, и список проекта отвечает отказом. "+
				"Это событие прав, а не удаление.")
	default:
		msg := "Группа " + state.ID.ValueString() + " не найдена, и подтвердить отсутствие нечем: " +
			"в проекте не видно ни одной группы. Если она действительно удалена вне Terraform, " +
			"уберите её из состояния вручную:\n  terraform state rm <адрес ресурса>"
		if verr != nil {
			msg += "\n\nПодробности: " + verr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие группы не подтверждено", msg)
	}
}

func (r *placementGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state placementGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.UpdatePlacementGroupRequest{PlacementGroupId: state.ID.ValueString()}
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

	if len(paths) > 0 {
		body.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
		httpResp, err := r.c.Do(ctx, http.MethodPatch, placementGroupsPath+"/"+state.ID.ValueString(), body, nil)
		if err != nil {
			resp.Diagnostics.AddError("Изменение группы не отправлено", err.Error())
			return
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			resp.Diagnostics.AddError("Край отверг изменение группы", out.Message)
			return
		}
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Изменение группы не завершилось", err.Error())
				return
			}
		}
	}

	g, rerr := r.readPlacementGroup(ctx, state.ID.ValueString(), false)
	if rerr != nil {
		resp.Diagnostics.AddError("Группа изменена, но не прочитана обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	applyPlacementGroup(ctx, &plan, g)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *placementGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state placementGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	httpResp, err := r.c.Do(ctx, http.MethodDelete, placementGroupsPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление группы не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Удаление группы не завершилось", err.Error())
			}
		}
	case client.OutcomeNotFound:
		verdict, _ := r.c.ConfirmAbsence(ctx, placementGroupsPath, client.ScopeProject,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			resp.Diagnostics.AddError("Удаление группы не подтверждено",
				"Край ответил «не найдено», но отсутствие не подтверждено (исход: "+verdict.String()+").")
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление группы", out.Message)
	}
}

func (r *placementGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *placementGroupResource) readPlacementGroup(ctx context.Context, id string, retryAuthz bool) (*placementGroupJSON, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		httpResp, err := r.c.Do(ctx, http.MethodGet, placementGroupsPath+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(httpResp)
		switch out.Kind {
		case client.OutcomeOK:
			var g placementGroupJSON
			if err := json.Unmarshal(httpResp.Body, &g); err != nil {
				return nil, fmt.Errorf("разбор группы: %w", err)
			}
			return &g, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к группе %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение группы %s: %s", id, out.Message)
		}
	}
}

type placementGroupJSON struct {
	ID            string            `json:"id"`
	ProjectID     string            `json:"projectId"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Labels        map[string]string `json:"labels"`
	Strategy      string            `json:"strategy"`
	PlacementType string            `json:"placementType"`
	ZoneID        string            `json:"zoneId"`
	RegionID      string            `json:"regionId"`
	CreatedAt     string            `json:"createdAt"`
}

// applyPlacementGroup переносит ответ края в состояние.
//
// Незаданная координата остаётся null, а не пустой строкой: пустая строка в
// состоянии означала бы «арендатор задал пустой регион», и проверка «ровно одна
// координата» увидела бы две заданных — то есть план расходился бы после каждого
// применения, а сама группа при этом законна.
func applyPlacementGroup(ctx context.Context, m *placementGroupModel, g *placementGroupJSON) {
	m.ID = types.StringValue(g.ID)
	m.ProjectID = types.StringValue(g.ProjectID)
	m.Name = types.StringValue(g.Name)
	m.Description = types.StringValue(g.Description)
	m.Labels = mapToTF(ctx, g.Labels)
	m.Strategy = types.StringValue(g.Strategy)
	m.PlacementType = types.StringValue(g.PlacementType)
	m.CreatedAt = types.StringValue(g.CreatedAt)

	if g.ZoneID != "" {
		m.ZoneID = types.StringValue(g.ZoneID)
	}
	if g.RegionID != "" {
		m.RegionID = types.StringValue(g.RegionID)
	}
}
