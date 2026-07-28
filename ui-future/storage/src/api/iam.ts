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
//   - /iam/v1/accessBindings        (AccessBindingService; Create/Delete/Get + listByResource/listBySubject)
//
// E0 (текущая фаза): без auth-interceptor; UI шлёт запросы без Bearer
// (api-gateway допускает анонимный доступ). Operations.principal_* — пусто/stub.

import { api } from "./client";

// ====== Account ======
export interface Account {
  id: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  owner_user_id?: string;
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
  // KAC-125 — User per-Account; nullable для backward compat.
  account_id?: string;
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

// ====== Role ======
// Backend gRPC-gateway emit'ит JSON camelCase by default — поэтому в API ответе
// будут `isSystem`/`accountId`/`createdAt`. Старые snake_case оставлены для
// backwards-compat (некоторые endpoint'ы шлют их). KAC-171 follow-up: preset
// system-roles были скрыты в AccessBindings dropdown потому что `is_system`
// undefined → filter never matched.
export interface Role {
  id: string;
  account_id?: string;
  accountId?: string;
  name: string;
  description?: string;
  permissions?: string[];
  is_system?: boolean;
  isSystem?: boolean;
  created_at?: string;
  createdAt?: string;
}
export interface RoleList {
  roles: Role[];
  next_page_token?: string;
}

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

export interface AccessBinding {
  id: string;
  subject_type: string;
  subject_id: string;
  role_id: string;
  resource_type: string;
  resource_id: string;
  created_at?: string;
  // RBAC v2 (KAC-214): output-only scope tier (CLUSTER/ACCOUNT/PROJECT).
  scope?: Scope;
}
export interface AccessBindingList {
  access_bindings: AccessBinding[];
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
  listAccounts: (q?: Record<string, string>) => api.list<AccountList>(IAM.accounts, q),
  // Projects — account_id обязателен по proto, но handler допускает list-all.
  listProjects: (q?: Record<string, string>) => api.list<ProjectList>(IAM.projects, q),
  // Users
  // KAC-125: Invite user by email (admin OR editor permission on account).
  inviteUser: (req: InviteUserRequest) =>
    api.post<{
      id?: string;
      metadata?: { user_id?: string; account_id?: string; magic_link_url?: string };
      response?: User;
      error?: { code: number; message: string };
    }>(`${IAM.users}:invite`, req),
  listUsers: (q?: Record<string, string>) => api.list<UserList>(IAM.users, q),
  // SAs
  listServiceAccounts: (q?: Record<string, string>) => api.list<ServiceAccountList>(IAM.serviceAccounts, q),
  // Groups
  listGroups: (q?: Record<string, string>) => api.list<GroupList>(IAM.groups, q),
  // Group members — custom GET endpoint /iam/v1/groups/{group_id}:listMembers
  listGroupMembers: (groupId: string, q?: Record<string, string>) =>
    api.list<GroupMemberList>(`${IAM.groups}/${groupId}:listMembers`, q),
  // Roles
  listRoles: (q?: Record<string, string>) => api.list<RoleList>(IAM.roles, q),
  // AccessBindings: list-by-resource + list-by-subject (custom verbs)
  listAccessBindingsByResource: (resource_type: string, resource_id: string, q?: Record<string, string>) =>
    api.list<AccessBindingList>(`${IAM.accessBindings}:listByResource`, {
      resource_type,
      resource_id,
      ...(q ?? {}),
    }),
  listAccessBindingsBySubject: (subject_type: string, subject_id: string, q?: Record<string, string>) =>
    api.list<AccessBindingList>(`${IAM.accessBindings}:listBySubject`, {
      subject_type,
      subject_id,
      ...(q ?? {}),
    }),
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
    if (q?.include_revoked !== undefined) query.include_revoked = q.include_revoked ? "true" : "false";
    if (q?.subject_type_filter) query.subject_type_filter = q.subject_type_filter;
    return api.list<AccessBindingList>(`${IAM.accounts}/${encodeURIComponent(accountId)}/accessBindings`, query);
  },
};
