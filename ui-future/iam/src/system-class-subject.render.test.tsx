// Показ системного класса субъекта в перечне и карточке выдачи (kacho#3238;
// приёмка kaname#829, SC-53). Расширение карточки и ячейка перечня
// ИСПОЛНЯЮТСЯ, утверждается отрисованное: класс назван подписью, ссылки нет,
// системная выдача помечена «платформенная». Каждое отрицание стоит в паре с
// близнецом-человеком, у которого ссылка есть.

import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import { REGISTRY } from "@shared/lib/resource-registry";

import "./registerExtensions";

function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const subjectColumn = REGISTRY["access-bindings"].columns.find((c) => c.header === "Субъект");

function showListCell(row: Record<string, unknown>) {
  expect(subjectColumn?.render).toBeDefined();
  return show(<div data-testid="cell">{subjectColumn!.render!(row)}</div>);
}

const classRow = {
  id: "acb-1",
  subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" }],
  subject_type: "system_class",
  subject_id: "authenticated",
  system: false,
};

describe("SC-53: перечень выдач", () => {
  it("класс показан подписью и без ссылки", () => {
    showListCell(classRow);
    const cell = screen.getByTestId("cell");
    expect(cell.textContent).toContain("Вошедшие (любой аутентифицированный)");
    expect(cell.querySelector("a")).toBeNull();
    expect(cell.textContent).not.toContain("платформенная");
  });

  it("близнец: человек показан ссылкой — отсутствие ссылки у класса не свойство ячейки вообще", () => {
    showListCell({ id: "acb-2", subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }] });
    expect(screen.getByTestId("cell").querySelector("a")).not.toBeNull();
  });

  it("системная выдача помечена «платформенная»", () => {
    showListCell({
      ...classRow,
      subjects: [{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "anonymous" }],
      system: true,
    });
    const cell = screen.getByTestId("cell");
    expect(cell.textContent).toContain("Аноним (без входа)");
    expect(cell.textContent).toContain("платформенная");
    expect(cell.querySelector("a")).toBeNull();
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
