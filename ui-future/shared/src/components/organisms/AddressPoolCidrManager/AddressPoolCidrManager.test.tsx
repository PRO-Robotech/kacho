// Диапазоны пула адресов правятся двумя отдельными глаголами (правка поля их не
// меняет вовсе), и каждый уходит на край сразу. Проверка на клиенте — то
// единственное, что отделяет опечатку от отказа; отказ края («в блоке есть
// выданные адреса») обязан быть показан, а не проглочен.
//
// Пул рисуется тем же `CidrTableSection`, что подсеть, сеть и набор префиксов,
// поэтому предмет этой пробы — ровно то, чем пул от них ОТЛИЧАЕТСЯ и что общая
// секция знать не может: одно-словный глагол, id пула вторым вхождением в теле,
// короткие имена полей семейства и ключ виджета заполненности. Каждое из
// четырёх край принимает молча, если ошибиться, — ответ будет успешным.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const action = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: {
    action,
    get: jest.fn(),
    list: jest.fn(),
    post: jest.fn(),
    create: jest.fn(),
    update: jest.fn(),
    delete: jest.fn(),
  },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { AddressPoolCidrManager } = await import("./AddressPoolCidrManager");

let client: QueryClient;

function show(v4: string[] = [], v6: string[] = []) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AddressPoolCidrManager poolId="ap-1" v4Blocks={v4} v6Blocks={v6} />
    </QueryClientProvider>,
  );
}

/** Секция семейства — ближайший предок бейджа, у которого есть своё поле ввода.
 *  Семейство различается бейджем шапки («IPv4»/«IPv6»), заголовок у обеих общий
 *  («CIDR») — тем же словом и в том же виде, что у подсети и сети. */
function family(badge: string): HTMLElement {
  let el: HTMLElement | null = screen.getByText(new RegExp(`^${badge}\\b`));
  while (el && !el.querySelector("input")) el = el.parentElement;
  if (!el) throw new Error(`секция «${badge}» не найдена`);
  return el;
}

const v4card = () => family("IPv4");
const v6card = () => family("IPv6");
const addBtn = (card: HTMLElement) => within(card).getByRole("button", { name: /Добавить/ });

function type(card: HTMLElement, value: string) {
  fireEvent.change(within(card).getByRole("textbox"), { target: { value } });
}

beforeEach(() => {
  jest.clearAllMocks();
  action.mockResolvedValue({});
});

describe("AddressPoolCidrManager", () => {
  it("пустое семейство названо пустым — тем же словом, что у соседних наборов", () => {
    show();

    expect(screen.getAllByText("CIDR-блоков нет")).toHaveLength(2);
  });

  it("показывает диапазоны — каждый в СВОЁМ семействе", () => {
    // Число диапазонов больше не утверждается: счётчик снят из шапки секции
    // решением владельца («отображать кол-во элементов не нужно»). «По
    // семействам» при этом не потеряно, а усилено — диапазон ищется ВНУТРИ
    // своей секции, то есть проверяется сам показанный набор, а не подпись
    // над ним.
    show(["198.51.100.0/24"], ["2001:db8::/64"]);

    expect(within(v4card()).getByText("198.51.100.0/24")).toBeInTheDocument();
    expect(within(v6card()).getByText("2001:db8::/64")).toBeInTheDocument();
    // Контроль в обратную сторону: без него «диапазон внутри своей секции»
    // зеленело бы и на реализации, рисующей оба семейства одним списком.
    expect(within(v4card()).queryByText("2001:db8::/64")).not.toBeInTheDocument();
  });

  it("семейство названо В ЗАГОЛОВКЕ — иначе две секции подряд неразличимы", () => {
    // Семейство переехало из плитки слева в заголовок (решение владельца):
    // плитка ушла вместе со своей шапкой, и обе секции пула стали называться
    // «CIDR» и «CIDR» — то есть перестали различаться.
    show(["198.51.100.0/24"], ["2001:db8::/64"]);

    expect(screen.getByRole("heading", { name: "IPv4 CIDR" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "IPv6 CIDR" })).toBeInTheDocument();
  });

  it("на пустом вводе добавить нечем", () => {
    show();

    expect(addBtn(v4card())).toBeDisabled();
  });

  it("добавление уходит одно-словным глаголом и несёт id пула в теле", async () => {
    show();

    type(v4card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v4card()));

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:addCidrBlocks", {
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
      expect(action).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:addCidrBlocks", {
        address_pool_id: "ap-1",
        v6_cidr_blocks: ["2001:db8::/64"],
      }),
    );
  });

  it("ответ приходит СИНХРОННЫЙ, и заполненность пула пересчитывается вместе с ним", async () => {
    // Пул отвечает собой, а не Operation, поэтому дожидаться нечего — кэш
    // обновляется сразу. Виджет заполненности считается из тех же блоков и
    // лежит под СВОИМ ключом: без него число пережило бы снятие блока, из
    // которого оно и считалось.
    show();
    const invalidate = jest.spyOn(client, "invalidateQueries");

    type(v4card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v4card()));

    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ["address-pools"] }));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["pool-util", "ap-1"] });
  });

  it("блок без префикса на край не уходит, а отказ стоит РЯДОМ С ПОЛЕМ", () => {
    show();

    type(v4card(), "198.51.100.0");
    fireEvent.click(addBtn(v4card()));

    expect(within(v4card()).getByRole("alert")).toHaveTextContent("CIDR должен содержать префикс (например /24).");
    expect(toastError).not.toHaveBeenCalled();
    expect(action).not.toHaveBeenCalled();
  });

  it("претензия снимается, как только значение исправили", () => {
    // Контроль в обратную сторону: без него «претензия показана» зеленело бы и
    // на реализации, которая показывает её навсегда.
    show();

    type(v4card(), "198.51.100.0");
    fireEvent.click(addBtn(v4card()));
    expect(within(v4card()).queryByRole("alert")).toBeInTheDocument();

    type(v4card(), "198.51.100.0/24");

    expect(within(v4card()).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("в шестёрочное семейство четвёрка не проходит, и пример префикса — шестёрочный", () => {
    show();

    type(v6card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v6card()));
    expect(within(v6card()).getByRole("alert")).toHaveTextContent("Похоже не на IPv6-адрес.");

    type(v6card(), "2001:db8::");
    fireEvent.click(addBtn(v6card()));
    expect(within(v6card()).getByRole("alert")).toHaveTextContent("CIDR должен содержать префикс (например /64).");

    expect(action).not.toHaveBeenCalled();
  });

  it("дубликат не уходит на край молча", () => {
    show(["198.51.100.0/24"]);

    type(v4card(), "198.51.100.0/24");
    fireEvent.click(addBtn(v4card()));

    expect(within(v4card()).getByRole("alert")).toHaveTextContent("Этот CIDR уже добавлен.");
    expect(action).not.toHaveBeenCalled();
  });

  it("кнопка снятия на строке снимает именно её диапазон", async () => {
    show(["198.51.100.0/24", "203.0.113.0/24"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "Удалить CIDR" })[1]);

    await waitFor(() =>
      expect(action).toHaveBeenCalledWith("/vpc/v1/addressPools/ap-1:removeCidrBlocks", {
        address_pool_id: "ap-1",
        v4_cidr_blocks: ["203.0.113.0/24"],
      }),
    );
  });

  it("отказ края показывается текстом сервера, без кода протокола", async () => {
    action.mockRejectedValue(new ApiError(400, 9, null, "CIDR has allocated addresses"));
    show(["198.51.100.0/24"]);

    fireEvent.click(within(v4card()).getAllByRole("button", { name: "Удалить CIDR" })[0]);

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("IPv4 CIDR удаление: CIDR has allocated addresses"));
  });
});
