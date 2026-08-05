// Порядок хуков ResourceListPage при появлении scope-значения.
//
// Предмет. Страница списка принимает `parentField` (ресурс проектный или
// аккаунтный) и значение scope — из URL-параметра либо пропом от вызывающего.
// Пока значение не выбрано, страница показывает заглушку «Выберите проект» и
// выходит РАНЬШЕ, чем вызывает остальные свои хуки. Значение приходит не только
// при первом рендере: `IamScopedListShell` берёт аккаунт из context-store, а
// `InstancesPage`/`NlbPage`/`RegistryPage` — из параметра маршрута, и оба
// источника меняются БЕЗ размонтирования компонента.
//
// React связывает хуки с их порядковым номером в рендере. Компонент, у которого
// между двумя рендерами меняется число вызванных хуков, — не стилистическая
// придирка линтера, а падение: React отвергает такой рендер целиком, и
// пользователь вместо списка получает пустой экран ровно в тот момент, когда
// выбрал проект.
//
// Проба перерисовывает страницу с null → значение, то есть воспроизводит смену
// scope на живом дереве, а не разбирает исходник.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("ResourceListPage — порядок хуков при появлении scope", () => {
  it("переживает переход «проект не выбран» → «проект выбран» без потери рендера", async () => {
    const spec = REGISTRY.networks;
    globalThis.fetch = () =>
      Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        text: () => Promise.resolve(JSON.stringify({ [spec.payloadKey]: [{ id: "net-1", name: "netto" }] })),
      } as Response);

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const tree = (parentValue: string | null) => (
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/vpc/networks"]}>
          <PageHeaderSlotProvider>
            <HeaderRightSlot />
            <ResourceListPage spec={spec} parentField="project_id" parentValue={parentValue} panelForms />
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>
    );

    const { rerender } = render(tree(null));
    await screen.findByText("Выберите проект");

    // Тот же смонтированный компонент получает выбранный проект.
    rerender(tree("prj-1"));

    await waitFor(() => expect(screen.queryByText("Выберите проект")).toBeNull());
    expect(await screen.findAllByText("netto")).not.toHaveLength(0);
  });
});
