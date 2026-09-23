// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SIGNED_IN, installLane, refusal } from "@shared/test/lane-fake";

// Экран входа: что он делает на каждом исходе глагола (приёмка F8, S1). Сеть —
// дублёр полосы с телами службы; экран — настоящий. Сквозная проба того же
// предмета — `ui-future/e2e/specs/identity-ceremony.spec.ts`; здесь закреплены
// свойства, которые браузерная проба видит только исходом.

const { LoginPage } = await import("./LoginPage");

function renderAt(url: string, leave = jest.fn<(to: string) => void>()) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <LoginPage leave={leave} />
    </MemoryRouter>,
  );
  return leave;
}

const email = () => screen.getByLabelText<HTMLInputElement>("Адрес электронной почты");
const password = () => screen.getByLabelText<HTMLInputElement>("Пароль");
const submit = () => screen.getByRole("button", { name: "Войти" });

async function formShown() {
  await screen.findByRole("form", { name: "Вход в консоль" });
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("экран входа", () => {
  it("F8-04 · вход зовёт НАШ глагол с признаком своего вида и уводит на адрес возврата", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": SIGNED_IN });
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.change(password(), { target: { value: "p" } });
    fireEvent.click(submit());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    const [posted] = lane.of("POST", "/iam/v1/auth/login");
    expect(posted.body).toEqual({ email: "a@kacho.local", password: "p", csrfToken: "tok-login-1" });
    expect(lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query)).toEqual(["?form=login"]);
    // Отрицание в паре с положительным выше: ни одного обращения мимо полосы.
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("F8-06 · поле, названное службой, отмечено — и только оно; своего правила экран не применял", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": refusal(400, 3, "Illegal argument password: required"),
    });
    renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(submit());
    await screen.findByText("Illegal argument password: required");
    expect(password()).toHaveAttribute("aria-invalid", "true");
    expect(email()).not.toHaveAttribute("aria-invalid");
    // Пустой пароль ушёл службе — консоль его не задержала (Р2).
    expect(lane.of("POST", "/iam/v1/auth/login")[0].body).toMatchObject({ password: "" });
  });

  it("F8-07 · отказ о поле, которого на форме нет, назван предупреждением, а не пустым экраном", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(400, 3, "Illegal argument csrfToken: required") });
    renderAt("/login");
    await formShown();
    fireEvent.click(submit());
    expect(await screen.findByRole("alert")).toHaveTextContent("Illegal argument csrfToken: required");
  });

  it("F8-08 · отвергнутый признак формы: экран СРАЗУ добывает свежий, и повторная отправка несёт его", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": (_c, nth) =>
        nth === 1 ? refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED") : SIGNED_IN,
    });
    const leave = renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.change(password(), { target: { value: "p" } });
    fireEvent.click(submit());
    expect(await screen.findByRole("alert")).toHaveTextContent("form token rejected");
    await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/csrf")).toHaveLength(2));
    expect(email().value).toBe("a@kacho.local");
    fireEvent.click(submit());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    expect(lane.of("POST", "/iam/v1/auth/login")[1].body).toMatchObject({ csrfToken: "tok-login-2" });
  });

  it("F8-09 · отказ по частоте: срок назван числом из Retry-After, отправка закрыта", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": {
        ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
        headers: { "Retry-After": "897" },
      },
    });
    renderAt("/login");
    await formShown();
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).toHaveTextContent("Повторить можно через 897 с.");
    expect(submit()).toBeDisabled();
    // Отправка формы мимо кнопки — тоже закрыта: обращений не прибавилось.
    act(() => {
      fireEvent.submit(screen.getByRole("form", { name: "Вход в консоль" }));
    });
    expect(lane.of("POST", "/iam/v1/auth/login")).toHaveLength(1);
  });

  it("F8-10 · служба не ответила: текст службы, приглашение повторить, введённое цело", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(503, 14, "request not performed; try again later") });
    renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(submit()).toBeEnabled();
    expect(email().value).toBe("a@kacho.local");
  });

  it("F8-11 · у человека с живой сессией формы нет — он уходит на адрес возврата", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": SIGNED_IN });
    const leave = renderAt("/login?returnTo=/dashboard");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(screen.queryByLabelText("Адрес электронной почты")).toBeNull();
  });

  it("F8-12 · негодный носитель — та же форма, что без сессии, и экран о причине молчит", async () => {
    // Край отвечает на негодный носитель отказом; для экрана это «сессии нет».
    lane = installLane({ "GET /iam/v1/auth/me": refusal(401, 16, "session ended; sign in again") });
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    expect(leave).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("F8-13 · чужой адрес возврата отвергнут во всех четырёх формах, свой — соблюдён", async () => {
    for (const [returnTo, expected] of [
      ["https://evil.example/dashboard", "/"],
      ["//evil.example/dashboard", "/"],
      ["/\\evil.example/dashboard", "/"],
      ["javascript:alert(1)", "/"],
      ["/dashboard?f8-13=kontrol", "/dashboard?f8-13=kontrol"],
    ]) {
      lane = installLane({ "POST /iam/v1/auth/login": SIGNED_IN });
      const leave = jest.fn<(to: string) => void>();
      const { unmount } = render(
        <MemoryRouter initialEntries={[`/login?returnTo=${encodeURIComponent(returnTo)}`]}>
          <LoginPage leave={leave} />
        </MemoryRouter>,
      );
      await formShown();
      fireEvent.click(submit());
      await waitFor(() => expect(leave).toHaveBeenCalled());
      expect([returnTo, leave.mock.calls[0][0]]).toEqual([returnTo, expected]);
      unmount();
      lane.restore();
    }
  });

  it("F8-39 · пути на восстановление и подтверждение адреса нет, на регистрацию — есть", async () => {
    lane = installLane({});
    renderAt("/login");
    await formShown();
    const destinations = screen.getAllByRole("link").map((a) => new URL((a as HTMLAnchorElement).href).pathname);
    expect(destinations).not.toContain("/recovery");
    expect(destinations).not.toContain("/verification");
    expect(destinations).toContain("/registration");
  });

  it("F8-40 · экран отказа — функция ТОЛЬКО тела ответа: заведён адрес или нет, экран один", async () => {
    const screens: string[] = [];
    for (const address of ["known@kacho.local", "unknown@kacho.local"]) {
      lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
      const { container, unmount } = render(
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage leave={jest.fn()} />
        </MemoryRouter>,
      );
      await formShown();
      fireEvent.change(email(), { target: { value: address } });
      fireEvent.click(submit());
      await screen.findByRole("alert");
      screens.push(screen.getByRole("alert").textContent ?? "");
      // Поля формы те же; значение адреса — введённое самим человеком.
      screens.push(String(container.querySelectorAll("input").length));
      unmount();
      lane.restore();
    }
    expect(screens[0]).toBe("authentication failed");
    expect(screens[0]).toBe(screens[2]);
    expect(screens[1]).toBe(screens[3]);
  });
});
