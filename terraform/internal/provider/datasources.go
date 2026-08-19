// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Источники данных — чтение существующего БЕЗ взятия под управление.
//
// Каркас источника отличается от каркаса ресурса не только тем, что у него один метод
// вместо пяти. Отличия, из которых сделан этот файл:
//
//  1. У источника нет состояния между применениями: сравнивать не с чем, поэтому ни
//     плана, ни маски изменения, ни подтверждения отсутствия ради снятия из состояния.
//  2. Зато у него есть ВХОД от пользователя (идентификатор, имя, фильтры), и вход обязан
//     быть Required, а не Computed: значение задаёт вызывающий, и «край подставит» здесь
//     было бы ложью, которую фреймворк к тому же отвергает целиком.
//  3. Ненайденное — ОТКАЗ, а не пустой результат. Пустой результат уезжает дальше по
//     конфигурации пустой строкой и всплывает в чужом месте: подсеть создаётся с
//     `zone_id = ""`, край отвечает про негодную зону, и виноватым выглядит он.
//
// Общая механика вынесена сюда целиком. Источников десять; десять копий разбора ответа и
// обхода курсора разошлись бы молча — и разошлись бы именно там, где расхождение не видно:
// на исправном входе все десять отвечают «прочитано».
//
// Специфика источника выражается ДЕКЛАРАТИВНО таблицей полей. Одна и та же таблица
// обслуживает и одиночный источник, и списочный: иначе `kacho_geo_zone` и `kacho_geo_zones`
// стали бы двумя описаниями одного предмета, и первое же новое поле контракта попало бы в
// одно из них.

const (
	// dsPageSize — размер страницы при обходе курсора.
	//
	// Предел края — 1000, и значение выбрано ниже него намеренно: списки, которые проверяют
	// права пообъектно (сети, подсети), считают партии по сотне на страницу, и просить у них
	// максимум значило бы класть бюджет запроса в одну корзину. Каталоги (регионы, зоны, типы
	// дисков и машин) пообъектной проверки не делают, но общий размер важнее выигрыша в один
	// запрос на каталоге из десяти строк.
	dsPageSize = 500

	// dsMaxPages — потолок обхода курсора.
	//
	// Существует не ради экономии, а ради того, чтобы «обход не завершился» никогда не
	// выглядело как «больше ничего нет». Достигнутый потолок — ОТКАЗ с числом, а не молчаливое
	// усечение: усечение, поданное как полный ответ, — ровно тот класс, где детектор всегда
	// врёт, потому что сравнивает длину с собственной обрезкой.
	dsMaxPages = 200
)

// ---- описание поля --------------------------------------------------------------------

type dsKind int

const (
	dsString dsKind = iota
	dsBool
	dsInt64
	dsStringList
	dsStringMap
	dsObject
)

// dsField — одно поле, отдаваемое источником.
//
// Имя ОДНО и то же для атрибута источника и для ключа ответа края: `snake_case` здесь,
// `lowerCamel` на проводе (`lowerCamel` из flat.go). Второе имя завело бы второе место для
// опечатки, а край молча отбрасывает неизвестные ключи — то есть опечатка не дала бы ни
// отказа, ни предупреждения, только пустое поле в состоянии.
type dsField struct {
	name string
	kind dsKind
	doc  string

	// nested — состав вложенного объекта; заполняется только у dsObject.
	nested []dsField
}

// attribute — атрибут схемы источника.
//
// required=true — это ВХОД: значение приходит от пользователя, и Computed при этом выключен.
// Обязательное и вычисляемое одновременно — запрещённая пара: фреймворк объявляет это в
// документации КАЖДОГО вида атрибута («Required and Computed cannot both be true»), а
// наблюдаемо ломается позже, у пользователя, на плане.
//
// Держится эта пара ОДНОЙ строкой ниже, а не проверкой, и это измерено, а не предположено:
// `Schema.ValidateImplementation` и `Schema.Validate` на схеме, где вход помечен и
// обязательным, и вычисляемым, остаются ЗЕЛЁНЫМИ (проверено инъекцией: `computed := true`
// не покраснило ни одну из них). Значит проба «схема валидна» этого свойства НЕ проверяет, и
// считать её защитой нельзя — свойство здесь по построению: вычисляемое ровно то, что не
// обязательное.
func (f dsField) attribute(required bool) dsschema.Attribute {
	computed := !required
	switch f.kind {
	case dsBool:
		return dsschema.BoolAttribute{Required: required, Computed: computed, MarkdownDescription: f.doc}
	case dsInt64:
		return dsschema.Int64Attribute{Required: required, Computed: computed, MarkdownDescription: f.doc}
	case dsStringList:
		return dsschema.ListAttribute{Required: required, Computed: computed,
			ElementType: types.StringType, MarkdownDescription: f.doc}
	case dsStringMap:
		return dsschema.MapAttribute{Required: required, Computed: computed,
			ElementType: types.StringType, MarkdownDescription: f.doc}
	case dsObject:
		return dsschema.SingleNestedAttribute{Computed: true, MarkdownDescription: f.doc,
			Attributes: dsAttributes(f.nested, nil)}
	default:
		return dsschema.StringAttribute{Required: required, Computed: computed, MarkdownDescription: f.doc}
	}
}

// dsAttributes собирает атрибуты по таблице. inputs — имена полей, приходящих от
// пользователя; всё остальное вычисляемое.
func dsAttributes(fields []dsField, inputs []string) map[string]dsschema.Attribute {
	isInput := map[string]bool{}
	for _, n := range inputs {
		isInput[n] = true
	}
	out := make(map[string]dsschema.Attribute, len(fields))
	for _, f := range fields {
		out[f.name] = f.attribute(isInput[f.name])
	}
	return out
}

// dsElementAttributes — те же поля внутри элемента СПИСКА, отдаваемого источником.
//
// Здесь Computed стоит на каждом вложенном поле, и это не забытое правило про элементы
// набора. То правило — про набор, который заполняет ПОЛЬЗОВАТЕЛЬ: там подстановка умолчания
// меняет значение элемента, а значением элемент и адресуется. Здесь список целиком
// вычисляемый, пользователь в него не пишет ничего, и фреймворк требует Computed от каждого
// вложенного поля вычисляемого списка.
func dsElementAttributes(fields []dsField) map[string]dsschema.Attribute {
	return dsAttributes(fields, nil)
}

// dsObjectType — тип значения по таблице. Объявлен ОДИН раз и выводится из той же таблицы,
// что и схема: разойдись он с ней хоть одним полем, объект молча не собрался бы, и поле
// исчезло бы из состояния.
func dsObjectType(fields []dsField) types.ObjectType {
	attrs := make(map[string]attr.Type, len(fields))
	for _, f := range fields {
		switch f.kind {
		case dsBool:
			attrs[f.name] = types.BoolType
		case dsInt64:
			attrs[f.name] = types.Int64Type
		case dsStringList:
			attrs[f.name] = types.ListType{ElemType: types.StringType}
		case dsStringMap:
			attrs[f.name] = types.MapType{ElemType: types.StringType}
		case dsObject:
			attrs[f.name] = dsObjectType(f.nested)
		default:
			attrs[f.name] = types.StringType
		}
	}
	return types.ObjectType{AttrTypes: attrs}
}

// dsValue — значение поля из ответа края.
//
// Отсутствующий ключ даёт НУЛЕВОЕ значение своего вида, а не пропуск: пропуск оставил бы
// атрибут неизвестным, а результат источника обязан быть известен целиком.
func dsValue(ctx context.Context, f dsField, v any) (attr.Value, error) {
	switch f.kind {
	case dsBool:
		b, _ := v.(bool)
		return types.BoolValue(b), nil
	case dsInt64:
		// numOf принимает и число, и строку: 64-битные целые protojson пишет СТРОКОЙ
		// (`"memoryMib": "8192"`), а 32-битные — числом. Разбор только числа тихо дал бы
		// здесь ноль.
		return types.Int64Value(numOf(v)), nil
	case dsStringList:
		var ss []string
		if list, ok := v.([]any); ok {
			for _, e := range list {
				if s, ok := e.(string); ok {
					ss = append(ss, s)
				}
			}
		}
		return listFromStrings(ctx, ss), nil
	case dsStringMap:
		mm := map[string]string{}
		if obj, ok := v.(map[string]any); ok {
			for k, e := range obj {
				if s, ok := e.(string); ok {
					mm[k] = s
				}
			}
		}
		return mapToTF(ctx, mm), nil
	case dsObject:
		return dsObjectValue(ctx, f.nested, v)
	default:
		s, _ := v.(string)
		return types.StringValue(s), nil
	}
}

// dsObjectValue — вложенный объект из ответа края.
//
// Край не прислал объект — значение null, а НЕ объект из нулей. Объект из нулей утверждал бы
// «у этого типа машины ноль ядер и ноль памяти»; null говорит «край этого не прислал», и это
// разные сообщения.
func dsObjectValue(ctx context.Context, fields []dsField, v any) (attr.Value, error) {
	t := dsObjectType(fields)
	obj, ok := v.(map[string]any)
	if !ok {
		return types.ObjectNull(t.AttrTypes), nil
	}
	attrs := make(map[string]attr.Value, len(fields))
	for _, f := range fields {
		val, err := dsValue(ctx, f, obj[lowerCamel(f.name)])
		if err != nil {
			return nil, err
		}
		attrs[f.name] = val
	}
	res, diags := types.ObjectValue(t.AttrTypes, attrs)
	if diags.HasError() {
		return nil, fmt.Errorf("сборка вложенного объекта: %v", diags.Errors())
	}
	return res, nil
}

// ---- общая механика вызовов ------------------------------------------------------------

// dataSourceClient — единственная реализация Configure на все источники.
func dataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Внутренняя ошибка провайдера",
			fmt.Sprintf("ожидался *client.Client, получено %T", req.ProviderData))
		return nil
	}
	return c
}

// fetchObject читает один объект по адресу.
//
// На ошибке транспорта исход возвращается НЕ нулевым. Нулевое значение OutcomeKind — это
// OutcomeOK, и вызывающий, забывший проверить ошибку первой, прочитал бы сорванный вызов как
// успех. Тип с осмысленным нулём такую ошибку прощает; здесь она не прощается.
func fetchObject(ctx context.Context, c *client.Client, p string) (map[string]any, client.Outcome, error) {
	resp, err := c.Do(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, client.Outcome{Kind: client.OutcomeMalformed, Code: -1, Message: err.Error()}, err
	}
	out := client.Classify(resp)
	if out.Kind != client.OutcomeOK {
		return nil, out, nil
	}
	var w map[string]any
	if err := json.Unmarshal(resp.Body, &w); err != nil {
		return nil, out, fmt.Errorf("разбор ответа края: %w", err)
	}
	return w, out, nil
}

// walkPages обходит коллекцию курсором и отдаёт объекты по одному.
//
// visit возвращает true, когда обход пора прекратить (искомое найдено). Единственная
// реализация обхода на весь файл: вторая разошлась бы с первой в обращении с курсором, и
// разошлась бы молча — обе возвращают «страница прочитана».
func walkPages(
	ctx context.Context, c *client.Client, collection string, q url.Values, itemsAttr string,
	visit func(map[string]any) bool,
) (client.Outcome, error) {
	itemsKey := lowerCamel(itemsAttr)
	token := ""
	for page := 1; ; page++ {
		if page > dsMaxPages {
			return client.Outcome{Kind: client.OutcomeOK}, fmt.Errorf(
				"обход коллекции %s не завершился за %d страниц по %d записей: край продолжает "+
					"выдавать курсор. Неполный ответ здесь не отдаётся намеренно — усечённый "+
					"список, поданный как полный, тише и опаснее отказа",
				collection, dsMaxPages, dsPageSize)
		}

		qq := url.Values{}
		for k, vs := range q {
			for _, v := range vs {
				qq.Add(k, v)
			}
		}
		qq.Set("pageSize", strconv.Itoa(dsPageSize))
		if token != "" {
			qq.Set("pageToken", token)
		}

		resp, err := c.Do(ctx, http.MethodGet, collection+"?"+qq.Encode(), nil, nil)
		if err != nil {
			return client.Outcome{Kind: client.OutcomeMalformed, Code: -1, Message: err.Error()}, err
		}
		if out := client.Classify(resp); out.Kind != client.OutcomeOK {
			return out, nil
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return client.Outcome{Kind: client.OutcomeOK}, fmt.Errorf("разбор списка %s: %w", collection, err)
		}

		// Пустой список край не присылает вовсе — protojson опускает пустой массив.
		// Отсутствие ключа означает «ноль записей», а не «ответ не тот».
		if arr, ok := raw[itemsKey]; ok {
			var items []map[string]any
			if err := json.Unmarshal(arr, &items); err != nil {
				return client.Outcome{Kind: client.OutcomeOK},
					fmt.Errorf("разбор списка %s: поле %q не является массивом объектов: %w",
						collection, itemsKey, err)
			}
			for _, it := range items {
				if visit(it) {
					return client.Outcome{Kind: client.OutcomeOK}, nil
				}
			}
		}

		next := ""
		if tok, ok := raw["nextPageToken"]; ok {
			_ = json.Unmarshal(tok, &next)
		}
		if next == "" {
			return client.Outcome{Kind: client.OutcomeOK}, nil
		}
		token = next
	}
}

// collectPages — весь список целиком.
func collectPages(
	ctx context.Context, c *client.Client, collection string, q url.Values, itemsAttr string,
) ([]map[string]any, client.Outcome, error) {
	items := []map[string]any{}
	out, err := walkPages(ctx, c, collection, q, itemsAttr, func(m map[string]any) bool {
		items = append(items, m)
		return false
	})
	if err != nil || out.Kind != client.OutcomeOK {
		// Частично собранное НЕ возвращается: половина списка, поданная как список, — это
		// неверный ответ, а не бедный.
		return nil, out, err
	}
	return items, out, nil
}

// applyFields раскладывает объект края по атрибутам состояния.
//
// inputs пропускаются: их значение пришло от пользователя и уже лежит в состоянии (фреймворк
// заполняет состояние источника копией настройки). Переписать их эхом края значило бы
// рисковать расхождением с настройкой — Terraform считает такой результат несогласованным и
// отвергает его целиком. Вместо перезаписи эхо СВЕРЯЕТСЯ, см. checkEcho.
func applyFields(ctx context.Context, st setter, fields []dsField, inputs []string, w map[string]any) error {
	skip := map[string]bool{}
	for _, n := range inputs {
		skip[n] = true
	}
	for _, f := range fields {
		if skip[f.name] {
			continue
		}
		val, err := dsValue(ctx, f, w[lowerCamel(f.name)])
		if err != nil {
			return err
		}
		if d := st.SetAttribute(ctx, path.Root(f.name), val); d.HasError() {
			return fmt.Errorf("%s: %v", f.name, d.Errors())
		}
	}
	return nil
}

// checkEcho сверяет то, что вернул край, с тем, что задал пользователь.
//
// Пустое эхо не утверждает ничего и пропускается; отличающееся — останавливает чтение.
// Молча принять чужой объект под чужим именем нельзя: дальше по конфигурации на него
// сошлются как на запрошенный.
func checkEcho(human, attrName, want string, w map[string]any) error {
	got, _ := w[lowerCamel(attrName)].(string)
	if got == "" || got == want {
		return nil
	}
	return fmt.Errorf("%s: запрошено %s=%q, край вернул %q. Провайдер не выдаёт один объект за "+
		"другой — чтение остановлено", human, attrName, want, got)
}

// ---- фильтры списочных источников -------------------------------------------------------

// dsFilter — фильтр, который ПРИНИМАЕТ списочный запрос края.
//
// Фильтров, которых край не принимает, здесь нет и быть не может: поле, принятое источником и
// не уехавшее в запрос, вернуло бы полный список под видом отфильтрованного — то есть ответ
// на вопрос, которого не задавали.
//
// Имя атрибута и имя параметра запроса — одно имя: `snake_case` здесь, `lowerCamel` в строке
// запроса.
type dsFilter struct {
	name string
	kind dsKind
	doc  string
}

// narrows — сужает ли заданное значение выдачу, и если нет, то почему это отказ.
//
// У края «фильтра нет» выражается нулевым значением поля: пустой строкой, `false`, нулём.
// Значит `open_for_placement = false` и отсутствие поля — ДВА способа сказать одно и то же, а
// выглядит первое как «покажи закрытые». Такого запроса контракт не умеет, и молча вернуть
// полный список означало бы принять поле и не применить его. Поэтому — явный отказ.
func (f dsFilter) narrows(v attr.Value) (ok bool, title, detail string) {
	switch f.kind {
	case dsBool:
		b, isB := v.(types.Bool)
		if !isB || b.ValueBool() {
			return true, "", ""
		}
		return false, "Фильтр " + f.name + " умеет только сужать",
			"Край понимает `" + f.name + " = true` как «оставить только такие» и не умеет " +
				"обратного вопроса: `false` для него означает «не фильтровать». Два способа " +
				"сказать «без фильтра» разошлись бы в чтении конфигурации, поэтому уберите " +
				"поле вместо `false`."
	case dsInt64:
		n, isN := v.(types.Int64)
		if !isN || n.ValueInt64() > 0 {
			return true, "", ""
		}
		return false, "Фильтр " + f.name + " не сужает",
			"Край читает ноль и отрицательное значение как отсутствие фильтра. Уберите поле, " +
				"если фильтровать не нужно, — иначе оно принято и ничего не меняет."
	default:
		s, isS := v.(types.String)
		if !isS || s.ValueString() != "" {
			return true, "", ""
		}
		return false, "Фильтр " + f.name + " не сужает",
			"Пустая строка означает у края «без фильтра». Уберите поле, если фильтровать не " +
				"нужно, — иначе оно принято и ничего не меняет."
	}
}

// queryValue — значение фильтра в строке запроса.
func (f dsFilter) queryValue(v attr.Value) string {
	switch f.kind {
	case dsBool:
		return "true" // сюда доходит только сужающее значение, см. narrows
	case dsInt64:
		n, _ := v.(types.Int64)
		return strconv.FormatInt(n.ValueInt64(), 10)
	default:
		s, _ := v.(types.String)
		return s.ValueString()
	}
}

// readFilter достаёт значение фильтра из настройки.
//
// Второе значение — «задан ли». Неизвестное значение здесь ОТКАЗ, а не «не задан»: чтение
// источника исполняется тогда, когда настройка уже вычислена, и неизвестное на этом месте
// означало бы, что фильтр тихо не применится к уже отданному ответу.
func readFilter(ctx context.Context, cfg getter, f dsFilter) (attr.Value, bool, error) {
	p := path.Root(f.name)
	switch f.kind {
	case dsBool:
		var v types.Bool
		if d := cfg.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsUnknown() {
			return nil, false, fmt.Errorf("значение фильтра %s неизвестно на момент чтения", f.name)
		}
		return v, !v.IsNull(), nil
	case dsInt64:
		var v types.Int64
		if d := cfg.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsUnknown() {
			return nil, false, fmt.Errorf("значение фильтра %s неизвестно на момент чтения", f.name)
		}
		return v, !v.IsNull(), nil
	default:
		var v types.String
		if d := cfg.GetAttribute(ctx, p, &v); d.HasError() {
			return nil, false, fmt.Errorf("%s: %v", f.name, d.Errors())
		}
		if v.IsUnknown() {
			return nil, false, fmt.Errorf("значение фильтра %s неизвестно на момент чтения", f.name)
		}
		return v, !v.IsNull(), nil
	}
}

// configGetter — настройка в общем виде чтения; пара к planGetter/stateGetter из
// flat_access.go. Существует по той же причине: чтобы предикат фильтра писался один раз, а не
// по копии на источник значений.
type configGetter struct{ c tfsdk.Config }

func (g configGetter) GetAttribute(ctx context.Context, p path.Path, target any) diagList {
	return g.c.GetAttribute(ctx, p, target)
}

// ---- справочник: одно описание, два источника -------------------------------------------

// catalogSpec — читаемый справочник края: у него нет создающих глаголов, поэтому ресурсом он
// быть не может, и единственная разумная форма для него — источник данных.
type catalogSpec struct {
	name      string // суффикс имени одиночного источника
	nameMany  string // суффикс имени списочного источника
	human     string // как называть в тексте отказа: «Регион»
	humanMany string // «регионов» — для «в каталоге …»
	pathCol   string // коллекция у края
	itemsAttr string // имя массива: атрибут списочного источника и, в lowerCamel, ключ ответа
	descrOne  string
	descrMany string
	filters   []dsFilter
	fields    []dsField
}

var geoRegionCatalog = catalogSpec{
	name: "_geo_region", nameMany: "_geo_regions",
	human: "Регион", humanMany: "регионов",
	pathCol: "/geo/v1/regions", itemsAttr: "regions",
	descrOne: "Регион — координата размещения, общая для всей платформы. Справочник ведёт " +
		"администратор облака: создать регион арендатор не может, поэтому у него нет ресурса, " +
		"только чтение.",
	descrMany: "Все регионы платформы. Читается целиком, обходом курсора: усечённый список " +
		"здесь не отдаётся, иначе выбор зоны делался бы по неполному каталогу.",
	// Отдельного человекочитаемого имени у региона НЕТ и не будет: его
	// идентификатор назначает администратор облака, и он читаем by construction
	// («ru-central1»). Второе поле того же назначения снято у владельца (#716)
	// вместе с местом, где два написания расходились.
	filters: []dsFilter{{
		name: "open_for_placement", kind: dsBool,
		doc: "Оставить только регионы, открытые для размещения. Обратного вопроса у края нет: " +
			"`false` означает «не фильтровать» и потому отвергается — уберите поле.",
	}},
	fields: []dsField{
		{name: "id", kind: dsString,
			doc: "Идентификатор региона, например `ru-central1`. Это рукописный слаг, а не " +
				"сгенерённая строка, — единственное исключение из общего вида идентификаторов " +
				"платформы, потому что его пишут руками в каждое размещение."},
		{name: "created_at", kind: dsString, doc: "Момент создания по данным края."},
		{name: "country_code", kind: dsString, doc: "Код страны по ISO-3166 alpha-2."},
		{name: "open_for_placement", kind: dsBool,
			doc: "Открыт ли регион для размещения. Это АДМИНИСТРАТИВНАЯ доступность, а не " +
				"обещание ёмкости: создание всё равно может не пройти по вместимости."},
		{name: "open_zone_count_hint", kind: dsInt64,
			doc: "Подсказка: сколько зон региона открыты для размещения. Считается в момент " +
				"чтения и авторитетной величиной НЕ является — источником остаётся сам список " +
				"зон (`kacho_geo_zones` с `open_for_placement = true`). Инвариант на этом " +
				"числе строить нельзя."},
	},
}

var geoZoneCatalog = catalogSpec{
	name: "_geo_zone", nameMany: "_geo_zones",
	human: "Зона", humanMany: "зон",
	pathCol: "/geo/v1/zones", itemsAttr: "zones",
	descrOne: "Зона доступности — часть региона. Справочник ведёт администратор облака; " +
		"арендатор его только читает.",
	descrMany: "Зоны платформы, при желании суженные регионом. Читается целиком, обходом " +
		"курсора.",
	// Человекочитаемого имени у зоны нет — см. каталог регионов выше (#716).
	filters: []dsFilter{
		{name: "region_id", kind: dsString,
			doc: "Оставить только зоны этого региона. Фильтр исполняет край — обхода всех зон " +
				"платформы здесь нет."},
		{name: "open_for_placement", kind: dsBool,
			doc: "Оставить только зоны, открытые для размещения. `false` край читает как " +
				"«не фильтровать» и потому отвергается — уберите поле."},
	},
	fields: []dsField{
		{name: "id", kind: dsString,
			doc: "Идентификатор зоны, например `ru-central1-a`. Рукописный слаг."},
		{name: "region_id", kind: dsString,
			doc: "Регион, которому принадлежит зона. Берите регион ОТСЮДА, а не отрезанием " +
				"суффикса от идентификатора зоны: написание — не источник связи, авторитетный " +
				"ответ даёт только сам каталог."},
		{name: "created_at", kind: dsString, doc: "Момент создания по данным края."},
		{name: "open_for_placement", kind: dsBool,
			doc: "Открыта ли зона для размещения. Административная доступность, не ёмкость."},
		{name: "placement_blocked_reason", kind: dsString,
			doc: "Почему размещение закрыто, одним ответом: `NONE`, `ZONE_DOWN` (выведена сама " +
				"зона) или `REGION_DOWN` (выведен весь регион). Избавляет от второго запроса к " +
				"региону."},
	},
}

var storageDiskTypeCatalog = catalogSpec{
	name: "_storage_disk_type", nameMany: "_storage_disk_types",
	human: "Тип диска", humanMany: "типов диска",
	pathCol: "/storage/v1/diskTypes", itemsAttr: "disk_types",
	descrOne: "Тип диска — класс хранения, на котором заводится том. Каталог ведёт " +
		"администратор облака.",
	descrMany: "Все типы диска. Читается целиком, обходом курсора.",
	fields: []dsField{
		{name: "id", kind: dsString,
			doc: "Идентификатор типа диска, например `network-ssd`. Рукописный слаг."},
		{name: "name", kind: dsString, doc: "Человекочитаемое имя."},
		{name: "description", kind: dsString, doc: "Описание типа диска."},
		{name: "zone_ids", kind: dsStringList,
			doc: "Зоны, в которых тип предлагается. ПУСТОЙ список означает «во всех зонах», а " +
				"не «ни в одной»: край отвергает том только тогда, когда список непуст и зона " +
				"тома в него не входит."},
		{name: "performance_tier", kind: dsString,
			doc: "Класс производительности, например `standard` или `high`."},
	},
}

var computeMachineTypeCatalog = catalogSpec{
	name: "_compute_machine_type", nameMany: "_compute_machine_types",
	human: "Тип машины", humanMany: "типов машины",
	pathCol: "/compute/v1/machineTypes", itemsAttr: "machine_types",
	descrOne: "Тип машины — размер, которым заводится вычислительный экземпляр. Читается по " +
		"идентификатору вида `mt-…`; поиск по человекочитаемому имени делает списочный " +
		"источник `kacho_compute_machine_types` с фильтром `name`.",
	descrMany: "Типы машин каталога, при желании суженные именем, семейством или числом " +
		"ускорителей. Читается целиком, обходом курсора.",
	filters: []dsFilter{
		{name: "name", kind: dsString,
			doc: "Точное человекочитаемое имя типа, например `std-v3-2`. Имя в каталоге " +
				"уникально, поэтому такой фильтр оставляет не больше одной записи."},
		{name: "family", kind: dsString,
			doc: "Семейство: `STANDARD`, `COMPUTE`, `MEMORY` или `GPU`. Значение сверяет край и " +
				"на незнакомом отвечает отказом с перечнем допустимых — эта проверка не " +
				"повторяется здесь, чтобы список допустимых не разошёлся с контрактом."},
		{name: "min_gpus", kind: dsInt64,
			doc: "Оставить типы, у которых ускорителей не меньше указанного. Ноль край читает " +
				"как отсутствие фильтра и потому отвергается — уберите поле."},
	},
	fields: []dsField{
		{name: "id", kind: dsString,
			doc: "Идентификатор типа машины вида `mt-…`. Присваивается краем и неизменяем."},
		{name: "name", kind: dsString,
			doc: "Человекочитаемое имя, например `std-v3-2`. Уникально в каталоге."},
		{name: "description", kind: dsString, doc: "Описание типа машины."},
		{name: "family", kind: dsString,
			doc: "Семейство: `STANDARD`, `COMPUTE`, `MEMORY`, `GPU`."},
		{name: "effective_resources", kind: dsObject,
			doc: "Размер, выведенный краем из записи каталога. Только чтение.",
			nested: []dsField{
				{name: "v_cpu", kind: dsInt64, doc: "Число vCPU."},
				{name: "memory_mib", kind: dsInt64,
					doc: "Память в МиБ — именно в мебибайтах, не в байтах."},
				{name: "gpus", kind: dsInt64, doc: "Число ускорителей; ноль вне семейства `GPU`."},
				{name: "gpu_type", kind: dsString,
					doc: "Модель ускорителя, например `a100-80g`; пусто вне семейства `GPU`."},
			}},
		{name: "available_zones", kind: dsStringList,
			doc: "Зоны, в которых тип доступен к заказу. Подсказка края, снимок на момент чтения."},
		{name: "status", kind: dsString,
			doc: "Состояние в каталоге: `AVAILABLE` — заказывается, `DEPRECATED` — ещё " +
				"работает, но не рекомендуется, `RETIRED` — на создании отвергается."},
		{name: "labels", kind: dsStringMap, doc: "Метки вида ключ-значение."},
		{name: "created_at", kind: dsString, doc: "Момент создания по данным края."},
	},
}

// ---- одиночный справочник ----------------------------------------------------------------

type catalogOneDataSource struct {
	spec catalogSpec
	c    *client.Client
}

func newCatalogOne(spec catalogSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &catalogOneDataSource{spec: spec} }
}

func (d *catalogOneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.spec.name
}

func (d *catalogOneDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = dataSourceClient(req, resp)
}

func (d *catalogOneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		MarkdownDescription: d.spec.descrOne,
		Attributes:          dsAttributes(d.spec.fields, []string{"id"}),
	}
}

func (d *catalogOneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if id.IsNull() || id.IsUnknown() || id.ValueString() == "" {
		resp.Diagnostics.AddError("Идентификатор не задан",
			"Укажите id — "+strings.ToLower(d.spec.human)+" читается по нему.")
		return
	}

	// Экранирование пути: идентификаторы справочника — рукописные слаги, и хотя годный слаг
	// экранирования не требует, негодный не должен превращаться в другой адрес.
	w, out, err := fetchObject(ctx, d.c, d.spec.pathCol+"/"+url.PathEscape(id.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Чтение не удалось: "+d.spec.human, err.Error())
		return
	}
	switch out.Kind {
	case client.OutcomeOK:
	case client.OutcomeNotFound:
		title, detail := d.absence(ctx, id.ValueString())
		resp.Diagnostics.AddError(title, detail)
		return
	case client.OutcomeDenied, client.OutcomeUnauthenticated:
		resp.Diagnostics.AddError("Справочник не читается",
			d.spec.human+" "+id.ValueString()+": "+out.Message+"\n\nЭто событие ПРАВ, а не "+
				"отсутствие записи: право читать справочник есть у любой опознанной личности, "+
				"поэтому отказ здесь означает негодный или истёкший токен.")
		return
	default:
		resp.Diagnostics.AddError("Чтение не удалось: "+d.spec.human, out.Message)
		return
	}

	if err := checkEcho(d.spec.human, "id", id.ValueString(), w); err != nil {
		resp.Diagnostics.AddError("Край вернул не тот объект", err.Error())
		return
	}
	if err := applyFields(ctx, stateSetter{&resp.State}, d.spec.fields, []string{"id"}, w); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
	}
}

// absence — что означает «записи нет по идентификатору».
//
// Один ответ «не найдено» не устанавливает ничего: тем же ответом край отвечает на отказ в
// доступе. Различение здесь своё, а не общее `absenceDiagnostics`: тот спрашивает список
// ПРОЕКТА по ИМЕНИ, а у справочника нет ни проекта, ни фильтра по имени — подставить в него
// пустую область значило бы получить пустой список на каждом подтверждении и остановку на
// ровном месте.
//
// Различение: каталог непуст ⇒ читать его мы вправе ⇒ записи действительно нет, и тогда
// перечень существующих идентификаторов полезнее любого нашего текста (слаг пишут руками,
// и опечатка в нём выглядит ровно как отсутствие). Каталог пуст или недоступен ⇒ сказать
// нечего, и провайдер это признаёт, а не решает за оператора.
func (d *catalogOneDataSource) absence(ctx context.Context, id string) (title, detail string) {
	head := d.spec.human + " " + id + " у края не найден"

	items, out, err := collectPages(ctx, d.c, d.spec.pathCol, url.Values{}, d.spec.itemsAttr)
	switch {
	case err != nil:
		return head, head + ", и подтвердить это нечем: перечень " + d.spec.humanMany +
			" тоже не прочитан.\n\nПодробности: " + err.Error()
	case out.Kind == client.OutcomeDenied || out.Kind == client.OutcomeUnauthenticated:
		return "Справочник не читается",
			head + ", и перечень " + d.spec.humanMany + " отвечает отказом: " + out.Message +
				"\n\nЭто событие ПРАВ, а не отсутствие записи."
	case out.Kind != client.OutcomeOK:
		return head, head + ", и перечень " + d.spec.humanMany + " не прочитан: " + out.Message
	case len(items) == 0:
		return "Отсутствие не подтверждено",
			head + ", и подтвердить отсутствие нечем: перечень " + d.spec.humanMany +
				" пуст целиком. Различить «такой записи нет» и «справочник не наполнен или не " +
				"виден» по одному ответу нельзя — на отказ в доступе край отвечает тем же " +
				"«не найдено»."
	}

	known := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it["id"].(string); ok && s != "" {
			known = append(known, s)
		}
	}
	const show = 20
	list := strings.Join(known, ", ")
	if len(known) > show {
		list = strings.Join(known[:show], ", ") + ", … (всего " + strconv.Itoa(len(known)) + ")"
	}
	return head, head + ". В справочнике сейчас: " + list + ".\n\nИдентификатор здесь — " +
		"рукописный слаг, а не сгенерённая строка, поэтому опечатка в нём выглядит ровно как " +
		"отсутствие записи."
}

var (
	_ datasource.DataSource              = (*catalogOneDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*catalogOneDataSource)(nil)
)

// ---- списочный справочник -----------------------------------------------------------------

type catalogManyDataSource struct {
	spec catalogSpec
	c    *client.Client
}

func newCatalogMany(spec catalogSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &catalogManyDataSource{spec: spec} }
}

func (d *catalogManyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.spec.nameMany
}

func (d *catalogManyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = dataSourceClient(req, resp)
}

func (d *catalogManyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]dsschema.Attribute{
		d.spec.itemsAttr: dsschema.ListNestedAttribute{
			Computed: true,
			MarkdownDescription: "Записи справочника в порядке, в котором их отдаёт край " +
				"(по времени создания и идентификатору).",
			NestedObject: dsschema.NestedAttributeObject{
				Attributes: dsElementAttributes(d.spec.fields),
			},
		},
	}
	for _, f := range d.spec.filters {
		switch f.kind {
		case dsBool:
			attrs[f.name] = dsschema.BoolAttribute{Optional: true, MarkdownDescription: f.doc}
		case dsInt64:
			attrs[f.name] = dsschema.Int64Attribute{Optional: true, MarkdownDescription: f.doc}
		default:
			attrs[f.name] = dsschema.StringAttribute{Optional: true, MarkdownDescription: f.doc}
		}
	}
	resp.Schema = dsschema.Schema{MarkdownDescription: d.spec.descrMany, Attributes: attrs}
}

// ValidateConfig ловит незужающий фильтр на этапе проверки — там, где ошибку ещё дёшево
// исправить. Тот же предикат применяется и при чтении: значение, неизвестное на проверке,
// становится известным только к чтению, и без второй проверки фильтр тихо не применился бы.
func (d *catalogManyDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	for _, f := range d.spec.filters {
		v, set, err := readFilter(ctx, configGetter{req.Config}, f)
		if err != nil {
			// На этапе проверки неизвестное значение — норма: судить можно только о том, что
			// уже известно. Отказ по неизвестности выносится в чтение, где значение есть.
			continue
		}
		if !set {
			continue
		}
		if ok, title, detail := f.narrows(v); !ok {
			resp.Diagnostics.AddAttributeError(path.Root(f.name), title, detail)
		}
	}
}

func (d *catalogManyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	q := url.Values{}
	for _, f := range d.spec.filters {
		v, set, err := readFilter(ctx, configGetter{req.Config}, f)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root(f.name), "Фильтр не прочитан", err.Error())
			return
		}
		if !set {
			continue
		}
		if ok, title, detail := f.narrows(v); !ok {
			resp.Diagnostics.AddAttributeError(path.Root(f.name), title, detail)
			return
		}
		q.Set(lowerCamel(f.name), f.queryValue(v))
	}

	items, out, err := collectPages(ctx, d.c, d.spec.pathCol, q, d.spec.itemsAttr)
	if err != nil {
		resp.Diagnostics.AddError("Список не прочитан: "+d.spec.human, err.Error())
		return
	}
	switch out.Kind {
	case client.OutcomeOK:
	case client.OutcomeDenied, client.OutcomeUnauthenticated:
		resp.Diagnostics.AddError("Справочник не читается",
			out.Message+"\n\nПраво читать справочник есть у любой опознанной личности, поэтому "+
				"отказ здесь означает негодный или истёкший токен.")
		return
	default:
		resp.Diagnostics.AddError("Список не прочитан: "+d.spec.human, out.Message)
		return
	}

	elemType := dsObjectType(d.spec.fields)
	values := make([]attr.Value, 0, len(items))
	for _, it := range items {
		v, err := dsObjectValue(ctx, d.spec.fields, it)
		if err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		values = append(values, v)
	}
	list, diags := types.ListValue(elemType, values)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.spec.itemsAttr), list)...)
}

var (
	_ datasource.DataSource                   = (*catalogManyDataSource)(nil)
	_ datasource.DataSourceWithConfigure      = (*catalogManyDataSource)(nil)
	_ datasource.DataSourceWithValidateConfig = (*catalogManyDataSource)(nil)
)

// ---- поиск существующего ресурса по имени --------------------------------------------------

// lookupSpec — поиск ресурса проекта по имени.
//
// Зачем это отдельно от импорта: импорт ЗАБИРАЕТ ресурс под управление, и Terraform начинает
// отвечать за его жизнь — вплоть до удаления. Сослаться на чужую сеть, заведённую другой
// командой или вручную, при этом невозможно: её нельзя ни импортировать (она не наша), ни
// назвать идентификатором (он у каждого стенда свой). Источник по имени закрывает ровно этот
// разрыв: читает и не берёт на себя ничего.
type lookupSpec struct {
	name      string
	human     string
	pathCol   string
	itemsAttr string
	descr     string
	fields    []dsField
}

var vpcNetworkLookup = lookupSpec{
	name: "_vpc_network_by_name", human: "Сеть",
	pathCol: networksPath, itemsAttr: "networks",
	descr: "Поиск СУЩЕСТВУЮЩЕЙ сети по имени в проекте — чтобы сослаться на чужую сеть, не " +
		"забирая её под управление Terraform.\n\n" +
		"Имя уникально в пределах проекта, поэтому ответ либо один, либо его нет вовсе. " +
		"Ненайденное — отказ, а не пустой результат: пустая строка уехала бы дальше по " +
		"конфигурации и всплыла бы отказом края в другом месте.\n\n" +
		":::note Чем это отличается от импорта\n" +
		"Импорт берёт ресурс под управление, и `destroy` его удалит. Источник только читает — " +
		"чужая сеть остаётся чужой.\n:::",
	fields: []dsField{
		{name: "id", kind: dsString, doc: "Неизменяемый идентификатор найденной сети."},
		{name: "project_id", kind: dsString, doc: "Проект, в котором идёт поиск."},
		{name: "name", kind: dsString, doc: "Имя сети. Ищется ДОСЛОВНО, без частичных совпадений."},
		{name: "description", kind: dsString, doc: "Описание сети."},
		{name: "labels", kind: dsStringMap, doc: "Метки вида ключ-значение."},
		{name: "created_at", kind: dsString, doc: "Момент создания по данным края."},
		{name: "default_security_group_id", kind: dsString,
			doc: "Группа безопасности сети по умолчанию."},
		{name: "default_route_table_id", kind: dsString,
			doc: "Таблица маршрутов сети по умолчанию."},
		{name: "ipv4_cidr_blocks", kind: dsStringList,
			doc: "Объявленные блоки IPv4 — супернет сети. Подсеть обязана лежать внутри одного " +
				"из них."},
		{name: "ipv6_cidr_blocks", kind: dsStringList, doc: "Объявленные блоки IPv6."},
	},
}

var vpcSubnetLookup = lookupSpec{
	name: "_vpc_subnet_by_name", human: "Подсеть",
	pathCol: subnetsPath, itemsAttr: "subnets",
	descr: "Поиск СУЩЕСТВУЮЩЕЙ подсети по имени в проекте — чтобы сослаться на чужую подсеть, " +
		"не забирая её под управление Terraform.\n\n" +
		"Имя уникально в пределах проекта. Ненайденное — отказ, а не пустой результат.",
	fields: []dsField{
		{name: "id", kind: dsString, doc: "Неизменяемый идентификатор найденной подсети."},
		{name: "project_id", kind: dsString, doc: "Проект, в котором идёт поиск."},
		{name: "name", kind: dsString, doc: "Имя подсети. Ищется ДОСЛОВНО."},
		{name: "description", kind: dsString, doc: "Описание подсети."},
		{name: "labels", kind: dsStringMap, doc: "Метки вида ключ-значение."},
		{name: "created_at", kind: dsString, doc: "Момент создания по данным края."},
		{name: "network_id", kind: dsString, doc: "Сеть, которой принадлежит подсеть."},
		{name: "placement_type", kind: dsString,
			doc: "Тип размещения: `ZONAL` — подсеть живёт в одной зоне, `REGIONAL` — anycast по " +
				"региону. Задано ровно одно из `zone_id` и `region_id`, и какое именно — " +
				"определяет этот признак."},
		{name: "zone_id", kind: dsString,
			doc: "Зона размещения; пусто у региональной подсети. У региональной подсети зоны " +
				"НЕТ вовсе — зональные проверки к ней не применяются."},
		{name: "region_id", kind: dsString, doc: "Регион размещения; пусто у зональной подсети."},
		{name: "route_table_id", kind: dsString, doc: "Таблица маршрутов подсети."},
		{name: "ipv4_cidr_primary", kind: dsString, doc: "Первичный блок IPv4; пусто у подсети только с IPv6."},
		{name: "ipv4_cidr_blocks", kind: dsStringList, doc: "Дополнительные блоки IPv4 сверх первичного."},
		{name: "ipv6_cidr_primary", kind: dsString, doc: "Первичный блок IPv6; пусто у подсети только с IPv4."},
		{name: "ipv6_cidr_blocks", kind: dsStringList, doc: "Дополнительные блоки IPv6 сверх первичного."},
	},
}

type lookupDataSource struct {
	spec lookupSpec
	c    *client.Client
}

func newLookupByName(spec lookupSpec) func() datasource.DataSource {
	return func() datasource.DataSource { return &lookupDataSource{spec: spec} }
}

func (d *lookupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.spec.name
}

func (d *lookupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = dataSourceClient(req, resp)
}

func (d *lookupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		MarkdownDescription: d.spec.descr,
		Attributes:          dsAttributes(d.spec.fields, []string{"project_id", "name"}),
	}
}

func (d *lookupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var project, name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("project_id"), &project)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if project.ValueString() == "" || name.ValueString() == "" {
		resp.Diagnostics.AddError("Поиск не задан",
			"Укажите project_id и name: "+strings.ToLower(d.spec.human)+" ищется по имени в "+
				"пределах проекта, и имя уникально только там.")
		return
	}

	// Сужение делает КРАЙ: списочный запрос принимает фильтр `name="…"` с точным равенством,
	// поэтому обычный поиск — один запрос, без обхода страниц проекта.
	//
	// Найденное имя всё равно сверяется ДОСЛОВНО, а обход курсора продолжается, пока край
	// отдаёт страницы. Это не перестраховка ради красоты: фильтр, который край однажды
	// перестанет применять, вернул бы первую попавшуюся строку проекта — и провайдер выдал бы
	// чужую сеть за запрошенную, не сказав ни слова. Со сверкой худшее, что может случиться, —
	// поиск станет стоить обхода страниц проекта.
	q := url.Values{}
	q.Set(client.ScopeProject, project.ValueString())
	q.Set("filter", fmt.Sprintf("name=%q", name.ValueString()))

	var found map[string]any
	out, err := walkPages(ctx, d.c, d.spec.pathCol, q, d.spec.itemsAttr, func(m map[string]any) bool {
		if s, ok := m["name"].(string); ok && s == name.ValueString() {
			found = m
			return true
		}
		return false
	})
	if err != nil {
		resp.Diagnostics.AddError("Поиск не выполнен: "+d.spec.human, err.Error())
		return
	}
	switch out.Kind {
	case client.OutcomeOK:
	case client.OutcomeDenied, client.OutcomeUnauthenticated:
		resp.Diagnostics.AddError("Проект не читается",
			"Список проекта "+project.ValueString()+" отвечает отказом: "+out.Message+
				"\n\nЭто событие ПРАВ, а не отсутствие ресурса.")
		return
	default:
		resp.Diagnostics.AddError("Поиск не выполнен: "+d.spec.human, out.Message)
		return
	}

	if found == nil {
		title, detail := d.absence(ctx, project.ValueString(), name.ValueString())
		resp.Diagnostics.AddError(title, detail)
		return
	}

	if err := checkEcho(d.spec.human, "project_id", project.ValueString(), found); err != nil {
		resp.Diagnostics.AddError("Край вернул не тот объект", err.Error())
		return
	}
	inputs := []string{"project_id", "name"}
	if err := applyFields(ctx, stateSetter{&resp.State}, d.spec.fields, inputs, found); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
	}
}

// absence — что означает «ресурса с таким именем в проекте не видно».
//
// Различение берётся у клиента (`ConfirmAbsence`), а не пишется здесь заново: оно уже
// устроено ровно под этот вопрос — список проекта по имени, затем контрольная страница
// проекта без фильтра. Вторая копия того же рассуждения разошлась бы с первой молча, потому
// что обе отвечают одинаково на исправном стенде.
//
// Цена одного лишнего запроса по имени принимается осознанно: он делается только на пути
// отказа, где счёт идёт не на запросы, а на понятность.
func (d *lookupDataSource) absence(ctx context.Context, projectID, name string) (title, detail string) {
	head := d.spec.human + " с именем " + strconv.Quote(name) + " в проекте " + projectID + " не найдена"

	verdict, err := d.c.ConfirmAbsence(ctx, d.spec.pathCol, client.ScopeProject, projectID, name)
	switch verdict {
	case client.VerdictGone:
		return head, head + ".\n\nПроект читается, ресурсы этого типа в нём есть — значит имени " +
			"действительно нет. Имя уникально в пределах проекта и ищется дословно: проверьте " +
			"регистр и проект."
	case client.VerdictPresent:
		// Край ответил себе противоречиво: по тому же фильтру поиск нашёл, а дословная сверка
		// имени — нет. Скрывать это нельзя: расхождение означает, что фильтр сужает не тем, чем
		// назван, и тогда ЛЮБОЙ ответ этого источника ненадёжен.
		return "Край отвечает противоречиво",
			head + " при дословной сверке имени, но тот же список по тому же фильтру возвращает " +
				"строку. Это значит, что фильтр по имени сужает выдачу не дословно, и " +
				"провайдер отказывается выдавать похожий объект за запрошенный."
	case client.VerdictDenied:
		return "Проект не читается",
			head + ", и список проекта тоже отвечает отказом. Это событие ПРАВ, а не отсутствие " +
				"ресурса."
	default:
		text := head + ", и подтвердить отсутствие нечем: в проекте не видно ни одного ресурса " +
			"этого типа. Различить «нет такого имени» и «доступ отозван» край не позволяет — " +
			"ответы совпадают дословно."
		if err != nil {
			text += "\n\nПодробности: " + err.Error()
		}
		return "Отсутствие не подтверждено", text
	}
}

var (
	_ datasource.DataSource              = (*lookupDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*lookupDataSource)(nil)
)

// ---- конструкторы для реестра провайдера ----------------------------------------------------

// NewGeoRegionDataSource — kacho_geo_region.
func NewGeoRegionDataSource() datasource.DataSource { return newCatalogOne(geoRegionCatalog)() }

// NewGeoRegionsDataSource — kacho_geo_regions.
func NewGeoRegionsDataSource() datasource.DataSource { return newCatalogMany(geoRegionCatalog)() }

// NewGeoZoneDataSource — kacho_geo_zone.
func NewGeoZoneDataSource() datasource.DataSource { return newCatalogOne(geoZoneCatalog)() }

// NewGeoZonesDataSource — kacho_geo_zones.
func NewGeoZonesDataSource() datasource.DataSource { return newCatalogMany(geoZoneCatalog)() }

// NewStorageDiskTypeDataSource — kacho_storage_disk_type.
func NewStorageDiskTypeDataSource() datasource.DataSource {
	return newCatalogOne(storageDiskTypeCatalog)()
}

// NewStorageDiskTypesDataSource — kacho_storage_disk_types.
func NewStorageDiskTypesDataSource() datasource.DataSource {
	return newCatalogMany(storageDiskTypeCatalog)()
}

// NewComputeMachineTypeDataSource — kacho_compute_machine_type.
func NewComputeMachineTypeDataSource() datasource.DataSource {
	return newCatalogOne(computeMachineTypeCatalog)()
}

// NewComputeMachineTypesDataSource — kacho_compute_machine_types.
func NewComputeMachineTypesDataSource() datasource.DataSource {
	return newCatalogMany(computeMachineTypeCatalog)()
}

// NewVPCNetworkByNameDataSource — kacho_vpc_network_by_name.
func NewVPCNetworkByNameDataSource() datasource.DataSource {
	return newLookupByName(vpcNetworkLookup)()
}

// NewVPCSubnetByNameDataSource — kacho_vpc_subnet_by_name.
func NewVPCSubnetByNameDataSource() datasource.DataSource {
	return newLookupByName(vpcSubnetLookup)()
}
