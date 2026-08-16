// Три исхода мутации — и то, что ни один из них не молчит.
//
// Предмет пробы — НЕ «уходит ли запрос» (это знает форма), а «что увидел
// пользователь». Поэтому каждое утверждение читает сигнал, а не вызов.
//
// Главный случай здесь третий: операция ЗАВЕРШИЛАСЬ с отказом. Ответ на сам
// запрос при этом успешен, идентификатор ресурса в метаданных уже выдан, и
// сообщение «создана», выданное по факту приёма, было бы ложью. Проба на приём
// такой дефект не ловит вовсе — она зелёная ровно тогда, когда он есть.

import { jest } from "@jest/globals";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import React from "react";
import { ApiError } from "@shared/api/client";
import type { Operation } from "@shared/api/types";

const toastError = jest.fn<(m: string) => string>();
const toastSuccess = jest.fn<(m: string) => string>();
let operation: Operation | undefined;
let opFetchError: unknown;

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: (id: string | null) => ({ data: id ? operation : undefined, error: id ? opFetchError : undefined }),
}));

const { useSignalledMutation } = await import("./use-signalled-mutation");

const NETWORK = { label: "Облачная сеть", gender: "f" as const, name: "net-1" };

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return React.createElement(QueryClientProvider, { client: qc }, children);
}

function setup(mutationFn: () => Promise<unknown>, onSucceeded?: () => void) {
  return renderHook(
    () =>
      useSignalledMutation({
        verb: "create",
        subject: NETWORK,
        expectOperation: true,
        mutationFn,
        onSucceeded,
      }),
    { wrapper },
  );
}

beforeEach(() => {
  toastError.mockClear();
  toastSuccess.mockClear();
  operation = undefined;
  opFetchError = undefined;
});

describe("исход 1 — не принята: край отказал сразу", () => {
  it("отказ назван причиной края, успех не объявляется", async () => {
    const { result } = setup(() => Promise.reject(new ApiError(403, "PERMISSION_DENIED", null, "PERMISSION_DENIED: no path")));
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toBe("Облачная сеть net-1 не создана: PERMISSION_DENIED: no path");
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  // Именно этот случай владелец наблюдал на форме создания региона: край отвечал
  // 403, а на экране не появлялось ничего.
  it("403 не проходит молча", async () => {
    const { result } = setup(() => Promise.reject(new ApiError(403, "PERMISSION_DENIED", null, "Forbidden")));
    act(() => result.current.run(undefined as never));
    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
  });
});

describe("исход 2 — принята: операция идёт", () => {
  it("пока операция не завершилась, исход НЕ объявляется", async () => {
    operation = { id: "op-1", done: false } as Operation;
    const { result } = setup(() => Promise.resolve({ id: "op-1", done: false }));
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(result.current.pending).toBe(true));
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });
});

describe("исход 3 — завершена", () => {
  it("успех: сообщение согласовано по роду, onSucceeded вызван", async () => {
    operation = { id: "op-1", done: true } as Operation;
    const onSucceeded = jest.fn();
    const { result } = setup(() => Promise.resolve({ id: "op-1", done: true }), onSucceeded);
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(toastSuccess.mock.calls[0][0]).toBe("Облачная сеть net-1 создана");
    expect(onSucceeded).toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });

  /**
   * ГЛАВНЫЙ случай. Запрос принят, операция заведена, `done=true` — и в ней отказ.
   * Успех здесь объявить нельзя: ресурса нет, а идентификатор в метаданных был
   * выдан ДО отказа.
   */
  it("принята, но ПРОВАЛИЛАСЬ: успех не объявляется, назван отказ операции", async () => {
    operation = {
      id: "op-1",
      done: true,
      error: { code: 9, message: "subnet cidr overlaps existing subnet" },
      metadata: { networkId: "ntw-phantom" },
    } as unknown as Operation;
    const onSucceeded = jest.fn();
    const { result } = setup(() => Promise.resolve({ id: "op-1", done: false }), onSucceeded);
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toBe(
      "Облачная сеть net-1 не создана: subnet cidr overlaps existing subnet",
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    // Успеха не было — значит и продолжения успеха (сброс кэшей, переход) быть
    // не должно: переход увёл бы со страницы, «подтвердив» несозданное.
    expect(onSucceeded).not.toHaveBeenCalled();
  });

  it("опрос операции не прошёл — это отказ, а не вечный спиннер", async () => {
    opFetchError = new Error("Network request failed");
    const { result } = setup(() => Promise.resolve({ id: "op-1", done: false }));
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toContain("не создана");
    expect(toastError.mock.calls[0][0]).toContain("не удалось прочитать статус операции");
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("ответ без операции там, где она обещана, — не успех", async () => {
    const { result } = setup(() => Promise.resolve({ some: "resource" }));
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(toastError.mock.calls[0][0]).toContain("не создана");
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});

describe("синхронный ресурс", () => {
  it("исход известен сразу, и он объявляется", async () => {
    const { result } = renderHook(
      () =>
        useSignalledMutation({
          verb: "update",
          subject: { label: "Пул адресов", gender: "m", name: "pool-1" },
          expectOperation: false,
          mutationFn: () => Promise.resolve({ id: "apl-1", name: "pool-1" }),
        }),
      { wrapper },
    );
    act(() => result.current.run(undefined as never));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(toastSuccess.mock.calls[0][0]).toBe("Пул адресов pool-1 обновлён");
  });
});
