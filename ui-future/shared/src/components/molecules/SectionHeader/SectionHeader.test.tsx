// SectionHeader существует ради ОДНОГО свойства: иконка ресурса берётся из
// контекста detail-страницы, а не выписывается на каждом месте вызова. Если
// контекст перестанет читаться, все шапки табов молча останутся без иконки —
// внешне это выглядит как решение дизайнера, а не как поломка. Поэтому проба
// утверждает и наследование, и приоритет явного значения, и работу вне
// контекста.

import { render, screen } from "@testing-library/react";
import { DetailHeaderProvider } from "@shared/components/molecules/PanelHeader";
import { SectionHeader } from "./SectionHeader";

describe("SectionHeader", () => {
  it("рисует заголовок и действия справа", () => {
    render(<SectionHeader title="Обзор подсети" right={<button type="button">Изменить</button>} />);
    expect(screen.getByText("Обзор подсети")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Изменить" })).toBeInTheDocument();
  });

  it("берёт иконку ресурса из контекста detail-страницы", () => {
    render(
      <DetailHeaderProvider value={{ icon: <span data-testid="ctx-icon">S</span> }}>
        <SectionHeader title="Обзор" />
      </DetailHeaderProvider>,
    );
    expect(screen.getByTestId("ctx-icon")).toBeInTheDocument();
  });

  it("явная иконка перебивает контекстную", () => {
    // Related-таб показывает иконку ДОЧЕРНЕГО ресурса; если приоритет
    // перевернуть, вкладка «Адреса» подсети получит иконку подсети.
    render(
      <DetailHeaderProvider value={{ icon: <span data-testid="ctx-icon">S</span> }}>
        <SectionHeader title="Адреса" icon={<span data-testid="own-icon">A</span>} />
      </DetailHeaderProvider>,
    );
    expect(screen.getByTestId("own-icon")).toBeInTheDocument();
    expect(screen.queryByTestId("ctx-icon")).not.toBeInTheDocument();
  });

  it("вне detail-страницы работает без иконки, а не падает", () => {
    const { container } = render(<SectionHeader title="Сети" eyebrow="Список" />);
    expect(screen.getByText("Сети")).toBeInTheDocument();
    // Плитки нет — блок остаётся из одной колонки текста, без пустого места
    // под иконку.
    expect(container.querySelectorAll("svg")).toHaveLength(0);
  });

  it("показывает вид секции над заголовком", () => {
    render(<SectionHeader title="frontend-subnet" eyebrow="Операции" />);
    expect(screen.getByText("Операции")).toBeInTheDocument();
    expect(screen.getByText("frontend-subnet")).toBeInTheDocument();
  });
});
