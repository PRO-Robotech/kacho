// Адаптер редактора меток к общей схеме формы: хранит правку списком пар, а в
// значение формы кладёт объект по пути. Существенны две вещи, и обе не видны
// глазом:
//
//   * первая же добавленная пара пуста, поэтому объект НЕ меняется. Если бы
//     адаптер перечитывал значение формы на каждый ответ, пустая строка
//     исчезала бы сразу после нажатия «Добавить метку»;
//   * значение кладётся ПО ПУТИ, не в корень: у вложенных форм метки лежат
//     внутри своей ветки, и запись в корень тихо потеряла бы их.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { LabelsFieldRenderer } from "./LabelsEditor";

function renderEditor(value: Record<string, unknown>, path = "labels", label = "Метки") {
  const onChange = jest.fn<(next: Record<string, unknown>) => void>();
  const utils = render(
    <LabelsFieldRenderer pathPrefix="" path={path} label={label} value={value} onChange={onChange} />,
  );
  return { onChange, ...utils };
}

describe("form/LabelsEditor", () => {
  it("разворачивает карту из значения формы в строки", () => {
    renderEditor({ labels: { env: "prod" } });
    expect(screen.getByDisplayValue("env")).toBeInTheDocument();
    expect(screen.getByDisplayValue("prod")).toBeInTheDocument();
  });

  it("кладёт карту по указанному пути, не в корень", () => {
    const { onChange } = renderEditor({ spec: { labels: {} } }, "spec.labels");

    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));
    fireEvent.change(screen.getByPlaceholderText("ключ"), { target: { value: "env" } });

    const last = onChange.mock.calls.at(-1)![0];
    expect(last).toEqual({ spec: { labels: { env: "" } } });
  });

  it("пустая строка остаётся на экране, хотя карта от неё не изменилась", () => {
    // Именно здесь ломается наивная синхронизация: объект тот же, и строку
    // «возвращают» обратно — пользователь жмёт «Добавить» и ничего не видит.
    const { onChange, rerender } = renderEditor({ labels: {} });

    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));
    expect(screen.getByPlaceholderText("ключ")).toBeInTheDocument();

    const next = onChange.mock.calls.at(-1)![0];
    expect(next).toEqual({ labels: {} });

    rerender(<LabelsFieldRenderer pathPrefix="" path="labels" label="Метки" value={next} onChange={onChange} />);
    expect(screen.getByPlaceholderText("ключ")).toBeInTheDocument();
  });

  it("внешнее обновление значения перечитывается", () => {
    // Форма правки заполняется после первого ответа сервера — новая карта
    // обязана доехать до строк.
    const onChange = jest.fn<(next: Record<string, unknown>) => void>();
    const { rerender } = render(
      <LabelsFieldRenderer pathPrefix="" path="labels" label="Метки" value={{ labels: {} }} onChange={onChange} />,
    );
    expect(screen.queryByDisplayValue("env")).not.toBeInTheDocument();

    rerender(
      <LabelsFieldRenderer pathPrefix="" path="labels" label="Метки" value={{ labels: { env: "prod" } }} onChange={onChange} />,
    );

    expect(screen.getByDisplayValue("env")).toBeInTheDocument();
  });

  it("не-объект по пути читает как пустую карту, а не падает", () => {
    // По этому пути может лежать что угодно, пока форма собирается.
    renderEditor({ labels: "не карта" });
    expect(screen.queryAllByLabelText("Удалить строку")).toHaveLength(0);
  });

  it("подпись рисуется, когда она задана, и не заводит пустого места, когда нет", () => {
    const withLabel = renderEditor({ labels: {} }, "labels", "Метки");
    expect(screen.getByText("Метки")).toBeInTheDocument();
    withLabel.unmount();

    const bare = renderEditor({ labels: {} }, "labels", "");
    expect(bare.container.querySelector("label")).toBeNull();
  });
});
