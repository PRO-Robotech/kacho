// Дерево размещения интерфейса «сеть → подсеть → адрес» — ЧТО предложено
// оператору на выбор и ЧТО оказывается в форме, когда он выбрал.
//
// ЧТО ДЕРЖИТСЯ — две стороны, и вторая ломалась молча.
//
// ПЕРВАЯ, состав предложенного. Варианты приходят пропом `options`, а не
// детьми: проходной <div> не показывает НИ ОДНОГО, и утверждение «сеть можно
// выбрать» было бы истинно на форме, где выбирать нечего вовсе. Хуже того —
// истинно и тогда, когда список сетей не доехал: «выбора нет» и «выбор пуст»
// выглядели бы одинаково. Здесь они разведены парой: загруженная сеть
// предложена по имени (положительно) и, когда сетей нет, не предложено ничего,
// кроме приглашения выбрать (обратно). Без второй половины первая зеленела бы
// на заменителе, рисующем подпись всегда.
//
// ВТОРАЯ, форма выбранного (#1350). Настоящее дерево зовёт `onChange` МАССИВОМ
// ПУТИ (`@rc-component/cascader`, `lib/Cascader.js:111-122`), и носитель на это
// написан: `arr[0]`/`arr[1]`/`arr[2]` — сеть, подсеть, адрес. Заменитель,
// отдававший дерево плоским списком, звал `onChange` СТРОКОЙ — и те же индексы
// читали ЗНАКИ: выбор сети `net-1` клал в форму `subnet_id: "e"`, второй знак
// строки. Проба, написанная поверх такого заменителя, закрепила бы это нормой.
//
// Утверждается НАБЛЮДАЕМОЕ: текст вариантов на каждом уровне и значение,
// которое форма отправит краю. Роли, по которым идёт поиск, производит сама
// библиотека: уровень — `role="menu"` (`lib/OptionList/Column.js:98-102`),
// вариант — `role="menuitemcheckbox"` (там же, :161).
//
// Инъекция в обе стороны: `Cascader: Select` в заменителе — красное; заменитель
// на месте — молчание.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError: class ApiError extends Error {},
}));

const { contextApi } = await import("@shared/lib/context-store");
const { NicSpecFields } = await import("./NicSpecFields");

const PREFIX = "network_interface_specs[0]";

function show(onChange: (v: unknown) => void = () => undefined) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <NicSpecFields pathPrefix={PREFIX} value={{ network_interface_specs: [{}] }} onChange={onChange} />
    </QueryClientProvider>,
  );
}

/** Уровни дерева: настоящая библиотека рисует каждый как `role="menu"`. */
const levels = () => screen.getAllByRole("menu");
/** Подписи вариантов уровня — то, что оператор читает в столбце. */
const offered = (level: number) =>
  within(levels()[level])
    .queryAllByRole("menuitemcheckbox")
    .map((o) => o.textContent ?? "");
/** Выбор варианта уровня — то, что оператор делает мышью. */
const pick = (level: number, name: string | RegExp) =>
  fireEvent.click(within(levels()[level]).getByRole("menuitemcheckbox", { name }));

beforeEach(() => {
  jest.clearAllMocks();
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
});

describe("NicSpecFields — дерево размещения интерфейса", () => {
  it("предлагает выбрать загруженную сеть по её имени", async () => {
    list.mockImplementation((path: string) => {
      if (path.includes("/networks")) return Promise.resolve({ networks: [{ id: "net-1", name: "основная" }] });
      if (path.includes("/subnets")) return Promise.resolve({ subnets: [] });
      if (path.includes("/addresses")) return Promise.resolve({ addresses: [] });
      return Promise.resolve({});
    });

    show();

    await waitFor(() => {
      expect(offered(0)).toContain("основная");
    });
  });

  it("когда сетей нет, выбирать нечего, но приглашение остаётся — контроль в обратную сторону", async () => {
    list.mockImplementation(() => Promise.resolve({ networks: [], subnets: [], addresses: [] }));

    show();

    // Приглашение — это и есть то, что отличает «выбор пуст» от «поля нет».
    await waitFor(() => {
      expect(screen.getByText("Выберите сеть → подсеть → адрес")).toBeInTheDocument();
    });
    expect(offered(0)).not.toContain("основная");
  });
});

describe("NicSpecFields — выбор адреса по уровням дерева", () => {
  beforeEach(() => {
    list.mockImplementation((path: string) => {
      if (path.includes("/networks")) return Promise.resolve({ networks: [{ id: "net-1", name: "основная" }] });
      if (path.includes("/subnets"))
        return Promise.resolve({ subnets: [{ id: "sub-1", name: "фронт", network_id: "net-1" }] });
      if (path.includes("/addresses"))
        return Promise.resolve({
          addresses: [
            { id: "addr-1", name: "веб", internal_ipv4_address: { subnet_id: "sub-1", address: "10.0.0.5" } },
          ],
        });
      return Promise.resolve({});
    });
  });

  it("раскрывает подсеть и адрес — уровни ниже первого существуют", async () => {
    show();
    await waitFor(() => expect(offered(0)).toContain("основная"));

    pick(0, "основная");
    // Второй уровень — подсети выбранной сети. Плоский список их не рисует вовсе.
    expect(offered(1)).toContain("фронт");

    pick(1, "фронт");
    // Третий — адреса подсети, «(без адреса)» и приглашение создать.
    expect(offered(2)).toEqual(["(без адреса)", "веб — 10.0.0.5", "+ Создать адрес…"]);
  });

  it("выбранный адрес приезжает в форму подсетью и адресом, а не знаками строки", async () => {
    const onChange = jest.fn();
    show(onChange);
    await waitFor(() => expect(offered(0)).toContain("основная"));

    pick(0, "основная");
    pick(1, "фронт");
    pick(2, "веб — 10.0.0.5");

    // Наблюдаемое: что форма отправит краю после выбора оператора.
    const committed = onChange.mock.calls.at(-1)?.[0] as Record<string, Array<Record<string, unknown>>>;
    const nic = committed.network_interface_specs[0];
    expect(nic.subnet_id).toBe("sub-1");
    expect(nic.primary_v4_address_spec).toEqual({ address: "10.0.0.5" });
    // Обратная сторона того же утверждения: путь читается элементами, а не
    // знаками. `"e"` — второй знак строки `"net-1"`, и он здесь и стоял.
    expect(nic.subnet_id).not.toBe("e");
    expect(nic._addr_cascader).toEqual(["net-1", "sub-1", "addr-1"]);
  });
});
