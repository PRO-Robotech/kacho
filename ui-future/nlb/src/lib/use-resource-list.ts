// Polling hook для получения списка ресурсов через REST GET.
// spec.apiPath = полный path: /iam/v1/projects, /vpc/v1/networks и т.д.

import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { ResourceSpec } from "./resource-registry";

/**
 * useResourceList — поллит GET <apiPath>?<filterField>=<filterValue> каждые 3 сек.
 *
 * filterField + filterValue — параметр родителя (project_id / account_id).
 * Если оба null — список без фильтра (для cluster-scoped ресурсов).
 */
export function useResourceList<T = Record<string, unknown>>(
  spec: ResourceSpec,
  filterField: string | null,
  filterValue: string | null,
) {
  return useQuery({
    queryKey: [spec.id, "list", filterField, filterValue],
    queryFn: () => {
      const q: Record<string, string> = {};
      if (filterField && filterValue) q[filterField] = filterValue;
      return api.list<Record<string, T[]>>(spec.apiPath, q);
    },
    refetchInterval: 3_000,
    enabled: !filterField || !!filterValue,
    staleTime: 0,
  });
}
