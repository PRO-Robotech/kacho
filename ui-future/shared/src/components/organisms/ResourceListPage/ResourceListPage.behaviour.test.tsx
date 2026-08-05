// Two decisions the list page used to make by guessing, and now takes as data.
//
//   * the actions column. It was appended unconditionally, so a read-only
//     catalog resource got a column holding a menu button that opens nothing.
//     Three of the four standalone remotes had already grown the guard locally;
//     the fourth (nlb) had not, which is exactly the drift a single source is
//     supposed to make impossible.
//
//   * panel-vs-modal create. It was derived from the service prefix of the spec
//     id, which meant the shared copy encoded the vpc/iam route table and each
//     remote's copy encoded its own — the same spec resolving differently
//     depending on which copy rendered it. Whether `/create` is a route at all
//     is a fact about the app's route table, so the app states it.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;

/** Answers every list GET with a single row under the spec's payload key. */
function stubList(payloadKey: string, rows: Record<string, unknown>[]) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: rows })),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderList(spec: (typeof REGISTRY)[string], panelForms: boolean, at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms={panelForms} />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ResourceListPage — actions column", () => {
  it("is omitted for a resource with no row action", async () => {
    const spec = REGISTRY["disk-types"];
    // disk-types has no name column — its first column is the id.
    stubList(spec.payloadKey, [{ id: "dt-1", description: "ssd" }]);
    renderList(spec, true, "/system/disk-types");
    await screen.findByText("dt-1");
    expect(screen.queryByLabelText("Действия")).toBeNull();
  });

  it("is present for a resource that can be updated or deleted", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, true, "/projects/p1/vpc/networks");
    // The name shows up in the breadcrumb as well as the cell — the row is what
    // matters here, so wait for any of them and then assert on the action.
    await screen.findAllByText("netto");
    expect(await screen.findByLabelText("Действия")).toBeTruthy();
  });
});

describe("ResourceListPage — panelForms is the caller's decision", () => {
  it("sends Create to the page route when the app registered one", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, true, "/projects/p1/vpc/networks");
    await waitFor(() =>
      expect(screen.getByRole("link", { name: /Создать/ }).getAttribute("href")).toBe(
        "/projects/p1/vpc/networks/create",
      ),
    );
  });

  it("sends Create to the modal flag when the app registered no create route", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, false, "/projects/p1/vpc/networks");
    await waitFor(() =>
      expect(screen.getByRole("link", { name: /Создать/ }).getAttribute("href")).toBe(
        "/projects/p1/vpc/networks?modal=networks-create",
      ),
    );
  });
});
