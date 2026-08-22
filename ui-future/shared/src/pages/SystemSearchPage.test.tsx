// Глобальный поиск спрашивает СЕРВЕР и знает про машины и тома (#373).
//
// ПРЕДМЕТ (три отдельных дефекта в одном месте):
//
//  1. поиск тянул девять списков по 500 строк — БЕЗУСЛОВНО, на открытии
//     страницы, ещё до того как пользователь что-нибудь набрал;
//  2. совпадение искалось подстрокой в браузере, поэтому за пределом пятисот
//     строк ресурс был невидим — а ответ «ничего не найдено» выглядел так же
//     уверенно, как настоящий;
//  3. машины и тома не индексировались вовсе, то есть самый частый запрос
//     администратора («найди эту машину») не решался в принципе.
//
// ЧТО СТАЛО. Ничего не спрашивается, пока не набран запрос. Где владелец
// разбирает выражение фильтра и сохраняет оператор — спрашивается СЕРВЕР, и
// ответ полон. Где не разбирает — остаётся срез по прочитанной странице, и
// страница называет эти области поимённо: неполноту нельзя выдавать за полноту.

import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { REGISTRY } from "@shared/lib/resource-registry";
import { requestUrl } from "@shared/test/fetch-capture";
import { contextApi } from "@shared/lib/context-store";
import { SystemSearchPage, SEARCH_DOMAINS, PROJECT_SCOPED_DOMAINS } from "./SystemSearchPage";

const realFetch = globalThis.fetch;
let urls: string[] = [];

// project-scoped областям (networks и т.п.) для запроса нужен выбранный
// проект — иначе край резолвит отсутствующий `project_id` в `project:*` и
// отвечает отказом (#465), а не пустым списком. По умолчанию проект выбран,
// чтобы существующие пробы, утверждающие про эти области, не путали «нет
// проекта» с «владелец не разбирает выражение».
beforeEach(() => {
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
});

function stub() {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = requestUrl(input);
    urls.push(url);
    const path = url.split("?")[0];
    const d = SEARCH_DOMAINS.find((x) => x.path === path);
    const rows = d ? [{ id: `${d.key}-1`, name: "нечто", project_id: "prj-1" }] : [];
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [d?.key ?? "x"]: rows })),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
  contextApi.setProject(null);
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/search"]}>
        <SystemSearchPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function queryKind(path: string): { filter: string | null; projectId: string | null } | null {
  const u = urls.find((x) => x.split("?")[0] === path);
  if (!u) return null;
  const qs = new URLSearchParams(u.split("?")[1] ?? "");
  return { filter: qs.get("filter"), projectId: qs.get("project_id") };
}

describe("глобальный поиск", () => {
  it("до ввода запроса не спрашивает НИЧЕГО", async () => {
    // Пятнадцать списков на открытии страницы — это стоимость, которую платит
    // каждый заглянувший, и платит зря: искать он ещё ничего не начал.
    stub();
    renderPage();
    await new Promise((r) => setTimeout(r, 300));
    expect(urls).toEqual([]);
  });

  it("машины и тома входят в область поиска", () => {
    // Именно их отсутствие делало неразрешимым «найди эту машину».
    const ids = SEARCH_DOMAINS.map((d) => d.specId);
    expect(ids).toContain("compute-instances");
    expect(ids).toContain("volumes");
  });

  it("там, где владелец разбирает выражение, уходит СЕРВЕРНЫЙ запрос", async () => {
    stub();
    renderPage();
    fireEvent.change(screen.getByPlaceholderText(/Поиск|Найти/i), { target: { value: "прод" } });

    const network = SEARCH_DOMAINS.find((d) => d.specId === "networks")!;
    await waitFor(() => expect(queryKind(network.path)).not.toBeNull());
    expect(queryKind(network.path)!.filter).toBe('name CONTAINS "прод"');
  });

  it("project-scoped область несёт project_id выбранного проекта (#465)", async () => {
    // Прежде запрос уходил без `project_id` ВСЕГДА — край резолвит отсутствие
    // в `project:*` и отвечает отказом в правах, а не пустым списком: поиск
    // по сети/подсети/машине падал целиком отказом сервера, не «не найдено».
    stub();
    renderPage();
    fireEvent.change(screen.getByPlaceholderText(/Поиск|Найти/i), { target: { value: "прод" } });

    const network = SEARCH_DOMAINS.find((d) => d.specId === "networks")!;
    expect(network.scope).toBe("project");
    await waitFor(() => expect(queryKind(network.path)).not.toBeNull());
    expect(queryKind(network.path)!.projectId).toBe("prj-1");
  });

  it("без выбранного проекта project-scoped область НЕ спрашивается вовсе", async () => {
    // Не отказ — отсутствие вопроса: молчаливый провал в 403 хуже честного
    // «эту область не смотрели», и вопрос без обязательного параметра
    // задавать незачем.
    contextApi.setProject(null);
    stub();
    renderPage();
    fireEvent.change(screen.getByPlaceholderText(/Поиск|Найти/i), { target: { value: "прод" } });

    const network = SEARCH_DOMAINS.find((d) => d.specId === "networks")!;
    // Молчание сделало бы «Найдено: 0» неотличимым от «ресурса нет»: страница
    // называет непросмотренные project-scoped области поимённо и числом —
    // тем же приёмом, что клиентскую неполноту (PARTIAL_DOMAINS) выше. Ждём
    // ИМЕННО эту строку (debounce доехал, active обновился) — только тогда
    // отсутствие запроса что-то утверждает, а не просто «ещё не успел».
    expect(PROJECT_SCOPED_DOMAINS.length).toBeGreaterThan(0);
    await screen.findByText(new RegExp(String(PROJECT_SCOPED_DOMAINS.length)));
    expect(screen.getByText(/выберите проект/i)).toBeTruthy();
    expect(queryKind(network.path)).toBeNull();
  });

  it("а там, где не разбирает, фильтр НЕ отправляется", async () => {
    // Владелец, который выражения не знает, молча его игнорирует: список
    // вернулся бы полным под видом отфильтрованного. Это строго хуже среза.
    stub();
    renderPage();
    fireEvent.change(screen.getByPlaceholderText(/Поиск|Найти/i), { target: { value: "прод" } });

    const projects = SEARCH_DOMAINS.find((d) => d.specId === "projects")!;
    expect(REGISTRY[projects.specId]?.serverSearchField).toBeUndefined();
    await waitFor(() => expect(queryKind(projects.path)).not.toBeNull());
    expect(queryKind(projects.path)!.filter).toBeNull();
  });

  it("области, где поиск неполон, названы поимённо", async () => {
    // Молчание сделало бы неполный ответ неотличимым от полного, и «ничего не
    // найдено» читалось бы как утверждение об отсутствии ресурса.
    stub();
    renderPage();
    fireEvent.change(screen.getByPlaceholderText(/Поиск|Найти/i), { target: { value: "прод" } });

    // Ждём УЗЕЛ, а не совпадение текста: регулярка по всему документу зависит
    // от формулировки и молча перестанет что-либо утверждать, когда её
    // перепишут. Признак устойчив к правке текста, потому что он и есть
    // объявление места.
    //
    // Бюджет ожидания ВЫВЕДЕН, а не подобран: ввод отстаёт на объявленные
    // 300 мс (`useDebounced`), дальше идут заглушка сети и рендер. Умолчание
    // библиотеки — 1000 мс, и на нагруженном ранере этого не хватало: проба
    // краснела на ветках, консоли не касавшихся (#983). Больший предел НЕ
    // замедляет зелёный путь — он ограничивает только красный.
    expect(await screen.findByTestId("search-partial-notice", {}, { timeout: 5000 })).toBeTruthy();
  });

  it("каждая область поиска — реальная спека реестра", () => {
    // Иначе список областей и реестр разъедутся молча, и область будет
    // спрашивать несуществующий адрес.
    for (const d of SEARCH_DOMAINS) {
      const spec = REGISTRY[d.specId];
      // Отсутствие спеки называется своим именем: иначе отказ приходит строкой
      // ниже, про `apiPath` у `undefined`, и виновником выглядит адрес.
      expect(spec ? d.specId : `НЕТ СПЕКИ: ${d.specId}`).toBe(d.specId);
      expect(d.path).toBe(spec.apiPath);
      expect(d.key).toBe(spec.payloadKey);
    }
  });
});
