// Подписи разделов и ресурсов — из единственного источника: литерал рядом
// с местом показа расходится молча, ссылка — нет (см. entity-names.ts).
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";

export type RemoteIconName =
  | "activity"
  | "cable"
  | "camera"
  | "cloud"
  | "folder"
  | "git-branch"
  | "globe"
  | "hard-drive"
  | "key"
  | "layers"
  | "lock"
  | "network"
  | "route"
  | "scale"
  | "server"
  | "shield"
  | "users";

export interface RemoteNavItem {
  key: string;
  icon: RemoteIconName;
  label: string;
  path: string;
  requiresProject?: boolean;
}

export interface RemoteNavSection {
  key: string;
  segment: string;
  icon: RemoteIconName;
  label: string;
  landingPath: string;
  requiresProject?: boolean;
  items: RemoteNavItem[];
}

export const IAM_NAVIGATION: RemoteNavSection[] = [
  {
    key: "iam",
    segment: "iam",
    icon: "key",
    label: SERVICES.iam.menuTitle,
    landingPath: "/iam/accounts",
    items: [
      { key: "iam-accounts", icon: "layers", label: ENTITIES.accounts.plural, path: "/iam/accounts" },
      // «Мои квоты» — пределы, носителем которых является ЛИЧНОСТЬ (#622).
      //
      // Стоит сразу за аккаунтами, потому что единственный сегодняшний вид —
      // потолок НАД аккаунтами: человек тянется к кнопке «создать аккаунт»
      // именно отсюда, и предел обязан попадаться на глаза раньше отказа.
      //
      // Подпись — литерал, а не `ENTITIES`: квота не ресурс консоли, своей
      // спеки в общем реестре у неё нет, и заводить её ради одной подписи
      // значило бы объявить сущность, которой не существует.
      { key: "iam-quotas", icon: "scale", label: "Мои квоты", path: "/iam/quotas" },
      { key: "iam-projects", icon: "folder", label: ENTITIES.projects.plural, path: "/iam/projects" },
      { key: "iam-users", icon: "users", label: ENTITIES.users.plural, path: "/iam/users" },
      {
        key: "iam-service-accounts",
        icon: "key",
        label: ENTITIES["service-accounts"].plural,
        path: "/iam/service-accounts",
      },
      { key: "iam-groups", icon: "git-branch", label: ENTITIES.groups.plural, path: "/iam/groups" },
      { key: "iam-roles", icon: "lock", label: ENTITIES.roles.plural, path: "/iam/roles" },
      {
        key: "iam-access-bindings",
        icon: "shield",
        label: ENTITIES["access-bindings"].plural,
        path: "/iam/access-bindings",
      },
      { key: "iam-operations", icon: "activity", label: ENTITIES.operations.plural, path: "/iam/operations" },
    ],
  },
];

export const DASHBOARD_NAVIGATION = IAM_NAVIGATION;
export default IAM_NAVIGATION;
