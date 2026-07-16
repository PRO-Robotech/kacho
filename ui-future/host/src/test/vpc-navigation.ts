export const DASHBOARD_NAVIGATION = [
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
];

export default DASHBOARD_NAVIGATION;
