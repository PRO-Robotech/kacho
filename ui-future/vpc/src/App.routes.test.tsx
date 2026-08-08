// Сайдбар предлагает переход только туда, куда ЭТО приложение умеет попасть.
//
// Проверка идёт через ТУ ЖЕ ЦЕПОЧКУ, что строит меню (набор модулей приложения →
// buildSidebarGroups → адреса переходов), и сверяет адреса с НАСТОЯЩЕЙ таблицей
// маршрутов — сопоставителем самого роутера, а не поиском строки в исходнике
// App.tsx. Разница не косметическая: текстовый поиск пропускает маршрут,
// объявленный с другим именем параметра, и краснеет на упоминании в комментарии.
//
// Шапка `AppRoutes` ссылалась на такой файл (KAC-199) — в дереве его не было.
//
// Собственный стаб antd здесь НАМЕРЕННЫЙ: общий стаб набора проб не несёт
// `ConfigProvider`, а без него ESM-линкер не собирает граф App. Компоненты тут
// не рендерятся вовсе — `AppRoutes()` только СТРОИТ дерево элементов, поэтому
// стабу достаточно отдать имена, которые граф импортирует.
//
// Перечень имён устареть МОЛЧА не может: недостающее имя ESM-линкер объявляет
// ошибкой «does not provide an export named …» и суита краснеет целиком. Свежий
// перечень выводится, а не вспоминается:
//   git grep -zPo 'import\s*\{[^}]*\}\s*from\s*"antd"' -- ui-future/vpc/src ui-future/shared/src

import { jest } from "@jest/globals";
import React from "react";
import { createRoutesFromElements, matchRoutes } from "react-router";
import { COMMON_BOTTOM } from "@shared/lib/service-modules";
import { buildSidebarGroups, flattenGroups } from "@shared/lib/sidebar-groups";
import type { ServiceModule } from "@shared/lib/service-modules";

jest.unstable_mockModule("antd/locale/ru_RU", () => ({ __esModule: true, default: {} }));

jest.unstable_mockModule("antd", () => {
  const C = ({ children, ...p }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("div", p, children);
  const withParts = (extra: Record<string, unknown>) => Object.assign(C, extra);
  return {
    __esModule: true,
    Alert: C,
    App: C,
    AutoComplete: C,
    Avatar: C,
    Badge: C,
    Button: C,
    Card: C,
    Cascader: C,
    Checkbox: C,
    Col: C,
    Collapse: C,
    ConfigProvider: C,
    Descriptions: withParts({ Item: C }),
    Divider: C,
    Dropdown: C,
    Empty: Object.assign(C, { PRESENTED_IMAGE_SIMPLE: null }),
    Form: withParts({ Item: C, List: C, useForm: () => [{}], useWatch: () => undefined }),
    Image: C,
    Input: withParts({ TextArea: C, Search: C }),
    InputNumber: C,
    Layout: withParts({ Content: C, Header: C, Sider: C }),
    List: C,
    Menu: C,
    Modal: withParts({ confirm: () => undefined, destroyAll: () => undefined }),
    Popconfirm: C,
    Result: C,
    Row: C,
    Segmented: C,
    Select: C,
    Space: C,
    Spin: C,
    Statistic: C,
    Switch: C,
    Table: C,
    Tabs: C,
    Tag: C,
    Tooltip: C,
    Tree: C,
    Typography: withParts({ Text: C, Title: C, Paragraph: C }),
    theme: { useToken: () => ({ token: {} }), defaultAlgorithm: {}, darkAlgorithm: {} },
  };
});

let appModules: ServiceModule[];

/** Адреса, которые сайдбар реально предлагает при данном положении. */
function destinations(pathname: string): string[] {
  const groups = buildSidebarGroups(pathname, "prj-1", "acc-1", COMMON_BOTTOM, appModules);
  return flattenGroups(groups).map((leaf) => leaf.to("prj-1"));
}

describe("маршруты приложения против того, что предлагает навигатор", () => {
  let routes: ReturnType<typeof createRoutesFromElements>;

  beforeAll(async () => {
    // Оба импорта — ДИНАМИЧЕСКИЕ и только здесь: статический притянул бы `antd`
    // раньше, чем зарегистрирован стаб выше, и подмена опоздала бы к линковке.
    ({ VPC_APP_MODULES: appModules } = await import("@/components/organisms/ServiceSidebar"));
    const { AppRoutes } = await import("@/App");
    const tree = (AppRoutes as unknown as () => React.ReactElement<{ children: React.ReactNode }>)();
    routes = createRoutesFromElements(tree.props.children);
  });

  /** Совпал ли адрес с ОБЪЯВЛЕННЫМ маршрутом, а не с ловушкой «всё остальное». */
  const routed = (to: string): boolean => {
    const m = matchRoutes(routes, to);
    if (!m || m.length === 0) return false;
    return m[m.length - 1].route.path !== "*";
  };

  it("объём осмотренного назван: сколько маршрутов и сколько предложений навигатора", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустая таблица
    // маршрутов или пустой навигатор сделали бы утверждения ниже вакуумными.
    // Считается всё ДЕРЕВО, а не верхний ярус: большинство маршрутов вложено.
    const count = (rs: typeof routes): number =>
      rs.reduce((n, r) => n + 1 + count((r.children ?? []) as typeof routes), 0);

    expect(routes.length).toBeGreaterThanOrEqual(5);
    expect(count(routes)).toBeGreaterThanOrEqual(30);
    expect(destinations("/dashboard").length).toBeGreaterThanOrEqual(5);
  });

  it("предикат отличает объявленный маршрут от ловушки «всё остальное» (контроль в обе стороны)", () => {
    expect(routed("/auth/login")).toBe(true);
    expect(routed("/no/such/section/at/all")).toBe(false);
  });

  it("каждый переход навигатора вне модуля ведёт в объявленный маршрут", () => {
    const dead = destinations("/dashboard").filter((d) => !routed(d));
    expect(dead).toEqual([]);
  });

  it("каждый переход навигатора внутри модуля VPC ведёт в объявленный маршрут", () => {
    const dead = destinations("/projects/prj-1/vpc/networks").filter((d) => !routed(d));
    expect(dead).toEqual([]);
  });

  it("«Профиль» пользовательского меню ведёт в объявленный маршрут", () => {
    // Меню сайдбара и шапки уводит сюда; прежде оно вело в /iam/users, которого
    // в standalone-сборке vpc нет вовсе.
    expect(routed("/auth/settings")).toBe(true);
  });

  it("раздела управления доступом приложение не маршрутизирует — и навигатор его не предлагает", () => {
    expect(routed("/iam/accounts")).toBe(false);
    expect(destinations("/dashboard").filter((d) => d.startsWith("/iam"))).toEqual([]);
    expect(destinations("/iam/accounts").filter((d) => d.startsWith("/iam"))).toEqual([]);
  });
});
