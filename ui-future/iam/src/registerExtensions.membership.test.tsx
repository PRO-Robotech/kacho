// Провязка: секция членства ДОЕЗЖАЕТ до карточки сотрудника.
//
// ПОЧЕМУ ОТДЕЛЬНОЙ ПРОБОЙ. Проба самой секции монтирует компонент напрямую и о
// том, попал ли он в карточку, не говорит ничего: собери расширение без него —
// она останется зелёной. Это ровно тот стык, на котором «возможность оплачена
// разработкой и пользователю не досталась»: код есть, экран его не показывает.
//
// Утверждается ИСХОД — что карточка спрашивает членство у текущего аккаунта, —
// а не наличие узла в дереве: узел можно вернуть и не отрисовать.

import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";
import { requestUrl } from "@shared/test/fetch-capture";
import type { DetailExtCtx } from "@shared/components/organisms/ResourceDetailExtensions";

jest.unstable_mockModule("antd", () => antdStub());

const ACCOUNT = "acc-A";
const PERSON = "usr-P";

let calls: string[] = [];

function serve() {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = requestUrl(input);
    calls.push(url);
    const body = url.includes("/memberships")
      ? {
          memberships: [{ id: "mbr-1", accountId: ACCOUNT, accountName: "Ромашка", userId: PERSON, state: "ACTIVE" }],
        }
      : { id: ACCOUNT, name: "Ромашка" };
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body)),
    } as unknown as Response);
  }) as unknown as typeof fetch;
}

describe("карточка сотрудника: секция членства провязана", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(async () => {
    calls = [];
    serve();
    const { contextApi } = await import("@shared/lib/context-store");
    contextApi.setAccount({ id: ACCOUNT, name: "Ромашка" });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("расширение пользователя отдаёт секцию под обзором", async () => {
    // verifies #1085
    await import("@/registerExtensions");
    const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");
    const ext = detailExtension("users");

    const ctx = {
      data: { id: PERSON },
      projectId: null,
      detailBase: `/iam/users/${PERSON}`,
      navigate: () => {},
    } as unknown as DetailExtCtx;

    const node = ext?.overviewBelow?.(ctx);
    expect(node).toBeTruthy();

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { container } = render(
      <QueryClientProvider client={client}>
        <MemoryRouter>{node}</MemoryRouter>
      </QueryClientProvider>,
    );

    // ИСХОД: вопрос ушёл, и ушёл по аккаунт-скоупному пути с человеком в фильтре.
    await waitFor(() => expect(calls.filter((u) => u.includes("memberships")).length).toBeGreaterThan(0));
    const membership = calls.find((u) => u.includes("memberships"))!;
    expect(membership).toContain(`/iam/v1/accounts/${ACCOUNT}/memberships`);
    expect(decodeURIComponent(membership)).toContain(`filter=userId="${PERSON}"`);

    // И членство названо на экране, а не только спрошено.
    await waitFor(() => expect(container.querySelector(`a[href="/iam/accounts/${ACCOUNT}"]`)).not.toBeNull());
  });

  it("без идентификатора человека секция не спрашивает ничего", async () => {
    // verifies #1085 — карточка рисуется и до того, как приехали её данные;
    // вопрос с пустым человеком отобрал бы членства всего аккаунта.
    await import("@/registerExtensions");
    const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");
    const ext = detailExtension("users");

    const ctx = {
      data: {},
      projectId: null,
      detailBase: "/iam/users/",
      navigate: () => {},
    } as unknown as DetailExtCtx;

    const node = ext?.overviewBelow?.(ctx);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>{node}</MemoryRouter>
      </QueryClientProvider>,
    );

    await new Promise((r) => setTimeout(r, 50));
    expect(calls.filter((u) => u.includes("memberships"))).toHaveLength(0);
  });
});
