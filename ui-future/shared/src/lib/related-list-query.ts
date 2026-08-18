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

/**
 * snake_case поле спеки → camelCase подстановка пути (registry_id → registryId).
 *
 * Экспортируется, потому что ту же связь между именем поля и именем сегмента
 * читает чтение списка. Своя копия там уже жила: одна и та же связь, объявленная
 * дважды, — то самое «два места об одном предмете», и разошлись бы они на первом
 * же имени, которое кто-то один научится разбирать иначе.
 */
export const toPathCamel = (s: string) => s.replace(/_([a-z])/g, (_m, c: string) => c.toUpperCase());

/**
 * Что такое подстановка адреса — ОДНО объявление на всю консоль.
 *
 * Предикат читают три разных решения: чем сужается дочерний список
 * (`childListPathScope` ниже), уходит ли списочный запрос (`resolveListPath`) и
 * уходит ли запрос КАРТОЧКИ (`ResourceShell`). Каждый держал свою копию
 * выражения, и это ровно тот случай, когда копии расходятся молча: на валидном
 * входе все три согласны, а разойдутся они на форме подстановки, которую кто-то
 * один научится читать.
 *
 * Выражение объявлено глобальным намеренно — им пользуется `pathPlaceholders`;
 * `String.prototype.match` с глобальным выражением состояния не хранит, поэтому
 * общий экземпляр здесь безопасен.
 */
const PATH_PLACEHOLDER = /\{[^}]+\}/g;

/** Подстановки адреса по порядку: `["{registryId}", "{repository}"]`. */
export function pathPlaceholders(apiPath: string): string[] {
  return apiPath.match(PATH_PLACEHOLDER) ?? [];
}

/**
 * В адресе осталась подстановка — значит закрыть её нечем, и запрос по нему
 * отправлять НЕЛЬЗЯ: он уйдёт с литералом `{registryId}` и вернётся отказом,
 * неотличимым от «такого ресурса нет».
 */
export function hasUnresolvedPathSegment(apiPath: string): boolean {
  return pathPlaceholders(apiPath).length > 0;
}

/**
 * Закрыть подстановки адреса ОДНОИМЁННЫМИ параметрами маршрута.
 *
 * Правило одно и без исключений: `{registryId}` закрывает параметр маршрута
 * `registryId`. Никакого отображения «имя в адресе → имя в маршруте» не
 * заводится намеренно — таблица соответствий это второе место об одном предмете,
 * и расходиться она начала бы молча, на ресурсе, который в неё забыли внести.
 * Совпадение имён проверяемо глазом прямо в объявлении маршрута.
 *
 * Ресурс под родителем существует ровно в области родителя: репозиторий — в
 * своём реестре, тег — в своём репозитории этого реестра. Поэтому родителя несёт
 * АДРЕС СТРАНИЦЫ, а не догадка консоли: два репозитория с одним именем в разных
 * реестрах — это два разных ресурса, и различает их только реестр в адресе.
 *
 * Пустое значение заполнением НЕ считается: подставившись, оно дало бы `//` и
 * увело бы запрос по чужому адресу, а охрана незакрытой подстановки при этом
 * замолчала бы — то есть защита выглядела бы исполненной.
 */
export function fillPathFromParams(apiPath: string, params: Record<string, string | undefined>): string {
  let path = apiPath;
  for (const placeholder of pathPlaceholders(apiPath)) {
    const name = placeholder.slice(1, -1);
    // Ключ принимается в ОБЕИХ формах — `registryId` и `registry_id`, — потому
    // что параметры маршрута названы как сегменты (camelCase), а поля спеки —
    // как поля ответа (snake_case). Требовать от вызывающего знать, какую форму
    // ждёт резолвер, значило бы завести второе правило об одном предмете; тот же
    // договор уже у чтения списка.
    const value = params[name] ?? params[Object.keys(params).find((k) => toPathCamel(k) === name) ?? ""];
    if (typeof value === "string" && value !== "") path = path.split(placeholder).join(value);
  }
  return path;
}

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
  const placeholders = pathPlaceholders(apiPath);
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
