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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const networksPath = "/vpc/v1/networks"

// networkModel — состояние ресурса.
//
// Блоки адресов — полноценный аргумент (Optional + Computed): задаются при создании и
// приводятся при изменении. Приведение идёт НЕ маской обновления (поле в ней неизменяемо),
// а парой действий края, и ОДИНАКОВО для обеих семей — см. reconcileCidrBlocks.
//
// Здесь стояло «блоки не объявлены аргументом, их приведение живёт в следующей задаче».
// Это пережило свой предмет: аргумент объявлен, приведение написано — и утверждение
// объясняло бы следующему читателю отсутствие того, что у него перед глазами.
type networkModel struct {
	ID                     types.String `tfsdk:"id"`
	ProjectID              types.String `tfsdk:"project_id"`
	Name                   types.String `tfsdk:"name"`
	Description            types.String `tfsdk:"description"`
	Labels                 types.Map    `tfsdk:"labels"`
	CreatedAt              types.String `tfsdk:"created_at"`
	DefaultSecurityGroupID types.String `tfsdk:"default_security_group_id"`
	DefaultRouteTableID    types.String `tfsdk:"default_route_table_id"`
	IPv4CidrBlocks         types.List   `tfsdk:"ipv4_cidr_blocks"`
	IPv6CidrBlocks         types.List   `tfsdk:"ipv6_cidr_blocks"`
}

type networkResource struct{ c *client.Client }

// NewNetworkResource — конструктор для реестра провайдера.
func NewNetworkResource() resource.Resource { return &networkResource{} }

func (r *networkResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameVPCNetwork
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
					"набора и вызывает их сам. Блок, внутри которого ещё живёт подсеть, край " +
					"снять не даст, и apply остановится на его отказе."},
			"ipv6_cidr_blocks": schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Объявленные блоки IPv6. Приводятся ТЕМИ ЖЕ действиями края, " +
					"тем же вычислением разницы и в тех же вызовах, что IPv4: обе семьи уходят " +
					"одним запросом на действие, поэтому правка IPv4 и IPv6 в одном плане " +
					"применяется краем одной транзакцией."},
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
	// Ключ повторной подачи считается по ВСЕМУ телу запроса, а не по трём его полям.
	//
	// Прежняя редакция собирала для ключа отдельную карту из проекта, имени и описания —
	// то есть блоки супернета, метки и решение о группе безопасности по умолчанию в ключ
	// не входили. Край ключ соблюдает, поэтому следствие ровно одно и оно неприятное:
	// создание, отвергнутое ПО СОСТАВУ запроса (негодный блок, негодная метка), после
	// правки настройки воспроизводилось бы ТОЙ ЖЕ операцией — пользователь исправляет
	// конфигурацию, получает прежний отказ и заключает, что правка не применилась.
	//
	// С полным телом держатся оба свойства сразу: повтор ТОГО ЖЕ запроса (потерян ответ,
	// оборвалась сеть) не создаёт дубль, а изменённый запрос — это другой запрос, и он
	// уходит на край заново. Ошибка сборки тела здесь не проглатывается: пустой ключ
	// молча снял бы защиту от дубля.
	raw, err := client.MarshalBody(body)
	if err != nil {
		resp.Diagnostics.AddError("Тело запроса не собрано", err.Error())
		return
	}
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(
		typeNameVPCNetwork, plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), raw)}

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
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт по сообщению на каждое
	// поле вместо одного — про само чтение.
	sealUnknowns(&plan)
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
	verdict, verr := r.c.ConfirmAbsence(ctx, networksPath, client.ScopeProject,
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
	//
	// Разница считается по КАЖДОЙ семье отдельно, а применяется ОБЕИМИ сразу: семьи
	// независимы (неизвестный план одной не мешает привести другую), но края обеих
	// касается один и тот же вызов — см. reconcileCidrBlocks.
	var delta cidrDelta
	delta.addV4, delta.removeV4 = cidrFamilyDiff(ctx, plan.IPv4CidrBlocks, state.IPv4CidrBlocks)
	delta.addV6, delta.removeV6 = cidrFamilyDiff(ctx, plan.IPv6CidrBlocks, state.IPv6CidrBlocks)
	if err := r.reconcileCidrBlocks(ctx, state.ID.ValueString(), delta); err != nil {
		resp.Diagnostics.AddError("Приведение блоков адресов сети не удалось", err.Error())
		// Обещание, записанное у reconcileCidrBlocks («в состоянии окажется ФАКТИЧЕСКИ
		// применённое»), держится ЗДЕСЬ — иначе оно было бы обещанием без исполнителя:
		// пара действий общей транзакции не имеет, поэтому добавление могло примениться,
		// а снятие отказать. Ранний возврат без чтения оставлял в состоянии ПРЕЖНИЙ набор
		// блоков, то есть провайдер отчитывался о супернете, которого на краю уже нет.
		//
		// Читаем обратно и пишем факт; отказ при этом остаётся, apply падает. Пишется
		// СОСТОЯНИЕ, а не план: в плане вычисляемые атрибуты неизвестны, а неизвестное
		// после apply Terraform не принимает даже вместе с ошибкой. Провал самого чтения
		// молчалив намеренно: первичный отказ уже назван, и второе сообщение про чтение
		// увело бы от причины.
		if net, rerr := r.readNetwork(ctx, state.ID.ValueString(), false); rerr == nil {
			applyNetwork(ctx, &state, net)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		}
		return
	}

	// Пустая маска НИКОГДА не отправляется: у ресурсов домена она означает полнообъектную
	// запись, при которой поля берутся как нули из запроса, — самый дешёвый способ молча
	// стереть чужую настройку. Пустая маска здесь означает «менять ЭТИМ запросом нечего»:
	// например, правились ТОЛЬКО блоки адресов, у которых свой путь.
	//
	// «Нечего менять» НЕ означает «не идём на край»: блоки выше уже приведены, а обратное
	// чтение ниже выполняется всегда — и выйти отсюда рано нельзя именно поэтому. В плане
	// вычисляемые атрибуты ещё НЕИЗВЕСТНЫ, а состояние неизвестного не хранит: ранний
	// возврат стоил ошибки «invalid result object after apply» на живом стенде, причём
	// изменение к тому моменту уже применилось — дефект был только в том, что провайдер
	// рассказал о результате.
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
		verdict, cerr := r.c.ConfirmAbsence(ctx, networksPath, client.ScopeProject,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			detail := "Край ответил «не найдено», но подтвердить отсутствие сети " +
				state.ID.ValueString() + " не удалось (исход: " + verdict.String() + "). " +
				"Возможно, доступ отозван, а сеть цела."
			// Причина неудавшегося подтверждения называется так же, как на пути чтения:
			// без неё «исход: Ambiguous» не отличает «в проекте пусто» от «список не
			// прочитан вовсе», и оператор ищет разницу перебором.
			if cerr != nil {
				detail += "\n\nПодробности: " + cerr.Error()
			}
			resp.Diagnostics.AddError("Удаление сети не подтверждено", detail)
		}
	default:
		resp.Diagnostics.AddError("Край отверг удаление сети", out.Message)
	}
}

// ImportState принимает идентификатор ресурса.
//
// Проверка формата — ОБЩАЯ (importByID), и это не косметика. Здесь стояла своя копия,
// сверявшая строку только с префиксом сети; её комментарий при этом заявлял проверку по
// «общему каталогу префиксов платформы», которой в теле не было. Общая проверка спрашивает
// каталог по-настоящему (ids.HasKnownPrefix), поэтому ловит и опечатку в самой таблице
// видов провайдера, а не только чужой ввод.
//
// Дисциплина та же, что у сервисов: заведомо негодный идентификатор получает терминальный
// отказ с внятным текстом, а не уезжает в сеть, чтобы вернуться оттуда «ресурс не найден» —
// ответом, который для строки, не являющейся идентификатором, не значит ничего.
func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "Сеть", ids.PrefixNetwork, req, resp)
}

// readNetwork читает сеть по идентификатору.
//
// Ожидание и различение исходов — ОБЩИЕ (readByID); здесь остаётся только разбор тела в
// форму сети. Здесь стояла своя копия цикла со сроком ожидания 20 с — тем самым, который
// на общем пути уже был поднят до 60 с по наблюдению: при одиночном создании право на свой
// свежий ресурс видно почти сразу, но в одном apply с несколькими ресурсами материализация
// идёт под конкуренцией, и на 20 с обратное чтение отдавало «не найдено» на ТОЛЬКО ЧТО
// созданном ресурсе. Две копии одного механизма разошлись ровно тем, что одну из них
// починили.
//
// retryAuthz включается ТОЛЬКО на первом обратном чтении после создания. В обычном чтении
// такого ретрая нет — там он замаскировал бы настоящий отзыв доступа.
func (r *networkResource) readNetwork(ctx context.Context, id string, retryAuthz bool) (*networkJSON, error) {
	raw, err := readByID(ctx, r.c, networksPath, id, retryAuthz)
	if err != nil {
		return nil, err
	}
	var n networkJSON
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("разбор сети: %w", err)
	}
	return &n, nil
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

// Суффикс-действия края над супернетом. Пути берутся ДОСЛОВНО: у vpc они пишутся через
// дефис, у nlb — слитно, и никакое правило вывода пути из имени метода здесь не работает.
const (
	verbAddCidrBlocks    = "add-cidr-blocks"
	verbRemoveCidrBlocks = "remove-cidr-blocks"
)

// cidrDelta — приведение супернета сети: что добавить и что снять, по семьям.
type cidrDelta struct {
	addV4, addV6       []string
	removeV4, removeV6 []string
}

func (d cidrDelta) hasAdd() bool    { return len(d.addV4) > 0 || len(d.addV6) > 0 }
func (d cidrDelta) hasRemove() bool { return len(d.removeV4) > 0 || len(d.removeV6) > 0 }

// cidrFamilyDiff — что добавить и что снять у ОДНОЙ семьи блоков.
//
// Неизвестный план исключает семью из приведения целиком, и это не перестраховка: на
// неизвестном значении stringsFromTF отдаёт nil, поэтому «пока не знаю» стало бы
// неотличимо от «пусто», а разница с текущим набором вышла бы «снять всё». Семья, чей
// план неизвестен (значение ссылается на ещё не созданный ресурс), приводится следующим
// применением — когда значение станет известным. Вторая семья от этого не страдает:
// считаются они порознь.
func cidrFamilyDiff(ctx context.Context, planned, current types.List) (add, remove []string) {
	if planned.IsUnknown() || planned.Equal(current) {
		return nil, nil
	}
	return diffSets(stringsFromTF(ctx, current), stringsFromTF(ctx, planned))
}

// reconcileCidrBlocks приводит набор блоков адресов сети к желаемому — ОБЕ семьи наравне.
//
// Почему паритет здесь обязателен. До этой правки IPv6 уходил на край только при создании:
// при изменении сверялся и приводился один IPv4, а `ipv6_cidr_blocks` при этом оставался
// объявленным изменяемым аргументом. Пользователь правил супернет IPv6, видел его в плане,
// получал «apply завершён» — и не получал ничего: сеть жила без блоков, которые считала
// объявленными и он, и состояние Terraform. Это ровно запрещённый класс «принято и
// проигнорировано»: поле, которое принято и не применяется, хуже отвергнутого — отказ
// виден сразу, а несделанное только по последствиям и в чужой отладке. Исходов у такого
// поля три, и «молча выбросить» среди них нет: применять, отвергать явно, или объявить
// пересоздающим. Здесь край применять УМЕЕТ — значит применяем.
//
// Почему обе семьи ОДНИМ вызовом на действие, а не двумя вызовами подряд. Это не экономия
// запроса: край принимает обе семьи одним сообщением контракта и обрабатывает их в одной
// writer-транзакции под замком строки сети — обе колонки супернета пишутся одним UPDATE,
// событие изменения тоже одно. Два вызова дали бы две операции и два коммита, между
// которыми apply может оборваться, и сеть осталась бы с приведённым IPv4 и неприведённым
// IPv6 — в состоянии, которого нет ни в настройке, ни в прежнем состоянии.
//
// Пара действий (добавить / снять) общей транзакции не имеет by construction. Отсюда два
// правила, оба видны в коде:
//
//   - порядок фиксирован — сначала добавление, потом снятие;
//   - каждое действие доводится до конца прежде следующего.
//
// Пустое действие не отправляется: край отвергает вызов, в котором обе семьи пусты
// (`ipv4_cidr_blocks or ipv6_cidr_blocks is required`), и такой запрос завалил бы apply,
// которому нечего было делать.
//
// Компенсации нет намеренно: откат добавленных блоков был бы второй мутацией ради успеха
// собственного плана. При отказе снятия apply падает, называя обе половины, а в состоянии
// окажется ФАКТИЧЕСКИ применённое — его принесёт обратное чтение.
func (r *networkResource) reconcileCidrBlocks(ctx context.Context, id string, d cidrDelta) error {
	if d.hasAdd() {
		// Тело — ТИП КОНТРАКТА, а не собранная руками карта. Прежняя редакция строила
		// map[string]any с единственным ключом «ipv4CidrBlocks» — в него уезжали и блоки
		// IPv6. Край молча отбрасывает ключи, которых не ждёт, и не присланных не
		// требует, поэтому промах не давал ни отказа, ни предупреждения: запрос уходил
		// успешным и не делал того, ради чего послан. С типом контракта такой промах
		// невозможен by construction — имя поля проверяет компилятор.
		body := &vpcv1.AddNetworkCidrBlocksRequest{
			NetworkId:      id,
			Ipv4CidrBlocks: d.addV4,
			Ipv6CidrBlocks: d.addV6,
		}
		if err := awaitMutation(ctx, r.c, http.MethodPost,
			networksPath+"/"+id+":"+verbAddCidrBlocks, body); err != nil {
			return fmt.Errorf("добавление блоков (IPv4 %v, IPv6 %v): %w", d.addV4, d.addV6, err)
		}
	}

	if d.hasRemove() {
		body := &vpcv1.RemoveNetworkCidrBlocksRequest{
			NetworkId:      id,
			Ipv4CidrBlocks: d.removeV4,
			Ipv6CidrBlocks: d.removeV6,
		}
		if err := awaitMutation(ctx, r.c, http.MethodPost,
			networksPath+"/"+id+":"+verbRemoveCidrBlocks, body); err != nil {
			// Про уже применённое добавление говорится, только если оно было: иначе
			// сообщение приписывало бы краю мутацию, которой не происходило, и читатель
			// искал бы её след.
			if d.hasAdd() {
				return fmt.Errorf("блоки (IPv4 %v, IPv6 %v) добавлены, но снятие блоков "+
					"(IPv4 %v, IPv6 %v) не удалось: %w",
					d.addV4, d.addV6, d.removeV4, d.removeV6, err)
			}
			return fmt.Errorf("снятие блоков (IPv4 %v, IPv6 %v): %w", d.removeV4, d.removeV6, err)
		}
	}
	return nil
}
