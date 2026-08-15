// Правка пула адресов. Диапазоны у пула правятся ОТДЕЛЬНЫМИ глаголами и в
// сообщение правки не входят вовсе — попади они в маску, край отверг бы весь
// запрос. Тип и зона неизменяемы. Плюс общее для всех правок: маска несёт
// только тронутое, а форма без изменений на край не ходит.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const get = jest.fn<(path: string) => Promise<Record<string, unknown>>>();
const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();
const invalidate = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  // `list` возвращает промис по контракту порта, но ждать заменителю нечего —
  // `Promise.resolve` говорит это прямо, `async` без `await` обещало ожидание.
  api: {
    get,
    update,
    list: jest.fn(() => Promise.resolve({})),
    create: jest.fn(),
    delete: jest.fn(),
    action: jest.fn(),
    post: jest.fn(),
  },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: () => ({ data: undefined }),
}));

const { InlineAddressPoolEditForm } = await import("./InlineAddressPoolEditForm");

const POOL = {
  id: "ap-1",
  name: "pool-a",
  description: "было",
  kind: "EXTERNAL_PUBLIC",
  zone_id: "",
  v4_cidr_blocks: ["198.51.100.0/24"],
  v6_cidr_blocks: [],
  is_default: false,
  selector_priority: 0,
};

function show() {
  const onCancel = jest.fn();
  const onSuccess = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineAddressPoolEditForm poolId="ap-1" onCancel={onCancel} onSuccess={onSuccess} />
    </QueryClientProvider>,
  );
  return { onCancel, onSuccess };
}

function underLabel(label: string | RegExp): HTMLElement {
  let el: HTMLElement | null = screen.getByText(label);
  while (el && !el.querySelector("input, textarea, select")) el = el.parentElement;
  if (!el) throw new Error(`контрол под подписью «${String(label)}» не найден`);
  return el;
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const body = () => update.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  get.mockResolvedValue({ ...POOL });
  update.mockResolvedValue({});
});

describe("InlineAddressPoolEditForm", () => {
  it("до загрузки пула форму не показывает, а говорит о загрузке", () => {
    get.mockReturnValue(new Promise(() => {}));
    show();

    expect(screen.getByText("Загрузка…")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Сохранить" })).not.toBeInTheDocument();
  });

  it("форма открыта на текущих значениях пула", async () => {
    show();

    expect(await screen.findByDisplayValue("pool-a")).toBeInTheDocument();
    expect(screen.getByDisplayValue("было")).toBeInTheDocument();
  });

  it("тип и зона показаны, но править их нечем", async () => {
    show();

    await screen.findByDisplayValue("pool-a");
    expect(within(underLabel("Тип")).getByRole("combobox")).toBeDisabled();
    expect(within(underLabel("Зона")).getByRole("textbox")).toBeDisabled();
  });

  it("глобальный пул назван глобальным, а не пустой зоной", async () => {
    show();

    await screen.findByDisplayValue("pool-a");
    expect(within(underLabel("Зона")).getByRole("textbox")).toHaveValue("(глобальный)");
  });

  it("сохранение без правок на край не идёт", async () => {
    const { onCancel } = show();

    await screen.findByDisplayValue("pool-a");
    save();

    expect(update).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("в маску попадает ровно тронутое поле", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1", expect.anything()));
    expect(body().update_mask).toBe("description");
  });

  it("диапазоны в сообщение правки не уезжают — у них свои глаголы", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(body()).not.toHaveProperty("v4_cidr_blocks");
    expect(body()).not.toHaveProperty("v6_cidr_blocks");
    expect(String(body().update_mask)).not.toContain("cidr");
  });

  it("маска называет поля так, как их читает край", async () => {
    show();

    await screen.findByDisplayValue("pool-a");
    // Переключатель — переключатель, а не поле ввода: общий заменитель раньше
    // рисовал его текстовым полем и отдавал в обработчик СОБЫТИЕ вместо нового
    // состояния, поэтому «выключить» на нём не срабатывало ни разу.
    fireEvent.click(within(underLabel("Default")).getByRole("switch"));
    fireEvent.change(within(underLabel("Selector priority")).getByRole("textbox"), { target: { value: "5" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(String(body().update_mask)).toContain("selectorPriority");
    expect(String(body().update_mask)).not.toContain("selector_priority");
  });

  it("успех обновляет список и закрывает форму", async () => {
    const { onCancel, onSuccess } = show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Пул адресов pool-a обновлён"));
    expect(invalidate).toHaveBeenCalled();
    expect(onSuccess).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("отказ края показан текстом сервера, без кода протокола, форма остаётся открытой", async () => {
    update.mockRejectedValue(new ApiError(409, 9, null, "default pool already exists"));
    const { onCancel } = show();

    fireEvent.change(await screen.findByDisplayValue("было"), { target: { value: "стало" } });
    save();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Сохранить пул адресов: default pool already exists"));
    expect(onCancel).not.toHaveBeenCalled();
  });
});
