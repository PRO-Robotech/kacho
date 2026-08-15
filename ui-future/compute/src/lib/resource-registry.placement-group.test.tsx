// Группа размещения (`PlacementGroup`) заведена в стволе как полноценный ресурс
// compute — список, чтение, создание, правка, удаление и подмаршрут операций,
// адрес которого консоль уже знала. Самого ресурса в консоли не было: ни ключа в
// реестре, ни раздела, — то есть правило взаимного размещения машин нельзя было
// ни создать, ни увидеть.
//
// Контракт (ground truth), сверено по
// proto/kacho/cloud/compute/v1/placement_group{,_service}.proto:
//   CreatePlacementGroupRequest {project_id, name, description, labels,
//                                strategy, placement_type, zone_id, region_id}
//   UpdatePlacementGroupRequest {placement_group_id, update_mask, name,
//                                description, labels} — якорь и стратегия
//                                неизменяемы
//   PlacementGroup.PlacementType ZONAL(zone_id) XOR REGIONAL(region_id):
//     строка, где заполнены оба, описывает размещение, которого не бывает.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { hasOperationsSubroute } from "@shared/lib/operations-subroute";

import { REGISTRY } from "./resource-registry";

const asObj = (v: unknown) => v as Record<string, unknown>;

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

describe("группа размещения объявлена ресурсом консоли", () => {
  it("спека есть и адресует поверхность ствола", () => {
    const spec = REGISTRY["placement-groups"];
    expect(spec).toBeDefined();
    expect(spec.apiPath).toBe("/compute/v1/placementGroups");
    expect(spec.payloadKey).toBe("placement_groups");
    expect(spec.scope).toBe("project");
    expect(spec.ops).toEqual({ create: true, update: true, delete: true });
    expect(spec.mutationsReturnOperation).toBe(true);
  });

  it("несёт вкладку операций — из перечня, а не сборкой на месте", () => {
    expect(hasOperationsSubroute(REGISTRY["placement-groups"].apiPath)).toBe(true);
    // Контроль в обратную сторону: у каталога типов машин подмаршрута нет.
    expect(hasOperationsSubroute(REGISTRY["machine-types"].apiPath)).toBe(false);
  });

  it("несёт пустое состояние", () => {
    expect(REGISTRY["placement-groups"].emptyState?.title).toBeTruthy();
    expect(REGISTRY["placement-groups"].emptyState?.body).toBeTruthy();
  });

  it("намерение названо словами предмета, а не машинным словарём", () => {
    const strategy = (REGISTRY["placement-groups"].fields ?? []).find((f) => f.name === "strategy");
    expect(strategy?.type).toBe("enum");
    const labels = strategy?.type === "enum" ? strategy.options.map((o) => o.label) : [];
    expect(labels.join(" ")).toMatch(/разнести/i);
    expect(labels.join(" ")).toMatch(/сблизить/i);
  });

  it("якорь размещения — ссылка СПИСКОМ, а не свободная строка", () => {
    const fields = REGISTRY["placement-groups"].fields ?? [];
    const zone = fields.find((f) => f.name === "zone_id");
    const region = fields.find((f) => f.name === "region_id");
    // `ref` — выбор из списка каталога geo; `string` заставлял бы оператора
    // знать идентификатор зоны наизусть и принимал бы любую опечатку.
    expect(zone?.type).toBe("ref");
    expect(region?.type).toBe("ref");
    // Ветвь якоря взаимоисключающая: видна ровно одна.
    expect(zone?.visibleWhen).toEqual({ field: "placement_type", equals: "ZONAL" });
    expect(region?.visibleWhen).toEqual({ field: "placement_type", equals: "REGIONAL" });
  });

  it("на провод уходит ровно одна координата якоря", () => {
    const spec = REGISTRY["placement-groups"];
    const zonal = spec.sanitize!(
      asObj({ ...(spec.template({ projectId: "prj-1" }) as object), placement_type: "ZONAL", zone_id: "zone-a" }),
    );
    expect(zonal.zone_id).toBe("zone-a");
    expect(zonal).not.toHaveProperty("region_id");

    const regional = spec.sanitize!(
      asObj({
        ...(spec.template({ projectId: "prj-1" }) as object),
        placement_type: "REGIONAL",
        zone_id: "zone-a",
        region_id: "reg-1",
      }),
    );
    expect(regional.region_id).toBe("reg-1");
    expect(regional).not.toHaveProperty("zone_id");
  });

  it("в списке якорь показан ссылкой на свою зону, а не идентификатором", async () => {
    const col = REGISTRY["placement-groups"].columns.find((c) => c.path === "placement_type");
    expect(col?.render).toBeDefined();

    stubList({ zones: [{ id: "zone-a", name: "ru-central1-a" }] });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/projects/prj-1/compute/placement-groups"]}>
          {col!.render!({ placement_type: "ZONAL", zone_id: "zone-a", region_id: "" })}
        </MemoryRouter>
      </QueryClientProvider>,
    );

    const link = await screen.findByRole("link", { name: "ru-central1-a" });
    expect(link).toHaveAttribute("href", "/system/zones/zone-a");
  });
});
