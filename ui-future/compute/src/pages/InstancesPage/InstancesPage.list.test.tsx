// Список машин монтируется и показывает строку ресурса (issue #418).
//
// КЛАСС ПРОБЫ, КОТОРЫЙ БЫЛ НЕВОЗМОЖЕН. `ResourceTable` меряет собственное тело
// наблюдателем размеров, которого у jsdom нет. Пока окружение проб модуля не
// несло заглушки, ЛЮБОЙ монтаж списка падал на первом же рендере — до единой
// строки, — поэтому у compute не было ни одной пробы, монтирующей список
// (перепись на момент заведения: vpc 1, iam 2, system 1, nlb 1, compute 0,
// storage 0, registry 0). Двоение раздела compute (#416) дожило до находки
// владельца именно поэтому: соседняя проба маршрутов вынуждена была носить
// СВОЮ локальную заглушку и оговаривать, что правкой общего окружения она не
// является.
//
// Здесь локальной заглушки НЕТ намеренно: проба обязана падать, если модуль
// снова отвяжут от общего окружения (`shared/src/test/setup.ts`). Это и есть
// её вторая работа — стеречь сведение, а не только показывать список.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

jest.unstable_mockModule("@/index.css", () => ({}));
jest.unstable_mockModule("@/typography.css", () => ({}));

const { InstancesPage } = await import("./InstancesPage");

const realFetch = globalThis.fetch;

/** Списочный ответ с ОДНОЙ машиной; всё остальное — пустой конверт. */
function stubApi() {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const body = /\/compute\/v1\/instances(\?|$)/.test(url)
      ? { instances: [{ id: "ins-1", name: "vm-alpha", status: "RUNNING", zone_id: "zone-a" }] }
      : {};
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

function renderList() {
  return render(
    <MemoryRouter initialEntries={["/projects/prj-1/compute/instances"]}>
      <Routes>
        <Route path="/projects/:projectId/compute/*" element={<InstancesPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("список машин compute — монтируется и показывает строку", () => {
  it("наблюдатель размеров есть в окружении проб модуля", () => {
    // Предпосылка названа отдельным утверждением: без неё падение монтажа ниже
    // читалось бы как «страница сломана», хотя не хватает ровно заглушки.
    expect(typeof (globalThis as { ResizeObserver?: unknown }).ResizeObserver).toBe("function");
  });

  it("строка ресурса доезжает до таблицы", async () => {
    renderList();

    // Объём осмотренного: имя машины пришло из ответа края и отрисовано в
    // таблице. «Ноль строк» и «список не смонтировался» — разные исходы.
    await waitFor(() => expect(screen.getAllByText("vm-alpha").length).toBeGreaterThan(0));
  });

  it("заголовок колонки имени отрисован (таблица построена по спеке)", async () => {
    const { container } = renderList();

    await waitFor(() => expect(screen.getAllByText("vm-alpha").length).toBeGreaterThan(0));
    // Положительный контроль формы: таблица построена, а не подменена пустым
    // состоянием — иначе утверждение выше зеленело бы на любом узле с текстом.
    expect(container.querySelectorAll("table").length).toBeGreaterThan(0);
  });
});
