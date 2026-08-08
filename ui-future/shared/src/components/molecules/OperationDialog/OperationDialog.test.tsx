// Окно слежения за асинхронной Operation. Мутации в Kachō возвращают операцию,
// а не ресурс (ban #9), поэтому это окно — единственное место, где пользователь
// узнаёт исход. Существенно то, что три состояния РАЗЛИЧИМЫ и что окно нельзя
// закрыть на полпути: «выполняется», «успешно», «ошибка» — и закрытие,
// разрешённое только когда исход уже известен.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OperationDialog, extractOperationId } from "./OperationDialog";

const realFetch = globalThis.fetch;

function stubOperation(op: unknown) {
  globalThis.fetch = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(op)),
    } as Response)) as typeof fetch;
}

function stubOperationFailure() {
  globalThis.fetch = (() =>
    Promise.resolve({
      ok: false,
      status: 503,
      statusText: "Service Unavailable",
      text: () => Promise.resolve(JSON.stringify({ code: "UNAVAILABLE", message: "operations service is down" })),
    } as Response)) as typeof fetch;
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderDialog(opId: string | null, handlers?: { onSuccess?: () => void; onClose?: () => void }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationDialog
        opId={opId}
        title="Создание подсети"
        onSuccess={handlers?.onSuccess ?? (() => {})}
        onClose={handlers?.onClose ?? (() => {})}
      />
    </QueryClientProvider>,
  );
}

describe("OperationDialog", () => {
  it("без операции окна нет", () => {
    stubOperation({});
    renderDialog(null);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("пока операция идёт — показывает выполнение и не даёт закрыть", async () => {
    stubOperation({ id: "op-1", done: false });
    renderDialog("op-1");

    expect(await screen.findByText("Выполнение операции…")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Создание подсети" })).toBeInTheDocument();
    // Кнопки закрытия нет: исход ещё неизвестен, и закрытое окно оставило бы
    // пользователя без единственного источника исхода.
    expect(screen.queryByRole("button", { name: "Закрыть" })).not.toBeInTheDocument();
  });

  it("на успешном завершении сообщает вызывающему", async () => {
    const onSuccess = jest.fn();
    stubOperation({ id: "op-1", done: true });
    renderDialog("op-1", { onSuccess });

    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(screen.getByText("Успешно завершено")).toBeInTheDocument();
  });

  it("завершённая с ошибкой операция успехом не считается и называет причину", async () => {
    const onSuccess = jest.fn();
    stubOperation({ id: "op-1", done: true, error: { code: 9, message: "subnet overlaps" } });
    renderDialog("op-1", { onSuccess });

    expect(await screen.findByText("Операция завершилась с ошибкой")).toBeInTheDocument();
    expect(screen.getByText("subnet overlaps")).toBeInTheDocument();
    // done=true с error — это ОТКАЗ. Прочитать его как успех значит объявить
    // созданным ресурс, которого нет.
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it("недоступный опрос операции — тоже отказ, а не бесконечное ожидание", async () => {
    // `useOperation` делает две попытки повтора (моргание сети/пира), поэтому
    // отказ доезжает не мгновенно; ждём столько, сколько стоит в самом хуке —
    // иначе проба меряла бы длительность повторов, а не исход.
    stubOperationFailure();
    renderDialog("op-1");

    expect(await screen.findByText("Операция завершилась с ошибкой", undefined, { timeout: 15000 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Закрыть" })).toBeInTheDocument();
  });

  it("закрытие после исхода доезжает до вызывающего", async () => {
    const onClose = jest.fn();
    stubOperation({ id: "op-1", done: true, error: { code: 9, message: "boom" } });
    renderDialog("op-1", { onClose });

    fireEvent.click(await screen.findByRole("button", { name: "Закрыть" }));

    expect(onClose).toHaveBeenCalled();
  });
});

describe("extractOperationId", () => {
  it("читает операцию из конверта и из плоского ответа", () => {
    expect(extractOperationId({ operation: { id: "op-1" } as never })).toBe("op-1");
    expect(extractOperationId({ id: "op-2", done: false })).toBe("op-2");
  });

  it("ресурс с идентификатором операцией не считает", () => {
    // Дискриминатор плоской формы — наличие булева `done`, а не просто `id`:
    // синхронный ответ ресурсом тоже несёт `id`, и принять его за операцию
    // значило бы начать опрашивать несуществующую.
    expect(extractOperationId({ id: "sub-1", name: "frontend" })).toBeNull();
  });

  it("на ответе без операции возвращает пусто, а не выдумывает идентификатор", () => {
    expect(extractOperationId(null)).toBeNull();
    expect(extractOperationId({})).toBeNull();
  });
});
