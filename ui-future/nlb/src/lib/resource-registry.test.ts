import { jest } from "@jest/globals";
import type { IntField } from "./form-schema";

// resource-registry ↔ RefNameLink / RefSelect — циклический import (оба резолвят
// REGISTRY/resourceProjectPath/getResource; NlbVipSourceField тянет RefSelect →
// весь form-движок). Разрываем цикл на время чистого логического теста
// ESM-моками — компоненты не рендерятся, проверяем только spec-данные.
jest.unstable_mockModule("@/components/molecules/RefNameLink", () => ({ RefNameLink: () => null }));
jest.unstable_mockModule("@/components/organisms/form/RefSelect", () => ({ RefSelect: () => null }));

const { REGISTRY, getResource, resourceServicePrefix, resourceProjectPath } = await import("./resource-registry");

describe("NLB resource-registry", () => {
  it("registers the three NLB resources + compute/vpc ref-targets для VIP-picker'а", () => {
    expect(Object.keys(REGISTRY).sort()).toEqual(
      [
        "addresses",
        "compute-instances",
        "compute-regions",
        "listeners",
        "load-balancers",
        "network-interfaces",
        "subnets",
        "target-groups",
        "zones",
      ].sort(),
    );
  });

  it("load-balancers spec — apiPath / payloadKey / ops без start+stop", () => {
    const lb = getResource("load-balancers")!;
    expect(lb.apiPath).toBe("/nlb/v1/networkLoadBalancers");
    // proto repeated-поле — network_load_balancers (не load_balancers).
    expect(lb.payloadKey).toBe("network_load_balancers");
    expect(lb.scope).toBe("project");
    // Start/Stop намеренно НЕ экспонируются в UI — ops только CRUD.
    expect(lb.ops).toEqual({ create: true, update: true, delete: true });
    // Листенеры — связанный registry-таб.
    expect(lb.related).toEqual([{ childId: "listeners", filterField: "load_balancer_id", label: "Листенеры" }]);
  });

  it("listeners / target-groups — apiPath + payloadKey", () => {
    expect(getResource("listeners")!.apiPath).toBe("/nlb/v1/listeners");
    expect(getResource("listeners")!.payloadKey).toBe("listeners");
    expect(getResource("target-groups")!.apiPath).toBe("/nlb/v1/targetGroups");
    expect(getResource("target-groups")!.payloadKey).toBe("target_groups");
  });

  it("listener port fields carry proto range min/max (не дефолтятся в 0)", () => {
    const listener = getResource("listeners")!;
    const port = listener.fields!.find((f) => f.name === "port") as IntField;
    expect(port.type).toBe("int");
    expect(port.min).toBe(1);
    expect(port.max).toBe(65535);
    expect((listener.template({ projectId: "p" }) as Record<string, unknown>).port).toBeUndefined();
  });

  describe("load-balancers sanitize — per-family VIP-oneof + placement стрижка", () => {
    it("ZONAL INTERNAL: строит v4_source subnet, drops zones + vip_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        type: "INTERNAL",
        placement_type: "ZONAL",
        disabled_announce_zones: ["z1"],
        vip_source: { _v4_enabled: true, _v4_mode: "subnet", v4: { subnet_id: "sub-1" } },
        name: "x",
      });
      expect(out.placement_type).toBe("ZONAL");
      // ZONAL → drain-зоны неприменимы → выкидываются.
      expect(out.disabled_announce_zones).toBeUndefined();
      // vip_source (UI) → wire-oneof v4_source; служебное поле удалено.
      expect(out.vip_source).toBeUndefined();
      expect(out.v4_source).toEqual({ subnet_id: "sub-1" });
      expect(out.name).toBe("x");
    });

    it("REGIONAL INTERNAL: keeps zones (plain string[]) as-is", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        type: "INTERNAL",
        placement_type: "REGIONAL",
        disabled_announce_zones: ["z1", "z2"],
        vip_source: { _v4_enabled: true, _v4_mode: "subnet", v4: { subnet_id: "sub-9" } },
      });
      expect(out.placement_type).toBe("REGIONAL");
      expect(out.disabled_announce_zones).toEqual(["z1", "z2"]);
    });

    it("EXTERNAL: строит v4_source public, drops placement_type + zones + vip_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        type: "EXTERNAL",
        placement_type: "ZONAL",
        disabled_announce_zones: ["z1"],
        vip_source: { _v4_enabled: true, _v4_mode: "public", v4: {} },
      });
      expect(out.placement_type).toBeUndefined();
      expect(out.disabled_announce_zones).toBeUndefined();
      expect(out.vip_source).toBeUndefined();
      expect(out.v4_source).toEqual({ public: {} });
    });

    it("оба семейства выключены → ни v4_source, ни v6_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({ type: "INTERNAL", vip_source: { _v4_enabled: false, _v6_enabled: false } });
      expect(out.v4_source).toBeUndefined();
      expect(out.v6_source).toBeUndefined();
    });
  });

  it("load-balancers validate — требует хотя бы одно семейство VIP", () => {
    const lb = getResource("load-balancers")!;
    expect(lb.validate!({ vip_source: { _v4_enabled: false, _v6_enabled: false } })).toMatch(/источник VIP/);
    expect(lb.validate!({ vip_source: { _v4_enabled: true, _v6_enabled: false } })).toBeNull();
  });

  it("service prefix + project path routing", () => {
    expect(resourceServicePrefix("load-balancers")).toBe("nlb");
    expect(resourceServicePrefix("compute-regions")).toBe("compute");
    expect(resourceProjectPath("target-groups", "prj-1")).toBe("/projects/prj-1/nlb/target-groups");
    expect(resourceProjectPath("load-balancers", null)).toBeNull();
  });
});
