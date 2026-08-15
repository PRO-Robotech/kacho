// Подписи разделов и ресурсов — из единственного источника: литерал рядом
// с местом показа расходится молча, ссылка — нет (см. entity-names.ts).
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";

export type RemoteIconName = "cloud" | "server" | "layers";

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

// Compute — домен виртуальных машин. Секция монтируется под
// /projects/:projectId/compute/*. Диски/снимки вынесены в отдельный домен
// Storage (см. storage-remote).
export const COMPUTE_NAVIGATION: RemoteNavSection[] = [
  {
    key: "compute",
    segment: "compute",
    icon: "cloud",
    label: SERVICES.compute.menuTitle,
    landingPath: "compute/instances",
    requiresProject: true,
    items: [
      {
        key: "compute-instances",
        icon: "server",
        label: ENTITIES.instances.plural,
        path: "compute/instances",
        requiresProject: true,
      },
      {
        key: "compute-placement-groups",
        icon: "layers",
        label: "Группы размещения",
        path: "compute/placement-groups",
        requiresProject: true,
      },
      {
        key: "compute-machine-types",
        icon: "layers",
        label: ENTITIES["machine-types"].plural,
        path: "compute/machine-types",
        requiresProject: true,
      },
      // Ключи входа в гостевую систему — ресурс проекта со своим жизненным
      // циклом (завести / переименовать / отозвать), а не поле формы машины.
      // Без пункта в навигации ресурс есть, а дойти до него нельзя.
      {
        key: "compute-guest-access-keys",
        icon: "layers",
        label: "Ключи доступа",
        path: "compute/guest-access-keys",
        requiresProject: true,
      },
    ],
  },
];

export const DASHBOARD_NAVIGATION = COMPUTE_NAVIGATION;
export default COMPUTE_NAVIGATION;
