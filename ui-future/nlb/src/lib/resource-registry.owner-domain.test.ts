import { jest } from "@jest/globals";

// resource-registry ↔ RefNameLink / RefSelect — циклический import; тот же
// разрыв цикла ESM-моками, что в соседней пробе resource-registry.test.ts.
jest.unstable_mockModule("@/components/molecules/RefNameLink", () => ({ RefNameLink: () => null }));
jest.unstable_mockModule("@/components/organisms/form/RefSelect", () => ({ RefSelect: () => null }));

const { REGISTRY, resourceProjectPath, resourceServicePrefix } = await import("./resource-registry");

// Домен ссылки называет ВЛАДЕЛЕЦ ресурса, а не модуль, из которого смотрят.
// Карточка балансировщика ссылается на подсеть и адрес (vpc) и на машину
// (compute) — их маршруты обслуживает не nlb-remote.
describe("адресация ссылок nlb — по домену владельца", () => {
  it("свои ресурсы остаются под nlb", () => {
    expect(resourceServicePrefix("load-balancers")).toBe("nlb");
    expect(resourceProjectPath("target-groups", "prj-1")).toBe("/projects/prj-1/nlb/target-groups");
    expect(resourceProjectPath("load-balancers", null)).toBeNull();
  });

  it("подсеть и адрес принадлежат vpc, машина — compute", () => {
    expect(resourceServicePrefix("subnets")).toBe("vpc");
    expect(resourceServicePrefix("addresses")).toBe("vpc");
    expect(resourceServicePrefix("network-interfaces")).toBe("vpc");
    expect(resourceServicePrefix("compute-instances")).toBe("compute");
  });

  it("глобальный каталог живёт под /system/*", () => {
    expect(resourceProjectPath("zones", "prj-1")).toBe("/system/zones");
  });

  it("перепись: каждый ресурс реестра называет домен владельца", () => {
    const ids = Object.keys(REGISTRY);
    expect(ids.length).toBeGreaterThan(0);
    expect(ids.filter((id: string) => resourceServicePrefix(id) === null)).toEqual([]);
  });
});
