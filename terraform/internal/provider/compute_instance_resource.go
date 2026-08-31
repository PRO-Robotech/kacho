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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const (
	instancesPath    = "/compute/v1/instances"
	machineTypesPath = "/compute/v1/machineTypes"
)

// Машина — ресурс, у которого запрос создания ШИРЕ того, чем машину заводят.
//
// Контракт создания несёт больше десяти вложенных спецификаций. Провайдер выражает
// НЕ ВСЕ — и это не сокращение объёма работы, а выбор: необъявленное поле честнее
// объявленного и молча непримененного. Что не объявлено и почему — в описании схемы
// ниже (раздел «Чего в этом ресурсе НЕТ») и в комментариях у каждого решения.
//
// Три свойства края определяют форму ресурса, и каждое стоит назвать до кода:
//
//  1. ЧАСТЬ ВХОДА КРАЙ НЕ ЭХАЕТ. Ответ ресурса не содержит ни `hostname`, ни
//     спецификаций интерфейсов, ни трёх флагов запуска: они читаются краем на
//     создании и в проекцию машины не попадают. Значит их значение в состоянии
//     берётся ИЗ НАСТРОЙКИ и обратным чтением не трогается, а импорт ресурса
//     невозможен (см. конец файла — там сказано почему, и это не пропуск).
//
//  2. ЧАСТЬ ВХОДА НЕИЗМЕНЯЕМА. Маска изменения края несёт восемь полей; всё
//     остальное меняется только пересозданием, и провайдер обязан сказать это
//     пересозданием, а не сделать вид, что правка применилась.
//
//  3. ИМЯ ТИПА МАШИНЫ ПРИХОДИТ ОБРАТНО ДРУГИМ. Край принимает и слаг `mt-…`, и
//     стабильное имя типа, а эхает ВСЕГДА канонический слаг. Написание вызывающего
//     сохраняется (instKeepMachineTypeSpelling) — иначе имя в настройке давало бы
//     вечное расхождение плана, а попытка его закрыть упиралась бы в отказ края
//     «машина обязана быть остановлена».
type computeInstanceModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	ZoneID      types.String `tfsdk:"zone_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
	Hostname    types.String `tfsdk:"hostname"`

	InstanceKind        types.String `tfsdk:"instance_kind"`
	MachineTypeID       types.String `tfsdk:"machine_type_id"`
	CPUGuaranteePercent types.Int64  `tfsdk:"cpu_guarantee_percent"`
	ServiceAccountID    types.String `tfsdk:"service_account_id"`
	PlacementGroupID    types.String `tfsdk:"placement_group_id"`
	GuestAccessKeyIDs   types.List   `tfsdk:"guest_access_key_ids"`

	// Вложенные объекты — types.Object, а НЕ указатели на структуры: указатель
	// неспособен держать НЕИЗВЕСТНОЕ значение, а источник загрузки и спецификацию
	// вызывающий вправе собрать из выражения, которое в момент плана ещё не
	// вычислено. На указателе провайдер отвечал бы «Value Conversion Error… target
	// type cannot handle unknown values» — отказом, из которого не видно ни поля,
	// ни причины.
	BootSource    types.Object `tfsdk:"boot_source"`
	VMSpec        types.Object `tfsdk:"vm_spec"`
	ContainerSpec types.Object `tfsdk:"container_spec"`

	NetworkInterfaceSpecs  types.List `tfsdk:"network_interface_specs"`
	UseDefaultNetwork      types.Bool `tfsdk:"use_default_network"`
	AssignExternalAddress  types.Bool `tfsdk:"assign_external_address"`
	AcknowledgeUnreachable types.Bool `tfsdk:"acknowledge_unreachable"`

	CreatedAt          types.String `tfsdk:"created_at"`
	Status             types.String `tfsdk:"status"`
	StatusReason       types.String `tfsdk:"status_reason"`
	FQDN               types.String `tfsdk:"fqdn"`
	EffectiveResources types.Object `tfsdk:"effective_resources"`
	NetworkInterfaces  types.List   `tfsdk:"network_interfaces"`
}

// instBootSourceModel — единственный вход ОС. На вход край принимает ровно два поля;
// остальные четыре у него output-only и на входе дают отказ по имени.
type instBootSourceModel struct {
	Type types.String `tfsdk:"type"`
	ID   types.String `tfsdk:"id"`
}

type instVMSpecModel struct {
	UserData        types.String `tfsdk:"user_data"`
	MetadataOptions types.Object `tfsdk:"metadata_options"`
}

// Ручки «требовать ли сеансовый ключ» здесь нет намеренно: поле СНЯТО С КОНТРАКТА
// (`instance.proto`, номер 8 и имя зарезервированы), потому что ключ обязателен by
// construction. Держать атрибут, который край не читает, значило бы обещать
// пользователю управление, которого нет: план сходился бы, а поведение не менялось.
type instMetadataOptionsModel struct {
	MetadataEndpoint types.String `tfsdk:"metadata_endpoint"`
}

type instContainerSpecModel struct {
	Command       types.List   `tfsdk:"command"`
	Args          types.List   `tfsdk:"args"`
	Env           types.Map    `tfsdk:"env"`
	WorkingDir    types.String `tfsdk:"working_dir"`
	Ports         types.List   `tfsdk:"ports"`
	RestartPolicy types.String `tfsdk:"restart_policy"`
}

type instContainerPortModel struct {
	ContainerPort types.Int64  `tfsdk:"container_port"`
	Protocol      types.String `tfsdk:"protocol"`
}

// instNICSpecModel — заказ интерфейса на запуске. Край читает у него РОВНО два поля;
// остальные четыре (адреса v4/v6, индекс, ссылка на готовый интерфейс) он отвергает
// синхронно по имени, поэтому их здесь нет: объявить их значило бы предложить выбор,
// который край не примет.
type instNICSpecModel struct {
	SubnetID         types.String `tfsdk:"subnet_id"`
	SecurityGroupIDs types.List   `tfsdk:"security_group_ids"`
}

// instNICMirrorModel — то, что у машины ФАКТИЧЕСКИ подключено. Ведёт этот список не
// compute, а владелец интерфейсов; здесь он только зеркало и только на чтение.
type instNICMirrorModel struct {
	Index            types.String `tfsdk:"index"`
	NICID            types.String `tfsdk:"nic_id"`
	MACAddress       types.String `tfsdk:"mac_address"`
	SubnetID         types.String `tfsdk:"subnet_id"`
	SecurityGroupIDs types.List   `tfsdk:"security_group_ids"`
	PrimaryV4Address types.Object `tfsdk:"primary_v4_address"`
	PrimaryV6Address types.Object `tfsdk:"primary_v6_address"`
}

type computeInstanceResource struct{ c *client.Client }

// NewComputeInstanceResource — конструктор для реестра провайдера.
func NewComputeInstanceResource() resource.Resource { return &computeInstanceResource{} }

func (r *computeInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compute_instance"
}

func (r *computeInstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ---- типы значений ----------------------------------------------------------------
//
// Каждый тип объявлен ОДИН раз и здесь. Разойдись он со схемой хоть одним полем, объект
// молча не собрался бы, и содержимое пропало бы из состояния — то есть машина выглядела
// бы заведённой без источника загрузки.

func instBootSourceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
	}
}

func instMetadataOptionsAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"metadata_endpoint": types.StringType,
	}
}

func instVMSpecAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"user_data":        types.StringType,
		"metadata_options": types.ObjectType{AttrTypes: instMetadataOptionsAttrTypes()},
	}
}

func instContainerPortAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"container_port": types.Int64Type,
		"protocol":       types.StringType,
	}
}

func instContainerSpecAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"command":        types.ListType{ElemType: types.StringType},
		"args":           types.ListType{ElemType: types.StringType},
		"env":            types.MapType{ElemType: types.StringType},
		"working_dir":    types.StringType,
		"ports":          types.ListType{ElemType: types.ObjectType{AttrTypes: instContainerPortAttrTypes()}},
		"restart_policy": types.StringType,
	}
}

func instEffectiveResourcesAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"v_cpu":      types.Int64Type,
		"memory_mib": types.Int64Type,
		"gpus":       types.Int64Type,
		"gpu_type":   types.StringType,
	}
}

func instAddressAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"address": types.StringType}
}

// instNICMirrorObjectType — тип элемента ЗЕРКАЛА интерфейсов.
//
// Парного типа для ЗАКАЗА интерфейса здесь нет намеренно: заказ край не эхает,
// поэтому собирать его из ответа не из чего — его значение приходит из настройки и
// обратным чтением не трогается.
func instNICMirrorObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"index":              types.StringType,
		"nic_id":             types.StringType,
		"mac_address":        types.StringType,
		"subnet_id":          types.StringType,
		"security_group_ids": types.ListType{ElemType: types.StringType},
		"primary_v4_address": types.ObjectType{AttrTypes: instAddressAttrTypes()},
		"primary_v6_address": types.ObjectType{AttrTypes: instAddressAttrTypes()},
	}}
}

// ---- схема ---------------------------------------------------------------------------

func (r *computeInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: instanceResourceDescription,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Неизменяемый идентификатор машины (`ins-…`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},

			"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Проект-владелец. Изменение пересоздаёт машину."},
			"zone_id": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Зона машины.\n\n" +
					"Зона **неизменяема**: край отвечает на неё в маске изменения " +
					"`zoneId is immutable after Instance.Create`, поэтому правка пересоздаёт " +
					"машину. Переноса живой машины в другую зону у края нет вовсе: действия " +
					"`:relocate` контракт не объявляет, и другого способа сменить зону, кроме " +
					"пересоздания, не существует."},
			"name": schema.StringAttribute{Required: true,
				MarkdownDescription: "Имя машины в пределах проекта.\n\n" +
					"Провайдер требует его СТРОЖЕ края: край допускает машину без имени, а " +
					"провайдеру имя — единственный способ подтвердить отсутствие ресурса, " +
					"которого не видно по идентификатору (по имени идёт контрольный список " +
					"проекта). Без имени пропавшую машину нельзя было бы отличить от " +
					"отозванного доступа."},
			"description": schema.StringAttribute{Optional: true, Computed: true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Произвольное описание, до 256 знаков."},
			"labels": schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Метки вида ключ-значение."},

			"hostname": schema.StringAttribute{Optional: true, PlanModifiers: replace,
				MarkdownDescription: "Имя хоста, из которого край выводит `fqdn`.\n\n" +
					"Край это поле **не эхает**: в проекции машины его нет — есть только " +
					"производный `fqdn`. Поэтому значение в состоянии берётся из настройки, " +
					"а не подтверждается чтением, и изменение пересоздаёт машину " +
					"(изменяющей операции у имени хоста нет)."},

			"instance_kind": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Род машины: `VM` или `CONTAINER`. Неизменяем после создания.\n\n" +
					"Это **первый различитель** контракта, и он задаётся отдельно от " +
					"спецификации намеренно — см. раздел «Род машины и спецификация» в " +
					"описании ресурса."},
			"machine_type_id": schema.StringAttribute{Required: true,
				MarkdownDescription: "Тип машины — единственный канал размера (число ядер, " +
					"память, ускорители). Принимается **слаг** `mt-…` либо **стабильное имя** " +
					"типа из каталога `/compute/v1/machineTypes`.\n\n" +
					"Край отвечает всегда слагом. Написание, выбранное здесь, провайдер " +
					"сохраняет: если задано имя, он спрашивает у каталога имя того типа, " +
					"который край назвал в ответе, и при совпадении оставляет имя. Иначе " +
					"каждый план предлагал бы правку, которую применить нельзя.\n\n" +
					":::warning Смена типа требует ОСТАНОВЛЕННОЙ машины\n" +
					"Размер меняется только у остановленной машины; на работающей край " +
					"отвечает `instance must be STOPPED to change sizing or placement`. " +
					"Провайдер **не** пересоздаёт машину ради смены размера — это уничтожило " +
					"бы её; он отправляет изменение и передаёт отказ края как есть.\n:::"},
			"cpu_guarantee_percent": schema.Int64Attribute{Optional: true, Computed: true,
				Default: int64default.StaticInt64(0),
				MarkdownDescription: "Гарантированная доля процессора на каждое ядро, `0`–`100`. " +
					"`0` — без гарантии (машина берёт то, что свободно).\n\n" +
					"Значение действует у семейств типов `STANDARD`, `COMPUTE`, `MEMORY`; у " +
					"семейства `GPU` край его принимает и не применяет — это его решение, " +
					"названное в контракте, и провайдер о нём предупреждает, а не скрывает.\n\n" +
					"Меняется на тех же условиях, что и тип машины: только у остановленной."},
			"service_account_id": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Служебная учётка, от имени которой работает машина " +
					"(`sva-…`). Меняется у работающей машины.\n\n" +
					"Ссылка мягкая: удаление учётки машину не роняет — связь просто " +
					"перестаёт разрешаться."},
			"placement_group_id": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Группа размещения. Её якорь обязан быть когерентен " +
					"зоне машины: зональная группа — та же зона, региональная — тот же регион."},
			"guest_access_key_ids": schema.ListAttribute{Optional: true, Computed: true,
				ElementType: types.StringType,
				MarkdownDescription: "Ключи входа гостя — ссылками по неизменяемому " +
					"идентификатору. Набор ЗАМЕНЯЕТСЯ целиком; ключ обязан принадлежать тому же " +
					"проекту. Материал ключа сюда не кладётся: ключ — отдельный ресурс со своим " +
					"сроком жизни."},

			"boot_source": schema.SingleNestedAttribute{Required: true,
				MarkdownDescription: "Источник загрузки — единственный вход операционной " +
					"системы. **Неизменяем**: край отвечает на него в маске изменения " +
					"`bootSource is immutable after Instance.Create`, поэтому правка " +
					"пересоздаёт машину.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{Required: true,
						MarkdownDescription: "Кому принадлежит образ: `storage.image` (образ " +
							"диска) или `registry.image` (образ OCI). Перечень ведёт край; " +
							"своей копии провайдер не держит — негодное значение край " +
							"отвергает синхронно и называет поле, а вторая копия разошлась " +
							"бы с первой молча."},
					"id": schema.StringAttribute{Required: true,
						MarkdownDescription: "Идентификатор образа **вместе с тегом или " +
							"отпечатком**: `img-<base32>:<тег>`, " +
							"`img-<base32>@sha256:<hex>` или `репозиторий/имя:тег`. " +
							"Голый идентификатор без тега край отвергает: он не определяет, " +
							"что именно загружать."},
				}},

			"vm_spec": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Настройка машины рода `VM`. Задаётся только при " +
					"`instance_kind = VM`.\n\n" +
					"Изменяется у живой машины, но **вступает в силу при следующей загрузке** — " +
					"край сообщает это в `status_reason`.",
				Attributes: map[string]schema.Attribute{
					"user_data": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Данные первичной настройки (cloud-config / cloud-init)."},
					"metadata_options": schema.SingleNestedAttribute{Optional: true,
						MarkdownDescription: "Доступ гостя к службе метаданных.",
						Attributes: map[string]schema.Attribute{
							"metadata_endpoint": schema.StringAttribute{Optional: true,
								MarkdownDescription: "Доступна ли служба метаданных изнутри " +
									"машины: `ENABLED` или `DISABLED`."},
						}},
				}},

			"container_spec": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Настройка машины рода `CONTAINER`. Задаётся только при " +
					"`instance_kind = CONTAINER`.\n\n" +
					"**Неизменяема**: в маске изменения края этого поля нет вовсе, поэтому " +
					"правка пересоздаёт машину. Код возврата задания здесь не объявлен — " +
					"край его сегодня не производит ни в одном состоянии.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"command": schema.ListAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Точка входа. Пусто — точка входа образа."},
					"args": schema.ListAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Аргументы точки входа."},
					"env": schema.MapAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Переменные окружения."},
					"working_dir": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Рабочий каталог."},
					"ports": schema.ListNestedAttribute{Optional: true,
						MarkdownDescription: "Объявленные порты контейнера.",
						NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
							"container_port": schema.Int64Attribute{Required: true,
								MarkdownDescription: "Номер порта."},
							"protocol": schema.StringAttribute{Optional: true,
								MarkdownDescription: "`TCP` или `UDP`. Пусто — `TCP`."},
						}}},
					"restart_policy": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Когда перезапускать задание: `NEVER`, " +
							"`ON_FAILURE`, `ALWAYS`. Пусто — `NEVER`."},
				}},

			"network_interface_specs": schema.ListNestedAttribute{Optional: true,
				MarkdownDescription: "Интерфейсы, которые край заводит машине на запуске. " +
					"**Список, а не набор**: порядок задаёт номера гнёзд (`eth0`, `eth1`, …).\n\n" +
					"Обязательно одно из двух: этот список **или** `use_default_network`.\n\n" +
					"Край это поле **не эхает** — фактически подключённое видно в " +
					"`network_interfaces`. Значение здесь берётся из настройки, а изменение " +
					"пересоздаёт машину: изменяющей операции у заказа интерфейсов нет " +
					"(подключение и отключение готового интерфейса — отдельные действия края, " +
					"и провайдер их не выражает).",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				// Ни одного Computed и ни одного значения по умолчанию ВНУТРИ элемента.
				// Отсутствие выражается null: подстановка умолчания сделала бы значение
				// элемента другим, чем его написал вызывающий, и расхождение всплыло бы
				// на следующем же плане.
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"subnet_id": schema.StringAttribute{Required: true,
						MarkdownDescription: "Подсеть интерфейса. Обязательна — другого способа " +
							"назвать интерфейс у края нет.\n\n" +
							"**Зона подсети обязана совпадать с зоной машины.** Проверить это " +
							"здесь нечем: подсеть принадлежит другому сервису, и её зону знает " +
							"только он. Проверку делает край на пути запроса, обращаясь к " +
							"владельцу подсети, и отвечает `NetworkInterface subnet is in zone " +
							"<A>, instance zone is <B>`. Исключение — **региональная** подсеть: " +
							"зоны она не несёт, поэтому сверяется регион, и отказ звучит как " +
							"`NetworkInterface subnet must be in the same region as the instance`."},
					"security_group_ids": schema.ListAttribute{Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Группы безопасности интерфейса."},
				}}},

			"use_default_network": schema.BoolAttribute{Optional: true,
				PlanModifiers: []planmodifier.Bool{boolRequiresReplace{}},
				MarkdownDescription: "Взять подсеть и группу безопасности проекта по умолчанию " +
					"вместо `network_interface_specs`. Одно из двух обязательно.\n\n" +
					"Край это поле не эхает; изменение пересоздаёт машину."},

			"assign_external_address": schema.BoolAttribute{Optional: true,
				PlanModifiers: []planmodifier.Bool{boolRequiresReplace{}},
				MarkdownDescription: "Заказать машине внешний адрес.\n\n" +
					"Для рода `VM` обязательно одно из двух: этот признак **или** " +
					"`acknowledge_unreachable`. Иначе край отказывает: " +
					"`VM will be RUNNING but unreachable (no external address)`. Это страж, " +
					"а не косметика — он не даёт завести машину, до которой некому достучаться, " +
					"молча.\n\n" +
					"Край это поле не эхает; изменение пересоздаёт машину."},
			"acknowledge_unreachable": schema.BoolAttribute{Optional: true,
				PlanModifiers: []planmodifier.Bool{boolRequiresReplace{}},
				MarkdownDescription: "Подтвердить, что машина будет работать без внешнего " +
					"адреса и снаружи недостижима. Снимает страж, описанный у " +
					"`assign_external_address`. Роду `CONTAINER` не нужен.\n\n" +
					"Край это поле не эхает; изменение пересоздаёт машину."},

			"created_at": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Момент создания по данным края."},
			"status": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Состояние машины у края: `PROVISIONING`, `RUNNING`, " +
					"`STOPPING`, `STOPPED`, `STARTING`, `RESTARTING`, `UPDATING`, `ERROR`, " +
					"`CRASHED`, `DELETING`.\n\n" +
					":::note Только чтение — и это следствие, а не упущение\n" +
					"Провайдер не выражает пуск, остановку и перезапуск (почему — в разделе " +
					"«Жизненный цикл» описания ресурса). Значит состояние здесь **наблюдается, " +
					"а не задаётся**: расхождение по нему ничего не запускает и не " +
					"останавливает, и машина, остановленная мимо Terraform, обратно им не " +
					"поднимется. Именно поэтому поле вычисляемое: желаемым состоянием оно " +
					"быть не может.\n:::"},
			"status_reason": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Пояснение края к состоянию. Сюда же попадает отметка " +
					"об отложенном изменении — `takes effect on next boot`."},
			"fqdn": schema.StringAttribute{Computed: true,
				MarkdownDescription: "Доменное имя машины, выведенное краем из `hostname`."},

			"effective_resources": schema.SingleNestedAttribute{Computed: true,
				MarkdownDescription: "Размер, который край разрешил по выбранному типу машины. " +
					"Только чтение: источник истины — каталог типов.",
				Attributes: map[string]schema.Attribute{
					"v_cpu":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Число vCPU."},
					"memory_mib": schema.Int64Attribute{Computed: true, MarkdownDescription: "Память в МиБ (не в байтах)."},
					"gpus":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Число ускорителей."},
					"gpu_type":   schema.StringAttribute{Computed: true, MarkdownDescription: "Модель ускорителя."},
				}},

			"network_interfaces": schema.ListNestedAttribute{Computed: true,
				MarkdownDescription: "Что у машины подключено ФАКТИЧЕСКИ. Только чтение — " +
					"список ведёт владелец интерфейсов, край лишь зеркалит его.\n\n" +
					"Это единственное свидетельство того, доехал ли заказ из " +
					"`network_interface_specs`: пока сага запуска не материализовала " +
					"интерфейсы, список пуст, хотя заказ принят и проверен.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"index":       schema.StringAttribute{Computed: true, MarkdownDescription: "Номер гнезда."},
					"nic_id":      schema.StringAttribute{Computed: true, MarkdownDescription: "Идентификатор интерфейса."},
					"mac_address": schema.StringAttribute{Computed: true, MarkdownDescription: "Аппаратный адрес."},
					"subnet_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Подсеть интерфейса."},
					"security_group_ids": schema.ListAttribute{Computed: true, ElementType: types.StringType,
						MarkdownDescription: "Группы безопасности интерфейса."},
					"primary_v4_address": schema.SingleNestedAttribute{Computed: true,
						MarkdownDescription: "Основной адрес IPv4.",
						Attributes: map[string]schema.Attribute{
							"address": schema.StringAttribute{Computed: true},
						}},
					"primary_v6_address": schema.SingleNestedAttribute{Computed: true,
						MarkdownDescription: "Основной адрес IPv6.",
						Attributes: map[string]schema.Attribute{
							"address": schema.StringAttribute{Computed: true},
						}},
				}}},
		},
	}
}

// instanceResourceDescription — описание ресурса. Вынесено в константу, потому что
// половина его — это перечень того, чего в ресурсе НЕТ, с причиной по каждому пункту:
// такой перечень обязан читаться целиком, а не выуживаться из схемы.
const instanceResourceDescription = "Машина Kachō — виртуальная машина или контейнерное задание.\n\n" +
	"## Род машины и спецификация\n\n" +
	"Контракт держит **три** связанных поля: `instance_kind`, `vm_spec` и " +
	"`container_spec`. Отношение между ними такое:\n\n" +
	"* `instance_kind` — **обязателен** и неизменяем; это первый различитель, " +
	"  от которого зависит весь жизненный цикл машины;\n" +
	"* спецификация — **необязательна**: и машина без `vm_spec` (без данных первичной " +
	"  настройки), и задание без `container_spec` (с точкой входа самого образа) — " +
	"  законные, обычные случаи;\n" +
	"* заданная спецификация обязана **совпадать** с родом: `container_spec` при " +
	"  `VM` и `vm_spec` при `CONTAINER` край отвергает по имени поля.\n\n" +
	"Отсюда ответ на напрашивающийся вопрос: **одним взаимоисключающим блоком выбор " +
	"НЕ выражается**. Вывести род из того, какой блок задан, значило бы сделать " +
	"невыразимой машину без спецификации — а это как раз частый случай; подставлять " +
	"же пустой блок за вызывающего значит писать за него то, чего он не писал. " +
	"Поэтому род объявлен отдельным полем, а несовпадающую пару провайдер отвергает " +
	"на проверке настройки — до обращения к краю.\n\n" +
	"## Жизненный цикл: почему нет пуска, остановки и перезапуска\n\n" +
	"У края есть действия `:start`, `:stop`, `:restart` — и провайдер их **не " +
	"выражает**. Terraform описывает желаемое состояние, а не команды: конфигурация " +
	"говорит, какой машина должна БЫТЬ, и повторное применение неизменной " +
	"конфигурации обязано ничего не делать. Команда в эту форму не укладывается — " +
	"«перезапусти» невозможно ни описать состоянием, ни применить повторно без " +
	"последствий.\n\n" +
	"Из этого следует и вид поля `status`: оно **вычисляемое, только на чтение**. " +
	"Состояние машины меняется само (запуск, авария, перезапуск краем), поэтому " +
	"желаемым состоянием оно быть не может; расхождение по нему ничего не " +
	"инициирует. Машину, остановленную мимо Terraform, `apply` обратно не поднимет — " +
	"он лишь запишет `STOPPED` в состояние. Пускать и останавливать машины следует " +
	"тем, чем это и является: командой — из консоли или вызовом края.\n\n" +
	":::warning Побочное следствие для смены размера\n" +
	"Тип машины и гарантированная доля процессора меняются краем только у " +
	"**остановленной** машины. Остановить её через Terraform нечем, поэтому такая " +
	"правка отказывает (`instance must be STOPPED to change sizing or placement`) до " +
	"тех пор, пока машину не остановят снаружи. Пересоздавать машину ради смены " +
	"размера провайдер не станет: это уничтожение данных, и решать его за владельца " +
	"нельзя.\n:::\n\n" +
	"## Когерентность размещения\n\n" +
	"Зона машины и зона подсети **каждого** её интерфейса обязаны совпадать. " +
	"Исключение — региональная (эникаст) подсеть: зоны она не несёт, для неё " +
	"сверяется регион.\n\n" +
	"Проверить это провайдером **нельзя**: подсеть принадлежит другому сервису, и её " +
	"зону знает только он. Отвергает несовпадение **край**, на пути запроса, " +
	"обращаясь к владельцу подсети:\n\n" +
	"* `NetworkInterface subnet is in zone <A>, instance zone is <B>` — зональная подсеть;\n" +
	"* `NetworkInterface subnet must be in the same region as the instance` — региональная.\n\n" +
	"Отказ приходит до того, как машина станет долговечной, поэтому «полумашины» с " +
	"чужезонным интерфейсом не возникает.\n\n" +
	"## Импорт не поддерживается\n\n" +
	"Существующую машину под управление Terraform взять нельзя, и это решение, а не " +
	"пропуск: `hostname`, `network_interface_specs`, `use_default_network`, " +
	"`assign_external_address` и `acknowledge_unreachable` край в ответе не " +
	"возвращает. Импортированное состояние было бы пустым в этих полях, и первый же " +
	"план предложил бы **пересоздать** живую машину. Записать туда содержимое " +
	"настройки, не спросив край, провайдер не вправе: это значило бы утверждать " +
	"применённым то, что он не проверял.\n\n" +
	"## Чего в этом ресурсе НЕТ — и почему\n\n" +
	"Запрос создания у края шире, чем этот ресурс. Необъявленное поле честнее " +
	"объявленного и молча непримененного, поэтому каждое отсутствие названо:\n\n" +
	"* **`ssh_public_keys`** — край **отвергает** это поле по имени: доставлять ключи " +
	"  внутрь машины ему нечем (ни службы метаданных, ни агента в госте). Объявить его " +
	"  значило бы предложить ручку, любое использование которой даёт отказ. Ключи " +
	"  кладутся через `vm_spec.user_data` (cloud-init).\n" +
	"* **`secondary_volume_specs`** — край проверяет форму заказа и **не создаёт по " +
	"  нему ничего**: материализация тома — следующая фаза платформы. Диск, который " +
	"  никогда не появится, — это обещание, за которое никто не отвечает. Тома " +
	"  заводятся ресурсом `kacho_storage_volume`; подключение тома к машине — действие " +
	"  края (`:attachDisk`), а не поле, и провайдер действий не выражает.\n" +
	"* **`metadata`** — свободная карта данных машины. Её в контракте больше НЕТ: " +
	"  номер поля и имя зарезервированы, приём на создании снят. Отдельной операции " +
	"  правки у неё тоже не бывает — контракт это оговаривает прямо, потому что " +
	"  прежний отказ отсылал к RPC, которого не существует. Держать поле здесь значило " +
	"  бы обещать возможность, у которой нет ни хранения, ни глагола.\n" +
	"* **`placement_group_id`** — край хранит слаг и проверяет только его форму; " +
	"  ничего по нему не размещается, и ресурса группы размещения в платформе нет. " +
	"  Ссылаться некуда, эффекта нет.\n" +
	"* **`network_settings`, `filesystem_specs`, `local_disk_specs`, " +
	"  `maintenance_policy`, `maintenance_grace_period`, `serial_port_settings`** — " +
	"  край отвергает все шесть по имени: подсистем за ними у него нет.\n" +
	"* **адрес интерфейса, его номер и ссылка на готовый интерфейс** внутри " +
	"  `network_interface_specs` — край отвергает и их: адрес выдаёт владелец подсети, " +
	"  номер гнезда назначает сервер, а готовый интерфейс подключается отдельным " +
	"  действием.\n" +
	"* **`boot_source.name`, `boot_source.resolved_digest`, " +
	"  `boot_source.materialized_volume`** — на входе край их отвергает, а в ответе " +
	"  сегодня **не заполняет**: их заполняет сага разрешения образа, которой ещё нет. " +
	"  Показывать прочерк на месте факта значит утверждать о машине неправду. " +
	"  `boot_source.image_kind` не объявлен по другой причине: край выводит его " +
	"  механически из `type`, и это было бы второе имя одного и того же факта.\n" +
	"* **зеркала дисков** (`boot_disk`, `secondary_disks`) — у них нет в этом ресурсе " +
	"  входа-напарника: подключение тома выражается действием края, которого провайдер " +
	"  не выражает. Зеркало без своего входа — наблюдение ради наблюдения.\n\n" +
	"## Что провайдер меняет без пересоздания\n\n" +
	"`name`, `description`, `labels`, `service_account_id`, `vm_spec` (вступает в силу " +
	"при следующей загрузке), а также `machine_type_id` и `cpu_guarantee_percent` — " +
	"последние два только у остановленной машины. Всё остальное неизменяемо и " +
	"пересоздаёт машину."

// ---- перечисления --------------------------------------------------------------------

// instEnumValue — числовое значение варианта перечисления по его ИМЕНИ.
//
// Таблица берётся у СГЕНЕРЁННОГО типа контракта, а не переписывается: своя копия
// разошлась бы с контрактом молча. Неизвестное имя — ОТКАЗ, а не ноль: ноль в этих
// перечислениях означает «не задано», поэтому опечатка прошла бы как умолчание, а
// вызывающий получил бы от края «поле обязательно» — сообщение не про свою причину.
//
// Пустое, null и неизвестное дают ноль без ошибки: «не задано» — законный вход, а
// обязательность поля — отдельная ответственность вызывающего.
func instEnumValue(field string, v types.String, table map[string]int32, names map[int32]string) (int32, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return 0, nil
	}
	n, ok := table[v.ValueString()]
	if !ok || n == 0 {
		return 0, fmt.Errorf("поле %q: значение %q не входит в перечисление; допустимы: %s",
			field, v.ValueString(), strings.Join(instEnumAllowed(names), ", "))
	}
	return n, nil
}

// instEnumAllowed — имена вариантов перечисления, кроме нулевого. Нулевой не
// перечисляется намеренно: он означает «не задано», и предлагать его как значение
// значило бы предлагать способ ничего не сказать.
func instEnumAllowed(names map[int32]string) []string {
	keys := make([]int32, 0, len(names))
	for k := range names {
		if k == 0 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, names[k])
	}
	return out
}

// ---- проверка настройки ----------------------------------------------------------------

// ValidateConfig ловит на этапе плана то, что иначе приехало бы отказом из сети.
//
// Проверяется РОВНО то, чего край требует, и ни одним условием больше: провайдер,
// придумавший себе ограничение, отвергает законную настройку, а узнаётся это уже у
// пользователя. Единственное намеренное ужесточение — непустое имя, и его причина
// названа у самого поля.
func (r *computeInstanceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg computeInstanceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() == "" {
		resp.Diagnostics.AddError("Пустое имя машины",
			"Край пустое имя допускает, а провайдер — нет, и это осознанно строже. Имя — "+
				"единственное, чем провайдер подтверждает отсутствие машины, которой не видно "+
				"по идентификатору: он ищет её по имени в списке проекта. Без имени пропавшую "+
				"машину нельзя отличить от отозванного доступа.")
	}

	// Род машины проверяется здесь, а не только при отправке: неизвестное имя
	// превратилось бы в ноль, а ноль край читает как «поле не прислано» и отвечает
	// «instanceKind is required» — сообщение, из которого опечатка не видна.
	if _, err := instEnumValue("instance_kind", cfg.InstanceKind,
		computev1.InstanceKind_value, computev1.InstanceKind_name); err != nil {
		resp.Diagnostics.AddError("Неизвестный род машины", err.Error())
	}

	// Спецификация обязана совпадать с родом. НЕИЗВЕСТНЫЙ блок считается ЗАДАННЫМ:
	// ссылка на ещё не вычисленное значение приходит сюда неизвестной, и счесть её
	// пустой значило бы пропустить настоящее несовпадение. Судить о неизвестном роде
	// нельзя ни в какую сторону, поэтому пара проверяется только при известном роде.
	if !cfg.InstanceKind.IsNull() && !cfg.InstanceKind.IsUnknown() {
		switch cfg.InstanceKind.ValueString() {
		case computev1.InstanceKind_VM.String():
			if !cfg.ContainerSpec.IsNull() {
				resp.Diagnostics.AddError("Спецификация не того рода",
					"instance_kind = VM, но задан container_spec. Край отвергает эту пару по "+
						"имени поля: у машины и у контейнерного задания разные жизненные циклы, "+
						"и спецификация принадлежит роду, а не наоборот.")
			}
		case computev1.InstanceKind_CONTAINER.String():
			if !cfg.VMSpec.IsNull() {
				resp.Diagnostics.AddError("Спецификация не того рода",
					"instance_kind = CONTAINER, но задан vm_spec. Край отвергает эту пару по "+
						"имени поля.")
			}
		}
	}

	// Откуда взять сеть — край требует одно из двух. Неизвестное засчитывается за
	// заданное по той же причине, что и выше.
	netSpecGiven := !cfg.NetworkInterfaceSpecs.IsNull()
	defaultNetGiven := cfg.UseDefaultNetwork.IsUnknown() ||
		(!cfg.UseDefaultNetwork.IsNull() && cfg.UseDefaultNetwork.ValueBool())
	if !netSpecGiven && !defaultNetGiven {
		resp.Diagnostics.AddError("Не сказано, откуда у машины сеть",
			"Задайте network_interface_specs либо use_default_network = true. Край требует "+
				"одного из двух и без этого машину не заводит: интерфейс без подсети не "+
				"существует, а «взять по умолчанию» — отдельное осознанное решение, а не "+
				"умолчание провайдера.")
	}

	// Страж достижимости. Проверяется только при ИЗВЕСТНОМ роде VM и только когда оба
	// признака известны: неизвестный признак может разрешиться в true, и отвергать
	// настройку по догадке нельзя.
	if cfg.InstanceKind.ValueString() == computev1.InstanceKind_VM.String() &&
		!cfg.AssignExternalAddress.IsUnknown() && !cfg.AcknowledgeUnreachable.IsUnknown() &&
		!cfg.AssignExternalAddress.ValueBool() && !cfg.AcknowledgeUnreachable.ValueBool() {
		resp.Diagnostics.AddError("Машина будет недостижима, и это не подтверждено",
			"Для instance_kind = VM край требует одного из двух: assign_external_address = true "+
				"(заказать внешний адрес) либо acknowledge_unreachable = true (согласиться, что "+
				"машина будет работать и снаружи до неё не достучаться).\n\n"+
				"Это страж, а не формальность: без него легко завести работающую машину, к "+
				"которой нет доступа, и узнать об этом лишь когда она понадобится.")
	}

	// Перечисления внутри спецификаций — тем же средством, что и при отправке. Одна
	// реализация на две точки вызова: вторая копия разошлась бы с первой молча.
	if vm := instVMSpecOf(ctx, cfg.VMSpec); vm != nil {
		if mo := instMetadataOptionsOf(ctx, vm.MetadataOptions); mo != nil {
			if _, err := instEnumValue("vm_spec.metadata_options.metadata_endpoint",
				mo.MetadataEndpoint, computev1.MetadataOption_value, computev1.MetadataOption_name); err != nil {
				resp.Diagnostics.AddError("Неизвестное значение доступа к службе метаданных", err.Error())
			}
		}
	}
	if cs := instContainerSpecOf(ctx, cfg.ContainerSpec); cs != nil {
		if _, err := instEnumValue("container_spec.restart_policy", cs.RestartPolicy,
			computev1.RestartPolicy_value, computev1.RestartPolicy_name); err != nil {
			resp.Diagnostics.AddError("Неизвестная политика перезапуска", err.Error())
		}
	}
}

// ---- разбор значений настройки -----------------------------------------------------------

func instBootSourceOf(ctx context.Context, o types.Object) *instBootSourceModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m instBootSourceModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

func instVMSpecOf(ctx context.Context, o types.Object) *instVMSpecModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m instVMSpecModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

func instMetadataOptionsOf(ctx context.Context, o types.Object) *instMetadataOptionsModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m instMetadataOptionsModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

func instContainerSpecOf(ctx context.Context, o types.Object) *instContainerSpecModel {
	if o.IsNull() || o.IsUnknown() {
		return nil
	}
	var m instContainerSpecModel
	if diags := o.As(ctx, &m, basetypes.ObjectAsOptions{}); diags.HasError() {
		return nil
	}
	return &m
}

// instNICSpecsOf — заказы интерфейсов. Неизвестный и null дают nil: «не задал» и
// «задал пустым» различаются, а пустой список вместо отсутствия край прочитал бы как
// «сети нет» и отказал бы вместо того, чтобы взять сеть по умолчанию.
func instNICSpecsOf(ctx context.Context, l types.List) []instNICSpecModel {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]instNICSpecModel, 0, len(l.Elements()))
	_ = l.ElementsAs(ctx, &out, false)
	return out
}

// ---- сборка запроса --------------------------------------------------------------------

// instBootSourceToProto — источник загрузки в форму запроса.
//
// Неразобранный блок — ОТКАЗ, а не nil. Вернуть здесь nil значило бы отправить запрос
// без источника ОС и получить от края отказ «bootSource is required» — сообщение о
// том, чего вызывающий не делал: блок он написал.
func instBootSourceToProto(ctx context.Context, o types.Object) (*computev1.BootSource, error) {
	bs := instBootSourceOf(ctx, o)
	if bs == nil {
		return nil, fmt.Errorf("boot_source: блок задан, но его значение не прочиталось")
	}
	// name/resolvedDigest/materializedVolume/imageKind не отправляются: край считает
	// их выходными и отвергает запрос, если они заданы. Их и нет в схеме.
	return &computev1.BootSource{Type: bs.Type.ValueString(), Id: bs.ID.ValueString()}, nil
}

func instVMSpecToProto(ctx context.Context, o types.Object) (*computev1.VmSpec, error) {
	vm := instVMSpecOf(ctx, o)
	if vm == nil {
		return nil, nil
	}
	out := &computev1.VmSpec{UserData: vm.UserData.ValueString()}
	mo := instMetadataOptionsOf(ctx, vm.MetadataOptions)
	if mo == nil {
		return out, nil
	}
	endpoint, err := instEnumValue("vm_spec.metadata_options.metadata_endpoint",
		mo.MetadataEndpoint, computev1.MetadataOption_value, computev1.MetadataOption_name)
	if err != nil {
		return nil, err
	}
	out.MetadataOptions = &computev1.MetadataOptions{
		MetadataEndpoint: computev1.MetadataOption(endpoint),
	}
	return out, nil
}

func instContainerSpecToProto(ctx context.Context, o types.Object) (*computev1.ContainerSpec, error) {
	cs := instContainerSpecOf(ctx, o)
	if cs == nil {
		return nil, nil
	}
	policy, err := instEnumValue("container_spec.restart_policy", cs.RestartPolicy,
		computev1.RestartPolicy_value, computev1.RestartPolicy_name)
	if err != nil {
		return nil, err
	}
	out := &computev1.ContainerSpec{
		Command:       stringsFromTF(ctx, cs.Command),
		Args:          stringsFromTF(ctx, cs.Args),
		Env:           mapFromTF(ctx, cs.Env),
		WorkingDir:    cs.WorkingDir.ValueString(),
		RestartPolicy: computev1.RestartPolicy(policy),
	}
	// exit_code не отправляется: край считает его выходным и отвергает заданное
	// значение по имени. Его и нет в схеме.
	if !cs.Ports.IsNull() && !cs.Ports.IsUnknown() {
		ports := make([]instContainerPortModel, 0, len(cs.Ports.Elements()))
		_ = cs.Ports.ElementsAs(ctx, &ports, false)
		for i := range ports {
			out.Ports = append(out.Ports, &computev1.ContainerPort{
				// #nosec G115 -- диапазон порта закреплён контрактом края
				ContainerPort: int32(ports[i].ContainerPort.ValueInt64()),
				Protocol:      ports[i].Protocol.ValueString(),
			})
		}
	}
	return out, nil
}

func instNICSpecsToProto(ctx context.Context, l types.List) []*computev1.NetworkInterfaceSpec {
	specs := instNICSpecsOf(ctx, l)
	if specs == nil {
		return nil
	}
	out := make([]*computev1.NetworkInterfaceSpec, 0, len(specs))
	for i := range specs {
		out = append(out, &computev1.NetworkInterfaceSpec{
			SubnetId:         specs[i].SubnetID.ValueString(),
			SecurityGroupIds: stringsFromTF(ctx, specs[i].SecurityGroupIDs),
		})
	}
	return out
}

// instanceCreateBody собирает запрос создания.
//
// Род машины и спецификация кладутся ОТДЕЛЬНО и оба по своему условию: край требует
// род безусловно, а спецификацию — только совпадающую с родом. Ветка `default` здесь
// не нужна и не заведена: отсутствие спецификации — законный исход, а несовпадение
// уже отвергнуто на проверке настройки.
func instanceCreateBody(ctx context.Context, plan *computeInstanceModel) (*computev1.CreateInstanceRequest, error) {
	kind, err := instEnumValue("instance_kind", plan.InstanceKind,
		computev1.InstanceKind_value, computev1.InstanceKind_name)
	if err != nil {
		return nil, err
	}
	boot, err := instBootSourceToProto(ctx, plan.BootSource)
	if err != nil {
		return nil, err
	}

	body := &computev1.CreateInstanceRequest{
		ProjectId:     plan.ProjectID.ValueString(),
		ZoneId:        plan.ZoneID.ValueString(),
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueString(),
		Labels:        mapFromTF(ctx, plan.Labels),
		Hostname:      plan.Hostname.ValueString(),
		InstanceKind:  computev1.InstanceKind(kind),
		MachineTypeId: plan.MachineTypeID.ValueString(),
		BootSource:    boot,
		// #nosec G115 -- диапазон закреплён контрактом края: 0..100
		CpuGuaranteePercent:    int32(plan.CPUGuaranteePercent.ValueInt64()),
		ServiceAccountId:       plan.ServiceAccountID.ValueString(),
		PlacementGroupId:       plan.PlacementGroupID.ValueString(),
		GuestAccessKeyIds:      stringsFromList(ctx, plan.GuestAccessKeyIDs),
		NetworkInterfaceSpecs:  instNICSpecsToProto(ctx, plan.NetworkInterfaceSpecs),
		UseDefaultNetwork:      plan.UseDefaultNetwork.ValueBool(),
		AssignExternalAddress:  plan.AssignExternalAddress.ValueBool(),
		AcknowledgeUnreachable: plan.AcknowledgeUnreachable.ValueBool(),
	}

	vm, err := instVMSpecToProto(ctx, plan.VMSpec)
	if err != nil {
		return nil, err
	}
	if vm != nil {
		body.Spec = &computev1.CreateInstanceRequest_VmSpec{VmSpec: vm}
	}
	cs, err := instContainerSpecToProto(ctx, plan.ContainerSpec)
	if err != nil {
		return nil, err
	}
	if cs != nil {
		body.Spec = &computev1.CreateInstanceRequest_ContainerSpec{ContainerSpec: cs}
	}
	return body, nil
}

// ---- разбор ответа края -------------------------------------------------------------

// instanceWire — форма ответа края. Разбирается своим типом, а не сгенерённым:
// сгенерённый потребовал бы разрешения google.protobuf.Any через глобальный реестр
// типов, которого у провайдера нет. Тело ЗАПРОСА при этом остаётся сгенерённым —
// именно там опечатка в имени поля прошла бы молча.
//
// Целые числа объявлены как `any`: 64-разрядные край кодирует строкой, 32-разрядные —
// числом, и одна структура несёт оба вида (см. numOf).
type instanceWire struct {
	ID                  string            `json:"id"`
	ProjectID           string            `json:"projectId"`
	ZoneID              string            `json:"zoneId"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Labels              map[string]string `json:"labels"`
	CreatedAt           string            `json:"createdAt"`
	Status              string            `json:"status"`
	StatusReason        string            `json:"statusReason"`
	FQDN                string            `json:"fqdn"`
	InstanceKind        string            `json:"instanceKind"`
	MachineTypeID       string            `json:"machineTypeId"`
	CPUGuaranteePercent any               `json:"cpuGuaranteePercent"`
	PlacementGroupID    string            `json:"placementGroupId"`
	GuestAccessKeyIDs   []string          `json:"guestAccessKeyIds"`

	EffectiveResources *struct {
		VCPU      any    `json:"vCpu"`
		MemoryMiB any    `json:"memoryMib"`
		GPUs      any    `json:"gpus"`
		GPUType   string `json:"gpuType"`
	} `json:"effectiveResources"`

	BootSource *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"bootSource"`

	// Служебная учётка приходит обратно НЕ тем, чем ушла: на входе это строка
	// `service_account_id`, в ответе — ссылка `serviceAccount{type,id,name}`. Оба
	// имени выписаны дословно, потому что вывести одно из другого нечем.
	ServiceAccount *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"serviceAccount"`

	VMSpec *struct {
		UserData        string `json:"userData"`
		MetadataOptions *struct {
			MetadataEndpoint string `json:"metadataEndpoint"`
		} `json:"metadataOptions"`
	} `json:"vmSpec"`

	ContainerSpec *struct {
		Command    []string          `json:"command"`
		Args       []string          `json:"args"`
		Env        map[string]string `json:"env"`
		WorkingDir string            `json:"workingDir"`
		Ports      []struct {
			ContainerPort any    `json:"containerPort"`
			Protocol      string `json:"protocol"`
		} `json:"ports"`
		RestartPolicy string `json:"restartPolicy"`
	} `json:"containerSpec"`

	NetworkInterfaces []struct {
		Index            string   `json:"index"`
		MACAddress       string   `json:"macAddress"`
		SubnetID         string   `json:"subnetId"`
		NICID            string   `json:"nicId"`
		SecurityGroupIDs []string `json:"securityGroupIds"`
		PrimaryV4Address *struct {
			Address string `json:"address"`
		} `json:"primaryV4Address"`
		PrimaryV6Address *struct {
			Address string `json:"address"`
		} `json:"primaryV6Address"`
	} `json:"networkInterfaces"`
}

// applyInstance раскладывает ответ края по полям ресурса.
//
// Поля, которых край НЕ эхает (hostname, заказы интерфейсов, три признака запуска),
// здесь не трогаются намеренно: их значение пришло из настройки, и подстановка нуля
// означала бы «пользователь задал пусто» — то есть пересоздание машины на следующем
// же плане.
func applyInstance(ctx context.Context, m *computeInstanceModel, raw []byte) error {
	var w instanceWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("разбор ответа края: %w", err)
	}

	m.ID = types.StringValue(w.ID)
	m.ProjectID = types.StringValue(w.ProjectID)
	m.ZoneID = types.StringValue(w.ZoneID)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Labels = mapToTF(ctx, w.Labels)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.Status = types.StringValue(w.Status)
	m.StatusReason = types.StringValue(w.StatusReason)
	m.FQDN = types.StringValue(w.FQDN)
	m.InstanceKind = types.StringValue(w.InstanceKind)
	m.MachineTypeID = types.StringValue(w.MachineTypeID)
	m.CPUGuaranteePercent = types.Int64Value(numOf(w.CPUGuaranteePercent))

	// Пустая строка вместо null: поле необязательное и НЕ вычисляемое, поэтому null в
	// настройке обязан совпадать с null в состоянии, а «учётки нет» край выражает
	// отсутствием ссылки целиком.
	m.ServiceAccountID = types.StringNull()
	if w.ServiceAccount != nil {
		m.ServiceAccountID = strOrNull(w.ServiceAccount.ID)
	}
	m.PlacementGroupID = strOrNull(w.PlacementGroupID)

	// Набор ключей ВЫЧИСЛЯЕМЫЙ, поэтому пустой ответ обязан лечь пустым списком, а
	// не null: у вычисляемого атрибута null означает «край ещё не сказал», и на нём
	// план расходился бы навсегда — ровно тот вечный дрейф, ради которого обратное
	// чтение вообще пишется.
	m.GuestAccessKeyIDs = listFromStrings(ctx, w.GuestAccessKeyIDs)

	if w.BootSource == nil {
		// Ветка выбирается СВОИМ условием, а её отсутствие — отказ. Машины без
		// источника загрузки край не производит; записать сюда null значило бы отдать
		// Terraform состояние, в котором машина есть, а чем она загружается —
		// неизвестно.
		return fmt.Errorf("край вернул машину без источника загрузки: в ответе нет bootSource")
	}
	bootObj, diags := types.ObjectValue(instBootSourceAttrTypes(), map[string]attr.Value{
		"type": types.StringValue(w.BootSource.Type),
		"id":   types.StringValue(w.BootSource.ID),
	})
	if diags.HasError() {
		return fmt.Errorf("источник загрузки края не укладывается в объект: %v", diags.Errors())
	}
	m.BootSource = bootObj

	if err := instApplyEffectiveResources(m, &w); err != nil {
		return err
	}
	if err := instApplyVMSpec(m, &w); err != nil {
		return err
	}
	if err := instApplyContainerSpec(ctx, m, &w); err != nil {
		return err
	}
	return instApplyNICMirror(ctx, m, &w)
}

func instApplyEffectiveResources(m *computeInstanceModel, w *instanceWire) error {
	if w.EffectiveResources == nil {
		m.EffectiveResources = types.ObjectNull(instEffectiveResourcesAttrTypes())
		return nil
	}
	obj, diags := types.ObjectValue(instEffectiveResourcesAttrTypes(), map[string]attr.Value{
		"v_cpu":      types.Int64Value(numOf(w.EffectiveResources.VCPU)),
		"memory_mib": types.Int64Value(numOf(w.EffectiveResources.MemoryMiB)),
		"gpus":       types.Int64Value(numOf(w.EffectiveResources.GPUs)),
		"gpu_type":   types.StringValue(w.EffectiveResources.GPUType),
	})
	if diags.HasError() {
		return fmt.Errorf("размер края не укладывается в объект: %v", diags.Errors())
	}
	m.EffectiveResources = obj
	return nil
}

func instApplyVMSpec(m *computeInstanceModel, w *instanceWire) error {
	if w.VMSpec == nil {
		m.VMSpec = types.ObjectNull(instVMSpecAttrTypes())
		return nil
	}
	opts := types.ObjectNull(instMetadataOptionsAttrTypes())
	if mo := w.VMSpec.MetadataOptions; mo != nil {
		v, diags := types.ObjectValue(instMetadataOptionsAttrTypes(), map[string]attr.Value{
			"metadata_endpoint": strOrNull(mo.MetadataEndpoint),
		})
		if diags.HasError() {
			return fmt.Errorf("настройка службы метаданных края не укладывается в объект: %v", diags.Errors())
		}
		opts = v
	}
	obj, diags := types.ObjectValue(instVMSpecAttrTypes(), map[string]attr.Value{
		"user_data":        strOrNull(w.VMSpec.UserData),
		"metadata_options": opts,
	})
	if diags.HasError() {
		return fmt.Errorf("спецификация машины края не укладывается в объект: %v", diags.Errors())
	}
	m.VMSpec = obj
	return nil
}

func instApplyContainerSpec(ctx context.Context, m *computeInstanceModel, w *instanceWire) error {
	if w.ContainerSpec == nil {
		m.ContainerSpec = types.ObjectNull(instContainerSpecAttrTypes())
		return nil
	}
	ports := types.ListNull(types.ObjectType{AttrTypes: instContainerPortAttrTypes()})
	if len(w.ContainerSpec.Ports) > 0 {
		items := make([]instContainerPortModel, 0, len(w.ContainerSpec.Ports))
		for _, p := range w.ContainerSpec.Ports {
			items = append(items, instContainerPortModel{
				ContainerPort: types.Int64Value(numOf(p.ContainerPort)),
				Protocol:      strOrNull(p.Protocol),
			})
		}
		v, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: instContainerPortAttrTypes()}, items)
		if diags.HasError() {
			return fmt.Errorf("порты контейнера края не укладываются в список: %v", diags.Errors())
		}
		ports = v
	}
	obj, diags := types.ObjectValue(instContainerSpecAttrTypes(), map[string]attr.Value{
		"command":        instListOrNull(ctx, w.ContainerSpec.Command),
		"args":           instListOrNull(ctx, w.ContainerSpec.Args),
		"env":            instMapOrNull(ctx, w.ContainerSpec.Env),
		"working_dir":    strOrNull(w.ContainerSpec.WorkingDir),
		"ports":          ports,
		"restart_policy": strOrNull(w.ContainerSpec.RestartPolicy),
	})
	if diags.HasError() {
		return fmt.Errorf("спецификация задания края не укладывается в объект: %v", diags.Errors())
	}
	m.ContainerSpec = obj
	return nil
}

func instApplyNICMirror(ctx context.Context, m *computeInstanceModel, w *instanceWire) error {
	// Пустой список, а НЕ null: поле вычисляемое, и null давал бы «известно после
	// применения» на каждом плане неизменной инфраструктуры.
	mirror := make([]instNICMirrorModel, 0, len(w.NetworkInterfaces))
	for _, n := range w.NetworkInterfaces {
		item := instNICMirrorModel{
			Index:            types.StringValue(n.Index),
			NICID:            types.StringValue(n.NICID),
			MACAddress:       types.StringValue(n.MACAddress),
			SubnetID:         types.StringValue(n.SubnetID),
			SecurityGroupIDs: listFromStrings(ctx, n.SecurityGroupIDs),
			PrimaryV4Address: types.ObjectNull(instAddressAttrTypes()),
			PrimaryV6Address: types.ObjectNull(instAddressAttrTypes()),
		}
		if n.PrimaryV4Address != nil {
			v, diags := types.ObjectValue(instAddressAttrTypes(),
				map[string]attr.Value{"address": types.StringValue(n.PrimaryV4Address.Address)})
			if diags.HasError() {
				return fmt.Errorf("адрес интерфейса не укладывается в объект: %v", diags.Errors())
			}
			item.PrimaryV4Address = v
		}
		if n.PrimaryV6Address != nil {
			v, diags := types.ObjectValue(instAddressAttrTypes(),
				map[string]attr.Value{"address": types.StringValue(n.PrimaryV6Address.Address)})
			if diags.HasError() {
				return fmt.Errorf("адрес интерфейса не укладывается в объект: %v", diags.Errors())
			}
			item.PrimaryV6Address = v
		}
		mirror = append(mirror, item)
	}
	list, diags := types.ListValueFrom(ctx, instNICMirrorObjectType(), mirror)
	if diags.HasError() {
		return fmt.Errorf("интерфейсы края не укладываются в список: %v", diags.Errors())
	}
	m.NetworkInterfaces = list
	return nil
}

// instListOrNull / instMapOrNull — нули края означают «поле не задано»: край
// опускает значения по умолчанию, и обратно они приходят отсутствием. Записать их
// значением значило бы объявить вызывающему то, чего он не писал, — и получить
// расхождение на следующем плане, а у неизменяемого блока ещё и пересоздание машины.
//
// Булев близнец этой пары снят вместе со своим единственным читателем: поле
// «требовать ли сеансовый ключ» СНЯТО С КОНТРАКТА, и помощник без читателя жил бы
// мёртвым — тем самым, который следующий примет за действующий.

func instListOrNull(ctx context.Context, in []string) types.List {
	if len(in) == 0 {
		return types.ListNull(types.StringType)
	}
	return listFromStrings(ctx, in)
}

func instMapOrNull(ctx context.Context, in map[string]string) types.Map {
	if len(in) == 0 {
		return types.MapNull(types.StringType)
	}
	return mapToTF(ctx, in)
}

// ---- сохранение написания -----------------------------------------------------------------

// instNormalize приводит значение к канонической форме сравнения: пустое, ложное,
// нулевое и отсутствующее — одно и то же.
//
// Так их видит КРАЙ: он опускает значения по умолчанию, поэтому `command = []`,
// `env = {}` и `metadata_options = {}` возвращаются отсутствием. Без нормализации
// такая настройка объявляла бы вечное расхождение, а у неизменяемого блока —
// пересоздание живой машины на ровном месте.
func instNormalize(ctx context.Context, v attr.Value) attr.Value {
	switch t := v.(type) {
	case types.String:
		if t.IsNull() || t.IsUnknown() || t.ValueString() == "" {
			return types.StringNull()
		}
		return t
	case types.Bool:
		if t.IsNull() || t.IsUnknown() || !t.ValueBool() {
			return types.BoolNull()
		}
		return t
	case types.Int64:
		if t.IsNull() || t.IsUnknown() || t.ValueInt64() == 0 {
			return types.Int64Null()
		}
		return t
	case types.Map:
		if t.IsNull() || t.IsUnknown() || len(t.Elements()) == 0 {
			return types.MapNull(t.ElementType(ctx))
		}
		return t
	case types.List:
		if t.IsNull() || t.IsUnknown() || len(t.Elements()) == 0 {
			return types.ListNull(t.ElementType(ctx))
		}
		elems := make([]attr.Value, 0, len(t.Elements()))
		for _, e := range t.Elements() {
			elems = append(elems, instNormalize(ctx, e))
		}
		out, diags := types.ListValue(t.ElementType(ctx), elems)
		if diags.HasError() {
			return t
		}
		return out
	case types.Object:
		at := t.AttributeTypes(ctx)
		if t.IsNull() || t.IsUnknown() {
			return types.ObjectNull(at)
		}
		attrs := make(map[string]attr.Value, len(t.Attributes()))
		empty := true
		for k, e := range t.Attributes() {
			n := instNormalize(ctx, e)
			attrs[k] = n
			if !n.IsNull() {
				empty = false
			}
		}
		if empty {
			return types.ObjectNull(at)
		}
		out, diags := types.ObjectValue(at, attrs)
		if diags.HasError() {
			return t
		}
		return out
	}
	return v
}

// instSameValue — одно ли это значение по СУЩЕСТВУ, а не по написанию.
func instSameValue(ctx context.Context, a, b attr.Value) bool {
	return instNormalize(ctx, a).Equal(instNormalize(ctx, b))
}

// instKeepMachineTypeSpelling сохраняет написание типа машины, выбранное вызывающим.
//
// Край принимает и слаг `mt-…`, и стабильное имя типа, а эхает ВСЕГДА слаг. Без этого
// настройка с именем ломалась бы дважды: применение падало бы на несогласованности
// («в плане одно, в состоянии другое»), а обновление состояния объявляло бы вечное
// расхождение, закрыть которое нечем — смена размера требует остановленной машины.
//
// Совпадение ПРОВЕРЯЕТСЯ, а не предполагается: провайдер спрашивает у каталога имя
// того типа, который край назвал в ответе. Если каталог недоступен, остаётся
// написание вызывающего — на создании край уже принял эту строку как ссылку именно на
// эту машину, поэтому предлагать по нечитаемому ответу правку, которую всё равно не
// применить, было бы хуже молчания.
func (r *computeInstanceResource) instKeepMachineTypeSpelling(ctx context.Context, want, got types.String) types.String {
	if want.IsNull() || want.IsUnknown() || want.ValueString() == "" {
		return got
	}
	if want.ValueString() == got.ValueString() || got.ValueString() == "" {
		return got
	}
	// Вызывающий назвал слаг, а край ответил другим слагом — это НАСТОЯЩЕЕ различие
	// (тип машины сменили мимо Terraform), и прятать его нельзя.
	if strings.HasPrefix(want.ValueString(), "mt-") {
		return got
	}
	name, err := r.machineTypeName(ctx, got.ValueString())
	if err != nil {
		return want
	}
	if name == want.ValueString() {
		return want
	}
	return got
}

// machineTypeName — стабильное имя типа машины по его слагу. Каталог типов читает
// любой аутентифицированный вызывающий, поэтому лишних прав это не требует.
func (r *computeInstanceResource) machineTypeName(ctx context.Context, slug string) (string, error) {
	resp, err := r.c.Do(ctx, http.MethodGet, machineTypesPath+"/"+slug, nil, nil)
	if err != nil {
		return "", err
	}
	if out := client.Classify(resp); out.Kind != client.OutcomeOK {
		return "", fmt.Errorf("чтение типа машины %s: %s", slug, out.Message)
	}
	var mt struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Body, &mt); err != nil {
		return "", fmt.Errorf("разбор ответа края: %w", err)
	}
	return mt.Name, nil
}

// instKeepSpelling сохраняет написание вызывающего везде, где край вернул то же самое
// ПО СУЩЕСТВУ. Меняется только написание; разные значения остаются разными.
func (r *computeInstanceResource) instKeepSpelling(ctx context.Context, want, got *computeInstanceModel) {
	got.MachineTypeID = r.instKeepMachineTypeSpelling(ctx, want.MachineTypeID, got.MachineTypeID)
	if instSameValue(ctx, want.VMSpec, got.VMSpec) {
		got.VMSpec = want.VMSpec
	}
	if instSameValue(ctx, want.ContainerSpec, got.ContainerSpec) {
		got.ContainerSpec = want.ContainerSpec
	}
}

// ---- гашение неизвестного -------------------------------------------------------------

// instSealObject заменяет НЕИЗВЕСТНЫЕ значения внутри объекта на null.
//
// Общий гаситель (sealUnknowns) обходит поля модели рефлексией и знает строку, число,
// признак, список, набор и карту, но внутрь types.Object НЕ заходит: его содержимое
// лежит в неэкспортируемых полях. У машины вычисляемый объект есть — размер, который
// край выводит из типа машины, — поэтому ранняя запись состояния унесла бы туда
// неизвестное, а Terraform не принимает после применения НИ ОДНОГО неизвестного и
// отвечает на каждое отдельным «invalid result object», среди которых настоящая
// причина (сорвавшееся обратное чтение) теряется.
//
// Вид значения, который эта функция гасить не умеет, гасит объект ЦЕЛИКОМ. Иначе
// появление нового вида поля прошло бы молча — то есть проверка потеряла бы предмет
// ровно там, где она и нужна.
func instSealObject(ctx context.Context, o types.Object) types.Object {
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
		case types.Bool:
			if tv.IsUnknown() {
				attrs[k] = types.BoolNull()
				continue
			}
			attrs[k] = tv
		case types.Int64:
			if tv.IsUnknown() {
				attrs[k] = types.Int64Null()
				continue
			}
			attrs[k] = tv
		case types.List:
			if tv.IsUnknown() {
				attrs[k] = types.ListNull(tv.ElementType(ctx))
				continue
			}
			attrs[k] = tv
		case types.Map:
			if tv.IsUnknown() {
				attrs[k] = types.MapNull(tv.ElementType(ctx))
				continue
			}
			attrs[k] = tv
		case types.Object:
			attrs[k] = instSealObject(ctx, tv)
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

func instSealUnknowns(ctx context.Context, m *computeInstanceModel) {
	m.BootSource = instSealObject(ctx, m.BootSource)
	m.VMSpec = instSealObject(ctx, m.VMSpec)
	m.ContainerSpec = instSealObject(ctx, m.ContainerSpec)
	m.EffectiveResources = instSealObject(ctx, m.EffectiveResources)
	sealUnknowns(m)
}

// ---- CRUD -------------------------------------------------------------------------------

func (r *computeInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan computeInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := plan

	body, err := instanceCreateBody(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Запрос создания машины не собран", err.Error())
		return
	}

	id, err := awaitCreate(ctx, r.c, instancesPath, "instanceId", "kacho_compute_instance",
		plan.ProjectID.ValueString()+"/"+plan.Name.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Создание машины не завершилось", instanceCreateHint(err))
		return
	}

	// Идентификатор записывается ДО обратного чтения: если чтение сорвётся, машина уже
	// создана, и без этой записи Terraform о ней не узнает никогда.
	plan.ID = types.StringValue(id)
	// Неизвестные вычисляемые значения гасятся до записи: Terraform не принимает НИ
	// ОДНОГО неизвестного после apply, и без этого сорвавшееся чтение даёт по
	// сообщению на каждое вместо одного — про само чтение.
	instSealUnknowns(ctx, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := readByID(ctx, r.c, instancesPath, id, true)
	if err != nil {
		resp.Diagnostics.AddError("Машина создана, но не прочитана обратно", err.Error())
		return
	}
	if err := applyInstance(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	r.instKeepSpelling(ctx, &want, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// instanceCreateHint добавляет к отказу края то, что из него самого не видно.
//
// Два условия создания живут не в теле запроса, а в чужих ресурсах, и отказ по ним
// читается как отказ платформы: зона подсети принадлежит другому сервису, а страж
// достижимости срабатывает на сочетании двух признаков.
func instanceCreateHint(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "subnet is in zone"), strings.Contains(msg, "same region as the instance"):
		return msg + "\n\nЗона машины и зона подсети КАЖДОГО её интерфейса обязаны совпадать; " +
			"у региональной (эникаст) подсети зоны нет, и сверяется регион. Провайдер этого " +
			"проверить не может — подсеть принадлежит другому сервису, и её зону знает только " +
			"он. Либо заведите машину в зоне подсети, либо возьмите подсеть своей зоны."
	case strings.Contains(msg, "unreachable"):
		return msg + "\n\nЗадайте assign_external_address = true либо acknowledge_unreachable = true."
	case strings.Contains(msg, "machine type"):
		return msg + "\n\nДоступные типы машин — в каталоге " + machineTypesPath +
			"; принимается и слаг mt-…, и стабильное имя типа."
	}
	return msg
}

func (r *computeInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state computeInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := state

	raw, err := readByID(ctx, r.c, instancesPath, state.ID.ValueString(), false)
	if err == nil {
		if err := applyInstance(ctx, &state, raw); err != nil {
			resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
			return
		}
		r.instKeepSpelling(ctx, &want, &state)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var missing *notFoundError
	if !asNotFound(err, &missing) {
		resp.Diagnostics.AddError("Чтение машины не удалось", err.Error())
		return
	}
	// Одиночное «не найдено» ничего не устанавливает: тот же ответ приходит при отказе
	// в доступе, и он побайтово равен настоящему отсутствию.
	remove, title, detail := absenceDiagnostics(ctx, r.c, instancesPath, client.ScopeProject,
		"Машина", state.ID.ValueString(), state.ProjectID.ValueString(), state.Name.ValueString())
	switch {
	case remove:
		resp.State.RemoveResource(ctx)
	case title != "":
		resp.Diagnostics.AddError(title, detail)
	}
}

// Update меняет ТОЛЬКО то, что край принимает в маске изменения: имя, описание, метки,
// служебную учётку, тип машины, гарантию процессора и спецификацию машины.
//
// Всего остального здесь нет и быть не может — эти поля помечены пересоздающими,
// поэтому их правка до этого метода не доходит вовсе.
func (r *computeInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state computeInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want := plan
	id := state.ID.ValueString()

	body := &computev1.UpdateInstanceRequest{InstanceId: id}
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
	if !plan.ServiceAccountID.Equal(state.ServiceAccountID) {
		body.ServiceAccountId = plan.ServiceAccountID.ValueString()
		paths = append(paths, "service_account_id")
	}
	if !plan.MachineTypeID.Equal(state.MachineTypeID) {
		body.MachineTypeId = plan.MachineTypeID.ValueString()
		paths = append(paths, "machine_type_id")
	}
	if !plan.PlacementGroupID.Equal(state.PlacementGroupID) && !plan.PlacementGroupID.IsUnknown() {
		body.PlacementGroupId = plan.PlacementGroupID.ValueString()
		paths = append(paths, "placement_group_id")
	}
	// Набор ключей входа передаётся ЦЕЛИКОМ и только когда он изменился: пустая
	// маска у края означала бы правку всех изменяемых полей, а набор ключей в неё
	// намеренно не входит — иначе правка описания снимала бы весь доступ.
	if !plan.GuestAccessKeyIDs.Equal(state.GuestAccessKeyIDs) && !plan.GuestAccessKeyIDs.IsUnknown() {
		body.GuestAccessKeyIds = stringsFromList(ctx, plan.GuestAccessKeyIDs)
		paths = append(paths, "guest_access_key_ids")
	}
	if !plan.CPUGuaranteePercent.Equal(state.CPUGuaranteePercent) {
		// #nosec G115 -- диапазон закреплён контрактом края: 0..100
		body.CpuGuaranteePercent = int32(plan.CPUGuaranteePercent.ValueInt64())
		paths = append(paths, "cpu_guarantee_percent")
	}
	// Спецификация машины сравнивается ПО СУЩЕСТВУ: пустой блок и его отсутствие край
	// не различает, и посылать изменение ради разницы в написании значило бы штамповать
	// машине отметку «вступит в силу при следующей загрузке» на ровном месте.
	if !instSameValue(ctx, plan.VMSpec, state.VMSpec) {
		vm, err := instVMSpecToProto(ctx, plan.VMSpec)
		if err != nil {
			resp.Diagnostics.AddError("Спецификация машины не собрана", err.Error())
			return
		}
		body.VmSpec = vm
		paths = append(paths, "vm_spec")
	}

	// Пустая маска запросом не отправляется: край трактует её как «применить всё
	// изменяемое», а это не то, о чём просили. Обратное чтение делается ВСЕГДА — иначе
	// в состоянии осталась бы неизвестность плана.
	if len(paths) > 0 {
		body.UpdateMask = fieldMask(paths)
		if err := awaitMutation(ctx, r.c, http.MethodPatch, instancesPath+"/"+id, body); err != nil {
			resp.Diagnostics.AddError("Изменение машины не завершилось", instanceUpdateHint(err))
			return
		}
	}

	raw, err := readByID(ctx, r.c, instancesPath, id, false)
	if err != nil {
		resp.Diagnostics.AddError("Машина изменена, но не прочитана обратно", err.Error())
		return
	}
	plan.ID = state.ID
	if err := applyInstance(ctx, &plan, raw); err != nil {
		resp.Diagnostics.AddError("Ответ края не разобран", err.Error())
		return
	}
	r.instKeepSpelling(ctx, &want, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// instanceUpdateHint объясняет отказ, причина которого лежит в жизненном цикле, а не
// в самом изменении.
func instanceUpdateHint(err error) string {
	msg := err.Error()
	if !strings.Contains(msg, "STOPPED") {
		return msg
	}
	return msg + "\n\nТип машины и гарантия процессора меняются только у ОСТАНОВЛЕННОЙ машины. " +
		"Остановить её через Terraform нечем: провайдер не выражает пуск и остановку — это " +
		"команды, а не желаемое состояние (см. описание ресурса). Остановите машину из консоли " +
		"или вызовом края и примените снова.\n\n" +
		"Пересоздавать машину ради смены размера провайдер не станет сам: это уничтожение " +
		"данных, и такое решение принимает владелец — явным `terraform taint` либо " +
		"`-replace=<адрес ресурса>`."
}

func (r *computeInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state computeInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := awaitMutation(ctx, r.c, http.MethodDelete,
		instancesPath+"/"+state.ID.ValueString(), nil); err != nil {
		resp.Diagnostics.AddError("Удаление машины не завершилось",
			err.Error()+"\n\nПривязки интерфейсов и томов край снимает сам, поэтому отказ здесь "+
				"обычно означает, что владелец интерфейсов или томов не отвечает, — удаление "+
				"тогда не начато, а не сделано наполовину: строку машины край удаляет ПОСЛЕДНЕЙ. "+
				"Сами интерфейсы и тома при этом не удаляются: снимается только привязка.")
	}
}

// ImportState отсутствует НАМЕРЕННО, и это не пропуск.
//
// Импортировать можно то, что читается обратно. Пять полей входа край не эхает —
// hostname, заказы интерфейсов, признак сети по умолчанию и два признака стража
// достижимости, — поэтому импортированное состояние было бы пустым в них, и первый же
// план предложил бы ПЕРЕСОЗДАТЬ живую машину: все пять неизменяемы. Записать туда
// содержимое настройки, не спросив край, нельзя: это значило бы утверждать
// применённым то, что провайдер не проверял, — и утверждать это о работающей машине.
var _ resource.Resource = (*computeInstanceResource)(nil)
var _ resource.ResourceWithValidateConfig = (*computeInstanceResource)(nil)
