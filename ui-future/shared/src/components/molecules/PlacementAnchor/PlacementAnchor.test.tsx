// Якорь размещения ресурса — зона (ZONAL) либо регион (REGIONAL). И зона, и
// регион суть РЕСУРСЫ каталога geo со своими карточками, поэтому якорь обязан
// вести на них, как всякая ссылка на чужой ресурс: иконка типа + имя + переход.
// Плоский моноширинный текст показывал идентификатор, из которого пользователю
// нечего было сделать: ни узнать имя, ни открыть карточку.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { PlacementAnchor } from "./PlacementAnchor";

const realFetch = globalThis.fetch;

function stubList(payload: unknown) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function show(row: Record<string, unknown>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/subnets/sub-1"]}>
        <PlacementAnchor row={row} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("PlacementAnchor", () => {
  it("зональный ресурс ведёт на карточку СВОЕЙ зоны", async () => {
    stubList({ zones: [{ id: "zone-a", name: "ru-central1-a" }] });
    show({ placement_type: "ZONAL", zone_id: "zone-a", region_id: "" });

    const link = await screen.findByRole("link", { name: "ru-central1-a" });
    expect(link).toHaveAttribute("href", "/system/zones/zone-a");
  });

  it("региональный ресурс ведёт на карточку СВОЕГО региона", async () => {
    stubList({ regions: [{ id: "reg-1", name: "ru-central1" }] });
    show({ placement_type: "REGIONAL", zone_id: "", region_id: "reg-1" });

    const link = await screen.findByRole("link", { name: "ru-central1" });
    expect(link).toHaveAttribute("href", "/system/regions/reg-1");
  });

  it("вид размещения назван словом, а не только ссылкой", async () => {
    // Тег несёт то, чего в имени якоря нет: anycast-подсеть от зональной по
    // имени региона не отличить.
    stubList({ regions: [{ id: "reg-1", name: "ru-central1" }] });
    show({ placement_type: "REGIONAL", zone_id: "", region_id: "reg-1" });

    expect(await screen.findByText("REGIONAL")).toBeInTheDocument();
  });

  it("вид выводится из якоря, когда сервер его не назвал", async () => {
    // `placement_type` — производное поле; на старых записях его может не быть,
    // и тогда единственный признак — какой из двух якорей заполнен.
    stubList({ zones: [{ id: "zone-a", name: "ru-central1-a" }] });
    show({ zone_id: "zone-a", region_id: "" });

    expect(await screen.findByText("ZONAL")).toBeInTheDocument();
  });

  it("без якоря рисует прочерк, а не пустое место", () => {
    stubList({});
    show({ placement_type: "", zone_id: "", region_id: "" });

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
