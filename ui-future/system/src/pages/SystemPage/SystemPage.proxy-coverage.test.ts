// Dev-прокси system обязан покрывать домены спек, которые монтирует его таблица
// маршрутов.
//
// Тот же класс, что уже сработал в iam: промах прокси в dev не печатает ничего —
// список просто пуст, форма просто без опций, — поэтому расхождение между
// «какие ресурсы модуль показывает» и «куда он умеет дозвониться» живёт молча.
// Здесь оно закрыто предикатом, а не памятью: перечень спек читается из САМОЙ
// таблицы маршрутов, а адреса — из общего реестра.
//
// Проверяются все три канала спеки: публичный apiPath, admin.basePath (внутренний
// CRUD) и internalGetPath (вкладка internal-проекции) — модуль ходит по каждому.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { REGISTRY } from "@shared/lib/resource-registry";

const here = path.dirname(fileURLToPath(import.meta.url));
const systemRoot = path.resolve(here, "../../..");

const pageSource = readFileSync(path.join(here, "SystemPage.tsx"), "utf8");
const viteSource = readFileSync(path.join(systemRoot, "vite.config.ts"), "utf8");

/** Спеки, на которые ссылается таблица маршрутов модуля. */
function routedSpecIds(): string[] {
  const ids = new Set<string>();
  for (const m of pageSource.matchAll(/REGISTRY\.([A-Za-z][A-Za-z0-9]*)/g)) ids.add(m[1]);
  for (const m of pageSource.matchAll(/REGISTRY\["([^"]+)"\]/g)) ids.add(m[1]);
  return [...ids].sort();
}

/** Все адреса, по которым модуль может пойти ради этих спек. */
function addressesOf(ids: string[]): string[] {
  const out = new Set<string>();
  for (const id of ids) {
    const spec = REGISTRY[id] as
      { apiPath?: string; admin?: { basePath?: string }; internalGetPath?: string } | undefined;
    if (!spec) continue;
    for (const p of [spec.apiPath, spec.admin?.basePath, spec.internalGetPath]) {
      if (p) out.add(p);
    }
  }
  return [...out].sort();
}

function proxyPrefixes(): string[] {
  const start = viteSource.indexOf("proxy: {");
  expect(start).toBeGreaterThan(-1);
  const end = viteSource.indexOf("\n    },", start);
  return [...viteSource.slice(start, end).matchAll(/^\s{6}"(\/[^"]+)":/gm)].map((m) => m[1]);
}

const covered = (address: string, prefixes: string[]) => prefixes.some((p) => address.startsWith(p));

describe("system dev-прокси против маршрутизируемых спек", () => {
  const ids = routedSpecIds();
  const addresses = addressesOf(ids);
  const prefixes = proxyPrefixes();

  it("объём осмотренного назван: сколько спек, адресов и прокси прочитано", () => {
    expect(ids.length).toBeGreaterThanOrEqual(3);
    expect(addresses.length).toBeGreaterThanOrEqual(5);
    expect(prefixes.length).toBeGreaterThanOrEqual(3);
    // Модуль обязан выходить за пределы одного домена — иначе утверждение ниже
    // было бы тождественно истинным.
    expect(new Set(addresses.map((a) => a.split("/")[1])).size).toBeGreaterThanOrEqual(2);
  });

  it("каждый адрес маршрутизируемой спеки резолвится через прокси", () => {
    const unreachable = addresses.filter((a) => !covered(a, prefixes));
    expect(unreachable).toEqual([]);
  });

  it("непокрытый префикс предикат считает недостижимым (контроль в обратную сторону)", () => {
    expect(covered("/geo/v1/regions", prefixes)).toBe(true);
    expect(covered("/nothing/v1/here", prefixes)).toBe(false);
  });
});
