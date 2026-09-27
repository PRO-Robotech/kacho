// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { requestBody, requestUrl } from "./fetch-capture";

/**
 * Дублёр полосы формы для проб экранов церемоний — сеть, и только сеть.
 *
 * Отвечает ТЕМИ ЖЕ телами, что полоса нашей службы (`google.rpc.Status` на
 * отказ, `{user, session}` на успех входа), и не снисходительнее её: признак
 * формы выдаётся своим видом и запись вызова хранит тело, которое экран
 * действительно отправил. Правил о содержимом полей у дублёра нет — как их нет
 * и у экрана: отказ, который нужен пробе, она объявляет сама.
 */

export interface LaneCall {
  method: string;
  path: string;
  query: string;
  body: Record<string, unknown> | null;
}

export interface LaneAnswer {
  status: number;
  body?: unknown;
  headers?: Record<string, string>;
}

type Handler = (call: LaneCall, nth: number) => LaneAnswer;

/** Отказ в форме полосы. */
export function refusal(status: number, code: number, message: string, reason?: string): LaneAnswer {
  return {
    status,
    body: {
      code,
      message,
      details: reason
        ? [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason, domain: "iam.kaname.cloud" }]
        : [],
    },
  };
}

export const SESSION = { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "1", emailVerified: false };
export const SIGNED_IN: LaneAnswer = {
  status: 200,
  body: { user: { id: "usr-1", email: "a@kacho.local", displayName: "a" }, session: SESSION },
};

/**
 * Поставить дублёр. `routes` — «МЕТОД путь» → ответ; признак формы и «кто я»
 * отвечают по умолчанию (признак — `tok-<вид>-<номер выдачи>`, сессии нет).
 */
export function installLane(routes: Record<string, Handler | LaneAnswer>) {
  const calls: LaneCall[] = [];
  const seen = new Map<string, number>();
  const original = globalThis.fetch;
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(requestUrl(input), "http://console.test");
    const method = (init?.method ?? "GET").toUpperCase();
    const call: LaneCall = { method, path: url.pathname, query: url.search, body: requestBody(init?.body) };
    calls.push(call);
    const key = `${method} ${url.pathname}`;
    const nth = (seen.get(key) ?? 0) + 1;
    seen.set(key, nth);
    const route = routes[key];
    let answer: LaneAnswer;
    if (route) answer = typeof route === "function" ? route(call, nth) : route;
    else if (key === "GET /iam/v1/auth/csrf") {
      // Номер выдачи — СВОЙ у каждого вида: признаки разных форм независимы.
      const kind = url.searchParams.get("form") ?? "";
      const issued = (seen.get(`csrf ${kind}`) ?? 0) + 1;
      seen.set(`csrf ${kind}`, issued);
      answer = { status: 200, body: { csrfToken: `tok-${kind}-${issued}` } };
    } else if (key === "GET /iam/v1/auth/me") answer = { status: 200, body: { user: null } };
    else answer = refusal(404, 5, `дублёр: ${key} не объявлен пробой`);
    const headers = Object.fromEntries(Object.entries(answer.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]));
    return Promise.resolve({
      ok: answer.status >= 200 && answer.status < 300,
      status: answer.status,
      statusText: "",
      headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
      text: () => Promise.resolve(answer.body === undefined ? "" : JSON.stringify(answer.body)),
    } as unknown as Response);
  });
  return {
    calls,
    /** Вызовы по методу и пути. */
    of: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path),
    restore: () => {
      globalThis.fetch = original;
    },
  };
}
