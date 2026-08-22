// Состав набора префиксов меняется не правкой поля, а двумя глаголами, и каждое
// действие уходит на край сразу. Предмет пробы — ЧТО именно ушло: путь глагола и
// ИМЯ ПОЛЯ семейства.
//
// Имя поля здесь не мелочь. Глаголы этого ресурса принимают `v4_cidr_blocks` /
// `v6_cidr_blocks`, а у подсети и сети — `ipv4_…`/`ipv6_…`. Край разбирает тело,
// отбрасывая неизвестные ключи МОЛЧА: перепутанное имя дало бы успешный ответ на
// действие, ничего не изменившее, — и на экране это выглядело бы как «сохранил, а
// оно не сохранилось». Поэтому пара имён объявляется у владельца ресурса и
// проверяется здесь, а рядом стоит законный близнец той же формы (сеть), на
// котором проба обязана МОЛЧАТЬ.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const action = jest.fn<(path: string, body: unknown) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { action, get: jest.fn(), list: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: jest.fn(), success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { CidrGroupBlocksManager } = await import("./CidrGroupBlocksManager");
const { NetworkCidrManager } = await import("@shared/components/organisms/NetworkCidrManager");

function wrap(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

/** Секция семейства — ближайший предок бейджа, у которого есть своё поле ввода. */
function family(badge: string): HTMLElement {
  let el: HTMLElement | null = screen.getByText(new RegExp(`^${badge}\\b`));
  while (el && !el.querySelector("input")) el = el.parentElement;
  if (!el) throw new Error(`секция «${badge}» не найдена`);
  return el;
}

function add(card: HTMLElement, value: string) {
  fireEvent.change(within(card).getByRole("textbox"), { target: { value } });
  fireEvent.click(within(card).getByRole("button", { name: /Добавить/ }));
}

beforeEach(() => {
  jest.clearAllMocks();
  action.mockResolvedValue({});
});

describe("состав набора префиксов", () => {
  it("показывает состав в СВОЁМ семействе, пустое называет пустым", () => {
    // Число префиксов больше не утверждается: счётчик снят из шапки секции
    // решением владельца («отображать кол-во элементов не нужно»). Взамен
    // утверждение стало точнее — префикс ищется ВНУТРИ секции своего семейства,
    // а пустое семейство названо в своей.
    wrap(<CidrGroupBlocksManager cidrGroupId="cdg-1" v4Blocks={["203.0.113.0/24"]} v6Blocks={[]} />);

    expect(within(family("IPv4")).getByText("203.0.113.0/24")).toBeInTheDocument();
    expect(within(family("IPv6")).getByText("Префиксов нет")).toBeInTheDocument();
    // Контроль в обратную сторону: непустое семейство пустым не названо.
    expect(within(family("IPv4")).queryByText("Префиксов нет")).not.toBeInTheDocument();
  });

  it("семейство названо В ЗАГОЛОВКЕ — иначе обе секции называются «Префиксы»", () => {
    // Решение владельца: семейство переехало из плитки слева в заголовок.
    // Плитка ушла вместе со своей шапкой, и без переезда обе секции набора
    // назывались бы одинаково.
    wrap(<CidrGroupBlocksManager cidrGroupId="cdg-1" v4Blocks={["203.0.113.0/24"]} v6Blocks={[]} />);

    expect(screen.getByRole("heading", { name: "IPv4 Префиксы" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "IPv6 Префиксы" })).toBeInTheDocument();
  });

  it("добавление уходит глаголом набора и полем, которое ЭТОТ край принимает", async () => {
    wrap(<CidrGroupBlocksManager cidrGroupId="cdg-1" v4Blocks={[]} v6Blocks={[]} />);

    add(family("IPv4"), "203.0.113.0/24");

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(action).toHaveBeenCalledWith("/vpc/v1/cidrGroups/cdg-1:add-cidr-blocks", {
      v4_cidr_blocks: ["203.0.113.0/24"],
    });
  });

  it("снятие уходит своим глаголом, а не тем же самым", async () => {
    wrap(<CidrGroupBlocksManager cidrGroupId="cdg-1" v4Blocks={["203.0.113.0/24"]} v6Blocks={[]} />);

    fireEvent.click(within(family("IPv4")).getAllByRole("button")[0]);

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(action).toHaveBeenCalledWith("/vpc/v1/cidrGroups/cdg-1:remove-cidr-blocks", {
      v4_cidr_blocks: ["203.0.113.0/24"],
    });
  });

  it("второе семейство называет СВОЁ поле, а не поле первого", async () => {
    wrap(<CidrGroupBlocksManager cidrGroupId="cdg-1" v4Blocks={[]} v6Blocks={[]} />);

    add(family("IPv6"), "2001:db8::/48");

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(action).toHaveBeenCalledWith("/vpc/v1/cidrGroups/cdg-1:add-cidr-blocks", {
      v6_cidr_blocks: ["2001:db8::/48"],
    });
  });

  it("законный близнец той же формы — сеть — по-прежнему шлёт СВОЁ имя поля", async () => {
    // Контроль в обратную сторону: без него «набор шлёт v4_cidr_blocks» зеленело
    // бы и на реализации, которая переименовала поле у ВСЕХ ресурсов сразу.
    wrap(<NetworkCidrManager networkId="net-1" v4Blocks={[]} v6Blocks={[]} />);

    add(family("IPv4"), "10.30.0.0/16");

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(action).toHaveBeenCalledWith("/vpc/v1/networks/net-1:add-cidr-blocks", {
      ipv4_cidr_blocks: ["10.30.0.0/16"],
    });
  });
});
