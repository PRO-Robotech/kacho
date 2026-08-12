// Широкая таблица прокручивается вбок, и при этом строка обязана оставаться
// узнаваемой: колонка идентичности (первая) закрепляется слева, столбец
// действий (последний, без заголовка) — справа. Иначе, уехав вправо, читатель
// видит значения, не понимая, к какому ресурсу они относятся, и не может
// дотянуться до действий, не прокрутившись обратно.
import { render, screen } from "@testing-library/react";
import { ResourceTable } from "./ResourceTable";
import type { Column } from "./ResourceTable";

interface Row {
  id: string;
  name: string;
  cidr: string;
}

const rows: Row[] = [{ id: "net-1", name: "core", cidr: "10.0.0.0/16" }];

function cols(withActions: boolean): Column<Row>[] {
  const base: Column<Row>[] = [
    { header: "Имя", cell: (r) => r.name },
    { header: "CIDR", cell: (r) => r.cidr },
  ];
  return withActions ? [...base, { header: "", cell: () => "⋯" }] : base;
}

function headers() {
  return screen.getAllByRole("columnheader");
}

describe("ResourceTable — закрепление краёв при прокрутке вбок", () => {
  it("служебная колонка выбора не получает ширину колонки данных", () => {
    // Реальный регресс: таблице правил группы безопасности первая колонка —
    // выбор строки, и ширина 260 превратила её в пустую полосу через пол-экрана.
    // Закрепляется отрезок до имени включительно, но узкому — узкое.
    const withSelect: Column<Row>[] = [
      { header: "", cell: () => "☐" },
      { header: "Имя", cell: (r) => r.name },
      { header: "CIDR", cell: (r) => r.cidr },
      { header: "", cell: () => "⋯" },
    ];
    render(<ResourceTable rows={rows} columns={withSelect} rowKey={(r) => r.id} />);
    const h = headers();
    expect(h[0]).toHaveAttribute("data-fixed", "left");
    expect(h[0]).toHaveAttribute("data-width", "48");
    expect(h[1]).toHaveAttribute("data-fixed", "left");
    expect(h[1]).toHaveAttribute("data-width", "260");
    expect(h[2]).not.toHaveAttribute("data-fixed");
  });

  it("колонка идентичности закреплена слева", () => {
    render(<ResourceTable rows={rows} columns={cols(true)} rowKey={(r) => r.id} />);
    expect(headers()[0]).toHaveAttribute("data-fixed", "left");
  });

  it("столбец действий закреплён справа", () => {
    render(<ResourceTable rows={rows} columns={cols(true)} rowKey={(r) => r.id} />);
    const h = headers();
    expect(h[h.length - 1]).toHaveAttribute("data-fixed", "right");
  });

  it("закреплённые колонки несут явную ширину", () => {
    // Без ширины antd закрепление ИГНОРИРУЕТ: колонка не липнет, а проба,
    // смотрящая только на `fixed`, этого не видит. Знание оплачено в registry
    // (там оно записано комментарием у своей копии таблицы) — здесь оно держится
    // проверкой, а не комментарием.
    render(<ResourceTable rows={rows} columns={cols(true)} rowKey={(r) => r.id} />);
    const h = headers();
    expect(h[0]).toHaveAttribute("data-width");
    expect(h[h.length - 1]).toHaveAttribute("data-width");
  });

  it("промежуточные колонки НЕ закреплены", () => {
    // Положительный контроль: без него оба утверждения выше зеленели бы и на
    // таблице, закрепившей вообще всё, — то есть на таблице, которая вбок не
    // прокручивается по построению.
    render(<ResourceTable rows={rows} columns={cols(true)} rowKey={(r) => r.id} />);
    expect(headers()[1]).not.toHaveAttribute("data-fixed");
    // Ширина назначается только закреплённым: остальные держат натуральную.
    expect(headers()[1]).not.toHaveAttribute("data-width");
  });

  it("последняя колонка С заголовком справа НЕ закрепляется", () => {
    // Закрепляется именно столбец действий, узнаваемый по пустому заголовку.
    // Обычная последняя колонка данных закрепления не получает: иначе на узком
    // экране закреплённые края съели бы всю видимую ширину.
    render(<ResourceTable rows={rows} columns={cols(false)} rowKey={(r) => r.id} />);
    const h = headers();
    expect(h[h.length - 1]).not.toHaveAttribute("data-fixed");
  });
});
