// Список ресурсов с источником извне (kacho#2925, полоса G4; замысел NTF-6 З13,
// CX6-31 и условия к коду пересверки `0fff30f0`, M32).
//
// ЗАЧЕМ ЭТО СВОЙСТВО. `ResourceListPage` сам читает список, опрашивает его раз в
// три секунды, открывает поток изменений и сам дочитывает страницы. Экран,
// который читает ленту сам (центр уведомлений: строки и пометки из ОДНОГО
// снимка), на таком компоненте получил бы два источника одного экрана — строки
// из опроса компонента и пометки из своего снимка. Поэтому `source` заменяет
// чтение целиком: ни запроса, ни опроса, ни потока.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое, а не разметка:
//   · запросов к `spec.apiPath` 0 и открытий потока 0 за 10 с управляемых часов;
//     поток считается подменой ЕГО ТРАНСПОРТА (приёмник событий браузера), а не
//     вниманием к коду: «ни потока» держит счёт, а не чтение хука;
//   · строки и состояние экрана — из `source`; «Показать ещё» зовёт
//     `source.loadMore` ровно раз и недоступна при `loadMoreDisabled`;
//   · серверное сужение спеки вместе с `source` — отказ отрисовки, а не
//     нарисованная и проигнорированная ручка;
//   · близнецы: без `source` опрос прежний (запросов ≥ 2 за 3 с) и поток
//     открывается; спека без серверного сужения рисует строки `source`.
//
// Близнецы стоят здесь же намеренно: без них «запросов 0» и «открытий 0»
// зеленели бы на подмене, которая не считает ничего.

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, type ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import type { ResourceSpec } from "@shared/lib/resource-spec";
import { ResourceListPage, type ResourceListSource } from "./ResourceListPage";

const NARROWING_REFUSAL =
  "ResourceListPage: source excludes server-side narrowing (listFilters, search.serverTerm, serverSearchField)";

const realFetch = globalThis.fetch;
const realEventSource = (globalThis as { EventSource?: unknown }).EventSource;

/** Пути запросов, ушедших через `fetch`, — в порядке ухода. */
let requested: string[] = [];
/** Адреса, по которым строился приёмник событий, — то есть открытия потока. */
let streamOpens: string[] = [];

/**
 * Приёмник событий, подставленный вместо браузерного: клиент потока строит его
 * сам (`new EventSource` в `hub.ts`), поэтому каждое открытие потока проходит
 * через этот конструктор и попадает в счёт. Кадра открытия он не шлёт — значит
 * покрытие не наступает, и у близнеца без `source` опрос остаётся включённым.
 */
class CountingEventSource {
  readyState = 0;
  onerror: ((ev: Event) => void) | null = null;
  constructor(readonly url: string) {
    streamOpens.push(url);
  }
  addEventListener(): void {}
  close(): void {
    this.readyState = 2;
  }
}

beforeEach(() => {
  jest.useFakeTimers();
  requested = [];
  streamOpens = [];
  (globalThis as { EventSource?: unknown }).EventSource = CountingEventSource;
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    requested.push(url);
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: new Headers({ "content-type": "application/json" }),
      text: () => Promise.resolve(JSON.stringify({ [plain.payloadKey]: [{ id: "nic-edge-1", name: "из-края" }] })),
    } as Response);
  };
});

afterEach(() => {
  jest.useRealTimers();
  globalThis.fetch = realFetch;
  (globalThis as { EventSource?: unknown }).EventSource = realEventSource;
});

/** Запросы, ушедшие к пути списка спеки (а не к справочникам фильтров). */
function listRequests(spec: ResourceSpec): string[] {
  return requested.filter((u) => new URL(u, "http://console.test").pathname === spec.apiPath);
}

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
}

class Catch extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  render() {
    return this.state.error ? <p data-testid="refused">{this.state.error.message}</p> : this.props.children;
  }
}

function renderList(spec: ResourceSpec, source?: ResourceListSource) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/network-interfaces"]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <Catch>
            <ResourceListPage
              spec={spec}
              parentField="project_id"
              parentValue="prj-1"
              panelForms
              source={source}
            />
          </Catch>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Источник и счётчик его дочитываний — отдельно: утверждают о счётчике. */
function makeSource(over: Partial<ResourceListSource> = {}): { source: ResourceListSource; loadMore: jest.Mock<() => void> } {
  const loadMore = jest.fn<() => void>();
  return {
    source: {
      rows: [{ id: "nic-src-1", name: "из-источника" }],
      isLoading: false,
      error: null,
      hasMore: true,
      loadMore,
      loadMoreDisabled: false,
      isFetchingMore: false,
      ...over,
    },
    loadMore,
  };
}

// Спека с видом из словаря потока и без серверного сужения: у неё поток БЫ
// открылся, будь чтение своим, и отказа отрисовки у неё нет — значит «нет
// потока» и «нет запросов» могут быть обязаны только `source`.
const plain = REGISTRY["network-interfaces"];

describe("ResourceListPage с source: чтения, опроса и потока нет", () => {
  it("запросов к spec.apiPath 0 и открытий потока 0 за 10 с; строки и «Показать ещё» — из source", async () => {
    // verifies #2925
    const { source, loadMore } = makeSource();
    renderList(plain, source);
    await advance(10_000);

    process.stdout.write(`[G4] source: запросов к ${plain.apiPath} ${listRequests(plain).length}, открытий потока ${streamOpens.length} за 10 с\n`);
    expect(listRequests(plain)).toEqual([]);
    expect(streamOpens).toEqual([]);

    expect(screen.getAllByText("из-источника").length).toBeGreaterThan(0);
    expect(screen.queryByText("из-края")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Показать ещё" }));
    expect(loadMore).toHaveBeenCalledTimes(1);
  });

  it("«Показать ещё» недоступна при loadMoreDisabled — нажатие source.loadMore не зовёт", async () => {
    // verifies #2925
    const { source, loadMore } = makeSource({ loadMoreDisabled: true });
    renderList(plain, source);
    await advance(0);
    const more = screen.getByRole("button", { name: "Показать ещё" });
    expect((more as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(more);
    expect(loadMore).toHaveBeenCalledTimes(0);
  });

  it("отказ источника — экран отказа, а не строки и не приглашение создать", async () => {
    // verifies #2925
    renderList(plain, makeSource({ rows: [], hasMore: false, error: new Error("источник отказал") }).source);
    await advance(0);
    expect(screen.queryByText("из-источника")).toBeNull();
    expect(screen.getAllByText(/источник отказал/).length).toBeGreaterThan(0);
  });

  it("близнец без source: опрос прежний — запросов ≥ 2 за 3 с, поток открывается", async () => {
    // verifies #2925
    renderList(plain);
    await advance(3_000);
    process.stdout.write(`[G4] без source: запросов к ${plain.apiPath} ${listRequests(plain).length} за 3 с, открытий потока ${streamOpens.length}\n`);
    expect(listRequests(plain).length).toBeGreaterThanOrEqual(2);
    expect(streamOpens.length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("из-края").length).toBeGreaterThan(0);
  });
});

describe("ResourceListPage с source: серверное сужение спеки не принимается молча", () => {
  const narrowed: [string, ResourceSpec][] = [
    ["listFilters", { ...plain, listFilters: [{ kind: "toggle", param: "onlyMine", label: "Только мои" }] }],
    ["search.serverTerm", { ...plain, search: { placeholder: "Поиск", serverTerm: "search" } }],
    ["serverSearchField", { ...plain, serverSearchField: "name" }],
  ];

  it.each(narrowed)("source вместе с %s — отказ отрисовки с текстом замысла", async (_axis, spec) => {
    // verifies #2925
    const spy = jest.spyOn(console, "error").mockImplementation(() => undefined);
    try {
      renderList(spec, makeSource().source);
      await advance(0);
      expect(screen.getByTestId("refused").textContent).toBe(NARROWING_REFUSAL);
      expect(screen.queryByText("из-источника")).toBeNull();
    } finally {
      spy.mockRestore();
    }
  });

  it("близнец: пустой listFilters и спека без серверного сужения рисуют строки source", async () => {
    // verifies #2925
    renderList({ ...plain, listFilters: [] }, makeSource().source);
    await advance(0);
    expect(screen.queryByTestId("refused")).toBeNull();
    expect(screen.getAllByText("из-источника").length).toBeGreaterThan(0);
  });
});
