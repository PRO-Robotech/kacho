// Правила полей доходят до ВОСЬМИ ресурсов сети — и это утверждается по каждому.
//
// # Почему перепись, а не один представитель
//
// Восемь типов ресурсов VPC рисуются ОДНИМ телом формы, поэтому соблазн
// проверить один и объявить «дошло до всех» велик. Он же и опасен: у восьми
// разные схемы, и правило, читаемое телом, ловит у них РАЗНОЕ — у сети это
// обязательное подполе строки списка, у подсети ссылка и ветвление размещения, у
// адреса поле, видимое только в одной ветви вида. Проверив один, о семи
// остальных знаешь ровно столько же, сколько до проверки.
//
// # Что здесь утверждается по каждому
//
//  1. ИСХОД нетронутой формы, собранной ШАБЛОНОМ САМОГО РЕСУРСА, — тем самым,
//     с которого начинает арендатор. Ожидание выписано таблицей ниже: семь
//     ресурсов отправить нельзя и форма называет поле, один можно;
//  2. отказ стоит В БЛОКЕ своего поля, а не сверху формы; у списка — В ТОЙ
//     СТРОКЕ, где не заполнили ввод, а не у поля целиком (решение владельца:
//     «невведённые поля подсвечивать там, где не ввели поле, а не сбоку»);
//  3. заполненная форма отправляется — без этого «не отправляется» было бы
//     одинаково зелено и у правила, и у формы, которая не отправляется никогда.
//
// # Что перепись нашла
//
// Шаблон сети кладёт ОДНУ ПУСТУЮ строку CIDR, а её подполе объявлено
// обязательным: нетронутая форма создания сети уходила на край с пустым
// диапазоном и возвращалась отказом края. То же у подсети, таблицы маршрутов,
// группы безопасности, шлюза и адреса — с пустой ссылкой на владельца.
//
// # Что перепись нашла ВТОРОЙ раз (2026-08-21)
//
// Когда отказ подполя перестал всплывать к полю целиком, у СЕТИ он перестал
// показываться вовсе: её список рисуется таблицей значений, а та ветвь
// возвращалась раньше того места, где сообщение ставится. Наблюдаемое следствие
// — форма отказывалась отправляться и молчала о причине: нажатие на «Создать»
// не давало ни ответа, ни объяснения. Одна половина свойства («не уезжает на
// сервер») держалась и была зелёной, вторая («форма называет поле») — нет,
// поэтому и понадобились ОБА утверждения, а не одно.

import { jest } from "@jest/globals";
import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { REGISTRY, applyFieldDefaults  } from "@shared/lib/resource-registry";
import type { ArrayField, FormField } from "@shared/lib/form-schema";
import { ResourceFormBody, isFullWidthField } from "./ResourceFormBody";

/** Ресурсы сети — все восемь. Перечень тот же, что у VPC_SCOPED_IDS модуля. */
const VPC_IDS = [
  "networks",
  "subnets",
  "addresses",
  "route-tables",
  "security-groups",
  "network-interfaces",
  "gateways",
  "cidr-groups",
] as const;

/**
 * Чего ждём от НЕТРОНУТОЙ формы каждого ресурса.
 *
 * Таблица выписана намеренно: вывести её из тех же правил, что проверяются, —
 * значит проверить правило само собой. Подпись — то слово, которое арендатор
 * видит в левой колонке; отказ обязан стоять в блоке этого поля.
 *
 * `inRow` — отказ относится не к полю целиком, а к ОДНОЙ строке его списка, и
 * стоять обязан у той строки, чей ввод не заполнен. Значение — точный текст
 * сообщения: у строки списка оно называет ПОДПОЛЕ и номер строки, а не имя поля,
 * поэтому «называет поле» без дословной сверки означало бы здесь другое.
 */
const EXPECTED: Record<
  (typeof VPC_IDS)[number],
  { blocked: boolean; label?: string; inRow?: string }
> = {
  networks: {
    blocked: true,
    label: "CIDR IPv4",
    inRow: "Строка 1. «CIDR»: поле обязательное — без него ресурс не создать.",
  },
  subnets: { blocked: true, label: "Облачная сеть" },
  addresses: { blocked: true, label: "Зона" },
  "route-tables": { blocked: true, label: "Сеть" },
  "security-groups": { blocked: true, label: "Облачная сеть" },
  "network-interfaces": { blocked: true, label: "Подсеть" },
  gateways: { blocked: true, label: "Подсеть" },
  // Набор блоков у группы префиксов пуст и законен: пустой список — это список,
  // а не незаполненное поле. Положительный контроль всей переписи.
  "cidr-groups": { blocked: false },
};

/** Значение, законное по объявлению поля. Ни у одного обязательного поля сети нет образца. */
function plausible(f: FormField): unknown {
  switch (f.type) {
    case "enum":
      return f.options[0]?.value ?? "";
    case "int":
      return f.min ?? 1;
    case "bool":
      return true;
    case "labels":
      return { env: "prod" };
    case "array":
      return [filledRow(f)];
    default:
      return "ref-1";
  }
}

function filledRow(f: ArrayField): Record<string, unknown> {
  const item: Record<string, unknown> = f.newItem ? f.newItem() : {};
  for (const sub of f.itemFields) item[sub.name] = plausible(sub);
  return item;
}

/** Кладёт значение по dotted-пути (пути обязательных полей адреса вложенные). */
function put(obj: Record<string, unknown>, path: string, value: unknown): void {
  const keys = path.split(".");
  let cur = obj;
  for (const k of keys.slice(0, -1)) {
    if (typeof cur[k] !== "object" || cur[k] === null) cur[k] = {};
    cur = cur[k] as Record<string, unknown>;
  }
  cur[keys[keys.length - 1]] = value;
}

function initial(id: string): Record<string, unknown> {
  const spec = REGISTRY[id];
  const tpl = spec.template({ projectId: "prj-1" }) as Record<string, unknown>;
  return applyFieldDefaults(spec.fields, { ...tpl });
}

function filled(id: string): Record<string, unknown> {
  const spec = REGISTRY[id];
  const obj = initial(id);
  for (const f of spec.fields ?? []) {
    if (f.hidden) continue;
    if (f.required) put(obj, f.name, plausible(f));
    // Строки списков заполняются и там, где сам список необязателен: пустая
    // строка внутри уже заведённой — это отказ, а шаблон сети её и кладёт.
    if (f.type === "array") {
      const rows = obj[f.name];
      if (Array.isArray(rows) && rows.length > 0) obj[f.name] = rows.map(() => filledRow(f));
    }
  }
  return obj;
}

/**
 * Поле-ссылка спрашивает свой список через общий клиент запросов, поэтому
 * оболочка нужна даже там, где запрос не уходит: без выбранного проекта он
 * выключен, но сам хук всё равно вызывается.
 */
function inShell(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

function show(id: string, obj: Record<string, unknown>, spec = REGISTRY[id]) {
  const onSubmit = jest.fn();
  const view = render(
    inShell(
      <ResourceFormBody
        spec={spec}
        mode="create"
        obj={obj}
        onChange={() => {}}
        submitLabel="Создать"
        submitting={false}
        onSubmit={onSubmit}
        onCancel={() => {}}
      />,
    ),
  );
  return { onSubmit, view };
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: "Создать" }));

/**
 * Блок поля в сетке формы — тот узел, у которого есть СВОЯ подпись.
 *
 * Он же и граница поиска: форма целиком содержит подписи всех полей, поэтому
 * проверка «где-то выше по дереву встретилось это слово» приняла бы любой отказ
 * формы за отказ этого поля — то есть перестала бы утверждать место.
 */
const fieldBlock = (node: HTMLElement): { label: string } | null => {
  for (let p: HTMLElement | null = node.parentElement; p; p = p.parentElement) {
    const labelEl = p.querySelector(":scope > label");
    if (labelEl) return { label: (labelEl.textContent ?? "").trim() };
  }
  return null;
};

/** Отказ, показанный ВНУТРИ блока поля с этой подписью. */
const errorAtField = (label: string): HTMLElement | undefined =>
  screen.queryAllByRole("alert").find((el) => (fieldBlock(el)?.label ?? "").includes(label));

/** Строка списка, в которой стоит отказ: ближайшая обёртка со СВОИМ вводом. */
const errorRow = (error: HTMLElement): HTMLElement | null => {
  for (let p: HTMLElement | null = error.parentElement; p; p = p.parentElement) {
    if (p.querySelector("input, textarea")) return p;
  }
  return null;
};

describe("правила полей читаются на всех восьми ресурсах сети", () => {
  it("перепись непуста и покрывает ровно восемь типов", () => {
    expect(VPC_IDS).toHaveLength(8);
    expect(Object.keys(EXPECTED)).toHaveLength(8);
    for (const id of VPC_IDS) expect(REGISTRY[id]?.fields?.length ?? 0).toBeGreaterThan(0);
  });

  for (const id of VPC_IDS) {
    const expected = EXPECTED[id];

    it(`${id}: нетронутая форма ${expected.blocked ? "не отправляется и называет поле" : "отправляется"}`, () => {
      const { onSubmit, view } = show(id, initial(id));

      submit();

      if (!expected.blocked) {
        expect(onSubmit).toHaveBeenCalledTimes(1);
        expect(screen.queryAllByRole("alert")).toHaveLength(0);
        view.unmount();
        return;
      }
      expect(onSubmit).not.toHaveBeenCalled();
      const error = errorAtField(expected.label!);
      expect(error).toBeDefined();
      // Отказ называет ПОЛЕ и ПРАВИЛО, а не «проверьте введённые данные».
      expect(error!.textContent).toMatch(/«[^»]+»:/);

      if (expected.inRow !== undefined) {
        // Отказ подполя стоит У СВОЕЙ СТРОКИ, а не у поля целиком: прямым
        // потомком блока поля он оказался бы ровно во втором случае.
        expect(error!.parentElement?.querySelector(":scope > label")).toBeFalsy();
        expect(error!.textContent).toBe(expected.inRow);
        // Претензия ОДНА — и сказана она один раз. Всплытие к полю целиком
        // (снято решением владельца) давало то же сообщение вторым местом, и
        // читатель искал незаполненный ввод глазами по всему списку.
        expect(screen.queryAllByRole("alert")).toHaveLength(1);
        // Читающий с экрана слышит причину вместе с вводом, а не отдельной
        // репликой неизвестно о чём: ссылка ведёт в ЭТО сообщение.
        const input = document.querySelector('input[aria-invalid="true"]');
        expect(input).not.toBeNull();
        expect(input).toHaveValue("");
        expect(error!.id).not.toBe("");
        expect(input!.getAttribute("aria-describedby")).toBe(error!.id);
        // У КАКОЙ строки он стоит — отдельной пробой ниже: шаблон кладёт ровно
        // одну строку, и на одной строке «у своей строки» неотличимо от «под
        // всей таблицей». Здесь это утверждать нечем.
      }
      view.unmount();
    });

    it(`${id}: заполненная форма отправляется`, () => {
      const { onSubmit, view } = show(id, filled(id));

      submit();

      expect(onSubmit).toHaveBeenCalledTimes(1);
      view.unmount();
    });
  }
});

describe("отказ списка стоит у ТОЙ строки, где не заполнили ввод", () => {
  // ПРЕДМЕТ (решение владельца 2026-08-20): «невведённые поля подсвечивать там,
  // где не ввели поле, а не сбоку». Для списка это значит: сообщение стоит у
  // своей строки, а не у поля целиком и не одной кучей под таблицей.
  //
  // Строк здесь ДВЕ, и это не украшение: на одной строке «у своей строки» и «под
  // всей таблицей» дают один и тот же DOM, и утверждение зеленеет при любом
  // размещении. Первая строка заполнена — она же положительный контроль: ввод,
  // к которому претензий нет, негодным не помечается.
  const spec = {
    ...REGISTRY["networks"],
    fields: [
      {
        name: "ipv4_cidr_blocks",
        label: "CIDR IPv4",
        type: "array",
        itemLabel: "CIDR",
        itemFields: [{ name: "value", label: "CIDR", type: "string", required: true }],
      },
    ],
  } as unknown as (typeof REGISTRY)["networks"];

  it("сообщение — в обёртке своей строки, а не под таблицей", () => {
    show("networks", { ipv4_cidr_blocks: [{ value: "10.0.0.0/16" }, { value: "" }] }, spec);

    submit();

    // Незаполненная строка одна — значит и сообщение на странице ровно одно:
    // всплытие того же отказа к полю целиком ловится здесь.
    const allErrors = screen.queryAllByRole("alert");
    expect(allErrors).toHaveLength(1);
    const error = errorAtField("CIDR IPv4");
    expect(error).toBeDefined();
    expect(error!.textContent).toBe("Строка 2. «CIDR»: поле обязательное — без него ресурс не создать.");

    // Ближайшая обёртка со вводом — это СТРОКА, а не таблица: у таблицы вводов
    // два, и сообщение, уехавшее под неё, здесь и ловится.
    const row = errorRow(error!);
    expect(row).not.toBeNull();
    const inputs = [...row!.querySelectorAll("input")];
    expect(inputs).toHaveLength(1);
    expect(inputs[0]).toHaveValue("");

    // Положительный контроль: заполненная строка отказа не несёт и негодной не
    // помечена — иначе «помечает негодным» зеленело бы на разметке, метящей всё.
    const all = [...document.querySelectorAll("input")];
    expect(all).toHaveLength(2);
    expect(all[0]).toHaveValue("10.0.0.0/16");
    expect(all[0]).not.toHaveAttribute("aria-invalid");
    expect(all[1]).toHaveAttribute("aria-invalid", "true");
    expect(all[1].getAttribute("aria-describedby")).toBe(error!.id);
  });
});

describe("отказ живёт ровно столько, сколько его предмет", () => {
  // Синтетическая схема, а не ресурс сети: предмет здесь — ЖИЗНЬ сообщения в
  // теле формы, и она не должна зависеть от того, как устроено поле-ссылка
  // конкретного ресурса. Обязательное строковое поле — самый прямой вход в это
  // свойство.
  const spec = {
    ...REGISTRY["route-tables"],
    fields: [{ name: "id", label: "Идентификатор", type: "string", required: true }],
  } as unknown as typeof REGISTRY["route-tables"];

  // Имя латиницей: правило хуков признаёт компонентом только имя с заглавной
  // ЛАТИНСКОЙ буквы, и при кириллическом считало вызов хука вызовом в обычной
  // функции — то есть находкой на исправном коде.
  function LiveForm() {
    const [obj, setObj] = useState<Record<string, unknown>>({ id: "" });
    return (
      <ResourceFormBody
        spec={spec}
        mode="create"
        obj={obj}
        onChange={setObj}
        submitLabel="Создать"
        submitting={false}
        onSubmit={() => {}}
        onCancel={() => {}}
      />
    );
  }

  it("не показывается, пока отправку не пробовали", () => {
    render(inShell(<LiveForm />));

    expect(screen.queryAllByRole("alert")).toHaveLength(0);
  });

  it("гаснет, как только поле поправили, — не дожидаясь второй отправки", () => {
    render(inShell(<LiveForm />));
    submit();
    expect(errorAtField("Идентификатор")).toBeDefined();

    fireEvent.change(document.querySelector("input")!, { target: { value: "rtb-1" } });

    expect(errorAtField("Идентификатор")).toBeUndefined();
  });
});

describe("обязательность видна ДО отправки, а не только по отказу", () => {
  // Звёздочка у подписи помечена `aria-hidden` — она украшение, и обязательность
  // программы чтения с экрана берут из САМОГО ввода. Пока `aria-required` не
  // ставил никто, читающий с экрана не узнавал об обязательности вовсе: ни до
  // отправки, ни после неё.
  const schema = (required: boolean) =>
    ({
      ...REGISTRY["route-tables"],
      fields: [{ name: "id", label: "Идентификатор", type: "string", required }],
    }) as unknown as (typeof REGISTRY)["route-tables"];

  const input = () => document.querySelector("input")!;

  it("ввод обязательного поля объявляет обязательность сразу", () => {
    render(inShell(
      <ResourceFormBody
        spec={schema(true)}
        mode="create"
        obj={{ id: "" }}
        onChange={() => {}}
        submitLabel="Создать"
        submitting={false}
        onSubmit={() => {}}
        onCancel={() => {}}
      />,
    ));

    expect(input()).toHaveAttribute("aria-required", "true");
    // И это ДО отправки: отказов на экране ещё нет.
    expect(screen.queryAllByRole("alert")).toHaveLength(0);
  });

  it("ввод необязательного поля обязательность не объявляет", () => {
    render(inShell(
      <ResourceFormBody
        spec={schema(false)}
        mode="create"
        obj={{ id: "" }}
        onChange={() => {}}
        submitLabel="Создать"
        submitting={false}
        onSubmit={() => {}}
        onCancel={() => {}}
      />,
    ));

    expect(input()).not.toHaveAttribute("aria-required");
  });

  it("после отказа ввод помечен негодным и указывает на своё сообщение", () => {
    render(inShell(
      <ResourceFormBody
        spec={schema(true)}
        mode="create"
        obj={{ id: "" }}
        onChange={() => {}}
        submitLabel="Создать"
        submitting={false}
        onSubmit={() => {}}
        onCancel={() => {}}
      />,
    ));

    submit();

    expect(input()).toHaveAttribute("aria-invalid", "true");
    const describedBy = input().getAttribute("aria-describedby");
    expect(describedBy).toBeTruthy();
    // Ссылка ведёт в СВОЁ сообщение, а не в пустоту: непроверенный
    // `aria-describedby` — та же форма без содержания.
    expect(document.getElementById(describedBy!)).toHaveTextContent("«Идентификатор»:");
  });
});

describe("обязательность нечем показать у поля во всю ширину — и этого поля нет", () => {
  // Поле, выходящее из пары «имя слева, ввод справа» (правила группы
  // безопасности, собственный виджет, составной список), рисуется БЕЗ подписи —
  // значит и звёздочки у него нет: её ставит производитель подписи. Значит
  // обязательность такого поля не видна НИЧЕМ до самой отправки.
  //
  // Мехaнизма под это не заведено намеренно: предмета нет. Перепись по ВСЕМУ
  // реестру (не только по сети) даёт ноль таких полей, а строить показ для
  // случая, которого не существует, — это мёртвый код, который никто не увидит
  // сломанным.
  //
  // Проба — не украшение переписи, а её самоистечение: заведут такое поле — она
  // покраснеет и назовёт его. Тогда сначала учат форму показывать
  // обязательность, и только потом заводят поле.
  it("ни одного обязательного поля во всю ширину во всём реестре", () => {
    const found: string[] = [];
    let total = 0;
    for (const [id, spec] of Object.entries(REGISTRY)) {
      for (const f of spec.fields ?? []) {
        total++;
        if (f.required && !f.hidden && isFullWidthField(f)) found.push(`${id}.${f.name} (${f.type})`);
      }
    }

    // Объём осмотренного — отдельным утверждением: «ноль находок» обязано быть
    // отличимо от «ноль прочитанных полей».
    expect(total).toBeGreaterThan(100);
    expect(found).toEqual([]);
  });
});
