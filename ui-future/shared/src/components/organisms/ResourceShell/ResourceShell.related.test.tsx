// Вкладка дочернего ресурса на карточке родителя: КТО сужает список по родителю
// и что происходит, когда сузить некому.
//
// Предмет. Вкладка читает список ребёнка той же курсорной страницей, что и
// страница списка. Пока сужение по родителю делал ТОЛЬКО клиент
// (`all.filter(...)` по `filterField`), вкладка показывала пересечение «дети
// этого родителя» × «первая страница списка ПРОЕКТА» — и выдавала его за весь
// список: сеть, чьи подсети не попали в первую страницу, показывала неполную
// вкладку, и ничто об этом не сообщало. На своей странице ресурса продолжение
// курсора есть, то есть один и тот же вопрос имел два ответа.
//
// Что утверждается ниже:
//   1. ребро, объявившее серверное поле сужения, спрашивает СЕРВЕР — проверяется
//      сам запрос (адрес и параметры), а не наличие текста на экране;
//   2. ребро БЕЗ такого поля параметр не выдумывает (парный контроль к п.1) и
//      показывает продолжение курсора — той же формой, что страница списка
//      («Показать ещё»);
//   3. продолжение показано ровно тогда, когда за курсором есть ещё
//      (отрицание в паре с положительным);
//   4. продолжение — ПО ДЕЙСТВИЮ пользователя: до нажатия ни один запрос не
//      несёт `pageToken`. Автоматическая догрузка на эффекте — прямой путь к
//      бесконечному рендеру, который в этой консоли дважды убивал прогон по
//      памяти, а вердикта не оставлял ни одной пробе суиты;
//   5. страница, на которой детей этого родителя нет, но курсор остался, НЕ
//      объявляется пустой: «создайте первый» поверх недочитанного списка —
//      утверждение о ресурсе, которого никто не проверял.
//
// Запросы вкладки отличимы от запросов резолва имён (`RefNameLink` спрашивает
// тот же путь с `pageSize=500`) отсутствием размера страницы — вкладка его не
// задаёт. Поэтому строки фикстур намеренно не ссылаются на ресурс той же
// категории, что и предмет утверждения: тогда к пути предмета обращается только
// вкладка.

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

interface ListPage {
  rows: Record<string, unknown>[];
  /** Курсор следующей страницы; пусто — конец списка. */
  next?: string;
}

interface Stubbed {
  /** Детальное чтение: путь → ресурс. */
  detail: Record<string, Record<string, unknown>>;
  /** Список: путь → (ключ полезной нагрузки, страница по курсору). */
  list: Record<string, { payloadKey: string; page: (token: string) => ListPage }>;
}

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

/** Стенд отвечает 404 на путь, которого не объявляли: непокрытый запрос обязан
 *  быть виден отказом, а не молча превращаться в пустой список. */
function jsonMiss(path: string): Promise<Response> {
  return Promise.resolve({
    ok: false,
    status: 404,
    statusText: "Not Found",
    text: () => Promise.resolve(JSON.stringify({ code: 5, message: `no stub for ${path}` })),
  } as Response);
}

/**
 * Адрес запроса из любой формы, которую принимает настоящий `fetch`. Клиент
 * консоли зовёт его строкой, но заменитель не вправе быть уже настоящего: на
 * `URL`/`Request` приведение по умолчанию дало бы `[object Object]`, то есть один
 * и тот же путь для любого запроса — и все утверждения об адресе стали бы
 * истинными сразу.
 */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubApi(s: Stubbed): URL[] {
  const calls: URL[] = [];
  const stub: typeof globalThis.fetch = (input) => {
    const u = new URL(requestUrl(input), "http://console.test");
    calls.push(u);
    const list = s.list[u.pathname];
    if (list) {
      const p = list.page(u.searchParams.get("pageToken") ?? "");
      // Край отдаёт camelCase, клиент переводит его в proto-style — стенд обязан
      // отвечать в той же форме, иначе курсор читался бы не оттуда, где он есть.
      return jsonOk({ [list.payloadKey]: p.rows, nextPageToken: p.next ?? "" });
    }
    const detail = s.detail[u.pathname];
    if (detail) return jsonOk(detail);
    return jsonMiss(u.pathname);
  };
  globalThis.fetch = stub;
  return calls;
}

/** Запросы ВКЛАДКИ к пути ребёнка (без `pageSize` — см. шапку файла). */
function tabCalls(calls: URL[], path: string): URL[] {
  return calls.filter((u) => u.pathname === path && u.searchParams.get("pageSize") === null);
}

const CREATED = "2026-08-01T10:00:00Z";

const NETWORK = { id: "net-1", name: "основная", description: "", created_at: CREATED, labels: {} };
const SUBNET_PARENT = {
  id: "sub-1",
  name: "подсеть-родитель",
  description: "",
  created_at: CREATED,
  labels: {},
  network_id: "net-1",
  placement_type: "ZONAL",
  zone_id: "z-a",
  ipv4_cidr_primary: "10.0.0.0/24",
};

function subnetRow(n: number, networkId = "net-1"): Record<string, unknown> {
  return {
    id: `sub-${n}`,
    name: `подсеть-${n}`,
    description: "",
    created_at: CREATED,
    labels: {},
    network_id: networkId,
    placement_type: "ZONAL",
    zone_id: "z-a",
    ipv4_cidr_primary: `10.0.${n}.0/24`,
  };
}

function addressRow(n: number, subnetId = "sub-1"): Record<string, unknown> {
  return {
    id: `adr-${n}`,
    name: `адрес-${n}`,
    description: "",
    created_at: CREATED,
    labels: {},
    reserved: true,
    used: false,
    internal_ipv4_address: { subnet_id: subnetId, address: `10.0.0.${n}` },
  };
}

/** Карточка ресурса, открытая на вкладке `tab`. */
function showTab(specId: string, uid: string, tab: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/vpc/${REGISTRY[specId].route}/${uid}/${tab}`]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/projects/:projectId/vpc/:route/:uid/*" element={<ResourceShell spec={REGISTRY[specId]} />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

describe("ребро с серверным полем сужения — сужает СЕРВЕР", () => {
  it("вкладка подсетей сети спрашивает список, сужённый по родителю", async () => {
    const calls = stubApi({
      detail: { "/vpc/v1/networks/net-1": NETWORK },
      list: {
        "/vpc/v1/subnets": { payloadKey: "subnets", page: () => ({ rows: [subnetRow(1), subnetRow(2)] }) },
        "/geo/v1/zones": { payloadKey: "zones", page: () => ({ rows: [{ id: "z-a", name: "зона A" }] }) },
      },
    });
    showTab("networks", "net-1", "subnets");

    await waitFor(() => expect(tabCalls(calls, "/vpc/v1/subnets").length).toBeGreaterThan(0));
    // Утверждается ЗАПРОС: адрес и полный набор параметров. Присутствие строк на
    // экране про сужение не говорит ничего — клиентский фильтр даёт ту же картину.
    expect(Object.fromEntries(tabCalls(calls, "/vpc/v1/subnets")[0].searchParams)).toEqual({
      project_id: "prj-1",
      filter: 'network_id="net-1"',
    });
  });

  it("строки сужённого ответа показаны — положительный контроль", async () => {
    stubApi({
      detail: { "/vpc/v1/networks/net-1": NETWORK },
      list: {
        "/vpc/v1/subnets": { payloadKey: "subnets", page: () => ({ rows: [subnetRow(1), subnetRow(2)] }) },
        "/geo/v1/zones": { payloadKey: "zones", page: () => ({ rows: [{ id: "z-a", name: "зона A" }] }) },
      },
    });
    showTab("networks", "net-1", "subnets");

    expect(await screen.findByText("подсеть-1")).toBeInTheDocument();
    expect(screen.getByText("подсеть-2")).toBeInTheDocument();
  });

  it("вкладка таблиц маршрутов сети сужается тем же полем", async () => {
    const calls = stubApi({
      detail: { "/vpc/v1/networks/net-1": NETWORK },
      list: {
        "/vpc/v1/routeTables": {
          payloadKey: "route_tables",
          page: () => ({ rows: [{ id: "rtb-1", name: "основная", created_at: CREATED, network_id: "net-1" }] }),
        },
      },
    });
    showTab("networks", "net-1", "route-tables");

    await waitFor(() => expect(tabCalls(calls, "/vpc/v1/routeTables").length).toBeGreaterThan(0));
    expect(Object.fromEntries(tabCalls(calls, "/vpc/v1/routeTables")[0].searchParams)).toEqual({
      project_id: "prj-1",
      filter: 'network_id="net-1"',
    });
  });

  it("вкладка групп безопасности сети сужается тем же полем", async () => {
    const calls = stubApi({
      detail: { "/vpc/v1/networks/net-1": NETWORK },
      list: {
        "/vpc/v1/securityGroups": {
          payloadKey: "security_groups",
          page: () => ({ rows: [{ id: "sg-1", name: "по умолчанию", created_at: CREATED, network_id: "net-1" }] }),
        },
      },
    });
    showTab("networks", "net-1", "security-groups");

    await waitFor(() => expect(tabCalls(calls, "/vpc/v1/securityGroups").length).toBeGreaterThan(0));
    expect(Object.fromEntries(tabCalls(calls, "/vpc/v1/securityGroups")[0].searchParams)).toEqual({
      project_id: "prj-1",
      filter: 'network_id="net-1"',
    });
  });

  it("сужённый список тоже длиннее страницы — продолжение показано и здесь", async () => {
    // Сужение на сервере не отменяет курсор: у сети с сотнями подсетей страница
    // кончается так же. Один предмет — один вид, поэтому продолжение то же.
    stubApi({
      detail: { "/vpc/v1/networks/net-1": NETWORK },
      list: {
        "/vpc/v1/subnets": {
          payloadKey: "subnets",
          page: (t) => (t ? { rows: [subnetRow(2)] } : { rows: [subnetRow(1)], next: "cur-2" }),
        },
        "/geo/v1/zones": { payloadKey: "zones", page: () => ({ rows: [{ id: "z-a", name: "зона A" }] }) },
      },
    });
    showTab("networks", "net-1", "subnets");

    expect(await screen.findByText("Показать ещё")).toBeInTheDocument();
  });
});

describe("ребро без серверного поля — обрезание не молчаливое", () => {
  // Текст приглашения читается из спеки ребёнка, а не выписывается здесь: два
  // утверждения ниже (есть приглашение / нет приглашения) без него пусты.
  const EMPTY_TITLE = REGISTRY.addresses.emptyState?.title ?? "";

  it("предпосылка: у ребёнка есть текст приглашения создать", () => {
    expect(EMPTY_TITLE).not.toBe("");
  });

  const addressesTab = () => ({
    detail: { "/vpc/v1/subnets/sub-1": SUBNET_PARENT },
    list: {
      "/vpc/v1/subnets": { payloadKey: "subnets", page: () => ({ rows: [SUBNET_PARENT] }) },
      "/geo/v1/zones": { payloadKey: "zones", page: () => ({ rows: [{ id: "z-a", name: "зона A" }] }) },
    },
  });

  it("параметра, которого владелец не принимает, вкладка не выдумывает", async () => {
    // Адрес хранит подсеть внутри jsonb, и в белом списке фильтра владельца её
    // нет. Значит ребро серверного поля не объявляет — и запрос обязан уйти
    // ровно тем, чем уходил: сужением по проекту, без выдуманного параметра.
    const calls = stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": { payloadKey: "addresses", page: () => ({ rows: [addressRow(5)] }) },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    await waitFor(() => expect(tabCalls(calls, "/vpc/v1/addresses").length).toBeGreaterThan(0));
    expect(Object.fromEntries(tabCalls(calls, "/vpc/v1/addresses")[0].searchParams)).toEqual({
      project_id: "prj-1",
    });
  });

  it("за курсором есть ещё → продолжение показано", async () => {
    stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": {
          payloadKey: "addresses",
          page: (t) => (t ? { rows: [addressRow(6)] } : { rows: [addressRow(5)], next: "cur-2" }),
        },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    expect(await screen.findByText("адрес-5")).toBeInTheDocument();
    expect(screen.getByText("Показать ещё")).toBeInTheDocument();
  });

  it("страница одна → продолжения нет (парное отрицание)", async () => {
    stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": { payloadKey: "addresses", page: () => ({ rows: [addressRow(5)] }) },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    expect(await screen.findByText("адрес-5")).toBeInTheDocument();
    expect(screen.queryByText("Показать ещё")).not.toBeInTheDocument();
  });

  it("продолжение — по нажатию, и до нажатия НИ ОДИН запрос не несёт курсора", async () => {
    const calls = stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": {
          payloadKey: "addresses",
          page: (t) => (t ? { rows: [addressRow(6)] } : { rows: [addressRow(5)], next: "cur-2" }),
        },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    const more = await screen.findByText("Показать ещё");
    // Автоматическая догрузка на эффекте выдала бы себя здесь: вторая страница
    // приехала бы без единого нажатия.
    expect(calls.filter((u) => u.searchParams.get("pageToken"))).toHaveLength(0);
    expect(screen.queryByText("адрес-6")).not.toBeInTheDocument();

    fireEvent.click(more);

    expect(await screen.findByText("адрес-6")).toBeInTheDocument();
    expect(calls.filter((u) => u.searchParams.get("pageToken") === "cur-2").length).toBeGreaterThan(0);
    // Первая страница остаётся на экране: продолжение ДОБАВЛЯЕТ, а не заменяет.
    expect(screen.getByText("адрес-5")).toBeInTheDocument();
  });

  it("детей на прочитанных страницах нет, но курсор остался → не «создайте первый»", async () => {
    stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": {
          payloadKey: "addresses",
          // Первая страница списка проекта — чужие адреса; свои, возможно, дальше.
          page: (t) => (t ? { rows: [addressRow(6)] } : { rows: [addressRow(9, "sub-other")], next: "cur-2" }),
        },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    // Приглашение «создайте первый» здесь означало бы утверждение об отсутствии
    // ресурсов, которого никто не проверял: список недочитан.
    expect(await screen.findByText("Показать ещё")).toBeInTheDocument();
    expect(screen.queryByText(EMPTY_TITLE)).not.toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  it("детей нет и курсор кончился → приглашение создать (парный положительный)", async () => {
    stubApi({
      ...addressesTab(),
      list: {
        ...addressesTab().list,
        "/vpc/v1/addresses": { payloadKey: "addresses", page: () => ({ rows: [addressRow(9, "sub-other")] }) },
      },
    });
    showTab("subnets", "sub-1", "addresses");

    expect(await screen.findByText(EMPTY_TITLE)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText("Показать ещё")).not.toBeInTheDocument();
  });
});
