import type { RelatedSpec } from "./resource-spec";

/**
 * Параметры запроса, которыми вкладка дочернего ресурса просит СЕРВЕР сузить
 * список по родителю. `undefined` — сужать некому, вкладка спрашивает список
 * в области родителя и сужает страницу сама.
 *
 * Механизмов ровно два, и выбирает между ними ВЛАДЕЛЕЦ ребёнка, а не консоль:
 *
 *   · выражение `filter` — разбирается по белому списку имён КОЛОНОК владельца
 *     (`serverFilterField`);
 *   · типизированное поле списочного запроса — отдельный параметр
 *     (`serverParamField`), для ссылок, которых у владельца отдельной колонкой
 *     нет: у адреса подсеть лежит внутри jsonb и в двух семьях, поэтому
 *     выражением она не выражается вовсе.
 *
 * Оба сразу объявить нельзя — это запрещено типом `RelatedSpec`, поэтому здесь
 * нет ветки «кто победил»: побеждать некому.
 */
export function relatedListQuery(
  related: Pick<RelatedSpec, "serverFilterField" | "serverParamField">,
  parentId: string,
): Record<string, string> | undefined {
  if (!parentId) return undefined;
  if (related.serverParamField) return { [related.serverParamField]: parentId };
  if (related.serverFilterField) return { filter: `${related.serverFilterField}="${parentId}"` };
  return undefined;
}
