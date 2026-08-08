// Поле, которое править нельзя: неизменяемое после создания (core #15 — id и
// прочие адресуемые координаты) либо заданное контекстом. Требования: значение
// ВИДНО (спрятать его значило бы скрыть от пользователя то, чем ресурс
// адресуется), поле недоступно для ввода, и рядом есть признак блокировки —
// иначе поле читается как обычное, просто «не работающее».

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";

// antd переопределён локально: общий заменитель `Input` не рисует `suffix`, а
// замок живёт именно там — без него признак блокировки ненаблюдаем.
jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Input: Object.assign(
    ({ suffix, ...p }: Record<string, unknown> & { suffix?: React.ReactNode }) =>
      React.createElement("span", null, React.createElement("input", p), suffix),
    {
      TextArea: (p: Record<string, unknown>) => React.createElement("textarea", p),
      Search: (p: Record<string, unknown>) => React.createElement("input", { type: "search", ...p }),
    },
  ),
}));

const { ImmutableField } = await import("./ImmutableField");

describe("ImmutableField", () => {
  it("строковое значение показывает и не даёт править", () => {
    render(<ImmutableField value="net01h9zt6k3mqx4v" reason="Неизменяемо после создания" />);

    const input = screen.getByRole<HTMLInputElement>("textbox");
    expect(input.value).toBe("net01h9zt6k3mqx4v");
    expect(input).toBeDisabled();
  });

  it("числовое значение не теряется", () => {
    render(<ImmutableField value={65535} reason="Задано из контекста" />);
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe("65535");
  });

  it("несёт признак блокировки", () => {
    render(<ImmutableField value="net-1" reason="Неизменяемо после создания" />);
    expect(screen.getByLabelText("immutable-lock")).toBeInTheDocument();
  });

  it("пустое значение показывает прочерком, а не пустотой", () => {
    // Пустое поле без метки неотличимо от «поле есть, значение не загрузилось».
    render(<ImmutableField value="" reason="Задано из контекста" />);
    const input = screen.getByRole<HTMLInputElement>("textbox");
    expect(input.value).toBe("");
    expect(input).toHaveAttribute("placeholder", "—");
  });

  it("узел (ссылка/тег) показывает как есть и тоже помечает замком", () => {
    render(<ImmutableField value={<a href="/vpc/v1/networks/net-1">core</a>} reason="Неизменяемо после создания" />);

    expect(screen.getByRole("link", { name: "core" })).toBeInTheDocument();
    expect(screen.getByLabelText("immutable-lock")).toBeInTheDocument();
    // Это не поле ввода: править нечего, и вводить туда нельзя вовсе.
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("отсутствующий узел тоже даёт прочерк", () => {
    render(<ImmutableField value={null} reason="Задано из контекста" />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
