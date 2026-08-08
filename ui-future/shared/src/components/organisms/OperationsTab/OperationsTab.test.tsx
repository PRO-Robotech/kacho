// Вкладка операций ресурса. Существенны три её решения:
//
//   * путь списка НЕ собирается здесь — он приходит готовым. Прежде он клеился
//     из адреса ресурса, и у части реестра такого связывания в стволе нет вовсе:
//     вкладка спрашивала несуществующий адрес и показывала отказ края;
//   * новое сверху. Журнал, отсортированный как попало, бесполезен для разбора;
//   * отказ доступа и «не реализовано» названы РАЗНО — это разные действия
//     пользователя (просить права против ждать реализации).

import { jest } from "@jest/globals";
import React from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

// antd переопределён локально: общий заменитель подменяет `Result` пустым
// div'ом, своих пропсов он не рисует — на нём обе ветки отказа (нет прав / не
// реализовано) неотличимы друг от друга и от пустого экрана.
jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Result: ({ title, subTitle }: { title?: React.ReactNode; subTitle?: React.ReactNode }) =>
    React.createElement("div", { role: "alert" }, title, subTitle),
}));

const { REGISTRY } = await import("@shared/lib/resource-registry");
const { OperationsTab } = await import("./OperationsTab");

const realFetch = globalThis.fetch;
let asked: string[] = [];

function stubServer(reply: (url: string) => { ok: boolean; status: number; body: unknown }) {
  globalThis.fetch = ((url: string) => {
    asked.push(String(url));
    const r = reply(String(url));
    return Promise.resolve({
      ok: r.ok,
      status: r.status,
      statusText: String(r.status),
      text: () => Promise.resolve(JSON.stringify(r.body)),
    } as Response);
  }) as typeof fetch;
}

beforeEach(() => {
  asked = [];
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderTab(listPath = "/vpc/v1/subnets/sub-1/operations") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationsTab spec={REGISTRY["subnets"]} resourceId="sub-1" listPath={listPath} />
    </QueryClientProvider>,
  );
}

describe("OperationsTab", () => {
  it("спрашивает ровно тот путь, который ей дали", async () => {
    stubServer((url) => ({ ok: true, status: 200, body: url.includes("/users") ? { users: [] } : { operations: [] } }));
    renderTab("/vpc/v1/subnets/sub-1/operations");

    await waitFor(() => expect(asked.some((u) => u.includes("/vpc/v1/subnets/sub-1/operations"))).toBe(true));
  });

  it("показывает операции ресурса, новое сверху", async () => {
    stubServer((url) =>
      url.includes("/users")
        ? { ok: true, status: 200, body: { users: [] } }
        : {
            ok: true,
            status: 200,
            body: {
              operations: [
                { id: "op-old", description: "старая", done: true, created_at: "2026-08-01T10:00:00Z" },
                { id: "op-new", description: "новая", done: true, created_at: "2026-08-07T10:00:00Z" },
              ],
            },
          },
    );
    renderTab();

    await screen.findByText("новая");
    const rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]).getByText("новая")).toBeInTheDocument();
    expect(within(rows[1]).getByText("старая")).toBeInTheDocument();
  });

  it("проставляет операции идентификатор своего ресурса, если сервер его не назвал", async () => {
    stubServer((url) =>
      url.includes("/users")
        ? { ok: true, status: 200, body: { users: [] } }
        : { ok: true, status: 200, body: { operations: [{ id: "op-1", description: "создание", done: true }] } },
    );
    renderTab();

    await screen.findByText("создание");
    expect(screen.getByText("sub-1")).toBeInTheDocument();
  });

  it("отказ доступа объясняет правами", async () => {
    stubServer(() => ({ ok: false, status: 403, body: { code: "PERMISSION_DENIED", message: "no path" } }));
    renderTab();

    expect(await screen.findByRole("alert")).toHaveTextContent("Недостаточно прав для просмотра операций этого ресурса.");
  });

  it("нереализованный список объясняет нереализованностью, а не правами", async () => {
    // Разные действия пользователя: в первом случае просить права, во втором —
    // ждать реализации. Один текст на оба случая посылает не туда.
    stubServer(() => ({ ok: false, status: 501, body: { code: "UNIMPLEMENTED", message: "not implemented" } }));
    renderTab();

    // 501 — не 4xx, поэтому вкладка делает одну повторную попытку (её условие
    // повтора исключает только клиентские коды). Ждём дольше умолчания: иначе
    // проба мерила бы длительность повтора, а не выбранный текст.
    const alert = await screen.findByRole("alert", undefined, { timeout: 10000 });
    expect(alert).toHaveTextContent("ListOperations для этого ресурса пока не реализован.");
    expect(alert).not.toHaveTextContent("Недостаточно прав");
  });

  it("пустой журнал говорит, что операций не было", async () => {
    stubServer((url) => ({ ok: true, status: 200, body: url.includes("/users") ? { users: [] } : { operations: [] } }));
    renderTab();

    expect(await screen.findByText("Операций пока нет.")).toBeInTheDocument();
  });
});
