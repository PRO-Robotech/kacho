// Подписи разделов и ресурсов — из единственного источника: литерал рядом
// с местом показа расходится молча, ссылка — нет (см. entity-names.ts).
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";
import { TOKENS_LANDING_PATH, TOKENS_SECTION_LABEL, TOKENS_SECTIONS } from "./labels";

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

/** Иконка пункта части «Токены и ключи» — по сегменту его адреса.
 *  Живёт здесь, а не рядом с подписью: иконку показывает меню, рейл — нет. */
const TOKENS_ITEM_ICONS: Record<string, RemoteIconName> = {
  "service-account-keys": "key",
  "user-tokens": "lock",
};

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
      {
        key: "system-cluster-admins",
        icon: "shield",
        label: "Администраторы кластера",
        path: "/system/cluster/admins",
      },
      {
        // ПРЕДЕЛЫ — зона рута: величины всех трёх уровней (платформа, аккаунт,
        // проект) и их правка. Страница существовала и работала, но пункта на
        // неё не было НИ ОДНОГО: попасть можно было только набрав адрес руками.
        //
        // Называется «Пределы», а не «Квоты», и это не синонимы: квота — это
        // предел ПЛЮС израсходованное, и «занято» здесь не показывается вовсе.
        // Считают расход владельцы типов у себя, каждый по своему носителю;
        // страница администратора отвечает на другой вопрос — «сколько
        // разрешено и кем это объявлено».
        key: "system-limits",
        icon: "scale",
        label: "Пределы",
        path: "/system/limits",
      },
    ],
  },
  // Tokens & keys (выпуск OAuth-креденшалов). Под /system/tokens/*.
  //
  // Подписи и адреса — из `./labels`, того же источника, из которого их берёт
  // рейл части (`TokensLayout`). Прежде они стояли литералами в обоих местах, и
  // правка подписи доезжала до одного из двух. Иконка остаётся здесь: рейл её
  // не показывает, поэтому общей она не является.
  {
    key: "tokens",
    segment: "tokens",
    icon: "key",
    label: TOKENS_SECTION_LABEL,
    landingPath: TOKENS_LANDING_PATH,
    items: TOKENS_SECTIONS.map((section) => ({
      key: `tokens-${section.segment}`,
      icon: TOKENS_ITEM_ICONS[section.segment] ?? "key",
      label: section.label,
      path: section.path,
    })),
  },
];

export const DASHBOARD_NAVIGATION = SYSTEM_NAVIGATION;
export default SYSTEM_NAVIGATION;
