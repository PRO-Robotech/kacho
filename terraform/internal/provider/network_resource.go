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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const networksPath = "/vpc/v1/networks"

// networkModel — состояние ресурса.
//
// Блоки адресов ЗДЕСЬ не объявлены аргументом: сеть их не принимает в Update, а приводятся
// они отдельными действиями края. В TF-1 они только читаются (Computed) — их приведение
// живёт в следующей задаче плана и получит свои сценарии.
type networkModel struct {
	ID                         types.String `tfsdk:"id"`
	ProjectID                  types.String `tfsdk:"project_id"`
	Name                       types.String `tfsdk:"name"`
	Description                types.String `tfsdk:"description"`
	Labels                     types.Map    `tfsdk:"labels"`
	CreateDefaultSecurityGroup types.Bool   `tfsdk:"create_default_security_group"`
	CreatedAt                  types.String `tfsdk:"created_at"`
	DefaultSecurityGroupID     types.String `tfsdk:"default_security_group_id"`
	DefaultRouteTableID        types.String `tfsdk:"default_route_table_id"`
	IPv4CidrBlocks             types.List   `tfsdk:"ipv4_cidr_blocks"`
	IPv6CidrBlocks             types.List   `tfsdk:"ipv6_cidr_blocks"`
}

type networkResource struct{ c *client.Client }

// NewNetworkResource — конструктор для реестра провайдера.
func NewNetworkResource() resource.Resource { return &networkResource{} }

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_network"
}

func (r *networkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *networkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Сеть VPC — область адресации, внутри которой живут подсети.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Неизменяемый идентификатор сети. По нему выполняется импорт.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Проект-владелец. Сменить его у существующей сети нельзя — ресурс будет пересоздан.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Имя сети в пределах проекта. " +
					"Провайдер требует его СТРОЖЕ края: имя — единственный способ найти уже " +
					"созданный ресурс, если ответ на создание потерялся, и без него повтор " +
					"создал бы дубль.",
			},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Произвольное описание."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки вида ключ-значение."},
			"create_default_security_group": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Создавать ли группу безопасности по умолчанию вместе с сетью. " +
					"Край это значение НЕ возвращает, поэтому после импорта оно принимается на " +
					"веру. Изменение пересоздаёт сеть.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},
			"default_security_group_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Группа безопасности по умолчанию, созданная краем."},
			"default_route_table_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Таблица маршрутов по умолчанию, созданная краем."},
			"ipv4_cidr_blocks": schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Объявленные блоки IPv4 — супернет сети. Подсеть обязана лежать " +
					"внутри одного из них, поэтому сеть без блоков подсеть не примет. " +
					"Изменяется НЕ обычной правкой, а отдельными действиями края " +
					"(`:add-cidr-blocks` / `:remove-cidr-blocks`) — провайдер вычисляет разницу " +
					"набора и вызывает их сам."},
			"ipv6_cidr_blocks": schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Объявленные блоки IPv6. Приводятся теми же действиями, что IPv4."},
		},
	}
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.CreateNetworkRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),

		// Супернет задаётся ПРИ СОЗДАНИИ: сеть без объявленных блоков отвергает подсеть,
		// поэтому «создам сеть, блоки добавлю потом» — не рабочий порядок для того, кто
		// сразу описывает сеть вместе с подсетями.
		Ipv4CidrBlocks: stringsFromTF(ctx, plan.IPv4CidrBlocks),
		Ipv6CidrBlocks: stringsFromTF(ctx, plan.IPv6CidrBlocks),
	}
	if !plan.CreateDefaultSecurityGroup.IsNull() {
		v := plan.CreateDefaultSecurityGroup.ValueBool()
		body.CreateDefaultSecurityGroup = &v
	}

	raw, _ := json.Marshal(map[string]any{
		"projectId": body.ProjectId, "name": body.Name, "description": body.Description,
	})
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		"kacho_vpc_network", plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

	httpResp, err := r.c.Do(ctx, http.MethodPost, networksPath, body, hdr)
	if err != nil {
		resp.Diagnostics.AddError("Создание сети не отправлено", err.Error())
		return
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		resp.Diagnostics.AddError("Край отверг создание сети", out.Message)
		return
	}

	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err != nil {
		resp.Diagnostics.AddError("Ответ на создание сети не разобран", err.Error())
		return
	}
	done, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Создание сети не завершилось", err.Error())
		return
	}
	id, ok := done.MetadataString("networkId")
	if !ok || id == "" {
		resp.Diagnostics.AddError("Край не сообщил идентификатор сети",
			"Операция завершилась успехом, но её метаданные не содержат networkId. "+
				"Записывать в состояние нечего, а ресурс, возможно, создан — проверьте проект вручную.")
		return
	}

	// Идентификатор в состояние — ДО первого обратного чтения.
	//
	// Это единственная точка, где потеря необратима: ресурс создан, apply прерван,
	// состояние пусто — следующий apply создаст дубль. Поэтому сначала записываем то, что
	// уже знаем, и лишь затем идём читать.
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	net, diagErr := r.readNetwork(ctx, id, true)
	if diagErr != nil {
		resp.Diagnostics.AddError("Сеть создана, но не прочитана обратно", diagErr.Error())
		return
	}
	applyNetwork(ctx, &plan, net)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	net, err := r.readNetwork(ctx, state.ID.ValueString(), false)
	if err == nil {
		applyNetwork(ctx, &state, net)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение сети не удалось", err.Error())
		return
	}

	// Одиночное «не найдено» ничего не устанавливает: тот же ответ приходит при отказе в
	// доступе, и он побайтово равен настоящему отсутствию.
	verdict, verr := r.c.ConfirmAbsence(ctx, networksPath,
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
		// Ресурс жив, первый ответ был окном материализации прав — состояние не трогаем.
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к проекту утрачен",
			"Сеть "+state.ID.ValueString()+" не читается, и список проекта тоже отвечает отказом. "+
				"Это событие ПРАВ, а не удаление ресурса: продолжать нельзя, иначе план предложит "+
				"пересоздать инфраструктуру, которая цела.")
	default:
		msg := "Сеть " + state.ID.ValueString() + " не найдена, и подтвердить её отсутствие нечем: " +
			"в проекте не видно ни одной сети. Различить «сеть удалена» и «доступ к ней отозван» " +
			"край не позволяет — ответы совпадают дословно.\n\n" +
			"Если сеть действительно удалена вне Terraform, уберите её из состояния вручную:\n" +
			"  terraform state rm <адрес ресурса>"
		if verr != nil {
			msg += "\n\nПодробности: " + verr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие сети не подтверждено", msg)
	}
}

func (r *networkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.UpdateNetworkRequest{NetworkId: state.ID.ValueString()}
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

	// Блоки адресов приводятся ОТДЕЛЬНЫМИ действиями края, а не маской: поле неизменяемо
	// в Update и было бы отвергнуто. Порядок пары фиксирован — сначала добавление, потом
	// удаление: обратный проводит сеть через состояние без нужного супернета, а удаление
	// блока с подсетями детерминированно отказывает, то есть падает первым и оставляет
	// план невыполненным целиком.
	if !plan.IPv4CidrBlocks.Equal(state.IPv4CidrBlocks) && !plan.IPv4CidrBlocks.IsUnknown() {
		if err := r.reconcileCidrBlocks(ctx, state.ID.ValueString(),
			stringsFromTF(ctx, state.IPv4CidrBlocks), stringsFromTF(ctx, plan.IPv4CidrBlocks)); err != nil {
			resp.Diagnostics.AddError("Приведение блоков адресов сети не удалось", err.Error())
			return
		}
	}

	// Пустая маска НИКОГДА не отправляется: у части ресурсов домена она означает
	// полнообъектную запись, при которой поля берутся как нули из запроса, — самый дешёвый
	// способ молча стереть чужую настройку. Нечего менять — не идём на край вовсе.
	// Пустая маска означает «менять этим запросом нечего» — например, правились ТОЛЬКО
	// блоки адресов, у которых свой путь. Запрос не отправляется, но выйти здесь нельзя:
	// в плане вычисляемые атрибуты ещё НЕИЗВЕСТНЫ, а состояние неизвестного не хранит.
	// Ранний возврат стоил ошибки «invalid result object after apply» на живом стенде,
	// причём изменение к тому моменту уже применилось — то есть дефект был только в том,
	// что провайдер рассказал о результате.
	if len(paths) > 0 {
		body.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}

		httpResp, err := r.c.Do(ctx, http.MethodPatch, networksPath+"/"+state.ID.ValueString(), body, nil)
		if err != nil {
			resp.Diagnostics.AddError("Изменение сети не отправлено", err.Error())
			return
		}
		if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
			resp.Diagnostics.AddError("Край отверг изменение сети", out.Message)
			return
		}
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Изменение сети не завершилось", err.Error())
				return
			}
		}
	}

	net, rerr := r.readNetwork(ctx, state.ID.ValueString(), false)
	if rerr != nil {
		resp.Diagnostics.AddError("Сеть изменена, но не прочитана обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	applyNetwork(ctx, &plan, net)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	httpResp, err := r.c.Do(ctx, http.MethodDelete, networksPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление сети не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
			if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
				resp.Diagnostics.AddError("Удаление сети не завершилось", err.Error())
			}
		}
	case client.OutcomeNotFound:
		// Цель достигнута — но только если отсутствие подтверждено. Тот же ответ приходит
		// при отказе в доступе, и безусловное «404 значит удалено» оставило бы живой
		// ресурс вне состояния.
		verdict, _ := r.c.ConfirmAbsence(ctx, networksPath,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			resp.Diagnostics.AddError("Удаление сети не подтверждено",
				"Край ответил «не найдено», но подтвердить отсутствие сети "+
					state.ID.ValueString()+" не удалось (исход: "+verdict.String()+"). "+
					"Возможно, доступ отозван, а сеть цела.")
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление сети", out.Message)
	}
}

// ImportState принимает идентификатор ресурса.
//
// Формат проверяется ЗДЕСЬ, общим каталогом префиксов платформы (pkg/ids) — до любого
// обращения к краю. Это та же дисциплина, что у сервисов: заведомо негодный идентификатор
// получает терминальный отказ с внятным текстом, а не уезжает в сеть, чтобы вернуться
// оттуда «ресурс не найден» — ответом, который для строки, не являющейся идентификатором,
// не значит ничего.
func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !ids.IsValid(req.ID, ids.PrefixNetwork) {
		resp.Diagnostics.AddError("Негодный идентификатор сети",
			"Строка "+strconv.Quote(req.ID)+" не является идентификатором сети Kachō: "+
				"он начинается с «"+ids.PrefixNetwork+"» и состоит из знаков crockford-base32. "+
				"Идентификатор виден в выводе списка сетей и в консоли.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readNetwork читает сеть по идентификатору.
//
// retryAuthz включается ТОЛЬКО на первом обратном чтении после создания: право на
// собственный свежий ресурс материализуется не мгновенно. В обычном чтении такого ретрая
// нет — там он замаскировал бы настоящий отзыв доступа.
func (r *networkResource) readNetwork(ctx context.Context, id string, retryAuthz bool) (*networkJSON, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		httpResp, err := r.c.Do(ctx, http.MethodGet, networksPath+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(httpResp)
		switch out.Kind {
		case client.OutcomeOK:
			var n networkJSON
			if err := json.Unmarshal(httpResp.Body, &n); err != nil {
				return nil, fmt.Errorf("разбор сети: %w", err)
			}
			return &n, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к сети %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение сети %s: %s", id, out.Message)
		}
	}
}

// networkJSON — ответ края. Разбор терпим к неизвестным полям: край добавляет их вперёд
// провайдера, и строгий разбор ломал бы каждое чтение на первом же новом поле.
type networkJSON struct {
	ID                     string            `json:"id"`
	ProjectID              string            `json:"projectId"`
	CreatedAt              string            `json:"createdAt"`
	Name                   string            `json:"name"`
	Description            string            `json:"description"`
	Labels                 map[string]string `json:"labels"`
	DefaultSecurityGroupID string            `json:"defaultSecurityGroupId"`
	DefaultRouteTableID    string            `json:"defaultRouteTableId"`
	IPv4CidrBlocks         []string          `json:"ipv4CidrBlocks"`
	IPv6CidrBlocks         []string          `json:"ipv6CidrBlocks"`
}

type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }

func asNotFound(err error, target **notFoundError) bool {
	nf, ok := err.(*notFoundError)
	if ok {
		*target = nf
	}
	return ok
}

// reconcileCidrBlocks приводит набор блоков адресов сети к желаемому.
//
// Это ПАРА независимых действий края, каждое со своей операцией: общей транзакции у них
// нет by construction. Отсюда два правила, оба видны в коде:
//
//   - порядок фиксирован — сначала добавление, потом удаление;
//   - каждое действие доводится до конца прежде следующего.
//
// Компенсации нет намеренно: откат добавленных блоков был бы второй мутацией ради успеха
// собственного плана. При отказе второго действия apply падает, называя обе половины, а в
// состоянии окажется ФАКТИЧЕСКИ применённое — его принесёт обратное чтение.
func (r *networkResource) reconcileCidrBlocks(ctx context.Context, id string, have, want []string) error {
	add, remove := diffSets(have, want)

	if len(add) > 0 {
		if err := r.cidrVerb(ctx, id, "add-cidr-blocks", add); err != nil {
			return fmt.Errorf("добавление блоков %v: %w", add, err)
		}
	}
	if len(remove) > 0 {
		if err := r.cidrVerb(ctx, id, "remove-cidr-blocks", remove); err != nil {
			return fmt.Errorf("блоки %v добавлены, но удаление блоков %v не удалось: %w",
				add, remove, err)
		}
	}
	return nil
}

// cidrVerb вызывает одно действие края над блоками и дожидается его операции.
//
// Путь берётся ДОСЛОВНО: у vpc суффикс-действия пишутся через дефис, у nlb — иначе, и
// никакое правило вывода пути из имени метода здесь не работает.
func (r *networkResource) cidrVerb(ctx context.Context, id, verb string, blocks []string) error {
	body := map[string]any{"networkId": id, "ipv4CidrBlocks": blocks}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	httpResp, err := r.c.DoRaw(ctx, http.MethodPost, networksPath+"/"+id+":"+verb, raw, nil)
	if err != nil {
		return err
	}
	if out := client.Classify(httpResp); out.Kind != client.OutcomeOK {
		return fmt.Errorf("%s", out.Message)
	}
	var op client.Operation
	if err := json.Unmarshal(httpResp.Body, &op); err == nil && op.ID != "" {
		if _, err := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
			return err
		}
	}
	return nil
}
