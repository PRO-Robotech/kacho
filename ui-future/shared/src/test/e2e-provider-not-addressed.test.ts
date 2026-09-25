// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  codeSources,
  formatFinding,
  providerAddressesIn,
  providerCensusOf,
  type Excuse,
} from "./provider-address-census";

/**
 * F8-43 — код сквозного набора не обращается к чужому поставщику ни одним
 * транспортом (приёмка F8, Р6 п. 4; DoD 16).
 *
 * Посев, фикстура и оснастка подъёма уровня ходят КОНТЕКСТОМ ЗАПРОСОВ
 * (`page.request`, `request.newContext()`), а его обращений перепись страниц не
 * видит. Поэтому отрицание о поставщике по коду набора держит не она, а эта
 * перепись: она судит текст, которым обращение выпущено, и транспорт ей
 * безразличен. Обход — ровно `ui-future/e2e`, который гейт прод-файлов
 * исключает.
 *
 * Отнесённое по референту одно: подсадка положительной стороны переписи
 * страниц в F8-18 — обращение к поставщику из кода страницы, ради которого та
 * перепись и обязана покраснеть. Оно разрешено только внутри теста F8-18 и
 * только своим перечнем (`covers`): прежде исключение по префиксу имени теста
 * прощало и адрес, дописанный в соседний тест F8-18 (8 → 9 молча, F3).
 */

const e2eRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../e2e");

export const SUITE_EXCUSES: readonly Excuse[] = [
  {
    file: /^specs\/identity-ceremony\.spec\.ts$/,
    test: /^F8-18 /,
    reason: "подсадка положительной стороны переписи страниц (Р6 п. 3): обращение к поставщику из кода страницы",
    // Четыре подсаженных обращения и четыре строки ожидаемой переписи — и ничего
    // сверх: адрес поставщика, дописанный в другой тест F8-18, встал бы сверх
    // перечня и краснит (F3), а не проходит по префиксу имени теста.
    covers: [
      "specs/identity-ceremony.spec.ts адрес поставщика «/.ory/kratos/public/sessions/whoami»",
      "specs/identity-ceremony.spec.ts адрес поставщика «/oauth2/auth»",
      "specs/identity-ceremony.spec.ts адрес поставщика в шаблоне «${…}/self-service/logout/browser»",
      "specs/identity-ceremony.spec.ts адрес поставщика «/.ory/kratos/public/self-service/login/browser»",
      "specs/identity-ceremony.spec.ts адрес поставщика в шаблоне «GET ${…}/.ory/kratos/public/sessions/whoami запрос»",
      "specs/identity-ceremony.spec.ts адрес поставщика в шаблоне «GET ${…}/oauth2/auth запрос»",
      "specs/identity-ceremony.spec.ts адрес поставщика в шаблоне «GET ${…}/self-service/logout/browser документ»",
      "specs/identity-ceremony.spec.ts адрес поставщика в шаблоне «GET ${…}/.ory/kratos/public/self-service/login/browser документ»",
    ],
  },
];

const census = providerCensusOf(
  codeSources(e2eRoot, () => true),
  SUITE_EXCUSES,
);

process.stdout.write(
  `\n[F8-43] файлов кода набора прочитано ${census.filesRead} · ссылок на адрес поставщика ${census.findings.length}` +
    ` · отнесено по референту ${census.excused.length} · исключений без предмета ${census.staleExcuses.length}\n`,
);

describe("F8-43 · код набора не обращается к поставщику ни одним транспортом", () => {
  it("F8-43 · перепись прочла код набора — знаменатель обхода", () => {
    expect(census.filesRead).toBeGreaterThan(20);
  });

  it("F8-43 · ссылок на адрес поставщика в коде набора ноль", () => {
    expect(census.findings.map(formatFinding)).toEqual([]);
  });

  it("F8-43 · исключение по референту чему-то служит", () => {
    expect(census.staleExcuses).toEqual([]);
  });

  it("F8-43 · исключение относит ровно свой перечень — ни сверх, ни меньше", () => {
    expect(census.excuseDrift).toEqual([]);
  });

  it("F8-43 · подсаженная ссылка краснит с координатой, а ссылка на путь нашего глагола — нет", () => {
    // Изменён ровно один факт — чей это адрес.
    const planted = providerAddressesIn(
      "specs/fixtures.ts",
      'export const s = (page) => page.request.get("/.ory/kratos/public/sessions/whoami");',
    );
    expect(planted.map(formatFinding)).toEqual([
      "specs/fixtures.ts:1 адрес поставщика «/.ory/kratos/public/sessions/whoami»",
    ]);
    const twin = providerAddressesIn(
      "specs/fixtures.ts",
      'export const s = (page) => page.request.get("/iam/v1/auth/second-factor");',
    );
    expect(twin).toEqual([]);
    // Ручка базы поставщика в окружении — тоже обращение к нему.
    expect(
      providerAddressesIn("specs/x.ts", "export const b = process.env.KACHO_KRATOS_PUBLIC;").map((f) => f.what),
    ).toEqual(["ручка базы поставщика KACHO_KRATOS_PUBLIC"]);
  });
});
