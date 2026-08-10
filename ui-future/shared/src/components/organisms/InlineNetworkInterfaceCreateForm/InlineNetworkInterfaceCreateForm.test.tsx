// Создание сетевого интерфейса. Подсеть у интерфейса — якорь размещения и
// неизменяема после создания, поэтому форма обязана:
//
//  1. не отправлять запрос без подсети и сказать об этом сама — иначе человек
//     получает отказ края на то, что видно на месте;
//  2. держать выбор адресов и групп безопасности ЗАПЕРТЫМ, пока подсеть не
//     выбрана: адрес чужой подсети край отвергнет, а на вид он ничем не хуже;
//  3. отправлять подсеть тем полем, которое несёт контракт, и не досылать
//     ничего сверх набора.
//
// Проверяется наблюдаемое: показанный текст отказа, состояние полей и ТЕЛО
// ушедшего запроса.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();
const create = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, create, get: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: () => ({ data: undefined }),
}));

const { InlineNetworkInterfaceCreateForm } = await import("./InlineNetworkInterfaceCreateForm");

function show(props: Record<string, unknown> = {}) {
  const onCancel = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineNetworkInterfaceCreateForm projectId="prj-1" onCancel={onCancel} {...props} />
    </QueryClientProvider>,
  );
  return { onCancel };
}

const selectShowing = (optionText: string) =>
  [...document.querySelectorAll("select")].find((s) =>
    [...s.options].some((o) => o.textContent === optionText),
  ) as HTMLSelectElement | undefined;

const save = () => fireEvent.click(screen.getByRole("button", { name: "Создать сетевой интерфейс" }));
const body = () => create.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  list.mockImplementation(async (path: string) => {
    if (path.includes("/subnets")) return { subnets: [{ id: "sub-1", name: "внутренняя" }] };
    return {};
  });
  create.mockResolvedValue({});
});

describe("InlineNetworkInterfaceCreateForm", () => {
  it("без подсети запрос не уходит, и человеку сказано почему", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    save();

    expect(toastError).toHaveBeenCalledWith("Выберите подсеть для интерфейса.");
    expect(create).not.toHaveBeenCalled();
  });

  it("выбранная подсеть уходит своим полем", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    fireEvent.change(selectShowing("внутренняя")!, { target: { value: "sub-1" } });
    save();

    await waitFor(() => expect(create).toHaveBeenCalledWith("/vpc/v1/networkInterfaces", expect.anything()));
    expect(body().subnet_id).toBe("sub-1");
    expect(body().project_id).toBe("prj-1");
  });

  it("заданная подсеть заперта — интерфейс не переносят между подсетями", async () => {
    show({ subnetId: "sub-1" });

    expect(selectShowing("Выберите подсеть")!.disabled).toBe(true);
  });

  it("незаданная подсеть выбирается — контроль в обратную сторону", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    expect(selectShowing("внутренняя")!.disabled).toBe(false);
  });

  it("пока подсеть не выбрана, адреса и группы выбрать нельзя и сказано почему", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    // Подсказка объясняет запрет; без неё поле выглядит просто сломанным.
    expect(screen.getAllByText("Сначала выберите подсеть").length).toBeGreaterThanOrEqual(3);
  });

  it("после выбора подсети запрет снимается", async () => {
    // Положительный близнец: без него предыдущее утверждение зеленело бы на
    // форме, где эти поля заперты всегда.
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    fireEvent.change(selectShowing("внутренняя")!, { target: { value: "sub-1" } });

    await waitFor(() => expect(screen.queryByText("Сначала выберите подсеть")).not.toBeInTheDocument());
  });
});
