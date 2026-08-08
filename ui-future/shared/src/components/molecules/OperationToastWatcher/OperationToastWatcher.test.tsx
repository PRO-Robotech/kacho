// Невидимый наблюдатель за асинхронной Operation: разметки не даёт, поэтому
// единственное его наблюдаемое поведение — уведомления и вызов вызывающего.
// Проверяется то, ради чего он существует: пользователь узнаёт исход, исход
// объявляется РОВНО ОДИН раз (опрос идёт секундами, и каждый ответ мог бы
// породить свой всплывающий отчёт), а ожидание снимается при смене предмета.

import { jest } from "@jest/globals";
import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "@shared/lib/toast";
import { OperationToastWatcher } from "./OperationToastWatcher";

const realFetch = globalThis.fetch;

function stubOperation(byId: Record<string, unknown>) {
  globalThis.fetch = ((url: string) => {
    const id = String(url).split("/operations/")[1] ?? "";
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(byId[id] ?? {})),
    } as Response);
  }) as typeof fetch;
}

let loadingSpy: jest.Spied<typeof toast.loading>;
let successSpy: jest.Spied<typeof toast.success>;
let errorSpy: jest.Spied<typeof toast.error>;
let dismissSpy: jest.Spied<typeof toast.dismiss>;

beforeEach(() => {
  loadingSpy = jest.spyOn(toast, "loading").mockReturnValue("t-load");
  successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
  errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
  dismissSpy = jest.spyOn(toast, "dismiss").mockReturnValue(undefined as never);
});

afterEach(() => {
  jest.restoreAllMocks();
  globalThis.fetch = realFetch;
});

function renderWatcher(opId: string | null, onDone?: (ok: boolean) => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationToastWatcher opId={opId} title="Удаление сети core" onDone={onDone} />
    </QueryClientProvider>,
  );
}

describe("OperationToastWatcher", () => {
  it("без операции молчит и разметки не даёт", () => {
    stubOperation({});
    const { container } = renderWatcher(null);
    expect(container).toBeEmptyDOMElement();
    expect(loadingSpy).not.toHaveBeenCalled();
  });

  it("объявляет ожидание, пока операция идёт", async () => {
    stubOperation({ "op-1": { id: "op-1", done: false } });
    renderWatcher("op-1");

    await waitFor(() => expect(loadingSpy).toHaveBeenCalledWith("Удаление сети core…"));
    expect(successSpy).not.toHaveBeenCalled();
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it("на успехе сообщает о готовности и снимает ожидание", async () => {
    const onDone = jest.fn<(ok: boolean) => void>();
    stubOperation({ "op-1": { id: "op-1", done: true } });
    const { unmount } = renderWatcher("op-1", onDone);

    await waitFor(() => expect(successSpy).toHaveBeenCalledWith("Удаление сети core: готово"));
    expect(onDone).toHaveBeenCalledWith(true);

    unmount();
    expect(dismissSpy).toHaveBeenCalledWith("t-load");
  });

  it("операция с ошибкой — отказ с текстом сервера, а не успех", async () => {
    const onDone = jest.fn<(ok: boolean) => void>();
    stubOperation({ "op-1": { id: "op-1", done: true, error: { code: 9, message: "network is not empty" } } });
    renderWatcher("op-1", onDone);

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith("Удаление сети core: network is not empty"));
    expect(successSpy).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledWith(false);
  });

  it("объявляет исход один раз, а не на каждый ответ опроса", async () => {
    // Опрос идёт секундами; отчёт на каждый ответ засыпал бы экран
    // уведомлениями об одном и том же событии.
    stubOperation({ "op-1": { id: "op-1", done: true } });
    const { rerender } = renderWatcher("op-1");

    await waitFor(() => expect(successSpy).toHaveBeenCalledTimes(1));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    rerender(
      <QueryClientProvider client={qc}>
        <OperationToastWatcher opId="op-1" title="Удаление сети core" />
      </QueryClientProvider>,
    );

    expect(successSpy).toHaveBeenCalledTimes(1);
  });
});
