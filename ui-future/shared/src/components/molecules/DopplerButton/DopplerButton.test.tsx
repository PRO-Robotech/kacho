// Кнопка отправки формы, пульсирующая, пока асинхронная Operation не завершена.
// Мутации в Kachō асинхронны (ban #9), и эта пульсация — единственный признак
// того, что запрос принят и идёт: без неё пользователь жмёт «Создать» повторно.
// Поэтому утверждается связь состояния ожидания с наблюдаемым видом и то, что
// цвет пульсации следует деструктивности действия.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { DopplerButton } from "./DopplerButton";

function buttonEl(name: string): HTMLElement {
  return screen.getByRole("button", { name });
}

describe("DopplerButton", () => {
  it("зовёт обработчик по клику", () => {
    const onClick = jest.fn();
    render(<DopplerButton onClick={onClick}>Создать</DopplerButton>);

    fireEvent.click(buttonEl("Создать"));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("в покое не пульсирует", () => {
    render(<DopplerButton>Создать</DopplerButton>);
    const el = buttonEl("Создать");
    expect(el.className).toContain("doppler-btn");
    expect(el.className).not.toContain("is-pulsing");
    expect(el.style.getPropertyValue("--doppler-c")).toBe("");
  });

  it("в ожидании пульсирует", () => {
    render(<DopplerButton pulsing>Создать</DopplerButton>);
    const el = buttonEl("Создать");
    expect(el.className).toContain("is-pulsing");
    expect(el.style.getPropertyValue("--doppler-c")).not.toBe("");
  });

  it("деструктивное действие пульсирует красным, обычное — синим", () => {
    // Цвет здесь несёт смысл: синяя пульсация на удалении читалась бы как
    // обычное сохранение.
    render(
      <>
        <DopplerButton pulsing danger>
          Удалить
        </DopplerButton>
        <DopplerButton pulsing>Создать</DopplerButton>
      </>,
    );
    // Утверждается ТОКЕН, а не литерал: тон кольца обязан следовать палитре
    // продукта в обеих темах. Прежде здесь стояли `rgba(255, 77, 79, .6)` и
    // `rgba(22, 119, 255, .55)` — цвета прежней палитры, не менявшиеся ни в
    // одной теме; проба закрепляла ровно тот хардкод, из-за которого кольцо
    // шло вокруг кнопки другого оттенка.
    const danger = buttonEl("Удалить").style.getPropertyValue("--doppler-c");
    const primary = buttonEl("Создать").style.getPropertyValue("--doppler-c");
    expect(danger).toContain("var(--kc-danger)");
    expect(primary).toContain("var(--kc-primary)");
    // Контроль в обратную сторону: два разных токена могли бы разрешиться в
    // один цвет, но подмена `danger` на обычную отправку обязана быть видна.
    expect(danger).not.toBe(primary);
  });

  it("собственные класс и стиль вызывающего не затираются", () => {
    render(
      <DopplerButton pulsing className="w-full" style={{ marginTop: "8px" }}>
        Создать
      </DopplerButton>,
    );
    const el = buttonEl("Создать");
    expect(el.className).toContain("w-full");
    expect(el.className).toContain("is-pulsing");
    expect(el.style.marginTop).toBe("8px");
  });

  it("состояние загрузки antd пульсации не включает", () => {
    // Пульсация привязана к `pulsing` — внешнему ожиданию асинхронной Operation,
    // а не к собственному `loading` кнопки. Смешать их значило бы пульсировать
    // на любом «занят», в том числе там, где никакой операции не заводилось.
    render(<DopplerButton loading>Создать</DopplerButton>);
    const el = buttonEl("Создать");
    expect(el.className).toContain("doppler-btn");
    expect(el.className).not.toContain("is-pulsing");
    expect(el.style.getPropertyValue("--doppler-c")).toBe("");
  });
});
