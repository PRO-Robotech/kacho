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
  // Приёмка F6b, Р3: отказ края неподтверждённой сессии — значение службы,
  // побайтово: `403`, без вызова `WWW-Authenticate` и без `metadata`.
  addressNotVerified: {
    who: "край: адрес почты не подтверждён (F6b Р3)",
    status: 403,
    body: { code: 7, message: "email address is not verified", details: info("EMAIL_NOT_VERIFIED") },
  },
  // Отказ края по каталогу прав — близнец F6b-26: тот же статус и код, другая причина.
  catalogDenied: {
    who: "край: отказ по каталогу прав",
    status: 403,
    body: {
      code: 7,
      message: "permission denied",
      details: [
        {
          "@type": "type.googleapis.com/google.rpc.ErrorInfo",
          reason: "AUTHZ_DENIED",
          domain: "kaname.cloud.iam.v1",
          metadata: { deny_reasons: "no path" },
        },
      ],
    },
  },
  addressAlreadyVerified: {
    who: "служба: адрес уже подтверждён (F6b Р9)",
    status: 400,
    body: { code: 9, message: "email address is already verified", details: info("EMAIL_ALREADY_VERIFIED") },
  },
  inviteNotValid: {
    who: "служба: приглашение негодно (F6b Р9, F6b-44)",
    status: 400,
    body: {
      code: 9,
      message: "invite is no longer valid; ask an account administrator to invite again",
      details: info("INVITE_NOT_VALID"),
    },
  },
};

function actionOf(p: Producer, surface: RefusalSurface): RefusalAction {
  const h = Object.fromEntries(Object.entries(p.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]));
  const res = { status: p.status, headers: { get: (n: string) => h[n.toLowerCase()] ?? null } } as unknown as Response;
  const refusal = refusalOf(res, typeof p.body === "string" ? p.body : JSON.stringify(p.body));
  return refusalActionOf({ ...refusal, status: refusal.status, challenge: refusal.challenge }, surface);
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
    addressNotVerified: "show",
    catalogDenied: "show",
    addressAlreadyVerified: "address-confirmed",
    inviteNotValid: "show",
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
    addressNotVerified: "confirm-address",
    catalogDenied: "show",
    addressAlreadyVerified: "show",
    inviteNotValid: "show",
  };
  for (const [name, expected] of Object.entries(PLATFORM)) {
    it(`C2 · запрос платформы · ${PRODUCERS[name].who} → ${expected}`, () => {
      expect(actionOf(PRODUCERS[name], "platform")).toBe(expected);
    });
  }

  it("C2 · один статус, разные признаки — разные действия: 403/7 и 401/16 судит не статус", () => {
    expect(new Set([actionOf(PRODUCERS.formToken, "ceremony"), actionOf(PRODUCERS.notFresh, "ceremony")]).size).toBe(2);
    expect(new Set([actionOf(PRODUCERS.authFailed, "platform"), actionOf(PRODUCERS.floor, "platform")]).size).toBe(2);
  });

  it("Р10 · «сессия кончилась» на платформе — «войдите»: повтора с текущим носителем нет", () => {
    // Повтор (условие C18 редакции 6) невыполним: ответ края на прежний
    // носитель гасит печенье, повторять нечем. Перевыпуск упорядочивает
    // транспорт вкладки (`carrier-order.ts`), и такого ответа вкладке не
    // приходит вовсе.
    expect(actionOf(PRODUCERS.ended, "platform")).toBe("sign-in");
    expect(actionOf(PRODUCERS.ended, "ceremony")).toBe("show");
  });
});

describe("F6b · действие на отказ адреса — только по точному значению причины", () => {
  it("F6b-25 · отказ края EMAIL_NOT_VERIFIED на платформе ведёт на экран подтверждения", () => {
    expect(actionOf(PRODUCERS.addressNotVerified, "platform")).toBe("confirm-address");
  });

  it("F6b-26 · близнец: отказ по каталогу прав того же статуса и кода никуда не уводит", () => {
    expect(actionOf(PRODUCERS.catalogDenied, "platform")).toBe("show");
  });

  it("F6b-25 · значение причины сравнивается точно: регистр, пробел и приставка — «показать»", () => {
    for (const reason of ["email_not_verified", "EMAIL_NOT_VERIFIED ", "EMAIL_NOT_VERIFIED_X", "X_EMAIL_NOT_VERIFIED"]) {
      expect([reason, refusalActionOf({ status: 403, reason, challenge: null }, "platform")]).toEqual([reason, "show"]);
    }
    expect(refusalActionOf({ status: 403, reason: "EMAIL_NOT_VERIFIED", challenge: null }, "platform")).toBe(
      "confirm-address",
    );
  });

  it("F6b-42 · «адрес уже подтверждён» уводит с экрана подтверждения; на платформе — «показать»", () => {
    expect(actionOf(PRODUCERS.addressAlreadyVerified, "ceremony")).toBe("address-confirmed");
    expect(actionOf(PRODUCERS.addressAlreadyVerified, "platform")).toBe("show");
  });

  it("F6b-44 · негодное приглашение и неизвестная причина того же статуса — «показать»", () => {
    expect(actionOf(PRODUCERS.inviteNotValid, "ceremony")).toBe("show");
    expect(refusalActionOf({ status: 400, reason: "SOMETHING_NEW", challenge: null }, "ceremony")).toBe("show");
  });
});
