import type { RemoteNavSection } from "dashboard/navigation";

// Тест-фикстура nlb-навигации для host-jest. Метка/иконка секции совпадают с
// агрегатной dashboard-навигацией ("Network Load Balancer" / "scale"), чтобы
// существующие rail-ассерты оставались зелёными; реальный remote
// (nlb/src/navigation.ts) несёт продуктовую метку "Network Load Balancing".
export const NLB_NAVIGATION: RemoteNavSection[] = [
  {
    key: "nlb",
    segment: "nlb",
    icon: "scale",
    label: "Network Load Balancer",
    landingPath: "nlb/load-balancers",
    requiresProject: true,
    items: [
      {
        key: "nlb-load-balancers",
        icon: "scale",
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
