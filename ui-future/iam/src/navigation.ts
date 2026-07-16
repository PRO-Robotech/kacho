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
    label: "Identity and Access Management",
    landingPath: "/iam/accounts",
    items: [
      { key: "iam-accounts", icon: "layers", label: "Аккаунты", path: "/iam/accounts" },
      { key: "iam-projects", icon: "folder", label: "Проекты", path: "/iam/projects" },
      { key: "iam-users", icon: "users", label: "Пользователи", path: "/iam/users" },
      { key: "iam-service-accounts", icon: "key", label: "Сервисные аккаунты", path: "/iam/service-accounts" },
      { key: "iam-groups", icon: "git-branch", label: "Группы", path: "/iam/groups" },
      { key: "iam-roles", icon: "lock", label: "Роли", path: "/iam/roles" },
      { key: "iam-access-bindings", icon: "shield", label: "Связки прав", path: "/iam/access-bindings" },
      { key: "iam-operations", icon: "activity", label: "Операции", path: "/iam/operations" },
    ],
  },
];

export const DASHBOARD_NAVIGATION = IAM_NAVIGATION;
export default IAM_NAVIGATION;
