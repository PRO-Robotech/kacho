import type { RemoteNavSection } from "dashboard/navigation";

// Тест-стаб storage/navigation для host-jest. Зеркалит storage-remote nav (Volume +
// Image + Snapshot + DiskType), чтобы rail-ассерты проверяли появление
// ресурс-пунктов новой редизайн-модели (в частности Образ/Image). Стаб также
// обязателен, чтобы HostRail's Promise.allSettled(import("storage/navigation"))
// РЕЗОЛВИЛСЯ под jest: без стаба незастабленный bare-specifier динамический import
// ВИСНЕТ на CI-ранере (--experimental-vm-modules never settles → allSettled не
// резолвится → setSections не вызывается → рендер shell'а виснет); локально
// reject'ится быстро. Виновники: App/HostShell/HostRail. См. PRO-Robotech/kacho#7.
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
      { key: "storage-images", icon: "layers", label: "Образы", path: "storage/images", requiresProject: true },
      { key: "storage-snapshots", icon: "camera", label: "Снимки", path: "storage/snapshots", requiresProject: true },
    ],
  },
];
export const DASHBOARD_NAVIGATION = STORAGE_NAVIGATION;
export default STORAGE_NAVIGATION;
