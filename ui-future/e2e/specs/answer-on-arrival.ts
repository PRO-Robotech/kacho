// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { CDPSession, Page, Request, Response } from "@playwright/test";
import { LANE } from "./ceremony-seed";

/**
 * Ответ глагола полосы — с телом, снятым ДО того, как его получила страница.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ
 *
 * Вход, регистрация, выход и повышение отвечают, и экран сразу уходит
 * документом (`window.location.replace`). Браузер держит тело ответа, пока жив
 * документ, который его получил, и уход документом его снимает — не позже, а
 * В МОМЕНТ ухода: `Response.body()` падает `Protocol error
 * (Network.getResponseBody): No resource with given identifier found` и когда
 * его зовут сразу по прибытии ответа. Замер: на посадке `own` @9038186d0d5 так
 * упали одиннадцать проб F8, и одиннадцать же — при чтении по прибытии (прогон
 * `fe12bcfce72`); вне стенда страница «fetch → text → location.replace» теряет
 * тело восемь раз из восьми, а при уходе через 300 мс и без ухода — ни разу.
 * Раннее чтение при этом гонку ВЕДЁТ, а не выигрывает: оно успевает, только
 * если экран уходит позже, чем дошло чтение. Так его выиграла запись трассы на
 * прогоне 36201294380 — разбор в `answer-on-arrival.spec.ts`. Исход зависит от
 * того, насколько быстр экран, поэтому опереться на него нельзя.
 *
 * Поэтому тело снимается на стадии ОТВЕТА (`Fetch.requestPaused`,
 * `requestStage: "Response"`): браузер ответ уже получил, страница — ещё нет;
 * тело читается, ответ отпускается НЕИЗМЕННЫМ, и дальше его получает страница.
 * Запрос делает сам браузер — происхождение, печенья и признаки запроса те же,
 * что без пробы; печенья ответа ставятся как обычно. Ответ, подставленный
 * `page.route(…).fulfill`, проходит ту же стадию и снимается тем же путём.
 *
 * Перехват ставится ДО отправки (`captureAnswers`), и `lanePostAnswer` без него
 * падает с названной причиной: перехват, поставленный вдогонку за нажатием,
 * пропускал бы ответ молча — а это ровно тот исход, от которого помощник
 * заведён.
 */

/** Пути глаголов полосы, чьи ответы снимаются: все, кроме выдачи признака формы. */
export const LANE_VERBS: readonly string[] = Object.values(LANE).filter((path) => path !== LANE.csrf);

/** Ответ глагола: статус, заголовки и запрос — браузера; тело — снятое до страницы. */
export interface LaneAnswer {
  status(): number;
  url(): string;
  headers(): Record<string, string>;
  request(): Request;
  text(): Promise<string>;
  json(): Promise<unknown>;
}

interface Captured {
  method: string;
  url: string;
  status: number | null;
  body: string | null;
  failure: string | null;
  taken: boolean;
}

const CAPTURES = new WeakMap<Page, Captured[]>();

/** Поставить перехват ответов на путях `paths` страницы. Повторный вызов ничего не меняет. */
export async function captureAnswers(page: Page, paths: readonly string[]): Promise<void> {
  if (CAPTURES.has(page)) return;
  if (paths.length === 0) throw new Error("captureAnswers: перечень путей пуст — перехватывать нечего");
  const captured: Captured[] = [];
  const cdp = await page.context().newCDPSession(page);
  cdp.on("Fetch.requestPaused", (event) => {
    void release(cdp, captured, event);
  });
  await cdp.send("Fetch.enable", {
    patterns: paths.map((path) => ({ urlPattern: `*${path}`, requestStage: "Response" as const })),
  });
  CAPTURES.set(page, captured);
}

async function release(
  cdp: CDPSession,
  captured: Captured[],
  event: {
    requestId: string;
    request: { url: string; method: string };
    responseStatusCode?: number;
    responseErrorReason?: string;
  },
): Promise<void> {
  const entry: Captured = {
    method: event.request.method,
    url: event.request.url,
    status: event.responseStatusCode ?? null,
    body: null,
    failure: null,
    taken: false,
  };
  if (event.responseErrorReason !== undefined) {
    entry.failure = `ответа нет: ${event.responseErrorReason}`;
  } else {
    try {
      const got = await cdp.send("Fetch.getResponseBody", { requestId: event.requestId });
      entry.body = got.base64Encoded ? Buffer.from(got.body, "base64").toString("utf8") : got.body;
    } catch (e) {
      entry.failure = `тело не снято: ${e instanceof Error ? e.message : String(e)}`;
    }
  }
  // Запись — ДО того, как ответ отпущен: событие ответа страницы приходит после
  // отпускания, и к нему снятое уже лежит.
  captured.push(entry);
  try {
    await cdp.send("Fetch.continueResponse", { requestId: event.requestId });
  } catch {
    // Ответа без тела (ошибка сети) стадия ответа продолжить не даёт — только запрос.
    await cdp.send("Fetch.continueRequest", { requestId: event.requestId }).catch(() => undefined);
  }
}

/** Ответ, совпавший с `matches`, — со снятым телом. */
export async function answerOnArrival(page: Page, matches: (r: Response) => boolean): Promise<LaneAnswer> {
  const captured = CAPTURES.get(page);
  if (!captured) {
    throw new Error(
      "ответы глаголов этой страницы не перехватываются: captureAnswers(page, …) не позван до отправки — " +
        "тело ответа, за которым экран уходит документом, было бы потеряно",
    );
  }
  const res = await page.waitForResponse(matches);
  // Событие ответа страницы может прийти раньше, чем перехват записал снятое;
  // окончание загрузки — нет: загрузка идёт лишь после того, как ответ отпущен,
  // а отпускается он после записи. Ждётся условие, а не время.
  await res.finished();
  const method = res.request().method();
  const entry = captured.find((c) => !c.taken && c.method === method && c.url === res.url());
  if (!entry) {
    throw new Error(
      `ответ ${method} ${new URL(res.url()).pathname} (${res.status()}) не прошёл перехват — ` +
        "путь не объявлен в captureAnswers",
    );
  }
  entry.taken = true;
  const body = (): Promise<string> =>
    entry.body !== null
      ? Promise.resolve(entry.body)
      : Promise.reject(new Error(`ответ ${method} ${new URL(res.url()).pathname}: ${entry.failure ?? "тела нет"}`));
  return {
    status: () => res.status(),
    url: () => res.url(),
    headers: () => res.headers(),
    request: () => res.request(),
    text: body,
    json: async () => JSON.parse(await body()) as unknown,
  };
}

/** Ответ глагола `POST <path>` — со снятым телом. */
export function lanePostAnswer(page: Page, path: string): Promise<LaneAnswer> {
  return answerOnArrival(page, (r) => new URL(r.url()).pathname === path && r.request().method() === "POST");
}
