// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Окно повторного подтверждения обязано ВЕСТИ тот второй фактор, который
// служба личности действительно предложила (#1213).
//
// ПРЕДМЕТ. Каталог прав объявляет 32 глаголам пол уровня «2», край его
// спрашивает на браузерной полосе — значит у арендатора обязан существовать
// способ уровень поднять. Окно вело ровно один способ (ключ доступа), а
// настройки объявляют ключ доступа БЕСПАРОЛЬНЫМ, то есть ПЕРВЫМ фактором:
// в потоке `aal=aal2` служба его не предлагает вовсе. Две стороны об одном
// предмете, и неверна их РАЗНИЦА.
//
// ЧТО ЗДЕСЬ ДУБЛИРУЕТСЯ. Только сеть: разбор потока (`findNode`, `csrfToken`)
// берётся НАСТОЯЩИЙ, иначе дублёр оказался бы снисходительнее продукта ровно
// в том месте, где живёт дефект.

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { UiNode } from "@shared/lib/kratos";
import { STEP_UP_METHODS } from "@shared/lib/step-up-methods";
import type { StepUpModal as StepUpModalExport, stepUpEnrollUrl as EnrollUrl } from "./StepUpModal";

type StepUpHandler = ((acr?: string) => Promise<void>) | null;

let registered: StepUpHandler = null;
const setStepUpHandler = jest.fn((h: StepUpHandler) => {
  registered = h;
});
const markMfaFresh = jest.fn();
const refresh = jest.fn(async () => {});

const auth = { setStepUpHandler, markMfaFresh, refresh } as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({ useAuth: () => auth }));
// Кодировщик — чистая функция; подменяется, чтобы не тянуть страницу входа
// вместе с её деревом импортов.
jest.unstable_mockModule("@shared/pages/auth/Login", () => ({
  bufferToBase64Url: () => "AAAA",
}));

let StepUpModal: typeof StepUpModalExport;
let stepUpEnrollUrl: typeof EnrollUrl;

/** Узел потока — ровно та форма, что приходит от службы личности. */
const input = (name: string, group: string, value?: unknown): UiNode => ({
  type: "input",
  group,
  attributes: { name, value },
});

const csrf = input("csrf_token", "default", "csrf-1");

/** Маркерный узел каждого способа — тот, по которому его узнают. */
const marker: Record<string, UiNode> = {
  totp: input("totp_code", "totp"),
  lookup_secret: input("lookup_secret", "lookup_secret"),
  webauthn: input(
    "webauthn_login_trigger",
    "webauthn",
    JSON.stringify({ publicKey: { challenge: "x" } }),
  ),
};

/** Ответы сети: сперва инициация потока, затем его отправка. */
let responses: unknown[] = [];
let requests: Array<{ url: string; body: unknown }> = [];

function flowWith(nodes: UiNode[]) {
  return { id: "flw-1", type: "browser", ui: { action: "/x", method: "POST", nodes: [csrf, ...nodes] } };
}

beforeAll(async () => {
  ({ StepUpModal, stepUpEnrollUrl } = await import("./StepUpModal"));
});

beforeEach(() => {
  registered = null;
  responses = [];
  requests = [];
  jest.clearAllMocks();
  (globalThis as unknown as { fetch: unknown }).fetch = jest.fn((url: unknown, init: unknown) => {
    const i = (init ?? {}) as { body?: string };
    requests.push({ url: String(url), body: i.body ? JSON.parse(i.body) : undefined });
    const next = responses.shift();
    if (next === undefined) return Promise.reject(new Error("лишний запрос к службе личности"));
    return Promise.resolve({
      ok: true,
      status: 200,
      text: () => Promise.resolve(JSON.stringify(next)),
    });
  });
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: {
      get: jest.fn(() =>
        Promise.resolve({
          id: "c1",
          rawId: new ArrayBuffer(4),
          type: "public-key",
          response: {
            authenticatorData: new ArrayBuffer(4),
            clientDataJSON: new ArrayBuffer(4),
            signature: new ArrayBuffer(4),
            userHandle: null,
          },
        }),
      ),
    },
  });
});

/** Открывает окно и дожидается, пока поток разобран. */
async function open(acr = "2"): Promise<{ settled: () => boolean; rejected: () => boolean }> {
  let done = false;
  let bad = false;
  render(<StepUpModal />);
  await act(async () => {
    void registered?.(acr)
      .then(() => {
        done = true;
      })
      .catch(() => {
        done = true;
        bad = true;
      });
    // Явный сброс микрозадач: подъём потока идёт обещанием, и без него окно
    // осталось бы в состоянии «спрашиваем способ».
    await Promise.resolve();
  });
  return { settled: () => done, rejected: () => bad };
}

describe("StepUpModal — второй фактор", () => {
  it("ведёт одноразовый код, когда служба личности предложила его", async () => {
    responses = [flowWith([marker.totp]), { id: "flw-1", ui: { action: "/x", method: "POST", nodes: [] } }];
    const p = await open();

    const field = await screen.findByTestId("stepup-code");
    fireEvent.change(field, { target: { value: "123456" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("stepup-confirm"));
      await Promise.resolve();
    });

    await waitFor(() => expect(p.settled()).toBe(true));
    expect(p.rejected()).toBe(false);
    expect(requests[1].body).toMatchObject({ method: "totp", totp_code: "123456", csrf_token: "csrf-1" });
    expect(markMfaFresh).toHaveBeenCalledTimes(1);
  });

  it("без второго фактора НАЗЫВАЕТ это, ведёт настраивать и запрос НЕ пропускает", async () => {
    responses = [flowWith([input("identifier", "password")])];
    const p = await open();

    // Утверждается ВИДИМОЕ, а не разметка: заменитель `Alert` метки не
    // пропускает, а человек читает именно текст.
    expect(await screen.findByText(/Второй фактор не настроен/)).toBeInTheDocument();
    expect(screen.getByTestId("stepup-enroll")).toBeInTheDocument();
    // Подтверждения не предлагается вовсе: подтверждать нечем.
    expect(screen.queryByTestId("stepup-confirm")).toBeNull();
    // Fail-closed: обещание запроса не разрешено — ни успехом, ни молча.
    expect(p.settled()).toBe(false);

    // Ведёт именно ТУДА, где второй фактор заводят, и возвращает обратно
    // абсолютным адресом: относительный служба личности разрешала бы
    // относительно своего, то есть увела бы не туда.
    const url = stepUpEnrollUrl();
    expect(url).toContain("/self-service/settings/browser");
    expect(decodeURIComponent(url)).toContain(window.location.href);
  });

  it("ключ доступа остаётся, когда служба личности предлагает его вторым фактором", async () => {
    responses = [flowWith([marker.webauthn]), { id: "flw-1", ui: { action: "/x", method: "POST", nodes: [] } }];
    const p = await open();

    // Способ назван и в пояснении, и на кнопке — человек видит, ЧЕМ
    // подтверждать, а не только что подтвердить нужно.
    // Совпадений два — пояснение и кнопка, — и это ровно то, что видит человек.
    expect(await screen.findAllByText(/ключом доступа/i)).toHaveLength(2);
    expect(screen.getByTestId("stepup-confirm").textContent).toMatch(/ключ/i);

    await act(async () => {
      fireEvent.click(screen.getByTestId("stepup-confirm"));
      await Promise.resolve();
    });

    await waitFor(() => expect(p.settled()).toBe(true));
    expect(p.rejected()).toBe(false);
    expect(requests[1].body).toMatchObject({ method: "webauthn" });
  });

  // Перечень способов читает ГЕЙТ ДЕРЕВА на стороне развёртывания и по нему
  // судит достижимость уровня. Значит перечень не вправе врать: каждый
  // названный способ окно обязано довести до подтверждения.
  it.each(STEP_UP_METHODS.map((m) => [m]))(
    "объявленный способ %s окно действительно предлагает, а не только называет",
    async (method) => {
      responses = [flowWith([marker[method]])];
      await open();

      expect(await screen.findByTestId("stepup-confirm")).toBeInTheDocument();
      expect(screen.queryByText(/Второй фактор не настроен/)).toBeNull();
    },
  );
});
