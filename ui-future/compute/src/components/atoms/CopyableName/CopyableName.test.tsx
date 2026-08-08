// CopyableName — РЕНДЕР-тесты.
//
// Осознанно НЕ повторяют шаблон «открыть собственный исходник и убедиться, что в
// его тексте встречается имя экспорта» (kacho#5): такая проба читает файл как
// текст и проверяет, что он содержит собственное имя, — то есть зелена всегда.
// Доказано: компонент, бросающий при любом рендере, проходил её как `1 passed`.
//
// Здесь проверяется НАБЛЮДАЕМОЕ поведение: что видит пользователь и что попадает в
// clipboard. Сломанный компонент такие тесты валят.
//
// Имя приёма в этой шапке намеренно НЕ называется дословно: перечень долга в
// `internal/repohygiene/uisourcereadtest_test.go` ищет его по СЫРОМУ ТЕКСТУ и не
// отличает код от прозы о коде, поэтому объяснение запрета зачитывалось за его
// нарушение — и эта проба, ничего дурного не делавшая, числилась долгом.

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { CopyableName } from ".";

jest.mock("@/lib/toast", () => ({ toast: { success: jest.fn(), error: jest.fn() } }));

/** navigator.clipboard в jsdom отсутствует — подставляем перехватываемый writeText. */
function mockClipboard() {
  const writeText = jest.fn<(t: string) => Promise<void>>().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  return writeText;
}

describe("CopyableName", () => {
  it("показывает имя", () => {
    render(<CopyableName name="web-01" />);

    expect(screen.getByText("web-01")).toBeInTheDocument();
  });

  it("подставляет fallback, когда имя пустое", () => {
    // Смысл fallback — показать id безымянного ресурса, а не пустоту.
    render(<CopyableName name="" fallback="insabc123" />);

    expect(screen.getByText("insabc123")).toBeInTheDocument();
  });

  it("показывает (unnamed), когда нет ни имени, ни fallback", () => {
    render(<CopyableName name="" />);

    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
  });

  it("копирует имя в clipboard по клику", async () => {
    // ПОРЯДОК ВАЖЕН: userEvent.setup() ставит СВОЮ заглушку navigator.clipboard и
    // затирает нашу, если та установлена раньше → writeText не вызывается ни разу.
    const user = userEvent.setup();
    const writeText = mockClipboard();
    render(<CopyableName name="web-01" />);

    await user.click(screen.getByText("web-01"));

    expect(writeText).toHaveBeenCalledWith("web-01");
  });

  it("копирует ИМЕННО fallback, когда имя пустое", async () => {
    // Регрессия-лок: копировать надо то, что показано, а не пустую строку.
    // ПОРЯДОК ВАЖЕН: userEvent.setup() ставит СВОЮ заглушку navigator.clipboard и
    // затирает нашу, если та установлена раньше → writeText не вызывается ни разу.
    const user = userEvent.setup();
    const writeText = mockClipboard();
    render(<CopyableName name="" fallback="insabc123" />);

    await user.click(screen.getByText("insabc123"));

    expect(writeText).toHaveBeenCalledWith("insabc123");
  });
});
