// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
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

  it("F8-25 · отказ края на глаголе с носителем назван, экран остаётся на месте", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/password": { status: 401, body: { code: 16, message: "session ended; sign in again" } },
    });
    renderPage();
    const password = await screen.findByRole("region", { name: "Пароль" });
    fireEvent.click(within(password).getByRole("button", { name: "Сменить пароль" }));
    expect(await within(password).findByRole("alert")).toHaveTextContent("session ended; sign in again");
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
    expect(lane.of("POST", "/iam/v1/auth/second-factor/confirm")[0].body).toEqual({
      code: "123456",
      csrfToken: "tok-second-factor-1",
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

  it("без сессии — путь ко входу с возвратом сюда, и ни одной формы", async () => {
    lane = installLane({});
    renderPage();
    const link = await screen.findByRole<HTMLAnchorElement>("link", { name: "Войти" });
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/settings");
    expect(screen.queryByRole("region", { name: "Пароль" })).toBeNull();
  });
});
