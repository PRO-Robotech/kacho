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
 * Отнесённое по референту — прокси разработки и ручки и построители базы в
 * `config.ts` — печатается поимённо, находкой не считается и остаётся
 * предметом снятия ручек (#2733). Исключение, которому нечего исключать,
 * краснит перепись: оно пережило бы свой предмет. Так снято исключение
 * страницы доступности служб — её обращение к поставщику сняла #2733.
 *
 * Исключение относит ТОЧНЫЙ перечень (`covers`), а не всё, что в файле
 * появится: обёртка построителя в `config.ts` и её вызов из прод-файла прежде
 * давали «обращений 0» при отнесённом 8 → 10 (F3). Сверх перечня — красное.
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
    reason: "прокси сервера разработки: в браузер не попадает; снимается вместе с ручками (#2733)",
    covers: [
      "dashboard/vite.config.ts ручка базы поставщика KACHO_KRATOS_UI_BASE",
      "host/vite.config.ts ручка базы поставщика KACHO_KRATOS_UI_BASE",
    ],
  },
  {
    file: /^shared\/src\/lib\/config\.ts$/,
    reason: "ручки базы поставщика и их построители — объявление, не обращение; вызывающих вне файла нет (#2733)",
    // Поле ручки, её значение по умолчанию и построитель — и ничего сверх: обёртка
    // построителя, заведённая здесь, встала бы сверх перечня и краснит (F3).
    covers: [
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
      "shared/src/lib/config.ts ручка базы поставщика VITE_KRATOS_URL",
      "shared/src/lib/config.ts адрес поставщика «/.ory/kratos/public»",
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
    ],
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
