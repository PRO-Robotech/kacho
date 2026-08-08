// Кнопка — общий примитив всех форм и диалогов дерева. Проверяется то, на что
// опираются вызывающие: обработчик зовётся, `disabled` его глушит, вариант и
// размер доезжают до классов (на них держатся деструктивные подтверждения —
// «удалить» обязано выглядеть иначе, чем «сохранить»), а `asChild` не заводит
// вложенной кнопки внутри ссылки.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { Button } from "./Button";

describe("Button", () => {
  it("зовёт обработчик по клику", () => {
    const onClick = jest.fn();
    render(<Button onClick={onClick}>Сохранить</Button>);

    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("выключенная кнопка обработчик не зовёт", () => {
    const onClick = jest.fn();
    render(
      <Button disabled onClick={onClick}>
        Сохранить
      </Button>,
    );

    const button = screen.getByRole("button", { name: "Сохранить" });
    expect(button).toBeDisabled();
    fireEvent.click(button);

    expect(onClick).not.toHaveBeenCalled();
  });

  it("деструктивный вариант отличим от обычного", () => {
    // Подтверждение удаления опознаётся пользователем по цвету кнопки; если
    // вариант не доезжает до классов, «удалить» выглядит как «сохранить».
    render(
      <>
        <Button variant="destructive">Удалить</Button>
        <Button>Сохранить</Button>
      </>,
    );
    const destructive = screen.getByRole("button", { name: "Удалить" }).className;
    const primary = screen.getByRole("button", { name: "Сохранить" }).className;

    expect(destructive).toContain("bg-destructive");
    expect(primary).toContain("bg-primary");
    expect(destructive).not.toBe(primary);
  });

  it("размер меняет класс, а собственный className не затирается", () => {
    render(
      <Button size="sm" className="w-full">
        Ок
      </Button>,
    );
    const cls = screen.getByRole("button", { name: "Ок" }).className;
    expect(cls).toContain("h-8");
    expect(cls).toContain("w-full");
  });

  it("asChild отдаёт разметку потомку, не оборачивая его в button", () => {
    render(
      <Button asChild>
        <a href="/vpc/v1/networks">К сетям</a>
      </Button>,
    );
    const link = screen.getByRole("link", { name: "К сетям" });
    expect(link.tagName).toBe("A");
    expect(link.className).toContain("bg-primary");
    // Вложенная кнопка внутри ссылки — невалидная разметка и двойная точка
    // фокуса; asChild заводится именно чтобы её не было.
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("пробрасывает ref на живой узел", () => {
    let node: HTMLButtonElement | null = null;
    render(
      <Button
        ref={(el) => {
          node = el;
        }}
      >
        Ок
      </Button>,
    );
    expect(node).toBe(screen.getByRole("button", { name: "Ок" }));
  });
});
