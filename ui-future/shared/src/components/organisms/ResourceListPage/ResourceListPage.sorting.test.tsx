// Сортировка не предлагается там, где она СОЛГАЛА БЫ (#373).
//
// ПРЕДМЕТ. Стрелка в шапке сортирует массив загруженных строк. Пока за курсором
// есть непрочитанные страницы, это не «сортировка списка», а перестановка
// случайной его части: «Показать ещё» подмешивает новые строки, и порядок молча
// меняется на глазах у читателя. Верхняя строка такой таблицы не является ни
// первой, ни последней — она первая среди прочитанных, а этого читатель не знает.
//
// РЕШЕНИЕ. Порядок серверу не заказывается: поле порядка снято с контракта
// осознанно, список отдаётся курсором по `(created_at, id)`. Значит выбор один —
// сортировать только тогда, когда сортировать есть что целиком.
//
// ПОЧЕМУ НЕ «ПОДПИСЬ ВМЕСТО ЗАПРЕТА». Подпись под стрелкой не меняет того, что
// пользователь ВИДИТ: он видит упорядоченную таблицу и читает её как ответ.
// Сортировка, которая лжёт, хуже отсутствующей.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;

/** `nextToken` непуст — за курсором есть непрочитанные страницы. */
function stubList(payloadKey: string, rows: Record<string, unknown>[], nextToken: string) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: rows, next_page_token: nextToken })),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderList(spec: (typeof REGISTRY)[string]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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

function sortableHeaders(): Element[] {
  return Array.from(document.querySelectorAll("th[data-sortable]"));
}

describe("сортировка предлагается только когда прочитан весь список", () => {
  it("список ПРОЧИТАН целиком — сортировка есть", async () => {
    // Положительный контроль. Без него «нет сортировки» означало бы и «мы её
    // убрали правильно», и «мы её сломали везде», а различить было бы нечем.
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }], "");
    renderList(spec);
    await screen.findAllByText("netto");
    await waitFor(() => expect(sortableHeaders().length).toBeGreaterThan(0));
  });

  it("за курсором есть ещё страницы — сортировки нет", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }], "cursor-1");
    renderList(spec);
    await screen.findAllByText("netto");
    // Кнопка догрузки — независимое свидетельство, что страница действительно
    // считает список неполным; без неё проба могла бы зеленеть на пустой таблице.
    await screen.findByText("Показать ещё");
    expect(sortableHeaders()).toEqual([]);
  });
});
