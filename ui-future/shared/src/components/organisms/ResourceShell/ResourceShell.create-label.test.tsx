// Подпись «создать» на вкладке связанного ресурса называет ДЕЙСТВИЕ.
//
// ЧТО ЗДЕСЬ УТВЕРЖДАЛОСЬ ПРЕЖДЕ. Кнопка в шапке карточки писала «Создать
// таблица маршрутов» — именительный падеж там, где «создать» управляет
// винительным, — и проба требовала склонённой формы из реестра («Создать
// таблицу маршрутов»).
//
// ЧТО ИЗМЕНИЛОСЬ. Решением владельца кнопка называет действие: «Создать».
// Предмет назван РЯДОМ — вкладкой, на которой кнопка стоит («Таблицы
// маршрутов»), — и повторять его в двадцати точках правее незачем. Проба
// правится вместе с предметом.
//
// ЧЕМ ЗАКРЫТО ОСЛАБЛЕНИЕ. «На кнопке имени ребёнка нет» выполняется и на
// экране, где не отрисовалось ничего, поэтому утверждается ПАРА: имени нет на
// кнопке — и оно есть на вкладке. Обе половины опровержимы: вернут имя на
// кнопку — покраснеет первая; уберут подпись вкладки — вторая.
//
// ПОЧЕМУ ПО-ПРЕЖНЕМУ ТАБЛИЦА МАРШРУТОВ. У мужского и среднего рода винительный
// совпадает с именительным, поэтому на «шлюзе» и «IP-адресе» возврат ЛЮБОЙ из
// двух прежних сборок подписи выглядел бы одинаково. «Таблица маршрутов» →
// «таблицу маршрутов» — формы различаются, и обе названы ниже дословно.
//
// Механизм склонения из продукта не ушёл и здесь не сторожится: полная форма
// осталась там, где предмет рядом не назван, и её держит
// `shared/src/lib/resource-label.test.ts`.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

const NETWORK = {
  id: "net-1",
  project_id: "prj-1",
  name: "сеть-1",
  description: "",
  created_at: "2026-01-01T00:00:00Z",
  labels: {},
};

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

beforeEach(() => {
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = new URL(requestUrl(input), "http://console.test");
    if (url.pathname === "/vpc/v1/networks/net-1") return jsonOk(NETWORK);
    return jsonOk({ route_tables: [], subnets: [], operations: [], nextPageToken: "" });
  };
});

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

function открытьВкладку(tab: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/vpc/networks/net-1/${tab}`]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <Routes>
            <Route path="/projects/:projectId/vpc/:route/:uid/*" element={<ResourceShell spec={REGISTRY.networks} />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("вкладка связанного ресурса: подпись действия", () => {
  it("кнопка в шапке называет действие, ребёнка называет вкладка", async () => {
    открытьВкладку("route-tables");

    await waitFor(() => expect(screen.getByRole("button", { name: /Создать/ })).toBeInTheDocument());
    const подпись = screen.getByRole("button", { name: /Создать/ }).textContent ?? "";

    expect(подпись).toBe("Создать");
    // Обе прежние сборки названы дословно: и та, что стояла в продукте, и та,
    // которой её чинили. Возврат любой красит эту пробу.
    expect(подпись).not.toContain("таблицу маршрутов");
    expect(подпись).not.toContain("таблица маршрутов");

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к трём отрицаниям: имя ребёнка на экране ЕСТЬ —
    // его несёт вкладка, на которой кнопка и стоит.
    expect(screen.getAllByText("Таблицы маршрутов").length).toBeGreaterThan(0);
  });

  it("вкладка обзора кнопки создания ребёнка не даёт (парное отрицание)", async () => {
    // Без этого утверждение выше зеленело бы на оболочке, которая ставит одну и
    // ту же кнопку на всех вкладках сразу.
    //
    // Отрицание перенацелено вместе с подписью: прежде оно искало «Создать
    // таблицу маршрутов» — строку, которой после решения владельца нет НИГДЕ, —
    // и потому было истинно по построению. Спрашивается ровно та подпись,
    // которую продукт теперь производит.
    открытьВкладку("");

    // Имя ресурса на карточке стоит дважды — заголовком и строкой обзора,
    // поэтому ждём ЛЮБОЕ его вхождение: предмет пробы не в том, сколько их.
    await waitFor(() => expect(screen.getAllByText("сеть-1").length).toBeGreaterThan(0));
    expect(screen.queryByRole("button", { name: /Создать/ })).toBeNull();
  });
});
