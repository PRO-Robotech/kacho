// Показ системного класса субъекта в перечне и карточке выдачи (kacho#3238;
// приёмка kaname#829, SC-53). Расширение карточки и ячейка перечня
// ИСПОЛНЯЮТСЯ, утверждается отрисованное: класс назван подписью, ссылки на
// субъект нет. Каждое отрицание стоит в паре с близнецом-человеком, у которого
// ссылка есть. Невидимость системной выдачи держит область, а не метка
// (решение Д5 приёмки), — её утверждает сквозная проба SC-53.

import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import { REGISTRY } from "@shared/lib/resource-registry";
import { buildSpecColumns } from "@shared/lib/spec-columns";

import "./registerExtensions";

function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// Ячейку берём так, как её рисует ПЕРЕЧЕНЬ: колонка «Субъект» у выдачи —
// колонка идентичности, и `buildSpecColumns` рисует её ссылкой на карточку
// выдачи, а собственный `render` колонки отбрасывает. Проба, звавшая `render`
// напрямую, утверждала то, чего оператор не видит (kacho#3238, console-e2e
// SC-53: на стенде в ячейке стоял голый `authenticated`).
const listColumns = buildSpecColumns(REGISTRY["access-bindings"]);
const subjectColumn = listColumns.find((c) => c.header === "Субъект");

function showListCell(row: Record<string, unknown>) {
  expect(subjectColumn).toBeDefined();
  return show(<div data-testid="cell">{subjectColumn!.cell(row)}</div>);
}

const classRow = {
  id: "acb-1",
  subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" }],
  subject_type: "system_class",
  subject_id: "authenticated",
  system: false,
};

describe("SC-53: перечень выдач", () => {
  it("класс показан подписью класса, ссылка ячейки — только на карточку выдачи", () => {
    showListCell(classRow);
    const cell = screen.getByTestId("cell");
    expect(cell.textContent).toContain("Вошедшие (любой аутентифицированный)");
    expect(cell.textContent).not.toContain("authenticated");
    const hrefs = Array.from(cell.querySelectorAll("a")).map((a) => a.getAttribute("href"));
    // Страницы у класса нет — ни одна ссылка не ведёт на ресурс субъекта.
    expect(hrefs).toEqual(["/iam/access-bindings/acb-1"]);
  });

  it("близнец: у человека та же ячейка называет его идентификатор — подпись класса не свойство ячейки вообще", () => {
    showListCell({
      id: "acb-2",
      subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
      subject_type: "user",
      subject_id: "usr-1",
    });
    const cell = screen.getByTestId("cell");
    expect(cell.textContent).toContain("usr-1");
    expect(cell.textContent).not.toContain("Вошедшие");
  });

  it("класс «аноним» — своей подписью", () => {
    showListCell({
      ...classRow,
      subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "anonymous" }],
      subject_id: "anonymous",
    });
    expect(screen.getByTestId("cell").textContent).toContain("Аноним (без входа)");
  });
});

describe("SC-53: карточка выдачи", () => {
  const ext = detailExtension("access-bindings");

  function subjectsShown(data: Record<string, unknown>): HTMLElement {
    const rows =
      ext?.overviewExtra?.({ data, projectId: null, detailBase: "/iam/access-bindings/acb-1", navigate: () => {} }) ??
      [];
    const subjects = rows.find((r) => r.label === "Субъекты");
    expect(subjects).toBeDefined();
    show(<div data-testid="subjects">{subjects!.value}</div>);
    return screen.getByTestId("subjects");
  }

  it("класс показан подписью и без ссылки", () => {
    const shown = subjectsShown({ ...classRow, target: { resources: { resources: [] } } });
    expect(shown.textContent).toBe("Вошедшие (любой аутентифицированный)");
    expect(shown.querySelector("a")).toBeNull();
  });

  it("близнец: человек в карточке — ссылкой", () => {
    const shown = subjectsShown({ id: "acb-2", subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }] });
    expect(shown.querySelector("a")).not.toBeNull();
  });
});
