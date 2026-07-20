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
import type { FormField } from "./form-schema";
import { setByPath } from "./path";
import { formatBytes } from "./bytes";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { LabelsCell } from "@/components/atoms/LabelsCell";

export interface ResourceColumn {
  header: string;
  path: string;
  format?: "text" | "uid-short" | "datetime" | "status" | "code" | "list" | "references";
  className?: string;
  render?: (row: Record<string, unknown>) => ReactNode;
}

export interface ResourceSpec {
  id: string;
  route: string;
  apiPath: string;
  payloadKey: string;
  singular: string;
  plural: string;
  genitive?: string;
  description?: string;
  serviceTitle?: string;
  scope: "global" | "project" | "account";
  ops: {
    create: boolean;
    update: boolean;
    delete: boolean;
    restart?: boolean;
    start?: boolean;
    stop?: boolean;
  };
  columns: ResourceColumn[];
  fields?: FormField[];
  childRoute?: string;
  template: (ctx: { projectId?: string; accountId?: string }) => unknown;
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
  hydrate?: (obj: Record<string, unknown>) => Record<string, unknown>;
  validate?: (obj: Record<string, unknown>) => string | null;
  internalGetPath?: string;
  related?: { childId: string; filterField: string | string[]; label?: string }[];
  facet?: { path: string; label: string; options: { value: string; label: string }[] };
  loadAllPages?: boolean;
  docs?: { label: string; href: string }[];
  emptyState?: { title: string; body: string; docs?: string[] };
}

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
        description: "Зона размещения инстанса (immutable после Create). Cross-service ref → geo.Zone.",
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
          "Сильный первый дискриминатор (immutable после Create): VM запускает ОС из storage.image; CONTAINER — эфемерный rootfs из OCI registry.image.",
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
        name: "boot_source.id",
        label: "Образ",
        type: "string",
        required: true,
        createOnly: true,
        placeholder: "img-9k2m4x7q1n8p:22.04-lts   |   ml/bert-trainer:cu121",
        description:
          "Ссылка на образ с тегом/дайджестом внутри id: «img-<base32>:<tag>» / «img-<base32>@sha256:<hex>» (storage.image) либо «repo/name:tag» (registry.image).",
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
        name: "ssh_public_keys",
        label: "SSH-ключи",
        type: "text",
        rows: 3,
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        placeholder: "ssh-ed25519 AAAA… user@host\n(по одному ключу на строку)",
        description:
          "SSH-ключи (по одному на строку). Без ssh-ключа и без внешнего адреса VM недостижима — включите внешний адрес или отметьте «допустить недостижимость».",
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
      ssh_public_keys: "",
      assign_external_address: false,
      acknowledge_unreachable: false,
      use_default_network: true,
      labels: {},
    }),
    // UI-форма → wire. Оставляем ровно одну ветку oneof spec по instance_kind;
    // boot_source режем до {type,id}; ssh_public_keys (textarea) → string[];
    // пустые опциональные скаляры не шлём.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const kind = out.instance_kind;

      // boot_source: на вход только {type,id} (output-only/form-only поля срезаем).
      const bs = (out.boot_source as Record<string, unknown> | undefined) ?? {};
      out.boot_source = { type: bs.type, id: bs.id };

      if (kind === "CONTAINER") {
        delete out.vm_spec;
        delete out.ssh_public_keys;
        delete out.assign_external_address;
        delete out.acknowledge_unreachable;
        const cs = { ...((out.container_spec as Record<string, unknown> | undefined) ?? {}) };
        if (!cs.working_dir) delete cs.working_dir;
        out.container_spec = cs;
      } else {
        delete out.container_spec;
        // ssh_public_keys: textarea → string[] (одна строка = один ключ), пустые срезаем.
        const raw = typeof out.ssh_public_keys === "string" ? out.ssh_public_keys : "";
        const keys = raw
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean);
        if (keys.length > 0) out.ssh_public_keys = keys;
        else delete out.ssh_public_keys;
        const vs = { ...((out.vm_spec as Record<string, unknown> | undefined) ?? {}) };
        if (!vs.user_data) delete vs.user_data;
        out.vm_spec = vs;
      }

      if (!out.service_account_id) delete out.service_account_id;
      return out;
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

  // ====== cross-service ref-цели (read-only, для RefSelect) ======
  // geo.Zone — zone_id при Create.
  zones: {
    id: "zones",
    route: "zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    plural: "Зоны",
    serviceTitle: "Geography",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [{ header: "Идентификатор", path: "id", format: "text", className: "font-mono" }],
    template: () => ({}),
  },

  // storage.Volume — attach-disk picker (project-scoped).
  volumes: {
    id: "volumes",
    route: "volumes",
    apiPath: "/storage/v1/volumes",
    payloadKey: "volumes",
    singular: "Том",
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

// resourceServicePrefix — service-segment под /projects/:projectId/ per spec.id.
// Навигируемые ресурсы remote'а — инстанс + каталог типов машин (сегмент `compute`).
// Ref-цели (zones/volumes/network-interfaces) не навигируются в этом remote.
export function resourceServicePrefix(_specId: string): "compute" {
  return "compute";
}

export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  if (!projectId) return null;
  const spec = REGISTRY[specId];
  if (!spec) return null;
  const prefix = resourceServicePrefix(specId);
  return `/projects/${projectId}/${prefix}/${spec.route}`;
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
    if (f.type === "string" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "int" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "enum" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "bool" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    }
  }
  return cur;
}
