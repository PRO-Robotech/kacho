// Наблюдаемое: КУДА ведёт ссылка на чужой ресурс с карточки машины.
//
// Проба утверждает АДРЕС, а не присутствие текста (правило 2 канона консоли):
// прежняя форма дефекта — ссылка есть, выглядит рабочей и ведёт в маршрут,
// которого у compute-remote нет; catch-all уводит человека на список машин, и
// диагностика инцидента ломается на первом переходе.

import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { RefNameLink } from "./RefNameLink";

function hrefOf(specId: string, refId: string): string | null {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/compute/instances/ins-1"]}>
        <RefNameLink specId={specId} refId={refId} projectId="prj-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return container.querySelector("a")?.getAttribute("href") ?? null;
}

describe("ссылка с карточки машины открывает карточку ВЛАДЕЛЬЦА", () => {
  it("сетевой интерфейс — карточка vpc", () => {
    expect(hrefOf("network-interfaces", "nic-1")).toBe("/projects/prj-1/vpc/network-interfaces/nic-1");
  });

  it("том — карточка storage", () => {
    expect(hrefOf("volumes", "vol-1")).toBe("/projects/prj-1/storage/volumes/vol-1");
  });

  it("зона — карточка глобального каталога под /system/*", () => {
    expect(hrefOf("zones", "zone-1")).toBe("/system/zones/zone-1");
  });

  // Положительный контроль: свой ресурс остаётся под своим доменом — иначе
  // проба зеленела бы на правке, которая просто увела ВСЕ ссылки в чужой домен.
  it("своя машина остаётся под compute", () => {
    expect(hrefOf("compute-instances", "ins-2")).toBe("/projects/prj-1/compute/instances/ins-2");
  });
});
