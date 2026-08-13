// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Плоский ресурс — общая механика для ресурсов Kachō, устроенных ОДИНАКОВО.
//
// Одинаковы у них: асинхронная мутация с ожиданием операции, идентификатор из метаданных
// ПОСЛЕ проверки отказа, обратное чтение, подтверждение отсутствия отдельным запросом,
// маска изменения из разницы плана и состояния. Повторить это тринадцать раз значит
// завести тринадцать мест, где однажды забудут проверку отказа операции — ту самую, что
// отличает настоящий идентификатор от фантомного.
//
// Специфика ресурса выражается ДЕКЛАРАТИВНО — набором полей. Поле описывает и схему, и
// перенос в запрос, и разбор ответа: три места, которые иначе разъезжаются молча.
//
// Что каркас НЕ обслуживает: ресурсы с собственными глаголами края (состав группы целей),
// с вложенными объектами и с секретом. У них своя механика, и загонять её сюда значило бы
// сделать каркас условным — то есть перестать понимать, что он делает.
type flatSpec struct {
	tfName  string // имя ресурса в конфигурации
	human   string // как называть в тексте отказа
	pathCol string // путь коллекции у края
	idField string // поле идентификатора в метаданных операции
	prefix  string // префикс идентификатора из каталога платформы
	descr   string // описание ресурса для документации

	// scopeParam / scopeAttr — чем подтверждается отсутствие: имя параметра списочного
	// запроса и атрибут ресурса, из которого берётся его значение.
	scopeParam string
	scopeAttr  string

	// deleteHint — что мешает удалению. Говорится ЗАРАНЕЕ: иначе причина выясняется
	// перебором.
	deleteHint string

	// kindAttr / kindArms — выбор ВЕТВИ oneof, которую плоским значением задать
	// нельзя: ветвь не несёт полей, поэтому «значение» у неё отсутствует by
	// construction, а выбор — есть, и он меняет поведение ресурса.
	//
	// kindAttr — имя атрибута в конфигурации (обычная обязательная строка, объявляется
	// в fields как все прочие); kindArms — значение этого атрибута → имя поля-ветви в
	// запросе создания и пустое сообщение этой ветви. Обратное направление (ответ края →
	// значение атрибута) выводится из того, какая ветвь пришла в ответе: иначе
	// перечитывание молча стирало бы выбор и давало вечное расхождение плана.
	//
	// Неизвестное значение — ОТКАЗ с перечнем допустимых, а не тихое «ветвь не выбрана»:
	// тихий пропуск отдал бы краю запрос без ветви и превратил опечатку в отказ края,
	// который вызывающий прочитал бы как дефект продукта.
	kindAttr string
	kindArms map[string]func() (string, proto.Message)

	newCreate func() proto.Message
	newUpdate func() proto.Message
	// updateIDField — имя поля идентификатора в запросе изменения (у каждого своё).
	updateIDField string

	fields []fieldSpec
}

type fieldKind int

const (
	fString fieldKind = iota
	fBool
	fInt64
	fStringList
	fStringMap
)

// fieldSpec — одно поле ресурса.
//
// Имя ОДНО и то же для схемы Terraform, поля контракта и ключа ответа: `snake_case` здесь,
// `lowerCamel` на проводе. Держать три разных имени значило бы завести три места для
// опечатки, из которых компилятор ловит одно.
type fieldSpec struct {
	name      string
	kind      fieldKind
	required  bool
	computed  bool // только чтение: в запрос не уходит
	immutable bool // задаётся при создании, меняется только пересозданием
	doc       string

	// updateOnly — поля НЕТ в запросе создания, оно есть только в запросе изменения.
	//
	// Так бывает, когда край задумал значение как настройку уже существующего ресурса
	// (видимость репозиториев по умолчанию у реестра). Тогда создание идёт в два шага:
	// создать, потом донастроить. Пропустить поле молча нельзя — вызывающий задал его и
	// вправе получить применённым; отправить в создание тоже нельзя — контракта нет.
	updateOnly bool

	// sendEmpty — отправлять поле, даже когда оно пусто. По умолчанию пустое не
	// отправляется: край отличает «не задано» от «задано пустым», и слать пустое вместо
	// отсутствия значило бы стирать значение при каждом создании.
	sendEmpty bool
}

type flatResource struct {
	spec flatSpec
	c    *client.Client
}

// newFlatResource — конструктор ресурса по его описанию.
func newFlatResource(spec flatSpec) func() resource.Resource {
	return func() resource.Resource { return &flatResource{spec: spec} }
}

func (r *flatResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = r.spec.tfName
}

func (r *flatResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *flatResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true,
			MarkdownDescription: "Неизменяемый идентификатор ресурса.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
	}
	for _, f := range r.spec.fields {
		attrs[f.name] = f.attribute()
	}
	resp.Schema = schema.Schema{MarkdownDescription: r.spec.descr, Attributes: attrs}
}

func (f fieldSpec) attribute() schema.Attribute {
	opt := !f.required && !f.computed
	// Обязательное НЕ бывает вычисляемым: значение задаёт пользователь, и «край подставит»
	// здесь ложь. Фреймворк это ловит на загрузке схемы, но ловит уже у пользователя —
	// поэтому условие стоит здесь, а не в комментарии.
	comp := !f.required
	// Вычисляемое НЕ помечается пересоздающим: значение приходит от края, план видит его
	// неизвестным, и «изменение требует замены» приняло бы неизвестное за другое значение —
	// ресурс пересоздавался бы на каждой правке чего угодно.
	// Неизменяемое поле ОБЯЗАНО брать значение из состояния.
	//
	// Вычисляемое поле фреймворк планирует НЕИЗВЕСТНЫМ, когда его нет в настройке, — а
	// «изменение требует замены» видит неизвестное как другое значение. Вместе это
	// пересоздавало ресурс на правке чего угодно: том с размером блока 4096 в состоянии
	// уходил в замену от смены метки. Значение неизменяемого поля измениться не может по
	// определению, поэтому взять его из состояния и верно, и безопасно.
	strMods := []planmodifier.String{}
	if f.immutable && !f.computed {
		strMods = append(strMods,
			stringplanmodifier.UseStateForUnknown(),
			stringplanmodifier.RequiresReplace())
	}
	switch f.kind {
	case fBool:
		a := schema.BoolAttribute{Required: f.required, Optional: opt, Computed: comp,
			MarkdownDescription: f.doc}
		if f.immutable && !f.computed {
			a.PlanModifiers = []planmodifier.Bool{
				boolplanmodifier.UseStateForUnknown(), boolRequiresReplace{}}
		}
		return a
	case fInt64:
		a := schema.Int64Attribute{Required: f.required, Optional: opt, Computed: comp,
			MarkdownDescription: f.doc}
		if f.immutable && !f.computed {
			a.PlanModifiers = []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(), int64RequiresReplace{}}
		}
		return a
	case fStringList:
		a := schema.ListAttribute{Required: f.required, Optional: opt, Computed: comp,
			ElementType: types.StringType, MarkdownDescription: f.doc}
		if f.immutable && !f.computed {
			a.PlanModifiers = []planmodifier.List{
				listplanmodifier.UseStateForUnknown(), listRequiresReplace{}}
		}
		return a
	case fStringMap:
		return schema.MapAttribute{Required: f.required, Optional: opt, Computed: comp,
			ElementType: types.StringType, MarkdownDescription: f.doc}
	default:
		return schema.StringAttribute{Required: f.required, Optional: opt, Computed: comp,
			MarkdownDescription: f.doc, PlanModifiers: strMods}
	}
}

// ---- перенос значений ---------------------------------------------------------------

// setProtoField кладёт значение в поле контракта ПО ИМЕНИ.
//
// Имя берётся из описания поля, а не пишется вторым списком: расхождение имён — самая
// дешёвая ошибка в такой механике и самая тихая, потому что край молча отбрасывает
// неизвестные ключи. Отсутствие поля в контракте здесь — ОТКАЗ, а не пропуск.
func setProtoField(m proto.Message, name string, v any) error {
	fd := m.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return fmt.Errorf("в контракте %s нет поля %q — описание ресурса разошлось с контрактом",
			m.ProtoReflect().Descriptor().FullName(), name)
	}
	refl := m.ProtoReflect()
	switch val := v.(type) {
	case string:
		// Перечисление задаётся ИМЕНЕМ варианта, а не числом: число в конфигурации
		// нечитаемо и молча меняет смысл при вставке варианта в контракт. Неизвестное имя
		// здесь — отказ, а не ноль: ноль означал бы «не задано», и опечатка прошла бы как
		// умолчание.
		if fd.Kind() == protoreflect.EnumKind {
			ev := fd.Enum().Values().ByName(protoreflect.Name(val))
			if ev == nil {
				var allowed []string
				vs := fd.Enum().Values()
				for i := 0; i < vs.Len(); i++ {
					allowed = append(allowed, string(vs.Get(i).Name()))
				}
				return fmt.Errorf("поле %q: значение %q не входит в перечисление; допустимы: %s",
					name, val, strings.Join(allowed, ", "))
			}
			refl.Set(fd, protoreflect.ValueOfEnum(ev.Number()))
			return nil
		}
		refl.Set(fd, protoreflect.ValueOfString(val))
	case bool:
		refl.Set(fd, protoreflect.ValueOfBool(val))
	case int64:
		if fd.Kind() == protoreflect.Int32Kind {
			refl.Set(fd, protoreflect.ValueOfInt32(int32(val))) // #nosec G115 -- диапазон закреплён контрактом
			return nil
		}
		refl.Set(fd, protoreflect.ValueOfInt64(val))
	case []string:
		l := refl.Mutable(fd).List()
		for _, s := range val {
			l.Append(protoreflect.ValueOfString(s))
		}
	case map[string]string:
		mp := refl.Mutable(fd).Map()
		for k, s := range val {
			mp.Set(protoreflect.ValueOfString(k).MapKey(), protoreflect.ValueOfString(s))
		}
	default:
		return fmt.Errorf("поле %q: неподдерживаемый вид значения %T", name, v)
	}
	return nil
}

// valueOf читает значение поля из плана. Второе значение — «задано ли»: пустое и
// неизвестное в запрос не уходят.
func (r *flatResource) valueOf(ctx context.Context, get getter, f fieldSpec) (any, bool, error) {
	p := path.Root(f.name)
	switch f.kind {
	case fBool:
		var v types.Bool
		if d := get.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsNull() || v.IsUnknown() {
			return nil, false, nil
		}
		return v.ValueBool(), true, nil
	case fInt64:
		var v types.Int64
		if d := get.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsNull() || v.IsUnknown() {
			return nil, false, nil
		}
		return v.ValueInt64(), true, nil
	case fStringList:
		var v types.List
		if d := get.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		out := stringsFromTF(ctx, v)
		return out, len(out) > 0 || f.sendEmpty, nil
	case fStringMap:
		var v types.Map
		if d := get.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		out := mapFromTF(ctx, v)
		return out, len(out) > 0 || f.sendEmpty, nil
	default:
		var v types.String
		if d := get.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsNull() || v.IsUnknown() {
			return nil, false, nil
		}
		return v.ValueString(), v.ValueString() != "" || f.sendEmpty, nil
	}
}

// getter — общий вид плана, состояния и настройки. Три разных типа фреймворка с одним
// методом; интерфейс избавляет от трёх копий каждой функции.
type getter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diagList
}

// ---- разбор ответа края --------------------------------------------------------------

// applyWire раскладывает ответ края по полям ресурса.
//
// Ключ ответа — lowerCamel от имени поля. Отсутствующий в ответе ключ даёт НУЛЕВОЕ
// значение соответствующего вида, а не пропуск: пропуск оставил бы в состоянии
// неизвестность плана, и Terraform отверг бы результат целиком.
func (r *flatResource) applyWire(ctx context.Context, st setter, raw []byte) error {
	var w map[string]any
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	if id, ok := w["id"].(string); ok {
		if d := st.SetAttribute(ctx, path.Root("id"), types.StringValue(id)); d.HasError() {
			return fmt.Errorf("id: %v", d.Errors())
		}
	}
	for _, f := range r.spec.fields {
		v := w[lowerCamel(f.name)]
		if r.spec.kindAttr != "" && f.name == r.spec.kindAttr {
			// Атрибута с таким именем в ответе нет и быть не может: край отдаёт ВЕТВЬ.
			// Значение выводится из того, какая ветвь пришла; ни одной — оставляем
			// пустым, и это отдельный исход, а не «первая по списку».
			v = nil
			for name, armFn := range r.spec.kindArms {
				field, _ := armFn()
				if _, present := w[lowerCamel(field)]; present {
					v = name
					break
				}
			}
		}
		var val attr.Value
		switch f.kind {
		case fBool:
			b, _ := v.(bool)
			val = types.BoolValue(b)
		case fInt64:
			val = types.Int64Value(numOf(v))
		case fStringList:
			var ss []string
			if list, ok := v.([]any); ok {
				for _, e := range list {
					if s, ok := e.(string); ok {
						ss = append(ss, s)
					}
				}
			}
			val = listFromStrings(ctx, ss)
		case fStringMap:
			mm := map[string]string{}
			if obj, ok := v.(map[string]any); ok {
				for k, e := range obj {
					if s, ok := e.(string); ok {
						mm[k] = s
					}
				}
			}
			val = mapToTF(ctx, mm)
		default:
			s, _ := v.(string)
			val = types.StringValue(s)
		}
		if d := st.SetAttribute(ctx, path.Root(f.name), val); d.HasError() {
			return fmt.Errorf("%s: %v", f.name, d.Errors())
		}
	}
	return nil
}

// lowerCamel — snake_case в форму провода. Первое слово со строчной, остальные с
// прописной: `ipv4_cidr_primary` → `ipv4CidrPrimary`.
func lowerCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// ---- CRUD -----------------------------------------------------------------------------

func (r *flatResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	body := r.spec.newCreate()
	for _, f := range r.spec.fields {
		if f.computed || f.updateOnly || (r.spec.kindAttr != "" && f.name == r.spec.kindAttr) {
			continue
		}
		v, ok, err := r.valueOf(ctx, planGetter{req.Plan}, f)
		if err != nil {
			resp.Diagnostics.AddError("Значение поля не прочитано", err.Error())
			return
		}
		if !ok {
			continue
		}
		if err := setProtoField(body, f.name, v); err != nil {
			resp.Diagnostics.AddError("Описание ресурса разошлось с контрактом", err.Error())
			return
		}
	}
	if r.spec.kindAttr != "" {
		var kind types.String
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(r.spec.kindAttr), &kind)...)
		if resp.Diagnostics.HasError() {
			return
		}
		arm, ok := r.spec.kindArms[kind.ValueString()]
		if !ok {
			resp.Diagnostics.AddError("Вид ресурса не распознан",
				fmt.Sprintf("%s = %q; допустимы: %s",
					r.spec.kindAttr, kind.ValueString(), strings.Join(sortedKeys(r.spec.kindArms), ", ")))
			return
		}
		field, msg := arm()
		if err := setProtoField2(body, field, msg); err != nil {
			resp.Diagnostics.AddError("Описание ресурса разошлось с контрактом", err.Error())
			return
		}
	}

	var scope types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(r.spec.scopeAttr), &scope)...)
	var name types.String
	if r.hasField("name") {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := awaitCreate(ctx, r.c, r.spec.pathCol, r.spec.idField, r.spec.tfName,
		scope.ValueString()+"/"+name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание не завершилось: "+r.spec.human, err.Error())
		return
	}

	// Идентификатор пишется ДО обратного чтения: сорвавшееся чтение иначе оставило бы
	// созданный ресурс вне управления навсегда.
	resp.State.Raw = req.Plan.Raw
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(id))...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, r.spec.pathCol, id, true)
	if err != nil {
		resp.Diagnostics.AddError(r.spec.human+" создан, но не прочитан обратно", err.Error())
		return
	}
	if err := r.applyWire(ctx, stateSetter{&resp.State}, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}

	// Второй шаг создания: поля, которых нет в контракте создания, досылаются изменением.
	// Иначе заданное вызывающим значение молча не применилось бы, а состояние показало бы
	// умолчание края — то есть провайдер соврал бы об исходе.
	if raw2, err := r.applyUpdateOnly(ctx, planGetter{req.Plan}, id); err != nil {
		resp.Diagnostics.AddError("Ресурс создан, но донастройка не завершилась", err.Error())
		return
	} else if raw2 != nil {
		if err := r.applyWire(ctx, stateSetter{&resp.State}, raw2); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		}
	}
}

// applyUpdateOnly досылает поля, отсутствующие в контракте создания.
//
// Возвращает свежее чтение, если что-то отправлялось, и nil, если досылать было нечего.
func (r *flatResource) applyUpdateOnly(ctx context.Context, from getter, id string) ([]byte, error) {
	body := r.spec.newUpdate()
	if err := setProtoField(body, r.spec.updateIDField, id); err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range r.spec.fields {
		if !f.updateOnly {
			continue
		}
		v, ok, err := r.valueOf(ctx, from, f)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := setProtoField(body, f.name, v); err != nil {
			return nil, err
		}
		paths = append(paths, f.name)
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if err := setProtoField2(body, "update_mask", &fieldmaskpb.FieldMask{Paths: paths}); err != nil {
		return nil, err
	}
	if err := awaitMutation(ctx, r.c, http.MethodPatch, r.spec.pathCol+"/"+id, body); err != nil {
		return nil, err
	}
	return readByID(ctx, r.c, r.spec.pathCol, id, false)
}

func (r *flatResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id, scope, name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(r.spec.scopeAttr), &scope)...)
	if r.hasField("name") {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, r.spec.pathCol, id.ValueString(), false)
	if err == nil {
		if err := r.applyWire(ctx, stateSetter{&resp.State}, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		}
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение не удалось: "+r.spec.human, err.Error())
		return
	}
	remove, title, detail := absenceDiagnostics(ctx, r.c, r.spec.pathCol, r.spec.scopeParam,
		r.spec.human, id.ValueString(), scope.ValueString(), name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *flatResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.spec.newUpdate()
	if err := setProtoField(body, r.spec.updateIDField, id.ValueString()); err != nil {
		resp.Diagnostics.AddError("Описание ресурса разошлось с контрактом", err.Error())
		return
	}
	var paths []string

	for _, f := range r.spec.fields {
		if f.computed || f.immutable {
			continue
		}
		planV, planOK, err := r.valueOf(ctx, planGetter{req.Plan}, f)
		if err != nil {
			resp.Diagnostics.AddError("Значение поля не прочитано", err.Error())
			return
		}
		stateV, stateOK, err := r.valueOf(ctx, stateGetter{req.State}, f)
		if err != nil {
			resp.Diagnostics.AddError("Значение поля не прочитано", err.Error())
			return
		}
		if planOK == stateOK && sameValue(planV, stateV) {
			continue
		}
		if planOK {
			if err := setProtoField(body, f.name, planV); err != nil {
				resp.Diagnostics.AddError("Описание ресурса разошлось с контрактом", err.Error())
				return
			}
		}
		paths = append(paths, f.name)
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё
	// изменяемое», а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе в
	// состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		if err := setProtoField2(body, "update_mask", &fieldmaskpb.FieldMask{Paths: paths}); err != nil {
			resp.Diagnostics.AddError("Описание ресурса разошлось с контрактом", err.Error())
			return
		}
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			r.spec.pathCol+"/"+id.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение не завершилось: "+r.spec.human, err.Error())
			return
		}
	}

	resp.State.Raw = req.Plan.Raw
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	raw, err := readByID(ctx, r.c, r.spec.pathCol, id.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError(r.spec.human+" изменён, но не прочитан обратно", err.Error())
		return
	}
	if err := r.applyWire(ctx, stateSetter{&resp.State}, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
	}
}

func (r *flatResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		r.spec.pathCol+"/"+id.ValueString(), nil); err != nil {
		detail := err.Error()
		if r.spec.deleteHint != "" {
			detail += "\n\n" + r.spec.deleteHint
		}
		resp.Diagnostics.AddError("Удаление не завершилось: "+r.spec.human, detail)
	}
}

func (r *flatResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, r.spec.human, r.spec.prefix, req, resp)
}

func (r *flatResource) hasField(name string) bool {
	for _, f := range r.spec.fields {
		if f.name == name {
			return true
		}
	}
	return false
}

// setProtoField2 — то же, что setProtoField, для значений-сообщений (маска изменения).
func setProtoField2(m proto.Message, name string, v proto.Message) error {
	fd := m.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return fmt.Errorf("в контракте %s нет поля %q",
			m.ProtoReflect().Descriptor().FullName(), name)
	}
	m.ProtoReflect().Set(fd, protoreflect.ValueOfMessage(v.ProtoReflect()))
	return nil
}

func sameValue(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case int64:
		y, ok := b.(int64)
		return ok && x == y
	case []string:
		y, ok := b.([]string)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	case map[string]string:
		y, ok := b.(map[string]string)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if y[k] != v {
				return false
			}
		}
		return true
	}
	return false
}

// sortedKeys — детерминированный перечень допустимых значений для текста отказа.
// Порядок обхода map в Go случаен, и без сортировки один и тот же отказ печатался бы
// каждый раз иначе — расхождение, которое читается как изменение поведения.
func sortedKeys(m map[string]func() (string, proto.Message)) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
