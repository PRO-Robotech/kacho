// OperationsTab — generic список операций (LRO) для конкретного ресурса.
//
// Путь списка компонент НЕ собирает — он приходит готовым, пропом `listPath`.
// Подмаршрут `<apiPath>/{id}/operations` ствол несёт НЕ у всякого ресурса: у
// вложенных ресурсов реестра (репозитории и теги) и у каталога размещения geo
// такого связывания нет вовсе, и склеенный здесь из `spec.apiPath` адрес
// спрашивал бы то, чего нет, — край отвечал отказом, а оператор читал это как
// поломку, а не как «операций у этого ресурса не бывает». У кого подмаршрут
// есть, консоль утверждает в одном месте — `@shared/lib/operations-subroute`,
// сверяемом с деревом proto в обе стороны; решение показать вкладку принимает
// вызывающий, и принять его иначе он не может: без пути вкладки нет.
//
// Фильтры: input по идентификатору + Select по статусу.
// Второго отбора «по исходу» (тумблер Все/С ошибкой/Успешные) здесь НЕТ: он
// говорил о том же предмете грубее — «с ошибкой» это статусы «Ошибка» и
// «Отменена», «успешные» — статус «Выполнена». Два способа задать один отбор
// заставляют угадывать, который из них сейчас действует.
// Колонки — см. OperationsTable.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Input, Select } from "antd";
import { api, ApiError } from "@/api/client";
import { ErrorResult } from "@/components/molecules/ErrorResult";
import { ColumnSettings, useHiddenColumns } from "@/components/molecules/TableToolbar";
import {
  OperationsTable,
  operationColumnTitles,
  type Op,
  statusOf,
  type OperationStatus,
} from "@/components/molecules/OperationsTable";
import { clientScope, narrowingTitle, scopeSuffix } from "@shared/lib/list-scope";
import type { ResourceSpec } from "@/lib/resource-registry";
import { HeaderSlotPortal } from "@/components/organisms/DetailShell";

interface Props {
  spec: ResourceSpec;
  resourceId: string;
  /** Готовый путь ствола — `operationsListPath(spec.apiPath, resourceId)`. */
  listPath: string;
}

const STATUS_OPTIONS: { value: OperationStatus | "all"; label: string }[] = [
  { value: "all", label: "Все статусы" },
  { value: "running", label: "Выполняется" },
  { value: "done", label: "Выполнена" },
  { value: "error", label: "Ошибка" },
  { value: "cancelled", label: "Отменена" },
];

export function OperationsTab({ spec, resourceId, listPath }: Props) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<OperationStatus | "all">("all");
  const [hiddenCols, toggleCol] = useHiddenColumns("cols:" + spec.id + "-operations");

  const { data, isLoading, isError, error } = useQuery({
    queryKey: [spec.id, "operations", resourceId],
    queryFn: () =>
      api.list<{ operations: Op[]; next_page_token?: string }>(listPath, {
        pageSize: "200",
      }),
    enabled: !!resourceId,
    // Поллинг только пока запрос успешен. При ошибке (403 без доступа / 404 / 501
    // не реализовано) поллинг и retry выключаются — иначе endpoint переспрашивается
    // каждые 5с и состояние ошибки мигает.
    refetchInterval: (q) => (q.state.status === "error" ? false : 5_000),
    retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 1,
    staleTime: 0,
  });

  // Хуки — ВСЕГДА до раннего return (Rules of Hooks).
  //
  // Область ручек (#373): читается ОДНА страница, продолжения у вкладки нет,
  // поэтому и отбор по статусу, и поиск по идентификатору судят о прочитанном.
  // Курсор в типе ответа был объявлен и не читался ни разу.
  const scope = clientScope(!!data?.next_page_token);
  const ops = useMemo(() => {
    const raw = data?.operations ?? [];
    return (
      raw
        .map((o) => ({ ...o, resource_id: o.resource_id ?? resourceId }))
        // новое сверху — сортировка по дате создания desc.
        .sort((a, b) => {
          const ta = a.created_at ? Date.parse(a.created_at) : 0;
          const tb = b.created_at ? Date.parse(b.created_at) : 0;
          return tb - ta;
        })
    );
  }, [data, resourceId]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return ops.filter((o) => {
      if (status !== "all" && statusOf(o) !== status) return false;
      if (!q) return true;
      return (o.id ?? "").toLowerCase().includes(q);
    });
  }, [ops, query, status]);

  if (isError) {
    const st = error instanceof ApiError ? error.status : undefined;
    if (st === 403) {
      return (
        <ErrorResult
          error={error}
          status="403"
          title="403"
          subTitle="Недостаточно прав для просмотра операций этого ресурса."
        />
      );
    }
    const httpStatus = st === 501 ? "404" : undefined;
    return (
      <ErrorResult
        error={error}
        status={httpStatus}
        title={httpStatus === "404" ? "404" : undefined}
        subTitle={httpStatus === "404" ? "ListOperations для этого ресурса пока не реализован." : undefined}
      />
    );
  }

  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      {/* Фильтры операций — на уровень имени ресурса (зона 3, правый слот). */}
      <HeaderSlotPortal>
        <Input
          placeholder={`Фильтр по идентификатору ${scopeSuffix(scope)}`}
          title={narrowingTitle(scope)}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          allowClear
          style={{ width: 260 }}
        />
        <Select
          value={status}
          onChange={setStatus}
          options={STATUS_OPTIONS}
          title={narrowingTitle(scope)}
          style={{ width: 180 }}
        />
        {/* Где есть фильтр — есть и выбор столбцов. */}
        <ColumnSettings
          columns={operationColumnTitles().map((t) => ({ key: t, label: t }))}
          hidden={hiddenCols}
          onToggle={toggleCol}
        />
      </HeaderSlotPortal>

      <OperationsTable rows={filtered} loading={isLoading} hiddenColumns={hiddenCols} empty={ops.length > 0 && filtered.length === 0} />
    </div>
  );
}
