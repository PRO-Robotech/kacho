// Полоса утилизации IP-блока — единственное, по чему администратор видит, что
// пул кончается. Её содержание — АРИФМЕТИКА: доля, свободный остаток и цвет
// порога. Ошибка здесь не роняет ничего: полоса рисуется, просто показывает не
// то число, — и пул исчерпывается «неожиданно».

import { render, screen, within } from "@testing-library/react";
import { CIDRBreakdown, IpamUtilizationBar } from "./IpamUtilizationBar";

/** Цветной сегмент — тот, у которого задана ширина в процентах. */
function fillOf(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>("[style*='width']");
  if (!el) throw new Error("сегмент заполнения не найден");
  return el;
}

describe("IpamUtilizationBar", () => {
  it("считает долю от общего и свободный остаток", () => {
    const { container } = render(<IpamUtilizationBar total={256} used={64} />);

    expect(screen.getByText("64 / 256 (25%)")).toBeInTheDocument();
    expect(within(screen.getByText("свободно:").parentElement!).getByText("192")).toBeInTheDocument();
    expect(fillOf(container).style.width).toBe("25%");
  });

  it("принимает числа строками — так они приходят с края", () => {
    // Счётчики адресов уезжают на wire строками (int64 в JSON); молчаливая
    // конкатенация вместо сложения дала бы правдоподобную чушь.
    render(<IpamUtilizationBar total="256" used="64" />);
    expect(screen.getByText("64 / 256 (25%)")).toBeInTheDocument();
  });

  it("уважает свободный остаток, присланный сервером, вместо своей разности", () => {
    render(<IpamUtilizationBar total={256} used={64} free={100} />);
    expect(within(screen.getByText("свободно:").parentElement!).getByText("100")).toBeInTheDocument();
  });

  it("пустой блок не делит на ноль", () => {
    const { container } = render(<IpamUtilizationBar total={0} used={0} />);
    expect(screen.getByText("0 / 0 (0%)")).toBeInTheDocument();
    expect(fillOf(container).style.width).toBe("0%");
  });

  it("присланную долю зажимает в границы, а не рисует за краем", () => {
    const over = render(<IpamUtilizationBar total={10} used={10} percent={140} />);
    expect(fillOf(over.container).style.width).toBe("100%");

    const under = render(<IpamUtilizationBar total={10} used={0} percent={-5} />);
    expect(fillOf(under.container).style.width).toBe("0%");
  });

  it("меняет тон на порогах 30 / 70 / 90", () => {
    // Порог — единственный сигнал «пора расширять пул»; заезд границы на
    // единицу делает предупреждение поздним.
    //
    // Наблюдаемое — ЗАЛИВКА, а не имя класса: тон приходит из набора состояний
    // продукта (`--status-*`) инлайн-стилем, и обе темы читают его сами. Проба
    // на имя класса палитры Tailwind закрепляла бы ровно тот хардкод, из-за
    // которого полоса не участвовала в теме.
    const cases: Array<[number, string]> = [
      [29, "var(--status-ok-fg)"],
      [30, "var(--status-info-fg)"],
      [69, "var(--status-info-fg)"],
      [70, "var(--status-warn-fg)"],
      [89, "var(--status-warn-fg)"],
      [90, "var(--status-error-fg)"],
      [100, "var(--status-error-fg)"],
    ];
    for (const [pct, tone] of cases) {
      const { container } = render(<IpamUtilizationBar total={100} used={pct} />);
      expect(fillOf(container).style.background).toBe(tone);
    }

    // Контроль в обратную сторону: четыре порога обязаны дать ЧЕТЫРЕ разных
    // тона. Без него набор совпадений зеленел бы и на полосе одного цвета.
    expect(new Set(cases.map(([, tone]) => tone)).size).toBe(4);
  });

  it("показывает подпись, когда она есть, и не заводит пустой строки, когда её нет", () => {
    render(<IpamUtilizationBar total={10} used={1} label="Пул по умолчанию" />);
    expect(screen.getByText("Пул по умолчанию")).toBeInTheDocument();

    const bare = render(<IpamUtilizationBar total={10} used={1} />);
    expect(bare.container.firstElementChild!.children).toHaveLength(2);
  });
});

describe("CIDRBreakdown", () => {
  it("не рисует таблицы, когда разбивки нет", () => {
    expect(render(<CIDRBreakdown cidrs={[]} />).container).toBeEmptyDOMElement();
  });

  it("строка на каждый блок со своей долей", () => {
    const { container } = render(
      <CIDRBreakdown
        cidrs={[
          { cidr: "10.0.0.0/24", total: 256, used: 250 },
          { cidr: "10.0.1.0/24", total: 256, used: 10 },
        ]}
      />,
    );

    expect(screen.getByText("10.0.0.0/24")).toBeInTheDocument();
    expect(screen.getByText("250/256")).toBeInTheDocument();
    expect(screen.getByText("10/256")).toBeInTheDocument();

    const fills = container.querySelectorAll<HTMLElement>("tbody [style*='width']");
    expect(fills).toHaveLength(2);
    // 250/256 = 97% — тон ошибки; 10/256 = 3% — тон здоровья.
    expect(fills[0].style.background).toBe("var(--status-error-fg)");
    expect(fills[1].style.background).toBe("var(--status-ok-fg)");
  });

  it("блок нулевого размера не делит на ноль", () => {
    const { container } = render(<CIDRBreakdown cidrs={[{ cidr: "10.0.0.0/32", total: 0, used: 0 }]} />);
    expect(container.querySelector<HTMLElement>("tbody [style*='width']")!.style.width).toBe("0%");
  });
});
