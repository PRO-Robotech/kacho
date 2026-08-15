// Раздел, которого нет в таблице маршрутов, не отличим от раздела, которого не
// задумывали: сайдбар предлагает переход, роутер отвечает ловушкой «всё
// остальное», и оператор попадает обратно на список машин без единого признака,
// что раздел вообще существует.
//
// Проверка идёт через НАСТОЯЩИЙ сопоставитель роутера, а не поиском строки в
// исходнике: текстовый поиск краснеет на упоминании в комментарии и молчит на
// маршруте, объявленном другим именем параметра.

import { jest } from "@jest/globals";
import { createRoutesFromElements, matchRoutes } from "react-router";
import type React from "react";

jest.unstable_mockModule("@/index.css", () => ({}));

const { ComputeRoutes } = await import("./InstancesPage");

const tree = (ComputeRoutes as unknown as () => React.ReactElement<{ children: React.ReactNode }>)();
const routes = createRoutesFromElements(tree.props.children);

/** Совпал ли адрес с ОБЪЯВЛЕННЫМ маршрутом, а не с ловушкой «всё остальное». */
function routed(to: string): boolean {
  const m = matchRoutes(routes, to);
  if (!m || m.length === 0) return false;
  return m[m.length - 1].route.path !== "*";
}

describe("маршруты раздела compute", () => {
  it("объём осмотренного назван — таблица маршрутов непуста", () => {
    expect(routes.length).toBeGreaterThanOrEqual(5);
  });

  it("предикат отличает объявленный маршрут от ловушки (контроль в обе стороны)", () => {
    expect(routed("/instances")).toBe(true);
    expect(routed("/no/such/section/at/all")).toBe(false);
  });

  it("группа размещения открывается: список, создание, карточка, правка, вкладки", () => {
    expect(routed("/placement-groups")).toBe(true);
    expect(routed("/placement-groups/create")).toBe(true);
    expect(routed("/placement-groups/plg-1")).toBe(true);
    expect(routed("/placement-groups/plg-1/edit")).toBe(true);
    expect(routed("/placement-groups/plg-1/operations")).toBe(true);
  });
});
