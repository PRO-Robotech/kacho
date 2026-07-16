import type { RemoteNavSection } from "dashboard/navigation";

export const DASHBOARD_NAVIGATION: RemoteNavSection[] = [
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
    ],
  },
  {
    key: "compute",
    segment: "compute",
    icon: "cloud",
    label: "Compute Cloud",
    landingPath: "compute/instances",
    requiresProject: true,
    items: [],
  },
  {
    key: "nlb",
    segment: "nlb",
    icon: "scale",
    label: "Network Load Balancer",
    landingPath: "nlb/load-balancers",
    requiresProject: true,
    items: [],
  },
  {
    key: "iam",
    segment: "iam",
    icon: "key",
    label: "Identity and Access Management",
    landingPath: "/iam/accounts",
    items: [{ key: "iam-accounts", icon: "layers", label: "Аккаунты", path: "/iam/accounts" }],
  },
];
