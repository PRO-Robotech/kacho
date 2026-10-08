// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { installLane, type LaneAnswer } from "@shared/test/lane-fake";
import { installPowWorker, loadPowVectors } from "@shared/test/pow-worker-fake";

// Экран восстановления доступа на ограничителе края (приёмка NTF-2, Р5,
// NTF2-58, NTF2-72 шаг 1; замысел `issue-2917` З10, CX2-30).
//
// Это модульная сторона того, что сквозная проба П12 утверждает браузером
// (`e2e/specs/ntf2-58-limiter-refusal.spec.ts`): экран показывает `message`
// ответа ДОСЛОВНО и своего текста отказа не имеет; на `503` решатель не
// создаётся; бюджет решателя отсчитывает таймер главного потока; размонтирование
// завершает решатель. Сеть — дублёр полосы, `Worker` — дублёр решателя,
// отвечающий настоящим решением, когда велит проба.

const RECOVERY = "/iam/v1/auth/recovery";
const PROOF = "x-kacho-proof";
const UNIFORM = /Если адрес зарегистрирован, мы отправили письмо/;

const vectors = loadPowVectors();
const BUDGET_MS = vectors.solverBudgetSeconds * 1000;
const CHALLENGE_1 = vectors.vectors[0].challenge;
const CHALLENGE_2 = vectors.vectors.find((v) => v.challenge !== CHALLENGE_1)!.challenge;

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

function unavailable(message: string): LaneAnswer {
  return { status: 503, body: { code: 14, message, details: [] } };
}

/** Экран — испытуемый; его модуль спрашивается в каждой пробе ПОСЛЕ фикстуры. */
async function renderRecovery() {
  const { RecoveryPage } = await import("./RecoveryPage");
  return render(
    <MemoryRouter initialEntries={["/recovery"]}>
      <RecoveryPage />
    </MemoryRouter>,
  );
}

const email = () => screen.getByLabelText<HTMLInputElement>("Адрес электронной почты");
const submit = () => screen.getByRole<HTMLButtonElement>("button");

function send(address: string) {
  fireEvent.change(email(), { target: { value: address } });
  fireEvent.click(submit());
}

async function settle() {
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
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

describe("экран восстановления на ограничителе края", () => {
  it("NTF2-72 шаг 1 · ответ 200 {} — единый текст, один запрос без доказательства, ошибки нет", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({ [`POST ${RECOVERY}`]: { status: 200, body: {} } });
    await renderRecovery();
    send("z@kacho.local");

    expect(await screen.findByText(UNIFORM)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(1);
    expect(posts[0].body).toEqual({
      email: "z@kacho.local",
      csrfToken: "tok-recovery-1",
    });
    expect(PROOF in posts[0].headers).toBe(false);
    expect(workers.lives).toHaveLength(0);
  });

  it("NTF2-58 (а) · 503 — `request limiter is unavailable` дословно, форма редактируема, решателя нет", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: unavailable("request limiter is unavailable"),
    });
    await renderRecovery();
    send("z@kacho.local");

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("request limiter is unavailable");
    await waitFor(() => expect(submit()).toBeEnabled());
    expect(email()).toBeEnabled();
    expect(lane.of("POST", RECOVERY)).toHaveLength(1);
    expect(lane.of("POST", RECOVERY).filter((c) => PROOF in c.headers)).toHaveLength(0);
    expect(workers.lives).toHaveLength(0);
    expect(screen.queryByText(UNIFORM)).toBeNull();
  });

  it("NTF2-58 (г) · текст отказа — из ответа: `limiter probe <n>` дословно, копии текста края нет", async () => {
    // verifies #2917
    const n = 7301;
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: unavailable(`limiter probe ${n}`),
    });
    await renderRecovery();
    send("z@kacho.local");

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(`limiter probe ${n}`);
    expect(document.body.textContent).not.toContain("request limiter is unavailable");
    await waitFor(() => expect(submit()).toBeEnabled());
    expect(lane.of("POST", RECOVERY)).toHaveLength(1);
    expect(workers.lives).toHaveLength(0);
  });

  it("NTF2-58 близнец (в) · вызов в пределах бюджета — повтор с X-Kacho-Proof, единый текст, ошибки нет", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => (nth === 1 ? challenge(CHALLENGE_1, 8) : { status: 200, body: {} }),
    });
    await renderRecovery();
    send("z@kacho.local");
    await waitFor(() => expect(workers!.lives).toHaveLength(1));
    act(() => {
      workers!.lives[0].answer();
    });

    expect(await screen.findByText(UNIFORM)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(2);
    expect(PROOF in posts[0].headers).toBe(false);
    expect(posts[1].headers[PROOF]?.startsWith(`${CHALLENGE_1}:`)).toBe(true);
  });

  it("NTF2-58 (б) · бюджет по таймеру страницы: в Bs − 1 с решатель жив и форма в отправке; в Bs — `proof of work required`, форма редактируема, новая отправка — новый вызов", async () => {
    // verifies #2917
    jest.useFakeTimers({
      doNotFake: ["queueMicrotask", "nextTick", "setImmediate"],
    });
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => challenge(nth === 1 ? CHALLENGE_1 : CHALLENGE_2, 24),
    });
    await renderRecovery();
    await settle();
    send("z@kacho.local");
    await settle();
    expect(workers.lives).toHaveLength(1);

    await act(async () => {
      await jest.advanceTimersByTimeAsync(BUDGET_MS - 1000);
    });
    expect(workers.lives[0].terminated).toBe(false);
    expect(submit()).toBeDisabled();
    expect(lane.of("POST", RECOVERY).filter((c) => PROOF in c.headers)).toHaveLength(0);

    await act(async () => {
      await jest.advanceTimersByTimeAsync(1000);
    });
    expect(workers.lives[0].terminated).toBe(true);
    expect(screen.getByRole("alert").textContent).toBe("proof of work required");
    expect(submit()).toBeEnabled();
    expect(email()).toBeEnabled();

    await act(async () => {
      workers!.lives[0].answer();
      await jest.advanceTimersByTimeAsync(vectors.challengeTtlSeconds * 1000);
    });
    expect(lane.of("POST", RECOVERY).filter((c) => PROOF in c.headers)).toHaveLength(0);

    fireEvent.click(submit());
    await settle();
    const posts = lane.of("POST", RECOVERY);
    expect(posts).toHaveLength(2);
    expect(PROOF in posts[1].headers).toBe(false);
    expect(workers.lives).toHaveLength(2);
    expect(workers.lives[1].posted.at(-1)).toMatchObject({
      challenge: CHALLENGE_2,
    });
  });

  it("CX2-30 · размонтирование экрана завершает решатель; его поздний ответ не выпускает доказательства", async () => {
    // verifies #2917
    workers = installPowWorker();
    lane = installLane({
      [`POST ${RECOVERY}`]: (_c, nth) => (nth === 1 ? challenge(CHALLENGE_1, 8) : { status: 200, body: {} }),
    });
    const view = await renderRecovery();
    send("z@kacho.local");
    await waitFor(() => expect(workers!.lives).toHaveLength(1));

    view.unmount();
    expect(workers.lives[0].terminated).toBe(true);
    workers.lives[0].answer();
    await settle();
    expect(lane.of("POST", RECOVERY)).toHaveLength(1);
  });
});
