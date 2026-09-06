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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Проект, группа и служебная учётка описываются ОДНИМ типом — их форма совпадает
// полностью: аккаунт-владелец, имя, описание, метки; изменяемы только три последних.
//
// Это не экономия строк, а защита от расхождения: три копии одной механики завели бы
// четвёртую копированием, и одна из них однажды забыла бы проверку отказа операции — ту,
// что отделяет настоящий идентификатор от фантомного. Различия ресурсов вынесены в
// таблицу ниже и видны целиком.
// Префиксы здесь — ЛИТЕРАЛЫ, и это не второй словарь.
//
// Общий каталог pkg/ids держит префиксы iam ровно так же — литералами, и объясняет почему:
// их владелец сервис iam, а его внутренние константы в общую библиотеку не импортируются
// (запрет на internal). Принадлежность каталогу проверяет ids.HasKnownPrefix, форму —
// ids.IsValid; здесь литерал стоит рядом с путём и полем метаданных, то есть весь вид
// ресурса виден одним куском.
type iamNamedKind struct {
	tfName     string // имя в конфигурации
	human      string // как называть в тексте отказа
	path       string // путь коллекции
	idField    string // поле идентификатора в метаданных операции
	prefix     string // префикс идентификатора
	descr      string // описание ресурса для документации
	deleteHint string // что мешает удалению — сказано пользователю заранее
}

var (
	iamProjectKind = iamNamedKind{
		tfName: typeNameIAMProject, human: "Проект", path: "/iam/v1/projects",
		idField: "projectId", prefix: "prj",
		descr: "Проект — область, внутри которой живут ресурсы платформы.",
		deleteHint: "Удалению мешает пользовательская роль, определённая в этом проекте: " +
			"ссылка на проект защищена на уровне хранилища.",
	}
	iamGroupKind = iamNamedKind{
		tfName: typeNameIAMGroup, human: "Группа", path: "/iam/v1/groups",
		idField: "groupId", prefix: "grp",
		descr: "Группа субъектов. Права выдаются группе, а люди и служебные учётки в неё добавляются.",
		deleteHint: "Удалению мешают действующие выдачи прав на эту группу. Члены группы " +
			"удалению НЕ мешают — членство снимается вместе с ней.",
	}
	iamServiceAccountKind = iamNamedKind{
		tfName: typeNameIAMServiceAccount, human: "Служебная учётка", path: "/iam/v1/serviceAccounts",
		idField: "serviceAccountId", prefix: "sva",
		descr: "Служебная учётная запись — принципал для машинного доступа.",
		deleteHint: "Удалению мешают действующие выдачи прав и членство в группах. " +
			"Ключи учётки удалению не мешают.",
	}
)

type iamNamedModel struct {
	ID          types.String `tfsdk:"id"`
	AccountID   types.String `tfsdk:"account_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

type iamNamedResource struct {
	kind iamNamedKind
	c    *client.Client
}

// NewIAMProjectResource, NewIAMGroupResource, NewIAMServiceAccountResource — конструкторы
// для реестра провайдера.
func NewIAMProjectResource() resource.Resource { return &iamNamedResource{kind: iamProjectKind} }
func NewIAMGroupResource() resource.Resource   { return &iamNamedResource{kind: iamGroupKind} }
func NewIAMServiceAccountResource() resource.Resource {
	return &iamNamedResource{kind: iamServiceAccountKind}
}

// MoveState — переезд состояния с прежнего имени типа.
//
// Объявление обязательно: одного блока `moved` в настройке оператора НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план. Что именно принимается и почему — iam_type_names.go.
func (r *iamNamedResource) MoveState(ctx context.Context) []resource.StateMover {
	return movedFromRetiredTypeName(ctx, r)
}

func (r *iamNamedResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = r.kind.tfName
}

func (r *iamNamedResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *iamNamedResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: r.kind.descr + "\n\n" + r.kind.deleteHint,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор. По нему выполняется импорт.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"account_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Аккаунт-владелец. Изменение пересоздаёт ресурс.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя в пределах аккаунта. Обязательно: имя — единственный " +
					"способ найти уже созданный ресурс, если ответ на создание потерялся."},
			"description": schema.StringAttribute{Optional: true, Computed: true},
			"labels":      schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (r *iamNamedResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamNamedModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.createBody(ctx, &plan)
	id, err := awaitCreate(ctx, r.c, r.kind.path, r.kind.idField, r.kind.tfName,
		plan.AccountID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание не завершилось: "+r.kind.human, err.Error())
		return
	}

	// Идентификатор в состояние ДО первого обратного чтения: это единственная точка, где
	// потеря необратима — ресурс создан, apply прерван, состояние пусто, следующий apply
	// создаёт дубль.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт шесть сообщений об
	// «invalid result object» вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, r.kind.path, id, true)
	if err != nil {
		resp.Diagnostics.AddError(r.kind.human+" создан, но не прочитан обратно", err.Error())
		return
	}
	r.apply(ctx, &plan, raw)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamNamedResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamNamedModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, r.kind.path, state.ID.ValueString(), false)
	if err == nil {
		r.apply(ctx, &state, raw)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение не удалось: "+r.kind.human, err.Error())
		return
	}

	remove, title, detail := absenceDiagnostics(ctx, r.c, r.kind.path, client.ScopeAccount,
		r.kind.human, state.ID.ValueString(), state.AccountID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *iamNamedResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state iamNamedModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, paths := r.updateBody(ctx, &plan, &state)
	if len(paths) > 0 {
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			r.kind.path+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение не завершилось: "+r.kind.human, err.Error())
			return
		}
	}
	// Обратное чтение выполняется ВСЕГДА, даже когда запрос не отправлялся: в плане
	// вычисляемые атрибуты ещё неизвестны, а состояние неизвестного не хранит.
	raw, err := readByID(ctx, r.c, r.kind.path, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError(r.kind.human+" изменён, но не прочитан обратно", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(ctx, &plan, raw)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamNamedResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamNamedModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := awaitMutation(ctx, r.c, http.MethodDelete, r.kind.path+"/"+state.ID.ValueString(), nil)
	if err == nil {
		return
	}
	// Отказ края передаётся дословно: он называет предмет — действующие выдачи прав,
	// членство в группах, ссылающуюся роль. Пересказывать его своими словами значило бы
	// потерять то, что пользователю и нужно.
	resp.Diagnostics.AddError("Удаление не завершилось: "+r.kind.human,
		err.Error()+"\n\n"+r.kind.deleteHint)
}

func (r *iamNamedResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, r.kind.human, r.kind.prefix, req, resp)
}

func (r *iamNamedResource) createBody(ctx context.Context, plan *iamNamedModel) proto.Message {
	acc, name := plan.AccountID.ValueString(), plan.Name.ValueString()
	descr, labels := plan.Description.ValueString(), mapFromTF(ctx, plan.Labels)
	switch r.kind.tfName {
	case iamGroupKind.tfName:
		return &iamv1.CreateGroupRequest{AccountId: acc, Name: name, Description: descr, Labels: labels}
	case iamServiceAccountKind.tfName:
		return &iamv1.CreateServiceAccountRequest{AccountId: acc, Name: name, Description: descr, Labels: labels}
	default:
		return &iamv1.CreateProjectRequest{AccountId: acc, Name: name, Description: descr, Labels: labels}
	}
}

func (r *iamNamedResource) updateBody(ctx context.Context, plan, state *iamNamedModel) (proto.Message, []string) {
	var paths []string
	name, descr := "", ""
	var labels map[string]string
	if !plan.Name.Equal(state.Name) {
		name = plan.Name.ValueString()
		paths = append(paths, "name")
	}
	if !plan.Description.Equal(state.Description) {
		descr = plan.Description.ValueString()
		paths = append(paths, "description")
	}
	if !plan.Labels.Equal(state.Labels) {
		labels = mapFromTF(ctx, plan.Labels)
		paths = append(paths, "labels")
	}
	if len(paths) == 0 {
		return nil, nil
	}
	mask := &fieldmaskpb.FieldMask{Paths: paths}
	id := state.ID.ValueString()
	switch r.kind.tfName {
	case iamGroupKind.tfName:
		return &iamv1.UpdateGroupRequest{GroupId: id, UpdateMask: mask, Name: name, Description: descr, Labels: labels}, paths
	case iamServiceAccountKind.tfName:
		return &iamv1.UpdateServiceAccountRequest{ServiceAccountId: id, UpdateMask: mask, Name: name, Description: descr, Labels: labels}, paths
	default:
		return &iamv1.UpdateProjectRequest{ProjectId: id, UpdateMask: mask, Name: name, Description: descr, Labels: labels}, paths
	}
}

// iamNamedJSON — ответ края. Разбор терпим к неизвестным полям: край добавляет их вперёд
// провайдера, и строгий разбор ломал бы чтение на первом же новом поле.
type iamNamedJSON struct {
	ID          string            `json:"id"`
	AccountID   string            `json:"accountId"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   string            `json:"createdAt"`
}

func (r *iamNamedResource) apply(ctx context.Context, m *iamNamedModel, raw []byte) {
	var j iamNamedJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return
	}
	m.ID = types.StringValue(j.ID)
	m.AccountID = types.StringValue(j.AccountID)
	m.Name = types.StringValue(j.Name)
	m.Description = types.StringValue(j.Description)
	m.Labels = mapToTF(ctx, j.Labels)
	m.CreatedAt = types.StringValue(j.CreatedAt)
}
