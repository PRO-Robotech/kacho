// Право ресурса на столбец действий — предикат ЗАКРЫТЫЙ (#1081).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Столбец действий обязан появляться от НАЛИЧИЯ действия у ресурса, а не от
// отсутствия его имени в закрытом списке. Прежняя редакция читалась наоборот:
// среди слагаемых стояло `!MOVE_INCAPABLE.includes(spec.id)` — то есть меню
// получал всякий, кого не внесли в перечень «перемещать нечем».
//
// Умолчание, разрешающее всё, ошибается молча и всегда в одну сторону: новый
// справочник, о котором забыли, получает столбец с единственным пунктом
// «Переместить» — окном-заглушкой, печатающим REST-вызов, какого у ресурса нет
// ни на контракте, ни на крае. Продукт предлагает действие, которого не
// существует.
//
// Тот же класс, что пустой список доверенных отправителей: перечень, задающий
// «кому запрещено», а не «кому разрешено», на пустом месте разрешает всем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
//	A. ОБЕ СТОРОНЫ. Справочник без единого действия столбца не получает;
//	   ресурс с удалением, с правкой либо с объявленным глаголом — получает.
//	B. ЗАКРЫТОСТЬ. Перепись по всему реестру: у КАЖДОЙ спеки право на меню
//	   совпадает с наличием действия. Спека, которой этот файл не знает,
//	   получает меню только от своего состава, а не от умолчания.
//	C. ПЕРЕЧЕНЬ ИСКЛЮЧЕНИЙ НЕ РАСТЁТ МОЛЧА. Он выписан здесь дословно: новая
//	   запись роняет пробу и обязана прийти с причиной.
//
// `MOVE_INCAPABLE` остаётся — им решается пункт меню «Переместить». Источником
// ПРАВА НА СТОЛБЕЦ он быть перестал, и это разные вопросы.

import { REGISTRY } from "@shared/lib/resource-registry";
import { resourceHasRowActions } from "./RowActionsMenu";
import type { ResourceSpec } from "@shared/lib/resource-registry";

/** Действие у спеки есть ⇔ правка, удаление или объявленный глагол строки. */
function carriesAction(spec: ResourceSpec): boolean {
  return spec.ops.update || spec.ops.delete || (spec.rowVerbs?.length ?? 0) > 0;
}

// Исключения — поимённо и с причиной. Пусто было бы лучше всего; пока список
// непуст, каждая запись объясняет, ЧТО она добавляет к меню сверх состава спеки.
const NAMED_EXCEPTIONS: Record<string, string> = {
  // Меню сети несёт «Создать подсеть» — пункт, которого нет ни в `ops`, ни в
  // `rowVerbs`: он собирается из id ресурса самим меню.
  networks: "меню несёт «Создать подсеть»",
};

describe("resourceHasRowActions — предикат закрыт", () => {
  it("справочник без единого действия столбца не получает", () => {
    // Живой список консоли: маршрутизируется модулем compute и стоит в наборе
    // сквозных проб. Его меню состояло из «Просмотр» (то же, что даёт ссылка в
    // колонке идентичности) и заглушки «Переместить».
    const catalog = REGISTRY["machine-types"];
    expect(catalog.ops).toEqual({ create: false, update: false, delete: false });
    expect(catalog.rowVerbs ?? []).toHaveLength(0);
    expect(resourceHasRowActions(catalog)).toBe(false);
  });

  it("ресурс с удалением — получает", () => {
    // Парный положительный контроль: без него утверждение выше зеленело бы на
    // предикате, отвечающем «нет» всем подряд.
    expect(REGISTRY["networks"].ops.delete).toBe(true);
    expect(resourceHasRowActions(REGISTRY["networks"])).toBe(true);
  });

  it("ресурс, которого реестр не знает, права на меню от умолчания не получает", () => {
    // Инъекция: имени нет ни в одном закрытом перечне дерева. При открытом по
    // умолчанию предикате такая спека получала меню — именно так его получали
    // справочники, о которых забыли.
    const unknown = {
      ...REGISTRY["machine-types"],
      id: "some-catalog-nobody-listed",
      ops: { create: false, update: false, delete: false },
      rowVerbs: undefined,
    } as ResourceSpec;
    expect(resourceHasRowActions(unknown)).toBe(false);
  });

  it("тот же незнакомый ресурс С удалением — получает", () => {
    // Законный близнец инъекции выше: отличается ровно тем, чьё наличие
    // предикат и обязан читать.
    const unknown = {
      ...REGISTRY["machine-types"],
      id: "some-catalog-nobody-listed",
      ops: { create: false, update: false, delete: true },
      rowVerbs: undefined,
    } as ResourceSpec;
    expect(resourceHasRowActions(unknown)).toBe(true);
  });

  it("перечень исключений не растёт молча", () => {
    // Пункт C. Запись без причины здесь невозможна by construction: перечень —
    // отображение «имя → причина», и новая запись роняет утверждение ниже,
    // пока её причину не написали.
    expect(Object.keys(NAMED_EXCEPTIONS)).toEqual(["networks"]);
    for (const id of Object.keys(NAMED_EXCEPTIONS)) {
      expect({ id, hasActions: resourceHasRowActions(REGISTRY[id]) }).toEqual({ id, hasActions: true });
      expect(NAMED_EXCEPTIONS[id].length).toBeGreaterThan(0);
    }
  });

  it("перепись: право на меню у КАЖДОЙ спеки совпадает с наличием действия", () => {
    const ids = Object.keys(REGISTRY);
    // Пустой обход — не «ноль находок», а «ноль прочитанного»: перепись по
    // исчезнувшему реестру зеленела бы при любом предикате.
    expect(ids.length).toBeGreaterThan(0);

    const disagree: string[] = [];
    let withMenu = 0;
    let withoutAction = 0;
    for (const id of ids) {
      const spec = REGISTRY[id];
      const expected = carriesAction(spec) || id in NAMED_EXCEPTIONS;
      const actual = resourceHasRowActions(spec);
      if (actual) withMenu++;
      if (!carriesAction(spec)) withoutAction++;
      if (actual !== expected) {
        disagree.push(
          `${id}: меню=${actual}, действие=${carriesAction(spec)}, исключение=${id in NAMED_EXCEPTIONS}`,
        );
      }
    }
    // Ключи латиницей: имя в коде — только латиницей (иначе омоглиф делает имя
    // невыбираемым), а текст читателю несут значения и сообщения.
    expect({ inspected: ids.length, disagreements: disagree }).toEqual({
      inspected: ids.length,
      disagreements: [],
    });
    // Объём осмотренного печатается как часть утверждения, а не комментарием:
    // «ноль расхождений» обязано быть отличимо от «ноль прочитанного».
    expect(withMenu).toBeGreaterThan(0);
    expect(withoutAction).toBeGreaterThan(0);
  });
});
