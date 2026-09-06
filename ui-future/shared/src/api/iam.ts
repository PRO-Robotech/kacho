// IAM API types + helpers — flat resources verbatim из kacho.cloud.iam.v1.
// URL-ы из google.api.http annotations в kacho-proto/proto/kacho/cloud/iam/v1/*.
//
// Все мутации возвращают Operation envelope (см. operation.proto).
// Список ресурсов:
//   - /iam/v1/accounts              (AccountService)
//   - /iam/v1/projects              (ProjectService; require account_id)
//   - /iam/v1/users                 (UserService; read+delete only)
//   - /iam/v1/serviceAccounts       (ServiceAccountService; require account_id)
//   - /iam/v1/groups                (GroupService; require account_id; +addMember/removeMember/listMembers)
//   - /iam/v1/roles                 (RoleService; system + custom)
//   - /iam/v1/accessBindings        (AccessBindingService; Create/Delete/Get + listByScope/listBySubject)
//
// E0 (текущая фаза): без auth-interceptor; UI шлёт запросы без Bearer
// (api-gateway допускает анонимный доступ). Operations.principal_* — пусто/stub.

import { api } from "./client";
import type { CredentialKind } from "@shared/lib/tokens-util";
import { snakeToCamelPath } from "@shared/lib/update-mask";

// ====== IAM-1 redesign: dotted tier / scope discriminators ======
// Снятие путаницы «scope»/«tier»: Role.definitionTier несёт dotted tierType +
// anchor id; AccessBinding.scopeType — dotted anchor-tier. Значения — закрытый
// dotted-словарь (iam.account | iam.project | iam.cluster). tierType==iam.cluster
// ⇒ system-роль (isSystem° derived). См. acceptance IAM-1 F4/F7.
export type TierType = "iam.account" | "iam.project" | "iam.cluster";
export type IamScopeType = "iam.account" | "iam.project" | "iam.cluster";

// DefinitionTier — wire-проекция «где определена роль» {tierType,tierId} над
// типизированными FK+CHECK-XOR (ровно один anchor). Backend gateway отдаёт
// camelCase; читаем обе формы для устойчивости.
export interface DefinitionTier {
  tier_type?: TierType;
  tierType?: TierType;
  tier_id?: string;
  tierId?: string;
}

// ====== Account ======
export interface Account {
  id: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  // ownerUserId° — output-only, derived-from-caller (НЕ принимается в Create-body,
  // immutable в Update). Зеркало субъекта owner-AccessBinding. IAM-1 F1.
  owner_user_id?: string;
  ownerUserId?: string;
  // IAM-1: защита от удаления + жизненный статус аккаунта (output-only).
  deletion_protection?: boolean;
  deletionProtection?: boolean;
  status?: string;
  created_at?: string;
}
export interface AccountList {
  accounts: Account[];
  next_page_token?: string;
}

// ====== Project ======
export interface Project {
  id: string;
  account_id?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  created_at?: string;
}
export interface ProjectList {
  projects: Project[];
  next_page_token?: string;
}

// ====== User (KAC-125: per-Account + invite-status) ======
export type InviteStatus = "PENDING" | "ACTIVE" | "BLOCKED";

export interface User {
  id: string;
  external_id?: string;
  email?: string;
  display_name?: string;
  created_at?: string;
  // Аккаунта у пользователя больше нет (kacho#471): принадлежность выражается
  // членствами, которых у человека может быть несколько, и край это поле не
  // отдаёт — номер и имя зарезервированы в контракте. Оставить его объявленным
  // значило бы обещать значение, которого в ответе не бывает.
  invite_status?: InviteStatus;
  invited_by?: string;
}
export interface UserList {
  users: User[];
  next_page_token?: string;
}

export interface InviteUserRequest {
  account_id: string;
  email: string;
  display_name?: string;
  project_id?: string;
  role_id?: string;
}

// ====== Членство человека в аккаунте (MembershipService) ======
//
// Принадлежность — ОТДЕЛЬНАЯ связь, а не поле человека: людей и аккаунты
// связывает «многие ко многим», и поле на записи человека умело бы назвать лишь
// один аккаунт. Ровно поэтому оно снято с контракта (#471), а читаемая
// принадлежность вернулась своим ресурсом (#1085).
//
// Аккаунт стоит В ПУТИ обоих чтений, а не термом фильтра, и это несущая часть
// контракта, а не форма адреса: вопроса «в каких аккаунтах состоит этот
// человек» на поверхности НЕТ ВОВСЕ. Параметра для него не существует, значит
// задать его по ошибке — в новой ветке, в оптимизации, «права уже проверены
// выше» — нельзя by construction.

/** Состояний ровно два, и это закреплено конструкцией хранилища. */
export type MembershipState = "PENDING" | "ACTIVE";

export interface Membership {
  id: string;
  account_id: string;
  /** Зеркало имени аккаунта, best-effort. Пусто — имя не задано, а НЕ «аккаунта нет». */
  account_name?: string;
  user_id: string;
  state?: MembershipState;
  /** След приглашения, переживающий вход. Пусто у членства, заведённого не приглашением. */
  invited_by?: string;
  created_at?: string;
  updated_at?: string;
}

export interface MembershipList {
  memberships: Membership[];
  next_page_token?: string;
}

/**
 * Путь строится ЗДЕСЬ, в поверхности API домена, по той же причине, что и у
 * глаголов человека выше: перепись путей консоли резолвит голову литерала в
 * объявление `IAM` и сверяет получившийся путь с `google.api.http` контракта.
 * Собранный «на месте» путь ушёл бы из-под этого надзора.
 */
export function accountMembershipsPath(accountId: string): string {
  return `${IAM.accounts}/${encodeURIComponent(accountId)}/memberships`;
}

/**
 * Терм отбора по человеку — единственный, который принимает эта поверхность.
 *
 * Оператор только равенство: подстрочный поиск грамматика разбирает, а чтение
 * отвергает явно, и сводить его к равенству молча значило бы ответить уверенно
 * и неверно. Терм действует ВНУТРИ названного аккаунта — оракула из него не
 * возникает.
 */
export function membershipUserFilter(userId: string): string {
  return `userId="${userId}"`;
}

// ====== ServiceAccount ======
export interface ServiceAccount {
  id: string;
  account_id?: string;
  name: string;
  description?: string;
  created_at?: string;
}
export interface ServiceAccountList {
  service_accounts: ServiceAccount[];
  next_page_token?: string;
}

// ====== ServiceAccount OAuth keys (SAKeyService) ======
// Статические OAuth-ключи сервисного аккаунта (Class A workload identity). Секрет
// (private_key_pem) выдается ОДИН раз в ответе Issue-операции и нигде не хранится —
// UI обязан показать его сразу и дать сохранить. List/Get секрет не содержат.
export interface ServiceAccountOAuthClient {
  id: string; // soc_…
  sva_id?: string;
  hydra_client_id?: string;
  description?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by_user_id?: string;
  created_at?: string;
  /** Вид удостоверения — ЧЕМ оно себя предъявляет. От него зависит смысл
   *  пустого `expires_at`: у ключевой пары «бессрочно», у секрета такой строки
   *  не бывает вовсе. */
  credential_kind?: CredentialKind;
}
export interface ListSAKeysResponse {
  keys?: ServiceAccountOAuthClient[];
  next_page_token?: string;
}
// Ответ Issue-операции (Operation.response). Несёт ОДНОРАЗОВОЕ значение, и форм
// у него ДВЕ: `private_key_pem` у ключевой пары либо `secret` у секрета —
// заполнено ровно одно, и решает это вид.
export interface IssueSAKeyResponse {
  key?: ServiceAccountOAuthClient;
  client_id?: string;
  private_key_pem?: string;
  public_key_pem?: string;
  algorithm?: string;
  key_id?: string;
  audiences?: string[];
  /** ПОКАЗЫВАЕТСЯ ОДИН РАЗ. Заполнен ТОЛЬКО у вида SECRET. */
  secret?: string;
}
// Тело Issue-запроса. created_by_user_id проставляет backend из принципала — не шлем.
export interface IssueSAKeyBody {
  description?: string;
  ttl_seconds?: number;
}

// REST-путь коллекции ключей сервисного аккаунта: /iam/v1/serviceAccounts/{id}/keys.
export function saKeysPath(serviceAccountId: string): string {
  return `${IAM.serviceAccounts}/${encodeURIComponent(serviceAccountId)}/keys`;
}

// ====== User OAuth tokens (UserTokenService) ======
// Персональные OAuth-токены пользователя (workload identity под личностью юзера).
// Секрет (private_key_pem) выдается ОДИН раз в ответе Issue-операции и нигде не
// хранится — UI обязан показать его сразу и дать сохранить. List/Get секрет не содержат.
export interface UserOAuthClient {
  id: string; // uoc_…
  user_id?: string;
  hydra_client_id?: string;
  description?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by_user_id?: string;
  created_at?: string;
  /** Вид удостоверения — см. `ServiceAccountOAuthClient.credential_kind`. */
  credential_kind?: CredentialKind;
}
export interface ListUserTokensResponse {
  tokens?: UserOAuthClient[];
  next_page_token?: string;
}
// Ответ Issue-операции (Operation.response) — см. IssueSAKeyResponse: форм две,
// заполнено ровно одно поле, и решает это вид.
export interface IssueUserTokenResponse {
  key?: UserOAuthClient;
  client_id?: string;
  private_key_pem?: string;
  public_key_pem?: string;
  algorithm?: string;
  key_id?: string;
  audiences?: string[];
  /** ПОКАЗЫВАЕТСЯ ОДИН РАЗ. Заполнен ТОЛЬКО у вида SECRET. */
  secret?: string;
}
// Тело Issue-запроса. created_by_user_id проставляет backend из принципала — не шлем.
export interface IssueUserTokenBody {
  description?: string;
  ttl_seconds?: number;
}

// REST-путь коллекции токенов пользователя: /iam/v1/users/{user_id}/tokens.
export function userTokensPath(userId: string): string {
  return `${IAM.users}/${encodeURIComponent(userId)}/tokens`;
}

// ====== Участие пользователя в аккаунте (UserService.Block / Unblock) ======
//
// Запрет участия и его возврат — ДЕЙСТВИЯ, а не правка поля: у действия нет
// маски, поэтому «забыть поле» и задеть им всех, кого коснулся, здесь
// невозможно by construction. Оба отвечают `Operation`.
//
// Путь строится ЗДЕСЬ, в поверхности API домена, а не в общем виде строки, и
// это не вкусовщина: перепись глаголов консоли резолвит голову литерала в
// объявление `IAM` и сверяет получившийся путь с `google.api.http` контракта.
// Собранный «на месте» из кусочков путь ушёл бы из-под этого надзора — и
// снятый с контракта глагол остался бы живой кнопкой.
export function userBlockPath(userId: string): string {
  return `${IAM.users}/${encodeURIComponent(userId)}:block`;
}
export function userUnblockPath(userId: string): string {
  return `${IAM.users}/${encodeURIComponent(userId)}:unblock`;
}

// ====== Исключение человека из аккаунта (UserService.RemoveFromAccount) ======
//
// ЭТО НЕ ЗАПРЕТ И НЕ УДАЛЕНИЕ, и путать их дорого. Запрет выше пишется в
// состояние ГЛОБАЛЬНОЙ строки человека и останавливает ему вход на платформу
// целиком; удаление стирает эту строку и во всех его аккаунтах сразу. Оба —
// действия уровня облака (#1102 / #1131). Исключение снимает СТРОКУ ЧЛЕНСТВА:
// человек перестаёт участвовать в НАЗВАННОМ аккаунте и продолжает работать в
// остальных без единого изменения записи. Это то, что директива владельца
// оставляет распорядителю аккаунта (#1127).
//
// Аккаунт называется ТЕЛОМ, а не выводится из строки: у человека их может быть
// несколько, и вывести аккаунт из его записи значило бы исключить не оттуда.
export function userRemoveFromAccountPath(userId: string): string {
  return `${IAM.users}/${encodeURIComponent(userId)}:removeFromAccount`;
}

// ====== Group ======
export interface Group {
  id: string;
  account_id?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  created_at?: string;
}
export interface GroupList {
  groups: Group[];
  next_page_token?: string;
}
export interface GroupMember {
  member_type: string; // "user" | "service_account"
  member_id: string;
  added_at?: string;
}
export interface GroupMemberList {
  members: GroupMember[];
  next_page_token?: string;
}

// ====== Rule (RBAC rules-model) ======
// Публичная поверхность роли — набор правил `rules[]` (источник истины для UI).
// Каждое Rule — однородный грант глаголов `verbs` над декартовым произведением
// `module × resources`, опц. суженный `resource_names[]` (pin-by-id) XOR
// `match_labels{}` (AND-equality). На проводе имена ПОЛЕЙ camelCase
// (`resourceNames`/`matchLabels`), а КЛЮЧИ самой карты — нет: это метки тенанта,
// и `match_labels` входит в opaque-исключение конвертера наравне с `labels`.
// `module` — scalar (ровно один модуль на правило).
export interface Rule {
  module: string;
  resources: string[];
  verbs: string[];
  resource_names?: string[];
  match_labels?: Record<string, string>;
}

export type RuleArm = "ARM_ANCHOR" | "ARM_NAMES" | "ARM_LABELS";

/** Выводит арм правила из его формы (наличие resource_names XOR match_labels). */
export function ruleArm(rule: Rule): RuleArm {
  if (rule.resource_names && rule.resource_names.length > 0) return "ARM_NAMES";
  if (rule.match_labels && Object.keys(rule.match_labels).length > 0)
    return "ARM_LABELS";
  return "ARM_ANCHOR";
}

// ====== Role ======
// Backend gRPC-gateway emit'ит JSON camelCase by default — поэтому в API ответе
// будут `isSystem`/`accountId`/`createdAt`. Старые snake_case оставлены для
// backwards-compat (некоторые endpoint'ы шлют их). KAC-171 follow-up: preset
// system-roles были скрыты в AccessBindings dropdown потому что `is_system`
// undefined → filter never matched.
export interface Role {
  id: string;
  // Legacy flat anchor-FK (AS-IS): retained для back-compat чтения. IAM-1 wire-
  // проекция — definitionTier{tierType,tierId} (dotted). Оба читаемы.
  account_id?: string;
  accountId?: string;
  // IAM-1 F4: definitionTier — где определена роль (dotted tierType + anchor id).
  // isSystem° = derived (tierType==iam.cluster) — не хранимый bool на wire.
  definition_tier?: DefinitionTier;
  definitionTier?: DefinitionTier;
  name: string;
  description?: string;
  // RBAC rules-model: публичная поверхность роли. UI рендерит и редактирует из неё.
  rules?: Rule[];
  // INTERNAL compiled-форма — на public поверхности отсутствует/пуста (two-projection,
  // IAM-1 F5). На входе НЕ принимается; UI её НЕ рендерит и НЕ шлёт.
  permissions?: string[];
  is_system?: boolean;
  isSystem?: boolean;
  // IAM-1 F6: канонический system-catalog — честный co-материализованный verb-набор.
  // authoredVerbs° — что задано в rules; effectiveVerbs° — что реально даёт (editor
  // включает delete*). verbNotes° — дословные пояснения (delete* → «co-materialized
  // on in-scope leaf objects, NOT on the account/project anchor itself»).
  authored_verbs?: string[];
  authoredVerbs?: string[];
  effective_verbs?: string[];
  effectiveVerbs?: string[];
  verb_notes?: Record<string, string>;
  verbNotes?: Record<string, string>;
  // OCC-токен для Role.Update под конкуренцией (IAM-1 снимает с wire — OCC server-side;
  // поле оставлено optional для back-compat чтения legacy-ответов).
  resource_version?: string;
  created_at?: string;
  createdAt?: string;
}

/** isSystem° — derived: роль системная ⟺ definitionTier.tierType == iam.cluster.
 *  Fallback на хранимый is_system/isSystem (AS-IS до redesign-миграции). */
export function roleIsSystem(
  role: Pick<
    Role,
    "is_system" | "isSystem" | "definition_tier" | "definitionTier"
  >,
): boolean {
  const dt = role.definition_tier ?? role.definitionTier;
  const tt = dt?.tier_type ?? dt?.tierType;
  if (tt) return tt === "iam.cluster";
  return role.is_system === true || role.isSystem === true;
}

/** definitionTier роли, независимо от camel/snake. */
export function roleDefinitionTier(
  role: Pick<Role, "definition_tier" | "definitionTier">,
): DefinitionTier | undefined {
  return role.definition_tier ?? role.definitionTier;
}

/** Канонический порядок system-ролей (viewer→editor→admin→owner) — для группировки
 *  каталога, когда сервер не гарантировал first-in-order. IAM-1 F6. */
export const SYSTEM_ROLE_CANON_ORDER = [
  "viewer",
  "editor",
  "admin",
  "owner",
] as const;
export interface RoleList {
  roles: Role[];
  next_page_token?: string;
}

// ====== PermissionCatalog (RBAC rules-model, backend-driven) ======
// Grantable-token каталог для RulesEditor dropdown'ов. Источник истины — backend
// (`authzmap.objectTypes` + closed verbs), отдаётся публичным sync read-RPC
// GET /iam/v1/permissionCatalog. Каталог immutable-в-рантайме (платформенная
// метаданность) — UI кэширует через react-query.
//
// Wire — camelCase (`hasVerbRelations`/`hasListEndpoint`/`closedVerbs`/
// `labelSelectable`/`wildcardPolicy.*`); api-клиент прогоняет ответ через
// camelToSnake → в UI ключи snake_case.
export interface CatalogResource {
  // 2-й сегмент токена (camelCase singular `securityGroup`/`routeTable`/…, либо
  // pluralized для loadbalancer).
  resource: string;
  // verb-bearing leaf (true) vs tier-only ancestor (iam.account/iam.project → false).
  has_verb_relations?: boolean;
  // публичный per-object filtered List на external-листенере есть (true) →
  // resource_names-picker рендерит Select инстансов; false → free-text fallback.
  has_list_endpoint?: boolean;
  // тип label-selectable (есть resource-feed для match_labels-реконсайла). false →
  // match_labels по этому типу запрещён backend'ом; RulesEditor блокирует submit.
  label_selectable?: boolean;
  // Глаголы, которые правило роли вправе назвать НА ЭТОМ ресурсе, в каноническом
  // порядке показа. ЭТО источник выпадающего списка редактора ролей, а не
  // `closed_verbs` (#1128): набор глаголов принадлежит ТИПУ, и пересечение не
  // выражает ни расширения (`addTargets` у групп целей), ни сужения (`update`
  // снят у `iam.user`).
  //
  // Поле отсутствует у ресурса, который глаголов не несёт (ярусный предок), и у
  // ответа СТАРОГО края — на wire proto3 опускает пустой repeated, поэтому «нет
  // набора» и «набор пуст» на проводе неразличимы. Различает их
  // `has_verb_relations`: глагольный ресурс без набора — это старый край, и
  // редактор откатывается на общий словарь.
  verbs?: string[];
}
export interface CatalogModule {
  module: string; // 1-й сегмент токена (iam/vpc/compute/loadbalancer)
  resources?: CatalogResource[];
}
export interface WildcardPolicy {
  // verb-`*` grantable в custom-роли (bounded).
  verb_wildcard_allowed_custom?: boolean;
  // module-`*`/resource-`*` — system-only (custom → INVALID_ARGUMENT).
  module_resource_wildcard_system_only?: boolean;
}
export interface PermissionCatalog {
  modules?: CatalogModule[];
  closed_verbs?: string[];
  wildcard_policy?: WildcardPolicy;
}

// GET /iam/v1/permissionCatalog — публичный sync read grantable-таксономии
// (модули → ресурсы + флаги + closed_verbs + wildcard-политика). Read sync, НЕ
// Operation. UI кэширует через react-query (usePermissionCatalog).
export const PERMISSION_CATALOG_PATH = "/iam/v1/permissionCatalog";

// ====== AccessBinding ======
export type SubjectType = "user" | "service_account" | "group";
// RBAC v2 (KAC-214): resource_type ограничен высокоуровневыми скоупами,
// которые принимает AccessBindingsPage. Legacy resource-manager типы
// (folder/organization/cloud) удалены — backend validResourceTypes их не
// содержит (KAC-124 / KAC-223 mig0008).
export type ResourceType = "account" | "project" | "cluster";

// RBAC v2 (KAC-214): anchor-tier binding'а. Output-only — backend derive
// из resource_type; в CreateAccessBindingRequest поля scope НЕТ.
export type Scope = "CLUSTER" | "ACCOUNT" | "PROJECT" | "SCOPE_UNSPECIFIED";

// ====== IAM-1 F8: AccessBinding.target — REQUIRED least-priv spine ======
// ResourceRef — closed-table {type,id} (без name — least-info, anti-oracle; в
// отличие от generic reference.Referrer{type,id,name°}). `type` — dotted из
// закрытого type-registry (compute.instance, vpc.network, …). Graceful-dangling.
export interface ResourceRef {
  type: string;
  id: string;
}

// target oneof: allInScope{} (широчайший явный opt-in — все объекты под anchor'ом,
// включая будущие) XOR resources[] (per-object least-priv). REQUIRED — самый
// широкий грант достижим ТОЛЬКО явным allInScope (нет sentinel-по-умолчанию).
//
// Форма per-object арма — ВЛОЖЕННАЯ: `AccessTarget.resources` это само сообщение
// `AccessTargetResources{repeated ResourceRef resources}`, поэтому на проводе
// `target.resources.resources[]`. Ровно это пишет buildCreateAccessBindingBody;
// читать плоско — значит не увидеть ни одного per-object гранта.
export type TargetKind = "allInScope" | "resources";
export interface AccessTargetResources {
  resources?: ResourceRef[];
}
export interface AccessBindingTarget {
  all_in_scope?: Record<string, never>;
  allInScope?: Record<string, never>;
  resources?: AccessTargetResources;
}

/** Объекты per-object арма (пусто, если арм не выбран). */
export function targetResources(t: AccessBindingTarget | undefined): ResourceRef[] {
  const rows = t?.resources?.resources;
  return Array.isArray(rows) ? rows : [];
}

/** Дискриминатор target'а из его формы (allInScope vs resources[]). */
export function targetKind(
  t: AccessBindingTarget | undefined,
): TargetKind | undefined {
  if (!t) return undefined;
  if (targetResources(t).length > 0) return "resources";
  if (t.all_in_scope !== undefined || t.allInScope !== undefined)
    return "allInScope";
  return undefined;
}

export interface AccessBinding {
  id: string;
  // Legacy single-subject (AS-IS): retained для back-compat — сервер по-прежнему
  // заполняет пару на чтении (= subjects[0]). IAM-1 — subjects[].
  subject_type: string;
  subject_id: string;
  role_id: string;
  created_at?: string;
  // Здесь стояли `resource_type` / `resource_id` (обязательными!) и `scope`.
  // Ни одного из этих полей у AccessBinding ствола нет: тройка снята в пользу
  // scope_type/scope_id, а теги и имена `scope`/`scope_ref` — надгробия
  // (`reserved 15,16,17,18`). Объявлять их обязательными значило обещать
  // читателю поле, которого он никогда не получит. Одноимённые поля ЕСТЬ у
  // SubjectPrivilege — это другое сообщение, см. ниже.
  // ── IAM-1 redesign (additive) ──
  // subjects[] — 1..N грантополучателей (per-subject независимый tuple-set/revoke).
  subjects?: Subject[];
  // scopeType/scopeId — dotted anchor-tier + anchor id (immutable). F7.
  scope_type?: IamScopeType;
  scopeType?: IamScopeType;
  scope_id?: string;
  scopeId?: string;
  // target — REQUIRED least-priv spine (allInScope{} | resources[ResourceRef]). F8.
  target?: AccessBindingTarget;
  // Жизненный статус (PENDING/ACTIVE/REVOKED). Delete=hard(404) / :revoke=soft. F10.
  status?: AccessBindingStatus;
  revoked_at?: string;
  revokedAt?: string;
  granted_by_user_id?: string;
  grantedByUserId?: string;
  deletion_protection?: boolean;
  deletionProtection?: boolean;
}
export interface AccessBindingList {
  access_bindings: AccessBinding[];
  next_page_token?: string;
}

// ====== AssignableRole ======
// Lean public-safe проекция Role для scope-first grant-формы. Сервер вычисляет
// scope_group (SYSTEM/ACCOUNT/PROJECT) из scope-полей роли — UI группирует picker
// РОВНО по этому полю, без клиентской scope-логики. `permissions` НЕ возвращаются
// (picker ≠ role-detail). Wire — camelCase; api.list прогоняет ответ через
// camelToSnake → в UI ключи snake_case (role_id/scope_group/…).
export type ScopeGroup =
  "SYSTEM" | "ACCOUNT" | "PROJECT" | "SCOPE_GROUP_UNSPECIFIED";

export interface AssignableRole {
  role_id: string;
  name: string;
  description?: string;
  is_system?: boolean;
  // Серверно-вычисленный групп-маркер: UI рисует секции «Системные / Account-роли
  // / Project-роли» напрямую по этому полю.
  scope_group?: ScopeGroup;
  created_at?: string;
}
export interface AssignableRolesList {
  roles: AssignableRole[];
  next_page_token?: string;
}

// AccessBinding.Status (proto enum): PENDING / ACTIVE / REVOKED. Output-only.
export type AccessBindingStatus = "PENDING" | "ACTIVE" | "REVOKED";

// ====== Canonical subjects (thin-binding) ======
// Subject — один грантополучатель биндинга. `type` — enum SubjectType, и на
// проводе это ИМЯ значения (SUBJECT_TYPE_USER/…): нижне-регистровую "user"
// protojson не распознаёт и схлопывает в SUBJECT_TYPE_UNSPECIFIED. Сервер это
// переживает — он выводит тип из префикса id (usr/sva/grp) — но полагаться на
// восстановление того, что мы сами стёрли, нечего: посылаем имя значения.
// Нижний регистр — только внутренний UI-тип (SubjectType). `id` — usr…/sva…/grp….
// Биндинг несёт subjects[] (1..32); per-subject независимый tuple-set / revoke.
export interface Subject {
  type: WireSubjectType;
  id: string;
}

// UpdateAccessBindingBody — единственное mutable-поле AccessBinding —
// `deletion_protection`. Шлётся с update_mask=["deletionProtection"] для снятия
// защиты перед удалением protected (owner-auto) binding'а.
export interface UpdateAccessBindingBody {
  deletion_protection: boolean;
  update_mask: string;
}

// ====== CreateAccessBindingRequest body ======
// Ground truth: proto/kacho/cloud/iam/v1/access_binding_service.proto.
//   required : subjects[] (или legacy single subject_type/subject_id), role_id,
//              scope_type (dotted), scope_id, target
//   tombstone: теги 9/10/11 с именами target_ref / scope_ref
//   renamed  : resource_type/resource_id → scope_type/scope_id (redesign-2026 F7)
// Тело собирается здесь одним местом, потому что имя поля, которого в сообщении
// нет, край выбрасывает молча (DiscardUnknown) — и грант «проходит», не создав
// ничего.

/** Anchor id кластерного singleton'а — единственная строка cluster-тира. */
export const CLUSTER_SCOPE_ID = "cluster_root";

/** Dotted `scope_type`, который принимает CreateAccessBindingRequest. */
export const SCOPE_TYPE_BY_TIER: Record<AccessBindingScopeTier, IamScopeType> = {
  CLUSTER: "iam.cluster",
  ACCOUNT: "iam.account",
  PROJECT: "iam.project",
};

export type AccessBindingScopeTier = "CLUSTER" | "ACCOUNT" | "PROJECT";

/** Proto enum wire-form SubjectType (имя значения, не строчная строка). */
export const SUBJECT_TYPE_ENUM: Record<SubjectType, WireSubjectType> = {
  user: "SUBJECT_TYPE_USER",
  service_account: "SUBJECT_TYPE_SERVICE_ACCOUNT",
  group: "SUBJECT_TYPE_GROUP",
};

export type WireSubjectType = "SUBJECT_TYPE_USER" | "SUBJECT_TYPE_SERVICE_ACCOUNT" | "SUBJECT_TYPE_GROUP";

export interface CreateAccessBindingInput {
  /** 1..32 грантополучателей; каждый даёт независимый tuple-set / revoke. */
  subjects: { type: WireSubjectType; id: string }[];
  roleId: string;
  scopeTier: AccessBindingScopeTier;
  /** Anchor id; для CLUSTER подставляется singleton, если не задан. */
  scopeId: string;
  /** Пусто → allInScope{} (широчайший грант — только явным opt-in). */
  targetResources?: ResourceRef[];
}

/**
 * Тело POST /iam/v1/accessBindings ровно в форме CreateAccessBindingRequest.
 *
 * Бросает, если грант нечем адресовать (нет субъекта / роли / anchor'а): пустое
 * обязательное поле уехало бы на сервер и вернулось 400 «Illegal argument …», а на
 * пути без проверки `res.ok` — вообще молча.
 */
export function buildCreateAccessBindingBody(input: CreateAccessBindingInput): Record<string, unknown> {
  const subjects = input.subjects.filter((s) => s && s.id);
  if (subjects.length === 0) throw new Error("AccessBinding: не выбран ни один субъект");
  if (!input.roleId) throw new Error("AccessBinding: не выбрана роль");
  const scopeId = input.scopeTier === "CLUSTER" ? input.scopeId || CLUSTER_SCOPE_ID : input.scopeId;
  if (!scopeId) throw new Error("AccessBinding: не выбрана область действия (anchor)");

  const body: Record<string, unknown> = {
    subjects,
    role_id: input.roleId,
    scope_type: SCOPE_TYPE_BY_TIER[input.scopeTier],
    scope_id: scopeId,
    // AccessTarget — oneof; per-object арм сам является сообщением
    // AccessTargetResources{repeated ResourceRef resources}, отсюда вложенность.
    target: input.targetResources?.length
      ? { resources: { resources: input.targetResources.map((r) => ({ type: r.type, id: r.id })) } }
      : { all_in_scope: {} },
  };
  return body;
}

// ====== SubjectPrivilege ======
// Обогащённая public-safe проекция AccessBinding для вкладки «Привилегии».
// role_name резолвится сервером (dangling role → пусто, UI fallback на role_id).
// derivation: DIRECT (прямая привязка) vs GROUP (через членство в группе).
export type Derivation = "DIRECT" | "GROUP" | "DERIVATION_UNSPECIFIED";

export interface SubjectPrivilege {
  binding_id: string;
  role_id: string;
  // resolved сервером (пусто для удалённой роли — UI fallback на role_id).
  role_name?: string;
  // В ОТЛИЧИЕ от AccessBinding, это сообщение действительно несёт и старую
  // тройку (теги 4/5/6), и точечную пару (12/13) — обе живые на ревизии.
  resource_type?: string;
  resource_id?: string;
  scope?: Scope;
  scope_type?: IamScopeType;
  scope_id?: string;
  status?: AccessBindingStatus;
  created_at?: string;
  granted_by_user_id?: string;
  expires_at?: string;
  derivation?: Derivation;
  /** Группа, через членство в которой пришло право (тег 14). */
  via_group_id?: string;
}
export interface SubjectPrivilegeList {
  privileges: SubjectPrivilege[];
  next_page_token?: string;
}

// ====== Endpoints map ======
export const IAM = {
  accounts: "/iam/v1/accounts",
  projects: "/iam/v1/projects",
  users: "/iam/v1/users",
  serviceAccounts: "/iam/v1/serviceAccounts",
  groups: "/iam/v1/groups",
  roles: "/iam/v1/roles",
  accessBindings: "/iam/v1/accessBindings",
} as const;

// ====== List helpers (без auth) ======
export const iamApi = {
  // Accounts
  listAccounts: (q?: Record<string, string>) =>
    api.list<AccountList>(IAM.accounts, q),
  // Projects — account_id обязателен по proto, но handler допускает list-all.
  listProjects: (q?: Record<string, string>) =>
    api.list<ProjectList>(IAM.projects, q),
  // Users
  // KAC-125: Invite user by email (admin OR editor permission on account).
  inviteUser: (req: InviteUserRequest) =>
    api.post<{
      id?: string;
      metadata?: {
        user_id?: string;
        account_id?: string;
        magic_link_url?: string;
      };
      response?: User;
      error?: { code: number; message: string };
    }>(`${IAM.users}:invite`, req),
  listUsers: (q?: Record<string, string>) => api.list<UserList>(IAM.users, q),
  // Членства НАЗВАННОГО аккаунта. Аккаунт — отдельный аргумент, а не элемент
  // `q`: он стоит в пути, и забыть его нельзя — без него не собирается адрес.
  listAccountMemberships: (accountId: string, q?: Record<string, string>) =>
    api.list<MembershipList>(accountMembershipsPath(accountId), q),
  // SAs
  listServiceAccounts: (q?: Record<string, string>) =>
    api.list<ServiceAccountList>(IAM.serviceAccounts, q),
  // SA OAuth keys (SAKeyService.List) — метаданные ключей без секрета.
  listSaKeys: (serviceAccountId: string, q?: Record<string, string>) =>
    api.list<ListSAKeysResponse>(saKeysPath(serviceAccountId), q),
  // User OAuth tokens (UserTokenService.List) — метаданные токенов без секрета.
  listUserTokens: (userId: string, q?: Record<string, string>) =>
    api.list<ListUserTokensResponse>(userTokensPath(userId), q),
  // Groups
  listGroups: (q?: Record<string, string>) =>
    api.list<GroupList>(IAM.groups, q),
  // Group members — custom GET endpoint /iam/v1/groups/{group_id}:listMembers
  listGroupMembers: (groupId: string, q?: Record<string, string>) =>
    api.list<GroupMemberList>(`${IAM.groups}/${groupId}:listMembers`, q),
  // Roles
  listRoles: (q?: Record<string, string>) => api.list<RoleList>(IAM.roles, q),
  // Permission-каталог (RBAC rules-model) — grantable-таксономия для RulesEditor.
  fetchPermissionCatalog: () =>
    api.get<PermissionCatalog>(PERMISSION_CATALOG_PATH),
  // AccessBindings: list-by-scope + list-by-subject (custom verbs).
  // Глагол называется `:listByScope` — прежнее имя `:listByResource` снято с
  // контракта вместе с wire-именем; пара resource_type/resource_id остаётся
  // принимаемой (край всё ещё выводит область запроса именно из неё).
  listAccessBindingsByResource: (
    resource_type: string,
    resource_id: string,
    q?: Record<string, string>,
  ) =>
    api.list<AccessBindingList>(`${IAM.accessBindings}:listByScope`, {
      resource_type,
      resource_id,
      ...(q ?? {}),
    }),
  listAccessBindingsBySubject: (
    subject_type: string,
    subject_id: string,
    q?: Record<string, string>,
  ) =>
    api.list<AccessBindingList>(`${IAM.accessBindings}:listBySubject`, {
      subject_type,
      subject_id,
      ...(q ?? {}),
    }),
  /**
   * GET /iam/v1/accessBindings:listAssignableRoles — backend-driven набор ролей,
   * валидных для (resource_type, resource_id). Сервер применяет тот же предикат
   * isRoleAssignable, что энфорсит AccessBinding.Create, и аннотирует каждую роль
   * scope_group (SYSTEM/ACCOUNT/PROJECT). Grant-форма рендерит РОВНО этот набор,
   * сгруппированный по scope_group — без клиентской scope-логики. Read sync.
   */
  listAssignableRoles: (
    resource_type: string,
    resource_id: string,
    pageSize = "1000",
  ) =>
    api.list<AssignableRolesList>(`${IAM.accessBindings}:listAssignableRoles`, {
      resource_type,
      resource_id,
      page_size: pageSize,
    }),
  /**
   * GET /iam/v1/accessBindings:listSubjectPrivileges — обогащённые привилегии
   * (resolved role_name + scope) субъекта. subject_type ∈
   * {"user","service_account","group"}. Cursor-pagination через page_size /
   * page_token. Read sync.
   */
  listSubjectPrivileges: (
    subject_type: string,
    subject_id: string,
    q?: Record<string, string>,
  ) =>
    api.list<SubjectPrivilegeList>(
      `${IAM.accessBindings}:listSubjectPrivileges`,
      {
        subject_type,
        subject_id,
        ...(q ?? {}),
      },
    ),
  /**
   * PATCH /iam/v1/accessBindings/{id} — снять/поставить `deletion_protection`
   * (единственное mutable-поле). Используется, чтобы СНЯТЬ защиту с protected
   * (owner-auto) binding перед удалением. Async через Operation.
   */
  updateAccessBindingDeletionProtection: (
    id: string,
    deletionProtection: boolean,
  ) =>
    api.update(`${IAM.accessBindings}/${encodeURIComponent(id)}`, {
      deletion_protection: deletionProtection,
      // A FieldMask path travels as a STRING VALUE, so the request-side key
      // transform never touches it — it has to be written the way protojson reads
      // it, and protojson rejects any path containing "_" outright.
      update_mask: snakeToCamelPath("deletion_protection"),
    } satisfies UpdateAccessBindingBody),
  /**
   * POST /iam/v1/accessBindings/{id}:revoke — IAM-1 F10 soft-revoke (async
   * Operation). В отличие от Delete (hard, Get→404), :revoke переводит binding в
   * status=REVOKED с retention (revokedAt°/grantedByUserId° удержаны, replay
   * emitted-ledger). Re-grant после revoke — новая ACTIVE-строка.
   */
  revokeAccessBinding: (id: string) =>
    api.post<Record<string, unknown>>(
      `${IAM.accessBindings}/${encodeURIComponent(id)}:revoke`,
      {},
    ),
  /**
   * KAC item #1: GET /iam/v1/accounts/{account_id}/accessBindings — все
   * AccessBinding'и видимые админу в account'е (включает project-scoped + account-scoped).
   * Опциональные фильтры:
   *   - subject_type_filter — "user" | "service_account" | "group"
   *   - include_revoked — "true" / "false" (default false)
   *   - page_size / page_token — opaque cursor pagination.
   */
  listAccessBindingsByAccount: (
    accountId: string,
    q?: {
      page_size?: number | string;
      page_token?: string;
      include_revoked?: boolean;
      subject_type_filter?: string;
    },
  ) => {
    const query: Record<string, string> = {};
    if (q?.page_size !== undefined) query.page_size = String(q.page_size);
    if (q?.page_token) query.page_token = q.page_token;
    if (q?.include_revoked !== undefined)
      query.include_revoked = q.include_revoked ? "true" : "false";
    if (q?.subject_type_filter)
      query.subject_type_filter = q.subject_type_filter;
    return api.list<AccessBindingList>(
      `${IAM.accounts}/${encodeURIComponent(accountId)}/accessBindings`,
      query,
    );
  },
};
