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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	computev1 "github.com/PRO-Robotech/kacho/terraform/internal/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const instancesPath = "/compute/v1/instances"

type instanceModel struct {
	ID                types.String `tfsdk:"id"`
	ProjectID         types.String `tfsdk:"project_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Labels            types.Map    `tfsdk:"labels"`
	ZoneID            types.String `tfsdk:"zone_id"`
	MachineTypeID     types.String `tfsdk:"machine_type_id"`
	BootSourceType    types.String `tfsdk:"boot_source_type"`
	BootSourceID      types.String `tfsdk:"boot_source_id"`
	SubnetID          types.String `tfsdk:"subnet_id"`
	SecurityGroupIDs  types.List   `tfsdk:"security_group_ids"`
	GuestAccessKeyIDs types.List   `tfsdk:"guest_access_key_ids"`
	PlacementGroupID  types.String `tfsdk:"placement_group_id"`
	ServiceAccountID  types.String `tfsdk:"service_account_id"`
	Status            types.String `tfsdk:"status"`
	StatusReason      types.String `tfsdk:"status_reason"`
	FQDN              types.String `tfsdk:"fqdn"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

type instanceResource struct{ c *client.Client }

// NewInstanceResource — конструктор для реестра провайдера.
func NewInstanceResource() resource.Resource { return &instanceResource{} }

func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_instance"
}

func (r *instanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *instanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Машина. Создаётся в своей зоне; источник загрузки и зона неизменяемы.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор машины.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт машину."},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true},
			"labels":      schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"zone_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Зона размещения. Неизменяема: смена зоны — это другая машина."},
			"machine_type_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Тип машины из каталога. Меняется только на остановленной машине."},
			"boot_source_type": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Вид источника загрузки: `storage.image`, `storage.snapshot` " +
					"или `storage.volume`. Неизменяем."},
			"boot_source_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Идентификатор источника загрузки у его владельца. Неизменяем."},
			"subnet_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Подсеть интерфейса. Обязана быть в той же зоне, что машина " +
					"(региональная подсеть из зональной проверки исключена)."},
			"security_group_ids": schema.ListAttribute{Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Группы безопасности интерфейса. Меняются пересозданием машины: " +
					"правка состава интерфейса живёт в отдельных глаголах, а не в теле машины."},
			"guest_access_key_ids": schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Ключи входа гостя — ссылками по неизменяемому идентификатору. " +
					"Набор ЗАМЕНЯЕТСЯ целиком; ключ обязан принадлежать тому же проекту. " +
					"Материал ключа сюда не кладётся: ключ — отдельный ресурс со своим сроком жизни."},
			"placement_group_id": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Группа размещения. Её якорь обязан быть когерентен зоне машины: " +
					"зональная группа — та же зона, региональная — тот же регион."},
			"service_account_id": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Служебная учётка машины."},
			"status":        schema.StringAttribute{Computed: true},
			"status_reason": schema.StringAttribute{Computed: true},
			"fqdn":          schema.StringAttribute{Computed: true},
			"created_at":    schema.StringAttribute{Computed: true},
		},
	}
}

func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.CreateInstanceRequest{
		ProjectId:     plan.ProjectID.ValueString(),
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueString(),
		Labels:        mapFromTF(ctx, plan.Labels),
		ZoneId:        plan.ZoneID.ValueString(),
		MachineTypeId: plan.MachineTypeID.ValueString(),
		BootSource: &computev1.BootSource{
			Type: plan.BootSourceType.ValueString(),
			Id:   plan.BootSourceID.ValueString(),
		},
		NetworkInterfaceSpecs: []*computev1.NetworkInterfaceSpec{{
			SubnetId:         plan.SubnetID.ValueString(),
			SecurityGroupIds: stringsFromList(ctx, plan.SecurityGroupIDs),
		}},
		GuestAccessKeyIds: stringsFromList(ctx, plan.GuestAccessKeyIDs),
		PlacementGroupId:  plan.PlacementGroupID.ValueString(),
		ServiceAccountId:  plan.ServiceAccountID.ValueString(),
	}

	// Ключ идемпотентности строится из АДРЕСА ресурса и тела: повтор того же
	// применения после обрыва ответа не должен родить вторую машину.
	raw, _ := json.Marshal(map[string]any{
		"projectId": body.ProjectId, "name": body.Name, "zoneId": body.ZoneId,
		"machineTypeId": body.MachineTypeId,
		"bootSource":    map[string]string{"type": body.BootSource.Type, "id": body.BootSource.Id},
	})
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		"kacho_compute_instance", plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, instancesPath, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание машины не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Край отверг создание машины", out.Message)
		return
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ на создание машины не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Создание машины не завершилось", err.Error())
		return
	}
	id, ok := done.MetadataString("instanceId")
	if !ok || id == "" {
		resp.Diagnostics.AddError("Край не сообщил идентификатор машины",
			"Операция успешна, но метаданные не содержат instanceId — записывать в состояние нечего.")
		return
	}

	// Идентификатор в состояние ДО обратного чтения: если чтение не удастся,
	// машина уже создана, и состояние без её идентификатора означало бы, что
	// следующее применение создаст вторую.
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, rerr := r.readInstance(ctx, id, true)
	if rerr != nil {
		resp.Diagnostics.AddError("Машина создана, но не прочитана обратно", rerr.Error())
		return
	}
	applyInstance(ctx, &plan, in)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, err := r.readInstance(ctx, state.ID.ValueString(), false)
	if err == nil {
		applyInstance(ctx, &state, in)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение машины не удалось", err.Error())
		return
	}

	// «Не найдено» само по себе не означает «удалена»: тот же ответ приходит при
	// утрате доступа, потому что край скрывает существование. Убрать ресурс из
	// состояния по неразличённому ответу значит потерять живую машину.
	verdict, verr := r.c.ConfirmAbsence(ctx, instancesPath,
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к проекту утрачен",
			"Машина "+state.ID.ValueString()+" не читается, и список проекта отвечает отказом. "+
				"Это событие прав, а не удаление.")
	default:
		msg := "Машина " + state.ID.ValueString() + " не найдена, и подтвердить отсутствие нечем: " +
			"в проекте не видно ни одной машины. Если она действительно удалена вне Terraform, " +
			"уберите её из состояния вручную:\n  terraform state rm <адрес ресурса>"
		if verr != nil {
			msg += "\n\nПодробности: " + verr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие машины не подтверждено", msg)
	}
}

func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.UpdateInstanceRequest{InstanceId: state.ID.ValueString()}
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
	if !plan.ServiceAccountID.Equal(state.ServiceAccountID) && !plan.ServiceAccountID.IsUnknown() {
		body.ServiceAccountId = plan.ServiceAccountID.ValueString()
		paths = append(paths, "service_account_id")
	}
	if !plan.MachineTypeID.Equal(state.MachineTypeID) {
		body.MachineTypeId = plan.MachineTypeID.ValueString()
		paths = append(paths, "machine_type_id")
	}
	if !plan.PlacementGroupID.Equal(state.PlacementGroupID) && !plan.PlacementGroupID.IsUnknown() {
		body.PlacementGroupId = plan.PlacementGroupID.ValueString()
		paths = append(paths, "placement_group_id")
	}
	// Набор ключей входа передаётся ЦЕЛИКОМ и только когда он изменился: пустая
	// маска у края означала бы правку всех изменяемых полей, а набор ключей в неё
	// намеренно не входит — иначе правка описания снимала бы весь доступ.
	if !plan.GuestAccessKeyIDs.Equal(state.GuestAccessKeyIDs) && !plan.GuestAccessKeyIDs.IsUnknown() {
		body.GuestAccessKeyIds = stringsFromList(ctx, plan.GuestAccessKeyIDs)
		paths = append(paths, "guest_access_key_ids")
	}

	if len(paths) > 0 {
		body.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
		httpResp, err := r.c.Do(ctx, http.MethodPatch, instancesPath+"/"+state.ID.ValueString(), body, nil)
		if err != nil {
			resp.Diagnostics.AddError("Изменение машины не отправлено", err.Error())
			return
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			resp.Diagnostics.AddError("Край отверг изменение машины", out.Message)
			return
		}
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Изменение машины не завершилось", err.Error())
				return
			}
		}
	}

	in, rerr := r.readInstance(ctx, state.ID.ValueString(), false)
	if rerr != nil {
		resp.Diagnostics.AddError("Машина изменена, но не прочитана обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	applyInstance(ctx, &plan, in)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	httpResp, err := r.c.Do(ctx, http.MethodDelete, instancesPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление машины не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Удаление машины не завершилось", err.Error())
			}
		}
	case client.OutcomeNotFound:
		verdict, _ := r.c.ConfirmAbsence(ctx, instancesPath,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			resp.Diagnostics.AddError("Удаление машины не подтверждено",
				"Край ответил «не найдено», но отсутствие не подтверждено (исход: "+verdict.String()+").")
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление машины", out.Message)
	}
}

func (r *instanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readInstance читает машину, при необходимости пережидая окно материализации прав.
//
// Сразу после создания собственный ресурс может кратко ответить отказом либо
// «не найдено»: право создателя материализуется у владельца прав вне пути
// запроса. Это ограниченное окно, и переждать его — работа клиента, а не повод
// заводить серверный барьер.
func (r *instanceResource) readInstance(ctx context.Context, id string, retryAuthz bool) (*instanceJSON, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		httpResp, err := r.c.Do(ctx, http.MethodGet, instancesPath+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(httpResp)
		switch out.Kind {
		case client.OutcomeOK:
			var in instanceJSON
			if err := json.Unmarshal(httpResp.Body, &in); err != nil {
				return nil, fmt.Errorf("разбор машины: %w", err)
			}
			return &in, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к машине %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение машины %s: %s", id, out.Message)
		}
	}
}

type instanceJSON struct {
	ID                string            `json:"id"`
	ProjectID         string            `json:"projectId"`
	CreatedAt         string            `json:"createdAt"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	Labels            map[string]string `json:"labels"`
	ZoneID            string            `json:"zoneId"`
	Status            string            `json:"status"`
	StatusReason      string            `json:"statusReason"`
	FQDN              string            `json:"fqdn"`
	MachineTypeID     string            `json:"machineTypeId"`
	PlacementGroupID  string            `json:"placementGroupId"`
	GuestAccessKeyIDs []string          `json:"guestAccessKeyIds"`
	ServiceAccount    *struct {
		ID string `json:"id"`
	} `json:"serviceAccount"`
}

func applyInstance(ctx context.Context, m *instanceModel, in *instanceJSON) {
	m.ID = types.StringValue(in.ID)
	m.ProjectID = types.StringValue(in.ProjectID)
	m.Name = types.StringValue(in.Name)
	m.Description = types.StringValue(in.Description)
	m.Labels = mapToTF(ctx, in.Labels)
	m.ZoneID = types.StringValue(in.ZoneID)
	m.MachineTypeID = types.StringValue(in.MachineTypeID)
	m.Status = types.StringValue(in.Status)
	m.StatusReason = types.StringValue(in.StatusReason)
	m.FQDN = types.StringValue(in.FQDN)
	m.CreatedAt = types.StringValue(in.CreatedAt)
	m.GuestAccessKeyIDs = listFromStrings(ctx, in.GuestAccessKeyIDs)

	// Группа размещения читается обратно, только если край её вернул: подставить
	// пустую строку вместо null значило бы записать в состояние «пользователь
	// задал пустую группу», и план разошёлся бы после каждого применения.
	if in.PlacementGroupID != "" {
		m.PlacementGroupID = types.StringValue(in.PlacementGroupID)
	}
	if in.ServiceAccount != nil && in.ServiceAccount.ID != "" {
		m.ServiceAccountID = types.StringValue(in.ServiceAccount.ID)
	}
}
