// Наблюдаемое: что видит оператор, когда край ответил на действие БЕЗ операции.
//
// Мутации Kachō отвечают `Operation`, а не ресурсом (ban #9). Ответ, в котором
// операции нет, — не «выполнено синхронно», а нарушение контракта: подтвердить
// выполнение нечем. Прежняя форма дефекта: разбор шёл своим ключом
// (`extractOperationId` → `if (id) … else …`), и ветка `else` МОЛЧА читалась как
// успех — список обновлялся, оператор уходил в уверенности, что машина
// запущена. Это ровно тот же класс, что фантомный ресурс: признак успеха берётся
// из ответа, который успеха не утверждал.
//
// Отдельно про то, почему «операции нет» — состояние достижимое, а не выдуманное:
// разбор признаёт конверт верхнего уровня только при БУЛЕВОМ `done`, а
// сериализация опускает значения по умолчанию, поэтому операция с `done=false`
// приезжает без ключа `done` вовсе.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const start = jest.fn<() => Promise<unknown>>();
const errorToast = jest.fn<(text: string) => void>();
const invalidate = jest.fn<(specId: string, projectId: string | null) => void>();

jest.unstable_mockModule("@/api/resources", () => ({
  instancesApi: { start, stop: start, restart: start },
}));
jest.unstable_mockModule("@/lib/toast", () => ({
  toast: { error: errorToast, success: jest.fn(), info: jest.fn() },
}));
jest.unstable_mockModule("@/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
}));

const { InstanceActions } = await import("./InstanceActions");

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <InstanceActions instanceId="ins-1" status="STOPPED" projectId="prj-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  start.mockReset();
  errorToast.mockReset();
  invalidate.mockReset();
});

describe("действие над машиной: ответ без операции подтверждением не является", () => {
  it("ответ без операции — отказ оператору, а не молчаливое обновление списка", async () => {
    start.mockResolvedValue({});
    mount();
    await userEvent.click(screen.getByRole("button", { name: /Запустить/ }));

    await waitFor(() => expect(errorToast).toHaveBeenCalled());
    expect(String(errorToast.mock.calls[0]?.[0])).toMatch(/операц/i);
    // Обновление списка здесь означало бы «выполнено»: подтверждать нечем.
    expect(invalidate).not.toHaveBeenCalled();
  });

  // Положительный контроль: законный ответ с операцией проходит и отказом НЕ
  // становится. Без него утверждение выше зеленело бы на правке, которая просто
  // объявляет отказ на любой ответ.
  it("ответ С операцией отказом не становится", async () => {
    start.mockResolvedValue({ id: "op-1", done: false });
    mount();
    await userEvent.click(screen.getByRole("button", { name: /Запустить/ }));

    await waitFor(() => expect(start).toHaveBeenCalled());
    expect(errorToast).not.toHaveBeenCalled();
  });
});
