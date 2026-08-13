// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const machineTypesPath = "/compute/v1/machineTypes"

type machineTypeModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Family         types.String `tfsdk:"family"`
	Status         types.String `tfsdk:"status"`
	VCPU           types.Int64  `tfsdk:"vcpu"`
	MemoryMiB      types.Int64  `tfsdk:"memory_mib"`
	GPUs           types.Int64  `tfsdk:"gpus"`
	GPUType        types.String `tfsdk:"gpu_type"`
	AvailableZones types.List   `tfsdk:"available_zones"`
	Labels         types.Map    `tfsdk:"labels"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

type machineTypeDataSource struct{ c *client.Client }

// NewMachineTypeDataSource — конструктор для реестра провайдера.
func NewMachineTypeDataSource() datasource.DataSource { return &machineTypeDataSource{} }

func (d *machineTypeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_machine_type"
}

func (d *machineTypeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Внутренняя ошибка провайдера",
			fmt.Sprintf("ожидался *client.Client, получено %T", req.ProviderData))
		return
	}
	d.c = c
}

// ConfigValidators: искать можно ПО ОДНОМУ ключу — по имени каталога либо по
// идентификатору. Два заданных ключа означали бы вопрос, у которого может быть
// два разных ответа, и молчаливый выбор одного из них.
func (d *machineTypeDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		exactlyOneOf(
			path.Root("name"),
			path.Root("id"),
		),
	}
}

func (d *machineTypeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Тип машины из каталога размеров.\n\n" +
			"Существует, чтобы конфигурация называла размер ИМЕНЕМ (`std-v3-2`), а машине " +
			"доставался неизменяемый идентификатор. Каталог ведёт облако: имена в нём " +
			"стабильны и задокументированы, а идентификаторы у разных установок разные — " +
			"вписанный в конфигурацию идентификатор сделал бы её непереносимой.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор типа. Задайте ЛИБО его, ЛИБО `name`."},
			"name": schema.StringAttribute{Optional: true, Computed: true,
				MarkdownDescription: "Стабильное имя каталога, например `std-v3-2`. Задайте ЛИБО его, ЛИБО `id`."},
			"description": schema.StringAttribute{Computed: true},
			"family": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Класс типа: `STANDARD`, `COMPUTE`, `MEMORY`, `GPU`."},
			"status": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Пригодность к заказу: `AVAILABLE`, `DEPRECATED`, `RETIRED`. " +
					"На двух последних источник данных предупреждает — на плане, а не на применении."},
			"vcpu":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Число vCPU."},
			"memory_mib": schema.Int64Attribute{Computed: true, MarkdownDescription: "Память в МиБ (не в байтах)."},
			"gpus":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Число GPU (0 вне класса `GPU`)."},
			"gpu_type":   schema.StringAttribute{Computed: true, MarkdownDescription: "Модель GPU (пусто вне класса `GPU`)."},
			"available_zones": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Зоны, где тип заказуем. Подсказка о ёмкости, а не обещание: " +
					"годится для `precondition`, сверяющей зону машины с этим перечнем."},
			"labels":     schema.MapAttribute{Computed: true, ElementType: types.StringType},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *machineTypeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg machineTypeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var mt *machineTypeJSON
	var err error
	if id := cfg.ID.ValueString(); id != "" {
		mt, err = d.getByID(ctx, id)
	} else {
		mt, err = d.getByName(ctx, cfg.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Тип машины не разрешён", err.Error())
		return
	}

	// Предупреждение, а не отказ: прочитать снятый с продажи тип нужно как раз тому,
	// кто с него уезжает. Отказ здесь запер бы его в конфигурации, которую нельзя ни
	// применить, ни разобрать. Предупреждение же видно НА ПЛАНЕ — то есть до того, как
	// применение упрётся в отказ края при создании машины.
	switch mt.Status {
	case "RETIRED":
		resp.Diagnostics.AddWarning("Тип машины снят с продажи",
			"Тип "+mt.Name+" ("+mt.ID+") имеет состояние RETIRED: край откажет в создании машины "+
				"с этим типом. Читать его атрибуты можно — заказать нельзя.")
	case "DEPRECATED":
		resp.Diagnostics.AddWarning("Тип машины устаревает",
			"Тип "+mt.Name+" ("+mt.ID+") имеет состояние DEPRECATED: он ещё заказуем, но перестанет "+
				"им быть. Запланируйте переход на действующий тип.")
	}

	applyMachineType(ctx, &cfg, mt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

func (d *machineTypeDataSource) getByID(ctx context.Context, id string) (*machineTypeJSON, error) {
	httpResp, err := d.c.Do(ctx, http.MethodGet, machineTypesPath+"/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	switch out := client.Classify(httpResp); out.Kind {
	case client.OutcomeOK:
		var mt machineTypeJSON
		if err := json.Unmarshal(httpResp.Body, &mt); err != nil {
			return nil, fmt.Errorf("разбор типа машины: %w", err)
		}
		return &mt, nil
	case client.OutcomeNotFound:
		return nil, fmt.Errorf("в каталоге нет типа с идентификатором %q. "+
			"Идентификаторы у разных установок разные — если конфигурация переносится, "+
			"адресуйте тип по name", id)
	default:
		return nil, fmt.Errorf("чтение типа машины %s: %s", id, out.Message)
	}
}

// getByName разрешает имя каталога в запись.
//
// Ноль и больше одного — РАЗНЫЕ отказы, и оба отказы: молча взять первый из
// нескольких значило бы выбрать размер машины за арендатора, а сходство имён —
// ровно тот случай, когда выбор не безразличен.
func (d *machineTypeDataSource) getByName(ctx context.Context, name string) (*machineTypeJSON, error) {
	q := url.Values{}
	q.Set("name", name)
	q.Set("pageSize", "100")

	httpResp, err := d.c.Do(ctx, http.MethodGet, machineTypesPath+"?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	out := client.Classify(httpResp)
	if out.Kind != client.OutcomeOK {
		return nil, fmt.Errorf("список типов машин: %s", out.Message)
	}

	var page struct {
		MachineTypes []machineTypeJSON `json:"machineTypes"`
	}
	if err := json.Unmarshal(httpResp.Body, &page); err != nil {
		return nil, fmt.Errorf("разбор списка типов машин: %w", err)
	}

	// Фильтр края — по точному имени, но полагаться на это молча нельзя: ослабнет
	// фильтр — и источник данных начнёт отдавать не тот размер, ничего не сказав.
	// Поэтому имя сверяется здесь ещё раз.
	var exact []machineTypeJSON
	for _, mt := range page.MachineTypes {
		if mt.Name == name {
			exact = append(exact, mt)
		}
	}

	switch len(exact) {
	case 1:
		return &exact[0], nil
	case 0:
		return nil, fmt.Errorf("в каталоге нет типа машины с именем %q "+
			"(просмотрено записей на странице: %d). Перечень действующих типов — "+
			"GET %s", name, len(page.MachineTypes), machineTypesPath)
	default:
		return nil, fmt.Errorf("имени %q отвечает %d записей каталога — выбрать за вас нельзя. "+
			"Адресуйте тип по id", name, len(exact))
	}
}

type machineTypeJSON struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Family             string            `json:"family"`
	Status             string            `json:"status"`
	AvailableZones     []string          `json:"availableZones"`
	Labels             map[string]string `json:"labels"`
	CreatedAt          string            `json:"createdAt"`
	EffectiveResources *struct {
		VCPU      flexInt64 `json:"vCpu"`
		MemoryMiB flexInt64 `json:"memoryMib"`
		GPUs      flexInt64 `json:"gpus"`
		GPUType   string    `json:"gpuType"`
	} `json:"effectiveResources"`
}

// applyMachineType переносит запись каталога в состояние.
//
// Размер приходит вложенным и НЕОБЯЗАТЕЛЬНЫМ: край вправе его не заполнить, и
// разыменование без проверки уронило бы провайдер там, где корректный ответ
// означает всего лишь «размер не объявлен».
//
// Про форму 64-битного целого на проводе — см. flexInt64 в helpers.go.
func applyMachineType(ctx context.Context, m *machineTypeModel, mt *machineTypeJSON) {
	m.ID = types.StringValue(mt.ID)
	m.Name = types.StringValue(mt.Name)
	m.Description = types.StringValue(mt.Description)
	m.Family = types.StringValue(mt.Family)
	m.Status = types.StringValue(mt.Status)
	m.AvailableZones = listFromStrings(ctx, mt.AvailableZones)
	m.Labels = mapToTF(ctx, mt.Labels)
	m.CreatedAt = types.StringValue(mt.CreatedAt)

	if r := mt.EffectiveResources; r != nil {
		m.VCPU = types.Int64Value(int64(r.VCPU))
		m.MemoryMiB = types.Int64Value(int64(r.MemoryMiB))
		m.GPUs = types.Int64Value(int64(r.GPUs))
		m.GPUType = types.StringValue(r.GPUType)
		return
	}
	m.VCPU = types.Int64Value(0)
	m.MemoryMiB = types.Int64Value(0)
	m.GPUs = types.Int64Value(0)
	m.GPUType = types.StringValue("")
}
