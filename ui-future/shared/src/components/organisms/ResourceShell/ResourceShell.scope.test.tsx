// Вкладка дочернего ресурса называет ОБЛАСТЬ своих ручек (#373).
//
// ПРЕДМЕТ. Страница списка уже отвечает честно: стрелка сортировки там
// предлагается только на дочитанном списке, а строка поиска говорит, спрашивает
// она сервер или сужает загруженное. Вкладка связанного ресурса рисует ТУ ЖЕ
// таблицу теми же строками — и не говорила ни того, ни другого: у таблицы стояло
// умолчание «набор полон», а у строки поиска — подпись «Поиск по имени или
// идентификатору» без единого слова о том, по чему именно ищут.
//
// Пока предмет один и тот же, ответ не вправе зависеть от того, с какой стороны
// на него смотрят: на своей странице список честен, на вкладке — нет.
//
// ЧТО УТВЕРЖДАЕТСЯ. Наблюдаемое: что видит пользователь — есть ли у колонок
// стрелка и что написано в поле ввода. Ни один assert не заглядывает в пропсы.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

const CREATED = "2026-08-01T10:00:00Z";
const NETWORK = { id: "net-1", name: "основная", description: "", created_at: CREATED, labels: {} };

function subnetRow(n: number): Record<string, unknown> {
  return {
    id: `sub-${n}`,
    name: `подсеть-${n}`,
    description: "",
    created_at: CREATED,
    labels: {},
    network_id: "net-1",
    placement_type: "ZONAL",
    zone_id: "z-a",
    ipv4_cidr_primary: `10.0.${n}.0/24`,
  };
}

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

/**
 * Стенд карточки сети с вкладкой подсетей.
 *
 * `next` — курсор следующей страницы; пустая строка означает «список дочитан».
 * Непокрытый путь отвечает ОТКАЗОМ, а не пустым списком: молчаливый пустой
 * ответ на неожиданный адрес зеленил бы утверждения о пустоте таблицы.
 */
function stubNetworkWithSubnets(next: string): void {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const u = new URL(raw, "http://console.test");
    if (u.pathname === "/vpc/v1/networks/net-1") return jsonOk(NETWORK);
    if (u.pathname === "/vpc/v1/subnets") {
      return jsonOk({ subnets: [subnetRow(1), subnetRow(2)], nextPageToken: next });
    }
    if (u.pathname === "/geo/v1/zones") return jsonOk({ zones: [{ id: "z-a", name: "зона A" }], nextPageToken: "" });
    return Promise.resolve({
      ok: false,
      status: 404,
      statusText: "Not Found",
      text: () => Promise.resolve(JSON.stringify({ code: 5, message: `нет заглушки для ${u.pathname}` })),
    } as Response);
  }) as typeof globalThis.fetch;
}

function showSubnetsTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/networks/net-1/subnets"]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/projects/:projectId/vpc/:route/:uid/*" element={<ResourceShell spec={REGISTRY.networks} />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function sortableHeaders(): Element[] {
  return Array.from(document.querySelectorAll("th[data-sortable]"));
}

/** Поле ввода вкладки — единственное на этой поверхности. */
function searchBox(): HTMLInputElement {
  return screen.getAllByRole("textbox")[0] as HTMLInputElement;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

describe("вкладка связанного ресурса: порядок предлагается только на полном наборе", () => {
  it("курсор дочитан — стрелка сортировки есть (положительный контроль)", async () => {
    // Без этого утверждения «стрелок нет» означало бы и «убрали правильно», и
    // «сломали везде», а различить было бы нечем.
    stubNetworkWithSubnets("");
    showSubnetsTab();

    await screen.findByText("подсеть-1");
    await waitFor(() => expect(sortableHeaders().length).toBeGreaterThan(0));
  });

  it("за курсором есть ещё — стрелки сортировки нет", async () => {
    stubNetworkWithSubnets("cursor-1");
    showSubnetsTab();

    await screen.findByText("подсеть-1");
    // Кнопка продолжения — независимое свидетельство, что вкладка сама считает
    // список недочитанным. Без неё проба зеленела бы на пустой таблице.
    await screen.findByText("Показать ещё");
    expect(sortableHeaders()).toEqual([]);
  });
});

describe("вкладка связанного ресурса: строка поиска называет свою область", () => {
  it("курсор дочитан — поиск объявляет, что судит обо всём списке", async () => {
    stubNetworkWithSubnets("");
    showSubnetsTab();

    await screen.findByText("подсеть-1");
    expect(searchBox().placeholder).toMatch(/по всему списку/);
  });

  it("за курсором есть ещё — поиск объявляет, что судит о загруженном", async () => {
    stubNetworkWithSubnets("cursor-1");
    showSubnetsTab();

    await screen.findByText("подсеть-1");
    await screen.findByText("Показать ещё");
    // Именно это утверждение и было ложью: пустой ответ такого поиска читается
    // как «такого ресурса нет», хотя означает «нет среди прочитанного».
    expect(searchBox().placeholder).toMatch(/среди загруженных/);
  });
});
