// Навигационный слой консоли: ОДИН источник на оба уровня сайдбара.
//
// Первый уровень (рейл 62px) показывает МОДУЛИ, второй — типы ресурсов
// активного модуля. Оба читают один и тот же перечень секций, приезжающий из
// удалённых модулей. Держать его в компоненте рейла значило бы, что второй
// уровень заводит СВОЮ копию загрузки и нормализации, — а копия расходится
// молча, как это уже было с общим листом стилей.

import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import {
  Activity,
  Cable,
  Camera,
  Cloud,
  Folder,
  GitBranch,
  Globe,
  HardDrive,
  KeyRound,
  Layers,
  Lock,
  Network,
  Route,
  Scale,
  Server,
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
  ContainerOutlined,
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
  TagsOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { REMOTE_MODULES } from "../remotes/moduleCatalog";
import type { RemoteIconName, RemoteNavItem, RemoteNavSection } from "dashboard/navigation";

export type ShellNavItem = {
  key: string;
  icon: ReactElement;
  label: string;
  to: (projectId: string | null) => string;
  matches: (pathname: string) => boolean;
  requiresProject?: boolean;
  /** Модуль раздела не загрузился: кнопка остаётся, переход закрыт (#371). */
  unavailable?: boolean;
};

/** Раздел рейла с пометкой недоступности его модуля. */
export type RailSection = RemoteNavSection & { unavailable?: boolean };

// Загрузчики навигации — по одному literal-спецификатору на remote, иначе
// @originjs/vite-plugin-federation перестанет резолвить их статически.
const REMOTE_NAV_LOADERS: Record<string, () => Promise<unknown>> = {
  dashboard: () => import("dashboard/navigation"),
  vpc: () => import("vpc/navigation"),
  compute: () => import("compute/navigation"),
  storage: () => import("storage/navigation"),
  nlb: () => import("nlb/navigation"),
  registry: () => import("registry/navigation"),
  iam: () => import("iam/navigation"),
  // Администрирование СПРАШИВАЕТСЯ наравне с прочими. Его здесь не было, при
  // том что модуль навигацию экспонирует (`./navigation` в его объявлении), а
  // каркас объявляет его удалённым. Следствие: раздел оставался без второго
  // сайдбара вовсе и подавал свои разделы горизонтальными вкладками — второй
  // конструкцией навигации в одном продукте.
  //
  // Перечень рукописный by construction: спецификатор импорта обязан быть
  // литералом, иначе плагин федерации перестанет резолвить его статически. Цена
  // названа прямо: забытая строка не роняет ничего и молча снимает раздел с
  // навигации — ровно это здесь и произошло.
  system: () => import("system/navigation"),
};
export const NAV_REMOTES = Object.keys(REMOTE_NAV_LOADERS);
// Подсказка на кнопке рейла. Говорит ПОЛЬЗОВАТЕЛЮ, а не разработчику: «модуль
// не загрузился» объясняет устройство консоли тому, кто про него не знает, и
// пугает. Причина отказа остаётся в журнале браузера. Текст держится тем же
// словом, что и экран отказа раздела, — состояние одно, значит и подпись одна.
export const UNAVAILABLE_REASON = "Раздел временно недоступен: ведутся технические работы";

export const loadRemoteNavigation = (remote: string): Promise<unknown> => REMOTE_NAV_LOADERS[remote]();

export const iconSize = 18;
export const iconByName: Record<RemoteIconName, ReactElement> = {
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

export const fallbackIcon = <Layers size={iconSize} />;

// Иконки ресурсных пунктов сайдбара — те же AntD Outlined-иконки, что таблицы/
// шапки деталей (ResourceIcon.ICONS в remote'ах). Ключ — specId (= последний
// сегмент nav-path). Синхронизация: пользователь видит один глиф ресурса и в
// рейле, и в таблице. Модульные (section) иконки остаются lucide.
export const antdSize = { fontSize: iconSize };
export const antdIconBySpec: Record<string, ReactElement> = {
  // vpc
  networks: <ApartmentOutlined style={antdSize} />,
  subnets: <ClusterOutlined style={antdSize} />,
  addresses: <GlobalOutlined style={antdSize} />,
  "route-tables": <NodeIndexOutlined style={antdSize} />,
  "security-groups": <SafetyOutlined style={antdSize} />,
  "network-interfaces": <ApiOutlined style={antdSize} />,
  gateways: <GatewayOutlined style={antdSize} />,
  "cidr-groups": <TagsOutlined style={antdSize} />,
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
  "placement-groups": <ContainerOutlined style={antdSize} />,
  // MachineType (read-only sizing-каталог, compute-remote). iconByName не несёт
  // cpu/machine-глифа → host-валидный RemoteIconName fallback `layers`, а точную
  // ресурс-иконку даёт этот specId-маппинг (как images/volumes/disk-types).
  "machine-types": <ProductOutlined style={antdSize} />,
  // storage — блочное хранение целиком принадлежит storage-remote'у
  // (Volume/Snapshot/Image/DiskType); отдельного раздела дисков у compute нет.
  volumes: <HddOutlined style={antdSize} />,
  snapshots: <CameraOutlined style={antdSize} />,
  // Образ (boot-image, storage-remote): specId "images" → FileImageOutlined.
  images: <FileImageOutlined style={antdSize} />,
  "disk-types": <AppstoreOutlined style={antdSize} />,
  // admin / system
  "address-pools": <AppstoreOutlined style={antdSize} />,
  regions: <AppstoreOutlined style={antdSize} />,
  zones: <AppstoreOutlined style={antdSize} />,
};

// specId ресурса = последний сегмент nav-path ("nlb/load-balancers" → "load-balancers").
export function specIdFromPath(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? "";
}

/**
 * Раздел, которому принадлежит адрес, — по ПЕРВОМУ СЕГМЕНТУ, а не по перечню
 * особых случаев.
 *
 * Адреса консоли двух видов: область проекта (`/projects/<id>/<раздел>/…`) и
 * область аккаунта либо кластера (`/<раздел>/…` — сегодня это `iam` и
 * `system`). Прежде здесь перечислялись случаи, и `iam` был назван поимённо, а
 * `system` — нет: раздел администрирования оставался БЕЗ второго сайдбара
 * вовсе, и перечень своих разделов подавал горизонтальными вкладками, то есть
 * второй конструкцией навигации в одном продукте.
 *
 * Теперь сопоставление идёт по объявленному `segment` любого раздела, и
 * следующий раздел вне области проекта заработает сам, без правки этой функции.
 *
 * ПЕРВЫЙ СЕГМЕНТ — НЕ ЕДИНСТВЕННЫЙ ПРИЗНАК: раздел может лежать ВНУТРИ чужого
 * адресного пространства. «Токены и ключи» объявлены своим разделом (рейл
 * показывает их отдельной плиткой), а живут под `/system/tokens/…` — по первому
 * сегменту находился сосед, и колонка показывала ЕГО пункты. Пока у этой части
 * была собственная полоса вкладок, расхождение не бросалось в глаза; как только
 * полосу сняли, части одного раздела остались вовсе без перечня.
 *
 * Поэтому сначала спрашивается ПРИНАДЛЕЖНОСТЬ АДРЕСА: раздел, чей пункт
 * совпадает с открытым адресом, и есть тот, чьи пункты надо показать. Сегмент
 * остаётся запасным признаком — он отвечает на посадочных адресах, где ни один
 * пункт ещё не выбран.
 */
export function activeSection(sections: RailSection[], pathname: string): RailSection | null {
  const inProject = pathname.match(/^\/projects\/[^/]+\/([^/]+)/);
  if (inProject) return sections.find((section) => section.segment === inProject[1]) ?? null;

  const top = pathname.split("/").filter(Boolean)[0];
  if (!top) return null;

  // Принадлежность адреса пункту — сильнее совпадения по сегменту: у вложенного
  // раздела сегмент не совпадает с первым сегментом его же адресов.
  const byItem = sections.find((section) =>
    section.items.some((item) => pathname === item.path || pathname.startsWith(`${item.path}/`)),
  );
  if (byItem) return byItem;

  return sections.find((section) => section.segment === top) ?? null;
}

export function toShellItem(item: RemoteNavItem): ShellNavItem {
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

export function normalizeRemoteNavigation(remote: unknown): RailSection[] {
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
            // Темы приезжают из чужого бандла, поэтому просеиваются так же, как
            // остальные поля: пункт с негодной темой не вправе обрушить всю
            // навигацию модуля.
            //
            // Прежде здесь просеивалась ПАРА «подпись + адрес», и условие
            // требовало непустого адреса. Адрес у всех был «#» — то есть
            // мёртвое значение оказалось несущим: сними его, и темы пропали бы
            // из панели, пройдя этот самый отбор. Теперь тема — строка, и
            // отбор судит ровно то, что показывается (#1611).
            docs: Array.isArray(item.docs)
              ? item.docs.map((doc) => stringField(doc)).filter((doc) => doc.length > 0)
              : undefined,
          }))
        : [],
    }))
    .filter((section) => section.key && section.segment && section.label && section.landingPath);
}

export function dedupeSections(sections: RailSection[]): RailSection[] {
  const byKey = new Map<string, RailSection>();
  for (const section of sections) {
    byKey.set(section.key, section);
  }
  return [...byKey.values()];
}

/**
 * Проставляет пометку недоступности разделам модулей, чья навигация не приехала,
 * и ДОБАВЛЯЕТ раздел, если его не дал никто (агрегат `dashboard` мог упасть
 * вместе с ним). Запасное описание раздела берётся из каталога модулей —
 * единственного места, где живёт имя раздела.
 */
export function withUnavailable(sections: RailSection[], downRemotes: string[]): RailSection[] {
  if (downRemotes.length === 0) return sections;
  const down = new Set(downRemotes);
  const out = sections.map((section) => (down.has(section.key) ? { ...section, unavailable: true } : section));
  const present = new Set(out.map((section) => section.key));

  for (const module of REMOTE_MODULES) {
    if (!module.section || !down.has(module.remote) || present.has(module.section.key)) continue;
    out.push({
      key: module.section.key,
      segment: module.section.segment,
      icon: module.section.icon,
      label: module.label,
      landingPath: module.section.landingPath,
      requiresProject: module.section.requiresProject,
      items: [],
      unavailable: true,
    });
  }

  return out;
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

export function remotePath(projectId: string | null, path: string) {
  if (path.startsWith("/")) {
    return path;
  }
  return projectId ? `/projects/${projectId}/${path}` : "/dashboard";
}

export function matchesRemotePath(pathname: string, path: string) {
  if (path.startsWith("/")) {
    return pathname === path || pathname.startsWith(`${path}/`);
  }
  return new RegExp(`^/projects/[^/]+/${path.replace(/\//g, "\\/")}(?:/|$)`).test(pathname);
}

/**
 * Перечень секций модулей: грузится ОДИН раз на страницу и раздаётся обоим
 * уровням сайдбара.
 *
 * Кэш — промис, а не результат: два уровня монтируются одновременно, и без него
 * federation-импорты пошли бы дважды. Ключ загрузчика в кэше не участвует
 * намеренно — подменённый загрузчик нужен пробе, и проба обязана получать
 * СВОЙ ответ, а не чужой из кэша.
 */
let sectionsPromise: Promise<RailSection[]> | null = null;

async function fetchSections(load: (remote: string) => Promise<unknown>): Promise<RailSection[]> {
  const results = await Promise.allSettled(NAV_REMOTES.map((remote) => load(remote)));
  const loaded = results.flatMap((r) => (r.status === "fulfilled" ? normalizeRemoteNavigation(r.value) : []));
  const down = NAV_REMOTES.filter((_, i) => results[i].status === "rejected");
  // Раздел упавшего модуля НЕ выпадает из меню: он остаётся под своим именем с
  // пометкой недоступности. Пропавший раздел неотличим от «такого сервиса нет»
  // — это и есть тихая форма отказа (#371).
  return withUnavailable(dedupeSections(loaded), down);
}

export function useModuleSections(load: (remote: string) => Promise<unknown> = loadRemoteNavigation): RailSection[] {
  const [sections, setSections] = useState<RailSection[]>([]);

  useEffect(() => {
    let cancelled = false;
    const isDefault = load === loadRemoteNavigation;
    const promise = isDefault ? (sectionsPromise ??= fetchSections(load)) : fetchSections(load);
    promise
      .then((next) => {
        if (!cancelled) setSections(next);
      })
      .catch(() => {
        if (!cancelled) setSections(withUnavailable([], NAV_REMOTES));
      });
    return () => {
      cancelled = true;
    };
    // Загрузчик читается ОДИН раз: зависимость от него перезапускала бы загрузку
    // после каждого setSections (обёртка, созданная на рендере, каждый раз
    // новая) — бесконечный цикл рендера, который выглядит как «прогон завис», а
    // не как падение.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return sections;
}
