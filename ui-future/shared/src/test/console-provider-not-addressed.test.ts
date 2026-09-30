// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import { codeSources, formatFinding, providerCensusOf, type Excuse } from "./provider-address-census";

/**
 * F8-37 — прод-файлов консоли, обращающихся к чужому поставщику личности, НОЛЬ.
 *
 * Церемонии ведёт консоль своими экранами и глаголами нашей службы (приёмка F8,
 * DoD 14): ни переходом на адрес потока поставщика, ни запросом к его потоку,
 * ни чтением его сессии — ни прямо, ни через построитель его адреса или его
 * клиент. Браузерная перепись этого не заменяет: она видит только пути,
 * пройденные сценарием; здесь судится всё дерево.
 *
 * Отнесённое по референту — прокси сервера разработки к внешнему экрану входа —
 * печатается поимённо, находкой не считается и снимается одним изменением с
 * полосой раздачи к тому же экрану (`deploy/templates/configmap-nginx.yaml`) и
 * её потребителями вне консоли (остаток #2874). Исключение, которому нечего
 * исключать, краснит перепись: оно пережило бы свой предмет. Так сняты
 * исключение страницы доступности служб (#2733) и исключение ручки базы
 * поставщика в `config.ts` — ручка снята вместе с ним (#2874).
 *
 * Исключение относит ТОЧНЫЙ перечень (`covers`), а не всё, что в файле
 * появится: обёртка построителя в исключённом файле и её вызов из прод-файла
 * прежде давали «обращений 0» при отнесённом 8 → 10 (F3). Сверх перечня — красное.
 *
 * Способность упасть — `console-provider-not-addressed.injection.test.ts`.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Прод-файлы консоли: без проб, без сквозного набора, без каталогов проб. */
function isProductFile(rel: string): boolean {
  return /\.(ts|tsx)$/.test(rel) && !/\.test\./.test(rel) && !rel.startsWith("e2e/") && !/(^|\/)test\//.test(rel);
}

export const CONSOLE_EXCUSES: readonly Excuse[] = [
  {
    file: /(^|\/)vite\.config\.ts$/,
    reason:
      "прокси сервера разработки к внешнему экрану входа: в браузер не попадает; снимается с полосой раздачи (#2874)",
    covers: ["dashboard/vite.config.ts ручка базы поставщика", "host/vite.config.ts ручка базы поставщика"],
  },
];

const sources = codeSources(uiRoot, isProductFile);
const census = providerCensusOf(sources, CONSOLE_EXCUSES);

process.stdout.write(
  `\n[F8-37] прод-файлов консоли прочитано ${census.filesRead} · обращений к поставщику ${census.findings.length}` +
    ` · отнесено по референту ${census.excused.length} в ${new Set(census.excused.map((e) => e.file)).size} файлах` +
    ` · исключений без предмета ${census.staleExcuses.length}\n` +
    census.excused.map((e) => `    по референту: ${formatFinding(e)} — ${e.reason}\n`).join(""),
);

describe("F8-37 · прод-файлов консоли, обращающихся к чужому поставщику, ноль", () => {
  it("F8-37 · перепись прочла прод-дерево консоли — знаменатель обхода", () => {
    // Нижняя граница, а не точное число: ноль означал бы обход не того корня.
    expect(census.filesRead).toBeGreaterThan(300);
  });

  it("F8-37 · ни переходом, ни запросом к потоку, ни чтением сессии поставщика", () => {
    expect(census.findings.map(formatFinding)).toEqual([]);
  });

  it("F8-37 · каждое исключение по референту чему-то служит", () => {
    expect(census.staleExcuses).toEqual([]);
  });

  it("F8-37 · исключение относит ровно свой перечень — ни сверх, ни меньше", () => {
    expect(census.excuseDrift).toEqual([]);
  });
});
