// service-modules — реестр «компонентов» (опубликованных сервисов) Kachō-консоли.
//
// Каждый модуль (vpc / compute) описывает:
//   • плашку на дашборде (label / icon / color / description / список stat-метрик);
//   • собственный набор ссылок сайдбара (items) — рендерится, когда активен этот модуль.
//
// Модуль считается «активным», если текущий URL начинается с `/projects/:projectId/<segment>/...`
// (см. moduleFromPathname). Дашборд / IAM / System — вне модулей; сайдбар
// в этом случае показывает лаунчеры модулей (COMMON_TOP → лаунчеры → COMMON_BOTTOM).

import type { ReactNode } from "react";
import {
  HomeOutlined,
  SearchOutlined,
  ApartmentOutlined,
  ClusterOutlined,
  GlobalOutlined,
  NodeIndexOutlined,
  SafetyOutlined,
  GatewayOutlined,
  ApiOutlined,
  HistoryOutlined,
  DesktopOutlined,
  HddOutlined,
  FileImageOutlined,
  CameraOutlined,
  SettingOutlined,
  UserOutlined,
  CloudServerOutlined,
  LockOutlined,
} from "@ant-design/icons";

export interface NavLeaf {
  key: string;
  icon: ReactNode;
  /** Tooltip / aria-label. */
  label: string;
  to: (projectId: string | null) => string;
  matches: (pathname: string) => boolean;
  requiresProject?: boolean;
}

/** Stat-метрика плашки: считается через GET `${listPath}?project_id=…&pageSize=1000` → `resp[payloadKey].length`. */
export interface ModuleStat {
  key: string;
  label: string;
  listPath: string;
  payloadKey: string;
}

export interface ServiceModule {
  /** Стабильный ключ модуля (`vpc` | `compute`). */
  key: string;
  /** URL-сегмент под `/projects/:projectId/`. */
  segment: string;
  /** Полное имя для плашки. */
  label: string;
  /** Короткое имя (бейдж / breadcrumb). */
  short: string;
  icon: ReactNode;
  color: string;
  description: string;
  /** `true` — модуль project-scoped (VPC/Compute): дашборд-плашка кликабельна
   *  только когда выбран project; иначе — disabled-плашка с подсказкой. */
  requiresProject?: boolean;
  /** Реальный route для перехода. Возвращает `null`, если перейти нельзя
   *  (project-scoped модуль без выбранного project) — DashboardPage делает
   *  плашку disabled. */
  landing: (projectId: string | null, accountId: string | null) => string | null;
  stats: ModuleStat[];
  items: NavLeaf[];
}

const seg = (f: string | null, path: string) => (f ? `/projects/${f}/${path}` : "/dashboard");
const projectRe = (path: string) => new RegExp(`^/projects/[^/]+/${path.replace(/\//g, "\\/")}`);
const iamSeg = (path: string) => `/iam/${path}`;
const iamRe = (path: string) => new RegExp(`^/iam/${path.replace(/\//g, "\\/")}`);

export const SERVICE_MODULES: ServiceModule[] = [
  {
    key: "vpc",
    segment: "vpc",
    label: "Virtual Private Cloud",
    short: "VPC",
    icon: <ApartmentOutlined />,
    color: "#3D8DF5",
    description: "Облачные сети, подсети, группы безопасности, публичные IP, таблицы маршрутизации.",
    requiresProject: true,
    landing: (f) => (f ? `/projects/${f}/vpc/networks` : null),
    stats: [
      { key: "networks", label: "Сетей", listPath: "/vpc/v1/networks", payloadKey: "networks" },
      { key: "subnets", label: "Подсетей", listPath: "/vpc/v1/subnets", payloadKey: "subnets" },
      { key: "sgs", label: "Групп безопасности", listPath: "/vpc/v1/securityGroups", payloadKey: "security_groups" },
    ],
    items: [
      {
        key: "networks",
        icon: <ApartmentOutlined />,
        label: "Облачные сети",
        to: (f) => seg(f, "vpc/networks"),
        matches: (p) => projectRe("vpc/networks").test(p),
        requiresProject: true,
      },
      {
        key: "subnets",
        icon: <ClusterOutlined />,
        label: "Подсети",
        to: (f) => seg(f, "vpc/subnets"),
        matches: (p) => projectRe("vpc/subnets").test(p),
        requiresProject: true,
      },
      {
        key: "addresses",
        icon: <GlobalOutlined />,
        label: "IP-адреса",
        to: (f) => seg(f, "vpc/addresses"),
        matches: (p) => projectRe("vpc/addresses").test(p),
        requiresProject: true,
      },
      {
        key: "route-tables",
        icon: <NodeIndexOutlined />,
        label: "Таблицы маршрутов",
        to: (f) => seg(f, "vpc/route-tables"),
        matches: (p) => projectRe("vpc/route-tables").test(p),
        requiresProject: true,
      },
      {
        key: "security-groups",
        icon: <SafetyOutlined />,
        label: "Группы безопасности",
        to: (f) => seg(f, "vpc/security-groups"),
        matches: (p) => projectRe("vpc/security-groups").test(p),
        requiresProject: true,
      },
      {
        key: "network-interfaces",
        icon: <ApiOutlined />,
        label: "Сетевые интерфейсы",
        to: (f) => seg(f, "vpc/network-interfaces"),
        matches: (p) => projectRe("vpc/network-interfaces").test(p),
        requiresProject: true,
      },
      {
        key: "gateways",
        icon: <GatewayOutlined />,
        label: "Шлюзы",
        to: (f) => seg(f, "vpc/gateways"),
        matches: (p) => projectRe("vpc/gateways").test(p),
        requiresProject: true,
      },
      {
        key: "operations",
        icon: <HistoryOutlined />,
        label: "Операции",
        to: (f) => seg(f, "vpc/operations"),
        matches: (p) => projectRe("vpc/operations").test(p),
        requiresProject: true,
      },
    ],
  },
  {
    key: "compute",
    segment: "compute",
    label: "Compute Cloud",
    short: "Compute",
    icon: <CloudServerOutlined />,
    color: "#36CFC9",
    description: "Виртуальные машины, диски, образы и снимки дисков.",
    requiresProject: true,
    landing: (f) => (f ? `/projects/${f}/compute/instances` : null),
    stats: [
      { key: "instances", label: "Машин", listPath: "/compute/v1/instances", payloadKey: "instances" },
      { key: "disks", label: "Дисков", listPath: "/compute/v1/disks", payloadKey: "disks" },
      { key: "images", label: "Образов", listPath: "/compute/v1/images", payloadKey: "images" },
    ],
    items: [
      {
        key: "compute-instances",
        icon: <DesktopOutlined />,
        label: "Виртуальные машины",
        to: (f) => seg(f, "compute/instances"),
        matches: (p) => projectRe("compute/instances").test(p),
        requiresProject: true,
      },
      {
        key: "compute-disks",
        icon: <HddOutlined />,
        label: "Диски",
        to: (f) => seg(f, "compute/disks"),
        matches: (p) => projectRe("compute/disks").test(p),
        requiresProject: true,
      },
      {
        key: "compute-images",
        icon: <FileImageOutlined />,
        label: "Образы",
        to: (f) => seg(f, "compute/images"),
        matches: (p) => projectRe("compute/images").test(p),
        requiresProject: true,
      },
      {
        key: "compute-snapshots",
        icon: <CameraOutlined />,
        label: "Снимки дисков",
        to: (f) => seg(f, "compute/snapshots"),
        matches: (p) => projectRe("compute/snapshots").test(p),
        requiresProject: true,
      },
    ],
  },
  // KAC-141 / KAC-171: NLB module — L4 Network Load Balancer.
  {
    key: "nlb",
    segment: "nlb",
    label: "Network Load Balancer",
    short: "NLB",
    icon: <NodeIndexOutlined />,
    color: "#FA8C16",
    description: "L4 балансировщики трафика TCP/UDP: LoadBalancer, Listener, Target Group.",
    requiresProject: true,
    landing: (f) => (f ? `/projects/${f}/nlb/load-balancers` : null),
    stats: [
      {
        key: "load-balancers",
        label: "Балансировщиков",
        listPath: "/nlb/v1/networkLoadBalancers",
        payloadKey: "network_load_balancers",
      },
      { key: "listeners", label: "Listeners", listPath: "/nlb/v1/listeners", payloadKey: "listeners" },
      { key: "target-groups", label: "Target Groups", listPath: "/nlb/v1/targetGroups", payloadKey: "target_groups" },
    ],
    items: [
      {
        key: "load-balancers",
        icon: <ApartmentOutlined />,
        label: "Балансировщики",
        to: (f) => seg(f, "nlb/load-balancers"),
        matches: (p) => projectRe("nlb/load-balancers").test(p),
        requiresProject: true,
      },
      {
        key: "listeners",
        icon: <ApiOutlined />,
        label: "Listeners",
        to: (f) => seg(f, "nlb/listeners"),
        matches: (p) => projectRe("nlb/listeners").test(p),
        requiresProject: true,
      },
      {
        key: "target-groups",
        icon: <ClusterOutlined />,
        label: "Target Groups",
        to: (f) => seg(f, "nlb/target-groups"),
        matches: (p) => projectRe("nlb/target-groups").test(p),
        requiresProject: true,
      },
      {
        key: "nlb-operations",
        icon: <HistoryOutlined />,
        label: "Операции",
        to: (f) => seg(f, "nlb/operations"),
        matches: (p) => projectRe("nlb/operations").test(p),
        requiresProject: true,
      },
    ],
  },
  // KAC-117/120: IAM — отдельный module-block, параллельно VPC/Compute.
  // IAM ресурсы: Account, Project, User, ServiceAccount, Group, Role, AccessBinding.
  // Не требует project context (живёт на уровне /iam/*).
  {
    key: "iam",
    segment: "iam",
    label: "Identity and Access Management",
    short: "IAM",
    icon: <LockOutlined />,
    color: "#9B59F6",
    description: "Аккаунты, проекты, пользователи, сервисные аккаунты, группы, роли и связки прав.",
    landing: () => "/iam/accounts",
    stats: [
      { key: "accounts", label: "Аккаунтов", listPath: "/iam/v1/accounts", payloadKey: "accounts" },
      { key: "projects", label: "Проектов", listPath: "/iam/v1/projects", payloadKey: "projects" },
      { key: "roles", label: "Ролей", listPath: "/iam/v1/roles", payloadKey: "roles" },
    ],
    items: [
      {
        key: "iam-accounts",
        icon: <ApartmentOutlined />,
        label: "Аккаунты",
        to: () => iamSeg("accounts"),
        matches: (p) => iamRe("accounts").test(p),
      },
      {
        key: "iam-projects",
        icon: <ClusterOutlined />,
        label: "Проекты",
        to: () => iamSeg("projects"),
        matches: (p) => iamRe("projects").test(p),
      },
      {
        key: "iam-users",
        icon: <UserOutlined />,
        label: "Пользователи",
        to: () => iamSeg("users"),
        matches: (p) => iamRe("users").test(p),
      },
      {
        key: "iam-service-accounts",
        icon: <ApiOutlined />,
        label: "Сервисные аккаунты",
        to: () => iamSeg("service-accounts"),
        matches: (p) => iamRe("service-accounts").test(p),
      },
      {
        key: "iam-groups",
        icon: <NodeIndexOutlined />,
        label: "Группы",
        to: () => iamSeg("groups"),
        matches: (p) => iamRe("groups").test(p),
      },
      {
        key: "iam-roles",
        icon: <SafetyOutlined />,
        label: "Роли",
        to: () => iamSeg("roles"),
        matches: (p) => iamRe("roles").test(p),
      },
      {
        key: "iam-access-bindings",
        icon: <GatewayOutlined />,
        label: "Связки прав",
        to: () => iamSeg("access-bindings"),
        matches: (p) => iamRe("access-bindings").test(p),
      },
    ],
  },
];

/** Активный модуль по URL — `/projects/:projectId/<segment>/...` → ServiceModule | null.
 *  Также матчит `/iam/...` → IAM module (которому проектный контекст не нужен). */
export function moduleFromPathname(pathname: string): ServiceModule | null {
  if (pathname.startsWith("/iam")) {
    return SERVICE_MODULES.find((mod) => mod.segment === "iam") ?? null;
  }
  const m = pathname.match(/^\/projects\/[^/]+\/([^/]+)/);
  if (!m) return null;
  return SERVICE_MODULES.find((mod) => mod.segment === m[1]) ?? null;
}

/** Верхний общий блок сайдбара (всегда виден). */
export const COMMON_TOP: NavLeaf[] = [
  {
    key: "dashboard",
    icon: <HomeOutlined />,
    label: "Все сервисы",
    to: (f) => (f ? `/projects/${f}/dashboard` : "/dashboard"),
    matches: (p) => p === "/dashboard" || /^\/projects\/[^/]+\/dashboard$/.test(p),
  },
  {
    key: "search",
    icon: <SearchOutlined />,
    label: "Поиск",
    to: () => "/system/search",
    matches: (p) => p.startsWith("/system/search"),
  },
];

/** Нижний общий блок сайдбара (всегда виден). KAC-120: IAM удалён отсюда —
 *  теперь регистрируется как module-block в SERVICE_MODULES (параллельно VPC/Compute).
 *  KAC-198: `profile` leaf убран — user-menu (avatar + email + Профиль/Выйти dropdown)
 *  теперь рендерится в `ServiceSidebar` как отдельный компонент `SidebarUserButton`
 *  вместо обычного NavLeaf — для unauthenticated case он показывает LoginButton. */
export const COMMON_BOTTOM: NavLeaf[] = [
  {
    key: "system",
    icon: <SettingOutlined />,
    label: "Администрирование",
    to: () => "/system/regions",
    // KAC-196: добавлен /system/cluster/admins (cluster RBAC management) под
    // тем же admin-entry; AdminLayout-табы рендерят его как отдельный таб.
    matches: (p) => /^\/system\/(regions|zones|address-pools|cluster\/admins)/.test(p),
  },
];
