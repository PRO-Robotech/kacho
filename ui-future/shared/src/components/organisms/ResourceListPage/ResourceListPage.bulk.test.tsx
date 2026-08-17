// Выделение строк и групповое удаление — с подтверждением ПО ЧИСЛУ (#373).
//
// ПРЕДМЕТ. Групповых действий не было как класса (`rowSelection` — ноль
// вхождений на всё дерево). Снять сорок тестовых ресурсов означало около двухсот
// действий мышью, и отобрать их было нечем. Эксплуатация парка больше пары
// десятков ресурсов шла мимо консоли.
//
// ПОЧЕМУ ПОДТВЕРЖДЕНИЕ ПО ЧИСЛУ, А НЕ ПО ИМЕНИ КАЖДОГО. Перечень из сорока имён
// не читают — его прокручивают до кнопки. Число же отвечает на единственный
// вопрос, который здесь задают себе: «столько я и выделял?» Ошибка выделения
// проявляется именно в числе, а не в имени сорок первого.
//
// ОТДЕЛЬНО — ЧТО УДАЛЯЕТСЯ. Удаляются ВЫДЕЛЕННЫЕ строки, а не «всё, что подошло
// под фильтр»: второе снесло бы и то, что за курсором и на экран не приезжало.

import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { requestUrl } from "@shared/test/fetch-capture";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;
interface Call {
  method: string;
  url: string;
}
let calls: Call[] = [];

function stubList(payloadKey: string, rows: Record<string, unknown>[]) {
  calls = [];
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    calls.push({ method, url: requestUrl(input) });
    if (method === "DELETE") {
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        text: () => Promise.resolve(JSON.stringify({ operation: { id: "op-1", done: true } })),
      } as Response);
    }
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: rows })),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderList(spec: (typeof REGISTRY)[string]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/p1/vpc/networks"]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const строки = [
  { id: "net-1", name: "первая" },
  { id: "net-2", name: "вторая" },
  { id: "net-3", name: "третья" },
];

/** Флажок строки, найденной по имени. */
function отметить(имя: string) {
  const row = screen.getAllByText(имя).find((n) => n.closest("tr"))!.closest("tr")!;
  fireEvent.click(within(row as HTMLElement).getByRole("checkbox"));
}

describe("групповое удаление", () => {
  it("строки выделяются, и число выделенных показано", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, строки);
    renderList(spec);
    await screen.findAllByText("первая");

    отметить("первая");
    отметить("третья");

    expect(await screen.findByText(/Выделено: 2/)).toBeTruthy();
  });

  it("подтверждение называет ЧИСЛО, а не перечень имён", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, строки);
    renderList(spec);
    await screen.findAllByText("первая");
    отметить("первая");
    отметить("вторая");

    fireEvent.click(screen.getByRole("button", { name: /Удалить выделенные/ }));

    const окно = await screen.findByText(/Удалить 2/);
    expect(окно).toBeTruthy();
  });

  it("удаляются ровно выделенные — по одному запросу на каждую", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, строки);
    renderList(spec);
    await screen.findAllByText("первая");
    отметить("первая");
    отметить("третья");

    fireEvent.click(screen.getByRole("button", { name: /Удалить выделенные/ }));
    // Подтверждение нажимается В ОКНЕ. Слово «Удалить» стоит на странице не
    // единожды: то же слово несёт пункт меню КАЖДОЙ строки. Пока общий дублёр
    // не рисовал состав меню, поиск по всей странице был однозначен случайно —
    // и обещал бы то же самое на продукте, где он неоднозначен.
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(calls.filter((c) => c.method === "DELETE")).toHaveLength(2));
    const цели = calls.filter((c) => c.method === "DELETE").map((c) => c.url.split("/").pop());
    expect(цели.sort()).toEqual(["net-1", "net-3"]);
    // Отрицание в паре с положительным: невыделенная строка не тронута.
    expect(цели).not.toContain("net-2");
  });

  it("без выделения кнопка группового действия не предлагается", async () => {
    // Кнопка, которая ничего не сделает, — приглашение к действию без предмета.
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, строки);
    renderList(spec);
    await screen.findAllByText("первая");
    expect(screen.queryByRole("button", { name: /Удалить выделенные/ })).toBeNull();
  });

  it("ресурс, который нельзя удалять, выделения не предлагает", async () => {
    // Иначе выделение ведёт к действию, которого у ресурса нет: у справочника
    // размещения удаления из этой поверхности не существует.
    const spec = REGISTRY["machine-types"];
    expect(spec.ops.delete).toBe(false);
    stubList(spec.payloadKey, [{ id: "mt-1", name: "малый" }]);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/system/machine-types"]}>
          <PageHeaderSlotProvider>
            <HeaderRightSlot />
            <ResourceListPage spec={spec} panelForms />
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await screen.findAllByText("малый");
    // Флажок выделения ищется В ТАБЛИЦЕ, а не по всей странице: настройка
    // видимости колонок — тоже флажки, и она законна на любом списке. Пока
    // общий дублёр не рисовал содержимое выпадающего блока, «на странице нет
    // ни одного флажка» было верно случайно и утверждало не про свой предмет.
    expect(within(screen.getByRole("table")).queryAllByRole("checkbox")).toEqual([]);
  });
});
