import { REGISTRY, resourceProjectPath, resourceServicePrefix } from "./resource-registry";

// Ссылка на ЧУЖОЙ ресурс адресуется доменом его ВЛАДЕЛЬЦА, а не тем модулем, из
// которого на неё смотрят. Карточка машины ссылается на сетевой интерфейс (vpc),
// том (storage) и зону (глобальный каталог под /system/*) — ни один из трёх не
// обслуживается маршрутами compute-remote, и адрес вида
// /projects/<id>/compute/<чужой ресурс> попадает в его catch-all, выбрасывая
// человека обратно на список машин. Диагностика инцидента ломается на первом же
// переходе.
describe("адресация ссылок с карточки машины — по домену владельца", () => {
  it("сетевой интерфейс принадлежит vpc", () => {
    expect(resourceServicePrefix("network-interfaces")).toBe("vpc");
    expect(resourceProjectPath("network-interfaces", "prj-1")).toBe("/projects/prj-1/vpc/network-interfaces");
  });

  it("том принадлежит storage", () => {
    expect(resourceServicePrefix("volumes")).toBe("storage");
    expect(resourceProjectPath("volumes", "prj-1")).toBe("/projects/prj-1/storage/volumes");
  });

  it("зона — глобальный каталог под /system/*, а не ресурс проекта", () => {
    // Положительный контроль стоит рядом: не «всё ведёт в /system», а именно
    // глобальный каталог. Свои ресурсы модуля остаются project-scoped.
    expect(resourceProjectPath("zones", "prj-1")).toBe("/system/zones");
    expect(resourceProjectPath("compute-instances", "prj-1")).toBe("/projects/prj-1/compute/instances");
  });

  it("перепись: каждый ресурс реестра называет домен владельца", () => {
    const ids = Object.keys(REGISTRY);
    expect(ids.length).toBeGreaterThan(0);
    const unnamed = ids.filter((id) => resourceServicePrefix(id) === null);
    expect(unnamed).toEqual([]);
  });
});
