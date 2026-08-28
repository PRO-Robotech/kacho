// Исход удаления тега сообщается ОБЩИМ разбором, а не своим чтением ключа.
//
// `DeleteTag` объявлен возвращающим `Operation` (registry_service.proto), значит
// у исхода ТРИ состояния, а не два:
//
//   • операция дошла до `done` без ошибки — выполнено;
//   • операция дошла до `done` с ошибкой — отказ;
//   • ОПРОС операции не удался — исход НЕИЗВЕСТЕН. Это не «ещё идёт»: ответа не
//     будет, ждать нечего, и молчание оставляет человека перед вращающейся
//     кнопкой навсегда — без единого слова о том, что случилось.
//
// Своё чтение (`if (!op?.done) return;`) третье состояние от второго не
// отличает by construction: при отказе опроса `op` остаётся неопределённым, и
// ветка выходит молча.
//
// Четвёртый случай — ответ БЕЗ операции. Контракт её обещает, поэтому такой
// ответ есть нарушение контракта, а не «выполнено синхронно»: подтвердить
// выполнение нечем, и слово «удалён» было бы сказано без свидетельства.
//
// Утверждается СИГНАЛ (что человеку сказали), а не вызов: проба на «ушёл ли
// запрос» зелена ровно тогда, когда дефект есть.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import React from "react";
import type { Operation } from "@shared/api/types";

const toastError = jest.fn<(m: string) => string>();
const toastSuccess = jest.fn<(m: string) => string>();
let operation: Operation | undefined;
let opFetchError: unknown;

jest.unstable_mockModule("@/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

// Опрос операции подменён на уровне хука: предмет пробы — как читается ЕГО
// ответ, а не как он устроен. Обе половины ответа существенны, поэтому у
// подделки они раздельные: `data` и `error`.
jest.unstable_mockModule("@/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: (id: string | null) => ({ data: id ? operation : undefined, error: id ? opFetchError : undefined }),
}));
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: (id: string | null) => ({ data: id ? operation : undefined, error: id ? opFetchError : undefined }),
}));

const { RepositoryTagsPanel } = await import("./RepositoryTagsPanel");

const realFetch = globalThis.fetch;

const TAG_ROW = {
  tag: "v1", registry_id: "reg-1", repository: "nginx", digest: "sha256:abc",
  size_bytes: "1048576", media_type: "application/vnd.oci.image.manifest.v1+json",
  created_at: "2026-08-03T10:00:00Z",
};

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/**
 * Стенд отвечает ТОЛЬКО по объявленным адресам; на прочие — отказ.
 * Снисходительный стенд сделал бы невидимым дефект, ради которого проба пишется.
 */
function stubApi(deleteAnswer: unknown) {
  globalThis.fetch = (input, init) => {
    const p = decodeURIComponent(new URL(requestUrl(input), "http://console.test").pathname);
    const method = (init?.method ?? "GET").toUpperCase();
    const ok = (body: unknown) =>
      Promise.resolve({ ok: true, status: 200, statusText: "OK", text: () => Promise.resolve(JSON.stringify(body)) } as Response);
    if (method === "DELETE" && p.endsWith("/tags/v1")) return ok(deleteAnswer);
    if (p === "/registry/v1/registries/reg-1/repositories/nginx/tags") return ok({ tags: [TAG_ROW], nextPageToken: "" });
    return Promise.resolve({
      ok: false, status: 404, statusText: "Not Found",
      text: () => Promise.resolve(JSON.stringify({ code: 5, message: `no stub for ${p}` })),
    } as Response);
  };
}

beforeEach(() => {
  toastError.mockClear();
  toastSuccess.mockClear();
  operation = undefined;
  opFetchError = undefined;
});

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

function mountPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    React.createElement(
      QueryClientProvider,
      { client: qc },
      React.createElement(
        MemoryRouter,
        { initialEntries: ["/projects/prj-1/registry/registries/reg-1/repositories"] },
        React.createElement(RepositoryTagsPanel, { registryId: "reg-1", repository: "nginx", onClose: () => {} }),
      ),
    ),
  );
}

/** Нажать удаление тега и подтвердить — путём человека, а не вызовом функции. */
async function deleteTagV1() {
  const trigger = await screen.findByLabelText("Удалить тег");
  fireEvent.click(trigger);
  fireEvent.click(await screen.findByRole("button", { name: /^Удалить$/ }));
}

describe("исход удаления тега не молчит ни в одном из состояний", () => {
  it("опрос операции не удался — человеку сказано, а не вечное ожидание", async () => {
    stubApi({ id: "op-1", done: false });
    opFetchError = new Error("operations unavailable");
    mountPanel();
    await deleteTagV1();

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toContain("не удалось прочитать статус операции");
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("ответ без операции не объявляется успехом — подтвердить нечем", async () => {
    stubApi({});
    mountPanel();
    await deleteTagV1();

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toContain("сервер не вернул операцию");
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("операция завершилась отказом — сказано об отказе, а не об успехе", async () => {
    stubApi({ id: "op-1", done: false });
    operation = { id: "op-1", done: true, error: { code: 7, message: "PERMISSION_DENIED: no path" } } as Operation;
    mountPanel();
    await deleteTagV1();

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toContain("PERMISSION_DENIED: no path");
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  // Положительный контроль: без него три отрицания выше зеленели бы на панели,
  // где удаление не работает вовсе, — а именно так выглядит сорванная проба.
  it("операция дошла до конца без ошибки — сказано об успехе", async () => {
    stubApi({ id: "op-1", done: false });
    operation = { id: "op-1", done: true } as Operation;
    mountPanel();
    await deleteTagV1();

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(toastSuccess.mock.calls[0][0]).toContain("v1");
    expect(toastError).not.toHaveBeenCalled();
  });
});
