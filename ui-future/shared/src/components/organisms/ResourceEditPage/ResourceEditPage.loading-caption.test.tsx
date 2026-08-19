// Подпись ожидания — то, ЧТО ВИДИТ ОПЕРАТОР, пока страница читает ресурс.
//
// ПРЕДМЕТ (#625). Подпись приходит вертушке ПРОПОМ (`tip`), а не детьми. Пока
// общий заменитель подменял `Spin` пустым `<div>{children}</div>`, подпись
// уезжала в АТРИБУТ DOM — настоящий antd таких атрибутов не производит ни
// одного, — и состояние ожидания было неотличимо от ПУСТОГО ЭКРАНА: проба
// «страница что-то показывает» зеленела бы и на странице, которая не показывает
// ничего.
//
// Почему это не косметика. Ожидание и отказ выглядят для оператора одинаково,
// если ожидание молчит: он читает пустоту как «форма не открылась» и уходит.
// Подпись — единственное, что отличает одно от другого до прихода ответа.
//
// Утверждается наблюдаемое: текст на экране, пока ответа нет, и его исчезновение
// после ответа (парный контроль — иначе утверждение зеленело бы на странице,
// которая показывает «Загрузка…» всегда).

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceEditPage } from "./ResourceEditPage";

const realFetch = globalThis.fetch;

/** Край, который ЕЩЁ НЕ ОТВЕТИЛ: ровно то состояние, ради которого подпись есть. */
function stubPending() {
  globalThis.fetch = () => new Promise<Response>(() => undefined);
}

/** Край, который ответил: контроль в обратную сторону. */
function stubAnswered(body: Record<string, unknown>) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body)),
    } as Response);
}

function renderEdit() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/regions/reg-1/edit"]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/system/regions/:uid/edit" element={<ResourceEditPage spec={REGISTRY.regions} />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceEditPage — подпись ожидания", () => {
  it("пока ответа нет, оператор ВИДИТ, что идёт загрузка", async () => {
    stubPending();
    renderEdit();

    expect(await screen.findByText("Загрузка…")).toBeInTheDocument();
  });

  it("после ответа подписи ожидания на экране НЕТ", async () => {
    // Парный контроль: без него утверждение выше зеленело бы на странице,
    // которая показывает «Загрузка…» и после прихода данных.
    stubAnswered({ id: "reg-1", country_code: "RU", description: "" });
    renderEdit();

    expect(await screen.findByDisplayValue("RU")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Загрузка…")).not.toBeInTheDocument());
  });
});
