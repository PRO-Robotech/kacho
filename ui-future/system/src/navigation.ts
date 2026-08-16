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

export const SYSTEM_NAVIGATION: RemoteNavSection[] = [
  // System / Administration (admin-only, kacho-only global-ресурсы).
  // Обслуживаются system-remote под /system/* (см. SystemPage SystemRoutes).
  {
    key: "system",
    segment: "system",
    icon: "globe",
    label: SERVICES.system.menuTitle,
    landingPath: "/system/regions",
    items: [
      { key: "system-regions", icon: "globe", label: ENTITIES.regions.plural, path: "/system/regions" },
      { key: "system-zones", icon: "route", label: ENTITIES.zones.plural, path: "/system/zones" },
      {
        key: "system-address-pools",
        icon: "network",
        label: ENTITIES["address-pools"].plural,
        path: "/system/address-pools",
      },
      { key: "system-cluster-admins", icon: "shield", label: "Администраторы кластера", path: "/system/cluster/admins" },
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
