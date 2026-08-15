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

export const VPC_NAVIGATION: RemoteNavSection[] = [
  {
    key: "vpc",
    segment: "vpc",
    icon: "network",
    label: SERVICES.vpc.menuTitle,
    landingPath: "vpc/networks",
    requiresProject: true,
    items: [
      {
        key: "vpc-networks",
        icon: "network",
        label: ENTITIES.networks.plural,
        path: "vpc/networks",
        requiresProject: true,
      },
      {
        key: "vpc-subnets",
        icon: "git-branch",
        label: ENTITIES.subnets.plural,
        path: "vpc/subnets",
        requiresProject: true,
      },
      {
        key: "vpc-addresses",
        icon: "globe",
        label: ENTITIES.addresses.plural,
        path: "vpc/addresses",
        requiresProject: true,
      },
      {
        key: "vpc-route-tables",
        icon: "route",
        label: ENTITIES["route-tables"].plural,
        path: "vpc/route-tables",
        requiresProject: true,
      },
      {
        key: "vpc-security-groups",
        icon: "shield",
        label: ENTITIES["security-groups"].plural,
        path: "vpc/security-groups",
        requiresProject: true,
      },
      {
        key: "vpc-network-interfaces",
        icon: "cable",
        label: ENTITIES["network-interfaces"].plural,
        path: "vpc/network-interfaces",
        requiresProject: true,
      },
      {
        key: "vpc-gateways",
        icon: "layers",
        label: ENTITIES.gateways.plural,
        path: "vpc/gateways",
        requiresProject: true,
      },
      {
        key: "vpc-cidr-groups",
        icon: "route",
        label: ENTITIES["cidr-groups"].plural,
        path: "vpc/cidr-groups",
        requiresProject: true,
      },
      {
        key: "vpc-operations",
        icon: "activity",
        label: ENTITIES.operations.plural,
        path: "vpc/operations",
        requiresProject: true,
      },
    ],
  },
];

export const DASHBOARD_NAVIGATION = VPC_NAVIGATION;
export default VPC_NAVIGATION;
