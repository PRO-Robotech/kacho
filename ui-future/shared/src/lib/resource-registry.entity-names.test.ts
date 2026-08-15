// Подписи реестра ресурсов против ЕДИНСТВЕННОГО источника имён.
//
// Реестр `shared` подписи ИМПОРТИРУЕТ (`entity-names`), поэтому источник один по
// построению. Эта проба стережёт возвращение литерала рядом с местом показа:
// именно так у обработчика балансировщика и целевой группы в этом файле оказались
// английские `plural` посреди русского интерфейса, при том что `singular` рядом
// был русским — то есть расхождение жило внутри ОДНОЙ записи и глазом не читалось.
//
// Перепись объявлена первой пробой: «расхождений нет» отличимо от «ничего не
// сверялось».

import { REGISTRY } from "./resource-registry";
import { ENTITIES } from "./entity-names";

const named = Object.keys(REGISTRY).filter((key) => key in ENTITIES);

describe("реестр ресурсов — подписи выведены из единственного источника", () => {
  it(`осмотрено: записей реестра ${Object.keys(REGISTRY).length}, из них именующих сущность ${named.length}`, () => {
    expect(named.length).toBeGreaterThanOrEqual(20);
  });

  it("singular и plural записи совпадают с каноном", () => {
    const divergent: string[] = [];
    for (const key of named) {
      const canon = ENTITIES[key as keyof typeof ENTITIES];
      const spec = REGISTRY[key];
      if (spec.singular !== canon.singular) {
        divergent.push(
          `${key}.singular: «${spec.singular}», канон «${canon.singular}»`,
        );
      }
      if (spec.plural !== canon.plural) {
        divergent.push(
          `${key}.plural: «${spec.plural}», канон «${canon.plural}»`,
        );
      }
    }
    expect(divergent).toEqual([]);
  });
});
