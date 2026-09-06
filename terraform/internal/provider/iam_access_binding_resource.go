// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const accessBindingsPath = "/iam/v1/accessBindings"

// Привязка прав — сама выдача: КОМУ (`subjects`), какая роль (`role_id`), ГДЕ
// (`scope_type`/`scope_id`) и НА ЧТО в пределах этого якоря (`target`).
//
// Изменяемы у привязки ровно два поля — защита от удаления и метки. Всё остальное задаётся
// при выдаче и меняется только пересозданием, и это не ограничение провайдера: у края в
// маске изменения принимаются лишь `deletion_protection` и `labels`, любое другое имя
// отвергается синхронно. Поэтому «сменить роль» или «добавить получателя» означает снять
// выдачу и выдать заново — и лучше, чтобы это было видно в плане как замена, чем чтобы
// правка молча не применилась.
type accessBindingModel struct {
	ID        types.String `tfsdk:"id"`
	RoleID    types.String `tfsdk:"role_id"`
	ScopeType types.String `tfsdk:"scope_type"`
	ScopeID   types.String `tfsdk:"scope_id"`
	Subjects  types.Set    `tfsdk:"subjects"`
	Target    types.Object `tfsdk:"target"`
	ExpiresAt types.String `tfsdk:"expires_at"`

	DeletionProtection types.Bool `tfsdk:"deletion_protection"`
	Labels             types.Map  `tfsdk:"labels"`

	Status          types.String `tfsdk:"status"`
	CreatedAt       types.String `tfsdk:"created_at"`
	GrantedByUserID types.String `tfsdk:"granted_by_user_id"`
	MaterializedAt  types.String `tfsdk:"materialized_at"`
}

// accessTargetModel — на какие объекты под якорем действует выдача.
//
// Поле модели у привязки объявлено types.Object, а НЕ указателем на эту структуру:
// указатель неспособен держать НЕИЗВЕСТНОЕ значение, а цель выдачи сплошь и рядом
// собирается из идентификаторов ресурсов, которых в момент плана ещё нет. На указателе
// провайдер отвечал бы «target type cannot handle unknown values» — отказом, из которого
// не видно ни поля, ни причины.
type accessTargetModel struct {
	AllInScope types.Bool `tfsdk:"all_in_scope"`
	Resources  types.Set  `tfsdk:"resources"`
}

// targetResourceModel — один объект в пообъектной цели. Оба поля обязательны: цель
// закрытая, и «объект без типа» либо «тип без объекта» не значат ничего.
type targetResourceModel struct {
	Type types.String `tfsdk:"type"`
	ID   types.String `tfsdk:"id"`
}

// subjectModel — один получатель права.
//
// `type` задаётся ЯВНО, хотя край умеет вывести его из префикса идентификатора: вывод
// означал бы, что получатель права в конфигурации не назван, а угадан, и опечатка в
// префиксе давала бы выдачу другому виду принципала вместо отказа.
type subjectModel struct {
	Type types.String `tfsdk:"type"`
	ID   types.String `tfsdk:"id"`
}

type accessBindingResource struct{ c *client.Client }

// NewIAMAccessBindingResource — конструктор для реестра провайдера.
func NewIAMAccessBindingResource() resource.Resource { return &accessBindingResource{} }

var (
	_ resource.Resource                   = (*accessBindingResource)(nil)
	_ resource.ResourceWithValidateConfig = (*accessBindingResource)(nil)
	_ resource.ResourceWithImportState    = (*accessBindingResource)(nil)
	_ resource.ResourceWithConfigure      = (*accessBindingResource)(nil)
)

// MoveState — переезд состояния с прежнего имени типа.
//
// Объявление обязательно: одного блока `moved` в настройке оператора НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план. Что именно принимается и почему — iam_type_names.go.
func (r *accessBindingResource) MoveState(ctx context.Context) []resource.StateMover {
	return movedFromRetiredTypeName(ctx, r)
}

func (r *accessBindingResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameIAMAccessBinding
}

func (r *accessBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// scopeTiers — уровни иерархии, к которым привязка вправе крепиться.
//
// Перечень выписан ЛИТЕРАЛАМИ, и вот почему это не «второй словарь», а осознанный выбор,
// в отличие от типов объектов цели (см. validateTargetResources): уровней ровно три и они
// СТРУКТУРНЫ — кластер, аккаунт, проект, — это сама форма иерархии, а не растущий каталог
// ресурсов. Край на негодном значении отвечает «Illegal argument scopeType», не называя
// допустимых; здесь отказ приходит раньше и с перечнем. Цена названа честно: заведи край
// четвёртый уровень — эта проверка станет тем, что лжёт, и правится она здесь.
var scopeTiers = []string{"iam.cluster", "iam.account", "iam.project"}

func (r *accessBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Всё, что не «защита от удаления» и не «метки», край в маске изменения не принимает,
	// поэтому каждое такое поле помечено пересоздающим ЯВНО. Умолчание здесь означало бы
	// правку, которая проходит планом и не доезжает до края.
	replaceStr := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Привязка прав: **кому** (`subjects`), **какая роль** (`role_id`), " +
			"**где** (`scope_type` + `scope_id`) и **на что** под этим якорем (`target`).\n\n" +
			":::warning Право на многих выдаётся ГРУППЕ, а не перечислением субъектов\n" +
			"Если получателей больше одного — или состав может измениться, — заведите " +
			"`kaname_group`, выдайте право ей и добавляйте людей и служебные учётки в группу. " +
			"Цена перечисления видна на снятии: убрать одного из группы — одна строка членства; " +
			"убрать одного из `subjects` — снятие выданных кортежей по всем объектам роли, а так " +
			"как состав неизменяем, ещё и пересоздание самой привязки. Перечисление законно ровно " +
			"в одном случае: получатель ОДИН и меняться не будет (служебная учётка модуля, чья " +
			"личность и есть предмет выдачи).\n:::\n\n" +
			":::danger `destroy` УДАЛЯЕТ привязку, а не отзывает её\n" +
			"У края два разных действия: `Delete` сносит строку (после него `Get` отвечает «не " +
			"найдено»), `:revoke` строку СОХРАНЯЕТ со статусом `REVOKED` ради аудиторского следа. " +
			"Доступ снимается одинаково обоими — один и тот же набор кортежей в той же транзакции " +
			"записи, — поэтому выбор не про безопасность, а про то, что значит «ресурса нет». " +
			"Провайдер зовёт `Delete`: см. описание удаления ниже.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор привязки (префикс `acb`). " +
					"По нему выполняется импорт.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			"role_id": schema.StringAttribute{Required: true, PlanModifiers: replaceStr,
				MarkdownDescription: "Роль, которая выдаётся. Смена роли — это другая выдача: " +
					"край не принимает `role_id` в маске изменения, поэтому правка пересоздаёт привязку."},

			"scope_type": schema.StringAttribute{Required: true, PlanModifiers: replaceStr,
				MarkdownDescription: "Уровень якоря выдачи в дотированной форме: `" +
					strings.Join(scopeTiers, "` · `") + "`. Якорь — это ГДЕ действует право; " +
					"на что именно под ним — отдельно, в `target`."},

			"scope_id": schema.StringAttribute{Required: true, PlanModifiers: replaceStr,
				MarkdownDescription: "Идентификатор объекта-якоря: `cluster_kacho_root` для " +
					"`iam.cluster`, `acc…` для `iam.account`, `prj…` для `iam.project`."},

			// НАБОР, а не список: получатели — множество, порядок между ними ничего не
			// значит, и список объявлял бы перестановку изменением, то есть заменой всей
			// привязки. Внутри элемента нет ни Computed, ни умолчания — элемент набора
			// адресуется своим значением целиком, и подстановка умолчания в один элемент
			// сломала бы поиск настройки для остальных.
			"subjects": schema.SetNestedAttribute{Required: true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
				MarkdownDescription: "Получатели права — НАБОР, от 1 до 32. Порядок не значим. " +
					"Состав неизменяем: край не принимает `subjects` в маске изменения, поэтому " +
					"добавление или снятие получателя пересоздаёт привязку. Норму «право на " +
					"многих — группе» см. в предупреждении выше.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{Required: true,
						MarkdownDescription: "Вид принципала: `" +
							strings.Join(subjectTypeNames(), "` · `") + "`."},
					"id": schema.StringAttribute{Required: true,
						MarkdownDescription: "Идентификатор принципала — `usr…`, `sva…` или " +
							"`grp…`, согласованный с `type`."},
				}}},

			"target": schema.SingleNestedAttribute{Required: true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				MarkdownDescription: "На какие объекты под якорем действует выдача. Обязателен и " +
					"умолчания НЕ имеет: широчайшая выдача берётся только явным " +
					"`all_in_scope = true`, чтобы её нельзя было получить по невнимательности. " +
					"Ровно одна ветвь — либо `all_in_scope`, либо `resources`.",
				Attributes: map[string]schema.Attribute{
					"all_in_scope": schema.BoolAttribute{Optional: true,
						MarkdownDescription: "`true` — право действует на ВСЕ объекты под якорем, " +
							"включая ещё не созданные. Это самая широкая выдача из возможных.\n\n" +
							"Значение `false` не выражает ничего и отвергается: ветвь выбирается " +
							"ОПУСКАНИЕМ поля, а два способа сказать одно и то же разошлись бы " +
							"между настройкой и состоянием."},
					// Тот же довод, что у получателей: пообъектная цель — множество, порядок
					// в ней не значим.
					"resources": schema.SetNestedAttribute{Optional: true,
						MarkdownDescription: "Пообъектная выдача — закрытый набор объектов, до 256. " +
							"Порядок не значим.",
						NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{Required: true,
								MarkdownDescription: "Дотированный тип объекта, например " +
									"`compute.instance`, `vpc.network`, `storage.volumes`. " +
									"Написание неединообразно (где-то единственное число, где-то " +
									"множественное) — сверяйтесь с документацией домена, а не с " +
									"догадкой."},
							"id": schema.StringAttribute{Required: true,
								MarkdownDescription: "Идентификатор объекта. Подстановочный знак " +
									"`*` не принимается: широчайшая выдача выражается ветвью " +
									"`all_in_scope`, а не звёздочкой в пообъектном перечне."},
						}}},
				}},

			"expires_at": schema.StringAttribute{Optional: true, PlanModifiers: replaceStr,
				MarkdownDescription: "Момент, после которого выдача перестаёт действовать, в " +
					"RFC 3339 (`2026-12-31T23:59:59Z`). Не задан — выдача бессрочна.\n\n" +
					":::tip Не меньше пяти минут вперёд\n" +
					"Доступ материализуется асинхронно в ограниченном окне, поэтому более короткий " +
					"срок мог бы истечь раньше, чем право вообще заработает; край такое значение " +
					"отвергает. И помните: это АБСОЛЮТНЫЙ момент — записанный в конфигурацию, он " +
					"однажды окажется в прошлом, и следующий `apply` упрётся в тот же отказ.\n:::"},

			"deletion_protection": schema.BoolAttribute{Optional: true, Computed: true,
				Default: booldefault.StaticBool(false),
				MarkdownDescription: "Защита от случайного удаления. Пока `true`, край отвергает " +
					"удаление синхронно; снимается обычной правкой этого поля.\n\n" +
					"Помните, что защита действует и на ЗАМЕНУ: правка `role_id`, `subjects`, " +
					"`target` или `scope_*` пересоздаёт привязку, то есть сначала удаляет её — и " +
					"упрётся в защиту. Снимите флаг отдельным `apply`, затем меняйте остальное."},

			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки самой привязки. Изменяемы."},

			// Вычисляемые поля разделены по ОДНОМУ признаку — меняется ли значение за жизнь
			// привязки, — и признак этот виден только в контракте, а не в схеме.
			//
			// Неизменяемые (`created_at`, `granted_by_user_id`: чеканятся при вставке и
			// больше не трогаются) несут UseStateForUnknown. Без него правка меток или
			// защиты от удаления показывала бы их в плане как «известно после применения» —
			// то есть план обещал бы пересчёт того, что пересчитаться не может, и прятал
			// настоящее изменение среди трёх шумовых строк.
			//
			// Изменяемые (`status`: PENDING→ACTIVE→REVOKED; `materialized_at`: обновляется
			// каждым проходом материализации) его НЕ несут намеренно. Там удержание
			// прежнего значения было бы враньём в другую сторону: план утверждал бы, что
			// поле останется прежним, а обратное чтение приносило бы другое, и применение
			// срывалось бы несогласованностью. Не унифицировать их «для единообразия».
			"status": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Состояние выдачи: `PENDING`, `ACTIVE` или `REVOKED`. " +
					"Отозванную привязку провайдер считает отсутствующей — см. описание удаления."},
			"created_at": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"granted_by_user_id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Кто выдал право. Заполняется краем из личности вызывающего.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"materialized_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Когда пообъектный доступ этой выдачи в последний раз стал " +
					"живым. Пусто означает «ещё не материализовалось» ЛИБО «этой привязке нечего " +
					"материализовать пообъектно» (выдача на уровне кластера) — но НИКОГДА «выдача " +
					"не работает». Поле полезно ровно тем, что отличает окно распространения от " +
					"ошибки в самой выдаче: пока оно пусто, отказ доступа у получателя может быть " +
					"временным."},
		},
	}
}

// ---- типы значений -------------------------------------------------------------------
//
// Каждый объявлен ОДИН раз и рядом со своей схемой: разойдись он с ней хоть одним полем,
// значение молча не собралось бы, и вложенная часть исчезла бы из состояния.

func subjectObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
	}}
}

func targetResourceObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
	}}
}

func accessTargetObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"all_in_scope": types.BoolType,
		"resources":    types.SetType{ElemType: targetResourceObjectType()},
	}}
}

// ---- чтение значений Terraform ---------------------------------------------------------

// subjectsOf — получатели из набора. Неизвестный и null дают nil: «не задал» и «задал
// пустым» — разные вещи, и на первом провайдеру нечего утверждать.
func subjectsOf(ctx context.Context, s types.Set) []subjectModel {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := make([]subjectModel, 0, len(s.Elements()))
	_ = s.ElementsAs(ctx, &out, false)
	return out
}

// accessTargetOf — цель выдачи из значения Terraform. nil означает «сказать пока нечего»:
// цель целиком не задана либо ещё не вычислена.
func accessTargetOf(ctx context.Context, o types.Object) *accessTargetModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m accessTargetModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

func targetResourcesOf(ctx context.Context, s types.Set) []targetResourceModel {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := make([]targetResourceModel, 0, len(s.Elements()))
	_ = s.ElementsAs(ctx, &out, false)
	return out
}

// ---- перечисление вида принципала ------------------------------------------------------

// subjectTypeNames — допустимые имена вариантов в порядке их номеров, из СГЕНЕРЁННОЙ
// таблицы контракта.
//
// Своя копия перечня разошлась бы с контрактом молча, поэтому имена берутся у
// сгенерённого типа. Нулевой вариант («не указан») из перечня исключён намеренно: на нём
// край выводит вид принципала из префикса идентификатора, то есть догадывается, — а в
// конфигурации получатель права называется прямо.
func subjectTypeNames() []string {
	nums := make([]int, 0, len(iamv1.SubjectType_name))
	for n := range iamv1.SubjectType_name {
		if n != int32(iamv1.SubjectType_SUBJECT_TYPE_UNSPECIFIED) {
			nums = append(nums, int(n))
		}
	}
	sort.Ints(nums)
	out := make([]string, 0, len(nums))
	for _, n := range nums {
		out = append(out, iamv1.SubjectType_name[int32(n)]) // #nosec G115 -- ключи взяты из той же таблицы
	}
	return out
}

// subjectTypeOf — вариант перечисления по ИМЕНИ. Неизвестное имя — отказ с перечнем
// допустимых, а не ноль: ноль означал бы «не задано», и опечатка прошла бы как умолчание.
func subjectTypeOf(name string) (iamv1.SubjectType, error) {
	if v, ok := iamv1.SubjectType_value[name]; ok && v != int32(iamv1.SubjectType_SUBJECT_TYPE_UNSPECIFIED) {
		return iamv1.SubjectType(v), nil
	}
	return 0, fmt.Errorf("вид принципала %q не входит в перечисление; допустимы: %s",
		name, strings.Join(subjectTypeNames(), ", "))
}

// ---- проверка настройки ----------------------------------------------------------------

// ValidateConfig ловит до отправки то, что край отвергнет, и то, о чём стоит предупредить.
//
// Проверяется только детерминированное: часы сюда не заглядывают. Требование «срок не
// ближе пяти минут» здесь НЕ проверяется намеренно — план исполняется в один момент,
// применение в другой, и проверка по часам делала бы одну и ту же настройку то годной, то
// негодной. Формат разбирается (он от времени не зависит), а само окно адъюдицирует край,
// у которого часы те же, что у выдачи.
func (r *accessBindingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg accessBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if v := cfg.ScopeType; !v.IsNull() && !v.IsUnknown() {
		known := false
		for _, t := range scopeTiers {
			if t == v.ValueString() {
				known = true
				break
			}
		}
		if !known {
			resp.Diagnostics.AddError("Негодный уровень якоря",
				fmt.Sprintf("scope_type = %q. Допустимы: %s. Уровень задаётся дотированной "+
					"формой — короткое «account» край тоже отвергает.",
					v.ValueString(), strings.Join(scopeTiers, ", ")))
		}
	}

	validateBindingSubjects(ctx, cfg.Subjects, resp)
	validateBindingTarget(ctx, cfg.Target, resp)

	if v := cfg.ExpiresAt; !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		if _, err := time.Parse(time.RFC3339, v.ValueString()); err != nil {
			resp.Diagnostics.AddError("Негодный срок выдачи",
				fmt.Sprintf("expires_at = %q не разбирается как момент RFC 3339 "+
					"(например 2026-12-31T23:59:59Z): %v", v.ValueString(), err))
		}
	}
}

func validateBindingSubjects(ctx context.Context, set types.Set, resp *resource.ValidateConfigResponse) {
	if set.IsNull() || set.IsUnknown() {
		return
	}
	subs := subjectsOf(ctx, set)
	switch {
	case len(subs) == 0:
		resp.Diagnostics.AddError("Получатель права не назван",
			"В subjects пусто. Выдача без получателя не значит ничего, и край отвергает её.")
	case len(subs) > 32:
		resp.Diagnostics.AddError("Получателей больше, чем принимает край",
			fmt.Sprintf("В subjects задано %d получателей, край принимает не более 32. "+
				"Это тот случай, ради которого существуют группы.", len(subs)))
	case len(subs) > 1:
		// Предупреждение, а не отказ: край такую выдачу принимает, и норма продукта
		// говорит о том, КАК правильно, а не о том, что невозможно. Молчать здесь значило
		// бы дать перечислению выглядеть равноправным способом.
		resp.Diagnostics.AddWarning("Право выдаётся перечислением, а не группе",
			fmt.Sprintf("В subjects задано получателей: %d. Норма платформы: право, которое "+
				"получает больше одного принципала — или чей состав может измениться, — выдаётся "+
				"ГРУППЕ, а люди и служебные учётки добавляются в неё. Цена перечисления видна на "+
				"снятии: убрать одного из группы — одна строка членства; убрать одного отсюда — "+
				"снятие выданных кортежей по всем объектам роли плюс пересоздание привязки "+
				"(состав неизменяем). Перечисление законно, когда получатель один и меняться "+
				"не будет.", len(subs)))
	}

	for i, s := range subs {
		if s.Type.IsUnknown() || s.Type.IsNull() {
			continue
		}
		if _, err := subjectTypeOf(s.Type.ValueString()); err != nil {
			resp.Diagnostics.AddError("Негодный вид принципала",
				fmt.Sprintf("subjects[%d]: %v", i, err))
		}
	}
}

func validateBindingTarget(ctx context.Context, obj types.Object, resp *resource.ValidateConfigResponse) {
	t := accessTargetOf(ctx, obj)
	if t == nil {
		return
	}

	// Ложь в all_in_scope не выражает ничего: ветвь выбирается опусканием поля. Отвергаем
	// явно, иначе «all_in_scope = false» без resources прочиталось бы как «цель не задана»
	// и вернулось бы отказом края про отсутствующее поле, которое пользователь написал.
	if !t.AllInScope.IsNull() && !t.AllInScope.IsUnknown() && !t.AllInScope.ValueBool() {
		resp.Diagnostics.AddError("Ложь в all_in_scope ничего не выражает",
			"target.all_in_scope = false. Ветвь цели выбирается ОПУСКАНИЕМ поля, а не ложью: "+
				"два способа сказать «не эта ветвь» разошлись бы между настройкой и состоянием. "+
				"Уберите поле и задайте target.resources.")
		return
	}

	res := targetResourcesOf(ctx, t.Resources)

	// Неизвестное считается ЗАДАННЫМ — и у набора, и у флага.
	//
	// У набора это уже было: ссылка на ещё не созданный объект приходит именно
	// неизвестной. У флага — не было, и на нём проверка отвергала верную настройку:
	// `all_in_scope`, вычисляемый выражением (переменная, условие по чужому атрибуту),
	// приходит сюда неизвестным, `ValueBool()` отдаёт на нём ЛОЖЬ, и настройка, где
	// выбрана как раз широчайшая ветвь, получала отказ «цель выдачи не задана» — про
	// поле, которое пользователь написал. Ложь и «ещё не вычислено» здесь неразличимы
	// по значению, поэтому различать надо по состоянию, а не по нему.
	//
	// Пока ветвь не вычислена, утверждать о её выборе нечего: взаимоисключение
	// адъюдицирует край, у которого к моменту применения оба значения есть, — и его
	// отказ называет поле. Проверки, не зависящие от выбора ветви (потолок набора,
	// форма ссылок), исполняются ниже в любом случае.
	allUnknown := t.AllInScope.IsUnknown()
	all := !t.AllInScope.IsNull() && !allUnknown && t.AllInScope.ValueBool()
	hasRes := !t.Resources.IsNull() && (t.Resources.IsUnknown() || len(res) > 0)

	switch {
	case allUnknown:
		// Ветвь ещё не вычислена — молчим (см. выше). Пустая ветвь здесь намеренная:
		// без неё оба отказа ниже сработали бы на неизвестном значении.
	case all && hasRes:
		resp.Diagnostics.AddError("Цель выдачи задана двумя ветвями сразу",
			"В target заданы и all_in_scope, и resources. Ветви взаимоисключающи: «все объекты "+
				"под якорем» и «вот эти объекты» — разные выдачи, и край отвергнет обе вместе.")
	case !all && !hasRes:
		resp.Diagnostics.AddError("Цель выдачи не задана",
			"В target не выбрана ни одна ветвь. Умолчания у цели нет намеренно: широчайшая "+
				"выдача берётся только явным all_in_scope = true, чтобы её нельзя было получить "+
				"по невнимательности. Задайте либо all_in_scope = true, либо непустой resources.")
	}

	if len(res) > 256 {
		resp.Diagnostics.AddError("Объектов в цели больше, чем принимает край",
			fmt.Sprintf("В target.resources задано %d объектов, край принимает не более 256. "+
				"Выдача такого размера — повод пересмотреть якорь, а не дробить перечень.", len(res)))
	}
	validateTargetResources(res, resp)
}

// validateTargetResources проверяет ФОРМУ ссылки на объект, но не сам тип.
//
// Закрытая таблица типов живёт у владельца — сервиса iam, — и переписать её сюда значило
// бы завести второй словарь, который разойдётся с первым молча: край добавит тип, а
// провайдер начнёт отвергать законную настройку и окажется тем, кто лжёт. Поэтому
// принадлежность типа таблице адъюдицирует край — его отказ называет и поле, и значение.
// Здесь ловится только то, что таблице заведомо не принадлежит ни при каком её составе:
// пустое значение, недотированная форма и подстановочный знак.
func validateTargetResources(res []targetResourceModel, resp *resource.ValidateConfigResponse) {
	for i, rr := range res {
		if !rr.Type.IsUnknown() && !rr.Type.IsNull() {
			v := rr.Type.ValueString()
			switch {
			case v == "*":
				resp.Diagnostics.AddError("Подстановочный знак в типе объекта",
					fmt.Sprintf("target.resources[%d].type = \"*\". Широчайшая выдача выражается "+
						"ветвью all_in_scope, а не звёздочкой: звёздочка в пообъектном перечне "+
						"выглядела бы сужением, оставаясь расширением.", i))
			case !strings.Contains(v, "."):
				resp.Diagnostics.AddError("Тип объекта задан не дотированной формой",
					fmt.Sprintf("target.resources[%d].type = %q. Тип называется как "+
						"«<домен>.<ресурс>», например compute.instance или vpc.subnet.", i, v))
			}
		}
		if !rr.ID.IsUnknown() && !rr.ID.IsNull() && rr.ID.ValueString() == "*" {
			resp.Diagnostics.AddError("Подстановочный знак в идентификаторе объекта",
				fmt.Sprintf("target.resources[%d].id = \"*\". Пообъектная выдача перечисляет "+
					"объекты поимённо; «все» — это ветвь all_in_scope.", i))
		}
	}
}

// ---- перевод модели в запрос края --------------------------------------------------------

func subjectsToProto(ctx context.Context, set types.Set) ([]*iamv1.Subject, error) {
	subs := subjectsOf(ctx, set)
	if len(subs) == 0 {
		return nil, fmt.Errorf("subjects: получатель права не назван")
	}
	out := make([]*iamv1.Subject, 0, len(subs))
	for i, s := range subs {
		t, err := subjectTypeOf(s.Type.ValueString())
		if err != nil {
			return nil, fmt.Errorf("subjects[%d]: %w", i, err)
		}
		out = append(out, &iamv1.Subject{Type: t, Id: s.ID.ValueString()})
	}
	return out, nil
}

func targetToProto(ctx context.Context, obj types.Object) (*iamv1.AccessTarget, error) {
	t := accessTargetOf(ctx, obj)
	if t == nil {
		return nil, fmt.Errorf("target: цель выдачи не задана")
	}
	res := targetResourcesOf(ctx, t.Resources)
	all := t.AllInScope.ValueBool()
	switch {
	case all && len(res) > 0:
		return nil, fmt.Errorf("target: all_in_scope и resources взаимоисключающи")
	case all:
		return &iamv1.AccessTarget{
			Target: &iamv1.AccessTarget_AllInScope{AllInScope: &iamv1.AccessTargetAllInScope{}},
		}, nil
	case len(res) > 0:
		refs := make([]*iamv1.ResourceRef, 0, len(res))
		for _, rr := range res {
			refs = append(refs, &iamv1.ResourceRef{Type: rr.Type.ValueString(), Id: rr.ID.ValueString()})
		}
		return &iamv1.AccessTarget{
			Target: &iamv1.AccessTarget_Resources{
				Resources: &iamv1.AccessTargetResources{Resources: refs}},
		}, nil
	default:
		return nil, fmt.Errorf("target: не выбрана ни одна ветвь — задайте all_in_scope = true " +
			"либо непустой resources")
	}
}

// expiresAtToProto — срок выдачи в форму контракта. Пустое и неизвестное дают nil:
// «не задал срок» означает бессрочную выдачу, и подстановка нуля превратила бы её в
// выдачу, истёкшую в начале эпохи.
func expiresAtToProto(v types.String) (*timestamppb.Timestamp, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v.ValueString())
	if err != nil {
		return nil, fmt.Errorf("expires_at=%q не разбирается как момент RFC 3339 "+
			"(например 2026-12-31T23:59:59Z): %w", v.ValueString(), err)
	}
	return timestamppb.New(t), nil
}

func (r *accessBindingResource) createBody(ctx context.Context, plan *accessBindingModel) (*iamv1.CreateAccessBindingRequest, error) {
	subs, err := subjectsToProto(ctx, plan.Subjects)
	if err != nil {
		return nil, err
	}
	target, err := targetToProto(ctx, plan.Target)
	if err != nil {
		return nil, err
	}
	expires, err := expiresAtToProto(plan.ExpiresAt)
	if err != nil {
		return nil, err
	}
	// Устаревшая одиночная пара subject_type/subject_id НЕ заполняется: край проецирует её
	// на первый элемент subjects при чтении, и заполнять её на входе значило бы завести
	// второй способ сказать то же самое — тот самый, на расхождении которого край отвечает
	// отказом, если пара не совпала с subjects[0].
	return &iamv1.CreateAccessBindingRequest{
		RoleId:             plan.RoleID.ValueString(),
		ScopeType:          plan.ScopeType.ValueString(),
		ScopeId:            plan.ScopeID.ValueString(),
		Subjects:           subs,
		Target:             target,
		ExpiresAt:          expires,
		DeletionProtection: plan.DeletionProtection.ValueBool(),
		Labels:             mapFromTF(ctx, plan.Labels),
	}, nil
}

// ---- разбор ответа края -------------------------------------------------------------------

// accessBindingWire — форма ответа края. Разбирается своим типом, а не сгенерённым:
// сгенерённый потребовал бы разрешения google.protobuf.Any через глобальный реестр типов,
// которого у провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым — именно там
// опечатка в имени поля прошла бы молча.
type accessBindingWire struct {
	ID                 string            `json:"id"`
	RoleID             string            `json:"roleId"`
	ScopeType          string            `json:"scopeType"`
	ScopeID            string            `json:"scopeId"`
	Status             string            `json:"status"`
	CreatedAt          string            `json:"createdAt"`
	ExpiresAt          string            `json:"expiresAt"`
	GrantedByUserID    string            `json:"grantedByUserId"`
	MaterializedAt     string            `json:"materializedAt"`
	DeletionProtection bool              `json:"deletionProtection"`
	Labels             map[string]string `json:"labels"`
	Subjects           []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"subjects"`
	Target *struct {
		// Пустой объект ветви «все под якорем»: важно САМО присутствие ключа, полей у
		// него нет. Указатель отличает «ветвь выбрана» от «ветви нет».
		AllInScope *struct{} `json:"allInScope"`
		Resources  *struct {
			Resources []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"resources"`
		} `json:"resources"`
	} `json:"target"`
}

func applyAccessBinding(ctx context.Context, m *accessBindingModel, raw []byte) error {
	var w accessBindingWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.RoleID = types.StringValue(w.RoleID)
	m.ScopeType = types.StringValue(w.ScopeType)
	m.ScopeID = types.StringValue(w.ScopeID)
	m.Status = types.StringValue(w.Status)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.GrantedByUserID = types.StringValue(w.GrantedByUserID)
	m.MaterializedAt = types.StringValue(w.MaterializedAt)
	m.DeletionProtection = types.BoolValue(w.DeletionProtection)
	m.Labels = mapToTF(ctx, w.Labels)

	if w.ExpiresAt == "" {
		m.ExpiresAt = types.StringNull()
	} else {
		m.ExpiresAt = types.StringValue(w.ExpiresAt)
	}

	// Состав получателей и цель НЕ затираются, когда край их не вернул.
	//
	// Оба поля обязательны и неизменяемы, то есть исчезнуть у живой привязки не могут;
	// пустой ответ здесь означает не «их сняли», а «эта строка их не несёт» — так
	// выглядят привязки, заведённые до появления цели. Записать на их месте null значило
	// бы объявить пользователю удаление того, чего никто не удалял, и на обратном чтении
	// после создания сорвать применение несогласованностью.
	if len(w.Subjects) > 0 {
		subs := make([]subjectModel, 0, len(w.Subjects))
		for _, s := range w.Subjects {
			subs = append(subs, subjectModel{
				Type: types.StringValue(s.Type),
				ID:   types.StringValue(s.ID),
			})
		}
		set, diags := types.SetValueFrom(ctx, subjectObjectType(), subs)
		if diags.HasError() {
			return fmt.Errorf("получатели края не укладываются в набор: %v", diags.Errors())
		}
		m.Subjects = set
	}

	if w.Target != nil {
		tm := accessTargetModel{
			AllInScope: types.BoolNull(),
			Resources:  types.SetNull(targetResourceObjectType()),
		}
		switch {
		case w.Target.AllInScope != nil:
			tm.AllInScope = types.BoolValue(true)
		case w.Target.Resources != nil:
			refs := make([]targetResourceModel, 0, len(w.Target.Resources.Resources))
			for _, rr := range w.Target.Resources.Resources {
				refs = append(refs, targetResourceModel{
					Type: types.StringValue(rr.Type),
					ID:   types.StringValue(rr.ID),
				})
			}
			set, diags := types.SetValueFrom(ctx, targetResourceObjectType(), refs)
			if diags.HasError() {
				return fmt.Errorf("объекты цели края не укладываются в набор: %v", diags.Errors())
			}
			tm.Resources = set
		}
		obj, diags := types.ObjectValueFrom(ctx, accessTargetObjectType().AttrTypes, tm)
		if diags.HasError() {
			return fmt.Errorf("цель выдачи края не укладывается в объект: %v", diags.Errors())
		}
		m.Target = obj
	}
	return nil
}

// keepExpiresSpelling сохраняет написание срока из настройки, когда край вернул ТОТ ЖЕ
// момент.
//
// Момент один, а записей у него много: «…T10:00:00+03:00» и «…T07:00:00Z» — это одно и то
// же время. Край хранит момент в UTC и усекает до секунды, поэтому его ответ почти
// никогда не совпадает со строкой пользователя буквально, — а сравнивает план со
// состоянием Terraform по СТРОКЕ. Без этой сверки законная настройка давала бы
// расхождение на каждом плане и срыв применения на первом же.
func keepExpiresSpelling(want types.String, m *accessBindingModel) {
	if want.IsNull() || want.IsUnknown() || want.ValueString() == "" {
		return
	}
	if m.ExpiresAt.IsNull() {
		return
	}
	a, err1 := time.Parse(time.RFC3339, want.ValueString())
	b, err2 := time.Parse(time.RFC3339, m.ExpiresAt.ValueString())
	if err1 != nil || err2 != nil {
		return
	}
	// Усечение до секунды — не вольность: край сам усекает временные метки до секунд, и
	// доли секунды из настройки до него не доезжают.
	if a.Truncate(time.Second).Equal(b.Truncate(time.Second)) {
		m.ExpiresAt = want
	}
}

// ---- CRUD ---------------------------------------------------------------------------------

func (r *accessBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accessBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.createBody(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Негодная выдача прав", err.Error())
		return
	}

	// Адрес для ключа идемпотентности — якорь и роль: имени у привязки нет, а эта пара
	// вместе с телом запроса описывает выдачу однозначно.
	id, err := awaitCreate(ctx, r.c, accessBindingsPath, "accessBindingId",
		typeNameIAMAccessBinding,
		plan.ScopeID.ValueString()+"/"+plan.RoleID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Выдача прав не завершилась", err.Error())
		return
	}

	// Идентификатор пишется в состояние ДО обратного чтения: если чтение сорвётся, право
	// уже выдано, и без этой записи Terraform о нём не узнает никогда — а невидимая
	// действующая выдача хуже отсутствующей.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после применения, и без этого сорвавшееся чтение даёт по сообщению на
	// каждое поле вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, accessBindingsPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Право выдано, но привязка не прочитана обратно", err.Error())
		return
	}
	wantExpires := plan.ExpiresAt
	if err := applyAccessBinding(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepExpiresSpelling(wantExpires, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *accessBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accessBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	raw, err := readByID(ctx, r.c, accessBindingsPath, id, false)
	if err == nil {
		wantExpires := state.ExpiresAt
		if err := applyAccessBinding(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		// Отозванная привязка читается как ОТСУТСТВУЮЩАЯ, хотя строка у края есть.
		//
		// Мягкий отзыв снимает все выданные кортежи и оставляет строку ради аудиторского
		// следа: объект существует, доступа не даёт. Держать его в состоянии значило бы
		// показывать план без изменений на выдаче, которой больше нет, — форма без
		// содержания. Снятие из состояния приводит настройку и действительность к
		// согласию: следующее применение выдаст право заново, новой строкой.
		if state.Status.ValueString() == "REVOKED" {
			resp.Diagnostics.AddWarning("Привязка отозвана вне Terraform",
				"Привязка "+id+" существует у края со статусом REVOKED: строка сохранена ради "+
					"аудиторского следа, но доступа она больше не даёт. Провайдер убирает её из "+
					"состояния, поэтому следующий план предложит выдать право заново — новой "+
					"привязкой. Отозванная строка останется у края: это её назначение.")
			resp.State.RemoveResource(ctx)
			return
		}
		keepExpiresSpelling(wantExpires, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение привязки прав не удалось", err.Error())
		return
	}

	verdict, lerr := r.confirmAbsence(ctx, id, state.ScopeID.ValueString())
	switch verdict {
	case client.VerdictGone:
		resp.State.RemoveResource(ctx)
	case client.VerdictPresent:
		resp.Diagnostics.AddError("Привязка видна в списке, но не читается",
			"Список выдач якоря "+state.ScopeID.ValueString()+" перечисляет "+id+
				", а чтение по идентификатору отвечает «не найдено». Повторите позже; если "+
				"держится — это расхождение на стороне края, а не состояния Terraform.")
	case client.VerdictDenied:
		resp.Diagnostics.AddError("Доступ к выдачам якоря утрачен",
			"Привязка "+id+" не читается, и список выдач якоря "+state.ScopeID.ValueString()+
				" тоже отвечает отказом. Это событие ПРАВ, а не удаление привязки: продолжать "+
				"нельзя, иначе план предложит выдать заново то, что цело.")
	default:
		d := "Привязка " + id + " не найдена, и подтвердить отсутствие нечем: у якоря " +
			state.ScopeID.ValueString() + " не видно ни одной выдачи. Различить «удалена» и " +
			"«доступ отозван» край не позволяет — ответы совпадают дословно.\n\nЕсли привязка " +
			"действительно удалена вне Terraform, уберите её из состояния:\n" +
			"  terraform state rm <адрес ресурса>"
		if lerr != nil {
			d += "\n\nПодробности: " + lerr.Error()
		}
		resp.Diagnostics.AddError("Отсутствие не подтверждено", d)
	}
}

// confirmAbsence устанавливает, что означает «привязки нет по идентификатору».
//
// Общий absenceDiagnostics здесь НЕ применим, и это проверено по контракту, а не
// предположено: он сужает контрольный запрос фильтром `name="…"` и передаёт область
// параметром `projectId`/`accountId`, — а у привязки нет имени вовсе, её списочный запрос
// не принимает ни того параметра, ни другого, и грамматика его фильтра закрыта ключами
// subject/role/scope/scopeId, на неизвестном ключе отвечая отказом. То есть общий помощник
// не «работал бы хуже», он отвечал бы отказом на КАЖДОМ подтверждении, и всякое настоящее
// удаление вставало бы остановкой.
//
// Дисциплина сохранена дословно, меняется только вопрос, которым спрашивают:
//
//  1. привязки якоря перечисляются страницами; наш идентификатор найден — привязка жива
//     (первый ответ был окном прав);
//  2. не найден, но выдачи у якоря есть ⇒ пообъектная выдача работает ⇒ наша действительно
//     удалена;
//  3. у якоря не видно НИ ОДНОЙ выдачи ⇒ различить «удалена» и «права ушли» нечем ⇒
//     остановка, решение за оператором;
//  4. обход не уложился в отведённые страницы ⇒ тоже остановка: неполный обход не
//     доказывает отсутствия.
//
// Отозванные строки в обход ВКЛЮЧЕНЫ: они существуют, и «наш идентификатор среди них»
// означает, что привязка у края есть, — то самое расхождение с чтением, о котором
// вызывающему сообщают отдельно, а не удаление.
func (r *accessBindingResource) confirmAbsence(ctx context.Context, id, scopeID string) (client.Verdict, error) {
	// Страница взята потолком контракта, чтобы обход был как можно короче; предел страниц
	// существует потому, что бесконечный обход — это не подтверждение, а зависание.
	const (
		pageSize = 1000
		maxPages = 20
	)
	// Якорь в состоянии пуст ровно в одном случае — импорт по идентификатору, которого у
	// края нет: до первого удачного чтения о привязке не известно ничего, кроме строки,
	// которую назвал оператор. Спрашивать список «выдач якоря » бессмысленно, и ответ на
	// такой вопрос выглядел бы находкой; поэтому случай назван прямо.
	if scopeID == "" {
		return client.VerdictAmbiguous, fmt.Errorf(
			"привязка %s не читается, а якорь, по которому её искать, в состоянии не записан — "+
				"так выглядит импорт идентификатора, которого у края нет либо который закрыт "+
				"правами; проверьте строку идентификатора", id)
	}
	seen, token := 0, ""
	for i := 0; i < maxPages; i++ {
		q := url.Values{}
		q.Set("filter", fmt.Sprintf("scopeId=%q", scopeID))
		q.Set("includeRevoked", "true")
		q.Set("pageSize", strconv.Itoa(pageSize))
		if token != "" {
			q.Set("pageToken", token)
		}
		resp, err := r.c.Do(ctx, http.MethodGet, accessBindingsPath+"?"+q.Encode(), nil, nil)
		if err != nil {
			return client.VerdictAmbiguous, err
		}
		switch out := client.Classify(resp); out.Kind {
		case client.OutcomeOK:
		case client.OutcomeDenied, client.OutcomeUnauthenticated:
			return client.VerdictDenied, nil
		default:
			return client.VerdictAmbiguous,
				fmt.Errorf("список выдач якоря %s: %s", scopeID, out.Message)
		}

		var page struct {
			AccessBindings []struct {
				ID string `json:"id"`
			} `json:"accessBindings"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return client.VerdictAmbiguous, fmt.Errorf("разбор списка выдач: %w", err)
		}
		for _, b := range page.AccessBindings {
			seen++
			if b.ID == id {
				return client.VerdictPresent, nil
			}
		}
		token = page.NextPageToken
		if token == "" {
			if seen == 0 {
				return client.VerdictAmbiguous, nil
			}
			return client.VerdictGone, nil
		}
	}
	return client.VerdictAmbiguous,
		fmt.Errorf("обход выдач якоря %s не завершён за %d страниц по %d — "+
			"отсутствие по неполному обходу не устанавливается", scopeID, maxPages, pageSize)
}

// Update меняет ровно то, что край принимает в маске изменения: защиту от удаления и
// метки. Остальные поля до сюда не доходят — они помечены пересоздающими.
func (r *accessBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state accessBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	body := &iamv1.UpdateAccessBindingRequest{AccessBindingId: id}
	var paths []string
	if !plan.DeletionProtection.Equal(state.DeletionProtection) {
		body.DeletionProtection = plan.DeletionProtection.ValueBool()
		paths = append(paths, "deletion_protection")
	}
	if !plan.Labels.Equal(state.Labels) {
		body.Labels = mapFromTF(ctx, plan.Labels)
		paths = append(paths, "labels")
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё
	// изменяемое», а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе в
	// состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch, accessBindingsPath+"/"+id, body); err != nil {
			resp.Diagnostics.AddError("Изменение привязки прав не завершилось", err.Error())
			return
		}
	}

	plan.ID = state.ID
	raw, err := readByID(ctx, r.c, accessBindingsPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError("Привязка изменена, но не прочитана обратно", err.Error())
		return
	}
	wantExpires := plan.ExpiresAt
	if err := applyAccessBinding(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepExpiresSpelling(wantExpires, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete зовёт ЖЁСТКОЕ удаление (`DELETE …/{id}`), а не мягкий отзыв (`…:revoke`).
//
// Чем они различаются у края: удаление сносит строку — после него чтение по
// идентификатору отвечает «не найдено»; отзыв строку СОХРАНЯЕТ, переводя её в терминальный
// REVOKED и проставляя, кто и когда отозвал, ради аудиторского следа. Доступ снимается
// ОДИНАКОВО обоими — тот же набор выданных кортежей снимается в той же транзакции записи,
// — поэтому выбор здесь не про безопасность.
//
// Он про то, что значит «ресурса нет». Terraform управляет соответствием настройки и
// действительности по АДРЕСУ ресурса, и `destroy` обязан сделать этот адрес
// несуществующим. Мягкий отзыв оставил бы объект по тому же идентификатору: провайдер
// отчитался бы об удалении, а край продолжал бы отдавать привязку по её адресу. Отсюда три
// следствия, каждое наблюдаемо:
//
//   - каждый цикл «создать — уничтожить» оставлял бы отозванную строку, о которой Terraform
//     больше не знает, и убрать её было бы некому;
//   - импорт такого идентификатора вернул бы в состояние мёртвую выдачу, неотличимую по
//     форме от живой;
//   - подтверждение отсутствия (confirmAbsence) находило бы наш идентификатор в списке и
//     отвечало «жива» — то есть провайдер сам себе доказывал бы, что уничтоженного не
//     уничтожали.
//
// Мягкий отзыв — отдельное НАМЕРЕНИЕ («снять доступ, но сохранить след»), и у Terraform
// нет глагола между «есть» и «нет», которым его выразить. Кому нужен след — зовёт
// `:revoke` сам; провайдер увидит REVOKED при следующем чтении, уберёт привязку из
// состояния и предложит выдать право заново (см. Read).
func (r *accessBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accessBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := awaitMutation(ctx, r.c, http.MethodDelete,
		accessBindingsPath+"/"+state.ID.ValueString(), nil)
	if err == nil {
		return
	}
	// Отказ края передаётся дословно — он называет предмет. Подсказка добавляется к нему,
	// а не вместо него: самая частая причина здесь одна и она поправима одной правкой.
	resp.Diagnostics.AddError("Удаление привязки прав не завершилось",
		err.Error()+"\n\nЧаще всего мешает deletion_protection: пока флаг взведён, край "+
			"отвергает удаление синхронно. Снимите его отдельным apply "+
			"(deletion_protection = false), затем уничтожайте. Тот же флаг мешает и ЗАМЕНЕ "+
			"привязки — замена начинается с удаления.")
}

func (r *accessBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "привязка прав", "acb", req, resp)
}
