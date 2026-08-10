// Правка сетевого интерфейса. Подсеть у него неизменяема после создания —
// форма обязана показать её и НЕ отправлять; всё остальное едет строго по
// маске, иначе правка имени переписала бы набор адресов и групп тем, что форма
// случайно загрузила.
//
// Отдельный предмет — набор ссылок: он сравнивается БЕЗ учёта порядка. Порядок
// приходит с края и меняется сам по себе; считая его правкой, форма отправляла
// бы запрос на каждое открытие и «сохраняла» бы то, чего никто не менял.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const get = jest.fn<(path: string) => Promise<Record<string, unknown>>>();
const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();
const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { get, list, update, create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: jest.fn(), success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: () => ({ data: undefined }),
}));

const { InlineNetworkInterfaceEditForm } = await import("./InlineNetworkInterfaceEditForm");

const NIC = {
  id: "nic-1",
  name: "eth0",
  description: "было",
  labels: { env: "prod" },
  subnet_id: "sub-1",
  v4_address_ids: ["adr-a", "adr-b"],
  v6_address_ids: [],
  security_group_ids: ["sg-1"],
};

function show(nic: Record<string, unknown> = NIC) {
  const onCancel = jest.fn();
  get.mockResolvedValue({ ...nic });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineNetworkInterfaceEditForm projectId="prj-1" nicId="nic-1" onCancel={onCancel} />
    </QueryClientProvider>,
  );
  return { onCancel };
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const body = () => update.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  list.mockResolvedValue({});
  update.mockResolvedValue({});
});

describe("InlineNetworkInterfaceEditForm", () => {
  it("до загрузки интерфейса формы нет, а есть слово о загрузке", () => {
    get.mockReturnValue(new Promise(() => {}));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <InlineNetworkInterfaceEditForm projectId="prj-1" nicId="nic-1" onCancel={jest.fn()} />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Загрузка…")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Сохранить" })).not.toBeInTheDocument();
  });

  it("форма открыта на текущих значениях", async () => {
    show();

    expect(await screen.findByDisplayValue("eth0")).toBeInTheDocument();
    expect(screen.getByDisplayValue("было")).toBeInTheDocument();
  });

  it("сохранение без правок на край не идёт", async () => {
    const { onCancel } = show();

    await screen.findByDisplayValue("eth0");
    save();

    expect(update).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("в маску попадает ровно тронутое поле, подсеть в неё не входит", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("eth0"), { target: { value: "eth1" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalledWith("/vpc/v1/networkInterfaces/nic-1", expect.anything()));
    expect(body().update_mask).toBe("name");
    expect(body()).not.toHaveProperty("subnet_id");
  });

  it("порядок ссылок правкой не считается", async () => {
    // Край волен вернуть тот же набор в другом порядке; принимая это за
    // правку, форма слала бы запрос на каждое открытие.
    show({ ...NIC, v4_address_ids: ["adr-b", "adr-a"] });

    await screen.findByDisplayValue("eth0");
    save();

    expect(update).not.toHaveBeenCalled();
  });

  it("подсеть показана и заперта — она неизменяема после создания", async () => {
    show();

    const subnet = (await screen.findByDisplayValue("sub-1")) as HTMLInputElement;
    expect(subnet.disabled).toBe(true);
  });
});
