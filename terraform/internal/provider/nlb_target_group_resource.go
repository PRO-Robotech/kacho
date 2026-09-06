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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/protobuf/types/known/durationpb"

	nlbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const targetGroupsPath = "/nlb/v1/targetGroups"

// Группа целей — самостоятельный ресурс: она заводится до балансировщика и живёт
// независимо от него, а слушатель на неё только ссылается.
//
// Цели — ПОЛЕ группы, а не отдельные ресурсы: у цели нет собственного идентификатора,
// адресовать её извне нечем, и край меняет их целым списком тем же запросом. Поэтому
// список целей описывается здесь же, а не отдельным ресурсом с привязкой.
type targetGroupModel struct {
	ID                  types.String `tfsdk:"id"`
	ProjectID           types.String `tfsdk:"project_id"`
	RegionID            types.String `tfsdk:"region_id"`
	Name                types.String `tfsdk:"name"`
	Description         types.String `tfsdk:"description"`
	Labels              types.Map    `tfsdk:"labels"`
	Port                types.Int64  `tfsdk:"port"`
	DeregistrationDelay types.String `tfsdk:"deregistration_delay"`
	SlowStart           types.String `tfsdk:"slow_start"`
	HealthCheck         types.Object `tfsdk:"health_check"`
	Targets             types.Set    `tfsdk:"targets"`
	Status              types.String `tfsdk:"status"`
	CreatedAt           types.String `tfsdk:"created_at"`
}

// healthModel — проверка живости. Ровно одна из четырёх проб обязана быть задана: это
// требование края (oneof с exactly_one), и провайдер проверяет его ДО отправки, чтобы
// оператор получил указание на поле, а не «invalid argument» из глубины.
type healthModel struct {
	Interval           types.String `tfsdk:"interval"`
	Timeout            types.String `tfsdk:"timeout"`
	HealthyThreshold   types.Int64  `tfsdk:"healthy_threshold"`
	UnhealthyThreshold types.Int64  `tfsdk:"unhealthy_threshold"`
	EffectivePort      types.Int64  `tfsdk:"effective_port"`
	TCP                *tcpProbe    `tfsdk:"tcp"`
	HTTP               *httpProbe   `tfsdk:"http"`
	HTTPS              *httpProbe   `tfsdk:"https"`
	GRPC               *grpcProbe   `tfsdk:"grpc"`
}

type tcpProbe struct {
	Port types.Int64 `tfsdk:"port"`
}

// httpProbe обслуживает и http, и https: у края это разные варианты oneof с ОДИНАКОВЫМ
// набором полей, и совпадение не случайно — контракт объявляет HTTPS как «то же, что HTTP,
// поверх TLS». Разойдутся наборы — разойдутся и типы.
type httpProbe struct {
	Port          types.Int64  `tfsdk:"port"`
	Path          types.String `tfsdk:"path"`
	ExpectedCodes types.String `tfsdk:"expected_codes"`
	Host          types.String `tfsdk:"host"`
	Headers       types.Map    `tfsdk:"headers"`
}

type grpcProbe struct {
	Port        types.Int64  `tfsdk:"port"`
	ServiceName types.String `tfsdk:"service_name"`
}

// targetModel — одна цель. Ровно одна из четырёх форм адресации обязана быть задана.
//
// Наблюдаемого состояния цели (жива ли она, когда начала выводиться) здесь НЕТ намеренно:
// оно меняется само по себе, и его присутствие в ресурсе давало бы вечный дрейф плана на
// инфраструктуре, которую никто не трогал.
type targetModel struct {
	InstanceID types.String `tfsdk:"instance_id"`
	NicID      types.String `tfsdk:"nic_id"`
	IPRef      *ipRefModel  `tfsdk:"ip_ref"`
	ExternalIP *extIPModel  `tfsdk:"external_ip"`
	Weight     types.Int64  `tfsdk:"weight"`
}

type ipRefModel struct {
	SubnetID types.String `tfsdk:"subnet_id"`
	Address  types.String `tfsdk:"address"`
}

type extIPModel struct {
	Address types.String `tfsdk:"address"`
	ZoneID  types.String `tfsdk:"zone_id"`
}

type targetGroupResource struct {
	c *client.Client
	// targetsBudget — срок ожидания сходимости состава; ноль означает умолчание.
	// Поле существует, чтобы проба могла утверждать САМ ОТКАЗ по сроку, а не любую
	// ошибку: «вернулось не nil» зеленеет и на обрыве соединения.
	targetsBudget time.Duration
}

// NewNLBTargetGroupResource — конструктор для реестра провайдера.
func NewNLBTargetGroupResource() resource.Resource { return &targetGroupResource{} }

func (r *targetGroupResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameNLBTargetGroup
}

func (r *targetGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// probePortAttr — порт пробы. Значение по умолчанию задано ЯВНО, а не получено обратным
// чтением: край эхает ноль, и без Default план после apply расходился бы с состоянием.
func probePortAttr() schema.Int64Attribute {
	return schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0),
		MarkdownDescription: "Порт пробы. `0` — брать порт самой группы."}
}

func httpProbeAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"port": probePortAttr(),
		"path": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			MarkdownDescription: "Путь запроса пробы, например `/healthz`."},
		"expected_codes": schema.StringAttribute{Optional: true, Computed: true,
			Default: stringdefault.StaticString(""),
			MarkdownDescription: "Коды, считающиеся живыми: перечисление и/или диапазоны — " +
				"`200-299`, `200,204`. Пусто — любой 2xx."},
		"host": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			MarkdownDescription: "Значение заголовка `Host` в запросе пробы."},
		"headers": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
			MarkdownDescription: "Дополнительные заголовки запроса пробы."},
	}
}

func (r *targetGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Группа целей балансировщика: куда раздаётся трафик и как проверяется живость.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт группу."},
			"region_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Регион. Обязан совпадать с регионом балансировщика, который " +
					"будет на неё ссылаться."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя в пределах проекта."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default: stringdefault.StaticString("")},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"port": schema.Int64Attribute{Required: true,
				MarkdownDescription: "Порт, на который раздаётся трафик, `1`–`65535`."},
			"deregistration_delay": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Окно вывода цели из обслуживания, например `30s`. " +
					"Край подставляет `300s`, если не задано.\n\n" +
					":::warning Это окно оплачивается временем `destroy`\n" +
					"Вывод цели двухфазный: сначала она помечается выводимой и лишь по истечении " +
					"окна снимается. Для групп под управлением Terraform разумно ставить `0s` — " +
					"иначе каждое удаление будет ждать столько, сколько здесь указано.\n:::"},
			"slow_start": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Окно постепенного набора нагрузки новой целью, например `30s`."},
			"status": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Состояние группы у края: `ACTIVE` или `DELETING`."},
			"created_at": schema.StringAttribute{Computed: true},

			"health_check": schema.SingleNestedAttribute{Required: true,
				MarkdownDescription: "Проверка живости. Ровно одна проба из четырёх — `tcp`, `http`, " +
					"`https`, `grpc` — обязана быть задана.",
				Attributes: map[string]schema.Attribute{
					"interval": schema.StringAttribute{Optional: true, Computed: true,
						Default:             stringdefault.StaticString("2s"),
						MarkdownDescription: "Период проверки, `1s`–`600s`."},
					"timeout": schema.StringAttribute{Optional: true, Computed: true,
						Default:             stringdefault.StaticString("1s"),
						MarkdownDescription: "Срок ожидания ответа. Не больше периода."},
					"healthy_threshold": schema.Int64Attribute{Optional: true, Computed: true,
						Default:             int64default.StaticInt64(2),
						MarkdownDescription: "Сколько удачных проверок подряд делают цель живой, `2`–`10`."},
					"unhealthy_threshold": schema.Int64Attribute{Optional: true, Computed: true,
						Default:             int64default.StaticInt64(2),
						MarkdownDescription: "Сколько неудачных проверок подряд выводят цель, `2`–`10`."},
					"effective_port": schema.Int64Attribute{Computed: true,
						MarkdownDescription: "Порт, на который проба идёт фактически: порт пробы, " +
							"а если он не задан — порт группы."},
					"tcp": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Проба соединением, без полезной нагрузки.",
						Attributes:          map[string]schema.Attribute{"port": probePortAttr()}},
					"http": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Проба запросом HTTP.",
						Attributes:          httpProbeAttrs()},
					"https": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Проба запросом HTTPS — то же, что HTTP, поверх TLS.",
						Attributes:          httpProbeAttrs()},
					"grpc": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Проба `grpc.health.v1.Check`.",
						Attributes: map[string]schema.Attribute{
							"port": probePortAttr(),
							"service_name": schema.StringAttribute{Optional: true, Computed: true,
								Default: stringdefault.StaticString(""),
								MarkdownDescription: "Имя сервиса для пробы. " +
									"Пусто — здоровье сервера целиком."},
						}},
				}},

			// НАБОР, а не список: край не сохраняет порядок целей — он читает их
			// упорядоченными по времени создания и идентификатору, а у целей одного запроса
			// время совпадает, и спор разрешает крокфордов идентификатор. Список сверялся бы
			// по индексу и объявлял бы перестановку изменением. См. `api-conventions.md`
			// §«Пустое значение обязано означать „пусто"».
			"targets": schema.SetNestedAttribute{Optional: true,
				MarkdownDescription: "Цели группы — НАБОР: порядок не значим и краем не " +
					"сохраняется. Ровно одна форма адресации на цель.\n\n" +
					"Наблюдаемого состояния цели здесь нет намеренно: оно меняется само и давало бы " +
					"вечный дрейф плана.",
				// Ни одного Computed и ни одного значения по умолчанию ВНУТРИ элемента —
				// намеренно. Элемент набора адресуется своим значением целиком, поэтому
				// подстановка умолчания в первый элемент меняет ключ и ломает поиск
				// настройки для остальных: измерено — вес 50 у второй цели превращался
				// в 0 на следующем же плане. Отсутствие выражается null, и обратное
				// чтение приводит нули края обратно в null.
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"instance_id": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Машина целиком — трафик пойдёт на её основной интерфейс."},
					"nic_id": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Конкретный сетевой интерфейс."},
					"ip_ref": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Адрес внутри облака, привязанный к подсети.",
						Attributes: map[string]schema.Attribute{
							"subnet_id": schema.StringAttribute{Required: true},
							"address":   schema.StringAttribute{Required: true},
						}},
					"external_ip": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Адрес вне облака.",
						Attributes: map[string]schema.Attribute{
							"address": schema.StringAttribute{Required: true},
							"zone_id": schema.StringAttribute{Required: true},
						}},
					"weight": schema.Int64Attribute{Optional: true,
						MarkdownDescription: "Вес цели, `1`–`1000`. Вес по умолчанию задаётся " +
							"ОПУСКАНИЕМ поля, а не нулём: ноль и есть «по умолчанию», и два " +
							"способа сказать одно и то же разошлись бы в состоянии."},
				}}},
		},
	}
}

// healthOf — проверка живости из значения Terraform.
//
// Поле модели объявлено types.Object, а НЕ указателем на структуру: указатель неспособен
// держать НЕИЗВЕСТНОЕ значение, а проверку живости вызывающий вправе собрать из переменной,
// которая в момент плана ещё не вычислена. На указателе провайдер отвечал бы «Value
// Conversion Error… target type cannot handle unknown values» — отказом, из которого не
// видно ни поля, ни причины.
func healthOf(ctx context.Context, o types.Object) *healthModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m healthModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

// healthObjectType — тип значения проверки живости. Объявлен ОДИН раз: разойдись он со
// схемой хоть одним полем, объект молча не собрался бы, и проверка исчезла бы из состояния.
func healthObjectType() attr.Type {
	probe := func(attrs map[string]attr.Type) attr.Type { return types.ObjectType{AttrTypes: attrs} }
	httpAttrs := map[string]attr.Type{
		"port": types.Int64Type, "path": types.StringType,
		"expected_codes": types.StringType, "host": types.StringType,
		"headers": types.MapType{ElemType: types.StringType},
	}
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"interval": types.StringType, "timeout": types.StringType,
		"healthy_threshold": types.Int64Type, "unhealthy_threshold": types.Int64Type,
		"effective_port": types.Int64Type,
		"tcp":            probe(map[string]attr.Type{"port": types.Int64Type}),
		"http":           probe(httpAttrs),
		"https":          probe(httpAttrs),
		"grpc":           probe(map[string]attr.Type{"port": types.Int64Type, "service_name": types.StringType}),
	}}
}

// probeOf — какая проба задана и сколько их. Число нужно, чтобы отличить «ни одной» от
// «двух», имя — чтобы сравнивать выбор между планом и состоянием.
func (h *healthModel) probeOf() (name string, count int) {
	for _, p := range []struct {
		n  string
		on bool
	}{
		{"tcp", h.TCP != nil}, {"http", h.HTTP != nil},
		{"https", h.HTTPS != nil}, {"grpc", h.GRPC != nil},
	} {
		if p.on {
			name, count = p.n, count+1
		}
	}
	return name, count
}

func (t *targetModel) identityCount() int {
	n := 0
	for _, on := range []bool{
		isSet(t.InstanceID), isSet(t.NicID), t.IPRef != nil, t.ExternalIP != nil,
	} {
		if on {
			n++
		}
	}
	return n
}

// isSet — задано ли строковое значение. Неизвестное считается ЗАДАННЫМ: на этапе проверки
// конфигурации ссылка на ещё не созданный ресурс приходит именно неизвестной, и счесть её
// пустой значило бы отвергнуть законную конфигурацию.
func isSet(v types.String) bool {
	if v.IsUnknown() {
		return true
	}
	return !v.IsNull() && v.ValueString() != ""
}

// ValidateConfig ловит два условия края до отправки — выбор пробы и выбор адресации.
func (r *targetGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg targetGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if hc := healthOf(ctx, cfg.HealthCheck); hc != nil {
		if _, n := hc.probeOf(); n != 1 {
			resp.Diagnostics.AddError("Проба живости задана не однозначно",
				fmt.Sprintf("В health_check задано проб: %d. Ровно одна из tcp, http, https, grpc "+
					"обязана присутствовать — край требует этого и отвергнет запрос.", n))
		}
	}
	for i, t := range targetsOf(ctx, cfg.Targets) {
		if !t.Weight.IsNull() && !t.Weight.IsUnknown() && t.Weight.ValueInt64() == 0 {
			resp.Diagnostics.AddError("Нулевой вес задан явно",
				fmt.Sprintf("У цели targets[%d] weight = 0. Ноль и означает «вес по умолчанию», "+
					"поэтому он задаётся ОПУСКАНИЕМ поля: два способа сказать одно и то же "+
					"разошлись бы между настройкой и состоянием.", i))
		}
		if n := t.identityCount(); n != 1 {
			resp.Diagnostics.AddError("Цель адресована не однозначно",
				fmt.Sprintf("У цели targets[%d] задано форм адресации: %d. Ровно одна из "+
					"instance_id, nic_id, ip_ref, external_ip обязана присутствовать.", i, n))
		}
	}
}

// ---- перевод модели в запрос края -------------------------------------------------

// toDuration — длительность из формы Go. Неизвестное и пустое дают nil: «не задал» и
// «ноль» — разные вещи, а ноль у окна вывода означает «снимать сразу».
func toDuration(v types.String, attr string) (*durationpb.Duration, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, nil
	}
	d, err := time.ParseDuration(v.ValueString())
	if err != nil {
		return nil, fmt.Errorf("%s=%q не разбирается как длительность (например 30s, 1m): %w",
			attr, v.ValueString(), err)
	}
	return durationpb.New(d), nil
}

// goDuration переводит длительность из формы protojson («30s», «0.500s») в форму Go.
//
// Без нормализации «30s» пользователя разошлось бы с «30s» края на первом же дробном
// значении: план сравнивает строки, а не длительности.
func goDuration(s string) string {
	if s == "" {
		return ""
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	return d.String()
}

func (p *httpProbe) options(ctx context.Context) (port int64, path, codes, host string, hdr map[string]string) {
	return p.Port.ValueInt64(), p.Path.ValueString(), p.ExpectedCodes.ValueString(),
		p.Host.ValueString(), mapFromTF(ctx, p.Headers)
}

func (h *healthModel) toProto(ctx context.Context) (*nlbv1.HealthCheck, error) {
	interval, err := toDuration(h.Interval, "health_check.interval")
	if err != nil {
		return nil, err
	}
	timeout, err := toDuration(h.Timeout, "health_check.timeout")
	if err != nil {
		return nil, err
	}
	hc := &nlbv1.HealthCheck{
		Interval: interval, Timeout: timeout,
		HealthyThreshold:   h.HealthyThreshold.ValueInt64(),
		UnhealthyThreshold: h.UnhealthyThreshold.ValueInt64(),
	}
	switch {
	case h.TCP != nil:
		hc.Options = &nlbv1.HealthCheck_Tcp{
			Tcp: &nlbv1.HealthCheck_TcpOptions{Port: h.TCP.Port.ValueInt64()}}
	case h.HTTP != nil:
		port, path, codes, host, hdr := h.HTTP.options(ctx)
		hc.Options = &nlbv1.HealthCheck_Http{Http: &nlbv1.HealthCheck_HttpOptions{
			Port: port, Path: path, ExpectedCodes: codes, Host: host, Headers: hdr}}
	case h.HTTPS != nil:
		port, path, codes, host, hdr := h.HTTPS.options(ctx)
		hc.Options = &nlbv1.HealthCheck_Https{Https: &nlbv1.HealthCheck_HttpsOptions{
			Port: port, Path: path, ExpectedCodes: codes, Host: host, Headers: hdr}}
	case h.GRPC != nil:
		hc.Options = &nlbv1.HealthCheck_Grpc{Grpc: &nlbv1.HealthCheck_GrpcOptions{
			Port: h.GRPC.Port.ValueInt64(), ServiceName: h.GRPC.ServiceName.ValueString()}}
	default:
		return nil, fmt.Errorf("health_check: не задана ни одна проба (tcp, http, https, grpc)")
	}
	return hc, nil
}

// targetsOf — цели из набора. Неизвестный и null дают nil: «не задал» и «задал пустым»
// различаются, и пустой массив вместо отсутствия стирал бы цели при каждом создании.
func targetsOf(ctx context.Context, s types.Set) []targetModel {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := make([]targetModel, 0, len(s.Elements()))
	_ = s.ElementsAs(ctx, &out, false)
	return out
}

func targetsToProto(ctx context.Context, set types.Set) []*nlbv1.Target {
	return targetsSliceToProto(targetsOf(ctx, set))
}

func targetsSliceToProto(ts []targetModel) []*nlbv1.Target {
	if ts == nil {
		return nil
	}
	out := make([]*nlbv1.Target, 0, len(ts))
	for _, t := range ts {
		// #nosec G115 -- диапазон веса закреплён контрактом края: 0-1000
		pt := &nlbv1.Target{Weight: int32(t.Weight.ValueInt64())}
		switch {
		case t.IPRef != nil:
			pt.Identity = &nlbv1.Target_IpRef{IpRef: &nlbv1.Target_InCloudIP{
				SubnetId: t.IPRef.SubnetID.ValueString(), Address: t.IPRef.Address.ValueString()}}
		case t.ExternalIP != nil:
			pt.Identity = &nlbv1.Target_ExternalIp{ExternalIp: &nlbv1.Target_ExternalIP{
				Address: t.ExternalIP.Address.ValueString(), ZoneId: t.ExternalIP.ZoneID.ValueString()}}
		case t.NicID.ValueString() != "":
			pt.Identity = &nlbv1.Target_NicId{NicId: t.NicID.ValueString()}
		default:
			pt.Identity = &nlbv1.Target_InstanceId{InstanceId: t.InstanceID.ValueString()}
		}
		out = append(out, pt)
	}
	return out
}

// ---- разбор ответа края -----------------------------------------------------------

// tgWire — форма ответа края. Разбирается своим типом, а не сгенерённым: сгенерённый
// потребовал бы разрешения google.protobuf.Any через глобальный реестр типов, которого у
// провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым — именно там опечатка в имени
// поля прошла бы молча.
type tgWire struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	RegionID    string            `json:"regionId"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Port        int64             `json:"port"`
	Status      string            `json:"status"`
	CreatedAt   string            `json:"createdAt"`
	DeregDelay  string            `json:"deregistrationDelay"`
	SlowStart   string            `json:"slowStart"`
	HealthCheck struct {
		Interval           string `json:"interval"`
		Timeout            string `json:"timeout"`
		HealthyThreshold   any    `json:"healthyThreshold"`
		UnhealthyThreshold any    `json:"unhealthyThreshold"`
		EffectivePort      int64  `json:"effectivePort"`
		TCP                *struct {
			Port any `json:"port"`
		} `json:"tcp"`
		HTTP  *httpWire `json:"http"`
		HTTPS *httpWire `json:"https"`
		GRPC  *struct {
			Port        any    `json:"port"`
			ServiceName string `json:"serviceName"`
		} `json:"grpc"`
	} `json:"healthCheck"`
	Targets []struct {
		InstanceID string `json:"instanceId"`
		NicID      string `json:"nicId"`
		IPRef      *struct {
			SubnetID string `json:"subnetId"`
			Address  string `json:"address"`
		} `json:"ipRef"`
		ExternalIP *struct {
			Address string `json:"address"`
			ZoneID  string `json:"zoneId"`
		} `json:"externalIp"`
		Weight int64 `json:"weight"`
	} `json:"targets"`
}

type httpWire struct {
	Port          any               `json:"port"`
	Path          string            `json:"path"`
	ExpectedCodes string            `json:"expectedCodes"`
	Host          string            `json:"host"`
	Headers       map[string]string `json:"headers"`
}

// numOf — целое из поля, которое protojson отдаёт то числом, то строкой: 64-разрядные
// целые он кодирует строкой, 32-разрядные — числом, и одна структура несёт оба вида.
func numOf(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func (w *httpWire) toModel(ctx context.Context) *httpProbe {
	return &httpProbe{
		Port: types.Int64Value(numOf(w.Port)), Path: types.StringValue(w.Path),
		ExpectedCodes: types.StringValue(w.ExpectedCodes), Host: types.StringValue(w.Host),
		Headers: mapToTF(ctx, w.Headers),
	}
}

func applyTargetGroup(ctx context.Context, m *targetGroupModel, raw []byte) error {
	var w tgWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}
	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.RegionID = types.StringValue(w.RegionID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.Port = types.Int64Value(w.Port)
	m.Status = types.StringValue(w.Status)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.DeregistrationDelay = types.StringValue(goDuration(w.DeregDelay))
	m.SlowStart = types.StringValue(goDuration(w.SlowStart))

	h := &healthModel{
		Interval:           types.StringValue(goDuration(w.HealthCheck.Interval)),
		Timeout:            types.StringValue(goDuration(w.HealthCheck.Timeout)),
		HealthyThreshold:   types.Int64Value(numOf(w.HealthCheck.HealthyThreshold)),
		UnhealthyThreshold: types.Int64Value(numOf(w.HealthCheck.UnhealthyThreshold)),
		EffectivePort:      types.Int64Value(w.HealthCheck.EffectivePort),
	}
	switch {
	case w.HealthCheck.TCP != nil:
		h.TCP = &tcpProbe{Port: types.Int64Value(numOf(w.HealthCheck.TCP.Port))}
	case w.HealthCheck.HTTP != nil:
		h.HTTP = w.HealthCheck.HTTP.toModel(ctx)
	case w.HealthCheck.HTTPS != nil:
		h.HTTPS = w.HealthCheck.HTTPS.toModel(ctx)
	case w.HealthCheck.GRPC != nil:
		h.GRPC = &grpcProbe{Port: types.Int64Value(numOf(w.HealthCheck.GRPC.Port)),
			ServiceName: types.StringValue(w.HealthCheck.GRPC.ServiceName)}
	}
	hcObj, diags := types.ObjectValueFrom(ctx, healthObjectType().(types.ObjectType).AttrTypes, h)
	if diags.HasError() {
		return fmt.Errorf("проверка живости края не укладывается в объект: %v", diags.Errors())
	}
	m.HealthCheck = hcObj

	if w.Targets == nil {
		m.Targets = types.SetNull(targetObjectType())
		return nil
	}
	ts := make([]targetModel, 0, len(w.Targets))
	for _, t := range w.Targets {
		tm := targetModel{
			InstanceID: strOrNull(t.InstanceID),
			NicID:      strOrNull(t.NicID),
			Weight:     intOrNull(t.Weight),
		}
		if t.IPRef != nil {
			tm.IPRef = &ipRefModel{SubnetID: types.StringValue(t.IPRef.SubnetID),
				Address: types.StringValue(t.IPRef.Address)}
		}
		if t.ExternalIP != nil {
			tm.ExternalIP = &extIPModel{Address: types.StringValue(t.ExternalIP.Address),
				ZoneID: types.StringValue(t.ExternalIP.ZoneID)}
		}
		ts = append(ts, tm)
	}
	set, diags := types.SetValueFrom(ctx, targetObjectType(), ts)
	if diags.HasError() {
		return fmt.Errorf("цели края не укладываются в набор: %v", diags.Errors())
	}
	m.Targets = set
	return nil
}

// strOrNull / intOrNull — нули края означают «поле не задано»: у цели ровно одна форма
// адресации, поэтому остальные приходят пустыми, а вес ноль и есть «по умолчанию».
// Записать их значением значило бы объявить пользователю то, чего он не писал, — и
// получить расхождение на следующем плане.
func strOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func intOrNull(v int64) types.Int64 {
	if v == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(v)
}

// targetObjectType — тип элемента набора целей. Объявлен ЗДЕСЬ и один раз: разойдись он
// со схемой хоть одним полем, набор молча не собрался бы, а цели пропали бы из состояния.
func targetObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"instance_id": types.StringType,
		"nic_id":      types.StringType,
		"ip_ref": types.ObjectType{AttrTypes: map[string]attr.Type{
			"subnet_id": types.StringType, "address": types.StringType}},
		"external_ip": types.ObjectType{AttrTypes: map[string]attr.Type{
			"address": types.StringType, "zone_id": types.StringType}},
		"weight": types.Int64Type,
	}}
}

// keepTargetsSpelling сохраняет написание состава из настройки, когда край вернул то же
// самое по СОДЕРЖАНИЮ.
//
// Пустой состав и отсутствие состава для края одно и то же — он всегда отвечает массивом,
// — но для Terraform это РАЗНЫЕ значения, и apply падает «был null, стал пустой набор» на
// совершенно законной настройке. Сохраняется именно написание вызывающего: он его выбрал,
// и менять его молча значит спорить с ним ни о чём.
func keepTargetsSpelling(ctx context.Context, want types.Set, m *targetGroupModel) {
	a := targetsOf(ctx, want)
	b := targetsOf(ctx, m.Targets)
	if len(a) != len(b) {
		return
	}
	keys := map[string]bool{}
	for _, t := range a {
		keys[targetKey(t)] = true
	}
	for _, t := range b {
		if !keys[targetKey(t)] {
			return
		}
	}
	m.Targets = want
}

// ---- CRUD -------------------------------------------------------------------------

func (r *targetGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan targetGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planHealth := healthOf(ctx, plan.HealthCheck)
	if planHealth == nil {
		resp.Diagnostics.AddError("Проверка живости не задана",
			"health_check обязателен: край не создаёт группу без пробы.")
		return
	}

	hc, err := planHealth.toProto(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Негодная проверка живости", err.Error())
		return
	}
	dereg, err := toDuration(plan.DeregistrationDelay, "deregistration_delay")
	if err != nil {
		resp.Diagnostics.AddError("Негодная длительность", err.Error())
		return
	}
	slow, err := toDuration(plan.SlowStart, "slow_start")
	if err != nil {
		resp.Diagnostics.AddError("Негодная длительность", err.Error())
		return
	}

	body := &nlbv1.CreateTargetGroupRequest{
		ProjectId:   plan.ProjectID.ValueString(),
		RegionId:    plan.RegionID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Labels:      mapFromTF(ctx, plan.Labels),
		// #nosec G115 -- диапазон порта закреплён контрактом края: 1-65535
		Port:                int32(plan.Port.ValueInt64()),
		HealthCheck:         hc,
		Targets:             targetsToProto(ctx, plan.Targets),
		DeregistrationDelay: dereg,
		SlowStart:           slow,
	}

	id, err := awaitCreate(ctx, r.c, targetGroupsPath, "targetGroupId", typeNameNLBTargetGroup,
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание группы целей не завершилось", err.Error())
		return
	}

	// Идентификатор записывается ДО обратного чтения: если чтение сорвётся, ресурс уже
	// создан, и без этой записи Terraform о нём не узнает никогда.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ ОДНОГО
	// неизвестного после apply, и без этого сорвавшееся чтение даёт шесть сообщений об
	// «invalid result object» вместо одного — про само чтение.
	sealUnknowns(&plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, targetGroupsPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Группа целей создана, но не прочитана обратно", err.Error())
		return
	}
	wantTargets := plan.Targets
	if err := applyTargetGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepTargetsSpelling(ctx, wantTargets, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *targetGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state targetGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := readByID(ctx, r.c, targetGroupsPath, state.ID.ValueString(), false)
	if err == nil {
		wantTargets := state.Targets
		if err := applyTargetGroup(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		keepTargetsSpelling(ctx, wantTargets, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение группы целей не удалось", err.Error())
		return
	}
	remove, title, detail := absenceDiagnostics(ctx, r.c, targetGroupsPath, client.ScopeProject,
		"Группа целей", state.ID.ValueString(), state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

func (r *targetGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state targetGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &nlbv1.UpdateTargetGroupRequest{TargetGroupId: state.ID.ValueString()}
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
	if !plan.Port.Equal(state.Port) {
		// #nosec G115 -- диапазон порта закреплён контрактом края: 1-65535
		body.Port = int32(plan.Port.ValueInt64())
		paths = append(paths, "port")
	}
	if !plan.DeregistrationDelay.Equal(state.DeregistrationDelay) {
		d, err := toDuration(plan.DeregistrationDelay, "deregistration_delay")
		if err != nil {
			resp.Diagnostics.AddError("Негодная длительность", err.Error())
			return
		}
		body.DeregistrationDelay = d
		paths = append(paths, "deregistration_delay")
	}
	if !plan.SlowStart.Equal(state.SlowStart) {
		d, err := toDuration(plan.SlowStart, "slow_start")
		if err != nil {
			resp.Diagnostics.AddError("Негодная длительность", err.Error())
			return
		}
		body.SlowStart = d
		paths = append(paths, "slow_start")
	}
	if !plan.HealthCheck.Equal(state.HealthCheck) {
		hc, err := healthOf(ctx, plan.HealthCheck).toProto(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Негодная проверка живости", err.Error())
			return
		}
		body.HealthCheck = hc
		paths = append(paths, "health_check")
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё
	// изменяемое», а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе в
	// состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch,
			targetGroupsPath+"/"+state.ID.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Изменение группы целей не завершилось", err.Error())
			return
		}
	}

	// Состав меняется СВОИМИ действиями края: он отвергает `targets` в маске изменения
	// явно («targets must be modified via AddTargets / RemoveTargets»), и у этих действий
	// отдельные права — их выдают независимо от права менять саму группу.
	composed := false
	if !plan.Targets.Equal(state.Targets) {
		if err := r.reconcileTargets(ctx, state.ID.ValueString(),
			targetsOf(ctx, state.Targets), targetsOf(ctx, plan.Targets)); err != nil {
			resp.Diagnostics.AddError("Состав целей не приведён к заданному", err.Error())
			return
		}
		composed = true
	}

	var raw []byte
	var err error
	if composed {
		raw, err = r.awaitTargets(ctx, state.ID.ValueString(), targetsOf(ctx, plan.Targets))
	} else {
		raw, err = readByID(ctx, r.c, targetGroupsPath, state.ID.ValueString(), false)
	}
	if err != nil {
		resp.Diagnostics.AddError("Группа целей изменена, но не прочитана обратно", err.Error())
		return
	}
	plan.ID = state.ID
	wantTargets := plan.Targets
	if err := applyTargetGroup(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	keepTargetsSpelling(ctx, wantTargets, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete сначала опустошает состав, потом снимает саму группу: край отказывается удалять
// непустую группу («TargetGroup has N target(s); remove them first»).
//
// Состав берётся ЧТЕНИЕМ у края, а не из состояния: состояние могло отстать, а снимать
// надо ровно то, что там лежит сейчас.
func (r *targetGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state targetGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	if raw, err := readByID(ctx, r.c, targetGroupsPath, id, false); err == nil {
		var cur targetGroupModel
		if err := applyTargetGroup(ctx, &cur, raw); err == nil {
			if have := targetsOf(ctx, cur.Targets); len(have) > 0 {
				err := r.removeTargets(ctx, id, have)
				if err == nil {
					_, err = r.awaitTargets(ctx, id, nil)
				}
				if err != nil {
					resp.Diagnostics.AddError("Цели не сняты перед удалением группы",
						err.Error()+"\n\nКрай не удаляет непустую группу. Если отказ говорит про "+
							"вывод целей — это ОКНО ВЫВОДА: цель помечается выводимой и снимается "+
							"лишь по истечении deregistration_delay (сейчас "+
							state.DeregistrationDelay.ValueString()+"). Для групп под управлением "+
							"Terraform ставьте `0s`.")
					return
				}
			}
		}
	}

	if err := awaitMutation(ctx, r.c, http.MethodDelete, targetGroupsPath+"/"+id, nil); err != nil {
		resp.Diagnostics.AddError("Удаление группы целей не завершилось", err.Error())
		return
	}
}

// targetKey — канонический ключ элемента: личность И вес. Вес входит намеренно —
// добавление на уже существующую личность идемпотентно и вес НЕ меняет (измерено), значит
// смена веса выражается снятием и добавлением, а не изменением цели: метода
// `TargetGroupService/UpdateTarget` у края нет.
func targetKey(t targetModel) string {
	switch {
	case t.IPRef != nil:
		return "ip|" + t.IPRef.SubnetID.ValueString() + "|" + t.IPRef.Address.ValueString() +
			"|" + strconv.FormatInt(t.Weight.ValueInt64(), 10)
	case t.ExternalIP != nil:
		return "ext|" + t.ExternalIP.Address.ValueString() + "|" + t.ExternalIP.ZoneID.ValueString() +
			"|" + strconv.FormatInt(t.Weight.ValueInt64(), 10)
	case t.NicID.ValueString() != "":
		return "nic|" + t.NicID.ValueString() + "|" + strconv.FormatInt(t.Weight.ValueInt64(), 10)
	default:
		return "ins|" + t.InstanceID.ValueString() + "|" + strconv.FormatInt(t.Weight.ValueInt64(), 10)
	}
}

// reconcileTargets приводит состав к заданному.
//
// Порядок — СНАЧАЛА снятие, потом добавление, и он не произволен: добавление на уже
// существующую личность край принимает как no-op, поэтому обратный порядок оставил бы
// прежний вес и молча разошёлся бы с настройкой. Цена порядка названа честно: цель, у
// которой меняется только вес, на короткое время выходит из группы.
func (r *targetGroupResource) reconcileTargets(ctx context.Context, id string, have, want []targetModel) error {
	inWant := map[string]bool{}
	for _, t := range want {
		inWant[targetKey(t)] = true
	}
	inHave := map[string]bool{}
	for _, t := range have {
		inHave[targetKey(t)] = true
	}

	var remove, add []targetModel
	for _, t := range have {
		if !inWant[targetKey(t)] {
			remove = append(remove, t)
		}
	}
	for _, t := range want {
		if !inHave[targetKey(t)] {
			add = append(add, t)
		}
	}

	if len(remove) > 0 {
		if err := r.removeTargets(ctx, id, remove); err != nil {
			return err
		}
	}
	if len(add) > 0 {
		if err := r.verbTargets(ctx, id, ":addTargets", add); err != nil {
			return err
		}
	}
	return nil
}

// awaitTargets читает группу, пока её состав не совпадёт с заданным.
//
// Ожидание НЕ вкусовое и не «на всякий случай»: замер на живом крае — операция снятия
// объявляется завершённой через ~0,2 с, а цель исчезает из чтения через ~1,3 с.
// `Operation.done` означает долговечность предмета мутации, а не видимость её следствия —
// это контракт платформы, а не дефект. Без этого ожидания обратное чтение отдаёт прежний
// состав, и Terraform останавливается на собственной несогласованности.
//
// Ожидание ОГРАНИЧЕНО и на исходе говорит, чего именно не дождалось: молчаливая сдача
// записала бы в состояние состав, которого у края нет.
func (r *targetGroupResource) awaitTargets(ctx context.Context, id string, want []targetModel) ([]byte, error) {
	wantKeys := map[string]bool{}
	for _, t := range want {
		wantKeys[targetKey(t)] = true
	}
	budget := r.targetsBudget
	if budget == 0 {
		budget = 30 * time.Second
	}
	deadline := time.Now().Add(budget)
	var last []targetModel
	for {
		raw, err := readByID(ctx, r.c, targetGroupsPath, id, false)
		if err != nil {
			return nil, err
		}
		var cur targetGroupModel
		if err := applyTargetGroup(ctx, &cur, raw); err != nil {
			return nil, err
		}
		last = targetsOf(ctx, cur.Targets)
		if len(last) == len(wantKeys) {
			ok := true
			for _, t := range last {
				if !wantKeys[targetKey(t)] {
					ok = false
					break
				}
			}
			if ok {
				return raw, nil
			}
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("состав целей не сошёлся за %s: задано %d, у края %d — "+
				"край принял действия состава, но чтение их пока не показывает. Повторите apply; "+
				"если расхождение держится, состав менял кто-то ещё", budget, len(wantKeys), len(last))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (r *targetGroupResource) removeTargets(ctx context.Context, id string, ts []targetModel) error {
	return r.verbTargets(ctx, id, ":removeTargets", ts)
}

func (r *targetGroupResource) verbTargets(ctx context.Context, id, verb string, ts []targetModel) error {
	body := &nlbv1.AddTargetsRequest{TargetGroupId: id, Targets: targetsSliceToProto(ts)}
	if verb == ":removeTargets" {
		rm := &nlbv1.RemoveTargetsRequest{TargetGroupId: id, Targets: targetsSliceToProto(ts)}
		return awaitMutation(ctx, r.c, http.MethodPost, targetGroupsPath+"/"+id+verb, rm)
	}
	return awaitMutation(ctx, r.c, http.MethodPost, targetGroupsPath+"/"+id+verb, body)
}

func (r *targetGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importByID(ctx, "группа целей", "tgr", req, resp)
}
