// ContextBadge — единственный источник разметки блока «плитка / действие /
// заголовок». Его смысл в том, что необязательные части ОТСУТСТВУЮТ, когда их не
// передали: пустой eyebrow или пустая плитка, отрисованные как пустой блок,
// смещают заголовок и дают тот самый «прыжок» между экранами, ради устранения
// которого блок и заведён.

import { render, screen } from "@testing-library/react";
import { ContextBadge } from "./ContextBadge";

describe("ContextBadge", () => {
  it("рисует заголовок", () => {
    render(<ContextBadge title="Подсети" />);
    expect(screen.getByText("Подсети")).toBeInTheDocument();
  });

  it("рисует действие, плитку и подзаголовок, когда они переданы", () => {
    render(
      <ContextBadge
        icon={<span data-testid="icon">◆</span>}
        eyebrow="Обзор"
        title="frontend-subnet"
        subtitle="10.0.1.0/24"
      />,
    );
    expect(screen.getByTestId("icon")).toBeInTheDocument();
    expect(screen.getByText("Обзор")).toBeInTheDocument();
    expect(screen.getByText("frontend-subnet")).toBeInTheDocument();
    expect(screen.getByText("10.0.1.0/24")).toBeInTheDocument();
  });

  it("не заводит узлов под непереданные части", () => {
    const { container } = render(<ContextBadge title="Только заголовок" />);
    // Контроль отсутствия: в блоке ровно обёртка + колонка текста + заголовок.
    // Пустая плитка или пустой eyebrow дали бы лишний div и сдвиг заголовка.
    expect(container.querySelectorAll("div")).toHaveLength(3);
  });

  it("держит плитку по верху, когда есть третья строка, и по центру, когда её нет", () => {
    // Выравнивание — не косметика: с подзаголовком блок трёхстрочный, и плитка,
    // центрированная по трём строкам, уезжает вниз относительно заголовка.
    const withSubtitle = render(<ContextBadge title="A" subtitle="B" />).container.firstElementChild as HTMLElement;
    expect(withSubtitle.style.alignItems).toBe("flex-start");

    const withoutSubtitle = render(<ContextBadge title="A" />).container.firstElementChild as HTMLElement;
    expect(withoutSubtitle.style.alignItems).toBe("center");
  });

  it("принимает узел, а не только строку, в заголовке", () => {
    render(<ContextBadge title={<em>сложный</em>} />);
    expect(screen.getByText("сложный").tagName).toBe("EM");
  });
});
