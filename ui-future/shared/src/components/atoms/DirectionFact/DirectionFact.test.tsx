// Направление правила названо СЛОВОМ, а не цветом.
//
// Прежде оно рисовалось цветным тегом: зелёный INGRESS, синий EGRESS. Цвет здесь
// не значит ничего — оба направления штатны, — но зелёный читается как «хорошо»,
// тогда как разрешающее входящее правило чаще всего и открывает лишнее.
//
// Проба держит три вещи: наружу выходит русское слово, а не машинный словарь;
// глиф ничего не сигнализирует цветом (тусклый тон обоих направлений); и
// незнакомое значение показывается как есть, а не подменяется одним из двух —
// последнее и есть близнец, без которого «всегда пишем слово» было бы верно за
// счёт того, что атом отвечает одинаково на любой вход.

import { render, screen } from "@testing-library/react";
import { DirectionFact } from "./DirectionFact";

describe("DirectionFact", () => {
  it("называет направление словом, а не машинным значением", () => {
    const { rerender } = render(<DirectionFact value="INGRESS" />);
    expect(screen.getByText("Входящий")).toBeInTheDocument();
    expect(screen.queryByText("INGRESS")).not.toBeInTheDocument();

    rerender(<DirectionFact value="egress" />);
    expect(screen.getByText("Исходящий")).toBeInTheDocument();
    expect(screen.queryByText(/EGRESS/i)).not.toBeInTheDocument();
  });

  it("оба направления одинаково штатны — цветом ни одно не выделено", () => {
    const { container: вход } = render(<DirectionFact value="INGRESS" />);
    const { container: выход } = render(<DirectionFact value="EGRESS" />);

    const цвет = (c: HTMLElement) => c.querySelector<HTMLElement>("[style*='color']")!.style.color;
    expect(цвет(вход)).toBe("var(--kc-text-tertiary)");
    expect(цвет(выход)).toBe(цвет(вход));
  });

  it("незнакомое значение показывается как есть (близнец)", () => {
    render(<DirectionFact value="SOMETHING_NEW" />);

    expect(screen.getByText("SOMETHING_NEW")).toBeInTheDocument();
    expect(screen.queryByText("Входящий")).not.toBeInTheDocument();
    expect(screen.queryByText("Исходящий")).not.toBeInTheDocument();
  });
});
