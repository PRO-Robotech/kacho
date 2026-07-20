// Реестр ресурсов: метаданные для generic ListPage / DetailPage / Create-Edit.
// Scope: 7 ресурсов Kachō proto.
// apiPath содержит полный путь с доменным префиксом (verbatim из proto google.api.http annotations).

import type { ReactNode } from "react";
import { Tag } from "antd";
import type { FormField } from "./form-schema";
import { setByPath, getByPath as getByPathImpl } from "./path";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import {
  RoutesEditor,
  type RouteEntry,
} from "@shared/components/organisms/RoutesEditor";
import { CopyableName } from "@shared/components/atoms/CopyableName";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { IamRefLink } from "@shared/components/molecules/IamRefLink";
import { LabelsCell } from "@shared/components/atoms/LabelsCell";
import { NicSpecFields } from "@shared/components/organisms/form/NicSpecFields";
import {
  roleIsSystem,
  targetKind,
  type AccessBindingTarget,
  type DefinitionTier,
  type Role,
} from "@shared/api/iam";

export interface ResourceColumn {
  header: string;
  // Путь в плоском объекте: "name", "status", "zone_id"
  path: string;
  format?:
    | "text"
    | "uid-short"
    | "datetime"
    | "status"
    | "code"
    | "list"
    | "references";
  className?: string;
  render?: (row: Record<string, unknown>) => ReactNode;
}

export interface ResourceSpec {
  id: string;
  // route path в SPA (без leading slash)
  route: string;
  // Полный URL-path для REST: /<domain>/v1/<plural>
  // Verbatim из proto google.api.http annotations.
  apiPath: string;
  // ключ массива в List response: "networks", "projects"
  payloadKey: string;
  // singular label для UI
  singular: string;
  // plural label
  plural: string;
  // родительный падеж ед.ч. («Обзор шлюзА», «Операции сетИ») — заголовок
  // мастер-ресурса в зоне-3 (обзор/операции/json). Fallback: plural.
  genitive?: string;
  description?: string;
  /** Service-domain заголовок (отображается в breadcrumb перед именем категории).
   *  Примеры: "Virtual Private Cloud", "IAM", "Администрирование". */
  serviceTitle?: string;
  // global = cluster-scoped, project = в выбранном Project, account = в выбранном Account
  scope: "global" | "project" | "account";
  // поддерживаемые операции
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
  // Если задан — кнопка в строке ведёт сюда вместо DetailPage. Используется
  // для иерархического drill-flow Account → Projects → VPC.
  childRoute?: string;
  // skeleton-объект для Create-формы.
  // projectId — выбранный Project (VPC/Compute scope).
  // accountId — выбранный Account (kacho.cloud.iam.v1.Project.account_id).
  template: (ctx: { projectId?: string; accountId?: string }) => unknown;
  // Опциональная нормализация payload перед отправкой на API.
  // Используется для конвертации form-internal представления (wrapper-объекты, toggle-поля)
  // в wire format (plain arrays, oneof etc.).
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
  /** Обратная sanitize: wire → form. Вызывается InlineResourceEditForm перед
   *  установкой initial form-state. Используется когда у формы есть array-of-ref
   *  или array-of-string поля, для которых wire-format = массив строк, а
   *  form-format = массив объектов `{value: "..."}` (см. NIC v4/v6_address_ids
   *  / security_group_ids; Subnet v4/v6_cidr_blocks). Без hydrate-адаптера
   *  RefSelect получает массив строк вместо объектов и не отображает
   *  выбранные значения в edit-режиме. */
  hydrate?: (obj: Record<string, unknown>) => Record<string, unknown>;
  /** Клиентская pre-submit валидация формы (form-values → сообщение об ошибке |
   *  null). Возвращает первый нарушенный инвариант (напр. IAM-1: Role.rules[]
   *  non-empty, definitionTier XOR); null = ок. Дополняет per-field required —
   *  для кросс-полевых/структурных гейтов, как в compute/storage remote. */
  validate?: (obj: Record<string, unknown>) => string | null;
  /** Path-template для internal/infra-проекции ресурса (плейсхолдер `{id}`).
   *  Если задан — на DetailPage появляется tab "jsonint", который делает
   *  GET <internalGetPath с подставленным {id}> и pretty-print'ит JSON-ответ.
   *  Пример: "/vpc/v1/networks/{id}/internal". Большинство ресурсов его не имеют. */
  internalGetPath?: string;
  /** KAC-233: связанные дочерние ресурсы — отдельные табы со встроенными
   *  таблицами в ResourceShell. childId — ключ ребёнка в REGISTRY; filterField —
   *  поле(я) ребёнка, ссылающееся на этот ресурс (client-side фильтр; массив =
   *  OR по нескольким полям, напр. subnet→addresses v4∪v6). label —
   *  переопределение заголовка таба (по умолчанию childSpec.plural). */
  related?: {
    childId: string;
    filterField: string | string[];
    label?: string;
  }[];
  /** KAC-233: ссылки на документацию по типу ресурса (блок «Документация» в
   *  aside DetailShell). Kachō-style. */
  docs?: { label: string; href: string }[];
  /** KAC-233: welcome-копирайт для пустой таблицы этого ресурса (когда он
   *  показан как ребёнок и список пуст). Kachō-style. */
  emptyState?: { title: string; body: string; docs?: string[] };
}

// Pool kinds — единственный валидный тип. KAC-70 удалил EXTERNAL_TEST/
// RESERVED_INTERNAL из proto enum kacho.cloud.vpc.v1.AddressPoolKind
// (`reserved 2, 100`).
const POOL_KINDS = [{ value: "EXTERNAL_PUBLIC", label: "External" }];

// Общие колонки
const COL_NAME: ResourceColumn = {
  header: "Имя",
  path: "name",
  format: "text",
  className: "font-medium",
};
const COL_CREATED: ResourceColumn = {
  header: "Дата создания",
  path: "created_at",
  format: "datetime",
};
const COL_ID: ResourceColumn = {
  header: "Идентификатор",
  path: "id",
  format: "uid-short",
};

// Strict — для IAM (Account, Project).
// Совпадает с backend validate.Name (Kachō `/[a-z]([-a-z0-9]{0,61}[a-z0-9])?/`).
const FIELD_NAME: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  required: true,
  placeholder: "my-resource",
  description:
    "Строчные латинские буквы, цифры и дефисы. Должно начинаться с буквы, длина 2–63 символа.",
  pattern: "^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$",
};

// Permissive — для VPC ресурсов (Network/Subnet/Address/RouteTable).
const FIELD_NAME_VPC: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-network",
  description:
    "Латинские буквы (любой регистр), цифры, «-» и «_». Должно начинаться с буквы, длина до 63 символов. Можно оставить пустым.",
  pattern: "^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$",
};

// Compute name-regex — lowercase-only (kacho-compute/CLAUDE.md §5).
const FIELD_NAME_COMPUTE: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-disk",
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

// Hidden поле для project-context
const FIELD_PROJECT_ID: FormField = {
  name: "project_id",
  label: "Project",
  type: "string",
  hidden: true,
};

// Hidden поле для account-context (IAM: Project / ServiceAccount scoped по Account).
const FIELD_ACCOUNT_ID: FormField = {
  name: "account_id",
  label: "Account",
  type: "string",
  hidden: true,
};

// Generic labels editor — map<string,string> через LabelsEditor (key=value rows
// + "Добавить метку"). Подключается ко всем VPC-ресурсам.
const FIELD_LABELS: FormField = {
  name: "labels",
  label: "Метки",
  type: "labels",
};

// VPC-1 Subnet cell: immutable primary CIDR anchor + "+N" additional-ranges
// hint (additional ranges managed via :add/:remove-cidr-blocks, not shown inline).
function CidrPrimaryCell({ primary, extra }: { primary: unknown; extra: unknown }): ReactNode {
  const p = typeof primary === "string" ? primary : "";
  const more = Array.isArray(extra) ? extra.length : 0;
  if (!p) return <span className="text-muted-foreground">—</span>;
  return (
    <span className="font-mono text-xs">
      {p}
      {more > 0 && <span className="text-muted-foreground"> +{more}</span>}
    </span>
  );
}

// VPC-1 Network cell: declared supernet blocks (IPv4 first, then IPv6), each on
// its own line; "+N" collapses long lists. Empty → dash.
function SupernetCell({ v4, v6 }: { v4: unknown; v6: unknown }): ReactNode {
  const list = [
    ...(Array.isArray(v4) ? (v4 as unknown[]) : []),
    ...(Array.isArray(v6) ? (v6 as unknown[]) : []),
  ].filter((x): x is string => typeof x === "string" && x !== "");
  if (list.length === 0) return <span className="text-muted-foreground">—</span>;
  const head = list.slice(0, 2);
  const more = list.length - head.length;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 2, alignItems: "flex-start" }}>
      {head.map((c) => (
        <span key={c} className="font-mono text-xs">
          {c}
        </span>
      ))}
      {more > 0 && <span className="text-muted-foreground text-xs">+{more}</span>}
    </span>
  );
}

// VPC-1 Subnet cell: server-derived placement — ZONAL(zone) | REGIONAL(region,
// anycast). Discriminated by placement_type° with zone_id/region_id fallback.
function PlacementCell({ row }: { row: Record<string, unknown> }): ReactNode {
  const pt = String(row.placement_type ?? "");
  const zone = String(row.zone_id ?? "");
  const region = String(row.region_id ?? "");
  const anchor = region || zone;
  if (!pt && !anchor) return <span className="text-muted-foreground">—</span>;
  const isRegional = pt === "REGIONAL" || (!zone && !!region);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <Tag color={isRegional ? "geekblue" : "blue"}>{isRegional ? "REGIONAL" : "ZONAL"}</Tag>
      {anchor && <span className="font-mono text-xs">{anchor}</span>}
    </span>
  );
}

// ── IAM-1 render helpers (definitionTier / scopeType / target) ──
const IAM_DASH = <span className="text-muted-foreground">—</span>;

// Dotted tier/scope → цвет тега (cluster=red / account=blue / project=green).
function iamTierColor(dotted: string): string {
  return dotted === "iam.cluster"
    ? "red"
    : dotted === "iam.account"
      ? "blue"
      : dotted === "iam.project"
        ? "green"
        : "default";
}

// definitionTier роли (IAM-1 F4) → тег tierType + ref-ссылка на anchor
// (account/project). cluster-tier (system) → id без ref (нет IAM-ресурса cluster).
// Legacy fallback — flat account_id.
function definitionTierCell(row: Record<string, unknown>): ReactNode {
  const dt = (row.definition_tier ?? row.definitionTier) as
    DefinitionTier | undefined;
  const tt = dt?.tier_type ?? dt?.tierType ?? "";
  const tid = dt?.tier_id ?? dt?.tierId ?? "";
  if (!tt) {
    const acc = (row.account_id ?? row.accountId) as string | undefined;
    return acc ? <IamRefLink specId="accounts" refId={acc} /> : IAM_DASH;
  }
  const spec =
    tt === "iam.account"
      ? "accounts"
      : tt === "iam.project"
        ? "projects"
        : undefined;
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        flexWrap: "wrap",
      }}
    >
      <Tag color={iamTierColor(tt)}>{tt}</Tag>
      {spec && tid ? (
        <IamRefLink specId={spec} refId={tid} />
      ) : tid ? (
        <CopyableId id={tid} />
      ) : null}
    </span>
  );
}

// AccessBinding scopeType (dotted, IAM-1 F7) с legacy fallback (scope enum).
function scopeTypeCell(row: Record<string, unknown>): ReactNode {
  const st = String(row.scope_type ?? row.scopeType ?? "");
  if (st) return <Tag color={iamTierColor(st)}>{st}</Tag>;
  const s = String(row.scope ?? "");
  if (!s || s === "SCOPE_UNSPECIFIED") return IAM_DASH;
  const color =
    s === "CLUSTER"
      ? "red"
      : s === "ACCOUNT"
        ? "blue"
        : s === "PROJECT"
          ? "green"
          : "default";
  return <Tag color={color}>{s}</Tag>;
}

// AccessBinding scope anchor (scopeId, IAM-1 F7) — ref по типу scope; legacy
// fallback resource_id/resource_type.
function scopeAnchorCell(row: Record<string, unknown>): ReactNode {
  const st = String(row.scope_type ?? row.scopeType ?? "");
  const rt = String(row.resource_type ?? "");
  const anchorType =
    st === "iam.account"
      ? "account"
      : st === "iam.project"
        ? "project"
        : st === "iam.cluster"
          ? "cluster"
          : rt;
  const anchorId = String(row.scope_id ?? row.scopeId ?? row.resource_id ?? "");
  if (!anchorId) return IAM_DASH;
  const spec =
    anchorType === "account"
      ? "accounts"
      : anchorType === "project"
        ? "projects"
        : undefined;
  return spec ? (
    <IamRefLink specId={spec} refId={anchorId} />
  ) : (
    <CopyableId id={anchorId} />
  );
}

// AccessBinding target (IAM-1 F8, allInScope | resources[]) → компактный тег.
function targetCell(row: Record<string, unknown>): ReactNode {
  const t = row.target as AccessBindingTarget | undefined;
  const kind = targetKind(t);
  if (kind === "resources") {
    const n = t?.resources?.length ?? 0;
    return (
      <Tag color="geekblue" title="Per-object least-priv">
        {n} объект{n === 1 ? "" : "а/ов"}
      </Tag>
    );
  }
  if (kind === "allInScope")
    return (
      <Tag title="Весь scope (явный opt-in)" color="default">
        весь scope
      </Tag>
    );
  return IAM_DASH;
}

// Subject type (UI-строка / enum-имя) → registry specId.
function subjectSpecId(t: string): string | undefined {
  if (t === "user" || t === "USER" || t === "SUBJECT_TYPE_USER") return "users";
  if (t === "group" || t === "GROUP" || t === "SUBJECT_TYPE_GROUP")
    return "groups";
  if (
    t === "service_account" ||
    t === "SERVICE_ACCOUNT" ||
    t === "SUBJECT_TYPE_SERVICE_ACCOUNT"
  )
    return "service-accounts";
  return undefined;
}

// AccessBinding subjects (IAM-1 subjects[]) → первый как ref-ссылка + «+N».
// Legacy single subject_type/subject_id — fallback.
function accessBindingSubjectsCell(row: Record<string, unknown>): ReactNode {
  const subjects = row.subjects as
    Array<{ type?: string; id?: string }> | undefined;
  const list =
    Array.isArray(subjects) && subjects.length > 0
      ? subjects
      : row.subject_id
        ? [{ type: String(row.subject_type ?? ""), id: String(row.subject_id) }]
        : [];
  if (list.length === 0) return IAM_DASH;
  const first = list[0];
  const spec = subjectSpecId(String(first.type ?? ""));
  const firstNode = spec ? (
    <IamRefLink
      specId={spec}
      refId={first.id}
      nameField={spec === "users" ? "email" : "name"}
    />
  ) : (
    <CopyableId id={String(first.id ?? "")} />
  );
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        flexWrap: "wrap",
      }}
    >
      {firstNode}
      {list.length > 1 && (
        <Tag title={`Ещё ${list.length - 1} субъект(ов)`}>
          +{list.length - 1}
        </Tag>
      )}
    </span>
  );
}

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== iam ======
  // proto: kacho.cloud.iam.v1.AccountService / ProjectService.

  // Account — global-scoped (ListAccounts без обязательных полей).
  accounts: {
    id: "accounts",
    route: "accounts",
    apiPath: "/iam/v1/accounts",
    payloadKey: "accounts",
    singular: "Аккаунт",
    plural: "Аккаунты",
    genitive: "Аккаунта",
    serviceTitle: "IAM",
    scope: "global",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        // ownerUserId° — output-only, derived-from-caller (IAM-1 F1). camel/snake.
        header: "Владелец",
        path: "owner_user_id",
        render: (row) => (
          <IamRefLink
            specId="users"
            refId={(row.owner_user_id ?? row.ownerUserId) as string | undefined}
            nameField="email"
          />
        ),
      },
      {
        header: "Защита",
        path: "deletion_protection",
        render: (row) =>
          row.deletion_protection || row.deletionProtection ? (
            <Tag color="gold" title="Защита от удаления включена">
              Да
            </Tag>
          ) : (
            <span className="text-muted-foreground">—</span>
          ),
      },
      { header: "Статус", path: "status", format: "status" },
      COL_CREATED,
      COL_ID,
    ],
    // IAM-1 F1: ownerUserId НЕ в Create-форме (derived-from-caller, output-only;
    // передача в body → sync INVALID_ARGUMENT). Create-сага co-commit'ит default
    // Project + owner-AccessBinding (F2) — сервер-сторона, не форма.
    fields: [
      FIELD_NAME,
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        default: false,
        description:
          "Запретить удаление аккаунта, пока защита не снята (Update).",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
    ],
    related: [
      { childId: "projects", filterField: "account_id", label: "Проекты" },
      {
        childId: "service-accounts",
        filterField: "account_id",
        label: "Сервисные аккаунты",
      },
      { childId: "groups", filterField: "account_id", label: "Группы" },
    ],
    docs: [
      { label: "Аккаунты и организации", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    emptyState: {
      title: "Создайте первый Account",
      body:
        "Account — верхнеуровневый tenant Kachō: владелец, проекты, пользователи и роли живут внутри него. " +
        "Создайте Account, чтобы начать выдавать доступ и заводить проекты.",
      docs: ["Аккаунты и организации"],
    },
    template: () => ({
      name: "",
      description: "",
      deletion_protection: false,
      labels: {},
    }),
  },

  // Project — account-scoped (ListProjects требует account_id). account_id
  // приходит из выбранного Account (IAM Account-селектор), поле скрыто.
  projects: {
    id: "projects",
    route: "projects",
    apiPath: "/iam/v1/projects",
    payloadKey: "projects",
    singular: "Проект",
    plural: "Проекты",
    genitive: "Проекта",
    serviceTitle: "IAM",
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => (
          <IamRefLink specId="accounts" refId={row.account_id as string} />
        ),
      },
      COL_CREATED,
      COL_ID,
    ],
    // IAM-1 F3: accountId immutable (Move удалён, строго 2 уровня) — hidden
    // (наполняется из Account-контекста) + immutable (исключён из update_mask;
    // cross-account перенос сломал бы scope-координату downstream). name — mutable.
    fields: [
      FIELD_NAME,
      {
        name: "account_id",
        label: "Account",
        type: "string",
        hidden: true,
        immutable: true,
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
    ],
    // Клик по проекту в списке ведёт на его IAM-detail (/iam/projects/:id) —
    // без childRoute drill идёт на generic ResourceShell detail, а не на дашборд.
    docs: [
      { label: "Проекты", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    template: ({ accountId }) => ({
      name: "",
      account_id: accountId ?? "",
      description: "",
    }),
  },

  // ServiceAccount — account-scoped (ListServiceAccounts требует account_id).
  "service-accounts": {
    id: "service-accounts",
    route: "service-accounts",
    apiPath: "/iam/v1/serviceAccounts",
    payloadKey: "service_accounts",
    singular: "Сервисный аккаунт",
    plural: "Сервисные аккаунты",
    serviceTitle: "IAM",
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => (
          <IamRefLink specId="accounts" refId={row.account_id as string} />
        ),
      },
      COL_CREATED,
      COL_ID,
    ],
    fields: [FIELD_NAME, FIELD_ACCOUNT_ID, FIELD_DESCRIPTION],
    docs: [
      { label: "Сервисные аккаунты", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    template: ({ accountId }) => ({
      name: "",
      account_id: accountId ?? "",
      description: "",
    }),
  },

  // User — read+delete only (создаётся через signup / InternalUserService).
  // Registry-запись нужна для ref-резолва (Account.owner_user_id) и RefNameLink;
  // отдельная generic-страница не используется — UI остаётся кастомным.
  users: {
    id: "users",
    route: "users",
    apiPath: "/iam/v1/users",
    payloadKey: "users",
    singular: "Пользователь",
    plural: "Пользователи",
    serviceTitle: "IAM",
    scope: "global",
    ops: { create: false, update: false, delete: true },
    columns: [
      { header: "Эл. почта", path: "email", format: "text" },
      { header: "Отображаемое имя", path: "display_name", format: "text" },
      { header: "Статус", path: "invite_status", format: "status" },
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => (
          <IamRefLink
            specId="accounts"
            refId={row.account_id as string | undefined}
          />
        ),
      },
      { header: "ID", path: "id", format: "uid-short" },
      { header: "External ID", path: "external_id", format: "uid-short" },
      { header: "Создан", path: "created_at", format: "datetime" },
    ],
    docs: [
      { label: "Пользователи и приглашения", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    template: () => ({}),
  },

  // Group — account-scoped (ListGroups требует account_id). Generic список +
  // деталь + create/edit (name/description/labels). Членство (group_members) —
  // доменная extra-tab на детали через detailExtension (не registry-child).
  groups: {
    id: "groups",
    route: "groups",
    apiPath: "/iam/v1/groups",
    payloadKey: "groups",
    singular: "Группа",
    plural: "Группы",
    genitive: "Группы",
    serviceTitle: "IAM",
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => (
          <IamRefLink
            specId="accounts"
            refId={row.account_id as string | undefined}
          />
        ),
      },
      COL_ID,
      { header: "Описание", path: "description", format: "text" },
      COL_CREATED,
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [FIELD_NAME, FIELD_ACCOUNT_ID, FIELD_LABELS, FIELD_DESCRIPTION],
    docs: [
      { label: "Группы и членство", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    emptyState: {
      title: "Создайте первую группу",
      body:
        "Группа объединяет пользователей и сервисные аккаунты, чтобы выдавать им доступ одной привязкой. " +
        "Назначьте группе роль на ресурс — и все её участники получат соответствующие права.",
      docs: ["Группы и членство"],
    },
    template: ({ accountId }) => ({
      name: "",
      account_id: accountId ?? "",
      description: "",
      labels: {},
    }),
  },

  // Role — RBAC: system (read-only catalog, is_system=true) + custom (account-
  // scoped). Generic список + деталь; permissions редактируются доменной веткой
  // (в generic fields их нет). Различие system/custom — колонка «Тип».
  roles: {
    id: "roles",
    route: "roles",
    apiPath: "/iam/v1/roles",
    payloadKey: "roles",
    singular: "Роль",
    plural: "Роли",
    genitive: "Роли",
    serviceTitle: "IAM",
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Тип",
        path: "is_system",
        // IAM-1 F4/F6: isSystem° derived (definitionTier.tierType==iam.cluster);
        // fallback на хранимый is_system/isSystem (AS-IS до миграции).
        render: (row) =>
          roleIsSystem(row as unknown as Role) ? (
            <Tag color="purple">system</Tag>
          ) : (
            <Tag color="default">custom</Tag>
          ),
      },
      COL_ID,
      {
        // IAM-1 F4: «Уровень» = definitionTier (dotted tierType + anchor).
        // Заменяет плоскую колонку «Аккаунт» (слово «scope» снято с роли).
        header: "Уровень",
        path: "definition_tier",
        render: (row) => definitionTierCell(row),
      },
      { header: "Описание", path: "description", format: "text" },
      {
        // RBAC rules-model: роль описывается rules[] (module/resources/verbs),
        // permissions[] в Get/List пуст (compiled-форма не отдаётся). Показываем
        // module-чипы правил + счётчик.
        header: "Правила",
        path: "rules",
        render: (row) => {
          const rules =
            (row.rules as Array<{ module?: string }> | undefined) ?? [];
          if (rules.length === 0)
            return <span className="text-muted-foreground">—</span>;
          const modules = Array.from(
            new Set(rules.map((r) => r.module || "*")),
          );
          const head = modules.slice(0, 3);
          const more = modules.length - head.length;
          return (
            <span
              style={{
                display: "inline-flex",
                flexWrap: "wrap",
                gap: 4,
                alignItems: "center",
              }}
            >
              {head.map((m, i) => (
                <code
                  key={i}
                  style={{
                    fontSize: 11,
                    fontFamily: "ui-monospace, SFMono-Regular, monospace",
                  }}
                >
                  {m}
                </code>
              ))}
              {more > 0 && (
                <span style={{ fontSize: 11, color: "rgba(0,0,0,.45)" }}>
                  +{more}
                </span>
              )}
              <span style={{ fontSize: 11, color: "rgba(0,0,0,.45)" }}>
                · {rules.length}
              </span>
            </span>
          );
        },
      },
      COL_CREATED,
    ],
    // generic-поля create/edit — name/description/account_id; permissions —
    // доменная ветка, здесь его нет.
    fields: [FIELD_NAME, FIELD_ACCOUNT_ID, FIELD_DESCRIPTION],
    docs: [
      { label: "Роли и разрешения", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    emptyState: {
      title: "Создайте первую пользовательскую роль",
      body:
        "Роль — набор разрешений (`модуль.ресурс.имя.действие`), который выдаётся субъекту привязкой доступа. " +
        "Системные роли поставляются платформой и доступны только для чтения; собственные роли вы создаёте под свои сценарии.",
      docs: ["Роли и разрешения"],
    },
    // IAM-1 F5: permissions[] (compiled) — output-only Internal-проекция, на вход
    // НЕ принимается. Авторская политика — rules[] (bespoke InlineRoleCreateForm).
    template: ({ accountId }) => ({
      name: "",
      account_id: accountId ?? "",
      description: "",
      rules: [],
    }),
  },

  // AccessBinding — RBAC. Registry обеспечивает generic ДЕТАЛЬ (Обзор/Операции/
  // JSON/Документация) + колонки + IamRefLink-резолв субъекта/роли/ресурса.
  // Единого flat-List RPC у AccessBinding нет (list — by-resource/by-subject/
  // by-account), поэтому СПИСОК остаётся bespoke (AccessBindingsPage). Create —
  // bespoke AccessBindingCreatePage (/iam/access-bindings/create) → ops.create=false.
  // revoke = Delete (ops.delete). Wire-поля сверены с api/iam.ts AccessBinding
  // (granted_by/deletion_protection/status в future отсутствуют — не показываем).
  "access-bindings": {
    id: "access-bindings",
    route: "access-bindings",
    apiPath: "/iam/v1/accessBindings",
    payloadKey: "access_bindings",
    singular: "Привязка доступа",
    plural: "Привязки доступа",
    genitive: "привязки доступа",
    serviceTitle: "IAM",
    scope: "account",
    ops: { create: false, update: false, delete: true },
    columns: [
      {
        // Субъект(ы): IAM-1 — subjects[] (1..N); первый как ref-ссылка + «+N».
        // Legacy single subject_type/subject_id — fallback.
        header: "Субъект",
        path: "subject_id",
        render: (row) => accessBindingSubjectsCell(row),
      },
      {
        header: "Роль",
        path: "role_id",
        render: (row) => (
          <IamRefLink
            specId="roles"
            refId={row.role_id as string | undefined}
          />
        ),
      },
      {
        // Область — IAM-1 F7 scopeType (dotted iam.account/project/cluster);
        // legacy fallback scope enum.
        header: "Область",
        path: "scope_type",
        render: (row) => scopeTypeCell(row),
      },
      {
        // Anchor — scopeId (ref по типу scope); legacy resource_id fallback.
        header: "Anchor",
        path: "scope_id",
        render: (row) => scopeAnchorCell(row),
      },
      {
        // Цель — IAM-1 F8 target (allInScope | resources[]); REQUIRED least-priv.
        header: "Цель",
        path: "target",
        render: (row) => targetCell(row),
      },
      { header: "Статус", path: "status", format: "status" },
      {
        // Кто выдал привязку (grantedByUserId°, output-only) — ссылка на юзера.
        header: "Кто выдал",
        path: "granted_by_user_id",
        render: (row) => {
          const grantedBy = (row.granted_by_user_id ?? row.grantedByUserId) as
            string | undefined;
          return grantedBy ? (
            <IamRefLink
              specId="users"
              refId={grantedBy}
              nameField="email"
              maxChars={24}
            />
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        // deletionProtection=true → owner-auto-binding (нельзя удалить без снятия).
        header: "Защита",
        path: "deletion_protection",
        render: (row) =>
          row.deletion_protection || row.deletionProtection ? (
            <Tag color="gold" title="Защита от удаления (owner-привязка)">
              Owner
            </Tag>
          ) : (
            <span className="text-muted-foreground">—</span>
          ),
      },
      COL_CREATED,
    ],
    docs: [
      { label: "Привязки доступа", href: "#" },
      { label: "Управление доступом", href: "#" },
    ],
    emptyState: {
      title: "Нет привязок доступа",
      body:
        "Привязка доступа назначает субъекту (пользователю, сервисному аккаунту или группе) роль на ресурсе " +
        "(Account, Project или кластер). Создайте привязку, чтобы выдать доступ.",
      docs: ["Привязки доступа"],
    },
    // create — bespoke AccessBindingCreatePage; template лишь удовлетворяет
    // обязательному полю ResourceSpec.template + поддерживает URL-preset.
    template: ({ accountId }) => ({
      subject_type: "user",
      subject_id: "",
      role_id: "",
      resource_type: "account",
      resource_id: accountId ?? "",
    }),
  },

  // ====== vpc ======
  // proto: GET /vpc/v1/networks

  networks: {
    id: "networks",
    route: "networks",
    apiPath: "/vpc/v1/networks",
    payloadKey: "networks",
    internalGetPath: "/vpc/v1/networks/{id}/internal",
    related: [
      { childId: "subnets", filterField: "network_id", label: "Подсети" },
      {
        childId: "route-tables",
        filterField: "network_id",
        label: "Таблицы маршрутов",
      },
      {
        childId: "security-groups",
        filterField: "network_id",
        label: "Группы безопасности",
      },
    ],
    docs: [
      { label: "Облачные сети и подсети", href: "#" },
      { label: "Таблицы маршрутизации", href: "#" },
      { label: "Группы безопасности", href: "#" },
      { label: "Адреса облачных ресурсов", href: "#" },
    ],
    emptyState: {
      title: "Создайте вашу первую облачную сеть",
      body:
        "Облачная сеть Kachō объединяет подсети, таблицы маршрутизации и группы безопасности в единое " +
        "изолированное адресное пространство. Внутри сети ресурсы общаются напрямую, а наружу — через шлюзы " +
        "и публичные адреса.",
      docs: ["Облачные сети и подсети"],
    },
    singular: "Облачная сеть",
    plural: "Облачные сети",
    genitive: "Облачной сети",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        // VPC-1: declared supernet (immutable via Update; grown/shrunk via
        // :add/:remove-cidr-blocks). Compact IPv4-first view.
        header: "Супернет",
        path: "ipv4_cidr_blocks",
        render: (row) => <SupernetCell v4={row.ipv4_cidr_blocks} v6={row.ipv6_cidr_blocks} />,
      },
      {
        header: "Группа безопасности по умолчанию",
        path: "default_security_group_id",
        render: (row) => (
          <RefNameLink
            specId="security-groups"
            refId={row.default_security_group_id as string | undefined}
            maxChars={42}
          />
        ),
      },
      {
        // VPC-1: system-provisioned default RT (output-only), echoed on create.
        header: "Таблица маршрутизации по умолчанию",
        path: "default_route_table_id",
        render: (row) => (
          <RefNameLink specId="route-tables" refId={row.default_route_table_id as string | undefined} maxChars={42} />
        ),
      },
      {
        header: "Дата создания",
        path: "created_at",
        format: "datetime",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    // VPC-1: declared supernet ipv4/ipv6_cidr_blocks[] is required at Create and
    // immutable through Update — grown/shrunk only via :add/:remove-cidr-blocks
    // on the detail page (editHidden). default-SG + default-RT are provisioned
    // unconditionally by the server (no opt-out flag).
    fields: [
      FIELD_NAME_VPC,
      {
        name: "ipv4_cidr_blocks",
        label: "Супернет IPv4",
        type: "array",
        itemLabel: "CIDR",
        description:
          "Объявленное адресное пространство сети (IPv4). Из него нарезаются CIDR подсетей. Неизменяемо через Update — расширяется/сужается verb-действиями на странице сети.",
        immutable: true,
        editHidden: true,
        newItem: () => ({ value: "" }),
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true, placeholder: "10.20.0.0/16" }],
      },
      {
        name: "ipv6_cidr_blocks",
        label: "Супернет IPv6",
        type: "array",
        itemLabel: "CIDR",
        description: "Опционально. Объявленное адресное пространство сети (IPv6).",
        immutable: true,
        editHidden: true,
        newItem: () => ({ value: "" }),
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true, placeholder: "fd00:20::/48" }],
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      labels: {},
      // VPC-1: supernet declared at create; default-SG + default-RT provisioned
      // unconditionally server-side (create_default_security_group flag retired).
      ipv4_cidr_blocks: [{ value: "" }],
      ipv6_cidr_blocks: [],
    }),
    // {value:"…"} form-objects ↔ wire string[] for the supernet array fields.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of ["ipv4_cidr_blocks", "ipv6_cidr_blocks"]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw
            .map((item) =>
              typeof item === "object" && item !== null && "value" in (item as object)
                ? (item as Record<string, unknown>)["value"]
                : item,
            )
            .filter((v) => typeof v === "string" && v);
        }
      }
      return out;
    },
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of ["ipv4_cidr_blocks", "ipv6_cidr_blocks"]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw.map((item) => (typeof item === "string" ? { value: item } : item));
        }
      }
      return out;
    },
  },

  // proto: GET /vpc/v1/subnets

  subnets: {
    id: "subnets",
    route: "subnets",
    apiPath: "/vpc/v1/subnets",
    payloadKey: "subnets",
    related: [
      {
        // Под подсетью адреса всегда ВНУТРЕННИЕ (фильтр по internal_*.subnet_id).
        childId: "addresses",
        filterField: [
          "internal_ipv4_address.subnet_id",
          "internal_ipv6_address.subnet_id",
        ],
        label: "IP-адреса",
      },
    ],
    docs: [
      { label: "Облачные сети и подсети", href: "#" },
      { label: "CIDR-блоки подсети", href: "#" },
      { label: "Резервирование внутренних IP-адресов", href: "#" },
    ],
    emptyState: {
      title: "Создайте вашу первую подсеть",
      body:
        "Подсеть — диапазон IP-адресов внутри облачной сети Kachō, привязанный к зоне доступности. Ресурсы " +
        "(виртуальные машины, балансировщики, сетевые интерфейсы) размещаются в подсетях и получают адреса " +
        "из их CIDR-блоков.",
      docs: ["Облачные сети и подсети"],
    },
    singular: "Подсеть",
    plural: "Подсети",
    genitive: "Подсети",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Сеть",
        path: "network_id",
        render: (row) => (
          <RefNameLink
            specId="networks"
            refId={row.network_id as string | undefined}
          />
        ),
      },
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        // VPC-1: primary CIDR anchor (immutable) + count of additional ranges
        // (managed via :add/:remove-cidr-blocks). v4_cidr_blocks[] retired.
        header: "IPv4 CIDR",
        path: "ipv4_cidr_primary",
        render: (row) => <CidrPrimaryCell primary={row.ipv4_cidr_primary} extra={row.ipv4_cidr_blocks} />,
      },
      {
        header: "IPv6 CIDR",
        path: "ipv6_cidr_primary",
        render: (row) => <CidrPrimaryCell primary={row.ipv6_cidr_primary} extra={row.ipv6_cidr_blocks} />,
      },
      {
        // VPC-1: placement_type° server-derived — ZONAL shows zone, REGIONAL
        // shows region (anycast). Single column reflects the anchor either way.
        header: "Размещение",
        path: "placement_type",
        render: (row) => <PlacementCell row={row} />,
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
      {
        header: "Таблица маршрутизации",
        path: "route_table_id",
        render: (row) => (
          <RefNameLink
            specId="route-tables"
            refId={row.route_table_id as string | undefined}
          />
        ),
      },
    ],
    // VPC-1: placement is a server-derived discriminator — the form channel
    // `_placement` (ZONAL|REGIONAL) gates zone_id XOR region_id via visibleWhen;
    // sanitize drops the discriminator + inactive channel and NEVER sends
    // placement_type (server rejects it). ipv4/ipv6_cidr_primary are the
    // immutable placement anchor (one required); additional ranges live on the
    // detail page (verbs :add/:remove-cidr-blocks), not in this form.
    fields: [
      FIELD_NAME_VPC,
      {
        name: "network_id",
        label: "Network",
        type: "ref",
        refResource: "networks",
        refProjectScoped: true,
        required: true,
        immutable: true, // within-service FK, VRF-scoping — immutable after Create
      },
      {
        name: "_placement",
        label: "Размещение",
        type: "enum",
        required: true,
        default: "ZONAL",
        description:
          "Тип размещения подсети. ZONAL — привязана к одной зоне доступности; REGIONAL — anycast во всём регионе. Определяет placementType° на сервере (неизменяемо после создания).",
        options: [
          { value: "ZONAL", label: "ZONAL — в одной зоне доступности" },
          { value: "REGIONAL", label: "REGIONAL — во всём регионе (anycast)" },
        ],
        editHidden: true,
      },
      {
        name: "zone_id",
        label: "Зона доступности",
        type: "ref",
        refResource: "zones",
        required: true,
        immutable: true,
        visibleWhen: { field: "_placement", equals: "ZONAL" },
      },
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        visibleWhen: { field: "_placement", equals: "REGIONAL" },
      },
      {
        name: "ipv4_cidr_primary",
        label: "Основной IPv4 CIDR",
        type: "string",
        placeholder: "10.20.0.0/24",
        description:
          "Неизменяемый основной CIDR-блок подсети (⊆ одного супернет-блока сети). Хотя бы один из IPv4/IPv6 обязателен. Доп. диапазоны добавляются на странице подсети.",
        immutable: true,
      },
      {
        name: "ipv6_cidr_primary",
        label: "Основной IPv6 CIDR",
        type: "string",
        placeholder: "fd00:20::/64",
        description: "Опционально. Неизменяемый основной IPv6 CIDR-блок подсети (⊆ IPv6-супернета сети).",
        immutable: true,
      },
      {
        name: "route_table_id",
        label: "Route Table",
        type: "ref",
        refResource: "route-tables",
        refProjectScoped: true,
        placeholder: "— авто: default сети —",
        description:
          "Опционально. Если не задано — авто-ассоциируется таблица маршрутизации по умолчанию сети (network.defaultRouteTableId°).",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      network_id: "",
      _placement: "ZONAL",
      zone_id: "",
      region_id: "",
      // VPC-1: explicit immutable primary anchor (одного из v4/v6 достаточно;
      // доп. диапазоны — через :add-cidr-blocks на detail). placement_type НЕ
      // отправляется — сервер выводит его из zone_id XOR region_id.
      ipv4_cidr_primary: "",
      ipv6_cidr_primary: "",
      description: "",
    }),
    // Strip the form-only `_placement` discriminator + the inactive placement
    // channel, and drop empty primary/route fields. placement_type is never in
    // the payload (server-derived; explicit reject).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const placement = out["_placement"];
      delete out["_placement"];
      delete out["placement_type"];
      if (placement === "REGIONAL") delete out["zone_id"];
      else delete out["region_id"];
      for (const key of ["zone_id", "region_id", "ipv4_cidr_primary", "ipv6_cidr_primary", "route_table_id"]) {
        if (out[key] === "" || out[key] == null) delete out[key];
      }
      return out;
    },
    // wire → form: derive the `_placement` channel from region_id/placement_type
    // so an edit view opens on the correct branch (edit is bespoke, kept for
    // generic-form parity / RefSelect).
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const region = typeof out["region_id"] === "string" ? (out["region_id"] as string) : "";
      const pt = typeof out["placement_type"] === "string" ? (out["placement_type"] as string) : "";
      out["_placement"] = pt === "REGIONAL" || (!out["zone_id"] && region) ? "REGIONAL" : "ZONAL";
      return out;
    },
  },

  // proto: GET /vpc/v1/addresses

  addresses: {
    id: "addresses",
    route: "addresses",
    apiPath: "/vpc/v1/addresses",
    payloadKey: "addresses",
    docs: [
      { label: "Адреса облачных ресурсов", href: "#" },
      { label: "Резервирование внутренних IP-адресов", href: "#" },
    ],
    emptyState: {
      title: "Зарезервируйте первый IP-адрес",
      body:
        "IP-адрес можно зарезервировать в подсети (внутренний) или выделить публичный (внешний) для доступа " +
        "к ресурсам Kachō извне. Зарезервированный адрес сохраняется за вами, пока вы его не освободите.",
      docs: ["Адреса облачных ресурсов"],
    },
    singular: "IP-адрес",
    // Нейтральный plural — список содержит и внешние (Публичные), и внутренние
    // адреса; вид различается колонкой «Вид» (Публичный/Внутренний). Раньше было
    // «Публичные IP-адреса», что вводило в заблуждение для внутренних.
    plural: "IP-адреса",
    genitive: "IP-адреса",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "IP-адрес",
        path: "external_ipv4_address.address",
        render: (row) => {
          const ext = (
            row.external_ipv4_address as { address?: string } | undefined
          )?.address;
          const ext6 = (
            row.external_ipv6_address as { address?: string } | undefined
          )?.address;
          const int = (
            row.internal_ipv4_address as { address?: string } | undefined
          )?.address;
          const int6 = (
            row.internal_ipv6_address as { address?: string } | undefined
          )?.address;
          // KAC-58: показываем external_ipv6_address наравне с external_ipv4
          // (обе ветки oneof; форма теперь предлагает только external).
          // internal_* оставлены в render для backward compat — Address-ресурсы,
          // созданные через compute Instance.Create flow до KAC-58 / напрямую
          // через API, останутся видимыми.
          const ip = ext || ext6 || int || int6;
          if (!ip) return <span className="text-muted-foreground">—</span>;
          return <span className="font-mono text-xs">{ip}</span>;
        },
      },
      {
        header: "Используется",
        path: "used",
        render: (row) =>
          row.used ? "Да" : <span className="text-muted-foreground">Нет</span>,
      },
      {
        header: "Версия",
        path: "ip_version",
        render: (row) => {
          const v = (row.ip_version as string | undefined) ?? "";
          if (!v) return <span className="text-muted-foreground">—</span>;
          // IPV4 / IPV6 / IP_VERSION_UNSPECIFIED
          return v.replace(/^IP_VERSION_/, "").replace(/^IPV/, "IPv");
        },
      },
      {
        header: "Вид",
        path: "type",
        render: (row) => {
          const t = (row.type as string | undefined) ?? "";
          if (t === "EXTERNAL") return "Публичный";
          if (t === "INTERNAL") return "Внутренний";
          return <span className="text-muted-foreground">—</span>;
        },
      },
      {
        header: "Защита от удаления",
        path: "deletion_protection",
        render: (row) =>
          row.deletion_protection ? (
            "Да"
          ) : (
            <span className="text-muted-foreground">Нет</span>
          ),
      },
      {
        // `used_by` — output-only список kacho.cloud.reference.Reference
        // (см. Address.used_by в types.ts). Для эфемерных compute-NIC адресов
        // referrer.type=compute_instance, referrer.id=<instance id>.
        // Generic rendering — format: "references" из spec-columns.tsx.
        header: "Ресурс",
        path: "used_by",
        format: "references",
      },
      {
        header: "Дата создания",
        path: "created_at",
        format: "datetime",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_VPC,
      // Discriminator + spec'ы — create-only (Address spec иммутабелен, см.
      // CLAUDE.md kacho-vpc §4.4). Скрываем в edit-форме.
      {
        name: "_address_kind",
        label: "Тип адреса",
        type: "enum",
        required: true,
        default: "external",
        description:
          "Тип резервируемого IP-адреса. Внешний IPv4/IPv6 выделяется из IPv4/IPv6 пула выбранной зоны. Внутренний IPv4/IPv6 выделяется из CIDR-блока выбранной подсети.",
        // KAC-100: вернули Internal IPv4 / Internal IPv6 (откат UI-части KAC-61).
        // Internal-адреса также могут аллоцироваться compute-сервисом при
        // Instance.Create через nic-spec, но прямое резервирование с руки —
        // поддерживается.
        options: [
          { value: "external", label: "Внешний IPv4" },
          { value: "external_v6", label: "Внешний IPv6" },
          { value: "internal", label: "Внутренний IPv4" },
          { value: "internal_v6", label: "Внутренний IPv6" },
        ],
        editHidden: true,
      },
      {
        // Общая зона для external IPv4/IPv6 — UI-поле, sanitize кладёт его в
        // активную ветку spec'а (external_ipv4/v6_address_spec.zone_id). Не
        // сбрасывается при переключении IPv4↔IPv6 (раньше были два поля под
        // разными ветками → значение терялось при смене типа).
        name: "_zone_id",
        label: "Зона",
        type: "ref",
        refResource: "zones",
        required: true,
        description:
          "Зона, в которой выделяется внешний адрес. Оставьте поле «Адрес» пустым, чтобы адрес был выделен автоматически из пула зоны.",
        visibleWhen: {
          field: "_address_kind",
          equals: ["external", "external_v6"],
        },
        editHidden: true,
      },
      {
        name: "external_ipv4_address_spec.address",
        label: "Адрес",
        type: "string",
        placeholder: "auto",
        description:
          "Конкретный IPv4-адрес для резервирования. Оставьте пустым — адрес будет выделен автоматически из IPv4-пула выбранной зоны.",
        visibleWhen: { field: "_address_kind", equals: "external" },
        editHidden: true,
      },
      {
        // KAC-58: External IPv6 — sparse counter-based allocator (миграция 0021).
        // Зона — общее поле `_zone_id` выше (для external и external_v6).
        name: "external_ipv6_address_spec.address",
        label: "Адрес",
        type: "string",
        placeholder: "auto",
        description:
          "Конкретный IPv6-адрес для резервирования. Оставьте пустым — адрес будет выделен автоматически из IPv6-пула выбранной зоны.",
        visibleWhen: { field: "_address_kind", equals: "external_v6" },
        editHidden: true,
      },
      {
        // KAC-100: Internal IPv4 — резервирование с руки. Адрес выделяется
        // из IPv4 CIDR подсети (kacho-vpc InternalAddressService.AllocateInternalIP).
        name: "internal_ipv4_address_spec.subnet_id",
        label: "Подсеть",
        type: "ref",
        refResource: "subnets",
        refProjectScoped: true,
        required: true,
        description:
          "Подсеть, из IPv4-CIDR которой выделяется внутренний адрес. Оставьте поле «Адрес» пустым для автоматического выделения.",
        visibleWhen: { field: "_address_kind", equals: "internal" },
        editHidden: true,
      },
      {
        name: "internal_ipv4_address_spec.address",
        label: "Адрес",
        type: "string",
        placeholder: "auto",
        description:
          "Конкретный IPv4-адрес из CIDR выбранной подсети. Оставьте пустым — будет выделен автоматически.",
        visibleWhen: { field: "_address_kind", equals: "internal" },
        editHidden: true,
      },
      {
        // KAC-100: Internal IPv6 — резервирование с руки. Адрес выделяется
        // из IPv6 CIDR подсети.
        name: "internal_ipv6_address_spec.subnet_id",
        label: "Подсеть",
        type: "ref",
        refResource: "subnets",
        refProjectScoped: true,
        required: true,
        description:
          "Подсеть, из IPv6-CIDR которой выделяется внутренний адрес. Оставьте поле «Адрес» пустым для автоматического выделения.",
        visibleWhen: { field: "_address_kind", equals: "internal_v6" },
        editHidden: true,
      },
      {
        name: "internal_ipv6_address_spec.address",
        label: "Адрес",
        type: "string",
        placeholder: "auto",
        description:
          "Конкретный IPv6-адрес из CIDR выбранной подсети. Оставьте пустым — будет выделен автоматически.",
        visibleWhen: { field: "_address_kind", equals: "internal_v6" },
        editHidden: true,
      },
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        default: false,
        description:
          "Если включена, адрес нельзя будет удалить, пока защита не будет снята.",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      _address_kind: "external",
      external_ipv4_address_spec: { zone_id: "", address: "" },
      deletion_protection: false,
    }),
    // Убирает поле-переключатель _address_kind и неактивный oneof из payload.
    // KAC-100: оставляет активную ветку из {external, external_v6, internal,
    // internal_v6}; неактивные внутренние ветки выкидываются.
    sanitize: (obj) => {
      const kind = obj["_address_kind"];
      const result: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(obj)) {
        if (k === "_address_kind" || k === "_zone_id") continue;
        if (k === "external_ipv4_address_spec" && kind !== "external") continue;
        if (k === "external_ipv6_address_spec" && kind !== "external_v6")
          continue;
        if (k === "internal_ipv4_address_spec" && kind !== "internal") continue;
        if (k === "internal_ipv6_address_spec" && kind !== "internal_v6")
          continue;
        result[k] = v;
      }
      // Общая зона `_zone_id` → в активную external-ветку spec'а.
      const zone = obj["_zone_id"];
      if (zone) {
        if (kind === "external") {
          result["external_ipv4_address_spec"] = {
            ...(result["external_ipv4_address_spec"] as
              Record<string, unknown> | undefined),
            zone_id: zone,
          };
        } else if (kind === "external_v6") {
          result["external_ipv6_address_spec"] = {
            ...(result["external_ipv6_address_spec"] as
              Record<string, unknown> | undefined),
            zone_id: zone,
          };
        }
      }
      return result;
    },
  },

  // proto: GET /vpc/v1/routeTables (camelCase в URL)

  "route-tables": {
    id: "route-tables",
    route: "route-tables",
    apiPath: "/vpc/v1/routeTables",
    payloadKey: "route_tables",
    docs: [
      { label: "Таблицы маршрутизации", href: "#" },
      { label: "Статическая маршрутизация", href: "#" },
      { label: "Маршрутизация через NAT-инстанс", href: "#" },
    ],
    emptyState: {
      title: "Создайте вашу первую таблицу маршрутизации",
      body:
        "С помощью таблиц маршрутизации вы можете построить маршруты между облачной сетью Kachō и другими " +
        "виртуальными или локальными сетями, либо настроить отказоустойчивую схему передачи данных с " +
        "маршрутами в нескольких зонах доступности.",
      docs: ["Статическая маршрутизация", "Маршрутизация через NAT-инстанс"],
    },
    singular: "Таблица маршрутов",
    plural: "Таблицы маршрутов",
    genitive: "Таблицы маршрутов",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Сеть",
        path: "network_id",
        render: (row) => (
          <RefNameLink
            specId="networks"
            refId={row.network_id as string | undefined}
          />
        ),
      },
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        header: "Статические маршруты",
        path: "static_routes",
        render: (row) => {
          const routes =
            (row.static_routes as
              | Array<{
                  destination_prefix?: string;
                  next_hop_address?: string;
                }>
              | undefined) ?? [];
          if (routes.length === 0)
            return <span className="text-muted-foreground">—</span>;
          return (
            <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
              {routes.map((r, i) => (
                <span
                  key={i}
                  style={{
                    fontFamily: "ui-monospace, SFMono-Regular, monospace",
                    fontSize: 12,
                    whiteSpace: "nowrap",
                  }}
                >
                  {r.destination_prefix ?? "?"} → {r.next_hop_address ?? "?"}
                </span>
              ))}
            </div>
          );
        },
      },
      {
        header: "Дата создания",
        path: "created_at",
        format: "datetime",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_VPC,
      {
        name: "network_id",
        label: "Сеть",
        type: "ref",
        refResource: "networks",
        refProjectScoped: true,
        required: true,
        immutable: true,
        description: "Облачная сеть, в которой действуют эти маршруты.",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
      // Static Routes — в самом низу формы (объёмный блок, не должен
      // мешать редактированию основных полей).
      //
      // ⚠️ Gateway-режим (next_hop oneof = gateway_id) пока НЕ поддержан
      // backend'ом kacho-vpc: proto-поле есть, но domain.StaticRoute хранит
      // только NextHopAddress; handler требует next_hop_address. Поэтому
      // UI оставляет только IP-режим — до KAC-issue на поддержку gateway_id.
      {
        name: "static_routes",
        label: "Статические маршруты",
        type: "custom",
        // KAC-239/KAC-246: в Create маршруты добавляются ТОЙ ЖЕ таблицей, что и в
        // detail (RoutesPanel) — controlled RoutesEditor (Префикс назначения |
        // Следующий узел | ⌫ + dashed «Добавить маршрут»). В Edit скрыто —
        // маршруты правятся RoutesPanel отдельно (full-replace).
        editHidden: true,
        // fullWidth:false — рендерить как обычное labeled-поле (label «Статические
        // маршруты» слева 200px + таблица в wrapper-колонке 570px), выровнено с
        // остальными полями. Без этого custom → full-width.
        fullWidth: false,
        description: "При обновлении список заменяется целиком (full-replace).",
        render: ({ value, onChange }) => {
          const routes =
            (getByPath(value, "static_routes") as RouteEntry[] | undefined) ??
            [];
          return (
            <RoutesEditor
              value={routes}
              onChange={(next) =>
                onChange(setByPath(value, "static_routes", next))
              }
            />
          );
        },
      },
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      network_id: "",
      description: "",
      static_routes: [],
    }),
    // Выкидываем пустые строки маршрутов (без префикса/next-hop) перед POST.
    sanitize: (obj) => {
      const routes = Array.isArray(obj.static_routes)
        ? (obj.static_routes as RouteEntry[]).filter(
            (r) =>
              (r?.destination_prefix ?? "").trim() !== "" &&
              (r?.next_hop_address ?? "").trim() !== "",
          )
        : [];
      return { ...obj, static_routes: routes };
    },
  },

  // proto: GET /vpc/v1/networkInterfaces — ENI-подобный NetworkInterface (эпик KAC-2).
  // Публичная проекция: tenant-facing намерение + результат (id/name/привязки/
  // выделенные tenant-адреса/status). Инфра-поля (hv_id/sid/host_iface/...) —
  // только во InternalNetworkInterfaceService, тут не показываются (см. workspace
  // CLAUDE.md §«Инфра-чувствительные данные»). Мутации (Create/Update/Delete/
  // Attach/Detach) async → Operation, как у остальных VPC-ресурсов.

  "network-interfaces": {
    id: "network-interfaces",
    route: "network-interfaces",
    apiPath: "/vpc/v1/networkInterfaces",
    payloadKey: "network_interfaces",
    internalGetPath: "/vpc/v1/networkInterfaces/{id}/internal",
    singular: "Сетевой интерфейс",
    plural: "Сетевые интерфейсы",
    genitive: "Сетевого интерфейса",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Подсеть",
        path: "subnet_id",
        render: (row) => (
          <RefNameLink
            specId="subnets"
            refId={row.subnet_id as string | undefined}
          />
        ),
      },
      {
        // mac_address — output-only, аллоцируется kacho-vpc при Create
        // (префикс 0e: + 40 бит crypto/rand), стабилен на жизни NIC,
        // уникален в пределах cloud (KAC-48). Клиент не может задать.
        header: "MAC",
        path: "mac_address",
        render: (row) => {
          const mac = row.mac_address as string | undefined;
          return mac ? (
            <CopyableId id={mac} />
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        // NIC теперь ссылается на Address-ресурсы по id (v4_address_ids).
        // Здесь — компактно число привязанных IPv4-адресов; сами адреса
        // (с IP-значением) видны на DetailPage / в ресурсе Address.
        header: "IPv4-адреса",
        path: "v4_address_ids",
        render: (row) => {
          const ids = row.v4_address_ids as string[] | undefined;
          const n = Array.isArray(ids) ? ids.length : 0;
          return n > 0 ? (
            <span className="font-mono text-xs">{n}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        header: "IPv6-адреса",
        path: "v6_address_ids",
        render: (row) => {
          const ids = row.v6_address_ids as string[] | undefined;
          const n = Array.isArray(ids) ? ids.length : 0;
          return n > 0 ? (
            <span className="font-mono text-xs">{n}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        header: "Статус",
        path: "status",
        format: "status",
      },
      {
        // `used_by` — output-only kacho.cloud.reference.Reference, заполняется
        // когда compute-инстанс присоединяет NIC ({referrer:{type:"compute_instance",
        // id:"<instance id>"}, type:"USED_BY"}). instance_id у NIC больше нет.
        header: "Используется",
        path: "used_by",
        render: (row) => {
          const ub = row.used_by as
            { referrer?: { type?: string; id?: string } } | undefined;
          const ref = ub?.referrer;
          if (!ref?.id) return <span className="text-muted-foreground">—</span>;
          if (ref.type === "compute_instance") {
            return <RefNameLink specId="compute-instances" refId={ref.id} />;
          }
          return (
            <span className="font-mono text-xs">
              {ref.type ?? "?"}: {ref.id}
            </span>
          );
        },
      },
      {
        header: "Дата создания",
        path: "created_at",
        format: "datetime",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_VPC,
      {
        name: "subnet_id",
        label: "Подсеть",
        type: "ref",
        refResource: "subnets",
        refProjectScoped: true,
        required: true,
        immutable: true,
        description:
          "Subnet, в которой создаётся интерфейс. Менять нельзя после создания.",
      },
      // NIC ссылается на Address-ресурсы по id (модель KAC-2/KAC-7): NIC
      // больше не хранит IP-строки, а держит список id внутренних Address'ов
      // из своей подсети. Здесь — ref-list на ресурс `addresses`, отфильтрованный
      // по subnet_id формы (GET /vpc/v1/addresses?subnet_id=<form.subnet_id>),
      // с «+ Создать адрес» прямо в дропдауне (InlineResourceCreateForm
      // с pre-filled internal_ipv4_address_spec.subnet_id — «создать» = «выделить
      // IPv4 из CIDR этой подсети»). На success id появляется в списке.
      {
        name: "v4_address_ids",
        label: "IPv4-адрес",
        type: "array",
        itemLabel: "адрес",
        // KAC-55: на одной NIC максимум один IPv4 (и максимум один IPv6).
        // Multi-IP per VM — через несколько NIC, не secondary addresses в одном
        // NIC. Backend отбивает > 1 sync InvalidArgument + DB CHECK
        // network_interfaces_v4_addr_max1 (миграция 0018) как backstop.
        maxItems: 1,
        description:
          "Опционально. IPv4 Address-ресурс из выбранной подсети. Можно создать новый прямо в дропдауне.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "Address",
            type: "ref",
            refResource: "addresses",
            required: true,
            // `addresses` ресурс project-scoped — ListAddressesRequest.project_id
            // (required). RefSelect авто-добавляет ?project_id=<project-context>;
            // refQueryFromField докидывает &subnet_id=<form.subnet_id> сверху.
            // Итог: GET /vpc/v1/addresses?project_id=<project>&subnet_id=<subnet>.
            refProjectScoped: true,
            refQueryFromField: { param: "subnet_id", field: "subnet_id" },
            // Только внутренние IPv4-адреса (у которых выставлен
            // internal_ipv4_address) — отсекаем external / IPv6-only.
            refFilter: (row) => !!row.internal_ipv4_address,
            createResource: "addresses",
            createTitle: "Выделить IPv4-адрес из подсети",
            createPresetFields: (form) => ({
              _address_kind: "internal",
              "internal_ipv4_address_spec.subnet_id": form["subnet_id"] ?? "",
            }),
          },
        ],
      },
      {
        name: "v6_address_ids",
        label: "IPv6-адрес",
        type: "array",
        itemLabel: "адрес",
        // KAC-55: на одной NIC максимум один IPv6 (и максимум один IPv4).
        maxItems: 1,
        description:
          "Опционально. IPv6 Address-ресурс из выбранной подсети. Можно создать новый прямо в дропдауне.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "Address",
            type: "ref",
            refResource: "addresses",
            required: true,
            // см. комментарий у v4_address_ids — project-scoped + subnet_id-фильтр.
            refProjectScoped: true,
            refQueryFromField: { param: "subnet_id", field: "subnet_id" },
            // Только внутренние IPv6-адреса (у которых выставлен
            // internal_ipv6_address).
            refFilter: (row) => !!row.internal_ipv6_address,
            createResource: "addresses",
            createTitle: "Выделить IPv6-адрес из подсети",
            createPresetFields: (form) => ({
              _address_kind: "internal_v6",
              "internal_ipv6_address_spec.subnet_id": form["subnet_id"] ?? "",
            }),
          },
        ],
      },
      // В SG-create-форме сеть выбирает пользователь: generic-форма не делает
      // cross-field dependent-lookup, поэтому не выводит default из
      // subnet_id (subnet → network.default_security_group_id).
      {
        name: "security_group_ids",
        label: "Группы безопасности",
        type: "array",
        itemLabel: "SG",
        description:
          "Опционально. Если не задано — действует SG по умолчанию для сети. Можно создать новую группу прямо в дропдауне.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "Security Group",
            type: "ref",
            refResource: "security-groups",
            refProjectScoped: true,
            required: true,
            createResource: "security-groups",
            createTitle: "Создать группу безопасности",
          },
        ],
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      subnet_id: "",
      v4_address_ids: [],
      v6_address_ids: [],
      security_group_ids: [],
      description: "",
      labels: {},
    }),
    // Конвертирует [{value: "..."}, ...] → ["...", ...] для wire format
    // (как subnets.v4_cidr_blocks / instance NIC security_group_ids).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of [
        "v4_address_ids",
        "v6_address_ids",
        "security_group_ids",
      ]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw
            .map((item) =>
              typeof item === "object" &&
              item !== null &&
              "value" in (item as object)
                ? (item as Record<string, unknown>)["value"]
                : item,
            )
            .filter((v) => typeof v === "string" && v);
        }
      }
      return out;
    },
    // Inverse sanitize: wire → form. Backend возвращает массивы id-строк, форма
    // ждёт массивы объектов {value: "..."} (для RefSelect). Без этого в
    // edit-режиме RefSelect получает массив строк и не показывает имена.
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of [
        "v4_address_ids",
        "v6_address_ids",
        "security_group_ids",
      ]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw.map((item) =>
            typeof item === "string" ? { value: item } : item,
          );
        }
      }
      return out;
    },
  },

  // proto: GET /vpc/v1/securityGroups (camelCase в URL)

  "security-groups": {
    id: "security-groups",
    route: "security-groups",
    apiPath: "/vpc/v1/securityGroups",
    payloadKey: "security_groups",
    docs: [
      { label: "Группы безопасности", href: "#" },
      { label: "Правила групп безопасности", href: "#" },
    ],
    emptyState: {
      title: "Создайте вашу первую группу безопасности",
      body:
        "Группа безопасности — набор правил, определяющих разрешённый входящий и исходящий трафик для " +
        "ресурсов облачной сети Kachō (виртуальных машин, балансировщиков, сетевых интерфейсов).",
      docs: ["Группы безопасности"],
    },
    singular: "Группа безопасности",
    plural: "Группы безопасности",
    genitive: "Группы безопасности",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Сеть",
        path: "network_id",
        // KAC-243: network_id у SG — обязателен и неизменяем (kacho-proto вернул
        // (required) на CreateSecurityGroupRequest.network_id). Бессетевых SG
        // больше нет; «—» остаётся только для legacy-строк до backfill-миграции.
        render: (row) => {
          const nid = row.network_id as string | undefined;
          return nid ? (
            <RefNameLink specId="networks" refId={nid} />
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      { header: "По умолчанию", path: "default_for_network", format: "text" },
      COL_CREATED,
      COL_ID,
    ],
    fields: [
      FIELD_NAME_VPC,
      {
        name: "network_id",
        label: "Network",
        type: "ref",
        refResource: "networks",
        refProjectScoped: true,
        // KAC-243: network_id обязателен при Create и неизменяем после.
        // На табе сети «Группы безопасности» preset+locked (см. ResourceFormBody
        // ImmutableField); standalone-create — обязателен выбор сети.
        required: true,
        placeholder: "Выберите сеть",
        description:
          "Сеть, которой принадлежит группа безопасности. Обязательна и неизменяема после создания. " +
          "SG→SG-правила допустимы только между группами одной сети.",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      {
        name: "rules",
        label: "Rules",
        type: "sg-rules",
        description:
          "Direction + protocol/ports + target (cidr | другая SG | predefined). Без правил — default-deny.",
        // В Update RPC backend ждёт `rule_specs`, не `rules` (Kachō контракт).
        // В edit-форме скрываем — правила меняются через спец-RPC UpdateRules /
        // UpdateRule на отдельной вкладке.
        editHidden: true,
      },
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      network_id: "",
      description: "",
      rules: [],
    }),
    // Чистит UI-дискриминаторы (_protocol_mode/_ports_any/_target_kind) и
    // неактивные ветки oneof перед PATCH/POST. См. SgRulesEditor.
    // network_id обязателен (KAC-243); пустой выбрасываем только чтобы не слать
    // "" — backend всё равно отвергнет Create без сети (INVALID_ARGUMENT).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      if (!out["network_id"]) delete out["network_id"];
      const raw = out["rules"];
      if (Array.isArray(raw)) {
        out["rules"] = raw.map((r) =>
          sanitizeSgRule(r as Record<string, unknown>),
        );
      }
      return out;
    },
  },

  // proto: GET /vpc/v1/gateways

  gateways: {
    id: "gateways",
    route: "gateways",
    apiPath: "/vpc/v1/gateways",
    payloadKey: "gateways",
    singular: "Шлюз",
    plural: "Шлюзы",
    genitive: "Шлюза",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_VPC,
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      // gateway_type oneof — пока единственный вариант shared_egress_gateway_spec
      // (proto: CreateGatewayRequest.shared_egress_gateway_spec). Backend
      // отвергает с InvalidArgument "Illegal argument gateway" если oneof
      // пустой или поле названо иначе (например прежнее shared_egress_gateway
      // от response-сообщения Gateway, а не запроса). См. kacho-vpc gateway.go:91.
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      shared_egress_gateway_spec: {},
    }),
  },

  // ====== compute (Disk / Image / Snapshot / Instance) ======
  // proto: GET /compute/v1/{disks|images|snapshots|instances}. Name-regex lowercase-only
  // (kacho-compute/CLAUDE.md §5: `^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$`).

  // disk-types — read-only справочник, используется как refResource в dropdown'ах.
  "disk-types": {
    id: "disk-types",
    route: "disk-types",
    apiPath: "/compute/v1/diskTypes",
    payloadKey: "disk_types",
    singular: "Тип диска",
    plural: "Типы дисков",
    serviceTitle: "Compute Cloud",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      { header: "Описание", path: "description", format: "text" },
      { header: "Зоны", path: "zone_ids", format: "list" },
    ],
    template: () => ({}),
  },

  // compute-zones — read-only справочник зон. kacho-compute — owner Geography
  // (Region/Zone перенесены из vpc, эпик KAC-15; см. workspace CLAUDE.md
  // §«Кросс-доменные ссылки на ресурсы»). Admin-CRUD — registry-запись `zones`.
  "compute-zones": {
    id: "compute-zones",
    route: "compute-zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    plural: "Зоны (Compute)",
    serviceTitle: "Compute Cloud",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Статус", path: "status", format: "status" },
    ],
    template: () => ({}),
  },

  "compute-regions": {
    id: "compute-regions",
    route: "compute-regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    plural: "Регионы (Compute)",
    serviceTitle: "Compute Cloud",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      { header: "Название", path: "name", format: "text" },
      { header: "Статус", path: "status", format: "status" },
    ],
    template: () => ({}),
  },

  "compute-disks": {
    id: "compute-disks",
    route: "disks",
    apiPath: "/compute/v1/disks",
    payloadKey: "disks",
    singular: "Диск",
    plural: "Диски",
    genitive: "Диска",
    serviceTitle: "Compute Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Зона", path: "zone_id", format: "text" },
      { header: "Тип", path: "type_id", format: "text" },
      {
        header: "Размер",
        path: "size",
        render: (row) => (
          <span className="font-mono text-xs">{fmtBytesGiB(row.size)}</span>
        ),
      },
      {
        header: "Источник",
        path: "source_image_id",
        render: (row) => {
          const img = row.source_image_id as string | undefined;
          const snap = row.source_snapshot_id as string | undefined;
          if (img) return <RefNameLink specId="compute-images" refId={img} />;
          if (snap)
            return <RefNameLink specId="compute-snapshots" refId={snap} />;
          return <span className="text-muted-foreground">—</span>;
        },
      },
      {
        header: "Привязан к ВМ",
        path: "instance_ids",
        render: (row) => {
          const ids = (row.instance_ids as string[] | undefined) ?? [];
          if (ids.length === 0)
            return <span className="text-muted-foreground">—</span>;
          return <RefNameLink specId="compute-instances" refId={ids[0]} />;
        },
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      {
        name: "zone_id",
        label: "Зона",
        type: "ref",
        refResource: "compute-zones",
        required: true,
        immutable: true,
      },
      {
        name: "type_id",
        label: "Тип диска",
        type: "ref",
        refResource: "disk-types",
        immutable: true,
        placeholder: "network-ssd (по умолчанию)",
      },
      {
        name: "size",
        label: "Размер (ГиБ)",
        type: "int",
        required: true,
        default: 10,
        min: 4,
        description:
          "Минимум — размер источника (image/snapshot), либо 4 ГиБ. В Update только увеличение.",
      },
      {
        name: "_disk_source",
        label: "Источник",
        type: "enum",
        default: "blank",
        description: "Пустой диск, либо клон из образа / снимка.",
        options: [
          { value: "blank", label: "Пустой диск" },
          { value: "image", label: "Из образа" },
          { value: "snapshot", label: "Из снимка" },
        ],
        editHidden: true,
      },
      {
        name: "image_id",
        label: "Образ",
        type: "ref",
        refResource: "compute-images",
        refProjectScoped: true,
        visibleWhen: { field: "_disk_source", equals: "image" },
        editHidden: true,
      },
      {
        name: "snapshot_id",
        label: "Снимок",
        type: "ref",
        refResource: "compute-snapshots",
        refProjectScoped: true,
        visibleWhen: { field: "_disk_source", equals: "snapshot" },
        editHidden: true,
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      zone_id: "",
      type_id: "",
      size: 10,
      _disk_source: "blank",
      description: "",
      labels: {},
    }),
    // ГиБ → байты для size; вырезаем _disk_source-дискриминатор и неактивный oneof.
    sanitize: (obj) => {
      const out: Record<string, unknown> = {};
      const src = obj["_disk_source"];
      for (const [k, v] of Object.entries(obj)) {
        if (k === "_disk_source") continue;
        if (k === "image_id" && src !== "image") continue;
        if (k === "snapshot_id" && src !== "snapshot") continue;
        if (k === "size") {
          out[k] = gibToBytes(v);
          continue;
        }
        out[k] = v;
      }
      return out;
    },
  },

  "compute-images": {
    id: "compute-images",
    route: "images",
    apiPath: "/compute/v1/images",
    payloadKey: "images",
    singular: "Образ",
    plural: "Образы",
    genitive: "Образа",
    serviceTitle: "Compute Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Семейство", path: "family", format: "text" },
      {
        header: "Мин. размер диска",
        path: "min_disk_size",
        render: (row) => (
          <span className="font-mono text-xs">
            {fmtBytesGiB(row.min_disk_size)}
          </span>
        ),
      },
      {
        header: "ОС",
        path: "os.type",
        render: (row) => {
          const t = (row.os as { type?: string } | undefined)?.type;
          return t ? t : <span className="text-muted-foreground">—</span>;
        },
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      {
        name: "family",
        label: "Семейство",
        type: "string",
        placeholder: "ubuntu-2204-lts",
        description: "Опционально. Lowercase, 3..63, начинается с буквы.",
        immutable: true,
        pattern: "^([a-z][-a-z0-9]{1,61}[a-z0-9])?$",
      },
      {
        name: "_image_source",
        label: "Источник",
        type: "enum",
        default: "disk",
        required: true,
        options: [
          { value: "disk", label: "Из диска" },
          { value: "snapshot", label: "Из снимка" },
          { value: "image", label: "Из другого образа" },
          { value: "uri", label: "По URI (pre-signed URL)" },
        ],
        editHidden: true,
      },
      {
        name: "disk_id",
        label: "Диск",
        type: "ref",
        refResource: "compute-disks",
        refProjectScoped: true,
        visibleWhen: { field: "_image_source", equals: "disk" },
        editHidden: true,
      },
      {
        name: "snapshot_id",
        label: "Снимок",
        type: "ref",
        refResource: "compute-snapshots",
        refProjectScoped: true,
        visibleWhen: { field: "_image_source", equals: "snapshot" },
        editHidden: true,
      },
      {
        name: "image_id",
        label: "Исходный образ",
        type: "ref",
        refResource: "compute-images",
        refProjectScoped: true,
        visibleWhen: { field: "_image_source", equals: "image" },
        editHidden: true,
      },
      {
        name: "uri",
        label: "URI",
        type: "string",
        placeholder: "https://...",
        visibleWhen: { field: "_image_source", equals: "uri" },
        editHidden: true,
      },
      {
        name: "min_disk_size",
        label: "Мин. размер диска (ГиБ)",
        type: "int",
        min: 4,
        description:
          "Опционально. Если задано — диски из образа не могут быть меньше.",
        immutable: true,
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      _image_source: "disk",
      description: "",
      labels: {},
    }),
    sanitize: (obj) => {
      const out: Record<string, unknown> = {};
      const src = obj["_image_source"];
      for (const [k, v] of Object.entries(obj)) {
        if (k === "_image_source") continue;
        if (k === "disk_id" && src !== "disk") continue;
        if (k === "snapshot_id" && src !== "snapshot") continue;
        if (k === "image_id" && src !== "image") continue;
        if (k === "uri" && src !== "uri") continue;
        if (k === "min_disk_size") {
          if (v === undefined || v === null || v === "") continue;
          out[k] = gibToBytes(v);
          continue;
        }
        if (k === "family" && (v === undefined || v === "")) continue;
        out[k] = v;
      }
      return out;
    },
  },

  "compute-snapshots": {
    id: "compute-snapshots",
    route: "snapshots",
    apiPath: "/compute/v1/snapshots",
    payloadKey: "snapshots",
    singular: "Снимок диска",
    plural: "Снимки дисков",
    genitive: "Снимка диска",
    serviceTitle: "Compute Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Статус", path: "status", format: "status" },
      {
        header: "Исходный диск",
        path: "source_disk_id",
        render: (row) => (
          <RefNameLink
            specId="compute-disks"
            refId={row.source_disk_id as string | undefined}
          />
        ),
      },
      {
        header: "Размер диска",
        path: "disk_size",
        render: (row) => (
          <span className="font-mono text-xs">
            {fmtBytesGiB(row.disk_size)}
          </span>
        ),
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      {
        name: "disk_id",
        label: "Исходный диск",
        type: "ref",
        refResource: "compute-disks",
        refProjectScoped: true,
        required: true,
        immutable: true,
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      disk_id: "",
      description: "",
      labels: {},
    }),
  },

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
    ops: {
      create: true,
      update: true,
      delete: true,
      start: true,
      stop: true,
      restart: true,
    },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Зона", path: "zone_id", format: "text" },
      { header: "Платформа", path: "platform_id", format: "text" },
      {
        header: "vCPU / RAM",
        path: "resources",
        render: (row) => {
          const r = row.resources as
            { cores?: string | number; memory?: string | number } | undefined;
          if (!r) return <span className="text-muted-foreground">—</span>;
          return (
            <span className="font-mono text-xs">
              {r.cores ?? "?"} vCPU · {fmtBytesGiB(r.memory)}
            </span>
          );
        },
      },
      {
        header: "Внутренний IP",
        path: "network_interfaces",
        render: (row) => {
          const nics =
            (row.network_interfaces as
              | Array<{ primary_v4_address?: { address?: string } }>
              | undefined) ?? [];
          const ip = nics[0]?.primary_v4_address?.address;
          return ip ? (
            <span className="font-mono text-xs">{ip}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        header: "Загрузочный диск",
        path: "boot_disk.disk_id",
        render: (row) => {
          const bd = (row.boot_disk as { disk_id?: string } | undefined)
            ?.disk_id;
          return <RefNameLink specId="compute-disks" refId={bd} />;
        },
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      {
        name: "zone_id",
        label: "Зона",
        type: "ref",
        refResource: "compute-zones",
        required: true,
        immutable: true,
      },
      {
        name: "platform_id",
        label: "Платформа",
        type: "enum",
        required: true,
        default: "standard-v3",
        options: [
          { value: "standard-v1", label: "Intel Broadwell (standard-v1)" },
          { value: "standard-v2", label: "Intel Cascade Lake (standard-v2)" },
          { value: "standard-v3", label: "Intel Ice Lake (standard-v3)" },
          {
            value: "highfreq-v3",
            label: "Intel Ice Lake, 3.1 GHz (highfreq-v3)",
          },
        ],
        immutable: true,
        description: "Менять platform_id можно только когда ВМ остановлена.",
      },
      {
        name: "resources_spec.cores",
        label: "vCPU (cores)",
        type: "int",
        required: true,
        default: 2,
        min: 2,
        description:
          "2,4,6,8,...; зависит от платформы. Менять только когда ВМ остановлена.",
        editHidden: true,
      },
      {
        name: "resources_spec.memory_gib",
        label: "RAM (ГиБ)",
        type: "int",
        required: true,
        default: 2,
        min: 1,
        description: "Кратно 1 ГиБ. Менять только когда ВМ остановлена.",
        editHidden: true,
      },
      {
        name: "resources_spec.core_fraction",
        label: "Гарантированная доля vCPU, %",
        type: "enum",
        default: "100",
        options: [
          { value: "5", label: "5%" },
          { value: "20", label: "20%" },
          { value: "50", label: "50%" },
          { value: "100", label: "100%" },
        ],
        editHidden: true,
      },
      {
        name: "_boot_source",
        label: "Загрузочный диск",
        type: "enum",
        default: "image",
        required: true,
        options: [
          { value: "image", label: "Создать из образа" },
          { value: "disk", label: "Использовать существующий диск" },
        ],
        editHidden: true,
      },
      {
        name: "boot_disk_spec.disk_spec.image_id",
        label: "Образ для загрузочного диска",
        type: "ref",
        refResource: "compute-images",
        refProjectScoped: true,
        visibleWhen: { field: "_boot_source", equals: "image" },
        editHidden: true,
      },
      {
        name: "boot_disk_spec.disk_spec.size_gib",
        label: "Размер загрузочного диска (ГиБ)",
        type: "int",
        default: 10,
        min: 4,
        visibleWhen: { field: "_boot_source", equals: "image" },
        editHidden: true,
      },
      {
        name: "boot_disk_spec.disk_spec.type_id",
        label: "Тип загрузочного диска",
        type: "ref",
        refResource: "disk-types",
        placeholder: "network-ssd (по умолчанию)",
        visibleWhen: { field: "_boot_source", equals: "image" },
        editHidden: true,
      },
      {
        name: "boot_disk_spec.disk_id",
        label: "Существующий диск",
        type: "ref",
        refResource: "compute-disks",
        refProjectScoped: true,
        visibleWhen: { field: "_boot_source", equals: "disk" },
        editHidden: true,
      },
      {
        name: "boot_disk_spec.auto_delete",
        label: "Удалять загрузочный диск вместе с ВМ",
        type: "bool",
        default: true,
        editHidden: true,
      },
      {
        name: "network_interface_specs",
        label: "Сетевые интерфейсы",
        type: "array",
        itemLabel: "интерфейс",
        description:
          "Минимум один сетевой интерфейс. Выберите сеть → подсеть → внутренний адрес (Cascader) и режим публичного IP (Segmented); либо переключитесь на «существующий NetworkInterface» (тогда подсеть/SG/адрес берутся из него). Подсеть должна быть в той же зоне, что и ВМ.",
        editHidden: true,
        // Дефолт NIC-айтема: пустой spec, external-IP = «без адреса».
        // `_*`-поля — служебные UI-state (cascader path / external mode), их
        // вычищает sanitizeInstanceCreate перед submit.
        newItem: () => ({
          _addr_cascader: undefined,
          subnet_id: "",
          primary_v4_address_spec: { address: "" },
          _ext_mode: "none",
          _use_existing_nic: false,
          nic_id: "",
          security_group_ids: [],
        }),
        itemFields: [
          // Bespoke NIC-секция: Network→Subnet→Address Cascader + Segmented
          // external-IP + (advanced) existing-NIC ref. См. NicSpecFields.tsx.
          {
            name: "_nic_config",
            label: "",
            type: "custom",
            render: (p) => (
              <NicSpecFields
                pathPrefix={p.pathPrefix}
                value={p.value}
                onChange={p.onChange}
              />
            ),
          },
          // Группы безопасности — generic ArrayField с inline-create «+ SG»
          // (без изменений; на NIC-айтеме остаётся как было).
          {
            name: "security_group_ids",
            label: "Группы безопасности",
            type: "array",
            itemLabel: "SG",
            description:
              "Опционально. Применяются к интерфейсу. Можно создать новую прямо в дропдауне.",
            newItem: () => ({ value: "" }),
            itemFields: [
              {
                name: "value",
                label: "Security Group",
                type: "ref",
                refResource: "security-groups",
                refProjectScoped: true,
                required: true,
                createResource: "security-groups",
                createTitle: "Создать группу безопасности",
              },
            ],
          },
        ],
      },
      {
        name: "hostname",
        label: "Hostname",
        type: "string",
        placeholder: "(= id если пусто)",
        pattern: "^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$",
        editHidden: true,
      },
      {
        name: "service_account_id",
        label: "Service Account ID",
        type: "string",
        placeholder: "(опционально)",
      },
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      zone_id: "",
      platform_id: "standard-v3",
      resources_spec: { cores: 2, memory_gib: 2, core_fraction: "100" },
      _boot_source: "image",
      boot_disk_spec: {
        auto_delete: true,
        disk_spec: { size_gib: 10, type_id: "" },
      },
      network_interface_specs: [
        {
          _addr_cascader: undefined,
          subnet_id: "",
          primary_v4_address_spec: { address: "" },
          _ext_mode: "none",
          _use_existing_nic: false,
          nic_id: "",
          security_group_ids: [],
        },
      ],
      description: "",
      labels: {},
    }),
    sanitize: (obj) => sanitizeInstanceCreate(obj),
  },

  // ====== System (kacho-only admin: Region / Zone / AddressPool) ======
  // Admin-only: эти ресурсы exposed через apiGW REST для admin UI.
  // На external TLS endpoint НЕ публикуются — admin-CRUD идёт через internal mux.
  // Geography (Region/Zone) обслуживает kacho-geo (`/geo/v1/*`, эпик #82):
  //   read  = geo.v1.{Region,Zone}Service (Get/List, sync);
  //   admin = geo.v1.Internal{Region,Zone}Service (Create/Update/Delete → Operation).
  // AddressPool — kacho-vpc (`/vpc/v1/addressPools`). Все мутации async → Operation.

  regions: {
    id: "regions",
    route: "regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    plural: "Регионы",
    serviceTitle: "Администрирование",
    scope: "global",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      { header: "Имя", path: "name", format: "text" },
      COL_CREATED,
    ],
    fields: [
      {
        name: "id",
        label: "Region ID",
        type: "string",
        required: true,
        immutable: true,
        placeholder: "<region-id>",
        description: "Lower-snake-kebab. Immutable PK.",
        pattern: "^[a-z][a-z0-9-]*$",
      },
      {
        name: "name",
        label: "Name",
        type: "string",
        placeholder: "Region display name",
      },
    ],
    template: () => ({ id: "", name: "" }),
  },

  zones: {
    id: "zones",
    route: "zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    plural: "Зоны",
    serviceTitle: "Администрирование",
    scope: "global",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Имя", path: "name", format: "text" },
      COL_CREATED,
    ],
    fields: [
      {
        name: "id",
        label: "Zone ID",
        type: "string",
        required: true,
        immutable: true,
        placeholder: "<zone-id>",
        pattern: "^[a-z][a-z0-9-]*$",
      },
      {
        name: "region_id",
        label: "Region",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
      },
      {
        name: "name",
        label: "Name",
        type: "string",
        placeholder: "Zone display name",
      },
    ],
    template: () => ({ id: "", region_id: "", name: "" }),
  },

  "address-pools": {
    id: "address-pools",
    route: "address-pools",
    apiPath: "/vpc/v1/addressPools",
    payloadKey: "pools",
    singular: "Пул адресов",
    plural: "Пулы адресов",
    genitive: "Пула адресов",
    serviceTitle: "Администрирование",
    scope: "global",
    ops: { create: true, update: true, delete: true },
    columns: [
      // Те же колонки и стиль, что у subnets list (CopyableName/Id, отдельные
      // v4/v6 блоки, LabelsCell): visual parity по запросу user'а.
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Тип", path: "kind", format: "text" },
      { header: "Зона", path: "zone_id", format: "text" },
      {
        header: "IPv4 CIDR",
        path: "v4_cidr_blocks",
        render: (row) => {
          const v4 = (row.v4_cidr_blocks as string[] | undefined) ?? [];
          return v4.length > 0 ? (
            <span className="font-mono text-xs">{v4.join(", ")}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        header: "IPv6 CIDR",
        path: "v6_cidr_blocks",
        render: (row) => {
          const v6 = (row.v6_cidr_blocks as string[] | undefined) ?? [];
          return v6.length > 0 ? (
            <span className="font-mono text-xs">{v6.join(", ")}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      { header: "По умолчанию", path: "is_default", format: "text" },
      {
        header: "Метки селектора",
        path: "selector_labels",
        render: (row) => (
          <LabelsCell
            labels={row.selector_labels as Record<string, string> | undefined}
          />
        ),
      },
      {
        header: "Приоритет селектора",
        path: "selector_priority",
        format: "text",
      },
    ],
    fields: [
      {
        name: "name",
        label: "Name",
        type: "string",
        placeholder: "<pool-name>",
      },
      { name: "description", label: "Description", type: "text", rows: 2 },
      {
        // kind — UI ограничен одним значением, скрыт; backend требует поле в payload.
        name: "kind",
        label: "Kind",
        type: "enum",
        options: POOL_KINDS,
        required: true,
        default: "EXTERNAL_PUBLIC",
        immutable: true,
        hidden: true,
      },
      {
        name: "zone_id",
        label: "Zone",
        type: "ref",
        refResource: "zones",
        immutable: true,
        description: "Опционально. Если пусто — глобальный пул (fallback).",
      },
      // KAC-71: spec address-pools используется только для admin list+filter
      // (для Create/Edit модалок — custom InlineAddressPool*Form с
      // <SubnetCidrChips/>, см. resource-registry.tsx top-of-file note). Поля
      // ниже только для FormFieldRenderer-fallback'а; реальная форма всегда
      // через ResourceFormModal custom-ветку.
      {
        name: "v4_cidr_blocks",
        label: "IPv4 CIDR blocks",
        type: "array",
        itemLabel: "v4-CIDR",
        description:
          "IPv4 CIDR-блоки, из которых аллоцируются внешние v4 адреса.",
        // KAC-269: CIDR задаётся только при Create; Update больше не меняет CIDR
        // (proto убрал поля из UpdateAddressPoolRequest). В edit-форме скрыто и
        // не попадает в update_mask — изменение через :addCidrBlocks /
        // :removeCidrBlocks (AddressPoolCidrManager).
        createOnly: true,
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "CIDR",
            type: "string",
            placeholder: "198.51.100.0/24",
          },
        ],
      },
      {
        name: "v6_cidr_blocks",
        label: "IPv6 CIDR blocks",
        type: "array",
        itemLabel: "v6-CIDR",
        description:
          "IPv6 CIDR-блоки, из которых аллоцируются внешние v6 адреса.",
        // KAC-269: createOnly — см. v4_cidr_blocks выше.
        createOnly: true,
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "CIDR",
            type: "string",
            placeholder: "2001:db8::/64",
          },
        ],
      },
      {
        name: "is_default",
        label: "Default for zone+kind",
        type: "bool",
        default: false,
        description: "Один is_default=true на (zone, kind).",
      },
      {
        name: "selector_priority",
        label: "Selector priority",
        type: "int",
        default: 0,
        description: "Tie-break при равенстве specificity. Higher wins.",
      },
    ],
    template: () => ({
      name: "",
      description: "",
      kind: "EXTERNAL_PUBLIC",
      zone_id: "",
      v4_cidr_blocks: [],
      v6_cidr_blocks: [],
      is_default: false,
      selector_priority: 0,
    }),
    // KAC-71: cidr_blocks разделён на v4_cidr_blocks + v6_cidr_blocks. Конвертирует
    // [{value: "..."}] → ["..."] для wire format (как subnets.v4/v6_cidr_blocks),
    // отбрасывает пустые и legacy-поле cidr_blocks.
    sanitize: (obj) => {
      const flat: Record<string, unknown> = { ...obj };
      for (const key of ["v4_cidr_blocks", "v6_cidr_blocks"]) {
        const raw = flat[key];
        if (Array.isArray(raw)) {
          flat[key] = raw
            .map((item) =>
              typeof item === "object" &&
              item !== null &&
              "value" in (item as object)
                ? (item as Record<string, unknown>)["value"]
                : item,
            )
            .filter((v) => typeof v === "string" && v.trim() !== "");
        }
      }
      delete flat["cidr_blocks"];
      return flat;
    },
  },

  // Hypervisor resource удалён (KAC-36/KAC-82, post-kube-ovn): kube-ovn управляет
  // инвентарём нод через k8s Node objects, наша таблица hypervisors / proto-сервис
  // больше не нужны. См. kacho-compute миграция 0006_drop_hypervisors.sql.

  // ====== nlb (KAC-141: Network Load Balancer; KAC-171 UI integration) ======
  // proto: kacho.cloud.nlb.v1
  // REST: /nlb/v1/networkLoadBalancers, /nlb/v1/listeners, /nlb/v1/targetGroups
  // ID prefixes: nlb / lst / tgr

  "load-balancers": {
    id: "load-balancers",
    route: "load-balancers",
    apiPath: "/nlb/v1/networkLoadBalancers",
    // KAC-226: proto ListNetworkLoadBalancersResponse repeated-поле —
    // `network_load_balancers` (на проводе networkLoadBalancers → camelToSnake).
    // Было "load_balancers" → ResourceListPage читал data[undefined] → список пуст.
    payloadKey: "network_load_balancers",
    singular: "Балансировщик нагрузки",
    plural: "Балансировщики нагрузки",
    genitive: "Балансировщика нагрузки",
    serviceTitle: "Network Load Balancer",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE, // DNS-1123 — lowercase + цифры + дефисы (как у NLB regex)
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        description:
          "Регион размещения балансировщика. Cross-service ref → compute.Region; verified на request-path.",
      },
      {
        name: "type",
        label: "Тип",
        type: "enum",
        required: true,
        default: "EXTERNAL",
        options: [
          { value: "EXTERNAL", label: "EXTERNAL (публичный VIP)" },
          { value: "INTERNAL", label: "INTERNAL (cluster-internal VIP)" },
        ],
        description:
          "Тип VIP-адреса: EXTERNAL — публичный, INTERNAL — внутренний (immutable после Create).",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      type: "EXTERNAL",
      labels: {},
    }),
  },

  listeners: {
    id: "listeners",
    route: "listeners",
    apiPath: "/nlb/v1/listeners",
    payloadKey: "listeners",
    singular: "Обработчик",
    plural: "Listeners",
    serviceTitle: "Network Load Balancer",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Балансировщик",
        path: "load_balancer_id",
        render: (row) => (
          <RefNameLink
            specId="load-balancers"
            refId={row.load_balancer_id as string | undefined}
            maxChars={36}
          />
        ),
      },
      { header: "Протокол", path: "protocol", format: "code" },
      { header: "Порт", path: "port", format: "text" },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "load_balancer_id",
        label: "Балансировщик",
        type: "string",
        required: true,
        description:
          "ID балансировщика-родителя (immutable после Create). Within-service FK → load_balancers.",
      },
      {
        name: "protocol",
        label: "Протокол",
        type: "enum",
        required: true,
        options: [
          { value: "TCP", label: "TCP" },
          { value: "UDP", label: "UDP" },
        ],
        description: "L4 транспорт (immutable после Create).",
      },
      {
        name: "port",
        label: "Порт",
        type: "int",
        required: true,
        description: "Внешний порт 1..65535 (immutable после Create).",
      },
      {
        name: "target_port",
        label: "Порт на target",
        type: "int",
        required: false,
        description:
          "Порт на target-е (1..65535). Если не задан — равен `port`.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      load_balancer_id: "",
      protocol: "TCP",
      port: 0,
      labels: {},
    }),
  },

  "target-groups": {
    id: "target-groups",
    route: "target-groups",
    apiPath: "/nlb/v1/targetGroups",
    payloadKey: "target_groups",
    singular: "Целевая группа",
    plural: "Target Groups",
    genitive: "Целевой группы",
    serviceTitle: "Network Load Balancer",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => (
          <CopyableName
            name={(row.name as string) ?? ""}
            fallback={row.id as string}
          />
        ),
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => (
          <LabelsCell
            labels={row.labels as Record<string, string> | undefined}
          />
        ),
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description:
          "Регион размещения target-group (immutable после Create). Cross-service ref → compute.Region.",
      },
      {
        name: "deregistration_delay_seconds",
        label: "Drain timeout (с)",
        type: "int",
        required: false,
        default: 300,
        description:
          "Сколько ждать прекращения трафика перед удалением target'а из активного набора (0..3600). По умолчанию 300.",
      },
      {
        name: "health_check.name",
        label: "HC: имя",
        type: "string",
        required: true,
        description:
          "Имя health-check'а (3-63 символа, lowercase + цифры + дефисы). Уникально в пределах target-group.",
      },
      {
        name: "health_check.tcp_options.port",
        label: "HC: TCP-порт",
        type: "int",
        required: true,
        default: 80,
        description: "TCP-порт для health-check'а (1..65535). По умолчанию 80.",
      },
      {
        name: "health_check.interval",
        label: "HC: интервал",
        type: "string",
        required: true,
        default: "2s",
        description:
          "Интервал между health-check'ами (Duration в формате 'Ns', range 1s-600s). По умолчанию 2s.",
      },
      {
        name: "health_check.timeout",
        label: "HC: таймаут",
        type: "string",
        required: true,
        default: "1s",
        description:
          "Таймаут одного health-check'а (Duration). По умолчанию 1s.",
      },
      {
        name: "health_check.unhealthy_threshold",
        label: "HC: failure threshold",
        type: "int",
        required: true,
        default: 2,
        description:
          "Сколько failed checks подряд до перевода в UNHEALTHY (2..10). По умолчанию 2.",
      },
      {
        name: "health_check.healthy_threshold",
        label: "HC: success threshold",
        type: "int",
        required: true,
        default: 2,
        description:
          "Сколько успешных checks подряд до перевода в HEALTHY (2..10). По умолчанию 2.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      deregistration_delay_seconds: 300,
      health_check: {
        name: "default-hc",
        tcp_options: { port: 80 },
        interval: "2s",
        timeout: "1s",
        unhealthy_threshold: 2,
        healthy_threshold: 2,
      },
      labels: {},
    }),
  },
};

// Экспортирована для тестов.
export function sanitizeSgRule(
  r: Record<string, unknown>,
): Record<string, unknown> {
  const protoMode =
    (r._protocol_mode as string | undefined) ??
    (r.protocol_name
      ? "name"
      : typeof r.protocol_number === "number"
        ? "number"
        : "any");
  const portsAny = typeof r._ports_any === "boolean" ? r._ports_any : !r.ports;
  const targetKind =
    (r._target_kind as string | undefined) ??
    (r.cidr_blocks
      ? "cidr"
      : r.security_group_id
        ? "sg"
        : r.predefined_target
          ? "predefined"
          : "cidr");

  const out: Record<string, unknown> = {};
  // copy non-discriminator persistent fields
  for (const [k, v] of Object.entries(r)) {
    if (k.startsWith("_")) continue;
    out[k] = v;
  }
  // protocol oneof-like
  if (protoMode === "any") {
    delete out.protocol_name;
    delete out.protocol_number;
  } else if (protoMode === "name") {
    delete out.protocol_number;
  } else if (protoMode === "number") {
    delete out.protocol_name;
  }
  // ports
  if (portsAny) {
    delete out.ports;
  }
  // target oneof — оставляем только нужный
  if (targetKind === "cidr") {
    delete out.security_group_id;
    delete out.predefined_target;
  } else if (targetKind === "sg") {
    delete out.cidr_blocks;
    delete out.predefined_target;
  } else if (targetKind === "predefined") {
    delete out.cidr_blocks;
    delete out.security_group_id;
  }
  return out;
}

// === compute byte/GiB helpers ===
const GIB = 1024 * 1024 * 1024;

/** fmtBytesGiB — отображает число байт как "<N> ГиБ" (округление вверх до целых). */
export function fmtBytesGiB(v: unknown): string {
  const n = typeof v === "string" ? Number(v) : typeof v === "number" ? v : NaN;
  if (!Number.isFinite(n) || n <= 0) return "—";
  const gib = n / GIB;
  return `${gib >= 10 ? Math.round(gib) : Math.round(gib * 10) / 10} ГиБ`;
}

/** gibToBytes — конвертирует значение из ГиБ-инпута в строку байт для wire format. */
export function gibToBytes(v: unknown): string | undefined {
  const n = typeof v === "string" ? Number(v) : typeof v === "number" ? v : NaN;
  if (!Number.isFinite(n) || n <= 0) return undefined;
  return String(Math.round(n * GIB));
}

/** sanitizeInstanceCreate — превращает form-internal представление CreateInstanceRequest
 *  в wire format: memory_gib→memory (байты), size_gib→size, core_fraction строка→число,
 *  boot_disk oneof (disk_spec vs disk_id), one-to-one NAT toggle → one_to_one_nat_spec,
 *  security_group_ids [{value}]→[ids]; вырезает _boot_source и пустые поля. */
export function sanitizeInstanceCreate(
  obj: Record<string, unknown>,
): Record<string, unknown> {
  const o = { ...obj } as Record<string, unknown>;

  // resources_spec
  const rs = { ...((o["resources_spec"] as Record<string, unknown>) ?? {}) };
  if (rs["memory_gib"] !== undefined) {
    rs["memory"] = gibToBytes(rs["memory_gib"]);
    delete rs["memory_gib"];
  }
  if (rs["cores"] !== undefined && rs["cores"] !== "")
    rs["cores"] = Number(rs["cores"]);
  if (rs["core_fraction"] !== undefined && rs["core_fraction"] !== "")
    rs["core_fraction"] = Number(rs["core_fraction"]);
  o["resources_spec"] = rs;

  // boot_disk_spec
  const bootSource = o["_boot_source"];
  const bds = { ...((o["boot_disk_spec"] as Record<string, unknown>) ?? {}) };
  if (bootSource === "image") {
    const ds = { ...((bds["disk_spec"] as Record<string, unknown>) ?? {}) };
    if (ds["size_gib"] !== undefined) {
      ds["size"] = gibToBytes(ds["size_gib"]);
      delete ds["size_gib"];
    }
    if (ds["type_id"] === "" || ds["type_id"] === undefined)
      delete ds["type_id"];
    if (ds["image_id"] === "" || ds["image_id"] === undefined)
      delete ds["image_id"];
    bds["disk_spec"] = ds;
    delete bds["disk_id"];
  } else {
    // existing disk
    delete bds["disk_spec"];
  }
  o["boot_disk_spec"] = bds;
  delete o["_boot_source"];

  // network_interface_specs — собираем wire-shape из form-internal представления
  // NIC-айтема (NicSpecFields.tsx). Возможные результаты на айтем:
  //   {nic_id}                                            — выбран существующий NIC;
  //   {subnet_id}                                         — подсеть, без адресов;
  //   {subnet_id, primary_v4_address_spec.address}        — подсеть + внутренний IPv4;
  //   + опц. primary_v4_address_spec.one_to_one_nat_spec  — external-IP режим;
  //   + опц. security_group_ids: [...]
  const nics = Array.isArray(o["network_interface_specs"])
    ? (o["network_interface_specs"] as Record<string, unknown>[])
    : [];
  o["network_interface_specs"] = nics.map((nic) => {
    const out: Record<string, unknown> = {};
    const sgs = Array.isArray(nic["security_group_ids"])
      ? (nic["security_group_ids"] as unknown[])
          .map((it) =>
            typeof it === "object" && it !== null && "value" in (it as object)
              ? (it as Record<string, unknown>)["value"]
              : it,
          )
          .filter((v) => typeof v === "string" && v)
      : [];
    // Существующий NetworkInterface (nic_id) — отдаём только nic_id (+ SG, если заданы);
    // подсеть/адрес берутся из самого NIC (см. compute.v1.NetworkInterfaceSpec.nic_id, KAC-5).
    if (nic["_use_existing_nic"] === true && nic["nic_id"]) {
      out["nic_id"] = nic["nic_id"];
      if (sgs.length > 0) out["security_group_ids"] = sgs;
      return out;
    }
    if (nic["subnet_id"]) out["subnet_id"] = nic["subnet_id"];
    if (sgs.length > 0) out["security_group_ids"] = sgs;
    const primaryAddr =
      typeof nic["primary_v4_address_spec"] === "object" &&
      nic["primary_v4_address_spec"] !== null
        ? ((nic["primary_v4_address_spec"] as Record<string, unknown>)[
            "address"
          ] as string | undefined)
        : undefined;
    const pv4: Record<string, unknown> = {};
    if (primaryAddr) pv4["address"] = primaryAddr;
    const extMode = nic["_ext_mode"] as string | undefined;
    if (extMode === "auto") {
      pv4["one_to_one_nat_spec"] = { ip_version: "IPV4" };
    } else if (extMode === "list") {
      const ipVal = nic["_ext_addr_value"] as string | undefined;
      // OneToOneNatSpec.address — это IP-строка (не Address-id), см. proto.
      if (ipVal) pv4["one_to_one_nat_spec"] = { address: ipVal };
    }
    if (Object.keys(pv4).length > 0) out["primary_v4_address_spec"] = pv4;
    return out;
  });

  // strip optional empties
  for (const k of ["hostname", "service_account_id"]) {
    if (o[k] === "" || o[k] === undefined) delete o[k];
  }
  return o;
}

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// resourceServicePrefix — service-segment под /projects/:projectId/ (или
// /iam/ для IAM-scoped) per spec.id. Соответствует routes в App.tsx
// (KAC-198 fix: некоторые компоненты строили `/projects/<pid>/<route>` без
// этого сегмента — детальная страница 404'илась).
export function resourceServicePrefix(
  specId: string,
): "vpc" | "compute" | "nlb" | "iam" {
  if (specId.startsWith("compute-")) return "compute";
  switch (specId) {
    // NLB domain
    case "network-load-balancers":
    case "load-balancers":
    case "listeners":
    case "target-groups":
      return "nlb";
    // IAM domain — пути под /iam/<route>, не под /projects/
    case "accounts":
    case "projects":
    case "users":
    case "service-accounts":
    case "groups":
    case "roles":
    case "access-bindings":
      return "iam";
    // Compute admin (без compute- префикса)
    case "regions":
    case "zones":
    case "address-pools":
      return "compute";
    default:
      // VPC ресурсы: networks, subnets, addresses, route-tables,
      // security-groups, network-interfaces, gateways
      return "vpc";
  }
}

// resourceProjectPath — полный SPA-путь до listing данного ресурса в
// контексте project'а. Возвращает null для IAM-ресурсов (они не scoped to
// project) и когда projectId не известен.
export function resourceProjectPath(
  specId: string,
  projectId: string | null | undefined,
): string | null {
  const prefix = resourceServicePrefix(specId);
  if (prefix === "iam") return null;
  if (!projectId) return null;
  const spec = REGISTRY[specId];
  if (!spec) return null;
  return `/projects/${projectId}/${prefix}/${spec.route}`;
}

// Thin generic wrapper over the single lib/path implementation (superset that
// also resolves bracket-indexed array paths like "spec.rules[0].direction").
// Kept as a named export (re-exported as getResourceValueByPath) so the many
// detail/list call sites keep their <T> type signature unchanged.
export function getByPath<T = unknown>(
  obj: unknown,
  path: string,
): T | undefined {
  return getByPathImpl(obj, path) as T | undefined;
}

// applyDefaults — для Create-формы прогоняем все поля и подставляем default-ы
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
