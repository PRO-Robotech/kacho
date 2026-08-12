// Имя ресурса в таблице — самая частая точка перехода в консоли, и вести себя
// она обязана как ссылка: у клика по строке нет ни адреса, ни вида ссылки, её
// нельзя открыть в новой вкладке и по ней не видно, что она куда-то ведёт.

import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { buildSpecColumns } from "./spec-columns";
import { REGISTRY } from "./resource-registry";

function cellOf(
  specId: string,
  row: Record<string, unknown>,
  projectId?: string,
  extra: { nameIcon?: boolean; nameHref?: (r: Record<string, unknown>) => string | null } = {},
) {
  const spec = REGISTRY[specId];
  const col = buildSpecColumns(spec, { projectId, ...extra }).find((c) => c.header === "Имя")!;
  return render(<MemoryRouter>{col.cell(row)}</MemoryRouter>);
}

describe("колонка имени ресурса", () => {
  it("ведёт на карточку этого же ресурса", () => {
    cellOf("networks", { id: "net-1", name: "core" }, "prj-1");

    expect(screen.getByRole("link", { name: /core/ })).toHaveAttribute("href", "/projects/prj-1/vpc/networks/net-1");
  });

  it("без проекта в контексте имя показано, но ссылкой не притворяется", () => {
    // Подчёркнутый текст, никуда не ведущий, обещает переход, которого нет.
    cellOf("networks", { id: "net-1", name: "core" });

    expect(screen.getByText("core")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("строка без имени показывает идентификатор, а не пустоту", () => {
    cellOf("networks", { id: "net-1", name: "" }, "prj-1");

    expect(screen.getByRole("link", { name: /net-1/ })).toBeInTheDocument();
  });

  it("в списке самого ресурса иконки у имени НЕТ", () => {
    // Столбец одинаковых значков ничего не сообщает: тип назван заголовком
    // страницы. Иконка нужна там, где в одном окне соседствуют разные типы.
    const { container } = cellOf("networks", { id: "net-1", name: "core" }, "prj-1");

    expect(container.querySelector("span[aria-hidden]")).toBeNull();
  });

  it("во вложенной таблице иконка типа есть", () => {
    const { container } = cellOf("networks", { id: "net-1", name: "core" }, "prj-1", { nameIcon: true });

    expect(container.querySelector("span[aria-hidden]")).not.toBeNull();
  });

  it("адрес можно задать вызывающему — ссылка ведёт туда же, куда вёл клик", () => {
    // Список со своим `childRoute` адресует карточку не как «база + id»;
    // подмена клика ссылкой не вправе менять адресацию.
    cellOf("networks", { id: "net-1", name: "core" }, "prj-1", {
      nameHref: () => "/projects/prj-1/vpc/networks/net-1?tab=subnets",
    });

    expect(screen.getByRole("link", { name: /core/ })).toHaveAttribute(
      "href",
      "/projects/prj-1/vpc/networks/net-1?tab=subnets",
    );
  });

  it("ресурс без поля имени ведёт с ПЕРВОЙ колонки — по ней его и узнают", () => {
    // У пользователя имени нет вовсе, его узнают по почте. Правило «поле
    // называется name» оставляло такие ресурсы вовсе без перехода.
    const spec = REGISTRY["users"];
    const first = buildSpecColumns(spec, { projectId: "prj-1" })[0];
    render(<MemoryRouter>{first.cell({ id: "usr-1", email: "a@b.c" })}</MemoryRouter>);

    expect(screen.getByRole("link", { name: /a@b\.c/ })).toHaveAttribute("href", "/iam/users/usr-1");
  });

  it("кликабельного внутри ссылки НЕТ — копирование стоит рядом", () => {
    // Прежде собственная отрисовка колонки (кнопка «скопировать имя»)
    // оборачивалась в ссылку: клик по тексту попадал на кнопку, гасился ею и
    // копировал вместо перехода. Ссылка была, выглядела рабочей и не работала.
    cellOf("networks", { id: "net-1", name: "core" }, "prj-1");

    const link = screen.getByRole("link", { name: /core/ });
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/networks/net-1");
    expect(link.querySelector("button")).toBeNull();
    // Копирование осталось — отдельной кнопкой ВНЕ ссылки.
    expect(screen.getByRole("button", { name: /Скопировать|Скопировано/i })).toBeInTheDocument();
  });
});
