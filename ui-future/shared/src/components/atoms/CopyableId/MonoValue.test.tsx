// Моноширинное значение СТРОКИ СВОЙСТВ — текст, и только текст.
//
// Компонент заведён решением владельца: в строке свойств копирование общее —
// одна кнопка столбцом у правого края, — поэтому само значение кнопкой быть
// перестало. До этой пробы его контракт не держало ничто, а рисует он девять
// строк свойств (идентификатор, проект, MAC, дайджест образа, FQDN, …), то есть
// расхождение здесь видно сразу на всех карточках консоли.
//
// Утверждается ПАРА, а не одно свойство: значение обрезается ПОКАЗОМ (одна
// строка + многоточие) и при этом НЕ усекается по существу — в разметке и в
// подсказке остаётся полная строка. Без второй половины проба зеленела бы на
// «починке» через `value.slice(0, 12)`, то есть на потере данных: общая кнопка
// строки копирует ИСХОДНОЕ значение, и обрезанное показанное — единственное, что
// у пользователя остаётся перед глазами.
import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { MonoValue } from "./MonoValue";

const VALUE = "rtb6qc1500147672jdmp";

describe("MonoValue", () => {
  it("одна строка: не переносится и обрезается многоточием", () => {
    render(<MonoValue value={VALUE} />);
    const node = screen.getByText(VALUE);

    expect(node.getAttribute("class") ?? "").toContain("t-mono");
    expect(node.style.whiteSpace).toBe("nowrap");
    expect(node.style.textOverflow).toBe("ellipsis");
    expect(node.style.overflow).toBe("hidden");
    // Без этого гибкий элемент не становится уже своего содержимого, и обрезка
    // не наступает НИКОГДА: правило выглядело бы исполненным и не делало ничего.
    // Отсутствие свойства даёт пустую строку → NaN, то есть проба падает и на нём.
    expect(parseFloat(node.style.minWidth)).toBe(0);
  });

  it("обрезка — только показ: полное значение остаётся в разметке и в подсказке", () => {
    render(<MonoValue value={VALUE} />);
    const node = screen.getByText(VALUE);

    expect(node.textContent).toBe(VALUE);
    expect(node).toHaveAttribute("title", VALUE);
  });

  it("копирования не предлагает: в строке свойств оно общее", () => {
    const writeText = jest.fn<(text: string) => Promise<void>>();
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const { container } = render(<MonoValue value={VALUE} />);

    expect(container.querySelector("button")).toBeNull();
    fireEvent.click(screen.getByText(VALUE));
    expect(writeText).not.toHaveBeenCalled();
    // Парный положительный: «кнопки нет» не достигнуто тем, что нет и значения.
    expect(screen.getByText(VALUE)).toBeInTheDocument();
  });

  it("пустое значение — прочерк, а не пустая моноширинная строка с подсказкой", () => {
    const { container } = render(<MonoValue value="" />);

    expect(screen.getByText("—")).toBeInTheDocument();
    // Близнец к пробе подсказки: пустая строка не имеет права уехать в `title`,
    // иначе на карточке висит невидимое значение с пустой подсказкой.
    expect(container.querySelector("[title]")).toBeNull();
    expect(container.querySelector(".t-mono")).toBeNull();
  });
});
