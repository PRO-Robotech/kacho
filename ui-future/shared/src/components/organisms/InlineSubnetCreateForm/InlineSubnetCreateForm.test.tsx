// Создание подсети. Предмет — что уходит на край и что форма отвергает САМА.
//
//  1. `placement_type` в теле НЕ отправляется: край выводит его из того, какая
//     из двух координат заполнена. Отправить и координату, и вывод из неё —
//     значит завести второе место об одном предмете, из которых верно одно;
//  2. зона и регион взаимоисключающи. Уехавшие обе — заведомо отвергаемое тело;
//  3. хотя бы один основной диапазон обязателен, и форма говорит об этом сама,
//     не отправляя запрос: иначе человек получает отказ края на то, что видно
//     на месте;
//  4. диапазон без длины префикса отвергается здесь же — «10.20.0.0» выглядит
//     осмысленно и не является диапазоном.
//
// Проверяется наблюдаемое: показанный текст отказа и ТЕЛО ушедшего запроса.

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

const { InlineSubnetCreateForm } = await import("./InlineSubnetCreateForm");

function show(props: Record<string, unknown> = {}) {
  const onCancel = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineSubnetCreateForm projectId="prj-1" onCancel={onCancel} {...props} />
    </QueryClientProvider>,
  );
  return { onCancel };
}

/**
 * Список опознаётся по варианту, который в нём ВИДЕН, — так же, как его
 * опознаёт человек. Ловить его по соседству с подписью значит закреплять
 * раскладку разметки, а не наблюдаемое.
 */
const selectShowing = (optionText: string) =>
  [...document.querySelectorAll("select")].find((s) => [...s.options].some((o) => o.textContent === optionText));

const pick = (optionText: string, value: string) =>
  fireEvent.change(selectShowing(optionText)!, { target: { value } });
const typeIn = (placeholder: string, value: string) =>
  fireEvent.change(screen.getByPlaceholderText(placeholder), { target: { value } });
const save = () => fireEvent.click(screen.getByRole("button", { name: "Создать подсеть" }));
const body = () => create.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  // Порт возвращает промис — заменитель возвращает его же, но ждать ему нечего:
  // ответ известен сразу. `async` без `await` обещало ожидание, которого нет.
  list.mockImplementation((path: string) => {
    if (path.includes("/networks")) return Promise.resolve({ networks: [{ id: "net-1", name: "основная" }] });
    // Каталог размещения отвечает ОДНИМ полем идентичности: подписи у региона и
    // зоны нет (#716). Фикстура, дописывающая её, была бы снисходительнее
    // продукта и прятала бы ровно тот путь, по которому подпись берётся теперь.
    if (path.includes("/zones")) return Promise.resolve({ zones: [{ id: "ru-central1-a" }] });
    if (path.includes("/regions")) return Promise.resolve({ regions: [{ id: "ru-central1" }] });
    return Promise.resolve({ route_tables: [] });
  });
  create.mockResolvedValue({});
});

describe("InlineSubnetCreateForm", () => {
  it("зональная подсеть уходит с зоной и БЕЗ вывода из неё", async () => {
    show({ networkId: "net-1" });

    await waitFor(() => expect(selectShowing("ru-central1-a")).toBeDefined());
    pick("ru-central1-a", "ru-central1-a");
    typeIn("10.20.0.0/24", "10.20.0.0/24");
    save();

    await waitFor(() => expect(create).toHaveBeenCalledWith("/vpc/v1/subnets", expect.anything()));
    expect(body().zone_id).toBe("ru-central1-a");
    expect(body().region_id).toBeUndefined();
    // Край выводит режим размещения сам; присланный вывод стал бы вторым местом
    // об одном предмете.
    expect(body()).not.toHaveProperty("placement_type");
  });

  it("региональная подсеть уходит с регионом и БЕЗ зоны", async () => {
    show({ networkId: "net-1" });

    pick("REGIONAL — во всём регионе", "REGIONAL");
    await waitFor(() => expect(selectShowing("ru-central1")).toBeDefined());
    pick("ru-central1", "ru-central1");
    typeIn("10.20.0.0/24", "10.20.0.0/24");
    save();

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(body().region_id).toBe("ru-central1");
    expect(body().zone_id).toBeUndefined();
    expect(body()).not.toHaveProperty("placement_type");
  });

  it("без основного диапазона запрос не уходит, и человеку сказано почему", async () => {
    show({ networkId: "net-1" });

    await waitFor(() => expect(selectShowing("ru-central1-a")).toBeDefined());
    pick("ru-central1-a", "ru-central1-a");
    save();

    expect(toastError).toHaveBeenCalledWith("Укажите основной CIDR (IPv4 или IPv6).");
    expect(create).not.toHaveBeenCalled();
  });

  it("диапазон без длины префикса отвергается на месте", async () => {
    show({ networkId: "net-1" });

    await waitFor(() => expect(selectShowing("ru-central1-a")).toBeDefined());
    pick("ru-central1-a", "ru-central1-a");
    typeIn("10.20.0.0/24", "10.20.0.0");
    save();

    expect(toastError).toHaveBeenCalledWith(
      "Основной IPv4 CIDR должен содержать префикс, например 10.20.0.0/24.",
    );
    expect(create).not.toHaveBeenCalled();
  });

  it("без зоны зональная подсеть не отправляется", () => {
    show({ networkId: "net-1" });

    typeIn("10.20.0.0/24", "10.20.0.0/24");
    save();

    expect(toastError).toHaveBeenCalledWith("Выберите зону доступности.");
    expect(create).not.toHaveBeenCalled();
  });

  it("без сети подсеть не отправляется", () => {
    // Положительный контроль отрицаний: сеть выбирается, и тогда всё уходит
    // (первый кейс). Здесь — её отсутствие.
    show();

    typeIn("10.20.0.0/24", "10.20.0.0/24");
    save();

    expect(toastError).toHaveBeenCalledWith("Выберите сеть для подсети.");
    expect(create).not.toHaveBeenCalled();
  });

  it("заданная сеть заперта, незаданная — выбирается", () => {
    // Пара, а не одиночное отрицание: «заперто» тривиально выполнялось бы и на
    // форме, где список сетей вообще не отрисован.
    show({ networkId: "net-1" });
    expect(selectShowing("Выберите сеть")!.disabled).toBe(true);

  });

  it("без заданной сети список сетей открыт и наполнен", async () => {
    show();

    await waitFor(() => expect(selectShowing("основная")).toBeDefined());
    expect(selectShowing("основная")!.disabled).toBe(false);
  });
});
