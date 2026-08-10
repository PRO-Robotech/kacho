// Правка метаданных группы безопасности. Правила у неё правятся ОТДЕЛЬНОЙ
// панелью (по одному, своим глаголом), поэтому форма обязана не отправлять их
// вовсе: попади они в тело или маску — правка имени переписала бы весь набор
// правил тем, что форма случайно загрузила. Остальное — общая дисциплина
// маски: без правок запроса нет, в маске только тронутое.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const get = jest.fn<(path: string) => Promise<Record<string, unknown>>>();
const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const invalidate = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  // `list` возвращает промис по контракту порта, но ждать заменителю нечего —
  // `Promise.resolve` говорит это прямо, `async` без `await` обещало ожидание.
  api: { get, update, list: jest.fn(() => Promise.resolve({})), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: () => ({ data: undefined }),
}));

const { InlineSecurityGroupEditForm } = await import("./InlineSecurityGroupEditForm");

const SG = {
  id: "sg-1",
  name: "web",
  description: "было",
  labels: { env: "prod" },
  rules: [{ id: "sgr-1", direction: "INGRESS", protocol_name: "TCP" }],
};

function show() {
  const onCancel = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineSecurityGroupEditForm projectId="prj-1" sgId="sg-1" onCancel={onCancel} />
    </QueryClientProvider>,
  );
  return { onCancel };
}


const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const body = () => update.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  get.mockResolvedValue({ ...SG });
  update.mockResolvedValue({});
});

describe("InlineSecurityGroupEditForm", () => {
  it("до загрузки группы форму не показывает, а говорит о загрузке", () => {
    get.mockReturnValue(new Promise(() => {}));
    show();

    expect(screen.getByText("Загрузка…")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Сохранить" })).not.toBeInTheDocument();
  });

  it("форма открыта на текущих значениях группы", async () => {
    show();

    expect(await screen.findByDisplayValue("web")).toBeInTheDocument();
    expect(screen.getByDisplayValue("было")).toBeInTheDocument();
  });

  it("правил в форме нет — ими правит отдельная панель", async () => {
    show();

    await screen.findByDisplayValue("web");
    expect(screen.queryByText("Правила")).not.toBeInTheDocument();
    expect(screen.queryByText("INGRESS")).not.toBeInTheDocument();
  });

  it("сохранение без правок на край не идёт", async () => {
    const { onCancel } = show();

    await screen.findByDisplayValue("web");
    save();

    expect(update).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("в маску попадает ровно тронутое поле", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalledWith("/vpc/v1/securityGroups/sg-1", expect.anything()));
    expect(body().update_mask).toBe("description");
  });

  it("правила в тело не уезжают — иначе правка имени переписала бы их набор", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("web"), { target: { value: "web-2" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(body()).not.toHaveProperty("rules");
    expect(String(body().update_mask)).not.toContain("rules");
    expect(body().update_mask).toBe("name");
  });

  it("успех обновляет список и закрывает форму", async () => {
    const { onCancel } = show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(invalidate).toHaveBeenCalledWith("security-groups", "prj-1"));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("отказ края показан кодом и текстом, форма остаётся открытой", async () => {
    update.mockRejectedValue(new ApiError(409, "ALREADY_EXISTS", null, "security group name taken"));
    const { onCancel } = show();

    fireEvent.change(await screen.findByDisplayValue("web"), { target: { value: "web-2" } });
    save();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("Сохранить группу безопасности: ALREADY_EXISTS: security group name taken"),
    );
    expect(onCancel).not.toHaveBeenCalled();
  });

});
