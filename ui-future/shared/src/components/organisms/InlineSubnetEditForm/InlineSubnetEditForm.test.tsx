// Правка подсети. Размещение и диапазоны у подсети неизменяемы после создания
// (первые — вовсе, дополнительные диапазоны — отдельными глаголами), поэтому
// форма обязана: показать размещение и НЕ отправлять его, а в маску класть
// ровно тронутое. Попади неизменяемое в тело — край отверг бы правку имени
// целиком, и пользователь получил бы отказ на действие, которого не совершал.
//
// Отдельно проверяется выбор таблицы маршрутов: он обязан предлагать только
// таблицы СВОЕЙ сети. Чужая таблица в списке — приглашение отправить заведомо
// отвергаемое значение.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { formDividers } from "@shared/test/form-divider";
import { ApiError } from "@shared/api/client";

const get = jest.fn<(path: string) => Promise<Record<string, unknown>>>();
const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();
const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const invalidate = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { get, list, update, create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: jest.fn(), success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: () => ({ data: undefined }),
}));

const { InlineSubnetEditForm } = await import("./InlineSubnetEditForm");

const ZONAL = {
  id: "sub-1",
  name: "web",
  description: "было",
  labels: { env: "prod" },
  network_id: "net-1",
  zone_id: "ru-central1-a",
  region_id: "ru-central1",
  placement_type: "ZONAL",
  v4_cidr_block: "10.0.1.0/24",
  route_table_id: "",
};

function show(subnet: Record<string, unknown> = ZONAL) {
  const onCancel = jest.fn();
  get.mockResolvedValue({ ...subnet });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineSubnetEditForm projectId="prj-1" subnetId="sub-1" onCancel={onCancel} />
    </QueryClientProvider>,
  );
  return { onCancel };
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const body = () => update.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  list.mockResolvedValue({ route_tables: [] });
  update.mockResolvedValue({});
});

describe("InlineSubnetEditForm", () => {
  it("до загрузки подсети формы нет, а есть слово о загрузке", () => {
    get.mockReturnValue(new Promise(() => {}));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <InlineSubnetEditForm projectId="prj-1" subnetId="sub-1" onCancel={jest.fn()} />
      </QueryClientProvider>,
    );

    expect(screen.getByText("Загрузка…")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Сохранить" })).not.toBeInTheDocument();
  });

  it("форма открыта на текущих значениях подсети", async () => {
    show();

    expect(await screen.findByDisplayValue("web")).toBeInTheDocument();
    expect(screen.getByDisplayValue("было")).toBeInTheDocument();
  });

  it("зона показана и заперта — размещение после создания не меняется", async () => {
    show();

    const zone = await screen.findByDisplayValue<HTMLInputElement>("ru-central1-a");
    expect(zone.disabled).toBe(true);
    expect(screen.getByText("Зона доступности")).toBeInTheDocument();
  });

  it("у региональной подсети показан регион, а не пустая зона", async () => {
    // Эникаст: зоны у такой подсети нет by construction, и подпись обязана
    // говорить о регионе — иначе поле выглядит незаполненным.
    show({ ...ZONAL, zone_id: "", placement_type: "REGIONAL" });

    const region = await screen.findByDisplayValue<HTMLInputElement>("ru-central1");
    expect(region.disabled).toBe(true);
    expect(screen.getByText("Регион")).toBeInTheDocument();
    expect(screen.queryByText("Зона доступности")).not.toBeInTheDocument();
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

    await waitFor(() => expect(update).toHaveBeenCalledWith("/vpc/v1/subnets/sub-1", expect.anything()));
    expect(body().update_mask).toBe("description");
  });

  it("неизменяемое в маску не уезжает даже при правке имени", async () => {
    show();

    fireEvent.change(await screen.findByDisplayValue("web"), { target: { value: "web-2" } });
    save();

    await waitFor(() => expect(update).toHaveBeenCalled());
    const mask = String(body().update_mask);
    expect(mask).toBe("name");
    for (const immutable of ["zoneId", "zone_id", "placementType", "networkId", "v4CidrBlock"]) {
      expect(mask).not.toContain(immutable);
    }
    expect(body()).not.toHaveProperty("zone_id");
    expect(body()).not.toHaveProperty("network_id");
  });

  it("предлагаются таблицы маршрутов только своей сети", async () => {
    list.mockResolvedValue({
      route_tables: [
        { id: "rtb-own", name: "своя", network_id: "net-1" },
        { id: "rtb-alien", name: "чужая", network_id: "net-2" },
      ],
    });
    show();

    await screen.findByDisplayValue("web");
    await waitFor(() => expect(screen.getByText("своя")).toBeInTheDocument());
    // Чужая таблица — заведомо отвергаемое значение: показывать её значит
    // предлагать отказ.
    expect(screen.queryByText("чужая")).not.toBeInTheDocument();
  });
});

describe("InlineSubnetEditForm — черта", () => {
  // ПОРЯДОК ПОЛЕЙ ОДИН НА ВСЕ ФОРМЫ (решение владельца): общие поля, черта,
  // поля самого ресурса. Рукописная форма подчиняется тому же порядку, что и
  // общее тело формы, — иначе две соседние формы читаются как два разных места
  // продукта (канон консоли, правило 9).
  //
  // Утверждается МЕСТО черты, а не её наличие: черта, уехавшая в конец формы,
  // тоже «есть» и при этом ничего не отделяет.
  it("стоит между «Метки» и «Таблица маршрутов»", async () => {
    show();
    await screen.findByText("Таблица маршрутов");

    const [divider] = formDividers();
    expect(divider).toBeDefined();

    const position = (el: Element) => [...document.body.querySelectorAll("*")].indexOf(el);
    expect(position(screen.getByText("Метки"))).toBeLessThan(position(divider));
    expect(position(divider)).toBeLessThan(position(screen.getByText("Таблица маршрутов")));
  });
});
