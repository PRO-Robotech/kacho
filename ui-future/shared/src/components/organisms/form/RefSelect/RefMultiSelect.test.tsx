// Поле множественного выбора чужого ресурса. Предмет пробы — ДВА свойства,
// которые до правки владельца (2026-08-21) не выполнялись:
//
//   1. выбранное живёт ВНУТРИ поля. Прежний вид рисовал фишки отдельной строкой
//      НАД выпадающим списком: у той строки нет ни рамки поля, ни его ширины,
//      она растёт по мере выбора и наезжает на соседнее поле формы;
//   2. фишка адреса называет АДРЕС. Прежняя подпись была «имя: адрес» — имя
//      ресурса-адреса о выборе не говорит ничего и съедает ширину, которой в
//      поле и так мало.
//
// ПОЧЕМУ У ПРОБЫ СВОЙ ЗАМЕНИТЕЛЬ `Select`
//
// Общий заменитель antd рисует выпадающий список ОДИНОЧНЫМ `<select>`: он не
// знает ни множественного режима, ни `tagRender`, поэтому фишек не показывает
// вовсе — и любое утверждение о них было бы истинным при любом коде. Здесь
// заменитель переопределён ровно в той части, которая и есть предмет пробы
// (штатный приём, объявленный в шапке `test/antd-stub.ts`), и выполняет правило
// настоящего компонента: в режиме `multiple` выбранное рисует САМ элемент
// управления, внутри своего корня.
//
// Что отсюда следует для вердикта: «фишка внутри поля» — утверждение о
// продукте, а не о заменителе. Вернись компонент к собственной строке фишек
// рядом с полем — их текст оказался бы вне корня, и проба покраснеет.
// Свёртка хвоста в «+N» — единственное, что здесь утверждается ОБЪЯВЛЕНИЕМ:
// в jsdom нет раскладки, поэтому переполнение ширины наблюдать нечем.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { antdStub } from "@shared/test/antd-stub";

const list = jest.fn<(path: string, q: Record<string, string>) => Promise<Record<string, unknown>>>();

interface StubOption {
  value: string;
  label?: React.ReactNode;
}
interface StubTagProps {
  value: string;
  label: React.ReactNode;
  closable: boolean;
  onClose: () => void;
}
interface StubSelectProps {
  mode?: string;
  value?: string[];
  options?: StubOption[];
  onChange?: (next: string[]) => void;
  onSearch?: (term: string) => void;
  tagRender?: (props: StubTagProps) => React.ReactElement;
  maxTagCount?: number | string;
  maxCount?: number;
  placeholder?: React.ReactNode;
  disabled?: boolean;
  notFoundContent?: React.ReactNode;
  [key: string]: unknown;
}

// Заменитель множественного выбора: корень поля, внутри него — фишки выбранного
// и список вариантов. Выбор варианта ДОБАВЛЯЕТ значение к набору — так же, как
// настоящий antd, который отдаёт в `onChange` новый массив целиком.
const MultiSelect = ({
  mode,
  value = [],
  options = [],
  onChange,
  onSearch,
  tagRender,
  maxTagCount,
  maxCount,
  placeholder,
  disabled,
  notFoundContent,
}: StubSelectProps) =>
  React.createElement(
    "div",
    {
      "data-testid": "ref-field",
      "data-mode": mode ?? "",
      "data-max-tag-count": String(maxTagCount ?? ""),
      "data-max-count": maxCount === undefined ? "" : String(maxCount),
    },
    mode === "multiple" && tagRender
      ? value.map((uid) =>
          React.cloneElement(
            tagRender({
              value: uid,
              label: options.find((o) => o.value === uid)?.label ?? uid,
              closable: true,
              onClose: () => onChange?.(value.filter((v) => v !== uid)),
            }),
            { key: uid },
          ),
        )
      : null,
    React.createElement("input", {
      key: "__search__",
      type: "search",
      "aria-label": "select-search",
      onChange: (e: { target: { value: string } }) => onSearch?.(e.target.value),
    }),
    React.createElement(
      "select",
      {
        key: "__picker__",
        value: "",
        disabled,
        onChange: (e: { target: { value: string } }) => onChange?.([...value, e.target.value]),
      },
      React.createElement("option", { key: "__placeholder__", value: "" }, placeholder),
      ...options.map((o) => React.createElement("option", { key: o.value, value: o.value }, o.label)),
    ),
    options.length === 0 && notFoundContent !== undefined
      ? React.createElement("div", { key: "__empty__" }, notFoundContent as React.ReactNode)
      : null,
  );

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/context-store", () => ({
  useProjectStore: (sel: (s: { project: { id: string } | null }) => unknown) => sel({ project: { id: "prj-1" } }),
  useContext: (sel: (s: { account: null }) => unknown) => sel({ account: null }),
}));

jest.unstable_mockModule("antd", () => ({ ...antdStub(), Select: MultiSelect }));

const { RefMultiSelect } = await import("./RefMultiSelect");

const ADDRESSES = {
  addresses: [
    { id: "adr-a", name: "reserved-front", internal_ipv4_address: { address: "10.0.0.5" } },
    { id: "adr-b", name: "reserved-back", internal_ipv4_address: { address: "10.0.0.9" } },
  ],
};

function show(over: Partial<Parameters<typeof RefMultiSelect>[0]> = {}) {
  const onChange = jest.fn<(next: string[]) => void>();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const props = { refResource: "addresses", projectId: "prj-1", value: [] as string[], onChange, ...over };
  render(
    <QueryClientProvider client={client}>
      <RefMultiSelect {...props} />
    </QueryClientProvider>,
  );
  return { onChange };
}

const field = () => screen.getByTestId("ref-field");
const optionLabels = () => [...field().querySelectorAll("option")].map((o) => o.textContent ?? "");
const pick = (uid: string) => fireEvent.change(field().querySelector("select")!, { target: { value: uid } });

beforeEach(() => {
  jest.clearAllMocks();
  list.mockResolvedValue(ADDRESSES);
});

describe("выбранное удерживается внутри поля", () => {
  it("фишка выбранного адреса — потомок поля, а не сосед", async () => {
    show({ value: ["adr-a"] });

    const tag = await screen.findByText("10.0.0.5");
    expect(field().contains(tag)).toBe(true);
    // Ни одного узла с этим текстом СНАРУЖИ: собственная строка фишек рядом с
    // полем — ровно то, что чинится.
    expect(screen.getAllByText("10.0.0.5").every((el) => field().contains(el))).toBe(true);
  });

  it("поле объявлено множественным и сворачивает хвост в «+N»", async () => {
    show({ value: ["adr-a"] });

    await screen.findByText("10.0.0.5");
    expect(field().getAttribute("data-mode")).toBe("multiple");
    // Утверждение об ОБЪЯВЛЕНИИ, и это сказано вслух: в jsdom нет раскладки,
    // поэтому выход фишек за ширину поля наблюдать нечем. Без свёртки ширина
    // поля перестаёт быть пределом для числа фишек.
    expect(field().getAttribute("data-max-tag-count")).toBe("responsive");
  });

  it("снятие фишки убирает ровно её", async () => {
    const { onChange } = show({ value: ["adr-a", "adr-b"] });

    await screen.findByText("10.0.0.5");
    fireEvent.click(within(field()).getAllByRole("button", { name: "close" })[0]);

    expect(onChange).toHaveBeenCalledWith(["adr-b"]);
  });
});

describe("подпись выбранного адреса — только адрес", () => {
  it("в фишке стоит адрес, имени ресурса в ней нет", async () => {
    show({ value: ["adr-a"] });

    const tag = await screen.findByText("10.0.0.5");
    expect(tag.textContent).not.toContain("reserved-front");
  });

  it("в СПИСКЕ выбора имя остаётся — по нему и выбирают (положительный контроль)", async () => {
    // Без этого утверждения «в фишке нет имени» выполнялось бы реализацией,
    // которая не показывает имя нигде, — а тогда выбирать не из чего.
    show();

    await waitFor(() => expect(optionLabels()).toContain("reserved-front · 10.0.0.5"));
  });

  it("адрес набран моноширинным — как все адреса консоли", async () => {
    show({ value: ["adr-a"] });

    const tag = await screen.findByText("10.0.0.5");
    expect(tag.getAttribute("style")).toContain("var(--font-mono)");
  });

  it("подпись переживает сужение списка вводом", async () => {
    // Сервер отвечает ПО ВВОДУ, и уже выбранный адрес в этот ответ попадать не
    // обязан. Без памяти фишка выродилась бы в `adr-a` ровно тогда, когда
    // человек ищет другое значение.
    show({ value: ["adr-a"] });

    await screen.findByText("10.0.0.5");
    list.mockResolvedValue({ addresses: [ADDRESSES.addresses[1]] });
    fireEvent.change(screen.getByLabelText("select-search"), { target: { value: "back" } });

    await waitFor(() => expect(optionLabels()).toContain("reserved-back · 10.0.0.9"));
    expect(screen.getByText("10.0.0.5")).toBeInTheDocument();
  });
});

describe("предел выбора", () => {
  it("объявлен вводу и удержан значением", async () => {
    const { onChange } = show({ maxItems: 1, value: ["adr-a"] });

    await screen.findByText("10.0.0.5");
    expect(field().getAttribute("data-max-count")).toBe("1");

    pick("adr-b");

    // Поле формы принадлежит форме: превышение здесь означало бы отказ владельца
    // уже на отправке.
    expect(onChange).toHaveBeenCalledWith(["adr-a"]);
  });

  it("без предела набор растёт (контроль)", async () => {
    const { onChange } = show({ value: ["adr-a"] });

    await screen.findByText("10.0.0.5");
    pick("adr-b");

    expect(onChange).toHaveBeenCalledWith(["adr-a", "adr-b"]);
  });
});

describe("«+ Создать …» — действие, а не значение", () => {
  it("в набор не попадает", async () => {
    const { onChange } = show({ createResource: "addresses" });

    await waitFor(() => expect(optionLabels().some((l) => l.startsWith("+ "))).toBe(true));
    pick("__create__");

    expect(onChange).toHaveBeenCalledWith([]);
    expect(onChange).not.toHaveBeenCalledWith(["__create__"]);
  });
});

describe("область поиска названа честно", () => {
  it("ввод уходит запросом — владелец адресов умеет сужать по имени", async () => {
    show();

    await waitFor(() => expect(list).toHaveBeenCalled());
    fireEvent.change(screen.getByLabelText("select-search"), { target: { value: "front" } });

    await waitFor(() =>
      expect(list).toHaveBeenCalledWith("/vpc/v1/addresses", {
        filter: 'name CONTAINS "front"',
        project_id: "prj-1",
        pageSize: "500",
      }),
    );
  });

  it("пустой ответ называет свою ОБЛАСТЬ, а не отсутствие ресурса", async () => {
    // Владелец адресов сужает сам, поэтому пустой ответ здесь — утверждение обо
    // всём списке, и текст обязан это сказать. У ресурса, который сужать не
    // умеет, тот же пустой ответ означал бы «нет среди загруженных», и выдать
    // одно за другое значит соврать о мире: продолжения у выпадающего списка
    // нет, проверить пользователю нечем.
    list.mockResolvedValue({ addresses: [] });
    show();

    expect(await screen.findByText(/искали по всему списку/i)).toBeInTheDocument();
  });
});
