// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const addressesPath = "/vpc/v1/addresses"

// Адрес — ресурс с ВЫБОРОМ ВИДА, а не с набором ручек.
//
// Вид задаётся ровно одной спецификацией из четырёх (внешний/внутренний × v4/v6), и после
// создания он не меняется: у края в маске изменения адреса пять полей — имя, описание,
// метки, защита от удаления и признак удержания, — а спецификации там нет вовсе. Значит
// смена вида, зоны, подсети или самого адреса — это ДРУГОЙ ресурс, и провайдер обязан
// сказать это пересозданием, а не сделать вид, что правка применилась (правило «принято и
// не применяется — запрещено»).
//
// Каркас плоских ресурсов сюда не подходит: он описывает поле именем, одинаковым для схемы,
// контракта и ответа, а здесь такого имени нет by construction — запрос называет ветку
// `external_ipv4_address_spec`, ответ ту же сущность `external_ipv4_address`. Провайдер
// сводит их явно, обеими сторонами.
type addressModel struct {
	ID                 types.String `tfsdk:"id"`
	ProjectID          types.String `tfsdk:"project_id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Labels             types.Map    `tfsdk:"labels"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
	Reserved           types.Bool   `tfsdk:"reserved"`

	// Четыре взаимоисключающие спецификации. types.Object, а НЕ указатель на структуру:
	// указатель неспособен держать НЕИЗВЕСТНОЕ значение, а спецификацию вызывающий вправе
	// собрать из выражения, которое в момент плана ещё не вычислено (подсеть заводится тем
	// же apply). На указателе провайдер отвечал бы «Value Conversion Error… target type
	// cannot handle unknown values» — отказом, из которого не видно ни поля, ни причины.
	ExternalIPv4 types.Object `tfsdk:"external_ipv4"`
	InternalIPv4 types.Object `tfsdk:"internal_ipv4"`
	InternalIPv6 types.Object `tfsdk:"internal_ipv6"`
	ExternalIPv6 types.Object `tfsdk:"external_ipv6"`

	CreatedAt types.String `tfsdk:"created_at"`
	Used      types.Bool   `tfsdk:"used"`
	Type      types.String `tfsdk:"type"`
	IPVersion types.String `tfsdk:"ip_version"`
	UsedBy    types.List   `tfsdk:"used_by"`
}

// addrExternalModel обслуживает и v4, и v6: у края это разные ветки с ОДИНАКОВЫМ набором
// полей, и совпадение не случайно — контракт прямо объявляет внешний v6 зеркалом v4.
// Разойдутся наборы — разойдутся и типы.
type addrExternalModel struct {
	Address types.String `tfsdk:"address"`
	ZoneID  types.String `tfsdk:"zone_id"`
}

// addrInternalModel — внутренний адрес, v4 и v6 тоже одинаковы по форме.
type addrInternalModel struct {
	Address  types.String `tfsdk:"address"`
	SubnetID types.String `tfsdk:"subnet_id"`
}

// addrUsedByModel — кто держит адрес. Только чтение.
type addrUsedByModel struct {
	Referrer types.Object `tfsdk:"referrer"`
	Type     types.String `tfsdk:"type"`
	Owned    types.Bool   `tfsdk:"owned"`
}

type addressResource struct{ c *client.Client }

// NewVPCAddressResource — конструктор для реестра провайдера.
func NewVPCAddressResource() resource.Resource { return &addressResource{} }

func (r *addressResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameVPCAddress
}

func (r *addressResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ---- типы значений -------------------------------------------------------------------
//
// Каждый тип объявлен ОДИН раз и здесь. Разойдись он со схемой хоть одним полем, объект
// молча не собрался бы, и спецификация пропала бы из состояния — то есть ресурс выглядел бы
// как адрес без вида.

func addrExternalAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"address": types.StringType,
		"zone_id": types.StringType,
	}
}

func addrInternalAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"address":   types.StringType,
		"subnet_id": types.StringType,
	}
}

func addrReferrerAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
		"name": types.StringType,
	}
}

func addrUsedByObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"referrer": types.ObjectType{AttrTypes: addrReferrerAttrTypes()},
		"type":     types.StringType,
		"owned":    types.BoolType,
	}}
}

// ---- схема ------------------------------------------------------------------------------

// addrSpecAddressAttribute — сам адрес внутри спецификации.
//
// Optional И Computed: вызывающий вправе назвать адрес сам, а вправе и промолчать — тогда
// его выдаёт край из пула. Без Computed план утверждал бы null там, где после применения
// стоит выданное значение, и Terraform остановился бы на собственной несогласованности.
func addrSpecAddressAttribute(doc string) schema.StringAttribute {
	return schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: doc}
}

func addrExternalSpecAttribute(family, doc string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional:            true,
		MarkdownDescription: doc,
		// Вид адреса не меняется: в маске изменения его нет. Пересоздание — единственный
		// честный способ это сказать.
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		Attributes: map[string]schema.Attribute{
			"address": addrSpecAddressAttribute("Конкретный адрес " + family + ". Не задан — край " +
				"выдаст адрес из пула сам."),
			"zone_id": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Зона, из которой берётся адрес. **Пустая зона законна** " +
					"и означает адрес из глобального пула, зоне не принадлежащий, — в отличие от " +
					"подсети, где якорь размещения обязателен."},
		},
	}
}

func addrInternalSpecAttribute(family, doc string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional:            true,
		MarkdownDescription: doc,
		PlanModifiers:       []planmodifier.Object{objectplanmodifier.RequiresReplace()},
		Attributes: map[string]schema.Attribute{
			"address": addrSpecAddressAttribute("Конкретный адрес " + family + " внутри подсети. " +
				"Не задан — край выдаст адрес из её блоков сам. Заданный обязан лежать в одном " +
				"из блоков подсети."),
			"subnet_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Подсеть, из которой берётся адрес. Обязательна: у " +
					"внутреннего адреса область — это подсеть, и другой ветки выбора у контракта нет. " +
					"Зону адрес не несёт — он наследует её от подсети."},
		},
	}
}

func (r *addressResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Адрес VPC — выданный проекту IP.\n\n" +
			"Вид адреса задаётся **ровно одной** спецификацией из четырёх: `external_ipv4`, " +
			"`internal_ipv4`, `internal_ipv6`, `external_ipv6`. Вид, зона, подсеть и сам адрес " +
			"после создания **не меняются** — край их в изменении не принимает, поэтому правка " +
			"любого из них пересоздаёт ресурс.\n\n" +
			"Меняются только имя, описание, метки, защита от удаления и признак удержания.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор адреса. По нему выполняется импорт.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт адрес.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя адреса в пределах проекта.\n\n" +
					"Провайдер требует его СТРОЖЕ края: край допускает адрес без имени, а " +
					"провайдеру имя — единственный способ найти уже созданный ресурс, если ответ " +
					"на создание потерялся, и подтвердить отсутствие ресурса, которого не видно " +
					"по идентификатору. Без имени повтор создал бы дубль."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Произвольное описание, до 256 знаков."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки вида ключ-значение."},

			"deletion_protection": schema.BoolAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Защита от удаления. Пока включена, край отказывает в " +
					"удалении адреса.\n\n" +
					":::warning Провайдер НЕ снимает защиту сам\n" +
					"Снятие защиты — решение владельца ресурса, а не побочный эффект `destroy`. " +
					"Поставьте `deletion_protection = false`, примените, и лишь затем удаляйте. " +
					"Иначе `destroy` отказывает, и это правильный отказ.\n:::"},

			"reserved": schema.BoolAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Держит ли проект адрес сам по себе, а не как приложение к " +
					"тому, кто его занял.\n\n" +
					"Создание этого поля НЕ принимает: заказанный тенантом адрес рождается " +
					"удерживаемым, и выбирать при создании нечего. Поэтому `reserved = false` " +
					"применяется ВТОРЫМ запросом сразу после создания — между шагами существует " +
					"краткое окно, в котором адрес удерживается.\n\n" +
					"Защитой это не является: контракт прямо оговаривает, что сегодня на признак " +
					"никто не смотрит — удаление гейтится `used` и `deletion_protection`. " +
					"Это правдивое утверждение о том, как адрес появился, а не запрет."},

			"external_ipv4": addrExternalSpecAttribute("IPv4",
				"Внешний адрес IPv4 — виден снаружи облака."),
			"internal_ipv4": addrInternalSpecAttribute("IPv4",
				"Внутренний адрес IPv4 — из блоков указанной подсети."),
			"internal_ipv6": addrInternalSpecAttribute("IPv6",
				"Внутренний адрес IPv6 — из блоков IPv6 указанной подсети."),
			"external_ipv6": addrExternalSpecAttribute("IPv6",
				"Внешний адрес IPv6 — зеркало внешнего IPv4 по форме."),

			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},
			"used": schema.BoolAttribute{Computed: true,
				MarkdownDescription: "Занят ли адрес кем-нибудь. Занятый адрес край удалить не даёт; " +
					"кто именно его держит — в `used_by`."},
			"type": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Вид адреса, как его называет край: `INTERNAL` или `EXTERNAL`."},
			"ip_version": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Версия протокола: `IPV4` или `IPV6`."},

			"used_by": schema.ListNestedAttribute{Computed: true,
				MarkdownDescription: "Кто держит адрес. Только чтение — этот список ведёт край. " +
					"Он же отвечает на вопрос «почему адрес не удаляется», когда `used = true`.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"referrer": schema.SingleNestedAttribute{Computed: true,
						MarkdownDescription: "Ресурс, который держит адрес.",
						Attributes: map[string]schema.Attribute{
							// Пример берётся из ПРОИЗВОДИТЕЛЕЙ этого поля, а не из соседнего
							// контракта: `compute_instance` (так стоит в комментарии к
							// used_by у самого адреса) держателем АДРЕСА не бывает — этим
							// именем помечается держатель сетевого интерфейса. Адрес
							// занимают только двое, и оба видны в дереве по имени, которое
							// записывают.
							"type": schema.StringAttribute{Computed: true,
								MarkdownDescription: "Вид ресурса, который держит адрес: " +
									"`network_interface` (адрес занят интерфейсом) либо " +
									"`network_load_balancer` (адрес заказан балансировщиком). " +
									"Перечень ведёт край и он может пополниться."},
							"id": schema.StringAttribute{Computed: true},
							"name": schema.StringAttribute{Computed: true,
								MarkdownDescription: "Имя на момент привязки — зеркало, а не источник истины."},
						}},
					// Перечисление названо тем, что край ПРОИЗВОДИТ, а не всей таблицей
					// контракта: у адреса каждому держателю проставляется `USED_BY`
					// безусловно, второго значения здесь не появляется. Обещать оба
					// значило бы предложить строить условие на ветке, которой нет.
					"type": schema.StringAttribute{Computed: true,
						MarkdownDescription: "Характер связи. У адреса край проставляет " +
							"`USED_BY` каждому держателю; `MANAGED_BY` контракт допускает, " +
							"но на этом ресурсе край его не производит."},
					"owned": schema.BoolAttribute{Computed: true,
						MarkdownDescription: "Распоряжается ли этот ресурс жизнью адреса, а не " +
							"просто ссылается на него."},
				}}},
		},
	}
}

// ---- проверка настройки ------------------------------------------------------------------

// ValidateConfig ловит выбор вида ДО обращения к краю.
//
// Край отвергнет и ноль спецификаций, и две, но его отказ приедет после сетевого вызова и
// будет выглядеть как отказ платформы. Ошибка настройки обязана называться на этапе проверки.
func (r *addressResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg addressModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// НЕИЗВЕСТНАЯ спецификация считается ЗАДАННОЙ.
	//
	// Ссылка на ещё не созданный ресурс приходит сюда неизвестной, и счесть её пустой
	// значило бы отвергать законную настройку: «заведи подсеть и возьми в ней адрес» — это
	// один apply, и в момент проверки подсети ещё нет. Судить о неизвестном нельзя ни в
	// какую сторону, поэтому здесь оно засчитывается за присутствие блока — а присутствие и
	// есть предмет этой проверки.
	var given []string
	for _, s := range []struct {
		name string
		o    types.Object
	}{
		{"external_ipv4", cfg.ExternalIPv4},
		{"internal_ipv4", cfg.InternalIPv4},
		{"internal_ipv6", cfg.InternalIPv6},
		{"external_ipv6", cfg.ExternalIPv6},
	} {
		if !s.o.IsNull() {
			given = append(given, s.name)
		}
	}
	if len(given) != 1 {
		detail := "Вид адреса выбирается ровно одной спецификацией из четырёх: external_ipv4, " +
			"internal_ipv4, internal_ipv6, external_ipv6. "
		if len(given) == 0 {
			detail += "Не задана ни одна — краю неоткуда узнать, какой адрес заводить."
		} else {
			detail += "Заданы: " + strings.Join(given, ", ") + ". Это разные ресурсы, а не " +
				"настройки одного: внешний и внутренний адрес живут в разных пулах, а версия " +
				"протокола выбирается вместе с ними."
		}
		resp.Diagnostics.AddError("Вид адреса задан не однозначно", detail)
	}

	if !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() == "" {
		resp.Diagnostics.AddError("Пустое имя адреса",
			"Край пустое имя у адреса допускает, а провайдер — нет, и это осознанно строже. "+
				"Имя — единственное, чем провайдер находит уже созданный адрес при потерянном "+
				"ответе и чем подтверждает его отсутствие. Без имени повтор создал бы дубль, а "+
				"пропавший ресурс нельзя было бы отличить от отозванного доступа.")
	}
}

// ---- разбор значений настройки ------------------------------------------------------------

func addrExternalSpecOf(ctx context.Context, o types.Object) (*addrExternalModel, error) {
	if o.IsNull() || o.IsUnknown() {
		return nil, nil
	}
	var m addrExternalModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil, fmt.Errorf("спецификация внешнего адреса не разобрана: %v", diags.Errors())
	}
	return &m, nil
}

func addrInternalSpecOf(ctx context.Context, o types.Object) (*addrInternalModel, error) {
	if o.IsNull() || o.IsUnknown() {
		return nil, nil
	}
	var m addrInternalModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil, fmt.Errorf("спецификация внутреннего адреса не разобрана: %v", diags.Errors())
	}
	return &m, nil
}

// Преобразование требований СНЯТО вместе с самим блоком: оба его поля сняты с
// контракта (словарь защиты от атак в общем фундаменте называл конкретного внешнего
// поставщика и ни на чём не ветвился; у возможности исходящей почты не было ни одного
// законного непустого входа), а сообщение с нулём полей — пустая разновидность.

// addressCreateBody собирает запрос создания и ВЫБИРАЕТ ветку по своему условию.
//
// `default` здесь — отказ, а не «значит четвёртый вариант». Ветка без своего условия уже
// стоила краю разыменования nil на теле, в котором не было ни одной спецификации.
func addressCreateBody(ctx context.Context, plan *addressModel) (*vpcv1.CreateAddressRequest, error) {
	body := &vpcv1.CreateAddressRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
	}
	if !plan.DeletionProtection.IsNull() && !plan.DeletionProtection.IsUnknown() {
		body.DeletionProtection = plan.DeletionProtection.ValueBool()
	}

	switch {
	case !plan.ExternalIPv4.IsNull():
		s, err := addrExternalSpecOf(ctx, plan.ExternalIPv4)
		if err != nil || s == nil {
			return nil, addrSpecDecodeErr("external_ipv4", err)
		}
		body.AddressSpec = &vpcv1.CreateAddressRequest_ExternalIpv4AddressSpec{
			ExternalIpv4AddressSpec: &vpcv1.ExternalIpv4AddressSpec{
				Address: s.Address.ValueString(),
				ZoneId:  s.ZoneID.ValueString(),
			}}
	case !plan.InternalIPv4.IsNull():
		s, err := addrInternalSpecOf(ctx, plan.InternalIPv4)
		if err != nil || s == nil {
			return nil, addrSpecDecodeErr("internal_ipv4", err)
		}
		body.AddressSpec = &vpcv1.CreateAddressRequest_InternalIpv4AddressSpec{
			InternalIpv4AddressSpec: &vpcv1.InternalIpv4AddressSpec{
				Address: s.Address.ValueString(),
				Scope: &vpcv1.InternalIpv4AddressSpec_SubnetId{
					SubnetId: s.SubnetID.ValueString()},
			}}
	case !plan.InternalIPv6.IsNull():
		s, err := addrInternalSpecOf(ctx, plan.InternalIPv6)
		if err != nil || s == nil {
			return nil, addrSpecDecodeErr("internal_ipv6", err)
		}
		body.AddressSpec = &vpcv1.CreateAddressRequest_InternalIpv6AddressSpec{
			InternalIpv6AddressSpec: &vpcv1.InternalIpv6AddressSpec{
				Address: s.Address.ValueString(),
				Scope: &vpcv1.InternalIpv6AddressSpec_SubnetId{
					SubnetId: s.SubnetID.ValueString()},
			}}
	case !plan.ExternalIPv6.IsNull():
		s, err := addrExternalSpecOf(ctx, plan.ExternalIPv6)
		if err != nil || s == nil {
			return nil, addrSpecDecodeErr("external_ipv6", err)
		}
		body.AddressSpec = &vpcv1.CreateAddressRequest_ExternalIpv6AddressSpec{
			ExternalIpv6AddressSpec: &vpcv1.ExternalIpv6AddressSpec{
				Address: s.Address.ValueString(),
				ZoneId:  s.ZoneID.ValueString(),
			}}
	default:
		return nil, fmt.Errorf("не задана ни одна спецификация адреса. Вид выбирается ровно " +
			"одной из external_ipv4, internal_ipv4, internal_ipv6, external_ipv6 — без неё край " +
			"не знает, какой адрес заводить.\n\nПроверка настройки этого не поймала потому, что " +
			"на этапе плана значение было НЕИЗВЕСТНЫМ (оно засчитывается за присутствие блока), " +
			"а при применении разрешилось в null")
	}
	return body, nil
}

func addrSpecDecodeErr(name string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: блок задан, но его значение не прочиталось", name)
}

// ---- разбор ответа края ---------------------------------------------------------------

// addressWire — форма ответа края. Разбирается своим типом, а не сгенерённым: сгенерённый
// потребовал бы разрешения google.protobuf.Any через глобальный реестр типов, которого у
// провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым — именно там опечатка в имени
// поля прошла бы молча.
//
// Ключи спецификаций в ответе НЕ те же, что в запросе (`…Address` против `…AddressSpec`) —
// оба имени выписаны здесь дословно, потому что вывести одно из другого нечем.
type addressWire struct {
	ID                 string            `json:"id"`
	ProjectID          string            `json:"projectId"`
	CreatedAt          string            `json:"createdAt"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Labels             map[string]string `json:"labels"`
	Reserved           bool              `json:"reserved"`
	Used               bool              `json:"used"`
	Type               string            `json:"type"`
	IPVersion          string            `json:"ipVersion"`
	DeletionProtection bool              `json:"deletionProtection"`

	ExternalIPv4 *addrExternalWire `json:"externalIpv4Address"`
	InternalIPv4 *addrInternalWire `json:"internalIpv4Address"`
	InternalIPv6 *addrInternalWire `json:"internalIpv6Address"`
	ExternalIPv6 *addrExternalWire `json:"externalIpv6Address"`

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

type addrExternalWire struct {
	Address string `json:"address"`
	ZoneID  string `json:"zoneId"`
}

type addrInternalWire struct {
	Address  string `json:"address"`
	SubnetID string `json:"subnetId"`
}

func (w *addrExternalWire) toObject() (types.Object, error) {
	// `address` пишется ЗНАЧЕНИЕМ даже когда край вернул пустую строку, а `zone_id` — null.
	//
	// Разница не косметическая. `address` — вычисляемое поле: null в состоянии на следующем
	// плане превращается фреймворком в НЕИЗВЕСТНОЕ, а неизвестное не равно состоянию, и
	// «изменение пересоздаёт ресурс» на всей спецификации сработало бы на ровном месте —
	// адрес пересоздавался бы каждым apply. У `zone_id` вычисляемости нет, и пустая строка
	// вместо null разошлась бы с настройкой, где зона не задана.
	obj, diags := types.ObjectValue(addrExternalAttrTypes(), map[string]attr.Value{
		"address": types.StringValue(w.Address),
		"zone_id": strOrNull(w.ZoneID),
	})
	if diags.HasError() {
		return types.ObjectNull(addrExternalAttrTypes()),
			fmt.Errorf("внешний адрес края не укладывается в объект: %v", diags.Errors())
	}
	return obj, nil
}

func (w *addrInternalWire) toObject() (types.Object, error) {
	obj, diags := types.ObjectValue(addrInternalAttrTypes(), map[string]attr.Value{
		"address":   types.StringValue(w.Address),
		"subnet_id": types.StringValue(w.SubnetID),
	})
	if diags.HasError() {
		return types.ObjectNull(addrInternalAttrTypes()),
			fmt.Errorf("внутренний адрес края не укладывается в объект: %v", diags.Errors())
	}
	return obj, nil
}

func applyAddress(ctx context.Context, m *addressModel, raw []byte) error {
	var w addressWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}

	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.Reserved = types.BoolValue(w.Reserved)
	m.Used = types.BoolValue(w.Used)
	m.Type = types.StringValue(w.Type)
	m.IPVersion = types.StringValue(w.IPVersion)
	m.DeletionProtection = types.BoolValue(w.DeletionProtection)

	m.ExternalIPv4 = types.ObjectNull(addrExternalAttrTypes())
	m.ExternalIPv6 = types.ObjectNull(addrExternalAttrTypes())
	m.InternalIPv4 = types.ObjectNull(addrInternalAttrTypes())
	m.InternalIPv6 = types.ObjectNull(addrInternalAttrTypes())

	var err error
	switch {
	case w.ExternalIPv4 != nil:
		m.ExternalIPv4, err = w.ExternalIPv4.toObject()
	case w.InternalIPv4 != nil:
		m.InternalIPv4, err = w.InternalIPv4.toObject()
	case w.InternalIPv6 != nil:
		m.InternalIPv6, err = w.InternalIPv6.toObject()
	case w.ExternalIPv6 != nil:
		m.ExternalIPv6, err = w.ExternalIPv6.toObject()
	default:
		// Ветка выбирается СВОИМ условием, `default` — отказ. Адрес без вида краем не
		// производится; молча записать все четыре спецификации в null значило бы отдать
		// Terraform состояние, в котором ресурс есть, а чем он является — неизвестно.
		return fmt.Errorf("край вернул адрес, не назвав его вида: ни одной из четырёх " +
			"спецификаций в ответе нет")
	}
	if err != nil {
		return err
	}

	usedBy := make([]addrUsedByModel, 0, len(w.UsedBy))
	for _, u := range w.UsedBy {
		ref := types.ObjectNull(addrReferrerAttrTypes())
		if u.Referrer != nil {
			v, diags := types.ObjectValue(addrReferrerAttrTypes(), map[string]attr.Value{
				"type": strOrNull(u.Referrer.Type),
				"id":   strOrNull(u.Referrer.ID),
				"name": strOrNull(u.Referrer.Name),
			})
			if diags.HasError() {
				return fmt.Errorf("держатель адреса не укладывается в объект: %v", diags.Errors())
			}
			ref = v
		}
		usedBy = append(usedBy, addrUsedByModel{
			Referrer: ref,
			Type:     strOrNull(u.Type),
			Owned:    types.BoolValue(u.Owned),
		})
	}
	// Пустой список, а НЕ null: край всегда отвечает массивом, и null здесь давал бы
	// «известно после применения» на каждом плане неизменной инфраструктуры.
	list, diags := types.ListValueFrom(ctx, addrUsedByObjectType(), usedBy)
	if diags.HasError() {
		return fmt.Errorf("держатели адреса не укладываются в список: %v", diags.Errors())
	}
	m.UsedBy = list
	return nil
}

// ---- написание адреса --------------------------------------------------------------------

// addrSameIP — одна ли это запись адреса.
//
// Сравниваются РАЗОБРАННЫЕ значения, а не строки: явно заданный внешний адрес край
// приводит к канонической форме, поэтому `2001:DB8::1` возвращается как `2001:db8::1`.
func addrSameIP(a, b string) bool {
	if a == b {
		return true
	}
	x, err := netip.ParseAddr(a)
	if err != nil {
		return false
	}
	y, err := netip.ParseAddr(b)
	if err != nil {
		return false
	}
	return x == y
}

// keepAddrIPSpelling сохраняет написание адреса, выбранное вызывающим, когда край вернул ТОТ ЖЕ
// адрес своей записью.
//
// Без этого настройка `2001:DB8::1` ломалась бы дважды: применение падало бы на
// несогласованности («в плане одно, в состоянии другое»), а обновление состояния объявляло
// бы вечное расхождение — и, поскольку спецификация пересоздаёт ресурс, каждый план сносил
// бы живой адрес и заводил новый. Меняется только НАПИСАНИЕ и только при равенстве адресов;
// разные адреса остаются разными.
func keepAddrIPSpelling(ctx context.Context, want, got types.Object) types.Object {
	if want.IsNull() || want.IsUnknown() || got.IsNull() || got.IsUnknown() {
		return got
	}
	w, ok := want.Attributes()["address"].(types.String)
	if !ok || w.IsNull() || w.IsUnknown() || w.ValueString() == "" {
		return got
	}
	g, ok := got.Attributes()["address"].(types.String)
	if !ok || g.IsNull() || g.ValueString() == w.ValueString() {
		return got
	}
	if !addrSameIP(w.ValueString(), g.ValueString()) {
		return got
	}
	attrs := make(map[string]attr.Value, len(got.Attributes()))
	for k, v := range got.Attributes() {
		attrs[k] = v
	}
	attrs["address"] = w
	out, diags := types.ObjectValue(got.AttributeTypes(ctx), attrs)
	if diags.HasError() {
		return got
	}
	return out
}

func keepAddrSpelling(ctx context.Context, want, got *addressModel) {
	got.ExternalIPv4 = keepAddrIPSpelling(ctx, want.ExternalIPv4, got.ExternalIPv4)
	got.InternalIPv4 = keepAddrIPSpelling(ctx, want.InternalIPv4, got.InternalIPv4)
	got.InternalIPv6 = keepAddrIPSpelling(ctx, want.InternalIPv6, got.InternalIPv6)
	got.ExternalIPv6 = keepAddrIPSpelling(ctx, want.ExternalIPv6, got.ExternalIPv6)
}

// ---- гашение неизвестного внутри спецификации ----------------------------------------------

// sealAddrSpecUnknowns заменяет НЕИЗВЕСТНЫЕ значения внутри спецификации на null.
//
// Общий гаситель (sealUnknowns) обходит поля модели рефлексией и знает строку, число,
// признак, список, набор и карту, но внутрь types.Object НЕ заходит: его содержимое лежит в
// неэкспортируемых полях. У адреса внутри спецификации есть вычисляемое поле — сам адрес
// выдаёт край, — поэтому ранняя запись состояния унесла бы туда неизвестное, а Terraform не
// принимает после применения НИ ОДНОГО неизвестного и отвечает на каждое отдельным «invalid
// result object», среди которых настоящая причина (сорвавшееся обратное чтение) теряется.
//
// Вид значения, который эта функция гасить не умеет, гасит объект ЦЕЛИКОМ. Иначе появление
// пятого вида поля прошло бы молча — то есть проверка потеряла бы предмет ровно там, где она
// и нужна.
func sealAddrSpecUnknowns(ctx context.Context, o types.Object) types.Object {
	at := o.AttributeTypes(ctx)
	if o.IsUnknown() {
		return types.ObjectNull(at)
	}
	if o.IsNull() {
		return o
	}
	attrs := make(map[string]attr.Value, len(o.Attributes()))
	for k, v := range o.Attributes() {
		switch tv := v.(type) {
		case types.String:
			if tv.IsUnknown() {
				attrs[k] = types.StringNull()
				continue
			}
			attrs[k] = tv
		case types.Object:
			attrs[k] = sealAddrSpecUnknowns(ctx, tv)
		default:
			if v.IsUnknown() {
				return types.ObjectNull(at)
			}
			attrs[k] = v
		}
	}
	out, diags := types.ObjectValue(at, attrs)
	if diags.HasError() {
		return types.ObjectNull(at)
	}
	return out
}

func sealAddressUnknowns(ctx context.Context, m *addressModel) {
	m.ExternalIPv4 = sealAddrSpecUnknowns(ctx, m.ExternalIPv4)
	m.InternalIPv4 = sealAddrSpecUnknowns(ctx, m.InternalIPv4)
	m.InternalIPv6 = sealAddrSpecUnknowns(ctx, m.InternalIPv6)
	m.ExternalIPv6 = sealAddrSpecUnknowns(ctx, m.ExternalIPv6)
	sealUnknowns(m)
}

// ---- CRUD ---------------------------------------------------------------------------------

func (r *addressResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan addressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := plan

	body, err := addressCreateBody(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Спецификация адреса не собрана", err.Error())
		return
	}

	id, err := awaitCreate(ctx, r.c, addressesPath, "addressId", typeNameVPCAddress,
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание адреса не завершилось", err.Error())
		return
	}

	// Идентификатор записывается ДО обратного чтения: если чтение сорвётся, адрес уже
	// создан, и без этой записи Terraform о нём не узнает никогда.
	plan.ID = types.StringValue(id)
	sealAddressUnknowns(ctx, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, addressesPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Адрес создан, но не прочитан обратно", err.Error())
		return
	}
	if err := applyAddress(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepAddrSpelling(ctx, &want, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Второй шаг создания: признак удержания.
	//
	// Контракт создания его не несёт — адрес рождается удерживаемым, — поэтому отказ от
	// удержания досылается изменением. Пропустить его нельзя: вызывающий задал значение и
	// вправе получить его применённым, а состояние иначе показало бы умолчание края, то есть
	// провайдер соврал бы об исходе.
	if want.Reserved.IsNull() || want.Reserved.IsUnknown() || want.Reserved.ValueBool() {
		return
	}
	patch := &vpcv1.UpdateAddressRequest{
		AddressId:  id,
		Reserved:   false,
		UpdateMask: fieldMask([]string{"reserved"}),
	}
	if err := awaitMutation(ctx, r.c, http.MethodPatch, addressesPath+"/"+id, patch); err != nil {
		// Сказано то, что произойдёт, а не то, чего хотелось бы. Отказ, вернувшийся ИЗ
		// СОЗДАНИЯ, помечает ресурс подлежащим замене — следующий apply не «дошлёт
		// изменение», а УДАЛИТ адрес и заведёт новый; у внешнего адреса это смена уже
		// выданного IP. Прежняя редакция обещала здесь безобидный повтор, то есть
		// ошибалась ровно в ту сторону, в которую оператору дороже всего поверить.
		resp.Diagnostics.AddError("Адрес создан, но отказ от удержания не применён",
			err.Error()+"\n\nАдрес существует и удерживается проектом (reserved = true). "+
				"Это записано в состояние, поэтому расхождение с настройкой видно в плане.\n\n"+
				"Но создание завершилось отказом, а такой ресурс Terraform помечает "+
				"подлежащим замене: следующий apply УДАЛИТ адрес и заведёт новый. Если "+
				"этого не нужно — снимите пометку (`terraform untaint <адрес ресурса>`) и "+
				"примените снова: тогда reserved = false уедет обычным изменением.")
		return
	}
	raw, err = readByID(ctx, r.c, addressesPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError("Адрес изменён, но не прочитан обратно", err.Error())
		return
	}
	if err := applyAddress(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepAddrSpelling(ctx, &want, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *addressResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state addressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := state

	raw, err := readByID(ctx, r.c, addressesPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyAddress(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		keepAddrSpelling(ctx, &want, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение адреса не удалось", err.Error())
		return
	}
	remove, title, detail := absenceDiagnostics(ctx, r.c, addressesPath, client.ScopeProject,
		"Адрес", state.ID.ValueString(), state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

// Update меняет ТОЛЬКО то, что край принимает в маске изменения: имя, описание, метки,
// защиту от удаления и признак удержания.
//
// Спецификации здесь нет и быть не может — она помечена пересоздающей, поэтому её правка до
// этого метода не доходит вовсе.
func (r *addressResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state addressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := plan
	id := state.ID.ValueString()

	body := &vpcv1.UpdateAddressRequest{AddressId: id}
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
	if !plan.DeletionProtection.Equal(state.DeletionProtection) {
		body.DeletionProtection = plan.DeletionProtection.ValueBool()
		paths = append(paths, "deletion_protection")
	}
	if !plan.Reserved.Equal(state.Reserved) {
		body.Reserved = plan.Reserved.ValueBool()
		paths = append(paths, "reserved")
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё изменяемое»,
	// а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе в состоянии осталась
	// бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch, addressesPath+"/"+id, body); err != nil {
			resp.Diagnostics.AddError("Изменение адреса не завершилось", err.Error())
			return
		}
	}

	raw, err := readByID(ctx, r.c, addressesPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError("Адрес изменён, но не прочитан обратно", err.Error())
		return
	}
	plan.ID = state.ID
	if err := applyAddress(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepAddrSpelling(ctx, &want, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *addressResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state addressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		addressesPath+"/"+state.ID.ValueString(), nil); err != nil {
		resp.Diagnostics.AddError("Удаление адреса не завершилось",
			err.Error()+"\n\nКрай отказывает в удалении адреса по двум причинам, и обе "+
				"снимаются ДО удаления:\n\n"+
				"  • включена защита от удаления. Провайдер НЕ снимает её сам: это решение "+
				"владельца ресурса, а не побочный эффект destroy. Поставьте "+
				"deletion_protection = false, примените — и лишь затем удаляйте;\n\n"+
				"  • адрес занят (used = true). Отвяжите его от того, кто его держит: край "+
				"называет этот ресурс в своём отказе, он же виден в used_by.")
	}
}

func (r *addressResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "адрес", ids.PrefixAddress, req, resp)
}
