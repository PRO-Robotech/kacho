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

/** snake_case поле спеки → camelCase подстановка пути (registry_id → registryId). */
const toPathCamel = (s: string) => s.replace(/_([a-z])/g, (_m, c: string) => c.toUpperCase());

export interface ChildListPathScope {
  /** Значения подстановок пути, названные связью и найденные у родителя. */
  pathParams: Record<string, string>;
  /** Все подстановки пути закрыты — сужает СЕРВЕР, по сегментам. */
  pathScoped: boolean;
}

/**
 * Третий механизм сужения дочернего списка — АДРЕС.
 *
 * Два первых (`relatedListQuery` выше) кладут родителя в запрос. Но часть детей
 * адресуется путём: репозитории лежат под реестром, теги — под репозиторием
 * реестра (`/registry/v1/registries/{registryId}/repositories/{repository}/tags`).
 * Здесь сужает сам адрес, и параметр родителя в запросе не нужен вовсе.
 *
 * Откуда берутся значения — правило одно на все уровни: подстановку закрывает
 * ОДНОИМЁННОЕ поле родительской строки, а если такого поля нет — идентичность
 * родителя (то самое значение, которым он адресован в URL своей карточки).
 * Проверено обоими уровнями registry, и они разные: у реестра есть `id` и нет
 * `registry_id`, у репозитория есть `registry_id` и нет `id` вовсе — его
 * натуральный ключ это пара «реестр + имя». Поле сильнее идентичности, иначе
 * `{registryId}` у тегов закрылся бы именем образа.
 *
 * Идентичность закрывает ПОСЛЕДНЮЮ подстановку пути, а не первую подвернувшуюся:
 * вложенность идёт снаружи внутрь, поэтому непосредственного родителя адресует
 * самый внутренний сегмент, а внешние приходят полями его строки — теми же,
 * которыми сужен он сам. Первая подвернувшаяся дала бы реестру имя образа.
 *
 * Подстановки берутся ТОЛЬКО из тех, что назвала связь (`filterField`): владелец
 * ребёнка объявляет, чем тот сужается, и догадка консоли о недостающем сегменте
 * была бы адресом, которого никто не объявлял.
 *
 * `pathScoped:false` при непустом `pathParams` — законный и важный исход: часть
 * пути заполнить нечем, и вызывающий обязан НЕ отправлять запрос (иначе он
 * уходит с литералом `{repository}` в адресе — ровно тот класс, из-за которого
 * вкладка тегов молчала).
 */
export function childListPathScope(
  apiPath: string,
  filterFields: string[],
  parentRow: Record<string, unknown> | undefined,
  parentId: string,
): ChildListPathScope {
  const placeholders: string[] = apiPath.match(/\{[^}]+\}/g) ?? [];
  if (placeholders.length === 0) return { pathParams: {}, pathScoped: false };

  const pathParams: Record<string, string> = {};
  // Сначала — только собственные поля родителя.
  for (const field of filterFields) {
    if (!placeholders.includes(`{${toPathCamel(field)}}`)) continue;
    const own = parentRow?.[field];
    if (typeof own === "string" && own !== "") pathParams[field] = own;
  }
  // Затем идентичность — и ровно в САМУЮ ВНУТРЕННЮЮ подстановку, если её никто
  // не закрыл. Родитель у ребёнка один, поэтому подстановка от идентичности тоже
  // одна.
  const innermost = placeholders[placeholders.length - 1];
  const innermostField = filterFields.find((f) => `{${toPathCamel(f)}}` === innermost);
  if (parentId && innermostField && !pathParams[innermostField]) pathParams[innermostField] = parentId;

  const filled = placeholders.every((p) => filterFields.some((f) => `{${toPathCamel(f)}}` === p && !!pathParams[f]));
  return { pathParams, pathScoped: filled };
}
