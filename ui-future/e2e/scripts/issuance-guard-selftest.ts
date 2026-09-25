// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА СТРАЖА МЕСТ ВЫПУСКА В НАСТОЯЩЕМ БРАУЗЕРЕ (приёмка F8, Р10, F8-46).
 *
 * Страж ставится в контекст браузера той же проводкой, что у набора проб
 * (`specs/issuance-guard.ts`: `guardBrowser` → `guardContext`), и судится
 * ИСПОЛНЕНИЕМ: страница на сервере петли выпускает обращение каждой формой, которой
 * код добирается до `fetch` окна, а сервер пишет, что до него дошло.
 *
 *   • ИНЪЕКЦИЯ — формы из возвратов проверки (ссылка, привязка, строка и
 *     вычисленный ключ, отражение, разбор, окно фрейма пустого, `srcdoc` и
 *     своего происхождения — через `contentWindow`, `contentDocument` и
 *     `frames[i]`, окно объекта, результат `window.open()` и голого `open()`,
 *     `UIEvent.view`, `MessageEvent.source` — синтетический и присланный фреймом,
 *     сценарий страницы при разборе документа, проглоченный отказ): до сервера
 *     не дошло НИЧЕГО, находка названа адресом;
 *   • БЛИЗНЕЦЫ — тот же адрес, выпущенный НАСТОЯЩИМ упорядочивающим транспортом
 *     (`shared/src/api/carrier-order.ts`, разобранный без типов и отданный
 *     странице модулем): чтение, мутация, глагол, ставящий носитель; транспорт
 *     пробы — дошло, находок ноль.
 *
 * Изменён ровно один факт: через что выпущено обращение. Стенд не нужен; нужен
 * браузер — тот же, что у набора (`KACHO_CHROMIUM` либо установленный playwright).
 *
 *   • ФИКСТУРА НАБОРА — находки выше забирает `takeBreaches` напрямую, мимо
 *     фикстуры, поэтому снятый отказ в `issuanceLedger` (`specs/fixtures.ts`) их
 *     не менял (опыт E3 проверки: 40 из 40 зелёных). Судится и он: отдельный
 *     процесс прогонщика исполняет канарейку `issuance-canary/fixtures.canary.ts`,
 *     взявшую `test` у настоящей фикстуры набора, и её исход сверяется по отчёту:
 *     проглоченный обход — проба УПАЛА текстом фикстуры, близнец упорядочивающим
 *     транспортом — прошёл, до сервера дошёл только близнец.
 *
 * Запуск: node scripts/issuance-guard-selftest.ts
 */

import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { createRequire, stripTypeScriptTypes } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "@playwright/test";

import { PROBE_FETCH, guardBrowser, takeBreaches } from "../specs/issuance-guard.ts";
import { CANARY_ORIGIN_ENV, CANARY_OUT_ENV, FIXTURE_CANARY, FIXTURE_REFUSAL } from "./issuance-canary/names.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const senderSource = fs.readFileSync(path.join(here, "../../shared/src/api/carrier-order.ts"), "utf8");
const SENDER_JS = `${stripTypeScriptTypes(senderSource)}\nwindow.__ordered = orderedTransport;\n`;

const PAGE = "<!doctype html><html><body><p>страница</p></body></html>";
const FRAME = "<!doctype html><html><body><p>фрейм</p></body></html>";
const EARLY = '<!doctype html><html><body><script>fetch("/b/early").catch(() => undefined);</script></body></html>';
const WITH_SENDER = '<!doctype html><html><body><script type="module" src="/carrier-order.js"></script></body></html>';

let failed = 0;
let checked = 0;
function check(condition: boolean, what: string): void {
  checked += 1;
  if (condition) {
    console.log(`  ok — ${what}`);
    return;
  }
  console.log(`  ПРОВАЛ — ${what}`);
  failed += 1;
}

/** Формы выпуска мимо транспорта. `P` — адрес, до которого сервер не должен получить ничего. */
const RED: ReadonlyArray<[string, string]> = [
  ["голый fetch", "return fetch(P);"],
  ["window.fetch", "return window.fetch(P);"],
  ["globalThis.fetch", "return globalThis.fetch(P);"],
  ["self.fetch", "return self.fetch(P);"],
  ["top.fetch", "return top.fetch(P);"],
  ["parent.fetch", "return parent.fetch(P);"],
  ["frames.fetch", "return frames.fetch(P);"],
  ["document.defaultView.fetch", "return document.defaultView.fetch(P);"],
  ["ссылка в переменной", "const send = fetch; return send(P);"],
  ["привязка", "return globalThis.fetch.bind(globalThis)(P);"],
  ["fetch.call", "return fetch.call(window, P);"],
  ["Reflect.apply", "return Reflect.apply(fetch, window, [P]);"],
  ["ключ строкой", 'return window["fetch"](P);'],
  ["вычисленный ключ", 'return window[["fe", "tch"].join("")](P);'],
  ["отражение", 'return Reflect.get(window, "fetch")(P);'],
  ["разбор", "const { fetch: f } = window; return f(P);"],
  ["объект запроса", 'return fetch(new Request(P, { method: "POST", body: "{}" }));'],
  [
    "пустой фрейм: contentWindow",
    'const fr = document.createElement("iframe"); document.body.append(fr); return fr.contentWindow.fetch(P);',
  ],
  [
    "пустой фрейм: frames[0] (мимо contentWindow)",
    'const fr = document.createElement("iframe"); document.body.append(fr); return window[0].fetch(P);',
  ],
  [
    "пустой фрейм: contentDocument.defaultView",
    'const fr = document.createElement("iframe"); document.body.append(fr); return fr.contentDocument.defaultView.fetch(P);',
  ],
  [
    "фрейм srcdoc: frames[0]",
    'const fr = document.createElement("iframe"); fr.srcdoc = "<p>x</p>"; const loaded = new Promise((r) => (fr.onload = r)); document.body.append(fr); await loaded; return window.frames[0].fetch(P);',
  ],
  [
    "фрейм своего происхождения: frames[0]",
    'const fr = document.createElement("iframe"); fr.src = "/frame.html"; const loaded = new Promise((r) => (fr.onload = r)); document.body.append(fr); await loaded; return window[0].fetch(P);',
  ],
  [
    "псевдоним окна фрейма",
    'const fr = document.createElement("iframe"); document.body.append(fr); const w = fr.contentWindow; const alias = w; return alias.fetch(P);',
  ],
  [
    "окно объекта",
    'const o = document.createElement("object"); o.type = "text/html"; o.data = "/frame.html"; const loaded = new Promise((r) => (o.onload = r)); document.body.append(o); await loaded; return o.contentWindow.fetch(P);',
  ],
  ["window.open() пустого окна", 'const w = window.open("about:blank"); return w.fetch(P);'],
  ["голый open()", 'return open("about:blank").fetch(P);'],
  [
    "window.open() своего происхождения",
    'const w = window.open("/frame.html"); await new Promise((r) => w.addEventListener("load", r)); return w.fetch(P);',
  ],
  ["UIEvent.view", 'return new UIEvent("focus", { view: window }).view.fetch(P);'],
  ["MessageEvent.source", 'return new MessageEvent("message", { source: window }).source.fetch(P);'],
  [
    "MessageEvent.source, присланный фреймом",
    'const fr = document.createElement("iframe"); fr.srcdoc = "<script>parent.postMessage(1, \\"*\\")<\\/script>"; const got = new Promise((r) => window.addEventListener("message", (e) => r(e.source), { once: true })); document.body.append(fr); const src = await got; return src.fetch(P);',
  ],
  [
    "вычисленный ключ на UIEvent.view (N3)",
    'const v = new UIEvent("focus", { view: window }).view; return v[["fe", "tch"].join("")](P);',
  ],
  ["проглоченный отказ", "await fetch(P).catch(() => undefined); return;"],
];

let server: http.Server;
const reached: string[] = [];

async function start(): Promise<string> {
  server = http.createServer((req, res) => {
    const url = req.url ?? "/";
    if (/^\/(b|t|p)\//.test(url)) {
      reached.push(`${req.method} ${url}`);
      res.writeHead(200, { "content-type": "application/json" });
      res.end("{}");
      return;
    }
    const body =
      url === "/carrier-order.js"
        ? SENDER_JS
        : url === "/frame.html"
          ? FRAME
          : url === "/early.html"
            ? EARLY
            : url === "/sender.html"
              ? WITH_SENDER
              : PAGE;
    res.writeHead(200, { "content-type": url.endsWith(".js") ? "text/javascript" : "text/html; charset=utf-8" });
    res.end(body);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/** Исполнить форму в странице; возвращает текст отказа либо «исполнилось». */
async function run(page: Page, code: string, at: string): Promise<string> {
  return page.evaluate(
    async ({ code, at }) => {
      try {
        const body = new Function("P", `return (async () => { ${code} })();`) as (p: string) => Promise<unknown>;
        await body(at);
        return "исполнилось";
      } catch (e) {
        return String((e as Error)?.message ?? e);
      }
    },
    { code, at },
  );
}

/** Единица отчёта прогонщика: заголовок, исход и тексты отказов. */
type CanaryUnit = { title: string; status: string; errors: string };

/** Все единицы отчёта json прогонщика — обходом вложенных наборов. */
function canaryUnits(suites: unknown): CanaryUnit[] {
  type Spec = {
    title: string;
    tests: Array<{ results: Array<{ status: string; errors?: Array<{ message?: string }> }> }>;
  };
  type Suite = { specs?: Spec[]; suites?: Suite[] };
  const out: CanaryUnit[] = [];
  const walk = (list: Suite[] | undefined): void => {
    for (const suite of list ?? []) {
      for (const spec of suite.specs ?? []) {
        const results = spec.tests.flatMap((t) => t.results);
        out.push({
          title: spec.title,
          status: results.map((r) => r.status).join(",") || "не исполнена",
          errors: results.flatMap((r) => (r.errors ?? []).map((e) => e.message ?? "")).join("\n"),
        });
      }
      walk(suite.suites);
    }
  };
  walk(suites as Suite[] | undefined);
  return out;
}

/**
 * Исполнить канарейку фикстуры набора отдельным процессом прогонщика против
 * сервера петли `origin`. Процесс, не оставивший отчёта, — «не выполнилось».
 */
async function runFixtureCanary(
  origin: string,
): Promise<{ code: number | null; units: CanaryUnit[] | null; tail: string }> {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "kacho-fixture-canary-"));
  try {
    const cli = createRequire(import.meta.url).resolve("@playwright/test/cli");
    const config = path.join(here, "issuance-canary", "canary.playwright.config.ts");
    const { code, tail } = await new Promise<{ code: number | null; tail: string }>((resolve, reject) => {
      const child = spawn(process.execPath, [cli, "test", "--config", config], {
        cwd: path.join(here, ".."),
        env: { ...process.env, [CANARY_ORIGIN_ENV]: origin, [CANARY_OUT_ENV]: out },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let text = "";
      const keep = (chunk: Buffer) => {
        text = (text + chunk.toString("utf8")).slice(-4000);
      };
      child.stdout.on("data", keep);
      child.stderr.on("data", keep);
      const timer = setTimeout(() => child.kill("SIGKILL"), 180_000);
      child.on("error", (e) => {
        clearTimeout(timer);
        reject(e);
      });
      child.on("close", (c) => {
        clearTimeout(timer);
        resolve({ code: c, tail: text });
      });
    });
    const reportFile = path.join(out, "report.json");
    const units = fs.existsSync(reportFile)
      ? canaryUnits((JSON.parse(fs.readFileSync(reportFile, "utf8")) as { suites?: unknown }).suites)
      : null;
    return { code, units, tail };
  } finally {
    fs.rmSync(out, { recursive: true, force: true });
  }
}

async function main(): Promise<void> {
  const origin = await start();
  const browser: Browser = await chromium.launch(
    process.env.KACHO_CHROMIUM ? { executablePath: process.env.KACHO_CHROMIUM } : {},
  );
  guardBrowser(browser);
  try {
    const context = await browser.newContext({ baseURL: origin });

    console.log(`ИНЪЕКЦИЯ — формы выпуска мимо упорядочивающего транспорта: ${RED.length}`);
    let n = 0;
    for (const [name, code] of RED) {
      n += 1;
      const at = `/b/${n}`;
      const page = await context.newPage();
      await page.goto("/");
      const outcome = await run(page, code, at);
      const breaches = await takeBreaches(browser);
      const hit = reached.filter((r) => r.endsWith(` ${at}`));
      const named = breaches.some((b) => b.includes(at) && b.includes("выпуск мимо упорядочивающего транспорта"));
      check(
        hit.length === 0 && named,
        `${name}: до сервера дошло ${hit.length}, находка ${named ? "названа" : "НЕ названа"} · ${outcome.slice(0, 80)}`,
      );
      for (const p of context.pages()) await p.close();
    }

    console.log("ИНЪЕКЦИЯ — сценарий страницы при разборе документа");
    {
      const page = await context.newPage();
      await page.goto("/early.html");
      const breaches = await takeBreaches(browser);
      check(
        !reached.some((r) => r.endsWith(" /b/early")) && breaches.some((b) => b.includes("/b/early")),
        "обращение сценария до первого кадра страницы: страж стоял раньше него",
      );
      await page.close();
    }

    console.log("БЛИЗНЕЦЫ — настоящий упорядочивающий транспорт и транспорт пробы");
    {
      const page = await context.newPage();
      await page.goto("/sender.html");
      await page.waitForFunction(() => typeof (window as unknown as { __ordered?: unknown }).__ordered === "object");
      const outcome = await page.evaluate(async () => {
        const t = (
          window as unknown as {
            __ordered: { fetch(u: string, i?: RequestInit, o?: { setsCarrier?: boolean }): Promise<Response> };
          }
        ).__ordered;
        try {
          await t.fetch("/t/read");
          await t.fetch("/t/mutation", { method: "POST", body: "{}" });
          await t.fetch("/t/verb", { method: "POST", body: "{}" }, { setsCarrier: true });
          return "исполнилось";
        } catch (e) {
          return String((e as Error)?.message ?? e).slice(0, 120);
        }
      });
      const viaProbe = await page.evaluate(async (key) => {
        const probeFetch = (window as unknown as Record<symbol, typeof fetch>)[Symbol.for(key)];
        await probeFetch("/p/1");
        return "исполнилось";
      }, PROBE_FETCH);
      const breaches = await takeBreaches(browser);
      check(outcome === "исполнилось", `упорядочивающий транспорт исполнился: ${outcome}`);
      check(
        ["GET /t/read", "POST /t/mutation", "POST /t/verb"].every((r) => reached.includes(r)),
        `чтение, мутация и глагол, ставящий носитель, дошли до сервера: ${reached.filter((r) => r.includes("/t/")).join(", ")}`,
      );
      check(viaProbe === "исполнилось" && reached.includes("GET /p/1"), "транспорт пробы дошёл до сервера");
      check(breaches.length === 0, `находок у законного выпуска 0: ${breaches.join(" | ") || "—"}`);
      await page.close();
    }

    console.log("ПРОВОДКА — контекст набора под стражем, контекст мимо набора назван");
    {
      const own = await browser.newContext();
      check(
        (await takeBreaches(browser)).length === 0,
        "контекст, заведённый через browser.newContext набора, под стражем",
      );
      await own.close();
      // Мимо замены экземпляра — методом прототипа: так контекст завела бы оснастка,
      // минующая набор. Разбор пробы обязан это назвать, а не промолчать.
      const proto = Object.getPrototypeOf(browser) as { newContext(this: Browser): ReturnType<Browser["newContext"]> };
      const bare = await proto.newContext.call(browser);
      const breaches = await takeBreaches(browser);
      check(
        breaches.some((b) => b.includes("контекст браузера без стража")),
        `контекст мимо набора назван: ${breaches.join(" | ") || "—"}`,
      );
      await bare.close();
    }

    console.log("ФИКСТУРА НАБОРА — проглоченный обход роняет пробу, взявшую test у specs/fixtures.ts");
    {
      const { code, units, tail } = await runFixtureCanary(origin);
      if (units === null) {
        check(false, `процесс канарейки не оставил отчёта (код ${code}) — «не выполнилось»:\n${tail}`);
      } else {
        const swallowed = units.find((u) => u.title === FIXTURE_CANARY.swallowed.title);
        const twin = units.find((u) => u.title === FIXTURE_CANARY.twin.title);
        check(
          units.length === 2 && swallowed !== undefined && twin !== undefined,
          `единиц канарейки исполнено ${units.length}: ${units.map((u) => `«${u.title}» ${u.status}`).join(", ") || "—"}`,
        );
        check(
          swallowed?.status === "failed" &&
            swallowed.errors.includes(FIXTURE_REFUSAL) &&
            swallowed.errors.includes(FIXTURE_CANARY.swallowed.path),
          `проглоченный обход ${FIXTURE_CANARY.swallowed.path} — проба ${swallowed?.status ?? "не исполнена"}, ` +
            `отказ фикстуры ${swallowed?.errors.includes(FIXTURE_REFUSAL) ? "назван" : "НЕ назван"} ` +
            "(разбор issuanceLedger в specs/fixtures.ts)",
        );
        check(
          twin?.status === "passed",
          `близнец упорядочивающим транспортом — проба ${twin?.status ?? "не исполнена"}${twin?.errors ? ` · ${twin.errors.slice(0, 200)}` : ""}`,
        );
        check(code === 1, `процесс канарейки вышел с кодом ${code}, ожидался 1 — ровно одна проба красная`);
      }
      check(
        !reached.some((r) => r.endsWith(` ${FIXTURE_CANARY.swallowed.path}`)) &&
          reached.includes(`GET ${FIXTURE_CANARY.twin.path}`),
        `до сервера дошёл близнец и не дошёл обход: ${reached.filter((r) => r.includes("fixture-canary")).join(", ") || "—"}`,
      );
    }
    await context.close();
  } finally {
    await browser.close();
    await new Promise<void>((r) => server.close(() => r()));
  }

  const leaked = reached.filter((r) => r.includes(" /b/"));
  check(leaked.length === 0, `из форм мимо транспорта до сервера не дошло ни одной: ${leaked.join(", ") || "0"}`);
  console.log(`проверок ${checked} · провалено ${failed}`);
  if (failed > 0) process.exit(1);
}

await main();
