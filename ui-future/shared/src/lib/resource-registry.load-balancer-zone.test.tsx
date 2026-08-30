// Площадка балансировщика видна арендатору (задача продукта #1473).
//
// RED-фаза: контракт понёс `zone_id`, но консоль показывала регион и вид
// размещения словом, а КАКАЯ площадка — не показывала нигде. Общий якорь
// размещения на этом ресурсе вырождался: ветвь ZONAL была недостижима by
// construction, потому что зоны в ответе не было.
//
// Утверждается НАБЛЮДАЕМОЕ — адрес, по которому уходит ссылка, а не разметка:
// проба на присутствие текста осталась бы зелёной на плоском идентификаторе, из
// которого пользователю некуда пойти.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

jest.unstable_mockModule("@shared/api/client", () => ({
  api: {
    get: jest.fn(() => Promise.resolve({})),
    list: jest.fn(() => Promise.resolve({})),
    action: jest.fn(),
    post: jest.fn(),
  },
  ApiError,
}));

const { REGISTRY } = await import("./resource-registry");
const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");

function draw(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>{node}</QueryClientProvider>
    </MemoryRouter>,
  );
}

/** Ячейка колонки «Размещение» списка балансировщиков. */
function drawPlacementColumn(row: Record<string, unknown>) {
  const spec = REGISTRY["load-balancers"];
  const col = spec.columns.find((c) => c.header === "Размещение");
  if (!col?.render) throw new Error("у списка балансировщиков нет колонки размещения");
  return draw(<div>{col.render(row)}</div>);
}

/** Строки обзора карточки балансировщика, отрисованные как «подпись → значение». */
function drawOverviewExtra(row: Record<string, unknown>): string[] {
  const ext = detailExtension("load-balancers");
  if (!ext?.overviewExtra) throw new Error("у карточки балансировщика нет доменных строк обзора");
  const items = ext.overviewExtra({ data: row, projectId: "prj-1", detailBase: "/d", navigate: jest.fn() });
  const view = draw(
    <dl>
      {items.map((it, i) => (
        <div key={i}>
          <dt>{it.label}</dt>
          <dd>{it.value}</dd>
        </div>
      ))}
    </dl>,
  );
  return [...view.container.querySelectorAll("dt")].map((dt) => dt.textContent ?? "");
}

const zonal = {
  id: "nlb-1",
  region_id: "ru-central1",
  placement_type: "ZONAL",
  zone_id: "ru-central1-a",
};
const regional = {
  id: "nlb-2",
  region_id: "ru-central1",
  placement_type: "REGIONAL",
  zone_id: "",
};

describe("список балансировщиков называет площадку", () => {
  it("зональный ведёт на карточку СВОЕЙ зоны", () => {
    drawPlacementColumn(zonal);
    expect(screen.getByRole("link", { name: /ru-central1-a/ })).toHaveAttribute(
      "href",
      "/system/zones/ru-central1-a",
    );
  });

  it("региональный ведёт на карточку региона — зоны у anycast нет", () => {
    // Положительный контроль к предыдущему: без него «ссылка на зону» зеленело
    // бы и на реализации, ведущей на зону ВСЕГДА, в том числе с пустой.
    drawPlacementColumn(regional);
    expect(screen.getByRole("link", { name: /ru-central1/ })).toHaveAttribute(
      "href",
      "/system/regions/ru-central1",
    );
  });
});

describe("карточка балансировщика называет площадку", () => {
  it("зональный показывает строку размещения со ссылкой на зону", () => {
    expect(drawOverviewExtra(zonal)).toContain("Размещение");
    expect(screen.getByRole("link", { name: /ru-central1-a/ })).toHaveAttribute(
      "href",
      "/system/zones/ru-central1-a",
    );
  });

  it("региональный на той же строке показывает регион", () => {
    drawOverviewExtra(regional);
    expect(screen.getByRole("link", { name: /ru-central1/ })).toHaveAttribute(
      "href",
      "/system/regions/ru-central1",
    );
  });
});
