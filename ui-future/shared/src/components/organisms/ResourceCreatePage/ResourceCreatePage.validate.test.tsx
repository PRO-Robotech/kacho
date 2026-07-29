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
  globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    methods.push(init?.method ?? "GET");
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      text: async () => JSON.stringify({ id: "opr-1", done: true }),
    } as Response;
  }) as typeof globalThis.fetch;
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
  await userEvent.click(await screen.findByRole("button", { name: /Создать регион/i }));
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceCreatePage — spec.validate", () => {
  it("does not send the request when the spec rejects the form state", async () => {
    stubFetch();
    renderCreate({ ...REGISTRY.regions, validate: () => "зона обязана принадлежать региону" });
    await submit();

    expect(await screen.findByText("зона обязана принадлежать региону")).toBeInTheDocument();
    await waitFor(() => expect(methods.includes("POST")).toBe(false));
    expect(screen.queryByText("список регионов")).not.toBeInTheDocument();
  });

  it("sends the request when the spec accepts the form state", async () => {
    stubFetch();
    renderCreate({ ...REGISTRY.regions, validate: () => null });
    await submit();

    await waitFor(() => expect(methods.includes("POST")).toBe(true));
  });
});
