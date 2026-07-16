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

export const VPC_NAVIGATION: RemoteNavSection[] = [
  {
    key: "vpc",
    segment: "vpc",
    icon: "network",
    label: "Virtual Private Cloud",
    landingPath: "vpc/networks",
    requiresProject: true,
    items: [
      { key: "vpc-networks", icon: "network", label: "Облачные сети", path: "vpc/networks", requiresProject: true },
      { key: "vpc-subnets", icon: "git-branch", label: "Подсети", path: "vpc/subnets", requiresProject: true },
      { key: "vpc-addresses", icon: "globe", label: "IP-адреса", path: "vpc/addresses", requiresProject: true },
      {
        key: "vpc-route-tables",
        icon: "route",
        label: "Таблицы маршрутов",
        path: "vpc/route-tables",
        requiresProject: true,
      },
      {
        key: "vpc-security-groups",
        icon: "shield",
        label: "Группы безопасности",
        path: "vpc/security-groups",
        requiresProject: true,
      },
      {
        key: "vpc-network-interfaces",
        icon: "cable",
        label: "Сетевые интерфейсы",
        path: "vpc/network-interfaces",
        requiresProject: true,
      },
      { key: "vpc-gateways", icon: "layers", label: "Шлюзы", path: "vpc/gateways", requiresProject: true },
      { key: "vpc-operations", icon: "activity", label: "Операции", path: "vpc/operations", requiresProject: true },
    ],
  },
];

export const DASHBOARD_NAVIGATION = VPC_NAVIGATION;
export default VPC_NAVIGATION;
