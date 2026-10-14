// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { UNKNOWN_SESSION_TEXT } from "@shared/api/login-lane";
import { installLane } from "@shared/test/lane-fake";

// Страница «не удалось узнать» стража подтверждённости адреса (#2955, вид «отказ
// не восстанавливает шаг»). Человек видит её, когда ответ о сессии не назвал
// подтверждённость адреса либо не пришёл по существу, — и с неё обязан понять,
// что делать дальше и к кому идти, словами, которые ему что-то говорят.

const { ADDRESS_STATE_NEXT_STEP, ADDRESS_STATE_NOT_NAMED_TEXT, AddressStateUnknownPage } = await import(
  "./AddressStateUnknownPage"
);

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

function renderPage(text: string, onRetry = jest.fn()) {
  lane = installLane({});
  const { container } = render(
    <MemoryRouter>
      <AddressStateUnknownPage text={text} onRetry={onRetry} leave={jest.fn()} />
    </MemoryRouter>,
  );
  return { container, onRetry };
}

describe("страница «признак адреса не назван»", () => {
  it.each([
    ["не названо", ADDRESS_STATE_NOT_NAMED_TEXT],
    ["неизвестно", UNKNOWN_SESSION_TEXT],
  ])("#2955 · %s: видимый текст без внутреннего слова «край»", (_name, text) => {
    const { container } = renderPage(text);
    expect(container.textContent ?? "").not.toMatch(/кра[йяюе]/i);
  });

  it.each([
    ["не названо", ADDRESS_STATE_NOT_NAMED_TEXT],
    ["неизвестно", UNKNOWN_SESSION_TEXT],
  ])("#2955 · %s: следующий шаг и адресат обращения названы", (_name, text) => {
    renderPage(text);
    expect(screen.getByText(ADDRESS_STATE_NEXT_STEP)).toBeInTheDocument();
    expect(ADDRESS_STATE_NEXT_STEP).toMatch(/Проверить снова/);
    expect(ADDRESS_STATE_NEXT_STEP).toMatch(/администратор/);
    // Шаг, названный текстом, есть и действием: кнопки с теми же именами.
    expect(screen.getByRole("button", { name: "Проверить снова" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Выйти" })).toBeEnabled();
  });

  it("положительный близнец: текст, переданный стражем, показан дословно, и «Проверить снова» спрашивает снова", () => {
    const { onRetry } = renderPage(ADDRESS_STATE_NOT_NAMED_TEXT);
    expect(screen.getByText(ADDRESS_STATE_NOT_NAMED_TEXT)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Проверить снова" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
