// LoadBalancerDetailPage — доменная доводка generic ResourceShell для
// балансировщика нагрузки. Переиспользует единый 3-зонный layout (fixed-rail +
// isolated-scroll DetailShell) и registry-driven контент — bespoke только вкладка
// «Целевые группы»:
//   1) «Обзор» — единая таблица (регион / схема / размещение / VIP / session
//      affinity / статус / защита от удаления) через DETAIL_EXTENSIONS;
//   2) «Целевые группы» — тот же registry-driven список, что и standalone
//      /nlb/target-groups (колонки buildSpecColumns), отфильтрованный по
//      приаттаченным к этому LB; привязка/отвязка — inline verb-RPC
//      :attachTargetGroup (вложенный body) / :detachTargetGroup (плоский body),
//      через api/resources builder'ы (Operation envelope);
//   3) «Листенеры» — registry-driven связанный таб (spec.related, filterField
//      load_balancer_id) с auto-CTA «Создать листенер»;
//   4) «Операции» / «JSON» — из generic ResourceShell.
//
// attached_target_groups приходит в GET-ответе NetworkLoadBalancer (снимок
// pivot-таблицы) — из него берём id приаттаченных TG.

import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Input, Space, Spin } from "antd";
import { DeleteOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ResourceShell, type ResourceShellMode } from "@/components/organisms/ResourceShell";
import { ResourceTable, type Column } from "@/components/organisms/ResourceTable";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import type { DetailTab } from "@/components/organisms/DetailShell";
import type { DetailExtCtx } from "@/components/organisms/ResourceDetailExtensions";
import { ApiError, api } from "@/api/client";
import { loadBalancersApi, attachedTargetGroupIds } from "@/api/resources";
import { REGISTRY, getByPath } from "@/lib/resource-registry";
import { buildSpecColumns } from "@/lib/spec-columns";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

const LB_SPEC = REGISTRY["load-balancers"];
const TG_SPEC = REGISTRY["target-groups"];

// LbTargetGroupsTab — вкладка «Целевые группы»: registry-driven список TG,
// отфильтрованный по приаттаченным к этому балансировщику; привязка/отвязка —
// inline через verb-RPC. Тела attach/detach РАЗНЫЕ (attach вложенный, detach
// плоский) — строятся в api/resources builder'ах, не «по симметрии».
function LbTargetGroupsTab({
  lbId,
  projectId,
  attachedIds,
}: {
  lbId: string;
  projectId: string | null;
  attachedIds: string[];
}) {
  const navigate = useNavigate();
  const invalidate = useInvalidateResourceList();
  const [query, setQuery] = useState("");
  const [draft, setDraft] = useState<string | undefined>(undefined);
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);

  // Полный список TG проекта → фильтруем до приаттаченных (attached_target_groups
  // несёт только id, поэтому резолвим полные объекты для табличных колонок).
  const { data } = useQuery({
    queryKey: ["target-groups", "by-lb", projectId, attachedIds.join(",")],
    queryFn: () =>
      api.list<{ target_groups: Record<string, unknown>[] }>(TG_SPEC.apiPath, {
        project_id: projectId ?? "",
        pageSize: "500",
      }),
    enabled: !!projectId,
    refetchInterval: 5000,
  });
  const attachedSet = useMemo(() => new Set(attachedIds), [attachedIds]);
  const rows = useMemo(() => {
    const all = data?.target_groups ?? [];
    return all.filter((r) => attachedSet.has(getByPath<string>(r, "id") ?? ""));
  }, [data, attachedSet]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => {
      const nm = (getByPath<string>(r, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(r, "id") ?? "").toLowerCase();
      return nm.includes(q) || id.includes(q);
    });
  }, [rows, query]);

  const mut = useMutation({
    // Парные verb-RPC — РАЗНАЯ форма тела: attach вложенный `attached_target_group`,
    // detach плоский `target_group_id` (см. api/resources builder'ы).
    mutationFn: (params: { verb: "attach" | "detach"; tgId: string }) =>
      params.verb === "attach"
        ? loadBalancersApi.attachTargetGroup(lbId, params.tgId)
        : loadBalancersApi.detachTargetGroup(lbId, params.tgId),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpId(id);
      } else {
        setPendingId(null);
        invalidate("load-balancers", projectId);
      }
    },
    onError: (e) => {
      toast.error(`Target group: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`);
      setPendingId(null);
    },
  });
  const busy = mut.isPending || opId !== null;

  const onAttach = () => {
    if (!draft || attachedIds.includes(draft)) return;
    setOpTitle("Привязка target group");
    setPendingId(draft);
    mut.mutate({ verb: "attach", tgId: draft });
    setDraft(undefined);
  };
  const onDetach = (id: string) => {
    setOpTitle("Отвязка target group");
    setPendingId(id);
    mut.mutate({ verb: "detach", tgId: id });
  };

  const columns = useMemo<Column<Record<string, unknown>>[]>(() => {
    const cols = buildSpecColumns(TG_SPEC, { projectId: projectId ?? undefined });
    cols.push({
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (row) => {
        const id = getByPath<string>(row, "id") ?? "";
        return pendingId === id ? (
          <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
        ) : (
          <Button
            type="text"
            danger
            size="small"
            icon={<DeleteOutlined />}
            aria-label="Отвязать"
            onClick={(e) => {
              e.stopPropagation();
              onDetach(id);
            }}
            disabled={busy}
          />
        );
      },
    });
    return cols;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, pendingId, busy]);

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
        <div style={{ flex: 1 }} />
        <div style={{ minWidth: 240 }}>
          <RefSelect
            refResource="target-groups"
            refProjectScoped
            value={draft}
            onChange={(v) => setDraft(v || undefined)}
            refFilter={(row) => !attachedIds.includes((row.id as string) ?? "")}
            placeholder="Выбрать target group…"
            disabled={busy}
          />
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={onAttach} disabled={!draft || busy}>
          Привязать
        </Button>
      </div>
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          {query ? "По фильтру ничего не найдено." : "Целевые группы ещё не привязаны."}
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
      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingId(null);
          invalidate("load-balancers", projectId);
        }}
      />
    </Space>
  );
}

interface Props {
  mode?: ResourceShellMode;
}

export function LoadBalancerDetailPage({ mode }: Props) {
  // Bespoke вкладка «Целевые группы» — привязка/отвязка не выражается через
  // filterField (pivot attached_target_groups), поэтому подаётся сюда как
  // extraTab; «Листенеры» — обычный registry-related таб (spec.related).
  const extraTabs = (ctx: DetailExtCtx): DetailTab[] => {
    const lbId = getByPath<string>(ctx.data, "id") ?? "";
    const attachedIds = attachedTargetGroupIds(ctx.data);
    return [
      {
        id: "target-groups",
        label: "Целевые группы",
        count: attachedIds.length,
        render: () => <LbTargetGroupsTab lbId={lbId} projectId={ctx.projectId} attachedIds={attachedIds} />,
      },
    ];
  };

  return <ResourceShell spec={LB_SPEC} mode={mode} extraTabs={extraTabs} />;
}
