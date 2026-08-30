import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useStreamCoverage } from "@shared/hooks/use-stream-coverage";
import { apiList } from "../utils";
import type { ServiceModule } from "../lib/service-modules";
import { subscriptionHub, type SubscriptionHub } from "@shared/lib/subscription/hub";

export type CountMap = Record<string, number | null>;

/**
 * Счётчики плитки витрины.
 *
 * ---------------------------------------------------------------------------
 * ЧТО ЗДЕСЬ ЧИТАЕТСЯ И ПО ЧЬЕЙ КОМАНДЕ
 *
 * Величину счётчика событие потока НЕ НЕСЁТ — оно говорит об одном предмете, а
 * не о числе строк. Поэтому поток здесь не заменяет чтение, а НАЗНАЧАЕТ ЕГО
 * МОМЕНТ: пока владелец молчит, читать нечего, и списки не читаются вовсе.
 * Прежде вместо этого стоял повторитель на 60 с, и шесть его копий вместе
 * читали четырнадцать списочных путей по тысяче элементов НА ВКЛАДКУ —
 * постоянная нагрузка, не зависящая от того, менялось ли что-нибудь (#1632).
 *
 * ---------------------------------------------------------------------------
 * ПОЧЕМУ ХАБ БЕРЁТСЯ ИЗ `@shared`, А НЕ ЗАВОДИТСЯ ЗДЕСЬ
 *
 * Клиент потока в консоли ОДИН (решение владельца 2026-08-22, тело #1021):
 * второй разошёлся бы с первым в возобновлении с позиции и в разборе покрытия —
 * и разошёлся бы там, где расхождение читается как «изменений не было». Этот
 * модуль живёт без react-query, поэтому `useResourceStream` ему не годится (тот
 * перечитывает ключ запроса через `queryClient`), но САМ ХАБ от react-query не
 * зависит вовсе — и признак покрытия отдаёт `useStreamCoverage`.
 *
 * Здесь стояло, что провязка невозможна без своего решения над react-query.
 * Это оказалось верно лишь для хука: у хаба зависимостей нет ни одной.
 */
export function useModuleCounts(
  module: ServiceModule,
  scopeId: string | null,
  scopeKey = "project_id",
  hub: SubscriptionHub = subscriptionHub(),
): CountMap {
  const enabled = scopeKey === "" || scopeId != null;

  // Прочитанное хранится ВМЕСТЕ С ОБЛАСТЬЮ, для которой прочитано, а не голым
  // отображением. Голое пришлось бы гасить сбросом состояния прямо в эффекте на
  // каждой смене проекта — а это каскад перерисовок
  // (`react-hooks/set-state-in-effect`). Подпись области отвечает на тот же
  // вопрос без сброса: числа, прочитанные для ПРЕЖНЕГО проекта, с новым не
  // совпадают и его счётчиками прочитаться не могут.
  const scopeSig = `${scopeKey}\u0000${scopeId ?? ""}`;
  const [loaded, setLoaded] = useState<{ sig: string; counts: CountMap } | null>(null);

  // Номер захода, а не флаг отмены: чтения теперь запускает и событие потока,
  // поэтому их бывает несколько сразу, и записать состояние вправе только
  // последнее. Флаг отмены жил внутри одного эффекта и про соседний заход не
  // знал бы ничего — два ответа разошлись бы во времени, и на экран попал бы
  // тот, что пришёл позже, а не тот, что спрошен позже.
  const runRef = useRef(0);

  const loadCounts = useCallback(async () => {
    if (!enabled) return;
    const run = runRef.current + 1;
    runRef.current = run;

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

    if (run === runRef.current) {
      setLoaded({ sig: scopeSig, counts: next });
    }
  }, [enabled, module, scopeId, scopeKey, scopeSig]);

  // Обёртка, ничего не возвращающая: подписчику нужен обработчик, а не обещание.
  // Отдать сюда сам загрузчик значило бы отдать `Promise`, чей отказ никто не
  // читает (`@typescript-eslint/no-misused-promises`), — а отказ здесь штатный:
  // счётчик недоступного списка становится прочерком внутри самого загрузчика.
  const reload = useCallback(() => {
    void loadCounts();
  }, [loadCounts]);

  // Покрытие — по ВСЕМ видам плитки разом: список читается один на плитку,
  // поэтому снять опрос можно только когда покрыт каждый её вид. Плитка, чей
  // домен журнала не ведёт (iam), предмета не даёт вовсе и остаётся на опросе.
  const { streamed } = useStreamCoverage(
    {
      specIds: module.stats.map((stat) => stat.specId ?? null),
      // Подписка ключуется тем же проектом, каким сужается чтение: счётчик,
      // подписанный на чужой проект, обновлялся бы по чужим событиям.
      projectId: scopeKey === "project_id" ? scopeId : null,
      onChanged: reload,
      enabled,
    },
    hub,
  );

  useEffect(() => {
    reload();

    // поллинг остаётся: он гасится ПРИЗНАКОМ ПОКРЫТИЯ, а не снимается совсем, и
    // это разные вещи. Плитка, чей домен журнала не ведёт (iam — аккаунты,
    // проекты, роли), покрытия не получит НИКОГДА, и опрос для неё остаётся
    // единственным способом узнать новое число. Он же возвращается сам, когда
    // поток отказал или владелец не объявлен посадкой: покрытие снимается
    // вместе с каналом, и следующий тик снова читает списки. Снять повторитель
    // насовсем значило бы заморозить счётчики там, где потока нет.
    const timer = window.setInterval(() => {
      if (streamed) return;
      reload();
    }, 60_000);

    return () => {
      window.clearInterval(timer);
    };
  }, [reload, streamed]);

  // Прочерки, пока области нет либо числа прочитаны для ДРУГОЙ области. Отсюда
  // же и отсутствие сброса состояния: несовпадение подписи и есть ответ.
  return useMemo(
    () => (enabled && loaded?.sig === scopeSig ? loaded.counts : makeEmptyCounts(module)),
    [enabled, loaded, scopeSig, module],
  );
}

function makeEmptyCounts(module: ServiceModule): CountMap {
  return Object.fromEntries(module.stats.map((stat) => [stat.key, null]));
}
