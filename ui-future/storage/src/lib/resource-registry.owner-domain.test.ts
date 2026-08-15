import { REGISTRY, resourceProjectPath, resourceServicePrefix } from "./resource-registry";

// Домен ссылки называет ВЛАДЕЛЕЦ ресурса, а не модуль, из которого смотрят.
// Карточка тома ссылается на сетевой интерфейс и машину — их маршруты
// обслуживает не storage-remote.
describe("адресация ссылок storage — по домену владельца", () => {
  it("свои ресурсы остаются под storage", () => {
    expect(resourceServicePrefix("volumes")).toBe("storage");
    expect(resourceProjectPath("volumes", "prj-1")).toBe("/projects/prj-1/storage/volumes");
    expect(resourceProjectPath("volumes", null)).toBeNull();
  });

  it("машина принадлежит compute, сетевой интерфейс — vpc", () => {
    expect(resourceServicePrefix("compute-instances")).toBe("compute");
    expect(resourceServicePrefix("network-interfaces")).toBe("vpc");
  });

  it("глобальный каталог живёт под /system/*", () => {
    expect(resourceProjectPath("zones", "prj-1")).toBe("/system/zones");
    expect(resourceProjectPath("regions", null)).toBe("/system/regions");
  });

  it("перепись: каждый ресурс реестра называет домен владельца", () => {
    const ids = Object.keys(REGISTRY);
    expect(ids.length).toBeGreaterThan(0);
    expect(ids.filter((id) => resourceServicePrefix(id) === null)).toEqual([]);
  });
});
