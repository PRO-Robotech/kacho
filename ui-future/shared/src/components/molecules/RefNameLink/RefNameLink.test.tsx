// Ссылка на чужой ресурс по идентификатору. Kachō адресует ресурсы по
// неизменяемому `id` (core #15), а показывать пользователю надо имя — его и
// резолвит эта ссылка. Существенны: адрес перехода включает сегмент сервиса
// (без него маршрут не совпадает и клик уводит в пустоту), а нерезолвленный
// ресурс показывается идентификатором, а не исчезает.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { RefNameLink } from "./RefNameLink";

const realFetch = globalThis.fetch;

/** URL'ы, ушедшие со стенда за пробу — по ним видно, ЧТО именно спросили. */
let asked: string[] = [];

function stubList(payload: unknown) {
  asked = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    asked.push(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderLink(props: Partial<Parameters<typeof RefNameLink>[0]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/subnets"]}>
        <RefNameLink specId="networks" refId="net-1" projectId="prj-1" {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("RefNameLink", () => {
  it("без ссылки рисует прочерк, а не пустоту", () => {
    stubList({ networks: [] });
    renderLink({ refId: null });
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("резолвит имя по идентификатору и ведёт на карточку с сегментом сервиса", async () => {
    // Сегмент сервиса обязателен: без него маршрут карточки не совпадает и
    // клик уходит в запасной путь приложения.
    stubList({ networks: [{ id: "net-1", name: "core" }] });
    renderLink();

    const link = await screen.findByRole("link", { name: "core" });
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/networks/net-1");
  });

  // Пробы «клик не всплывает до строки» здесь НЕТ намеренно: строки таблиц были
  // кликабельны целиком, и ссылка гасила событие, чтобы клик по ней не уводил на
  // родителя. Строки перестали быть ссылками (решение владельца 2026-08-12) —
  // гасить нечего, и проба утверждала бы о механизме, которого в дереве нет.

  it("нерезолвленный идентификатор показывает усечённым, а не теряет", async () => {
    // Ресурс мог быть удалён или недоступен вызывающему; пустое место означало
    // бы «ссылки нет», хотя ссылка есть и указывает в никуда.
    stubList({ networks: [] });
    renderLink({ refId: "net01h9zt6k3mqx4vabc" });

    expect(await screen.findByTitle("net01h9zt6k3…")).toBeInTheDocument();
  });

  it("неизвестный тип ресурса показывает идентификатор без ссылки", () => {
    stubList({});
    renderLink({ specId: "нет-такого-ресурса" });
    expect(screen.getByText("net-1")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  // Регион и зона — ГЛОБАЛЬНЫЙ каталог размещения (`scope: "global"`), а не
  // ресурс проекта. Ссылка на них живёт на страницах ВНУТРИ проекта (подсеть
  // называет свой регион), поэтому проект в контексте есть — и запрос всё равно
  // не вправе его нести: у geo такого измерения нет.
  it("глобальный справочник спрашивается без project_id, хотя проект в контексте есть", async () => {
    stubList({ regions: [{ id: "ru-central1" }] });
    renderLink({ specId: "regions", refId: "ru-central1" });

    await screen.findByRole("link", { name: "ru-central1" });
    expect(asked).not.toHaveLength(0);
    expect(asked.some((u) => u.includes("project_id"))).toBe(false);
  });

  it("глобальный справочник резолвится и ведёт на карточку каталога", async () => {
    stubList({ regions: [{ id: "ru-central1" }] });
    renderLink({ specId: "regions", refId: "ru-central1" });

    const link = await screen.findByRole("link", { name: "ru-central1" });
    expect(link).toHaveAttribute("href", "/system/regions/ru-central1");
  });

  it("ресурс проекта по-прежнему спрашивается С project_id", async () => {
    // Положительный контроль: без него первая проба зеленела бы и на запросе,
    // потерявшем project_id для ВСЕХ ресурсов, — то есть на сломанном списке.
    stubList({ networks: [{ id: "net-1", name: "core" }] });
    renderLink();

    await screen.findByRole("link", { name: "core" });
    expect(asked.some((u) => u.includes("project_id=prj-1"))).toBe(true);
  });

  it("длинное имя обрезает, а полное оставляет в подсказке", async () => {
    stubList({ networks: [{ id: "net-1", name: "очень-длинное-имя-сети" }] });
    renderLink({ maxChars: 8 });

    // Ждём именно резолва: до ответа списка ссылка показывает усечённый
    // идентификатор, и утверждение об обрезке имени на нём было бы о другом.
    const link = await screen.findByTitle("очень-длинное-имя-сети");
    expect(link).toHaveTextContent("очень-дл…");
  });
});
