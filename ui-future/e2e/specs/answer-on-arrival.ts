// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { Page, Response } from "@playwright/test";

/**
 * Ответ, чьё тело прочитано ПО ПРИБЫТИИ, — для глаголов, за которыми экран
 * уходит документом.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ
 *
 * Вход, регистрация, выход и повышение отвечают, и экран сразу уходит
 * документом (`window.location.replace`). Браузер держит тело ответа, пока жив
 * документ, который его получил; проба, читающая тело ПОСЛЕ ухода, получает
 * `Protocol error (Network.getResponseBody): No resource with given identifier
 * found` — и падает на чтении своего же доказательства, не дойдя до
 * утверждения. Так на посадке `own` @9038186d0d5 упали одиннадцать проб F8,
 * чей глагол прошёл: сообщение об отказе у них собиралось из тела ответа и
 * читалось всегда, в том числе на успехе.
 *
 * Поэтому тело читается здесь, сразу за приходом ответа, пока документ цел, —
 * а прочитанное держит сам `Response`: повторное `text()`/`json()` отдаёт то же
 * содержимое и в браузер больше не ходит. Утверждения пробы от этого не
 * меняются: она по-прежнему утверждает статус и тело ответа.
 *
 * Тело не прочиталось и здесь — падение называет это прямо, а не выдаёт себя за
 * отказ глагола.
 */
export async function answerOnArrival(page: Page, matches: (r: Response) => boolean): Promise<Response> {
  const res = await page.waitForResponse(matches);
  try {
    await res.body();
  } catch (e) {
    throw new Error(
      `тело ответа ${res.request().method()} ${new URL(res.url()).pathname} (${res.status()}) не прочитано ` +
        `по прибытии: ${e instanceof Error ? e.message : String(e)}`,
    );
  }
  return res;
}

/** Ответ глагола `POST <path>` — с телом, прочитанным по прибытии. */
export function lanePostAnswer(page: Page, path: string): Promise<Response> {
  return answerOnArrival(page, (r) => new URL(r.url()).pathname === path && r.request().method() === "POST");
}
