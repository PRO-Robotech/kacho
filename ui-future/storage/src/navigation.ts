export type RemoteIconName = "camera" | "hard-drive" | "layers";

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

// Storage — домен блочного хранилища: Тома / Снимки / Типы дисков. Секция монтируется
// под /projects/:projectId/storage/*. DiskTypes — cluster-scoped справочник, но
// показывается внутри проектного контекста (список игнорирует project_id).
export const STORAGE_NAVIGATION: RemoteNavSection[] = [
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
      {
        key: "storage-disk-types",
        icon: "layers",
        label: "Типы дисков",
        path: "storage/disk-types",
        requiresProject: true,
      },
    ],
  },
];

export const DASHBOARD_NAVIGATION = STORAGE_NAVIGATION;
export default STORAGE_NAVIGATION;
