import { useEffect, useState } from "react";
import type { FC, ReactElement } from "react";
import {
  Activity,
  Cable,
  Camera,
  Cloud,
  Folder,
  GitBranch,
  Globe,
  HardDrive,
  Home,
  KeyRound,
  Layers,
  Lock,
  LogIn,
  Network,
  Route,
  Scale,
  Search,
  Server,
  Settings,
  Shield,
  Users,
} from "lucide-react";
import {
  ApartmentOutlined,
  ApiOutlined,
  AppstoreOutlined,
  BankOutlined,
  CameraOutlined,
  ClusterOutlined,
  DesktopOutlined,
  FileImageOutlined,
  GatewayOutlined,
  GlobalOutlined,
  HddOutlined,
  HistoryOutlined,
  KeyOutlined,
  NodeIndexOutlined,
  ProductOutlined,
  ProjectOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { KachoLogo, RailButton } from "../../atoms";
import { loginUrl } from "../../../utils/auth";
import type { HostContext } from "../../../utils";
import type { RemoteIconName, RemoteNavItem, RemoteNavSection } from "dashboard/navigation";

type ShellNavItem = {
  key: string;
  icon: ReactElement;
  label: string;
  to: (projectId: string | null) => string;
  matches: (pathname: string) => boolean;
  requiresProject?: boolean;
};

const iconSize = 18;
const iconByName: Record<RemoteIconName, ReactElement> = {
  activity: <Activity size={iconSize} />,
  cable: <Cable size={iconSize} />,
  camera: <Camera size={iconSize} />,
  cloud: <Cloud size={iconSize} />,
  folder: <Folder size={iconSize} />,
  "git-branch": <GitBranch size={iconSize} />,
  globe: <Globe size={iconSize} />,
  "hard-drive": <HardDrive size={iconSize} />,
  key: <KeyRound size={iconSize} />,
  layers: <Layers size={iconSize} />,
  lock: <Lock size={iconSize} />,
  network: <Network size={iconSize} />,
  route: <Route size={iconSize} />,
  scale: <Scale size={iconSize} />,
  server: <Server size={iconSize} />,
  shield: <Shield size={iconSize} />,
  users: <Users size={iconSize} />,
};
const fallbackIcon = <Layers size={iconSize} />;

// Иконки ресурсных пунктов сайдбара — те же AntD Outlined-иконки, что таблицы/
// шапки деталей (ResourceIcon.ICONS в remote'ах). Ключ — specId (= последний
// сегмент nav-path). Синхронизация: пользователь видит один глиф ресурса и в
// рейле, и в таблице. Модульные (section) иконки остаются lucide.
const antdSize = { fontSize: iconSize };
const antdIconBySpec: Record<string, ReactElement> = {
  // vpc
  networks: <ApartmentOutlined style={antdSize} />,
  subnets: <ClusterOutlined style={antdSize} />,
  addresses: <GlobalOutlined style={antdSize} />,
  "route-tables": <NodeIndexOutlined style={antdSize} />,
  "security-groups": <SafetyOutlined style={antdSize} />,
  "network-interfaces": <ApiOutlined style={antdSize} />,
  gateways: <GatewayOutlined style={antdSize} />,
  // nlb
  "load-balancers": <ApartmentOutlined style={antdSize} />,
  listeners: <ApiOutlined style={antdSize} />,
  "target-groups": <ClusterOutlined style={antdSize} />,
  // iam
  accounts: <BankOutlined style={antdSize} />,
  projects: <ProjectOutlined style={antdSize} />,
  users: <UserOutlined style={antdSize} />,
  "service-accounts": <RobotOutlined style={antdSize} />,
  groups: <TeamOutlined style={antdSize} />,
  roles: <SafetyCertificateOutlined style={antdSize} />,
  "access-bindings": <KeyOutlined style={antdSize} />,
  operations: <HistoryOutlined style={antdSize} />,
  // compute
  instances: <DesktopOutlined style={antdSize} />,
  disks: <HddOutlined style={antdSize} />,
  // MachineType (read-only sizing-каталог, compute-remote). iconByName не несёт
  // cpu/machine-глифа → host-валидный RemoteIconName fallback `layers`, а точную
  // ресурс-иконку даёт этот specId-маппинг (как images/volumes/disk-types).
  "machine-types": <ProductOutlined style={antdSize} />,
  snapshots: <CameraOutlined style={antdSize} />,
  // storage
  volumes: <HddOutlined style={antdSize} />,
  // Образ (boot-image, storage-remote): specId "images" → FileImageOutlined.
  images: <FileImageOutlined style={antdSize} />,
  "disk-types": <AppstoreOutlined style={antdSize} />,
  // admin / system
  "address-pools": <AppstoreOutlined style={antdSize} />,
  regions: <AppstoreOutlined style={antdSize} />,
  zones: <AppstoreOutlined style={antdSize} />,
};

// specId ресурса = последний сегмент nav-path ("nlb/load-balancers" → "load-balancers").
function specIdFromPath(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? "";
}

const commonTop: ShellNavItem[] = [
  {
    key: "dashboard",
    icon: <Home size={iconSize} />,
    label: "Все сервисы",
    to: (projectId) => (projectId ? `/projects/${projectId}/dashboard` : "/dashboard"),
    matches: (pathname) => pathname === "/dashboard" || /^\/projects\/[^/]+\/dashboard$/.test(pathname),
  },
  {
    key: "search",
    icon: <Search size={iconSize} />,
    label: "Поиск",
    to: () => "/system/search",
    matches: (pathname) => pathname.startsWith("/system/search"),
  },
];

export const HostRail: FC<{
  context?: HostContext;
  currentPath?: string;
  showReachability: boolean;
  navigate?: (path: string) => void | Promise<void>;
}> = ({
  context,
  currentPath = window.location.pathname,
  showReachability,
  navigate = (path) => window.location.assign(path),
}) => {
  const projectId = context?.project?.id ?? null;
  const [sections, setSections] = useState<RemoteNavSection[]>([]);

  useEffect(() => {
    let cancelled = false;

    void Promise.allSettled([
      import("dashboard/navigation"),
      import("vpc/navigation"),
      import("compute/navigation"),
      import("storage/navigation"),
      import("nlb/navigation"),
      import("registry/navigation"),
      import("iam/navigation"),
    ])
      .then((results) => {
        if (!cancelled) {
          setSections(
            dedupeSections(
              results.flatMap((result) =>
                result.status === "fulfilled" ? normalizeRemoteNavigation(result.value) : [],
              ),
            ),
          );
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSections([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const section = activeSection(sections, currentPath);
  const sectionItems = section
    ? section.items.map(toShellItem)
    : sections.map((remoteSection) => ({
        key: `section-${remoteSection.key}`,
        icon: iconByName[remoteSection.icon] ?? fallbackIcon,
        label: remoteSection.label,
        to: (nextProjectId: string | null) => remotePath(nextProjectId, remoteSection.landingPath),
        matches: () => false,
        requiresProject: remoteSection.requiresProject,
      }));

  const renderItem = (item: ShellNavItem) => {
    const disabled = !!item.requiresProject && !projectId;
    return (
      <RailButton
        key={item.key}
        active={item.matches(currentPath)}
        disabled={disabled}
        disabledLabel="Выберите проект"
        label={item.label}
        icon={item.icon}
        onClick={() => {
          if (!disabled) void navigate(item.to(projectId));
        }}
      />
    );
  };

  return (
    <nav className="rail-nav" aria-label="Host navigation">
      <button type="button" className="rail-brand" onClick={() => navigate("/dashboard")} aria-label="Kacho">
        <KachoLogo variant="mark" size={44} />
      </button>

      <div className="rail-items">
        {commonTop.map(renderItem)}
        {sectionItems.length > 0 && <div className="rail-section-divider" />}
        {sectionItems.map(renderItem)}
      </div>

      <div className="rail-bottom">
        <RailButton
          active={currentPath.startsWith("/system/") && !showReachability}
          label="Администрирование"
          icon={<Settings size={iconSize} />}
          onClick={() => navigate("/system/regions")}
        />
        <RailButton label="Войти" icon={<LogIn size={iconSize} />} onClick={() => window.location.assign(loginUrl())} />
      </div>
    </nav>
  );
};

function activeSection(sections: RemoteNavSection[], pathname: string) {
  if (pathname.startsWith("/iam")) {
    return sections.find((section) => section.segment === "iam") ?? null;
  }

  const match = pathname.match(/^\/projects\/[^/]+\/([^/]+)/);
  if (!match) return null;
  return sections.find((section) => section.segment === match[1]) ?? null;
}

function toShellItem(item: RemoteNavItem): ShellNavItem {
  return {
    key: item.key,
    // Иконка ресурса синхронизирована с таблицами (AntD ResourceIcon по specId);
    // lucide item.icon — fallback для пунктов без ресурс-иконки.
    icon: antdIconBySpec[specIdFromPath(item.path)] ?? iconByName[item.icon] ?? fallbackIcon,
    label: item.label,
    to: (projectId) => remotePath(projectId, item.path),
    matches: (pathname) => matchesRemotePath(pathname, item.path),
    requiresProject: item.requiresProject,
  };
}

function normalizeRemoteNavigation(remote: unknown): RemoteNavSection[] {
  const maybeModule = remote as {
    DASHBOARD_NAVIGATION?: unknown;
    default?: unknown;
  };
  const candidate =
    maybeModule.DASHBOARD_NAVIGATION ??
    (isRecord(maybeModule.default) ? maybeModule.default.DASHBOARD_NAVIGATION : undefined) ??
    maybeModule.default;

  if (!Array.isArray(candidate)) {
    return [];
  }

  return candidate
    .filter(isRecord)
    .map((section) => ({
      key: stringField(section.key),
      segment: stringField(section.segment),
      icon: isIconName(section.icon) ? section.icon : "layers",
      label: stringField(section.label, stringField(section.key)),
      landingPath: stringField(section.landingPath),
      requiresProject: Boolean(section.requiresProject),
      items: Array.isArray(section.items)
        ? section.items.filter(isRecord).map((item) => ({
            key: stringField(item.key),
            icon: isIconName(item.icon) ? item.icon : "layers",
            label: stringField(item.label, stringField(item.key)),
            path: stringField(item.path),
            requiresProject: Boolean(item.requiresProject),
          }))
        : [],
    }))
    .filter((section) => section.key && section.segment && section.label && section.landingPath);
}

function dedupeSections(sections: RemoteNavSection[]): RemoteNavSection[] {
  const byKey = new Map<string, RemoteNavSection>();
  for (const section of sections) {
    byKey.set(section.key, section);
  }
  return [...byKey.values()];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function stringField(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function isIconName(value: unknown): value is RemoteIconName {
  return typeof value === "string" && value in iconByName;
}

function remotePath(projectId: string | null, path: string) {
  if (path.startsWith("/")) {
    return path;
  }
  return projectId ? `/projects/${projectId}/${path}` : "/dashboard";
}

function matchesRemotePath(pathname: string, path: string) {
  if (path.startsWith("/")) {
    return pathname === path || pathname.startsWith(`${path}/`);
  }
  return new RegExp(`^/projects/[^/]+/${path.replace(/\//g, "\\/")}(?:/|$)`).test(pathname);
}
