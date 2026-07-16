// Реестр ресурсов compute-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC/NLB-remote. Домен — Compute: Instance
// (виртуальная машина). Мутации async → Operation-poll. ОС инстанса доставляется
// из OCI-образа (image), персистентные данные — на подключённых storage-томах.
//
// `zones` (geo) / `volumes` (storage) / `network-interfaces` (vpc) — cross-service
// ref-цели для RefSelect (zone_id при Create, attach-disk / attach-NIC на detail).

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
  placeholder: "Краткое описание виртуальной машины (опционально)",
};

const FIELD_PROJECT_ID: FormField = { name: "project_id", label: "Project", type: "string", hidden: true };
const FIELD_LABELS: FormField = { name: "labels", label: "Метки", type: "labels" };

const GIB = 1024 * 1024 * 1024;

function SizeCell({ value }: { value: unknown }): ReactNode {
  const s = formatBytes(value);
  return s === "—" ? <Typography.Text type="secondary">—</Typography.Text> : <>{s}</>;
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== compute: Instance ======
  // proto: kacho.cloud.compute.v1.InstanceService (/compute/v1/instances).
  // Create требует: project_id, zone_id, platform_id, resources_spec{cores>0,
  // memory>0, cpu_guarantee_percent 0..100}; image опционален. Мутируемые Update-
  // поля выведены минимально (name/description/labels) — sizing/re-pin образа
  // требуют STOPPED и вынесены из формы (createOnly).
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
      { label: "Диски и снимки (Storage)", href: "#" },
    ],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Зона", path: "zone_id", format: "text" },
      { header: "Платформа", path: "platform_id", format: "code" },
      { header: "vCPU", path: "resources.cores", format: "text" },
      { header: "Память", path: "resources.memory", render: (row) => <SizeCell value={(row.resources as Record<string, unknown> | undefined)?.memory} /> },
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
        name: "platform_id",
        label: "Платформа",
        type: "string",
        required: true,
        createOnly: true,
        default: "standard-v3",
        description: "Аппаратная платформа инстанса (задаётся при создании).",
      },
      {
        name: "image",
        label: "OCI-образ (ОС)",
        type: "string",
        createOnly: true,
        placeholder: "cr.kacho.cloud/<project>/ubuntu:22.04",
        description: "Ссылка на OCI-образ (kacho-registry), из которого доставляется ОС. Rootfs эфемерный.",
      },
      {
        name: "resources_spec.cores",
        label: "vCPU",
        type: "int",
        required: true,
        createOnly: true,
        min: 1,
        max: 80,
        default: 2,
        description: "Число ядер (vCPU) инстанса.",
      },
      {
        name: "memory_gib",
        label: "Память, ГиБ",
        type: "int",
        required: true,
        createOnly: true,
        min: 1,
        max: 256,
        default: 2,
        description: "Объём оперативной памяти в гибибайтах (ГиБ).",
      },
      {
        name: "resources_spec.cpu_guarantee_percent",
        label: "Гарантия CPU, %",
        type: "int",
        createOnly: true,
        min: 0,
        max: 100,
        default: 0,
        description: "Гарантированный baseline CPU на vCPU в процентах (0 — best-effort/burstable; 1..100 — гарантия).",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      zone_id: "",
      platform_id: "standard-v3",
      image: "",
      resources_spec: { cores: 2, cpu_guarantee_percent: 0 },
      memory_gib: 2,
      labels: {},
    }),
    // memory_gib (UI, ГиБ) → resources_spec.memory (байты). Пустой image не шлём.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const rs = { ...((out.resources_spec as Record<string, unknown> | undefined) ?? {}) };
      const gib = Number(out.memory_gib);
      if (Number.isFinite(gib) && gib > 0) rs.memory = String(Math.round(gib) * GIB);
      out.resources_spec = rs;
      delete out.memory_gib;
      if (!out.image) delete out.image;
      return out;
    },
    emptyState: {
      title: "Создайте первую виртуальную машину",
      body: "Инстанс запускается из OCI-образа (поле «OCI-образ»). Персистентные данные храните на томах Storage — их можно подключить к инстансу после создания.",
      docs: ["Виртуальные машины"],
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
// Навигируемый ресурс remote'а — инстанс (сегмент `compute`). Ref-цели (zones/
// volumes/network-interfaces) не навигируются в этом remote.
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
