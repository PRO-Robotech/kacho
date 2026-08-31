// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strings"

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

const routeTablesPath = "/vpc/v1/routeTables"

// Таблица маршрутизации со статическими маршрутами.
//
// Три решения этого файла приняты по дереву, а не по виду контракта, и каждое ниже
// названо вместе с тем, что его держит.
//
// # 1. Маршруты приводятся МАСКОЙ ИЗМЕНЕНИЯ, а не глаголами края
//
// Гранулярных действий над маршрутами контракт vpc не объявляет ВОВСЕ: у службы
// таблицы маршрутизации есть только Get, List, Create, Update, Delete и
// ListOperations.
//
// Здесь стояло обратное: перечислялись три суффикс-действия над маршрутами и
// утверждалось, что все они отвечают 501. Ни одного из трёх в дереве нет — как нет
// и поверхности, которая отвечала бы за них 501. Сами имена тут не повторены
// намеренно: названный в прозе токен читается гейтом как утверждение о крае, и
// цитата мёртвого адреса стала бы новой находкой вместо снятой.
//
// Причина, по которой их не завели, остаётся верной и названа тут же: у маршрута
// НЕТ идентификатора — `StaticRoute` несёт назначение, следующий узел и метки, и
// больше ничего, поэтому адресовать одну строку набора нечем. Рабочий путь один —
// `RouteTableService.Update` с `static_routes` в маске и полным списком в теле.
//
// Это противоположность блокам адресов сети (`network_resource.go`), где пара действий
// края — единственный путь, потому что поле неизменяемо в маске. Здесь наоборот: маска
// работает, а глаголы отвергнуты. Отсюда и разница в коде — там сверка разницы и пара
// вызовов, тут один PATCH с полным списком.
//
// Следствие для пользователя, названное в описании поля: правка одного маршрута — это
// перезапись всего набора. Промежуточного состояния при этом нет: подстановка идёт под
// row-lock в одной транзакции края.
//
// # 2. Маршруты — СПИСОК, а не набор
//
// Порядок край СОХРАНЯЕТ: набор лежит одним значением jsonb, пишется и читается
// дословно, ни одной сортировки на пути чтения нет. Этим он отличается от целей группы
// балансировщика, которые край перечитывает упорядоченными по времени и идентификатору,
// — там набор обязателен, иначе перестановка объявлялась бы изменением.
//
// Решающий довод даже не в сохранении порядка, а в АДРЕСАЦИИ ОТКАЗА: край отвергает
// негодный маршрут по индексу — `static_routes[2].destination_prefix must have zero
// host-bits`. У списка индекс 2 в сообщении — это индекс 2 в конфигурации пользователя.
// У набора индекса нет вовсе, и такое сообщение указывало бы в никуда.
//
// Третье: два маршрута с одним назначением через разные следующие узлы — законная
// запись (равноценные пути), и край её принимает. Набор схлопнул бы их молча.
//
// # 3. Ветка следующего узла через шлюз НЕ объявлена
//
// `StaticRoute.next_hop` — oneof из двух ветвей, но вторая (`gateway_id`) отвергается
// краем СИНХРОННО: «is not supported: route the next hop by IP address». Ни в домене, ни
// в хранилище шлюза нет — маршрут несёт только адрес.
//
// Поле оставлено на контракте потому, что край REST молча выбрасывает неизвестные ключи
// тела, и его удаление вернуло бы именованный отказ обратно в невнятный «next_hop_address
// is required». К Terraform этот довод НЕ переносится: схема закрыта, и неизвестный
// аргумент даёт терминальный отказ с именем прямо на разборе конфигурации. Объявить
// атрибут, единственный исход которого — отказ, значило бы показать ручку, которой нет.
// Поэтому его здесь нет, а отсутствие названо в описании `next_hop_address` — чтобы оно
// читалось как решение, а не как отставание провайдера от контракта.
//
// # 4. Создание таблицы НЕ трогает подсети — этого механизма в дереве нет
//
// Здесь стояло предупреждение, что край сам привязывает новую таблицу к каждой подсети
// сети, у которой таблица ещё не задана. Такого механизма нет: триггер снят миграцией
// `services/vpc/internal/migrations/0019_drop_rt_auto_assoc_trigger.sql` — именно потому,
// что он давал ВТОРОЙ, невидимый в API способ выбрать таблицу подсети, зависящий от
// порядка создания. Остался один: привязка ставится на создании ПОДСЕТИ — из явного
// `route_table_id` либо из `default_route_table_id` сети.
//
// Цена ошибки была не косметической: предупреждение объявляло расхождение по
// `kacho_vpc_subnet.route_table_id` после заведения таблицы НОРМАЛЬНЫМ и предлагало его
// не читать. Расхождение там теперь означает настоящий дрейф.
//
// Обратное направление живо, поэтому предупреждение переписано, а не снято: удаление
// таблицы снимает ссылку у подсетей (FK `ON DELETE SET NULL`) и у сети, если таблица была
// её таблицей по умолчанию. Затрагиваемая сторона осталась той же, поменялось событие.
type routeTableModel struct {
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	NetworkID    types.String `tfsdk:"network_id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Labels       types.Map    `tfsdk:"labels"`
	StaticRoutes types.List   `tfsdk:"static_routes"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

// staticRouteModel — один маршрут.
//
// Все поля — значения фреймворка, ни одного указателя на структуру: вложенного объекта
// внутри маршрута нет, а поля-ссылки вызывающий вправе собрать из ещё не вычисленного
// значения, и указатель НЕИЗВЕСТНОЕ держать не умеет.
type staticRouteModel struct {
	DestinationPrefix types.String `tfsdk:"destination_prefix"`
	NextHopAddress    types.String `tfsdk:"next_hop_address"`
	Labels            types.Map    `tfsdk:"labels"`
}

type routeTableResource struct{ c *client.Client }

// NewVPCRouteTableResource — конструктор для реестра провайдера.
func NewVPCRouteTableResource() resource.Resource { return &routeTableResource{} }

func (r *routeTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_route_table"
}

func (r *routeTableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *routeTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Неизменяемое поле берёт значение из состояния ПЕРЕД тем, как объявить замену:
	// вычисляемого значения у него нет, но план всё равно проходит через сравнение, и без
	// UseStateForUnknown неизвестное читалось бы как другое значение.
	replace := []planmodifier.String{
		stringplanmodifier.UseStateForUnknown(),
		stringplanmodifier.RequiresReplace(),
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Таблица маршрутизации сети VPC: набор статических маршрутов.\n\n" +
			"Заведение таблицы САМО ПО СЕБЕ не меняет ни одной подсети. Подсеть получает " +
			"свою таблицу в момент СВОЕГО создания — из явного `route_table_id` либо из " +
			"`default_route_table_id` сети, — поэтому чтобы эта таблица применялась, " +
			"укажите её в `route_table_id` нужных подсетей. Расхождение по " +
			"`kacho_vpc_subnet.route_table_id` после заведения таблицы нормальным НЕ " +
			"является.\n\n" +
			":::warning Удаление таблицы ЗАТРАГИВАЕТ подсети\n" +
			"Край снимает ссылку у каждой подсети, которая указывала на эту таблицу, и у " +
			"сети, если таблица была её таблицей по умолчанию. Вычисляемый " +
			"`route_table_id` у `kacho_vpc_subnet` при этом меняется — вот это расхождение " +
			"на следующем плане ожидаемо и относится к подсетям, а не к таблице.\n:::",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор таблицы. По нему выполняется импорт.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Край отвергает его в маске изменения " +
					"явно («project_id is immutable after RouteTable.Create»), поэтому смена " +
					"пересоздаёт таблицу."},
			"network_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Сеть-владелец. Обязана принадлежать тому же проекту. " +
					"Край отвергает её в маске изменения явно, поэтому смена пересоздаёт таблицу."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя таблицы в пределах проекта. Провайдер требует его " +
					"СТРОЖЕ края: имя — единственный способ найти уже созданный ресурс, если " +
					"ответ на создание потерялся, и без него повтор создал бы дубль."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Произвольное описание, до 256 знаков."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки вида ключ-значение."},
			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},

			// СПИСОК, а не набор, и НЕ вычисляемый.
			//
			// Не вычисляемый — чтобы снятие блока из конфигурации ОЧИЩАЛО набор. У
			// вычисляемого атрибута Terraform на пустой настройке удерживает значение из
			// состояния, и «убрал маршруты» молча не применилось бы — то есть поле было бы
			// принято и не применено.
			"static_routes": schema.ListNestedAttribute{Optional: true,
				MarkdownDescription: "Статические маршруты — СПИСОК: край сохраняет порядок " +
					"дословно, а свои отказы адресует индексом (`static_routes[2]…`), и у " +
					"списка этот индекс совпадает с индексом в конфигурации.\n\n" +
					"Набор правится ЦЕЛИКОМ, одним запросом: у маршрута нет идентификатора, " +
					"поэтому гранулярных действий над маршрутами у края нет вовсе — " +
					"контракт их не объявляет. Правка " +
					"одного маршрута — это перезапись всего списка; промежуточного состояния " +
					"нет, подстановка идёт одной транзакцией края.\n\n" +
					"Два маршрута с одним назначением через разные следующие узлы — законная " +
					"запись (равноценные пути), и она сохраняется как написана.",
				// Ни одного Computed и ни одного значения по умолчанию ВНУТРИ элемента:
				// край в маршруте ничего не вычисляет — он возвращает ровно то, что принял.
				// Подставленное умолчание отличалось бы от написанного и давало бы
				// расхождение на первом же плане. Отсутствие выражается null.
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"destination_prefix": schema.StringAttribute{Required: true,
						MarkdownDescription: "Назначение маршрута в записи CIDR, например " +
							"`10.1.0.0/16` или `::/0`. Разряды узла обязаны быть нулевыми: " +
							"`10.1.0.5/16` край отвергает и называет сетевой адрес, который " +
							"имелся в виду."},
					"next_hop_address": schema.StringAttribute{Required: true,
						MarkdownDescription: "Следующий узел — IP-адрес, IPv4 или IPv6.\n\n" +
							"Второй ветви следующего узла (через ресурс шлюза) у провайдера " +
							"НЕТ намеренно: край отвергает её синхронно — маршрут хранит " +
							"только адрес, — и атрибут, единственный исход которого отказ, " +
							"показывал бы ручку, которой не существует. Обходного пути тоже " +
							"нет, и искать его незачем: собственного адреса у " +
							"`kacho_vpc_gateway` не существует и в контракте, поэтому " +
							"следующим узлом называется адрес устройства внутри сети, а не " +
							"шлюз."},
					"labels": schema.MapAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Метки маршрута, до 64 на ресурс."},
				}}},
		},
	}
}

// ValidateConfig проверяет ЗАПИСЬ маршрута до обращения к краю.
//
// Край проверяет то же самое и отвечает точным текстом по индексу — но его отказ приедет
// после сетевого вызова, а в общем применении из нескольких ресурсов ещё и ПОСЛЕ того,
// как соседние ресурсы уже созданы. Опечатка в конфигурации обязана называться на этапе
// проверки, пока не создано ничего (тот же довод записан у подсети).
//
// Риск двух мест об одном предмете здесь мал и назван честно: предикат тот же самый —
// `net/netip` стандартной библиотеки на обеих сторонах, — и провайдер судит ровно о том,
// что край проверяет первым. Расхождение потребовало бы смены семантики стандартной
// библиотеки, а не решения края.
//
// Судить можно ТОЛЬКО о том, что уже известно: значение маршрута вправе прийти из ещё не
// вычисленной ссылки, и счесть неизвестное негодным значило бы отвергнуть законную
// конфигурацию.
func (r *routeTableResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg routeTableModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(staticRouteDiagnostics(ctx, cfg.StaticRoutes)...)
}

// staticRouteDiagnostics — само правило, отдельной функцией от чтения настройки.
//
// Разделение не косметическое: правило — чистая функция набора, и его можно предъявить
// таблицей случаев с положительным контролем в каждой строке. Внутри метода фреймворка та
// же таблица потребовала бы сборки сырого значения конфигурации на каждый случай, и первым,
// что перестали бы проверять, стал бы именно положительный контроль.
func staticRouteDiagnostics(ctx context.Context, list types.List) diagList {
	var diags diagList
	if list.IsUnknown() {
		return diags
	}
	for i, rt := range routesOf(ctx, list) {
		if routeFieldKnown(rt.DestinationPrefix) {
			dst := rt.DestinationPrefix.ValueString()
			prefix, err := netip.ParsePrefix(dst)
			switch {
			case err != nil:
				diags.AddError("Негодное назначение маршрута",
					fmt.Sprintf("static_routes[%d].destination_prefix = %q не разбирается как "+
						"запись CIDR. Ожидается вид `10.1.0.0/16` или `::/0`.", i, dst))
			case prefix.Masked() != prefix:
				diags.AddError("В назначении маршрута заданы разряды узла",
					fmt.Sprintf("static_routes[%d].destination_prefix = %q описывает адрес "+
						"внутри сети, а маршрут задаётся самой сетью. Укажите %q.",
						i, dst, prefix.Masked().String()))
			}
		}
		if routeFieldKnown(rt.NextHopAddress) {
			hop := rt.NextHopAddress.ValueString()
			if _, err := netip.ParseAddr(hop); err != nil {
				diags.AddError("Негодный следующий узел маршрута",
					fmt.Sprintf("static_routes[%d].next_hop_address = %q не разбирается как "+
						"IP-адрес. Следующий узел задаётся только адресом (IPv4 или IPv6).", i, hop))
			}
		}
	}
	return diags
}

// routeFieldKnown — годится ли значение для суждения. Неизвестное и null — не годятся: первое ещё
// не вычислено, второго нет. Обязательность полей элемента держит сам фреймворк.
func routeFieldKnown(v types.String) bool {
	return !v.IsNull() && !v.IsUnknown()
}

// ---- перевод модели в запрос края ---------------------------------------------------

// routesOf — маршруты из значения Terraform. Неизвестный и null дают nil: «не задал» и
// «задал пустым» различаются, и пустой список вместо отсутствия стирал бы набор.
func routesOf(ctx context.Context, l types.List) []staticRouteModel {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]staticRouteModel, 0, len(l.Elements()))
	_ = l.ElementsAs(ctx, &out, false)
	return out
}

// routesToProto — маршруты в форму контракта.
//
// Ветвь следующего узла всегда одна и та же: у контракта их две, но вторая краем
// отвергается, и провайдер её не объявляет (разбор — в шапке файла).
func routesToProto(ctx context.Context, l types.List) []*vpcv1.StaticRoute {
	rts := routesOf(ctx, l)
	if len(rts) == 0 {
		return nil
	}
	out := make([]*vpcv1.StaticRoute, 0, len(rts))
	for _, rt := range rts {
		out = append(out, &vpcv1.StaticRoute{
			Destination: &vpcv1.StaticRoute_DestinationPrefix{
				DestinationPrefix: rt.DestinationPrefix.ValueString()},
			NextHop: &vpcv1.StaticRoute_NextHopAddress{
				NextHopAddress: rt.NextHopAddress.ValueString()},
			Labels: mapFromTF(ctx, rt.Labels),
		})
	}
	return out
}

// staticRouteObjectType — тип элемента списка. Объявлен ЗДЕСЬ и один раз: разойдись он со
// схемой хоть одним полем, список молча не собрался бы, и маршруты пропали бы из состояния.
func staticRouteObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"destination_prefix": types.StringType,
		"next_hop_address":   types.StringType,
		"labels":             types.MapType{ElemType: types.StringType},
	}}
}

// ---- разбор ответа края -------------------------------------------------------------

// rtWire — форма ответа края. Разбирается своим типом, а не сгенерённым: сгенерённый
// потребовал бы разрешения google.protobuf.Any через глобальный реестр типов, которого у
// провайдера нет. Тело ЗАПРОСА остаётся сгенерённым — именно там опечатка в имени поля
// прошла бы молча.
//
// Ключа шлюза здесь НЕТ, и это не пропуск: край его не принимает ни при создании, ни при
// изменении, а в хранилище маршрута такого поля не существует — разбирать нечего.
type rtWire struct {
	ID           string            `json:"id"`
	ProjectID    string            `json:"projectId"`
	CreatedAt    string            `json:"createdAt"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Labels       map[string]string `json:"labels"`
	NetworkID    string            `json:"networkId"`
	StaticRoutes []struct {
		DestinationPrefix string            `json:"destinationPrefix"`
		NextHopAddress    string            `json:"nextHopAddress"`
		Labels            map[string]string `json:"labels"`
	} `json:"staticRoutes"`
}

func applyRouteTable(ctx context.Context, m *routeTableModel, raw []byte) error {
	var w rtWire
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

	if len(w.StaticRoutes) == 0 {
		m.StaticRoutes = types.ListNull(staticRouteObjectType())
		return nil
	}
	rts := make([]staticRouteModel, 0, len(w.StaticRoutes))
	for _, wr := range w.StaticRoutes {
		rts = append(rts, staticRouteModel{
			DestinationPrefix: types.StringValue(wr.DestinationPrefix),
			NextHopAddress:    types.StringValue(wr.NextHopAddress),
			Labels:            routeLabelsOrNull(ctx, wr.Labels),
		})
	}
	list, diags := types.ListValueFrom(ctx, staticRouteObjectType(), rts)
	if diags.HasError() {
		return fmt.Errorf("маршруты края не укладываются в список: %v", diags.Errors())
	}
	m.StaticRoutes = list
	return nil
}

// routeLabelsOrNull — метки маршрута из ответа края.
//
// Отсутствие меток выражается null, а не пустой картой: край опускает ключ, когда меток
// нет, и записать вместо этого `{}` значило бы объявить пользователю то, чего он не писал.
// Общий mapToTF здесь не годится — он приводит nil к пустой карте, и это верно для меток
// САМОГО ресурса (край их всегда эхает), но не для меток элемента списка, где пустая карта
// и отсутствие — разные значения плана.
func routeLabelsOrNull(ctx context.Context, in map[string]string) types.Map {
	if len(in) == 0 {
		return types.MapNull(types.StringType)
	}
	return mapToTF(ctx, in)
}

// routeKey — канонический ключ маршрута по СОДЕРЖАНИЮ.
//
// Метки складываются в порядке ключей, а пустая карта и её отсутствие дают одно и то же:
// край опускает пустые метки, поэтому иначе написанное пользователем `labels = {}`
// расходилось бы с прочитанным null на каждом плане.
func routeKey(ctx context.Context, rt staticRouteModel) string {
	labels := mapFromTF(ctx, rt.Labels)
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(rt.DestinationPrefix.ValueString())
	b.WriteString("|")
	b.WriteString(rt.NextHopAddress.ValueString())
	for _, k := range keys {
		b.WriteString("|")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(labels[k])
	}
	return b.String()
}

// keepRoutesSpelling сохраняет написание набора из настройки, когда край вернул то же
// самое по СОДЕРЖАНИЮ.
//
// Пустой набор и отсутствие набора для края одно и то же — он опускает пустой список, — но
// для Terraform это РАЗНЫЕ значения, и apply падает «был пустой список, стал null» на
// совершенно законной настройке. То же самое внутри маршрута с пустыми метками.
// Сохраняется именно написание вызывающего: он его выбрал, и менять его молча значит
// спорить с ним ни о чём.
//
// Сравнение ПОЗИЦИОННОЕ, в отличие от целей группы балансировщика: порядок здесь значим и
// краем сохраняется, поэтому перестановка — настоящее изменение, а не другое написание.
func keepRoutesSpelling(ctx context.Context, want types.List, m *routeTableModel) {
	if want.IsUnknown() {
		return
	}
	a := routesOf(ctx, want)
	b := routesOf(ctx, m.StaticRoutes)
	if len(a) != len(b) {
		return
	}
	for i := range a {
		if routeKey(ctx, a[i]) != routeKey(ctx, b[i]) {
			return
		}
	}
	m.StaticRoutes = want
}

// ---- CRUD ---------------------------------------------------------------------------

func (r *routeTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routeTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.CreateRouteTableRequest{
		ProjectId:    plan.ProjectID.ValueString(),
		NetworkId:    plan.NetworkID.ValueString(),
		Name:         plan.Name.ValueString(),
		Description:  plan.Description.ValueString(),
		Labels:       mapFromTF(ctx, plan.Labels),
		StaticRoutes: routesToProto(ctx, plan.StaticRoutes),
	}

	id, err := awaitCreate(ctx, r.c, routeTablesPath, "routeTableId", "kacho_vpc_route_table",
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание таблицы маршрутизации не завершилось", err.Error())
		return
	}

	// Идентификатор в состояние — ДО первого обратного чтения. Это единственная точка,
	// где потеря необратима: ресурс создан, apply прерван, состояние пусто — следующий
	// apply создаст дубль.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт сообщение на каждое
	// поле вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, routeTablesPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Таблица маршрутизации создана, но не прочитана обратно", err.Error())
		return
	}
	// Написание берётся ПОСЛЕ гашения неизвестных: неизвестный набор гасится в null, и
	// сохранять его как «написание пользователя» было бы записью неизвестного в состояние.
	wantRoutes := plan.StaticRoutes
	if err := applyRouteTable(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepRoutesSpelling(ctx, wantRoutes, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routeTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routeTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, routeTablesPath, state.ID.ValueString(), false)
	if err == nil {
		wantRoutes := state.StaticRoutes
		if err := applyRouteTable(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		keepRoutesSpelling(ctx, wantRoutes, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение таблицы маршрутизации не удалось", err.Error())
		return
	}
	// Одиночное «не найдено» ничего не устанавливает: тот же ответ приходит при отказе в
	// доступе, и он побайтово равен настоящему отсутствию.
	remove, title, detail := absenceDiagnostics(ctx, r.c, routeTablesPath, client.ScopeProject,
		"Таблица маршрутизации", state.ID.ValueString(),
		state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *routeTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state routeTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &vpcv1.UpdateRouteTableRequest{RouteTableId: state.ID.ValueString()}
	// Пути маски — В ФОРМЕ КОНТРАКТА (`static_routes`), а не в форме провода.
	// protojson переводит их сам и отвергает уже переведённые: путь `staticRoutes` даёт
	// ошибку сериализации «contains irreversible value», то есть запрос не уходит вовсе.
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

	// Маршруты — ОДНИМ полем маски, полным списком.
	//
	// Гранулярных действий у края нет (они отвечают 501), и это к лучшему: полная замена
	// под row-lock атомарна, тогда как пара «добавить/снять» провела бы таблицу через
	// состояние, которого пользователь не описывал.
	//
	// Очистка набора уходит НАМЕРЕННО, и делает её ПУТЬ В МАСКЕ, а не тело: пустой набор
	// `routesToProto` отдаёт как nil, и protojson опускает ключ целиком. То есть на проводе
	// «стереть все маршруты» — это `static_routes` в маске при отсутствующем списке, а не
	// присланный `[]`. Отсюда и отсутствие условия «отправлять только непустое»: оно
	// оставило бы снятие последнего маршрута непримененным.
	if !plan.StaticRoutes.Equal(state.StaticRoutes) && !plan.StaticRoutes.IsUnknown() {
		body.StaticRoutes = routesToProto(ctx, plan.StaticRoutes)
		paths = append(paths, "static_routes")
	}

	// Пустая маска НИКОГДА не отправляется: край трактует её как полнообъектную запись и
	// применяет из тела имя, описание, метки И МАРШРУТЫ — то есть стирает то, о чём не
	// просили. Маршруты в этом перечне названы отдельно: их пропуск читался бы как
	// «набор переживёт пустую маску», а он не переживает.
	// Обратное чтение делается ВСЕГДА: в плане вычисляемые атрибуты ещё неизвестны, а
	// состояние неизвестного не хранит, и ранний возврат дал бы «invalid result object
	// after apply» при уже применённом изменении.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			routeTablesPath+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение таблицы маршрутизации не завершилось", err.Error())
			return
		}
	}

	raw, err := readByID(ctx, r.c, routeTablesPath, state.ID.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Таблица маршрутизации изменена, но не прочитана обратно", err.Error())
		return
	}
	plan.ID = state.ID
	wantRoutes := plan.StaticRoutes
	if err := applyRouteTable(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepRoutesSpelling(ctx, wantRoutes, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routeTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routeTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		routeTablesPath+"/"+state.ID.ValueString(), nil); err != nil {
		// Ссылающиеся подсети удалению НЕ мешают: край сбрасывает у них привязку.
		// Поэтому подсказка говорит именно о последствии, а не выдумывает препятствие,
		// которого нет, — иначе она отправила бы искать причину не там.
		resp.Diagnostics.AddError("Удаление таблицы маршрутизации не завершилось",
			err.Error()+"\n\nПодсети этой сети удалению не мешают: край сбрасывает у них "+
				"привязку к таблице. Если таблица была назначена сети как таблица по "+
				"умолчанию, эта ссылка тоже снимается.")
	}
}

// ImportState принимает идентификатор ресурса. Формат проверяется общим каталогом
// префиксов платформы ДО обращения к краю: заведомо негодная строка получает терминальный
// отказ с внятным текстом, а не уезжает в сеть за ответом «не найдено», который для неё
// не значит ничего.
func (r *routeTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "таблица маршрутизации", ids.PrefixRouteTable, req, resp)
}
