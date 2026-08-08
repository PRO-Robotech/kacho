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
    renderFooter({ danger: true, submitting: true, submitLabel: "Удалить" });
    expect(screen.getByRole("button", { name: "Удалить" }).style.getPropertyValue("--doppler-c")).toBe(
      "rgba(255, 77, 79, 0.6)",
    );
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
