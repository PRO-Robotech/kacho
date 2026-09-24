// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { refusalOf } from "./login-lane";
import { refusalActionOf, type RefusalAction, type RefusalSurface } from "./refusal-action";

// Решение «что делать на отказ» на ТЕЛЕ И ЗАГОЛОВКАХ каждого производителя
// (приёмка F8, условие C2). Производители — служба доступа
// (`kaname` `internal/handler/loginlanehttp/handler.go`, `writeRefusal`: тело
// `{code, message, details}`, `details` всегда массив) и край
// (`gateway/internal/middleware/auth.go`, `writeHTTPUnauthorized`: тело без
// `details` и вызов `Bearer error="invalid_token"`; `authz.go` — вызов пола RFC
// 9470). Действие выбирают машинные признаки; статус — нет.

interface Producer {
  who: string;
  status: number;
  body: unknown;
  headers?: Record<string, string>;
}

const info = (reason: string) => [
  { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason, domain: "iam.kaname.cloud" },
];

const PRODUCERS: Record<string, Producer> = {
  formToken: {
    who: "служба: признак формы чужого вида",
    status: 403,
    body: { code: 7, message: "form token rejected", details: info("FORM_TOKEN_REJECTED") },
  },
  notFresh: {
    who: "служба: сессия не свежа",
    status: 403,
    body: {
      code: 7,
      message: "re-authentication required: present a credential again",
      details: info("SESSION_NOT_FRESH"),
    },
  },
  authFailed: {
    who: "служба: не сошлось",
    status: 401,
    body: { code: 16, message: "authentication failed", details: [] },
  },
  ended: {
    who: "край: сессия кончилась либо служба не ответила (F4d-23)",
    status: 401,
    body: { code: 16, message: "session ended; sign in again" },
    headers: { "WWW-Authenticate": 'Bearer error="invalid_token", error_description="session ended; sign in again"' },
  },
  floor: {
    who: "край: пол уровня",
    status: 401,
    body: { code: 16, message: "insufficient_user_authentication" },
    headers: {
      "WWW-Authenticate":
        'Bearer error="insufficient_user_authentication", error_description="Required ACR 2", acr_values="2"',
    },
  },
  tooMany: {
    who: "служба: потолок темпа",
    status: 429,
    body: { code: 8, message: "too many attempts; try again later", details: info("TOO_MANY_ATTEMPTS") },
    headers: { "Retry-After": "897" },
  },
  unavailable: {
    who: "служба: не выполнено",
    status: 503,
    body: { code: 14, message: "request not performed; try again later", details: [] },
  },
  notEnrolled: {
    who: "служба: второй фактор не заведён",
    status: 400,
    body: { code: 9, message: "second factor is not enrolled", details: info("SECOND_FACTOR_NOT_ENROLLED") },
  },
  unknownReason: {
    who: "служба: причина, которой консоль не знает",
    status: 403,
    body: { code: 7, message: "something new", details: info("SOMETHING_NEW") },
  },
  noBody: { who: "раздача: ответ без тела отказа", status: 502, body: "<html>bad gateway</html>" },
};

function actionOf(p: Producer, surface: RefusalSurface, rotatedSinceIssue = false): RefusalAction {
  const h = Object.fromEntries(Object.entries(p.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]));
  const res = { status: p.status, headers: { get: (n: string) => h[n.toLowerCase()] ?? null } } as unknown as Response;
  const refusal = refusalOf(res, typeof p.body === "string" ? p.body : JSON.stringify(p.body));
  return refusalActionOf(
    { ...refusal, status: refusal.status, challenge: refusal.challenge, rotatedSinceIssue },
    surface,
  );
}

describe("C2 · действие на отказ — по машинным признакам производителя", () => {
  const CEREMONY: Record<keyof typeof PRODUCERS, RefusalAction> = {
    formToken: "fresh-form-token",
    notFresh: "step-up-freshness",
    authFailed: "show",
    ended: "show",
    floor: "step-up-floor",
    tooMany: "show",
    unavailable: "show",
    notEnrolled: "show",
    unknownReason: "show",
    noBody: "show",
  };
  for (const [name, expected] of Object.entries(CEREMONY)) {
    it(`C2 · экран церемонии · ${PRODUCERS[name].who} → ${expected}`, () => {
      expect(actionOf(PRODUCERS[name], "ceremony")).toBe(expected);
    });
  }

  const PLATFORM: Record<keyof typeof PRODUCERS, RefusalAction> = {
    formToken: "show",
    notFresh: "step-up-freshness",
    authFailed: "sign-in",
    ended: "sign-in",
    floor: "step-up-floor",
    tooMany: "show",
    unavailable: "show",
    notEnrolled: "show",
    unknownReason: "show",
    noBody: "show",
  };
  for (const [name, expected] of Object.entries(PLATFORM)) {
    it(`C2 · запрос платформы · ${PRODUCERS[name].who} → ${expected}`, () => {
      expect(actionOf(PRODUCERS[name], "platform")).toBe(expected);
    });
  }

  it("C2 · один статус, разные признаки — разные действия: 403/7 и 401/16 судит не статус", () => {
    expect(new Set([actionOf(PRODUCERS.formToken, "ceremony"), actionOf(PRODUCERS.notFresh, "ceremony")]).size).toBe(2);
    expect(
      new Set([
        actionOf(PRODUCERS.authFailed, "platform"),
        actionOf(PRODUCERS.floor, "platform"),
        actionOf(PRODUCERS.ended, "platform", true),
      ]).size,
    ).toBe(3);
  });

  it("C18 · повтор после перевыпуска — только у `invalid_token` платформы; экран церемонии не повторяет сам", () => {
    expect(actionOf(PRODUCERS.ended, "platform", true)).toBe("replay");
    expect(actionOf(PRODUCERS.ended, "ceremony", true)).toBe("show");
    expect(actionOf(PRODUCERS.authFailed, "platform", true)).toBe("sign-in");
  });
});
