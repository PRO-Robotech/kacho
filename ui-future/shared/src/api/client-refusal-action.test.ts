// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";

// Уход на экран подтверждения — переход документа; в тестовом окружении его
// исполнить нечем, поэтому уход подменён и считается.
const leave = jest.fn();
jest.unstable_mockModule("@shared/pages/auth/address-confirmation-exit", () => ({
  leaveToAddressConfirmation: leave,
}));

const { EDGE_AUTHN_FAILED, EDGE_CREDENTIAL_STATE_UNKNOWN, responseOf } = await import("@shared/test/edge-answers");
const client = await import("./client");
const { api, ApiError } = client;
const { setStepUpRequester } = await import("./step-up");
const clientOf = () => client;

/** Отказ края неподтверждённой сессии — значение службы (приёмка F6b, Р3). */
const ADDRESS_NOT_VERIFIED_BODY = JSON.stringify({
  code: 7,
  message: "email address is not verified",
  details: [
    { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "EMAIL_NOT_VERIFIED", domain: "iam.kaname.cloud" },
  ],
});
/** Отказ края по каталогу прав — близнец F6b-26. */
const CATALOG_DENIED_BODY = JSON.stringify({
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
});

// Клиент API модулей отвечает на отказ ТЕМ ЖЕ решением, что каркас и экраны
// церемоний (`refusalActionOf`, приёмка F8, условие C2). Статус выбора не
// делает: у `401` края три смысла.

function answered(status: number, body: string, headers: Record<string, string> = {}) {
  const h = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    statusText: String(status),
    headers: { get: (n: string) => h[n.toLowerCase()] ?? null },
    text: () => Promise.resolve(body),
  } as unknown as Response);
}


describe("клиент API модулей: действие на отказ", () => {
  const original = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = original;
    setStepUpRequester(null);
  });

  it("Р10 · отказ края в удостоверении (KA1, Р2) отдаётся как есть: повтора с текущим носителем нет", async () => {
    // Повтор (условие C18 редакции 6) невыполним — ответ края гасит носитель.
    // Перевыпуск упорядочивает транспорт вкладки (`carrier-order.ts`).
    let n = 0;
    globalThis.fetch = () => {
      n += 1;
      return responseOf(EDGE_AUTHN_FAILED);
    };
    const refused = await api.get("/vpc/v1/networks").catch((e: unknown) => e);
    expect(refused).toBeInstanceOf(ApiError);
    expect((refused as InstanceType<typeof ApiError>).status).toBe(401);
    expect(n).toBe(1);
    expect(leave).not.toHaveBeenCalled();
  });

  it("KA1 Р1 · авторитет края не ответил (503) — отказ отдаётся как есть, повтора нет, никуда не уводит", async () => {
    let n = 0;
    globalThis.fetch = () => {
      n += 1;
      return responseOf(EDGE_CREDENTIAL_STATE_UNKNOWN);
    };
    const refused = await api.get("/vpc/v1/networks").catch((e: unknown) => e);
    expect(refused).toBeInstanceOf(ApiError);
    expect((refused as InstanceType<typeof ApiError>).status).toBe(503);
    expect(n).toBe(1);
    expect(leave).not.toHaveBeenCalled();
  });

  it("C2 · свежесть службы на запросе платформы — повышение «свежесть», а не пол", async () => {
    const asked = jest.fn(() => Promise.resolve());
    setStepUpRequester(asked);
    let n = 0;
    globalThis.fetch = () => {
      n += 1;
      return n === 1
        ? answered(
            403,
            JSON.stringify({
              code: 7,
              message: "re-authentication required: present a credential again",
              details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "SESSION_NOT_FRESH" }],
            }),
          )
        : answered(200, "{}");
    };
    await api.post("/iam/v1/something:verb", {});
    expect(asked).toHaveBeenCalledWith({ cause: "freshness" });
    expect(n).toBe(2);
  });
});

describe("F6b-25 · клиент модулей на отказ адреса уводит на экран подтверждения", () => {
  const original = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = original;
    leave.mockClear();
  });

  it("F6b-25 · EMAIL_NOT_VERIFIED края — уход на экран подтверждения, отказ отдаётся", async () => {
    globalThis.fetch = () => answered(403, ADDRESS_NOT_VERIFIED_BODY);
    await expect(clientOf().api.get("/vpc/v1/networks")).rejects.toBeInstanceOf(clientOf().ApiError);
    expect(leave).toHaveBeenCalledTimes(1);
  });

  it("F6b-26 · близнец: отказ по каталогу прав того же статуса и кода никуда не уводит", async () => {
    globalThis.fetch = () => answered(403, CATALOG_DENIED_BODY);
    await expect(clientOf().api.get("/vpc/v1/networks")).rejects.toBeInstanceOf(clientOf().ApiError);
    expect(leave).not.toHaveBeenCalled();
  });
});
