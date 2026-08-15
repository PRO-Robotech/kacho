// Заход ПРЯМЫМ адресом вкладки показывает действия, которые ставит сама вкладка.
//
// Предмет (#425). Действия панели правил группы безопасности живут не рядом с
// таблицей, а в правом слоте шапки страницы. Слот один, писателей двое: оболочка
// карточки и содержимое открытой вкладки. При переключении вкладки ВНУТРИ
// карточки оболочка уже смонтирована, её эффект не перезапускается, и запись
// вкладки доживает до экрана. При заходе прямым адресом оболочка монтируется
// заново, её эффект выполняется ПОСЛЕ эффекта вкладки (так работает React) — и
// действия вкладки исчезали.
//
// Со стороны это выглядело как «таблица правил есть, а завести правило нечем», и
// воспроизводилось только по ссылке из документации, из чужого сообщения или из
// закладки — то есть у того, кто не переключал вкладки руками.
//
// Проба берёт СИНТЕТИЧЕСКОЕ расширение, а не панель правил: предмет здесь —
// composition (оболочка + вкладка + слот), а не устройство конкретной панели.
// Панель правил проверяется своими пробами и сквозной пробой браузера.
import { useMemo } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { registerDetailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

const NETWORK = {
  id: "net-1",
  project_id: "prj-1",
  name: "сеть-1",
  description: "",
  created_at: "2026-01-01T00:00:00Z",
  labels: {},
  ipv4_cidr_blocks: ["10.0.0.0/16"],
};

/** Ответ стенда. `Response` в jsdom нет — отдаём ровно то, что читает клиент. */
function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

/** Адрес запроса из любой формы, которую принимает настоящий `fetch`. */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/** Отвечает на чтение карточки; всё прочее — пустой список. */
function stubApi() {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = new URL(requestUrl(input), "http://console.test");
    if (url.pathname === "/vpc/v1/networks/net-1") return jsonOk(NETWORK);
    return jsonOk({ networks: [], subnets: [], zones: [], operations: [], nextPageToken: "" });
  }) as typeof fetch;
}

/** Вкладка, которая ставит своё действие в шапку — как это делает панель правил. */
function TabWithActions() {
  const actions = useMemo(
    () => (
      <button type="button" onClick={() => undefined}>
        Добавить правило
      </button>
    ),
    [],
  );
  useHeaderRight(actions);
  return <div>таблица правил</div>;
}

/**
 * Карточка сети, открытая ПРЯМЫМ адресом вкладки — без переключения на неё.
 *
 * Ресурс кладётся в кэш ДО отрисовки, и это не ускорение пробы, а условие
 * дефекта: пока карточка ждёт ответ, вкладки на экране нет вовсе — оболочка
 * успевает записать в слот пустоту РАНЬШЕ, чем вкладка смонтируется, и запись
 * вкладки ложится последней. Дефект живёт там, где ответ уже есть: оболочка и
 * вкладка монтируются одним тактом, и тогда решает порядок эффектов — потомок,
 * затем родитель. Ровно так ведёт себя стенд, когда ресурс уже прочитан
 * (переход из списка) или отвечает быстрее первой отрисовки.
 */
function openTabByUrl(tab: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData([REGISTRY.networks.id, "shell-detail", "net-1"], NETWORK);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/vpc/networks/net-1/${tab}`]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <Routes>
            <Route
              path="/projects/:projectId/vpc/:route/:uid/*"
              element={<ResourceShell spec={REGISTRY.networks} />}
            />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeAll(() => {
  registerDetailExtension("networks", {
    extraTabs: () => [{ id: "probe-rules", label: "Правила", render: () => <TabWithActions /> }],
  });
});

beforeEach(stubApi);

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

describe("карточка ресурса, открытая прямым адресом вкладки", () => {
  it("показывает действия, поставленные содержимым вкладки", async () => {
    openTabByUrl("probe-rules");

    // Положительный контроль: вкладка действительно отрисована. Утверждение о
    // кнопке на неотрисованной вкладке ничего бы не значило — «кнопок нет» и
    // «страница не открылась» выглядят одинаково.
    await waitFor(() => expect(screen.getByText("таблица правил")).toBeInTheDocument());

    expect(
      await screen.findByRole("button", { name: "Добавить правило" }),
      // Именно здесь оболочка карточки затирала запись вкладки: для вкладки,
      // у которой нет собственного действия оболочки, она ставила в слот пустоту.
    ).toBeInTheDocument();
  });

  it("на вкладке обзора действий вкладки нет (отрицательный контроль)", async () => {
    openTabByUrl("");

    // Имя ресурса стоит и в заголовке карточки, и строкой обзора — считаем все.
    await waitFor(() => expect(screen.getAllByText("сеть-1").length).toBeGreaterThan(0));
    expect(screen.queryByRole("button", { name: "Добавить правило" })).toBeNull();
  });
});
