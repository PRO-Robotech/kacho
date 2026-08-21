// Клетка списка — одна строка, и это ДВА разных утверждения, каждое со своим
// способом сломаться:
//
//   * форма клетки: перенос запрещён, высота — одна строка, ширина — с пределом.
//     Проверяется объявлением: значения раскладки в jsdom не считаются вовсе, и
//     проба, которая бы «измерила» их здесь, утверждала бы ноль;
//   * подсказка: она обязана появляться ровно тогда, когда значение обрезано, и
//     НЕ появляться, когда оно поместилось. Второе — не украшение пробы: без
//     него утверждение зеленело бы и на клетке, которая вешает подсказку всегда,
//     то есть на подсказке, ничего не сообщающей.

import { render, screen } from "@testing-library/react";
import {
  CELL_BLEED,
  CELL_CLIP_CLASS,
  CELL_LINE_HEIGHT,
  CELL_MAX_WIDTH,
  CellClip,
  cellClipStyle,
  showTitleWhenClipped,
} from "./cellClip";

/** Раскладку jsdom не считает: обрезку изображаем, задав измерения напрямую. */
function measure(el: HTMLElement, scrollWidth: number, clientWidth: number, scrollHeight = 20, clientHeight = 20) {
  Object.defineProperty(el, "scrollWidth", { value: scrollWidth, configurable: true });
  Object.defineProperty(el, "clientWidth", { value: clientWidth, configurable: true });
  Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
  Object.defineProperty(el, "clientHeight", { value: clientHeight, configurable: true });
}

describe("клетка списка — форма", () => {
  it("значение не переносится и занимает ровно одну строку", () => {
    const s = cellClipStyle();
    expect(s.whiteSpace).toBe("nowrap");
    expect(s.overflow).toBe("hidden");
    expect(s.textOverflow).toBe("ellipsis");
    expect(s.lineHeight).toBe(`${CELL_LINE_HEIGHT}px`);
    // Высота клетки — одна строка плюс продых под заливку копирующей кнопки и
    // ореол фокуса: выше означало бы, что вторая строка содержимого частично
    // видна, ниже — что у кнопки срезан угол, а у ореола фокуса — половина.
    expect(s.maxHeight).toBe(CELL_LINE_HEIGHT + CELL_BLEED * 2);
    // Внутренняя область — РОВНО строка: продых уходит в поле, а не добавляет
    // высоты содержимому (без `border-box` он растил бы строку списка).
    expect(s.boxSizing).toBe("border-box");
    expect(s.paddingBlock).toBe(CELL_BLEED);
    expect(s.marginBlock).toBe(-CELL_BLEED);
  });

  it("предел ширины — общий, если колонка своего не назвала", () => {
    expect(cellClipStyle().maxWidth).toBe(CELL_MAX_WIDTH);
    expect(cellClipStyle(244).maxWidth).toBe(244);
  });

  it("клетка помечена крючком, по которому её находит обработчик наведения", () => {
    render(<CellClip>значение</CellClip>);
    expect(screen.getByText("значение")).toHaveClass(CELL_CLIP_CLASS);
  });
});

describe("клетка списка — подсказка договаривает обрезанное", () => {
  it("обрезанное значение получает подсказку с полным текстом", () => {
    render(<CellClip>rtb6qc1500147672jdmp</CellClip>);
    const cell = screen.getByText("rtb6qc1500147672jdmp");
    measure(cell, 400, 200);

    showTitleWhenClipped({ target: cell });

    expect(cell).toHaveAttribute("title", "rtb6qc1500147672jdmp");
  });

  it("поместившееся значение подсказки НЕ получает", () => {
    // Контроль в обратную сторону. Без него утверждение выше зеленело бы на
    // клетке, вешающей подсказку всегда: такая подсказка повторяет видимое и
    // перестаёт что-либо сообщать.
    render(<CellClip>net-1</CellClip>);
    const cell = screen.getByText("net-1");
    measure(cell, 200, 200);

    showTitleWhenClipped({ target: cell });

    expect(cell).not.toHaveAttribute("title");
  });

  it("подсказка снимается, когда обрезки не стало", () => {
    // Ширина колонки меняется вместе с окном: подсказка, оставшаяся от прежней
    // ширины, утверждала бы обрезку, которой уже нет.
    render(<CellClip>описание подлиннее</CellClip>);
    const cell = screen.getByText("описание подлиннее");
    measure(cell, 400, 200);
    showTitleWhenClipped({ target: cell });
    expect(cell).toHaveAttribute("title");

    measure(cell, 200, 200);
    showTitleWhenClipped({ target: cell });

    expect(cell).not.toHaveAttribute("title");
  });

  it("содержимое СТОЛБИКОМ обрезается по высоте и тоже договаривается", () => {
    // Набор статических маршрутов нарисован столбиком: многоточия там не будет
    // (оно про строчный поток), поэтому обрезку надо ловить по высоте — иначе
    // спрятанные строки пропали бы молча.
    render(
      <CellClip>
        <div>
          <span>10.0.0.0/8 → 192.168.0.1</span>
          <span>10.1.0.0/16 → 192.168.0.2</span>
        </div>
      </CellClip>,
    );
    const cell = screen.getByText("10.0.0.0/8 → 192.168.0.1").closest(`.${CELL_CLIP_CLASS}`) as HTMLElement;
    measure(cell, 200, 200, 40, 20);

    showTitleWhenClipped({ target: cell });

    expect(cell.getAttribute("title")).toContain("10.1.0.0/16");
  });

  it("наведение мимо клетки ничего не трогает", () => {
    // Обработчик стоит один на всю поверхность таблицы и получает наведение на
    // что угодно — на шапку, на пустое место, на полосу прокрутки.
    render(<div data-testid="вне">не клетка</div>);
    const outside = screen.getByTestId("вне");

    expect(() => showTitleWhenClipped({ target: outside })).not.toThrow();
    expect(outside).not.toHaveAttribute("title");
  });
});
