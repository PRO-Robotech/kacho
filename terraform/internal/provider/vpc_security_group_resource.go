// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const securityGroupsPath = "/vpc/v1/securityGroups"

// Группа безопасности — фильтр трафика сети, и правила живут ПОЛЕМ группы, а не отдельным
// ресурсом.
//
// Почему полем: у правила нет адресуемой извне личности. Идентификатор правила край
// присваивает сам и присваивает ЗАНОВО при каждой полной замене набора (`assignRuleIDs`
// выдаёт новый идентификатор всякому правилу с пустым), поэтому ссылаться на правило
// нечем — ни из другого ресурса, ни из собственного состояния. Край это же и признаёт
// формой контракта: набор правил меняется одним запросом с маской `rule_specs`, целиком.
type securityGroupModel struct {
	ID                types.String `tfsdk:"id"`
	ProjectID         types.String `tfsdk:"project_id"`
	NetworkID         types.String `tfsdk:"network_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Labels            types.Map    `tfsdk:"labels"`
	Rules             types.Set    `tfsdk:"rules"`
	DefaultForNetwork types.Bool   `tfsdk:"default_for_network"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

// sgRuleModel — одно правило.
//
// Идентификатора правила здесь НЕТ намеренно, и это не упущение: край переприсваивает его
// при каждой замене набора, поэтому в состоянии он расходился бы с краем после любой
// правки любого другого правила. Держать значение, которое обязано разойтись, — значит
// завести вечный дрейф плана на инфраструктуре, которую никто не трогал.
//
// `ports` и `cidr_blocks` — types.Object, а НЕ указатели на структуры: указатель не
// способен держать НЕИЗВЕСТНОЕ значение, а вызывающий вправе собрать вложенный объект из
// переменной, которая в момент плана ещё не вычислена. На указателе провайдер отвечал бы
// «Value Conversion Error… target type cannot handle unknown values» — отказом, из
// которого не видно ни поля, ни причины.
type sgRuleModel struct {
	Description     types.String `tfsdk:"description"`
	Labels          types.Map    `tfsdk:"labels"`
	Direction       types.String `tfsdk:"direction"`
	Ports           types.Object `tfsdk:"ports"`
	ProtocolName    types.String `tfsdk:"protocol_name"`
	ProtocolNumber  types.Int64  `tfsdk:"protocol_number"`
	CidrBlocks      types.Object `tfsdk:"cidr_blocks"`
	SecurityGroupID types.String `tfsdk:"security_group_id"`
	CidrGroupID     types.String `tfsdk:"cidr_group_id"`
}

type sgPortsModel struct {
	FromPort types.Int64 `tfsdk:"from_port"`
	ToPort   types.Int64 `tfsdk:"to_port"`
}

type sgCidrModel struct {
	V4CidrBlocks types.List `tfsdk:"v4_cidr_blocks"`
	V6CidrBlocks types.List `tfsdk:"v6_cidr_blocks"`
}

type securityGroupResource struct{ c *client.Client }

// NewVPCSecurityGroupResource — конструктор для реестра провайдера.
func NewVPCSecurityGroupResource() resource.Resource { return &securityGroupResource{} }

func (r *securityGroupResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameVPCSecurityGroup
}

func (r *securityGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ---- схема --------------------------------------------------------------------------

func (r *securityGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Группа безопасности — набор разрешающих правил для трафика " +
			"сетевых интерфейсов сети.\n\n" +
			":::warning Группа БЕЗ правил не пропускает ничего\n" +
			"Это её смысл, а не пробел в описании. Правила только РАЗРЕШАЮТ: запрещающего " +
			"правила в контракте нет, порядка и приоритета у правил нет, и что не разрешено " +
			"явно — не проходит. Поэтому `rules = []` (или опущенное поле) означает полностью " +
			"закрытый интерфейс в обе стороны, включая исходящий трафик: `EGRESS` тоже " +
			"выдаётся правилом.\n:::\n\n" +
			"Набор правил заменяется ЦЕЛИКОМ одним запросом — это форма контракта, а не " +
			"выбор провайдера. Правка одного правила отправляет весь набор.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор группы. По нему выполняется импорт.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт группу."},
			"network_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Сеть группы. Обязательна и НЕИЗМЕНЯЕМА: правило может " +
					"ссылаться на другую группу только внутри одной сети, поэтому привязка " +
					"задаётся на создании и не меняется. Изменение пересоздаёт группу — " +
					"операции переноса группы между сетями у края не существует."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя группы в пределах проекта.\n\n" +
					"Провайдер требует его СТРОЖЕ края (край принимает и пустое): имя — " +
					"единственный способ найти уже созданную группу, если ответ на создание " +
					"потерялся, и без него повтор создал бы дубль."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Произвольное описание группы."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки группы вида ключ-значение."},
			"default_for_network": schema.BoolAttribute{Computed: true,
				MarkdownDescription: "Является ли группа группой по умолчанию своей сети. " +
					"Такую группу сеть создаёт САМА и безусловно, и край " +
					"ОТКАЗЫВАЕТСЯ её удалять. Значение только читается: сделать группу " +
					"умолчанием или перестать ею быть край не позволяет."},
			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},

			// НАБОР, а не список.
			//
			// Правило адресуется своим значением целиком: собственного идентификатора,
			// который пережил бы правку набора, у него нет (см. комментарий у sgRuleModel).
			// Порядок правил не значит НИЧЕГО: все правила разрешающие, приоритета и
			// первого совпадения в модели нет, поэтому перестановка — не изменение.
			//
			// Список тоже round-trip'ился бы (край хранит набор массивом JSONB и отдаёт его
			// в том порядке, в каком получил) — и именно поэтому выбор пришлось делать по
			// смыслу, а не по механике: на списке чистая перестановка двух правил читалась
			// бы в плане как правка обоих, а край видит ровно тот же набор.
			"rules": schema.SetNestedAttribute{Optional: true,
				MarkdownDescription: "Правила группы — НАБОР: порядок не значим, все правила " +
					"разрешающие, приоритета нет.\n\n" +
					"Идентификатора правила здесь нет намеренно: край присваивает его заново " +
					"при каждой замене набора, и в состоянии он не удержится.\n\n" +
					"У правила ровно одна цель из трёх — `cidr_blocks`, `security_group_id`, " +
					"`cidr_group_id`; протокол задаётся ЛИБО именем, ЛИБО номером.",
				// Ни одного Computed и ни одного значения по умолчанию ВНУТРИ элемента —
				// намеренно. Элемент набора адресуется своим значением целиком, поэтому
				// подстановка умолчания в один элемент меняет его ключ и ломает поиск
				// настройки для остальных. Отсутствие выражается null, и обратное чтение
				// приводит нули края обратно в null.
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"direction": schema.StringAttribute{Required: true,
						MarkdownDescription: "Направление трафика: `INGRESS` или `EGRESS`. " +
							"Имя варианта сверяется с контрактом дословно."},
					"description": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Описание правила. Пустая строка и опущенное поле " +
							"для края одно и то же, поэтому пишется опускание."},
					"labels": schema.MapAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Метки правила. Пустая карта и опущенное поле для " +
							"края одно и то же, поэтому пишется опускание."},
					"ports": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Диапазон портов. Опущенный `ports` означает " +
							"ЛЮБОЙ порт — это и есть способ сказать «любой».\n\n" +
							"Границы задаются вместе: полудиапазона у края нет.",
						Attributes: map[string]schema.Attribute{
							// Обе границы Required: диапазон задаётся целиком либо не задаётся
							// вовсе. Два отдельных необязательных числа допускали бы состояние
							// «задан только from_port», которого край не принимает, — то есть
							// выразимую настройку, обречённую на отказ.
							"from_port": schema.Int64Attribute{Required: true,
								MarkdownDescription: "Нижняя граница, `0`–`65535`, либо `-1` — " +
									"«любой порт». `-1` принимается ТОЛЬКО вместе с такой же верхней " +
									"границей: полудиапазона «от любого до 80» край не знает."},
							"to_port": schema.Int64Attribute{Required: true,
								MarkdownDescription: "Верхняя граница, `0`–`65535`, не меньше нижней, " +
									"либо `-1` — «любой порт» (только вместе с такой же нижней)."},
						}},
					"protocol_name": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Протокол именем: `ANY` либо ключевое слово реестра " +
							"IANA (`tcp`, `udp`, `icmp`, …). Взаимоисключающ с `protocol_number`; " +
							"ни один не задан — любой протокол.\n\n" +
							"Набор допустимых имён держит край: копия словаря в провайдере " +
							"разошлась бы с ним молча и отвергала бы законные значения."},
					"protocol_number": schema.Int64Attribute{Optional: true,
						MarkdownDescription: "Протокол номером IANA, `0`–`255`, либо `-1` — любой. " +
							"Номер `0` не принимается: он неотличим от «протокол не задан»; " +
							"для протокола IANA 0 пишите `protocol_name = \"hopopt\"`."},
					"cidr_blocks": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Цель — блоки адресов. Хотя бы одно семейство " +
							"обязано быть непустым.",
						Attributes: map[string]schema.Attribute{
							"v4_cidr_blocks": schema.ListAttribute{Optional: true, ElementType: types.StringType,
								MarkdownDescription: "Блоки IPv4 с нулевыми младшими битами, " +
									"например `10.0.0.0/8`."},
							"v6_cidr_blocks": schema.ListAttribute{Optional: true, ElementType: types.StringType,
								MarkdownDescription: "Блоки IPv6 с нулевыми младшими битами."},
						}},
					"security_group_id": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Цель — другая группа безопасности. Обязана лежать " +
							"в ТОЙ ЖЕ сети: группы разных сетей друг друга не видят."},
					"cidr_group_id": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Цель — именованный набор префиксов " +
							"(`kacho_vpc_cidr_group`). Правило называет НАБОР, а не его состав: " +
							"перечень правится один раз и действует во всех правилах, которые " +
							"на него сослались.\n\nНабор обязан лежать в ТОМ ЖЕ проекте и не " +
							"быть пустым: пустой набор край отвергает — правило иначе либо " +
							"потеряло бы фильтр, либо перестало пропускать что-либо, и оба " +
							"исхода молчаливы.\n\nСсылайтесь атрибутом `id` ресурса набора, а не " +
							"строкой: тогда порядок уничтожения строит граф, и правило снимется " +
							"раньше набора, который край не даёт удалить, пока на него ссылаются."},
				}}},

			// Поля `used_by` в схеме НЕТ, хотя контракт группы его несёт: сервер его не
			// заполняет (механизм «кто меня использует» реализован у сетевого интерфейса, а
			// не здесь). Показать его значило бы предъявить прочерк на месте живого факта —
			// поле вернётся вместе со своим источником.
		},
	}
}

// ---- перечисление направления -------------------------------------------------------

// sgDirection — значение перечисления по имени из СГЕНЕРЁННОЙ таблицы.
//
// Таблица берётся у контракта, а не переписывается: своя копия разошлась бы с ним молча, и
// опечатка в имени варианта прошла бы как «направление не задано» — то есть как правило,
// которое край отвергнет уже после плана.
func sgDirection(v types.String) (vpcv1.SecurityGroupRule_Direction, bool) {
	if v.IsNull() || v.IsUnknown() {
		return vpcv1.SecurityGroupRule_DIRECTION_UNSPECIFIED, false
	}
	n, ok := vpcv1.SecurityGroupRule_Direction_value[v.ValueString()]
	if !ok || n == int32(vpcv1.SecurityGroupRule_DIRECTION_UNSPECIFIED) {
		return vpcv1.SecurityGroupRule_DIRECTION_UNSPECIFIED, false
	}
	return vpcv1.SecurityGroupRule_Direction(n), true
}

// sgDirectionNames — допустимые имена для текста отказа, выведенные из той же таблицы.
// Порядок фиксируется сортировкой: обход карты в Go случаен, и сообщение об ошибке иначе
// менялось бы от прогона к прогону.
func sgDirectionNames() string {
	names := make([]string, 0, len(vpcv1.SecurityGroupRule_Direction_value))
	for name, n := range vpcv1.SecurityGroupRule_Direction_value {
		if n == int32(vpcv1.SecurityGroupRule_DIRECTION_UNSPECIFIED) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ---- чтение вложенных объектов из значений Terraform ---------------------------------

func sgPortsObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"from_port": types.Int64Type, "to_port": types.Int64Type}}
}

func sgCidrObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"v4_cidr_blocks": types.ListType{ElemType: types.StringType},
		"v6_cidr_blocks": types.ListType{ElemType: types.StringType}}}
}

// sgRuleObjectType — тип элемента набора. Объявлен ОДИН раз: разойдись он со схемой хоть
// одним полем, набор молча не собрался бы, а правила пропали бы из состояния.
func sgRuleObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"description":       types.StringType,
		"labels":            types.MapType{ElemType: types.StringType},
		"direction":         types.StringType,
		"ports":             sgPortsObjectType(),
		"protocol_name":     types.StringType,
		"protocol_number":   types.Int64Type,
		"cidr_blocks":       sgCidrObjectType(),
		"security_group_id": types.StringType,
		"cidr_group_id":     types.StringType,
	}}
}

func sgPortsOf(ctx context.Context, o types.Object) *sgPortsModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m sgPortsModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

func sgCidrOf(ctx context.Context, o types.Object) *sgCidrModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m sgCidrModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

// sgRulesOf — правила из набора. Неизвестный и null дают nil: «не задал» и «задал пустым»
// различаются, а пустой массив вместо отсутствия стирал бы правила при каждом создании.
func sgRulesOf(ctx context.Context, s types.Set) []sgRuleModel {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := make([]sgRuleModel, 0, len(s.Elements()))
	_ = s.ElementsAs(ctx, &out, false)
	return out
}

// ---- проверка настройки --------------------------------------------------------------

// sgTargetCount — сколько целей задано у правила.
func (m *sgRuleModel) sgTargetCount() int {
	n := 0
	if !m.CidrBlocks.IsNull() {
		n++
	}
	if isSet(m.SecurityGroupID) {
		n++
	}
	if isSet(m.CidrGroupID) {
		n++
	}
	return n
}

// ValidateConfig ловит до отправки то, что край либо отвергнет, либо примет во ВРЕД
// вызывающему.
//
// Часть проверок здесь — не удобство, а единственный барьер: аннотация `exactly_one` на
// цели правила рантаймом НЕ читается (валидатора аннотаций в перехватчиках нет), поэтому
// правило без цели край принимает молча, сохраняет и отдаёт обратно БЕЗ цели — то есть
// как совсем другое правило, чем написал вызывающий.
//
// Вторая часть — вырожденные написания. У каждого из них есть второе, каноническое
// написание того же смысла, и край возвращает именно каноническое. Оставить оба
// выразимыми значило бы получить «Provider produced inconsistent result after apply» на
// совершенно законной с виду настройке.
func (r *securityGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg securityGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateSGName(resp, cfg.Name)
	for i, rule := range sgRulesOf(ctx, cfg.Rules) {
		at := fmt.Sprintf("rules[%d]", i)
		validateSGRuleDirection(resp, at, rule)
		validateSGRuleSpelling(ctx, resp, at, rule)
		validateSGRuleProtocol(resp, at, rule)
		validateSGRuleTarget(ctx, resp, at, rule)
	}
}

// validateSGName держит обещание, записанное у атрибута `name`.
//
// `Required: true` требует, чтобы атрибут был НАПИСАН, и пустую строку принимает; край
// принимает её тоже — его шаблон имени начинается с пустой альтернативы. То есть без этой
// проверки «провайдер требует имя СТРОЖЕ края» было бы объявлением без содержания.
//
// Предмет проверки не косметический. Имя — единственное, чем подтверждается ОТСУТСТВИЕ
// группы: подтверждение ищет ресурс списком с фильтром по имени, а пустое имя фильтра не
// даёт — запрос вырождается в контрольную страницу проекта. Тогда любой живой сосед
// читается как «наш ресурс на месте», и группа, удалённая вне Terraform, НИКОГДА не
// снимается из состояния: чтение не удалит её и даже не пожалуется. Второе следствие —
// адрес ключа идемпотентности вырождается в один проект, и повтор потерянного создания
// не находит своё.
//
// Неизвестное не отвергается: ссылка на ещё не вычисленное значение приходит именно
// такой, и счесть её пустой значило бы отвергнуть законную настройку.
func validateSGName(resp *resource.ValidateConfigResponse, name types.String) {
	if name.IsUnknown() || name.IsNull() || name.ValueString() != "" {
		return
	}
	resp.Diagnostics.AddError("Имя группы безопасности пусто",
		"name = \"\". Край пустое имя принимает, а провайдер — нет, и это не придирка: "+
			"по имени он подтверждает, что группа действительно удалена, и по нему же находит "+
			"уже созданную группу, если ответ на создание потерялся. С пустым именем удалённая "+
			"группа остаётся в состоянии навсегда, а повтор создания заводит дубль.\n\n"+
			"Задайте имя, уникальное в пределах проекта.")
}

func validateSGRuleDirection(resp *resource.ValidateConfigResponse, at string, rule sgRuleModel) {
	if rule.Direction.IsUnknown() {
		return
	}
	if _, ok := sgDirection(rule.Direction); !ok {
		resp.Diagnostics.AddError("Направление правила не распознано",
			fmt.Sprintf("У правила %s direction = %s. Допустимые имена: %s — они сверяются с "+
				"контрактом дословно.", at, strconv.Quote(rule.Direction.ValueString()),
				sgDirectionNames()))
	}
}

// validateSGRuleSpelling отвергает вырожденные написания: те, что означают ровно то же,
// что опускание поля, и потому возвращаются краем как опускание.
func validateSGRuleSpelling(ctx context.Context, resp *resource.ValidateConfigResponse, at string, rule sgRuleModel) {
	if !rule.Description.IsNull() && !rule.Description.IsUnknown() && rule.Description.ValueString() == "" {
		resp.Diagnostics.AddError("Пустое описание правила задано явно",
			fmt.Sprintf("У правила %s description = \"\". Пустая строка и опущенное поле для "+
				"края одно и то же, и обратно он отдаёт опускание: два написания одного "+
				"смысла разошлись бы между настройкой и состоянием. Уберите поле.", at))
	}
	if !rule.Labels.IsNull() && !rule.Labels.IsUnknown() && len(rule.Labels.Elements()) == 0 {
		resp.Diagnostics.AddError("Пустые метки правила заданы явно",
			fmt.Sprintf("У правила %s labels = {}. Пустая карта и опущенное поле для края одно "+
				"и то же. Уберите поле.", at))
	}
	p := sgPortsOf(ctx, rule.Ports)
	if p == nil || p.FromPort.IsUnknown() || p.ToPort.IsUnknown() {
		return
	}
	if p.FromPort.ValueInt64() == 0 && p.ToPort.ValueInt64() == 0 {
		resp.Diagnostics.AddError("Диапазон портов 0–0 означает не то, что кажется",
			fmt.Sprintf("У правила %s ports = { from_port = 0, to_port = 0 }. Край хранит "+
				"неуказанный диапазон теми же нулями и обратно отдаёт правило БЕЗ ports, то "+
				"есть «любой порт» — не «порт 0».\n\n"+
				"Если нужен любой порт — уберите ports (или задайте обе границы `-1`). Если "+
				"нужен именно порт 0 — выразить его контракт не позволяет.", at))
	}
}

func validateSGRuleProtocol(resp *resource.ValidateConfigResponse, at string, rule sgRuleModel) {
	nameSet := isSet(rule.ProtocolName)
	numSet := !rule.ProtocolNumber.IsNull()
	if nameSet && numSet {
		resp.Diagnostics.AddError("Протокол задан дважды",
			fmt.Sprintf("У правила %s заданы и protocol_name, и protocol_number. В контракте "+
				"это ОДНО поле с двумя написаниями (взаимоисключающая ветка), и отправить оба "+
				"нельзя: провайдеру пришлось бы выбрать за вас, какое из них вы имели в виду."+
				"\n\nНи одно из них не задано — законно и означает «любой протокол».", at))
	}
	if !rule.ProtocolName.IsNull() && !rule.ProtocolName.IsUnknown() && rule.ProtocolName.ValueString() == "" {
		resp.Diagnostics.AddError("Пустое имя протокола задано явно",
			fmt.Sprintf("У правила %s protocol_name = \"\". «Любой протокол» задаётся "+
				"ОПУСКАНИЕМ обоих полей протокола, а не пустой строкой.", at))
	}
	if numSet && !rule.ProtocolNumber.IsUnknown() && rule.ProtocolNumber.ValueInt64() == 0 {
		resp.Diagnostics.AddError("Номер протокола 0 не принимается",
			fmt.Sprintf("У правила %s protocol_number = 0. Хранилище держит «протокол не "+
				"задан» тем же нулём, поэтому правило читалось бы как «любой протокол» — шире, "+
				"чем вы просили.\n\nДля протокола IANA 0 пишите protocol_name = \"hopopt\"; "+
				"для «любого» — уберите оба поля протокола.", at))
	}
}

func validateSGRuleTarget(ctx context.Context, resp *resource.ValidateConfigResponse, at string, rule sgRuleModel) {
	if n := rule.sgTargetCount(); n != 1 {
		resp.Diagnostics.AddError("Цель правила задана не однозначно",
			fmt.Sprintf("У правила %s задано целей: %d. Ровно одна из cidr_blocks, "+
				"security_group_id, cidr_group_id обязана присутствовать.\n\n"+
				"Правило без цели край принимает МОЛЧА, сохраняет и отдаёт обратно без цели — "+
				"то есть как другое правило, чем вы написали. Поэтому отказ приходит здесь.", at, n))
		return
	}
	c := sgCidrOf(ctx, rule.CidrBlocks)
	if c == nil {
		return
	}
	v4Empty := listIsEmptyKnown(c.V4CidrBlocks)
	v6Empty := listIsEmptyKnown(c.V6CidrBlocks)
	if v4Empty || v6Empty {
		resp.Diagnostics.AddError("Пустой список блоков задан явно",
			fmt.Sprintf("У правила %s одно из семейств cidr_blocks задано пустым списком. "+
				"Пустой список и опущенное поле для края одно и то же, и обратно он отдаёт "+
				"опускание. Уберите семейство, которого нет.", at))
		return
	}
	if c.V4CidrBlocks.IsNull() && c.V6CidrBlocks.IsNull() {
		resp.Diagnostics.AddError("Цель cidr_blocks пуста",
			fmt.Sprintf("У правила %s задан cidr_blocks без единого блока. Край принимает "+
				"такое правило молча и отдаёт его обратно БЕЗ цели — правило перестаёт быть "+
				"тем, что вы написали. Задайте хотя бы одно семейство.", at))
	}
}

// listIsEmptyKnown — список задан и заведомо пуст. Неизвестный не считается пустым: на
// этапе проверки настройки ссылка на ещё не вычисленное значение приходит именно
// неизвестной, и счесть её пустой значило бы отвергнуть законную конфигурацию.
func listIsEmptyKnown(l types.List) bool {
	return !l.IsNull() && !l.IsUnknown() && len(l.Elements()) == 0
}

// ---- перевод модели в запрос края ----------------------------------------------------

func (m *sgRuleModel) toProto(ctx context.Context, at string) (*vpcv1.SecurityGroupRuleSpec, error) {
	dir, ok := sgDirection(m.Direction)
	if !ok {
		return nil, fmt.Errorf("%s.direction: %s не является направлением; допустимо: %s",
			at, strconv.Quote(m.Direction.ValueString()), sgDirectionNames())
	}
	spec := &vpcv1.SecurityGroupRuleSpec{
		Description: m.Description.ValueString(),
		Labels:      mapFromTF(ctx, m.Labels),
		Direction:   dir,
	}
	// Диапазон, который не прочитался, — ОТКАЗ, а не опускание.
	//
	// Опущенный `ports` означает у края «любой порт». Уронив нечитаемое значение молча,
	// провайдер отправил бы правило ШИРЕ написанного, и узнать об этом можно было бы
	// только по трафику: ни план, ни состояние расхождения не покажут — край вернёт
	// ровно то правило, которое получил. Ниже, у цели, та же развилка уже решена
	// отказом; здесь она была решена иначе, и разошлись эти две ветки в опасную сторону.
	if !m.Ports.IsNull() {
		p := sgPortsOf(ctx, m.Ports)
		if p == nil {
			return nil, fmt.Errorf("%s.ports: значение не читается", at)
		}
		spec.Ports = &vpcv1.PortRange{FromPort: p.FromPort.ValueInt64(), ToPort: p.ToPort.ValueInt64()}
	}
	// Ветка протокола выбирается ПО ЗАДАННОСТИ поля, а не по его значению: у края это
	// взаимоисключающая пара, и «выбрана ветка со значением 0» он отличает от «ветка не
	// выбрана». Взаимоисключение уже проверено в ValidateConfig — здесь остаётся выбор.
	switch {
	case isSet(m.ProtocolName):
		spec.Protocol = &vpcv1.SecurityGroupRuleSpec_ProtocolName{ProtocolName: m.ProtocolName.ValueString()}
	case !m.ProtocolNumber.IsNull():
		spec.Protocol = &vpcv1.SecurityGroupRuleSpec_ProtocolNumber{ProtocolNumber: m.ProtocolNumber.ValueInt64()}
	}
	switch {
	case !m.CidrBlocks.IsNull():
		c := sgCidrOf(ctx, m.CidrBlocks)
		if c == nil {
			return nil, fmt.Errorf("%s.cidr_blocks: значение не читается", at)
		}
		spec.Target = &vpcv1.SecurityGroupRuleSpec_CidrBlocks{CidrBlocks: &vpcv1.CidrBlocks{
			V4CidrBlocks: stringsFromTF(ctx, c.V4CidrBlocks),
			V6CidrBlocks: stringsFromTF(ctx, c.V6CidrBlocks),
		}}
	case isSet(m.SecurityGroupID):
		spec.Target = &vpcv1.SecurityGroupRuleSpec_SecurityGroupId{SecurityGroupId: m.SecurityGroupID.ValueString()}
	case isSet(m.CidrGroupID):
		spec.Target = &vpcv1.SecurityGroupRuleSpec_CidrGroupId{CidrGroupId: m.CidrGroupID.ValueString()}
	default:
		return nil, fmt.Errorf("%s: не задана ни одна цель (cidr_blocks, security_group_id, cidr_group_id)", at)
	}
	return spec, nil
}

// sgRuleSpecs — набор правил в форму запроса.
//
// Отсутствие набора и пустой набор дают ОДНО И ТО ЖЕ тело: protojson опускает пустое
// повторяемое поле, поэтому `nil` и срез нулевой длины уезжают на провод побайтово
// одинаково (проверено сериализацией обоих). Значит «снять все правила» выражает МАСКА
// (`rule_specs` в update_mask), а не форма тела, — и полагаться здесь на разницу между
// nil и пустым срезом нельзя: её не существует.
func sgRuleSpecs(ctx context.Context, set types.Set) ([]*vpcv1.SecurityGroupRuleSpec, error) {
	rules := sgRulesOf(ctx, set)
	if rules == nil {
		return nil, nil
	}
	out := make([]*vpcv1.SecurityGroupRuleSpec, 0, len(rules))
	for i := range rules {
		spec, err := rules[i].toProto(ctx, fmt.Sprintf("rules[%d]", i))
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	return out, nil
}

// ---- разбор ответа края ---------------------------------------------------------------

// sgWire — форма ответа края. Разбирается своим типом, а не сгенерённым: сгенерённый
// потребовал бы разрешения google.protobuf.Any через глобальный реестр типов, которого у
// провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым — именно там опечатка в имени
// поля прошла бы молча.
//
// Идентификатор правила здесь НЕ читается сознательно: край его выдаёт, но переприсваивает
// на каждой замене набора, поэтому в состоянии он не удержится.
type sgWire struct {
	ID                string            `json:"id"`
	ProjectID         string            `json:"projectId"`
	NetworkID         string            `json:"networkId"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	Labels            map[string]string `json:"labels"`
	CreatedAt         string            `json:"createdAt"`
	DefaultForNetwork bool              `json:"defaultForNetwork"`
	Rules             []sgRuleWire      `json:"rules"`
}

// sgRuleWire — правило в ответе.
//
// `fromPort`/`toPort`/`protocolNumber` объявлены `any`: 64-разрядные целые protojson
// кодирует СТРОКОЙ, и одна структура несёт оба вида. Разбор через numOf.
//
// `ports` и `cidrBlocks` — указатели: край опускает невыбранную ветку и присылает `null`
// у незаданного вложенного сообщения, и это единственный способ отличить «не задано» от
// «задано нулями».
type sgRuleWire struct {
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Direction   string            `json:"direction"`
	Ports       *struct {
		FromPort any `json:"fromPort"`
		ToPort   any `json:"toPort"`
	} `json:"ports"`
	ProtocolName string `json:"protocolName"`
	// Номер протокола приходит и как «0» при незаданном протоколе — он приводится к null,
	// иначе состояние утверждало бы то, чего вызывающий не писал.
	ProtocolNumber any `json:"protocolNumber"`
	CidrBlocks     *struct {
		V4CidrBlocks []string `json:"v4CidrBlocks"`
		V6CidrBlocks []string `json:"v6CidrBlocks"`
	} `json:"cidrBlocks"`
	SecurityGroupID string `json:"securityGroupId"`
	CidrGroupID     string `json:"cidrGroupId"`
}

// listOrNull / mapOrNull — пустое от края означает «поле не задано».
//
// Внутри элемента набора нет Computed, поэтому подставить пустое значение туда, где
// вызывающий не писал ничего, нельзя: элемент адресуется своим значением целиком, и
// подстановка сменила бы его ключ. Пустое приводится к null, а вырожденные написания
// («задал пустым») отвергаются в ValidateConfig — так у каждого смысла остаётся ровно одно
// написание.
func listOrNull(ctx context.Context, in []string) types.List {
	if len(in) == 0 {
		return types.ListNull(types.StringType)
	}
	return listFromStrings(ctx, in)
}

func mapOrNull(ctx context.Context, in map[string]string) types.Map {
	if len(in) == 0 {
		return types.MapNull(types.StringType)
	}
	return mapToTF(ctx, in)
}

func (w *sgRuleWire) toModel(ctx context.Context) (sgRuleModel, error) {
	m := sgRuleModel{
		Description:     strOrNull(w.Description),
		Labels:          mapOrNull(ctx, w.Labels),
		Direction:       types.StringValue(w.Direction),
		Ports:           types.ObjectNull(sgPortsObjectType().AttrTypes),
		ProtocolName:    strOrNull(w.ProtocolName),
		ProtocolNumber:  intOrNull(numOf(w.ProtocolNumber)),
		CidrBlocks:      types.ObjectNull(sgCidrObjectType().AttrTypes),
		SecurityGroupID: strOrNull(w.SecurityGroupID),
		CidrGroupID:     strOrNull(w.CidrGroupID),
	}
	if w.Ports != nil {
		obj, diags := types.ObjectValueFrom(ctx, sgPortsObjectType().AttrTypes, sgPortsModel{
			FromPort: types.Int64Value(numOf(w.Ports.FromPort)),
			ToPort:   types.Int64Value(numOf(w.Ports.ToPort)),
		})
		if diags.HasError() {
			return m, fmt.Errorf("диапазон портов края не укладывается в объект: %v", diags.Errors())
		}
		m.Ports = obj
	}
	if w.CidrBlocks != nil {
		obj, diags := types.ObjectValueFrom(ctx, sgCidrObjectType().AttrTypes, sgCidrModel{
			V4CidrBlocks: listOrNull(ctx, w.CidrBlocks.V4CidrBlocks),
			V6CidrBlocks: listOrNull(ctx, w.CidrBlocks.V6CidrBlocks),
		})
		if diags.HasError() {
			return m, fmt.Errorf("блоки адресов края не укладываются в объект: %v", diags.Errors())
		}
		m.CidrBlocks = obj
	}
	return m, nil
}

func applySecurityGroup(ctx context.Context, m *securityGroupModel, raw []byte) error {
	var w sgWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.NetworkID = types.StringValue(w.NetworkID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.DefaultForNetwork = types.BoolValue(w.DefaultForNetwork)

	if w.Rules == nil {
		m.Rules = types.SetNull(sgRuleObjectType())
		return nil
	}
	rules := make([]sgRuleModel, 0, len(w.Rules))
	for i := range w.Rules {
		rm, err := w.Rules[i].toModel(ctx)
		if err != nil {
			return err
		}
		rules = append(rules, rm)
	}
	set, diags := types.SetValueFrom(ctx, sgRuleObjectType(), rules)
	if diags.HasError() {
		return fmt.Errorf("правила края не укладываются в набор: %v", diags.Errors())
	}
	m.Rules = set
	return nil
}

// sgRuleKey — канонический ключ правила: ВСЁ его значение.
//
// Собственной личности у правила нет, поэтому сравнивать его можно только целиком. Ключ
// нужен ровно для одного — понять, вернул ли край то же самое по содержанию, чтобы
// сохранить написание вызывающего.
func sgRuleKey(ctx context.Context, m sgRuleModel) string {
	var b strings.Builder
	b.WriteString("dir=" + m.Direction.ValueString())
	b.WriteString("|d=" + m.Description.ValueString())
	b.WriteString("|l=" + sgLabelsKey(ctx, m.Labels))
	if p := sgPortsOf(ctx, m.Ports); p != nil {
		b.WriteString("|p=" + strconv.FormatInt(p.FromPort.ValueInt64(), 10) +
			"-" + strconv.FormatInt(p.ToPort.ValueInt64(), 10))
	}
	b.WriteString("|pn=" + m.ProtocolName.ValueString())
	b.WriteString("|px=" + strconv.FormatInt(m.ProtocolNumber.ValueInt64(), 10))
	if c := sgCidrOf(ctx, m.CidrBlocks); c != nil {
		// Порядок блоков НЕ нормализуется: край хранит их массивом и отдаёт в том порядке,
		// в каком получил, поэтому две перестановки одного набора — разные значения, и
		// склеивать их значило бы объявить состояние совпавшим там, где оно разошлось.
		b.WriteString("|v4=" + strings.Join(stringsFromTF(ctx, c.V4CidrBlocks), ","))
		b.WriteString("|v6=" + strings.Join(stringsFromTF(ctx, c.V6CidrBlocks), ","))
	}
	b.WriteString("|sg=" + m.SecurityGroupID.ValueString())
	b.WriteString("|cdg=" + m.CidrGroupID.ValueString())
	return b.String()
}

// sgLabelsKey — метки в устойчивую строку. Ключи сортируются: обход карты в Go случаен, и
// без сортировки ключ правила менялся бы от вызова к вызову.
func sgLabelsKey(ctx context.Context, m types.Map) string {
	kv := mapFromTF(ctx, m)
	if len(kv) == 0 {
		return ""
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+kv[k])
	}
	return strings.Join(parts, ";")
}

// keepRulesSpelling сохраняет написание набора из настройки, когда край вернул то же самое
// по СОДЕРЖАНИЮ.
//
// Пустой набор и отсутствие набора для края одно и то же — он всегда отвечает массивом, —
// но для Terraform это РАЗНЫЕ значения, и apply падает «был null, стал пустой набор» на
// совершенно законной настройке. Сохраняется именно написание вызывающего: он его выбрал,
// и менять его молча значит спорить с ним ни о чём.
func keepRulesSpelling(ctx context.Context, want types.Set, m *securityGroupModel) {
	a := sgRulesOf(ctx, want)
	b := sgRulesOf(ctx, m.Rules)
	if len(a) != len(b) {
		return
	}
	keys := map[string]bool{}
	for _, r := range a {
		keys[sgRuleKey(ctx, r)] = true
	}
	for _, r := range b {
		if !keys[sgRuleKey(ctx, r)] {
			return
		}
	}
	m.Rules = want
}

// ---- CRUD -----------------------------------------------------------------------------

func (r *securityGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan securityGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	specs, err := sgRuleSpecs(ctx, plan.Rules)
	if err != nil {
		resp.Diagnostics.AddError("Негодное правило группы безопасности", err.Error())
		return
	}
	body := &vpcv1.CreateSecurityGroupRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		NetworkId:   plan.NetworkID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
		RuleSpecs:   specs,
	}

	id, err := awaitCreate(ctx, r.c, securityGroupsPath, "securityGroupId",
		typeNameVPCSecurityGroup, plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание группы безопасности не завершилось", err.Error())
		return
	}

	// Идентификатор записывается ДО обратного чтения: если чтение сорвётся, группа уже
	// создана, и без этой записи Terraform о ней не узнает никогда.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт по сообщению на каждое
	// поле вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, securityGroupsPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Группа безопасности создана, но не прочитана обратно", err.Error())
		return
	}
	wantRules := plan.Rules
	if err := applySecurityGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepRulesSpelling(ctx, wantRules, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *securityGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state securityGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := readByID(ctx, r.c, securityGroupsPath, state.ID.ValueString(), false)
	if err == nil {
		wantRules := state.Rules
		if err := applySecurityGroup(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		keepRulesSpelling(ctx, wantRules, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение группы безопасности не удалось", err.Error())
		return
	}
	remove, title, detail := absenceDiagnostics(ctx, r.c, securityGroupsPath, client.ScopeProject,
		"Группа безопасности", state.ID.ValueString(), state.ProjectID.ValueString(),
		state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

// Update отправляет ОДИН запрос с маской.
//
// Правила меняются той же маской (`rule_specs`), а не отдельными действиями края.
// `UpdateRules`/`UpdateRule` существуют и работают, но ни одно из них полной замены не
// даёт: первое ИНКРЕМЕНТАЛЬНО — оно принимает `deletion_rule_ids` и `addition_rule_specs`,
// то есть требует идентификаторы правил, которых провайдер намеренно не держит (край
// переприсваивает их на каждой замене набора, см. комментарий у sgRuleModel); второе
// меняет у правила только описание и метки — ни портов, ни протокола, ни цели.
// Декларативному набору нужна полная замена, и её даёт именно маска.
func (r *securityGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state securityGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.UpdateSecurityGroupRequest{SecurityGroupId: state.ID.ValueString()}
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
	if !plan.Rules.Equal(state.Rules) {
		specs, err := sgRuleSpecs(ctx, plan.Rules)
		if err != nil {
			resp.Diagnostics.AddError("Негодное правило группы безопасности", err.Error())
			return
		}
		// Снятие всех правил несёт МАСКА, а не тело: край по `rule_specs` заменяет набор
		// целиком тем, что пришло, а пришедшее «ничего» — пустой набор и снятый блок
		// `rules` — на проводе неразличимо (см. sgRuleSpecs). Поэтому путь добавляется в
		// маску и когда правил не осталось: без него снятие последнего правила молча не
		// применилось бы.
		body.RuleSpecs = specs
		paths = append(paths, "rule_specs")
	}

	// Пустая маска НИКОГДА не отправляется: край трактует её как полнообъектную запись, при
	// которой поля берутся нулями из запроса, — самый дешёвый способ молча стереть чужую
	// настройку. Но выйти здесь нельзя: в плане вычисляемые атрибуты ещё НЕИЗВЕСТНЫ, а
	// состояние неизвестного не хранит, поэтому обратное чтение делается ВСЕГДА.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			securityGroupsPath+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение группы безопасности не завершилось", err.Error())
			return
		}
	}

	raw, err := readByID(ctx, r.c, securityGroupsPath, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Группа безопасности изменена, но не прочитана обратно", err.Error())
		return
	}
	plan.ID = state.ID
	wantRules := plan.Rules
	if err := applySecurityGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepRulesSpelling(ctx, wantRules, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *securityGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state securityGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		securityGroupsPath+"/"+state.ID.ValueString(), nil); err != nil {
		detail := err.Error()
		if state.DefaultForNetwork.ValueBool() {
			detail += "\n\nЭта группа — группа по умолчанию своей сети (default_for_network = " +
				"true), и край отказывается её удалять: она снимается вместе с сетью. Уберите " +
				"ресурс из состояния (terraform state rm <адрес>) либо удаляйте сеть."
		}
		resp.Diagnostics.AddError("Удаление группы безопасности не завершилось", detail)
	}
}

func (r *securityGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "группа безопасности", ids.PrefixSecurityGroup, req, resp)
}
