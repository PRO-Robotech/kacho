import type { RemoteIconName } from "dashboard/navigation";

// Имя раздела берётся из зеркала канона имён (`host/src/lib/entity-names`), а
// не выписывается здесь: тот же раздел называет себя в меню модуля, в крошке и
// на экране отказа, и литерал совпадал бы с ними ровно до первой правки имени.
// Оболочка импортирует ЗЕРКАЛО, а не `@shared`: её образ собирается из её
// дерева, каталога `shared/` в контексте сборки нет (см. шапку зеркала).
import { SERVICES } from "../lib/entity-names";

/**
 * Каталог удалённых модулей консоли — ОДНО место, где живёт имя раздела.
 *
 * Имя нужно двум разным поверхностям, и они обязаны совпадать: экран отказа
 * («Раздел «X» недоступен») и кнопка раздела в рейле. Две копии имени разошлись
 * бы молча, и пользователь читал бы это как два разных места продукта.
 *
 * `section` несут те модули, чей раздел ДОЛЖЕН оставаться в меню, даже когда его
 * навигация не приехала: без такого запасного описания упавший модуль просто
 * исчезает из рейла, а это неотличимо от «такого сервиса нет».
 * `dashboard` (агрегатная навигация) и `system` (кнопка «Администрирование»
 * внизу рейла) собственного раздела в меню не имеют — у них только имя.
 */
export interface RemoteModuleSection {
  key: string;
  segment: string;
  icon: RemoteIconName;
  landingPath: string;
  requiresProject?: boolean;
}

export interface RemoteModule {
  /** Имя remote'а федерации — оно же ключ раздела навигации. */
  remote: string;
  /** Имя раздела так, как его видит пользователь. */
  label: string;
  section?: RemoteModuleSection;
}

export const REMOTE_MODULES: readonly RemoteModule[] = [
  { remote: "dashboard", label: "Все сервисы" },
  {
    remote: "vpc",
    label: SERVICES.vpc.menuTitle,
    section: { key: "vpc", segment: "vpc", icon: "network", landingPath: "vpc/networks", requiresProject: true },
  },
  {
    remote: "compute",
    label: SERVICES.compute.menuTitle,
    section: {
      key: "compute",
      segment: "compute",
      icon: "cloud",
      landingPath: "compute/instances",
      requiresProject: true,
    },
  },
  {
    remote: "storage",
    label: SERVICES.storage.menuTitle,
    section: {
      key: "storage",
      segment: "storage",
      icon: "hard-drive",
      landingPath: "storage/volumes",
      requiresProject: true,
    },
  },
  {
    remote: "nlb",
    label: SERVICES.nlb.menuTitle,
    section: {
      key: "nlb",
      segment: "nlb",
      icon: "scale",
      landingPath: "nlb/load-balancers",
      requiresProject: true,
    },
  },
  {
    remote: "registry",
    label: SERVICES.registry.menuTitle,
    section: {
      key: "registry",
      segment: "registry",
      icon: "layers",
      landingPath: "registry/registries",
      requiresProject: true,
    },
  },
  {
    remote: "iam",
    label: SERVICES.iam.menuTitle,
    section: { key: "iam", segment: "iam", icon: "key", landingPath: "/iam/accounts" },
  },
  { remote: "system", label: SERVICES.system.menuTitle },
] as const;

const BY_REMOTE = new Map(REMOTE_MODULES.map((module) => [module.remote, module]));

/**
 * Имя раздела для экрана отказа. Неизвестный remote — не «прочерк по умолчанию»,
 * а отказ: безымянный экран отказа не отвечает на вопрос «что именно сломалось»,
 * а гейт покрытия рядом не даёт такому remote появиться незамеченным.
 */
export function moduleLabelOf(remote: string): string {
  const module = BY_REMOTE.get(remote);
  if (!module) throw new Error(`moduleCatalog: у remote "${remote}" нет имени раздела`);
  return module.label;
}
