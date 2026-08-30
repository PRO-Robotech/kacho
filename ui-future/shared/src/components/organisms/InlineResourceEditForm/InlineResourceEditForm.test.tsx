// Встраиваемая форма правки. Её предмет — МАСКА: край меняет ровно то, что в
// ней названо, поэтому поле, которого оператор не трогал, не должно там
// оказаться (иначе правка одного поля переписывает соседние), а не изменившая
// ничего форма не должна слать запрос вовсе.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { FormField } from "@shared/lib/form-schema";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { Operation } from "@shared/api/types";

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();
const invalidate = jest.fn();
let operation: Operation | undefined;

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: (id: string | null) => ({ data: id ? operation : undefined }),
}));

const { InlineResourceEditForm } = await import("./InlineResourceEditForm");

const FIELDS: FormField[] = [
  { type: "string", name: "name", label: "Имя" },
  { type: "string", name: "description", label: "Описание" },
  { type: "string", name: "cidr", label: "Диапазон", immutable: true },
];

function spec(over: Partial<ResourceSpec> = {}): ResourceSpec {
  return {
    id: "networks",
    route: "networks",
    apiPath: "/vpc/v1/networks",
    payloadKey: "networks",
    singular: "Сеть",
    plural: "Сети",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: FIELDS,
    template: () => ({}),
    ...over,
  } as unknown as ResourceSpec;
}

const DATA = { id: "net-1", name: "frontend", description: "было", cidr: "10.0.0.0/16" };

function show(over: Partial<Parameters<typeof InlineResourceEditForm>[0]> = {}) {
  const onCancel = jest.fn();
  const onSuccess = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineResourceEditForm
        spec={spec()}
        data={DATA}
        projectId="prj-1"
        onCancel={onCancel}
        onSuccess={onSuccess}
        {...over}
      />
    </QueryClientProvider>,
  );
  return { onCancel, onSuccess };
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const field = (value: string) => screen.getByDisplayValue(value);
const body = () => update.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  operation = undefined;
  update.mockResolvedValue({});
});

describe("InlineResourceEditForm", () => {
  it("ресурс без схемы формы честно говорит, что формы нет", () => {
    show({ spec: spec({ fields: undefined }) });

    expect(screen.getByRole("alert")).toHaveTextContent("нет form-schema");
  });

  it("форма открыта на текущих значениях ресурса", () => {
    show();

    expect(field("frontend")).toBeInTheDocument();
    expect(field("было")).toBeInTheDocument();
  });

  it("неизменяемое поле показано только для чтения, а не обещает правку", () => {
    show();

    expect(screen.getByTitle("Неизменяемо после создания")).toBeInTheDocument();
    expect(field("10.0.0.0/16")).toBeDisabled();
  });

  it("сохранение без единой правки на край не идёт — маска пуста", () => {
    const { onCancel } = show();

    save();

    expect(update).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("в маску попадает ровно тронутое поле, а соседнее не переписывается", async () => {
    show();

    fireEvent.change(field("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalledWith("/vpc/v1/networks/net-1", expect.anything()));
    expect(body().update_mask).toBe("description");
    expect(body()).not.toHaveProperty("name");
  });

  it("тело правки не тащит поля, которых в сообщении правки нет", async () => {
    show();

    fireEvent.change(field("frontend"), { target: { value: "backend" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(body()).not.toHaveProperty("id");
    expect(body()).not.toHaveProperty("cidr");
  });

  it("пока операция не завершилась, форма занята и не закрывается", async () => {
    update.mockResolvedValue({ operation: { id: "opr-1", done: false } });
    const { onCancel } = show();

    fireEvent.change(field("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(screen.getByRole("button", { name: "Сохранить" })).toBeDisabled());
    expect(onCancel).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  // Пара, а не одно утверждение: «ответ без операции» значит разное у ресурса,
  // который её обещал, и у того, который не обещал, — и решает это объявление
  // ресурса, а не форма.
  it("синхронный ответ закрывает форму у ресурса, который операции не обещал", async () => {
    const { onCancel, onSuccess } = show({ spec: spec({ mutationsReturnOperation: false }) });

    fireEvent.change(field("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1));
    expect(invalidate).toHaveBeenCalledWith("networks", "prj-1");
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(toastError).not.toHaveBeenCalled();
  });

  it("ответ без операции у ресурса, который её обещал, — отказ, а не успех", async () => {
    const { onCancel, onSuccess } = show();

    fireEvent.change(field("было"), { target: { value: "стало" } });
    save();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "Сохранить сеть: сервер не вернул операцию — подтвердить выполнение невозможно",
      ),
    );
    expect(onCancel).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("отказ края показан текстом сервера, без кода протокола, форма остаётся открытой", async () => {
    update.mockRejectedValue(new ApiError(400, 3, null, "name is too long"));
    const { onCancel } = show();

    fireEvent.change(field("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Сохранить сеть: name is too long"));
    expect(onCancel).not.toHaveBeenCalled();
  });
});
