// Строка поиска списка спрашивает СЕРВЕР, а не режет загруженную страницу (#373).
//
// ПРЕДМЕТ. Поиск фильтровал массив `items` — то есть строки, которые успели
// приехать курсором. На списке длиннее страницы он отвечал «ничего не найдено»
// про ресурс, который существует, и отвечал тем увереннее, чем больше у
// арендатора ресурсов. Это не косметика: пользователь получает утверждение об
// отсутствии предмета, которого край не делал.
//
// ЧТО ИМЕННО ПРОВЕРЯЕТСЯ. Не «фильтр применился» (это верно и для клиентского
// среза), а «ушёл ЗАПРОС с выражением фильтра» — и, зеркально, что строки,
// которые сервер вернул, страница больше не пересевает у себя.
//
// ПОЧЕМУ ЭТО ОПЦИЯ, А НЕ ПОВЕДЕНИЕ ВСЕХ. Владелец разбирает выражение фильтра по
// СВОЕМУ белому списку, и часть владельцев (iam) его не разбирает вовсе —
// незнакомое выражение там не отвергается, а молча игнорируется, и список
// вернулся бы ПОЛНЫМ под видом отфильтрованного. Это строго хуже клиентского
// среза. Поэтому право спросить сервер объявляется спекой (`serverSearchField`),
// а сходимость объявления с деревом владельца держит отдельная проба —
// `lib/list-server-search-parity.test.ts`.

import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { requestUrl } from "@shared/test/fetch-capture";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;
let urls: string[] = [];

function stubList(payloadKey: string, rows: Record<string, unknown>[], nextToken = "") {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    urls.push(requestUrl(input));
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: rows, next_page_token: nextToken })),
    } as Response);
  };
}

/** Адреса списка ЭТОГО ресурса — без запросов справочников (зоны, опции фильтров). */
function listUrls(apiPath: string): string[] {
  return urls.filter((u) => u.split("?")[0] === apiPath);
}

/**
 * Значение параметра `filter`, прочитанное разбором адреса, а не поиском подстроки.
 *
 * Пробел в строке запроса кодируется `+`, поэтому сравнение подстрокой после
 * `decodeURIComponent` не сошлось бы никогда — и проба была бы красной при
 * исправном коде. Читаем тем же разбором, каким его прочитает край.
 */
function filterParams(apiPath: string): (string | null)[] {
  return listUrls(apiPath).map((u) => new URLSearchParams(u.split("?")[1] ?? "").get("filter"));
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderList(spec: (typeof REGISTRY)[string], at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function typeSearch(value: string) {
  fireEvent.change(screen.getByPlaceholderText(/Поиск|Фильтр/), { target: { value } });
}

describe("поиск уходит на сервер там, где владелец его разбирает", () => {
  it("ввод имени порождает ЗАПРОС с выражением фильтра", async () => {
    const spec = REGISTRY.networks;
    expect(spec.serverSearchField).toBe("name");
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");
    const before = listUrls(spec.apiPath).length;

    typeSearch("web");

    await waitFor(() => {
      expect(filterParams(spec.apiPath).slice(before)).toContain('name CONTAINS "web"');
    });
  });

  it("строку, которую вернул сервер, страница больше не пересевает", async () => {
    // Ответ намеренно НЕ содержит подстроки запроса: сервер уже решил, что эта
    // строка подходит (он мог сузить по другому правилу нормализации). Клиентский
    // срез поверх ответа отбросил бы её — и вернул бы ровно тот дефект, ради
    // которого поиск и уехал на сервер, только этажом выше.
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "ПРОИЗВОДСТВО" }]);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("ПРОИЗВОДСТВО");

    typeSearch("web");

    // Подождать, пока запрос с фильтром действительно уйдёт, — иначе проба
    // утверждала бы про состояние ДО поиска и была бы зелёной всегда.
    await waitFor(() => {
      expect(filterParams(spec.apiPath).some((f) => f?.includes("CONTAINS"))).toBe(true);
    });
    expect(screen.getAllByText("ПРОИЗВОДСТВО").length).toBeGreaterThan(0);
  });

  it("кавычка в запросе не ломает выражение", async () => {
    // Грамматика владельца читает значение до СЛЕДУЮЩЕЙ кавычки и обратной косой
    // не понимает: кавычка, уехавшая в выражение как есть, закрыла бы строку
    // раньше времени, и весь список ответил бы отказом разбора.
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");

    typeSearch('we"b');

    await waitFor(() => {
      const sent = filterParams(spec.apiPath).filter((f): f is string => !!f);
      expect(sent.length).toBeGreaterThan(0);
      // Кавычек ровно две — открывающая и закрывающая; третья означала бы, что
      // значение закрылось раньше и остаток уехал мусором.
      for (const expr of sent) expect(expr).toBe('name CONTAINS "web"');
    });
  });

  it("перекрытие подписи ресурсом НЕ отменяет названия области", async () => {
    // Единственный ресурс с собственной подписью — пользователи: имени у них нет
    // вовсе, ищут по почте. Перекрытие называет ПРЕДМЕТ поиска; область обязана
    // остаться, иначе именно этот список — и только он — о ней молчит.
    //
    // Проба заведена слиянием с работой по переводу подписей (#478): там
    // подписи переехали в единственный источник, и стык двух правок виден
    // только здесь — обе стороны по отдельности зелены.
    const spec = REGISTRY.users;
    expect(spec.search?.placeholder).toBeDefined();
    expect(spec.search?.serverTerm).toBe("search");
    stubList(spec.payloadKey, [{ id: "usr-1", email: "kto@example.test" }], "");
    renderList(spec, "/iam/users");
    await waitFor(() => expect(listUrls(spec.apiPath).length).toBeGreaterThan(0));

    const searchField = await screen.findByPlaceholderText(/по всему списку/i);
    // И предмет поиска на месте — иначе «область названа» достигалось бы
    // затиранием того, ради чего перекрытие заводили.
    expect(searchField.getAttribute("placeholder")).toMatch(/почте/i);
  });

  it("пустая строка поиска фильтра не шлёт", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "netto" }]);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");
    typeSearch("web");
    await waitFor(() => expect(filterParams(spec.apiPath).some((f) => f?.includes("CONTAINS"))).toBe(true));
    typeSearch("");
    await waitFor(() => expect(filterParams(spec.apiPath).at(-1)).toBeNull());
  });
});

describe("законный близнец: владелец, который выражения не разбирает", () => {
  it("ресурс без объявления НЕ получает filter в запросе", async () => {
    // Аккаунты iam: белого списка выражения у владельца нет, незнакомое
    // выражение он игнорирует. Отправить его значило бы показать ПОЛНЫЙ список
    // под видом отфильтрованного.
    //
    // Близнец НАМЕРЕННО не пользователи, хотя раньше был ими: серверный поиск по
    // почте (`search.serverTerm`) сделал их полноценным серверным ресурсом, и
    // близнец, привязанный к ним, доказывал бы обратное своему предмету. Поэтому
    // ниже проверяются ОБА способа спросить сервер, а не один: близнец обязан
    // краснеть, если этот ресурс когда-нибудь научат любому из них.
    const spec = REGISTRY.accounts;
    expect(spec.serverSearchField).toBeUndefined();
    expect(spec.search?.serverTerm).toBeUndefined();
    stubList(spec.payloadKey, [{ id: "acc-1", name: "alpha" }]);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/iam/accounts"]}>
          <PageHeaderSlotProvider>
            <HeaderRightSlot />
            <ResourceListPage spec={spec} panelForms />
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(listUrls(spec.apiPath).length).toBeGreaterThan(0));

    typeSearch("alp");

    await new Promise((r) => setTimeout(r, 400));
    expect(filterParams(spec.apiPath).filter((f) => f !== null)).toEqual([]);
  });

  /**
   * Клиентский поиск имеет ДВЕ области, а не одну (#373).
   *
   * Прежде подпись была одна на оба состояния — «среди загруженных». На
   * дочитанном списке это правда, но правда пессимистичная: загруженное там и
   * есть весь набор, и подпись отговаривала верить верному ответу. Различие
   * стоит различать, поэтому ниже пара, а не один случай.
   */
  function renderAccountsList(spec: (typeof REGISTRY)[string]) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/iam/accounts"]}>
          <PageHeaderSlotProvider>
            <HeaderRightSlot />
            <ResourceListPage spec={spec} panelForms />
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  }

  it("за курсором есть ещё — строка поиска НАЗЫВАЕТ, что судит о загруженном", async () => {
    // Иначе одна и та же строка ввода означает на разных страницах разное, и
    // пользователь читает клиентский срез как ответ про список.
    const spec = REGISTRY.accounts;
    expect(spec.serverSearchField).toBeUndefined();
    expect(spec.search?.serverTerm).toBeUndefined();
    stubList(spec.payloadKey, [{ id: "acc-1", name: "alpha" }], "cursor-1");
    renderAccountsList(spec);
    await waitFor(() => expect(listUrls(spec.apiPath).length).toBeGreaterThan(0));
    expect(await screen.findByPlaceholderText(/среди загруженных/i)).toBeTruthy();
  });

  it("список дочитан — та же строка НАЗЫВАЕТ, что судит обо всём списке", async () => {
    // Положительный контроль к предыдущему: без него «среди загруженных» могло
    // бы стоять безусловно, и проба выше зеленела бы на подписи, не зависящей
    // ни от чего.
    const spec = REGISTRY.accounts;
    stubList(spec.payloadKey, [{ id: "acc-1", name: "alpha" }], "");
    renderAccountsList(spec);
    await waitFor(() => expect(listUrls(spec.apiPath).length).toBeGreaterThan(0));
    expect(await screen.findByPlaceholderText(/по всему списку/i)).toBeTruthy();
  });
});
