// Строка списка одной высоты у всех, а край таблицы — линия, а не тень.
//
// Оба свойства ломаются молча и оба сломаны были: идентификатор рвался посреди
// себя на две строки, длинное описание поднимало свою строку над соседними, а у
// правого края стоял тёмный мазок — внутренняя тень закреплённого столбца.
//
// Часть решения уезжает в объявление колонки (`onCell`/`onHeaderCell`) и в тему
// (`ConfigProvider`), поэтому заменитель здесь ДОПОЛНЕН: он рисует таблицу как
// прежде и вдобавок запоминает то, что таблица объявила antd. Без этого
// утверждать было бы не о чем — заменитель эти свойства роняет, и проба зеленела
// бы при снятой тени и при оставленной одинаково.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ThemeConfig } from "antd";
import { antdStub } from "@shared/test/antd-stub";

interface CapturedColumn {
  title?: React.ReactNode;
  width?: number | string;
  fixed?: "left" | "right";
  onCell?: () => { style?: React.CSSProperties };
  onHeaderCell?: () => { style?: React.CSSProperties };
}

const captured: {
  columns: CapturedColumn[];
  theme?: ThemeConfig;
  scroll?: { x?: unknown; y?: unknown };
} = { columns: [] };

jest.unstable_mockModule("antd", () => {
  const base = antdStub();
  const BaseTable = base.Table as React.FC<Record<string, unknown>>;
  return {
    ...base,
    Table: (props: Record<string, unknown>) => {
      captured.columns = (props.columns ?? []) as CapturedColumn[];
      captured.scroll = props.scroll as { x?: unknown; y?: unknown } | undefined;
      return React.createElement(BaseTable, props);
    },
    ConfigProvider: ({ theme, children }: { theme?: ThemeConfig; children?: React.ReactNode }) => {
      captured.theme = theme;
      return React.createElement(React.Fragment, null, children);
    },
  };
});

const { ResourceTable } = await import("./ResourceTable");
const { CELL_CLIP_CLASS, CELL_MAX_WIDTH, CELL_INSET } = await import("./cellClip");
type Column<T> = import("./ResourceTable").Column<T>;

interface Row {
  id: string;
  name: string;
  description: string;
}

const rows: Row[] = [
  { id: "rtb6qc1500147672jdmp", name: "core", description: "очень длинное описание таблицы маршрутов" },
  { id: "rtb6qc1500147672jdmq", name: "edge", description: "короткое" },
];

function cols(): Column<Row>[] {
  return [
    { header: "Имя", cell: (r) => <span>{r.name}</span> },
    { header: "Идентификатор", cell: (r) => <span>{r.id}</span> },
    { header: "Описание", cell: (r) => <span>{r.description}</span> },
    { header: "", cell: () => <button type="button">⋯</button> },
  ];
}

function renderTable() {
  return render(<ResourceTable rows={rows} columns={cols()} rowKey={(r) => r.id} complete />);
}

function clips(): HTMLElement[] {
  return Array.from(document.querySelectorAll<HTMLElement>(`.${CELL_CLIP_CLASS}`));
}

describe("клетка данных — одна строка у КАЖДОЙ строки и КАЖДОЙ колонки", () => {
  it("обёрнута каждая клетка данных, и ни одна не пропущена", () => {
    renderTable();
    // Три колонки данных на две строки — минус клетка ИДЕНТИЧНОСТИ каждой
    // строки: она живёт в две строки намеренно (имя, под ним идентификатор), и
    // обёртка одной строки прятала бы вторую целиком. Число названо, чтобы
    // «обёрнута хотя бы одна» не читалось как «обёрнуты все»: дефект был именно
    // у части колонок.
    expect(clips()).toHaveLength(4);
  });

  it("клетка идентичности НЕ обёрнута одной строкой — в ней две", () => {
    // Положительный близнец числа выше: без него «стало четыре вместо шести»
    // читалось бы как потеря обёртки у произвольных двух клеток.
    renderTable();
    expect(screen.getByText("core").closest(`.${CELL_CLIP_CLASS}`)).toBeNull();
  });

  it("служебный столбец действий НЕ обёрнут", () => {
    // Положительный контроль формы: без него утверждение выше зеленело бы и на
    // таблице, обернувшей вообще всё, — а кнопка действий выше строки текста, и
    // обёртка одной строки срезала бы ей края.
    renderTable();
    const action = screen.getAllByRole("button", { name: "⋯" })[0];
    expect(action.closest(`.${CELL_CLIP_CLASS}`)).toBeNull();
  });

  it("значение не переносится — идентификатор одной строкой", () => {
    renderTable();
    const cell = screen.getByText("rtb6qc1500147672jdmp").closest(`.${CELL_CLIP_CLASS}`) as HTMLElement;
    expect(cell.style.whiteSpace).toBe("nowrap");
    expect(cell.style.textOverflow).toBe("ellipsis");
  });

  it("предел ширины у закреплённой колонки берётся из её ширины, у обычной — общий", () => {
    renderTable();
    const plain = screen.getByText("короткое").closest(`.${CELL_CLIP_CLASS}`) as HTMLElement;
    // Клетка идентичности обёрткой одной строки не пользуется (в ней две), но
    // предел ширины у неё тот же и объявлен там же — иначе закрепление
    // разъехалось бы с показанным.
    const identity = screen.getByText("core").closest("span[style]") as HTMLElement;
    expect(identity.style.maxWidth).toBe(`${260 - CELL_INSET}px`);
    expect(plain.style.maxWidth).toBe(`${CELL_MAX_WIDTH}px`);
  });

  it("наведение на клетку внутри таблицы договаривает обрезанное", () => {
    // Провязка: обработчик стоит на поверхности таблицы, а не на каждой клетке.
    // Без этой пробы модуль клетки был бы проверен, а таблица могла бы его не
    // звать вовсе.
    renderTable();
    const cell = screen.getByText("очень длинное описание таблицы маршрутов").closest(
      `.${CELL_CLIP_CLASS}`,
    ) as HTMLElement;
    Object.defineProperty(cell, "scrollWidth", { value: 900, configurable: true });
    Object.defineProperty(cell, "clientWidth", { value: 300, configurable: true });

    fireEvent.mouseOver(cell);

    expect(cell).toHaveAttribute("title", "очень длинное описание таблицы маршрутов");
  });
});

describe("край таблицы — линия, а не тень", () => {
  it("тень закреплённой колонки снята темой", () => {
    renderTable();
    expect(captured.theme?.components?.Table?.colorSplit).toBe("transparent");
  });

  it("стык закрепления назван линией — и в шапке, и в теле", () => {
    renderTable();
    const identity = captured.columns[0];
    const actions = captured.columns[captured.columns.length - 1];
    expect(identity.onCell?.().style?.borderInlineEnd).toBe("1px solid var(--kc-border)");
    expect(identity.onHeaderCell?.().style?.borderInlineEnd).toBe("1px solid var(--kc-border)");
    expect(actions.onCell?.().style?.borderInlineStart).toBe("1px solid var(--kc-border)");
    expect(actions.onHeaderCell?.().style?.borderInlineStart).toBe("1px solid var(--kc-border)");
  });

  it("колонка ВНУТРИ прокручиваемой части линии не получает", () => {
    // Контроль в обратную сторону: линия сообщает про стык, а не разлиновывает
    // таблицу. Без этой пробы утверждения выше зеленели бы и на таблице, где
    // граница стоит у каждой колонки.
    renderTable();
    const middle = captured.columns[1];
    expect(middle.onCell?.().style?.borderInlineStart).toBeUndefined();
    expect(middle.onCell?.().style?.borderInlineEnd).toBeUndefined();
  });

  it("прокрутка вбок принадлежит таблице, а не странице", () => {
    // Утверждение об ОБЪЯВЛЕНИИ, и это сказано прямо: раскладку jsdom не
    // считает, поэтому «полоса появилась» здесь проверить нечем. Объявление
    // тем не менее несущее — сняв его, широкая таблица потянула бы вбок всю
    // страницу вместе с рейлом модулей и панелью типов.
    renderTable();
    expect(captured.scroll?.x).toBe("max-content");
  });

  it("клетки строки стоят по центру её высоты", () => {
    // Столбец действий выше строки текста: без выравнивания текст сидел бы по
    // верхнему краю строки, а кнопка — по середине, и строка читалась бы косой.
    renderTable();
    expect(captured.columns.every((c) => c.onCell?.().style?.verticalAlign === "middle")).toBe(true);
  });
});
