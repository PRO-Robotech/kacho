// Занятость адреса выделена наравне с соседними фактами той же карточки.
//
// Предмет (#446). Владелец: на карточке адреса «Используется ресурсом» выглядит
// неактивным (серым). Факт рисует общий `BoolFact`, и выделение у него —
// осознанный проп: цветом отмечается то, о чём стоит знать, иначе цвет перестаёт
// что-либо значить.
//
// Здесь его просто не передали. Соседние строки той же карточки —
// «Зарезервирован» и «Защита от удаления» — переданы с выделением, поэтому
// «занят» читался как выключенный на фоне включённых соседей: тише, чем ошибка,
// и заметно только рядом.
//
// Утверждается ЦВЕТ показанного текста, а не наличие пропа: проп можно передать
// и не отрисовать.
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { detailExtension } from "./ResourceDetailExtensions";

const ACCENT = "var(--kc-primary)";

function rows(data: Record<string, unknown>) {
  const ext = detailExtension("addresses");
  if (!ext?.overviewExtra) throw new Error("у карточки адреса нет строк обзора — предмет пробы отсутствует");
  return ext.overviewExtra({ data, projectId: "prj-1" } as never);
}

function showRow(data: Record<string, unknown>, label: string) {
  const row = rows(data).find((r) => r.label === label);
  if (!row) throw new Error(`строки «${label}» на карточке адреса нет — предмет пробы отсутствует`);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{row.value}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const ADDRESS = {
  id: "adr-1",
  internal_ipv4_address: { address: "10.0.0.5", subnet_id: "sub-1" },
  used: true,
  reserved: true,
  deletion_protection: true,
};

describe("карточка адреса — занятость", () => {
  it("«Используется ресурсом» выделено так же, как соседние факты", () => {
    showRow(ADDRESS, "Занятость");

    expect(screen.getByText("Используется ресурсом")).toHaveStyle({ color: ACCENT });
  });

  it("соседний факт той же карточки выделен так же (положительный контроль)", () => {
    // Без него утверждение выше зеленело бы и на карточке, где выделение снято у
    // всех — то есть на противоположном дефекте.
    showRow(ADDRESS, "Защита от удаления");

    expect(screen.getByText("Удаление запрещено")).toHaveStyle({ color: ACCENT });
  });

  it("свободный адрес выделения не получает (отрицательный контроль)", () => {
    showRow({ ...ADDRESS, used: false }, "Занятость");

    expect(screen.getByText("Свободен")).not.toHaveStyle({ color: ACCENT });
  });
});
