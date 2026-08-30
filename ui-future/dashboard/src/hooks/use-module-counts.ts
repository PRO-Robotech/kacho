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
    // поллинг остаётся: ЭТО ОСТАТОК РАБОТЫ, а не решение — предмет #1632.
    //
    // Тик этого повторителя читает списки ОДНОГО модуля, но страница заводит по
    // повторителю на каждый из шести, и вместе они читают 14 списочных путей с
    // `pageSize=1000` раз в минуту — НА ВКЛАДКУ. Журнал ведёт 11 из 14 видов
    // (vpc 3 · compute 1 · storage 3 · registry 1 · nlb 3), и по событию их
    // можно было бы не читать вовсе.
    //
    // Мешает не поток, а раскладка модуля: признак покрытия отдаёт
    // `useResourceStream`, а он живёт над react-query, которого в `dashboard`
    // нет ни зависимостью, ни провайдером. Заводить в обход `shared` второй
    // клиент подписки запрещено (решение владельца 2026-08-22, тело #1021),
    // поэтому провязка — своя работа, а не строка здесь. Оставшиеся три вида
    // (аккаунты, проекты, роли) — предмет iam, а журнала у iam нет, и они
    // останутся на опросе при любом исходе #1632.
    //
    // Прежняя правка подняла интервал с 15 с до 60 с, «чтобы снять основную
    // фоновую нагрузку»: подпорка, отодвинувшая цену, а не снявшая её. Здесь
    // она названа вслух, чтобы не читалась как решение.
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
