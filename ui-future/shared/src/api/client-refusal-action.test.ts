// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { api, ApiError } from "./client";
import { noteBearerRotated } from "./lane-epochs";
import { setStepUpRequester } from "./step-up";

// Клиент API модулей отвечает на отказ ТЕМ ЖЕ решением, что каркас и экраны
// церемоний (`refusalActionOf`, приёмка F8, условия C2 и C18). Статус выбора не
// делает: у `401` края три смысла, и повтор после перевыпуска носителя этой же
// вкладкой — не повышение и не «войдите».

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

const ENDED = { "WWW-Authenticate": 'Bearer error="invalid_token", error_description="session ended; sign in again"' };
const ENDED_BODY = '{"code":16,"message":"session ended; sign in again"}';

describe("клиент API модулей: действие на отказ", () => {
  const original = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = original;
    setStepUpRequester(null);
  });

  it("C18 · носитель перевыпущен этой вкладкой, пока запрос шёл: ОДИН повтор с текущим носителем", async () => {
    let n = 0;
    globalThis.fetch = (() => {
      n += 1;
      if (n === 1) {
        noteBearerRotated();
        return answered(401, ENDED_BODY, ENDED);
      }
      return answered(200, '{"networks":[]}');
    }) as typeof fetch;
    await expect(api.get("/vpc/v1/networks")).resolves.toEqual({ networks: [] });
    expect(n).toBe(2);
  });

  it("C18 · без перевыпуска в этой вкладке — отказ как есть, повтора нет", async () => {
    let n = 0;
    globalThis.fetch = (() => {
      n += 1;
      return answered(401, ENDED_BODY, ENDED);
    }) as typeof fetch;
    await expect(api.get("/vpc/v1/networks")).rejects.toBeInstanceOf(ApiError);
    expect(n).toBe(1);
  });

  it("C18 · повтор ОДИН: второй такой же отказ отдаётся как есть", async () => {
    let n = 0;
    globalThis.fetch = (() => {
      n += 1;
      noteBearerRotated();
      return answered(401, ENDED_BODY, ENDED);
    }) as typeof fetch;
    await expect(api.get("/vpc/v1/networks")).rejects.toBeInstanceOf(ApiError);
    expect(n).toBe(2);
  });

  it("C2 · свежесть службы на запросе платформы — повышение «свежесть», а не пол", async () => {
    const asked = jest.fn(() => Promise.resolve());
    setStepUpRequester(asked);
    let n = 0;
    globalThis.fetch = (() => {
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
    }) as typeof fetch;
    await api.post("/iam/v1/something:verb", {});
    expect(asked).toHaveBeenCalledWith({ cause: "freshness" });
    expect(n).toBe(2);
  });
});
