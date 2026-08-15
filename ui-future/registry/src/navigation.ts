// Подписи разделов и ресурсов — из единственного источника: литерал рядом
// с местом показа расходится молча, ссылка — нет (см. entity-names.ts).
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";

export type RemoteIconName = "activity" | "cable" | "git-branch" | "globe" | "layers" | "network" | "route" | "shield";

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

export const REGISTRY_NAVIGATION: RemoteNavSection[] = [
  {
    key: "registry",
    segment: "registry",
    icon: "layers",
    label: SERVICES.registry.menuTitle,
    landingPath: "registry/registries",
    requiresProject: true,
    items: [
      {
        key: "registry-registries",
        icon: "layers",
        label: ENTITIES.registries.plural,
        path: "registry/registries",
        requiresProject: true,
      },
    ],
  },
];

export const DASHBOARD_NAVIGATION = REGISTRY_NAVIGATION;
export default REGISTRY_NAVIGATION;
