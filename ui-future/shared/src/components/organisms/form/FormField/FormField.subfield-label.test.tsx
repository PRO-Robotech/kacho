// Подпись подполя составного виджета СВЯЗАНА со своим контролом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Строка составного списка (цели группы, спецификации интерфейсов) рисует свои
// подполя мелкой подписью сверху и вводом снизу. Подпись при этом была обычным
// текстом рядом: у поля ввода внутри строки НЕ БЫЛО доступного имени вовсе —
// ни `label for`, ни `aria-label`. Следствия два, и второе дороже первого:
//
//  1. читающий с экрана слышит «поле ввода» без единого слова о том, что вводить;
//  2. адресовать такое подполе можно только через разметку-соседа — по обёртке,
//     по классу, по «первому такому-то». Сквозная проба так и делала и падала:
//     обёртки формы (`.ant-form-item`) у подполя нет, а единственная обёртка с
//     нужным текстом — ВНЕШНЕЕ поле составного виджета.
//
// Здесь утверждается наблюдаемое — доступное имя контрола, — а не разметка:
// класс переименуют, обёртку заменят, а связь «эта подпись именует этот ввод»
// обязана остаться. Она же и есть тот признак, по которому подполе адресует
// сквозная проба (`ui-future/e2e/specs/contract-branches.spec.ts`).

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import type { FormField } from "@shared/lib/form-schema";
import { FormFieldRenderer } from "./FormField";

/** Составной список: подполей больше одного ⇒ строка рисует мелкие подписи. */
const targetsField: FormField = {
  name: "targets",
  label: "Цели",
  type: "array",
  itemLabel: "Цель",
  itemFields: [
    {
      name: "_identity_kind",
      label: "Чем названа цель",
      type: "enum",
      required: true,
      options: [
        { value: "instance_id", label: "Виртуальная машина" },
        { value: "external_ip", label: "Внешний адрес" },
      ],
    },
    {
      name: "external_ip.address",
      label: "Внешний адрес",
      type: "string",
      description: "Адрес вне облака.",
    },
  ],
};

/** Список из ОДНОГО подполя нескалярного вида: имя несёт левая колонка формы,
 *  своей подписи у подполя нет и быть не должно. */
const weightsField: FormField = {
  name: "weights",
  label: "Веса",
  type: "array",
  itemLabel: "Вес",
  itemFields: [{ name: "value", label: "Вес", type: "int" }],
};

function show(field: FormField, value: Record<string, unknown>) {
  const onChange = jest.fn();
  render(
    <FormFieldRenderer field={field} pathPrefix="" value={value} onChange={onChange} hideLabel />,
  );
  return { onChange };
}

describe("подполе составного списка именует свой ввод", () => {
  it("подпись подполя — доступное имя контрола, а не текст рядом", () => {
    show(targetsField, { targets: [{ _identity_kind: "external_ip" }] });

    // Ввод адреса и список вида — РАЗНЫЕ контролы с разными именами. Прежде оба
    // были безымянны, и различить их можно было только по порядку в разметке.
    expect(screen.getByLabelText("Внешний адрес")).toBeInTheDocument();
    expect(screen.getByLabelText("Чем названа цель")).toBeInTheDocument();
  });

  it("именованный ввод — тот самый: правка доносит значение по своему пути", () => {
    const { onChange } = show(targetsField, { targets: [{ _identity_kind: "external_ip" }] });

    // Положительный контроль к предыдущему: имя могло бы достаться СОСЕДНЕМУ
    // контролу, и «поле найдено» зеленело бы на неверной связи.
    fireEvent.change(screen.getByLabelText("Внешний адрес"), { target: { value: "203.0.113.7" } });

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        targets: [expect.objectContaining({ external_ip: { address: "203.0.113.7" } })],
      }),
    );
  });

  it("две строки — две РАЗНЫЕ связи, а не одна на обе", () => {
    const { onChange } = show(targetsField, {
      targets: [
        { _identity_kind: "external_ip" },
        { _identity_kind: "external_ip" },
      ],
    });

    const addressInputs = screen.getAllByLabelText("Внешний адрес");
    expect(addressInputs).toHaveLength(2);
    // Совпадение идентификаторов связало бы ОБЕ подписи с первым вводом: проба,
    // заполняющая «второй», молча писала бы в первый.
    expect((addressInputs[0] as HTMLInputElement).id).not.toBe((addressInputs[1] as HTMLInputElement).id);

    fireEvent.change(addressInputs[1], { target: { value: "203.0.113.8" } });
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        targets: [{ _identity_kind: "external_ip" }, expect.objectContaining({ external_ip: { address: "203.0.113.8" } })],
      }),
    );
  });

  it("у списка из одного подполя своей подписи НЕТ — её несёт левая колонка", () => {
    show(weightsField, { weights: [{}] });

    // Отрицание в паре с положительным выше: связь заводится там, где строка
    // рисует свои подписи, и не заводится там, где канон формы их не рисует.
    expect(screen.queryByLabelText("Вес")).not.toBeInTheDocument();
  });

  it("подполе со своим поддеревом подписью НЕ именуется — `for` указывал бы в пустоту", () => {
    const customField: FormField = {
      name: "specs",
      label: "Спецификации",
      type: "array",
      itemLabel: "Спецификация",
      itemFields: [
        { name: "note", label: "Заметка", type: "string" },
        {
          name: "widget",
          label: "Группы безопасности",
          type: "custom",
          render: () => <input aria-label="свой виджет" />,
        },
      ],
    };
    const { container } = render(
      <FormFieldRenderer field={customField} pathPrefix="" value={{ specs: [{}] }} onChange={jest.fn()} hideLabel />,
    );

    // Текст подписи на месте — виджет по-прежнему подписан для глаза.
    expect(screen.getByText("Группы безопасности")).toBeInTheDocument();
    // Но это не подпись контрола: у поддерева одного ввода нет, и `for` на него
    // утверждал бы, что по этому адресу есть контрол, которого там нет.
    expect(screen.queryByLabelText("Группы безопасности")).not.toBeInTheDocument();
    // Ни одна подпись строки не ведёт в пустоту.
    for (const l of Array.from(container.querySelectorAll("label[for]"))) {
      const forTarget = l.getAttribute("for") ?? "";
      expect(container.querySelector(`[id="${forTarget}"]`)).not.toBeNull();
    }
    // Положительный контроль той же строки: скалярное подполе рядом — именуется.
    expect(screen.getByLabelText("Заметка")).toBeInTheDocument();
  });
});
