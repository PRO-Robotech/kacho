// Просмотр ответа сервера как JSON. Ценность вкладки ровно в одном: показывать
// то, что ПРИШЛО, целиком и только для чтения. Редактируемый просмотр создавал
// бы иллюзию правки, которой некуда уехать (вкладка не знает ни пути, ни маски),
// а несериализованные данные — иллюзию пустого ответа.
//
// antd переопределён локально: общий стенд не отдаёт `ConfigProvider`, на
// котором стоит поставщик темы, и модуль не линкуется вовсе. Редактор Monaco
// подменён общим стендом простым узлом с теми же свойствами — на нём и текст, и
// режим наблюдаемы.

import { jest } from "@jest/globals";
import React from "react";
import { render } from "@testing-library/react";

jest.unstable_mockModule("antd", () => ({
  __esModule: true,
  ConfigProvider: ({ children }: React.PropsWithChildren) => React.createElement(React.Fragment, null, children),
  theme: {
    useToken: () => ({ token: { colorBorderSecondary: "#2f3138", borderRadius: 8 } }),
    defaultAlgorithm: () => ({}),
    darkAlgorithm: () => ({}),
  },
}));

const { ThemeProvider } = await import("@shared/lib/theme-context");
const { JsonMonacoView } = await import("./JsonMonacoView");

/** Подменённый редактор — единственный узел, несущий свойство `value`. */
function editorNode(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>("[value]");
  if (!el) throw new Error("узел редактора не найден");
  return el;
}

function renderView(ui: React.ReactElement) {
  return render(<ThemeProvider>{ui}</ThemeProvider>);
}

describe("JsonMonacoView", () => {
  it("сериализует данные с отступами", () => {
    const { container } = renderView(<JsonMonacoView data={{ id: "net-1", name: "core" }} />);
    expect(editorNode(container).getAttribute("value")).toBe('{\n  "id": "net-1",\n  "name": "core"\n}');
  });

  it("объявляет язык, чтобы подсветка была JSON, а не текстом", () => {
    const { container } = renderView(<JsonMonacoView data={{}} />);
    expect(editorNode(container).getAttribute("defaultLanguage")).toBe("json");
  });

  it("высота по умолчанию задана и переопределяется вызывающим", () => {
    const def = renderView(<JsonMonacoView data={{}} />);
    expect(editorNode(def.container).getAttribute("height")).toBe("60vh");

    const custom = renderView(<JsonMonacoView data={{}} height={240} />);
    expect(editorNode(custom.container).getAttribute("height")).toBe("240");
  });

  it("пустой ответ показывает как пустой объект, а не как пустоту", () => {
    const { container } = renderView(<JsonMonacoView data={{}} />);
    expect(editorNode(container).getAttribute("value")).toBe("{}");
  });

  it("различает отсутствие поля и присланный null", () => {
    // `null` — значение, которое сервер прислал; `undefined` — поля не было.
    // Показать их одинаково значило бы соврать о содержимом ответа.
    const { container } = renderView(<JsonMonacoView data={{ a: null, b: undefined }} />);
    expect(editorNode(container).getAttribute("value")).toBe('{\n  "a": null\n}');
  });

  it("не пересобирает строку, пока данные те же", () => {
    // Вкладка живёт на поллящемся запросе (3-5 с); пересборка на каждый ответ
    // перекармливала бы редактор новой строкой без причины.
    const data = { id: "net-1" };
    const { container, rerender } = render(
      <ThemeProvider>
        <JsonMonacoView data={data} />
      </ThemeProvider>,
    );
    const before = editorNode(container).getAttribute("value");

    rerender(
      <ThemeProvider>
        <JsonMonacoView data={data} />
      </ThemeProvider>,
    );

    expect(editorNode(container).getAttribute("value")).toBe(before);
  });
});
