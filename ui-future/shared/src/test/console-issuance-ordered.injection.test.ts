// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
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
/**
 * Упорядочивающий транспорт узнаётся по ИМПОРТУ из своего модуля, а не по имени:
 * близнец несёт тот же импорт, что и настоящий файл.
 */
const ORDERED_IMPORT = 'import { orderedTransport } from "@shared/api/carrier-order";';

describe("F8-46 · три формы из возврата круга 2 — находка; близнец через упорядочение — ноль", () => {
  it("F8-46 · ссылка на `fetch` в переменной — находка; близнец `orderedTransport.fetch` — ноль", () => {
    const census = issuanceCensusOf([{ file: API_CLIENT, text: lines("const send = fetch;", 'send("/iam/v1/me");') }], []);
    expect(census.findings.map(formatIssuance)).toEqual([`${API_CLIENT}:1 ссылка на транспорт fetch`]);
    const twin = issuanceCensusOf(
      [{ file: API_CLIENT, text: lines(ORDERED_IMPORT, "const send = orderedTransport.fetch;", 'send("/iam/v1/me");') }],
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
          text: lines(ORDERED_IMPORT, "const send = orderedTransport.fetch.bind(orderedTransport);", 'send("/iam/v1/me");'),
        },
      ],
      [],
    );
    expect(twin.findings).toEqual([]);
  });

  it('F8-46 · доступ по строке `window["fetch"](…)` — находка; близнец `orderedTransport["fetch"]` — ноль', () => {
    const census = issuanceCensusOf([{ file: API_CLIENT, text: 'window["fetch"]("/iam/v1/me");' }], []);
    expect(census.findings.map(formatIssuance)).toEqual([`${API_CLIENT}:1 вызов транспорта window.fetch`]);
    const twin = issuanceCensusOf([{ file: API_CLIENT, text: lines(ORDERED_IMPORT, 'orderedTransport["fetch"]("/iam/v1/me");') }], []);
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
  ["вызов упорядочения", `${ORDERED_IMPORT} orderedTransport.fetch("/iam/v1/me");`],
  ["ссылка на упорядочение", `${ORDERED_IMPORT} const send = orderedTransport.fetch;`],
  ["деструктуризация упорядочения", `${ORDERED_IMPORT} const { fetch: send } = orderedTransport;`],
  ["ключ объекта", `${ORDERED_IMPORT} const api = { fetch: orderedTransport.fetch };`],
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

/**
 * Возврат check-verifier круга 3 (F2b): распознаватель не знал окна, которое
 * ОТДАЁТ ЗНАЧЕНИЕМ API браузера — `contentWindow` фрейма, результат
 * `window.open()` и голого `open()`, `UIEvent.view`, `MessageEvent.source`, — и
 * молчал на чтении транспорта с такого окна, тогда как `….defaultView` краснел.
 *
 * Инъекции — в НАСТОЯЩИЙ `dashboard/src/utils/api-client.ts`: строка выпуска
 * через упорядочение заменяется той же строкой через окно, отданное API. Близнец —
 * тот же файл без правки: ровно один факт — чьим `fetch` выпущено обращение.
 * Текст находки утверждается целиком: окно узнано, а не пойман «неизвестный
 * получатель».
 */
const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const REAL_CLIENT = fs.readFileSync(path.join(uiRoot, API_CLIENT), "utf8");
const ORDERED_CALL = "  const res = await orderedTransport.fetch(path, {";

/** Настоящий файл с подсаженной формой вместо выпуска через упорядочение; `line` — строка выпуска. */
function plantedClient(pre: readonly string[], call: string): { text: string; line: number } {
  const rows = REAL_CLIENT.split("\n");
  const at = rows.indexOf(ORDERED_CALL);
  rows.splice(at, 1, ...pre.map((p) => `  ${p}`), `  const res = await ${call}(path, {`);
  return { text: rows.join("\n"), line: at + 1 + pre.length };
}

const REAL_FILE_FORMS: ReadonlyArray<[string, readonly string[], string, string]> = [
  ["x1 · contentWindow фрейма", ['const frame = document.querySelector("iframe") as HTMLIFrameElement;'], "frame.contentWindow!.fetch", "вызов транспорта frame.contentWindow.fetch"],
  ["contentWindow объекта", ['const obj = document.querySelector("object") as HTMLObjectElement;'], "obj.contentWindow!.fetch", "вызов транспорта obj.contentWindow.fetch"],
  ["псевдоним contentWindow", ['const frame = document.querySelector("iframe") as HTMLIFrameElement;', "const w = frame.contentWindow!;"], "w.fetch", "вызов транспорта w.fetch"],
  ["x2 · результат window.open()", ['const w = window.open("about:blank")!;'], "w.fetch", "вызов транспорта w.fetch"],
  ["результат голого open()", ['const w = open("about:blank")!;'], "w.fetch", "вызов транспорта w.fetch"],
  ["x4 · UIEvent.view", ['const view = new UIEvent("focus").view!;'], "view.fetch", "вызов транспорта view.fetch"],
  ["MessageEvent.source", ['const src = new MessageEvent("message").source as Window;'], "src.fetch", "вызов транспорта src.fetch"],
  ["близнец круга 3 · defaultView", ['const frame = document.querySelector("iframe") as HTMLIFrameElement;'], "frame.contentDocument!.defaultView!.fetch", "вызов транспорта frame.contentDocument!.defaultView.fetch"],
];

describe("F8-46 · F2b · окно, отданное API браузера, — глобальный объект; инъекция в настоящий api-client.ts", () => {
  it("F8-46 · предпосылка: в настоящем файле ровно одна строка выпуска через упорядочение", () => {
    expect(REAL_CLIENT.split("\n").filter((row) => row === ORDERED_CALL)).toHaveLength(1);
  });

  it("F8-46 · близнец — настоящий файл без правки (orderedTransport.fetch): 0 находок", () => {
    expect(issuancesIn(API_CLIENT, REAL_CLIENT).map(formatIssuance)).toEqual([]);
  });

  it.each(REAL_FILE_FORMS)("F8-46 · %s — находка на строке выпуска", (_form, pre, call, what) => {
    const { text, line } = plantedClient(pre, call);
    expect(issuancesIn(API_CLIENT, text).map(formatIssuance)).toEqual([`${API_CLIENT}:${line} ${what}`]);
  });
});

/**
 * Прочие законные формы того же класса. Окно, которое отдал API браузера,
 * судится как глобальный объект целиком: ключом транспорта, вычисленным ключом,
 * передачей значением, разбором, отражением. Окно, чьего происхождения разбор не
 * знает (`e.currentTarget as Window`, возврат функции), узнаётся там, где с него
 * читают транспорт: ключ транспорта на получателе, который не упорядочивающий
 * транспорт, — находка. Упорядочивающий транспорт узнаётся по импорту из своего
 * модуля, а не по имени.
 */
const WINDOW_RED_FORMS: ReadonlyArray<[string, string, readonly string[]]> = [
  [
    "contentWindow по строке",
    'frame["contentWindow"]!.fetch("/iam/v1/me");',
    ['вызов транспорта frame["contentWindow"].fetch'],
  ],
  [
    "contentWindow разбором",
    'const { contentWindow: w } = frame; w!.fetch("/iam/v1/me");',
    ["вызов транспорта w.fetch"],
  ],
  [
    "contentWindow разбором под своим именем",
    'const { contentWindow } = frame; contentWindow!.fetch("/iam/v1/me");',
    ["вызов транспорта contentWindow.fetch"],
  ],
  [
    "ключ из постоянной на окне фрейма",
    'const k = "fetch"; frame.contentWindow![k]("/iam/v1/me");',
    ["вызов транспорта frame.contentWindow.fetch"],
  ],
  [
    "вычисленный ключ на окне фрейма",
    'frame.contentWindow![name]("/iam/v1/me");',
    ["доступ к глобальному объекту frame.contentWindow по вычисленному ключу"],
  ],
  [
    "окно фрейма передано значением",
    "use(frame.contentWindow);",
    ["глобальный объект frame.contentWindow передан значением — что с ним сделают, распознавателю не видно"],
  ],
  [
    "разбор окна фрейма",
    "const { fetch: send } = frame.contentWindow!;",
    ["ссылка на транспорт frame.contentWindow.fetch"],
  ],
  [
    "отражение на окне фрейма",
    'Reflect.get(frame.contentWindow!, "fetch");',
    ["ссылка на транспорт frame.contentWindow.fetch отражением", "глобальный объект frame.contentWindow передан значением — что с ним сделают, распознавателю не видно"],
  ],
  [
    "окно-член передано значением",
    "use(window.top);",
    ["глобальный объект window.top передан значением — что с ним сделают, распознавателю не видно"],
  ],
  [
    "окно документа передано значением",
    "use(document.defaultView);",
    ["глобальный объект document.defaultView передан значением — что с ним сделают, распознавателю не видно"],
  ],
  [
    "open по строке",
    'window["open"]("about:blank")!.fetch("/iam/v1/me");',
    ['вызов транспорта window["open"]("about:blank").fetch'],
  ],
  [
    "open через запятую",
    '(0, window.open)("about:blank")!.fetch("/iam/v1/me");',
    ['вызов транспорта (0,window.open)("about:blank").fetch'],
  ],
  [
    "open псевдонимом",
    'const o = window.open; o("about:blank")!.fetch("/iam/v1/me");',
    ['вызов транспорта o("about:blank").fetch'],
  ],
  [
    "open с привязкой",
    'const o = window.open.bind(null); o("about:blank")![name]("/iam/v1/me");',
    ['доступ к глобальному объекту o("about:blank") по вычисленному ключу'],
  ],
  [
    "open через call",
    'window.open.call(null, "about:blank")![name]("/iam/v1/me");',
    ['доступ к глобальному объекту window.open.call(null,"about:blank") по вычисленному ключу'],
  ],
  [
    "document.open с тремя аргументами",
    'document.open("about:blank", "w", "")!.fetch("/iam/v1/me");',
    ['вызов транспорта document.open("about:blank","w","").fetch'],
  ],
  [
    "результат window.open() передан значением",
    'use(window.open("about:blank"));',
    ['глобальный объект window.open("about:blank") передан значением — что с ним сделают, распознавателю не видно'],
  ],
  [
    "результат open() передан значением",
    'const w = open("about:blank"); use(w);',
    ["глобальный объект w передан значением — что с ним сделают, распознавателю не видно"],
  ],
  [
    "вычисленный ключ на окне open()",
    'const w = window.open("about:blank")!; w[name]("/iam/v1/me");',
    ["доступ к глобальному объекту w по вычисленному ключу"],
  ],
  [
    "open() — переход документа на путь края",
    'open("/iam/v1/auth/logout");',
    ["переход документа на путь края «/iam/v1/auth/logout»"],
  ],
  [
    "UIEvent.view разбором",
    'const { view } = event; view!.fetch("/iam/v1/me");',
    ["вызов транспорта view.fetch"],
  ],
  [
    "разбор транспорта из UIEvent.view",
    "const { fetch: send } = event.view!;",
    ["ссылка на транспорт event.view.fetch"],
  ],
  [
    "MessageEvent.source",
    '(event.source as Window).fetch("/iam/v1/me");',
    ["вызов транспорта event.source.fetch"],
  ],
  [
    "окно события",
    '(event.currentTarget as Window).fetch("/iam/v1/me");',
    ["вызов транспорта event.currentTarget.fetch — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
  [
    "окно из функции",
    'windowOf(el).fetch("/iam/v1/me");',
    ["вызов транспорта windowOf(el).fetch — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
  [
    "разбор неизвестного получателя",
    "const { fetch: send } = windowOf(el);",
    ["ссылка на транспорт windowOf(el).fetch — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
  [
    "отражение на неизвестном получателе",
    'Reflect.get(windowOf(el), "fetch");',
    ["ссылка на транспорт windowOf(el).fetch отражением"],
  ],
  [
    "конструктор на неизвестном получателе",
    'new (windowOf(el)).WebSocket("wss://console.test/x");',
    ["построение windowOf(el).WebSocket — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
  [
    "двойник под именем упорядочения",
    'const orderedTransport = windowOf(el); orderedTransport.fetch("/iam/v1/me");',
    ["вызов транспорта orderedTransport.fetch — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
  [
    "одноимённый модуль — не упорядочение",
    'import { orderedTransport } from "./carrier-order"; orderedTransport.fetch("/iam/v1/me");',
    ["вызов транспорта orderedTransport.fetch — получатель не опознан, и это не упорядочивающий транспорт"],
  ],
];

const WINDOW_TWIN_FORMS: ReadonlyArray<[string, string, string]> = [
  ["упорядочение под другим именем импорта", API_CLIENT, 'import { orderedTransport as t } from "@shared/api/carrier-order"; t.fetch("/iam/v1/me");'],
  ["упорядочение пространством имён", API_CLIENT, 'import * as co from "@shared/api/carrier-order"; co.orderedTransport.fetch("/iam/v1/me");'],
  ["псевдоним упорядочения", API_CLIENT, `${ORDERED_IMPORT} const t = orderedTransport; t.fetch("/iam/v1/me");`],
  ["относительный импорт упорядочения", "shared/src/api/client.ts", 'import { orderedTransport } from "./carrier-order"; orderedTransport.fetch("/iam/v1/me");'],
  ["своя функция open", API_CLIENT, 'function open(id: string) { return { id }; } const w = open("x"); use(w);'],
  ["open из разбора", API_CLIENT, 'const { open } = props; const w = open("/iam/v1/me"); use(w);'],
  ["open у indexedDB", API_CLIENT, 'const req = indexedDB.open("db", 1); use(req);'],
  ["open у зависимости", API_CLIENT, "const es = this.deps.open(url); use(es);"],
  ["окно открыто, результат не взят", API_CLIENT, 'window.open("/settings", "_blank", "noopener");'],
  ["окно проверено на истинность", API_CLIENT, 'if (!window.open("about:blank")) use(null);'],
  ["член окна фрейма не транспорт", API_CLIENT, 'frame.contentWindow!.postMessage("x", "*");'],
  ["документ фрейма не окно", API_CLIENT, "use(frame.contentDocument);"],
  ["view — не окно по вычисленному ключу", API_CLIENT, "state.view[key] = 1;"],
  ["source — поток, не транспорт", API_CLIENT, "ch.source.close(); use(ch.source);"],
  ["свойство события не транспорт", API_CLIENT, "use(event.view!.innerWidth);"],
];

describe("F8-46 · F2b · прочие формы окна, отданного API, — находка; близнецы — ноль", () => {
  it.each(WINDOW_RED_FORMS)("F8-46 · %s — находка, названная своей причиной", (_form, code, what) => {
    expect(issuancesIn(API_CLIENT, lines("// подсажено", code)).map(formatIssuance)).toEqual(
      what.map((w) => `${API_CLIENT}:2 ${w}`),
    );
  });

  it.each(WINDOW_TWIN_FORMS)("F8-46 · близнец «%s» — ноль", (_form, file, code) => {
    expect(issuancesIn(file, lines("// подсажено", code)).map(formatIssuance)).toEqual([]);
  });
});
