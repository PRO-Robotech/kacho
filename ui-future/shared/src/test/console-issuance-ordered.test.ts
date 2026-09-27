// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import { ORDERING_TRANSPORT, formatIssuance, issuanceCensusOf, type IssuanceExcuse } from "./issuance-census";
import { codeSources } from "./provider-address-census";

/**
 * F8-46 · перепись мест выпуска — быстрая подсказка о том, что обращения консоли
 * к краю идут через упорядочивающий транспорт (приёмка F8, Р10, §11.1, N18).
 *
 * Вызов транспорта браузера вне `shared/src/api/carrier-order.ts` и переход
 * документа на путь края — находка с координатой; перечень узнаваемых форм — у
 * распознавателя (`issuance-census.ts`). До упорядочения таких мест было девять
 * в семи файлах; после — ноль СРЕДИ УЗНАННЫХ ФОРМ.
 *
 * Полноты перепись НЕ обещает: то, что мимо транспорта не выпускается ничего,
 * держит исполнение — страж `issuance-guard.ts` (jest всех модулей и браузер
 * сквозных проб), — а прод-код вне исполненного держит правило линта
 * `issuance-ordering.eslint.config.js`. Разбор выбора — в шапке распознавателя.
 *
 * Способность упасть — `console-issuance-ordered.injection.test.ts`.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Прод-файлы консоли: без проб, без сквозного набора, без каталогов проб. */
function isProductFile(rel: string): boolean {
  return /\.(ts|tsx)$/.test(rel) && !/\.test\./.test(rel) && !rel.startsWith("e2e/") && !/(^|\/)test\//.test(rel);
}

export const ISSUANCE_EXCUSES: readonly IssuanceExcuse[] = [
  {
    file: "shared/src/lib/subscription/hub.ts",
    what: /^построение EventSource$/,
    reason:
      "приёмник потока строит дом клиента потока (#1021, один на консоль); поток стоит на учёте упорядочения — " +
      "закрыт до глагола, ставящего носитель, открыт после исхода, не открывается, пока глагол идёт",
  },
];

const census = issuanceCensusOf(codeSources(uiRoot, isProductFile), ISSUANCE_EXCUSES);

process.stdout.write(
  `\n[F8-46] перепись мест выпуска: прод-файлов консоли прочитано ${census.filesRead}` +
    ` · в упорядочивающем транспорте ${census.transport.length}` +
    ` · вне упорядочения ${census.findings.length}` +
    ` · по основанию ${census.excused.length} · исключений без предмета ${census.staleExcuses.length}\n` +
    census.transport.map((f) => `    транспорт: ${formatIssuance(f)}\n`).join("") +
    census.excused.map((e) => `    по основанию: ${formatIssuance(e)} — ${e.reason}\n`).join(""),
);

describe("F8-46 · перепись мест выпуска: мимо упорядочения обращений к краю нет", () => {
  it("F8-46 · перепись прочла прод-дерево консоли — знаменатель обхода", () => {
    // Нижняя граница, а не точное число: ноль означал бы обход не того корня.
    expect(census.filesRead).toBeGreaterThan(300);
  });

  it("F8-46 · распознаватель видит сам упорядочивающий транспорт — иначе он не видит ничего", () => {
    expect(census.transport.map(formatIssuance)).not.toEqual([]);
    expect(census.transport.every((f) => f.file === ORDERING_TRANSPORT)).toBe(true);
  });

  it("F8-46 · вне упорядочивающего транспорта мест выпуска ноль", () => {
    expect(census.findings.map(formatIssuance)).toEqual([]);
  });

  it("F8-46 · каждое исключение по основанию чему-то служит", () => {
    expect(census.staleExcuses).toEqual([]);
  });
});
