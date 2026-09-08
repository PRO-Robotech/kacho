// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const subnetsPath = "/vpc/v1/subnets"

type subnetModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	NetworkID       types.String `tfsdk:"network_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Labels          types.Map    `tfsdk:"labels"`
	ZoneID          types.String `tfsdk:"zone_id"`
	RegionID        types.String `tfsdk:"region_id"`
	IPv4CidrPrimary types.String `tfsdk:"ipv4_cidr_primary"`
	IPv6CidrPrimary types.String `tfsdk:"ipv6_cidr_primary"`
	RouteTableID    types.String `tfsdk:"route_table_id"`
	PlacementType   types.String `tfsdk:"placement_type"`
	CreatedAt       types.String `tfsdk:"created_at"`
	IPv4CidrBlocks  types.List   `tfsdk:"ipv4_cidr_blocks"`
	IPv6CidrBlocks  types.List   `tfsdk:"ipv6_cidr_blocks"`
}

type subnetResource struct{ c *client.Client }

// NewSubnetResource — конструктор для реестра провайдера.
func NewSubnetResource() resource.Resource { return &subnetResource{} }

func (r *subnetResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameVPCSubnet
}

func (r *subnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *subnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Подсеть VPC. Размещается либо в зоне, либо в регионе — ровно одно из двух.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор подсети.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт подсеть."},
			"network_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Сеть-владелец. Изменение пересоздаёт подсеть."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя подсети в пределах проекта. Обязательно — см. пояснение у сети."},
			"description": schema.StringAttribute{Optional: true, Computed: true},
			"labels":      schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"zone_id": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				MarkdownDescription: "Зона размещения. Задаётся ровно одно из `zone_id` и `region_id`."},
			"region_id": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				MarkdownDescription: "Регион размещения для региональной (anycast) подсети."},
			"ipv4_cidr_primary": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: replace,
				MarkdownDescription: "Первичный блок IPv4. Неизменяем: изменение пересоздаёт подсеть."},
			"ipv6_cidr_primary": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: replace,
				MarkdownDescription: "Первичный блок IPv6. Неизменяем."},
			"route_table_id": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Таблица маршрутов. Если не задать, край подставит таблицу сети " +
					"по умолчанию — поэтому атрибут одновременно необязательный и вычисляемый: " +
					"«не задал» и «пусто» здесь разные состояния, и без этого план расходился бы " +
					"после каждого применения."},
			"placement_type": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Тип размещения. Выводится краем из заданного якоря; задать его нельзя."},
			"created_at": schema.StringAttribute{Computed: true},
			"ipv4_cidr_blocks": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Дополнительные блоки IPv4 сверх первичного."},
			"ipv6_cidr_blocks": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Дополнительные блоки IPv6 сверх первичного."},
		},
	}
}

// ValidateConfig — ровно один якорь размещения, и это устанавливается ДО обращения к краю.
//
// Край отвергнет и ноль, и два, но его отказ приедет после сетевого вызова и будет
// выглядеть как отказ платформы. Ошибка конфигурации обязана называться на этапе проверки.
func (r *subnetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg subnetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Судить можно ТОЛЬКО о том, что уже известно.
	//
	// Обе прежние редакции этой проверки ошибались, и каждая — своим краем. Первая
	// сравнивала строку: у неизвестного значения она пуста, и законная конфигурация с
	// зоной из переменной отвергалась как «якорь не задан». Вторая смотрела только на
	// null: неизвестное не null, поэтому модуль, передающий `region_id = null` рядом с
	// неизвестной зоной, получал «заданы оба».
	//
	// Верное правило: неизвестное — это «значение будет позже», и о нём нельзя утверждать
	// ни «задано», ни «пусто». Пока хоть один якорь неизвестен, проверка молчит; край
	// всё равно отвергнет негодную пару, а провайдер не станет отвергать годную.
	if cfg.ZoneID.IsUnknown() || cfg.RegionID.IsUnknown() {
		return
	}
	zone := !cfg.ZoneID.IsNull()
	region := !cfg.RegionID.IsNull()
	switch {
	case zone && region:
		resp.Diagnostics.AddError("Заданы оба якоря размещения",
			"У подсети задаётся ровно одно из zone_id и region_id. Зональная и региональная "+
				"подсети — разные вещи: у второй зоны нет вовсе, и зональные проверки к ней "+
				"не применяются.")
	case !zone && !region:
		resp.Diagnostics.AddError("Якорь размещения не задан",
			"Укажите zone_id (зональная подсеть) либо region_id (региональная, anycast).")
	}
}

func (r *subnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.CreateSubnetRequest{
		ProjectId:       plan.ProjectID.ValueString(),
		NetworkId:       plan.NetworkID.ValueString(),
		Name:            plan.Name.ValueString(),
		Description:     plan.Description.ValueString(),
		Labels:          mapFromTF(ctx, plan.Labels),
		ZoneId:          plan.ZoneID.ValueString(),
		RegionId:        plan.RegionID.ValueString(),
		Ipv4CidrPrimary: plan.IPv4CidrPrimary.ValueString(),
		Ipv6CidrPrimary: plan.IPv6CidrPrimary.ValueString(),
		RouteTableId:    plan.RouteTableID.ValueString(),
	}
	// placement_type НЕ отправляется: край выводит его сам и отвергает попытку задать.

	// Ключ идемпотентности считается по ТЕЛУ ЗАПРОСА ЦЕЛИКОМ, а не по рукописной выборке
	// полей. Здесь стояла карта из шести полей, и это делало ОТКАЗ ЛИПКИМ по всем
	// остальным: край ключ соблюдает и на повтор с тем же ключом возвращает ту же
	// операцию, поэтому правка метки или описания после отвергнутого создания
	// воспроизводила прежний отказ — пользователь правит настройку и уверен, что правка
	// не применилась.
	//
	// Байты берутся ТОЙ ЖЕ функцией, что и отправка: вторая сериализация разошлась бы с
	// первой молча, и разошлась бы именно там, где расхождение не видно.
	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameVPCSubnet, plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, subnetsPath, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание подсети не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Край отверг создание подсети", out.Message)
		return
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ на создание подсети не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Создание подсети не завершилось", err.Error())
		return
	}
	id, ok := done.MetadataString("subnetId")
	if !ok || id == "" {
		resp.Diagnostics.AddError("Край не сообщил идентификатор подсети",
			"Операция успешна, но метаданные не содержат subnetId — записывать в состояние нечего.")
		return
	}

	// Идентификатор в состояние ДО первого обратного чтения — см. пояснение у сети.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт по сообщению на каждое
	// поле вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, rerr := r.readSubnet(ctx, id, true)
	if rerr != nil {
		resp.Diagnostics.AddError("Подсеть создана, но не прочитана обратно", rerr.Error())
		return
	}
	applySubnet(ctx, &plan, sub)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := r.readSubnet(ctx, state.ID.ValueString(), false)
	if err == nil {
		applySubnet(ctx, &state, sub)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение подсети не удалось", err.Error())
		return
	}

	verdict, verr := r.c.ConfirmAbsence(ctx, subnetsPath, client.ScopeProject,
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к проекту утрачен",
			"Подсеть "+state.ID.ValueString()+" не читается, и список проекта отвечает отказом. "+
				"Это событие прав, а не удаление.")
	default:
		msg := "Подсеть " + state.ID.ValueString() + " не найдена, и подтвердить отсутствие нечем: " +
			"в проекте не видно ни одной подсети. Если она действительно удалена вне Terraform, " +
			"уберите её из состояния вручную:\n  terraform state rm <адрес ресурса>"
		if verr != nil {
			msg += "\n\nПодробности: " + verr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие подсети не подтверждено", msg)
	}
}

func (r *subnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state subnetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.UpdateSubnetRequest{SubnetId: state.ID.ValueString()}
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
	if !plan.RouteTableID.Equal(state.RouteTableID) && !plan.RouteTableID.IsUnknown() {
		body.RouteTableId = plan.RouteTableID.ValueString()
		paths = append(paths, "routeTableId")
	}

	// Пустая маска: запрос не отправляем, но и выйти нельзя — в плане вычисляемые
	// атрибуты ещё неизвестны, а состояние неизвестного не хранит. См. разбор у сети.
	if len(paths) > 0 {
		body.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}

		httpResp, err := r.c.Do(ctx, http.MethodPatch, subnetsPath+"/"+state.ID.ValueString(), body, nil)
		if err != nil {
			resp.Diagnostics.AddError("Изменение подсети не отправлено", err.Error())
			return
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			resp.Diagnostics.AddError("Край отверг изменение подсети", out.Message)
			return
		}
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Изменение подсети не завершилось", err.Error())
				return
			}
		}
	}

	sub, rerr := r.readSubnet(ctx, state.ID.ValueString(), false)
	if rerr != nil {
		resp.Diagnostics.AddError("Подсеть изменена, но не прочитана обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	applySubnet(ctx, &plan, sub)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subnetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	httpResp, err := r.c.Do(ctx, http.MethodDelete, subnetsPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление подсети не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Удаление подсети не завершилось", err.Error())
			}
		}
	case client.OutcomeNotFound:
		verdict, _ := r.c.ConfirmAbsence(ctx, subnetsPath, client.ScopeProject,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			resp.Diagnostics.AddError("Удаление подсети не подтверждено",
				"Край ответил «не найдено», но отсутствие не подтверждено (исход: "+verdict.String()+").")
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление подсети", out.Message)
	}
}

// ImportState принимает идентификатор ресурса.
//
// Формат проверяется ЗДЕСЬ, общим каталогом префиксов платформы (pkg/ids) — до любого
// обращения к краю. Это та же дисциплина, что у сервисов: заведомо негодный идентификатор
// получает терминальный отказ с внятным текстом, а не уезжает в сеть, чтобы вернуться
// оттуда «ресурс не найден» — ответом, который для строки, не являющейся идентификатором,
// не значит ничего.
func (r *subnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !ids.IsValid(req.ID, ids.PrefixSubnet) {
		resp.Diagnostics.AddError("Негодный идентификатор подсети",
			"Строка "+strconv.Quote(req.ID)+" не является идентификатором подсети Kachō: "+
				"он начинается с «"+ids.PrefixSubnet+"» и состоит из знаков crockford-base32.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *subnetResource) readSubnet(ctx context.Context, id string, retryAuthz bool) (*subnetJSON, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		httpResp, err := r.c.Do(ctx, http.MethodGet, subnetsPath+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(httpResp)
		switch out.Kind {
		case client.OutcomeOK:
			var s subnetJSON
			if err := json.Unmarshal(httpResp.Body, &s); err != nil {
				return nil, fmt.Errorf("разбор подсети: %w", err)
			}
			return &s, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к подсети %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение подсети %s: %s", id, out.Message)
		}
	}
}

type subnetJSON struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"projectId"`
	NetworkID       string            `json:"networkId"`
	CreatedAt       string            `json:"createdAt"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Labels          map[string]string `json:"labels"`
	PlacementType   string            `json:"placementType"`
	ZoneID          string            `json:"zoneId"`
	RegionID        string            `json:"regionId"`
	RouteTableID    string            `json:"routeTableId"`
	IPv4CidrPrimary string            `json:"ipv4CidrPrimary"`
	IPv4CidrBlocks  []string          `json:"ipv4CidrBlocks"`
	IPv6CidrPrimary string            `json:"ipv6CidrPrimary"`
	IPv6CidrBlocks  []string          `json:"ipv6CidrBlocks"`
}

func applySubnet(ctx context.Context, m *subnetModel, s *subnetJSON) {
	m.ID = types.StringValue(s.ID)
	m.ProjectID = types.StringValue(s.ProjectID)
	m.NetworkID = types.StringValue(s.NetworkID)
	m.Name = types.StringValue(s.Name)
	m.Description = types.StringValue(s.Description)
	m.Labels = mapToTF(ctx, s.Labels)
	m.PlacementType = types.StringValue(s.PlacementType)
	m.CreatedAt = types.StringValue(s.CreatedAt)
	m.RouteTableID = types.StringValue(s.RouteTableID)
	m.IPv4CidrPrimary = types.StringValue(s.IPv4CidrPrimary)
	m.IPv6CidrPrimary = types.StringValue(s.IPv6CidrPrimary)
	m.IPv4CidrBlocks = listFromStrings(ctx, s.IPv4CidrBlocks)
	m.IPv6CidrBlocks = listFromStrings(ctx, s.IPv6CidrBlocks)

	// Якоря размещения читаются обратно ровно так, как их вернул край: у зональной подсети
	// region_id пуст, у региональной — zone_id. Подставлять сюда пустую строку вместо null
	// нельзя: это выглядело бы как «пользователь задал оба».
	if s.ZoneID != "" {
		m.ZoneID = types.StringValue(s.ZoneID)
	}
	if s.RegionID != "" {
		m.RegionID = types.StringValue(s.RegionID)
	}
}
