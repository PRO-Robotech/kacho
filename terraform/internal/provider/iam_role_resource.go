// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const rolesPath = "/iam/v1/roles"

// Роль — набор прав. Сама по себе она никому ничего не даёт: право начинает действовать
// только тогда, когда роль выдана привязкой субъекту на область.
//
// Правила роли — ПОЛЕ роли, а не отдельные ресурсы: у правила нет собственного
// идентификатора, адресовать его извне нечем, и край принимает их только целым набором в
// том же запросе. Поэтому политика описывается здесь же.
type roleModel struct {
	ID             types.String `tfsdk:"id"`
	DefinitionTier types.Object `tfsdk:"definition_tier"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Labels         types.Map    `tfsdk:"labels"`
	Rules          types.List   `tfsdk:"rules"`

	AccountID      types.String `tfsdk:"account_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	IsSystem       types.Bool   `tfsdk:"is_system"`
	CreatedAt      types.String `tfsdk:"created_at"`
	AuthoredVerbs  types.List   `tfsdk:"authored_verbs"`
	EffectiveVerbs types.List   `tfsdk:"effective_verbs"`
	VerbNotes      types.Map    `tfsdk:"verb_notes"`
}

// roleTierModel — уровень, на котором роль ОПРЕДЕЛЕНА (не путать с областью выдачи: та
// живёт у привязки прав).
//
// Это ЕДИНСТВЕННЫЙ способ задать область определения в этом ресурсе, и так сделано
// намеренно. Контракт принимает ещё и пару `accountId`/`projectId` — как совместимость с
// прежней формой, — но объявляет каноническим именно уровень: при заданном уровне пара
// не читается вовсе. Выставить оба способа значило бы дать два написания одного и того
// же, которые однажды разойдутся между настройкой и состоянием. Пара остаётся в ресурсе
// только на чтение — как проекция ответа края.
type roleTierModel struct {
	TierType types.String `tfsdk:"tier_type"`
	TierID   types.String `tfsdk:"tier_id"`
}

// roleRuleModel — одно правило: однородная выдача «эти глаголы над этими типами одного
// модуля», необязательно суженная перечнем идентификаторов ЛИБО отбором по меткам.
type roleRuleModel struct {
	Module        types.String `tfsdk:"module"`
	Resources     types.List   `tfsdk:"resources"`
	Verbs         types.List   `tfsdk:"verbs"`
	ResourceNames types.List   `tfsdk:"resource_names"`
	MatchLabels   types.Map    `tfsdk:"match_labels"`
}

type roleResource struct{ c *client.Client }

// NewIAMRoleResource — конструктор для реестра провайдера.
func NewIAMRoleResource() resource.Resource { return &roleResource{} }

var (
	_ resource.Resource                   = (*roleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*roleResource)(nil)
	_ resource.ResourceWithImportState    = (*roleResource)(nil)
	_ resource.ResourceWithConfigure      = (*roleResource)(nil)
)

// MoveState — переезд состояния с прежнего имени типа.
//
// Объявление обязательно: одного блока `moved` в настройке оператора НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план. Что именно принимается и почему — iam_type_names.go.
func (r *roleResource) MoveState(ctx context.Context) []resource.StateMover {
	return movedFromRetiredTypeName(ctx, r)
}

func (r *roleResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameIAMRole
}

func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// roleTierObjectType / roleRuleObjectType — типы вложенных значений. Объявлены ОДИН раз:
// разойдись такой тип со схемой хоть одним полем, значение молча не соберётся, и
// вложенное поле исчезнет из состояния — без единого сообщения об ошибке.
func roleTierObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"tier_type": types.StringType,
		"tier_id":   types.StringType,
	}}
}

func roleRuleObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"module":         types.StringType,
		"resources":      types.ListType{ElemType: types.StringType},
		"verbs":          types.ListType{ElemType: types.StringType},
		"resource_names": types.ListType{ElemType: types.StringType},
		"match_labels":   types.MapType{ElemType: types.StringType},
	}}
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Пользовательская роль — набор прав, который выдаётся субъекту " +
			"привязкой (`kaname_access_binding`). Сама роль доступа не даёт: она описывает, " +
			"ЧТО можно делать, а привязка — КОМУ и ГДЕ.\n\n" +
			":::tip Право получает ГРУППА, а не перечень людей\n" +
			"Роль выдавайте группе и добавляйте в неё людей и служебные учётки: снять одного из " +
			"группы — одна запись членства, снять одного из перечня субъектов привязки — снятие " +
			"выдачи по всем объектам роли.\n:::\n\n" +
			":::warning Системные роли этим ресурсом не управляются\n" +
			"Системные роли платформа заводит миграциями. Край отказывает в их изменении и " +
			"удалении, а имя, совпадающее с системной ролью, отвергает как занятое.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор роли (префикс `rol`). По нему " +
					"выполняется импорт и на него ссылается привязка прав.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			// Уровень определения — types.Object, а НЕ указатель на структуру: указатель
			// неспособен держать НЕИЗВЕСТНОЕ значение, а `tier_id` почти всегда ссылается на
			// ещё не созданный проект или аккаунт. На указателе провайдер отвечал бы «target
			// type cannot handle unknown values» — отказом, из которого не видно ни поля, ни
			// причины.
			"definition_tier": schema.SingleNestedAttribute{Required: true,
				MarkdownDescription: "Уровень, на котором роль ОПРЕДЕЛЕНА, и его якорь. " +
					"Изменение пересоздаёт роль: перенести определение с одного якоря на " +
					"другой край не умеет.\n\n" +
					"Не путать с областью ВЫДАЧИ: та задаётся у привязки прав. " +
					"Роль, определённая на проекте, выдаётся только на своём проекте; " +
					"роль, определённая на аккаунте, — на своём аккаунте и внутри него.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"tier_type": schema.StringAttribute{Required: true,
						MarkdownDescription: "`iam.account` или `iam.project`.\n\n" +
							"`iam.cluster` здесь недопустим: роли этого уровня платформа " +
							"сеет миграциями, а не создаёт по запросу."},
					"tier_id": schema.StringAttribute{Required: true,
						MarkdownDescription: "Идентификатор якоря — аккаунта (`acc…`) или " +
							"проекта (`prj…`) — в зависимости от `tier_type`."},
				}},

			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя роли в пределах её уровня. Обязательно: по имени " +
					"провайдер находит уже созданную роль, если ответ на создание потерялся.\n\n" +
					"Имя косметическое и меняется свободно — привязки ссылаются на " +
					"идентификатор, а не на имя, поэтому переименование ничего не ломает."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Произвольное описание."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки САМОЙ роли. Не путать с `rules[].match_labels`: " +
					"те отбирают объекты под выдачей, а эти делают роль отбираемой по меткам " +
					"наравне с аккаунтом и проектом."},

			// СПИСОК, а не набор: порядок правил край СОХРАНЯЕТ. Политика хранится массивом
			// JSON и читается тем же массивом — ни записи, ни чтения, ни разбора, которые бы
			// её переупорядочивали, в потоке нет. Набор объявил бы перестановку правил
			// «отсутствием изменений» и, что хуже, потребовал бы уникальности элементов
			// целиком — а два правила одного модуля, различающиеся только сужением,
			// законны и не обязаны отличаться друг от друга ничем ещё.
			"rules": schema.ListNestedAttribute{Required: true,
				MarkdownDescription: "Политика роли — СПИСОК правил (`1`–`64`). Порядок " +
					"сохраняется краем и значим для чтения человеком, но не для смысла: " +
					"правила складываются.\n\n" +
					"Одно правило — однородная выдача: перечисленные глаголы над " +
					"перечисленными типами РОВНО ОДНОГО модуля. Роль, покрывающая несколько " +
					"модулей, — это несколько правил.",
				// Ни одного Computed и ни одного значения по умолчанию ВНУТРИ правила:
				// отсутствие сужения выражается опусканием поля, а не пустым списком.
				// Подстановка умолчания завела бы второе написание того же самого, и
				// настройка разошлась бы с ответом края на первом же плане.
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"module": schema.StringAttribute{Required: true,
						MarkdownDescription: "Модуль платформы, ровно один: `iam`, `vpc`, " +
							"`compute`, `loadbalancer`, `registry`, `storage`.\n\n" +
							"Перечень принадлежит краю и проверяется им: провайдер своей " +
							"копии не держит, иначе новый модуль платформы отвергался бы " +
							"провайдером как неизвестный. Обратите внимание: модуль " +
							"балансировщика называется `loadbalancer`, а не `nlb`."},
					"resources": schema.ListAttribute{Required: true, ElementType: types.StringType,
						MarkdownDescription: "Типы ресурсов модуля, `1`–`16`. Написание " +
							"неоднородно (`compute.instance`, `iam.serviceAccount`, но " +
							"`storage.volumes`, `registry.registries`) — сверяйтесь с " +
							"каталогом прав `GET /iam/v1/permissionCatalog`, неизвестная пара " +
							"«модуль + тип» отвергается краем."},
					"verbs": schema.ListAttribute{Required: true, ElementType: types.StringType,
						MarkdownDescription: "Глаголы выдачи, `1`–`16`, например `get`, " +
							"`list`, `create`, `update`, `delete`. Допустим `*` — «все " +
							"глаголы типа», но только единственным элементом."},
					"resource_names": schema.ListAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Сужение выдачи перечнем идентификаторов " +
							"конкретных объектов. Взаимоисключающе с `match_labels`. " +
							"Опустите поле, чтобы правило действовало на все объекты " +
							"перечисленных типов."},
					"match_labels": schema.MapAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Сужение выдачи отбором объектов по меткам " +
							"(совпадение по всем парам). Взаимоисключающе с " +
							"`resource_names`.\n\n" +
							"Работает не на всяком типе: отбор по меткам требует, чтобы " +
							"платформа вела зеркало объектов этого типа. Тип без зеркала " +
							"край отвергнет."},
				}}},

			// Проекция области определения на прежнюю пару полей. Только чтение: задаётся
			// она через definition_tier, а два способа сказать одно и то же разошлись бы.
			// Вычисляемое НЕ помечается пересоздающим — оно берёт значение из состояния,
			// иначе неизвестное в плане читалось бы как «другое значение» и роль
			// пересоздавалась бы от правки чего угодно.
			"account_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Аккаунт-якорь — заполнен, если `tier_type` = `iam.account`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Проект-якорь — заполнен, если `tier_type` = `iam.project`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"is_system": schema.BoolAttribute{Computed: true,
				MarkdownDescription: "Системная ли роль. У созданной этим ресурсом — всегда " +
					"`false`. Поле показывается ради импорта: втянув сюда системную роль, вы " +
					"получите ресурс, у которого край отвергает и изменение, и удаление, и " +
					"единственным признаком этого будет `true` здесь.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},

			// Метки версии, автора и времени последней правки в ресурсе НЕТ — намеренно.
			//
			// Контракт роли все три объявляет, но ответ края их не несёт: проекция чтения
			// (`roleCols` в `services/iam/internal/repo/kaname/pg/role_repo.go`) состоит из
			// идентификатора, якорей, имени, описания, политики, признака системности,
			// времени создания и меток, а сборка ответа
			// (`services/iam/internal/dto/toproto/role.go`) метку версии не заполняет
			// вовсе. Показать такое поле значит выдать пустую строку за факт о роли —
			// «автора нет», «не изменялась никогда». Вернутся они сюда вместе с
			// источником: как только проекция чтения их понесёт.
			//
			// Отдельно про версию — её тем более нельзя было оставить украшением: на ней
			// держалась бы защита от одновременной правки, и поле, всегда пустое,
			// объявляло бы включённой защиту, которая не срабатывает ни разу. Защита при
			// этом есть и работает без нашего участия: край сам читает версию строки в
			// начале операции и применяет изменение сверкой с ней, поэтому чужая правка,
			// успевшая вклиниться, роняет нашу независимо от того, прислал клиент метку
			// или нет.
			"created_at": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			"authored_verbs": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Что роль выдаёт по букве правил: объединение глаголов " +
					"всех правил (`*` раскрыт). Считает край."},
			"effective_verbs": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Что роль выдаёт ФАКТИЧЕСКИ. Отличается от " +
					"`authored_verbs` там, где платформа доматериализует глагол: " +
					"редактирующая роль удаляет то, что редактирует. Сверяйтесь с этим " +
					"полем, а не с написанным в `rules`."},
			"verb_notes": schema.MapAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Пояснения края к отдельным глаголам расширенного " +
					"набора — почему глагол там оказался."},
		},
	}
}

// ---- чтение значений Terraform ---------------------------------------------------------

// roleTierOf — уровень определения из значения Terraform. Неизвестное и null дают nil:
// решать по недовычисленному значению нечего.
func roleTierOf(ctx context.Context, o types.Object) *roleTierModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m roleTierModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

// roleRulesOf — правила из значения Terraform.
//
// Ошибка разбора возвращается, а не глотается: без неё Create отправил бы роль БЕЗ правил
// и получил бы от края отказ про пустую политику — сообщение, по которому настоящую
// причину (негодная форма правила в настройке) не восстановить.
func roleRulesOf(ctx context.Context, l types.List) ([]roleRuleModel, error) {
	if l.IsNull() || l.IsUnknown() {
		return nil, nil
	}
	out := make([]roleRuleModel, 0, len(l.Elements()))
	if diags := l.ElementsAs(ctx, &out, false); diags.HasError() {
		return nil, fmt.Errorf("правила роли не разбираются: %v", diags.Errors())
	}
	return out, nil
}

func roleRulesToProto(ctx context.Context, rs []roleRuleModel) []*iamv1.Rule {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*iamv1.Rule, 0, len(rs))
	for _, r := range rs {
		out = append(out, &iamv1.Rule{
			Module:        r.Module.ValueString(),
			Resources:     stringsFromTF(ctx, r.Resources),
			Verbs:         stringsFromTF(ctx, r.Verbs),
			ResourceNames: stringsFromTF(ctx, r.ResourceNames),
			MatchLabels:   mapFromTF(ctx, r.MatchLabels),
		})
	}
	return out
}

// ---- проверка настройки ------------------------------------------------------------

// ValidateConfig ловит СТРУКТУРНЫЕ условия края до отправки — те, нарушение которых
// отвергается всегда и не зависит от того, что край успел завести у себя нового.
//
// Чего здесь намеренно НЕТ: перечня модулей, перечня типов ресурсов и верхних границ
// количества. Всё это принадлежит краю и меняется вместе с платформой; своя копия
// начала бы отвергать законную настройку ровно в тот день, когда появится новый модуль
// или поднимут предел, — и виноватым выглядел бы край.
func (r *roleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg roleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if tier := roleTierOf(ctx, cfg.DefinitionTier); tier != nil && !tier.TierType.IsNull() && !tier.TierType.IsUnknown() {
		switch tier.TierType.ValueString() {
		case "iam.account", "iam.project":
		case "iam.cluster":
			resp.Diagnostics.AddError("Роль уровня кластера не создаётся",
				"definition_tier.tier_type = \"iam.cluster\". Роли этого уровня платформа сеет "+
					"миграциями и объявляет системными; край отвергнет запрос. Допустимы "+
					"\"iam.account\" и \"iam.project\".")
		default:
			resp.Diagnostics.AddError("Неизвестный уровень определения",
				fmt.Sprintf("definition_tier.tier_type = %q. Допустимы \"iam.account\" и "+
					"\"iam.project\".", tier.TierType.ValueString()))
		}
	}

	rules, err := roleRulesOf(ctx, cfg.Rules)
	if err != nil {
		resp.Diagnostics.AddError("Правила роли не разобраны", err.Error())
		return
	}
	if !cfg.Rules.IsUnknown() && len(rules) == 0 && !cfg.Rules.IsNull() {
		resp.Diagnostics.AddError("Политика роли пуста",
			"rules не содержит ни одного правила. Роль без правил не выдаёт ничего, и край "+
				"такую не заводит: политика — это от одного до 64 правил.")
	}

	for i, rule := range rules {
		where := fmt.Sprintf("rules[%d]", i)

		hasNames := listIsNonEmpty(rule.ResourceNames)
		hasLabels := mapIsNonEmpty(rule.MatchLabels)
		if hasNames && hasLabels {
			resp.Diagnostics.AddError("Правило сужено дважды",
				where+": заданы и resource_names, и match_labels. Край принимает ровно одно "+
					"сужение: перечнем идентификаторов ЛИБО отбором по меткам.")
		}
		// Пустой, но заданный список — второе написание отсутствия. Край его не отличит
		// от опущенного и вернёт опущенным, а Terraform увидит расхождение «был пустой
		// список, стал null» на совершенно законной настройке.
		if listIsEmptyButSet(rule.ResourceNames) {
			resp.Diagnostics.AddError("Пустое сужение задано явно",
				where+": resource_names = []. Отсутствие сужения выражается ОПУСКАНИЕМ поля — "+
					"край не отличает пустой перечень от отсутствующего и вернёт его "+
					"отсутствующим, а Terraform сочтёт это расхождением.")
		}
		if mapIsEmptyButSet(rule.MatchLabels) {
			resp.Diagnostics.AddError("Пустой отбор задан явно",
				where+": match_labels = {}. Отсутствие отбора выражается ОПУСКАНИЕМ поля.")
		}

		if isKnownString(rule.Module) && rule.Module.ValueString() == "*" {
			resp.Diagnostics.AddError("Подстановочный модуль недоступен",
				where+": module = \"*\". Подстановка по модулю разрешена только системным "+
					"ролям, а роль, заведённая этим ресурсом, системной не бывает. Назовите "+
					"модуль явно; роль на несколько модулей — это несколько правил.")
		}
		for _, res := range knownStrings(ctx, rule.Resources) {
			if res == "*" {
				resp.Diagnostics.AddError("Подстановочный тип ресурса недоступен",
					where+": resources содержит \"*\". Подстановка по типу разрешена только "+
						"системным ролям. Перечислите типы явно.")
				break
			}
		}
		verbs := knownStrings(ctx, rule.Verbs)
		for _, v := range verbs {
			if v == "*" && len(verbs) != 1 {
				resp.Diagnostics.AddError("Подстановка глагола стоит не одна",
					where+": verbs содержит \"*\" вместе с другими глаголами. \"*\" означает "+
						"«все глаголы типа» и допустим только единственным элементом.")
				break
			}
		}
		for _, n := range knownStrings(ctx, rule.ResourceNames) {
			if n == "*" {
				resp.Diagnostics.AddError("Подстановка вместо идентификатора",
					where+": resource_names содержит \"*\". Сужение перечнем называет "+
						"КОНКРЕТНЫЕ объекты; чтобы правило действовало на все — опустите поле.")
				break
			}
		}
		// Подстановка вместе с сужением — противоречие: «все объекты типа» и «вот эти
		// объекты» одновременно. Край отвергает такую пару отдельным сообщением.
		wildcarded := (isKnownString(rule.Module) && rule.Module.ValueString() == "*") ||
			containsWildcard(ctx, rule.Resources) || containsWildcard(ctx, rule.Verbs)
		if wildcarded && (hasNames || hasLabels) {
			resp.Diagnostics.AddError("Подстановка сужена перечнем",
				where+": подстановка \"*\" задана вместе с resource_names или match_labels. "+
					"Это взаимоисключающие способы указать объекты выдачи.")
		}
	}
}

// isKnownString / knownStrings / listIsNonEmpty / … — маленькие предикаты о значениях
// Terraform. Неизвестное трактуется как «сказать нечего», а НЕ как пустое: на этапе
// проверки настройки ссылка на ещё не созданный ресурс приходит именно неизвестной, и
// счесть её пустой значило бы отвергнуть законную конфигурацию.
func isKnownString(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

func knownStrings(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	return stringsFromTF(ctx, l)
}

func listIsNonEmpty(l types.List) bool {
	return !l.IsNull() && (l.IsUnknown() || len(l.Elements()) > 0)
}

func listIsEmptyButSet(l types.List) bool {
	return !l.IsNull() && !l.IsUnknown() && len(l.Elements()) == 0
}

func mapIsNonEmpty(m types.Map) bool {
	return !m.IsNull() && (m.IsUnknown() || len(m.Elements()) > 0)
}

func mapIsEmptyButSet(m types.Map) bool {
	return !m.IsNull() && !m.IsUnknown() && len(m.Elements()) == 0
}

func containsWildcard(ctx context.Context, l types.List) bool {
	for _, s := range knownStrings(ctx, l) {
		if s == "*" {
			return true
		}
	}
	return false
}

// ---- разбор ответа края --------------------------------------------------------------

// roleWire — форма ответа края. Разбирается своим типом, а не сгенерённым: сгенерённый
// потребовал бы разрешения google.protobuf.Any через глобальный реестр типов, которого у
// провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым — именно там опечатка в
// имени поля прошла бы молча.
type roleWire struct {
	ID             string            `json:"id"`
	AccountID      string            `json:"accountId"`
	ProjectID      string            `json:"projectId"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	IsSystem       bool              `json:"isSystem"`
	CreatedAt      string            `json:"createdAt"`
	Labels         map[string]string `json:"labels"`
	AuthoredVerbs  []string          `json:"authoredVerbs"`
	EffectiveVerbs []string          `json:"effectiveVerbs"`
	VerbNotes      map[string]string `json:"verbNotes"`
	DefinitionTier *struct {
		TierType string `json:"tierType"`
		TierID   string `json:"tierId"`
	} `json:"definitionTier"`
	Rules []struct {
		Module        string            `json:"module"`
		Resources     []string          `json:"resources"`
		Verbs         []string          `json:"verbs"`
		ResourceNames []string          `json:"resourceNames"`
		MatchLabels   map[string]string `json:"matchLabels"`
	} `json:"rules"`
}

// roleStringsOrNull / roleLabelsOrNull — пустое от края превращается в null, а не в пустой
// список или карту. Внутри правила отсутствие сужения выражается ОПУСКАНИЕМ поля, и
// записать туда пустое значило бы объявить пользователю то, чего он не писал, — и получить
// расхождение на следующем же плане.
func roleStringsOrNull(ctx context.Context, in []string) types.List {
	if len(in) == 0 {
		return types.ListNull(types.StringType)
	}
	return listFromStrings(ctx, in)
}

func roleLabelsOrNull(ctx context.Context, in map[string]string) types.Map {
	if len(in) == 0 {
		return types.MapNull(types.StringType)
	}
	return mapToTF(ctx, in)
}

func applyRole(ctx context.Context, m *roleModel, raw []byte) error {
	var w roleWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.AccountID = types.StringValue(w.AccountID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.IsSystem = types.BoolValue(w.IsSystem)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.AuthoredVerbs = listFromStrings(ctx, w.AuthoredVerbs)
	m.EffectiveVerbs = listFromStrings(ctx, w.EffectiveVerbs)
	m.VerbNotes = mapToTF(ctx, w.VerbNotes)

	// Уровень определения перезаписывается ТОЛЬКО когда край его назвал. Поле обязательное,
	// и записать в него null означало бы отдать Terraform результат, которого схема не
	// допускает; молчание края о нём — не повод стирать то, что задал пользователь.
	if w.DefinitionTier != nil {
		tier, diags := types.ObjectValueFrom(ctx, roleTierObjectType().AttrTypes, roleTierModel{
			TierType: types.StringValue(w.DefinitionTier.TierType),
			TierID:   types.StringValue(w.DefinitionTier.TierID),
		})
		if diags.HasError() {
			return fmt.Errorf("уровень определения края не укладывается в объект: %v", diags.Errors())
		}
		m.DefinitionTier = tier
	}

	rules := make([]roleRuleModel, 0, len(w.Rules))
	for _, wr := range w.Rules {
		rules = append(rules, roleRuleModel{
			Module:        types.StringValue(wr.Module),
			Resources:     listFromStrings(ctx, wr.Resources),
			Verbs:         listFromStrings(ctx, wr.Verbs),
			ResourceNames: roleStringsOrNull(ctx, wr.ResourceNames),
			MatchLabels:   roleLabelsOrNull(ctx, wr.MatchLabels),
		})
	}
	list, diags := types.ListValueFrom(ctx, roleRuleObjectType(), rules)
	if diags.HasError() {
		return fmt.Errorf("правила края не укладываются в список: %v", diags.Errors())
	}
	m.Rules = list
	return nil
}

// ---- CRUD -------------------------------------------------------------------------

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tier := roleTierOf(ctx, plan.DefinitionTier)
	if tier == nil {
		resp.Diagnostics.AddError("Уровень определения не задан",
			"definition_tier обязателен: край не заводит роль, не зная, к какому якорю она "+
				"привязана.")
		return
	}
	rules, err := roleRulesOf(ctx, plan.Rules)
	if err != nil {
		resp.Diagnostics.AddError("Правила роли не разобраны", err.Error())
		return
	}

	// Область задаётся ТОЛЬКО уровнем определения: он канонический вход, и при его
	// заданности край пару accountId/projectId не читает вовсе. Прислать вдобавок и её
	// значило бы отправить одно и то же двумя способами — а прочитан был бы один.
	body := &iamv1.CreateRoleRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
		Rules:       roleRulesToProto(ctx, rules),
		DefinitionTier: &iamv1.DefinitionTier{
			TierType: tier.TierType.ValueString(),
			TierId:   tier.TierID.ValueString(),
		},
	}

	id, err := awaitCreate(ctx, r.c, rolesPath, "roleId", typeNameIAMRole,
		tier.TierID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание роли не завершилось", err.Error()+
			"\n\nЕсли отказ говорит о занятом имени — проверьте, не совпадает ли оно с именем "+
			"системной роли платформы: их имена заняты глобально.")
		return
	}

	// Идентификатор записывается ДО обратного чтения: если чтение сорвётся, роль уже
	// создана, и без этой записи Terraform о ней не узнает никогда, а следующий apply
	// заведёт дубль.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт десяток сообщений об
	// «invalid result object» вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, rolesPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Роль создана, но не прочитана обратно", err.Error())
		return
	}
	if err := applyRole(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, rolesPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyRole(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение роли не удалось", err.Error())
		return
	}

	// Список ролей сужается ТОЛЬКО аккаунтом — иного параметра области у него нет. Поэтому
	// у роли, определённой на проекте, здесь пустая область: край отдаст неусечённый
	// каталог, и подтверждение пойдёт по нему. Подставить сюда идентификатор ПРОЕКТА как
	// «аккаунт» было бы хуже отсутствия сужения: край вернул бы каталог несуществующего
	// аккаунта, роль в нём не нашлась бы, и живая роль была бы объявлена удалённой.
	remove, title, detail := absenceDiagnostics(ctx, r.c, rolesPath, client.ScopeAccount,
		"Роль", state.ID.ValueString(), state.AccountID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &iamv1.UpdateRoleRequest{RoleId: state.ID.ValueString()}
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
		rules, err := roleRulesOf(ctx, plan.Rules)
		if err != nil {
			resp.Diagnostics.AddError("Правила роли не разобраны", err.Error())
			return
		}
		body.Rules = roleRulesToProto(ctx, rules)
		paths = append(paths, "rules")
		// Метка версии к правке правил НЕ прикладывается, и это не упущение: край её
		// не отдаёт (почему — в схеме, рядом с created_at), поэтому приложить было бы
		// нечего. Пустую метку край читает как «сверять нечем» и тихо пропускает свою
		// раннюю проверку — то есть отправка такой метки создавала бы видимость
		// оптимистичной блокировки, ни разу не сработавшей.
		//
		// Одновременную правку край ловит сам, ниже по своему пути: версию строки он
		// читает в начале операции и применяет изменение сверкой с ней. Чужая правка,
		// успевшая вклиниться между этим чтением и записью, роняет нашу — независимо от
		// того, прислал клиент метку или нет. Потерять чужую политику молча здесь нечем.
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё
	// изменяемое», а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе в
	// состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			rolesPath+"/"+state.ID.ValueString(), body); err != nil {
			detail := err.Error()
			if state.IsSystem.ValueBool() {
				detail += "\n\nЭта роль системная (is_system = true): край не меняет системные " +
					"роли. Такой ресурс попал под управление импортом; выведите его из " +
					"состояния (terraform state rm) и заведите свою роль."
			}
			detail += "\n\nЕсли отказ говорит об одновременном изменении — роль правил кто-то " +
				"ещё, пока применялась эта правка. Повторите apply: провайдер перечитает " +
				"роль и построит изменение от её нового состояния."
			resp.Diagnostics.AddError("Изменение роли не завершилось", detail)
			return
		}
	}

	plan.ID = state.ID
	raw, err := readByID(ctx, r.c, rolesPath, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Роль изменена, но не прочитана обратно", err.Error())
		return
	}
	if err := applyRole(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := awaitMutation(ctx, r.c, http.MethodDelete, rolesPath+"/"+state.ID.ValueString(), nil)
	if err == nil {
		return
	}
	// Что мешает удалению — называется сразу, иначе причина выясняется перебором.
	// Отказ края передаётся дословно (он называет предмет), а ниже — подсказка про
	// самую частую причину, а НЕ утверждение о ней: отказ приходит и по другим поводам.
	//
	// Роль освобождает УДАЛЕНИЕ привязки, а не её отзыв: ссылку держит сама строка
	// привязки, и отозванная строка её не отпускает — она продолжает существовать.
	// Здесь стояло обратное («отозванные привязки удалению не мешают»), и эта фраза
	// уводила в сторону ровно того, кто уже сделал единственное очевидное действие:
	// отозвал выдачи, получил тот же отказ и пошёл искать причину не там.
	detail := err.Error() +
		"\n\nСамая частая причина — привязки прав, ссылающиеся на эту роль. Их надо " +
		"УДАЛИТЬ: отзыв оставляет привязку на месте, а вместе с ней и ссылку на роль."
	if state.IsSystem.ValueBool() {
		detail += "\n\nЭта роль системная (is_system = true) и не удаляется вовсе: платформа " +
			"сеет такие роли миграциями. Выведите ресурс из состояния — terraform state rm."
	}
	resp.Diagnostics.AddError("Удаление роли не завершилось", detail)
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "роль", "rol", req, resp)
}
