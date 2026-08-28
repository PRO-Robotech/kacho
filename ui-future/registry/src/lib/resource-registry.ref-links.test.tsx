// Поле-ссылка на ЧУЖОЙ ресурс показывается ссылкой, а не плоским текстом.
//
// Правило 2 канона консоли: значение, которое есть идентификатор другого
// ресурса, рисуется `RefNameLink` — значок типа, имя, переход. Идентификатор
// человеку не адресован: Kachō адресует ресурсы неизменяемым `id` (ban #15), а
// работает человек с именем.
//
// У реестра такое поле одно — регион размещения. Реестр REGIONAL-anycast:
// `region_id` обязателен и неизменяем после создания, а сам регион живёт в
// ГЛОБАЛЬНОМ каталоге geo, то есть у него нет измерения «проект».
//
// Утверждается АДРЕС перехода, а не присутствие текста. Проба на текст осталась
// бы зелёной на плоском идентификаторе — из него никуда не перейти, и это ровно
// то состояние, которое правило запрещает.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";

import { REGISTRY } from "./resource-registry";
import { buildSpecColumns } from "./spec-columns";

const realFetch = globalThis.fetch;

function stubList(payload: unknown) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

/** Ячейка колонки с заголовком `header` — ровно тем путём, каким её строит список. */
function cellOf(specId: string, header: string, row: Record<string, unknown>): ReactNode {
  const spec = REGISTRY[specId];
  if (!spec) throw new Error(`нет спеки ${specId} — перечень реестра изменился`);
  const col = buildSpecColumns(spec).find((c) => c.header === header);
  if (!col) throw new Error(`нет колонки «${header}» у ${specId} — перечень колонок изменился`);
  return col.cell(row);
}

function draw(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/registry/registries"]}>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const ROW = { id: "reg-01h9zt6k3mqx4vab", name: "core", region_id: "ru-central1", endpoint: "reg.kacho.local" };

describe("правило 2: поле-ссылка реестра ведёт на карточку чужого ресурса", () => {
  it("регион размещения — ссылка на карточку региона, а не плоский текст", async () => {
    // Каталог размещения ГЛОБАЛЬНЫЙ, поэтому карточка живёт под /system/*, а не
    // внутри проекта: требовать проект значило бы не строить ссылку там, где
    // проекта в контексте нет вовсе.
    stubList({ regions: [{ id: "ru-central1" }] });
    draw(cellOf("registries", "Регион", ROW));

    const link = await screen.findByRole("link");
    expect(link).toHaveAttribute("href", "/system/regions/ru-central1");
  });

  // Положительный контроль в паре с отрицанием ниже: без него «ссылки нет»
  // зеленело бы на ячейке, которая не показывает НИЧЕГО.
  it("значение, ссылкой не являющееся, ссылкой и не становится", () => {
    stubList({ regions: [] });
    const { container } = draw(cellOf("registries", "Адрес", ROW));
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("reg.kacho.local");
  });

  it("непришедший регион остаётся прочерком, а не ссылкой в никуда", () => {
    stubList({ regions: [] });
    const { container } = draw(cellOf("registries", "Регион", { ...ROW, region_id: "" }));
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("—");
  });
});
