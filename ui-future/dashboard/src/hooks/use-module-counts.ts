import { useEffect, useState } from "react";
import { apiList } from "../utils";
import type { ServiceModule } from "../lib/service-modules";

export type CountMap = Record<string, number | null>;

export function useModuleCounts(module: ServiceModule, scopeId: string | null, scopeKey = "project_id"): CountMap {
  const [counts, setCounts] = useState<CountMap>(() => makeEmptyCounts(module));
  const enabled = scopeKey === "" || scopeId != null;

  useEffect(() => {
    let cancelled = false;

    async function loadCounts() {
      if (!enabled) {
        setCounts(makeEmptyCounts(module));
        return;
      }

      const next: CountMap = {};
      await Promise.all(
        module.stats.map(async (stat) => {
          const query: Record<string, string> = { pageSize: "1000" };
          if (scopeKey !== "" && scopeId != null) {
            query[scopeKey] = scopeId;
          }

          try {
            const list = await apiList<Record<string, unknown[] | undefined>>(stat.listPath, query);
            next[stat.key] = list[stat.payloadKey]?.length ?? 0;
          } catch {
            next[stat.key] = null;
          }
        }),
      );

      if (!cancelled) {
        setCounts(next);
      }
    }

    void loadCounts();
    // Фоновое обновление редкое: счётчики плиток меняются нечасто, а каждый тик —
    // до 12 списочных запросов (по 1000 элементов). 60с вместо 15с снимает
    // основную фоновую нагрузку/подтормаживание дашборда.
    const timer = window.setInterval(() => {
      void loadCounts();
    }, 60_000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [enabled, module, scopeId, scopeKey]);

  return counts;
}

function makeEmptyCounts(module: ServiceModule): CountMap {
  return Object.fromEntries(module.stats.map((stat) => [stat.key, null]));
}
