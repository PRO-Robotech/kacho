// Витрина сервисов против того, какие разделы консоль реально монтирует.
//
// Плитка — единственный вход в раздел с главной. Домен, у которого есть свой
// remote и свой project-маршрут, но нет плитки, недостижим ничем, кроме прямого
// ввода адреса: он существует, работает и невидим. Это ровно та же ошибка, что
// плитка, обещающая снятый раздел, только зеркальная — и она не ловится
// проверкой «каждый listPath существует», потому что отсутствующая плитка не
// называет ни одного пути.
//
// Ground truth — маршрутизатор консоли `ui-future/host/src/App.tsx`: строки
// `/projects/:projectId/<segment>/*`. Перечень ВЫВОДИТСЯ из него, а не
// выписывается здесь: смонтируют новый remote — гейт потребует плитку сам.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { SERVICE_MODULES } from "./service-modules";

const HOST_ROUTES = join(process.cwd(), "..", "host", "src", "App.tsx");

/**
 * Сегменты `/projects/:projectId/<segment>/*` из маршрутизатора консоли.
 *
 * `dashboard` исключён — это сама витрина, плитки на себя она не заводит.
 * Параметрический `:moduleKey` — заглушка «раздел ещё не подключён», доменом
 * не является.
 */
export function mountedProjectSegments(routerSource: string): string[] {
  const found = routerSource.match(/path="\/projects\/:projectId\/([a-z-]+)\/\*"/g) ?? [];
  return [
    ...new Set(
      found
        .map((m) => /\/projects\/:projectId\/([a-z-]+)\//.exec(m)?.[1] ?? "")
        .filter((seg) => seg && seg !== "dashboard"),
    ),
  ];
}

const routerSource = readFileSync(HOST_ROUTES, "utf8");

describe("витрина против смонтированных разделов консоли", () => {
  it("предпосылка: маршрутизатор консоли прочитан и несёт project-разделы", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного».
    expect(routerSource.length).toBeGreaterThan(0);
    expect(mountedProjectSegments(routerSource).length).toBeGreaterThan(2);
  });

  it("у каждого смонтированного раздела есть плитка", () => {
    const tiles = new Set(SERVICE_MODULES.map((m) => m.segment));
    const invisible = mountedProjectSegments(routerSource).filter((seg) => !tiles.has(seg));
    expect(invisible).toEqual([]);
  });

  it("плитка, требующая проект, ведёт в смонтированный раздел", () => {
    // Контроль в обратную сторону: плитка на несмонтированный сегмент отправляет
    // человека на заглушку «раздел не подключён».
    const mounted = new Set(mountedProjectSegments(routerSource));
    const dangling = SERVICE_MODULES.filter((m) => m.requiresProject).filter((m) => !mounted.has(m.segment));
    expect(dangling).toEqual([]);
  });

  it("плитка, требующая проект, отдаёт адрес при наличии проекта и молчит без него", () => {
    for (const m of SERVICE_MODULES.filter((x) => x.requiresProject)) {
      expect(m.landing("prj-1", null)).toContain(`/projects/prj-1/${m.segment}/`);
      expect(m.landing(null, null)).toBeNull();
    }
  });

  it("инъекция: раздел без плитки предикатом виден", () => {
    const synthetic = '<Route path="/projects/:projectId/quantum/*" element={<QuantumRemote />} />';
    expect(mountedProjectSegments(synthetic)).toEqual(["quantum"]);
    expect(SERVICE_MODULES.map((m) => m.segment)).not.toContain("quantum");
  });

  it("законный близнец: витрина сама себе плитку не требует", () => {
    expect(mountedProjectSegments('<Route path="/projects/:projectId/dashboard" element={<D />} />')).toEqual([]);
  });
});
