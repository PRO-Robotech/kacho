// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА ПРОИСХОЖДЕНИЯ СТЕНДА ПО HTTP (#1274): Secure-печенье службы
 * обязано доходить до стенда по http у ВСЕХ ТРЁХ клиентов пробы — посева, пути
 * запроса контекста браузера и самого браузера — и ни у одного не уходить мимо
 * происхождения стенда.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ НАСТОЯЩИЕ КЛИЕНТЫ И НАСТОЯЩИЙ СЕРВЕР
 *
 * Предмет — в ДОСТАВКЕ печенья: посев получал `403 FORM_TOKEN_REJECTED`, потому
 * что печенье формы до службы не доходило. Поэтому сервер петли выдаёт печенье
 * той же формы, что служба (`HttpOnly; Secure; SameSite=Lax`), и отдаёт обратно
 * заголовок `Cookie`, который получил, а спрашивают его настоящий
 * `APIRequestContext` playwright и настоящий браузер. Имя сервера — заведомо
 * нерезолвимое и НЕ localhost: localhost оба клиента считают защищённым, и на нём
 * контроль был бы зелёным даром. Имя отображается на петлю тем же
 * `installHostMapping` и тем же флагом резолвера, что у конфигурации проб.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРОГОНОВ ЧЕТЫРЕ
 *
 *   контроль  — без решения Secure-печенье не доходит: браузер его не принимает,
 *               путь запроса его не отправляет. Это воспроизведение предмета и
 *               ПРЕДПОСЫЛКА решения: покраснел контроль — клиент стал носить
 *               печенье сам, и его половина решения снимается;
 *   предмет   — с решением доходит у всех трёх, атрибуты печенья не переписаны;
 *   сужение   — другой порт того же имени Secure-печенья не получает, чужой путь
 *               печенья не получает, заголовок, названный вызывающим, остаётся
 *               его; стенд по https решения не получает вовсе;
 *   провязка  — решение ЗОВУТ: конфигурация проб, фикстура и посев (разбором
 *               исходника, по узлу вызова). Решение, которое никто не зовёт,
 *               доносит печенье безупречно и не мешает ничему.
 *
 * Гоняется после установки браузера и ДО стенда: секунды, стенд не нужен.
 */

import http from "node:http";
import type { AddressInfo } from "node:net";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { chromium, request, type APIRequestContext, type LaunchOptions } from "@playwright/test";

import { installHostMapping } from "../host-mapping.ts";
import {
  carryStandCookies,
  carryStandCookiesInBrowser,
  plainHttpStandOrigin,
  standCookieHeader,
  standSecureOriginArgs,
} from "../stand-secure-origin.ts";

const E2E = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
/** Имя стенда самопроверки: не localhost и не разрешается без отображения. */
const NAME = "console.kacho.selftest.invalid";
const FORM = "kacho_selftest_form";
const PLAIN = "kacho_selftest_plain";
const SCOPED = "kacho_selftest_scoped";

let checked = 0;
let failed = 0;
function check(condition: boolean, what: string): void {
  checked += 1;
  if (condition) {
    console.log(`  ok — ${what}`);
    return;
  }
  console.log(`  ПРОВАЛ — ${what}`);
  failed += 1;
}

/** Сервер петли: `/issue` выдаёт печенье формы службы, всякий путь отдаёт полученный `Cookie`. */
async function start(): Promise<{ server: http.Server; port: number }> {
  const server = http.createServer((req, res) => {
    if (req.url === "/issue") {
      res.setHeader("Set-Cookie", [
        `${FORM}=f1; Path=/; HttpOnly; Secure; SameSite=Lax`,
        `${PLAIN}=p1; Path=/; SameSite=Lax`,
        `${SCOPED}=s1; Path=/only; HttpOnly; Secure; SameSite=Lax`,
      ]);
    }
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
    res.end(`cookie=${req.headers.cookie ?? ""}`);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  return { server, port: (server.address() as AddressInfo).port };
}

/** Печенье, пришедшее серверу, по именам. */
function arrived(text: string): Map<string, string> {
  const out = new Map<string, string>();
  const raw = text.startsWith("cookie=") ? text.slice("cookie=".length) : "";
  for (const pair of raw.split(";")) {
    const i = pair.indexOf("=");
    if (i > 0) out.set(pair.slice(0, i).trim(), pair.slice(i + 1).trim());
  }
  return out;
}

async function echo(api: APIRequestContext, url: string, headers?: Record<string, string>) {
  return arrived(await (await api.get(url, headers ? { headers } : undefined)).text());
}

function launchOptions(args: string[]): LaunchOptions {
  return {
    ...(process.env.KACHO_CHROMIUM ? { executablePath: process.env.KACHO_CHROMIUM } : {}),
    args,
  };
}

/** Вызовы функции `name` в исходнике — узлами разбора, а не поиском по тексту. */
function callsOf(file: string, name: string): number {
  const src = ts.createSourceFile(file, fs.readFileSync(path.join(E2E, file), "utf-8"), ts.ScriptTarget.Latest, true);
  let n = 0;
  const walk = (node: ts.Node): void => {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === name) n += 1;
    ts.forEachChild(node, walk);
  };
  walk(src);
  return n;
}

async function main(): Promise<void> {
  const stand = await start();
  const other = await start();
  const STAND = `http://${NAME}:${stand.port}`;
  const OTHER = `http://${NAME}:${other.port}`;
  check(installHostMapping(NAME, "127.0.0.1")?.ip === "127.0.0.1", `имя ${NAME} отображено на петлю пути запроса Node`);
  // Обе формы правила, как у конфигурации проб: форма без порта действует не во всяком процессе.
  const resolver =
    `--host-resolver-rules=MAP ${NAME}:${stand.port} 127.0.0.1:${stand.port},` +
    `MAP ${NAME}:${other.port} 127.0.0.1:${other.port},MAP ${NAME} 127.0.0.1`;

  console.log("ПРОГОН 1 — КОНТРОЛЬ: решения нет, Secure-печенье по http не доходит");
  {
    const api = await request.newContext({ baseURL: STAND });
    await api.get("/issue");
    const got = await echo(api, "/echo");
    const stored = (await api.storageState()).cookies.find((c) => c.name === FORM);
    check(stored?.secure === true, "посев: хранилище playwright печенье формы ПРИНЯЛО и держит с Secure");
    check(
      !got.has(FORM) && got.get(PLAIN) === "p1",
      `посев: Secure-печенье НЕ отправлено, обычное отправлено — предмет воспроизведён (пришло: ${[...got.keys()].join(", ") || "—"})`,
    );
    await api.dispose();

    const browser = await chromium.launch(launchOptions([resolver]));
    try {
      const context = await browser.newContext({ baseURL: STAND });
      const page = await context.newPage();
      await page.goto("/issue");
      const seen = arrived((await (await page.goto("/echo"))!.text()));
      check(
        !seen.has(FORM) && !(await context.cookies()).some((c) => c.name === FORM),
        "браузер: Secure-печенье из происхождения по http НЕ принято — предмет воспроизведён",
      );
    } finally {
      await browser.close();
    }
  }

  console.log("ПРОГОН 2 — ПРЕДМЕТ: решение стоит, печенье доходит у всех трёх клиентов");
  {
    const api = await request.newContext({ baseURL: STAND });
    carryStandCookies(api, async () => (await api.storageState()).cookies, STAND);
    await api.get("/issue");
    const got = await echo(api, "/echo");
    check(got.get(FORM) === "f1" && got.get(PLAIN) === "p1", "посев: печенье формы и обычное дошли до стенда");
    const stored = (await api.storageState()).cookies.find((c) => c.name === FORM);
    check(stored?.secure === true && stored?.httpOnly === true, "посев: атрибуты печенья не переписаны — Secure и HttpOnly как выданы");
    check((await echo(api, "/only/echo")).get(SCOPED) === "s1", "посев: печенье своего пути дошло по своему пути");

    console.log("ПРОГОН 3 — СУЖЕНИЕ (посев)");
    check(!(await echo(api, "/echo")).has(SCOPED), "печенье чужого пути не перенесено");
    check(!(await echo(api, `${OTHER}/echo`)).has(FORM), "другой порт того же имени Secure-печенья не получил");
    const named = await echo(api, "/echo", { Cookie: "named=1" });
    check(named.size === 1 && named.get("named") === "1", "заголовок печенья, названный вызывающим, ушёл как назван");
    const wrapped = api.fetch;
    carryStandCookies(api, async () => [], STAND);
    check(api.fetch === wrapped, "повторная постановка не обернула перенос второй раз");
    await api.dispose();

    const tls = await request.newContext({ baseURL: STAND });
    const own = tls.fetch;
    carryStandCookies(tls, async () => [], "https://console.kacho.local");
    check(tls.fetch === own, "стенд по https: перенос не ставится");
    await tls.dispose();

    const browser = await chromium.launch(launchOptions([resolver, ...standSecureOriginArgs(STAND)]));
    try {
      const bare = await browser.newContext({ baseURL: STAND });
      const bp = await bare.newPage();
      await bp.goto("/issue");
      const seen = arrived((await (await bp.goto("/echo"))!.text()));
      check(seen.get(FORM) === "f1", "браузер: печенье формы принято и отправлено происхождению стенда");
      const kept = (await bare.cookies()).find((c) => c.name === FORM);
      check(kept?.secure === true, "браузер: печенье держится с Secure, как выдано");
      check(!arrived((await (await bp.goto(`${OTHER}/echo`))!.text())).has(FORM), "браузер: другой порт того же имени Secure-печенья не получил");
      check(
        !(await echo(bare.request, "/echo")).has(FORM),
        "путь запроса контекста без переноса Secure-печенье НЕ отправил — предпосылка переноса (контроль)",
      );

      carryStandCookiesInBrowser(browser, STAND);
      const context = await browser.newContext({ baseURL: STAND });
      const page = await context.newPage();
      await page.goto("/issue");
      check((await echo(context.request, "/echo")).get(FORM) === "f1", "путь запроса контекста (page.request): печенье формы дошло");
      check(!(await echo(context.request, `${OTHER}/echo`)).has(FORM), "путь запроса контекста: другой порт Secure-печенья не получил");
    } finally {
      await browser.close();
    }
  }

  console.log("СУЖЕНИЕ — решение по адресу стенда");
  check(plainHttpStandOrigin("https://console.kacho.local") === null, "https: происхождение не объявляется");
  check(standSecureOriginArgs("https://console.kacho.local").length === 0, "https: флага браузера нет");
  check(plainHttpStandOrigin(undefined) === null, "адреса нет — решения нет");
  check(
    standSecureOriginArgs("http://console.kacho.local:28080/login").join() ===
      "--unsafely-treat-insecure-origin-as-secure=http://console.kacho.local:28080",
    "http: флаг несёт ровно происхождение — схему, имя и порт, без пути",
  );
  const now = 1_000_000;
  const at = new URL("http://console.kacho.local:28080/iam/v1/auth/register");
  const cookie = (name: string, domain: string, p: string, expires: number) => ({ name, value: "v", domain, path: p, expires });
  check(
    standCookieHeader(
      [
        cookie("session", "console.kacho.local", "/", -1),
        cookie("fresh", ".kacho.local", "/iam", now + 60),
        cookie("stale", "console.kacho.local", "/", now - 1),
        cookie("foreign", "api.kacho.local", "/", -1),
        cookie("elsewhere", "console.kacho.local", "/vpc", -1),
      ],
      at,
      now,
    ) === "session=v; fresh=v",
    "заголовок: печенье сеанса и свежее по домену с точкой — да; истёкшее, чужого имени и чужого пути — нет",
  );

  console.log("ПРОВЯЗКА — решение зовут те, кто обязан");
  const wiring: Array<[string, string]> = [
    ["playwright.config.ts", "standSecureOriginArgs"],
    ["specs/fixtures.ts", "carryStandCookiesInBrowser"],
    ["specs/ceremony-seed.ts", "carryStandCookies"],
  ];
  for (const [file, name] of wiring) {
    const n = callsOf(file, name);
    check(n >= 1, `${file}: вызовов ${name} — ${n}`);
  }

  await new Promise<void>((r) => stand.server.close(() => r()));
  await new Promise<void>((r) => other.server.close(() => r()));
  console.log(`проверок ${checked} · провалено ${failed}`);
  if (failed > 0) process.exit(1);
}

await main();
