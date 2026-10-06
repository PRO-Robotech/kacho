// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type APIResponse, type TestInfo } from "@playwright/test";
import { LANE, SESSION_COOKIE, lastIssued, newSeed, type Cookie, type Seed } from "./ceremony-seed";

/**
 * Помощники сквозных проб полосы нашей сессии ЧЕРЕЗ КРАЙ — одно объявление на
 * все файлы, которые судят ответ края о сессии (`session-lane-edge.spec.ts`,
 * `cloud-admin-lane.spec.ts`). Отказ полосы личности F4d-22 утверждается ОДНОЙ
 * функцией: две копии этой проверки разошлись бы молча.
 */

export interface Answer {
  status: number;
  text: string;
  setCookie: string[];
  /** Вызов повышения края (`WWW-Authenticate`) — пусто, если его нет. */
  challenge: string;
}

export async function answerOf(res: APIResponse): Promise<Answer> {
  return {
    status: res.status(),
    text: await res.text(),
    setCookie: res
      .headersArray()
      .filter((h) => h.name.toLowerCase() === "set-cookie")
      .map((h) => h.value),
    challenge: res.headers()["www-authenticate"] ?? "",
  };
}

/** Печенье носителя у посева — как выдано службой; нет — шаг отказывает с причиной. */
export async function carrierOf(seed: Seed, who: string): Promise<Cookie> {
  const cookie = (await seed.api.storageState()).cookies.find((c) => c.name === SESSION_COOKIE);
  expect(cookie, `${who}: носителя ${SESSION_COOKIE} у контекста нет — шаг «Дано» не собран`).toBeTruthy();
  return cookie!;
}

/** Отказ полосы личности F4d-22: 401, текст отсечки, носитель гасится (`Max-Age=0`). */
export function expectSessionLaneRefusal(a: Answer, who: string) {
  expect(a.status, `${who}: отвергнутая сессия обязана получать 401 полосы личности — ${a.text}`).toBe(401);
  expect(a.text, `${who}: текст отказа — текст отсечки, один на пять причин (Ф1-17)`).toContain(
    "authentication failed",
  );
  expect(
    a.setCookie.some((v) => new RegExp(`^${SESSION_COOKIE}=;`).test(v) && /max-age=0/i.test(v)),
    `${who}: носитель обязан гаситься (F4d-24), Set-Cookie: ${JSON.stringify(a.setCookie)}`,
  ).toBe(true);
}

/** Контекст без носителя либо с названными печеньями — и только с ними. */
export async function bareSeed(testInfo: TestInfo, carrying: readonly Cookie[] = []): Promise<Seed> {
  return newSeed(testInfo, carrying);
}

/** Вход паролем своим контекстом: `200`, носитель у контекста — шаг утверждает свой исход. */
export async function signedIn(testInfo: TestInfo, email: string, password: string, who: string): Promise<Seed> {
  const seed = await bareSeed(testInfo);
  const res = await seed.submit(LANE.login, "login", { email, password });
  expect(
    res.status(),
    `${who}: вход ${email} отвергнут — ${res.status()} ${JSON.stringify(lastIssued(seed, LANE.login).body)}`,
  ).toBe(200);
  await carrierOf(seed, who);
  return seed;
}
