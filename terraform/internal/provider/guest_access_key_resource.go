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

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const guestAccessKeysPath = "/compute/v1/guestAccessKeys"

type guestAccessKeyModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	Labels      types.Map    `tfsdk:"labels"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

type guestAccessKeyResource struct{ c *client.Client }

// NewGuestAccessKeyResource — конструктор для реестра провайдера.
func NewGuestAccessKeyResource() resource.Resource { return &guestAccessKeyResource{} }

func (r *guestAccessKeyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameComputeGuestAccessKey
}

func (r *guestAccessKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *guestAccessKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Ключ входа гостя — публичный ключ, с которым арендатор входит в свою машину.\n\n" +
			"Ресурс со своим сроком жизни, а не поле машины: ключ, переданный полем при создании, " +
			"нельзя ни отозвать, ни заменить, ни узнать, где ещё он используется.\n\n" +
			"Закрытая половина сюда не кладётся НИКОГДА — ни полем, ни через состояние. " +
			"Состояние Terraform хранится открытым текстом, и закрытый ключ в нём означал бы, " +
			"что доступ к файлу состояния равен доступу в машину.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор ключа. Именно им машина на ключ ссылается.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт ключ."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Человекочитаемое имя, уникальное в проекте. Косметическое: " +
					"меняется свободно, в ссылке из машины не участвует."},
			"public_key": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Материал публичного ключа в форме, которую понимает гостевая система.\n\n" +
					"**Изменение пересоздаёт ключ, и это не оформление, а существо**: другой материал — " +
					"другой доступ, и подменить его на месте значило бы сменить того, кто может войти, " +
					"сохранив прежний идентификатор. Пересоздание выдаёт НОВЫЙ идентификатор, поэтому " +
					"машины, ссылающиеся на прежний, обязаны быть перепривязаны тем же применением — " +
					"их план это покажет."},
			"fingerprint": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Отпечаток, вычисленный краем из материала. Сверьте его с тем, что " +
					"видите у себя, — так узнаёте, тот ли ключ доехал. Годится для `precondition`."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"created_at": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *guestAccessKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan guestAccessKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.CreateGuestAccessKeyRequest{
		ProjectId: plan.ProjectID.ValueString(),
		Name:      plan.Name.ValueString(),
		PublicKey: plan.PublicKey.ValueString(),
		Labels:    mapFromTF(ctx, plan.Labels),
	}

	// Ключ идемпотентности строится из адреса и МАТЕРИАЛА: повтор того же применения
	// после обрыва ответа не должен родить второй ключ, а другой материал под тем же
	// именем обязан быть другой подачей, иначе повтор молча вернул бы прежний ключ.
	raw, _ := json.Marshal(map[string]string{
		"projectId": body.ProjectId, "name": body.Name, "publicKey": body.PublicKey,
	})
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameComputeGuestAccessKey, plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, guestAccessKeysPath, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание ключа не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Край отверг создание ключа", out.Message)
		return
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ на создание ключа не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Создание ключа не завершилось", err.Error())
		return
	}
	id, ok := done.MetadataString("guestAccessKeyId")
	if !ok || id == "" {
		resp.Diagnostics.AddError("Край не сообщил идентификатор ключа",
			"Операция успешна, но метаданные не содержат guestAccessKeyId — записывать в состояние нечего.")
		return
	}

	// Идентификатор в состояние ДО обратного чтения: ключ уже создан, и состояние без
	// него означало бы, что следующее применение заведёт второй.
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k, rerr := r.readGuestAccessKey(ctx, id, true)
	if rerr != nil {
		resp.Diagnostics.AddError("Ключ создан, но не прочитан обратно", rerr.Error())
		return
	}
	applyGuestAccessKey(ctx, &plan, k)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *guestAccessKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state guestAccessKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k, err := r.readGuestAccessKey(ctx, state.ID.ValueString(), false)
	if err == nil {
		applyGuestAccessKey(ctx, &state, k)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение ключа не удалось", err.Error())
		return
	}

	// «Не найдено» само по себе не означает «удалён»: тот же ответ приходит при утрате
	// доступа, потому что край скрывает существование. Убрать ключ из состояния по
	// неразличённому ответу значит на следующем применении завести второй — а прежний
	// останется жить, и войти им по-прежнему смогут.
	verdict, verr := r.c.ConfirmAbsence(ctx, guestAccessKeysPath, client.ScopeProject,
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к проекту утрачен",
			"Ключ "+state.ID.ValueString()+" не читается, и список проекта отвечает отказом. "+
				"Это событие прав, а не удаление.")
	default:
		msg := "Ключ " + state.ID.ValueString() + " не найден, и подтвердить отсутствие нечем: " +
			"в проекте не видно ни одного ключа. Если он действительно удалён вне Terraform, " +
			"уберите его из состояния вручную:\n  terraform state rm <адрес ресурса>"
		if verr != nil {
			msg += "\n\nПодробности: " + verr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие ключа не подтверждено", msg)
	}
}

func (r *guestAccessKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state guestAccessKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &computev1.UpdateGuestAccessKeyRequest{GuestAccessKeyId: state.ID.ValueString()}
	var paths []string
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueString()
		paths = append(paths, "name")
	}
	if !plan.Labels.Equal(state.Labels) {
		body.Labels = mapFromTF(ctx, plan.Labels)
		paths = append(paths, "labels")
	}

	if len(paths) > 0 {
		body.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
		httpResp, err := r.c.Do(ctx, http.MethodPatch, guestAccessKeysPath+"/"+state.ID.ValueString(), body, nil)
		if err != nil {
			resp.Diagnostics.AddError("Изменение ключа не отправлено", err.Error())
			return
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			resp.Diagnostics.AddError("Край отверг изменение ключа", out.Message)
			return
		}
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Изменение ключа не завершилось", err.Error())
				return
			}
		}
	}

	k, rerr := r.readGuestAccessKey(ctx, state.ID.ValueString(), false)
	if rerr != nil {
		resp.Diagnostics.AddError("Ключ изменён, но не прочитан обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	applyGuestAccessKey(ctx, &plan, k)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *guestAccessKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state guestAccessKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	httpResp, err := r.c.Do(ctx, http.MethodDelete, guestAccessKeysPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление ключа не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Удаление ключа не завершилось", err.Error())
			}
		}
	case client.OutcomeNotFound:
		verdict, _ := r.c.ConfirmAbsence(ctx, guestAccessKeysPath, client.ScopeProject,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			resp.Diagnostics.AddError("Удаление ключа не подтверждено",
				"Край ответил «не найдено», но отсутствие не подтверждено (исход: "+verdict.String()+"). "+
					"Молча снять ключ из состояния значило бы объявить доступ отозванным, не установив этого.")
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление ключа", out.Message)
	}
}

func (r *guestAccessKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readGuestAccessKey читает ключ, при необходимости пережидая окно материализации прав.
func (r *guestAccessKeyResource) readGuestAccessKey(ctx context.Context, id string, retryAuthz bool) (*guestAccessKeyJSON, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		httpResp, err := r.c.Do(ctx, http.MethodGet, guestAccessKeysPath+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(httpResp)
		switch out.Kind {
		case client.OutcomeOK:
			var k guestAccessKeyJSON
			if err := json.Unmarshal(httpResp.Body, &k); err != nil {
				return nil, fmt.Errorf("разбор ключа: %w", err)
			}
			return &k, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к ключу %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение ключа %s: %s", id, out.Message)
		}
	}
}

type guestAccessKeyJSON struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	Name        string            `json:"name"`
	PublicKey   string            `json:"publicKey"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   string            `json:"createdAt"`
}

// applyGuestAccessKey переносит ответ края в состояние.
//
// Материал ключа НЕ перезаписывается ответом края: край вправе вернуть его в
// канонизованном виде (иной пробел, иной комментарий в хвосте), и записав это
// в состояние, мы получили бы вечное расхождение с тем, что написал арендатор,
// а вместе с ним — пересоздание ключа на каждом плане. Что материал доехал
// верно, свидетельствует отпечаток, а не побайтовое эхо.
func applyGuestAccessKey(ctx context.Context, m *guestAccessKeyModel, k *guestAccessKeyJSON) {
	m.ID = types.StringValue(k.ID)
	m.ProjectID = types.StringValue(k.ProjectID)
	m.Name = types.StringValue(k.Name)
	m.Fingerprint = types.StringValue(k.Fingerprint)
	m.Labels = mapToTF(ctx, k.Labels)
	m.CreatedAt = types.StringValue(k.CreatedAt)

	// Исключение ровно одно и оно про ввоз: при импорте в состоянии материала нет
	// вовсе, и оставить его пустым значило бы предъявить арендатору план, снимающий
	// ключ, которого он не трогал.
	if m.PublicKey.IsNull() || m.PublicKey.IsUnknown() {
		m.PublicKey = types.StringValue(k.PublicKey)
	}
}
