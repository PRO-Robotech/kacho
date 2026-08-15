// Реестр ресурсов compute-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC/NLB-remote. Домен — Compute: Instance
// (виртуальная машина / контейнер-джоба) + MachineType (read-only каталог sizing).
//
// COMP-1 REDESIGN: Instance приведён к новой форме — instanceKind XOR
// (VM/CONTAINER), единый канал sizing'а machineTypeId (raw ResourcesSpec/platformId
// retired, ban #2), единый канал ОС bootSource{type,id} (storage.image vs
// registry.image), serviceAccount как reference.Referrer. Мутации async → Operation.
//
// `zones` (geo) / `volumes` (storage) / `network-interfaces` (vpc) / `machine-types`
// (compute-каталог) — ref-цели для RefSelect. `machine-types` также навигируемый
// read-only каталог.

import type { ReactNode } from "react";
import { Typography } from "antd";
import type { FormField } from "@shared/lib/form-schema";
import { flatIdList } from "@shared/lib/id-list";
import {
  GUEST_ACCESS_KEY_EMPTY_STATE,
  GUEST_ACCESS_KEY_FIELDS,
  guestAccessKeyTemplate,
} from "@shared/lib/guest-access-key-form";
import { setByPath } from "./path";
import { formatBytes } from "./bytes";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import type { ResourceColumn, ResourceSpec } from "@shared/lib/resource-spec";
import { REGISTRY as SHARED_REGISTRY } from "@shared/lib/resource-registry";
import {
  isSystemScopedResource,
  resourceListPath,
  resourceServicePrefix,
  type ServicePrefix,
} from "@shared/lib/service-prefix";

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`, и импортируется
// сюда. Реэкспорт оставлен, чтобы потребители этого модуля не меняли импорты: у него
// нет тела, поэтому разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы
// здесь запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.

export type { ResourceColumn, ResourceSpec };

// ── Общие FormField-константы ──

const FIELD_NAME: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-instance",
  description:
    "Строчные латинские буквы, цифры, «-» и «_». Должно начинаться с буквы, длина до 63 символов. Можно оставить пустым.",
  pattern: "^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$",
};

const FIELD_DESCRIPTION: FormField = {
  name: "description",
  label: "Описание",
  type: "text",
  rows: 2,
  placeholder: "Краткое описание инстанса (опционально)",
};

const FIELD_PROJECT_ID: FormField = { name: "project_id", label: "Project", type: "string", hidden: true };
const FIELD_LABELS: FormField = { name: "labels", label: "Метки", type: "labels" };

const MIB = 1024 * 1024;

// MemMiBCell — память MachineType/EffectiveResources хранится в МиБ (int64 строкой);
// приводим в человекочитаемый вид через общий formatBytes (МиБ → байты → GB).
function MemMiBCell({ value }: { value: unknown }): ReactNode {
  const mib = typeof value === "string" ? Number.parseInt(value, 10) : typeof value === "number" ? value : Number.NaN;
  const s = Number.isFinite(mib) && mib > 0 ? formatBytes(mib * MIB) : "—";
  return s === "—" ? <Typography.Text type="secondary">—</Typography.Text> : <>{s}</>;
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== compute: Instance (COMP-1 redesign) ======
  // proto: kacho.cloud.compute.v1.InstanceService (/compute/v1/instances).
  // Create требует: project_id, zone_id, instance_kind (VM|CONTAINER), machine_type_id
  // (mt- slug ИЛИ имя каталога), boot_source{type,id}. Sizing (raw cores/memory/
  // platform_id) retired → единый machine_type_id. Мутируемые Update-поля: name/
  // description/labels/service_account_id + STOPPED-gated machine_type_id/cpu_guarantee.
  // instance_kind/zone_id immutable; boot_source/spec/ssh/network — createOnly.
  "compute-instances": {
    id: "compute-instances",
    route: "instances",
    apiPath: "/compute/v1/instances",
    payloadKey: "instances",
    singular: "Виртуальная машина",
    accusative: "виртуальную машину",
    plural: "Виртуальные машины",
    genitive: "Виртуальной машины",
    serviceTitle: "Compute Cloud",
    scope: "project",
    // Start/Stop/Restart — доменные действия на detail (InstanceActions), не в ops.
    ops: { create: true, update: true, delete: true },
    docs: [
      { label: "Виртуальные машины", href: "#" },
      { label: "Типы машин (sizing)", href: "#" },
      { label: "Тома и снимки (Storage)", href: "#" },
    ],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Тип", path: "instance_kind", format: "code" },
      { header: "Зона", path: "zone_id", format: "text" },
      { header: "Тип машины", path: "machine_type_id", format: "code" },
      { header: "vCPU", path: "effective_resources.v_cpu", format: "text" },
      {
        header: "Память",
        path: "effective_resources.memory_mib",
        render: (row) => (
          <MemMiBCell value={(row.effective_resources as Record<string, unknown> | undefined)?.memory_mib} />
        ),
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME,
      FIELD_DESCRIPTION,
      {
        name: "zone_id",
        label: "Зона доступности",
        type: "ref",
        refResource: "zones",
        required: true,
        immutable: true,
        description: "Зона размещения машины. Неизменяема после создания.",
      },
      {
        name: "instance_kind",
        label: "Тип инстанса",
        type: "enum",
        required: true,
        immutable: true,
        default: "VM",
        options: [
          { value: "VM", label: "VM — виртуальная машина (ОС из storage.image)" },
          { value: "CONTAINER", label: "CONTAINER — контейнер-джоба (образ из registry.image)" },
        ],
        description:
          "Вид машины; неизменяем после создания. Виртуальная машина запускает операционную систему из образа диска, контейнер — из образа реестра.",
      },
      {
        name: "machine_type_id",
        label: "Тип машины",
        type: "ref",
        refResource: "machine-types",
        required: true,
        description:
          "Единый канал размера инстанса (vCPU/память/GPU) — каталог MachineType. Сменить размер можно на остановленном (STOPPED) инстансе.",
      },
      {
        name: "boot_source.type",
        label: "Источник ОС",
        type: "enum",
        required: true,
        createOnly: true,
        default: "storage.image",
        options: [
          { value: "storage.image", label: "storage.image — образ ОС (VM)" },
          { value: "registry.image", label: "registry.image — OCI-образ (CONTAINER)" },
        ],
        description:
          "Владелец образа: storage.image (диск-образ kacho-storage, для VM) или registry.image (OCI-артефакт kacho-registry, для CONTAINER).",
      },
      {
        // Образ — списком, а не строкой.
        //
        // Сервер принимает здесь РОВНО `img-<base32>`: `validateBootSource`
        // (services/compute) зовёт `corevalidate.ResourceID("Image", "img", …)`.
        // Прежняя подсказка предлагала две формы — с тегом и OCI-ссылку, — и обе
        // сервер отвергает: у образа хранилища нет ни поля тега, ни поля
        // дайджеста, а ветка registry.image отвергается целиком («у образа из
        // реестра сегодня нет durable-адреса»). То есть форма предлагала набрать
        // руками идентификатор, который она же могла показать списком.
        name: "boot_source.id",
        label: "Образ",
        type: "ref",
        refResource: "images",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        visibleWhen: { field: "boot_source.type", equals: "storage.image" },
        description:
          "Образ ОС, из которого материализуется загрузочный том машины. Список — образы текущего проекта; нет ни одного — создайте образ в разделе Storage.",
      },
      {
        name: "cpu_guarantee_percent",
        label: "Гарантия CPU, %",
        type: "int",
        min: 0,
        max: 100,
        default: 0,
        description:
          "Гарантированный baseline CPU на vCPU в процентах (0 — best-effort/burstable; 1..100 — гарантия). Применимо к STANDARD/COMPUTE/MEMORY. Меняется на STOPPED.",
      },
      {
        name: "service_account_id",
        label: "Сервисный аккаунт",
        type: "string",
        placeholder: "sva…",
        description:
          "Опционально: сервисный аккаунт (iam), доступный внутри инстанса. Для публичных образов можно не задавать.",
      },
      {
        // Ключи входа — ССЫЛКАМИ на ресурс, а не материалом в теле запроса.
        // Контракт это объясняет прямым текстом: ключ, переданный полем, живёт
        // ровно столько, сколько машина, и его нельзя ни отозвать, ни заменить,
        // ни узнать, где ещё он используется.
        name: "guest_access_key_ids",
        label: "Ключи доступа",
        type: "array",
        itemLabel: "ключ",
        createOnly: true,
        maxItems: 32,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        description:
          "Публичные ключи, с которыми вы войдёте в гостевую систему. Ключ — отдельный ресурс проекта: его можно отозвать, заменить и увидеть, где ещё он используется. Нет ни одного — создайте прямо здесь.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "Ключ доступа",
            type: "ref",
            refResource: "guest-access-keys",
            refProjectScoped: true,
            required: true,
            createResource: "guest-access-keys",
            createTitle: "Создать ключ доступа",
          },
        ],
      },
      // --- VM-specific (instanceKind = VM) ---
      {
        name: "vm_spec.user_data",
        label: "user-data (cloud-init)",
        type: "text",
        rows: 4,
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        placeholder: "#cloud-config\n…",
        description: "cloud-config / cloud-init user-data для VM.",
      },
      {
        name: "vm_spec.metadata_options.metadata_endpoint",
        label: "Metadata endpoint",
        type: "enum",
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        default: "ENABLED",
        options: [
          { value: "ENABLED", label: "ENABLED — доступен из гостя" },
          { value: "DISABLED", label: "DISABLED — недоступен" },
        ],
        description: "Доступность metadata-эндпоинта из гостевой ОС (vendor-agnostic).",
      },
      {
        name: "assign_external_address",
        label: "Внешний адрес",
        type: "bool",
        createOnly: true,
        default: false,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        description: "Запросить внешний IP-адрес для VM (F5).",
      },
      {
        name: "acknowledge_unreachable",
        label: "Допустить недостижимость",
        type: "bool",
        createOnly: true,
        default: false,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        description: "Подтвердить, что VM будет RUNNING, но недостижима (без ssh и без внешнего адреса).",
      },
      // --- CONTAINER-specific (instanceKind = CONTAINER) ---
      {
        name: "container_spec.restart_policy",
        label: "Restart policy",
        type: "enum",
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "CONTAINER" },
        default: "NEVER",
        options: [
          { value: "NEVER", label: "NEVER — не перезапускать" },
          { value: "ON_FAILURE", label: "ON_FAILURE — при ненулевом exit" },
          { value: "ALWAYS", label: "ALWAYS — всегда" },
        ],
        description: "Политика перезапуска контейнер-джобы.",
      },
      {
        name: "container_spec.working_dir",
        label: "Рабочая директория",
        type: "string",
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "CONTAINER" },
        placeholder: "/app",
        description: "Рабочая директория внутри контейнера.",
      },
      // --- network (F6) ---
      {
        name: "use_default_network",
        label: "Сеть по умолчанию",
        type: "bool",
        createOnly: true,
        default: true,
        description:
          "Использовать подсеть+SG проекта по умолчанию. Тонкую настройку интерфейсов делайте после создания на вкладке «Сетевые интерфейсы».",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      zone_id: "",
      instance_kind: "VM",
      machine_type_id: "",
      boot_source: { type: "storage.image", id: "" },
      cpu_guarantee_percent: 0,
      service_account_id: "",
      vm_spec: { user_data: "", metadata_options: { metadata_endpoint: "ENABLED" } },
      container_spec: { restart_policy: "NEVER", working_dir: "" },
      assign_external_address: false,
      acknowledge_unreachable: false,
      use_default_network: true,
      guest_access_key_ids: [],
      labels: {},
    }),
    // UI-форма → wire. Оставляем ровно одну ветку oneof spec по instance_kind;
    // boot_source режем до {type,id}; пустые опциональные скаляры не шлём.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const kind = out.instance_kind;

      // guest_access_key_ids: контракт ждёт ПЛОСКИЙ список идентификаторов, а
      // generic ArrayField хранит элемент объектом {value}. Пустой список не
      // шлём вовсе: пустое поле в теле означало бы «ключей нет», тогда как
      // арендатор просто не дошёл до него.
      out.guest_access_key_ids = flatIdList(out.guest_access_key_ids);
      if (!out.guest_access_key_ids) delete out.guest_access_key_ids;

      // boot_source: на вход только {type,id} (output-only/form-only поля срезаем).
      const bs = (out.boot_source as Record<string, unknown> | undefined) ?? {};
      out.boot_source = { type: bs.type, id: bs.id };

      if (kind === "CONTAINER") {
        delete out.vm_spec;
        delete out.assign_external_address;
        delete out.acknowledge_unreachable;
        const cs = { ...((out.container_spec as Record<string, unknown> | undefined) ?? {}) };
        if (!cs.working_dir) delete cs.working_dir;
        out.container_spec = cs;
      } else {
        delete out.container_spec;
        const vs = { ...((out.vm_spec as Record<string, unknown> | undefined) ?? {}) };
        if (!vs.user_data) delete vs.user_data;
        out.vm_spec = vs;
      }

      if (!out.service_account_id) delete out.service_account_id;
      return out;
    },
    // Клиент-валидация ДО submit — ровно тем же тоном, каким откажет сервер.
    //
    // Ветка `registry.image` объявлена в контракте и ОТВЕРГАЕТСЯ явно: у образа
    // из реестра сегодня нет durable-адреса (репозиторий адресуется парой
    // «реестр + имя», а имя переименовывается отдельным глаголом). Форма обязана
    // сказать это словами, а не отправлять запрос, который не может пройти:
    // подборщика образов у этой ветки нет by construction, и без пояснения
    // арендатор получил бы отказ про пустой идентификатор, а не про ветку.
    validate: (obj) => {
      const bs = (obj.boot_source as Record<string, unknown> | undefined) ?? {};
      if (bs.type === "registry.image") {
        return "Источник registry.image пока не принимается: у образа из реестра нет неизменяемого адреса, поэтому ссылка в машине сломалась бы после чужого переименования. Выберите storage.image.";
      }
      return null;
    },
    // wire → UI-форма (edit). service_account (Referrer) → service_account_id.
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const sa = obj.service_account as Record<string, unknown> | undefined;
      if (sa && typeof sa.id === "string") out.service_account_id = sa.id;
      return out;
    },
    emptyState: {
      title: "Создайте первую виртуальную машину",
      body: "Выберите тип инстанса (VM/CONTAINER), тип машины (sizing) и образ. ОС VM доставляется из storage.image, контейнер — из registry.image. Персистентные данные храните на томах Storage.",
      docs: ["Виртуальные машины"],
    },
  },

  // ====== compute: MachineType (read-only sizing catalog, F2/F7) ======
  // proto: kacho.cloud.compute.v1.MachineTypeService (/compute/v1/machineTypes).
  // Public read-only; admin-CRUD — InternalMachineTypeService (:9091, ban #6).
  // Cluster-scoped (без project). Также ref-цель для Instance.machine_type_id.
  "machine-types": {
    id: "machine-types",
    route: "machine-types",
    apiPath: "/compute/v1/machineTypes",
    payloadKey: "machine_types",
    singular: "Тип машины",
    accusative: "тип машины",
    plural: "Типы машин",
    genitive: "Типа машины",
    serviceTitle: "Compute Cloud",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Семейство", path: "family", format: "code" },
      { header: "vCPU", path: "effective_resources.v_cpu", format: "text" },
      {
        header: "Память",
        path: "effective_resources.memory_mib",
        render: (row) => (
          <MemMiBCell value={(row.effective_resources as Record<string, unknown> | undefined)?.memory_mib} />
        ),
      },
      { header: "GPU", path: "effective_resources.gpus", format: "text" },
      { header: "GPU-модель", path: "effective_resources.gpu_type", format: "code" },
      { header: "Зоны", path: "available_zones", format: "list" },
      { header: "Статус", path: "status", format: "status" },
    ],
    template: () => ({}),
    emptyState: {
      title: "Каталог типов машин пуст",
      body: "Типы машин задаёт администратор кластера (InternalMachineTypeService). Тип машины — единый канал размера инстанса (vCPU/память/GPU): выберите его при создании виртуальной машины.",
    },
  },

  // Группа размещения — правило взаимного размещения машин. Спека объявлена ОДИН
  // раз, в общем реестре, и здесь стоит ССЫЛКА на то же объявление: раздел
  // монтируют оба приложения (compute-remote и standalone-сборка vpc), а вторая
  // копия разошлась бы с первой молча — как уже разошлись копии формы ресурса.
  "placement-groups": SHARED_REGISTRY["placement-groups"],

  // ====== cross-service ref-цели (read-only, для RefSelect) ======
  // geo.Zone — zone_id при Create.
  zones: {
    id: "zones",
    route: "zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    accusative: "зону",
    plural: "Зоны",
    serviceTitle: "Geography",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [{ header: "Идентификатор", path: "id", format: "text", className: "font-mono" }],
    template: () => ({}),
  },

  // ====== compute: GuestAccessKey ======
  // proto: kacho.cloud.compute.v1.GuestAccessKeyService (/compute/v1/guestAccessKeys).
  // Мутации async → Operation. Mutable: name/labels; public_key задаётся при
  // создании и не правится — заменить ключ значит завести другой.
  //
  // Почему это ресурс, а не поле машины, сказано в самом контракте: ключ,
  // переданный полем, живёт ровно столько, сколько машина, и его нельзя ни
  // отозвать, ни заменить, ни узнать, где ещё он используется. Отсюда же и
  // отказ сервера на `sshPublicKeys` в запросе машины — он называет этот ресурс
  // заменой.
  //
  // Закрытая половина ключа здесь не хранится НИКОГДА и полем формы не является.
  "guest-access-keys": {
    id: "guest-access-keys",
    route: "guest-access-keys",
    apiPath: "/compute/v1/guestAccessKeys",
    payloadKey: "guest_access_keys",
    singular: "Ключ доступа",
    plural: "Ключи доступа",
    genitive: "Ключа доступа",
    accusative: "ключ доступа",
    serviceTitle: "Compute Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      // Отпечаток считаем МЫ — по нему арендатор сверяет, тот ли ключ доехал.
      { header: "Отпечаток", path: "fingerprint", format: "code" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    // Поля формы и шаблон — из ОДНОГО объявления (`@shared/lib/guest-access-key-form`):
    // тот же ресурс есть во втором реестре, и выписанные дважды поля разошлись бы
    // молча. Колонки остаются здесь — они несут разметку и берут атомы этого модуля.
    fields: GUEST_ACCESS_KEY_FIELDS,
    template: guestAccessKeyTemplate,
    emptyState: GUEST_ACCESS_KEY_EMPTY_STATE,
  },

  // storage.Image — источник загрузочного тома (project-scoped picker).
  // Здесь ТОЛЬКО цель ссылки: CRUD образа живёт в разделе Storage.
  images: {
    id: "images",
    route: "images",
    apiPath: "/storage/v1/images",
    payloadKey: "images",
    singular: "Образ",
    plural: "Образы",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },

  // storage.Volume — attach-disk picker (project-scoped).
  volumes: {
    id: "volumes",
    route: "volumes",
    apiPath: "/storage/v1/volumes",
    payloadKey: "volumes",
    singular: "Том",
    accusative: "том",
    plural: "Тома",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },

  // vpc.NetworkInterface — attach-NIC picker (project-scoped).
  "network-interfaces": {
    id: "network-interfaces",
    route: "network-interfaces",
    apiPath: "/vpc/v1/networkInterfaces",
    payloadKey: "network_interfaces",
    singular: "Сетевой интерфейс",
    accusative: "сетевой интерфейс",
    plural: "Сетевые интерфейсы",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },
};

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// Домен-владелец и сборка SPA-адреса — ОДНА реализация на дерево, в @shared.
// Здесь стояла своя: она возвращала `compute` на любой идентификатор, поэтому
// ссылка на сетевой интерфейс (vpc), том (storage) и зону (глобальный каталог)
// с карточки машины адресовалась сегментом compute-remote'а, маршрута такого у
// него нет, и catch-all выбрасывал человека обратно на список машин.
//
// Реестр остаётся модульным (в нём ровно те ресурсы, что показывает модуль), а
// правило сборки адреса — общее: `resourceListPath` принимает маршрут спеки.
export { resourceServicePrefix, resourceListPath, isSystemScopedResource };
export type { ServicePrefix };

export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  const spec = REGISTRY[specId];
  if (!spec) return null;
  return resourceListPath(specId, spec.route, projectId);
}

export function getByPath<T = unknown>(obj: unknown, path: string): T | undefined {
  return path.split(".").reduce<unknown>((acc, key) => {
    if (acc == null || typeof acc !== "object") return undefined;
    return (acc as Record<string, unknown>)[key];
  }, obj) as T | undefined;
}

export function applyFieldDefaults(
  fields: FormField[] | undefined,
  obj: Record<string, unknown>,
): Record<string, unknown> {
  if (!fields) return obj;
  let cur = obj;
  for (const f of fields) {
    // An update-only field is never seeded. On Create its message has no such
    // field at all, so a default would ride out as an unknown key and be dropped
    // in silence; on Update the value comes from the fetched resource, and seeding
    // one would push a field the operator never touched into update_mask.
    // ONE guard, ahead of the type split — a guard inside a per-type branch fixes
    // the field that prompted it and leaves the next one of another type open.
    if (f.updateOnly) continue;
    if (f.type !== "string" && f.type !== "int" && f.type !== "enum" && f.type !== "bool") continue;
    if (f.default === undefined) continue;
    cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
  }
  return cur;
}
