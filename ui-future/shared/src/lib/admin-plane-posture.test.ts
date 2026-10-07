// Посадка admin-плоскости читается из ОТВЕТА края на пробный запрос, а не
// объявляется профилем (#2692). Предметов здесь два.
//
// 1. Классификация: 404 на пробе означает ровно одно — на этом слушателе такого
//    маршрута нет (внешний край отвечает на необслуживаемую поверхность как на
//    несуществующую, by construction); настоящий отказ в правах и всё остальное
//    так читаться не вправе, иначе «нет прав» превратилось бы в «раздела нет».
//
// 2. Путь пробы (#3091): 404 называет посадку, только пока у пути нет
//    ПУБЛИЧНОГО близнеца. Прежний путь (`/vpc/v1/addressPools`) его получил с
//    ADM-1 S1 — и на внешней посадке проба стала отвечать «плоскость есть».
//    Поэтому путь судится по таблице маршрутов края, а не по комментарию: на
//    пути пробы под GET стоят только внутренние службы.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ApiError } from "@shared/api/client";
import { ADMIN_PLANE_PROBE_PATH, classifyAdminPlaneProbe } from "./admin-plane-posture";

const deny403 = new ApiError(403, 7, [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "AUTHZ_DENIED" }], "permission denied");

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../..");
const ROUTE_TABLE = "gateway/internal/middleware/rest_route_table_gen.go";
const ROW = /\{Method: "([A-Z]+)", Template: "([^"]+)", FQN: "([^"]+)"\}/g;

interface Route {
  method: string;
  template: string;
  service: string;
}

function routeTable(): Route[] {
  const text = readFileSync(path.join(repoRoot, ROUTE_TABLE), "utf8");
  const rows = [...text.matchAll(ROW)].map((m) => ({
    method: m[1],
    template: m[2],
    service: m[3].slice(0, m[3].indexOf("/")).split(".").pop() ?? "",
  }));
  // Второе выражение того же предмета: строки таблицы по их началу. Разошлись —
  // разборщик не знает формы записи, и вывод ниже был бы о другом наборе.
  const lines = text.split("\n").filter((l) => l.trimStart().startsWith("{Method:")).length;
  expect(rows.length).toBe(lines);
  expect(rows.length).toBeGreaterThan(0);
  return rows;
}

/** Службы, обслуживающие GET по пути; пустой список — пути в таблице нет. */
function getServices(routes: Route[], probePath: string): string[] {
  return routes.filter((r) => r.method === "GET" && r.template === probePath).map((r) => r.service);
}

/** Пригоден ли путь пробой посадки: он есть, и обслуживают его только внутренние службы. */
function namesTheLanding(services: string[]): boolean {
  return services.length > 0 && services.every((s) => s.startsWith("Internal"));
}

describe("путь пробы посадки", () => {
  it("на пути пробы под GET — только внутренние службы: 404 там значит «плоскости нет», а не «ресурса нет»", () => {
    const services = getServices(routeTable(), ADMIN_PLANE_PROBE_PATH);
    // Перечень служб сверяется целиком: при провале он и есть объяснение.
    expect(services).toEqual(["InternalClusterService"]);
    expect(namesTheLanding(services)).toBe(true);
  });

  it("инъекция настоящим входом дерева: путь с публичным близнецом посадку не называет (прежний путь, ADM-1 S1)", () => {
    const services = getServices(routeTable(), "/vpc/v1/addressPools");
    expect(services).toEqual(expect.arrayContaining(["AddressPoolService", "InternalAddressPoolService"]));
    expect(namesTheLanding(services)).toBe(false);
  });

  it("путь, которого в таблице нет, посадку тоже не называет", () => {
    expect(namesTheLanding(getServices(routeTable(), "/iam/v1/internal/kachoProbeAbsent"))).toBe(false);
  });

  it("проба читает одиночку административной плоскости — запись, которая есть всегда", () => {
    // `InternalClusterService/Get`: кластер `cluster_root` существует на любой
    // посадке, где плоскость есть, — 404 на нём не может значить «записи нет».
    expect(ADMIN_PLANE_PROBE_PATH).toBe("/iam/v1/internal/cluster");
  });
});

describe("classifyAdminPlaneProbe", () => {
  it("успешный ответ — плоскость обслуживается здесь", () => {
    expect(classifyAdminPlaneProbe(null)).toBe("present");
  });

  it("404 на пробе — плоскости на этой посадке нет", () => {
    expect(classifyAdminPlaneProbe(new ApiError(404, 5, [], "Not Found"))).toBe("absent");
  });

  it("настоящий отказ в правах остаётся отказом в правах, а не отсутствием раздела", () => {
    expect(classifyAdminPlaneProbe(deny403)).toBe("denied");
  });

  it("сбой, о котором нечего сказать, не выдаётся ни за отсутствие, ни за наличие", () => {
    expect(classifyAdminPlaneProbe(new ApiError(503, 14, [], "unavailable"))).toBe("unknown");
    expect(classifyAdminPlaneProbe(new TypeError("Failed to fetch"))).toBe("unknown");
  });
});
