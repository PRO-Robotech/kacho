// Реестр ресурсов registry-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC-remote. Домен — Container Registry:
// Registry (реестр, tenant-facing) → Repository (появляется при docker push,
// read-only) → Tag (тег образа; единственная мутация — DeleteTag, async).

import type { ReactNode } from "react";
import { Typography } from "antd";
import type { FormField } from "./form-schema";
import { setByPath } from "./path";
import { formatBytes } from "./bytes";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import { ArtifactTypesTag } from "@/components/atoms/ArtifactTypeTag";

export interface ResourceColumn {
  header: string;
  // Путь в плоском объекте: "name", "status", "region_id"
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
  /** Facet-фильтр списка (client-side по значению поля): напр. тип артефакта
   *  образа. Рендерит Select рядом с поиском, фильтрует загруженные строки. */
  facet?: { path: string; label: string; options: { value: string; label: string }[] };
  /** Грузить ВСЕ страницы списка (follow next_page_token) до рендера — чтобы
   *  client-side facet видел полный набор (path-scoped дети, напр. образы). */
  loadAllPages?: boolean;
  /** Ссылки на документацию (блок «Документация» в aside DetailShell). */
  docs?: { label: string; href: string }[];
  /** Welcome-копирайт для пустой таблицы этого ресурса. */
  emptyState?: { title: string; body: string; docs?: string[] };
}

// ── Общие FormField-константы ──

// Имя реестра — DNS-safe (lowercase + цифры + дефисы). Mutable: сменить можно и
// после создания — OCI-путь образа строится по идентификатору реестра, не по имени,
// поэтому переименование не ломает docker pull/push. Конфликт имени → ALREADY_EXISTS.
const FIELD_NAME_REGISTRY: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  required: true,
  placeholder: "my-registry",
  description:
    "Строчные латинские буквы, цифры и «-». Должно начинаться с буквы, длина до 63 символов. Можно изменить позже — имя не входит в OCI-путь (тот по идентификатору).",
  pattern: "^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$",
};

const FIELD_DESCRIPTION: FormField = {
  name: "description",
  label: "Описание",
  type: "text",
  rows: 2,
  placeholder: "Краткое описание реестра (опционально)",
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

// SizeCell — ячейка размера (байты int64 строкой) в человекочитаемом виде;
// пусто/0 → приглушённое «—» (в стиле datetime/text-ячеек), не «0 B».
function SizeCell({ value }: { value: unknown }): ReactNode {
  const s = formatBytes(value);
  return s === "—" ? <Typography.Text type="secondary">—</Typography.Text> : <>{s}</>;
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== registry ======
  // proto: kacho.cloud.registry.v1.RegistryService. Registry — tenant-facing
  // реестр контейнерных образов (project-scoped). Мутации async → Operation.

  registries: {
    id: "registries",
    route: "registries",
    apiPath: "/registry/v1/registries",
    payloadKey: "registries",
    singular: "Реестр",
    plural: "Реестры",
    genitive: "Реестра",
    serviceTitle: "Container Registry",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    docs: [
      { label: "Реестры контейнеров", href: "#" },
      { label: "Публикация образов (docker login / push)", href: "#" },
    ],
    // Репозитории — дочерний ресурс: появляются при docker push в реестр.
    // Отдельный registry-driven таб (read-only список, без CTA «Создать»).
    related: [{ childId: "repositories", filterField: "registry_id", label: "Репозитории" }],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      // REG-1 F4: registry REGIONAL-anycast — регион размещения (placement-якорь).
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Статус", path: "status", format: "status" },
      { header: "Репозиториев", path: "repository_count", format: "text" },
      { header: "Endpoint", path: "endpoint", format: "code" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_REGISTRY,
      FIELD_DESCRIPTION,
      // REG-1 F4: regionId — required + immutable, cross-service ref → geo.Region
      // (REGIONAL-anycast placement, peer-validate fail-closed). Смена региона
      // сломала бы storage-locality блобов → immutable после Create.
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        description:
          "Регион размещения реестра (REGIONAL/anycast, immutable после Create). Cross-service ref → geo.Region.",
      },
      // REG-1 F5: defaultRepositoryVisibility — видимость по умолчанию для новых
      // репозиториев реестра. PUBLIC требует прав администратора реестра (проверяется
      // на сервере: any-path-to-PUBLIC admin-gate).
      {
        name: "default_repository_visibility",
        label: "Видимость репозиториев по умолчанию",
        type: "enum",
        default: "PRIVATE",
        options: [
          { value: "PRIVATE", label: "PRIVATE — приватные (доступ по правам)" },
          { value: "PUBLIC", label: "PUBLIC — публичные (anonymous pull; требует прав администратора)" },
        ],
        description:
          "Видимость, наследуемая новыми репозиториями при создании. Переключение на PUBLIC требует прав администратора реестра.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      default_repository_visibility: "PRIVATE",
      labels: {},
    }),
    emptyState: {
      title: "Создайте первый реестр",
      body: "Реестр хранит контейнерные образы проекта. После создания выполните docker login к endpoint реестра и docker push — репозитории появятся автоматически.",
      docs: ["Реестры контейнеров", "Публикация образов (docker login / push)"],
    },
  },

  // ====== repository (OCI-репозиторий) ======
  // Репозиторий — read-only: репозитории НЕ создаются через API, они
  // материализуются при первом docker push в реестр. Единственный вход —
  // ListRepositories(registryId) (path-scoped под реестром). Мутаций нет.
  // Tenant-facing термин — «репозиторий» (id/route/apiPath/payloadKey =
  // repositories по OCI/REST-контракту).

  repositories: {
    id: "repositories",
    route: "repositories",
    // registryId подставляется из родителя (реестра); прямой fetch —
    // registriesApi.listRepositories(registryId) (см. api/resources.ts).
    apiPath: "/registry/v1/registries/{registryId}/repositories",
    payloadKey: "repositories",
    singular: "Репозиторий",
    plural: "Репозитории",
    genitive: "Репозитория",
    serviceTitle: "Container Registry",
    scope: "project",
    // Read-only: репозиторий появляется через docker push, а не через UI.
    ops: { create: false, update: false, delete: false },
    // Теги — дочерний ресурс репозитория (ListTags(registryId, repository)).
    related: [{ childId: "tags", filterField: ["registry_id", "repository"], label: "Теги" }],
    // Facet-фильтр по типу артефакта: отделить docker-образы от helm-чартов.
    // Фильтруем по массиву artifact_types (включение) — смешанный репозиторий
    // (docker + helm) попадает в обе категории. Значения — enum-имена проекции.
    facet: {
      path: "artifact_types",
      label: "Тип",
      options: [
        { value: "ARTIFACT_TYPE_CONTAINER_IMAGE", label: "Docker-образы" },
        { value: "ARTIFACT_TYPE_HELM_CHART", label: "Helm-чарты" },
        { value: "ARTIFACT_TYPE_OTHER", label: "Иные" },
      ],
    },
    // Репозитории пагинируются на handler-слое (next_page_token) — грузим ВСЕ
    // страницы, чтобы facet видел полный набор (helm-чарт со страницы 2+ не пропал).
    loadAllPages: true,
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.name as string} />,
      },
      // Тип(ы) артефакта — цветные иконки (docker + helm рядом для смешанного
      // репозитория); читаем массив artifact_types, fallback — primary artifact_type.
      {
        header: "Тип",
        path: "artifact_types",
        render: (row) => <ArtifactTypesTag value={row.artifact_types ?? row.artifact_type} />,
      },
      { header: "Тегов", path: "tag_count", format: "text" },
      // size_bytes — агрегат по репозиторию (int64 строкой) → человекочитаемо;
      // 0/пусто → «—» (никогда «0 B»).
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
      // updated_at — время последнего push (last pushed) в репозиторий.
      { header: "Обновлён", path: "updated_at", format: "datetime" },
    ],
    // Read-only ресурс — form-schema нет.
    template: () => ({}),
    emptyState: {
      title: "Репозитории появляются автоматически",
      body: "Репозиторий появляется при первом docker push в этот реестр. Пустой реестр не содержит репозиториев — выполните push, чтобы репозиторий появился здесь.",
      docs: ["Публикация образов (docker login / push)"],
    },
  },

  // ====== tag ======
  // Tag — версия образа (тег/манифест). Read-в основном; единственная мутация —
  // DeleteTag (async Operation). Создание/обновление тегов — через docker push,
  // не через UI.

  tags: {
    id: "tags",
    route: "tags",
    // registryId + repository подставляются из родителей; прямой fetch —
    // registriesApi.listTags(registryId, repository) (см. api/resources.ts).
    apiPath: "/registry/v1/registries/{registryId}/repositories/{repository}/tags",
    payloadKey: "tags",
    singular: "Тег",
    plural: "Теги",
    genitive: "Тега",
    serviceTitle: "Container Registry",
    scope: "project",
    // DeleteTag — единственная мутация (create/update нет: теги пишет docker push).
    ops: { create: false, update: false, delete: true },
    columns: [
      { header: "Тег", path: "tag", format: "text" },
      { header: "Digest", path: "digest", format: "code" },
      { header: "Размер", path: "size_bytes", format: "text" },
      { header: "Media type", path: "media_type", format: "text" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
    ],
    // Мутаций create/update нет — form-schema не требуется.
    template: () => ({}),
  },

  // ====== geo (read-only ref-цель) ======
  // Region — cross-service ref-цель (owner geo) для Registry.region_id (REG-1 F4,
  // REGIONAL-anycast). Read-only registry-запись нужна RefSelect'у для резолва
  // apiPath/payloadKey/имени в dropdown'е (не навигируется как реестр-ресурс).
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
// Все ресурсы этого remote принадлежат домену Container Registry → префикс
// маршрута `registry`. Явные ветки (а не fallback) — иначе cross-module ссылки
// уходили бы в чужой сегмент (/nlb/... → 404). `compute-*` оставлен как
// forward-compat для будущих cross-service ref-целей.
export function resourceServicePrefix(specId: string): "registry" | "compute" {
  if (specId.startsWith("compute-")) return "compute";
  switch (specId) {
    case "regions":
    case "zones":
      return "compute";
    case "registries":
    case "repositories":
    case "tags":
    default:
      return "registry";
  }
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
