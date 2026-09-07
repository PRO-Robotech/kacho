// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const cidrGroupsPath = "/vpc/v1/cidrGroups"

// cidrGroupModel — состояние ресурса «именованный набор префиксов».
//
// Состав — полноценный аргумент (Optional + Computed): задаётся при создании и приводится
// при изменении. Приведение идёт НЕ маской обновления (состава в ней нет вовсе), а парой
// действий края, и ОДИНАКОВО для обеих семей — см. reconcileBlocks.
type cidrGroupModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Labels         types.Map    `tfsdk:"labels"`
	CreatedAt      types.String `tfsdk:"created_at"`
	V4CidrBlocks   types.Set    `tfsdk:"v4_cidr_blocks"`
	V6CidrBlocks   types.Set    `tfsdk:"v6_cidr_blocks"`
	CidrBlockCount types.Int64  `tfsdk:"cidr_block_count"`
	UsedBy         types.List   `tfsdk:"used_by"`
}

// cidrGroupUsedByModel — кто ссылается на набор. Только чтение.
type cidrGroupUsedByModel struct {
	Referrer types.Object `tfsdk:"referrer"`
	Type     types.String `tfsdk:"type"`
	Owned    types.Bool   `tfsdk:"owned"`
}

type cidrGroupResource struct{ c *client.Client }

// NewVPCCidrGroupResource — конструктор для реестра провайдера.
func NewVPCCidrGroupResource() resource.Resource { return &cidrGroupResource{} }

func (r *cidrGroupResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameVPCCIDRGroup
}

func (r *cidrGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func cidrGroupReferrerAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
		"name": types.StringType,
	}
}

func cidrGroupUsedByObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"referrer": types.ObjectType{AttrTypes: cidrGroupReferrerAttrTypes()},
		"type":     types.StringType,
		"owned":    types.BoolType,
	}}
}

func (r *cidrGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Именованный набор префиксов — перечень сетей, на который " +
			"ссылаются правила групп безопасности вместо того, чтобы носить свою копию списка.\n\n" +
			"Предмет ресурса: перечень, повторённый в двадцати правилах, правится в двадцати " +
			"местах — и рано или поздно не во всех. Здесь предметом становится сам перечень: " +
			"правится один раз, действует везде, где на него сослались.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор набора (`cdg-…`). По нему " +
					"выполняется импорт и на него ссылаются правила.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Проект-владелец. Сменить его у существующего набора нельзя — " +
					"ресурс будет пересоздан.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя набора в пределах проекта.\n\n" +
					"Провайдер требует его СТРОЖЕ края: имя — единственный способ найти уже " +
					"созданный ресурс, если ответ на создание потерялся, и им же " +
					"подтверждается отсутствие набора при чтении. Без имени повтор создал бы " +
					"дубль, а удалённый вне Terraform набор никогда не снялся бы из состояния."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Произвольное описание."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки вида ключ-значение."},
			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},

			// НАБОР, а не список — и это не стиль.
			//
			// Край хранит членов в дочерней таблице и отдаёт их ОТСОРТИРОВАННЫМИ (по семье и
			// значению), а не в порядке, в каком их прислали. На списке любая конфигурация с
			// несортированным перечнем падала бы после применения на собственной
			// несогласованности Terraform («Provider produced inconsistent result after
			// apply»), хотя край сделал ровно то, о чём просили. У набора порядок не значим
			// by construction, а членство — это и есть предмет ресурса.
			"v4_cidr_blocks": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Члены набора семейства IPv4 в канонической записи " +
					"(младшие разряды узла нулевые). Не более 64 на семейство.\n\n" +
					"Изменяется НЕ обычной правкой, а отдельными действиями края " +
					"(`:add-cidr-blocks` / `:remove-cidr-blocks`) — провайдер вычисляет разницу " +
					"набора и вызывает их сам. Опустошить семью, на которую ссылается живое " +
					"правило, край не даст, и apply остановится на его отказе."},
			"v6_cidr_blocks": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Члены набора семейства IPv6. Приводятся ТЕМИ ЖЕ действиями " +
					"края и в тех же вызовах, что IPv4: обе семьи уходят одним запросом на " +
					"действие, поэтому правка IPv4 и IPv6 в одном плане применяется краем одной " +
					"транзакцией.\n\n" +
					"Семьи — РАЗНЫЕ поля, а не один список с выведенным семейством: член чужого " +
					"семейства отвергается на входе, и такое состояние невыразимо вовсе."},
			"cidr_block_count": schema.Int64Attribute{Computed: true,
				MarkdownDescription: "Число членов набора по обеим семьям. Считает край; " +
					"задать его нельзя."},

			"used_by": schema.ListNestedAttribute{Computed: true,
				MarkdownDescription: "Кто ссылается на набор — группы безопасности, чьи правила " +
					"его называют. Только чтение: список ведёт край, выводя его из ссылок самих " +
					"правил.\n\nОн же отвечает на вопрос «почему набор не удаляется»: набор с " +
					"живой ссылкой край удалить не даёт.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"referrer": schema.SingleNestedAttribute{Computed: true,
						MarkdownDescription: "Ресурс, который ссылается на набор.",
						Attributes: map[string]schema.Attribute{
							// Перечень назван тем, что край ПРОИЗВОДИТ: ссылаться на набор
							// умеет только правило группы безопасности, и вид проставляется
							// один. Обещать больше значило бы предложить строить условие на
							// ветке, которой нет.
							"type": schema.StringAttribute{Computed: true,
								MarkdownDescription: "Вид ссылающегося ресурса. Сегодня край " +
									"производит только `vpc.securityGroup`."},
							"id": schema.StringAttribute{Computed: true},
							"name": schema.StringAttribute{Computed: true,
								MarkdownDescription: "Имя на момент чтения — зеркало, а не источник истины."},
						}},
					"type": schema.StringAttribute{Computed: true,
						MarkdownDescription: "Характер связи. У набора край проставляет `USED_BY` " +
							"каждому ссылающемуся; `MANAGED_BY` контракт допускает, но на этом " +
							"ресурсе край его не производит."},
					"owned": schema.BoolAttribute{Computed: true,
						MarkdownDescription: "Распоряжается ли ссылающийся жизнью набора. У набора " +
							"всегда `false`: правило ссылается на него, но не владеет им — снятие " +
							"правила набора не удаляет."},
				}}},
		},
	}
}

// stringsFromSet — члены набора из значения Terraform. Неизвестное и null дают nil: «не
// задано» и «задано пустым» — разные вещи, и слать пустой набор вместо отсутствия значило
// бы стирать состав при каждом создании.
func stringsFromSet(ctx context.Context, s types.Set) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(s.Elements()))
	_ = s.ElementsAs(ctx, &out, false)
	return out
}

// setFromStrings — члены набора в значение Terraform. nil и пустой дают ПУСТОЙ набор, а не
// null: край всегда отвечает массивом, и null здесь означал бы расхождение на каждом плане.
func setFromStrings(ctx context.Context, in []string) types.Set {
	if in == nil {
		in = []string{}
	}
	v, diags := types.SetValueFrom(ctx, types.StringType, in)
	if diags.HasError() {
		return types.SetNull(types.StringType)
	}
	return v
}

// cidrGroupFamilyDiff — что добавить и что снять у ОДНОЙ семьи.
//
// Неизвестный план исключает семью из приведения целиком, и это не перестраховка: на
// неизвестном значении stringsFromSet отдаёт nil, поэтому «пока не знаю» стало бы
// неотличимо от «пусто», а разница с текущим составом вышла бы «снять всё». Семья, чей план
// неизвестен (значение ссылается на ещё не вычисленное), приводится следующим применением.
// Вторая семья от этого не страдает: считаются они порознь.
func cidrGroupFamilyDiff(ctx context.Context, planned, current types.Set) (add, remove []string) {
	if planned.IsUnknown() || planned.Equal(current) {
		return nil, nil
	}
	return diffSets(stringsFromSet(ctx, current), stringsFromSet(ctx, planned))
}

// ValidateConfig держит обещание, записанное у атрибута `name`.
//
// `Required: true` требует, чтобы атрибут был НАПИСАН, и пустую строку принимает; край
// принимает её тоже — его шаблон имени начинается с пустой альтернативы. То есть без этой
// проверки «провайдер требует имя СТРОЖЕ края» было бы объявлением без содержания.
//
// Предмет не косметический: пустое имя не даёт фильтра, и подтверждение отсутствия
// вырождается в контрольную страницу проекта. Тогда любой живой сосед читается как «наш
// набор на месте», и удалённый вне Terraform набор НИКОГДА не снимается из состояния.
func (r *cidrGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg cidrGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Судить можно ТОЛЬКО о том, что уже известно: имя, приходящее из ещё не вычисленной
	// переменной, неизвестно — и счесть его пустым значило бы отвергнуть законную настройку.
	if cfg.Name.IsUnknown() || cfg.Name.IsNull() {
		return
	}
	if cfg.Name.ValueString() == "" {
		resp.Diagnostics.AddError("Имя набора пусто",
			"Край пустое имя принимает, а провайдер — нет: имя единственное, чем "+
				"подтверждается отсутствие набора и чем находится уже созданный после "+
				"потерянного ответа. Без него удалённый вне Terraform набор не снимется из "+
				"состояния, а повтор создания заведёт дубль.")
	}
	resp.Diagnostics.Append(cidrGroupMemberDiagnostics(ctx, "v4_cidr_blocks", cfg.V4CidrBlocks, true)...)
	resp.Diagnostics.Append(cidrGroupMemberDiagnostics(ctx, "v6_cidr_blocks", cfg.V6CidrBlocks, false)...)
}

// cidrGroupMemberDiagnostics — запись члена набора, проверенная ДО обращения к краю.
//
// Правило отдельной функцией от чтения настройки: оно чистое от набора, и его можно
// предъявить таблицей случаев с положительным контролем в каждой строке (тот же довод
// записан у маршрутов таблицы маршрутизации).
//
// Проверяется РОВНО то, что край делает со значением сам, и тем же предикатом
// (`net/netip` стандартной библиотеки на обеих сторонах):
//
//   - не разбирается как запись CIDR — край отвергнет;
//   - разряды узла ненулевые — край отвергнет;
//   - чужое семейство в этом поле — край отвергнет, называя поле и индекс;
//   - НЕКАНОНИЧЕСКАЯ запись того же префикса (`2001:0db8::/32`) и повтор одного префикса
//     в двух написаниях — край примет, но вернёт СВОЮ запись и без дубля. Значение в
//     состоянии разошлось бы с настройкой, и Terraform остановился бы на собственной
//     несогласованности после применения — отказом, из которого не видно ни поля, ни
//     причины. Поэтому оба случая называются здесь, на плане, с готовой заменой в тексте.
//
// Судить можно ТОЛЬКО о том, что уже известно: член вправе прийти из ещё не вычисленной
// ссылки, и счесть неизвестное негодным значило бы отвергнуть законную конфигурацию.
func cidrGroupMemberDiagnostics(ctx context.Context, field string, set types.Set, wantV4 bool) diagList {
	var diags diagList
	if set.IsNull() || set.IsUnknown() {
		return diags
	}
	family := "IPv6"
	if wantV4 {
		family = "IPv4"
	}
	seen := map[netip.Prefix]string{}
	for _, raw := range stringsFromSet(ctx, set) {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			diags.AddError("Негодный член набора",
				fmt.Sprintf("%s: %q не разбирается как запись CIDR. Ожидается вид "+
					"`203.0.113.0/24` или `2001:db8::/32`.", field, raw))
			continue
		}
		if p.Masked() != p {
			diags.AddError("В члене набора заданы разряды узла",
				fmt.Sprintf("%s: %q описывает адрес внутри сети, а член набора — саму сеть. "+
					"Укажите %q.", field, raw, p.Masked().String()))
			continue
		}
		if p.Addr().Is4() != wantV4 || p.Addr().Is4In6() {
			diags.AddError("Член набора не того семейства",
				fmt.Sprintf("%s: %q — не %s. Семьи задаются РАЗНЫМИ полями: край отвергает "+
					"чужого члена на входе, поэтому смешанный набор не выразим вовсе.",
					field, raw, family))
			continue
		}
		if canonical := p.String(); canonical != raw {
			diags.AddError("Неканоническая запись члена набора",
				fmt.Sprintf("%s: %q край сохранит как %q и вернёт именно так. Настройка и "+
					"состояние разошлись бы после применения. Укажите %q.",
					field, raw, canonical, canonical))
			continue
		}
		if prev, dup := seen[p]; dup {
			diags.AddError("Один член набора задан дважды",
				fmt.Sprintf("%s: %q и %q — один и тот же префикс. Набор хранит членов по "+
					"значению, поэтому край сохранит его ОДИН раз, и состав в состоянии "+
					"окажется короче написанного.", field, prev, raw))
			continue
		}
		seen[p] = raw
	}
	return diags
}

func (r *cidrGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cidrGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.CreateCidrGroupRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),

		// Состав задаётся ПРИ СОЗДАНИИ: набор заводят ради его членов, и «создам пустой,
		// наполню потом» стоило бы лишнего применения, а на пустой набор ещё и нельзя
		// сослаться правилом.
		V4CidrBlocks: stringsFromSet(ctx, plan.V4CidrBlocks),
		V6CidrBlocks: stringsFromSet(ctx, plan.V6CidrBlocks),
	}

	// Ключ повторной подачи считается по ВСЕМУ телу запроса (см. awaitCreate): повтор того
	// же запроса не создаёт дубля, а исправленный запрос — другой запрос и уходит заново.
	id, err := awaitCreate(ctx, r.c, cidrGroupsPath, "cidrGroupId", typeNameVPCCIDRGroup,
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание набора префиксов не завершилось", err.Error())
		return
	}

	// Идентификатор в состояние — ДО первого обратного чтения. Это единственная точка, где
	// потеря необратима: ресурс создан, apply прерван, состояние пусто — следующий apply
	// создал бы дубль.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт по сообщению на каждое
	// поле вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, rerr := readByID(ctx, r.c, cidrGroupsPath, id, true)
	if rerr != nil {
		resp.Diagnostics.AddError("Набор префиксов создан, но не прочитан обратно", rerr.Error())
		return
	}
	if err := applyCidrGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cidrGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cidrGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, cidrGroupsPath, state.ID.ValueString(), false)
	if err == nil {
		if aerr := applyCidrGroup(ctx, &state, raw); aerr != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", aerr.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение набора префиксов не удалось", err.Error())
		return
	}
	// Одиночное «не найдено» ничего не устанавливает: тот же ответ приходит при отказе в
	// доступе, и он побайтово равен настоящему отсутствию.
	remove, title, detail := absenceDiagnostics(ctx, r.c, cidrGroupsPath, client.ScopeProject,
		"Набор префиксов", state.ID.ValueString(), state.ProjectID.ValueString(),
		state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *cidrGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state cidrGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	// Состав приводится ОТДЕЛЬНЫМИ действиями края, а не маской: контракт изменения его не
	// несёт вовсе, и попади он в маску — край отверг бы правку, назвав поле.
	//
	// Разница считается по КАЖДОЙ семье отдельно, а применяется ОБЕИМИ сразу: семьи
	// независимы (неизвестный план одной не мешает привести другую), но края обеих касается
	// один и тот же вызов — см. reconcileBlocks.
	var delta cidrDelta
	delta.addV4, delta.removeV4 = cidrGroupFamilyDiff(ctx, plan.V4CidrBlocks, state.V4CidrBlocks)
	delta.addV6, delta.removeV6 = cidrGroupFamilyDiff(ctx, plan.V6CidrBlocks, state.V6CidrBlocks)
	if err := r.reconcileBlocks(ctx, id, delta); err != nil {
		resp.Diagnostics.AddError("Приведение состава набора не удалось", err.Error())
		// Обещание, записанное у reconcileBlocks («в состоянии окажется ФАКТИЧЕСКИ
		// применённое»), держится ЗДЕСЬ: пара действий общей транзакции не имеет, поэтому
		// добавление могло примениться, а снятие отказать. Ранний возврат без чтения
		// оставил бы в состоянии ПРЕЖНИЙ состав — то есть провайдер отчитался бы о наборе,
		// которого на краю уже нет.
		//
		// Пишется СОСТОЯНИЕ, а не план: в плане вычисляемые атрибуты неизвестны, а
		// неизвестное после apply Terraform не принимает даже вместе с ошибкой. Провал
		// самого чтения молчалив намеренно: первичный отказ уже назван.
		if raw, rerr := readByID(ctx, r.c, cidrGroupsPath, id, false); rerr == nil {
			if aerr := applyCidrGroup(ctx, &state, raw); aerr == nil {
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			}
		}
		return
	}

	body, paths := cidrGroupUpdateBody(ctx, plan, state)
	// Пустая маска НИКОГДА не отправляется: у ресурсов домена она означает полнообъектную
	// запись. Пустая маска здесь означает «менять ЭТИМ запросом нечего» — например,
	// правился ТОЛЬКО состав, у которого свой путь.
	//
	// «Нечего менять» НЕ означает «не идём на край»: обратное чтение ниже выполняется
	// всегда, иначе в состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch, cidrGroupsPath+"/"+id, body); err != nil {
			resp.Diagnostics.AddError("Изменение набора префиксов не завершилось", err.Error())
			return
		}
	}

	raw, rerr := readByID(ctx, r.c, cidrGroupsPath, id, false)
	if rerr != nil {
		resp.Diagnostics.AddError("Набор префиксов изменён, но не прочитан обратно", rerr.Error())
		return
	}
	plan.ID = state.ID
	if err := applyCidrGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cidrGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cidrGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	httpResp, err := r.c.Do(ctx, http.MethodDelete, cidrGroupsPath+"/"+state.ID.ValueString(), nil, nil)
	if err != nil {
		resp.Diagnostics.AddError("Удаление набора префиксов не отправлено", err.Error())
		return
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var op client.Operation
		if uerr := json.Unmarshal(httpResp.Body, &op); uerr == nil && op.ID != "" {
			if _, aerr := r.c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); aerr != nil {
				resp.Diagnostics.AddError("Удаление набора префиксов не завершилось", aerr.Error())
			}
		}
	case client.OutcomeNotFound:
		// Цель достигнута — но только если отсутствие подтверждено: тот же ответ приходит
		// при отказе в доступе, и безусловное «404 значит удалено» оставило бы живой ресурс
		// вне состояния.
		verdict, cerr := r.c.ConfirmAbsence(ctx, cidrGroupsPath, client.ScopeProject,
			state.ProjectID.ValueString(), state.Name.ValueString())
		if verdict != client.VerdictGone {
			detail := "Край ответил «не найдено», но подтвердить отсутствие набора " +
				state.ID.ValueString() + " не удалось (исход: " + verdict.String() + "). " +
				"Возможно, доступ отозван, а набор цел."
			if cerr != nil {
				detail += "\n\nПодробности: " + cerr.Error()
			}
			resp.Diagnostics.AddError("Удаление набора префиксов не подтверждено", detail)
		}
	default:
		// Причина отказа называется ЗАРАНЕЕ: без неё «is in use» выясняется перебором.
		// Ссылку снимает тот, кто её поставил, — правило группы безопасности; порядок сноса
		// строится ссылкой в конфигурации, а не догадкой провайдера.
		resp.Diagnostics.AddError("Край отверг удаление набора префиксов",
			out.Message+"\n\nНабор, на который ссылается живое правило, край не удаляет — "+
				"иначе правило осталось бы с ссылкой в никуда. Кто ссылается, видно в "+
				"`used_by`; отказ края называет их числом.\n\nЕсли правило описано этой же "+
				"конфигурацией, сошлитесь на набор через его атрибут `id` — тогда порядок "+
				"уничтожения построит граф, и правило снимется раньше набора.")
	}
}

// ImportState принимает идентификатор ресурса.
//
// Проверка — ДЕФИСНОЙ формы: набор адресуется `cdg-<тело>`, и общая проверка слитной формы
// отвергла бы каждый настоящий идентификатор. Формат сверяется ДО обращения к краю:
// заведомо негодная строка получает терминальный отказ с внятным текстом, а не уезжает в
// сеть за ответом «не найдено», который для неё ничего не значит.
func (r *cidrGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importHyphenByID(ctx, "набор префиксов", ids.PrefixCidrGroupHyphen, req, resp)
}

// cidrGroupUpdateBody — тело изменения и его маска: ТОЛЬКО косметические поля.
//
// Состава здесь нет by construction — его нет в контракте изменения. Полная замена набора
// одним запросом дала бы «победил последний» двум редакторам, каждый из которых прислал
// свой полный список, а потолок пришлось бы судить по разнице двух множеств вместо дельты.
func cidrGroupUpdateBody(ctx context.Context, plan, state cidrGroupModel) (*vpcv1.UpdateCidrGroupRequest, []string) {
	body := &vpcv1.UpdateCidrGroupRequest{CidrGroupId: state.ID.ValueString()}
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
	return body, paths
}

// reconcileBlocks приводит состав набора к желаемому — ОБЕ семьи наравне.
//
// Почему обе семьи ОДНИМ вызовом на действие, а не двумя вызовами подряд. Край принимает
// обе семьи одним сообщением контракта и обрабатывает их в одной writer-транзакции под
// блокировкой строки набора. Два вызова дали бы две операции и два коммита, между которыми
// apply может оборваться, — и набор остался бы с приведённым IPv4 и неприведённым IPv6, в
// состоянии, которого нет ни в настройке, ни в прежнем состоянии.
//
// Порядок пары фиксирован — СНАЧАЛА добавление, потом снятие. Обратный проводит набор через
// состояние, где семья пуста, а опустошение семьи, на которую ссылается живое правило, край
// отвергает: правило либо потеряло бы фильтр, либо перестало пропускать что-либо — молча
// шире или молча уже написанного.
//
// Цена этого порядка названа честно: в промежуточном состоянии набор держит и новые члены,
// и старые, поэтому полная замена состава у набора, уже близкого к потолку (64 на семейство),
// упрётся в потолок и получит отказ края. Тогда замена делается в два применения — сначала
// снятие лишнего, затем добавление. Обратный порядок вылечил бы этот случай и сломал бы
// более частый: набор заводят ради ссылок на него, и опустошение отвергается у КАЖДОГО
// набора, на который сослались.
//
// Пустое действие не отправляется: край отвергает вызов, в котором обе семьи пусты, и такой
// запрос завалил бы apply, которому нечего было делать.
//
// Компенсации нет намеренно: откат добавленных членов был бы второй мутацией ради успеха
// собственного плана. При отказе снятия apply падает, называя обе половины, а в состоянии
// окажется ФАКТИЧЕСКИ применённое — его принесёт обратное чтение у вызывающего.
func (r *cidrGroupResource) reconcileBlocks(ctx context.Context, id string, d cidrDelta) error {
	if d.hasAdd() {
		// Тело — ТИП КОНТРАКТА, а не собранная руками карта: имя поля проверяет
		// компилятор. Карта с опечаткой уехала бы успешно и не сделала бы того, ради чего
		// послана, — край молча отбрасывает ключи, которых не ждёт.
		body := &vpcv1.AddCidrGroupCidrBlocksRequest{
			CidrGroupId:  id,
			V4CidrBlocks: d.addV4,
			V6CidrBlocks: d.addV6,
		}
		if err := awaitMutation(ctx, r.c, http.MethodPost,
			cidrGroupsPath+"/"+id+":"+verbAddCidrBlocks, body); err != nil {
			return fmt.Errorf("добавление членов (IPv4 %v, IPv6 %v): %w", d.addV4, d.addV6, err)
		}
	}

	if d.hasRemove() {
		body := &vpcv1.RemoveCidrGroupCidrBlocksRequest{
			CidrGroupId:  id,
			V4CidrBlocks: d.removeV4,
			V6CidrBlocks: d.removeV6,
		}
		if err := awaitMutation(ctx, r.c, http.MethodPost,
			cidrGroupsPath+"/"+id+":"+verbRemoveCidrBlocks, body); err != nil {
			// Про уже применённое добавление говорится, только если оно было: иначе
			// сообщение приписывало бы краю мутацию, которой не происходило.
			if d.hasAdd() {
				return fmt.Errorf("члены (IPv4 %v, IPv6 %v) добавлены, но снятие членов "+
					"(IPv4 %v, IPv6 %v) не удалось: %w",
					d.addV4, d.addV6, d.removeV4, d.removeV6, err)
			}
			return fmt.Errorf("снятие членов (IPv4 %v, IPv6 %v): %w", d.removeV4, d.removeV6, err)
		}
	}
	return nil
}

// cidrGroupWire — ответ края. Разбор терпим к неизвестным полям: край добавляет их вперёд
// провайдера, и строгий разбор ломал бы каждое чтение на первом же новом поле.
type cidrGroupWire struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	CreatedAt   string            `json:"createdAt"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`

	V4CidrBlocks []string `json:"v4CidrBlocks"`
	V6CidrBlocks []string `json:"v6CidrBlocks"`

	// Число членов приезжает ЧИСЛОМ: оно 32-разрядное, а строкой protojson кодирует только
	// 64-разрядные целые.
	CidrBlockCount int64 `json:"cidrBlockCount"`

	UsedBy []struct {
		Referrer *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"referrer"`
		Type  string `json:"type"`
		Owned bool   `json:"owned"`
	} `json:"usedBy"`
}

// applyCidrGroup переносит ответ края в состояние.
func applyCidrGroup(ctx context.Context, m *cidrGroupModel, raw []byte) error {
	var w cidrGroupWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор набора префиксов: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.V4CidrBlocks = setFromStrings(ctx, w.V4CidrBlocks)
	m.V6CidrBlocks = setFromStrings(ctx, w.V6CidrBlocks)
	m.CidrBlockCount = types.Int64Value(w.CidrBlockCount)

	usedBy := make([]cidrGroupUsedByModel, 0, len(w.UsedBy))
	for _, u := range w.UsedBy {
		ref := types.ObjectNull(cidrGroupReferrerAttrTypes())
		if u.Referrer != nil {
			v, diags := types.ObjectValue(cidrGroupReferrerAttrTypes(), map[string]attr.Value{
				"type": strOrNull(u.Referrer.Type),
				"id":   strOrNull(u.Referrer.ID),
				"name": strOrNull(u.Referrer.Name),
			})
			if diags.HasError() {
				return fmt.Errorf("ссылающийся на набор не укладывается в объект: %v", diags.Errors())
			}
			ref = v
		}
		usedBy = append(usedBy, cidrGroupUsedByModel{
			Referrer: ref,
			Type:     strOrNull(u.Type),
			Owned:    types.BoolValue(u.Owned),
		})
	}
	// Пустой список, а НЕ null: край всегда отвечает массивом, и null здесь давал бы
	// «известно после применения» на каждом плане неизменной инфраструктуры.
	list, diags := types.ListValueFrom(ctx, cidrGroupUsedByObjectType(), usedBy)
	if diags.HasError() {
		return fmt.Errorf("ссылающиеся на набор не укладываются в список: %v", diags.Errors())
	}
	m.UsedBy = list
	return nil
}
