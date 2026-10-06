// Подписи хоста против ЕДИНСТВЕННОГО источника имён (`shared/src/lib/entity-names`).
//
// Почему хост сверяется, а не импортирует. Образ хоста собирается из ЕГО дерева:
// его Dockerfile копирует только `host/`, каталога `shared/` в контексте сборки
// нет — значит импорт отсюда сломал бы сборку образа. Поэтому у хоста остаётся
// своя карта, но расхождение с каноном не может приземлиться молча: эта проба
// читает ОБА значения и сравнивает их.
//
// Что она ловит и почему это не косметика: до неё обработчик балансировщика
// назывался четырьмя способами, а виртуальная машина — двумя. Пользователь читает
// разные подписи как разные предметы, а правка подписи доезжает не всюду.
//
// Отрицания без положительного контроля здесь нет: первая проба утверждает объём
// осмотренного (сколько ключей сверено), поэтому «расхождений нет» отличимо от
// «ничего не сверялось».

import { MODULE_LABELS, RESOURCE_LABELS } from "./HostBreadcrumb";
import { ENTITIES, SERVICES } from "../../../../../shared/src/lib/entity-names";
import { SERVICES as HOST_SERVICES } from "../../../lib/entity-names";

const moduleKeys = Object.keys(MODULE_LABELS).filter((k) => k in SERVICES);
const entityKeys = Object.keys(RESOURCE_LABELS).filter((k) => k in ENTITIES);

describe("HostBreadcrumb — подписи выведены из единственного источника", () => {
  it(`осмотрено: разделов ${moduleKeys.length}, сущностей ${entityKeys.length} — перепись непуста`, () => {
    expect(moduleKeys.length).toBeGreaterThanOrEqual(6);
    expect(entityKeys.length).toBeGreaterThanOrEqual(20);
  });

  it("подпись раздела совпадает с каноном", () => {
    const divergent = moduleKeys
      .filter((k) => MODULE_LABELS[k] !== SERVICES[k as keyof typeof SERVICES].title)
      .map((k) => `${k}: у хоста «${MODULE_LABELS[k]}», канон «${SERVICES[k as keyof typeof SERVICES].title}»`);
    expect(divergent).toEqual([]);
  });

  it("зеркало хоста несёт КАЖДЫЙ раздел канона теми же двумя полями", () => {
    // Сверка выше обходит ключи ХОСТА и потому слепа к разделу, который есть в
    // каноне, а в зеркало не доехал: такой раздел в крошке, рейле и на экране
    // отказа хоста остался бы безымянным, а проба — зелёной. Здесь обход идёт
    // по КАНОНУ, и сравниваются оба поля, а не одно `title`.
    const canonKeys = Object.keys(SERVICES);
    const divergent = canonKeys.flatMap((k) => {
      const canon = SERVICES[k as keyof typeof SERVICES];
      const mirror = HOST_SERVICES[k];
      if (!mirror) return [`${k}: в зеркале хоста раздела нет, канон «${canon.title}» / «${canon.menuTitle}»`];
      return mirror.title === canon.title && mirror.menuTitle === canon.menuTitle
        ? []
        : [`${k}: у хоста «${mirror.title}» / «${mirror.menuTitle}», канон «${canon.title}» / «${canon.menuTitle}»`];
    });
    expect(canonKeys.length).toBeGreaterThanOrEqual(9);
    expect(divergent).toEqual([]);
  });

  it("раздел уведомлений назван в каноне одним словом в обоих полях (NTF-6, З14)", () => {
    // Предпосылка сверки выше: исчезни запись из канона — обход по канону её бы
    // просто не увидел, и зеркало осталось бы «согласованным» без раздела.
    expect(SERVICES.notify).toEqual({ title: "Уведомления", menuTitle: "Уведомления" });
    expect(MODULE_LABELS.notify).toBe("Уведомления");
  });

  it("подпись сущности совпадает с каноном", () => {
    const divergent = entityKeys
      .filter((k) => RESOURCE_LABELS[k] !== ENTITIES[k as keyof typeof ENTITIES].plural)
      .map((k) => `${k}: у хоста «${RESOURCE_LABELS[k]}», канон «${ENTITIES[k as keyof typeof ENTITIES].plural}»`);
    expect(divergent).toEqual([]);
  });
});
