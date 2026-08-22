// Подвал формы — место, где пользователь отправляет мутацию. Мутации
// асинхронны, поэтому подвал обязан: показывать, что отправка идёт; не
// принимать повторное нажатие (второй запрос создаст второй ресурс); не давать
// отменить на полпути; и отличать деструктивную отправку от обычной.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { FormFooter } from "./FormFooter";

function renderFooter(over: Partial<Parameters<typeof FormFooter>[0]> = {}) {
  const onSubmit = jest.fn();
  const onCancel = jest.fn();
  render(
    <FormFooter
      submitLabel="Создать"
      submitting={false}
      onSubmit={onSubmit}
      onCancel={onCancel}
      {...over}
    />,
  );
  return { onSubmit, onCancel };
}

describe("FormFooter", () => {
  it("отправляет и отменяет по нажатию", () => {
    const { onSubmit, onCancel } = renderFooter();

    fireEvent.click(screen.getByRole("button", { name: "Создать" }));
    fireEvent.click(screen.getByRole("button", { name: "Отменить" }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("во время отправки пульсирует и не даёт отменить", () => {
    // Отмена на полпути не отменяет уже принятую сервером операцию — она лишь
    // уводит пользователя с экрана, где виден её исход.
    const { onCancel } = renderFooter({ submitting: true });

    expect(screen.getByRole("button", { name: "Создать" }).className).toContain("is-pulsing");
    const cancel = screen.getByRole("button", { name: "Отменить" });
    expect(cancel).toBeDisabled();

    fireEvent.click(cancel);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("заблокированная отправка обработчик не зовёт", () => {
    // Блокировка приходит от формы (не пройдено подтверждение, не заполнено
    // обязательное) — клик обязан быть бесплодным, а не «почти отправить».
    const { onSubmit } = renderFooter({ submitDisabled: true });

    const submit = screen.getByRole("button", { name: "Создать" });
    expect(submit).toBeDisabled();
    fireEvent.click(submit);

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("деструктивная отправка отличима от обычной", () => {
    // Утверждается РАЗЛИЧИЕ, а не конкретный цвет. Цвет пульсации назначает
    // DopplerButton, и его собственная проба этот цвет и стережёт; второе место
    // об одном предмете расходится с первым при любой правке палитры — так и
    // вышло, когда тон кольца перевели с литерала на токен продукта.
    //
    // Различие же — то, ради чего подвал вообще передаёт `danger`: одинаковое
    // кольцо на удалении и на сохранении читалось бы как одно и то же действие.
    // Пара «оба назначены и они разные» падает на подмене `danger` любым
    // другим значением, а на смене палитры — не падает.
    const { unmount } = render(
      <FormFooter
        submitLabel="Удалить"
        submitting
        danger
        onSubmit={() => {}}
        onCancel={() => {}}
      />,
    );
    const danger = screen.getByRole("button", { name: "Удалить" }).style.getPropertyValue("--doppler-c");
    unmount();

    render(<FormFooter submitLabel="Создать" submitting onSubmit={() => {}} onCancel={() => {}} />);
    const plain = screen.getByRole("button", { name: "Создать" }).style.getPropertyValue("--doppler-c");

    expect(danger).not.toBe("");
    expect(plain).not.toBe("");
    expect(danger).not.toBe(plain);
  });

  it("липкий подвал прижимается к низу, обычный — нет", () => {
    // Длинная форма: без липкости кнопки уезжают за экран, и отправить форму
    // можно только долистав до конца.
    const sticky = render(
      <FormFooter submitLabel="Создать" submitting={false} onSubmit={() => {}} onCancel={() => {}} sticky />,
    );
    expect((sticky.container.firstElementChild as HTMLElement).style.position).toBe("sticky");

    const plain = render(
      <FormFooter submitLabel="Создать" submitting={false} onSubmit={() => {}} onCancel={() => {}} />,
    );
    expect((plain.container.firstElementChild as HTMLElement).style.position).toBe("");
  });
});

describe("подвал не «прыгает» при переходе создание↔правка", () => {
  // Подпись создания склоняет имя ресурса («Создать облачную сеть»), подпись
  // правки — одно слово («Сохранить»). Кнопка шириной ровно по подписи при
  // переходе съёживается втрое, и «Отменить» переезжает под курсор туда, где
  // секунду назад было подтверждение: промах по кнопке стоит отменённой работы.
  const width = (label: string, over: Partial<Parameters<typeof FormFooter>[0]> = {}) => {
    const { unmount } = render(
      <FormFooter
        submitLabel={label}
        submitting={false}
        onSubmit={() => {}}
        onCancel={() => {}}
        {...over}
      />,
    );
    const value = screen.getByRole("button", { name: label }).style.minWidth;
    unmount();
    return value;
  };

  it("основное действие держит одну ширину на обеих подписях", () => {
    expect(width("Создать облачную сеть")).toBe(width("Сохранить"));
  });

  it("пол ширины объявлен, а не оставлен содержимому", () => {
    // Отрицание «не прыгает» выполнено и пустой строкой у обеих подписей —
    // поэтому утверждается, что величина названа.
    expect(width("Сохранить")).toMatch(/^\d+px$/);
  });

  it("деструктивная отправка держит ту же ширину", () => {
    // Удаление выглядит ИНАЧЕ (линия опасности), но не УЖЕ: подвал у него тот
    // же, и кнопки в нём стоят там же.
    expect(width("Удалить", { danger: true })).toBe(width("Сохранить"));
  });
});
