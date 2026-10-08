// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { installLane, refusal, SIGNED_IN, type LaneAnswer, type LaneCall } from "@shared/test/lane-fake";
import { installPowWorker, leadingZeroBitsIndependently, loadPowVectors } from "@shared/test/pow-worker-fake";
import { FormTokenHolder, LaneRefusal, loginLane } from "./login-lane";

// Клиент полосы на ограничителе края: вызов PoW, `503` хранилища, бюджет решателя
// (приёмка NTF-2, Р5, NTF2-58, NTF2-72; Р9 — регистрация с кодом; замысел
// `issue-2917` З10, CX2-30).
//
// Здесь закреплено то, что экраны получают от клиента полосы готовым:
//   • вызов `429 PROOF_OF_WORK_REQUIRED` решается ОДИН раз — один `Worker`, повтор
//     того же тела с заголовком `X-Kacho-Proof: <challenge>:<nonce>`;
//   • второй вызов подряд и `RATE_LIMITED` — отказ дословно, без нового решателя;
//   • `503` звена — отказ дословно, решатель не создаётся, повтора нет;
//   • бюджет `Bs` — таймер главного потока: в `Bs − 1 с` решатель работает, в `Bs`
//     он завершён, запроса с доказательством нет ни тогда, ни после, следующая
//     отправка идёт без доказательства и получает новый вызов;
//   • новая отправка той же формы завершает прежний решатель, его ответ отброшен.
//
// Сеть — дублёр полосы (`lane-fake`), `Worker` — дублёр решателя
// (`pow-worker-fake`): он отвечает НАСТОЯЩИМ решением по `node:crypto`, когда
// велит проба, и, завершённый, не отвечает ни на что — как настоящий.
// Бюджет и срок вызова берутся из общего файла векторов края — того же, что
// судит константу консоли (`lib/pow/pow.test.ts`).

const RECOVERY = "/iam/v1/auth/recovery";
const REGISTER = "/iam/v1/auth/register";
const REGISTER_CONFIRM = "/iam/v1/auth/register/confirm";
const PROOF = "x-kacho-proof";

const vectors = loadPowVectors();
const BUDGET_MS = vectors.solverBudgetSeconds * 1000;
/** Вызовы края — настоящей формы (из файла векторов), различимые между собой. */
const CHALLENGE_1 = vectors.vectors[0].challenge;
const CHALLENGE_2 = vectors.vectors.find((v) => v.challenge !== CHALLENGE_1)!.challenge;

/** Вызов края — тело, как его пишет `anonmail.writeChallenge`. */
function challenge(token: string, bits: number): LaneAnswer {
  return {
    status: 429,
    body: {
      code: 8,
      message: "proof of work required",
      details: [
        {
          "@type": "type.googleapis.com/google.rpc.ErrorInfo",
          reason: "PROOF_OF_WORK_REQUIRED",
          domain: "api-gateway.kacho.cloud",
          metadata: {
            challenge: token,
            difficultyBits: String(bits),
            expiresAt: "2026-10-08T12:05:00Z",
          },
        },
      ],
    },
  };
}

/** `503` звена — форма собственных статусов края, `details` пуст. */
function unavailable(message: string): LaneAnswer {
  return { status: 503, body: { code: 14, message, details: [] } };
}

const RATE_LIMITED: LaneAnswer = {
  ...refusal(429, 8, "too many requests", "RATE_LIMITED"),
  headers: { "Retry-After": "120" },
};

const withProof = (calls: LaneCall[]) => calls.filter((c) => PROOF in c.headers);

/** Дать обещаниям клиента дойти до следующего ожидания. */
async function settle() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

/** Исход обещания, не дожидаясь его: `pending` — ещё не решено. */
function track<T>(p: Promise<T>) {
  const state: {
    kind: "pending" | "fulfilled" | "rejected";
    value?: T;
    error?: unknown;
  } = { kind: "pending" };
  p.then(
    (value) => Object.assign(state, { kind: "fulfilled", value }),
    (error: unknown) => Object.assign(state, { kind: "rejected", error }),
  );
  return state;
}

let lane: ReturnType<typeof installLane> | null = null;
let workers: ReturnType<typeof installPowWorker> | null = null;
afterEach(() => {
  lane?.restore();
  workers?.restore();
  lane = null;
  workers = null;
  jest.useRealTimers();
});

describe("вызов края решается прозрачно и один раз (NTF2-72, З10)", () => {
  it("NTF2-72 · вызов → один решатель → повтор ТОГО ЖЕ тела с X-Kacho-Proof, решатель завершён", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => (nth === 1 ? challenge(CHALLENGE_1, 8) : { status: 200, body: {} }),
    });
    const out = track(
      loginLane.requestRecovery(new FormTokenHolder("recovery"), {
        email: "z@kacho.local",
      }),
    );
    await settle();

    expect(workers.lives).toHaveLength(1);
    expect(workers.lives[0].posted.at(-1)).toMatchObject({
      challenge: CHALLENGE_1,
    });
    workers.lives[0].answer();
    await settle();

    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(2);
    expect(PROOF in posts[0].headers).toBe(false);
    expect(posts[0].body).toEqual({
      email: "z@kacho.local",
      csrfToken: "tok-recovery-1",
    });
    expect(posts[1].body).toEqual(posts[0].body);
    const proof = posts[1].headers[PROOF] ?? "";
    const cut = proof.lastIndexOf(":");
    const [token, nonce] = [proof.slice(0, cut), proof.slice(cut + 1)];
    expect(token).toBe(CHALLENGE_1);
    expect(leadingZeroBitsIndependently(token, nonce)).toBeGreaterThanOrEqual(8);
    expect(out.kind).toBe("fulfilled");
    expect(out.value).toEqual({});
    expect(workers.lives[0].terminated).toBe(true);
  });

  it("З10 · второй вызов подряд — отказ дословно, решатель второй раз не создаётся", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => challenge(nth === 1 ? CHALLENGE_1 : CHALLENGE_2, 8),
    });
    const out = track(
      loginLane.requestRecovery(new FormTokenHolder("recovery"), {
        email: "z@kacho.local",
      }),
    );
    await settle();
    expect(workers.lives).toHaveLength(1);
    workers.lives[0].answer();
    await settle();

    expect(lane.of("POST", RECOVERY)).toHaveLength(2);
    expect(workers.lives).toHaveLength(1);
    expect(out.kind).toBe("rejected");
    expect(out.error).toBeInstanceOf(LaneRefusal);
    const e = out.error as LaneRefusal;
    expect([e.status, e.code, e.message, e.reason]).toEqual([
      429,
      8,
      "proof of work required",
      "PROOF_OF_WORK_REQUIRED",
    ]);
  });

  it("З10 · RATE_LIMITED — отказ дословно со сроком, решателя нет, повтора нет", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({ [`POST ${RECOVERY}`]: RATE_LIMITED });
    const out = track(
      loginLane.requestRecovery(new FormTokenHolder("recovery"), {
        email: "z@kacho.local",
      }),
    );
    await settle();

    expect(lane.of("POST", RECOVERY)).toHaveLength(1);
    expect(workers.lives).toHaveLength(0);
    expect(out.kind).toBe("rejected");
    const e = out.error as LaneRefusal;
    expect([e.status, e.message, e.reason, e.retryAfterSeconds]).toEqual([
      429,
      "too many requests",
      "RATE_LIMITED",
      120,
    ]);
  });

  it("NTF2-58 (а) · 503 ограничителя — отказ дословно, решатель не создан, повтора нет", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: unavailable("request limiter is unavailable"),
    });
    const out = track(
      loginLane.requestRecovery(new FormTokenHolder("recovery"), {
        email: "z@kacho.local",
      }),
    );
    await settle();

    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(1);
    expect(withProof(posts)).toHaveLength(0);
    expect(workers.lives).toHaveLength(0);
    expect(out.kind).toBe("rejected");
    const e = out.error as LaneRefusal;
    expect([e.status, e.code, e.message]).toEqual([503, 14, "request limiter is unavailable"]);
  });

  it("NTF2-58 (г) · текст 503 берётся из ответа: своей копии текста края у клиента нет", async () => {
    // verifies #2917
    const n = 7301;
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: unavailable(`limiter probe ${n}`),
    });
    const out = track(
      loginLane.requestRecovery(new FormTokenHolder("recovery"), {
        email: "z@kacho.local",
      }),
    );
    await settle();

    expect(out.kind).toBe("rejected");
    expect((out.error as LaneRefusal).message).toBe(`limiter probe ${n}`);
    expect(lane.of("POST", RECOVERY)).toHaveLength(1);
    expect(workers.lives).toHaveLength(0);
  });
});

describe("срок жизни решателя — одна отправка и бюджет главного потока (NTF2-58 (б), CX2-30)", () => {
  it("NTF2-58 (б) · в Bs − 1 с решатель работает; в Bs завершён, доказательства нет ни тогда, ни после; новая отправка — новый вызов", async () => {
    // verifies #2917
    expect(BUDGET_MS).toBeLessThan(vectors.challengeTtlSeconds * 1000);
    jest.useFakeTimers({
      doNotFake: ["queueMicrotask", "nextTick", "setImmediate"],
    });
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => challenge(nth === 1 ? CHALLENGE_1 : CHALLENGE_2, 24),
    });
    const holder = new FormTokenHolder("recovery");
    const first = track(loginLane.requestRecovery(holder, { email: "z@kacho.local" }));
    await settle();
    expect(workers.lives).toHaveLength(1);

    await jest.advanceTimersByTimeAsync(BUDGET_MS - 1000);
    expect(workers.lives[0].terminated).toBe(false);
    expect(first.kind).toBe("pending");
    expect(withProof(lane.of("POST", RECOVERY))).toHaveLength(0);

    await jest.advanceTimersByTimeAsync(1000);
    expect(workers.lives[0].terminated).toBe(true);
    expect(first.kind).toBe("rejected");
    const e = first.error as LaneRefusal;
    expect([e.status, e.message, e.reason]).toEqual([429, "proof of work required", "PROOF_OF_WORK_REQUIRED"]);

    // «Ни после»: завершённый решатель не отвечает, и ни таймер, ни поздний ответ
    // не выпускают доказательства.
    workers.lives[0].answer();
    await jest.advanceTimersByTimeAsync(vectors.challengeTtlSeconds * 1000);
    expect(withProof(lane.of("POST", RECOVERY))).toHaveLength(0);
    expect(lane.of("POST", RECOVERY)).toHaveLength(1);

    // Следующая отправка — без доказательства, и получает НОВЫЙ вызов.
    track(loginLane.requestRecovery(holder, { email: "z@kacho.local" }));
    await settle();
    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(2);
    expect(PROOF in posts[1].headers).toBe(false);
    expect(workers.lives).toHaveLength(2);
    expect(workers.lives[1].posted.at(-1)).toMatchObject({
      challenge: CHALLENGE_2,
    });
  });

  it("CX2-30 · новая отправка той же формы завершает прежний решатель; его ответ отброшен", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => (nth === 1 ? challenge(CHALLENGE_1, 8) : { status: 200, body: {} }),
    });
    const holder = new FormTokenHolder("recovery");
    track(loginLane.requestRecovery(holder, { email: "z@kacho.local" }));
    await settle();
    expect(workers.lives).toHaveLength(1);

    // Вторая отправка не ждёт бюджета первой: решатель первой завершён сразу.
    const second = track(loginLane.requestRecovery(holder, { email: "z@kacho.local" }));
    await settle();
    expect(workers.lives[0].terminated).toBe(true);
    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(2);
    expect(PROOF in posts[1].headers).toBe(false);
    expect(second.kind).toBe("fulfilled");

    workers.lives[0].answer();
    await settle();
    expect(withProof(lane.of("POST", RECOVERY))).toHaveLength(0);
  });
});

describe("регистрация «сначала письмо, потом сессия» (Р9, З10)", () => {
  it("NTF2-72 · регистрация тоже решает вызов: повтор с доказательством, ответ 200 {} без сессии", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${REGISTER}`]: (_c, nth) => (nth === 1 ? challenge(CHALLENGE_1, 8) : { status: 200, body: {} }),
    });
    const out = track(
      loginLane.register(new FormTokenHolder("register"), {
        email: "a@kacho.local",
        password: "Kacho-E2E-2026!x",
      }),
    );
    await settle();
    expect(workers.lives).toHaveLength(1);
    workers.lives[0].answer();
    await settle();

    const posts = lane.of("POST", REGISTER);
    expect(posts).toHaveLength(2);
    expect(posts[1].headers[PROOF]?.startsWith(`${CHALLENGE_1}:`)).toBe(true);
    expect(posts[1].body).toEqual(posts[0].body);
    expect(out.kind).toBe("fulfilled");
    expect(out.value).toEqual({});
  });

  it("NTF2-80 · предъявление кода — register/confirm с адресом, кодом, паролем и признаком формы register-confirm", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({ [`POST ${REGISTER_CONFIRM}`]: SIGNED_IN });
    const out = track(
      loginLane.confirmRegistration(new FormTokenHolder("register-confirm"), {
        email: "a@kacho.local",
        code: "123456",
        password: "Kacho-E2E-2026!x",
      }),
    );
    await settle();

    const posts = lane.of("POST", REGISTER_CONFIRM);
    expect(posts).toHaveLength(1);
    expect(posts[0].body).toEqual({
      email: "a@kacho.local",
      code: "123456",
      password: "Kacho-E2E-2026!x",
      csrfToken: "tok-register-confirm-1",
    });
    expect(lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query)).toEqual(["?form=register-confirm"]);
    expect(out.kind).toBe("fulfilled");
    expect(workers.lives).toHaveLength(0);
  });
});
