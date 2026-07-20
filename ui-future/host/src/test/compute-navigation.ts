import type { RemoteNavSection } from "dashboard/navigation";

// Тест-стаб compute/navigation для host-jest. Зеркалит compute-remote nav (Instance
// + MachineType), чтобы rail-ассерты проверяли появление ресурс-пунктов новой
// редизайн-модели. Стаб также обязателен, чтобы HostRail's
// Promise.allSettled(import("compute/navigation")) РЕЗОЛВИЛСЯ под jest: без стаба
// незастабленный bare-specifier динамический import ВИСНЕТ на CI-ранере
// (--experimental-vm-modules never settles → allSettled не резолвится →
// setSections не вызывается → рендер shell'а виснет); локально reject'ится быстро.
// Виновники: App/HostShell/HostRail (все рендерят HostRail). См. PRO-Robotech/kacho#7.
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
      {
        key: "compute-machine-types",
        icon: "layers",
        label: "Типы машин",
        path: "compute/machine-types",
        requiresProject: true,
      },
    ],
  },
];
export const DASHBOARD_NAVIGATION = COMPUTE_NAVIGATION;
export default COMPUTE_NAVIGATION;
