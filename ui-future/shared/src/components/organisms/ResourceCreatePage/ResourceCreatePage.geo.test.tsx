// End-to-end behaviour of the create page for a resource with an admin plane,
// driven through the real API client (fetch is the only thing stubbed).
//
// Three things this pins, each of which was wrong or missing before:
//   * the POST goes to the internal admin path. The public geo path serves
//     reads only — a POST there is not routed to anything, so the previous
//     wiring sent every Create into the void.
//   * a done Operation carrying an error is a failure. Its metadata already
//     holds the id that was allocated before the failure, so reading the id
//     without the error reports a resource that does not exist.
//   * the warnings channel on Create metadata is shown. The operation succeeds
//     while the region is created CLOSED to placement; swallowing it leaves the
//     operator believing the entry is usable.

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useToasts } from "@shared/lib/toast";
import { ResourceCreatePage } from "./ResourceCreatePage";

/** Читает ту же toast-очередь, что и настоящий Toaster ремоута. */
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

interface Call {
  url: string;
  method: string;
  body: unknown;
}

const realFetch = globalThis.fetch;
let calls: Call[] = [];

/** Stub fetch: POST answers with `createAnswer`, the operation poll with `opAnswer`. */
function stubFetch(createAnswer: unknown, opAnswer: unknown) {
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    const payload = method === "POST" ? createAnswer : opAnswer;
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
  };
}

function renderCreate() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/regions/create"]}>
        <PageHeaderSlotProvider>
          <ToastProbe />
          <Routes>
            <Route path="/system/regions/create" element={<ResourceCreatePage spec={REGISTRY.regions} />} />
            <Route path="/system/regions" element={<div>список регионов</div>} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function submit() {
  // Именно кнопка, не текст: тосты об ошибке начинаются с тех же слов
  // («Создать Регион: …»), и поиск по тексту нашёл бы оба.
  const button = await screen.findByRole("button", { name: /Создать регион/i });
  await userEvent.click(button);
}

beforeEach(() => {
  calls = [];
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceCreatePage — Region", () => {
  it("posts to the internal admin path, not the read-only public one", async () => {
    stubFetch({ id: "opr-1", done: true }, { id: "opr-1", done: true });
    renderCreate();
    await submit();

    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST")!;
    expect(post.url).toBe("/geo/v1/internal/regions");
    expect(post.url).not.toBe("/geo/v1/regions");
  });

  it("seeds the create body closed to placement, matching the service default", async () => {
    stubFetch({ id: "opr-1", done: true }, { id: "opr-1", done: true });
    renderCreate();
    await submit();

    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST")!;
    expect((post.body as Record<string, unknown>).status).toBe("DOWN");
  });

  it("navigates away only once the operation is done without an error", async () => {
    stubFetch({ id: "opr-1", done: true }, { id: "opr-1", done: true });
    renderCreate();
    await submit();

    expect(await screen.findByText("список регионов")).toBeInTheDocument();
  });

  it("does not report success for a done operation that carries an error", async () => {
    stubFetch(
      { id: "opr-1", done: true },
      {
        id: "opr-1",
        done: true,
        // The id was allocated before the failure — it is present either way.
        metadata: { "@type": "…CreateRegionMetadata", region_id: "ru-central1" },
        error: { code: 6, message: "Region ru-central1 already exists" },
      },
    );
    renderCreate();
    await submit();

    expect(await screen.findByText(/already exists/)).toBeInTheDocument();
    expect(screen.queryByText("список регионов")).not.toBeInTheDocument();
  });

  it("does not report success when the operation status cannot be read", async () => {
    // The mutation was accepted, but the poll fails. Nothing is known about the
    // outcome — the old wiring simply spun forever.
    globalThis.fetch = (_input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method === "POST") {
        return Promise.resolve({
          ok: true,
          status: 200,
          statusText: "OK",
          text: () => Promise.resolve(JSON.stringify({ id: "opr-1", done: false })),
        } as Response);
      }
      return Promise.resolve({
        ok: false,
        status: 403,
        statusText: "Forbidden",
        text: () => Promise.resolve(JSON.stringify({ code: "PERMISSION_DENIED", message: "no path" })),
      } as Response);
    };

    renderCreate();
    await submit();

    // Опрос ретраится с backoff, прежде чем ошибка доедет наверх, — ждём дольше
    // умолчания RTL (1 с).
    expect(
      await screen.findByText(/не удалось прочитать статус операции/, undefined, { timeout: 15000 }),
    ).toBeInTheDocument();
    expect(screen.queryByText("список регионов")).not.toBeInTheDocument();
  });

  it("surfaces the warning that the region was created closed to placement", async () => {
    stubFetch(
      { id: "opr-1", done: true },
      {
        id: "opr-1",
        done: true,
        metadata: {
          "@type": "…CreateRegionMetadata",
          region_id: "ru-central1",
          warnings: ["region ru-central1 is created CLOSED to placement (status DOWN)"],
        },
      },
    );
    renderCreate();
    await submit();

    expect(await screen.findByText(/CLOSED to placement/)).toBeInTheDocument();
  });

  it("refuses to call a response without an operation a success", async () => {
    // The internal listener omits default values, so an Operation with
    // done=false has no `done` key. Guessing "synchronous success" there would
    // report a mutation complete that has not even started.
    stubFetch({ id: "ru-central1", name: "Central" }, {});
    renderCreate();
    await submit();

    expect(await screen.findByText(/сервер не вернул операцию/)).toBeInTheDocument();
    expect(screen.queryByText("список регионов")).not.toBeInTheDocument();
  });
});
