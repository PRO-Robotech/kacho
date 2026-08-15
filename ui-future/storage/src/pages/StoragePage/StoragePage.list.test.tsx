// Список томов монтируется и показывает строку ресурса (issue #418).
//
// КЛАСС ПРОБЫ, КОТОРЫЙ БЫЛ НЕВОЗМОЖЕН. `ResourceTable` меряет собственное тело
// наблюдателем размеров, которого у jsdom нет. Пока окружение проб модуля не
// несло заглушки, ЛЮБОЙ монтаж списка падал на первом же рендере — до единой
// строки, — поэтому у storage не было ни одной пробы, монтирующей список
// (перепись на момент заведения: vpc 1, iam 2, system 1, nlb 1, compute 0,
// storage 0, registry 0).
//
// Локальной заглушки здесь НЕТ намеренно: проба обязана падать, если модуль
// снова отвяжут от общего окружения (`shared/src/test/setup.ts`). Это её вторая
// работа — стеречь сведение, а не только показывать список.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

jest.unstable_mockModule("@/index.css", () => ({}));
jest.unstable_mockModule("@/typography.css", () => ({}));

const { StoragePage } = await import("./StoragePage");

const realFetch = globalThis.fetch;

/** Списочный ответ с ОДНИМ томом; всё остальное — пустой конверт. */
function stubApi() {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const body = /\/storage\/v1\/volumes(\?|$)/.test(url)
      ? { volumes: [{ id: "vol-1", name: "disk-alpha", status: "READY", zone_id: "zone-a" }] }
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
    <MemoryRouter initialEntries={["/projects/prj-1/storage/volumes"]}>
      <Routes>
        <Route path="/projects/:projectId/storage/*" element={<StoragePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("список томов storage — монтируется и показывает строку", () => {
  it("наблюдатель размеров есть в окружении проб модуля", () => {
    // Предпосылка названа отдельным утверждением: без неё падение монтажа ниже
    // читалось бы как «страница сломана», хотя не хватает ровно заглушки.
    expect(typeof (globalThis as { ResizeObserver?: unknown }).ResizeObserver).toBe("function");
  });

  it("строка ресурса доезжает до таблицы", async () => {
    renderList();

    // Объём осмотренного: имя тома пришло из ответа края и отрисовано в
    // таблице. «Ноль строк» и «список не смонтировался» — разные исходы.
    await waitFor(() => expect(screen.getAllByText("disk-alpha").length).toBeGreaterThan(0));
  });

  it("таблица построена по спеке, а не подменена пустым состоянием", async () => {
    const { container } = renderList();

    await waitFor(() => expect(screen.getAllByText("disk-alpha").length).toBeGreaterThan(0));
    // Положительный контроль формы: иначе утверждение выше зеленело бы на любом
    // узле с этим текстом.
    expect(container.querySelectorAll("table").length).toBeGreaterThan(0);
  });
});
