// Таблица операций — журнал того, что консоль запускала. Мутации асинхронны
// (ban #9), поэтому это единственное место, где видно, ЧТО не выполнилось и
// почему. Существенны: различимость исходов, показ текста отказа дословно,
// резолв инициатора в почту (идентификатор пользователя сам по себе никому
// ничего не говорит) и честный фоллбэк, когда справочник не доехал.

import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OperationsTable, matchesOutcome, statusOf, statusLabel, type Op } from "./OperationsTable";

const realFetch = globalThis.fetch;

function stubUsers(users: unknown[] | null) {
  globalThis.fetch = (() =>
    users === null
      ? Promise.resolve({
          ok: false,
          status: 403,
          statusText: "Forbidden",
          text: () => Promise.resolve(JSON.stringify({ code: "PERMISSION_DENIED", message: "no path" })),
        } as Response)
      : Promise.resolve({
          ok: true,
          status: 200,
          statusText: "OK",
          text: () => Promise.resolve(JSON.stringify({ users })),
        } as Response));
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderTable(rows: Op[], props: { showResourceKind?: boolean; empty?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationsTable rows={rows} {...props} />
    </QueryClientProvider>,
  );
}

function rowOf(text: string): HTMLElement {
  return screen.getByText(text).closest("tr")!;
}

describe("OperationsTable", () => {
  it("строка на операцию с её идентификатором и описанием", () => {
    stubUsers([]);
    renderTable([{ id: "op-1", description: "Создание подсети", created_by: "usr-1" }]);

    expect(screen.getByText("op-1")).toBeInTheDocument();
    expect(screen.getByText("Создание подсети")).toBeInTheDocument();
  });

  it("различает выполнение, успех, отказ и отмену", () => {
    stubUsers([]);
    renderTable([
      { id: "op-run", description: "идёт", done: false },
      { id: "op-ok", description: "успех", done: true },
      { id: "op-err", description: "отказ", done: true, error: { code: 9, message: "subnet is not empty" } },
      { id: "op-cancel", description: "отмена", done: true, error: { code: 1, message: "cancelled" } },
    ]);

    expect(within(rowOf("идёт")).getByText(statusLabel("running"))).toBeInTheDocument();
    expect(within(rowOf("успех")).getByText(statusLabel("done"))).toBeInTheDocument();
    expect(within(rowOf("отказ")).getByText(statusLabel("error"))).toBeInTheDocument();
    expect(within(rowOf("отмена")).getByText(statusLabel("cancelled"))).toBeInTheDocument();
  });

  it("показывает текст отказа дословно", () => {
    // Тон сообщений сервера — часть контракта; пересказ здесь сделал бы журнал
    // непригодным для разбора.
    stubUsers([]);
    renderTable([{ id: "op-1", description: "Удаление сети", done: true, error: { message: "network is not empty" } }]);

    expect(screen.getByText("network is not empty")).toBeInTheDocument();
  });

  it("резолвит инициатора в почту через справочник", async () => {
    stubUsers([{ id: "usr-1", email: "ops@kacho.local" }]);
    renderTable([{ id: "op-1", description: "Создание подсети", created_by: "usr-1" }]);

    expect(await screen.findByText("ops@kacho.local")).toBeInTheDocument();
  });

  it("без справочника показывает идентификатор как есть, а не пустоту", async () => {
    // Справочник глобальный, и доступа к нему у арендатора может не быть.
    // Пустая клетка означала бы «операцию никто не запускал».
    stubUsers(null);
    renderTable([{ id: "op-1", description: "Создание подсети", created_by: "usr-1" }]);

    await waitFor(() => expect(screen.getByText("usr-1")).toBeInTheDocument());
  });

  it("колонка типа ресурса появляется только там, где её просили", () => {
    stubUsers([]);
    const { unmount } = renderTable([{ id: "op-1", resource_kind: "subnets" }]);
    expect(screen.queryByText("Тип ресурса")).not.toBeInTheDocument();
    unmount();

    renderTable([{ id: "op-1", resource_kind: "subnets" }], { showResourceKind: true });
    expect(screen.getByText("Тип ресурса")).toBeInTheDocument();
    expect(screen.getByText("subnets")).toBeInTheDocument();
  });

  it("пустой журнал отличает «операций не было» от «фильтр ничего не нашёл»", () => {
    stubUsers([]);
    const { unmount } = renderTable([]);
    expect(screen.getByText("Операций пока нет.")).toBeInTheDocument();
    unmount();

    renderTable([], { empty: true });
    expect(screen.getByText("По фильтру ничего не найдено.")).toBeInTheDocument();
  });
});

describe("statusOf", () => {
  it("незавершённая операция — выполняется", () => {
    expect(statusOf({ done: false })).toBe("running");
  });

  it("завершённая без ошибки — успех, с ошибкой — отказ", () => {
    expect(statusOf({ done: true })).toBe("done");
    expect(statusOf({ done: true, error: { message: "boom" } })).toBe("error");
  });

  it("отмену отличает от отказа по коду", () => {
    expect(statusOf({ done: true, error: { code: 1, message: "cancelled" } })).toBe("cancelled");
  });
});

describe("matchesOutcome", () => {
  it("оставляет только отказавшие для фильтра ошибок", () => {
    expect(matchesOutcome({ done: true, error: { code: 13, message: "boom" } }, "error")).toBe(true);
    expect(matchesOutcome({ done: true }, "error")).toBe(false);
    expect(matchesOutcome({ done: false }, "error")).toBe(false);
  });

  it("оставляет только успешно завершённые для фильтра успеха", () => {
    expect(matchesOutcome({ done: true }, "ok")).toBe(true);
    expect(matchesOutcome({ done: true, error: { message: "x" } }, "ok")).toBe(false);
    expect(matchesOutcome({ done: false }, "ok")).toBe(false);
  });

  it("пропускает всё для фильтра «все»", () => {
    expect(matchesOutcome({ done: true }, "all")).toBe(true);
    expect(matchesOutcome({ done: true, error: { message: "x" } }, "all")).toBe(true);
    expect(matchesOutcome({ done: false }, "all")).toBe(true);
  });
});
