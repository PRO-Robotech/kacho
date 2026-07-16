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

export const NLB_NAVIGATION: RemoteNavSection[] = [
  {
    key: "nlb",
    segment: "nlb",
    icon: "cable",
    label: "Network Load Balancing",
    landingPath: "nlb/load-balancers",
    requiresProject: true,
    items: [
      {
        key: "nlb-load-balancers",
        icon: "network",
        label: "Балансировщики нагрузки",
        path: "nlb/load-balancers",
        requiresProject: true,
      },
      { key: "nlb-listeners", icon: "route", label: "Обработчики", path: "nlb/listeners", requiresProject: true },
      {
        key: "nlb-target-groups",
        icon: "layers",
        label: "Целевые группы",
        path: "nlb/target-groups",
        requiresProject: true,
      },
    ],
  },
];

export const DASHBOARD_NAVIGATION = NLB_NAVIGATION;
export default NLB_NAVIGATION;
