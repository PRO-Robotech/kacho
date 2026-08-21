// Как узнать черту формы в отрисованном дереве — ОДНО объявление на все пробы.
//
// Черта отделяет общие поля формы («как назвать») от полей самого ресурса («чем
// это будет») и стоит на КАЖДОЙ форме, у которой есть и те и другие — решение
// владельца о едином порядке полей (канон консоли, правило 8).
//
// Признак берётся ИЗ объявления стиля (`FORM_DIVIDER_STYLE`), а не выписывается
// здесь литералами. Это не удобство: пробы обязаны краснеть на форме, которая
// рисует СВОЮ черту мимо объявления, — а выписанный литерал такую черту
// засчитал бы, пока числа в ней случайно совпадают. Ровно этот случай и был в
// дереве: общее тело формы рисовало дословную копию объявления, и расхождения
// не было видно вовсе, потому что копия совпадала побайтово.
//
// Сверяются три свойства, а не все: `margin` браузер нормализует («4px 0 16px»
// → «4px 0px 16px»), и сравнение с сырым объявлением падало бы на нормализации,
// а не на предмете.
import { FORM_DIVIDER_STYLE } from "@shared/components/organisms/form/editor-surface";

const height =
  typeof FORM_DIVIDER_STYLE.height === "number" ? `${FORM_DIVIDER_STYLE.height}px` : String(FORM_DIVIDER_STYLE.height);

/** Похож ли узел на черту формы, объявленную продуктом. */
export function isFormDivider(el: Element): boolean {
  const s = (el as HTMLElement).style;
  if (!s) return false;
  return (
    s.gridColumn === FORM_DIVIDER_STYLE.gridColumn &&
    s.background === FORM_DIVIDER_STYLE.background &&
    s.height === height
  );
}

/** Черты формы в порядке DOM. Пусто — черты нет; это тоже вердикт. */
export function formDividers(root: ParentNode = document.body): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>("*")].filter(isFormDivider);
}
