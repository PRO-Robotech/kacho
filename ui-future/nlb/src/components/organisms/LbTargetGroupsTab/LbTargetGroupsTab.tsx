// LbTargetGroupsTab — вкладка «Целевые группы» на карточке балансировщика.
//
// ПРОИЗВОДНОЕ представление: какие группы обслуживает балансировщик, видно по
// его листенерам (`Listener.target_group_id`). Привязка меняется НА ЛИСТЕНЕРЕ —
// отсюда только чтение и переход.
//
// Почему вкладка только читает. Прежде она привязывала и отвязывала группы
// глаголами `:attachTargetGroup` / `:detachTargetGroup`, а список брала из
// снимка `attached_target_groups` в ответе Get(LB). Ни глаголов, ни поля в
// контракте НЕТ: M:N-привязка снята, целевая группа авторитетно живёт на
// листенере. То есть список был всегда пуст, а обе кнопки уходили в 404.
//
// Связь идёт ЧЕРЕЗ листенер и потому не выражается одним `filterField` — то
// есть связанным табом реестра (`spec.related`) её не подать. Это и есть
// единственное, что у карточки балансировщика своё; всё остальное — общая
// оболочка `@shared`. Подключается вкладка расширением по spec.id
// (`lib/nlb-detail-extensions`), а не собственной копией оболочки: копия
// отставала бы молча.

import { useMemo, useState } from "react";
import { HeaderSlotPortal } from "@shared/components/organisms/DetailShell";
import { useQuery } from "@tanstack/react-query";
import { useResourceStream } from "@shared/lib/subscription/use-resource-stream";
import { Space } from "antd";
import { ResourceTable, type Column } from "@/components/organisms/ResourceTable";
import { api } from "@/api/client";
import { targetGroupWiring, type TargetGroupWiring } from "@/api/resources";
import { REGISTRY, getByPath } from "@/lib/resource-registry";
import { buildSpecColumns } from "@/lib/spec-columns";
import { ColumnSettings, TableSearch, useHiddenColumns } from "@/components/molecules/TableToolbar";
import { ResourceLink } from "@/components/molecules/ResourceLink";
import { clientScope, noMatchesText, rowsAreComplete, type NarrowingScope } from "@shared/lib/list-scope";

const TG_SPEC = REGISTRY["target-groups"];
const LISTENER_SPEC = REGISTRY["listeners"];

/**
 * Столбец «Через листенер» называет ТУ САМУЮ запись, правкой которой привязка
 * меняется, — иначе представление сообщало бы связь, но умалчивало, где её
 * редактировать.
 */
export function LbTargetGroupsTab({ lbId, projectId }: { lbId: string; projectId: string | null }) {
  const [query, setQuery] = useState("");

  // ЧТЕНИЕ ПО СОБЫТИЮ, ОПРОС — ПОКА СОБЫТИЙ НЕТ (#1021). Признак покрытия свой
  // на КАЖДЫЙ вид: слушатели и группы целей — разные виды словаря владельца, и
  // объявить их одним признаком значило бы снять опрос с того, что владелец не
  // называл.
  const { streamed: listenersStreamed } = useResourceStream({
    specId: "listeners",
    projectId: projectId ?? null,
    invalidate: ["listeners", "by-lb", projectId, lbId],
    enabled: !!projectId,
  });
  const { streamed: targetGroupsStreamed } = useResourceStream({
    specId: "target-groups",
    projectId: projectId ?? null,
    invalidate: ["target-groups", "by-lb", projectId, lbId],
    enabled: !!projectId,
  });

  // Листенеры проекта → свои (фильтр по load_balancer_id, как у связанного
  // таба) → из них выводим привязанные группы.
  const listeners = useQuery({
    queryKey: ["listeners", "by-lb", projectId, lbId],
    queryFn: () =>
      api.list<{ listeners: Record<string, unknown>[]; next_page_token?: string }>(LISTENER_SPEC.apiPath, {
        project_id: projectId ?? "",
        pageSize: "500",
      }),
    enabled: !!projectId,
    refetchInterval: listenersStreamed ? false : 5000,
  });

  // Полный список TG проекта — резолвим полные объекты для табличных колонок
  // (листенер несёт только id группы).
  const groups = useQuery({
    queryKey: ["target-groups", "by-lb", projectId, lbId],
    queryFn: () =>
      api.list<{ target_groups: Record<string, unknown>[]; next_page_token?: string }>(TG_SPEC.apiPath, {
        project_id: projectId ?? "",
        pageSize: "500",
      }),
    enabled: !!projectId,
    refetchInterval: targetGroupsStreamed ? false : 5000,
  });

  const wiring = useMemo<TargetGroupWiring[]>(() => {
    const own = (listeners.data?.listeners ?? []).filter((r) => getByPath<string>(r, "load_balancer_id") === lbId);
    return targetGroupWiring(own);
  }, [listeners.data, lbId]);

  const wiredBy = useMemo(() => new Map(wiring.map((w) => [w.targetGroupId, w.listeners])), [wiring]);

  // Область вкладки (#373). Строки выводятся из ДВУХ одностраничных чтений
  // списка проекта — листенеров и целевых групп, — и продолжения ни у одного
  // из них здесь нет. Достаточно одного усечённого, чтобы вкладка перестала
  // отвечать про весь набор: непрочитанный листенер уносит с собой и свою
  // группу.
  const scope: NarrowingScope = clientScope(!!listeners.data?.next_page_token || !!groups.data?.next_page_token);

  const rows = useMemo(() => {
    const all = groups.data?.target_groups ?? [];
    return all.filter((r) => wiredBy.has(getByPath<string>(r, "id") ?? ""));
  }, [groups.data, wiredBy]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => {
      const nm = (getByPath<string>(r, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(r, "id") ?? "").toLowerCase();
      return nm.includes(q) || id.includes(q);
    });
  }, [rows, query]);

  const columns = useMemo<Column<Record<string, unknown>>[]>(() => {
    // Значка типа у имени НЕТ: вкладка показывает один тип, названный её же
    // ярлыком, и столбец одинаковых значков не различает ни одной строки — так
    // же, как на странице списка целевых групп (канон §9).
    const cols = buildSpecColumns(TG_SPEC, { projectId: projectId ?? undefined });
    cols.push({
      header: "Через листенер",
      className: "whitespace-nowrap",
      cell: (row) => {
        const wired = wiredBy.get(getByPath<string>(row, "id") ?? "") ?? [];
        return (
          <Space size={4} wrap>
            {/* Настоящая ССЫЛКА, а не обработчик клика: здесь стоял
                `Typography.Link` с `onClick` — у него нет адреса, поэтому его
                нельзя открыть в новой вкладке и нельзя скопировать переход, а
                вид ссылки он при этом обещает. Адрес собирает единственный вид
                ссылки консоли, а не рукописный литерал пути. */}
            {wired.map((l) => (
              // Иконка типа — как у всякой ссылки на ресурс в продукте: по ней
              // ссылку узнают до чтения имени. Здесь её не было, и ссылка на
              // листенер отличалась от соседних ссылок той же таблицы.
              <ResourceLink key={l.id} specId="listeners" id={l.id} name={l.name} projectId={projectId} icon plain />
            ))}
          </Space>
        );
      },
    });
    return cols;
  }, [projectId, wiredBy]);

  const [hidden, toggleHidden] = useHiddenColumns("cols:lb-target-groups");
  const shownColumns = useMemo(() => columns.filter((c) => !hidden.has(c.header)), [columns, hidden]);
  const toggleCols = useMemo(
    () => columns.filter((c) => c.header).map((c) => ({ key: c.header, label: c.header })),
    [columns],
  );

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      {/* РУЧКИ ПОДНИМАЮТСЯ В ШАПКУ КАРТОЧКИ, как у связанных вкладок.
          
          Соседняя вкладка того же балансировщика — «Листенеры» — рисуется общей
          оболочкой, и её поиск со «Столбцами» стоят в правом слоте на уровне
          имени ресурса. Здесь тот же ряд стоял ВНУТРИ тела вкладки, и при
          переходе между двумя вкладками одного ресурса ручки перепрыгивали
          сверху вниз, а таблица съезжала на их высоту.
          
          Портал — тот же, которым пользуется общая оболочка: слот один, и
          занимает его та вкладка, которая открыта. */}
      <HeaderSlotPortal>
        <TableSearch value={query} onChange={setQuery} scope={scope} width={320} />
        {/* Где есть фильтр — есть и выбор столбцов (требование владельца). */}
        <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
      </HeaderSlotPortal>
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          {query
            ? noMatchesText(scope)
            : "Ни один листенер этого балансировщика не направляет трафик в целевую группу. Группа задаётся на листенере."}
        </div>
      ) : (
        <ResourceTable
          rows={filtered}
          columns={shownColumns}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          complete={rowsAreComplete(scope)}
        />
      )}
    </Space>
  );
}
