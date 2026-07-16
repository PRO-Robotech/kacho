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

export const SYSTEM_NAVIGATION: RemoteNavSection[] = [
  // System / Administration (admin-only, kacho-only global-ресурсы).
  // Обслуживаются system-remote под /system/* (см. SystemPage SystemRoutes).
  {
    key: "system",
    segment: "system",
    icon: "globe",
    label: "Администрирование",
    landingPath: "/system/regions",
    items: [
      { key: "system-regions", icon: "globe", label: "Регионы", path: "/system/regions" },
      { key: "system-zones", icon: "route", label: "Зоны", path: "/system/zones" },
      { key: "system-address-pools", icon: "network", label: "Пулы адресов", path: "/system/address-pools" },
      { key: "system-cluster-admins", icon: "shield", label: "Cluster admins", path: "/system/cluster/admins" },
    ],
  },
  // Tokens & keys (выпуск OAuth-креденшалов). Под /system/tokens/*.
  {
    key: "tokens",
    segment: "tokens",
    icon: "key",
    label: "Токены и ключи",
    landingPath: "/system/tokens/service-account-keys",
    items: [
      {
        key: "tokens-sa-keys",
        icon: "key",
        label: "Ключи сервисных аккаунтов",
        path: "/system/tokens/service-account-keys",
      },
      { key: "tokens-user-tokens", icon: "lock", label: "Токены пользователей", path: "/system/tokens/user-tokens" },
    ],
  },
];

export const DASHBOARD_NAVIGATION = SYSTEM_NAVIGATION;
export default SYSTEM_NAVIGATION;
