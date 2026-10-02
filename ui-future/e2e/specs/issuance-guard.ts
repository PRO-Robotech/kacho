// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { Browser, BrowserContext } from "@playwright/test";
import { PROBE_TRANSPORT_KEY, installIssuanceGuard } from "../../shared/src/test/issuance-guard.ts";

/**
 * Страж исполнения мест выпуска В БРАУЗЕРЕ сквозных проб (приёмка F8, Р10, F8-46).
 *
 * Каждый контекст браузера, который заводит набор, — штатный контекст пробы и
 * заведённый ею самой (`browser.newContext`, `browser.newPage`), — получает
 * сценарий, ставящий стража (`shared/src/test/issuance-guard.ts`) в КАЖДЫЙ кадр
 * и каждое окно контекста до первого сценария страницы: фрейм, объект, окно
 * `open()`. Вызов `fetch` окна, выпущенный не упорядочивающим транспортом
 * консоли, до сети не доходит; страж отдаёт находку привязке контекста, и
 * проба падает при своём разборе (`specs/fixtures.ts`), даже если консоль
 * проглотила отказ.
 *
 * Собственные обращения ПРОБЫ (подсаженный экран, `page.evaluate`) консолью не
 * являются и идут транспортом пробы — `window[Symbol.for("kacho.probe.fetch")]`.
 *
 * Способность стража упасть и смолчать в настоящем браузере доказывает
 * `scripts/issuance-guard-selftest.ts` — без стенда.
 */

/** Имя привязки контекста, которой страж отдаёт находку. */
export const BREACH_BINDING = "__kachoIssuanceBreach";

/** Ключ транспорта пробы — для подсаженных экранов и `page.evaluate`. */
export const PROBE_FETCH = PROBE_TRANSPORT_KEY;

/** Сценарий контекста: страж в каждом кадре; находка — привязке и в консоль страницы. */
export function issuanceGuardScript(): string {
  return [
    "(() => {",
    '  "use strict";',
    "  const report = (what) => {",
    `    try { const b = window[${JSON.stringify(BREACH_BINDING)}]; if (typeof b === "function") void b(what); } catch (_) { /* привязки нет — остаётся консоль */ }`,
    '    try { console.error("[F8-46] " + what); } catch (_) { /* консоли нет — остаётся привязка */ }',
    "  };",
    `  (${installIssuanceGuard.toString()})(window, report);`,
    "})();",
  ].join("\n");
}

const guarded = new WeakSet<BrowserContext>();
/** Находки, ещё не отданные пробе: набор идёт одним процессом и одной пробой за раз. */
let pending: string[] = [];

/** Дождаться, пока страницы контекста отдадут уже выпущенные находки. */
async function settle(context: BrowserContext): Promise<void> {
  for (const page of context.pages()) {
    if (page.isClosed()) continue;
    // Круг до страницы и обратно: привязка, вызванная страницей раньше, приходит раньше ответа.
    await page.evaluate(() => undefined).catch(() => undefined);
  }
}

/** Поставить стража на контекст. Повторная постановка ничего не делает. */
export async function guardContext(context: BrowserContext): Promise<void> {
  if (guarded.has(context)) return;
  guarded.add(context);
  await context.exposeBinding(BREACH_BINDING, ({ frame }, what: unknown) => {
    pending.push(`${String(what)} · кадр ${frame.url()}`);
  });
  await context.addInitScript({ content: issuanceGuardScript() });
  // Контекст, закрытый пробой, отдаёт находки ДО закрытия: после него их не прочесть.
  const close = context.close.bind(context);
  context.close = async (options?: { reason?: string }) => {
    await settle(context);
    return close(options);
  };
}

const GUARDED_BROWSER = Symbol("kacho.issuance-guarded-browser");

/**
 * Каждый контекст этого браузера — под стражем: и штатный контекст пробы, и
 * заведённый ею самой. Заменяется `newContext` ЭКЗЕМПЛЯРА — через него идут и
 * штатная фабрика контекста, и `browser.newPage`.
 */
export function guardBrowser(browser: Browser): void {
  const own = browser as Browser & { [GUARDED_BROWSER]?: true };
  if (own[GUARDED_BROWSER]) return;
  own[GUARDED_BROWSER] = true;
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (options) => {
    const context = await newContext(options);
    await guardContext(context);
    return context;
  };
}

/** Забрать находки, пришедшие до начала пробы, — они не её, но и потеряться не вправе. */
export function takeStaleBreaches(): string[] {
  const stale = pending;
  pending = [];
  return stale;
}

/**
 * Находки пробы: сперва страницы всех открытых контекстов отдают уже выпущенное,
 * затем проверяется, что каждый открытый контекст под стражем.
 */
export async function takeBreaches(browser: Browser): Promise<string[]> {
  const out: string[] = [];
  for (const context of browser.contexts()) {
    if (!guarded.has(context))
      out.push("контекст браузера без стража мест выпуска — заведён мимо browser.newContext набора");
    await settle(context);
  }
  out.push(...pending);
  pending = [];
  return out;
}

export function formatBreaches(breaches: readonly string[]): string {
  return (
    `[F8-46] обращений консоли мимо упорядочивающего транспорта: ${breaches.length}. ` +
    "Каждое обращение консоли к сети выпускается через orderedTransport (@shared/api/carrier-order); " +
    'собственное обращение пробы — транспортом пробы window[Symbol.for("kacho.probe.fetch")]:\n' +
    breaches.map((b) => `  • ${b}`).join("\n")
  );
}
