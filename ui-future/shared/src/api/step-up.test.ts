// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Отказ «поднимите уровень» обязан ДОХОДИТЬ до окна подтверждения (#1213).
//
// ПРЕДМЕТ. Окно повторного подтверждения регистрировало свой обработчик в
// контексте личности, а ЧИТАТЕЛЯ у этой записи не было ни одного во всём дереве
// консоли: значение писали и не читали. То есть окно не открывалось НИКОГДА, и
// уровень из консоли поднять было нечем — сколько бы способов ни включали
// настройки службы личности.
//
// Здесь утверждается пара, и без второй половины первая ничего не стоит:
//   отказ ПО ПОЛУ ведёт к подтверждению и повтору запроса;
//   отказ БЕЗ пола (обычная неаутентифицированность) — НЕ ведёт.
// Иначе всякий отказ открывал бы окно, и оно превратилось бы в шум.

import { jest } from "@jest/globals";
import { api, ApiError } from "./client";
import { acrFromChallenge, challengeOf, isStepUpDenial, setStepUpRequester } from "./step-up";

const CHALLENGE =
  'Bearer error="insufficient_user_authentication", ' +
  'error_description="Required ACR 2 for this resource; presented ACR 1", acr_values="2"';

interface FakeResponse {
  ok: boolean;
  status: number;
  statusText: string;
  headers: { get: (n: string) => string | null };
  text: () => Promise<string>;
}

function response(status: number, body: string, www?: string): FakeResponse {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: String(status),
    headers: { get: (n: string) => (n.toLowerCase() === "www-authenticate" ? (www ?? null) : null) },
    text: () => Promise.resolve(body),
  };
}

describe("чтение вызова повышения из ответа", () => {
  it("ответ без заголовков читается как «повышения не просили», а не как согласие", () => {
    // Направление снисходительности одно и оно безопасное: подменённый ответ,
    // не отдающий заголовков, обязан давать ИСХОДНЫЙ отказ, а не повтор.
    expect(challengeOf({})).toBeNull();
    expect(challengeOf({ headers: {} })).toBeNull();
    expect(challengeOf({ headers: { get: () => CHALLENGE } })).toBe(CHALLENGE);
  });
});

describe("разбор вызова повышения", () => {
  it("узнаёт отказ по полу и достаёт затребованный уровень", () => {
    expect(isStepUpDenial(401, CHALLENGE)).toBe(true);
    expect(acrFromChallenge(CHALLENGE)).toBe("2");
  });

  it("обычная неаутентифицированность отказом по полу НЕ является", () => {
    // Положительный контроль наоборот: без него предикат зеленел бы на всём
    // подряд и окно открывалось бы на каждом отказе.
    expect(isStepUpDenial(401, 'Bearer realm="kacho", error="invalid_token"')).toBe(false);
    expect(isStepUpDenial(401, null)).toBe(false);
    expect(isStepUpDenial(403, CHALLENGE)).toBe(false);
    expect(acrFromChallenge('Bearer error="invalid_token"')).toBeUndefined();
  });
});

describe("клиент доводит отказ по полу до окна подтверждения", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    setStepUpRequester(null);
  });

  it("после подтверждения ПОВТОРЯЕТ запрос — ровно один раз", async () => {
    const calls: string[] = [];
    globalThis.fetch = jest.fn((url: unknown) => {
      calls.push(String(url));
      return Promise.resolve(
        calls.length === 1
          ? response(401, '{"code":16,"message":"step-up required"}', CHALLENGE)
          : response(200, JSON.stringify({ id: "grp-1" })),
      );
    }) as unknown as typeof fetch;

    const asked: Array<string | undefined> = [];
    setStepUpRequester((acr) => {
      asked.push(acr);
      return Promise.resolve();
    });

    await expect(api.get("/iam/v1/groups/grp-1")).resolves.toMatchObject({ id: "grp-1" });
    expect(asked).toEqual(["2"]);
    expect(calls).toHaveLength(2);
  });

  it("некому подтвердить — отказ отдаётся как есть, повтора НЕТ (fail-closed)", async () => {
    const calls: string[] = [];
    globalThis.fetch = jest.fn((url: unknown) => {
      calls.push(String(url));
      return Promise.resolve(response(401, '{"code":16,"message":"step-up required"}', CHALLENGE));
    }) as unknown as typeof fetch;

    await expect(api.get("/iam/v1/groups/grp-1")).rejects.toBeInstanceOf(ApiError);
    expect(calls).toHaveLength(1);
  });

  it("человек отменил подтверждение — отказ отдаётся как есть, повтора НЕТ", async () => {
    const calls: string[] = [];
    globalThis.fetch = jest.fn((url: unknown) => {
      calls.push(String(url));
      return Promise.resolve(response(401, '{"code":16,"message":"step-up required"}', CHALLENGE));
    }) as unknown as typeof fetch;
    setStepUpRequester(() => Promise.reject(new Error("отменено")));

    await expect(api.get("/iam/v1/groups/grp-1")).rejects.toBeInstanceOf(ApiError);
    expect(calls).toHaveLength(1);
  });

  it("повтор, снова отвергнутый по полу, окна ВТОРОЙ раз не открывает", async () => {
    // Иначе отказ, который подтверждением не лечится, дал бы бесконечную петлю
    // окон — состояние хуже исходного отказа.
    const calls: string[] = [];
    globalThis.fetch = jest.fn((url: unknown) => {
      calls.push(String(url));
      return Promise.resolve(response(401, '{"code":16,"message":"step-up required"}', CHALLENGE));
    }) as unknown as typeof fetch;
    let asked = 0;
    setStepUpRequester(() => {
      asked++;
      return Promise.resolve();
    });

    await expect(api.get("/iam/v1/groups/grp-1")).rejects.toBeInstanceOf(ApiError);
    expect(asked).toBe(1);
    expect(calls).toHaveLength(2);
  });

  it("отказ БЕЗ вызова повышения окна не открывает вовсе", async () => {
    globalThis.fetch = jest.fn(() =>
      Promise.resolve(response(401, '{"code":16,"message":"unauthenticated"}')),
    ) as unknown as typeof fetch;
    let asked = 0;
    setStepUpRequester(() => {
      asked++;
      return Promise.resolve();
    });

    await expect(api.get("/iam/v1/groups/grp-1")).rejects.toBeInstanceOf(ApiError);
    expect(asked).toBe(0);
  });
});
