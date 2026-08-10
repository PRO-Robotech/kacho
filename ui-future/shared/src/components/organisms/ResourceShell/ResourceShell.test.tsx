// Единая оболочка карточки ресурса. Предмет — что она ПОКАЗЫВАЕТ и, главное,
// чего НЕ обещает:
//
//  1. вкладка «Операции» появляется только у ресурса, чей подмаршрут ствол
//     действительно несёт. У каталожных и вложенных ресурсов его нет вовсе:
//     видимая вкладка спрашивала бы несуществующий адрес и показывала отказ
//     края там, где верный ответ — «операций у этого ресурса не бывает»;
//  2. обзор несёт пять обязательных строк, и пустое описание подписано
//     прочерком, а не пустотой — иначе «сервер не прислал» неотличимо от
//     «строка не отрисовалась»;
//  3. отказ чтения показан отказом, а не пустой карточкой.
//
// Отсутствие вкладки проверяется В ПАРЕ с её наличием на ресурсе, у которого
// подмаршрут есть: одиночное отрицание зеленело бы и на оболочке, вообще не
// рисующей вкладок.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { hasOperationsSubroute } from "@shared/lib/operations-subroute";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

function stubFetch(read: Record<string, unknown> | "error") {
  globalThis.fetch = () =>
    read === "error"
      ? Promise.resolve({
          ok: false,
          status: 404,
          statusText: "Not Found",
          text: () => Promise.resolve(JSON.stringify({ code: 5, message: "Network net-1 not found" })),
        } as Response)
      : Promise.resolve({
          ok: true,
          status: 200,
          statusText: "OK",
          text: () => Promise.resolve(JSON.stringify(read)),
        } as Response);
}

function showDetail(specId: string, uid: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/vpc/${REGISTRY[specId].route}/${uid}`]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route
              path="/projects/:projectId/vpc/:route/:uid"
              element={<ResourceShell spec={REGISTRY[specId]} />}
            />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceShell — обзор", () => {
  it("показывает имя ресурса и пять обязательных строк", async () => {
    stubFetch({ id: "net-1", name: "основная", description: "", created_at: "2026-08-01T10:00:00Z", labels: {} });
    showDetail("networks", "net-1");

    // Имя показано и в рейле (зона идентичности), и строкой обзора.
    expect((await screen.findAllByText("основная")).length).toBeGreaterThanOrEqual(2);
    for (const row of ["Идентификатор", "Имя", "Описание", "Дата создания", "Метки"]) {
      expect(screen.getByText(row)).toBeInTheDocument();
    }
  });

  it("пустое описание подписано прочерком, а не пустотой", async () => {
    stubFetch({ id: "net-1", name: "основная", description: "", created_at: "2026-08-01T10:00:00Z", labels: {} });
    showDetail("networks", "net-1");

    await screen.findAllByText("основная");
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("отказ чтения показан отказом, а не пустой карточкой", async () => {
    stubFetch("error");
    showDetail("networks", "net-1");

    await waitFor(() => expect(screen.queryByText("Идентификатор")).not.toBeInTheDocument());
    expect(await screen.findByText(/not found/i)).toBeInTheDocument();
  });
});

describe("ResourceShell — вкладка операций обещается ровно там, где подмаршрут есть", () => {
  it("у ресурса С подмаршрутом вкладка показана", async () => {
    // Предпосылка утверждения читается из общего перечня, а не выписана здесь.
    expect(hasOperationsSubroute(REGISTRY.networks.apiPath)).toBe(true);

    stubFetch({ id: "net-1", name: "основная", description: "", created_at: "2026-08-01T10:00:00Z", labels: {} });
    showDetail("networks", "net-1");

    await screen.findAllByText("основная");
    expect(screen.getByText("Операции")).toBeInTheDocument();
  });

  it("у ресурса БЕЗ подмаршрута вкладки нет", async () => {
    // Каталог размещения: подмаршрута операций ствол ему не объявляет.
    expect(hasOperationsSubroute(REGISTRY.zones.apiPath)).toBe(false);

    stubFetch({ id: "ru-central1-a", name: "зона A", description: "", created_at: "2026-08-01T10:00:00Z" });
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={["/system/zones/ru-central1-a"]}>
          <PageHeaderSlotProvider>
            <Routes>
              <Route path="/system/zones/:uid" element={<ResourceShell spec={REGISTRY.zones} />} />
            </Routes>
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findAllByText("зона A");
    // Видимая вкладка спрашивала бы адрес, которого нет, и показывала отказ
    // края вместо «операций у этого ресурса не бывает».
    expect(screen.queryByText("Операции")).not.toBeInTheDocument();
  });
});
