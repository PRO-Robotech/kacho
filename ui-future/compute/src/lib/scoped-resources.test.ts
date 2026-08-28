// Раздел, которого нет в общем реестре, не отличим от раздела, которого не
// задумывали (#406).
//
// Маршруты приложения строятся как `IDS.map((id) => REGISTRY[id])`, и
// исчезнувший id даёт `undefined` — роутер выбрасывает такой раздел МОЛЧА, и
// приложение поднимается «зелёным» с пропавшей страницей. Здесь перечень
// утверждается против общего реестра, поэтому исчезновение спеки становится
// падением С ИМЕНЕМ.

import { REGISTRY as SHARED_REGISTRY } from "@shared/lib/resource-registry";

import { REGISTRY } from "./resource-registry";
import { ALL_SCOPED_IDS, COMPUTE_REF_TARGET_IDS, COMPUTE_SCOPED_IDS } from "./scoped-resources";

describe("scoped-resources — каждый смонтированный id резолвится в общем реестре", () => {
  it.each(ALL_SCOPED_IDS.map((id) => [id]))("%s есть в общем реестре", (id) => {
    expect(Object.keys(SHARED_REGISTRY)).toContain(id);
  });

  it("несуществующий id проверку НЕ проходит", () => {
    // Положительный контроль к отрицанию выше: без него `toContain` над пустым
    // перечнем зеленел бы на любом реестре.
    expect(Object.keys(SHARED_REGISTRY)).not.toContain("resource-that-does-not-exist");
  });

  it("перечни непусты и складываются — «ноль находок» не должно означать «ноль прочитанного»", () => {
    expect(COMPUTE_SCOPED_IDS.length).toBeGreaterThan(0);
    expect(COMPUTE_REF_TARGET_IDS.length).toBeGreaterThan(0);
    expect(ALL_SCOPED_IDS.length).toBe(COMPUTE_SCOPED_IDS.length + COMPUTE_REF_TARGET_IDS.length);
  });
});

describe("реестр приложения — проекция общего, а не его копия", () => {
  it("резолвится КАЖДЫЙ объявленный id: проекция не теряет спеку молча", () => {
    const missing = ALL_SCOPED_IDS.filter((id) => !REGISTRY[id]);
    expect(missing).toEqual([]);
    expect(Object.keys(REGISTRY).length).toBe(ALL_SCOPED_IDS.length);
  });

  it("чужого раздела в реестре НЕТ — проекция сужает, а не тянет весь общий", () => {
    // Иначе «проекция» означала бы реэкспорт всего общего реестра, и раздел
    // чужого домена притворялся бы здешним.
    expect(Object.keys(REGISTRY)).not.toContain("networks");
    expect(Object.keys(SHARED_REGISTRY)).toContain("networks");
  });

  it("спека берётся ИЗ общего, а не объявляется заново", () => {
    // Тождество ссылки — самый сильный из доступных предикатов «это не копия»:
    // копия, даже побайтово равная, была бы другим объектом.
    expect(REGISTRY["compute-instances"]).toBe(SHARED_REGISTRY["compute-instances"]);
    expect(REGISTRY["zones"]).toBe(SHARED_REGISTRY["zones"]);
  });
});
