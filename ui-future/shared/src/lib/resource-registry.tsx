// Реестр ресурсов: метаданные для generic ListPage / DetailPage / Create-Edit.
// Scope: 7 ресурсов Kachō proto.
// apiPath содержит полный путь с доменным префиксом (verbatim из proto google.api.http annotations).

import type { ReactNode } from "react";
import { Tag } from "antd";
import type { FormField } from "./form-schema";
import { setByPath, getByPath as getByPathImpl } from "./path";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { PlacementBadge } from "@shared/components/atoms/PlacementBadge";
import {
  GEO_INTERNAL_REGIONS_PATH,
  GEO_INTERNAL_ZONES_PATH,
  GEO_REGIONS_PATH,
  GEO_ZONES_PATH,
  placementBlockedText,
  readCountHint,
  zoneBelongsToRegion,
  type PlacementBlockedReason,
} from "@shared/api/geo";
import { RoutesEditor, type RouteEntry } from "@shared/components/organisms/RoutesEditor";
import { CopyableName } from "@shared/components/atoms/CopyableName";
import { CidrListCell } from "@shared/components/molecules/CidrListCell";
import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { IamRefLink } from "@shared/components/molecules/IamRefLink";
import { LabelsCell } from "@shared/components/atoms/LabelsCell";
import { NicSpecFields } from "@shared/components/organisms/form/NicSpecFields";
import { stripFormOnlyKeys } from "@shared/lib/update-mask";
import {
  roleIsSystem,
  targetKind,
  targetResources,
  type AccessBindingTarget,
  type DefinitionTier,
} from "@shared/api/iam";
import { displayText } from "@shared/lib/display-text";
import { flatIdList } from "@shared/lib/id-list";
import {
  GUEST_ACCESS_KEY_EMPTY_STATE,
  GUEST_ACCESS_KEY_FIELDS,
  guestAccessKeyTemplate,
} from "@shared/lib/guest-access-key-form";
import { resourceListPath as resourceListPathImpl } from "@shared/lib/service-prefix";
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";
import type { ResourceColumn, ResourceSpec } from "./resource-spec";
// Подписи сущностей и разделов — из единственного источника (см. entity-names.ts):
// литерал рядом с местом показа расходится молча, ссылка — нет.
import { ENTITIES, SERVICES } from "./entity-names";

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`, и импортируется
// сюда. Реэкспорт оставлен, чтобы потребители этого модуля не меняли импорты: у него
// нет тела, поэтому разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы
// здесь запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.

export type { ResourceColumn, ResourceSpec };

// ── Наборы, которые форма правит ПОЛНОЙ ЗАМЕНОЙ ─────────────────────────────
//
// Схема формы уносит набор на край целиком, поэтому поле контракта, которого не
// назвал тип-черновик, исчезает у ВСЕХ элементов — включая нетронутые. Состав
// черновиков сверяется с контрактом гейтом
// `test/set-replacement-draft-composition`, а перепись мест он берёт обходом
// дерева: новое такое место без объявления рядом уронит его с координатой.

/** Строки маршрутов формы таблицы маршрутизации (`render` + `sanitize` ниже). */
export const STATIC_ROUTES_REPLACEMENT: SetReplacementDraft = {
  field: "static_routes",
  contract: "kacho/cloud/vpc/v1/route_table.proto",
  message: "StaticRoute",
  drafts: ["RouteEntry"],
};

/** Правила формы группы безопасности (`sanitize` ниже). */
export const SG_RULE_SPECS_REPLACEMENT: SetReplacementDraft = {
  field: "rule_specs",
  contract: "kacho/cloud/vpc/v1/security_group_service.proto",
  message: "SecurityGroupRuleSpec",
  drafts: ["RuleExt"],
};

// ── Geography (Region / Zone) — общие куски их спеков ────────────────────────

/** Slug-инвариант admin-назначаемого id Region/Zone — тот же, что энфорсит geo. */
const REGION_ZONE_ID_PATTERN = "^[a-z][a-z0-9]*(-[a-z0-9]+)*$";

/** Сырой admin-флаг обслуживания. UNSPECIFIED оператору не предлагаем. */
const GEO_STATUS_OPTIONS = [
  { value: "UP", label: "UP — принимает размещение" },
  { value: "DOWN", label: "DOWN — закрыт для размещения" },
];

/** Строки textarea → repeated-поле: пустые и пробельные отбрасываем. */
function splitLines(value: unknown): string[] {
  if (Array.isArray(value)) return value.filter((v): v is string => typeof v === "string" && v.trim() !== "");
  if (typeof value !== "string") return [];
  return value
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * Общая чистка формы Region/Zone перед отправкой: пустые опциональные скаляры не
 * шлём, пустой блок infra° не шлём. Пустая строка и «поле не задано» — разные
 * вещи, и сервер валидирует первую (напр. countryCode).
 */
function sanitizeGeoCommon(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  if (out.country_code === "") delete out.country_code;
  const infra = out.infra as Record<string, unknown> | undefined;
  if (infra) {
    const next: Record<string, unknown> = { ...infra };
    if (next.numeric_infra_id === "") delete next.numeric_infra_id;
    if (Object.keys(next).length === 0) delete out.infra;
    else out.infra = next;
  }
  return out;
}

/** Ячейка «Открытых зон»: подсказка read-time, int64 приезжает строкой. */
function CountHintCell({ value }: { value: unknown }): ReactNode {
  const n = readCountHint(value);
  // Подсказка, а не инвариант: сервер прямо предупреждает, что она может
  // расходиться с ZoneService.List?openForPlacement=true.
  return n === null ? <span className="text-muted-foreground">—</span> : <>{n}</>;
}

/** Ячейка «Причина»: только для закрытой строки и только для известной причины. */
function BlockedReasonCell({
  open,
  reason,
}: {
  open: boolean | undefined;
  reason: PlacementBlockedReason | undefined;
}): ReactNode {
  const text = open === false ? placementBlockedText(reason) : null;
  return text ? <>{text}</> : <span className="text-muted-foreground">—</span>;
}

// Pool kinds — единственный валидный тип. KAC-70 удалил EXTERNAL_TEST/
// RESERVED_INTERNAL из proto enum kacho.cloud.vpc.v1.AddressPoolKind
// (`reserved 2, 100`).
const POOL_KINDS = [{ value: "EXTERNAL_PUBLIC", label: "Внешний публичный" }];

// Правило группы размещения названо СЛЕДСТВИЕМ, а не машинным значением: «SPREAD»
// не говорит ни что группа разнесена, ни зачем. Словарь один и тот же в списке и
// на карточке — иначе один предмет читался бы двумя именами.
const PLACEMENT_STRATEGY_TEXT: Record<string, string> = {
  SPREAD: "Разнести по разным доменам отказа",
  PACK: "Сблизить в одном домене",
};
const placementDash = <span className="text-muted-foreground">—</span>;

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
  description: "Строчные латинские буквы, цифры и дефисы. Должно начинаться с буквы, длина 2–63 символа.",
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
  label: "Проект",
  type: "string",
  hidden: true,
};

// Hidden поле для account-context (IAM: Project / ServiceAccount scoped по Account).
const FIELD_ACCOUNT_ID: FormField = {
  name: "account_id",
  label: "Аккаунт",
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

// ── Источник VIP балансировщика (per-family oneof v4_source / v6_source) ──
//
// Контракт (loadbalancer.v1 + services/nlb/.../vip_source.go):
//   • хотя бы одно семейство обязано нести источник — иначе запрос отвергается
//     целиком («must declare a vip source for at least one ip family»);
//   • ветвь oneof связана с режимом балансировщика: `subnet_id` допустим ТОЛЬКО
//     для INTERNAL, `public {}` — ТОЛЬКО для EXTERNAL (validateSourceTypeMatrix);
//   • режим задаёт ЕДИНСТВЕННО `placement` — `type`/`placement_type` в запросе
//     существуют лишь затем, чтобы выставивший их клиент получил явный отказ.
//
// Поэтому «авто» означает разное по обе стороны и нормализуется от placement, а
// не от того, что осталось в виджете после смены режима: подсеть, выбранная в
// INTERNAL-черновике, после переключения на EXTERNAL схлопывается в public, а не
// уезжает телом, которое сервис отвергнет.

export function lbTypeFromPlacement(placement: string | undefined): "EXTERNAL" | "INTERNAL" {
  return placement === "EXTERNAL_REGIONAL" ? "EXTERNAL" : "INTERNAL";
}

/**
 * buildVipSourceOrNull — wire-ветвь oneof одного семейства, либо null, если
 * семейство не задано. Пустой subnet_id/address_id — это «не задано», а не
 * `{subnet_id: ""}`: выбранная ветвь oneof с пустой ссылкой возвращается с
 * сервиса как «required», то есть жалобой на поле, которого оператор не называл.
 */
export function buildVipSourceOrNull(
  placement: string | undefined,
  obj: Record<string, unknown>,
  family: "v4" | "v6",
): Record<string, unknown> | null {
  const mode = (obj[`_${family}_source`] as string | undefined) ?? "off";
  if (mode === "off") return null;
  const fam = (obj[`${family}_source`] as Record<string, unknown> | undefined) ?? {};
  if (mode === "address") {
    const id = (fam.address_id as string) || "";
    return id ? { address_id: id } : null;
  }
  // Автоматические ветви — «public» и «subnet» — нормализуются под placement:
  // выбранная в INTERNAL-черновике подсеть после переключения на EXTERNAL
  // схлопывается в public, а не уезжает телом, которое сервис отвергнет.
  if (lbTypeFromPlacement(placement) === "EXTERNAL") return { public: {} };
  const subnetId = (fam.subnet_id as string) || "";
  return subnetId ? { subnet_id: subnetId } : null;
}

/** Поля одного семейства: режим + ссылка активного режима. */
function vipSourceFields(family: "v4" | "v6", label: string): FormField[] {
  const mode = `_${family}_source`;
  return [
    {
      name: mode,
      label: `Источник VIP (${label})`,
      type: "enum",
      immutable: true,
      default: family === "v4" ? "public" : "off",
      options: [
        { value: "public", label: "Публичный (авто) — VIP выделяет платформа (EXTERNAL-размещение)" },
        { value: "subnet", label: "Из подсети (авто) — VIP выделяется из подсети (INTERNAL-размещение)" },
        { value: "address", label: "Линк адреса — заранее созданный Address" },
        { value: "off", label: "Не задавать это семейство" },
      ],
      description:
        "Ветвь источника VIP этого семейства. Хотя бы одно семейство обязано нести источник. Подсеть допустима только для INTERNAL-размещения, публичный VIP — только для EXTERNAL.",
    },
    {
      name: `${family}_source.subnet_id`,
      label: `Подсеть (${label})`,
      type: "ref",
      refResource: "subnets",
      refProjectScoped: true,
      immutable: true,
      visibleWhen: { field: mode, equals: "subnet" },
      description:
        "Подсеть, из которой выделяется адрес балансировщика при внутреннем размещении. Размещение подсети обязано совпадать с размещением балансировщика.",
    },
    {
      name: `${family}_source.address_id`,
      label: `Адрес (${label})`,
      type: "ref",
      refResource: "addresses",
      refProjectScoped: true,
      immutable: true,
      visibleWhen: { field: mode, equals: "address" },
      refFilter: (row) =>
        family === "v4"
          ? !!row.internal_ipv4_address || !!row.external_ipv4_address
          : !!row.internal_ipv6_address || !!row.external_ipv6_address,
      description: "Существующий Address, линкуемый как VIP. Сфера адреса обязана совпадать с режимом балансировщика.",
    },
  ];
}

// VPC-1 Subnet cell: immutable primary CIDR anchor + "+N" additional-ranges
// hint (additional ranges managed via :add/:remove-cidr-blocks, not shown inline).
// Основной блок семейства и дополнительные диапазоны — одним списком, каждый
// своей строкой. Прежде видимым оставался ТОЛЬКО основной, а дополнительные
// сворачивались в «+N»: число, из которого не узнать ни одного адреса.
function CidrPrimaryCell({ primary, extra }: { primary: unknown; extra: unknown }): ReactNode {
  return <CidrListCell items={[primary, extra]} />;
}

// VPC-1 Network cell: объявленный супернет (IPv4, затем IPv6) — каждый блок
// своей строкой. Свёртка хвоста в «+N» снята: она показывала два блока из
// скольких угодно.
function SupernetCell({ v4, v6 }: { v4: unknown; v6: unknown }): ReactNode {
  return <CidrListCell items={[v4, v6]} />;
}

// Размещение (ZONAL zone | REGIONAL region, anycast) рисует `PlacementAnchor` —
// он же ставит ССЫЛКУ на зону/регион. Здесь эта ветка прежде жила своей копией,
// показывавшей якорь плоским текстом.

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
  const dt = (row.definition_tier ?? row.definitionTier) as DefinitionTier | undefined;
  const tt = dt?.tier_type ?? dt?.tierType ?? "";
  const tid = dt?.tier_id ?? dt?.tierId ?? "";
  if (!tt) {
    const acc = (row.account_id ?? row.accountId) as string | undefined;
    return acc ? <IamRefLink specId="accounts" refId={acc} /> : IAM_DASH;
  }
  const spec = tt === "iam.account" ? "accounts" : tt === "iam.project" ? "projects" : undefined;
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
      {spec && tid ? <IamRefLink specId={spec} refId={tid} /> : tid ? <CopyableId id={tid} /> : null}
    </span>
  );
}

// AccessBinding scopeType — точечная форма (iam.cluster|iam.account|iam.project).
//
// Здесь стоял запасной путь на `scope`/`resource_type`/`resource_id`. Эти имена
// у AccessBinding ЗАРЕЗЕРВИРОВАНЫ надгробиями (`reserved 15,16,17,18; reserved
// "scope","scope_ref",…`), сервер их не отдаёт ни при каких условиях, и ветка
// была недостижимой — она документировала контракт, которого код не производит.
function scopeTypeCell(row: Record<string, unknown>): ReactNode {
  const st = displayText(row.scope_type ?? row.scopeType);
  if (!st) return IAM_DASH;
  return <Tag color={iamTierColor(st)}>{st}</Tag>;
}

// AccessBinding scope anchor (scopeId) — ссылка по типу якоря.
function scopeAnchorCell(row: Record<string, unknown>): ReactNode {
  const st = displayText(row.scope_type ?? row.scopeType);
  const anchorType =
    st === "iam.account" ? "account" : st === "iam.project" ? "project" : st === "iam.cluster" ? "cluster" : "";
  const anchorId = displayText(row.scope_id ?? row.scopeId);
  if (!anchorId) return IAM_DASH;
  const spec = anchorType === "account" ? "accounts" : anchorType === "project" ? "projects" : undefined;
  return spec ? <IamRefLink specId={spec} refId={anchorId} /> : <CopyableId id={anchorId} />;
}

// AccessBinding target (IAM-1 F8, allInScope | resources[]) → компактный тег.
function targetCell(row: Record<string, unknown>): ReactNode {
  const t = row.target as AccessBindingTarget | undefined;
  const kind = targetKind(t);
  if (kind === "resources") {
    const n = targetResources(t).length;
    return (
      <Tag color="geekblue" title="Права только на перечисленные объекты">
        {n} объект{n === 1 ? "" : "а/ов"}
      </Tag>
    );
  }
  if (kind === "allInScope")
    return (
      <Tag title="Вся область — выбрано явно" color="default">
        вся область
      </Tag>
    );
  return IAM_DASH;
}

// Subject type (UI-строка / enum-имя) → registry specId.
function subjectSpecId(t: string): string | undefined {
  if (t === "user" || t === "USER" || t === "SUBJECT_TYPE_USER") return "users";
  if (t === "group" || t === "GROUP" || t === "SUBJECT_TYPE_GROUP") return "groups";
  if (t === "service_account" || t === "SERVICE_ACCOUNT" || t === "SUBJECT_TYPE_SERVICE_ACCOUNT")
    return "service-accounts";
  return undefined;
}

// AccessBinding subjects (IAM-1 subjects[]) → первый как ref-ссылка + «+N».
// Legacy single subject_type/subject_id — fallback.
function accessBindingSubjectsCell(row: Record<string, unknown>): ReactNode {
  const subjects = row.subjects as Array<{ type?: string; id?: string }> | undefined;
  const list =
    Array.isArray(subjects) && subjects.length > 0
      ? subjects
      : row.subject_id
        ? [{ type: displayText(row.subject_type), id: displayText(row.subject_id) }]
        : [];
  if (list.length === 0) return IAM_DASH;
  const first = list[0];
  const spec = subjectSpecId(String(first.type ?? ""));
  const firstNode = spec ? (
    <IamRefLink specId={spec} refId={first.id} nameField={spec === "users" ? "email" : "name"} />
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
      {list.length > 1 && <Tag title={`Ещё ${list.length - 1} субъект(ов)`}>+{list.length - 1}</Tag>}
    </span>
  );
}

/**
 * Поле-список, чьи элементы приходят с сервера строками, а форма ведёт объектами
 * `{ value }`.
 *
 * Вынесено из ТРЁХ дословных копий (блоки сети, ссылки интерфейса, блоки
 * подсети): третью добавила волна правок консоли, и линт справедливо назвал её
 * ростом — `Array.isArray` сужает до `any[]`, поэтому возврат элемента был
 * небезопасным. Тип элемента здесь назван явно, и правило снимается не
 * подавлением, а тем, что утверждение стало верным.
 */
function hydrateStringListFields(out: Record<string, unknown>, keys: string[]): void {
  for (const key of keys) {
    const raw = out[key];
    if (!Array.isArray(raw)) continue;
    out[key] = (raw as unknown[]).map((item: unknown) => (typeof item === "string" ? { value: item } : item));
  }
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
    singular: ENTITIES.accounts.singular,
    accusative: "аккаунт",
    plural: ENTITIES.accounts.plural,
    genitive: "Аккаунта",
    serviceTitle: SERVICES.iam.title,
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
      { header: "Статус", path: "status", format: "status" },
      COL_CREATED,
      COL_ID,
    ],
    // IAM-1 F1: ownerUserId НЕ в Create-форме (derived-from-caller, output-only;
    // передача в body → sync INVALID_ARGUMENT). Create-сага co-commit'ит default
    // Project + owner-AccessBinding (F2) — сервер-сторона, не форма.
    fields: [FIELD_NAME, FIELD_LABELS, FIELD_DESCRIPTION],
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
      title: "Создайте первый аккаунт",
      body:
        "Account — верхнеуровневый tenant Kachō: владелец, проекты, пользователи и роли живут внутри него. " +
        "Создайте Account, чтобы начать выдавать доступ и заводить проекты.",
      docs: ["Аккаунты и организации"],
    },
    template: () => ({
      name: "",
      description: "",
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
    singular: ENTITIES.projects.singular,
    accusative: "проект",
    plural: ENTITIES.projects.plural,
    genitive: "Проекта",
    serviceTitle: SERVICES.iam.title,
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => <IamRefLink specId="accounts" refId={row.account_id as string} />,
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
        label: "Аккаунт",
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
    singular: ENTITIES["service-accounts"].singular,
    accusative: "сервисный аккаунт",
    plural: ENTITIES["service-accounts"].plural,
    serviceTitle: SERVICES.iam.title,
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => <IamRefLink specId="accounts" refId={row.account_id as string} />,
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
    // Пользователя знают по ПОЧТЕ: `name` у него нет вовсе, поэтому умолчание
    // «по имени или идентификатору» обещало бы поиск по несуществующему полю.
    // Сужает сервер (`filter=search="…"`, подстрока по почте и идентификатору):
    // клиентское сужение судило бы только о загруженной странице и молча
    // отвечало бы «нет такого» обо всём, что за курсором.
    search: { placeholder: "Поиск по почте или идентификатору", serverTerm: "search" },
    payloadKey: "users",
    singular: ENTITIES.users.singular,
    accusative: "пользователя",
    plural: ENTITIES.users.plural,
    serviceTitle: SERVICES.iam.title,
    scope: "global",
    ops: { create: false, update: false, delete: true },
    columns: [
      { header: "Эл. почта", path: "email", format: "text" },
      { header: "Отображаемое имя", path: "display_name", format: "text" },
      { header: "Статус", path: "invite_status", format: "status" },
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => <IamRefLink specId="accounts" refId={row.account_id as string | undefined} />,
      },
      { header: "ID", path: "id", format: "uid-short" },
      { header: "Внешний идентификатор", path: "external_id", format: "uid-short" },
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
    singular: ENTITIES.groups.singular,
    accusative: "группу",
    plural: ENTITIES.groups.plural,
    genitive: "Группы",
    serviceTitle: SERVICES.iam.title,
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Аккаунт",
        path: "account_id",
        render: (row) => <IamRefLink specId="accounts" refId={row.account_id as string | undefined} />,
      },
      COL_ID,
      { header: "Описание", path: "description", format: "text" },
      COL_CREATED,
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
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
    singular: ENTITIES.roles.singular,
    accusative: "роль",
    plural: ENTITIES.roles.plural,
    genitive: "Роли",
    serviceTitle: SERVICES.iam.title,
    scope: "account",
    ops: { create: true, update: true, delete: true },
    columns: [
      COL_NAME,
      {
        header: "Тип",
        path: "is_system",
        // IAM-1 F4/F6: isSystem° derived (definitionTier.tierType==iam.cluster);
        // fallback на хранимый is_system/isSystem (AS-IS до миграции).
        render: (row) => (roleIsSystem(row) ? <Tag color="purple">Системная</Tag> : <Tag color="default">Пользовательская</Tag>),
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
          const rules = (row.rules as Array<{ module?: string }> | undefined) ?? [];
          if (rules.length === 0) return <span className="text-muted-foreground">—</span>;
          const modules = Array.from(new Set(rules.map((r) => r.module || "*")));
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
              {more > 0 && <span style={{ fontSize: 11, color: "rgba(0,0,0,.45)" }}>+{more}</span>}
              <span style={{ fontSize: 11, color: "rgba(0,0,0,.45)" }}>· {rules.length}</span>
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
    singular: ENTITIES["access-bindings"].singular,
    accusative: "привязку доступа",
    plural: ENTITIES["access-bindings"].plural,
    genitive: "привязки доступа",
    serviceTitle: SERVICES.iam.title,
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
        render: (row) => <IamRefLink specId="roles" refId={row.role_id as string | undefined} />,
      },
      {
        // Область — точечный scopeType (iam.account|iam.project|iam.cluster).
        header: "Область",
        path: "scope_type",
        render: (row) => scopeTypeCell(row),
      },
      {
        // Якорь — scopeId, ссылка по типу якоря.
        header: "Якорь",
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
          const grantedBy = (row.granted_by_user_id ?? row.grantedByUserId) as string | undefined;
          return grantedBy ? (
            <IamRefLink specId="users" refId={grantedBy} nameField="email" maxChars={24} />
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
            <Tag color="gold" title="Защита от удаления: привязка владельца">
              Владелец
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
    // create — bespoke AccessBindingCreatePage (ops.create=false); template лишь
    // удовлетворяет обязательному полю ResourceSpec.template. Здесь стояла пара
    // resource_type/resource_id: у CreateAccessBindingRequest таких полей нет ни
    // под каким тегом, а обязательны (required) точечный scope_type и scope_id —
    // то есть реестр письменно объявлял форму запроса в снятом словаре.
    //
    // `target` (тег 15) тоже ОБЯЗАТЕЛЕН, и его отсутствие сервер отвергает первым
    // же стейтментом — «target is required; use target.allInScope{} to grant all
    // objects under the anchor». Скелет без него объявлял форму запроса, которую
    // нельзя отправить; арм назван так же, как его пишет собственный сборщик тела
    // консоли (`buildCreateAccessBindingBody`): точечный `resources`, когда
    // оператор выбрал объекты, и весь якорь — когда не выбрал.
    template: ({ accountId }) => ({
      subject_type: "user",
      subject_id: "",
      role_id: "",
      scope_type: "iam.account",
      scope_id: accountId ?? "",
      target: { all_in_scope: {} },
    }),
  },

  // ====== vpc ======
  // proto: GET /vpc/v1/networks

  networks: {
    id: "networks",
    route: "networks",
    apiPath: "/vpc/v1/networks",
    payloadKey: "networks",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    // proto: `InternalNetworkService.GetNetwork` → GET
    // /vpc/v1/networks/{network_id}:internal (глагольный суффикс отличает её от
    // публичного GET). Регистрируется только на cluster-internal mux.
    internalGetPath: "/vpc/v1/networks/{id}:internal",
    // Все трое детей сети сужаются по родителю НА СЕРВЕРЕ: `network_id` стоит в
    // белом списке выражения `filter` у каждого из трёх владельцев (паритет
    // держит `related-server-filter-parity.test.ts`, читающий эти списки из
    // прод-кода сервиса). Клиентский `filterField` остаётся подстраховкой:
    // сужение поверх курсорной страницы отфильтровало бы только то, что успело
    // приехать, и выдало бы это за весь список.
    related: [
      { childId: "subnets", filterField: "network_id", serverFilterField: "network_id", label: "Подсети" },
      {
        childId: "route-tables",
        filterField: "network_id",
        serverFilterField: "network_id",
        label: "Таблицы маршрутов",
      },
      {
        childId: "security-groups",
        filterField: "network_id",
        serverFilterField: "network_id",
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
    singular: ENTITIES.networks.singular,
    accusative: "облачную сеть",
    plural: ENTITIES.networks.plural,
    genitive: "Облачной сети",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        // VPC-1: declared supernet (immutable via Update; grown/shrunk via
        // :add/:remove-cidr-blocks). Compact IPv4-first view.
        header: "CIDR",
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
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
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
        label: "CIDR IPv4",
        type: "array",
        itemLabel: "CIDR",
        description:
          "CIDR сети (IPv4) — из него нарезаются CIDR подсетей. Неизменяемо через Update — расширяется/сужается verb-действиями на странице сети.",
        immutable: true,
        editHidden: true,
        newItem: () => ({ value: "" }),
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true, placeholder: "10.20.0.0/16" }],
      },
      {
        name: "ipv6_cidr_blocks",
        label: "CIDR IPv6",
        type: "array",
        itemLabel: "CIDR",
        description: "Опционально. CIDR сети (IPv6).",
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
            .map((item: unknown) =>
              typeof item === "object" && item !== null && "value" in item
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
      hydrateStringListFields(out, ["ipv4_cidr_blocks", "ipv6_cidr_blocks"]);
      return out;
    },
  },

  // proto: GET /vpc/v1/subnets

  subnets: {
    id: "subnets",
    route: "subnets",
    apiPath: "/vpc/v1/subnets",
    payloadKey: "subnets",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    related: [
      {
        // Под подсетью адреса всегда ВНУТРЕННИЕ (ссылка в internal_*.subnet_id).
        //
        // Сужает СЕРВЕР, и не выражением `filter`, а типизированным полем
        // запроса: у адреса подсеть лежит внутри jsonb и в ДВУХ семьях, поэтому
        // белым списком имён колонок она не выражается вовсе — владелец принимает
        // её отдельным полем `subnet_id` и отбирает объединение по обеим семьям.
        // Паритет с владельцем держит related-server-param-parity.test.ts.
        //
        // Клиентское сужение остаётся подстраховкой и здесь оно не косметика:
        // путь к ссылке в строке ответа ВЛОЖЕННЫЙ и их два, то есть выражением
        // фильтра эта же мысль не записывается.
        childId: "addresses",
        filterField: ["internal_ipv4_address.subnet_id", "internal_ipv6_address.subnet_id"],
        serverParamField: "subnet_id",
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
    singular: ENTITIES.subnets.singular,
    accusative: "подсеть",
    plural: ENTITIES.subnets.plural,
    genitive: "Подсети",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Сеть",
        path: "network_id",
        render: (row) => <RefNameLink specId="networks" refId={row.network_id as string | undefined} />,
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
        render: (row) => <PlacementAnchor row={row} maxChars={28} />,
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      {
        header: "Таблица маршрутизации",
        path: "route_table_id",
        render: (row) => <RefNameLink specId="route-tables" refId={row.route_table_id as string | undefined} />,
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
        label: "Облачная сеть",
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
          "Неизменяемый основной CIDR-блок подсети (⊆ одного CIDR-блока сети). Хотя бы один из IPv4/IPv6 обязателен. Доп. диапазоны добавляются на странице подсети.",
        immutable: true,
      },
      {
        name: "ipv6_cidr_primary",
        label: "Основной IPv6 CIDR",
        type: "string",
        placeholder: "fd00:20::/64",
        description: "Опционально. Неизменяемый основной IPv6 CIDR-блок подсети (⊆ IPv6 CIDR сети).",
        immutable: true,
      },
      {
        name: "route_table_id",
        label: "Таблица маршрутов",
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
      const region = typeof out["region_id"] === "string" ? out["region_id"] : "";
      const pt = typeof out["placement_type"] === "string" ? out["placement_type"] : "";
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
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
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
    singular: ENTITIES.addresses.singular,
    accusative: "IP-адрес",
    // Нейтральный plural — список содержит и внешние (Публичные), и внутренние
    // адреса; вид различается колонкой «Вид» (Публичный/Внутренний). Раньше было
    // «Публичные IP-адреса», что вводило в заблуждение для внутренних.
    plural: ENTITIES.addresses.plural,
    genitive: "IP-адреса",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "IP-адрес",
        path: "external_ipv4_address.address",
        render: (row) => {
          const ext = (row.external_ipv4_address as { address?: string } | undefined)?.address;
          const ext6 = (row.external_ipv6_address as { address?: string } | undefined)?.address;
          const int = (row.internal_ipv4_address as { address?: string } | undefined)?.address;
          const int6 = (row.internal_ipv6_address as { address?: string } | undefined)?.address;
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
        render: (row) => <BoolFact value={row.used} yes="Используется" no="Свободен" />,
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
        render: (row) => (
          <BoolFact value={row.deletion_protection} yes="Удаление запрещено" no="Удаление разрешено" accent />
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
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
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
        description: "Конкретный IPv4-адрес из CIDR выбранной подсети. Оставьте пустым — будет выделен автоматически.",
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
        description: "Конкретный IPv6-адрес из CIDR выбранной подсети. Оставьте пустым — будет выделен автоматически.",
        visibleWhen: { field: "_address_kind", equals: "internal_v6" },
        editHidden: true,
      },
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        default: false,
        description: "Если включена, адрес нельзя будет удалить, пока защита не будет снята.",
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
        if (k === "external_ipv6_address_spec" && kind !== "external_v6") continue;
        if (k === "internal_ipv4_address_spec" && kind !== "internal") continue;
        if (k === "internal_ipv6_address_spec" && kind !== "internal_v6") continue;
        result[k] = v;
      }
      // Общая зона `_zone_id` → в активную external-ветку spec'а.
      const zone = obj["_zone_id"];
      if (zone) {
        if (kind === "external") {
          result["external_ipv4_address_spec"] = {
            ...(result["external_ipv4_address_spec"] as Record<string, unknown> | undefined),
            zone_id: zone,
          };
        } else if (kind === "external_v6") {
          result["external_ipv6_address_spec"] = {
            ...(result["external_ipv6_address_spec"] as Record<string, unknown> | undefined),
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
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
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
    singular: ENTITIES["route-tables"].singular,
    accusative: "таблицу маршрутов",
    plural: ENTITIES["route-tables"].plural,
    genitive: "Таблицы маршрутов",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Сеть",
        path: "network_id",
        render: (row) => <RefNameLink specId="networks" refId={row.network_id as string | undefined} />,
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
          if (routes.length === 0) return <span className="text-muted-foreground">—</span>;
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
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
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
      // Следующий узел — взаимоисключающая группа: адрес ЛИБО шлюз. Выразимы
      // обе (#375). Здесь стояло, что сервер ветвь шлюза не поддерживает; это
      // было неверно и работало как причина не делать — разбор в шапке
      // `RoutesEditor.tsx`.
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
          // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion -- ложное срабатывание: getByPath<T> выводит T из самого утверждения типа. Без него T = unknown, и RoutesEditor его не принимает (проверено tsc: удаление даёт TS2740).
          const routes = (getByPath(value, "static_routes") as RouteEntry[] | undefined) ?? [];
          return <RoutesEditor value={routes} onChange={(next) => onChange(setByPath(value, "static_routes", next))} />;
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
    // Выкидываем НЕДОЗАПОЛНЕННЫЕ строки маршрутов перед отправкой.
    //
    // Полнота считается по ВЫБРАННОЙ ветви: прежде она считалась по адресу, и
    // маршрут на шлюз выбрасывался как пустой — то есть форма приняла бы ввод,
    // ничего не сказала и отправила бы таблицу без него.
    sanitize: (obj) => {
      const routes = Array.isArray(obj.static_routes)
        ? (obj.static_routes as RouteEntry[]).filter((r) => {
            if ((r?.destination_prefix ?? "").trim() === "") return false;
            return r?.gateway_id !== undefined
              ? r.gateway_id.trim() !== ""
              : (r?.next_hop_address ?? "").trim() !== "";
          })
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
  //
  // Вкладки «JSON (internal)» у NIC нет и быть не может: у
  // InternalNetworkInterfaceService НЕТ ни одной google.api.http-аннотации — его
  // proto прямо это объявляет («Pure gRPC service→service»). Стоявший здесь
  // internalGetPath адресовал маршрут, которого не существует ни в какой форме,
  // — снят, а не переписан.

  "network-interfaces": {
    id: "network-interfaces",
    route: "network-interfaces",
    apiPath: "/vpc/v1/networkInterfaces",
    payloadKey: "network_interfaces",
    singular: ENTITIES["network-interfaces"].singular,
    accusative: "сетевой интерфейс",
    plural: ENTITIES["network-interfaces"].plural,
    genitive: "Сетевого интерфейса",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Подсеть",
        path: "subnet_id",
        render: (row) => <RefNameLink specId="subnets" refId={row.subnet_id as string | undefined} />,
      },
      {
        // mac_address — output-only, аллоцируется kacho-vpc при Create
        // (префикс 0e: + 40 бит crypto/rand), стабилен на жизни NIC,
        // уникален в пределах cloud (KAC-48). Клиент не может задать.
        header: "MAC",
        path: "mac_address",
        render: (row) => {
          const mac = row.mac_address as string | undefined;
          return mac ? <CopyableId id={mac} /> : <span className="text-muted-foreground">—</span>;
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
          const ub = row.used_by as { referrer?: { type?: string; id?: string } } | undefined;
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
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
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
        description: "Subnet, в которой создаётся интерфейс. Менять нельзя после создания.",
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
        description: "Опционально. IPv4 Address-ресурс из выбранной подсети. Можно создать новый прямо в дропдауне.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "IP-адрес",
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
        description: "Опционально. IPv6 Address-ресурс из выбранной подсети. Можно создать новый прямо в дропдауне.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "IP-адрес",
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
            label: "Группа безопасности",
            type: "ref",
            refResource: "security-groups",
            refProjectScoped: true,
            required: true,
            createResource: "security-groups",
            createTitle: "Создать группу безопасности",
          },
        ],
      },
      {
        // Ограничение принимается НЕ НА КАЖДОМ СТЕНДЕ: полосу выдерживает
        // исполнитель датаплейна, и умение ограничивать её объявляет посадка
        // (`dataplane.executor.tenant-settable-bandwidth-limit`). Признак на
        // публичной поверхности не виден, поэтому консоль поле предлагает, а край
        // отвечает отказом с именем поля там, где умения нет. Описание об этом
        // говорит прямо: пользователь должен узнать причину из формы, а не из
        // отказа.
        name: "bandwidth_limit_mbps",
        label: "Ограничение полосы, Мбит/с",
        type: "int",
        required: false,
        min: 0,
        description:
          "Опционально. Верхняя граница полосы интерфейса; 0 — без ограничения. Принимается только выше гарантированной полосы интерфейса и не выше того, что гарантирует стенд; на стендах, где исполнитель этого не умеет, край отвергает величину.",
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
      bandwidth_limit_mbps: 0,
      description: "",
      labels: {},
    }),
    // Конвертирует [{value: "..."}, ...] → ["...", ...] для wire format
    // (как subnets.v4_cidr_blocks / instance NIC security_group_ids).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of ["v4_address_ids", "v6_address_ids", "security_group_ids"]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw
            .map((item: unknown) =>
              typeof item === "object" && item !== null && "value" in item
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
      hydrateStringListFields(out, ["v4_address_ids", "v6_address_ids", "security_group_ids"]);
      return out;
    },
  },

  // proto: GET /vpc/v1/securityGroups (camelCase в URL)

  "security-groups": {
    id: "security-groups",
    route: "security-groups",
    apiPath: "/vpc/v1/securityGroups",
    payloadKey: "security_groups",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
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
    singular: ENTITIES["security-groups"].singular,
    accusative: "группу безопасности",
    plural: ENTITIES["security-groups"].plural,
    genitive: "Группы безопасности",
    serviceTitle: SERVICES.vpc.title,
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
          return nid ? <RefNameLink specId="networks" refId={nid} /> : <span className="text-muted-foreground">—</span>;
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
        // Create-only: UpdateSecurityGroupRequest не несёт network_id.
        immutable: true,
        label: "Облачная сеть",
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
        // `rule_specs` — имя И в Create, И в Update (тег 6). Поля `rules` у этих
        // сообщений нет вовсе: правила, набранные в форме создания, край
        // выбрасывал молча, и группа создавалась пустой (default-deny) с 200.
        name: "rule_specs",
        label: "Правила",
        type: "sg-rules",
        // Перечень адресатов обязан совпадать с `oneof target` контракта: их три
        // — блоки адресов, другая группа, набор префиксов. До #512 здесь стоял
        // «предустановленный набор» (поле снято с контракта, номер и имя
        // зарезервированы), а живой «набор префиксов» не назывался вовсе. То
        // есть подпись предлагала выбор, которого нет, и умалчивала о том,
        // который есть, — и заметить это можно было только попыткой.
        description:
          "Направление, протокол с портами и адресат: диапазон адресов, другая группа безопасности или набор префиксов. Без правил трафик запрещён.",
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
      rule_specs: [],
    }),
    // Чистит UI-дискриминаторы (_protocol_mode/_ports_any/_target_kind) и
    // неактивные ветки oneof перед PATCH/POST. См. SgRulesEditor.
    // network_id обязателен (KAC-243); пустой выбрасываем только чтобы не слать
    // "" — backend всё равно отвергнет Create без сети (INVALID_ARGUMENT).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      if (!out["network_id"]) delete out["network_id"];
      const raw = out["rule_specs"];
      if (Array.isArray(raw)) {
        out["rule_specs"] = raw.map((r) => sanitizeSgRule(r as Record<string, unknown>));
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
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    singular: ENTITIES.gateways.singular,
    accusative: "шлюз",
    plural: ENTITIES.gateways.plural,
    genitive: "Шлюза",
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_VPC,
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      // Вид шлюза — ВЕТВЬ oneof, а не значение поля, поэтому в форме он enum, а на
      // провод уходит ключом ветви (см. sanitize ниже). Подставлять вид молча
      // нельзя: он решает, какое семейство назначения вправе идти через шлюз.
      {
        name: "_kind",
        label: "Вид шлюза",
        type: "enum",
        immutable: true,
        default: "nat",
        options: [
          { value: "nat", label: "Публичная трансляция исходящего IPv4 (NAT)" },
          { value: "egress_only", label: "Только исход, IPv6 — входящие соединения не устанавливаются" },
        ],
        description:
          "Выбирается при создании и неизменяем: смена вида — другой шлюз. Подсеть привязки обязана нести CIDR-блок того же семейства.",
      },
      {
        name: "subnet_id",
        label: "Подсеть",
        type: "ref",
        refResource: "subnets",
        refProjectScoped: true,
        required: true,
        immutable: true,
        description:
          "Привязка шлюза и его якорь размещения: своей зоны шлюз не несёт, он наследует размещение подсети.",
      },
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      subnet_id: "",
      _kind: "nat",
    }),
    // Ветвь oneof не выражается значением поля: у неё нет значения, у неё есть имя.
    // Поэтому выбранный вид уходит на провод КЛЮЧОМ ветви, а служебное поле формы
    // снимается — иначе край получил бы лишний ключ, который молча выбрасывает, и
    // шлюз создавался бы без вида.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const kind = (out._kind as string) || "nat";
      delete out._kind;
      if (kind === "egress_only") out.egress_only_gateway_spec = {};
      else out.nat_gateway_spec = {};
      return out;
    },
  },

  // proto: GET /vpc/v1/cidrGroups (kacho.cloud.vpc.v1.CidrGroupService).
  //
  // Набор префиксов — ИМЕНОВАННЫЙ список CIDR, на который правило группы
  // безопасности ссылается вместо собственной копии списка. Ради этого он и
  // заведён: диапазоны, встречающиеся в двадцати правилах, правятся в одном
  // месте, а не в двадцати (и, рано или поздно, не во всех двадцати).
  //
  // Состав НЕ меняется правкой: `UpdateCidrGroupRequest` полей состава не несёт
  // вовсе — только `:add-cidr-blocks` / `:remove-cidr-blocks`. Поэтому оба поля
  // объявлены `editHidden`, а на карточке их правит та же секция набора блоков,
  // что у подсети и у сети.
  "cidr-groups": {
    id: "cidr-groups",
    route: "cidr-groups",
    apiPath: "/vpc/v1/cidrGroups",
    payloadKey: "cidr_groups",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    singular: "Набор префиксов",
    plural: "Наборы префиксов",
    genitive: "Набора префиксов",
    accusative: "набор префиксов",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    // Мутации отвечают Operation (ban #9): ответ без operation-id — нарушение
    // контракта, а не синхронный успех.
    mutationsReturnOperation: true,
    docs: [
      { label: "Наборы префиксов", href: "#" },
      { label: "Правила групп безопасности", href: "#" },
    ],
    emptyState: {
      title: "Создайте ваш первый набор префиксов",
      body:
        "Набор префиксов — именованный список CIDR, на который ссылаются правила групп безопасности. " +
        "Список правится один раз, и каждое правило, которое на него ссылается, следует за ним.",
      docs: ["Наборы префиксов"],
    },
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
      {
        header: "IPv4",
        path: "v4_cidr_blocks",
        render: (row) => <CidrListCell items={[row.v4_cidr_blocks]} />,
      },
      {
        header: "IPv6",
        path: "v6_cidr_blocks",
        render: (row) => <CidrListCell items={[row.v6_cidr_blocks]} />,
      },
      { header: "Членов", path: "cidr_block_count", format: "text" },
      {
        // `used_by` — output-only kacho.cloud.reference.Reference: группы правил,
        // чьи правила ссылаются на этот набор. Сервер его ЗАПОЛНЯЕТ (выводит на
        // чтении из проекции ссылок), поэтому поле показывается — правило «поле
        // без источника не показывается» здесь выполнено.
        header: "Кем используется",
        path: "used_by",
        format: "references",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_VPC,
      FIELD_LABELS,
      FIELD_DESCRIPTION,
      {
        name: "v4_cidr_blocks",
        label: "IPv4-префиксы",
        type: "array",
        itemLabel: "CIDR",
        description:
          "Начальный состав набора (IPv4). Меняется не правкой, а действиями на странице набора — до 64 членов на семейство.",
        editHidden: true,
        newItem: () => ({ value: "" }),
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true, placeholder: "10.20.0.0/16" }],
      },
      {
        name: "v6_cidr_blocks",
        label: "IPv6-префиксы",
        type: "array",
        itemLabel: "CIDR",
        description: "Начальный состав набора (IPv6). Меняется действиями на странице набора.",
        editHidden: true,
        newItem: () => ({ value: "" }),
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true, placeholder: "fd00:20::/48" }],
      },
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      labels: {},
      v4_cidr_blocks: [],
      v6_cidr_blocks: [],
    }),
    // {value:"…"} формы ↔ string[] провода. Пустая строка не уезжает: край
    // получил бы члена, которого оператор не вводил, и отверг бы весь запрос.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      for (const key of ["v4_cidr_blocks", "v6_cidr_blocks"]) {
        const raw = out[key];
        if (Array.isArray(raw)) {
          out[key] = raw
            .map((item: unknown) =>
              typeof item === "object" && item !== null && "value" in item
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
      hydrateStringListFields(out, ["v4_cidr_blocks", "v6_cidr_blocks"]);
      return out;
    },
  },

  // ====== compute (Instance) ======
  // proto: GET /compute/v1/instances. Name-regex lowercase-only
  // (kacho-compute/CLAUDE.md §5: `^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$`).

  // disk-types — read-only справочник kacho-storage (владелец блочного хранения),
  // используется как refResource в dropdown'ах.
  "disk-types": {
    id: "disk-types",
    route: "disk-types",
    apiPath: "/storage/v1/diskTypes",
    // То же послабление и по той же причине: административный CRUD типа диска
    // синхронен (`rpc Create/Update/SetLifecycle` → `DiskType`, `rpc Delete` →
    // `DeleteDiskTypeResponse`). Второй и последний такой путь в дереве.
    mutationsReturnOperation: false,
    payloadKey: "disk_types",
    singular: ENTITIES["disk-types"].singular,
    accusative: "тип диска",
    plural: ENTITIES["disk-types"].plural,
    serviceTitle: SERVICES.compute.title,
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

  // compute-zones / compute-regions — read-only проекции ТОГО ЖЕ каталога
  // размещения, что и записи `zones` / `regions`: тот же публичный путь geo, те
  // же строки. Отдельные id живут потому, что на них ссылаются подборщики
  // (`Instance.zone_id`, региональные поля балансировщика) в этом и в соседних
  // приложениях; admin-CRUD — только у записей `zones` / `regions`.
  //
  // Владелец — kacho-geo, и всегда был им: здесь стояло «kacho-compute — owner
  // Geography, Region/Zone перенесены из vpc», чего не было ни в одной ревизии
  // (в proto compute нет ни одного сообщения Region/Zone).
  //
  // Колонки — публичная проекция: сырой admin-`status` у Zone ЗАРЕЗЕРВИРОВАН
  // (`reserved 3; reserved "status"`), у Region его не было никогда, и колонка,
  // привязанная к нему, рисовала пустую ячейку вечно. Доступность размещения
  // несёт производный openForPlacement° — как в записях `zones` / `regions`.
  "compute-zones": {
    id: "compute-zones",
    route: "compute-zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    accusative: "зону",
    plural: `Зоны (${SERVICES.compute.title})`,
    serviceTitle: SERVICES.compute.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
      {
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      {
        header: "Размещение",
        path: "open_for_placement",
        render: (row) => (
          <PlacementBadge
            open={row.open_for_placement as boolean | undefined}
            reason={row.placement_blocked_reason as PlacementBlockedReason | undefined}
          />
        ),
      },
    ],
    template: () => ({}),
  },

  "compute-regions": {
    id: "compute-regions",
    route: "compute-regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    accusative: "регион",
    plural: `Регионы (${SERVICES.compute.title})`,
    serviceTitle: SERVICES.compute.title,
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
      {
        header: "Размещение",
        path: "open_for_placement",
        render: (row) => <PlacementBadge open={row.open_for_placement as boolean | undefined} />,
      },
    ],
    template: () => ({}),
  },

  "compute-instances": {
    id: "compute-instances",
    route: "instances",
    apiPath: "/compute/v1/instances",
    payloadKey: "instances",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    singular: "Виртуальная машина",
    accusative: "виртуальную машину",
    plural: "Виртуальные машины",
    genitive: "Виртуальной машины",
    serviceTitle: SERVICES.compute.title,
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
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Тип", path: "instance_kind", format: "code" },
      {
        header: "Зона",
        path: "zone_id",
        render: (row) => <RefNameLink specId="zones" refId={row.zone_id as string | undefined} maxChars={28} />,
      },
      { header: "Тип машины", path: "machine_type_id", format: "code" },
      {
        header: "vCPU / RAM",
        path: "effective_resources",
        render: (row) => {
          const r = row.effective_resources as { v_cpu?: string | number; memory_mib?: string | number } | undefined;
          if (!r) return <span className="text-muted-foreground">—</span>;
          return (
            <span className="font-mono text-xs">
              {r.v_cpu ?? "?"} vCPU · {fmtMiBGiB(r.memory_mib)}
            </span>
          );
        },
      },
      {
        header: "Внутренний IP",
        path: "network_interfaces",
        render: (row) => {
          const nics =
            (row.network_interfaces as Array<{ primary_v4_address?: { address?: string } }> | undefined) ?? [];
          const ip = nics[0]?.primary_v4_address?.address;
          return ip ? (
            <span className="font-mono text-xs">{ip}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        // Источник ОС — Instance.bootSource{type,id}. Заменяет прежнюю колонку
        // «Загрузочный диск» (Instance.bootDisk — легаси-проекция, ведшая на
        // ретайренный дубль compute-дисков).
        header: "Образ",
        path: "boot_source.id",
        render: (row) => {
          const bs = row.boot_source as { id?: string; name?: string } | undefined;
          const label = bs?.name || bs?.id;
          return label ? (
            <span className="font-mono text-xs">{label}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "zone_id",
        label: "Зона доступности",
        type: "ref",
        refResource: "compute-zones",
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
          "Вид машины; неизменяем после создания. Виртуальная машина запускает операционную " +
          "систему из образа диска, контейнер — из образа реестра.",
      },
      {
        name: "machine_type_id",
        label: "Тип машины",
        type: "ref",
        refResource: "machine-types",
        required: true,
        description:
          "Единый канал размера инстанса (vCPU/память/GPU) — каталог MachineType. Сменить размер можно на " +
          "остановленном (STOPPED) инстансе.",
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
          "Владелец образа: storage.image (диск-образ kacho-storage, для VM) или registry.image " +
          "(OCI-артефакт kacho-registry, для CONTAINER).",
      },
      {
        // Образ — списком, а не строкой.
        //
        // Сервер принимает здесь РОВНО `img-<base32>`: `validateBootSource`
        // (services/compute) зовёт `corevalidate.ResourceID("Image", "img", …)`.
        // Прежняя подсказка предлагала форму с тегом и OCI-ссылку, и обе сервер
        // отвергает: у образа хранилища нет ни поля тега, ни поля дайджеста, а
        // ветка registry.image отвергается целиком. То есть форма предлагала
        // набрать руками идентификатор, который она же могла показать списком.
        name: "boot_source.id",
        label: "Образ",
        type: "ref",
        refResource: "images",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        visibleWhen: { field: "boot_source.type", equals: "storage.image" },
        description:
          "Образ ОС, из которого материализуется загрузочный том машины. Список — образы текущего проекта; " +
          "нет ни одного — создайте образ в разделе Storage.",
      },
      {
        name: "cpu_guarantee_percent",
        label: "Гарантия CPU, %",
        type: "int",
        min: 0,
        max: 100,
        default: 0,
        description:
          "Гарантированный baseline CPU на vCPU в процентах (0 — best-effort/burstable; 1..100 — гарантия). " +
          "Меняется на STOPPED.",
      },
      {
        name: "service_account_id",
        label: "Сервисный аккаунт",
        type: "string",
        placeholder: "sva… (опционально)",
        description: "Сервисный аккаунт (iam), доступный внутри инстанса. Для публичных образов можно не задавать.",
      },
      {
        // Ключи входа — ССЫЛКАМИ на ресурс, а не материалом в теле запроса.
        // Контракт объясняет это прямым текстом: ключ, переданный полем, живёт
        // ровно столько, сколько машина, и его нельзя ни отозвать, ни заменить,
        // ни узнать, где ещё он используется. Отсюда же и отказ сервера на
        // `sshPublicKeys` — он называет заменой именно это поле.
        name: "guest_access_key_ids",
        label: "Ключи доступа",
        type: "array",
        itemLabel: "ключ",
        createOnly: true,
        maxItems: 32,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        description:
          "Публичные ключи, с которыми вы войдёте в гостевую систему. Ключ — отдельный ресурс проекта: его " +
          "можно отозвать, заменить и увидеть, где ещё он используется. Нет ни одного — создайте прямо здесь.",
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
      // --- VM-only (instance_kind = VM) ---
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
        label: "Адрес службы метаданных",
        type: "enum",
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        default: "ENABLED",
        options: [
          { value: "ENABLED", label: "ENABLED — доступен из гостя" },
          { value: "DISABLED", label: "DISABLED — недоступен" },
        ],
        description: "Доступность службы метаданных из гостевой ОС.",
      },
      {
        name: "assign_external_address",
        label: "Внешний адрес",
        type: "bool",
        createOnly: true,
        default: false,
        visibleWhen: { field: "instance_kind", equals: "VM" },
        description: "Запросить внешний IP-адрес для VM.",
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
      // --- CONTAINER-only (instance_kind = CONTAINER) ---
      {
        name: "container_spec.restart_policy",
        label: "Политика перезапуска",
        type: "enum",
        createOnly: true,
        visibleWhen: { field: "instance_kind", equals: "CONTAINER" },
        default: "NEVER",
        options: [
          { value: "NEVER", label: "NEVER — не перезапускать" },
          { value: "ON_FAILURE", label: "ON_FAILURE — при ненулевом коде возврата" },
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
      // --- network ---
      // Create требует ЛИБО network_interface_specs, ЛИБО use_default_network —
      // ровно одно. Тумблер даёт вторую ветку: без него форма, в которой
      // интерфейс не сконфигурирован, получала отказ сервера и не имела способа
      // его удовлетворить.
      {
        name: "use_default_network",
        label: "Сеть по умолчанию",
        type: "bool",
        createOnly: true,
        default: true,
        description:
          "Использовать подсеть и группу безопасности проекта по умолчанию. Снимите, чтобы настроить интерфейсы вручную.",
      },
      {
        name: "network_interface_specs",
        label: "Сетевые интерфейсы",
        type: "array",
        itemLabel: "интерфейс",
        visibleWhen: { field: "use_default_network", equals: "false" },
        description:
          "Минимум один сетевой интерфейс. Выберите сеть → подсеть → внутренний адрес (Cascader) и режим " +
          "публичного IP (Segmented); либо переключитесь на «существующий NetworkInterface» (тогда подсеть/SG/" +
          "адрес берутся из него). Подсеть должна быть в той же зоне, что и ВМ.",
        editHidden: true,
        // Дефолт NIC-айтема: пустой spec, external-IP = «без адреса».
        // `_*`-поля — служебные UI-state (cascader path / external mode); их
        // вычищает sanitizeInstanceCreate, а транспортный buildCreateBody —
        // повторно и рекурсивно, включая вложенные в айтемы.
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
            render: (p) => <NicSpecFields pathPrefix={p.pathPrefix} value={p.value} onChange={p.onChange} />,
          },
          // Группы безопасности — generic ArrayField с inline-create «+ SG».
          {
            name: "security_group_ids",
            label: "Группы безопасности",
            type: "array",
            itemLabel: "SG",
            description: "Опционально. Применяются к интерфейсу. Можно создать новую прямо в дропдауне.",
            newItem: () => ({ value: "" }),
            itemFields: [
              {
                name: "value",
                label: "Группа безопасности",
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
        label: "Имя хоста",
        type: "string",
        placeholder: "(= id если пусто)",
        pattern: "^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$",
        editHidden: true,
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
      network_interface_specs: [],
      guest_access_key_ids: [],
      labels: {},
    }),
    sanitize: (obj) => sanitizeInstanceCreate(obj),
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
        return (
          "Источник registry.image пока не принимается: у образа из реестра нет неизменяемого адреса, " +
          "поэтому ссылка в машине сломалась бы после чужого переименования. Выберите storage.image."
        );
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
  },

  // ====== storage: Volume (read-only ref target) ======
  // proto: kacho.cloud.storage.v1.VolumeService (/storage/v1/volumes). Storage owns
  // block storage; the instance attach/detach verbs are specified in terms of a
  // Volume id ("vol"), so this is the picker they read. CRUD lives in the storage
  // remote — here it is a ref target only.
  volumes: {
    id: "volumes",
    route: "volumes",
    apiPath: "/storage/v1/volumes",
    payloadKey: "volumes",
    singular: ENTITIES.volumes.singular,
    accusative: "том",
    plural: ENTITIES.volumes.plural,
    genitive: "Тома",
    serviceTitle: SERVICES.storage.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
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
      { header: "Статус", path: "status", format: "status" },
      {
        header: "Зона",
        path: "zone_id",
        render: (row) => <RefNameLink specId="zones" refId={row.zone_id as string | undefined} maxChars={28} />,
      },
    ],
    template: () => ({}),
  },

  // ====== storage: Image (ref target) ======
  // proto: kacho.cloud.storage.v1.ImageService (/storage/v1/images). Образ — вход
  // в цепочку «образ → том → машина»: без него из пустого проекта загрузочный том
  // не получить вовсе (том делается пустым или из снимка, снимок — из тома).
  // Здесь ТОЛЬКО цель ссылки для подборщика образа в форме машины; CRUD живёт в
  // разделе Storage.
  images: {
    id: "images",
    route: "images",
    apiPath: "/storage/v1/images",
    payloadKey: "images",
    singular: "Образ",
    plural: "Образы",
    genitive: "Образа",
    accusative: "образ",
    serviceTitle: "Storage",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      { header: "Идентификатор", path: "id", render: (row) => <CopyableId id={(row.id as string) ?? ""} /> },
      { header: "Статус", path: "status", format: "status" },
    ],
    template: () => ({}),
  },

  // ====== compute: GuestAccessKey ======
  // proto: kacho.cloud.compute.v1.GuestAccessKeyService (/compute/v1/guestAccessKeys).
  // Мутации async → Operation. Mutable: name/labels; публичный ключ задаётся при
  // создании и не правится — заменить ключ значит завести другой.
  //
  // Почему это ресурс, а не поле машины, сказано в самом контракте: ключ,
  // переданный полем, живёт ровно столько, сколько машина, и его нельзя ни
  // отозвать, ни заменить, ни узнать, где ещё он используется. Отсюда же и отказ
  // сервера на `sshPublicKeys` в запросе машины — он называет этот ресурс заменой.
  //
  // Поля формы — из общего объявления, см. `@shared/lib/guest-access-key-form`:
  // ресурс живёт и во втором реестре, и выписанные дважды поля разошлись бы молча.
  "guest-access-keys": {
    id: "guest-access-keys",
    route: "guest-access-keys",
    apiPath: "/compute/v1/guestAccessKeys",
    payloadKey: "guest_access_keys",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    singular: "Ключ доступа",
    plural: "Ключи доступа",
    genitive: "Ключа доступа",
    accusative: "ключ доступа",
    serviceTitle: SERVICES.compute.menuTitle,
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
    fields: GUEST_ACCESS_KEY_FIELDS,
    template: guestAccessKeyTemplate,
    emptyState: GUEST_ACCESS_KEY_EMPTY_STATE,
  },

  // ====== compute: MachineType (read-only sizing catalog) ======
  // proto: kacho.cloud.compute.v1.MachineTypeService (/compute/v1/machineTypes).
  // Public read-only; admin-CRUD — InternalMachineTypeService (:9091, ban #6).
  // Cluster-scoped; ref-цель для Instance.machine_type_id.
  "machine-types": {
    id: "machine-types",
    route: "machine-types",
    apiPath: "/compute/v1/machineTypes",
    payloadKey: "machine_types",
    singular: ENTITIES["machine-types"].singular,
    accusative: "тип машины",
    plural: ENTITIES["machine-types"].plural,
    genitive: "Типа машины",
    serviceTitle: SERVICES.compute.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
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
      { header: "Семейство", path: "family", format: "code" },
      { header: "vCPU", path: "effective_resources.v_cpu", format: "text" },
      {
        header: "Память",
        path: "effective_resources.memory_mib",
        render: (row) => (
          <span className="font-mono text-xs">
            {fmtMiBGiB((row.effective_resources as Record<string, unknown> | undefined)?.memory_mib)}
          </span>
        ),
      },
      { header: "GPU", path: "effective_resources.gpus", format: "text" },
      { header: "Зоны", path: "available_zones", format: "list" },
      { header: "Статус", path: "status", format: "status" },
    ],
    template: () => ({}),
  },

  // proto: GET /compute/v1/placementGroups (kacho.cloud.compute.v1.PlacementGroupService).
  //
  // Группа размещения — правило ВЗАИМНОГО размещения машин: разнести (чтобы отказ
  // одного куска железа не унёс всю группу) либо сблизить (чтобы машины видели
  // друг друга коротким путём). Ровно два намерения, и оба выражаются без единого
  // числа: «разнести на N доменов отказа» описывало бы НАШУ раскладку железа, а не
  // намерение арендатора, — опубликовав число, мы обязались бы держать раскладку.
  //
  // Якорь размещения ВЗАИМОИСКЛЮЧАЮЩИЙ: группа либо зональная, либо региональная.
  // Это дискриминатор, а не пара необязательных полей: строка с обоими описывает
  // размещение, которого не бывает, и сервис отвергает её словами «a group is
  // anchored by exactly one coordinate». Поэтому форма показывает ровно одну
  // координату, а sanitize снимает вторую — иначе оператор заполнил бы обе и
  // получил отказ на поле, которого он не выбирал.
  //
  // Спека объявлена ЗДЕСЬ, а реестр compute на неё ССЫЛАЕТСЯ (второй копии нет):
  // раздел монтируют оба приложения — compute-remote своим маршрутом и vpc в
  // standalone-сборке.
  "placement-groups": {
    id: "placement-groups",
    route: "placement-groups",
    apiPath: "/compute/v1/placementGroups",
    payloadKey: "placement_groups",
    // Поиск по имени спрашивает сервер (#373): владелец держит `name` в белом
    // списке выражения И применяет разобранный узел через `ToSQL`, то есть
    // сохраняет ОПЕРАТОР. Сходимость с деревом владельца по обоим условиям —
    // `lib/list-server-search-parity.test.ts`.
    serverSearchField: "name",
    singular: "Группа размещения",
    plural: "Группы размещения",
    genitive: "Группы размещения",
    accusative: "группу размещения",
    serviceTitle: SERVICES.compute.menuTitle,
    scope: "project",
    ops: { create: true, update: true, delete: true },
    mutationsReturnOperation: true,
    docs: [{ label: "Группы размещения", href: "#" }],
    emptyState: {
      title: "Создайте вашу первую группу размещения",
      body:
        "Группа размещения — правило взаимного размещения машин: разнести их по разным доменам отказа " +
        "или, наоборот, сблизить. Машина входит в группу при создании.",
      docs: ["Группы размещения"],
    },
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
      {
        header: "Правило",
        path: "strategy",
        render: (row) => <>{PLACEMENT_STRATEGY_TEXT[displayText(row.strategy)] ?? placementDash}</>,
      },
      {
        // Якорь — ресурс каталога geo, поэтому ссылка, а не идентификатор. Ветку
        // ZONAL/REGIONAL рисует единственный `PlacementAnchor`.
        header: "Размещение",
        path: "placement_type",
        render: (row) => <PlacementAnchor row={row} maxChars={28} />,
      },
      {
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      FIELD_LABELS,
      {
        name: "strategy",
        label: "Правило размещения",
        type: "enum",
        required: true,
        immutable: true,
        default: "SPREAD",
        options: [
          { value: "SPREAD", label: "Разнести — отказ одного куска железа не унесёт всю группу" },
          { value: "PACK", label: "Сблизить — машины видят друг друга коротким путём" },
        ],
        description: "Выбирается при создании и неизменяемо: смена правила — другая группа.",
      },
      {
        name: "placement_type",
        label: "Якорь размещения",
        type: "enum",
        required: true,
        immutable: true,
        default: "ZONAL",
        options: [
          { value: "ZONAL", label: "Зона — машины в одной зоне" },
          { value: "REGIONAL", label: "Регион — машины в одном регионе, зоны разные" },
        ],
        description: "Ровно одна координата: зональная группа региона не несёт, региональная — зоны.",
      },
      {
        name: "zone_id",
        label: "Зона",
        type: "ref",
        refResource: "zones",
        required: true,
        immutable: true,
        visibleWhen: { field: "placement_type", equals: "ZONAL" },
        placeholder: "Выберите зону",
      },
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        visibleWhen: { field: "placement_type", equals: "REGIONAL" },
        placeholder: "Выберите регион",
      },
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      labels: {},
      strategy: "SPREAD",
      placement_type: "ZONAL",
      zone_id: "",
      region_id: "",
    }),
    // Неактивная координата снимается ПЕРЕД отправкой: сервис отвергает запрос,
    // в котором заполнены обе, и отвергает по полю, которое оператор не выбирал.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      if (out.placement_type === "REGIONAL") delete out.zone_id;
      else delete out.region_id;
      return out;
    },
  },

  // ====== Geography (kacho-geo): Region / Zone ====== + System AddressPool ======
  //
  // Region/Zone — ось размещения платформы, владелец kacho-geo. Две поверхности,
  // и это НЕ одна и та же:
  //   read  = geo.v1.{Region,Zone}Service — GET /geo/v1/{regions,zones}[/{id}].
  //           project-scope EXEMPT: каталог обязан читать любой аутентифицированный
  //           тенант, иначе ему нечего подставить в zoneId/regionId при запуске
  //           чего угодно размещаемого. authN при этом обязателен.
  //   admin = geo.v1.Internal{Region,Zone}Service — POST/PATCH/DELETE/GET на
  //           /geo/v1/internal/{regions,zones}. system_admin на кластере,
  //           исключительно через cluster-internal REST listener. Мутация по
  //           ПУБЛИЧНОМУ пути не смаршрутизирована вообще (до редизайна реестр
  //           слал Create именно туда — молча в никуда).
  //
  // Две проекции: публичные Region/Zone несут только tenant-facing намерение плюс
  // производный openForPlacement°; сырой admin-`status` и весь блок infra° живут
  // на InternalRegion/InternalZone. Поэтому форма редактирования читается с
  // internal-пути (`admin.readForEdit`) — по публичному чтению оператор увидел бы
  // пустой статус и переключал бы его вслепую.
  //
  // Мутации каталога завершаются синхронно, но всё равно отвечают Operation
  // (`done=true` сразу) — отсюда `mutationsReturnOperation`.
  //
  // AddressPool — kacho-vpc (`/vpc/v1/addressPools`), admin-RPC которого честно
  // отвечает самим ресурсом; флага Operation у него нет.

  regions: {
    id: "regions",
    route: "regions",
    apiPath: GEO_REGIONS_PATH,
    admin: { basePath: GEO_INTERNAL_REGIONS_PATH, readForEdit: true },
    mutationsReturnOperation: true,
    internalGetPath: `${GEO_INTERNAL_REGIONS_PATH}/{id}`,
    payloadKey: "regions",
    singular: ENTITIES.regions.singular,
    accusative: "регион",
    plural: ENTITIES.regions.plural,
    genitive: "Региона",
    description:
      "Региональная координата размещения. Регионы заводит администратор кластера; тенанты читают каталог, чтобы выбрать, где разместить ресурс.",
    serviceTitle: SERVICES.geo.title,
    scope: "global",
    ops: { create: true, update: true, delete: true },
    listFilters: [
      {
        kind: "toggle",
        param: "open_for_placement",
        label: "Только открытые для размещения",
        description:
          "Серверный фильтр по производному openForPlacement° — единственный фильтр размещения на публичной поверхности.",
      },
    ],
    columns: [
      { header: "Имя", path: "name", format: "text", className: "font-medium" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      { header: "Страна", path: "country_code", format: "text" },
      {
        header: "Размещение",
        path: "open_for_placement",
        render: (row) => <PlacementBadge open={row.open_for_placement as boolean | undefined} />,
      },
      {
        header: "Открытых зон",
        path: "open_zone_count_hint",
        render: (row) => <CountHintCell value={row.open_zone_count_hint} />,
      },
      COL_CREATED,
    ],
    fields: [
      {
        name: "id",
        label: "Идентификатор региона",
        type: "string",
        required: true,
        immutable: true,
        placeholder: "region-1",
        description:
          "Назначается администратором и неизменяем: он попадает в каждый размещаемый ресурс как координата. Строчные буквы и цифры, сегменты через дефис.",
        pattern: REGION_ZONE_ID_PATTERN,
      },
      {
        name: "name",
        label: "Название",
        type: "string",
        required: true,
        placeholder: "Центральная Россия",
        description: "Человекочитаемая подпись. Глобально уникальна, менять можно свободно.",
      },
      {
        name: "country_code",
        label: "Код страны",
        type: "string",
        placeholder: "RU",
        description: "ISO-3166 alpha-2, две заглавные буквы. Можно оставить пустым.",
        pattern: "^([A-Z]{2})?$",
      },
      {
        name: "status",
        label: "Обслуживание",
        type: "enum",
        options: GEO_STATUS_OPTIONS,
        description:
          "Сырой admin-флаг. DOWN — регион закрыт для размещения (и все его зоны вместе с ним). Виден только в admin-плоскости.",
      },
      {
        name: "infra.numeric_infra_id",
        label: "Числовой инфра-идентификатор",
        type: "string",
        immutable: true,
        placeholder: "1",
        description:
          "Инфраструктурный идентификатор региона. Задаётся один раз при создании; на публичную поверхность не выходит.",
        pattern: "^[0-9]*$",
      },
    ],
    // Свежий регион поднимается закрытым — тот же fail-safe, что и на сервере.
    template: () => ({ id: "", name: "", country_code: "", status: "DOWN", infra: { numeric_infra_id: "" } }),
    sanitize: (obj) => sanitizeGeoCommon(obj),
    hydrate: (obj) => obj,
    emptyState: {
      title: "Каталог регионов пуст",
      body: "Регион — верхний уровень оси размещения. Пока в каталоге нет ни одного региона, разместить нельзя ничего: zoneId и regionId берутся отсюда.",
    },
  },

  zones: {
    id: "zones",
    route: "zones",
    apiPath: GEO_ZONES_PATH,
    admin: { basePath: GEO_INTERNAL_ZONES_PATH, readForEdit: true },
    mutationsReturnOperation: true,
    internalGetPath: `${GEO_INTERNAL_ZONES_PATH}/{id}`,
    payloadKey: "zones",
    singular: ENTITIES.zones.singular,
    accusative: "зону",
    plural: ENTITIES.zones.plural,
    genitive: "Зоны",
    description:
      "Зональная координата размещения внутри региона. Зона открыта, только когда открыты и она сама, и её регион.",
    serviceTitle: SERVICES.geo.title,
    scope: "global",
    ops: { create: true, update: true, delete: true },
    listFilters: [
      {
        kind: "ref",
        param: "region_id",
        label: "Регион",
        refSpecId: "regions",
        allLabel: "Все регионы",
      },
      {
        kind: "toggle",
        param: "open_for_placement",
        label: "Только открытые для размещения",
        description: "Серверный фильтр по производному openForPlacement° (зона UP и её регион UP).",
      },
    ],
    columns: [
      { header: "Имя", path: "name", format: "text", className: "font-medium" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      {
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      {
        header: "Размещение",
        path: "open_for_placement",
        render: (row) => (
          <PlacementBadge
            open={row.open_for_placement as boolean | undefined}
            reason={row.placement_blocked_reason as PlacementBlockedReason | undefined}
          />
        ),
      },
      {
        header: "Причина",
        path: "placement_blocked_reason",
        render: (row) => (
          <BlockedReasonCell
            open={row.open_for_placement as boolean | undefined}
            reason={row.placement_blocked_reason as PlacementBlockedReason | undefined}
          />
        ),
      },
      COL_CREATED,
    ],
    fields: [
      {
        name: "id",
        label: "Идентификатор зоны",
        type: "string",
        required: true,
        immutable: true,
        placeholder: "region-1-a",
        description:
          "Назначается администратором и неизменяем. Обязан начинаться с идентификатора своего региона и дефиса.",
        pattern: REGION_ZONE_ID_PATTERN,
      },
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        description:
          "Регион зоны. Неизменяем: перенос зоны между регионами разошёлся бы с размещением каждого уже созданного в ней ресурса.",
      },
      {
        name: "name",
        label: "Название",
        type: "string",
        required: true,
        placeholder: "Зона A",
        description: "Человекочитаемая подпись. Глобально уникальна, менять можно свободно.",
      },
      {
        name: "status",
        label: "Обслуживание",
        type: "enum",
        options: GEO_STATUS_OPTIONS,
        description:
          "Сырой admin-флаг зоны. Зона открыта для размещения, только когда UP и она, и её регион. Виден только в admin-плоскости.",
      },
      {
        name: "infra.numeric_infra_id",
        label: "Числовой инфра-идентификатор",
        type: "string",
        immutable: true,
        placeholder: "1",
        description: "Задаётся один раз при создании; на публичную поверхность не выходит.",
        pattern: "^[0-9]*$",
      },
      {
        name: "infra.host_classes",
        label: "Классы хостов",
        type: "text",
        rows: 3,
        placeholder: "std-1\ngpu-a100",
        description: "Инвентарь классов хостов зоны, по одному в строке. Никогда не показывается тенанту.",
      },
      {
        name: "infra.failure_domain_count",
        label: "Доменов отказа",
        type: "int",
        min: 0,
        description: "Сколько доменов отказа внутри зоны. Никогда не показывается тенанту.",
      },
      {
        name: "infra.underlay_anchor",
        label: "Якорь транспортной сети",
        type: "string",
        placeholder: "spine-1",
        description: "Транспортная координата зоны. Никогда не показывается тенанту.",
      },
      {
        name: "infra.capacity_hint",
        label: "Запас ёмкости",
        type: "enum",
        options: [
          { value: "", label: "Не задано" },
          { value: "AMPLE", label: "AMPLE — запас есть" },
          { value: "CONSTRAINED", label: "CONSTRAINED — ограничен" },
          { value: "FULL", label: "FULL — исчерпан" },
        ],
        description:
          "Сигнал планировщику. Публичная ошибка нехватки ёмкости обезличена и этого значения не раскрывает.",
      },
    ],
    // Свежая зона поднимается закрытой — тот же fail-safe, что и на сервере.
    template: () => ({
      id: "",
      region_id: "",
      name: "",
      status: "DOWN",
      infra: {
        numeric_infra_id: "",
        host_classes: "",
        failure_domain_count: 0,
        underlay_anchor: "",
        capacity_hint: "",
      },
    }),
    // Связка id ↔ regionId, которую сервер проверяет первой. Не выводим регион из
    // имени зоны — оба идентификатора оператор вводит сам, а решает всё равно geo.
    validate: (obj) => {
      const id = typeof obj.id === "string" ? obj.id : "";
      const regionId = typeof obj.region_id === "string" ? obj.region_id : "";
      if (!id || !regionId) return null;
      if (!zoneBelongsToRegion(id, regionId)) {
        return `Идентификатор зоны «${id}» должен начинаться с идентификатора региона «${regionId}» и дефиса.`;
      }
      return null;
    },
    sanitize: (obj) => {
      const out = sanitizeGeoCommon(obj);
      // regionId у Update зарезервирован — в теле он всё равно будет отброшен, но
      // не отправляем то, чего в запросе нет.
      const infra = out.infra as Record<string, unknown> | undefined;
      if (infra && "host_classes" in infra) {
        infra.host_classes = splitLines(infra.host_classes);
        if ((infra.host_classes as string[]).length === 0) delete infra.host_classes;
      }
      if (infra && infra.capacity_hint === "") delete infra.capacity_hint;
      if (infra && infra.underlay_anchor === "") delete infra.underlay_anchor;
      if (infra && Object.keys(infra).length === 0) delete out.infra;
      return out;
    },
    // Обратно в форму: repeated поле — textarea по строке на элемент.
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const infra = obj.infra as Record<string, unknown> | undefined;
      if (infra && Array.isArray(infra.host_classes)) {
        out.infra = { ...infra, host_classes: (infra.host_classes as string[]).join("\n") };
      }
      return out;
    },
    emptyState: {
      title: "Каталог зон пуст",
      body: "Зона — точка размещения внутри региона. Пока зон нет, зональные ресурсы (подсети, машины, тома) создать нельзя.",
    },
  },

  "address-pools": {
    id: "address-pools",
    route: "address-pools",
    apiPath: "/vpc/v1/addressPools",
    // ПОСЛАБЛЕНИЕ, НАЗВАННОЕ ПО ПРИЧИНЕ: административный CRUD пула отвечает самим
    // ресурсом, а не операцией (`rpc Create/Update/AddCidrBlocks/RemoveCidrBlocks`
    // → `AddressPool`, `rpc Delete` → `DeleteAddressPoolResponse`). Умолчание
    // спеки строгое (ban #9), поэтому без этой строки успешный ответ пула
    // читался бы как нарушение контракта и показывал отказ на исправной правке.
    mutationsReturnOperation: false,
    payloadKey: "pools",
    singular: ENTITIES["address-pools"].singular,
    accusative: "пул адресов",
    plural: ENTITIES["address-pools"].plural,
    genitive: "Пула адресов",
    serviceTitle: SERVICES.system.title,
    scope: "global",
    ops: { create: true, update: true, delete: true },
    columns: [
      // Те же колонки и стиль, что у subnets list (CopyableName/Id, отдельные
      // v4/v6 блоки, LabelsCell): visual parity по запросу user'а.
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
      { header: "Тип", path: "kind", format: "text" },
      {
        header: "Зона",
        path: "zone_id",
        render: (row) => <RefNameLink specId="zones" refId={row.zone_id as string | undefined} maxChars={28} />,
      },
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
        render: (row) => <LabelsCell labels={row.selector_labels as Record<string, string> | undefined} />,
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
        label: "Имя",
        type: "string",
        placeholder: "<pool-name>",
      },
      { name: "description", label: "Описание", type: "text", rows: 2 },
      {
        // kind — UI ограничен одним значением, скрыт; backend требует поле в payload.
        name: "kind",
        label: "Вид",
        type: "enum",
        options: POOL_KINDS,
        required: true,
        default: "EXTERNAL_PUBLIC",
        immutable: true,
        hidden: true,
      },
      {
        name: "zone_id",
        label: "Зона",
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
        label: "Блоки IPv4 CIDR",
        type: "array",
        itemLabel: "v4-CIDR",
        description: "IPv4 CIDR-блоки, из которых аллоцируются внешние v4 адреса.",
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
        label: "Блоки IPv6 CIDR",
        type: "array",
        itemLabel: "v6-CIDR",
        description: "IPv6 CIDR-блоки, из которых аллоцируются внешние v6 адреса.",
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
        label: "По умолчанию для зоны и вида",
        type: "bool",
        default: false,
        description: "Пул по умолчанию — один на пару «зона + семейство адресов».",
      },
      {
        name: "selector_priority",
        label: "Приоритет выбора",
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
    // [{value: "..."}] → ["..."] для wire format (как subnets.v4/v6_cidr_blocks) и
    // отбрасывает пустые.
    //
    // Здесь стояло ещё и снятие ключа `cidr_blocks`. Снимать его не с чего: такого
    // поля нет ни в шаблоне, ни среди объявленных полей формы, а подстановки из
    // ссылки фильтруются по объявленным именам — то есть строка описывала работу,
    // предмета у которой не существует.
    sanitize: (obj) => {
      const flat: Record<string, unknown> = { ...obj };
      for (const key of ["v4_cidr_blocks", "v6_cidr_blocks"]) {
        const raw = flat[key];
        if (Array.isArray(raw)) {
          flat[key] = raw
            .map((item: unknown) =>
              typeof item === "object" && item !== null && "value" in item
                ? (item as Record<string, unknown>)["value"]
                : item,
            )
            .filter((v) => typeof v === "string" && v.trim() !== "");
        }
      }
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
    singular: ENTITIES["load-balancers"].singular,
    accusative: "балансировщик нагрузки",
    plural: ENTITIES["load-balancers"].plural,
    genitive: "Балансировщика нагрузки",
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
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
      {
        // NLB CONTRACT: placement — ЕДИНСТВЕННЫЙ авторитетный ввод режима на Create.
        // `type` / `placement_type` — производные output-only проекции; в запросе они
        // оставлены ТОЛЬКО чтобы клиент, который их выставил, получил явный
        // InvalidArgument. Форма их не шлёт.
        name: "placement",
        label: "Размещение",
        type: "enum",
        required: true,
        immutable: true,
        default: "EXTERNAL_REGIONAL",
        options: [
          { value: "EXTERNAL_REGIONAL", label: "EXTERNAL_REGIONAL — публичный, региональный" },
          { value: "INTERNAL_REGIONAL", label: "INTERNAL_REGIONAL — внутренний, региональный" },
          { value: "INTERNAL_ZONAL", label: "INTERNAL_ZONAL — внутренний, в одной зоне" },
        ],
        description: "Режим балансировщика. Неизменяем после создания; сочетания «внешний + зональный» в наборе нет.",
      },
      FIELD_NAME_COMPUTE, // DNS-1123 — lowercase + цифры + дефисы (как у NLB regex)
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        // Create-only: UpdateNetworkLoadBalancerRequest его не несёт.
        immutable: true,
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        description: "Регион размещения балансировщика.",
      },
      ...vipSourceFields("v4", "IPv4"),
      ...vipSourceFields("v6", "IPv6"),
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      placement: "EXTERNAL_REGIONAL",
      // Источник VIP пофамильно. Хотя бы одно семейство обязательно, иначе
      // resolveVipSources отвергает запрос целиком. IPv4 по умолчанию — «авто»
      // (для EXTERNAL это платформенный public VIP, ничего выбирать не нужно),
      // IPv6 выключен.
      _v4_source: "public",
      _v6_source: "off",
      v4_source: { subnet_id: "", address_id: "" },
      v6_source: { subnet_id: "", address_id: "" },
      labels: {},
    }),
    // Хотя бы одно семейство должно нести источник — тот же инвариант, что
    // resolveVipSources энфорсит на сервисе; ловим его до отправки.
    validate: (obj) => {
      const placement = obj.placement as string | undefined;
      if (!buildVipSourceOrNull(placement, obj, "v4") && !buildVipSourceOrNull(placement, obj, "v6")) {
        return "Укажите источник VIP хотя бы для одного семейства (IPv4 или IPv6).";
      }
      return null;
    },
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const placement = out.placement as string | undefined;
      const v4 = buildVipSourceOrNull(placement, out, "v4");
      const v6 = buildVipSourceOrNull(placement, out, "v6");
      delete out.v4_source;
      delete out.v6_source;
      delete out._v4_source;
      delete out._v6_source;
      if (v4) out.v4_source = v4;
      if (v6) out.v6_source = v6;
      return out;
    },
  },

  listeners: {
    id: "listeners",
    route: "listeners",
    apiPath: "/nlb/v1/listeners",
    payloadKey: "listeners",
    singular: ENTITIES.listeners.singular,
    accusative: "обработчик",
    plural: ENTITIES.listeners.plural,
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Балансировщик",
        path: "load_balancer_id",
        render: (row) => (
          <RefNameLink specId="load-balancers" refId={row.load_balancer_id as string | undefined} maxChars={36} />
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
        // Create-only: UpdateListenerRequest его не несёт.
        immutable: true,
        label: "Балансировщик",
        type: "string",
        required: true,
        description: "Балансировщик, которому принадлежит слушатель. Неизменяем после создания.",
      },
      {
        name: "protocol",
        // Create-only: UpdateListenerRequest его не несёт.
        immutable: true,
        label: "Протокол",
        type: "enum",
        required: true,
        options: [
          { value: "TCP", label: "TCP" },
          { value: "UDP", label: "UDP" },
        ],
        description: "Транспортный протокол. Неизменяем после создания.",
      },
      {
        name: "port",
        // Create-only: UpdateListenerRequest его не несёт.
        immutable: true,
        label: "Порт",
        type: "int",
        required: true,
        description: "Внешний порт (1..65535). Неизменяем после создания.",
      },
      FIELD_LABELS,
    ],
    template: () => ({
      name: "",
      description: "",
      load_balancer_id: "",
      protocol: "TCP",
      // `port` НЕ дефолтим: 0 вне диапазона [1,65535], который энфорсит
      // LbPort.Validate, поэтому засеянный ноль превращает «оператор не ввёл
      // порт» в тело, на которое сервис отвечает «port must be in range
      // [1, 65535]». Поле обязательное — значение вводит оператор.
      labels: {},
    }),
  },

  "target-groups": {
    id: "target-groups",
    route: "target-groups",
    apiPath: "/nlb/v1/targetGroups",
    payloadKey: "target_groups",
    singular: ENTITIES["target-groups"].singular,
    accusative: "целевую группу",
    plural: ENTITIES["target-groups"].plural,
    genitive: "Целевой группы",
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      {
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      {
        // CreateTargetGroupRequest.port — required: единый backend-порт группы, на
        // который таргеты получают перенаправленный трафик; эхо в
        // Listener.resolvedBackendPort.
        name: "port",
        label: "Порт бэкенда",
        type: "int",
        required: true,
        min: 1,
        max: 65535,
        default: 80,
        description: "Порт, на котором таргеты принимают перенаправленный трафик (1..65535).",
      },
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description: "Регион размещения группы целей. Неизменяем после создания.",
      },
      {
        // NLB-1c (B8): на проводе google.protobuf.Duration ("300s"); прежнее
        // int-секундное имя deregistration_delay_seconds — reserved и на Create,
        // и на Update. Форма редактирует число, sanitize/hydrate переводят.
        name: "deregistration_delay",
        label: "Время вывода из-под нагрузки (с)",
        type: "int",
        required: false,
        default: 300,
        min: 0,
        max: 3600,
        description:
          "Сколько ждать прекращения трафика перед удалением target'а из активного набора (0..3600). По умолчанию 300.",
      },
      {
        // Дискриминатор взаимоисключающей группы `HealthCheck.options`. Поле
        // ФОРМЫ, а не контракта (отсюда подчёркивание): на проводе ветвь
        // называет себя сама — тем, что она единственная заполненная. Без
        // дискриминатора выбрать ветвь нечем, и три из четырёх были невыразимы
        // (#375): проверка умела только TCP, а пути, коды ответа, заголовки и
        // имя службы объявлены контрактом и недостижимы из консоли.
        name: "_health_check_protocol",
        label: "HC: протокол",
        type: "enum",
        required: true,
        default: "tcp",
        options: [
          { value: "tcp", label: "TCP — соединение установилось" },
          { value: "http", label: "HTTP — ответ по пути" },
          { value: "https", label: "HTTPS — ответ по пути, шифрованно" },
          { value: "grpc", label: "gRPC — служба отвечает здоровой" },
        ],
        description:
          "Чем проверяется живость цели. TCP отвечает только за установленное соединение; остальные три спрашивают приложение.",
      },
      {
        name: "health_check.tcp.port",
        label: "HC: TCP-порт",
        type: "int",
        required: true,
        default: 80,
        visibleWhen: { field: "_health_check_protocol", equals: "tcp" },
        description: "TCP-порт для health-check'а (1..65535). По умолчанию 80.",
      },
      ...healthCheckAppFields("http"),
      ...healthCheckAppFields("https"),
      {
        name: "health_check.grpc.port",
        label: "HC: gRPC-порт",
        type: "int",
        required: false,
        visibleWhen: { field: "_health_check_protocol", equals: "grpc" },
        description: "Порт проверки. Пусто — берётся порт бэкенда группы.",
      },
      {
        name: "health_check.grpc.service_name",
        label: "HC: имя службы",
        type: "string",
        required: false,
        visibleWhen: { field: "_health_check_protocol", equals: "grpc" },
        placeholder: "grpc.health.v1.Health",
        description: "Имя службы в протоколе проверки здоровья. Пусто — спрашивается сервер целиком.",
      },
      {
        name: "health_check.interval",
        label: "HC: интервал",
        type: "string",
        required: true,
        default: "2s",
        description: "Интервал между health-check'ами (Duration в формате 'Ns', range 1s-600s). По умолчанию 2s.",
      },
      {
        name: "health_check.timeout",
        label: "HC: таймаут",
        type: "string",
        required: true,
        default: "1s",
        description: "Таймаут одного health-check'а (Duration). По умолчанию 1s.",
      },
      {
        name: "health_check.unhealthy_threshold",
        label: "Порог отказа проверки",
        type: "int",
        required: true,
        default: 2,
        description: "Сколько failed checks подряд до перевода в UNHEALTHY (2..10). По умолчанию 2.",
      },
      {
        name: "health_check.healthy_threshold",
        label: "Порог успеха проверки",
        type: "int",
        required: true,
        default: 2,
        description: "Сколько успешных checks подряд до перевода в HEALTHY (2..10). По умолчанию 2.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      port: 80,
      deregistration_delay: "300s",
      _health_check_protocol: "tcp",
      health_check: {
        tcp: { port: 80 },
        interval: "2s",
        timeout: "1s",
        unhealthy_threshold: 2,
        healthy_threshold: 2,
      },
      labels: {},
    }),
    // Форма правит секунды числом; контракт принимает Duration. Плюс ветвь
    // проверки живости: форма держит поля всех четырёх, в теле остаётся одна.
    sanitize: (obj) => {
      const out: Record<string, unknown> = sanitizeHealthCheck(obj);
      const raw = out["deregistration_delay"];
      // Пусто → не шлём вовсе, чтобы сервер применил СВОЙ дефолт. 0 — легальное
      // значение и обязано доехать явным "0s", а не быть спутанным с пустотой.
      const n =
        typeof raw === "number"
          ? raw
          : typeof raw === "string" && raw.trim() !== ""
            ? Number(raw.endsWith("s") ? raw.slice(0, -1) : raw)
            : NaN;
      if (Number.isFinite(n)) out["deregistration_delay"] = `${n}s`;
      else delete out["deregistration_delay"];
      return out;
    },
    // Duration → число, которое рендерит int-поле формы; заполненная ветвь
    // проверки живости → выбор в дискриминаторе.
    hydrate: (obj) => {
      const out: Record<string, unknown> = hydrateHealthCheck(obj);
      const raw = out["deregistration_delay"];
      if (typeof raw === "string" && raw.endsWith("s")) {
        const n = Number(raw.slice(0, -1));
        if (Number.isFinite(n)) out["deregistration_delay"] = n;
      }
      return out;
    },
  },
};

/** Ветви проверки живости, спрашивающие приложение (`http` и `https`), различаются
 *  только шифрованием — поля у них одни и те же, и выписывать их дважды значило бы
 *  завести два места об одном предмете. */
export const HEALTH_CHECK_APP_BRANCHES = ["http", "https"] as const;

/** Имена ветвей проверки живости, ЕДИНСТВЕННЫЙ перечень в консоли. */
export const HEALTH_CHECK_BRANCHES = ["tcp", "http", "https", "grpc"] as const;

function healthCheckAppFields(branch: "http" | "https"): FormField[] {
  const у = branch === "https" ? "HTTPS" : "HTTP";
  const when = { field: "_health_check_protocol", equals: branch } as const;
  return [
    {
      name: `health_check.${branch}.port`,
      label: `HC: ${у}-порт`,
      type: "int",
      required: false,
      visibleWhen: when,
      description: "Порт проверки. Пусто — берётся порт бэкенда группы.",
    },
    {
      name: `health_check.${branch}.path`,
      label: `HC: путь`,
      type: "string",
      required: false,
      visibleWhen: when,
      placeholder: "/healthz",
      description: "Путь запроса проверки. Пусто — корень «/».",
    },
    {
      name: `health_check.${branch}.expected_codes`,
      label: "HC: коды ответа",
      type: "string",
      required: false,
      visibleWhen: when,
      placeholder: "200-299",
      description: "Какие коды считать здоровыми: диапазон «200-299» или перечень «200,204». Пусто — любой 2xx.",
    },
    {
      name: `health_check.${branch}.host`,
      label: branch === "https" ? "HC: имя узла (Host / SNI)" : "HC: имя узла (Host)",
      type: "string",
      required: false,
      visibleWhen: when,
      description:
        branch === "https"
          ? "Заголовок Host и имя в рукопожатии TLS. Пусто — берётся адрес цели."
          : "Заголовок Host запроса проверки. Пусто — берётся адрес цели.",
    },
    {
      name: `health_check.${branch}.headers`,
      label: "HC: заголовки",
      type: "labels",
      required: false,
      visibleWhen: when,
      description: "Дополнительные заголовки запроса проверки.",
    },
  ];
}

/**
 * Ветвь проверки живости, названная формой, остаётся в теле ОДНА.
 *
 * Группа взаимоисключающая (`exactly_one`): две заполненные ветви — отказ
 * сервера. Форма держит поля всех четырёх, поэтому лишние обязана срезать сама,
 * а не надеяться, что пользователь их не тронул.
 *
 * Пустые строки внутри выбранной ветви не отправляются: у пути, кодов ответа и
 * имени узла есть СЕРВЕРНЫЕ умолчания, и пустая строка означала бы «пусто», а не
 * «как по умолчанию». Числа сохраняются целиком, включая 0: у порта это законное
 * значение со смыслом «взять порт группы».
 */
export function sanitizeHealthCheck(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  const hc = out["health_check"];
  const выбор = out["_health_check_protocol"];
  delete out["_health_check_protocol"];
  if (!hc || typeof hc !== "object") return out;

  const src = hc as Record<string, unknown>;
  const ветвь =
    typeof выбор === "string" && (HEALTH_CHECK_BRANCHES as readonly string[]).includes(выбор)
      ? выбор
      : (HEALTH_CHECK_BRANCHES.find((b) => src[b] !== undefined) ?? "tcp");

  const next: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(src)) {
    if ((HEALTH_CHECK_BRANCHES as readonly string[]).includes(k)) continue;
    next[k] = v;
  }
  const тело = src[ветвь];
  if (тело && typeof тело === "object") {
    const очищенное: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(тело as Record<string, unknown>)) {
      if (typeof v === "string" && v.trim() === "") continue;
      if (v === undefined || v === null) continue;
      if (typeof v === "object" && !Array.isArray(v) && Object.keys(v).length === 0) continue;
      очищенное[k] = v;
    }
    next[ветвь] = очищенное;
  } else {
    next[ветвь] = {};
  }
  out["health_check"] = next;
  return out;
}

/** Обратное: по заполненной ветви ответа восстановить выбор формы. */
export function hydrateHealthCheck(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  const hc = out["health_check"];
  if (hc && typeof hc === "object") {
    const src = hc as Record<string, unknown>;
    out["_health_check_protocol"] = HEALTH_CHECK_BRANCHES.find((b) => src[b] !== undefined) ?? "tcp";
  }
  return out;
}

/**
 * hasProtocolNumber — protocol_number is an int64, and protojson renders a 64-bit
 * integer as a JSON STRING. A rule read back from the server therefore carries
 * "47", not 47: a `typeof === "number"` test is false for every server-sent rule,
 * which resolved the protocol arm to "any" and silently widened the rule.
 */
export function hasProtocolNumber(v: unknown): boolean {
  if (typeof v === "number") return Number.isFinite(v);
  if (typeof v === "string") return v.trim() !== "" && Number.isFinite(Number(v));
  return false;
}

// Экспортирована для тестов.
export function sanitizeSgRule(r: Record<string, unknown>): Record<string, unknown> {
  const protoMode =
    (r._protocol_mode as string | undefined) ??
    (r.protocol_name ? "name" : hasProtocolNumber(r.protocol_number) ? "number" : "any");
  const portsAny = typeof r._ports_any === "boolean" ? r._ports_any : !r.ports;
  // Ветвь цели: та, что названа формой, иначе та, что заполнена в самом правиле.
  // `cidr_group_id` стоит в этой цепочке наравне с двумя прежними ветвями —
  // иначе правило, приехавшее с сервера со ссылкой на набор, вычищалось бы как
  // «CIDR-блоки» и теряло цель при первом же сохранении.
  //
  // Ветвей РОВНО ТРИ — столько же, сколько в `oneof target` контракта. Четвёртая
  // здесь стояла до #512 и выбиралась по полю, снятому с контракта: у сообщения
  // его номер и имя зарезервированы, значит ни один ответ края его не несёт и
  // ветвь не выбиралась НИКОГДА. Такая ветвь не «запас на будущее», а
  // утверждение, пережившее свой предмет: она выглядит как поддержка ещё одного
  // вида цели и не является ею.
  const targetKind =
    (r._target_kind as string | undefined) ??
    (r.cidr_blocks ? "cidr" : r.security_group_id ? "sg" : r.cidr_group_id ? "cidr-group" : "cidr");

  // Copy the persistent fields, dropping the form-only discriminators at EVERY
  // depth: the caller spreads a fetched SecurityGroupRule into this, and
  // SecurityGroupRuleSpec is not that message — a nested widget key would ride
  // along and be discarded at the edge without a word.
  const out: Record<string, unknown> = stripFormOnlyKeys({ ...r });
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
  // target oneof — оставляем ровно одну ветвь. Список ветвей ведётся ОДНИМ
  // перечнем, а не тремя ветками `if`: ветвь, забытая в одной из веток, уезжает
  // вместе с выбранной, и сервис отвергает правило целиком («ровно одна цель»).
  const TARGET_FIELD: Record<string, string> = {
    cidr: "cidr_blocks",
    sg: "security_group_id",
    "cidr-group": "cidr_group_id",
  };
  const keep = TARGET_FIELD[targetKind];
  for (const [kind, field] of Object.entries(TARGET_FIELD)) {
    if (kind !== targetKind || keep === undefined) delete out[field];
  }
  return out;
}

// === compute byte/GiB helpers ===
const GIB = 1024 * 1024 * 1024;

/**
 * fmtMiBGiB — память MachineType / EffectiveResources приходит в МиБ (int64
 * строкой); показываем человекочитаемо. Отдельно от fmtBytesGiB, который читает
 * БАЙТЫ: перепутать единицу — тихо показать в 1024 раза не то.
 */
export function fmtMiBGiB(v: unknown): string {
  const mib = typeof v === "string" ? Number(v) : typeof v === "number" ? v : NaN;
  if (!Number.isFinite(mib) || mib <= 0) return "—";
  const gib = mib / 1024;
  return `${gib >= 10 ? Math.round(gib) : Math.round(gib * 10) / 10} ГиБ`;
}

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

/**
 * sanitizeInstanceCreate — form-internal → CreateInstanceRequest.
 *
 * COMP-1 retired the raw sizing/boot channels (platform_id / resources_spec /
 * boot_disk_spec are RESERVED names on both Create and Update), so nothing is
 * converted into them any more. What is left to do here is contract shaping:
 *   - narrow boot_source to its two INPUT fields ({type,id}) — name /
 *     resolved_digest / materialized_volume are output-only and rejected on input;
 *   - keep exactly the `spec` oneof arm matching instance_kind, and drop the
 *     VM-only inputs on a CONTAINER;
 *   - NIC items: form-internal representation → NetworkInterfaceSpec;
 *   - drop empty optional scalars rather than sending "".
 * Form-only `_`-keys are removed here for the fields this function rebuilds, and
 * again — recursively, at every depth — by buildCreateBody on the transport edge.
 */
export function sanitizeInstanceCreate(obj: Record<string, unknown>): Record<string, unknown> {
  const o = { ...obj } as Record<string, unknown>;
  const kind = o["instance_kind"];

  // boot_source: на вход принимаются только {type,id}.
  const bs = (o["boot_source"] as Record<string, unknown> | undefined) ?? {};
  o["boot_source"] = { type: bs["type"], id: bs["id"] };

  // guest_access_key_ids: контракт ждёт ПЛОСКИЙ список идентификаторов, а
  // generic ArrayField хранит элемент объектом {value}. Пустой список не шлём
  // вовсе: пустое поле в теле утверждало бы «ключей нет», тогда как арендатор
  // просто не дошёл до него.
  const keys = flatIdList(o["guest_access_key_ids"]);
  if (keys) o["guest_access_key_ids"] = keys;
  else delete o["guest_access_key_ids"];

  if (kind === "CONTAINER") {
    delete o["vm_spec"];
    delete o["assign_external_address"];
    delete o["acknowledge_unreachable"];
    const cs = { ...((o["container_spec"] as Record<string, unknown> | undefined) ?? {}) };
    if (!cs["working_dir"]) delete cs["working_dir"];
    o["container_spec"] = cs;
  } else {
    delete o["container_spec"];
    const vs = { ...((o["vm_spec"] as Record<string, unknown> | undefined) ?? {}) };
    if (!vs["user_data"]) delete vs["user_data"];
    o["vm_spec"] = vs;
  }

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
  const specs = nics
    .map((nic) => {
      const out: Record<string, unknown> = {};
      // Тот же перевод «элемент-объект формы → плоский список», что и у ключей
      // доступа. Он живёт одним объявлением (`@shared/lib/id-list`): выписанный
      // здесь второй раз, он разошёлся бы с первым молча.
      const sgs = flatIdList(nic["security_group_ids"]) ?? [];
      // Существующий NetworkInterface (nic_id) — отдаём только nic_id (+ SG, если заданы);
      // подсеть/адрес берутся из самого NIC (см. compute.v1.NetworkInterfaceSpec.nic_id).
      if (nic["_use_existing_nic"] === true && nic["nic_id"]) {
        out["nic_id"] = nic["nic_id"];
        if (sgs.length > 0) out["security_group_ids"] = sgs;
        return out;
      }
      if (nic["subnet_id"]) out["subnet_id"] = nic["subnet_id"];
      if (sgs.length > 0) out["security_group_ids"] = sgs;
      const primaryAddr =
        typeof nic["primary_v4_address_spec"] === "object" && nic["primary_v4_address_spec"] !== null
          ? ((nic["primary_v4_address_spec"] as Record<string, unknown>)["address"] as string | undefined)
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
    })
    // Айтем, в котором не выбрано ни подсети, ни NIC, не является
    // NetworkInterfaceSpec — отправлять его нечем.
    .filter((spec) => spec["subnet_id"] || spec["nic_id"]);
  // Ровно один канал: явные спеки ИЛИ подсеть проекта по умолчанию.
  if (specs.length > 0) {
    o["network_interface_specs"] = specs;
    delete o["use_default_network"];
  } else {
    delete o["network_interface_specs"];
    o["use_default_network"] = true;
  }

  // strip optional empties
  for (const k of ["hostname", "service_account_id"]) {
    if (o[k] === "" || o[k] === undefined) delete o[k];
  }
  return o;
}

/**
 * Куда уходят Create / Update / Delete этого ресурса.
 *
 * Обычно — туда же, откуда он читается. У ресурса с admin-плоскостью (`admin`)
 * это разные пути: публичный путь geo Region/Zone обслуживает только чтение, и
 * POST по нему не смаршрутизирован вообще.
 */
export function mutationBasePath(spec: ResourceSpec): string {
  return spec.admin?.basePath ?? spec.apiPath;
}

/**
 * Откуда форма редактирования читает начальное состояние.
 *
 * У двухпроекционного ресурса мутируемые поля живут только на Internal-проекции,
 * поэтому читать надо оттуда — иначе форма покажет пустое значение там, где оно
 * есть, и оператор перезапишет его вслепую.
 */
export function editReadPath(spec: ResourceSpec, id: string): string {
  const base = spec.admin?.readForEdit ? spec.admin.basePath : spec.apiPath;
  return `${base}/${id}`;
}

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// Домен-владелец и правило сборки адреса живут в `@shared/lib/service-prefix` —
// чистом файле без React, чтобы модуль мог взять их, не таща за собой чужой
// реестр. Здесь только ре-экспорт: у него нет тела, поэтому разойтись с
// источником он не может, а вызывающие не меняют импортов.
export {
  isSystemScopedResource,
  resourceListPath,
  resourceServicePrefix,
  type ServicePrefix,
} from "@shared/lib/service-prefix";

// resourceProjectPath — полный SPA-путь до listing данного ресурса в
// контексте project'а. Возвращает null для IAM-ресурсов (они не scoped to
// project), для cluster-scoped каталога отдаёт /system/*, и null когда
// projectId не известен.
export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  const spec = REGISTRY[specId];
  if (!spec) return null;
  return resourceListPathImpl(specId, spec.route, projectId);
}

// Thin generic wrapper over the single lib/path implementation (superset that
// also resolves bracket-indexed array paths like "spec.rules[0].direction").
// Kept as a named export (re-exported as getResourceValueByPath) so the many
// detail/list call sites keep their <T> type signature unchanged.
export function getByPath<T = unknown>(obj: unknown, path: string): T | undefined {
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
