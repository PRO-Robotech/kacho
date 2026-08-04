// Каждый API-путь, который shared произносит в прод-коде, обязан принадлежать
// поверхности ствола.
//
// Путь, которого на поверхности нет, не даёт ошибки сборки и не виден на ревью:
// он существует как строка, доезжает до края и возвращает 404 — а страница,
// которая его зовёт, выглядит просто пустой. Именно так `/compute/v1/regions`
// прожил в поиске по системе рядом с реестром, который в том же пакете уже знал
// правильный адрес: два места об одном предмете, из которых верно одно.
//
// ИСТОЧНИК ИСТИНЫ — дерево, а не список в этом файле:
//   (1) http-аннотации `proto/**/*.proto` — 172 уникальных пути на ревизии,
//       где эта проба заведена;
//   (2) маршруты, которые край регистрирует САМ, мимо proto (`/iam/v1/auth/me`
//       регистрируется в gateway/internal/middleware напрямую). Их тоже читаем
//       из дерева, а не выписываем: выписанный список разошёлся бы с ним молча.
//
// Проба несёт проверку СВОЕЙ предпосылки: если proto/gateway/shared не читаются
// или читаются пусто, она падает, а не объявляет «ноль находок». И несёт
// контроль в обе стороны — законный путь обязан признаваться, выдуманный обязан
// отвергаться, иначе «все пути прошли» означало бы лишь, что сопоставитель
// согласен на всё.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

// cwd прогона — каталог приложения (ui-future/<app>), одинаково у всех девяти:
// их package.json запускает jest из своего каталога. Отсюда и относительные
// координаты дерева.
const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const PROTO_DIR = join(REPO_ROOT, "proto");
const GATEWAY_DIR = join(REPO_ROOT, "gateway");
const SHARED_SRC = resolve(APP_DIR, "../shared/src");

/** Домены, чьи пути край обслуживает; всё остальное — SPA-роутер, не API. */
const API_PREFIX = /^\/(vpc|compute|iam|geo|nlb|storage|registry|loadbalancer)\/v1(\/|$)|^\/operations(\/|$)/;

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

/**
 * Убирает комментарии, СОХРАНЯЯ строковые литералы.
 *
 * Гейт обязан читать исполняемую часть, а не текст: путь, названный в
 * объяснительном комментарии («такого маршрута больше нет»), — это разбор, а не
 * вызов, и падать на нём значило бы запретить объяснять.
 */
function stripComments(src: string): string {
  let out = "";
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      const quote = c;
      out += c;
      i++;
      while (i < src.length) {
        if (src[i] === "\\") {
          out += src[i] + (src[i + 1] ?? "");
          i += 2;
          continue;
        }
        out += src[i];
        if (src[i] === quote) {
          i++;
          break;
        }
        i++;
      }
      continue;
    }
    if (c === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      i += 2;
      while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) i++;
      i += 2;
      continue;
    }
    out += c;
    i++;
  }
  return out;
}

/**
 * Нормализует путь к сегментам сопоставления: снимает query, а всякую
 * подстановку — proto `{network_id}`, шаблон реестра `{id}`, интерполяцию
 * `${...}` — сводит к `*`. Верб остаётся при сегменте: `{network_id}:internal`
 * → `*:internal`, поэтому глагольная форма не сходится со слэшевой.
 */
function segments(path: string): string[] {
  const noQuery = path.split("?")[0];
  return noQuery
    .split("/")
    .map((s) => s.replace(/\$\{[^}]*\}/g, "*").replace(/\{[^}]*\}/g, "*"))
    .filter((s, idx) => !(idx === 0 && s === ""));
}

/** proto-параметр `{repository=**}` съедает один сегмент или больше. */
const GREEDY = "**";

function protoSegments(path: string): string[] {
  return path
    .split("/")
    .map((s) => s.replace(/\{[^}]*=\*\*\}/g, GREEDY).replace(/\{[^}]*\}/g, "*"))
    .filter((s, idx) => !(idx === 0 && s === ""));
}

/**
 * Сегмент — это пара «база + верб»: `{network_id}:internal` → `*` + `internal`.
 * Верб сравнивается строго и отдельно, иначе параметр ствола проглотил бы любой
 * суффикс, и `…/{id}:no-such-verb` прошло бы как обычный детальный GET.
 */
function splitSeg(seg: string): { base: string; verb: string } {
  const i = seg.indexOf(":");
  return i < 0 ? { base: seg, verb: "" } : { base: seg.slice(0, i), verb: seg.slice(i + 1) };
}

function matches(actual: string[], surface: string[]): boolean {
  const walkFrom = (i: number, j: number): boolean => {
    if (j === surface.length) return i === actual.length;
    const want = splitSeg(surface[j]);
    if (want.base === GREEDY) {
      for (let k = i + 1; k <= actual.length; k++) {
        if (splitSeg(actual[k - 1]).verb !== want.verb) continue;
        if (walkFrom(k, j + 1)) return true;
      }
      return false;
    }
    if (i === actual.length) return false;
    const got = splitSeg(actual[i]);
    if (got.verb !== want.verb) return false;
    // База-параметр ствола принимает любую базу вызывающего; конкретная база —
    // только себя (после сведения подстановок к `*`).
    if (want.base !== "*" && want.base !== got.base) return false;
    return walkFrom(i + 1, j + 1);
  };
  return walkFrom(0, 0);
}

// ── Поверхность ствола ───────────────────────────────────────────────────────

const protoFiles = walk(PROTO_DIR, /\.proto$/);
const protoPaths = [
  ...new Set(
    protoFiles.flatMap((f) =>
      [...readFileSync(f, "utf8").matchAll(/(?:get|post|patch|put|delete):\s*"([^"]+)"/g)].map((m) => m[1]),
    ),
  ),
];

// Край регистрирует часть маршрутов сам, вне proto. Читаем их из его исходников
// тем же способом, иначе живой `/iam/v1/auth/me` попал бы в находки ложно.
const gatewayFiles = walk(GATEWAY_DIR, /\.go$/).filter((f) => !f.endsWith("_test.go"));
const gatewayNative = [
  ...new Set(
    gatewayFiles.flatMap((f) =>
      [...readFileSync(f, "utf8").matchAll(/\.(?:HandleFunc|Handle)\(\s*"([^"]+)"/g)].map((m) => m[1]),
    ),
  ),
].filter((p) => API_PREFIX.test(p));

const SURFACE = [...protoPaths, ...gatewayNative].map(protoSegments);

function belongs(path: string): boolean {
  const segs = segments(path);
  return SURFACE.some((s) => matches(segs, s));
}

// ── Что произносит shared ────────────────────────────────────────────────────

const sharedFiles = walk(SHARED_SRC, /\.(ts|tsx)$/).filter(
  (f) => !/\.test\.(ts|tsx)$/.test(f) && !f.includes(`${join("src", "test")}`),
);

const spoken = new Map<string, string[]>();
for (const file of sharedFiles) {
  const code = stripComments(readFileSync(file, "utf8"));
  for (const m of code.matchAll(/["'`]([^"'`\n]*)["'`]/g)) {
    const lit = m[1];
    if (!API_PREFIX.test(lit)) continue;
    if (!spoken.has(lit)) spoken.set(lit, []);
    const seen = spoken.get(lit)!;
    const rel = file.slice(SHARED_SRC.length + 1);
    if (!seen.includes(rel)) seen.push(rel);
  }
}

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("прочитал дерево proto и получил из него поверхность", () => {
    expect(protoFiles.length).toBeGreaterThan(100);
    expect(protoPaths.length).toBeGreaterThan(150);
  });

  it("прочитал исходники края и нашёл маршруты, которые он регистрирует сам", () => {
    expect(gatewayFiles.length).toBeGreaterThan(10);
    // Ровно один такой маршрут лежит под доменным префиксом (`/iam/v1/auth/me`);
    // `/healthz`, `/readyz`, `/oauth/logout` под предикат API не попадают вовсе.
    expect(gatewayNative.length).toBeGreaterThanOrEqual(1);
  });

  it("прочитал прод-исходники shared и нашёл в них API-пути", () => {
    expect(sharedFiles.length).toBeGreaterThan(150);
    expect(spoken.size).toBeGreaterThan(40);
  });
});

describe("сопоставитель различает — контроль в обе стороны", () => {
  it("признаёт законные формы: список, детальный, вложенный, глагольный", () => {
    expect(belongs("/vpc/v1/subnets")).toBe(true);
    expect(belongs("/vpc/v1/addresses/${id}")).toBe(true);
    expect(belongs("/vpc/v1/addressPools/${poolId}/utilization?pageSize=200")).toBe(true);
    expect(belongs("/vpc/v1/networks/${id}:add-cidr-blocks")).toBe(true);
    expect(belongs("/vpc/v1/networks/{network_id}:internal")).toBe(true);
    expect(belongs("/iam/v1/auth/me")).toBe(true);
  });

  it("отвергает выдуманное, а не соглашается на всё", () => {
    expect(belongs("/vpc/v1/nope")).toBe(false);
    expect(belongs("/vpc/v1/networks/${id}:no-such-verb")).toBe(false);
    // Слэшевая форма там, где ствол несёт глагольную, — разные пути.
    expect(belongs("/vpc/v1/networks/${id}/internal")).toBe(false);
    // Регистр сегмента значим: край не нормализует kebab-case в camelCase.
    expect(belongs("/vpc/v1/route-tables")).toBe(false);
  });
});

describe("shared не адресует ничего вне поверхности ствола", () => {
  const cases = [...spoken.entries()].sort(([a], [b]) => a.localeCompare(b));

  it.each(cases)("%s", (path, files) => {
    expect(belongs(path) ? "" : `${path} ← ${files.join(", ")}`).toBe("");
  });
});
