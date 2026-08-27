// Дерево размещения интерфейса «сеть → подсеть → адрес» — ЧТО ИМЕННО предложено
// оператору на выбор.
//
// ЧТО ДЕРЖИТСЯ. Варианты приходят пропом `options`, а не детьми: проходной
// <div> не показывает НИ ОДНОГО, и утверждение «сеть можно выбрать» было бы
// истинно на форме, где выбирать нечего вовсе. Хуже того — истинно и тогда,
// когда список сетей не доехал: «выбора нет» и «выбор пуст» выглядели бы
// одинаково.
//
// Здесь они разведены парой: загруженная сеть предложена по имени (положительно)
// и, когда сетей нет, не предложено ничего, кроме приглашения выбрать
// (обратно). Без второй половины первая зеленела бы на заменителе, рисующем
// подпись всегда.
//
// ПОЧЕМУ ПО ТЕКСТУ, а не по разметке. Общий заменитель подменяет дерево выбора
// обычным списком, и его разметка — упрощение, а не форма настоящего antd.
// Утверждения ниже читают ТЕКСТ вариантов: он у настоящего и у заменителя один
// и тот же, потому что это и есть то, что видит оператор.
//
// Инъекция в обе стороны: `Cascader: Component` в заменителе — красное;
// заменитель на месте — молчание.

import { jest } from "@jest/globals";
import { render, waitFor } from "@testing-library/react";
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

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <NicSpecFields
        pathPrefix={PREFIX}
        value={{ network_interface_specs: [{}] }}
        onChange={() => undefined}
      />
    </QueryClientProvider>,
  );
}

/** Подписи предложенных вариантов — то, что оператор читает в списке. */
const offered = () => Array.from(document.querySelectorAll("option")).map((o) => o.textContent ?? "");

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
      expect(offered()).toContain("основная");
    });
  });

  it("когда сетей нет, выбирать нечего, но приглашение остаётся — контроль в обратную сторону", async () => {
    list.mockImplementation(() => Promise.resolve({ networks: [], subnets: [], addresses: [] }));

    show();

    // Приглашение — это и есть то, что отличает «выбор пуст» от «поля нет».
    await waitFor(() => {
      expect(offered()).toContain("Выберите сеть → подсеть → адрес");
    });
    expect(offered()).not.toContain("основная");
  });
});
