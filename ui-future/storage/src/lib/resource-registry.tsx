// Реестр ресурсов storage-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC/NLB-remote. Домен — Storage:
// Volume (том, tenant-facing) / Snapshot (снимок тома) / DiskType (каталог типов
// дисков, read-only). Мутации async → Operation-poll. `zones` — cross-service
// ref-цель (owner geo) для поля zone_id.

import type { ReactNode } from "react";
import { Tooltip, Typography } from "antd";
import type { FormField } from "@shared/lib/form-schema";
import { setByPath } from "./path";
import { formatBytes } from "./bytes";
import { acceptsNewVolumes, lifecycleLabel, tierLabel, LIFECYCLE_HINT, TIER_HINT } from "./storage-enums";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import type { ResourceColumn, ResourceSpec } from "@shared/lib/resource-spec";

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`, и импортируется
// сюда. Реэкспорт оставлен, чтобы потребители этого модуля не меняли импорты: у него
// нет тела, поэтому разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы
// здесь запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.

export type { ResourceColumn, ResourceSpec };

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

// TierCell / LifecycleCell — закрытые словари класса диска словами, а не
// токенами перечисления. Подписи и пояснения — в `lib/storage-enums`, чтобы у
// текста было ОДНО место: тот же словарь читают карточка класса и подпись опции
// в подборщике.
function TierCell({ value }: { value: unknown }): ReactNode {
  const label = tierLabel(value);
  if (!label) return <Typography.Text type="secondary">—</Typography.Text>;
  const hint = typeof value === "string" ? TIER_HINT[value] : undefined;
  return hint ? <Tooltip title={hint}>{label}</Tooltip> : <>{label}</>;
}

function LifecycleCell({ value }: { value: unknown }): ReactNode {
  const label = lifecycleLabel(value);
  if (!label) return <Typography.Text type="secondary">—</Typography.Text>;
  const hint = typeof value === "string" ? LIFECYCLE_HINT[value] : undefined;
  // Цветом выделяется только то, о чём стоит знать: класс, который НЕ принимает
  // новые тома. Красить и «принимает» значило бы не выделять ничего.
  const body = acceptsNewVolumes(value) ? <>{label}</> : <Typography.Text type="warning">{label}</Typography.Text>;
  return hint ? <Tooltip title={hint}>{body}</Tooltip> : body;
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== storage: Volume ======
  // proto: kacho.cloud.storage.v1.VolumeService (/storage/v1/volumes). Мутации
  // async → Operation. Mutable: name/description/labels/size_bytes(increase-only).
  // Immutable: zone_id/source_snapshot_id/source_image_id.
  //
  // disk_type_id в update_mask НЕ входит вовсе: класс меняется отдельным глаголом
  // `:changeDiskType` — смена класса это перемещение данных, а не правка поля.
  // Здесь он объявлен `immutable`, чтобы форма правки его не отправляла.
  //
  // `block_size` снят с контракта (`reserved 11`) и отсюда тоже: он принимался,
  // хранился и возвращался, но ни один путь не читал его значение — арендатор
  // читал назад собственный ввод и принимал зеркало за подтверждение.
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
      // Зона и класс диска — ссылки на ЧУЖИЕ ресурсы, значит ссылки (правило 2):
      // идентификатор вида `zone-…` пользователю не адресован, он работает с
      // именем. Зона — глобальный каталог geo, и `RefNameLink` спрашивает её без
      // `project_id`: измерения «проект» у каталога нет.
      {
        header: "Зона",
        path: "zone_id",
        render: (row) => <RefNameLink specId="zones" refId={row.zone_id as string | undefined} maxChars={28} />,
      },
      {
        header: "Тип диска",
        path: "disk_type_id",
        render: (row) => (
          <RefNameLink specId="disk-types" refId={row.disk_type_id as string | undefined} maxChars={28} />
        ),
      },
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
        description:
          "Класс хранилища тома. Правкой не меняется — для переезда на другой класс есть отдельное действие «Сменить класс диска» на карточке тома. Класс, выведенный из обращения, помечен в списке: новые тома он не принимает.",
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
  //
  // `zone_id` — СОБСТВЕННЫЙ якорь размещения снимка: output-only (снимается с
  // зоны исходного тома на Create) и неизменяемый, поэтому полем формы он не
  // является. Показывается — потому что копия (`:copy`) переносит снимок в
  // другую зону, и без якоря непонятно, откуда и куда.
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
      {
        header: "Зона",
        path: "zone_id",
        render: (row) => <RefNameLink specId="zones" refId={row.zone_id as string | undefined} maxChars={28} />,
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
      // Регион — глобальный каталог geo: ссылка (правило 2), запрос без
      // `project_id`.
      {
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
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
  //
  // Класс несёт ПОЛИТИКУ: ярус (закрытый словарь), состояние обращения, границы
  // размера, способности. Чисел производительности, координаты бэкенда, имени
  // пула и шаблона пространства имён на этой поверхности нет и не будет — они
  // живут на ревизии привязки (:9091) и меняются вместе с бэкендом.
  //
  // Прежняя колонка «Тариф» читала `performance_tier` — СВОБОДНУЮ строку, снятую
  // с контракта вместе с номером и именем. Она бы показывала пустую ячейку
  // вечно.
  "disk-types": {
    id: "disk-types",
    route: "disk-types",
    apiPath: "/storage/v1/diskTypes",
    payloadKey: "disk_types",
    singular: "Тип диска",
    plural: "Типы дисков",
    genitive: "Типа диска",
    description:
      "Класс хранилища, на котором создаётся том: ярус, состояние обращения, границы размера и способности. Каталог заводит администратор кластера; пустой каталог — законное состояние, пока класс не зарегистрирован, том не создаётся.",
    serviceTitle: "Storage",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text", className: "font-medium" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      {
        header: "Ярус",
        path: "tier",
        render: (row) => <TierCell value={row.tier} />,
      },
      // Состояние обращения названо СЛЕДСТВИЕМ (правило 6): «Принимает новые
      // тома» / «Новые тома не создаются». Токен `DEPRECATED` не говорит
      // читателю ни того, что класс ещё работает, ни того, что на нём нельзя
      // создать новый том, — а вопрос у этого поля ровно один.
      {
        header: "Обращение",
        path: "lifecycle",
        render: (row) => <LifecycleCell value={row.lifecycle} />,
      },
      { header: "Зоны", path: "zone_ids", format: "list" },
    ],
    template: () => ({}),
    emptyState: {
      title: "Каталог типов дисков пуст",
      body: "Класс диска описывает хранилище, на котором создаётся том: ярус, границы размера, способности. Каталог заводит администратор кластера — пока класс не зарегистрирован, том создать нельзя.",
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

/** Cluster-scoped каталог размещения: смонтирован под `/system/*`, а не внутри
 *  проекта. Тот же перечень, что в реестре shared, — и по той же причине: прогон
 *  этих ресурсов через project-scoped ветку даёт путь, которого нет, и ссылка
 *  ведёт в никуда. Storage ссылается на них с карточек тома, снимка и образа
 *  (зона, регион), поэтому ветка нужна и здесь. */
const SYSTEM_SCOPED = new Set(["regions", "zones"]);

// resourceProjectPath — полный SPA-путь до listing ресурса в контексте project'а.
export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  const spec = REGISTRY[specId];
  if (!spec) return null;
  // Проверка ДО требования projectId: у глобального каталога измерения «проект»
  // нет вовсе, и требовать его значило бы не строить ссылку там, где проекта в
  // контексте нет.
  if (SYSTEM_SCOPED.has(specId)) return `/system/${spec.route}`;
  if (!projectId) return null;
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
