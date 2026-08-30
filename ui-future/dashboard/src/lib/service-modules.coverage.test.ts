// Витрина сервисов против того, какие разделы консоль реально монтирует.
//
// Плитка — единственный вход в раздел с главной. Домен, у которого есть свой
// remote и свой project-маршрут, но нет плитки, недостижим ничем, кроме прямого
// ввода адреса: он существует, работает и невидим. Это ровно та же ошибка, что
// плитка, обещающая снятый раздел, только зеркальная — и она не ловится
// проверкой «каждый listPath существует», потому что отсутствующая плитка не
// называет ни одного пути.
//
// ПРЕДМЕТ — СОСТАВ ДЕРЕВА, а не текст одного файла. Прежняя редакция читала
// ровно один известный ей исходник маршрутизатора: смонтируй раздел в другом
// месте — и проверка осталась бы зелёной, ничего не заметив. Поэтому объявления
// `/projects/:projectId/<segment>/*` ищутся ОБХОДОМ всего дерева консоли;
// приложение, заведённое завтра, попадает под гейт само, а объём осмотренного
// утверждается отдельно — «ноль находок» обязано быть отличимо от «ноль
// прочитанного».

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { SERVICE_MODULES } from "./service-modules";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

// Ровно ФОРМА МОНТИРОВАНИЯ remote'а: сегмент со звёздочкой. Собственный
// маршрут приложения (`/projects/:projectId/edit`) разделом консоли не
// является и плитки не требует — без звёздочки предикат ловил бы и его.
const MOUNT = /path="\/projects\/:projectId\/([a-z-]+)\/\*"/g;

function sources(dir: string, out: string[] = []): string[] {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules") continue;
    const p = path.join(dir, entry);
    if (statSync(p).isDirectory()) sources(p, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(p);
  }
  return out;
}

function consoleSources(): string[] {
  const out: string[] = [];
  for (const pkg of readdirSync(uiRoot)) {
    if (pkg === "node_modules") continue;
    const src = path.join(uiRoot, pkg, "src");
    if (existsSync(src) && statSync(src).isDirectory()) sources(src, out);
  }
  return out;
}

/**
 * Сегменты `/projects/:projectId/<segment>/*`, объявленные ГДЕ УГОДНО в дереве.
 *
 * `dashboard` исключён — это сама витрина, плитки на себя она не заводит.
 * Параметрический `:moduleKey` доменом не является (заглушка «раздел ещё не
 * подключён») и в этот предикат не попадает by construction: он ловит только
 * буквенные сегменты.
 */
export function mountedProjectSegments(source: string): string[] {
  return [...new Set([...source.matchAll(MOUNT)].map((m) => m[1]).filter((seg) => seg && seg !== "dashboard"))];
}

const files = consoleSources();
const mounted = [...new Set(files.flatMap((f) => mountedProjectSegments(readFileSync(f, "utf8"))))].sort();

describe("витрина против смонтированных разделов консоли", () => {
  it("объём осмотренного назван: сколько исходников прочитано и сколько разделов найдено", () => {
    expect(files.length).toBeGreaterThan(100);
    expect(mounted.length).toBeGreaterThan(2);
    expect(SERVICE_MODULES.length).toBeGreaterThan(2);
  });

  it("у каждого смонтированного раздела есть плитка", () => {
    const tiles = new Set(SERVICE_MODULES.map((m) => m.segment));
    expect(mounted.filter((seg) => !tiles.has(seg))).toEqual([]);
  });

  it("плитка, требующая проект, ведёт в смонтированный раздел", () => {
    // Контроль в обратную сторону: плитка на несмонтированный сегмент отправляет
    // человека на заглушку «раздел не подключён».
    const set = new Set(mounted);
    expect(SERVICE_MODULES.filter((m) => m.requiresProject).filter((m) => !set.has(m.segment))).toEqual([]);
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
    // И обратная сторона того же различия: собственный маршрут приложения,
    // а не монтирование раздела, предикатом не ловится.
    expect(mountedProjectSegments('<Route path="/projects/:projectId/edit" element={<E />} />')).toEqual([]);
  });

  it("законный близнец: витрина сама себе плитку не требует", () => {
    expect(mountedProjectSegments('<Route path="/projects/:projectId/dashboard" element={<D />} />')).toEqual([]);
  });
});
