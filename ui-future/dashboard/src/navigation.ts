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

export const DASHBOARD_NAVIGATION: RemoteNavSection[] = [
  {
    key: "compute",
    segment: "compute",
    icon: "cloud",
    label: "Compute Cloud",
    landingPath: "compute/instances",
    requiresProject: true,
    items: [
      {
        key: "compute-instances",
        icon: "server",
        label: "Виртуальные машины",
        path: "compute/instances",
        requiresProject: true,
      },
      {
        key: "compute-machine-types",
        icon: "layers",
        label: "Типы машин",
        path: "compute/machine-types",
        requiresProject: true,
      },
    ],
  },
  {
    key: "storage",
    segment: "storage",
    icon: "hard-drive",
    label: "Storage",
    landingPath: "storage/volumes",
    requiresProject: true,
    items: [
      { key: "storage-volumes", icon: "hard-drive", label: "Тома", path: "storage/volumes", requiresProject: true },
      { key: "storage-snapshots", icon: "camera", label: "Снимки", path: "storage/snapshots", requiresProject: true },
      { key: "storage-images", icon: "layers", label: "Образы", path: "storage/images", requiresProject: true },
      {
        key: "storage-disk-types",
        icon: "layers",
        label: "Типы дисков",
        path: "storage/disk-types",
        requiresProject: true,
      },
    ],
  },
  {
    key: "nlb",
    segment: "nlb",
    icon: "scale",
    label: "Network Load Balancer",
    landingPath: "nlb/load-balancers",
    requiresProject: true,
    items: [
      {
        key: "nlb-load-balancers",
        icon: "network",
        label: "Балансировщики",
        path: "nlb/load-balancers",
        requiresProject: true,
      },
      { key: "nlb-listeners", icon: "cable", label: "Listeners", path: "nlb/listeners", requiresProject: true },
      {
        key: "nlb-target-groups",
        icon: "git-branch",
        label: "Target Groups",
        path: "nlb/target-groups",
        requiresProject: true,
      },
      { key: "nlb-operations", icon: "activity", label: "Операции", path: "nlb/operations", requiresProject: true },
    ],
  },
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
    ],
  },
];
