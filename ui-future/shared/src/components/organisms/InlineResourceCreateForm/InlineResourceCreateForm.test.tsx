// Встраиваемая форма создания. Её предмет — ЧТО уходит на край и когда
// закрывается окно: поле формы, которого нет в сообщении создания, край
// выбрасывает молча, а закрытие раньше завершения операции показывает успех
// там, где его ещё нет (и скрывает отказ, если он придёт).

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { FormField } from "@shared/lib/form-schema";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { Operation } from "@shared/api/types";

const create = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();
const invalidate = jest.fn();
let operation: Operation | undefined;

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { create, get: jest.fn(), list: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: (id: string | null) => ({ data: id ? operation : undefined }),
}));

const { InlineResourceCreateForm } = await import("./InlineResourceCreateForm");

const FIELDS: FormField[] = [
  { type: "string", name: "name", label: "Имя" } as FormField,
  { type: "string", name: "description", label: "Описание" } as FormField,
  { type: "string", name: "network_id", label: "Сеть" } as FormField,
];

function spec(over: Partial<ResourceSpec> = {}): ResourceSpec {
  return {
    id: "subnets",
    route: "subnets",
    apiPath: "/vpc/v1/subnets",
    payloadKey: "subnets",
    singular: "Подсеть",
    plural: "Подсети",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: FIELDS,
    template: () => ({}),
    ...over,
  } as unknown as ResourceSpec;
}

function show(over: Partial<Parameters<typeof InlineResourceCreateForm>[0]> = {}) {
  const onCancel = jest.fn();
  const onSuccess = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineResourceCreateForm
        spec={spec()}
        ctx={{ projectId: "prj-1" }}
        projectId="prj-1"
        onCancel={onCancel}
        onSuccess={onSuccess}
        {...over}
      />
    </QueryClientProvider>,
  );
  return { onCancel, onSuccess };
}

const submit = () => fireEvent.click(screen.getByRole("button", { name: /Создать подсеть/ }));
const body = () => create.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  operation = undefined;
  create.mockResolvedValue({});
});

describe("InlineResourceCreateForm", () => {
  it("ресурс без схемы формы честно говорит, что формы нет, и создавать нечем", () => {
    show({ spec: spec({ fields: undefined }) });

    expect(screen.getByRole("alert")).toHaveTextContent("нет form-schema");
    expect(screen.queryByRole("button", { name: /Создать/ })).not.toBeInTheDocument();
  });

  it("имя подставляется само — безымянный повтор упёрся бы в занятое имя", () => {
    show();

    expect(screen.getByDisplayValue(/^subnets-\d{6}$/)).toBeInTheDocument();
  });

  it("контекстное поле показано только для чтения и уезжает в тело как задано", async () => {
    show({ presetFields: { network_id: "net-7" } });

    expect(screen.getByTitle("Задано из контекста")).toBeInTheDocument();
    submit();

    await waitFor(() => expect(create).toHaveBeenCalledWith("/vpc/v1/subnets", expect.anything()));
    expect(body().network_id).toBe("net-7");
  });

  it("предустановленное редактируемое поле остаётся вводимым", () => {
    show({ editablePresetFields: { description: "по умолчанию" } });

    const input = screen.getByDisplayValue("по умолчанию");
    expect(input).toBeEnabled();
  });

  it("клиентская проверка ресурса останавливает отправку и объясняет причину", () => {
    show({ spec: spec({ validate: () => "нужен хотя бы один диапазон" }) });

    submit();

    expect(toastError).toHaveBeenCalledWith("нужен хотя бы один диапазон");
    expect(create).not.toHaveBeenCalled();
  });

  it("служебные поля виджета в тело не уезжают — край выбросил бы их молча", async () => {
    show({ editablePresetFields: { _address_kind: "internal" } });

    submit();

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(Object.keys(body())).not.toContain("_address_kind");
  });

  it("синхронный ответ без операции сразу закрывает форму и обновляет список", async () => {
    const { onCancel, onSuccess } = show();

    submit();

    await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1));
    expect(invalidate).toHaveBeenCalledWith("subnets", "prj-1");
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("пока операция не завершилась, форма занята и не закрывается", async () => {
    create.mockResolvedValue({ operation: { id: "opr-1", done: false } });
    const { onCancel, onSuccess } = show();

    submit();

    await waitFor(() => expect(screen.getByRole("button", { name: /Создать подсеть/ })).toBeDisabled());
    expect(onCancel).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("отказ края показан кодом и текстом, форма остаётся открытой", async () => {
    create.mockRejectedValue(new ApiError(409, "ALREADY_EXISTS", null, "subnet already exists"));
    const { onCancel } = show();

    submit();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("Создать Подсеть: ALREADY_EXISTS: subnet already exists"),
    );
    expect(onCancel).not.toHaveBeenCalled();
  });
});
