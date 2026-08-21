// Правила поля, объявленные схемой формы.
//
// Отрицания здесь всегда стоят В ПАРЕ с положительным: проверка «пустое поле
// отвергается» одинаково зелена и у настоящего правила, и у правила,
// отвергающего вообще всё. Поэтому у каждого запрета рядом лежит вход, который
// он обязан ПРОПУСТИТЬ.

import type { ArrayField, FormField } from "@shared/lib/form-schema";
import { checkField, checkFields, hasErrors } from "./field-rules";

const str = (over: Partial<FormField> & { name: string; label: string }): FormField =>
  ({ type: "string", ...over }) as FormField;

describe("обязательность", () => {
  it("пустое обязательное поле названо по имени и по правилу", () => {
    const problem = checkField(str({ name: "id", label: "Идентификатор", required: true }), "");

    expect(problem).toBe("«Идентификатор»: поле обязательное — без него ресурс не создать.");
  });

  it("заполненное обязательное поле молчит", () => {
    expect(checkField(str({ name: "id", label: "Идентификатор", required: true }), "ru-central1")).toBeNull();
  });

  it("строка из одних пробелов заполнением не считается", () => {
    expect(checkField(str({ name: "id", label: "Идентификатор", required: true }), "   ")).not.toBeNull();
  });

  it("необязательное пустое поле молчит", () => {
    expect(checkField(str({ name: "description", label: "Описание" }), "")).toBeNull();
  });

  it("выключенный переключатель — законный ответ «нет», а не пустота", () => {
    const bool = { name: "is_default", label: "По умолчанию", type: "bool", required: true } as FormField;

    expect(checkField(bool, false)).toBeNull();
  });

  it("пустой набор меток пустотой считается", () => {
    const labels = { name: "labels", label: "Метки", type: "labels", required: true } as FormField;

    expect(checkField(labels, {})).not.toBeNull();
    expect(checkField(labels, { env: "prod" })).toBeNull();
  });
});

describe("образец значения", () => {
  const name = str({
    name: "name",
    label: "Имя",
    pattern: "^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$",
  });

  it("значение не по образцу названо вместе с полем", () => {
    expect(checkField(name, "1-плохо")).toBe("«Имя»: значение «1-плохо» не подходит под правило поля.");
  });

  it("значение по образцу молчит", () => {
    expect(checkField(name, "my-network")).toBeNull();
  });

  // Негодный образец — дефект объявления. Обвинять за него того, кто вводит,
  // значит показывать отказ, который ничем не исправить.
  it("негодный образец не превращается в обвинение вводящему", () => {
    expect(checkField(str({ name: "x", label: "Х", pattern: "([" }), "что угодно")).toBeNull();
  });
});

describe("предельные величины числа", () => {
  const port = { name: "port", label: "Порт", type: "int", min: 1, max: 65535 } as FormField;

  it("ниже нижней границы", () => {
    expect(checkField(port, 0)).toBe("«Порт»: допустимо от 1 до 65535, введено 0.");
  });

  it("выше верхней границы", () => {
    expect(checkField(port, 70000)).toBe("«Порт»: допустимо от 1 до 65535, введено 70000.");
  });

  it("внутри границ молчит", () => {
    expect(checkField(port, 443)).toBeNull();
  });

  it("названа та граница, которая объявлена одна", () => {
    const only = { name: "prio", label: "Приоритет", type: "int", min: 0 } as FormField;

    expect(checkField(only, -1)).toBe("«Приоритет»: не меньше 0, введено -1.");
  });
});

describe("длина списка", () => {
  const arr = (over: Partial<ArrayField>): FormField =>
    ({
      name: "v4_address_ids",
      label: "IPv4 адрес",
      type: "array",
      itemLabel: "адрес",
      itemFields: [str({ name: "value", label: "Адрес" })],
      ...over,
    });

  it("слишком короткий список назван числом", () => {
    expect(checkField(arr({ minItems: 1, required: true }), [])).toBe(
      "«IPv4 адрес»: поле обязательное — без него ресурс не создать.",
    );
  });

  it("слишком длинный список назван числом", () => {
    expect(checkField(arr({ maxItems: 1 }), [{ value: "a" }, { value: "b" }])).toBe(
      "«IPv4 адрес»: не больше 1 — сейчас 2.",
    );
  });

  it("список в пределах молчит", () => {
    expect(checkField(arr({ maxItems: 1 }), [{ value: "a" }])).toBeNull();
  });
});

describe("набор полей", () => {
  const fields = [
    str({ name: "id", label: "Идентификатор", required: true }),
    str({ name: "description", label: "Описание" }),
  ];

  it("отказ адресован ПУТЁМ поля, а не порядковым номером", () => {
    expect(checkFields(fields, {})).toEqual({
      id: "«Идентификатор»: поле обязательное — без него ресурс не создать.",
    });
  });

  it("законный набор не даёт ни одного отказа", () => {
    expect(hasErrors(checkFields(fields, { id: "ru-central1" }))).toBe(false);
  });

  it("скрытое поле не требуется — заполнить его арендатор не может", () => {
    expect(checkFields([str({ name: "project_id", label: "Проект", required: true, hidden: true })], {})).toEqual({});
  });

  it("поле, заданное из контекста, с арендатора не спрашивается", () => {
    const locked = new Set(["network_id"]);

    expect(checkFields([str({ name: "network_id", label: "Сеть", required: true })], {}, { lockedPaths: locked })).toEqual({});
    // Положительный контроль: без запирания то же поле отказ даёт.
    expect(checkFields([str({ name: "network_id", label: "Сеть", required: true })], {})).not.toEqual({});
  });

  it("неизменяемое поле в правке не требуется — оно и не отправляется", () => {
    const f = [str({ name: "cidr", label: "CIDR", required: true, immutable: true })];

    expect(checkFields(f, {}, { editMode: true })).toEqual({});
    expect(checkFields(f, {}, { editMode: false })).not.toEqual({});
  });

  it("обязательное подполе строки списка названо вместе с номером строки", () => {
    const blocks = {
      name: "ipv4_cidr_blocks",
      label: "CIDR IPv4",
      type: "array",
      itemLabel: "CIDR",
      itemFields: [str({ name: "value", label: "CIDR", required: true })],
    } as FormField;

    const errors = checkFields([blocks], { ipv4_cidr_blocks: [{ value: "10.0.0.0/16" }, { value: "" }] });

    expect(errors).toEqual({
      "ipv4_cidr_blocks[1].value": "Строка 2. «CIDR»: поле обязательное — без него ресурс не создать.",
    });
  });
});
