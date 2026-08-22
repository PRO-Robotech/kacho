// Адресное пространство сети (в интерфейсе — «CIDR», тем же словом, что у
// подсети) меняется не правкой поля, а двумя разными глаголами, и каждое
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

/** Секция семейства — ближайший предок заголовка, у которого есть своё поле ввода.
 *
 *  Семейство стоит В ЗАГОЛОВКЕ («IPv4 CIDR» / «IPv6 CIDR»), а не отдельным
 *  бейджем-плиткой: плитка ушла вместе со своей шапкой, и пока семейство не
 *  переехало в заголовок, обе секции назывались одинаково — «CIDR» и «CIDR», —
 *  то есть не различались вовсе. Поэтому поиск идёт по НАЧАЛУ заголовка, а не
 *  по точному совпадению со словом. */
function family(badge: string): HTMLElement {
  let el: HTMLElement | null = screen.getByText(new RegExp(`^${badge}\\b`));
  while (el && !el.querySelector("input")) el = el.parentElement;
  if (!el) throw new Error(`секция «${badge}» не найдена`);
  return el;
}

const v4card = () => family("IPv4");
const v6card = () => family("IPv6");

function type(card: HTMLElement, value: string) {
  fireEvent.change(within(card).getByRole("textbox"), { target: { value } });
}

const addBtn = (card: HTMLElement) => within(card).getByRole("button", { name: /Добавить/ });

beforeEach(() => {
  jest.clearAllMocks();
  action.mockResolvedValue({});
});

describe("NetworkCidrManager", () => {
  it("пустое семейство названо пустым", () => {
    show([], []);

    expect(screen.getAllByText("CIDR-блоков нет")).toHaveLength(2);
  });

  it("показывает объявленные блоки — каждый в СВОЁМ семействе", () => {
    // Числа блоков здесь больше не утверждаются: счётчик снят из шапки секции
    // решением владельца («отображать кол-во элементов не нужно»). Утверждение
    // «по семействам» при этом не потеряно, а усилено: прежде оно опиралось на
    // счётчик рядом с бейджем, теперь блок ищется ВНУТРИ своей секции — то
    // есть проверяется принадлежность самого значения, а не подпись над ним.
    show(["10.30.0.0/16"], ["fd00:30::/48", "fd00:31::/48"]);

    expect(within(v4card()).getByText("10.30.0.0/16")).toBeInTheDocument();
    expect(within(v6card()).getByText("fd00:30::/48")).toBeInTheDocument();
    expect(within(v6card()).getByText("fd00:31::/48")).toBeInTheDocument();
    // Контроль в обратную сторону: шестёрка не показана в секции четвёрки —
    // без него «блок внутри своей секции» зеленело бы и на реализации,
    // рисующей оба семейства в одной.
    expect(within(v4card()).queryByText("fd00:30::/48")).not.toBeInTheDocument();
  });

  it("семейство названо В ЗАГОЛОВКЕ — иначе две секции подряд неразличимы", () => {
    // Семейство переехало из плитки слева в заголовок (решение владельца):
    // плитка ушла вместе со своей шапкой, и обе секции стали называться «CIDR»
    // и «CIDR» — то есть перестали различаться вовсе. Заголовки названы
    // дословно, а не «начинается с IPv4»: имя секции — то, что читает
    // пользователь, и оно часть решения.
    show(["10.30.0.0/16"], ["fd00:30::/48"]);

    expect(screen.getByRole("heading", { name: "IPv4 CIDR" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "IPv6 CIDR" })).toBeInTheDocument();
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

  it("блок без префикса на край не уходит, а отказ стоит РЯДОМ С ПОЛЕМ", () => {
    show();

    type(v4card(), "10.30.0.0");
    fireEvent.click(addBtn(v4card()));

    // Претензия живёт в своей секции, у поля, и называет ЧЕГО не хватает.
    // Всплывающее сообщение уехало бы из угла экрана раньше, чем человек
    // перечитает набранное, — и негодная строка осталась бы без объяснения.
    expect(within(v4card()).getByRole("alert")).toHaveTextContent("CIDR должен содержать префикс (например /16).");
    expect(toastError).not.toHaveBeenCalled();
    expect(action).not.toHaveBeenCalled();
  });

  it("претензия снимается, как только значение исправили", () => {
    // Контроль в обратную сторону: без него «претензия показана» зеленело бы и
    // на реализации, которая показывает её навсегда.
    show();

    type(v4card(), "10.30.0.0");
    fireEvent.click(addBtn(v4card()));
    expect(within(v4card()).queryByRole("alert")).toBeInTheDocument();

    type(v4card(), "10.30.0.0/16");

    expect(within(v4card()).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("отказ одного семейства не задевает соседнее", () => {
    // Секции самостоятельны: претензия к четвёрке не имеет отношения к шестёрке,
    // и общая на обе означала бы обвинение поля, в которое не вводили.
    show();

    type(v4card(), "10.30.0.0");
    fireEvent.click(addBtn(v4card()));

    expect(within(v6card()).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("в шестёрочное семейство четвёрка не проходит", () => {
    show();

    type(v6card(), "10.30.0.0/16");
    fireEvent.click(addBtn(v6card()));

    expect(within(v6card()).getByRole("alert")).toHaveTextContent("Похоже не на IPv6-адрес.");
    expect(action).not.toHaveBeenCalled();
  });

  it("дубликат не уходит на край молча", () => {
    show(["10.30.0.0/16"]);

    type(v4card(), "10.30.0.0/16");
    fireEvent.click(addBtn(v4card()));

    expect(within(v4card()).getByRole("alert")).toHaveTextContent("Этот CIDR уже добавлен.");
    expect(action).not.toHaveBeenCalled();
  });

  it("кнопка снятия на строке снимает именно её блок", async () => {
    show(["10.30.0.0/16", "10.31.0.0/16"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "Удалить CIDR" })[1]);

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/networks/net-1:remove-cidr-blocks", {
        ipv4_cidr_blocks: ["10.31.0.0/16"],
      }),
    );
  });

  it("отказ края показывается текстом сервера, без кода протокола, а не проглатывается", async () => {
    action.mockRejectedValue(new ApiError(400, 9, null, "network CIDR block still contains subnets"));
    show(["10.30.0.0/16"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "Удалить CIDR" })[0]);

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("IPv4 CIDR удаление: network CIDR block still contains subnets"),
    );
  });
});
