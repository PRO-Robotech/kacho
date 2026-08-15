// Подписи агрегатной навигации против ЕДИНСТВЕННОГО источника имён
// (`shared/src/lib/entity-names`).
//
// Почему dashboard сверяется, а не импортирует: его образ собирается из ЕГО
// дерева — Dockerfile копирует только `dashboard/`, каталога `shared/` в
// контексте сборки нет. Импорт отсюда сломал бы сборку образа, поэтому карта
// остаётся своей, а расхождение держится этой пробой.
//
// Предмет: dashboard дублирует и заголовки разделов, и подписи ресурсов —
// то есть ровно те значения, которые расходились. До правки здесь стояли
// «Listeners» и «Target Groups» английским посреди русского интерфейса и
// «Compute Cloud» / «Network Load Balancer» / «Container Registry» —
// продуктовые имена чужих платформ.

import { DASHBOARD_NAVIGATION } from "./navigation";
import { SERVICE_MODULES } from "./lib/service-modules";
import { ENTITIES, SERVICES } from "../../shared/src/lib/entity-names";

/** Сегмент адреса ресурса — последний кусок пути пункта меню. */
function segmentOf(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

const navSections = DASHBOARD_NAVIGATION.filter((s) => s.key in SERVICES);
const navItems = DASHBOARD_NAVIGATION.flatMap((s) => s.items).filter((i) => segmentOf(i.path) in ENTITIES);
const moduleTiles = SERVICE_MODULES.filter((m) => m.key in SERVICES);

describe("агрегатная навигация — подписи выведены из единственного источника", () => {
  it(`осмотрено: разделов меню ${navSections.length}, пунктов ${navItems.length}, плиток ${moduleTiles.length} — перепись непуста`, () => {
    expect(navSections.length).toBeGreaterThanOrEqual(4);
    expect(navItems.length).toBeGreaterThanOrEqual(15);
    expect(moduleTiles.length).toBeGreaterThanOrEqual(4);
  });

  it("заголовок раздела меню совпадает с каноном", () => {
    const divergent = navSections
      .filter((s) => s.label !== SERVICES[s.key as keyof typeof SERVICES].menuTitle)
      .map((s) => `${s.key}: «${s.label}», канон «${SERVICES[s.key as keyof typeof SERVICES].menuTitle}»`);
    expect(divergent).toEqual([]);
  });

  it("подпись пункта меню совпадает с каноном", () => {
    const divergent = navItems
      .filter((i) => i.label !== ENTITIES[segmentOf(i.path) as keyof typeof ENTITIES].plural)
      .map((i) => `${i.key}: «${i.label}», канон «${ENTITIES[segmentOf(i.path) as keyof typeof ENTITIES].plural}»`);
    expect(divergent).toEqual([]);
  });

  it("заголовок плитки сервиса совпадает с каноном", () => {
    const divergent = moduleTiles
      .filter((m) => m.label !== SERVICES[m.key as keyof typeof SERVICES].menuTitle)
      .map((m) => `${m.key}: «${m.label}», канон «${SERVICES[m.key as keyof typeof SERVICES].menuTitle}»`);
    expect(divergent).toEqual([]);
  });
});
