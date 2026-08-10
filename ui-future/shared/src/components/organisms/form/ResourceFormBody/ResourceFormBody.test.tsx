// Единое тело формы создания и правки. Оно решает, ЧТО пользователь увидит и
// что сможет тронуть; каждая ошибка здесь тихая:
//   • поле, скрытое условным гейтом навсегда, не отличить от «его нет в схеме»;
//   • неизменяемое поле, показанное обычным вводом, обещает правку, которой
//     край не даст;
//   • поле, заданное контекстом, показанное редактируемым, даёт переписать
//     привязку, которую вызывающий уже выбрал.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import type { FormField } from "@shared/lib/form-schema";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceFormBody, matchesVisibleWhen } from "./ResourceFormBody";

function spec(fields?: FormField[]): ResourceSpec {
  return {
    id: "networks",
    route: "networks",
    apiPath: "/vpc/v1/networks",
    payloadKey: "networks",
    singular: "Сеть",
    plural: "Сети",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields,
    template: () => ({}),
  } as unknown as ResourceSpec;
}

const str = (over: Partial<FormField> & { name: string; label: string }): FormField =>
  ({ type: "string", ...over }) as FormField;

function show(fields: FormField[] | undefined, over: Partial<Parameters<typeof ResourceFormBody>[0]> = {}) {
  const onSubmit = jest.fn();
  const onCancel = jest.fn();
  const onChange = jest.fn();
  render(
    <ResourceFormBody
      spec={spec(fields)}
      mode="create"
      obj={{}}
      onChange={onChange}
      submitLabel="Создать сеть"
      submitting={false}
      onSubmit={onSubmit}
      onCancel={onCancel}
      {...over}
    />,
  );
  return { onSubmit, onCancel, onChange };
}

const labels = () => screen.queryAllByText(/^поле /).map((el) => el.textContent);

describe("ResourceFormBody — состав видимых полей", () => {
  it("ресурс без схемы формы честно говорит, что формы нет", () => {
    show(undefined);

    expect(screen.getByText("У ресурса Сеть нет form-schema; используйте API напрямую.")).toBeInTheDocument();
  });

  it("показывает поля схемы и не показывает скрытые", () => {
    show([str({ name: "name", label: "поле имя" }), str({ name: "secret", label: "поле скрытое", hidden: true })]);

    expect(labels()).toEqual(["поле имя"]);
  });

  it("поле только для создания в правке не показывается", () => {
    const fields = [str({ name: "name", label: "поле имя" }), str({ name: "sg", label: "поле только-создание", createOnly: true })];

    show(fields);
    expect(labels()).toContain("поле только-создание");

    screen.getByText("поле только-создание").remove();
    show(fields, { mode: "edit", submitLabel: "Сохранить" });
    expect(labels()).not.toContain("поле только-создание");
  });

  it("поле только для правки в создании не показывается", () => {
    show([str({ name: "vis", label: "поле только-правка", updateOnly: true })]);

    expect(labels()).toEqual([]);
  });

  it("условное поле скрыто, пока дискриминатор не выбран", () => {
    const fields = [
      str({ name: "_kind", label: "поле вид" }),
      str({ name: "external", label: "поле внешний", visibleWhen: { field: "_kind", equals: "external" } }),
    ];

    show(fields);
    expect(labels()).toEqual(["поле вид"]);
  });

  it("условное поле появляется, когда дискриминатор совпал", () => {
    show(
      [
        str({ name: "_kind", label: "поле вид" }),
        str({ name: "external", label: "поле внешний", visibleWhen: { field: "_kind", equals: "external" } }),
      ],
      { obj: { _kind: "external" } },
    );

    expect(labels()).toContain("поле внешний");
  });
});

describe("matchesVisibleWhen — гейт читает значение, а не его тип", () => {
  it("без гейта поле видно", () => {
    expect(matchesVisibleWhen({}, undefined)).toBe(true);
  });

  it("отсутствующее значение гейт не выполняет", () => {
    expect(matchesVisibleWhen({}, { field: "k", equals: "x" })).toBe(false);
  });

  it("ложный тумблер сравнивается со строкой «false», а не прячет поле навсегда", () => {
    expect(matchesVisibleWhen({ k: false }, { field: "k", equals: "false" })).toBe(true);
    expect(matchesVisibleWhen({ k: true }, { field: "k", equals: "false" })).toBe(false);
  });

  it("набор допустимых значений принимает любое из них", () => {
    expect(matchesVisibleWhen({ k: "b" }, { field: "k", equals: ["a", "b"] })).toBe(true);
    expect(matchesVisibleWhen({ k: "c" }, { field: "k", equals: ["a", "b"] })).toBe(false);
  });
});

describe("ResourceFormBody — что можно тронуть", () => {
  it("поле, заданное контекстом, показано только для чтения и объясняет почему", () => {
    show([str({ name: "network_id", label: "поле сеть" })], { lockedPaths: new Set(["network_id"]) });

    expect(screen.getByTitle("Задано из контекста")).toBeInTheDocument();
    expect(screen.getByRole("textbox")).toBeDisabled();
  });

  it("неизменяемое поле в правке показано только для чтения с СВОЕЙ причиной", () => {
    show([str({ name: "cidr", label: "поле диапазон", immutable: true })], { mode: "edit", obj: { cidr: "10.0.0.0/16" } });

    expect(screen.getByTitle("Неизменяемо после создания")).toBeInTheDocument();
    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(screen.getByRole("textbox")).toHaveValue("10.0.0.0/16");
  });

  it("то же поле при создании остаётся вводимым — неизменяемость наступает после", () => {
    show([str({ name: "cidr", label: "поле диапазон", immutable: true })]);

    expect(screen.getByRole("textbox")).toBeEnabled();
    expect(screen.queryByTitle("Неизменяемо после создания")).not.toBeInTheDocument();
  });

  it("сужение вариантов перечисления показывает только разрешённые", () => {
    show(
      [
        {
          type: "enum",
          name: "_kind",
          label: "поле вид",
          options: [
            { value: "internal", label: "внутренний" },
            { value: "internal_v6", label: "внутренний v6" },
            { value: "external", label: "внешний" },
          ],
        },
      ],
      { fieldOptionsFilter: { _kind: ["internal", "internal_v6"] } },
    );

    const opts = [...screen.getByRole("combobox").querySelectorAll("option")].map((o) => o.textContent);
    expect(opts).toContain("внутренний");
    expect(opts).not.toContain("внешний");
  });
});

describe("ResourceFormBody — подвал", () => {
  it("подпись отправки задаёт вызывающий, и нажатие доходит до него", () => {
    const { onSubmit } = show([str({ name: "name", label: "поле имя" })]);

    fireEvent.click(screen.getByRole("button", { name: "Создать сеть" }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("отмена доходит до вызывающего и отправку не запускает", () => {
    const { onSubmit, onCancel } = show([str({ name: "name", label: "поле имя" })]);

    fireEvent.click(screen.getByRole("button", { name: /Отмен/ }));

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("пока запрос в полёте, отправку нельзя запустить второй раз", () => {
    show([str({ name: "name", label: "поле имя" })], { submitting: true });

    expect(screen.getByRole("button", { name: "Создать сеть" })).toBeDisabled();
  });
});
