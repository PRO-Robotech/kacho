// Плашка под шапкой: единственный признак того, что запущенная мутация ещё
// идёт. Три её свойства существенны:
//
//   * пока операции нет — плашки нет вовсе (пустая полоса сдвигала бы страницу);
//   * пока операция идёт — виден предмет, чтобы пользователь не жал «Создать»
//     повторно;
//   * на завершении плашка УХОДИТ, а исход объявляется уведомлением. Плашка,
//     пережившая свою операцию, сообщает о выполняющемся действии, которого нет.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";
import { OperationBanner } from "./OperationBanner";

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

afterEach(() => {
  operationStore.dismiss();
  jest.restoreAllMocks();
  globalThis.fetch = realFetch;
});

function renderBanner() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationBanner />
    </QueryClientProvider>,
  );
}

describe("OperationBanner", () => {
  it("без операции разметки не даёт", () => {
    stubOperation({});
    const { container } = renderBanner();
    expect(container).toBeEmptyDOMElement();
  });

  it("пока операция идёт — называет предмет и объявляет себя живой областью", async () => {
    stubOperation({ "op-1": { id: "op-1", done: false } });
    operationStore.start({ id: "op-1", title: "Создание сети core", resourceId: "networks" });
    renderBanner();

    expect(await screen.findByText("Создание сети core")).toBeInTheDocument();
    expect(screen.getByText("операция выполняется…")).toBeInTheDocument();
    // aria-live: иначе изменение шапки для средства чтения с экрана не
    // существует, и подтверждения запуска у пользователя нет вовсе.
    expect(screen.getByRole("status")).toHaveAttribute("aria-live", "polite");
  });

  it("на успешном завершении уходит, а исход объявляется уведомлением", async () => {
    const successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
    stubOperation({ "op-1": { id: "op-1", done: true } });
    operationStore.start({ id: "op-1", title: "Создание сети core", resourceId: "networks" });
    const { container } = renderBanner();

    await waitFor(() => expect(successSpy).toHaveBeenCalledWith("Создание сети core — готово"));
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("операция с ошибкой уходит с отказом, а не с готовностью", async () => {
    const errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
    const successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
    stubOperation({ "op-1": { id: "op-1", done: true, error: { code: 9, message: "cidr overlaps" } } });
    operationStore.start({ id: "op-1", title: "Создание сети core", resourceId: "networks" });
    renderBanner();

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith("Создание сети core: cidr overlaps"));
    expect(successSpy).not.toHaveBeenCalled();
  });

  it("отменённую операцию отличает от отказа", async () => {
    // Отмена — не авария: сообщать о ней тоном ошибки значит звать разбираться
    // туда, где разбираться не с чем.
    const infoSpy = jest.spyOn(toast, "info").mockReturnValue("t");
    const errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
    stubOperation({ "op-1": { id: "op-1", done: true, error: { code: 1, message: "отменена пользователем" } } });
    operationStore.start({ id: "op-1", title: "Создание сети core", resourceId: "networks" });
    renderBanner();

    await waitFor(() => expect(infoSpy).toHaveBeenCalledWith("Создание сети core: отменена пользователем"));
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it("плашку можно скрыть вручную", async () => {
    stubOperation({ "op-1": { id: "op-1", done: false } });
    operationStore.start({ id: "op-1", title: "Создание сети core", resourceId: "networks" });
    const { container } = renderBanner();

    const hide = await screen.findByLabelText("Скрыть");
    hide.click();

    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });
});
