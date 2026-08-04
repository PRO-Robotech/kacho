// Dev-прокси iam обязан покрывать домены, в которые iam-консоль реально ходит.
//
// Редактор правил роли (RulesEditor) рендерит picker реальных инстансов и для
// НЕ-iam типов: перечень `(модуль, ресурс) → REGISTRY-spec` живёт в
// shared/src/lib/resourceInstanceFetchers.ts и включает vpc-, compute- и
// nlb-ресурсы. Их List идёт по apiPath спеки, то есть по чужому префиксу.
//
// Почему это тест, а не «мелочь конфигурации». Промах прокси в dev выглядит НЕ
// как ошибка: запрос ловится `catch {}` в RulesEditor и деградирует до свободного
// ввода id — picker просто пуст. Отказ, который ничего не печатает и ни на что не
// влияет визуально, живёт годами; а автор правила молча теряет подсказку и
// вводит идентификаторы руками.
//
// Ground truth здесь двусторонний: перечень токенов и apiPath берутся из общего
// реестра (не выписываются), список прокси — из самого vite.config.ts.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { instanceFetcherFor } from "@shared/lib/resourceInstanceFetchers";

const here = path.dirname(fileURLToPath(import.meta.url));
const iamRoot = path.resolve(here, "..");
const uiRoot = path.resolve(iamRoot, "..");

const fetchersSource = readFileSync(path.join(uiRoot, "shared/src/lib/resourceInstanceFetchers.ts"), "utf8");
const viteSource = readFileSync(path.join(iamRoot, "vite.config.ts"), "utf8");

/** Токены каталога, для которых picker вообще может отрисоваться. */
function tokens(): { module: string; resource: string }[] {
  const start = fetchersSource.indexOf("const TOKEN_TO_REGISTRY_ID");
  const end = fetchersSource.indexOf("\n};", start);
  expect(start).toBeGreaterThan(-1);
  expect(end).toBeGreaterThan(start);
  return [...fetchersSource.slice(start, end).matchAll(/^\s*"([a-zA-Z]+)\.([a-zA-Z]+)":/gm)].map((m) => ({
    module: m[1],
    resource: m[2],
  }));
}

/** Ключи `server.proxy` из vite.config.ts (только API-префиксы). */
function proxyPrefixes(): string[] {
  const start = viteSource.indexOf("proxy: {");
  const end = viteSource.indexOf("\n    },", start);
  expect(start).toBeGreaterThan(-1);
  return [...viteSource.slice(start, end).matchAll(/^\s{6}"(\/[^"]+)":/gm)].map((m) => m[1]);
}

const covered = (apiPath: string, prefixes: string[]) => prefixes.some((p) => apiPath.startsWith(p));

describe("iam dev-прокси против путей, по которым ходит консоль", () => {
  const list = tokens();
  const prefixes = proxyPrefixes();
  const apiPaths = [...new Set(list.map((t) => instanceFetcherFor(t.module, t.resource)?.spec.apiPath ?? ""))].filter(
    Boolean,
  );

  it("объём осмотренного назван: сколько токенов, путей и прокси прочитано", () => {
    expect(list.length).toBeGreaterThanOrEqual(15);
    expect(apiPaths.length).toBeGreaterThanOrEqual(15);
    expect(prefixes.length).toBeGreaterThanOrEqual(2);
    // Перечень обязан выходить за пределы своего домена — иначе утверждение ниже
    // проверяло бы только /iam и было бы тождественно истинным.
    expect(apiPaths.some((p) => !p.startsWith("/iam/"))).toBe(true);
  });

  it("каждый путь picker'а правил резолвится через прокси", () => {
    const unreachable = apiPaths.filter((p) => !covered(p, prefixes)).sort();
    expect(unreachable).toEqual([]);
  });

  it("непокрытый префикс предикат считает недостижимым (контроль в обратную сторону)", () => {
    expect(covered("/iam/v1/roles", prefixes)).toBe(true);
    expect(covered("/nothing/v1/here", prefixes)).toBe(false);
  });
});
