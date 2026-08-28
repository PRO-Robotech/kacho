// Исход мутации читается ОБЩИМ разбором, а не «есть ли id операции» (#406).
//
// ПРЕДМЕТ. Все восемь мутаций машины типизированы как `Promise<{ operation }>`
// (`compute/src/api/resources.ts`), и шапка реестра объявляет «мутации async →
// Operation». Три места домена — пуск/остановка/перезапуск, подключение диска,
// подключение интерфейса — читали ответ так:
//
//     const id = extractOperationId(resp);
//     if (id) setOpId(id); else invalidate(...);
//
// То есть «операции в ответе НЕТ» трактовалось как СИНХРОННЫЙ УСПЕХ: список
// перечитывался, тоста об ошибке не было, оператор уходил в уверенности, что
// машина запущена. Между тем отсутствие операции у глагола, который обязан её
// вернуть, означает ровно обратное — доказательств, что мутация вообще
// исполнилась, нет.
//
// Общий разбор (`@shared/lib/operation-outcome`.`resolveMutationResponse`) это
// различает третьим исходом «нарушение контракта» и зовётся в шести местах
// `shared/`, но в `compute` не звался НИ РАЗУ.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое, а не форма вызова: пользователь при пустом
// ответе видит отказ. Проба, проверяющая «позван ли resolveMutationResponse»,
// закрепила бы способ, а не свойство, и пережила бы любой переезд функции.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

jest.unstable_mockModule("@/index.css", () => ({}));
jest.unstable_mockModule("@/typography.css", () => ({}));

const { InstanceActions } = await import("./InstanceActions");
const { toast } = await import("@/lib/toast");

const realFetch = globalThis.fetch;

/** Ответ края на глагол машины. `body` — ровно то, что вернёт сервер. */
function stubMutation(body: unknown) {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const payload = /:(start|stop|restart)$/.test(url) ? body : {};
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
  }) as typeof fetch;
}

function renderActions() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <InstanceActions instanceId="ins-1" status="STOPPED" projectId="prj-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
  jest.restoreAllMocks();
});

describe("глагол машины: ответ без операции — это отказ, а не тихий успех", () => {
  it("пустой ответ на :start даёт ОТКАЗ пользователю", async () => {
    stubMutation({});
    const err = jest.spyOn(toast, "error").mockImplementation(() => "");
    renderActions();

    await userEvent.click(screen.getByRole("button", { name: /Запустить/ }));

    await waitFor(() => expect(err).toHaveBeenCalled());
    // Текст — общего разбора, а не свой: второй текст об одном предмете
    // разошёлся бы с первым молча.
    expect(String(err.mock.calls[0]?.[0])).toMatch(/не вернул операцию/i);
  });

  it("ответ С операцией отказа НЕ даёт — отрицание в паре с положительным", async () => {
    // Без этого утверждение выше зеленело бы на кнопке, которая отказывает
    // всегда, — то есть на сломанном продукте.
    stubMutation({ operation: { id: "op-1", done: false } });
    const err = jest.spyOn(toast, "error").mockImplementation(() => "");
    renderActions();

    await userEvent.click(screen.getByRole("button", { name: /Запустить/ }));

    await waitFor(() => expect(screen.getByRole("button", { name: /Запустить/ })).toBeDisabled());
    expect(err).not.toHaveBeenCalled();
  });
});
