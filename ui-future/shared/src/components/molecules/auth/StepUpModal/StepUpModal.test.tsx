// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { requestFreshPresentation, requestStepUp } from "@shared/api/step-up";
import { installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Окно подтверждения ведёт церемонию НАШИМ глаголом (`POST /iam/v1/auth/step-up`),
// не покидая консоли, и отпускает отвергнутый запрос только тогда, когда
// служба подтвердила предъявление (приёмка F8, S2). Способы — из `stepUpMethods`
// службы, по умолчанию самый сильный, запасной код — никогда; ключ —
// испытанием повышения и церемонией браузера (приёмка F8-S7, Р5).

const { StepUpModal } = await import("./StepUpModal");

const CEREMONY: LaneAnswer = {
  status: 200,
  body: {
    session: { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "2", emailVerified: false },
    assurance: { level: "2", level2Reachable: true, missingForLevel2: [] },
  },
};

/** Чтение состояния формы SUA (Р6) с данным перечнем способов. */
function stateWith(stepUpMethods: string[]): LaneAnswer {
  return {
    status: 200,
    body: {
      totp: { enrolled: true, confirmedAt: "2026-10-14T21:04:00Z" },
      backupCodeSet: { remaining: 10, total: 10, issuedAt: "2026-10-14T21:04:00Z" },
      accessKeys: { available: true },
      stepUpMethods,
    },
  };
}

/** Чтение прежней формы (Ф12 Р4): перечня способов служба не называет. */
const STATE_WITHOUT_METHODS: LaneAnswer = {
  status: 200,
  body: { totp: { enrolled: true }, backupCodeSet: { remaining: 10, total: 10 } },
};

const BEGIN: LaneAnswer = {
  status: 200,
  body: {
    publicKey: {
      challenge: "Y2hhbGxlbmdl",
      rpId: "console.kacho.test",
      timeout: 60000,
      userVerification: "required",
      allowCredentials: [{ type: "public-key", id: "Y3JlZA" }],
    },
  },
};

function bytesOf(...n: number[]): ArrayBuffer {
  return new Uint8Array(n).buffer;
}

function browserCredential() {
  return {
    id: "Y3JlZA",
    rawId: bytesOf(0x63, 0x72, 0x65, 0x64),
    type: "public-key",
    response: {
      clientDataJSON: bytesOf(0x7b, 0x7d),
      authenticatorData: bytesOf(1, 2, 3),
      signature: bytesOf(4, 5, 6),
      userHandle: bytesOf(7, 8),
    },
  };
}

const EXPECTED_CREDENTIAL = {
  id: "Y3JlZA",
  rawId: "Y3JlZA",
  type: "public-key",
  response: { clientDataJSON: "e30", authenticatorData: "AQID", signature: "BAUG", userHandle: "Bwg" },
};

type CredentialsGet = (options?: CredentialRequestOptions) => Promise<unknown>;

function installBrowserKeys(get: CredentialsGet) {
  const w = window as unknown as Record<string, unknown>;
  const savedPkc = w.PublicKeyCredential;
  const savedCredentials = Object.getOwnPropertyDescriptor(navigator, "credentials");
  const spy = jest.fn(get);
  w.PublicKeyCredential = function PublicKeyCredential() {};
  Object.defineProperty(navigator, "credentials", { configurable: true, value: { get: spy } });
  return {
    get: spy,
    restore() {
      if (savedPkc === undefined) delete w.PublicKeyCredential;
      else w.PublicKeyCredential = savedPkc;
      if (savedCredentials) Object.defineProperty(navigator, "credentials", savedCredentials);
      else delete (navigator as unknown as Record<string, unknown>).credentials;
    },
  };
}

let lane: ReturnType<typeof installLane> | null = null;
let keys: ReturnType<typeof installBrowserKeys> | null = null;
afterEach(() => {
  lane?.restore();
  lane = null;
  keys?.restore();
  keys = null;
});

const dialog = () => screen.getByRole("dialog");
const radio = (name: string) => within(dialog()).getByRole("radio", { name });

/** Открыть окно просьбой и дождаться прочитанного перечня способов. */
async function open(ask: () => Promise<boolean>) {
  let granted: boolean | null = null;
  act(() => {
    void ask().then((ok) => {
      granted = ok;
    });
  });
  await screen.findByRole("dialog");
  await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/second-factor")).toHaveLength(1));
  return () => granted;
}

describe("окно подтверждения — выбор способа (F8-S7, Р5)", () => {
  it("F8-S7-24 · код и набор: по умолчанию «Код из приложения», запасной код — в «Другой способ», не выбран", async () => {
    lane = installLane({ "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp"]) });
    render(<StepUpModal />);
    await open(() => requestFreshPresentation());
    await waitFor(() => expect(radio("Код из приложения")).toHaveAttribute("aria-checked", "true"));
    const other = within(dialog()).getByRole("radiogroup", { name: "Другой способ" });
    expect(within(other).getByRole("radio", { name: "Запасной код" })).toHaveAttribute("aria-checked", "false");
    // Поле кода названо своим способом, переключатель им полем не назван.
    expect(within(dialog()).getByLabelText("Код из приложения").tagName).toBe("INPUT");
    expect(within(dialog()).queryByRole("button", { name: "Подтвердить ключом доступа" })).toBeNull();
    expect(within(dialog()).queryByRole("button", { name: "Войти заново" })).toBeNull();
  });

  it("F8-S7-20 · с ключом: по умолчанию ключ; стрелка переводит выбор внутри «Другой способ»", async () => {
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    lane = installLane({ "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp", "webauthn"]) });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    expect(await within(dialog()).findByRole("button", { name: "Подтвердить ключом доступа" })).toBeInTheDocument();
    const other = within(dialog()).getByRole("radiogroup", { name: "Другой способ" });
    expect(
      within(other)
        .getAllByRole("radio")
        .map((r) => r.textContent),
    ).toEqual(["Код из приложения", "Запасной код"]);
    expect(
      within(other)
        .getAllByRole("radio")
        .every((r) => r.getAttribute("aria-checked") === "false"),
    ).toBe(true);
    // Без выбора кода поля кода нет.
    expect(within(dialog()).queryByRole("textbox")).toBeNull();
    fireEvent.keyDown(within(other).getAllByRole("radio")[0], { key: "ArrowDown" });
    expect(radio("Запасной код")).toHaveAttribute("aria-checked", "true");
    expect(within(dialog()).getByLabelText("Запасной код").tagName).toBe("INPUT");
  });

  it("служба перечня не называет (ответ прежней формы): окно предлагает оба способа кода, код — по умолчанию", async () => {
    lane = installLane({ "GET /iam/v1/auth/second-factor": STATE_WITHOUT_METHODS });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    await waitFor(() => expect(radio("Код из приложения")).toHaveAttribute("aria-checked", "true"));
    expect(radio("Запасной код")).toBeInTheDocument();
    expect(within(dialog()).queryByText("Нет способа подтверждения")).toBeNull();
  });

  it("F8-S7-27 · способов нет на отказе свежести: «Нет способа подтверждения», «Настроить второй фактор», «Войти заново»", async () => {
    lane = installLane({ "GET /iam/v1/auth/second-factor": stateWith([]) });
    render(<StepUpModal />);
    await open(() => requestFreshPresentation());
    expect(await within(dialog()).findByText("Нет способа подтверждения")).toBeInTheDocument();
    const link = within(dialog()).getByRole<HTMLAnchorElement>("link", { name: "Настроить второй фактор" });
    expect(new URL(link.href).pathname).toBe("/settings");
    expect(within(dialog()).getByRole("button", { name: "Войти заново" })).toBeInTheDocument();
    expect(within(dialog()).queryByRole("textbox")).toBeNull();
    expect(within(dialog()).queryByRole("button", { name: "Подтвердить ключом доступа" })).toBeNull();
  });

  it("F8 Р12 · способов нет на вызове края об уровне: «Войти заново» не предложен — вход уровня не поднимает", async () => {
    lane = installLane({ "GET /iam/v1/auth/second-factor": stateWith([]) });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    expect(await within(dialog()).findByText("Нет способа подтверждения")).toBeInTheDocument();
    expect(within(dialog()).queryByRole("button", { name: "Войти заново" })).toBeNull();
  });
});

describe("окно подтверждения — каждый способ и отказы (F8-S7, Р5)", () => {
  it("F8-S7-23 · запасной код: step-up с lookup_secret, остаток назван, окно закрыто, просьба разрешена", async () => {
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp"]),
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: {
          ...(CEREMONY.body as object),
          backupCodeSet: {
            remaining: 9,
            total: 10,
            issuedAt: "2026-10-14T21:04:00Z",
            lastUsedAt: "2026-10-14T21:06:00Z",
          },
        },
      },
    });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.click(await within(dialog()).findByRole("radio", { name: "Запасной код" }));
    fireEvent.change(within(dialog()).getByLabelText("Запасной код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(granted()).toBe(true));
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toEqual({
      method: "lookup_secret",
      code: "ABCDEFGHJK",
      csrfToken: "tok-step-up-1",
    });
    expect(screen.getByRole("status")).toHaveTextContent("Осталось запасных кодов: 9");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("F8-S7-21 · ключом: испытание повышения → церемония браузера → step-up webauthn с утверждением", async () => {
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp", "webauthn"]),
      "POST /iam/v1/auth/step-up/access-key/begin": BEGIN,
      "POST /iam/v1/auth/step-up": CEREMONY,
    });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.click(await within(dialog()).findByRole("button", { name: "Подтвердить ключом доступа" }));
    await waitFor(() => expect(granted()).toBe(true));
    const order = lane.calls.filter((c) => c.method === "POST").map((c) => c.path);
    expect(order).toEqual(["/iam/v1/auth/step-up/access-key/begin", "/iam/v1/auth/step-up"]);
    expect(lane.of("POST", "/iam/v1/auth/step-up/access-key/begin")[0].body).toEqual({
      csrfToken: "tok-step-up-access-key-begin-1",
    });
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toEqual({
      method: "webauthn",
      credential: EXPECTED_CREDENTIAL,
      csrfToken: "tok-step-up-1",
    });
    expect(keys.get.mock.calls[0][0]?.publicKey?.rpId).toBe("console.kacho.test");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("F8-S7-32 · отказ церемонии браузером: текст консоли, step-up не зовётся, окно открыто", async () => {
    keys = installBrowserKeys(() =>
      Promise.reject(new DOMException("The operation either timed out", "NotAllowedError")),
    );
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["webauthn"]),
      "POST /iam/v1/auth/step-up/access-key/begin": BEGIN,
    });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.click(await within(dialog()).findByRole("button", { name: "Подтвердить ключом доступа" }));
    expect(await within(dialog()).findByRole("alert")).toHaveTextContent(
      "Подтверждение ключом прервано в браузере — повторите или выберите другой способ",
    );
    expect(within(dialog()).getByRole("alert").textContent).not.toMatch(/timed out|NotAllowed/);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(0);
    expect(granted()).toBeNull();
  });

  it("F8-S7-30 · неверный код (401): «Подтверждение не прошло…» в role=alert, окно открыто, просьба не разрешена", async () => {
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp"]),
      "POST /iam/v1/auth/step-up": refusal(401, 16, "authentication failed"),
    });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.change(await within(dialog()).findByLabelText("Код из приложения"), { target: { value: "000000" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Подтвердить" }));
    expect((await within(dialog()).findByRole("alert")).textContent).toBe(
      "Подтверждение не прошло. Повторите или выберите другой способ.",
    );
    expect(dialog()).toBeInTheDocument();
    expect(granted()).toBeNull();
  });

  it("F8-S7-31 · ключ отвергнут службой: тот же текст; новая попытка — новое испытание", async () => {
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["webauthn"]),
      "POST /iam/v1/auth/step-up/access-key/begin": BEGIN,
      "POST /iam/v1/auth/step-up": refusal(401, 16, "authentication failed"),
    });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    const key = await within(dialog()).findByRole("button", { name: "Подтвердить ключом доступа" });
    fireEvent.click(key);
    expect((await within(dialog()).findByRole("alert")).textContent).toBe(
      "Подтверждение не прошло. Повторите или выберите другой способ.",
    );
    fireEvent.click(within(dialog()).getByRole("button", { name: "Подтвердить ключом доступа" }));
    await waitFor(() => expect(lane!.of("POST", "/iam/v1/auth/step-up/access-key/begin")).toHaveLength(2));
  });

  it("F8-S7-33 · частота с Retry-After 600: «Повторите через 10 мин.», поле и кнопка недоступны", async () => {
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp"]),
      "POST /iam/v1/auth/step-up": {
        ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
        headers: { "Retry-After": "600" },
      },
    });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    fireEvent.change(await within(dialog()).findByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Подтвердить" }));
    expect(await within(dialog()).findByText("Слишком много попыток. Повторите через 10 мин.")).toBeInTheDocument();
    expect(within(dialog()).getByLabelText("Код из приложения")).toBeDisabled();
    expect(within(dialog()).getByRole("button", { name: "Подтвердить" })).toBeDisabled();
  });

  it("испытание ключа истекло (CHALLENGE_EXPIRED): «Время подтверждения ключом истекло — повторите»", async () => {
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    lane = installLane({
      "GET /iam/v1/auth/second-factor": stateWith(["webauthn"]),
      "POST /iam/v1/auth/step-up/access-key/begin": BEGIN,
      "POST /iam/v1/auth/step-up": refusal(
        400,
        9,
        "step-up challenge expired: begin step-up again",
        "CHALLENGE_EXPIRED",
      ),
    });
    render(<StepUpModal />);
    await open(() => requestStepUp("2"));
    fireEvent.click(await within(dialog()).findByRole("button", { name: "Подтвердить ключом доступа" }));
    expect((await within(dialog()).findByRole("alert")).textContent).toBe(
      "Время подтверждения ключом истекло — повторите",
    );
  });

  it("F8-36 · второго фактора нет: текст службы и путь к заведению, перечень перечитан, окно не закрывается молча", async () => {
    lane = installLane({
      "GET /iam/v1/auth/second-factor": STATE_WITHOUT_METHODS,
      "POST /iam/v1/auth/step-up": refusal(400, 9, "second factor is not enrolled", "SECOND_FACTOR_NOT_ENROLLED"),
    });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.click(await within(dialog()).findByRole("radio", { name: "Запасной код" }));
    fireEvent.change(within(dialog()).getByLabelText("Запасной код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Подтвердить" }));
    expect(await within(dialog()).findByRole("alert")).toHaveTextContent("second factor is not enrolled");
    const link = within(dialog()).getByRole<HTMLAnchorElement>("link", { name: "Настроить второй фактор" });
    expect(new URL(link.href).pathname).toBe("/settings");
    await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/second-factor")).toHaveLength(2));
    expect(dialog()).toBeInTheDocument();
    // Обещание не разрешено: действие без поручительства не повторяется.
    expect(granted()).toBeNull();
  });

  it("отмена отвергает просьбу — вызывающий отдаёт исходный отказ как есть", async () => {
    lane = installLane({ "GET /iam/v1/auth/second-factor": stateWith(["lookup_secret", "totp"]) });
    render(<StepUpModal />);
    const granted = await open(() => requestStepUp("2"));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Отмена" }));
    await waitFor(() => expect(granted()).toBe(false));
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(0);
  });

  it("снятое окно на просьбы не отвечает — мёртвое окно не поручится за живой запрос", async () => {
    const { unmount } = render(<StepUpModal />);
    unmount();
    expect(await requestStepUp("2")).toBe(false);
  });
});
