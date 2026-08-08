// PanelHeader — единая шапка секции: форма и таб detail-страницы обязаны
// получать ОДИН блок, иначе между экранами появляется «прыжок». Проверяется то,
// что от неё зависит: части рисуются, блок действий отсутствует целиком, когда
// действий нет (пустой контейнер справа съедает ширину заголовка), и контекст
// иконки доезжает до потомков.

import { render, screen } from "@testing-library/react";
import { DetailHeaderProvider, PanelHeader, useDetailHeaderIcon } from "./PanelHeader";

function IconProbe() {
  const icon = useDetailHeaderIcon();
  return <span data-testid="probe">{icon ?? "нет"}</span>;
}

describe("PanelHeader", () => {
  it("рисует иконку, вид, заголовок и подзаголовок", () => {
    render(
      <PanelHeader
        icon={<span data-testid="icon">◆</span>}
        eyebrow="Создание"
        title="Подсеть"
        subtitle="в сети frontend"
      />,
    );
    expect(screen.getByTestId("icon")).toBeInTheDocument();
    expect(screen.getByText("Создание")).toBeInTheDocument();
    expect(screen.getByText("Подсеть")).toBeInTheDocument();
    expect(screen.getByText("в сети frontend")).toBeInTheDocument();
  });

  it("рисует блок действий, когда он передан", () => {
    render(<PanelHeader title="Подсети" right={<button type="button">Создать</button>} />);
    expect(screen.getByRole("button", { name: "Создать" })).toBeInTheDocument();
  });

  it("без действий не заводит контейнера справа", () => {
    // Пустой контейнер получил бы `flex`-долю и сузил заголовок — то самое
    // расхождение между экранами, ради устранения которого шапка единая.
    const withRight = render(<PanelHeader title="A" right={<span>x</span>} />).container.firstElementChild!;
    const withoutRight = render(<PanelHeader title="A" />).container.firstElementChild!;
    expect(withRight.children).toHaveLength(2);
    expect(withoutRight.children).toHaveLength(1);
  });

  it("с подзаголовком выравнивает плитку по верху", () => {
    const withSubtitle = render(<PanelHeader title="A" subtitle="B" />).container.firstElementChild as HTMLElement;
    expect(withSubtitle.style.alignItems).toBe("flex-start");

    const plain = render(<PanelHeader title="A" />).container.firstElementChild as HTMLElement;
    expect(plain.style.alignItems).toBe("center");
  });
});

describe("useDetailHeaderIcon", () => {
  it("отдаёт иконку из провайдера", () => {
    render(
      <DetailHeaderProvider value={{ icon: "◆" }}>
        <IconProbe />
      </DetailHeaderProvider>,
    );
    expect(screen.getByTestId("probe")).toHaveTextContent("◆");
  });

  it("вне провайдера отдаёт пусто, а не падает", () => {
    // Шапка встречается и вне detail-страницы; бросок здесь уронил бы весь
    // экран из-за отсутствующей декорации.
    render(<IconProbe />);
    expect(screen.getByTestId("probe")).toHaveTextContent("нет");
  });
});
