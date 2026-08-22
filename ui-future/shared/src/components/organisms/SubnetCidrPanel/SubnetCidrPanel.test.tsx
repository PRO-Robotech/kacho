// Панель диапазонов подсети в блоке «Обзор». Своей логики не несёт — она
// раскладывает ДВЕ независимые секции семейств и раздаёт им их собственные
// данные. Перепутанная раскладка (обе секции об одном семействе, чужие блоки в
// секции) выглядит правдоподобно и приводит к тому, что операция уходит не в то
// семейство.

import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SubnetCidrPanel } from "./SubnetCidrPanel";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderPanel(over: Partial<Parameters<typeof SubnetCidrPanel>[0]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SubnetCidrPanel
        subnetId="sub-1"
        v4Primary="10.0.1.0/24"
        v6Primary="fd00:1::/64"
        v4Blocks={["10.0.2.0/24"]}
        v6Blocks={["fd00:2::/64"]}
        projectId="prj-1"
        {...over}
      />
    </QueryClientProvider>,
  );
}

/** Панель — две секции подряд: первая v4, вторая v6 (порядок и есть раскладка). */
function sections(container: HTMLElement): HTMLElement[] {
  return [...container.children] as HTMLElement[];
}

describe("SubnetCidrPanel", () => {
  it("рисует ровно две секции — по одной на семейство", () => {
    renderPanel();
    expect(screen.getByText(new RegExp("^IPv4\\b"))).toBeInTheDocument();
    expect(screen.getByText(new RegExp("^IPv6\\b"))).toBeInTheDocument();
  });

  it("основной диапазон каждого семейства показан", () => {
    // Он неизменяем и через глаголы правки не проходит — но скрыть его значило
    // бы утаить главный диапазон подсети.
    renderPanel();
    expect(screen.getByText("10.0.1.0/24")).toBeInTheDocument();
    expect(screen.getByText("fd00:1::/64")).toBeInTheDocument();
  });

  it("дополнительные диапазоны попадают в СВОЁ семейство", () => {
    const { container } = renderPanel();
    const [v4, v6] = sections(container);

    expect(within(v4).getByText(new RegExp("^IPv4\\b"))).toBeInTheDocument();
    expect(within(v4).getByText("10.0.2.0/24")).toBeInTheDocument();
    expect(within(v4).queryByText("fd00:2::/64")).not.toBeInTheDocument();

    expect(within(v6).getByText(new RegExp("^IPv6\\b"))).toBeInTheDocument();
    expect(within(v6).getByText("fd00:2::/64")).toBeInTheDocument();
    expect(within(v6).queryByText("10.0.2.0/24")).not.toBeInTheDocument();
  });

  it("семейство без диапазонов остаётся на экране, а не исчезает", () => {
    // Исчезнувшая секция читается как «этому семейству здесь не место», и
    // добавить первый диапазон становится неоткуда.
    renderPanel({ v6Primary: undefined, v6Blocks: [] });
    expect(screen.getByText(new RegExp("^IPv6\\b"))).toBeInTheDocument();
  });
});
