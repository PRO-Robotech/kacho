// Реестр ресурсов storage-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC/NLB-remote. Домен — Storage:
// Volume (том, tenant-facing) / Snapshot (снимок тома) / DiskType (каталог типов
// дисков, read-only). Мутации async → Operation-poll. `zones` — cross-service
// ref-цель (owner geo) для поля zone_id.

import type { ReactNode } from "react";
import { Typography } from "antd";
import type { FormField } from "./form-schema";
import { setByPath } from "./path";
import { formatBytes } from "./bytes";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import { RefNameLink } from "@/components/molecules/RefNameLink";

export interface ResourceColumn {
  header: string;
  // Путь в плоском объекте: "name", "status", "zone_id"
  path: string;
  format?: "text" | "uid-short" | "datetime" | "status" | "code" | "list" | "references";
  className?: string;
  render?: (row: Record<string, unknown>) => ReactNode;
}

export interface ResourceSpec {
  id: string;
  // route path в SPA (без leading slash)
  route: string;
  // Полный URL-path для REST: /<domain>/v1/<plural>
  apiPath: string;
  // ключ массива в List response
  payloadKey: string;
  // singular label для UI
  singular: string;
  // plural label
  plural: string;
  // родительный падеж ед.ч. — заголовок мастер-ресурса в зоне обзора. Fallback: plural.
  genitive?: string;
  description?: string;
  /** Service-domain заголовок (в breadcrumb перед именем категории). */
  serviceTitle?: string;
  // global = cluster-scoped, project = в выбранном Project, account = в Account
  scope: "global" | "project" | "account";
  // поддерживаемые операции (кнопки действий рендерятся по этим флагам)
  ops: {
    create: boolean;
    update: boolean;
    delete: boolean;
    restart?: boolean;
    start?: boolean;
    stop?: boolean;
  };
  // колонки для list-таблицы
  columns: ResourceColumn[];
  // schema полей формы (если undefined — fallback к JSON-editor)
  fields?: FormField[];
  // Path-template для drill-down link при клике на строку (плейсхолдер `:id`).
  childRoute?: string;
  // skeleton-объект для Create-формы.
  template: (ctx: { projectId?: string; accountId?: string }) => unknown;
  // Нормализация payload перед отправкой на API (form-internal → wire).
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
  // Обратная sanitize: wire → form (edit-режим).
  hydrate?: (obj: Record<string, unknown>) => Record<string, unknown>;
  // Клиентская валидация ДО submit (Create). Возвращает текст ошибки или null.
  validate?: (obj: Record<string, unknown>) => string | null;
  /** Path-template для internal/infra-проекции ресурса (плейсхолдер `{id}`). */
  internalGetPath?: string;
  /** Связанные дочерние ресурсы — отдельные табы во ResourceShell. */
  related?: { childId: string; filterField: string | string[]; label?: string }[];
  /** Facet-фильтр списка (client-side по значению поля). */
  facet?: { path: string; label: string; options: { value: string; label: string }[] };
  /** Грузить ВСЕ страницы списка (follow next_page_token) до рендера. */
  loadAllPages?: boolean;
  /** Ссылки на документацию (блок «Документация» в aside DetailShell). */
  docs?: { label: string; href: string }[];
  /** Welcome-копирайт для пустой таблицы этого ресурса. */
  emptyState?: { title: string; body: string; docs?: string[] };
}

// ── Общие FormField-константы ──

// Имя тома/снимка — DNS-1123 (lowercase + цифры + дефисы/подчёркивания).
const FIELD_NAME: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-volume",
  description:
    "Строчные латинские буквы, цифры, «-» и «_». Должно начинаться с буквы, длина до 63 символов. Можно оставить пустым.",
  pattern: "^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$",
};

const FIELD_DESCRIPTION: FormField = {
  name: "description",
  label: "Описание",
  type: "text",
  rows: 2,
  placeholder: "Краткое описание ресурса (опционально)",
};

const FIELD_PROJECT_ID: FormField = {
  name: "project_id",
  label: "Project",
  type: "string",
  hidden: true,
};

const FIELD_LABELS: FormField = {
  name: "labels",
  label: "Метки",
  type: "labels",
};

const GIB = 1024 * 1024 * 1024;

// SizeCell — размер (байты int64 строкой) в человекочитаемом виде; пусто/0 → «—».
function SizeCell({ value }: { value: unknown }): ReactNode {
  const s = formatBytes(value);
  return s === "—" ? <Typography.Text type="secondary">—</Typography.Text> : <>{s}</>;
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== storage: Volume ======
  // proto: kacho.cloud.storage.v1.VolumeService (/storage/v1/volumes). Мутации
  // async → Operation. Mutable: name/description/labels/size_bytes(increase-only).
  // Immutable: zone_id/disk_type_id/block_size/source_snapshot_id.
  volumes: {
    id: "volumes",
    route: "volumes",
    apiPath: "/storage/v1/volumes",
    payloadKey: "volumes",
    singular: "Том",
    plural: "Тома",
    genitive: "Тома",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    docs: [
      { label: "Тома (block storage)", href: "#" },
      { label: "Снимки томов", href: "#" },
    ],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Зона", path: "zone_id", format: "text" },
      { header: "Тип диска", path: "disk_type_id", format: "text" },
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
      { header: "Статус", path: "status", format: "status" },
      // used_by° — output-only зеркало attachments (кто использует том). Generic
      // "references"-рендер (spec-columns): показывает первого потребителя + «+N».
      { header: "Используется", path: "used_by", format: "references" },
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
        description: "Зона размещения тома (ZONAL placement, immutable после Create). Cross-service ref → geo.Zone.",
      },
      {
        name: "disk_type_id",
        label: "Тип диска",
        type: "ref",
        refResource: "disk-types",
        required: true,
        immutable: true,
        description: "Класс хранилища тома (immutable после Create). Каталог DiskType.",
      },
      {
        // Размер тома. Wire-поле — size_bytes (int64). UI вводит в ГиБ, sanitize
        // переводит в байты. Размер задаётся при Create; resize (increase-only)
        // не выведен в форму редактирования (editHidden) — mask строится по имени
        // поля, а size_gib не является wire-полем.
        name: "size_gib",
        label: "Размер, ГиБ",
        type: "int",
        required: true,
        min: 1,
        max: 4096,
        default: 10,
        editHidden: true,
        description: "Размер тома в гибибайтах (ГиБ), задаётся при создании.",
      },
      {
        name: "source_snapshot_id",
        label: "Из снимка",
        type: "ref",
        refResource: "snapshots",
        refProjectScoped: true,
        required: false,
        immutable: true,
        description: "Необязательно: восстановить том из снимка (immutable после Create). Пусто — чистый том.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      zone_id: "",
      disk_type_id: "",
      size_gib: 10,
      source_snapshot_id: "",
      labels: {},
    }),
    // size_gib (UI) → size_bytes (wire). Пустой source_snapshot_id не шлём.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const gib = Number(out.size_gib);
      if (Number.isFinite(gib) && gib > 0) out.size_bytes = String(Math.round(gib) * GIB);
      delete out.size_gib;
      if (!out.source_snapshot_id) delete out.source_snapshot_id;
      return out;
    },
    // size_bytes (wire) → size_gib (UI) для edit-формы.
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const bytes = typeof obj.size_bytes === "string" ? Number.parseInt(obj.size_bytes, 10) : Number(obj.size_bytes);
      if (Number.isFinite(bytes) && bytes > 0) out.size_gib = Math.max(1, Math.round(bytes / GIB));
      return out;
    },
    emptyState: {
      title: "Создайте первый том",
      body: "Том — это персистентный блочный диск. ОС инстанса доставляется из OCI-образа, а данные живут на подключённых томах. После создания том можно подключить к виртуальной машине в разделе Compute.",
      docs: ["Тома (block storage)"],
    },
  },

  // ====== storage: Snapshot ======
  // proto: kacho.cloud.storage.v1.SnapshotService (/storage/v1/snapshots).
  // Создаётся ИЗ тома (source_volume_id). Мутации async → Operation.
  snapshots: {
    id: "snapshots",
    route: "snapshots",
    apiPath: "/storage/v1/snapshots",
    payloadKey: "snapshots",
    singular: "Снимок",
    plural: "Снимки",
    genitive: "Снимка",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      {
        header: "Исходный том",
        path: "source_volume_id",
        render: (row) => (
          <RefNameLink specId="volumes" refId={row.source_volume_id as string | undefined} maxChars={32} />
        ),
      },
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      {
        name: "source_volume_id",
        label: "Исходный том",
        type: "ref",
        refResource: "volumes",
        refProjectScoped: true,
        required: true,
        immutable: true,
        description:
          "Том, с которого снимается point-in-time копия (immutable после Create). Within-service ref → Volume.",
      },
      FIELD_NAME,
      FIELD_DESCRIPTION,
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      source_volume_id: "",
      name: "",
      description: "",
      labels: {},
    }),
    emptyState: {
      title: "Создайте первый снимок",
      body: "Снимок — это point-in-time копия тома. Выберите том-источник, чтобы создать снимок; из снимка позже можно восстановить новый том.",
      docs: ["Снимки томов"],
    },
  },

  // ====== storage: Image (STOR-1 — boot-image ресурс) ======
  // proto: kacho.cloud.storage.v1.ImageService (/storage/v1/images). REGIONAL/anycast
  // (region_id). Создаётся РОВНО из одного источника — Snapshot XOR Volume (immutable).
  // Mutable: name/description/labels. size/min_disk/format/status — output-only.
  images: {
    id: "images",
    route: "images",
    apiPath: "/storage/v1/images",
    payloadKey: "images",
    singular: "Образ",
    plural: "Образы",
    genitive: "Образа",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    docs: [
      { label: "Образы (boot-image)", href: "#" },
      { label: "Тома (block storage)", href: "#" },
    ],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Регион", path: "region_id", format: "text" },
      {
        header: "Источник",
        path: "source_snapshot_id",
        render: (row) => {
          const snap = row.source_snapshot_id as string | undefined;
          const vol = row.source_volume_id as string | undefined;
          if (snap) return <RefNameLink specId="snapshots" refId={snap} maxChars={28} />;
          if (vol) return <RefNameLink specId="volumes" refId={vol} maxChars={28} />;
          return <Typography.Text type="secondary">—</Typography.Text>;
        },
      },
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
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
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        description:
          "Регион размещения образа (REGIONAL/anycast, immutable после Create). Cross-service ref → geo.Region.",
      },
      {
        // Дискриминатор источника (form-only): образ создаётся РОВНО из одного —
        // снимок XOR том. sanitize срезает `_source_kind` и неактивную ветку.
        name: "_source_kind",
        label: "Источник образа",
        type: "enum",
        required: true,
        createOnly: true,
        default: "snapshot",
        options: [
          { value: "snapshot", label: "Из снимка (Snapshot)" },
          { value: "volume", label: "Из тома (Volume)" },
        ],
        description: "Образ создаётся РОВНО из одного источника: снимок ИЛИ том (взаимоисключающе).",
      },
      {
        name: "source_snapshot_id",
        label: "Снимок-источник",
        type: "ref",
        refResource: "snapshots",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        visibleWhen: { field: "_source_kind", equals: "snapshot" },
        description: "Снимок, из которого создаётся образ (immutable). Same-DB ref → Snapshot.",
      },
      {
        name: "source_volume_id",
        label: "Том-источник",
        type: "ref",
        refResource: "volumes",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        visibleWhen: { field: "_source_kind", equals: "volume" },
        description: "Том, из которого создаётся образ (immutable). Same-DB ref → Volume.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      _source_kind: "snapshot",
      source_snapshot_id: "",
      source_volume_id: "",
      labels: {},
    }),
    // Ровно один источник по _source_kind; form-only дискриминатор срезаем.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const kind = out._source_kind;
      delete out._source_kind;
      if (kind === "volume") {
        delete out.source_snapshot_id;
        if (!out.source_volume_id) delete out.source_volume_id;
      } else {
        delete out.source_volume_id;
        if (!out.source_snapshot_id) delete out.source_snapshot_id;
      }
      return out;
    },
    // Клиент-валидация ДО submit: активный источник должен быть выбран.
    validate: (obj) => {
      const kind = obj._source_kind;
      const chosen = kind === "volume" ? obj.source_volume_id : obj.source_snapshot_id;
      if (!chosen) return "Выберите источник образа (снимок или том).";
      return null;
    },
    emptyState: {
      title: "Создайте первый образ",
      body: "Образ — это boot-seed для тома: том с указанным образом материализуется из него. Образ REGIONAL (anycast) и создаётся из снимка или тома проекта.",
      docs: ["Образы (boot-image)"],
    },
  },

  // ====== storage: DiskType (read-only catalog) ======
  // proto: kacho.cloud.storage.v1.DiskTypeService (/storage/v1/diskTypes). Public
  // read-only; admin-CRUD — Internal* API (:9091). Cluster-scoped (без project).
  // Также ref-цель для Volume.disk_type_id.
  "disk-types": {
    id: "disk-types",
    route: "disk-types",
    apiPath: "/storage/v1/diskTypes",
    payloadKey: "disk_types",
    singular: "Тип диска",
    plural: "Типы дисков",
    genitive: "Типа диска",
    serviceTitle: "Storage",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      { header: "Имя", path: "name", format: "text" },
      { header: "Описание", path: "description", format: "text" },
      { header: "Тариф", path: "performance_tier", format: "code" },
      { header: "Зоны", path: "zone_ids", format: "list" },
    ],
    template: () => ({}),
    emptyState: {
      title: "Каталог типов дисков пуст",
      body: "Типы дисков задаёт администратор кластера. Тип диска описывает класс хранилища, на котором создаётся том.",
    },
  },

  // ====== geo (read-only ref-цели) ======
  // Zone — cross-service ref-цель (owner geo) для Volume/Snapshot.zone_id. Read-only
  // registry-запись нужна RefSelect'у для резолва apiPath/payloadKey/имени в dropdown'е.
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

  // Region — cross-service ref-цель (owner geo) для Image.region_id (REGIONAL/anycast).
  regions: {
    id: "regions",
    route: "regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    plural: "Регионы",
    serviceTitle: "Geography",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [{ header: "Идентификатор", path: "id", format: "text", className: "font-mono" }],
    template: () => ({}),
  },
};

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// resourceServicePrefix — service-segment под /projects/:projectId/ per spec.id.
// Все навигируемые ресурсы этого remote принадлежат домену Storage → префикс
// маршрута `storage`. `zones` — ref-цель (не навигируется), но prefix задаём для
// полноты.
export function resourceServicePrefix(_specId: string): "storage" {
  return "storage";
}

// resourceProjectPath — полный SPA-путь до listing ресурса в контексте project'а.
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

// applyDefaults — для Create-формы прогоняем все поля и подставляем default-ы.
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
