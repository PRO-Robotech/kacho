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
import { hasMorePages, mergeCursorPages, type CursorPage } from "./cursor-pages";
import type { ResourceSpec } from "./resource-registry";

/**
 * useResourceList — поллит GET <apiPath>?<filterField>=<filterValue> каждые 3 сек
 * и следует курсору по запросу.
 *
 * filterField + filterValue — параметр родителя (project_id / account_id).
 * Если оба null — список без фильтра (для cluster-scoped ресурсов).
 * extraQuery — серверные фильтры списка (`spec.listFilters`): уходят в query, то
 * есть применяются ко всему списку, а не к загруженной странице.
 *
 * Форма `data` прежняя — `{ [payloadKey]: rows }`, только rows теперь со всех
 * загруженных страниц; сверху добавлены hasMore / fetchMore / isFetchingMore.
 */
export function useResourceList<T = Record<string, unknown>>(
  spec: ResourceSpec,
  filterField: string | null,
  filterValue: string | null,
  pageSize?: string,
  extraQuery?: Record<string, string>,
) {
  // Значения фильтров обязаны быть в ключе: иначе смена фильтра переиспользует
  // страницы, накопленные для предыдущего набора.
  const extraKey = extraQuery && Object.keys(extraQuery).length > 0 ? JSON.stringify(extraQuery) : null;

  const query = useInfiniteQuery({
    queryKey: [spec.id, "list", filterField, filterValue, pageSize ?? null, extraKey],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const q: Record<string, string> = { ...(extraQuery ?? {}) };
      if (filterField && filterValue) q[filterField] = filterValue;
      if (pageSize) q.pageSize = pageSize;
      if (pageParam) q.pageToken = pageParam as string;
      return api.list<CursorPage>(spec.apiPath, q);
    },
    getNextPageParam: (lastPage) => lastPage.next_page_token || undefined,
    refetchInterval: 3_000,
    enabled: !filterField || !!filterValue,
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
