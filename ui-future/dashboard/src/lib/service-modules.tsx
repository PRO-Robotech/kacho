import type { ReactNode } from "react";
import {
  Activity,
  Boxes,
  Camera,
  Cloud,
  FolderKanban,
  Globe2,
  HardDrive,
  History,
  KeyRound,
  Layers3,
  Network,
  Route,
  Scale,
  ScanSearch,
  ShieldCheck,
  UserRound,
} from "lucide-react";

export interface ModuleStat {
  key: string;
  label: string;
  listPath: string;
  payloadKey: string;
}

export interface ServiceModule {
  key: string;
  segment: string;
  label: string;
  short: string;
  icon: ReactNode;
  color: string;
  description: string;
  requiresProject?: boolean;
  landing: (projectId: string | null, accountId: string | null) => string | null;
  stats: ModuleStat[];
}

const iconSize = 16;

export const SERVICE_MODULES: ServiceModule[] = [
  {
    key: "vpc",
    segment: "vpc",
    label: "Virtual Private Cloud",
    short: "VPC",
    icon: <Network size={iconSize} />,
    color: "#3D8DF5",
    description: "Облачные сети, подсети, группы безопасности, публичные IP, таблицы маршрутизации.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/vpc/networks` : null),
    stats: [
      { key: "networks", label: "Сетей", listPath: "/vpc/v1/networks", payloadKey: "networks" },
      { key: "subnets", label: "Подсетей", listPath: "/vpc/v1/subnets", payloadKey: "subnets" },
      {
        key: "sgs",
        label: "Групп безопасности",
        listPath: "/vpc/v1/securityGroups",
        payloadKey: "securityGroups",
      },
    ],
  },
  {
    key: "compute",
    segment: "compute",
    label: "Compute Cloud",
    short: "Compute",
    icon: <Cloud size={iconSize} />,
    color: "#36CFC9",
    description: "Виртуальные машины, диски, образы и снимки дисков.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/compute/instances` : null),
    stats: [
      { key: "instances", label: "Машин", listPath: "/compute/v1/instances", payloadKey: "instances" },
      { key: "disks", label: "Дисков", listPath: "/compute/v1/disks", payloadKey: "disks" },
      { key: "images", label: "Образов", listPath: "/compute/v1/images", payloadKey: "images" },
    ],
  },
  {
    key: "nlb",
    segment: "nlb",
    label: "Network Load Balancer",
    short: "NLB",
    icon: <Scale size={iconSize} />,
    color: "#FA8C16",
    description: "L4 балансировщики трафика TCP/UDP: LoadBalancer, Listener, Target Group.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/nlb/load-balancers` : null),
    stats: [
      {
        key: "load-balancers",
        label: "Балансировщиков",
        listPath: "/nlb/v1/networkLoadBalancers",
        payloadKey: "networkLoadBalancers",
      },
      { key: "listeners", label: "Listeners", listPath: "/nlb/v1/listeners", payloadKey: "listeners" },
      { key: "target-groups", label: "Target Groups", listPath: "/nlb/v1/targetGroups", payloadKey: "targetGroups" },
    ],
  },
  {
    key: "iam",
    segment: "iam",
    label: "Identity and Access Management",
    short: "IAM",
    icon: <KeyRound size={iconSize} />,
    color: "#9B59F6",
    description: "Аккаунты, проекты, пользователи, сервисные аккаунты, группы, роли и связки прав.",
    landing: () => "/iam/accounts",
    stats: [
      { key: "accounts", label: "Аккаунтов", listPath: "/iam/v1/accounts", payloadKey: "accounts" },
      { key: "projects", label: "Проектов", listPath: "/iam/v1/projects", payloadKey: "projects" },
      { key: "roles", label: "Ролей", listPath: "/iam/v1/roles", payloadKey: "roles" },
    ],
  },
];

export const COMMON_DASHBOARD_ICONS = {
  empty: <FolderKanban size={40} />,
  go: <Activity size={14} />,
  locked: <ShieldCheck size={14} />,
  search: <ScanSearch size={16} />,
  route: <Route size={16} />,
  globe: <Globe2 size={16} />,
  layers: <Layers3 size={16} />,
  boxes: <Boxes size={16} />,
  disk: <HardDrive size={16} />,
  camera: <Camera size={16} />,
  history: <History size={16} />,
  user: <UserRound size={16} />,
};
