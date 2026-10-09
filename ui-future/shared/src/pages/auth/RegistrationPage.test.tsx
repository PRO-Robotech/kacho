// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SIGNED_IN, installLane, refusal } from "@shared/test/lane-fake";

// Экран регистрации: человек заводит себя сам (приёмка F8, S1, группа C);
// «сначала письмо, потом сессия» — приёмка NTF-2, Р9, NTF2-80, NTF2-82.

const { RegistrationPage } = await import("./RegistrationPage");

function renderAt(url: string, leave = jest.fn<(to: string) => void>()) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <RegistrationPage leave={leave} />
    </MemoryRouter>,
  );
  return leave;
}

const email = () => screen.getByLabelText<HTMLInputElement>("Адрес электронной почты");
const password = () => screen.getByLabelText<HTMLInputElement>("Пароль");
const submit = () => screen.getByRole("button", { name: "Завести учётную запись" });

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("экран регистрации", () => {
  it("NTF2-80 · регистрация — два шага: register отвечает 200 {} и экран спрашивает код; код уходит register/confirm, и только он уводит в консоль", async () => {
    // verifies #2917
    //
    // Приёмка NTF-2, Р9 «сначала письмо, потом сессия» замещает F8-14 (сессия
    // сразу): `register` сессии не выдаёт, и уход в консоль на его ответе был бы
    // уходом без сессии.
    lane = installLane({
      "POST /iam/v1/auth/register": { status: 200, body: {} },
      "POST /iam/v1/auth/register/confirm": SIGNED_IN,
    });
    const leave = renderAt("/registration");
    fireEvent.change(email(), { target: { value: "new@kacho.local" } });
    fireEvent.change(password(), { target: { value: "Kacho-E2E-2026!x" } });
    fireEvent.click(submit());

    const code = await screen.findByLabelText<HTMLInputElement>(/код/i);
    expect(leave).not.toHaveBeenCalled();
    expect(lane.of("POST", "/iam/v1/auth/register")[0].body).toEqual({
      email: "new@kacho.local",
      password: "Kacho-E2E-2026!x",
      csrfToken: "tok-register-1",
    });
    expect(lane.of("POST", "/iam/v1/auth/register/confirm")).toHaveLength(0);

    fireEvent.change(code, { target: { value: "123456" } });
    fireEvent.submit(code.closest("form")!);
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    expect(lane.of("POST", "/iam/v1/auth/register/confirm")[0].body).toEqual({
      email: "new@kacho.local",
      code: "123456",
      password: "Kacho-E2E-2026!x",
      csrfToken: "tok-register-confirm-1",
    });
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("NTF2-82 · неверный код — отказ службы дословно, в консоль не уводит, код можно ввести снова", async () => {
    // verifies #2917
    lane = installLane({
      "POST /iam/v1/auth/register": { status: 200, body: {} },
      "POST /iam/v1/auth/register/confirm": refusal(401, 16, "authentication failed"),
    });
    const leave = renderAt("/registration");
    fireEvent.change(email(), { target: { value: "new@kacho.local" } });
    fireEvent.change(password(), { target: { value: "Kacho-E2E-2026!x" } });
    fireEvent.click(submit());
    const code = await screen.findByLabelText<HTMLInputElement>(/код/i);
    fireEvent.change(code, { target: { value: "000000" } });
    fireEvent.submit(code.closest("form")!);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("authentication failed");
    expect(leave).not.toHaveBeenCalled();
    expect(screen.getByLabelText<HTMLInputElement>(/код/i)).toBeEnabled();
  });

  it("F8-15 · отказ показан ДОСЛОВНО и ни словом больше — занят ли адрес, экран не говорит", async () => {
    lane = installLane({
      "POST /iam/v1/auth/register": refusal(400, 9, "registration refused", "REGISTRATION_REFUSED"),
    });
    const leave = renderAt("/registration");
    fireEvent.change(email(), { target: { value: "taken@kacho.local" } });
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("registration refused");
    expect(leave).not.toHaveBeenCalled();
  });

  it("F8-16 · правило пароля судит служба: отмечено поле, которое она назвала", async () => {
    const message = "Illegal argument password: shorter than the declared minimum length";
    lane = installLane({
      "POST /iam/v1/auth/register": refusal(400, 3, message),
    });
    renderAt("/registration");
    fireEvent.change(email(), { target: { value: "new@kacho.local" } });
    fireEvent.change(password(), { target: { value: "abc" } });
    fireEvent.click(submit());
    await screen.findByText(message);
    expect(password()).toHaveAttribute("aria-invalid", "true");
    expect(email()).not.toHaveAttribute("aria-invalid");
    // Своего правила экран не применял: короткий пароль ушёл службе.
    expect(lane.of("POST", "/iam/v1/auth/register")[0].body).toMatchObject({
      password: "abc",
    });
  });

  it("путь ко входу есть и несёт адрес возврата", () => {
    lane = installLane({});
    renderAt("/registration?returnTo=%2Fiam%2Fusers");
    const link = screen.getByRole<HTMLAnchorElement>("link", {
      name: "Уже есть учётная запись — войти",
    });
    expect(new URL(link.href).pathname).toBe("/login");
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/iam/users");
  });
});
