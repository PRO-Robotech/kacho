import { jest } from "@jest/globals";
import type { IntField } from "@shared/lib/form-schema";
import { ENTITIES } from "@shared/lib/entity-names";

// resource-registry ↔ RefNameLink / RefSelect — циклический import (оба резолвят
// REGISTRY/resourceProjectPath/getResource; NlbVipSourceField тянет RefSelect →
// весь form-движок). Разрываем цикл на время чистого логического теста
// ESM-моками — компоненты не рендерятся, проверяем только spec-данные.
jest.unstable_mockModule("@/components/molecules/RefNameLink", () => ({ RefNameLink: () => null }));
jest.unstable_mockModule("@/components/organisms/form/RefSelect", () => ({ RefSelect: () => null }));

const { REGISTRY, getResource, resourceServicePrefix, resourceProjectPath } = await import("./resource-registry");

// Ресурсы, которые раздел РИСУЕТ (`NlbPage`), — против ресурсов, которые его
// формы НАЗЫВАЮТ ссылкой. Перечня ключей реестра здесь больше нет: реестр у
// консоли один и общий, его состав — свойство всей консоли, а не этого раздела,
// и выписанный список устаревал бы на каждом чужом ресурсе, ничего не измеряя.
const RENDERED = ["load-balancers", "listeners", "target-groups"] as const;

/** Идентификаторы ссылочных целей, названные полями спеки (включая подполя). */
function refTargets(specId: string): string[] {
  const out = new Set<string>();
  const walk = (fields: { refResource?: string; itemFields?: unknown[] }[] | undefined) => {
    for (const f of fields ?? []) {
      if (f.refResource) out.add(f.refResource);
      walk(f.itemFields as { refResource?: string; itemFields?: unknown[] }[] | undefined);
    }
  };
  walk(REGISTRY[specId]?.fields as { refResource?: string; itemFields?: unknown[] }[] | undefined);
  return [...out].sort();
}

describe("NLB resource-registry", () => {
  it("все три ресурса раздела в реестре есть", () => {
    for (const id of RENDERED) expect(getResource(id)).toBeDefined();
  });

  it("каждая ссылочная цель форм раздела резолвится в том же реестре", () => {
    // Ссылку рисует `RefSelect`/`RefNameLink` по идентификатору спеки. Цель,
    // которой в реестре нет, — не пустой список, а поле, не работающее ни при
    // каком вводе: имя не резолвится, ссылка не строится.
    const targets = [...new Set(RENDERED.flatMap((id) => refTargets(id)))].sort();
    // Перепись: «целей не найдено» обязано быть отличимо от «все резолвятся».
    expect(targets.length).toBeGreaterThan(3);
    expect(targets.filter((t) => !getResource(t))).toEqual([]);
  });

  it("предикат целей различает — контроль в обе стороны", () => {
    // Без него утверждение выше зеленело бы на пустом перечне и на реестре,
    // отдающем спеку по любому имени.
    expect(refTargets("listeners")).toContain("load-balancers");
    expect(getResource("нет-такого-ресурса")).toBeUndefined();
  });

  it("load-balancers spec — apiPath / payloadKey / ops без start+stop", () => {
    const lb = getResource("load-balancers")!;
    expect(lb.apiPath).toBe("/nlb/v1/networkLoadBalancers");
    // proto repeated-поле — network_load_balancers (не load_balancers).
    expect(lb.payloadKey).toBe("network_load_balancers");
    expect(lb.scope).toBe("project");
    // Start/Stop намеренно НЕ экспонируются в UI — ops только CRUD.
    expect(lb.ops).toEqual({ create: true, update: true, delete: true });
    // Обработчики — связанный registry-таб. Подпись берётся из единственного
    // источника имён (`entity-names`), а не литералом: литерал рядом с местом
    // показа расходится с каноном молча — так «Листенеры» и пережили здесь
    // переименование ресурса в «Обработчики».
    expect(lb.related).toEqual([
      { childId: "listeners", filterField: "load_balancer_id", label: ENTITIES.listeners.plural },
    ]);
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
    // Режим задаёт `placement` — единственный ввод режима, который форма несёт.
    // `type` / `placement_type` в запросе существуют лишь затем, чтобы выставивший
    // их клиент получил явный InvalidArgument, поэтому вход sanitize их не несёт и
    // выход нести не может.
    it("INTERNAL_ZONAL: строит v4_source subnet, drops zones + vip_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        placement: "INTERNAL_ZONAL",
        disabled_announce_zones: ["z1"],
        vip_source: { _v4_enabled: true, _v4_mode: "subnet", v4: { subnet_id: "sub-1" } },
        name: "x",
      });
      // ZONAL → drain-зоны неприменимы → выкидываются.
      expect(out.disabled_announce_zones).toBeUndefined();
      // vip_source (UI) → wire-oneof v4_source; служебное поле удалено.
      expect(out.vip_source).toBeUndefined();
      expect(out.v4_source).toEqual({ subnet_id: "sub-1" });
      expect(out.name).toBe("x");
    });

    it("INTERNAL_REGIONAL: keeps zones (plain string[]) as-is", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        placement: "INTERNAL_REGIONAL",
        disabled_announce_zones: ["z1", "z2"],
        vip_source: { _v4_enabled: true, _v4_mode: "subnet", v4: { subnet_id: "sub-9" } },
      });
      expect(out.disabled_announce_zones).toEqual(["z1", "z2"]);
    });

    it("EXTERNAL_REGIONAL: строит v4_source public, keeps zones, drops vip_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        placement: "EXTERNAL_REGIONAL",
        disabled_announce_zones: ["z1"],
        vip_source: { _v4_enabled: true, _v4_mode: "public", v4: {} },
      });
      // Ни type, ни placement_type форма не несёт и sanitize не производит.
      expect(out.type).toBeUndefined();
      expect(out.placement_type).toBeUndefined();
      // EXTERNAL_REGIONAL — тоже REGIONAL, drain-зоны применимы.
      expect(out.disabled_announce_zones).toEqual(["z1"]);
      expect(out.vip_source).toBeUndefined();
      expect(out.v4_source).toEqual({ public: {} });
    });

    it("оба семейства выключены → ни v4_source, ни v6_source", () => {
      const lb = getResource("load-balancers")!;
      const out = lb.sanitize!({
        placement: "INTERNAL_ZONAL",
        vip_source: { _v4_enabled: false, _v6_enabled: false },
      });
      expect(out.v4_source).toBeUndefined();
      expect(out.v6_source).toBeUndefined();
    });
  });

  it("load-balancers validate — требует хотя бы одно семейство VIP", () => {
    const lb = getResource("load-balancers")!;
    // validate строит source через _v4_mode/v4 (не _v4_enabled): без источника → ошибка.
    expect(lb.validate!({ vip_source: {} })).toMatch(/источник VIP/);
    // с валидным источником одного семейства (subnet) → null.
    expect(lb.validate!({ vip_source: { _v4_mode: "subnet", v4: { subnet_id: "sub-x" } } })).toBeNull();
  });

  it("service prefix + project path routing", () => {
    expect(resourceServicePrefix("load-balancers")).toBe("nlb");
    expect(resourceServicePrefix("compute-regions")).toBe("compute");
    expect(resourceProjectPath("target-groups", "prj-1")).toBe("/projects/prj-1/nlb/target-groups");
    expect(resourceProjectPath("load-balancers", null)).toBeNull();
  });
});
