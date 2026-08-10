// Отрисовка одного поля формы по его описанию. Предмет — решения, которые
// меняют то, что человек видит и может тронуть:
//
//  1. скрытое поле не рисуется вовсе, а не рисуется пустым;
//  2. поле, зависящее от дискриминатора, показывается ровно при своей ветке —
//     иначе форма предлагает заполнить то, чего в теле запроса не будет;
//  3. в режиме правки неизменяемое поле ЗАПЕРТО и говорит, почему: незапертое
//     приглашает править то, что край отвергнет, а запертое без объяснения
//     читается как поломка;
//  4. в режиме создания то же самое поле открыто — иначе ресурс не создать.
//
// Проверяется наблюдаемое: наличие поля, его состояние и текст рядом с ним, а
// не то, какую ветку выбрал модуль внутри.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import type { FormField } from "@shared/lib/form-schema";
import { FormFieldRenderer } from "./FormField";

function show(field: FormField, value: Record<string, unknown> = {}, editMode = false) {
  const onChange = jest.fn();
  render(
    <FormFieldRenderer field={field} pathPrefix="" value={value} onChange={onChange} editMode={editMode} />,
  );
  return { onChange };
}

const nameField: FormField = { name: "name", label: "Имя", type: "string" };

describe("FormFieldRenderer", () => {
  it("показывает подпись и текущее значение", () => {
    show(nameField, { name: "web" });

    expect(screen.getByText("Имя")).toBeInTheDocument();
    expect(screen.getByDisplayValue("web")).toBeInTheDocument();
  });

  it("правка поля доносит НОВОЕ значение по своему пути", () => {
    const { onChange } = show(nameField, { name: "web" });

    fireEvent.change(screen.getByDisplayValue("web"), { target: { value: "web-2" } });

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ name: "web-2" }));
  });

  it("скрытое поле не рисуется вовсе", () => {
    show({ ...nameField, hidden: true }, { name: "web" });

    expect(screen.queryByText("Имя")).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue("web")).not.toBeInTheDocument();
  });

  it("поле своей ветки показано, чужой — нет", () => {
    const zone: FormField = {
      name: "zone_id",
      label: "Зона",
      type: "string",
      visibleWhen: { field: "placement_type", equals: "ZONAL" },
    };

    show(zone, { placement_type: "ZONAL" });
    expect(screen.getByText("Зона")).toBeInTheDocument();

    screen.getByText("Зона").remove();
    show(zone, { placement_type: "REGIONAL" });
    // Предложить заполнить поле чужой ветки значит обещать, что оно уедет в
    // тело, — а его там не будет.
    expect(screen.queryByText("Зона")).not.toBeInTheDocument();
  });

  it("в правке неизменяемое поле заперто и объясняет запрет", () => {
    show({ ...nameField, immutable: true }, { name: "web" }, true);

    expect((screen.getByDisplayValue("web") as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText(/immutable после Create/)).toBeInTheDocument();
  });

  it("в создании то же поле открыто — контроль в обратную сторону", () => {
    // Без этой пары «заперто» зеленело бы и на поле, запертом всегда, то есть
    // на ресурсе, который нельзя создать.
    show({ ...nameField, immutable: true }, { name: "web" }, false);

    expect((screen.getByDisplayValue("web") as HTMLInputElement).disabled).toBe(false);
    expect(screen.queryByText(/immutable после Create/)).not.toBeInTheDocument();
  });

  it("поле, скрытое только в правке, в создании видно", () => {
    show({ ...nameField, editHidden: true }, { name: "web" }, false);
    expect(screen.getByText("Имя")).toBeInTheDocument();

    screen.getByText("Имя").remove();
    show({ ...nameField, editHidden: true }, { name: "web" }, true);
    expect(screen.queryByText("Имя")).not.toBeInTheDocument();
  });
});
