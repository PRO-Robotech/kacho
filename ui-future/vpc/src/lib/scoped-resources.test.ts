// Раздел, которого нет в общем реестре, не отличим от раздела, которого не
// задумывали.
//
// Маршруты приложения строятся как `IDS.map((id) => REGISTRY[id]).filter(Boolean)`.
// `filter(Boolean)` — не защита, а глушитель: id, исчезнувший из общего реестра,
// он выбрасывает без единого признака, и приложение поднимается «зелёным» с
// молча пропавшим разделом. Здесь список утверждается против реестра, поэтому
// исчезновение спеки становится падением с именем.
//
// Источник истины о том, какие ресурсы вообще существуют, — proto ствола: спека
// общего реестра несёт apiPath, который обязан принадлежать поверхности ствола.
// Здесь проверяется предыдущий шаг цепочки — что id вообще резолвится.

import { REGISTRY } from "@shared/lib/resource-registry";
import {
  ALL_SCOPED_IDS,
  COMPUTE_SCOPED_IDS,
  NLB_SCOPED_IDS,
  SYSTEM_SCOPED_IDS,
  VPC_SCOPED_IDS,
} from "./scoped-resources";

describe("scoped-resources — каждый смонтированный id резолвится в общем реестре", () => {
  it.each(ALL_SCOPED_IDS.map((id) => [id]))("%s есть в REGISTRY", (id) => {
    expect(Object.keys(REGISTRY)).toContain(id);
  });

  // Положительный контроль к отрицанию выше: проверка обязана уметь краснеть.
  // Без него `toContain` над пустым списком id зеленел бы на любом реестре.
  it("несуществующий id проверку НЕ проходит", () => {
    expect(Object.keys(REGISTRY)).not.toContain("resource-that-does-not-exist");
  });

  it("списки непусты — «ноль находок» не должно означать «ноль прочитанного»", () => {
    expect(VPC_SCOPED_IDS.length).toBeGreaterThan(0);
    expect(COMPUTE_SCOPED_IDS.length).toBeGreaterThan(0);
    expect(NLB_SCOPED_IDS.length).toBeGreaterThan(0);
    expect(SYSTEM_SCOPED_IDS.length).toBeGreaterThan(0);
    expect(ALL_SCOPED_IDS.length).toBe(
      VPC_SCOPED_IDS.length + COMPUTE_SCOPED_IDS.length + NLB_SCOPED_IDS.length + SYSTEM_SCOPED_IDS.length + 1,
    );
  });
});
