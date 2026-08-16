// Поиск в списке: КТО сужает и ПО ЧЕМУ.
//
// Предмет (#420). Поле поиска сужало загруженную страницу по `name` и `id`, а у
// пользователя `name` нет вовсе — его знают по почте. Поэтому ввод почты не
// находил ничего, а подпись поля («по имени или идентификатору») честно
// описывала то, чем искать нельзя.
//
// Хуже второе: даже найдись поле, сужение НА КЛИЕНТЕ судит только о том, что
// успело приехать на страницу. Список курсорный, и обо всём, что за курсором,
// такой поиск молча говорит «нет такого».
//
// Утверждается:
//   1. ресурс, объявивший серверный поиск, спрашивает СЕРВЕР — проверяется сам
//      запрос (параметр и его значение), а не текст на экране;
//   2. и при этом НЕ режет ответ сервера повторно у себя: строку, которую сервер
//      признал подходящей, страница обязана показать, даже если ни `name`, ни
//      `id` в неё не попали;
//   3. ресурс, ничего не объявивший, сужает как раньше — на клиенте, по имени и
//      идентификатору (положительный контроль: правка не отобрала поиск у всех
//      остальных);
//   4. подпись поля называет то, по чему оно ищет;
//   5. кавычка во вводе не уезжает в выражение фильтра. Грамматика выражения —
//      `field="value"`; сломанное выражение сервер разбирать перестаёт, и список
//      возвращается ПОЛНЫМ на запрос, который не нашёл никого.
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;

interface Row {
  id: string;
  [k: string]: unknown;
}

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/**
 * Стенд списка. `rowsFor` получает выражение `filter` — то есть стенд ведёт себя
 * как сервер: сужает он, а не проба. Иначе утверждение «клиент не режет ответ
 * повторно» было бы непроверяемым.
 */
function stubList(apiPath: string, payloadKey: string, rowsFor: (filter: string) => Row[]) {
  const calls: URL[] = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    const u = new URL(requestUrl(input), "http://console.test");
    calls.push(u);
    if (u.pathname === apiPath) {
      return jsonOk({ [payloadKey]: rowsFor(u.searchParams.get("filter") ?? ""), nextPageToken: "" });
    }
    return jsonOk({ nextPageToken: "" });
  };
  return calls;
}

function showList(specId: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/iam/users"]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="*" element={<ResourceListPage spec={REGISTRY[specId]} panelForms />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Последнее выражение `filter`, ушедшее на путь ресурса. */
function lastFilter(calls: URL[], apiPath: string): string | null {
  const hit = [...calls].reverse().find((u) => u.pathname === apiPath);
  return hit ? hit.searchParams.get("filter") : null;
}

const USERS_PATH = "/iam/v1/users";

const ADMIN: Row = { id: "usr-admin", email: "admin@prorobotech.ru", display_name: "Админ" };
const BILLING: Row = { id: "usr-bill", email: "billing@example.com", display_name: "Счета" };

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

describe("поиск в списке пользователей уходит на сервер", () => {
  it("часть почты уезжает выражением фильтра, а не режет страницу у клиента", async () => {
    const user = userEvent.setup();
    // Сервер сужает: на запрос с `admin` отдаёт только эту строку.
    const calls = stubList(USERS_PATH, "users", (filter) =>
      filter.includes("admin") ? [ADMIN] : [ADMIN, BILLING],
    );

    showList("users");
    await waitFor(() => expect(screen.getByText("billing@example.com")).toBeInTheDocument());

    const search = screen.getByPlaceholderText(/Поиск по почте/);
    await user.type(search, "admin");

    await waitFor(() => expect(lastFilter(calls, USERS_PATH)).toBe('search="admin"'));
    // Ответ сервера показан как есть: у строки нет ни `name`, ни совпадения по
    // `id`, и повторное сужение у клиента вычеркнуло бы её.
    await waitFor(() => expect(screen.queryByText("billing@example.com")).toBeNull());
    expect(screen.getByText("admin@prorobotech.ru")).toBeInTheDocument();
  });

  it("кавычка во вводе не уезжает в выражение", async () => {
    const user = userEvent.setup();
    const calls = stubList(USERS_PATH, "users", () => [ADMIN]);

    showList("users");
    await waitFor(() => expect(screen.getByText("admin@prorobotech.ru")).toBeInTheDocument());

    await user.type(screen.getByPlaceholderText(/Поиск по почте/), 'a"b');

    await waitFor(() => expect(lastFilter(calls, USERS_PATH)).toBe('search="ab"'));
  });

  it("пустой ввод не шлёт выражения вовсе", async () => {
    const user = userEvent.setup();
    const calls = stubList(USERS_PATH, "users", () => [ADMIN, BILLING]);

    showList("users");
    await waitFor(() => expect(screen.getByText("billing@example.com")).toBeInTheDocument());

    const search = screen.getByPlaceholderText(/Поиск по почте/);
    await user.type(search, "ad");
    await waitFor(() => expect(lastFilter(calls, USERS_PATH)).toBe('search="ad"'));

    await user.clear(search);
    await waitFor(() => expect(lastFilter(calls, USERS_PATH)).toBeNull());
  });
});

describe("ресурс без серверного поиска сужает как раньше", () => {
  it("сужение остаётся клиентским, по имени и идентификатору", async () => {
    // Положительный контроль правки: она добавляет поведение объявившим его
    // ресурсам и не отбирает поиск у остальных.
    const user = userEvent.setup();
    const calls = stubList("/iam/v1/roles", "roles", () => [
      { id: "rol-1", name: "vpc.network.admin", is_system: true },
      { id: "rol-2", name: "storage.volume.editor", is_system: true },
    ]);

    showList("roles");
    await waitFor(() => expect(screen.getByText("storage.volume.editor")).toBeInTheDocument());

    await user.type(screen.getByPlaceholderText(/имени или идентификатору/), "network");

    await waitFor(() => expect(screen.queryByText("storage.volume.editor")).toBeNull());
    expect(screen.getByText("vpc.network.admin")).toBeInTheDocument();
    // И ничего не спрашивает у сервера: у этого ресурса серверного поиска нет.
    expect(lastFilter(calls, "/iam/v1/roles")).toBeNull();
  });
});
