// Панель статических маршрутов правит их целиком: сохранение ЗАМЕНЯЕТ весь
// список, поэтому в теле уезжают и те строки, которых оператор не касался.
// Предмет пробы — что показано в режиме чтения, что уходит на край при
// сохранении и что отмена действительно отменяет.
//
// Чистый круг «загрузили → сохранили» для ветви шлюза проверяет соседний
// RoutesPanel.routes.test.ts; здесь — наблюдаемая часть.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { RoutesPanel } = await import("./RoutesPanel");

type Route = { destination_prefix?: string; next_hop_address?: string; gateway_id?: string };

function show(routes: Route[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RoutesPanel routeTableId="rt-1" projectId="prj-1" routes={routes} />
    </QueryClientProvider>,
  );
}

const startEdit = () => fireEvent.click(screen.getByRole("button", { name: /Редактировать/ }));
const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const cancel = () => fireEvent.click(screen.getByRole("button", { name: "Отменить" }));
const rowInputs = () => screen.getAllByRole("textbox");

beforeEach(() => {
  jest.clearAllMocks();
  update.mockResolvedValue({});
});

describe("RoutesPanel — режим чтения", () => {
  it("пустой список объясняет, как добавить маршрут, и таблицы не рисует", () => {
    show([]);

    expect(screen.getByText("Статических маршрутов нет — нажмите «Редактировать», чтобы добавить.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("показывает маршрут адресом следующего узла", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    expect(screen.getByText("10.0.0.0/8")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
  });

  it("шлюзовой маршрут показан шлюзом, а не пустой клеткой", () => {
    show([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);

    expect(screen.getByText("gtw-1")).toBeInTheDocument();
  });

  it("счётчик в подписи считает показанные маршруты", () => {
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
    ]);

    expect(screen.getByText("(2)")).toBeInTheDocument();
  });

  it("до перехода в правку полей ввода нет", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
  });
});

describe("RoutesPanel — правка", () => {
  it("правка открывает поля с текущими значениями", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();

    expect(rowInputs()[0]).toHaveValue("10.0.0.0/8");
    expect(rowInputs()[1]).toHaveValue("10.0.0.1");
  });

  it("строка со шлюзом объясняет пустое поле адреса, а не выглядит маршрутом без узла", () => {
    show([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);

    startEdit();

    expect(rowInputs()[1]).toHaveAttribute("placeholder", "шлюз gtw-1 — введите адрес, чтобы заменить");
    expect(rowInputs()[1]).toHaveValue("");
  });

  it("правка пустого списка сразу даёт куда писать", () => {
    show([]);

    startEdit();

    expect(rowInputs()).toHaveLength(2);
    expect(screen.getByText("(1)")).toBeInTheDocument();
  });

  it("добавленная строка появляется пустой и увеличивает счётчик", () => {
    show([]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));

    expect(rowInputs()).toHaveLength(4);
    expect(rowInputs()[2]).toHaveValue("");
    expect(screen.getByText("(2)")).toBeInTheDocument();
  });

  it("сохранение уходит полной заменой списка и маской своего поля", async () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "10.0.0.2" } });
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.2" }],
        update_mask: "staticRoutes",
      }),
    );
  });

  it("недописанная строка на край не уезжает", async () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));
    save();

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
      static_routes: [{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }],
      update_mask: "staticRoutes",
    });
  });

  it("снятая строка в сохранение не попадает", async () => {
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" },
    ]);

    startEdit();
    fireEvent.click(within(screen.getByRole("table")).getAllByRole("button", { name: "Удалить маршрут" })[0]);
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [{ destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" }],
        update_mask: "staticRoutes",
      }),
    );
  });

  it("отмена возвращает показанное и на край ничего не шлёт", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "10.0.0.9" } });
    cancel();

    expect(update).not.toHaveBeenCalled();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
    expect(screen.queryByText("10.0.0.9")).not.toBeInTheDocument();
  });

  it("отказ края показан кодом и текстом, правка не теряется", async () => {
    update.mockRejectedValue(new ApiError(400, "INVALID_ARGUMENT", null, "destination_prefix is not a CIDR"));
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    save();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("Статические маршруты: INVALID_ARGUMENT: destination_prefix is not a CIDR"),
    );
    expect(screen.getByRole("button", { name: "Сохранить" })).toBeInTheDocument();
  });
});
