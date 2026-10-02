// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SIGNED_IN, installLane, refusal } from "@shared/test/lane-fake";

// Экран регистрации: человек заводит себя сам (приёмка F8, S1, группа C).

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
  it("F8-14 · регистрация зовёт НАШ глагол с признаком своего вида и уводит в консоль", async () => {
    lane = installLane({ "POST /iam/v1/auth/register": SIGNED_IN });
    const leave = renderAt("/registration");
    fireEvent.change(email(), { target: { value: "new@kacho.local" } });
    fireEvent.change(password(), { target: { value: "Kacho-E2E-2026!x" } });
    fireEvent.click(submit());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    expect(lane.of("POST", "/iam/v1/auth/register")[0].body).toEqual({
      email: "new@kacho.local",
      password: "Kacho-E2E-2026!x",
      csrfToken: "tok-register-1",
    });
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
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
    lane = installLane({ "POST /iam/v1/auth/register": refusal(400, 3, message) });
    renderAt("/registration");
    fireEvent.change(email(), { target: { value: "new@kacho.local" } });
    fireEvent.change(password(), { target: { value: "abc" } });
    fireEvent.click(submit());
    await screen.findByText(message);
    expect(password()).toHaveAttribute("aria-invalid", "true");
    expect(email()).not.toHaveAttribute("aria-invalid");
    // Своего правила экран не применял: короткий пароль ушёл службе.
    expect(lane.of("POST", "/iam/v1/auth/register")[0].body).toMatchObject({ password: "abc" });
  });

  it("путь ко входу есть и несёт адрес возврата", () => {
    lane = installLane({});
    renderAt("/registration?returnTo=%2Fiam%2Fusers");
    const link = screen.getByRole<HTMLAnchorElement>("link", { name: "Уже есть учётная запись — войти" });
    expect(new URL(link.href).pathname).toBe("/login");
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/iam/users");
  });
});
