// Логические свойства балансировщика в «Обзоре» — названы СЛЕДСТВИЕМ, а не «Да».
//
// Предмет — правило 6 `ui.md`: «„Да“ рядом с подписью „Защита от удаления“ не
// говорит ни что защита включена, ни что удалить нельзя — читатель достраивает
// смысл сам». Эта страница показывала ровно такую пару, причём подпись
// «Защита от удаления» стоит в правиле дословным примером нарушения.
//
// Почему это чинится вместе с #405, а не «когда-нибудь»: общий словарь форматов
// логического варианта не нёс, и модуль держал СВОЙ рендер `boolTag(v, "Да",
// "Нет")`. Теперь общий модуль умеет `format:"bool"` и несёт `BoolFact`, то есть
// у собственной копии не осталось предмета — а вместе с предметом уходит и
// причина, по которой домен держал свой файл.
//
// Утверждается ПОКАЗАННЫЙ текст, а не тип узла: проба про то, что прочтёт
// человек. Отрицание («нет слова „Да“») стоит только в паре с положительным
// («сказано именно это») — иначе оно зеленело бы на пустой ячейке.

import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { detailExtension, type DescItem } from "./ResourceDetailExtensions";

function itemsFor(data: Record<string, unknown>): DescItem[] {
  const ext = detailExtension("load-balancers");
  if (!ext?.overviewExtra) throw new Error("у load-balancers нет overviewExtra");
  return ext.overviewExtra({ data, projectId: "prj-1", detailBase: "/x", navigate: () => {} });
}

function textOf(value: ReactNode): string {
  return render(<div>{value}</div>).container.textContent ?? "";
}

/** Значение строки «Обзора» по её подписи. Подпись — ReactNode (в неё кладут
 *  ⓘ-подсказку), поэтому сверяется ПОКАЗАННЫМ текстом: `String(<узел>)` дал бы
 *  «[object Object]» для любой подписи, и поиск нашёл бы первую попавшуюся. */
function valueByLabel(data: Record<string, unknown>, label: RegExp): string {
  const item = itemsFor(data).find((i) => label.test(textOf(i.label)));
  if (!item) throw new Error(`строки ${label} нет в обзоре балансировщика`);
  return textOf(item.value);
}

/** Балансировщик, у которого обе логические строки показываются: размещение не
 *  зональное (иначе строка межзональной балансировки не выводится вовсе). */
const LB = {
  id: "lb-000000000000000",
  region_id: "ru-central1",
  type: "EXTERNAL",
  status: "ACTIVE",
} as Record<string, unknown>;

describe("логические свойства балансировщика названы следствием", () => {
  it("защита включена — сказано, что удаление ЗАПРЕЩЕНО", () => {
    const v = valueByLabel({ ...LB, deletion_protection: true }, /защита от удаления/i);
    expect(v).toMatch(/удаление запрещено/i);
    expect(v).not.toMatch(/^\s*Да\s*$/);
  });

  it("защита выключена — сказано, что удаление РАЗРЕШЕНО, а не «Нет»", () => {
    // Ложь у булева поля — самостоятельное утверждение о ресурсе, и её тоже
    // надо назвать: «Нет» здесь не сообщает ничего.
    const v = valueByLabel({ ...LB, deletion_protection: false }, /защита от удаления/i);
    expect(v).toMatch(/удаление разрешено/i);
    expect(v).not.toMatch(/^\s*Нет\s*$/);
  });

  it("оба исхода различимы — положительный контроль", () => {
    // Без него отрицания выше выполнялись бы пустой ячейкой.
    const on = valueByLabel({ ...LB, deletion_protection: true }, /защита от удаления/i);
    const off = valueByLabel({ ...LB, deletion_protection: false }, /защита от удаления/i);
    expect(on).not.toBe("");
    expect(off).not.toBe("");
    expect(on).not.toBe(off);
  });

  it("межзональная балансировка тоже названа следствием, а не «Да»/«Нет»", () => {
    const on = valueByLabel({ ...LB, cross_zone_enabled: true }, /между зонами/i);
    const off = valueByLabel({ ...LB, cross_zone_enabled: false }, /между зонами/i);
    expect(on).toMatch(/зон/i);
    expect(off).toMatch(/зон/i);
    expect(on).not.toBe(off);
    for (const v of [on, off]) {
      expect(v).not.toMatch(/^\s*(Да|Нет)\s*$/);
    }
  });
});
