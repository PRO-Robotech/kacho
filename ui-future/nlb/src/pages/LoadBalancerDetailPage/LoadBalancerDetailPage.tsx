// LoadBalancerDetailPage — доменная доводка generic ResourceShell для
// балансировщика нагрузки. Переиспользует единый 3-зонный layout (fixed-rail +
// isolated-scroll DetailShell) и registry-driven контент — bespoke только вкладка
// «Целевые группы»:
//   1) «Обзор» — единая таблица (регион / схема / размещение / VIP / session
//      affinity / статус / защита от удаления) через DETAIL_EXTENSIONS;
//   2) «Целевые группы» — ПРОИЗВОДНОЕ представление: какие группы обслуживает
//      балансировщик, видно по его листенерам (`Listener.target_group_id`).
//      Привязка меняется НА ЛИСТЕНЕРЕ — отсюда только чтение и переход;
//   3) «Листенеры» — registry-driven связанный таб (spec.related, filterField
//      load_balancer_id) с auto-CTA «Создать листенер»;
//   4) «Операции» / «JSON» — из generic ResourceShell.
//
// Почему вкладка только читает. Прежде она привязывала и отвязывала группы
// глаголами `:attachTargetGroup` / `:detachTargetGroup`, а список брала из
// снимка `attached_target_groups` в ответе Get(LB). Ни глаголов, ни поля в
// контракте НЕТ: M:N-привязка снята, целевая группа авторитетно живёт на
// листенере. То есть список был всегда пуст, а обе кнопки уходили в 404.

import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Input, Space, Typography } from "antd";
import { ResourceShell, type ResourceShellMode } from "@/components/organisms/ResourceShell";
import { ResourceTable, type Column } from "@/components/organisms/ResourceTable";
import type { DetailTab } from "@/components/organisms/DetailShell";
import type { DetailExtCtx } from "@/components/organisms/ResourceDetailExtensions";
import { api } from "@/api/client";
import { targetGroupWiring, type TargetGroupWiring } from "@/api/resources";
import { REGISTRY, getByPath } from "@/lib/resource-registry";
import { buildSpecColumns } from "@/lib/spec-columns";

const LB_SPEC = REGISTRY["load-balancers"];
const TG_SPEC = REGISTRY["target-groups"];
const LISTENER_SPEC = REGISTRY["listeners"];

// LbTargetGroupsTab — вкладка «Целевые группы»: производный от листенеров
// список. Столбец «Через листенер» называет ТУ САМУЮ запись, правкой которой
// привязка меняется, — иначе представление сообщало бы связь, но умалчивало,
// где её редактировать.
function LbTargetGroupsTab({ lbId, projectId }: { lbId: string; projectId: string | null }) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");

  // Листенеры проекта → свои (фильтр по load_balancer_id, как у связанного
  // таба) → из них выводим привязанные группы.
  const listeners = useQuery({
    queryKey: ["listeners", "by-lb", projectId, lbId],
    queryFn: () =>
      api.list<{ listeners: Record<string, unknown>[] }>(LISTENER_SPEC.apiPath, {
        project_id: projectId ?? "",
        pageSize: "500",
      }),
    enabled: !!projectId,
    refetchInterval: 5000,
  });

  // Полный список TG проекта — резолвим полные объекты для табличных колонок
  // (листенер несёт только id группы).
  const groups = useQuery({
    queryKey: ["target-groups", "by-lb", projectId, lbId],
    queryFn: () =>
      api.list<{ target_groups: Record<string, unknown>[] }>(TG_SPEC.apiPath, {
        project_id: projectId ?? "",
        pageSize: "500",
      }),
    enabled: !!projectId,
    refetchInterval: 5000,
  });

  const wiring = useMemo<TargetGroupWiring[]>(() => {
    const own = (listeners.data?.listeners ?? []).filter((r) => getByPath<string>(r, "load_balancer_id") === lbId);
    return targetGroupWiring(own);
  }, [listeners.data, lbId]);

  const wiredBy = useMemo(() => new Map(wiring.map((w) => [w.targetGroupId, w.listeners])), [wiring]);

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
    const cols = buildSpecColumns(TG_SPEC, { projectId: projectId ?? undefined });
    cols.push({
      header: "Через листенер",
      className: "whitespace-nowrap",
      cell: (row) => {
        const wired = wiredBy.get(getByPath<string>(row, "id") ?? "") ?? [];
        return (
          <Space size={4} wrap>
            {wired.map((l) => (
              <Typography.Link
                key={l.id}
                onClick={(e) => {
                  e.stopPropagation();
                  if (projectId) navigate(`/projects/${projectId}/nlb/listeners/${l.id}`);
                }}
              >
                {l.name}
              </Typography.Link>
            ))}
          </Space>
        );
      },
    });
    return cols;
  }, [projectId, wiredBy, navigate]);

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <div style={{ display: "flex", gap: 8, alignItems: "flex-start", flexWrap: "wrap" }}>
        <Input.Search
          placeholder="Фильтр по имени или идентификатору"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          style={{ width: 320 }}
          allowClear
        />
      </div>
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          {query
            ? "По фильтру ничего не найдено."
            : "Ни один листенер этого балансировщика не направляет трафик в целевую группу. Группа задаётся на листенере."}
        </div>
      ) : (
        <ResourceTable
          rows={filtered}
          columns={columns}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          onRowClick={(r) => {
            const id = getByPath<string>(r, "id");
            if (id && projectId) navigate(`/projects/${projectId}/nlb/target-groups/${id}`);
          }}
        />
      )}
    </Space>
  );
}

interface Props {
  mode?: ResourceShellMode;
}

export function LoadBalancerDetailPage({ mode }: Props) {
  // Bespoke вкладка «Целевые группы» — связь выводится через листенеры и потому
  // не выражается одним filterField; «Листенеры» — обычный registry-related таб
  // (spec.related).
  const extraTabs = (ctx: DetailExtCtx): DetailTab[] => {
    const lbId = getByPath<string>(ctx.data, "id") ?? "";
    return [
      {
        id: "target-groups",
        label: "Целевые группы",
        render: () => <LbTargetGroupsTab lbId={lbId} projectId={ctx.projectId} />,
      },
    ];
  };

  return <ResourceShell spec={LB_SPEC} mode={mode} extraTabs={extraTabs} />;
}
