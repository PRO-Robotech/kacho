// Сброс второго фактора из меню строки пользователя — что уходит к краю и что
// человек читает об исходе (приёмка F8r, S1; kacho#3063).
//
// Отдельным файлом от `RowActionsMenu.rowverbs.test.tsx`: здесь подменены опрос
// операции и сообщения, а там предмет — состав меню на НАСТОЯЩЕМ клиенте, и
// подмена опроса сделала бы его утверждения о другом механизме.
//
// F8r-05 — пара: отмена окна к краю НЕ уходит; подтверждение уходит ровно один
// раз и ровно тем глаголом. Одно отрицание зеленело бы на пункте, который не
// зовёт ничего вовсе.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";
import type { Operation } from "@shared/api/types";

jest.unstable_mockModule("antd", () => antdStub());

const realClient = await import("@shared/api/client");
const apiAction = jest.fn<(path: string, body?: unknown) => Promise<unknown>>();
jest.unstable_mockModule("@shared/api/client", () => ({
  ...realClient,
  api: { ...realClient.api, action: apiAction },
}));

// Личность вызывающего подменена, чтобы проба не поднимала поток входа.
const realAuth = await import("@shared/contexts/AuthContext");
jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  ...realAuth,
  useSelfUserId: () => undefined,
}));

const toastError = jest.fn<(m: string) => string>();
const toastSuccess = jest.fn<(m: string) => string>();
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

let operation: Operation | undefined;
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: (id: string | null) => ({ data: id ? operation : undefined, error: undefined }),
}));

const { REGISTRY } = await import("@shared/lib/resource-registry");
const { RowActionsMenu } = await import("./RowActionsMenu");

const ITEM = "Сбросить второй фактор";

function renderUsersMenu() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/iam/users"]}>
        <RowActionsMenu
          spec={REGISTRY.users}
          row={{ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" }}
          basePath="/iam/users"
          projectId={null}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function openReset() {
  const item = screen.getAllByRole("menuitem").find((b) => (b.textContent ?? "").includes(ITEM));
  // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: пункт есть. Без него отрицание «не вызван» ниже
  // зеленело бы на меню без пункта.
  expect(item).toBeDefined();
  fireEvent.click(item!);
  return screen.getByRole("dialog");
}

beforeEach(() => {
  apiAction.mockReset();
  apiAction.mockResolvedValue({ id: "op-1", done: false });
  toastError.mockClear();
  toastSuccess.mockClear();
  operation = undefined;
});

describe("F8r-05 · отмена подтверждения не уходит к краю", () => {
  it("F8r-05 · окно закрыто отменой — api.action не вызван ни разу", () => {
    // verifies #3063
    renderUsersMenu();
    const dialog = openReset();
    fireEvent.click(screen.getByRole("button", { name: "Отмена" }));
    expect(dialog.isConnected).toBe(false);
    expect(apiAction).not.toHaveBeenCalled();
  });

  it("F8r-05 · близнец: «Сбросить» — api.action ровно один раз и без тела сверх пустого", async () => {
    renderUsersMenu();
    openReset();
    fireEvent.click(screen.getByRole("button", { name: "Сбросить" }));
    await waitFor(() => expect(apiAction).toHaveBeenCalledTimes(1));
    // Второй довод `undefined`: клиент шлёт `{}` (`api.action`), и ничего сверх.
    expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-1:resetSecondFactor", undefined);
  });
});

describe("Р5 · исход сброса назван словами действия", () => {
  it("F8r-01 · операция завершилась без ошибки — «Второй фактор сброшен»", async () => {
    // verifies #3063
    operation = { id: "op-1", done: true };
    renderUsersMenu();
    openReset();
    fireEvent.click(screen.getByRole("button", { name: "Сбросить" }));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Второй фактор сброшен"));
    expect(toastError).not.toHaveBeenCalled();
  });

  it("F8r-02 · отказ края — «Не удалось сбросить второй фактор: » и причина", async () => {
    apiAction.mockRejectedValue(
      realClient.apiErrorFromBody(
        403,
        "Forbidden",
        JSON.stringify({
          code: 7,
          message: "permission denied",
          details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "AUTHZ_DENIED" }],
        }),
      ),
    );
    renderUsersMenu();
    openReset();
    fireEvent.click(screen.getByRole("button", { name: "Сбросить" }));
    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
    const text = toastError.mock.calls[0][0];
    expect(text.startsWith("Не удалось сбросить второй фактор: ")).toBe(true);
    // Причина — вердикт консоли по признаку, а не проза края.
    expect(text).not.toContain("permission denied");
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});
