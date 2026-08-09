// Диапазоны пула адресов правятся двумя отдельными глаголами (правка поля их не
// меняет вовсе), и каждый уходит на край сразу. Проверка на клиенте — то
// единственное, что отделяет опечатку от отказа; отказ края («в блоке есть
// выданные адреса») обязан быть показан, а не проглочен.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const post = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { post, get: jest.fn(), list: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { AddressPoolCidrManager } = await import("./AddressPoolCidrManager");

function show(v4: string[] = [], v6: string[] = []) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AddressPoolCidrManager poolId="ap-1" v4Blocks={v4} v6Blocks={v6} />
    </QueryClientProvider>,
  );
}

/** Карточка семейства — ближайший предок подписи, у которого есть своё поле ввода. */
function family(label: string): HTMLElement {
  let el: HTMLElement | null = screen.getByText(label);
  while (el && !el.querySelector("input")) el = el.parentElement;
  if (!el) throw new Error(`карточка «${label}» не найдена`);
  return el;
}

const v4card = () => family("IPv4 CIDR blocks");
const v6card = () => family("IPv6 CIDR blocks");
const addBtn = (card: HTMLElement) => within(card).getByRole("button", { name: /Add/ });

function type(card: HTMLElement, value: string) {
  fireEvent.change(within(card).getByRole("textbox"), { target: { value } });
}

beforeEach(() => {
  jest.clearAllMocks();
  post.mockResolvedValue({});
});

describe("AddressPoolCidrManager", () => {
  it("пустое семейство названо пустым", () => {
    show();

    expect(screen.getAllByText("— пусто —")).toHaveLength(2);
  });

  it("показывает диапазоны и их число по семействам", () => {
    show(["198.51.100.0/24"], ["2001:db8::/64"]);

    expect(screen.getByText("198.51.100.0/24")).toBeInTheDocument();
    expect(screen.getByText("2001:db8::/64")).toBeInTheDocument();
    expect(within(v4card()).getByText("1 блок(ов)")).toBeInTheDocument();
  });

  it("на пустом вводе добавить нечем", () => {
    show();

    expect(addBtn(v4card())).toBeDisabled();
  });

  it("добавление уходит своим глаголом и несёт id пула в теле", async () => {
    show();

    type(v4card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v4card()));

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:addCidrBlocks", {
        address_pool_id: "ap-1",
        v4_cidr_blocks: ["198.51.100.0/24"],
      }),
    );
  });

  it("шестёрка уходит в своё поле", async () => {
    show();

    type(v6card(), "2001:db8::/64");
    fireEvent.click(addBtn(v6card()));

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:addCidrBlocks", {
        address_pool_id: "ap-1",
        v6_cidr_blocks: ["2001:db8::/64"],
      }),
    );
  });

  it("блок без префикса на край не уходит", () => {
    show();

    type(v4card(), "198.51.100.0");
    fireEvent.click(addBtn(v4card()));

    expect(toastError).toHaveBeenCalledWith("CIDR должен содержать префикс (например /24).");
    expect(post).not.toHaveBeenCalled();
  });

  it("в шестёрочное семейство четвёрка не проходит", () => {
    show();

    type(v6card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v6card()));

    expect(toastError).toHaveBeenCalledWith("Похоже не на IPv6-адрес.");
    expect(post).not.toHaveBeenCalled();
  });

  it("дубликат не уходит на край молча", () => {
    show(["198.51.100.0/24"]);

    type(v4card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v4card()));

    expect(toastError).toHaveBeenCalledWith("Этот CIDR уже добавлен.");
    expect(post).not.toHaveBeenCalled();
  });

  it("крестик снимает именно тот диапазон, на котором нажали", async () => {
    show(["198.51.100.0/24", "203.0.113.0/24"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "close" })[1]);

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:removeCidrBlocks", {
        address_pool_id: "ap-1",
        v4_cidr_blocks: ["203.0.113.0/24"],
      }),
    );
  });

  it("отказ края показывается кодом и текстом", async () => {
    post.mockRejectedValue(new ApiError(400, "FAILED_PRECONDITION", null, "CIDR has allocated addresses"));
    show(["198.51.100.0/24"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "close" })[0]);

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("IPv4 CIDR удаление: FAILED_PRECONDITION: CIDR has allocated addresses"),
    );
  });
});
