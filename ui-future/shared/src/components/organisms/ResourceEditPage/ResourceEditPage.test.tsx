// Полноэкранная правка ресурса. Предмет — три свойства, каждое из которых
// ломается молча и наблюдается только по ушедшему запросу:
//
//  1. начальное состояние читается с ТОЙ проекции, где живут правимые поля. У
//     двухпроекционного ресурса это внутренняя: прочитав публичную, форма
//     показала бы пустое поле там, где значение есть, — и оператор перезаписал
//     бы его вслепую;
//  2. в тело уходят ТОЛЬКО поля маски. Форма гидратирована ответом чтения, где
//     есть `id`, `createdAt`, зеркала и статус, — ни одно из них не является
//     полем сообщения правки, и досланное поле край отвергает целиком;
//  3. правка без изменений запроса не делает вовсе, а возвращает на карточку.
//
// Запрос перехватывается на уровне `fetch`: утверждается АДРЕС, МЕТОД и ТЕЛО —
// то, что увидел бы край, а не то, что написано в модуле.

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";
import { requestBody, requestUrl } from "@shared/test/fetch-capture";
import { ResourceEditPage } from "./ResourceEditPage";

interface Sent {
  url: string;
  method: string;
  body: Record<string, unknown> | null;
}

const realFetch = globalThis.fetch;
let sent: Sent[] = [];

function stubFetch(read: Record<string, unknown>) {
  sent = [];
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = requestUrl(input);
    const method = init?.method ?? "GET";
    sent.push({ url, method, body: requestBody(init?.body) });
    const payload = method === "GET" ? read : { id: "opr-1", done: true };
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
  };
}

function renderEdit(spec: ResourceSpec) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/regions/reg-1/edit"]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/system/regions/:uid/edit" element={<ResourceEditPage spec={spec} />} />
            <Route path="/system/regions/:uid" element={<div>карточка региона</div>} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const save = async () => userEvent.click(await screen.findByRole("button", { name: "Сохранить" }));
const reads = () => sent.filter((s) => s.method === "GET");
const patches = () => sent.filter((s) => s.method !== "GET");

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceEditPage", () => {
  it("начальное состояние читается с проекции, где живут правимые поля", async () => {
    // У региона правимые поля объявлены на внутренней проекции — читать надо
    // оттуда, а не с публичной.
    const spec = REGISTRY.regions;
    stubFetch({ id: "reg-1", name: "было", description: "" });
    renderEdit(spec);

    expect(await screen.findByDisplayValue("было")).toBeInTheDocument();
    const expected = spec.admin?.readForEdit ? spec.admin.basePath : spec.apiPath;
    await waitFor(() => expect(reads().length).toBeGreaterThan(0));
    expect(reads()[0].url).toContain(`${expected}/reg-1`);
  });

  it("правка без изменений запроса не делает и возвращает на карточку", async () => {
    stubFetch({ id: "reg-1", name: "было", description: "" });
    renderEdit(REGISTRY.regions);

    await screen.findByDisplayValue("было");
    await save();

    expect(await screen.findByText("карточка региона")).toBeInTheDocument();
    expect(patches()).toHaveLength(0);
  });

  it("в тело уходит только тронутое — ни id, ни зеркал чтения", async () => {
    stubFetch({
      id: "reg-1",
      name: "было",
      description: "",
      createdAt: "2026-08-01T00:00:00Z",
      status: "ACTIVE",
    });
    renderEdit(REGISTRY.regions);

    const nameInput = await screen.findByDisplayValue("было");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "стало");
    await save();

    await waitFor(() => expect(patches().length).toBe(1));
    const body = patches()[0].body!;
    expect(body.name).toBe("стало");
    expect(String(body.update_mask ?? body.updateMask)).toContain("name");
    // Поля ответа чтения полями сообщения правки не являются: досланные, они
    // отвергают всю правку.
    for (const alien of ["id", "createdAt", "created_at", "status"]) {
      expect(body).not.toHaveProperty(alien);
    }
  });

  it("ресурс без описания формы правится через API, и об этом сказано", async () => {
    stubFetch({ id: "reg-1", name: "было" });
    renderEdit({ ...REGISTRY.regions, fields: undefined });

    expect(await screen.findByText(/нет form-schema/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Сохранить" })).not.toBeInTheDocument();
  });
});
