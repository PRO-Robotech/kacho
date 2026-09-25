// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { formatIssuance, isEdgePathText, issuanceCensusOf, issuancesIn } from "./issuance-census";

/**
 * F8-46 · перепись мест выпуска способна упасть: подсаженный вызов транспорта
 * мимо упорядочения — находка С КООРДИНАТОЙ; близнец — тот же вызов
 * упорядочивающего транспорта — ноль. Изменён ровно один факт: через что
 * выпущено обращение.
 */

const lines = (...xs: string[]) => xs.join("\n");
const planted = (body: string) => [{ file: "host/src/Planted.tsx", text: body }];

describe("F8-46 · перепись мест выпуска способна упасть", () => {
  it("F8-46 · подсаженный вызов `fetch` мимо упорядочения — 1 находка с координатой", () => {
    const census = issuanceCensusOf(
      planted(lines("export async function read() {", '  return fetch("/iam/v1/accounts");', "}")),
      [],
    );
    expect(census.findings.map(formatIssuance)).toEqual(["host/src/Planted.tsx:2 вызов транспорта fetch"]);
  });

  it("F8-46 · близнец: тот же вызов упорядочивающего транспорта — 0 находок", () => {
    const census = issuanceCensusOf(
      planted(
        lines(
          'import { orderedTransport } from "@shared/api/carrier-order";',
          "export async function read() {",
          '  return orderedTransport.fetch("/iam/v1/accounts");',
          "}",
        ),
      ),
      [],
    );
    expect(census.findings).toEqual([]);
  });

  it("F8-46 · каждый вид транспорта браузера — находка", () => {
    const found = issuancesIn(
      "shared/src/planted.ts",
      lines(
        'window.fetch("/vpc/v1/networks");',
        'globalThis.fetch("/vpc/v1/networks");',
        'navigator.sendBeacon("/iam/v1/audit", "x");',
        'new EventSource("/subscription/v1/events");',
        "new XMLHttpRequest();",
        'new WebSocket("wss://console.test/x");',
      ),
    );
    expect(found.map((f) => f.line)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("F8-46 · переход документа на путь края — находка; на путь консоли — нет", () => {
    const found = issuancesIn(
      "shared/src/planted.ts",
      lines(
        'window.location.assign("/iam/v1/auth/logout");',
        "location.href = `/iam/v1/accounts/${1}`;",
        'window.location.assign("/settings");',
        'window.location.replace("/login?returnTo=%2F");',
      ),
    );
    expect(found.map(formatIssuance)).toEqual([
      "shared/src/planted.ts:1 переход документа на путь края «/iam/v1/auth/logout»",
      "shared/src/planted.ts:2 переход документа на путь края «/iam/v1/accounts/»",
    ]);
  });

  it("F8-46 · комментарий и текст, называющие транспорт, находками не являются", () => {
    const found = issuancesIn(
      "shared/src/planted.ts",
      lines('// прежде здесь стоял fetch("/iam/v1/me")', 'export const hint = "fetch(/iam/v1/me) не зовётся";'),
    );
    expect(found).toEqual([]);
  });

  it("F8-46 · исключение, которому нечего исключать, — само находка", () => {
    const census = issuanceCensusOf(planted("export const x = 1;"), [
      { file: "host/src/Planted.tsx", what: /^построение EventSource$/, reason: "подсаженное основание" },
    ]);
    expect(census.staleExcuses).toEqual(["host/src/Planted.tsx · ^построение EventSource$ — подсаженное основание"]);
  });

  it("F8-46 · пустой обход — отказ, а не ноль находок", () => {
    expect(() => issuanceCensusOf([], [])).toThrow("прочитала 0 файлов");
  });

  it("F8-46 · распознаватель пути края: пути доменов и операций — да, пути консоли — нет", () => {
    for (const edge of ["/iam/v1/me", "/vpc/v1/networks", "/operations/op-1", "https://console.test/iam/v1/accounts"]) {
      expect([edge, isEdgePathText(edge)]).toEqual([edge, true]);
    }
    for (const own of ["/settings", "/login", "/vpc/networks", "/kacho-logo.svg"]) {
      expect([own, isEdgePathText(own)]).toEqual([own, false]);
    }
  });
});
