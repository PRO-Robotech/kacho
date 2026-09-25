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

  it("F8-46 · типовое упоминание и проверка наличия транспорта находками не являются", () => {
    const found = issuancesIn(
      "shared/src/planted.ts",
      lines(
        "let es: EventSource | null = null;",
        "type Send = typeof globalThis.fetch;",
        "interface Client { fetch(url: string): Promise<Response> }",
        'export const available = () => typeof EventSource !== "undefined" && typeof fetch === "function";',
      ),
    );
    expect(found).toEqual([]);
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

/**
 * Возврат check-verifier круга 2 (F2): перепись видела только ВЫЗОВ транспорта —
 * `fetch(…)` и `<глобальный>.fetch(…)` — и молчала на трёх законных формах того же
 * обращения в `dashboard/src/utils/api-client.ts`: ссылке в переменной, привязанной
 * ссылке и доступе по строке. Предмет — не вызов, а ЧТЕНИЕ транспорта браузера:
 * получивший его выпускает обращение мимо упорядочения тем же способом.
 *
 * Каждая форма стоит в паре с близнецом, который меняет ровно один факт — чей
 * `fetch` читается: вместо транспорта браузера — упорядочивающий транспорт.
 */
const API_CLIENT = "dashboard/src/utils/api-client.ts";

describe("F8-46 · три формы из возврата круга 2 — находка; близнец через упорядочение — ноль", () => {
  it("F8-46 · ссылка на `fetch` в переменной — находка; близнец `orderedTransport.fetch` — ноль", () => {
    const census = issuanceCensusOf([{ file: API_CLIENT, text: lines("const send = fetch;", 'send("/iam/v1/me");') }], []);
    expect(census.findings.map(formatIssuance)).toEqual([`${API_CLIENT}:1 ссылка на транспорт fetch`]);
    const twin = issuanceCensusOf(
      [{ file: API_CLIENT, text: lines("const send = orderedTransport.fetch;", 'send("/iam/v1/me");') }],
      [],
    );
    expect(twin.findings).toEqual([]);
  });

  it("F8-46 · привязанная ссылка `globalThis.fetch.bind(globalThis)` — находка; близнец — ноль", () => {
    const census = issuanceCensusOf(
      [{ file: API_CLIENT, text: lines("const send = globalThis.fetch.bind(globalThis);", 'send("/iam/v1/me");') }],
      [],
    );
    expect(census.findings.map(formatIssuance)).toEqual([
      `${API_CLIENT}:1 ссылка на транспорт globalThis.fetch`,
      `${API_CLIENT}:1 глобальный объект globalThis передан значением — что с ним сделают, распознавателю не видно`,
    ]);
    const twin = issuanceCensusOf(
      [
        {
          file: API_CLIENT,
          text: lines("const send = orderedTransport.fetch.bind(orderedTransport);", 'send("/iam/v1/me");'),
        },
      ],
      [],
    );
    expect(twin.findings).toEqual([]);
  });

  it('F8-46 · доступ по строке `window["fetch"](…)` — находка; близнец `orderedTransport["fetch"]` — ноль', () => {
    const census = issuanceCensusOf([{ file: API_CLIENT, text: 'window["fetch"]("/iam/v1/me");' }], []);
    expect(census.findings.map(formatIssuance)).toEqual([`${API_CLIENT}:1 вызов транспорта window.fetch`]);
    const twin = issuanceCensusOf([{ file: API_CLIENT, text: 'orderedTransport["fetch"]("/iam/v1/me");' }], []);
    expect(twin.findings).toEqual([]);
  });
});

/**
 * Прочие законные формы записи того же предмета — по одной на строку таблицы.
 * Распознаватель, знающий три формы из возврата и не знающий соседних, вернул бы
 * ту же находку следующим кругом. Каждая форма — находка на своей строке; каждый
 * близнец — ноль.
 */
const RED_FORMS: ReadonlyArray<[string, string]> = [
  ["голый вызов", 'fetch("/iam/v1/me");'],
  ["передача аргументом", "useClient(fetch);"],
  ["сокращённое свойство", "createClient({ fetch });"],
  ["call/apply", 'fetch.call(undefined, "/iam/v1/me");'],
  ["запятая", '(0, fetch)("/iam/v1/me");'],
  ["необязательная цепочка", 'window?.fetch?.("/iam/v1/me");'],
  ["приведение типа", '(globalThis as unknown as Window).fetch("/iam/v1/me");'],
  ["self", 'self.fetch("/iam/v1/me");'],
  ["доступ по шаблону", 'globalThis[`fetch`]("/iam/v1/me");'],
  ["ключ из постоянной", 'const k = "fetch"; window[k]("/iam/v1/me");'],
  ["вычисленный ключ", 'window[name]("/iam/v1/me");'],
  ["псевдоним глобального объекта", 'const g = globalThis as unknown as Window; g.fetch("/iam/v1/me");'],
  ["цепочка глобальных", 'window.top.fetch("/iam/v1/me");'],
  ["окно документа", 'document.defaultView.fetch("/iam/v1/me");'],
  ["деструктуризация с переименованием", "const { fetch: send } = window;"],
  ["деструктуризация под тем же именем", "const { fetch } = globalThis;"],
  ["деструктуризация по строке", 'const { ["fetch"]: send } = self;'],
  ["отражение", 'Reflect.get(window, "fetch");'],
  ["локальное имя транспорта", 'import { fetch as send } from "undici";'],
  ["sendBeacon по строке", 'navigator["sendBeacon"]("/iam/v1/audit", "x");'],
  ["привязанный sendBeacon", "const beacon = navigator.sendBeacon.bind(navigator);"],
  ["псевдоним конструктора", 'const ES = EventSource; new ES("/subscription/v1/events");'],
  ["конструктор на глобальном объекте", 'new window.WebSocket("wss://console.test/x");'],
  ["конструктор по строке", 'new globalThis["XMLHttpRequest"]();'],
  ["наследование конструктора", "class Socket extends WebSocket {}"],
];

const TWIN_FORMS: ReadonlyArray<[string, string]> = [
  ["вызов упорядочения", 'orderedTransport.fetch("/iam/v1/me");'],
  ["ссылка на упорядочение", "const send = orderedTransport.fetch;"],
  ["деструктуризация упорядочения", "const { fetch: send } = orderedTransport;"],
  ["ключ объекта", "const api = { fetch: orderedTransport.fetch };"],
  [
    "состояние под ключом-символом",
    'const KEY = Symbol.for("k"); const g = globalThis as unknown as Record<symbol, object>; const o = (g[KEY] ??= {});',
  ],
  ["известный ключ глобального объекта", 'const cfg = window["__KACHO_CONFIG__"];'],
  ["член глобального объекта не транспорт", 'window.addEventListener("focus", onFocus);'],
  ["проверка наличия ключа", '"fetch" in window;'],
];

describe("F8-46 · каждая законная форма чтения транспорта — находка на своей строке", () => {
  it.each(RED_FORMS)("F8-46 · %s — находка", (_form, code) => {
    const found = issuancesIn(API_CLIENT, lines("// подсажено", code));
    expect(found.length).toBeGreaterThan(0);
    expect(found.map((f) => f.line)).toEqual(found.map(() => 2));
  });

  it.each(TWIN_FORMS)("F8-46 · близнец «%s» — ноль", (_form, code) => {
    expect(issuancesIn(API_CLIENT, lines("// подсажено", code)).map(formatIssuance)).toEqual([]);
  });
});
