export type RemoteIconName = "cloud" | "server";

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
    ],
  },
];

export const DASHBOARD_NAVIGATION = COMPUTE_NAVIGATION;
export default COMPUTE_NAVIGATION;
