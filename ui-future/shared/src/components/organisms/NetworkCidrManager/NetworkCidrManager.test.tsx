// Супернет сети меняется не правкой поля, а двумя разными глаголами, и каждое
// действие уходит на край сразу. Поэтому предмет пробы — ЧТО ушло на край:
// перепутанное семейство (v4-блок в поле v6) сервер примет и разложит сеть не
// туда, а проверка на клиенте — единственное, что стоит между опечаткой и
// отказом в конце длинной формы.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const action = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { action, get: jest.fn(), list: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { NetworkCidrManager } = await import("./NetworkCidrManager");

function show(v4: string[] = [], v6: string[] = []) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <NetworkCidrManager networkId="net-1" v4Blocks={v4} v6Blocks={v6} />
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

const v4card = () => family("Супернет IPv4");
const v6card = () => family("Супернет IPv6");

function type(card: HTMLElement, value: string) {
  fireEvent.change(within(card).getByRole("textbox"), { target: { value } });
}

const addBtn = (card: HTMLElement) => within(card).getByRole("button", { name: /Add/ });

beforeEach(() => {
  jest.clearAllMocks();
  action.mockResolvedValue({});
});

describe("NetworkCidrManager", () => {
  it("пустое семейство названо пустым", () => {
    show([], []);

    expect(screen.getAllByText("— пусто —")).toHaveLength(2);
  });

  it("показывает объявленные блоки и их число по семействам", () => {
    show(["10.30.0.0/16"], ["fd00:30::/48", "fd00:31::/48"]);

    expect(screen.getByText("10.30.0.0/16")).toBeInTheDocument();
    expect(screen.getByText("fd00:31::/48")).toBeInTheDocument();
    expect(within(v4card()).getByText("1 блок(ов)")).toBeInTheDocument();
    expect(within(v6card()).getByText("2 блок(ов)")).toBeInTheDocument();
  });

  it("на пустом вводе добавить нечем", () => {
    show();

    expect(addBtn(v4card())).toBeDisabled();
  });

  it("добавление уходит на край глаголом своего семейства", async () => {
    show();

    type(v4card(), "10.30.0.0/16");
    fireEvent.click(addBtn(v4card()));

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/networks/net-1:add-cidr-blocks", {
        ipv4_cidr_blocks: ["10.30.0.0/16"],
      }),
    );
  });

  it("шестёрка уходит в СВОЁ поле, а не в четвёрочное", async () => {
    show();

    type(v6card(), "fd00:30::/48");
    fireEvent.click(addBtn(v6card()));

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/networks/net-1:add-cidr-blocks", {
        ipv6_cidr_blocks: ["fd00:30::/48"],
      }),
    );
  });

  it("Enter добавляет так же, как кнопка", async () => {
    show();

    const input = within(v4card()).getByRole("textbox");
    fireEvent.change(input, { target: { value: "10.30.0.0/16" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
  });

  it("блок без префикса на край не уходит и объясняет отказ", () => {
    show();

    type(v4card(), "10.30.0.0");
    fireEvent.click(addBtn(v4card()));

    expect(toastError).toHaveBeenCalledWith("CIDR должен содержать префикс (например /16).");
    expect(action).not.toHaveBeenCalled();
  });

  it("в шестёрочное семейство четвёрка не проходит", () => {
    show();

    type(v6card(), "10.30.0.0/16");
    fireEvent.click(addBtn(v6card()));

    expect(toastError).toHaveBeenCalledWith("Похоже не на IPv6-адрес.");
    expect(action).not.toHaveBeenCalled();
  });

  it("дубликат не уходит на край молча", () => {
    show(["10.30.0.0/16"]);

    type(v4card(), "10.30.0.0/16");
    fireEvent.click(addBtn(v4card()));

    expect(toastError).toHaveBeenCalledWith("Этот CIDR уже добавлен.");
    expect(action).not.toHaveBeenCalled();
  });

  it("крестик на блоке снимает именно его", async () => {
    show(["10.30.0.0/16", "10.31.0.0/16"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "close" })[1]);

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/networks/net-1:remove-cidr-blocks", {
        ipv4_cidr_blocks: ["10.31.0.0/16"],
      }),
    );
  });

  it("отказ края показывается кодом и текстом, а не проглатывается", async () => {
    action.mockRejectedValue(new ApiError(400, "FAILED_PRECONDITION", null, "network CIDR block still contains subnets"));
    show(["10.30.0.0/16"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "close" })[0]);

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "IPv4 супернет удаление: FAILED_PRECONDITION: network CIDR block still contains subnets",
      ),
    );
  });
});
