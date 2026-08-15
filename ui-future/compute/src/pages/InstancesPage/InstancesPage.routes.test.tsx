// InstancesPage (issue #416) — раздел не двоится.
//
// `ComputeRoutes.test.tsx` рядом зовёт `ComputeRoutes()` НАПРЯМУЮ как функцию
// и сверяет получившуюся ТАБЛИЦУ маршрутов сопоставителем `matchRoutes`. Это
// не ловит класс регрессии слияния 000470f2 (PR #388): страница монтирует
// `ComputeFrame`, внутри которого стояли ДВА `<Routes>` — вынесенный
// `ComputeRoutes()` и встроенный узел, оставшийся от слитой стороны. Оба —
// СИБЛИНГИ одного дерева, и react-router рендерит совпадение КАЖДОГО дерева
// независимо: на адресе, который матчат оба (`/instances`, `/instances/create`,
// `/machine-types`, `/instances/:uid`), страница показывала содержимое ДВАЖДЫ;
// на адресе, который матчил только один из них (`/placement-groups` —
// только у `ComputeRoutes()`; `/guest-access-keys` — только у встроенного),
// «мёртвая» сторона доходила до своей ловушки «всё остальное» и уводила
// редиректом на список машин.
//
// Проба монтирует НАСТОЯЩИЙ компонент страницы (не `ComputeRoutes()` как
// функцию) под тем же деревом маршрутов, что и хост
// (`host/src/App.tsx`: `<Route path="/projects/:projectId/compute/*" .../>`),
// чтобы относительные пути и `useParams().projectId` резолвились по-настоящему,
// а не синтетическим срезом.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

jest.unstable_mockModule("@/index.css", () => ({}));
jest.unstable_mockModule("@/typography.css", () => ({}));

// jsdom не несёт ResizeObserver, а `ResourceTable` меряет им собственное тело
// (issue не про это: без стаба ЛЮБОЙ список compute падает на первом же
// рендере, до единой строки). nlb/registry/host/dashboard/shared уже несут
// этот стаб в СВОЁМ `src/test/setup.ts` — у compute (и у vpc) его нет; здесь
// он локальный для этой пробы, а не правка общего окружения модуля (предмет
// задачи — двоение маршрутов, не этот отдельно найденный пробел).
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  Object.assign(globalThis, { ResizeObserver: ResizeObserverStub });
}

const { InstancesPage } = await import("./InstancesPage");

const realFetch = globalThis.fetch;

/**
 * Отвечает на КАЖДЫЙ запрос: карточка `/instances/ins-1` получает плоский
 * ресурс, всё остальное (списочные GET) — пустой конверт.
 *
 * Список не нуждается в содержательном ответе: `.kc-surface`-подложка
 * (`ResourceListPage.tsx:298,307`) рендерится на ЛЮБОМ состоянии списка —
 * loading/rows/welcome/error, — поэтому пустой конверт этого не меняет.
 * Карточка — иначе: `ResourceShell` до прихода данных рисует спиннер БЕЗ
 * `.kc-surface` (`ResourceShell.tsx:341`), так что без ответа проба не
 * добралась бы до предмета проверки вовсе.
 */
function stubApi() {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const body = /\/instances\/ins-1(\?|$)/.test(url) ? { id: "ins-1", name: "vm-1" } : {};
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body)),
    } as Response);
  }) as typeof fetch;
}

beforeEach(stubApi);
afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[`/projects/prj-1/compute${path}`]}>
      <Routes>
        <Route path="/projects/:projectId/compute/*" element={<InstancesPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("InstancesPage — раздел компьюта не двоится (issue #416)", () => {
  const cases: Array<[string, string]> = [
    ["список машин", "/instances"],
    ["создание машины", "/instances/create"],
    ["типы машин", "/machine-types"],
    ["группы размещения", "/placement-groups"],
    ["ключи доступа", "/guest-access-keys"],
    ["карточка машины", "/instances/ins-1"],
  ];

  it.each(cases)("%s (%s) — ровно один узел страницы", async (_label, path) => {
    const { container } = renderAt(path);

    // Объём осмотренного назван: подложка обязана появиться хоть раз, иначе
    // «1 узел» и «0 узлов, потому что проба ничего не дождалась» неразличимы.
    await waitFor(() => expect(container.querySelectorAll(".kc-surface").length).toBeGreaterThan(0));

    expect(container.querySelectorAll(".kc-surface").length).toBe(1);
  });

  it("сохраняет группы размещения при повторном заходе (регрессия «мёртвого» встроенного узла)", async () => {
    // До фикса встроенный узел не нёс маршрута группы размещения вовсе и
    // проваливался в свою ловушку `*` → `<Navigate to=".../instances" replace />`
    // срабатывала СРАЗУ рядом с настоящим совпадением `ComputeRoutes()`, уводя
    // адрес на список машин ещё до того, как оператор успевал что-то увидеть.
    renderAt("/placement-groups");
    await waitFor(() => expect(screen.getAllByText("Группы размещения").length).toBeGreaterThan(0));
  });

  it("сохраняет ключи доступа при повторном заходе (регрессия «мёртвого» вынесенного узла)", async () => {
    // Зеркало предыдущего кейса с другой стороны: `ComputeRoutes()` до фикса
    // НЕ нёс guest-access-keys вовсе (см. задачу), поэтому ИМЕННО он проваливался
    // в свою ловушку и редиректил прочь, пока встроенный узел показывал страницу.
    renderAt("/guest-access-keys");
    await waitFor(() => expect(screen.getAllByText("Ключи доступа").length).toBeGreaterThan(0));
  });
});
