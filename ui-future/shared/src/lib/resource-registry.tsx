// Реестр ресурсов: метаданные для generic ListPage / DetailPage / Create-Edit.
// Scope: 7 ресурсов Kachō proto.
// apiPath содержит полный путь с доменным префиксом (verbatim из proto google.api.http annotations).

import type { ReactNode } from "react";
import { Tag, Tooltip, Typography } from "antd";
import { StopOutlined, UnlockOutlined, UserDeleteOutlined } from "@ant-design/icons";
import type { FormField } from "./form-schema";
import { NAME_FORM, NAME_FORM_REGISTRY, NAME_HINT, NAME_HINT_OPTIONAL, NAME_HINT_REGISTRY } from "./name-form";
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
import { ArtifactTypesTag } from "@shared/components/atoms/ArtifactTypeTag";
import { RepositoryLifecycleTag } from "@shared/components/atoms/RepositoryLifecycleTag";
import { VisibilityTag } from "@shared/components/atoms/VisibilityTag";
import { NicSpecFields } from "@shared/components/organisms/form/NicSpecFields";
import { NlbVipCell } from "@shared/components/molecules/NlbVipCell";
import {
  NlbVipSourceField,
  NlbDisabledZonesField,
  buildVipSourceOrNull,
  lbTypeFromPlacement,
  lbPlacementTypeFromPlacement,
} from "@shared/components/organisms/form/NlbVipSourceField";
import { stripFormOnlyKeys } from "@shared/lib/update-mask";
import {
  roleIsSystem,
  targetKind,
  targetResources,
  userBlockPath,
  userUnblockPath,
  userRemoveFromAccountPath,
  type AccessBindingTarget,
  type DefinitionTier,
} from "@shared/api/iam";
import { displayText } from "@shared/lib/display-text";
import { formatBytes } from "@shared/lib/bytes";
// Словарь класса диска — ЕДИНСТВЕННАЯ реализация, та же, что читают карточка
// класса и подпись опции в подборщике. Реестр домена storage ходил в неё через
// свой ре-экспорт; после сведения ходит напрямую.
import {
  LIFECYCLE_HINT,
  TIER_HINT,
  acceptsNewVolumes,
  lifecycleLabel,
  tierLabel,
} from "@shared/lib/storage-disk-type";
import { flatIdList } from "@shared/lib/id-list";
import {
  GUEST_ACCESS_KEY_EMPTY_STATE,
  GUEST_ACCESS_KEY_FIELDS,
  guestAccessKeyTemplate,
} from "@shared/lib/guest-access-key-form";
import { resourceListPath as resourceListPathImpl } from "@shared/lib/service-prefix";
// Форма группы целей — ОДНА реализация на оба реестра (`@shared` и `nlb`):
// пока она жила внутри одного, ветви проверки живости доехали до vpc/iam/system
// и не доехали до `/nlb/*`, который эту группу и рисует (#375).
import {
  healthCheckFields,
  hydrateHealthCheck,
  sanitizeHealthCheck,
  sanitizeTargets,
  targetsField,
} from "@shared/lib/target-group-form";
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";
import type { ResourceColumn, ResourceSpec, RowVerbContext, RowVerbState } from "./resource-spec";
// Подписи сущностей и разделов — из единственного источника (см. entity-names.ts):
// литерал рядом с местом показа расходится молча, ссылка — нет.
import { ENTITIES, SERVICES } from "./entity-names";

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`, и импортируется
// сюда. Реэкспорт оставлен, чтобы потребители этого модуля не меняли импорты: у него
// нет тела, поэтому разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы
// здесь запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.

export type { ResourceColumn, ResourceSpec };

/**
 * Выбрана ли ОБЛАСТЬ-аккаунт — предикат, от которого зависит пункт «Исключить из
 * аккаунта» И подтверждение соседнего пункта, который его называет.
 *
 * ПОЧЕМУ ОДНА ФУНКЦИЯ, А НЕ ДВА `!!ctx.accountId` ПОДРЯД (#1208). Это два места
 * об одном предмете: пункт РИСУЕТСЯ по этому условию, а соседнее подтверждение
 * ОБЕЩАЕТ его человеку. Порознь обе ветки защитимы и выглядят верными — неверна
 * ровно их РАЗНИЦА, а она в диффе не видна. С одним читаемым предикатом
 * расхождение невозможно by construction.
 *
 * ПОЧЕМУ ПУНКТ ВООБЩЕ ЗАВИСИТ ОТ ОБЛАСТИ, и почему «сделать его безусловным» —
 * не исход. Предмет исключения — ПАРА «человек × аккаунт», и второй половине
 * взяться неоткуда:
 *
 *   - `RemoveUserFromAccountRequest.account_id` объявлен ОБЯЗАТЕЛЬНЫМ
 *     (`proto/kaname/cloud/iam/v1/user_service.proto`) — запрос без него край
 *     отвергает, то есть пункт не работал бы ни при каком вводе;
 *   - на строке личности аккаунта НЕТ: поле снято с контракта (#471,
 *     `User.reserved 6 / "account_id"`), потому что членств у человека бывает
 *     несколько и одно значение из множества лгало бы;
 *   - и сама страница его не подразумевает: `ListUsersRequest.account_id`
 *     необязателен — надзор облака видит здесь людей ВСЕХ аккаунтов сразу.
 *
 * Значит единственный источник — выбранная область. Пункт, предложенный без
 * неё, был бы объявленной и неисполнимой возможностью; остаётся назвать условие.
 */
function accountScopeChosen(ctx: RowVerbContext): boolean {
  return !!ctx.accountId;
}

/**
 * Узкий выход, названный подтверждением ШИРОКОГО действия, — ОДИН текст на обе
 * ветки подтверждения, а не по фразе в каждой.
 *
 * ПОЧЕМУ ОДИН ПРОИЗВОДИТЕЛЬ (#1219). Ветка над ЧУЖОЙ строкой узкий выход
 * называла; ветка над СОБСТВЕННОЙ не называла его вовсе, и различие это никто не
 * решал — оно завелось побочным эффектом. Обе ветки по отдельности защитимы;
 * неверна ровно их РАЗНИЦА, а разница в диффе не видна (`architecture.md`
 * §«Параллельные полосы одного механизма обязаны сверяться МЕЖДУ СОБОЙ»). Пока
 * фраза строится здесь, «названо одной» и «не названо другой» разойтись не могут
 * by construction — так же, как `accountScopeChosen` не даёт разойтись «пункт
 * есть» и «пункт обещан» (#1208).
 *
 * ПОЧЕМУ НАД СОБОЙ ЭТОТ ВЫХОД ЕСТЬ, а не «недоступен и потому умолчан».
 * Измерено, а не предположено, по двум независимым признакам:
 *
 *   - МЕНЮ: `remove-from-account` над своей строкой РИСУЕТСЯ — у него своя ветка
 *     `isSelf` («Исключить из аккаунта СЕБЯ?»), а условие у него ровно одно и
 *     общее с соседом — выбранная область. Наблюдается и браузером: у арендатора
 *     проб единственная строка на странице собственная, и пункт на ней
 *     появляется (`ui-future/e2e/specs/users.spec.ts`, #1127).
 *   - МОДЕЛЬ ПРАВ: запрет гейтится `iam_user.identity_suspender`, а он есть
 *     `super_admin from account`; исключение гейтится `account.member_remover`,
 *     а он есть `editor`, то есть `… or admin or super_admin`. Круг первого
 *     ВЛОЖЕН в круг второго на том же аккаунте: всякий, кто вправе запретить
 *     участие, вправе и исключить из аккаунта. Обратное неверно — и это ровно та
 *     причина, по которой узкий выход стоит называть.
 *
 * ПРЕДИКАТ ПЕРЕСМОТРА этого абзаца назван, чтобы вложенность не наследовали на
 * веру: изменится тело любого из двух отношений в
 * `proto/kaname/cloud/iam/v1/fga_model.fga` — вложенность перемеряют заново.
 *
 * ПОЧЕМУ ЦЕНА ЗДЕСЬ ВЫШЕ, ЧЕМ У СОСЕДА. Запрет себе необратим самим
 * запрещающим: восстановление пароля его не снимает, вернуть доступ может только
 * администратор облака. Человек, не узнавший про узкий выход, платит за незнание
 * собственным доступом — а не чужим.
 */
function narrowExitFromAccount(ctx: RowVerbContext, isSelf: boolean): string {
  // Различается ТОЛЬКО тот, кого выводят. Всё остальное — сам факт упоминания и
  // условие появления пункта — обязано совпасть, поэтому строится один раз.
  const away = isSelf ? "выйти" : "вывести человека";
  return accountScopeChosen(ctx)
    ? `Чтобы ${away} ТОЛЬКО из этого аккаунта, есть «Исключить из аккаунта».`
    : `Чтобы ${away} ТОЛЬКО из одного аккаунта, есть «Исключить из аккаунта» — ` +
        `выберите область в шапке, и этот пункт появится здесь же, в меню строки.`;
}

// ── Наборы, которые форма правит ПОЛНОЙ ЗАМЕНОЙ ─────────────────────────────
//
// Схема формы уносит набор на край целиком, поэтому поле контракта, которого не
// назвал тип-черновик, исчезает у ВСЕХ элементов — включая нетронутые. Состав
// черновиков сверяется с контрактом гейтом
// `test/set-replacement-draft-composition`, а перепись мест он берёт обходом
// дерева: новое такое место без объявления рядом уронит его с координатой.

/** Строки маршрутов формы таблицы маршрутов (`render` + `sanitize` ниже). */
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

// Форма имени и её подсказка — в `name-form.ts`, в единственном экземпляре.
// Здесь только то, чем поля различаются между собой: обязательность и владелец
// формы. Разбор, почему форм две и чем держится совпадение с платформой, — там.

/** Имя обязательное (IAM). */
const FIELD_NAME: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  required: true,
  placeholder: "my-resource",
  description: NAME_HINT,
  pattern: NAME_FORM,
};

/**
 * Имя необязательное — vpc · compute · storage · nlb.
 *
 * Отдельной константы на сервис больше нет: форма у них ОДНА, а разное
 * объявление формы и есть предмет #1604. Различается только обязательность, и
 * она здесь единственная ось различия.
 */
const FIELD_NAME_OPTIONAL: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-resource",
  description: NAME_HINT_OPTIONAL,
  pattern: NAME_FORM,
};

// Имя реестра образов судится СВОЕЙ формой — почему, сказано в `name-form.ts`.
const FIELD_NAME_REGISTRY: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  required: true,
  placeholder: "my-registry",
  description: `${NAME_HINT_REGISTRY} Можно изменить позже — имя не входит в OCI-путь (тот по идентификатору).`,
  pattern: NAME_FORM_REGISTRY,
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
// Здесь стояла ВТОРАЯ реализация этих правил — четыре объявленных поля на
// семейство (`_v4_source`, `v4_source.subnet_id`, …) и свои `lbTypeFromPlacement`
// / `buildVipSourceOrNull` с другой сигнатурой. Их близнец жил в модуле `nlb`, и
// пользователь видел РАЗНЫЕ формы одного ресурса: маршрут `/nlb/*` рисует модуль,
// а этот реестр — оболочку. Реализация теперь одна и та, что богаче: выбор
// «сеть → адрес» деревом, отбор кандидатов по размещению и явный отказ от
// семейства (#1471).
//
// Сами правила — в `@shared/components/organisms/form/NlbVipSourceField`, рядом с
// виджетом, который их исполняет; отсюда они только зовутся.

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

// SizeCell — размер (байты int64 строкой) в человекочитаемом виде; пусто/0 → «—».
function SizeCell({ value }: { value: unknown }): ReactNode {
  const t = formatBytes(value);
  return t === "—" ? <Typography.Text type="secondary">—</Typography.Text> : <>{t}</>;
}

// TierCell / LifecycleCell — закрытые словари класса диска СЛОВАМИ, а не
// токенами перечисления. Подписи и пояснения живут в `@shared/lib/storage-disk-type`,
// чтобы у текста было ОДНО место: тот же словарь читают карточка класса и
// подпись опции в подборщике.
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
  // ====== iam ======
  // proto: kaname.cloud.iam.v1.AccountService / ProjectService.

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
    emptyState: {
      title: "Создайте первый аккаунт",
      body:
        "Аккаунт — верхний уровень Kachō: владелец, проекты, пользователи и роли живут внутри него. " +
        "Создайте аккаунт, чтобы начать выдавать доступ и заводить проекты.",
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
    emptyState: {
      title: "Создайте первый проект",
      body:
        "Проект — рабочая область внутри аккаунта: сети, машины, тома и права живут в его границах. " +
        "Ресурсы разных проектов не видят друг друга, поэтому проект и есть единица изоляции.",
      docs: ["Проекты и аккаунты"],
    },
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
    emptyState: {
      title: "Создайте первый сервисный аккаунт",
      body:
        "Сервисный аккаунт — учётная запись для программ: конвейеров, скриптов, внешних систем. " +
        "Ему выдают права так же, как человеку, но входит он по ключу, а не по паролю.",
      docs: ["Сервисные аккаунты и ключи"],
    },
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
    template: ({ accountId }) => ({
      name: "",
      account_id: accountId ?? "",
      description: "",
    }),
  },

  // User — read+delete only (создаётся через signup / InternalUserService).
  //
  // Запрет участия и его возврат — ДЕЙСТВИЯ (`:block` / `:unblock`), а не
  // правка поля: у действия нет маски, поэтому «забыть поле» и выключить всех,
  // кого коснулся, здесь невозможно by construction. Права решает край
  // (`v_update` на этом пользователе); консоль ничего не предугадывает — при
  // отказе прилетит 403, и его покажет общий механизм исхода операции.
  //
  // Объявление живёт ЗДЕСЬ, а не своей страницей: страница мимо общей оболочки
  // у этого ресурса уже была, её не рендерил ни один маршрут, и вместе с ней с
  // экрана ушли оба глагола (#421 → #440).
  // Здесь стояло «отдельная generic-страница не используется — UI остаётся
  // кастомным». В дереве НАОБОРОТ, и утверждение пережило свой предмет (#1441
  // п.2): список ведёт `IamUsersListShell` — тонкая обёртка над общим
  // `ResourceListPage`, а карточка и её вкладки ведут в общую оболочку
  // (`ResourceShell spec={REGISTRY.users}`, три маршрута в `IamPage`).
  // Опасность обычная для этого класса: следующий читатель принял бы фразу за
  // ограничение и не стал бы пользоваться общим — то есть завёл бы ту самую
  // страницу мимо оболочки, которую отсюда уже убирали (#421 → #440).
  //
  // Запись реестра при этом нужна и для ref-резолва (Account.owner_user_id) и
  // RefNameLink — это верно и остаётся.
  // `ops.update: false` здесь про МЕХАНИЗМ, а не про возможность: правку край
  // обслуживает, и консоль её даёт — своей страницей раздела IAM, мимо общей
  // формы. Причина названа полем `mutationsNotOffered`, чтобы пара «край умеет ·
  // ops говорят нет» не читалась как отсутствие возможности (#1593).
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
    // Край обслуживает правку пользователя, и консоль её ДАЁТ — своей страницей
    // раздела IAM, мимо общей формы. `ops` здесь говорят про механизм.
    mutationsNotOffered:
      "Правка дана своей страницей раздела IAM, а не общей формой: у пользователя " +
      "свой набор полей и свои ограничения. Общая форма его не выражает.",
    emptyState: {
      title: "Пригласите первого пользователя",
      body:
        "Пользователь — человек, который входит в консоль и работает с ресурсами аккаунта. " +
        "Права он получает привязкой роли, а не самим фактом приглашения.",
      docs: ["Пользователи и приглашения"],
    },
    columns: [
      { header: "Эл. почта", path: "email", format: "text" },
      { header: "Отображаемое имя", path: "display_name", format: "text" },
      { header: "Статус", path: "invite_status", format: "status" },
      // Столбца «Аккаунт» здесь нет намеренно (#473). Строка `users` — это
      // ЧЛЕНСТВО: одна личность держит по строке на каждый аккаунт, поэтому
      // «аккаунты человека» столбцом не выражаются, а показать их нечем —
      // такого поля нет ни на одном чтении контракта. Жёсткое предусловие
      // ветви «список аккаунтов» не выполнено by construction, и решение
      // автоматически становится «убрать столбец».
      //
      // Ответ «к какому аккаунту относится это членство» живёт на КАРТОЧКЕ, и
      // с #1085 у него снова есть источник — ресурс членства, читаемый
      // аккаунт-скоупно. На карточке значение одно, сверено чтением у текущего
      // аккаунта и не повторяется в каждой строке.
      //
      // В СПИСОК оно от этого не возвращается, и это не непоследовательность:
      // список охватывает несколько аккаунтов сразу, поэтому столбец назвал бы
      // один аккаунт там, где их несколько, — то есть солгал бы. Карточка
      // спрашивает про ОДИН названный аккаунт, список не спрашивает ни про
      // какой.
      { header: "ID", path: "id", format: "uid-short" },
      { header: "Внешний идентификатор", path: "external_id", format: "uid-short" },
      { header: "Создан", path: "created_at", format: "datetime" },
    ],
    rowVerbs: [
      {
        // ОДИН пункт на три состояния, а не пара кнопок: состояние здесь —
        // предмет, и предлагать «запретить» уже запрещённому значит предлагать
        // вызов, который ничего не изменит.
        key: "participation",
        resolve: (row, ctx): RowVerbState | null => {
          const id = (row.id as string | undefined) ?? "";
          const who = (row.email as string | undefined) || id;
          const status = row.invite_status as string | undefined;

          // Неподтверждённое приглашение край отвергает: внешней личности у
          // него ещё нет, а перевод в действующее — это активация при первом
          // входе, другой путь. Пункт остаётся ВИДИМЫМ и называет причину:
          // скрытый пункт неотличим от возможности, которой нет.
          if (status === "PENDING") {
            return {
              label: "Запретить участие",
              icon: <StopOutlined />,
              // Совет называет ПУНКТ ЭТОГО ЖЕ МЕНЮ, а не действие, которого нет.
              // Здесь стояло «Отзовите приглашение» — глагола отзыва у людей не
              // существует ни в контракте, ни на краю (#1442), и читатель уходил
              // искать его тем увереннее, что `Revoke` есть у выдач, ключей и
              // токенов. Исключение снимает строку членства независимо от её
              // состояния, поэтому неподтверждённое приглашение оно снимает так же.
              //
              // Предикат ОБЩИЙ с тем пунктом, который совет называет (#1208):
              // `remove-from-account` без выбранного аккаунта не рисуется вовсе,
              // и обещать его тогда значило бы повторить ровно ту ошибку, которую
              // эта правка снимает, — назвать действие, которого на экране нет.
              disabledReason: accountScopeChosen(ctx)
                ? "Приглашение ещё не подтверждено — запрещать нечего. " +
                  "Чтобы человек не участвовал в аккаунте, исключите его: " +
                  "пункт «Исключить из аккаунта» ниже."
                : "Приглашение ещё не подтверждено — запрещать нечего. " +
                  "Выберите аккаунт, чтобы исключить человека из него.",
              path: userBlockPath(id),
              confirmTitle: "Запретить участие?",
              confirmText: "",
              okText: "Запретить",
              progressTitle: "Запрет участия",
            };
          }

          if (status === "BLOCKED") {
            return {
              label: "Вернуть участие",
              icon: <UnlockOutlined />,
              path: userUnblockPath(id),
              confirmTitle: "Вернуть участие?",
              confirmText: `Разрешить «${who}» снова входить в этот аккаунт.`,
              okText: "Вернуть",
              progressTitle: "Возврат участия",
            };
          }

          // Самоблокировка НЕ запрещается, но предупреждение говорит прямо, чем
          // она кончится: самостоятельного пути снятия не существует по
          // построению (восстановление пароля запрет не снимает). Промолчать —
          // значит дать оператору выключить себя одним нажатием и узнать цену
          // потом.
          const isSelf = !!ctx.selfId && ctx.selfId === id;
          return {
            label: "Запретить участие",
            icon: <StopOutlined />,
            danger: true,
            path: userBlockPath(id),
            confirmTitle: isSelf ? "Запретить участие СЕБЕ?" : "Запретить участие?",
            // Узкое действие НАЗЫВАЕТСЯ в обоих состояниях области (#1208) И В
            // ОБЕИХ ВЕТКАХ подтверждения — над чужой строкой и над своей
            // (#1219). Различается только тот, кого выводят; сам факт упоминания
            // и условие берутся у ОДНОГО производителя.
            //
            // Промолчать о нём нельзя: человек, не узнавший про узкий выход,
            // выполнит широкий — ровно тот исход, ради предотвращения которого
            // фраза и написана. Над собой цена промаха выше: снять запрет
            // самому нельзя. Но и обещать безусловно нельзя: без выбранной
            // области пункта в меню НЕТ, и читающий ищет то, чего не видит, а
            // найдя вместо него единственный доступный пункт — запрещает вход
            // на платформу целиком.
            confirmText:
              (isSelf
                ? `Вы запрещаете себе («${who}») вход НА ПЛАТФОРМУ — во всех своих аккаунтах. ` +
                  `Снять запрет самостоятельно будет НЕЛЬЗЯ: восстановление пароля запрет не ` +
                  `снимает. Вернуть доступ сможет только администратор облака. `
                : `«${who}» больше не сможет входить НА ПЛАТФОРМУ — во всех своих аккаунтах, ` +
                  `не только в этом. Уже выданный токен доживёт свой срок; новый не выдадут. `) +
              narrowExitFromAccount(ctx, isSelf),
            okText: isSelf ? "Да, запретить себе" : "Запретить",
            progressTitle: "Запрет участия",
          };
        },
      },
      {
        // ИСКЛЮЧЕНИЕ ИЗ АККАУНТА — не запрет и не удаление, и различие тут не
        // оформительское (#1127). Запрет выше пишется в состояние ГЛОБАЛЬНОЙ
        // строки и выключает человеку вход на платформу целиком; удаление
        // стирает эту строку во всех его аккаунтах сразу. Оба — действия уровня
        // облака (#1102 / #1131), и распорядителю аккаунта край на них
        // отказывает. Исключение снимает СТРОКУ ЧЛЕНСТВА: человек перестаёт
        // участвовать здесь и продолжает работать в остальных своих аккаунтах.
        // Это то, что директива владельца распорядителю оставляет.
        key: "remove-from-account",
        resolve: (row, ctx): RowVerbState | null => {
          const id = (row.id as string | undefined) ?? "";
          const who = (row.email as string | undefined) || id;
          const accountId = ctx.accountId ?? "";

          // Без аккаунта пункта НЕТ вовсе: предмет исключения — пара, и запрос
          // без второй половины ушёл бы неизвестно куда. `null` здесь честнее
          // отключённого пункта: причина не в строке, а в том, что область не
          // выбрана, и подсказка на строке об этом не скажет.
          //
          // Предикат ОБЩИЙ с подтверждением соседнего глагола, который этот
          // пункт называет (#1208): пока читатель один, «пункт есть» и «пункт
          // обещан» разойтись не могут.
          if (!accountScopeChosen(ctx)) return null;

          const isSelf = !!ctx.selfId && ctx.selfId === id;
          return {
            label: "Исключить из аккаунта",
            icon: <UserDeleteOutlined />,
            danger: true,
            path: userRemoveFromAccountPath(id),
            // Вторая половина предмета едет ТЕЛОМ: у человека аккаунтов бывает
            // несколько, и вывести аккаунт из его строки нельзя — на строке
            // личности его больше нет.
            body: { accountId },
            confirmTitle: isSelf ? "Исключить из аккаунта СЕБЯ?" : "Исключить из аккаунта?",
            confirmText: isSelf
              ? `Вы выводите себя («${who}») из этого аккаунта. Вернуть себя обратно вы не ` +
                `сможете: приглашает тот, кто в аккаунте распоряжается. Личность и остальные ` +
                `ваши аккаунты не затрагиваются.`
              : `«${who}» перестанет участвовать в этом аккаунте. Личность сохраняется: в ` +
                `других своих аккаунтах человек продолжит работать. Сначала снимите его права ` +
                `в этом аккаунте — пока они есть, исключение отвергается.`,
            okText: isSelf ? "Да, исключить себя" : "Исключить",
            progressTitle: "Исключение из аккаунта",
          };
        },
      },
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [FIELD_NAME, FIELD_ACCOUNT_ID, FIELD_LABELS, FIELD_DESCRIPTION],
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
              {/* Счётчик правил и «+N» — второстепенный текст: тон берётся ролью темы,
                  потому что литерал чёрного 45% на тёмной странице был неразличим. */}
              {more > 0 && <span style={{ fontSize: 11, color: "var(--kc-text-tertiary)" }}>+{more}</span>}
              <span style={{ fontSize: 11, color: "var(--kc-text-tertiary)" }}>· {rules.length}</span>
            </span>
          );
        },
      },
      COL_CREATED,
    ],
    // generic-поля create/edit — name/description/account_id; permissions —
    // доменная ветка, здесь его нет.
    fields: [FIELD_NAME, FIELD_ACCOUNT_ID, FIELD_DESCRIPTION],
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
    ops: { create: false, update: true, delete: true },
    // Край обслуживает создание привязки, и консоль его ДАЁТ — страницей выдачи
    // (`/iam/access-bindings/create`): у выдачи свой мастер (субъект → роль →
    // область), которого общая форма не выражает.
    mutationsNotOffered:
      "Создание дано страницей выдачи прав, а не общей формой: предмет выдачи — " +
      "тройка «субъект · роль · область», и общая форма её не выражает.",
    // ФОРМА ПРАВКИ — РОВНО ПО МАСКЕ КРАЯ, не шире и не уже.
    //
    // Край принимает у привязки два поля: `deletion_protection` и `labels`
    // (`UpdateAccessBindingRequest`, остальное immutable — снимается и заводится
    // заново). Консоль же объявляла ресурс неправимым вовсе, и получалось хуже,
    // чем «не показали кнопку»: карточка ПОКАЗЫВАЛА замок, отзыв при включённом
    // замке пряталcя, а снять замок из консоли было нечем. Свойство видно,
    // управлять им невозможно — тупик, из которого выход только через API.
    fields: [
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        description: "Пока защита включена, привязку нельзя отозвать. Снимите её, чтобы отозвать доступ.",
      },
      { name: "labels", label: "Метки", type: "labels" },
    ],
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
    emptyState: {
      title: "Нет привязок доступа",
      body:
        "Привязка доступа назначает субъекту (пользователю, сервисному аккаунту или группе) роль на ресурсе " +
        "(аккаунте, проекте или кластере). Создайте привязку, чтобы выдать доступ.",
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
    emptyState: {
      title: "Создайте вашу первую облачную сеть",
      body:
        "Облачная сеть Kachō объединяет подсети, таблицы маршрутов и группы безопасности в единое " +
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
        multiline: true,
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
        header: "Таблица маршрутов по умолчанию",
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    // VPC-1: declared supernet ipv4/ipv6_cidr_blocks[] is required at Create and
    // immutable through Update — grown/shrunk only via :add/:remove-cidr-blocks
    // on the detail page (editHidden). default-SG + default-RT are provisioned
    // unconditionally by the server (no opt-out flag).
    fields: [
      FIELD_NAME_OPTIONAL,
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
        multiline: true,
        render: (row) => <CidrPrimaryCell primary={row.ipv4_cidr_primary} extra={row.ipv4_cidr_blocks} />,
      },
      {
        header: "IPv6 CIDR",
        path: "ipv6_cidr_primary",
        multiline: true,
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      {
        header: "Таблица маршрутов",
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
      FIELD_NAME_OPTIONAL,
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
          "Опционально. Если не задано — авто-ассоциируется таблица маршрутов по умолчанию сети (network.defaultRouteTableId°).",
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
          // Порядок предпочтения, а не список исключений: у адреса ровно одна
          // ветка oneof, и колонка показывает ту, что заполнена.
          //
          // Здесь стояло «internal_* оставлены для backward compat». Это было
          // неверно вдвойне: внутренний адрес — штатный вид, который арендатор
          // резервирует в подсети (так говорит и пустое состояние раздела), а до
          // #927 такие строки до колонки вообще не доходили — список отбрасывал
          // их раньше. То есть ветка, объявленная совместимостью со старым, была
          // единственным местом, готовым показать обычный сегодняшний ресурс.
          const ip = ext || ext6 || int || int6;
          if (!ip) return <span className="text-muted-foreground">—</span>;
          return <span className="font-mono text-xs">{ip}</span>;
        },
      },
      {
        header: "Используется",
        path: "used",
        render: (row) => <BoolFact value={row.used} yes="Используется" no="Свободен" yesTone="active" yesGlyph="link" />,
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
          <BoolFact
            value={row.deletion_protection}
            yes="Удаление запрещено"
            no="Удаление разрешено"
            yesTone="good"
            yesGlyph="lock"
            noTone="attention"
            noGlyph="unlock"
          />
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
    emptyState: {
      title: "Создайте вашу первую таблицу маршрутов",
      body:
        "С помощью таблиц маршрутов вы можете построить маршруты между облачной сетью Kachō и другими " +
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
    emptyState: {
      title: "Создайте первый сетевой интерфейс",
      body:
        "Сетевой интерфейс — точка подключения машины к подсети: он несёт её адреса и группы безопасности. " +
        "Интерфейс живёт отдельно от машины, поэтому его можно переносить между ними.",
      docs: ["Сетевые интерфейсы"],
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
      // Булево показывается СЛЕДСТВИЕМ, а не сырым `true`: в таблице стояло
      // именно оно — значение из ответа, ничего не сообщающее о предмете.
      // «По умолчанию» у группы безопасности означает, что её получают
      // интерфейсы, которым группу не назвали явно.
      {
        header: "По умолчанию",
        path: "default_for_network",
        format: "bool",
        boolLabels: { yes: "Группа по умолчанию", no: "Назначается явно" },
      },
      COL_CREATED,
      COL_ID,
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
    emptyState: {
      title: "Создайте первый шлюз",
      body:
        "Шлюз — выход из облачной сети наружу: через него ресурсы без публичного адреса обращаются в интернет. " +
        "Маршрут к шлюзу задаётся в таблице маршрутов подсети.",
      docs: ["Шлюзы и выход в интернет"],
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
        header: "Описание",
        path: "description",
        format: "text",
      },
      {
        header: "Метки",
        path: "labels",
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
        multiline: true,
        render: (row) => <CidrListCell items={[row.v4_cidr_blocks]} />,
      },
      {
        header: "IPv6",
        path: "v6_cidr_blocks",
        multiline: true,
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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

  // ====== storage: DiskType (read-only catalog) ======
  //
  // Спека ПОЛНАЯ (перенесена из реестра домена storage): ярус, состояние
  // обращения и границы размера показываются словами закрытого словаря, а не
  // токенами перечисления.
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
    genitive: "Типа диска",
    description:
      "Класс хранилища, на котором создаётся том: ярус, состояние обращения, границы размера и способности. Каталог заводит администратор кластера; пустой каталог — законное состояние, пока класс не зарегистрирован, том не создаётся.",
    serviceTitle: SERVICES.storage.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    // Каталог типов дисков ведёт оператор платформы, а не арендатор. Край
    // обслуживает CRUD, но у консоли для него нет admin-плоскости этого ресурса
    // (`spec.admin` не объявлен), поэтому мутации не выставлены НИКОМУ.
    mutationsNotOffered:
      "Каталог ведёт оператор платформы; admin-плоскости этого ресурса в консоли нет " +
      "(`spec.admin` не объявлен), поэтому мутации не выставлены и администратору.",
    columns: [
      { header: "Имя", path: "name", format: "text", className: "font-medium" },
      // Идентификатор — `uid-short`, а не `text`: этот формат и есть форма
      // идентификатора в продукте (значение плюс копирование одним значком).
      // Прежде здесь стоял `text` с моноширинным классом — идентификатор
      // выглядел похоже и НЕ копировался, тогда как у тома, снимка и образа в
      // том же модуле копировался. Один предмет, два вида (правило 9 канона).
      { header: "Идентификатор", path: "id", format: "uid-short" },
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
      // Зоны — ССЫЛКИ, по одной на зону (правило 2 канона консоли: поле, значение
      // которого есть идентификатор другого ресурса, показывается именем и ведёт
      // на карточку). Множественность от правила не освобождает: это несколько
      // ссылок, а не другой вид значения. До этого здесь стоял `format: "list"`,
      // и в каталоге классов зона была строкой `zone-…`, тогда как у тома, у
      // снимка и в форме выбора — именем: один ресурс, два прочтения на соседних
      // экранах.
      //
      // `projectId` не передаётся намеренно: зона — глобальный справочник geo, у
      // него нет измерения «проект», а страница каталога живёт под `/system/*`,
      // где проекта в контексте нет вовсе.
      {
        header: "Зоны",
        path: "zone_ids",
        render: (row) => {
          const ids = Array.isArray(row.zone_ids) ? (row.zone_ids as string[]).filter(Boolean) : [];
          if (ids.length === 0) return <span className="text-muted-foreground">—</span>;
          return (
            <span style={{ display: "inline-flex", flexWrap: "wrap", gap: 6, minWidth: 0 }}>
              {ids.map((id) => (
                <RefNameLink key={id} specId="zones" refId={id} maxChars={28} />
              ))}
            </span>
          );
        },
      },
    ],
    template: () => ({}),
    emptyState: {
      title: "Каталог типов дисков пуст",
      body: "Класс диска описывает хранилище, на котором создаётся том: ярус, границы размера, способности. Каталог заводит администратор кластера — пока класс не зарегистрирован, том создать нельзя.",
    },
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
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
    route: "compute-zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    accusative: "зону",
    plural: `Зоны (${SERVICES.compute.title})`,
    serviceTitle: SERVICES.compute.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Каталог зон доступности пуст",
      body:
        "Зона — независимая площадка внутри региона, в которой размещаются машины, тома и подсети. " +
        "Записи каталога заводит администратор облака; обратитесь к нему, если список пуст.",
      docs: ["Регионы и зоны доступности"],
    },
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
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
    route: "compute-regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    accusative: "регион",
    plural: `Регионы (${SERVICES.compute.title})`,
    serviceTitle: SERVICES.compute.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Каталог регионов пуст",
      body:
        "Регион — географическая область, объединяющая зоны доступности одной площадки. " +
        "Записи каталога заводит администратор облака; обратитесь к нему, если список пуст.",
      docs: ["Регионы и зоны доступности"],
    },
    columns: [
      {
        header: "Идентификатор",
        path: "id",
        format: "text",
        className: "font-mono",
      },
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
    emptyState: {
      title: "Создайте первую виртуальную машину",
      body:
        "Виртуальная машина — вычислительный узел с загрузочным диском и сетевыми интерфейсами. " +
        "Перед созданием понадобятся подсеть для интерфейса и образ либо том для загрузки.",
      docs: ["Виртуальные машины"],
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
      {
        // Тип машины — запись каталога размера со своей карточкой, значит
        // ссылка, а не моноширинный идентификатор. Рядом в этой же строке зона
        // ссылкой уже была: одна таблица, два поведения читались как «этот
        // переход не сделали» (#406).
        //
        // Две оси, и они РАЗНЫЕ: читается каталог глобально (`scope: "global"`,
        // запрос идёт без project_id), а РАЗДЕЛ его смонтирован внутри проекта,
        // потому что рисует его модуль compute, — поэтому адрес карточки
        // project-scoped.
        header: "Тип машины",
        path: "machine_type_id",
        render: (row) => (
          <RefNameLink specId="machine-types" refId={row.machine_type_id as string | undefined} maxChars={28} />
        ),
      },
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
          "Единый канал размера инстанса (vCPU/память/GPU) — каталог «Типы машин». Сменить размер можно на " +
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
          "публичного IP (Segmented); либо переключитесь на «существующий сетевой интерфейс» (тогда подсеть/SG/" +
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
      // Порядок тот же, что у сервиса: вид — сильный первый дискриминатор, и
      // отвергается он ПО СЕБЕ, а не через источник ОС. Прежде здесь стерёгся
      // только источник, поэтому пара «вид CONTAINER + образ ХРАНИЛИЩА» уходила
      // на сервер и проходила: получалась машина вида «контейнер» с корневой
      // файловой системой из образа диска.
      if (obj.instance_kind === "CONTAINER") {
        return (
          "Вид «контейнер» пока не создаётся: корень контейнера берётся из образа реестра, а у него нет " +
          "неизменяемого адреса — ссылка в машине сломалась бы после чужого переименования. Выберите VM."
        );
      }
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

  // ====== storage: Volume ======
  //
  // Спека ПОЛНАЯ, и приехала она из реестра домена storage — не наоборот.
  // Здесь стояла цель ссылки: без полей формы, без глаголов, с четырьмя
  // колонками. Свести реестры «взяв общее» значило бы снять у арендатора
  // создание тома, правку размера и смену класса, ничего об этом не сказав.
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
    ops: { create: true, update: true, delete: true },
    // Здесь стояло объявление `docs` с адресами `href: "#"` — тем и снято.
    // Адреса у документации в дереве нет ни одного, а ссылка, ведущая на ту же
    // страницу, обещает переход, которого не существует, и обнаруживает это
    // только кликом (правило 9 канона консоли). Читателя объявление не
    // достигало вовсе: `spec.docs` не читает НИ ОДНО место продукта — темы
    // показывает пустое состояние из `emptyState.docs`, и показывает их
    // текстом. То есть объявление было обещанием без исполнителя.
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
      FIELD_NAME_OPTIONAL,
      FIELD_DESCRIPTION,
      {
        name: "zone_id",
        label: "Зона доступности",
        type: "ref",
        refResource: "zones",
        required: true,
        immutable: true,
        description: "Зона размещения тома. Неизменяема после создания.",
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
        // Дискриминатор источника (form-only). У контракта источников РОВНО три
        // (`source_snapshot_id` и `source_image_id` взаимоисключающи, пусто в
        // обоих = чистый том), поэтому форма выражает выбор, а не предлагает
        // заполнить два поля, из которых сервер примет одно.
        //
        // Умолчание — «пустой том»: единственная ветка, которой не нужен предмет
        // в проекте. Открывать форму на ветке, требующей уже существующий
        // снимок, значит встречать свежий проект пустым списком.
        name: "_source_kind",
        label: "Источник данных",
        type: "enum",
        required: true,
        createOnly: true,
        default: "empty",
        options: [
          { value: "empty", label: "Пустой том — без данных" },
          { value: "snapshot", label: "Из снимка" },
          { value: "image", label: "Из образа (Image) — загрузочный том" },
        ],
        description:
          "Чем наполняется том при создании: ничем (пустой), снимком другого тома или образом. Загрузочный том машины делается ИЗ ОБРАЗА — это и есть первый шаг из пустого проекта. Источник неизменяем после создания.",
      },
      {
        name: "source_snapshot_id",
        label: "Снимок-источник",
        type: "ref",
        refResource: "snapshots",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        immutable: true,
        visibleWhen: { field: "_source_kind", equals: "snapshot" },
        description: "Снимок, из которого восстанавливается том. Задаётся при создании и потом не меняется.",
      },
      {
        // Образ — вход в цепочку «образ → том → машина». Без него из свежего
        // проекта загрузочный том не получить вовсе: снимок делается из тома, а
        // образ — из тома или снимка, то есть круг замкнут сам на себя.
        name: "source_image_id",
        label: "Образ-источник",
        type: "ref",
        refResource: "images",
        refProjectScoped: true,
        required: true,
        createOnly: true,
        immutable: true,
        visibleWhen: { field: "_source_kind", equals: "image" },
          description: "Образ, из которого создаётся загрузочный том. Задаётся при создании и потом не меняется.",
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
      _source_kind: "empty",
      source_snapshot_id: "",
      source_image_id: "",
      labels: {},
    }),
    // size_gib (UI) → size_bytes (wire); ровно одна ветка источника по
    // `_source_kind`, form-only дискриминатор срезаем.
    //
    // Неактивная ветка режется ПО ДИСКРИМИНАТОРУ, а не по пустоте значения:
    // пользователь мог выбрать образ и затем переключиться на снимок, и тогда
    // непустой `source_image_id` уехал бы вместе со снимком — сервер отверг бы
    // взаимоисключающую пару, назвав поле, которого в форме уже не видно.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const gib = Number(out.size_gib);
      if (Number.isFinite(gib) && gib > 0) out.size_bytes = String(Math.round(gib) * GIB);
      delete out.size_gib;
      const kind = out._source_kind;
      delete out._source_kind;
      if (kind === "snapshot") {
        delete out.source_image_id;
        if (!out.source_snapshot_id) delete out.source_snapshot_id;
      } else if (kind === "image") {
        delete out.source_snapshot_id;
        if (!out.source_image_id) delete out.source_image_id;
      } else {
        delete out.source_snapshot_id;
        delete out.source_image_id;
      }
      return out;
    },
    // Клиент-валидация ДО submit: активный источник должен быть выбран. Ветка
    // «пустой том» предмета не имеет и проходит без выбора — иначе проверка
    // отказывала бы всегда и её отрицание зеленело бы на чём угодно.
    validate: (obj) => {
      const kind = obj._source_kind;
      if (kind === "image" && !obj.source_image_id) return "Выберите образ, из которого создаётся том.";
      if (kind === "snapshot" && !obj.source_snapshot_id)
        return "Выберите снимок, из которого восстанавливается том.";
      return null;
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
      docs: ["Тома (блочное хранение)"],
    },
  },

  // ====== storage: Snapshot ======
  //
  // Спеки снимка в общем реестре не было ВОВСЕ, и это стоило двух форков сразу
  // (#1466): оболочка карточки резолвит спеку по маршруту здесь, поэтому домен
  // был обязан держать и свою оболочку, и свой реестр. Наблюдаемо было третье:
  // колонка «Источник» на карточке образа ссылается на снимок либо на том — и
  // на снимке ссылка вырождалась в плоский идентификатор, тогда как на томе в
  // той же колонке работала.
  snapshots: {
    id: "snapshots",
    route: "snapshots",
    apiPath: "/storage/v1/snapshots",
    payloadKey: "snapshots",
    singular: ENTITIES.snapshots.singular,
    accusative: "снимок",
    plural: ENTITIES.snapshots.plural,
    genitive: "Снимка",
    serviceTitle: SERVICES.storage.title,
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
        description: "Том, с которого снимается копия на момент времени. Неизменяем после создания.",
      },
      FIELD_NAME_OPTIONAL,
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

  // ====== storage: Image ======
  //
  // Спека ПОЛНАЯ (перенесена из реестра домена storage). Здесь стояла цель
  // ссылки для подборщика образа в форме машины — без полей формы и без
  // выбора источника.
  images: {
    id: "images",
    route: "images",
    apiPath: "/storage/v1/images",
    payloadKey: "images",
    singular: ENTITIES.images.singular,
    accusative: "образ",
    plural: ENTITIES.images.plural,
    genitive: "Образа",
    serviceTitle: SERVICES.storage.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
    // Здесь стояло объявление `docs` с адресами `href: "#"` — тем и снято.
    // Адреса у документации в дереве нет ни одного, а ссылка, ведущая на ту же
    // страницу, обещает переход, которого не существует, и обнаруживает это
    // только кликом (правило 9 канона консоли). Читателя объявление не
    // достигало вовсе: `spec.docs` не читает НИ ОДНО место продукта — темы
    // показывает пустое состояние из `emptyState.docs`, и показывает их
    // текстом. То есть объявление было обещанием без исполнителя.
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
      FIELD_NAME_OPTIONAL,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        description: "Регион размещения образа. Образ доступен из всего региона; неизменяем после создания.",
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
          { value: "snapshot", label: "Из снимка" },
          { value: "volume", label: "Из тома" },
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
        description: "Снимок, из которого создаётся образ. Неизменяем после создания.",
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
        description: "Том, из которого создаётся образ. Неизменяем после создания.",
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
      docs: ["Образы (загрузочные)"],
    },
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
        multiline: true,
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
    emptyState: {
      title: "Каталог типов машин пуст",
      // Размер назван ЦЕЛИКОМ — включая ускорители: две редакции этого
      // объяснения разошлись ровно там же, где разошлись колонки, и упоминала
      // ускорители та, что стояла у форка модуля.
      body:
        "Тип машины задаёт размер виртуальной машины — число ядер, объём памяти и графические ускорители. " +
        "Каталог заводит администратор облака: обратитесь к нему, если ни одного типа не видно.",
      docs: ["Типы машин"],
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
      // Модель ускорителя — то, чем два типа с одинаковым числом GPU отличаются
      // друг от друга: без неё выбор из каталога делается вслепую. Колонку
      // показывал ТОЛЬКО форк реестра модуля compute, поэтому одна и та же
      // запись каталога выглядела по-разному в двух местах продукта (#406).
      // Держит `resource-registry.machine-type-parity.test.tsx` — он читает
      // контракт, а не этот перечень.
      { header: "GPU-модель", path: "effective_resources.gpu_type", format: "code" },
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
        multiline: true,
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
      COL_CREATED,
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
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
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
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
    // Колонка идентичности — `id`: подписи-дубля у каталога размещения нет (#716).
    // Идентификатор назначает администратор, он человекочитаем by construction,
    // и по нему же строится переход на карточку (первая колонка становится
    // колонкой идентичности, см. buildSpecColumns).
    columns: [
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
    template: () => ({ id: "", country_code: "", status: "DOWN", infra: { numeric_infra_id: "" } }),
    sanitize: (obj) => sanitizeGeoCommon(obj),
    hydrate: (obj) => obj,
    emptyState: {
      title: "Каталог регионов пуст",
      body: "Регион — верхний уровень оси размещения. Пока в каталоге нет ни одного региона, разместить нельзя ничего: zoneId и regionId берутся отсюда.",
    },
  },

  zones: {
    id: "zones",
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
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
    // Колонка идентичности — `id` (#716, см. регионы выше).
    columns: [
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
    emptyState: {
      title: "Создайте первый пул адресов",
      body:
        "Пул адресов — диапазон, из которого арендаторам выдаются публичные адреса. " +
        "Пока пула нет, выделить внешний адрес не из чего, и создание адреса завершится отказом.",
      docs: ["Пулы публичных адресов"],
    },
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
      // Булево — следствием, а не «true»: с форматом `text` эта колонка печатала
      // пользователю служебное слово (правило 6 `ui.md`).
      {
        header: "По умолчанию",
        path: "is_default",
        format: "bool",
        boolLabels: { yes: "Пул по умолчанию", no: "Обычный пул" },
      },
      {
        header: "Метки селектора",
        path: "selector_labels",
        multiline: true,
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
        placeholder: "my-resource",
        description: NAME_HINT_OPTIONAL,
        pattern: NAME_FORM,
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
    // Действий-глаголов у балансировщика нет: `:start`/`:stop` сняты с контракта,
    // административное включение/выключение выражается полем admin_state.
    ops: { create: true, update: true, delete: true },
    emptyState: {
      title: "Создайте первый балансировщик нагрузки",
      body:
        "Балансировщик нагрузки принимает трафик на VIP-адрес и разносит его между целями внутри " +
        "региона Kachō. Дальше к нему добавляют обработчики — они задают протокол и порт приёма.",
      docs: ["Балансировщики нагрузки"],
    },
    // Обработчики — связанный дочерний ресурс (within-service FK
    // `load_balancer_id`): registry-driven вкладка карточки + призыв «создать».
    // Без записи путь «завёл балансировщик → завёл обработчик» из консоли не
    // проходится вовсе. Целевые группы одним `filterField` не выражаются (связь
    // идёт ЧЕРЕЗ обработчик) — их вкладку подаёт расширение карточки.
    related: [{ childId: "listeners", filterField: "load_balancer_id", label: ENTITIES.listeners.plural }],
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
        // Якорь размещения рисует единственный `PlacementAnchor` (правило 2
        // канона консоли): зональный балансировщик ведёт на СВОЮ зону,
        // региональный — на регион. Оба якоря суть ресурсы каталога geo со
        // своими карточками, поэтому это ссылка, а не плоский текст.
        //
        // Здесь стояла колонка «Регион», и она отвечала не на тот вопрос:
        // регион у ЛЮБОГО балансировщика есть, а площадку зонального она
        // назвать не могла. Ветвь ZONAL общего якоря была недостижима by
        // construction — контракт зоны не нёс вовсе (#1473).
        header: "Размещение",
        path: "placement_type",
        render: (row) => <PlacementAnchor row={row} maxChars={28} />,
      },
      // Схема (`type`) — производная проекция размещения, и именно она отвечает
      // на вопрос «внешний он или внутренний», с которого начинают чтение списка.
      { header: "Схема", path: "type", format: "code" },
      {
        // VIP резолвится в связанный vpc Address; ячейка показывает САМ адрес и
        // ведёт на его карточку. Без неё список молчит о том, куда идёт трафик.
        header: "Адрес",
        path: "v4_address_id",
        render: (row) => (
          <NlbVipCell
            v4AddressId={row.v4_address_id as string | undefined}
            v6AddressId={row.v6_address_id as string | undefined}
          />
        ),
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        multiline: true,
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
      FIELD_NAME_OPTIONAL, // DNS-1123 — lowercase + цифры + дефисы (как у NLB regex)
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        // Create-only: UpdateNetworkLoadBalancerRequest его не несёт.
        immutable: true,
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        description: "Регион размещения балансировщика. Неизменяем после создания.",
      },
      {
        // Источник VIP (per-family oneof v4_source/v6_source) — интерактивный
        // выбор: пофамильно (v4/v6) подсеть/адрес/публичный/не задавать; в правке
        // источник неизменяем (read-only резолвнутый Address). sanitize собирает
        // wire-oneof.
        name: "vip_source",
        label: "Источник VIP",
        type: "custom",
        immutable: true,
        render: ({ value, onChange, editMode }) => (
          <NlbVipSourceField value={value} onChange={onChange} editMode={editMode} />
        ),
      },
      {
        name: "disabled_announce_zones",
        label: "Зоны без анонса",
        type: "custom",
        // Drain только для REGIONAL; mutable через Update. fullWidth:false — label
        // слева (как обычное поле), multi-select зон справа.
        fullWidth: false,
        // Гейт по `placement` — единственному, что форма несёт. `placement_type`
        // объект формы не содержит (write-reject проекция), поэтому условие по
        // нему не выполнялось бы никогда и поле было бы недостижимо.
        visibleWhen: { field: "placement", equals: ["EXTERNAL_REGIONAL", "INTERNAL_REGIONAL"] },
        description:
          "Зоны, из которых anycast-VIP не анонсируется (drain). Пусто — анонс из всех здоровых зон региона.",
        render: ({ value, onChange }) => <NlbDisabledZonesField value={value} onChange={onChange} />,
      },
      {
        name: "session_affinity",
        label: "Привязка сессий",
        type: "enum",
        default: "FIVE_TUPLE",
        options: [
          { value: "FIVE_TUPLE", label: "По пяти полям (src ip+port, dst ip+port, proto)" },
          { value: "CLIENT_IP_ONLY", label: "Только по адресу клиента" },
        ],
        description:
          "Привязка соединений к цели: FIVE_TUPLE — по 5-tuple, CLIENT_IP_ONLY — только по IP клиента. Control-plane намерение (распределение трафика — data-plane).",
      },
      {
        name: "admin_state",
        label: "Административное состояние",
        type: "enum",
        // Значения несут префикс перечисления: у `AdminState` он объявлен в
        // контракте (`ADMIN_STATE_ENABLED`), в отличие от соседних `Placement`
        // и `SessionAffinity`, где значения объявлены без него. Короткая форма
        // отвергается краем — `invalid value for enum field adminState`.
        default: "ADMIN_STATE_ENABLED",
        options: [
          { value: "ADMIN_STATE_ENABLED", label: "ENABLED — принимает трафик" },
          { value: "ADMIN_STATE_DISABLED", label: "DISABLED — выключен администратором" },
        ],
        description:
          "Желаемое административное состояние. Выключение — не удаление: ресурс и его VIP сохраняются, приём трафика прекращается.",
      },
      {
        name: "cross_zone_enabled",
        label: "Межзональная балансировка",
        type: "bool",
        default: false,
        // REGIONAL-only: у ZONAL-балансировщика зона одна, и сервис отвечает на
        // `true` явным InvalidArgument. Поле скрыто по тому же `placement`, что и
        // зоны без анонса, а sanitize снимает его для ZONAL — чтобы скрытое поле
        // не уезжало телом, которое сервис отвергнет.
        visibleWhen: { field: "placement", equals: ["EXTERNAL_REGIONAL", "INTERNAL_REGIONAL"] },
        description: "Разносить трафик по целям всех зон региона. Применимо только к региональному размещению.",
      },
      {
        name: "security_group_ids",
        label: "Группы безопасности",
        type: "array",
        itemLabel: "SG",
        // INTERNAL-only: группы безопасности живут в сети, и сервис отвечает на
        // набор при внешнем размещении явным InvalidArgument.
        visibleWhen: { field: "placement", equals: ["INTERNAL_REGIONAL", "INTERNAL_ZONAL"] },
        description:
          "Опционально. Ограничивают доступ к VIP балансировщика. Только для внутреннего размещения; набор заменяется целиком.",
        newItem: () => ({ value: "" }),
        itemFields: [
          {
            name: "value",
            label: "Группа безопасности",
            type: "ref",
            refResource: "security-groups",
            refProjectScoped: true,
            required: true,
          },
        ],
      },
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        default: false,
        description: "Если включена, балансировщик нельзя удалить, пока защита не снята.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      placement: "EXTERNAL_REGIONAL",
      session_affinity: "FIVE_TUPLE",
      admin_state: "ADMIN_STATE_ENABLED",
      cross_zone_enabled: false,
      security_group_ids: [],
      deletion_protection: false,
      disabled_announce_zones: [],
      // vip_source — UI-представление источника VIP per-family (NlbVipSourceField).
      //
      // IPv4 — в авто-режиме своей схемы («из подсети» для INTERNAL,
      // нормализуется в «публичный» для EXTERNAL). IPv6 — ЯВНО не задаётся:
      // двойной стек включается по решению арендатора, а не по умолчанию.
      //
      // Прежде оба семейства стояли в режиме «из подсети». Для INTERNAL это
      // означало «пусто → семейство опущено», а для EXTERNAL (умолчание
      // размещения!) режим схлопывался в «публичный», который источник даёт
      // ВСЕГДА, — то есть внешний балансировщик по умолчанию уезжал с ОБОИМИ
      // семействами, и отказаться от одного было нечем.
      vip_source: {
        _v4_mode: "subnet",
        v4: { subnet_id: "", address_id: "" },
        _v6_mode: "off",
        v6: { subnet_id: "", address_id: "" },
      },
      labels: {},
    }),
    // Клиент-валидация ДО submit: источник VIP должен быть задан хотя бы для
    // одного семейства (IPv4/IPv6) — иначе backend отвергнет InvalidArgument.
    validate: (obj) => {
      const type = lbTypeFromPlacement(obj.placement as string | undefined);
      const vs = (obj.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(
        type,
        vs._v4_mode as string | undefined,
        vs.v4 as Record<string, unknown> | undefined,
      );
      const v6 = buildVipSourceOrNull(
        type,
        vs._v6_mode as string | undefined,
        vs.v6 as Record<string, unknown> | undefined,
      );
      if (!v4 && !v6) {
        return "Укажите источник VIP хотя бы для одного семейства (IPv4 или IPv6).";
      }
      return null;
    },
    // Собирает per-family oneof v4_source/v6_source из UI-представления
    // (NlbVipSourceField): семейство эмитится, только если у активного режима
    // есть значение (buildVipSourceOrNull ≠ null) — пустой addressId/subnetId
    // никогда не уходит на бэкенд. Ветвь oneof нормализуется под РЕЖИМ, а не под
    // то, что осталось в виджете: subnet_id валиден только для INTERNAL, public —
    // только для EXTERNAL (validateSourceTypeMatrix на стороне сервиса), поэтому
    // подсеть, выбранная в INTERNAL-черновике, после переключения на EXTERNAL
    // схлопывается в public, а не уезжает отвергаемым телом.
    //
    // Поля, чьё применение сервис ограничивает размещением, снимаются здесь же —
    // скрытое поле иначе уезжает телом, на которое приходит явный отказ:
    // disabled_announce_zones и cross_zone_enabled — REGIONAL-only,
    // security_group_ids — INTERNAL-only.
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const placement = out.placement as string | undefined;
      const type = lbTypeFromPlacement(placement);

      const vs = (out.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(
        type,
        vs._v4_mode as string | undefined,
        vs.v4 as Record<string, unknown> | undefined,
      );
      const v6 = buildVipSourceOrNull(
        type,
        vs._v6_mode as string | undefined,
        vs.v6 as Record<string, unknown> | undefined,
      );
      if (v4) out.v4_source = v4;
      if (v6) out.v6_source = v6;
      delete out.vip_source;

      if (lbPlacementTypeFromPlacement(placement) !== "REGIONAL") {
        delete out.disabled_announce_zones;
        delete out.cross_zone_enabled;
      }
      // Набор SG на проводе — `repeated string`, а в форме элемент объектом
      // (`{value}`): перевод один на всё дерево (`flatIdList`). Пустой набор в
      // тело не уезжает — пустой массив утверждал бы «ни одной группы», тогда
      // как арендатор чаще просто не дошёл до поля.
      if (type !== "INTERNAL") {
        delete out.security_group_ids;
      } else {
        const sgs = flatIdList(out.security_group_ids);
        if (sgs) out.security_group_ids = sgs;
        else delete out.security_group_ids;
      }

      return out;
    },
    // Обратное преобразование (провод → форма): сервис возвращает список строк,
    // а `ArrayField` держит элемент объектом. Без него в правке список групп
    // приезжает строками и `RefSelect` не показывает ни одного имени.
    hydrate: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      hydrateStringListFields(out, ["security_group_ids"]);
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
    emptyState: {
      title: "Создайте первый обработчик",
      body:
        "Обработчик — точка приёма трафика балансировщика: протокол и порт, на которых он слушает. " +
        "Обработчик указывает целевую группу, и с него начинается путь запроса к машинам.",
      docs: ["Обработчики"],
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
        header: "Балансировщик",
        path: "load_balancer_id",
        render: (row) => (
          <RefNameLink specId="load-balancers" refId={row.load_balancer_id as string | undefined} maxChars={36} />
        ),
      },
      { header: "Протокол", path: "protocol", format: "code" },
      { header: "Порт", path: "port", format: "text" },
      // `resolved_backend_port` — эхо порта привязанной группы целей. Без него
      // список говорит, КУДА трафик приходит, и молчит о том, куда он уходит.
      { header: "Порт на цели", path: "resolved_backend_port", format: "text" },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
    ],
    fields: [
      FIELD_NAME_OPTIONAL,
      FIELD_DESCRIPTION,
      {
        name: "load_balancer_id",
        // Create-only: UpdateListenerRequest его не несёт.
        immutable: true,
        label: "Балансировщик",
        type: "ref",
        refResource: "load-balancers",
        refProjectScoped: true,
        required: true,
        description: "Балансировщик, которому принадлежит обработчик. Неизменяем после создания.",
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
        min: 1,
        max: 65535,
        description: "Порт, на котором обработчик принимает входящий трафик (1..65535). Неизменяем после создания.",
      },
      {
        name: "default_target_group_id",
        label: "Целевая группа по умолчанию",
        type: "ref",
        refResource: "target-groups",
        refProjectScoped: true,
        required: false,
        description:
          "Целевая группа, принимающая трафик. Привязка живёт ЗДЕСЬ: у балансировщика собственной привязки к группе нет.",
      },
      FIELD_LABELS,
    ],
    template: () => ({
      name: "",
      description: "",
      load_balancer_id: "",
      protocol: "TCP",
      // Порта на цели здесь нет: он живёт на группе целей и приходит обратно
      // вычисляемым `resolved_backend_port`.
      default_target_group_id: "",
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
    emptyState: {
      title: "Создайте первую целевую группу",
      body:
        "Целевая группа — набор адресатов, между которыми балансировщик делит соединения. " +
        "Состояние каждой цели проверяется, и отказавшая исключается из раздачи до восстановления.",
      docs: ["Целевые группы"],
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
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        multiline: true,
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
      FIELD_NAME_OPTIONAL,
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
      ...healthCheckFields(),
      targetsField(),
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
    // проверки живости и ветвь идентичности каждой цели: форма держит поля всех
    // ветвей, в теле остаётся по одной.
    sanitize: (obj) => {
      const out: Record<string, unknown> = sanitizeTargets(sanitizeHealthCheck(obj));
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

  // ====== registry (Container Registry) ======
  //
  // Записи перенесены сюда из модульного реестра `registry/src/lib` (#409). До
  // переноса общий реестр их не нёс, а спеку по идентификатору резолвят ОБЩИЕ
  // оболочка карточки, подборщик ссылок и `RefNameLink` — поэтому ссылка на
  // реестр из соседнего раздела вырождалась в плоский идентификатор, а раздел
  // `/registry/*` был обязан держать и свой реестр, и свою оболочку сразу.
  //
  // proto: kacho.cloud.registry.v1. Registry (реестр, tenant-facing) →
  // Repository (появляется при docker push, read-only) → Tag (тег образа;
  // единственная мутация — DeleteTag, async).

  registries: {
    id: "registries",
    route: "registries",
    apiPath: "/registry/v1/registries",
    payloadKey: "registries",
    singular: ENTITIES.registries.singular,
    accusative: "реестр",
    plural: ENTITIES.registries.plural,
    genitive: "Реестра",
    serviceTitle: SERVICES.registry.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      // REG-1 F4: реестр — REGIONAL-anycast, якорь размещения его региона.
      //
      // Правило 2 канона консоли: якорь рисует единственный `PlacementAnchor` —
      // регион есть ресурс каталога geo со своей карточкой, поэтому показывается
      // ссылкой (иконка типа + имя + переход). Плоский идентификатор, стоявший
      // здесь, не давал ни имени, ни перехода, при том что соседняя колонка
      // «Имя» ссылкой уже была.
      {
        header: "Размещение",
        path: "region_id",
        render: (row) => <PlacementAnchor row={row} maxChars={28} />,
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Репозиториев", path: "repository_count", format: "text" },
      { header: "Адрес", path: "endpoint", format: "code" },
      COL_CREATED,
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_REGISTRY,
      FIELD_DESCRIPTION,
      // REG-1 F4: region_id — required + immutable, cross-service ref → geo.Region
      // (REGIONAL-anycast placement, peer-validate fail-closed). Смена региона
      // сломала бы storage-locality блобов → immutable после Create.
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "regions",
        required: true,
        immutable: true,
        description: "Регион размещения реестра. Реестр доступен из всего региона; неизменяем после создания.",
      },
      // REG-1 F5: default_repository_visibility — видимость по умолчанию для
      // новых репозиториев реестра. PUBLIC требует прав администратора реестра
      // (проверяется на сервере: any-path-to-PUBLIC admin-gate).
      {
        name: "default_repository_visibility",
        label: "Видимость репозиториев по умолчанию",
        type: "enum",
        // CreateRegistryRequest этого поля не несёт (только Update, тег 6) —
        // выбранное при создании PUBLIC край выбрасывал, и реестр получался
        // PRIVATE с успешным тостом.
        updateOnly: true,
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
      labels: {},
    }),
    emptyState: {
      title: "Создайте первый реестр",
      body: "Реестр хранит контейнерные образы проекта. После создания выполните docker login к endpoint реестра и docker push — репозитории появятся автоматически.",
      docs: ["Реестры контейнеров", "Публикация образов (docker login / push)"],
    },
  },

  // ====== repository (OCI-репозиторий) ======
  //
  // ЗДЕСЬ СТОЯЛО «репозитории НЕ создаются через API… Мутаций нет» — и это было
  // НЕВЕРНО о продукте (#1593). Контракт несёт `CreateRepository`,
  // `UpdateRepository`, `DeleteRepository` и `RenameRepository`, все с публичной
  // привязкой REST под `/registry/v1/registries/{}/repositories`. Утверждение
  // консоли повторяло страницу документации, страница повторяла консоль, и обе
  // расходились с контрактом — три поверхности, из которых верна одна.
  //
  // Цена была не теоретической: `visibility` репозитория — ЕДИНСТВЕННЫЙ рычаг,
  // делающий образ публичным, и меняет его именно `UpdateRepository`. Путь
  // «опубликовать образ» через `docker push` заканчивался приватным
  // репозиторием без выхода.
  //
  // Tenant-facing термин — «репозиторий» (id/route/apiPath/payloadKey =
  // repositories по OCI/REST-контракту).

  repositories: {
    id: "repositories",
    route: "repositories",
    // registryId подставляется из родителя (реестра); прямой fetch —
    // registriesApi.listRepositories(registryId) (см. registry/src/api/resources.ts).
    apiPath: "/registry/v1/registries/{registryId}/repositories",
    payloadKey: "repositories",
    singular: ENTITIES.repositories.singular,
    accusative: "репозиторий",
    plural: ENTITIES.repositories.plural,
    genitive: "Репозитория",
    serviceTitle: SERVICES.registry.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
    // ПОЧЕМУ «нет», хотя край обслуживает все три глагола, — препятствие
    // измерено, а не предположено. Репозиторий адресуется ПАРОЙ «реестр + имя»
    // (`{registryId}` + `{repository=**}`), тогда как общая оболочка мутаций
    // строит адрес как `apiPath + "/" + row.id`: у репозитория поля `id` нет
    // вовсе (`registry.proto`, message Repository), а `{registryId}` в `apiPath`
    // на пути мутации никем не подставляется — подстановка сегодня живёт только
    // на пути СПИСКА (`related-list-query`). То есть выставить глаголы значит
    // сперва научить оболочку адресовать строку по объявленному ключу, а не по
    // `id`; это работа над общей оболочкой, и она заведена своим предметом.
    //
    // Переименование сверх того требует СВОЕЙ церемонии, а не поля формы: имя
    // репозитория стоит в pull-пути (`$domain/$registryId/$repo:$tag`), движок
    // перевешивает теги и манифесты, и старое имя после этого отвечает 404 —
    // ломается каждый уже написанный `docker pull`. Механизм действий-глаголов
    // консоли (`RowVerb`) несёт подтверждение, но не несёт ввода нового
    // значения, поэтому и он не подходит как есть.
    mutationsNotOffered:
      "Край обслуживает создание, правку и удаление, но общая оболочка адресует строку по " +
      "полю `id`, которого у репозитория нет: его ключ — пара «реестр + имя». Выставить " +
      "глаголы можно только вместе с адресацией по объявленному ключу. Переименование " +
      "сверх того ломает каждый pull-путь и требует своей церемонии.",
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
      // REG-1 F7: класс исчезаемости (DURABLE survives-empty / EPHEMERAL push-materialized).
      {
        header: "Класс",
        path: "lifecycle",
        render: (row) => <RepositoryLifecycleTag value={row.lifecycle} />,
      },
      // REG-1 F5: видимость репозитория (PRIVATE / PUBLIC anonymous-pull).
      { header: "Видимость", path: "visibility", render: (row) => <VisibilityTag value={row.visibility} /> },
      { header: "Тегов", path: "tag_count", format: "text" },
      // size_bytes — агрегат по репозиторию (int64 строкой) → человекочитаемо;
      // 0/пусто → «—» (никогда «0 B»).
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
      // updated_at — время последнего push (last pushed) в репозиторий.
      { header: "Обновлён", path: "updated_at", format: "datetime" },
    ],
    // Формы нет, пока консоль не выставила глаголы (см. `mutationsNotOffered`).
    template: () => ({}),
    emptyState: {
      title: "В этом реестре пока нет репозиториев",
      // ЗДЕСЬ СТОЯЛО «Репозитории появляются автоматически» — заголовок учил
      // клиента тому же неверному утверждению, что и снятый комментарий выше:
      // будто push есть единственный путь. Он не единственный, он лишь
      // единственный, который сегодня даёт КОНСОЛЬ.
      body:
        "Самый короткий путь — docker login к endpoint реестра и docker push: репозиторий " +
        "появится здесь сразу после первой загрузки образа.",
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
    // registriesApi.listTags(registryId, repository) (см. registry/src/api/resources.ts).
    apiPath: "/registry/v1/registries/{registryId}/repositories/{repository}/tags",
    payloadKey: "tags",
    singular: ENTITIES.tags.singular,
    accusative: "тег",
    plural: ENTITIES.tags.plural,
    genitive: "Тега",
    serviceTitle: SERVICES.registry.title,
    scope: "project",
    // DeleteTag — единственная мутация (create/update нет: теги пишет docker push).
    ops: { create: false, update: false, delete: true },
    columns: [
      { header: "Тег", path: "tag", format: "text" },
      { header: "Дайджест", path: "digest", format: "code" },
      // Размер тега — тот же `size_bytes` того же домена, что у репозитория, и
      // потому тот же вид (правило 3 канона консоли, #1509). До переноса тег
      // показывал сырой `int64`, а соседний репозиторий — человекочитаемо;
      // перенос сохранил обе формы ДОСЛОВНО, чтобы остаться проверяемым
      // сравнением, и различие чинится здесь, своим заходом. Держит проба
      // `resource-registry.size-parity.test.tsx`: она сверяет ТЕКСТ ячейки
      // обеих спек, а не имя компонента.
      { header: "Размер", path: "size_bytes", render: (row) => <SizeCell value={row.size_bytes} /> },
      { header: "Тип содержимого", path: "media_type", format: "text" },
      COL_CREATED,
    ],
    // Мутаций create/update нет — form-schema не требуется.
    template: () => ({}),
    emptyState: {
      title: "Теги появляются после docker push",
      body:
        "Тег — версия образа в репозитории: имя, за которым стоит манифест и его дайджест. " +
        "В консоли теги не создаются — выполните docker push в этот репозиторий, и тег появится в списке.",
      docs: ["Публикация образов (docker login / push)"],
    },
  },
};

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

/**
 * Вид цели правила → поле контракта. ЕДИНСТВЕННЫЙ перечень ветвей `oneof target`
 * в консоли; сверяется с контрактом пробой `oneof-form-coverage`.
 *
 * Здесь стояла четвёртая запись — `predefined: "predefined_target"`. Эта ветвь
 * СНЯТА с контракта (`security_group_service.proto`: номер и имя в `reserved`),
 * то есть форма умела назвать цель, которой у контракта нет: край такой ключ
 * молча выбрасывает, и правило уезжало бы без цели вовсе. Ветвь, которой нет у
 * контракта, — находка того же рода, что ветвь контракта без формы, только с
 * другой стороны (#375).
 */
export const SG_RULE_TARGET_FIELD: Record<string, string> = {
  cidr: "cidr_blocks",
  sg: "security_group_id",
  "cidr-group": "cidr_group_id",
};

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
    // Ветвь выбрана, номер не назван: ключ со значением `undefined` не переживёт
    // сериализацию тела, ветвь исчезнет, и правило будет означать «любой
    // протокол» — расширение, о котором никто не просил. Ноль сервер отвергает
    // ЯВНО, называя поле; форма показывает этот отказ подписью «Номер IANA».
    if (!hasProtocolNumber(out.protocol_number)) out.protocol_number = 0;
  }
  // ports
  if (portsAny) {
    delete out.ports;
  }
  // target oneof — оставляем ровно одну ветвь. Список ветвей ведётся ОДНИМ
  // перечнем, а не тремя ветками `if`: ветвь, забытая в одной из веток, уезжает
  // вместе с выбранной, и сервис отвергает правило целиком («ровно одна цель»).
  const keep = SG_RULE_TARGET_FIELD[targetKind];
  for (const [kind, field] of Object.entries(SG_RULE_TARGET_FIELD)) {
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
