// Диапазоны подсети: основной якорь неизменяем после создания, дополнительные
// правятся двумя глаголами. Проба сторожит ровно это разделение — показанная
// кнопка снятия на основном якоре обещала бы операцию, которой край не даёт, а
// потерянный замок сделал бы неизменяемость невидимой.

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

const { CidrSection } = await import("./SubnetCidrManager");

function show(props: { kind?: "v4" | "v6"; blocks?: string[]; primary?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CidrSection subnetId="sub-1" kind={props.kind ?? "v4"} blocks={props.blocks ?? []} primary={props.primary} />
    </QueryClientProvider>,
  );
}

const input = () => screen.getByRole("textbox");
const addBtn = () => screen.getByRole("button", { name: /Добавить/ });
const removeBtns = () => screen.queryAllByRole("button", { name: "Удалить CIDR" });
const type = (v: string) => fireEvent.change(input(), { target: { value: v } });

beforeEach(() => {
  jest.clearAllMocks();
  action.mockResolvedValue({});
});

describe("CidrSection подсети", () => {
  it("пустой набор без якоря прямо назван пустым", () => {
    show();

    expect(screen.getByText("CIDR-блоков нет")).toBeInTheDocument();
  });

  it("основной якорь показан, подписан и снять его нечем", () => {
    show({ primary: "10.0.1.0/24" });

    expect(screen.getByText(/10\.0\.1\.0\/24/)).toBeInTheDocument();
    expect(screen.getByText("основной")).toBeInTheDocument();
    expect(removeBtns()).toHaveLength(0);
    expect(screen.queryByText("CIDR-блоков нет")).not.toBeInTheDocument();
  });

  // Здесь стояла проба «счётчик считает якорь вместе с дополнительными». Её
  // предмета больше нет: решением владельца («отображать кол-во элементов не
  // нужно») счётчик снят из шапки секции вовсе. Проба снята ВМЕСТЕ с предметом,
  // а не ослаблена: утверждение о числе, которого продукт не показывает,
  // зеленело бы вечно и не сторожило бы ничего. Что якорь и дополнительные
  // живут в одном наборе, сторожат соседние пробы — «основной якорь показан» и
  // «снять можно только дополнительные»: обе смотрят на сам набор, а не на
  // подпись над ним.

  it("снять можно только дополнительные — по кнопке на каждом", () => {
    show({ primary: "10.0.1.0/24", blocks: ["10.0.2.0/24", "10.0.3.0/24"] });

    expect(removeBtns()).toHaveLength(2);
  });

  it("на пустом вводе добавить нечем", () => {
    show();

    expect(addBtn()).toBeDisabled();
  });

  it("добавление уходит глаголом своего семейства", async () => {
    show();

    type("10.0.2.0/24");
    fireEvent.click(addBtn());

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/subnets/sub-1:add-cidr-blocks", {
        ipv4_cidr_blocks: ["10.0.2.0/24"],
      }),
    );
  });

  it("шестёрка уходит в своё поле", async () => {
    show({ kind: "v6" });

    type("fd00:1::/64");
    fireEvent.click(addBtn());

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/subnets/sub-1:add-cidr-blocks", {
        ipv6_cidr_blocks: ["fd00:1::/64"],
      }),
    );
  });

  it("Enter добавляет так же, как кнопка", async () => {
    show();

    fireEvent.change(input(), { target: { value: "10.0.2.0/24" } });
    fireEvent.keyDown(input(), { key: "Enter" });

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
  });

  it("блок без префикса на край не уходит, а отказ стоит РЯДОМ С ПОЛЕМ", () => {
    show();

    type("10.0.2.0");
    fireEvent.click(addBtn());

    // Претензия остаётся на экране у поля и называет, чего не хватает.
    // Всплывающее сообщение исчезало бы раньше, чем человек перечитает
    // набранное, — и негодная строка оставалась бы без объяснения.
    expect(screen.getByRole("alert")).toHaveTextContent("CIDR должен содержать префикс (например /24).");
    expect(toastError).not.toHaveBeenCalled();
    expect(action).not.toHaveBeenCalled();
  });

  it("претензия снимается, как только значение исправили", () => {
    // Контроль в обратную сторону: без него «претензия показана» зеленело бы и
    // на реализации, которая показывает её навсегда.
    show();

    type("10.0.2.0");
    fireEvent.click(addBtn());
    expect(screen.queryByRole("alert")).toBeInTheDocument();

    type("10.0.2.0/24");

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("в шестёрочное семейство четвёрка не проходит", () => {
    show({ kind: "v6" });

    type("10.0.2.0/24");
    fireEvent.click(addBtn());

    expect(screen.getByRole("alert")).toHaveTextContent("Похоже не на IPv6-адрес.");
    expect(action).not.toHaveBeenCalled();
  });

  it("дубликат дополнительного блока не уходит на край", () => {
    show({ blocks: ["10.0.2.0/24"] });

    type("10.0.2.0/24");
    fireEvent.click(addBtn());

    expect(screen.getByRole("alert")).toHaveTextContent("Этот CIDR уже добавлен.");
    expect(action).not.toHaveBeenCalled();
  });

  it("снятие уходит тем блоком, на котором нажали", async () => {
    show({ blocks: ["10.0.2.0/24", "10.0.3.0/24"] });

    fireEvent.click(within(screen.getByRole("table")).getAllByRole("button", { name: "Удалить CIDR" })[1]);

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/subnets/sub-1:remove-cidr-blocks", {
        ipv4_cidr_blocks: ["10.0.3.0/24"],
      }),
    );
  });

  it("отказ края показывается текстом сервера, без кода протокола", async () => {
    action.mockRejectedValue(new ApiError(400, 9, null, "subnet has allocated addresses"));
    show({ blocks: ["10.0.2.0/24"] });

    fireEvent.click(removeBtns()[0]);

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("IPv4 CIDR удаление: subnet has allocated addresses"));
  });
});
