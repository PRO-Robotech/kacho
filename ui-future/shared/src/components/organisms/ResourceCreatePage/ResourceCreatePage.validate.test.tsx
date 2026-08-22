// The spec's own client-side invariant runs before the request goes out.
//
// `ResourceSpec.validate` reads the form's UI discriminators (which branch of a
// oneof the operator picked) and rejects combinations the request body cannot
// even express. The inline create form has always called it; the full-page
// create form did not — so the same spec was checked in a modal and unchecked on
// a page, and the four standalone remotes carried the call only in their own
// copy of this page. The field was declared on the spec and read by nobody here:
// accepted and ignored.

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";
import { useToasts } from "@shared/lib/toast";
import { ResourceCreatePage } from "./ResourceCreatePage";

function ToastProbe() {
  const toasts = useToasts();
  return (
    <ul>
      {toasts.map((t) => (
        <li key={t.id}>{t.message}</li>
      ))}
    </ul>
  );
}

const realFetch = globalThis.fetch;
let methods: string[] = [];

function stubFetch() {
  methods = [];
  globalThis.fetch = (_input: RequestInfo | URL, init?: RequestInit) => {
    methods.push(init?.method ?? "GET");
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ id: "opr-1", done: true })),
    } as Response);
  };
}

function renderCreate(spec: ResourceSpec) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/regions/create"]}>
        <PageHeaderSlotProvider>
          <ToastProbe />
          <Routes>
            <Route path="/system/regions/create" element={<ResourceCreatePage spec={spec} />} />
            <Route path="/system/regions" element={<div>список регионов</div>} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function submit() {
  // Подпись кнопки — короткое «Создать» (решение владельца; канон консоли §3 и
  // §8: кнопка называет ДЕЙСТВИЕ, предмет уже назван заголовком над ней).
  //
  // Имя задано СТРОКОЙ, а не выражением: строка сверяется целиком, поэтому
  // вернувшееся склонение («Создать регион») эту пробу роняет, а не проходит
  // подстрокой.
  //
  // Что заголовок предмет НАЗЫВАЕТ — утверждает соседний файл этого же
  // каталога (`ResourceCreatePage.geo.test.tsx`, проба «кнопка называет
  // действие, предмет назван заголовком над ней»); без него короткая подпись
  // здесь была бы неотличима от страницы, которая не говорит, что создаётся.
  await userEvent.click(await screen.findByRole("button", { name: "Создать" }));
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceCreatePage — spec.validate", () => {
  it("does not send the request when the spec rejects the form state", async () => {
    stubFetch();
    renderCreate({ ...REGISTRY.regions, validate: () => "зона обязана принадлежать региону" });
    // Подлежащим сообщения служит идентификатор: подписи у каталога размещения
    // нет (#716), и автосгенерированного имени страница ему не подставляет —
    // идентификатор администратор вводит своими руками. Утверждение от этого
    // строже прежнего: раньше сверялось «какое-то имя есть», теперь — что
    // названо ровно то, что оператор ввёл.
    await userEvent.type(await screen.findByPlaceholderText("region-1"), "ru-central1");
    await submit();

    // Отказ собственной проверки читается ТОЙ ЖЕ формой, что отказ края
    // («<Ресурс> не создан: <причина>»): для пользователя это один и тот же
    // исход — ресурс не создан, — и знать, на чьей стороне это выяснилось,
    // ему незачем. Причина при этом сохраняется дословно.
    expect(
      await screen.findByText("Регион ru-central1 не создан: зона обязана принадлежать региону"),
    ).toBeInTheDocument();
    await waitFor(() => expect(methods.includes("POST")).toBe(false));
    expect(screen.queryByText("список регионов")).not.toBeInTheDocument();
  });

  it("sends the request when the spec accepts the form state", async () => {
    stubFetch();
    renderCreate({ ...REGISTRY.regions, validate: () => null });
    // Положительный контроль обязан подавать ЗАКОННЫЙ ввод: обязательный
    // идентификатор региона форма теперь требует до отправки, и без него проба
    // утверждала бы «запрос ушёл» о состоянии, в котором он уходить не должен.
    await userEvent.type(await screen.findByPlaceholderText("region-1"), "ru-central1");
    await submit();

    await waitFor(() => expect(methods.includes("POST")).toBe(true));
  });
});
