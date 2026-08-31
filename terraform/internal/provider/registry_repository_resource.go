// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Репозиторий реестра — единственный ресурс провайдера, адресуемый НЕ идентификатором.
//
// Его адрес — пара «реестр + имя», и имя может содержать косые черты (`team/service`):
// край объявляет его как `{repository=**}`. Отсюда три отличия от общего каркаса:
//
//  1. `id` собирается провайдером как `<registryId>/<repository>` — своего идентификатора
//     у репозитория нет вовсе, а состоянию Terraform адрес нужен;
//  2. имя ПЕРЕСОЗДАЁТ ресурс — и это выбор ПРОВАЙДЕРА, а не свойство края:
//     переименование у края есть (`:rename`), провайдер им не пользуется намеренно.
//     Довод — в том, что видит человек перед `apply`: пересоздание показывается планом
//     как снос с заведением заново, а внутриместное переименование прошло бы обычной
//     правкой атрибута. Путь загрузки образа завязан на имя, поэтому тихая подмена
//     сломала бы всех, кто по нему тянет, не показав этого в плане. Клиенту, которому
//     нельзя терять содержимое, остаётся `:rename` у края плюс `state rm` + `import`;
//  3. отсутствие подтверждается списком репозиториев ЭТОГО реестра, а не списком проекта.
type repositoryModel struct {
	ID          types.String `tfsdk:"id"`
	RegistryID  types.String `tfsdk:"registry_id"`
	Repository  types.String `tfsdk:"repository"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
	Visibility  types.String `tfsdk:"visibility"`

	TagCount      types.Int64  `tfsdk:"tag_count"`
	SizeBytes     types.Int64  `tfsdk:"size_bytes"`
	DownloadCount types.Int64  `tfsdk:"download_count"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

type repositoryResource struct{ c *client.Client }

// NewRegistryRepositoryResource — конструктор для реестра провайдера.
func NewRegistryRepositoryResource() resource.Resource { return &repositoryResource{} }

func (r *repositoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_repository"
}

func (r *repositoryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *repositoryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{
		stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Репозиторий образов внутри реестра.\n\n" +
			"Путь загрузки — `$домен/$registryId/$repository:$тег`: он строится по " +
			"НЕИЗМЕНЯЕМОМУ идентификатору реестра и имени репозитория.\n\n" +
			":::info Ресурс управляет НАЛОЖЕНИЕМ, а не содержимым\n" +
			"Репозиторий — это проекция движка хранения образов плюс наложенные на неё " +
			"метаданные: описание, метки, видимость. Terraform владеет только наложением. " +
			"Теги и слои появляются загрузкой образа (`docker push`) и исчезают их удалением — " +
			"этих действий у Terraform нет и быть не должно.\n\n" +
			"Отсюда два следствия, которые видны в работе: создание УСЫНОВЛЯЕТ уже имеющееся " +
			"в движке содержимое (счётчик тегов приедет непустым, если образы там уже лежат), " +
			"а удаление край ОТВЕРГАЕТ, пока репозиторий не пуст.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Адрес ресурса в состоянии — `<registryId>/<repository>`. " +
					"Своего идентификатора у репозитория нет: его адресует пара.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"registry_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Реестр-владелец. Перенос репозитория между реестрами " +
					"краем не поддержан — изменение пересоздаёт ресурс."},
			"repository": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Имя репозитория. Может содержать косые черты " +
					"(`team/service`). Провайдер меняет имя ПЕРЕСОЗДАНИЕМ: старый репозиторий " +
					"сносится вместе с содержимым, новый заводится по умолчанию реестра — " +
					"так план показывает разрушение, а не обычную правку атрибута. " +
					"Непересоздающий путь есть у края (`POST …/repositories/{repository}:rename`): " +
					"он переносит теги, наложение и права. Звать его надо вне Terraform, а " +
					"состояние приводить `state rm` + `import`."},
			"description": schema.StringAttribute{Optional: true, Computed: true},
			"labels": schema.MapAttribute{Optional: true, Computed: true,
				ElementType: types.StringType},
			"visibility": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "`PRIVATE` или `PUBLIC`. Не задано — умолчание реестра."},

			"tag_count":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Сколько тегов в репозитории."},
			"size_bytes":     schema.Int64Attribute{Computed: true},
			"download_count": schema.Int64Attribute{Computed: true},
			"created_at":     schema.StringAttribute{Computed: true},
			"updated_at":     schema.StringAttribute{Computed: true},
		},
	}
}

// repoPath — путь репозитория у края.
//
// Имя экранируется ПОСЕГМЕНТНО: край объявил его как `{repository=**}`, то есть косая
// черта в имени — часть адреса, а не разделитель, который надо прятать. Экранировав её
// целиком, мы получили бы `team%2Fservice` и промах по маршруту.
func repoPath(registryID, repository string) string {
	segs := strings.Split(repository, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/registry/v1/registries/" + url.PathEscape(registryID) + "/repositories/" +
		strings.Join(segs, "/")
}

func repoStateID(registryID, repository string) string { return registryID + "/" + repository }

func (r *repositoryResource) applyWire(ctx context.Context, m *repositoryModel, raw []byte) error {
	var w struct {
		RegistryID    string            `json:"registryId"`
		Name          string            `json:"name"`
		Description   string            `json:"description"`
		Labels        map[string]string `json:"labels"`
		Visibility    string            `json:"visibility"`
		TagCount      any               `json:"tagCount"`
		SizeBytes     any               `json:"sizeBytes"`
		DownloadCount any               `json:"downloadCount"`
		CreatedAt     string            `json:"createdAt"`
		UpdatedAt     string            `json:"updatedAt"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	if w.RegistryID != "" {
		m.RegistryID = types.StringValue(w.RegistryID)
	}
	if w.Name != "" {
		m.Repository = types.StringValue(w.Name)
	}
	m.ID = types.StringValue(repoStateID(m.RegistryID.ValueString(), m.Repository.ValueString()))
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.Visibility = types.StringValue(w.Visibility)
	m.TagCount = types.Int64Value(numOf(w.TagCount))
	m.SizeBytes = types.Int64Value(numOf(w.SizeBytes))
	m.DownloadCount = types.Int64Value(numOf(w.DownloadCount))
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.UpdatedAt = types.StringValue(w.UpdatedAt)
	return nil
}

func (r *repositoryResource) readRepo(ctx context.Context, registryID, repository string) ([]byte, error) {
	resp, err := r.c.Do(ctx, http.MethodGet, repoPath(registryID, repository), nil, nil)
	if err != nil {
		return nil, err
	}
	out := client.Classify(resp)
	switch out.Kind {
	case client.OutcomeOK:
		return resp.Body, nil
	case client.OutcomeNotFound:
		return nil, &notFoundError{msg: out.Message}
	default:
		return nil, fmt.Errorf("чтение репозитория %s: %s", repository, out.Message)
	}
}

func (r *repositoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &registryv1.CreateRepositoryRequest{
		RegistryId:  plan.RegistryID.ValueString(),
		Repository:  plan.Repository.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
	}
	if v := plan.Visibility.ValueString(); v != "" && !plan.Visibility.IsUnknown() {
		val, ok := registryv1.Visibility_value[v]
		if !ok {
			resp.Diagnostics.AddError("Негодная видимость",
				"visibility принимает PRIVATE или PUBLIC, получено "+v+
					". Не задано — наследуется умолчание реестра.")
			return
		}
		body.Visibility = registryv1.Visibility(val)
	}

	col := "/registry/v1/registries/" + url.PathEscape(plan.RegistryID.ValueString()) + "/repositories"
	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey("kacho_registry_repository",
		repoStateID(plan.RegistryID.ValueString(), plan.Repository.ValueString()), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, col, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание репозитория не завершилось", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Создание репозитория отвергнуто", out.Message)
		return
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
		if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
			resp.Diagnostics.AddError("Создание репозитория не завершилось", err.Error())
			return
		}
	}

	// Адрес пишется ДО обратного чтения: сорвавшееся чтение иначе оставило бы созданный
	// репозиторий вне управления навсегда.
	plan.ID = types.StringValue(repoStateID(
		plan.RegistryID.ValueString(), plan.Repository.ValueString()))
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body2, err := r.readRepo(ctx, plan.RegistryID.ValueString(), plan.Repository.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Репозиторий создан, но не прочитан обратно", err.Error())
		return
	}
	if err := r.applyWire(ctx, &plan, body2); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *repositoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := r.readRepo(ctx, state.RegistryID.ValueString(), state.Repository.ValueString())
	if err == nil {
		if err := r.applyWire(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение репозитория не удалось", err.Error())
		return
	}

	// Отсутствие подтверждается списком ЭТОГО реестра: «не найдено» при отказе в доступе
	// побайтово равно настоящему отсутствию, и одного такого ответа мало.
	verdict, lerr := r.c.ConfirmAbsence(ctx,
		"/registry/v1/registries/"+url.PathEscape(state.RegistryID.ValueString())+"/repositories",
		"", "", state.Repository.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
		// Список видит репозиторий, а чтение — нет: расхождение внутри края, продолжать нельзя.
		resp.Diagnostics.AddError("Репозиторий виден в списке, но не читается",
			"Реестр "+state.RegistryID.ValueString()+" перечисляет "+state.Repository.ValueString()+
				", а чтение отвечает «не найдено». Повторите позже; если держится — это "+
				"расхождение на стороне края, а не состояния Terraform.")
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к реестру утрачен",
			"Репозиторий не читается, и список реестра тоже отвечает отказом. Это событие ПРАВ, "+
				"а не удаление: продолжать нельзя, иначе план предложит пересоздать целое.")
	default:
		d := "Репозиторий " + state.Repository.ValueString() + " не найден, и подтвердить " +
			"отсутствие нечем: в реестре не видно ни одного репозитория. Различить «удалён» и " +
			"«доступ отозван» край не позволяет — ответы совпадают дословно.\n\nЕсли репозиторий " +
			"действительно удалён вне Terraform, уберите его из состояния:\n  terraform state rm <адрес>"
		if lerr != nil {
			d += "\n\nПодробности: " + lerr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие не подтверждено", d)
	}
}

func (r *repositoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &registryv1.UpdateRepositoryRequest{
		RegistryId: state.RegistryID.ValueString(),
		Repository: state.Repository.ValueString(),
	}
	var paths []string
	if !plan.Description.Equal(state.Description) {
		body.Description = plan.Description.ValueString()
		paths = append(paths, "description")
	}
	if !plan.Labels.Equal(state.Labels) {
		body.Labels = mapFromTF(ctx, plan.Labels)
		paths = append(paths, "labels")
	}
	if !plan.Visibility.Equal(state.Visibility) {
		if v := plan.Visibility.ValueString(); v != "" {
			val, ok := registryv1.Visibility_value[v]
			if !ok {
				resp.Diagnostics.AddError("Негодная видимость",
					"visibility принимает PRIVATE или PUBLIC, получено "+v+
						". Не задано — наследуется умолчание реестра.")
				return
			}
			body.Visibility = registryv1.Visibility(val)
		}
		paths = append(paths, "visibility")
	}

	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			repoPath(state.RegistryID.ValueString(), state.Repository.ValueString()), body); err != nil {
			resp.Diagnostics.AddError("Изменение репозитория не завершилось", err.Error())
			return
		}
	}

	raw, err := r.readRepo(ctx, state.RegistryID.ValueString(), state.Repository.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Репозиторий изменён, но не прочитан обратно", err.Error())
		return
	}
	if err := r.applyWire(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *repositoryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		repoPath(state.RegistryID.ValueString(), state.Repository.ValueString()), nil); err != nil {
		// Прежний текст утверждал, что удаление снимает содержимое. Это была ложь ровно
		// наоборот: край спрашивает движок и ОТВЕРГАЕТ удаление непустого репозитория —
		// содержимое ему не принадлежит. Сообщение, говорящее противоположное тому, что
		// делает край, отправляет читателя спасать данные, которым ничего не грозит, и
		// скрывает настоящую причину отказа.
		resp.Diagnostics.AddError("Удаление репозитория не завершилось",
			err.Error()+"\n\nЕсли отказ говорит о непустоте — это не состояние гонки: "+
				"Terraform владеет только НАЛОЖЕНИЕМ метаданных, а теги и слои живут в движке "+
				"хранения образов. Удалите образы (`docker`-клиентом или через API тегов) и "+
				"повторите. Провайдер их не трогает намеренно: снос данных, которых он не "+
				"заводил, не входит в его полномочия.")
		return
	}
}

// ImportState принимает адрес в том же виде, в каком его хранит состояние:
// `<registryId>/<repository>`. Имя может содержать косые черты, поэтому разделитель —
// ПЕРВАЯ черта, а не последняя: иначе `reg…/team/service` разобрался бы как реестр
// `reg…/team`.
func (r *repositoryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	reg, repo, ok := strings.Cut(req.ID, "/")
	if !ok || reg == "" || repo == "" {
		resp.Diagnostics.AddError("Негодный адрес для импорта",
			"Ожидается «<идентификатор реестра>/<имя репозитория>», например "+
				"`reg01hxyz.../team/service`. Получено: "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("registry_id"), reg)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("repository"), repo)...)
}
