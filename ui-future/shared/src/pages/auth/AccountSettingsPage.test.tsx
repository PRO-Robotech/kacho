// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { EDGE_CREDENTIAL_STATE_UNKNOWN, laneAnswerOf } from "@shared/test/edge-answers";
import { SESSION, SIGNED_IN, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Параметры учётной записи (приёмка F8, S2): смена пароля и второй фактор
// глаголами службы, не покидая консоли.

const { AccountSettingsPage } = await import("./AccountSettingsPage");

const NOT_ENROLLED: LaneAnswer = { status: 200, body: { totp: { enrolled: false } } };
const PENDING: LaneAnswer = { status: 200, body: { totp: { enrolled: false, pendingUntil: "2026-09-23T12:15:00Z" } } };
const ENROLLED: LaneAnswer = {
  status: 200,
  body: { totp: { enrolled: true, confirmedAt: "2026-09-23T12:00:00Z" }, backupCodes: { remaining: 10, total: 10 } },
};
const ENROLLMENT: LaneAnswer = {
  status: 200,
  body: { secret: "JBSWY3DPEHPK3PXP", otpauthUri: "otpauth://totp/kacho:a?secret=JBSWY3DPEHPK3PXP", expiresAt: "x" },
};
const CODES = ["0A1B2C3D4E", "5F6G7H8J9K"];
const CONFIRMED: LaneAnswer = {
  status: 200,
  body: {
    backupCodes: CODES,
    session: SESSION,
    assurance: { level: "2", level2Reachable: true, missingForLevel2: [] },
  },
};

function renderPage() {
  render(
    <MemoryRouter initialEntries={["/settings"]}>
      <AccountSettingsPage />
    </MemoryRouter>,
  );
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("параметры учётной записи", () => {
  it("F8-17 · адрес и признак его подтверждённости — из ответа края; действия «подтвердить» нет", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": SIGNED_IN, "GET /iam/v1/auth/second-factor": NOT_ENROLLED });
    renderPage();
    const account = await screen.findByRole("region", { name: "Учётная запись" });
    expect(account).toHaveTextContent("a@kacho.local");
    expect(account).toHaveTextContent("Адрес не подтверждён");
    expect(within(account).queryByRole("button")).toBeNull();
  });

  it("F8-23 · смена пароля зовёт наш глагол с признаком своего вида", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/password": { status: 200, body: { session: SESSION } },
    });
    renderPage();
    const password = await screen.findByRole("region", { name: "Пароль" });
    fireEvent.change(within(password).getByLabelText("Текущий пароль"), { target: { value: "old" } });
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "new-one" } });
    fireEvent.click(within(password).getByRole("button", { name: "Сменить пароль" }));
    expect(await within(password).findByRole("status")).toHaveTextContent("Пароль сменён.");
    expect(lane.of("POST", "/iam/v1/auth/password")[0].body).toEqual({
      currentPassword: "old",
      newPassword: "new-one",
      csrfToken: "tok-password-1",
    });
  });

  it("F8-25 · служба молчит краю на глаголе с носителем (KA1, Р1): ответ назван, повторить можно, экран на месте", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/password": laneAnswerOf(EDGE_CREDENTIAL_STATE_UNKNOWN),
    });
    renderPage();
    const password = await screen.findByRole("region", { name: "Пароль" });
    fireEvent.click(within(password).getByRole("button", { name: "Сменить пароль" }));
    const alert = await within(password).findByRole("alert");
    expect(alert).toHaveTextContent("credential state could not be established");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(lane.of("POST", "/iam/v1/auth/password")).toHaveLength(1);
  });

  it("F8-26/F8-27 · заведение: материал из ответа, подтверждение, коды один раз, состояние перечитано", async () => {
    let enrolled = false;
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": () => (enrolled ? ENROLLED : NOT_ENROLLED),
      "POST /iam/v1/auth/second-factor/enroll": ENROLLMENT,
      "POST /iam/v1/auth/second-factor/confirm": () => {
        enrolled = true;
        return CONFIRMED;
      },
    });
    renderPage();
    const factor = await screen.findByRole("region", { name: "Второй фактор" });
    expect(await within(factor).findByText("Второй фактор не настроен")).toBeInTheDocument();
    fireEvent.click(within(factor).getByRole("button", { name: "Настроить второй фактор" }));
    expect(await within(factor).findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
    fireEvent.change(within(factor).getByLabelText("Первый код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(factor).getByRole("button", { name: "Подтвердить" }));
    const list = await within(factor).findByRole("list", { name: "Запасные коды" });
    for (const code of CODES) expect(list).toHaveTextContent(code);
    expect(factor).toHaveTextContent("показываются один раз");
    await waitFor(() => expect(factor).toHaveTextContent("Второй фактор настроен"));
    // Признак — на ОДНУ отправку (условие C12): заведение унесло первый,
    // подтверждение несёт второй, добытый за ответом заведения.
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")[0].body).toEqual({ csrfToken: "tok-second-factor-1" });
    expect(lane.of("POST", "/iam/v1/auth/second-factor/confirm")[0].body).toEqual({
      code: "123456",
      csrfToken: "tok-second-factor-2",
    });
  });

  it("F8-28 · неверный первый код: отказ дословно, запасных кодов нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": (_c, nth) => (nth === 1 ? NOT_ENROLLED : PENDING),
      "POST /iam/v1/auth/second-factor/enroll": ENROLLMENT,
      "POST /iam/v1/auth/second-factor/confirm": refusal(401, 16, "authentication failed"),
    });
    renderPage();
    const factor = await screen.findByRole("region", { name: "Второй фактор" });
    fireEvent.click(await within(factor).findByRole("button", { name: "Настроить второй фактор" }));
    fireEvent.change(await within(factor).findByLabelText("Первый код из приложения"), { target: { value: "000000" } });
    fireEvent.click(within(factor).getByRole("button", { name: "Подтвердить" }));
    expect((await within(factor).findByRole("alert")).textContent).toBe("authentication failed");
    expect(within(factor).queryByRole("list", { name: "Запасные коды" })).toBeNull();
    await waitFor(() => expect(factor).toHaveTextContent("Настройка второго фактора не завершена"));
  });

  it("F8-29 · свежесть: повышение и возврат на ТОТ ЖЕ шаг — заведение повторено само", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/second-factor/enroll": (_c, nth) =>
        nth === 1
          ? refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH")
          : ENROLLMENT,
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: {
          session: SESSION,
          assurance: { level: "1", level2Reachable: true, missingForLevel2: ["second-factor"] },
        },
      },
    });
    renderPage();
    const factor = await screen.findByRole("region", { name: "Второй фактор" });
    fireEvent.click(await within(factor).findByRole("button", { name: "Настроить второй фактор" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Пароль"), { target: { value: "p" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
    expect(await within(factor).findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")).toHaveLength(2);
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toMatchObject({ method: "password", password: "p" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("F8-32 · снятие того, чего нет: текст службы назван", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": (_c, nth) => (nth === 1 ? ENROLLED : NOT_ENROLLED),
      "POST /iam/v1/auth/second-factor/remove": refusal(
        400,
        9,
        "second factor is not enrolled",
        "SECOND_FACTOR_NOT_ENROLLED",
      ),
    });
    renderPage();
    const factor = await screen.findByRole("region", { name: "Второй фактор" });
    fireEvent.click(await within(factor).findByRole("button", { name: "Снять второй фактор" }));
    fireEvent.click(within(factor).getByLabelText("Запасной код"));
    fireEvent.change(within(factor).getByLabelText("Код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(within(factor).getByRole("button", { name: "Снять" }));
    expect(await within(factor).findByRole("alert")).toHaveTextContent("second factor is not enrolled");
  });

  it("C6 · край не ответил о сессии — экран называет это и даёт спросить снова, а не зовёт входить", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": (_c, nth) => (nth === 1 ? refusal(503, 14, "unavailable") : SIGNED_IN),
    });
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("unavailable");
    expect(screen.queryByRole("link", { name: "Войти" })).toBeNull();
    expect(screen.queryByText(/доступны после входа/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Проверить снова" }));
    expect(await screen.findByRole("region", { name: "Учётная запись" })).toHaveTextContent("a@kacho.local");
  });

  it("C8 · поля подтверждённости в ответе края нет — признака нет, «не подтверждён» не выдумывается", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": {
        status: 200,
        body: { user: { id: "usr-1", email: "a@kacho.local", displayName: "a" }, session: { assuranceLevel: "1" } },
      },
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
    });
    renderPage();
    const account = await screen.findByRole("region", { name: "Учётная запись" });
    expect(account).toHaveTextContent("a@kacho.local");
    expect(account).not.toHaveTextContent("подтверждён");
  });

  it("C16 · способ не выбран — в теле его нет, и названное службой поле способа отмечено у переключателя", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": ENROLLED,
      "POST /iam/v1/auth/second-factor/remove": refusal(400, 3, "Illegal argument method: required"),
    });
    renderPage();
    const factor = await screen.findByRole("region", { name: "Второй фактор" });
    fireEvent.click(await within(factor).findByRole("button", { name: "Снять второй фактор" }));
    // Переключатель открыт БЕЗ выбора: ни один способ не отмечен.
    for (const label of ["Код из приложения", "Запасной код"]) {
      expect(within(factor).getByLabelText<HTMLInputElement>(label).checked).toBe(false);
    }
    fireEvent.change(within(factor).getByLabelText("Код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(within(factor).getByRole("button", { name: "Снять" }));
    await waitFor(() =>
      expect(within(factor).getByRole("radiogroup", { name: "Способ подтверждения" })).toHaveAttribute(
        "aria-invalid",
        "true",
      ),
    );
    expect(lane.of("POST", "/iam/v1/auth/second-factor/remove")[0].body).toEqual({
      code: "ABCDEFGHJK",
      csrfToken: "tok-second-factor-1",
    });
    expect(within(factor).getByText("Illegal argument method: required")).toBeInTheDocument();
  });

  it("без сессии — путь ко входу с возвратом сюда, и ни одной формы", async () => {
    lane = installLane({});
    renderPage();
    const link = await screen.findByRole<HTMLAnchorElement>("link", { name: "Войти" });
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/settings");
    expect(screen.queryByRole("region", { name: "Пароль" })).toBeNull();
  });
});

// ═══ S4 — группа L. Ключи доступа на /settings (приёмка F8, ред. 12, Р11) ═══
//
// Раздел ходит глаголами Ф7 через существующие маршруты края; дублёр отвечает
// телами в той форме, в какой их отдаёт край (`protojson`: поля camelCase,
// `bytes` — строкой base64 стандартного алфавита с дополнением, `int64` —
// строкой). Интерфейс браузера ключей — подставной: модульная проба судит, что
// раздел передал браузеру и что перенёс из его ответа в тело глагола.

const USER_ID = "usr-1";
const KEYS = `/iam/v1/users/${USER_ID}/accessKeys`;
const BEGIN_REGISTRATION = `${KEYS}:beginRegistration`;

/** Байты испытания: в base64 они дают оба знака стандартного алфавита (`+`, `/`). */
const CHALLENGE_BYTES = [251, 255, 191, 0, 1, 2, 3, 250];
const REGISTRATION_CHALLENGE: LaneAnswer = {
  status: 200,
  body: {
    challenge: "+/+/AAECA/o=",
    rp: { id: "console.kacho.local", name: "Kachō" },
    user: { id: "dXNyLTE=", name: "a@kacho.local", displayName: "a" },
    pubKeyCredParams: [{ type: "public-key", alg: "-7" }],
    authenticatorSelection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
    attestation: "none",
    extensions: { credProps: true },
    expiresAt: "2026-10-07T12:05:00Z",
  },
};

function keyRecord(id: string, name: string, extra: Record<string, unknown> = {}) {
  return { id, userId: USER_ID, name, createdAt: "2026-10-07T10:00:00Z", ...extra };
}

function listOf(...keys: ReturnType<typeof keyRecord>[]): LaneAnswer {
  return { status: 200, body: keys.length > 0 ? { accessKeys: keys } : {} };
}

const OPERATION_PENDING: LaneAnswer = { status: 200, body: { id: "op-1", done: false } };
const OPERATION_DONE: LaneAnswer = { status: 200, body: { id: "op-1", done: true, response: {} } };

/** Отказ края с нарушением поля — `BadRequest.fieldViolations` (N36). */
function fieldRefusal(field: string, description: string): LaneAnswer {
  return {
    status: 400,
    body: {
      code: 3,
      message: "invalid argument",
      details: [{ "@type": "type.googleapis.com/google.rpc.BadRequest", fieldViolations: [{ field, description }] }],
    },
  };
}

const bytes = (...b: number[]) => new Uint8Array(b).buffer;
const b64 = (buf: ArrayBuffer) => Buffer.from(new Uint8Array(buf)).toString("base64");

/** Ответ браузера на создание удостоверения — с полями СВЕРХ объявленных (F8-55). */
function createdCredential(rk: boolean | undefined) {
  const rawId = bytes(1, 2, 3, 4, 250, 251);
  const clientDataJSON = bytes(123, 34, 116, 34, 125);
  const attestationObject = bytes(163, 99, 102, 109, 116);
  return {
    credential: {
      id: "AQIDBPr7",
      rawId,
      type: "public-key",
      authenticatorAttachment: "platform",
      response: {
        clientDataJSON,
        attestationObject,
        transports: ["internal"],
        publicKeyAlgorithm: -7,
        getTransports: () => ["internal"],
        getPublicKeyAlgorithm: () => -7,
      },
      getClientExtensionResults: () =>
        rk === undefined ? { appidExclude: false } : { credProps: { rk }, appidExclude: false },
    },
    wire: { id: b64(rawId), clientDataJson: b64(clientDataJSON), attestationObject: b64(attestationObject) },
  };
}

type CredentialsCreate = (options?: CredentialCreationOptions) => Promise<unknown>;

/** Подставной интерфейс ключей браузера; `present: false` — браузер без него. */
function installBrowserKeys(create: CredentialsCreate, present = true) {
  const w = window as unknown as Record<string, unknown>;
  const savedPkc = w.PublicKeyCredential;
  const savedCredentials = Object.getOwnPropertyDescriptor(navigator, "credentials");
  const spy = jest.fn(create);
  if (present) {
    w.PublicKeyCredential = function PublicKeyCredential() {};
    Object.defineProperty(navigator, "credentials", { configurable: true, value: { create: spy, get: jest.fn() } });
  } else {
    delete w.PublicKeyCredential;
    Object.defineProperty(navigator, "credentials", { configurable: true, value: undefined });
  }
  return {
    create: spy,
    restore() {
      if (savedPkc === undefined) delete w.PublicKeyCredential;
      else w.PublicKeyCredential = savedPkc;
      if (savedCredentials) Object.defineProperty(navigator, "credentials", savedCredentials);
      else delete (navigator as unknown as Record<string, unknown>).credentials;
    },
  };
}

/** Журнал браузера за время пробы — материал церемонии в него не уходит. */
function watchJournal() {
  const methods = ["log", "info", "warn", "error", "debug"] as const;
  const spies = methods.map((m) => jest.spyOn(console, m).mockImplementation(() => undefined));
  return {
    entries: () => spies.flatMap((s, i) => s.mock.calls.map((args) => `${methods[i]}: ${args.map(String).join(" ")}`)),
    restore: () => spies.forEach((s) => s.mockRestore()),
  };
}

let keys: ReturnType<typeof installBrowserKeys> | null = null;
let journal: ReturnType<typeof watchJournal> | null = null;
afterEach(() => {
  keys?.restore();
  keys = null;
  journal?.restore();
  journal = null;
});

const KEYS_BASE = {
  "GET /iam/v1/auth/me": SIGNED_IN,
  "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
} as const;

async function keysRegion() {
  return screen.findByRole("region", { name: "Ключи доступа" });
}

function addKey(region: HTMLElement, name: string, description = "") {
  fireEvent.change(within(region).getByLabelText("Имя"), { target: { value: name } });
  fireEvent.change(within(region).getByLabelText("Описание"), { target: { value: description } });
  fireEvent.click(within(region).getByRole("button", { name: "Добавить ключ доступа" }));
}

function keyItem(region: HTMLElement, name: string) {
  return within(within(region).getByRole("list", { name: "Заведённые ключи" }))
    .getAllByRole("listitem")
    .find((li) => li.textContent?.includes(name));
}

describe("ключи доступа на /settings (F8, S4, группа L)", () => {
  it("F8-55 · браузер получает испытание как есть, служба — ровно объявленные поля; раздел ждёт done и перечитывает перечень", async () => {
    journal = watchJournal();
    const { credential, wire } = createdCredential(true);
    let registered = false;
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: () => (registered ? listOf(keyRecord("ak-1", "key-1", { description: "Ноутбук" })) : listOf()),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: OPERATION_PENDING,
      "GET /operations/op-1": () => {
        registered = true;
        return OPERATION_DONE;
      },
    });
    keys = installBrowserKeys(() => Promise.resolve(credential));
    renderPage();
    const region = await keysRegion();
    expect(await within(region).findByText("Ключей доступа нет")).toBeInTheDocument();
    addKey(region, "key-1", "Ноутбук");
    await waitFor(() => expect(keyItem(region, "key-1")).toBeDefined());

    const options = keys.create.mock.calls[0][0] as CredentialCreationOptions;
    const pk = options.publicKey!;
    expect({
      challenge: Array.from(new Uint8Array(pk.challenge as ArrayBuffer)),
      rp: pk.rp,
      user: { ...pk.user, id: Buffer.from(new Uint8Array(pk.user.id as ArrayBuffer)).toString() },
      pubKeyCredParams: pk.pubKeyCredParams,
      authenticatorSelection: pk.authenticatorSelection,
      attestation: pk.attestation,
      extensions: pk.extensions,
    }).toEqual({
      challenge: CHALLENGE_BYTES,
      rp: { id: "console.kacho.local", name: "Kachō" },
      user: { id: "usr-1", name: "a@kacho.local", displayName: "a" },
      pubKeyCredParams: [{ type: "public-key", alg: -7 }],
      authenticatorSelection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
      attestation: "none",
      extensions: { credProps: true },
    });
    expect(lane.of("POST", KEYS)[0].body).toEqual({
      name: "key-1",
      description: "Ноутбук",
      credential: { ...wire, discoverable: true },
    });
    // Порядок: испытание → приём результата → опрос операции → перечень.
    const order = lane.calls
      .filter((c) => c.path.startsWith(KEYS) || c.path.startsWith("/operations/"))
      .map((c) => `${c.method} ${c.path}`);
    expect(order.slice(-4)).toEqual([
      `POST ${BEGIN_REGISTRATION}`,
      `POST ${KEYS}`,
      "GET /operations/op-1",
      `GET ${KEYS}`,
    ]);
    // Материал церемонии в журнал браузера не уходит.
    const material = ["+/+/AAECA/o=", wire.id, wire.clientDataJson, wire.attestationObject, "console.kacho.local"];
    expect(journal.entries().filter((e) => material.some((m) => e.includes(m)))).toEqual([]);
  });

  it("F8-55 · обнаружимость переносится тремя состояниями: rk=false — false, без credProps — поля нет", async () => {
    for (const [rk, expected] of [
      [false, { discoverable: false }],
      [undefined, {}],
    ] as const) {
      const { credential, wire } = createdCredential(rk);
      lane = installLane({
        ...KEYS_BASE,
        [`GET ${KEYS}`]: listOf(),
        [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
        [`POST ${KEYS}`]: OPERATION_PENDING,
        "GET /operations/op-1": OPERATION_DONE,
      });
      keys = installBrowserKeys(() => Promise.resolve(credential));
      const { unmount } = render(
        <MemoryRouter initialEntries={["/settings"]}>
          <AccountSettingsPage />
        </MemoryRouter>,
      );
      const region = await keysRegion();
      await within(region).findByText("Ключей доступа нет");
      addKey(region, "key-x");
      await waitFor(() => expect(lane!.of("POST", KEYS)).toHaveLength(1));
      expect(lane.of("POST", KEYS)[0].body).toEqual({
        name: "key-x",
        description: "",
        credential: { ...wire, ...expected },
      });
      unmount();
      lane.restore();
      keys.restore();
      keys = null;
    }
  });

  it("F8-52 · человек отменил церемонию в браузере: текст консоли, приём результата не зовётся, кнопка снова доступна", async () => {
    journal = watchJournal();
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
    });
    keys = installBrowserKeys(() =>
      Promise.reject(
        new DOMException(
          "The operation either timed out or was not allowed. rp console.kacho.local",
          "NotAllowedError",
        ),
      ),
    );
    renderPage();
    const region = await keysRegion();
    await within(region).findByText("Ключей доступа нет");
    addKey(region, "key-52");
    expect(await within(region).findByText("Добавление ключа прервано в браузере — повторите")).toBeInTheDocument();
    expect(lane.of("POST", KEYS)).toHaveLength(0);
    expect(within(region).getByRole("button", { name: "Добавить ключ доступа" })).toBeEnabled();
    expect(region).not.toHaveTextContent("NotAllowedError");
    expect(region).not.toHaveTextContent("console.kacho.local");
    expect(journal.entries().filter((e) => e.includes("console.kacho.local") || e.includes("NotAllowedError"))).toEqual(
      [],
    );
  });

  it("F8-53 · браузер без интерфейса ключей: добавить нельзя, перечень и удаление есть, испытания не просят", async () => {
    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-53")) });
    keys = installBrowserKeys(() => Promise.reject(new Error("не должен зваться")), false);
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-53")).toBeDefined());
    expect(
      within(region).getByText("Этот браузер не работает с ключами доступа — добавьте ключ в другом браузере"),
    ).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Добавить ключ доступа" })).toBeNull();
    expect(within(keyItem(region, "key-53")!).getByRole("button", { name: "Удалить" })).toBeInTheDocument();
    expect(lane.of("POST", BEGIN_REGISTRATION)).toHaveLength(0);
  });

  it("F8-54 · операция заведения кончилась отказом: текст службы дословно, нового ключа нет, кнопка доступна", async () => {
    const ceiling =
      "access keys per user ceiling reached; WAY OUT: revoke an access key you no longer need, or raise own-ceilings.access-keys-per-user";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-old")),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: OPERATION_PENDING,
      "GET /operations/op-1": { status: 200, body: { id: "op-1", done: true, error: { code: 8, message: ceiling } } },
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-old")).toBeDefined());
    addKey(region, "key-54");
    expect(await within(region).findByText(ceiling)).toBeInTheDocument();
    expect(within(region).getAllByRole("listitem")).toHaveLength(1);
    expect(within(region).getByRole("button", { name: "Добавить ключ доступа" })).toBeEnabled();
  });

  it("F8-70 · отказ формы имени: текст стоит у поля «Имя», общий invalid argument у поля не стоит, введённое сохранено", async () => {
    const rule = "name must match ^[a-z][-a-z0-9]{1,61}[a-z0-9]$";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: fieldRefusal("name", rule),
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await within(region).findByText("Ключей доступа нет");
    addKey(region, "Laptop", "Ноутбук");
    const name = within(region).getByLabelText("Имя");
    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(document.getElementById(name.getAttribute("aria-describedby")!)).toHaveTextContent(rule);
    expect(within(region).getByLabelText("Описание")).not.toHaveAttribute("aria-invalid");
    expect(region).not.toHaveTextContent("invalid argument");
    expect(lane.of("POST", KEYS)[0].body).toMatchObject({ name: "Laptop" });
    expect((name as HTMLInputElement).value).toBe("Laptop");
    expect(within(region).getByLabelText("Описание")).toHaveValue("Ноутбук");
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    expect(within(region).getByRole("button", { name: "Добавить ключ доступа" })).toBeEnabled();
  });

  it("F8-71 · отказ состояния испытания: текст службы у раздела, поля не отмечены, новое нажатие — новое испытание", async () => {
    const expired = "registration challenge expired: begin registration again";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: refusal(400, 9, expired, "CHALLENGE_EXPIRED"),
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await within(region).findByText("Ключей доступа нет");
    addKey(region, "key-71");
    expect(await within(region).findByText(expired)).toBeInTheDocument();
    expect(within(region).getByLabelText("Имя")).not.toHaveAttribute("aria-invalid");
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    fireEvent.click(within(region).getByRole("button", { name: "Добавить ключ доступа" }));
    await waitFor(() => expect(lane!.of("POST", BEGIN_REGISTRATION)).toHaveLength(2));
  });

  it("F8-59 · снятие единственного ключа раздел не запрещает: DELETE по id, после done перечень пуст", async () => {
    let revoked = false;
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: () => (revoked ? listOf() : listOf(keyRecord("ak-only", "key-only"))),
      [`DELETE ${KEYS}/ak-only`]: OPERATION_PENDING,
      "GET /operations/op-1": () => {
        revoked = true;
        return OPERATION_DONE;
      },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("key-only");
    expect(dialog).toHaveTextContent("войти им больше будет нельзя");
    fireEvent.click(within(dialog).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText("Ключей доступа нет")).toBeInTheDocument();
    expect(lane.of("DELETE", `${KEYS}/ak-only`)).toHaveLength(1);
  });

  it("F8-60 · отмена в диалоге ничего не выпускает", async () => {
    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")) });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("key-only");
    fireEvent.click(within(dialog).getByRole("button", { name: "Отмена" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(lane.calls.filter((c) => c.method === "DELETE")).toHaveLength(0);
    expect(keyItem(region, "key-only")).toBeDefined();
  });

  it("F8-58 · последний способ входа: синхронный отказ снятия дословно, операции нет, ключ и кнопка на месте", async () => {
    const last = "last sign-in method cannot be revoked: enrol another sign-in method first";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")),
      [`DELETE ${KEYS}/ak-only`]: refusal(400, 9, last, "LAST_SIGN_IN_METHOD"),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText(last)).toBeInTheDocument();
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    expect(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" })).toBeInTheDocument();
  });

  it("F8-69 · последний способ входа при гонке: отказ из error операции дословно, перечень перечитан, ключ на месте", async () => {
    const last = "last sign-in method cannot be revoked: enrol another sign-in method first";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")),
      [`DELETE ${KEYS}/ak-only`]: OPERATION_PENDING,
      "GET /operations/op-1": { status: 200, body: { id: "op-1", done: true, error: { code: 9, message: last } } },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText(last)).toBeInTheDocument();
    await waitFor(() => expect(lane!.of("GET", KEYS)).toHaveLength(2));
    expect(within(keyItem(region, "key-only")!).getByRole("button", { name: "Удалить" })).toBeInTheDocument();
  });

  it("F8-61 · перечень дочитывается до конца: второе обращение несёт pageToken; без токена обращение одно", async () => {
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: (call) =>
        new URLSearchParams(call.query).get("pageToken") === "page-2"
          ? listOf(keyRecord("ak-2", "key-second"))
          : { status: 200, body: { accessKeys: [keyRecord("ak-1", "key-first")], nextPageToken: "page-2" } },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    const first = render(
      <MemoryRouter initialEntries={["/settings"]}>
        <AccountSettingsPage />
      </MemoryRouter>,
    );
    let region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-second")).toBeDefined());
    expect(keyItem(region, "key-first")).toBeDefined();
    expect(lane.of("GET", KEYS).map((c) => new URLSearchParams(c.query).get("pageToken"))).toEqual([null, "page-2"]);
    first.unmount();
    lane.restore();

    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-first")) });
    renderPage();
    region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-first")).toBeDefined());
    expect(lane.of("GET", KEYS)).toHaveLength(1);
  });

  it("F8-48 · ключ показан именем, описанием и моментом заведения; не использованный — «Ещё не использовался»", async () => {
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(
        keyRecord("ak-1", "seed-f8-48", { description: "Рабочий ноутбук" }),
        keyRecord("ak-2", "key-used", { lastUsedAt: "2026-10-07T11:00:00Z" }),
      ),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "seed-f8-48")).toBeDefined());
    const fresh = keyItem(region, "seed-f8-48")!;
    expect(fresh).toHaveTextContent("Рабочий ноутбук");
    expect(fresh).toHaveTextContent("07.10.2026");
    expect(fresh).toHaveTextContent("Ещё не использовался");
    expect(keyItem(region, "key-used")).not.toHaveTextContent("Ещё не использовался");
    expect(within(region).getByRole("button", { name: "Добавить ключ доступа" })).toBeInTheDocument();
  });
});

// ═══ S2 — группа M. Первый пароль из живой сессии (Р13) ════════════════════

const ENROLL = "/iam/v1/auth/password/enroll";
const PASSWORD_BASE = {
  "GET /iam/v1/auth/me": SIGNED_IN,
  "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
  [`GET ${KEYS}`]: listOf(),
} as const;

async function openEnroll() {
  const password = await screen.findByRole("region", { name: "Пароль" });
  fireEvent.click(within(password).getByRole("button", { name: "У меня нет пароля — завести" }));
  return password;
}

describe("первый пароль на /settings (F8, S2, группа M)", () => {
  it("F8-62 · заведение первого пароля: форму выбрал человек, глагол получил ровно объявленное, затем снова смена", async () => {
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: { status: 200, body: { session: SESSION } } });
    renderPage();
    const password = await screen.findByRole("region", { name: "Пароль" });
    // До нажатия формы заведения нет: по умолчанию — смена (Р13).
    expect(within(password).getByLabelText("Текущий пароль")).toBeInTheDocument();
    expect(within(password).queryByRole("button", { name: "Завести пароль" })).toBeNull();
    await openEnroll();
    expect(within(password).queryByLabelText("Текущий пароль")).toBeNull();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-62" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    expect(await within(password).findByRole("status")).toHaveTextContent("Пароль заведён");
    expect(within(password).getByLabelText("Текущий пароль")).toBeInTheDocument();
    expect(lane.calls.some((c) => c.path === "/iam/v1/auth/csrf" && c.query === "?form=password-enroll")).toBe(true);
    expect(lane.of("POST", ENROLL)[0].body).toEqual({
      newPassword: "first-password-62",
      csrfToken: "tok-password-enroll-1",
    });
  });

  it("F8-63 · человек с паролем выбрал заведение: отказ службы дословно, и раздел снова показывает смену", async () => {
    const already = "password is already set; change it with the current password";
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: refusal(409, 6, already, "PASSWORD_ALREADY_SET") });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "second-password-63" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    expect(await within(password).findByText(already)).toBeInTheDocument();
    expect(within(password).getByLabelText("Текущий пароль")).toBeInTheDocument();
    expect(within(password).getByRole("button", { name: "Сменить пароль" })).toBeInTheDocument();
  });

  it("F8-64 · свежесть истекла: окно называет пароль, второй фактор и «Войти заново»; «Войти заново» — выход и вход с возвратом сюда", async () => {
    window.history.pushState({}, "", "/settings");
    const leave = jest.fn();
    lane = installLane({
      ...PASSWORD_BASE,
      [`POST ${ENROLL}`]: refusal(
        403,
        7,
        "re-authentication required: present a credential again",
        "SESSION_NOT_FRESH",
      ),
      "POST /iam/v1/auth/logout": { status: 200, body: {} },
    });
    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <AccountSettingsPage leave={leave} />
      </MemoryRouter>,
    );
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-64" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Паролем")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Вторым фактором")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Войти заново" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login?returnTo=%2Fsettings"));
    expect(lane.of("POST", "/iam/v1/auth/logout")).toHaveLength(1);
    expect(lane.of("POST", ENROLL)).toHaveLength(1);
    window.history.pushState({}, "", "/");
  });

  it("F8-64 · после повышения заведение повторено тем же паролем со свежим признаком; окно уровня «Войти заново» не несёт", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`POST ${ENROLL}`]: (_c, nth) =>
        nth === 1
          ? refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH")
          : { status: 200, body: { session: SESSION } },
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: { session: SESSION, assurance: { level: "1", level2Reachable: true, missingForLevel2: [] } },
      },
    });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-64" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Пароль"), { target: { value: "seed-pass" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
    expect(await within(password).findByRole("status")).toHaveTextContent("Пароль заведён");
    expect(lane.of("POST", ENROLL).map((c) => c.body)).toEqual([
      { newPassword: "first-password-64", csrfToken: "tok-password-enroll-1" },
      { newPassword: "first-password-64", csrfToken: "tok-password-enroll-2" },
    ]);
  });

  it("F8-64 · положительная сторона Р12: окно, открытое вызовом края на уровень, пути «Войти заново» не несёт", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`GET ${KEYS}`]: {
        status: 401,
        headers: {
          "WWW-Authenticate": 'Bearer realm="kacho", error="insufficient_user_authentication", acr_values="2"',
        },
        body: { code: 16, message: "insufficient user authentication" },
      },
    });
    renderPage();
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("button", { name: "Войти заново" })).toBeNull();
    expect(within(dialog).queryByLabelText("Паролем")).toBeNull();
  });

  it("F8-65 · правило пароля: поле «Новый пароль» отмечено текстом службы, ушло введённое как есть", async () => {
    const rule = "Illegal argument newPassword: must be at least 12 characters";
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: refusal(400, 3, rule) });
    renderPage();
    const password = await openEnroll();
    const next = within(password).getByLabelText("Новый пароль");
    fireEvent.change(next, { target: { value: "short" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    await waitFor(() => expect(next).toHaveAttribute("aria-invalid", "true"));
    expect(document.getElementById(next.getAttribute("aria-describedby")!)).toHaveTextContent(rule);
    // Отказ стоит у поля, и только там: второго окна отказа у раздела нет.
    expect(within(password).getAllByRole("alert")).toEqual([
      document.getElementById(next.getAttribute("aria-describedby")!),
    ]);
    expect(lane.of("POST", ENROLL)[0].body).toMatchObject({ newPassword: "short" });
  });

  it("F8-66 · служба не ответила: отказ назван и предложено повторить, форма заведения открыта, введённое цело", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`POST ${ENROLL}`]: refusal(503, 14, "request not performed; try again later"),
    });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-66" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const alert = await within(password).findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(within(password).getByLabelText("Новый пароль")).toHaveValue("first-password-66");
    expect(within(password).getByRole("button", { name: "Завести пароль" })).toBeInTheDocument();
  });

  it("F8-68 · отрицательная сторона: под отказом смены пароля (code 16) подсказки экрана входа нет", async () => {
    lane = installLane({ ...PASSWORD_BASE, "POST /iam/v1/auth/password": refusal(401, 16, "authentication failed") });
    renderPage();
    const password = await screen.findByRole("region", { name: "Пароль" });
    fireEvent.click(within(password).getByRole("button", { name: "Сменить пароль" }));
    expect(await within(password).findByRole("alert")).toHaveTextContent("authentication failed");
    expect(password).not.toHaveTextContent("Не получается войти?");
  });
});
