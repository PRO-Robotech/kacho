// KAC-246: построение сгруппированной структуры расширенного сайдбара.
//
// Группы выводятся из service-modules: «Обзор» (COMMON_TOP) → активный
// модуль ИЛИ лаунчеры модулей → «Система» (COMMON_BOTTOM). Каждая группа —
// заголовок-caps + список NavLeaf. Сохраняет module-active поведение
// icon-rail: внутри модуля показываем его items, иначе — лаунчеры.

import type { ReactNode } from "react";
import {
  COMMON_BOTTOM,
  COMMON_TOP,
  SERVICE_MODULES,
  moduleFromPathname,
  type NavLeaf,
  type ServiceModule,
} from "@shared/lib/service-modules";

export interface SidebarGroup {
  key: string;
  /** Заголовок группы (caps, tertiary). Пустая строка — без заголовка. */
  title: string;
  leaves: NavLeaf[];
}

/** Лаунчер модуля (псевдо-NavLeaf на landing) — для дашборд-контекста. */
function moduleLauncher(m: ServiceModule, projectId: string | null, accountId: string | null): NavLeaf {
  return {
    key: `mod-${m.key}`,
    icon: m.icon,
    label: m.label,
    to: () => m.landing(projectId, accountId) ?? "/dashboard",
    matches: () => false,
    requiresProject: m.requiresProject,
  };
}

/**
 * Построить группы сайдбара под текущий pathname/context.
 *
 * @param pathname  активный URL — определяет активный модуль.
 * @param projectId выбранный проект (для requiresProject-гейтинга / `to`).
 * @param accountId выбранный аккаунт.
 * @param bottomItems отфильтрованный COMMON_BOTTOM (system виден только авторизованным).
 */
export function buildSidebarGroups(
  pathname: string,
  projectId: string | null,
  accountId: string | null,
  bottomItems: NavLeaf[] = COMMON_BOTTOM,
): SidebarGroup[] {
  const activeModule = moduleFromPathname(pathname);
  const groups: SidebarGroup[] = [];

  // 1. Обзор — общий верхний блок (дашборд / поиск).
  groups.push({ key: "overview", title: "Обзор", leaves: COMMON_TOP });

  if (activeModule) {
    // 2a. Внутри модуля — его собственный раздел с заголовком = short-имя.
    groups.push({
      key: activeModule.key,
      title: activeModule.short,
      leaves: activeModule.items,
    });
  } else {
    // 2b. Вне модуля (дашборд / system) — лаунчеры всех модулей одной группой.
    groups.push({
      key: "services",
      title: "Сервисы",
      leaves: SERVICE_MODULES.map((m) => moduleLauncher(m, projectId, accountId)),
    });
  }

  // 3. Система — нижний общий блок (администрирование).
  if (bottomItems.length > 0) {
    groups.push({ key: "system", title: "Система", leaves: bottomItems });
  }

  return groups;
}

/** Плоский список всех leaf'ов групп — для резолва active-ключа. */
export function flattenGroups(groups: SidebarGroup[]): NavLeaf[] {
  return groups.flatMap((g) => g.leaves);
}

/** Резолв активного leaf-ключа по pathname среди всех leaf'ов групп. */
export function activeLeafKey(groups: SidebarGroup[], pathname: string): string | null {
  return flattenGroups(groups).find((l) => l.matches(pathname))?.key ?? null;
}

export type { ReactNode };
