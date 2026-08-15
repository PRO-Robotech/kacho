import { REGISTRY, resourceProjectPath, resourceServicePrefix } from "./resource-registry";

// Домен ссылки называет ВЛАДЕЛЕЦ ресурса, а не модуль, из которого смотрят.
describe("адресация ссылок registry — по домену владельца", () => {
  it("свои ресурсы остаются под registry", () => {
    expect(resourceServicePrefix("registries")).toBe("registry");
    expect(resourceServicePrefix("repositories")).toBe("registry");
    expect(resourceServicePrefix("tags")).toBe("registry");
    expect(resourceProjectPath("registries", "prj-1")).toBe("/projects/prj-1/registry/registries");
    expect(resourceProjectPath("registries", null)).toBeNull();
  });

  it("глобальный каталог живёт под /system/*, а не в проекте", () => {
    expect(resourceProjectPath("regions", "prj-1")).toBe("/system/regions");
  });

  it("перепись: каждый ресурс реестра называет домен владельца", () => {
    const ids = Object.keys(REGISTRY);
    expect(ids.length).toBeGreaterThan(0);
    expect(ids.filter((id) => resourceServicePrefix(id) === null)).toEqual([]);
  });
});
