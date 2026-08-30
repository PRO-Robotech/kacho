// Чтение списка ресурса через REST GET, страницами по курсору.
//
// List отдаёт ОДНУ страницу плюс непрозрачный next_page_token; общего числа нет.
// Раньше хук брал только первый ответ — всё, что за курсором, оставалось
// невидимым, а счётчик рядом с заголовком читался как размер списка. Теперь
// страницы накапливаются, «Показать ещё» тянет следующую, а первая продолжает
// поллиться.
//
// spec.apiPath = полный path: /iam/v1/projects, /vpc/v1/networks и т.д.

import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { useResourceStream } from "./subscription/use-resource-stream";
import { hasMorePages, mergeCursorPages, type CursorPage } from "./cursor-pages";
import { hasUnresolvedPathSegment, toPathCamel } from "./related-list-query";
import type { ResourceSpec } from "./resource-registry";

export interface ResolvedListPath {
  path: string;
  query: Record<string, string>;
  /** False while the path still carries a placeholder nobody has filled. */
  resolved: boolean;
}

/**
 * Именованные значения подстановок пути: `{ registryId: "reg-1", repository: "nginx" }`.
 *
 * Ключ принимается в обеих формах — `registry_id` и `registryId`, — потому что
 * спека называет поля родителя в snake_case, а путь пишется в camelCase.
 * Требовать от вызывающего знать, какую именно форму ждёт резолвер, значило бы
 * завести второе правило об одном предмете; расходиться они начали бы молча.
 */
export type PathParams = Record<string, string | null | undefined>;

/**
 * Where the parent id goes: into the path or into the query.
 *
 * Child collections are addressed by path (`/registry/v1/registries/{registryId}/repositories`)
 * and the backend scopes them by that segment. Appending the parent to the query
 * instead would leave the `{registryId}` literal in the URL, and the request
 * would go out as written. While any placeholder is still unfilled the request
 * must not be issued at all — the parent simply is not known yet.
 *
 * Подстановок в пути бывает БОЛЬШЕ ОДНОЙ: теги репозитория адресуются
 * `/registry/v1/registries/{registryId}/repositories/{repository}/tags`. Одного
 * родительского идентификатора такому пути не хватает by construction, поэтому
 * резолвер, умеющий ровно одну подстановку, отвечает на него `resolved:false`
 * НАВСЕГДА — и вкладка молчит не потому, что ответ пуст, а потому, что запрос не
 * уходит вовсе. Именованные значения (`pathParams`) закрывают этот случай, не
 * ослабляя охранное свойство: незаполненная подстановка по-прежнему запрещает
 * запрос, а пустая строка заполнением НЕ считается — подставившись, она дала бы
 * `//` и увела бы запрос по чужому адресу.
 */
export function resolveListPath(
  apiPath: string,
  filterField: string | null,
  filterValue: string | null,
  pathParams?: PathParams,
): ResolvedListPath {
  let path = apiPath;
  const query: Record<string, string> = {};

  // Именованные значения — только в ПУТЬ. В query они не уезжают: лишний
  // параметр, которого сервер не объявлял, сузил бы список молча.
  if (pathParams) {
    for (const [key, raw] of Object.entries(pathParams)) {
      if (raw == null || raw === "") continue;
      const placeholder = `{${toPathCamel(key)}}`;
      if (path.includes(placeholder)) path = path.split(placeholder).join(raw);
    }
  }

  if (filterField && filterValue) {
    const placeholder = `{${toPathCamel(filterField)}}`;
    if (path.includes(placeholder)) path = path.split(placeholder).join(filterValue);
    else query[filterField] = filterValue;
  }
  return { path, query, resolved: !hasUnresolvedPathSegment(path) };
}

/**
 * Предел числа страниц у дочитывания. Сервер обязан когда-нибудь отдать пустой
 * курсор, но «обязан» — это не «всегда»: без предела неисправный или
 * зацикленный курсор дал бы бесконечный цикл ВНУТРИ запроса, то есть зависание
 * без единого падающего утверждения.
 */
export const MAX_PAGES_READ_AT_ONCE = 50;

/**
 * Дочитать список до конца, следуя курсору, и вернуть ОДНУ страницу со всеми
 * строками.
 *
 * Зачем это внутри запроса, а не в эффекте: эффект, зовущий продолжение на
 * каждый ответ, вызывает себя же — бесконечный рендер, который в этой консоли
 * дважды убивал прогон по памяти, не оставив вердикта ни одной пробе. Здесь
 * цикл конечен и живёт там, где его видно.
 *
 * Строки склеиваются тем же `mergeCursorPages`, что и постраничное чтение, —
 * один кодек на оба пути. Второй склеиватель разошёлся бы с первым молча, и
 * разошёлся бы ровно там, где расхождение не видно: на валидном входе оба
 * согласны.
 *
 * Если предел исчерпан, курсор последней страницы СОХРАНЯЕТСЯ: оборванное
 * чтение не вправе выдавать себя за дочитанное — иначе вызывающий скажет «это
 * весь список» про его часть.
 */
export async function readAllPages(
  fetchPage: (pageToken: string) => Promise<CursorPage>,
  payloadKey: string,
  maxPages: number = MAX_PAGES_READ_AT_ONCE,
): Promise<CursorPage> {
  const pages: CursorPage[] = [];
  let token = "";
  for (let i = 0; i < maxPages; i++) {
    const page = await fetchPage(token);
    pages.push(page);
    const next = page.next_page_token;
    if (!next) break;
    token = next;
  }
  const last = pages[pages.length - 1];
  const unfinished = last?.next_page_token;
  return {
    [payloadKey]: mergeCursorPages(pages, payloadKey),
    ...(unfinished ? { next_page_token: unfinished } : {}),
  };
}

/**
 * useResourceList — читает GET <apiPath>?<filterField>=<filterValue> и следует
 * курсору по запросу.
 *
 * ЧИТАЕТ ПО СОБЫТИЮ, А ОПРАШИВАЕТ ТОЛЬКО ПОКА СОБЫТИЙ НЕТ (#1021). Владелец
 * журнала объявил вид — опрос выключается, и список перечитывается на каждое
 * изменение. Владельца нет (домены без журнала), поток не открылся (возможность
 * не включена посадкой) или отказал — работает прежний опрос раз в три секунды.
 *
 * filterField + filterValue — параметр родителя (project_id / account_id).
 * Если оба null — список без фильтра (для cluster-scoped ресурсов).
 * extraQuery — серверные фильтры списка (`spec.listFilters`): уходят в query, то
 * есть применяются ко всему списку, а не к загруженной странице.
 *
 * Форма `data` прежняя — `{ [payloadKey]: rows }`, только rows теперь со всех
 * загруженных страниц; сверху добавлены hasMore / fetchMore / isFetchingMore.
 *
 * opts.pathParams — именованные значения подстановок пути для детей, адресуемых
 * НЕСКОЛЬКИМИ сегментами (теги репозитория). opts.loadAllPages — дочитать курсор
 * до конца в одном запросе (`spec.loadAllPages`): нужно там, где по набору судит
 * клиент (фасетный фильтр), потому что судить по первой странице значит отвечать
 * про набор, которого не читал.
 */
export interface UseResourceListOptions {
  pathParams?: PathParams;
  loadAllPages?: boolean;
}

export function useResourceList<T = Record<string, unknown>>(
  spec: ResourceSpec,
  filterField: string | null,
  filterValue: string | null,
  pageSize?: string,
  extraQuery?: Record<string, string>,
  opts?: UseResourceListOptions,
) {
  // Значения фильтров обязаны быть в ключе: иначе смена фильтра переиспользует
  // страницы, накопленные для предыдущего набора.
  const extraKey = extraQuery && Object.keys(extraQuery).length > 0 ? JSON.stringify(extraQuery) : null;
  const target = resolveListPath(spec.apiPath, filterField, filterValue, opts?.pathParams);
  const readAll = opts?.loadAllPages === true;

  // ПОТОК ВМЕСТО ОПРОСА — ТАМ, ГДЕ ВЛАДЕЛЕЦ САМ НАЗВАЛ ЭТОТ ВИД (#1021).
  //
  // Хук отдаёт один признак: покрыт ли вид потоком ПРЯМО СЕЙЧАС. Пока не
  // покрыт — работает опрос, ровно как раньше; покрыт — опрос выключается, а
  // страница перечитывает список ПО СОБЫТИЮ. Двух механизмов одновременно не
  // бывает by construction: включает и выключает их ОДНО значение.
  //
  // Проект берётся из оси родителя, и только когда она и есть проект: у
  // ресурса, адресованного другим родителем (теги репозитория, дети реестра),
  // проект неизвестен, и подписка открывается без него — незаданная ось не
  // сужает, а сужение по правам остаётся построчным у владельца.
  const { streamed } = useResourceStream({
    specId: spec.id,
    projectId: filterField === "project_id" ? filterValue : null,
    invalidate: [spec.id, "list"],
    // Пока путь несёт незаполненную подстановку, чтения нет вовсе — значит и
    // перечитывать нечего, и поток открывать не за чем.
    enabled: target.resolved,
  });

  const query = useInfiniteQuery({
    // Разрешённый путь — часть ключа: у детей, адресуемых путём, фильтр и его
    // значение одинаковы (их нет вовсе), и без пути две РАЗНЫЕ вкладки —
    // теги двух репозиториев — делили бы один кэш, показывая чужие строки.
    queryKey: [spec.id, "list", filterField, filterValue, pageSize ?? null, extraKey, target.path, readAll],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const q: Record<string, string> = { ...(extraQuery ?? {}), ...target.query };
      if (pageSize) q.pageSize = pageSize;
      const fetchPage = (token: string) =>
        api.list<CursorPage>(target.path, token ? { ...q, pageToken: token } : q);
      // Дочитывание — тем же запросом, а не вторым хуком: два пути чтения
      // разошлись бы молча (см. `readAllPages`).
      if (readAll) return readAllPages(fetchPage, spec.payloadKey);
      return fetchPage(typeof pageParam === "string" ? pageParam : "");
    },
    getNextPageParam: (lastPage) => lastPage.next_page_token || undefined,
    refetchInterval: streamed ? false : 3_000,
    // An unfilled path placeholder means the parent is not known yet; issuing
    // the request would spend every poll on an InvalidArgument.
    enabled: (!filterField || !!filterValue) && target.resolved,
    staleTime: 0,
  });

  const pages = query.data?.pages;
  const rows = mergeCursorPages<T>(pages, spec.payloadKey);

  return {
    ...query,
    // Совместимо с прежней формой ответа — вызывающие читают data[payloadKey].
    data: query.data ? ({ [spec.payloadKey]: rows } as Record<string, T[]>) : undefined,
    rows,
    hasMore: hasMorePages(pages),
    fetchMore: query.fetchNextPage,
    isFetchingMore: query.isFetchingNextPage,
  };
}
