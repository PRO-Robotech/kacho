import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { COMMON_BOTTOM } from "@shared/lib/service-modules";
import { buildSidebarGroups, flattenGroups } from "@shared/lib/sidebar-groups";
import { VPC_APP_MODULES } from "./ServiceSidebar";

// Сайдбар предлагает переход только туда, куда ЭТО приложение умеет попасть.
//
// Управление доступом из standalone-сборки vpc убрано (единственная поверхность
// IAM — ремоут iam, хост федерирует `/iam/*` туда). Значит и лаунчер в IAM
// отсюда предлагаться не должен: иначе кнопка есть, а за ней — молчаливый
// редирект «*» в корень, то есть ровно тот же класс, что и обращение к
// маршруту, которого нет.
//
// Утверждение идёт ЧЕРЕЗ ТУ ЖЕ ЦЕПОЧКУ, что строит меню: набор модулей
// приложения → buildSidebarGroups → адреса переходов.
const here = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(here, "ServiceSidebar.tsx"), "utf8");
const appSource = readFileSync(path.join(here, "../../../App.tsx"), "utf8");

function destinations(pathname: string): string[] {
  const groups = buildSidebarGroups(pathname, "prj-1", "acc-1", COMMON_BOTTOM, VPC_APP_MODULES);
  return flattenGroups(groups).map((leaf) => leaf.to("prj-1"));
}

describe("ServiceSidebar offers only destinations this app routes", () => {
  it("declares its public component exports", () => {
    expect(source).toContain("ServiceSidebar");
  });

  it("список модулей приложения непуст и не содержит IAM", () => {
    expect(VPC_APP_MODULES.length).toBeGreaterThan(0);
    expect(VPC_APP_MODULES.map((m) => m.key)).not.toContain("iam");
  });

  it("вне модуля лаунчеров в IAM нет", () => {
    expect(destinations("/dashboard").filter((d) => d.startsWith("/iam"))).toEqual([]);
  });

  it("и по «активному» IAM-адресу меню тоже не раскрывает раздел IAM", () => {
    expect(destinations("/iam/accounts").filter((d) => d.startsWith("/iam"))).toEqual([]);
  });

  it("«Профиль» ведёт в маршрут, который приложение объявляет", () => {
    expect(source).toContain('navigate("/auth/settings")');
    expect(appSource).toContain('path="/auth/settings"');
  });

  it("каждый проектный переход ведёт в раздел, объявленный маршрутами приложения", () => {
    // Раздел (первый сегмент после проекта) обязан встречаться в маршрутах —
    // либо своим экраном (`.../dashboard"`), либо поддеревом (`.../vpc/...`).
    const segments = destinations("/dashboard")
      .map((d) => /^\/projects\/[^/]+\/([^/]+)/.exec(d)?.[1])
      .filter((s): s is string => !!s);
    expect(segments.length).toBeGreaterThan(0);
    for (const seg of segments) {
      // Маршрут объявляется литералом (`path="…"`) либо шаблоном (`path={`…`}`),
      // когда раздел разворачивается по списку ресурсов реестра.
      const declared =
        appSource.includes(`path="/projects/:projectId/${seg}/`) ||
        appSource.includes(`path="/projects/:projectId/${seg}"`) ||
        appSource.includes("path={`/projects/:projectId/" + seg + "/");
      expect([seg, declared]).toEqual([seg, true]);
    }
  });
});
