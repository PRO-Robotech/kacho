import type { FC } from "react";
import { Home, LogIn, Search, Settings } from "lucide-react";
import { KachoLogo, RailButton } from "../../atoms";
import { loginUrl } from "../../../utils/auth";
import type { HostContext } from "../../../utils";
import { REMOTE_MODULES } from "../../../remotes/moduleCatalog";
import {
  UNAVAILABLE_REASON,
  activeSection,
  iconByName,
  iconSize,
  fallbackIcon,
  loadRemoteNavigation,
  remotePath,
  useModuleSections,
  type ShellNavItem,
} from "../../../lib/module-navigation";

/**
 * Разделы, которые рейл ставит САМ, — в полосе модулей их нет.
 *
 * Каталог модулей хоста объявляет это прямо: у `dashboard` и `system` нет
 * `section`, потому что первый — «Все сервисы» в верхней паре, а второй —
 * «Администрирование» внизу рейла. Сверять надо с каталогом, а не с памятью:
 * выписанный здесь перечень разошёлся бы с ним молча.
 *
 * Ключ раздела равен имени remote'а — это объявлено самим каталогом
 * («Имя remote'а федерации — оно же ключ раздела навигации»), поэтому сравнение
 * идёт по нему, а не по второй, выведенной здесь величине.
 *
 * ПОЧЕМУ ОТСЕКАЕТСЯ ТОЛЬКО ОБЪЯВЛЕННОЕ. Раздел, приехавший от remote'а, которого
 * в каталоге нет вовсе, из полосы НЕ пропадает: тихо исчезнувший раздел
 * неотличим от «такого сервиса нет» (#371) — это и есть тихая форма отказа.
 * Здесь снимается ровно то, чему каталог уже назначил другое место.
 *
 * Зачем это понадобилось: администрирование стало спрашиваться наравне с прочими
 * модулями (ради второго сайдбара, `module-navigation.tsx`), и его раздел поехал
 * в полосу модулей ВДОБАВОК к кнопке внизу — две кнопки с одним именем и одним
 * адресом назначения в одной полосе навигации.
 */
const SELF_PLACED_SECTIONS = new Set(REMOTE_MODULES.filter((module) => !module.section).map((module) => module.remote));

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

/**
 * Первый уровень сайдбара — МОДУЛИ, и только они.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ТИПОВ РЕСУРСОВ ЗДЕСЬ БОЛЬШЕ НЕТ
 *
 * Прежде рейл показывал типы ресурсов активного модуля — сети, подсети, таблицы
 * маршрутов — теми же иконками 44×42 и в той же полосе, что и сами модули. Два
 * разных уровня иерархии стояли одним списком, различаясь только разделителем,
 * и оба без подписей: по глифу приходилось угадывать и «какой это сервис», и
 * «какой это ресурс».
 *
 * Хуже того, перечень модулей при этом ИСЧЕЗАЛ: войдя в VPC, пользователь
 * переставал видеть Compute и Storage — переход между сервисами шёл через
 * «Все сервисы» и обратно.
 *
 * Теперь уровни разведены: здесь модули, всегда все; типы ресурсов активного
 * модуля — во втором уровне (`ModuleNav`), списком и с подписями.
 */
export const HostRail: FC<{
  context?: HostContext;
  currentPath?: string;
  showReachability: boolean;
  navigate?: (path: string) => void | Promise<void>;
  /**
   * Порт загрузки навигации модуля. Существует ради пробы: неразрешимый адрес
   * точки входа подаётся сюда ровно тем, чем он приходит в браузере — отказом
   * промиса. Умолчание — настоящие federation-импорты.
   */
  loadNavigation?: (remote: string) => Promise<unknown>;
}> = ({
  context,
  currentPath = window.location.pathname,
  showReachability,
  navigate = (path) => window.location.assign(path),
  loadNavigation = loadRemoteNavigation,
}) => {
  const projectId = context?.project?.id ?? null;
  const sections = useModuleSections(loadNavigation);
  const current = activeSection(sections, currentPath);

  // Лаунчер модуля — сам раздел, а не его пункты. Перечень полный ВСЕГДА:
  // уйти из модуля обязано быть можно, не возвращаясь на дашборд.
  const moduleItems: ShellNavItem[] = sections
    .filter((section) => !SELF_PLACED_SECTIONS.has(section.key))
    .map((section) => ({
      key: `section-${section.key}`,
      icon: iconByName[section.icon] ?? fallbackIcon,
      label: section.label,
      to: (nextProjectId: string | null) => remotePath(nextProjectId, section.landingPath),
      matches: () => current?.key === section.key,
      requiresProject: section.requiresProject,
      unavailable: section.unavailable,
    }));

  const renderItem = (item: ShellNavItem) => {
    const disabled = !!item.requiresProject && !projectId;
    return (
      <RailButton
        key={item.key}
        active={item.matches(currentPath)}
        disabled={disabled}
        disabledLabel="Выберите проект"
        unavailable={item.unavailable}
        unavailableReason={UNAVAILABLE_REASON}
        label={item.label}
        icon={item.icon}
        onClick={() => {
          if (!disabled && !item.unavailable) void navigate(item.to(projectId));
        }}
      />
    );
  };

  return (
    <nav
      className="rail-nav"
      aria-label="Host navigation"
      // Поля рейла — половина разницы между его шириной (62) и шириной пункта
      // (44): пункт обязан стоять по центру полосы, иначе активная подсветка
      // читается как съехавшая.
      style={{ paddingInline: 9 }}
    >
      <button
        type="button"
        className="rail-brand"
        onClick={() => navigate("/dashboard")}
        aria-label="Kacho"
        // Оправа знака: квадрат 32 с радиусом 9. Цвета оправы — литералы, а не
        // роли темы: знак продукта один и тот же в тёмной и светлой, поэтому
        // токен здесь означал бы, что он меняется вместе с темой, а он не
        // меняется. Сама графика знака не трогается — меняется только оправа.
        style={{
          width: 32,
          height: 32,
          flexShrink: 0,
          margin: "11px auto 14px",
          color: "#ffffff",
          background: "#496fe0",
          border: "1px solid rgb(156 181 255 / 0.45)",
          borderRadius: 9,
        }}
      >
        <KachoLogo variant="mark" size={22} />
      </button>

      <div className="rail-items">
        {commonTop.map(renderItem)}
        {moduleItems.length > 0 && <div className="rail-section-divider" />}
        {moduleItems.map(renderItem)}
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
