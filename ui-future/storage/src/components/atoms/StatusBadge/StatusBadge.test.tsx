// StatusBadge (storage) — РЕНДЕР-тесты.
//
// Осознанно НЕ повторяют шаблон `readFileSync(исходник) + expect(source).toContain("Имя")`,
// которым написаны 103 из 143 UI-тестов (kacho#5): такой тест зелен всегда — доказано,
// компонент, бросающий при любом рендере, проходил его как «1 passed».
//
// Проверяется наблюдаемое: что видит пользователь (текст и ТОН статуса).

import { render, screen } from "@testing-library/react";
import { StatusBadge } from ".";

/** Тон читаем из inline-style: TONE_STYLE задаёт цвет, класс у всех тонов один. */
function toneOf(el: HTMLElement): string {
  return el.getAttribute("style") ?? "";
}

describe("StatusBadge (storage)", () => {
  it("показывает — при отсутствии статуса", () => {
    render(<StatusBadge />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("нормализует префикс STATUS_", () => {
    // Бэкенд отдаёт и голое имя, и STATUS_-форму; пользователю префикс не нужен.
    render(<StatusBadge state="STATUS_IN_USE" />);

    expect(screen.getByText("In_use")).toBeInTheDocument();
  });

  it("нормализует префикс STATE_", () => {
    render(<StatusBadge state="STATE_CREATING" />);

    expect(screen.getByText("Creating")).toBeInTheDocument();
  });

  it("AVAILABLE — здоровый статус, тон обязан отличаться от muted", () => {
    // РЕГРЕССИЯ-ЛОК (kacho#7): AVAILABLE — основной «здоровый» статус тома
    // (api/types.ts: CREATING|AVAILABLE|IN_USE|DELETING|ERROR), но в TONE_BY_STATUS его
    // не было → он падал в fallback "muted", то есть доступный том выглядел
    // НЕАКТИВНЫМ (тем же тоном, что STOPPED/RELEASED). Сравниваем с заведомо muted-
    // статусом: тона обязаны различаться.
    render(<StatusBadge state="AVAILABLE" />);
    const available = screen.getByText("Available");

    render(<StatusBadge state="RELEASED" />);
    const muted = screen.getByText("Released");

    expect(toneOf(available)).not.toEqual(toneOf(muted));
  });

  it("AVAILABLE и READY — один тон (оба здоровые)", () => {
    render(<StatusBadge state="AVAILABLE" />);
    const available = screen.getByText("Available");

    render(<StatusBadge state="READY" />);
    const ready = screen.getByText("Ready");

    expect(toneOf(available)).toEqual(toneOf(ready));
  });

  it("неизвестный статус не роняет рендер (fallback)", () => {
    render(<StatusBadge state="WAT_IS_THIS" />);

    expect(screen.getByText("Wat_is_this")).toBeInTheDocument();
  });
});
