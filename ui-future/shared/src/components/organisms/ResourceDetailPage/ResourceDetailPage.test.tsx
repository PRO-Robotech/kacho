// Карточка ресурса. Предмет — три состояния, которые легко перепутать и
// которые для человека означают РАЗНОЕ:
//
//  1. чтение отказало → показан отказ края с его текстом. Пустая карточка
//     читается как «ресурс есть, но без полей»;
//  2. чтение прошло, а ресурса нет → отдельное «не найден». Это не то же самое,
//     что отказ: возвращаться и что делать дальше — разные ответы;
//  3. ресурс есть → показан «Обзор» с его полями.
//
// Отдельно закреплён обратно-совместимый адрес `…/edit`: он обязан ПЕРЕВОДИТЬ
// на карточку с открытым окном правки, а не показывать пустоту. Ссылка из
// закладок и из старых писем живёт долго, и её тихая поломка выглядит как
// «карточка перестала открываться».

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceDetailPage } from "./ResourceDetailPage";

const realFetch = globalThis.fetch;

type Reply = { ok: true; body: Record<string, unknown> } | { ok: false; status: number; body: unknown };

function stubFetch(reply: Reply) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: reply.ok,
      status: reply.ok ? 200 : reply.status,
      statusText: reply.ok ? "OK" : "Error",
      text: () => Promise.resolve(JSON.stringify(reply.ok ? reply.body : reply.body)),
    } as Response);
}

function Address() {
  const loc = useLocation();
  return <div data-testid="address">{`${loc.pathname}${loc.search}`}</div>;
}

function show(entry: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[entry]}>
        <PageHeaderSlotProvider>
          <Address />
          <Routes>
            <Route path="/projects/:projectId/vpc/networks/:uid" element={<ResourceDetailPage spec={REGISTRY.networks} />} />
            <Route
              path="/projects/:projectId/vpc/networks/:uid/edit"
              element={<ResourceDetailPage spec={REGISTRY.networks} />}
            />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const address = () => screen.getByTestId("address").textContent ?? "";

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceDetailPage", () => {
  it("показывает обзор с полями ресурса", async () => {
    stubFetch({
      ok: true,
      body: { id: "net-1", name: "основная", description: "опорная сеть", created_at: "2026-08-01T10:00:00Z" },
    });
    show("/projects/prj-1/vpc/networks/net-1");

    expect(await screen.findByText("Общее")).toBeInTheDocument();
    expect(screen.getByText("опорная сеть")).toBeInTheDocument();
  });

  it("отказ края показан его текстом, а не пустой карточкой", async () => {
    stubFetch({ ok: false, status: 403, body: { code: 7, message: "Network net-1 not found" } });
    show("/projects/prj-1/vpc/networks/net-1");

    expect(await screen.findByText(/not found/i)).toBeInTheDocument();
    expect(screen.queryByText("Общее")).not.toBeInTheDocument();
    // Возврат обязан быть предложен: тупик без выхода — тоже отказ.
    expect(screen.getByRole("button", { name: /Назад/ })).toBeInTheDocument();
  });

  it("пустой ответ — это «не найден», а не отказ", async () => {
    // Два разных состояния: у них разные ответы на вопрос «что делать дальше».
    stubFetch({ ok: true, body: null as unknown as Record<string, unknown> });
    show("/projects/prj-1/vpc/networks/net-1");

    expect(await screen.findByText("Ресурс не найден.")).toBeInTheDocument();
  });

  it("старый адрес правки переводит на карточку с открытым окном", async () => {
    stubFetch({ ok: true, body: { id: "net-1", name: "основная", created_at: "2026-08-01T10:00:00Z" } });
    show("/projects/prj-1/vpc/networks/net-1/edit");

    await waitFor(() => expect(address()).not.toContain("/edit"));
    expect(address()).toContain("modal=networks-edit");
    expect(address()).toContain("id=net-1");
  });
});
