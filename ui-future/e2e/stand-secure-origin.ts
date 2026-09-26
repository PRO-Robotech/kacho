// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { APIRequestContext, Browser } from "@playwright/test";

/**
 * ПРОИСХОЖДЕНИЕ СТЕНДА ПО HTTP — ЗАЩИЩЁННОЕ ДЛЯ ВСЕХ ТРЁХ КЛИЕНТОВ ПРОБЫ (#1274).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ: SECURE-ПЕЧЕНЬЕ ПО ОТКРЫТОМУ HTTP НЕ НОСИТ НИКТО
 *
 * Служба выдаёт печенье контекста формы (`kaname_form`) и носитель сессии
 * (`kaname_session`) с атрибутом `Secure` — это production-форма, и проба её не
 * трогает. Стенд конвейера отдаёт консоль по http под именем стенда
 * (`http://console.kacho.local:28080`). Secure-печенье по открытому http к имени,
 * отличному от `localhost`, не носит НИ ОДИН из клиентов пробы:
 *
 *   браузер       — Chromium не ПРИНИМАЕТ Secure из незащищённого происхождения;
 *   `page.request` и
 *   свой контекст — хранилище playwright принимает, но не ОТПРАВЛЯЕТ: протокол не
 *   посева          https и имя не localhost (playwright-core 1.56.1,
 *                   `lib/server/cookieStore.js` `Cookie.matches`,
 *                   `lib/server/network.js` `filterCookies`).
 *
 * Наблюдалось: прогон 36189499133 — исполнено 5 проб из 149, все пять на посеве
 * с `403 FORM_TOKEN_REJECTED`; на стенде той же формы — тем же текстом и
 * регистрация экраном (`register`), от которой зависит весь набор. Пробы писались
 * на стенде `http://localhost:<порт>`, а localhost оба клиента считают
 * защищённым, — поэтому там, где их писали, разрыв не был виден.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * РЕШЕНИЕ ОДНО И СНИМАЕТСЯ С АДРЕСА СТЕНДА
 *
 * Стенд по http — его происхождение объявляется защищённым сразу всем трём
 * клиентам: браузеру флагом Chromium (`standSecureOriginArgs`), путям запроса —
 * переносом печенья стенда явным заголовком (`carryStandCookies`). Стенд по
 * https — не делается НИЧЕГО: клиенты справляются сами, и перенос не ставится.
 *
 * ЭТО НЕ ПОСЛАБЛЕНИЕ ПРОДУКТУ. Атрибуты печенья не переписываются: браузер и
 * посев хранят их такими, какими выдала служба, и служба по-прежнему выдаёт
 * `Secure`. Защищённым объявлено РОВНО ОДНО происхождение — стенда: схема, имя и
 * порт. Другой порт того же имени и любое другое имя Secure-печенья не получают
 * (это доказывает самопроверка), то есть объявлено ровно то, что на боевой
 * посадке даёт TLS перед консолью.
 *
 * ЧЕГО ПЕРЕНОС НЕ ДЕЛАЕТ. Шаг переадресации playwright собирает заголовок
 * печенья заново СВОИМ хранилищем — там Secure к http снова не уходит. Пойдёт
 * обращение пути запроса к стенду через переадресацию — её шаг уйдёт без
 * Secure-печенья, и перенос придётся ставить и туда.
 *
 * СНЯТИЕ. Держит решение самопроверка `scripts/stand-secure-origin-selftest.ts`,
 * и её КОНТРОЛЬ — предпосылка: без флага браузер Secure не принимает, без
 * переноса путь запроса его не отправляет. Покраснел контроль — клиент стал
 * носить печенье сам, и соответствующая половина снимается вместе с ним. Стенд
 * конвейера переехал на https — решение гаснет само: `plainHttpStandOrigin`
 * вернёт `null`, и ни флаг, ни перенос не ставятся.
 */

/** Пометка на обёрнутом контексте и браузере: перенос ставится один раз. */
const CARRYING = Symbol.for("kacho.console.e2e.standCookies");

/**
 * Происхождение стенда, если он отдаётся по открытому http, иначе `null`.
 *
 * Решает СХЕМА адреса, а не имя: `localhost` браузер и так считает защищённым,
 * но хранилище playwright — не всякую его форму (`127.0.0.1` оно защищённым не
 * считает). Объявить защищённым уже защищённое безвредно; решать по имени
 * значило бы вести второй перечень исключений, расходящийся с клиентами молча.
 */
export function plainHttpStandOrigin(base: string | undefined): string | null {
  if (!base) return null;
  const url = new URL(base);
  return url.protocol === "http:" ? url.origin : null;
}

/** Флаги браузера: происхождение стенда по http — защищённое; по https — ничего. */
export function standSecureOriginArgs(base: string | undefined): string[] {
  const origin = plainHttpStandOrigin(base);
  return origin ? [`--unsafely-treat-insecure-origin-as-secure=${origin}`] : [];
}

/** Печенье в том виде, в каком его отдают хранилища playwright. */
export interface StandCookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  /** Секунды эпохи; `-1` — печенье сеанса. */
  expires: number;
}

/** RFC 6265 §5.1.3 — в той же форме, что у хранилища playwright. */
function domainMatches(host: string, domain: string): boolean {
  if (host === domain) return true;
  if (!domain.startsWith(".")) return false;
  return `.${host}`.endsWith(domain);
}

/** RFC 6265 §5.1.4 — в той же форме, что у хранилища playwright. */
function pathMatches(path: string, cookiePath: string): boolean {
  if (path === cookiePath) return true;
  const p = path.endsWith("/") ? path : `${path}/`;
  const c = cookiePath.endsWith("/") ? cookiePath : `${cookiePath}/`;
  return p.startsWith(c);
}

/**
 * Заголовок печенья обращения к стенду: всё печенье, чьи имя, путь и срок
 * подходят адресу, — как у хранилища playwright, с ОДНИМ отличием: атрибут
 * `Secure` удовлетворён, потому что происхождение стенда объявлено защищённым.
 * Подходящего нет — `undefined`, заголовок не ставится.
 */
export function standCookieHeader(
  cookies: readonly StandCookie[],
  url: URL,
  nowSeconds: number,
): string | undefined {
  const carried = cookies.filter(
    (c) =>
      domainMatches(url.hostname, c.domain) &&
      pathMatches(url.pathname, c.path) &&
      (c.expires === -1 || c.expires > nowSeconds),
  );
  return carried.length ? carried.map((c) => `${c.name}=${c.value}`).join("; ") : undefined;
}

function hasCookieHeader(headers: Record<string, string> | undefined): boolean {
  return Object.keys(headers ?? {}).some((k) => k.toLowerCase() === "cookie");
}

type Fetch = APIRequestContext["fetch"];

/**
 * Поставить перенос печенья стенда на контекст запросов — если стенд отдаётся
 * по http. Возвращает тот же контекст.
 *
 * Заменяется `fetch` ЭКЗЕМПЛЯРА: через него идут все глаголы контекста
 * (`get`, `post`, … зовут `this.fetch`). Переносится только в происхождение
 * стенда и только когда вызывающий не назвал печенье сам — заголовок, названный
 * вызывающим, остаётся его. Обращение объектом `Request` уходит как есть.
 *
 * `cookiesOf` — всё печенье хранилища этого контекста, без отбора по адресу:
 * отбор по адресу у playwright и есть тот, что роняет Secure.
 */
export function carryStandCookies(
  api: APIRequestContext,
  cookiesOf: () => Promise<readonly StandCookie[]>,
  standBase: string | undefined,
  resolveBase: string | undefined = standBase,
): APIRequestContext {
  const origin = plainHttpStandOrigin(standBase);
  const own = api as APIRequestContext & { [CARRYING]?: true };
  if (!origin || own[CARRYING]) return api;
  own[CARRYING] = true;
  const issue: Fetch = api.fetch.bind(api);
  const carrying: Fetch = async (urlOrRequest, options = {}) => {
    if (typeof urlOrRequest !== "string" || hasCookieHeader(options.headers)) return issue(urlOrRequest, options);
    const target = new URL(urlOrRequest, resolveBase ?? standBase);
    if (target.origin !== origin) return issue(urlOrRequest, options);
    const cookie = standCookieHeader(await cookiesOf(), target, Date.now() / 1000);
    if (cookie === undefined) return issue(urlOrRequest, options);
    return issue(urlOrRequest, { ...options, headers: { ...options.headers, cookie } });
  };
  api.fetch = carrying;
  return api;
}

/**
 * Перенос на КАЖДЫЙ контекст браузера: и штатный контекст пробы, и заведённый ею
 * самой. Заменяется `newContext` ЭКЗЕМПЛЯРА — через него идут и штатная фабрика
 * контекста, и `browser.newPage` (так же стоит страж мест выпуска,
 * `specs/issuance-guard.ts`). `page.request` — это `request` контекста.
 */
export function carryStandCookiesInBrowser(browser: Browser, standBase: string | undefined): void {
  const own = browser as Browser & { [CARRYING]?: true };
  if (!plainHttpStandOrigin(standBase) || own[CARRYING]) return;
  own[CARRYING] = true;
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (options) => {
    const context = await newContext(options);
    carryStandCookies(context.request, () => context.cookies(), standBase, options?.baseURL ?? standBase);
    return context;
  };
}
