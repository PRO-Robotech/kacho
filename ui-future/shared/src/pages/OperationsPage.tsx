// OperationsPage — project-scoped global список LRO операций по всем VPC ресурсам.
// Aggregation client-side: для каждого VPC-resource type списком собираются
// ресурсы проекта, затем по каждому делается ListOperations. Все операции
// объединяются и сортируются по created_at desc.
//
// Фильтры: id / Статус / Тип ресурса.

import { useMemo, useState } from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Select, Space, Tag, Typography } from "antd";
import { ReloadOutlined, DeploymentUnitOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { OperationsTable, type Op, statusOf, type OperationStatus } from "@shared/components/molecules/OperationsTable";
import { useProjectStore } from "@shared/lib/context-store";
import { REGISTRY } from "@shared/lib/resource-registry";

// Список VPC-ресурсов, у которых есть per-resource ListOperations.
const VPC_RESOURCES = [
  { id: "networks", label: "Network" },
  { id: "subnets", label: "Subnet" },
  { id: "network-interfaces", label: "Network Interface" },
  { id: "addresses", label: "Address" },
  { id: "route-tables", label: "Route Table" },
  { id: "security-groups", label: "Security Group" },
  { id: "gateways", label: "Gateway" },
] as const;

const STATUS_OPTIONS: { value: OperationStatus | "all"; label: string }[] = [
  { value: "all", label: "Все статусы" },
  { value: "running", label: "Выполняется" },
  { value: "done", label: "Выполнена" },
  { value: "error", label: "Ошибка" },
  { value: "cancelled", label: "Отменена" },
];

const KIND_OPTIONS = [
  { value: "all", label: "Все типы" },
  // Русские названия из реестра (singular), а не английские VPC_RESOURCES.label.
  ...VPC_RESOURCES.map((r) => ({ value: r.id, label: REGISTRY[r.id]?.singular ?? r.label })),
];

interface ResListResp {
  // Динамическое поле: payloadKey → массив ресурсов
  [k: string]: Array<{ id: string }> | string | undefined;
}

export function OperationsPage() {
  const project = useProjectStore((s) => s.project);
  const projectId = project?.id ?? null;
  const qc = useQueryClient();

  const headerRight = useMemo(
    () => (
      <Button
        size="small"
        icon={<ReloadOutlined />}
        onClick={() =>
          qc.invalidateQueries({
            predicate: (q) => Array.isArray(q.queryKey) && q.queryKey[1] === "operations",
          })
        }
      >
        Обновить
      </Button>
    ),
    [qc],
  );
  useHeaderRight(headerRight);

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Virtual Private Cloud</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Операции</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<OperationStatus | "all">("all");
  const [kind, setKind] = useState<string>("all");

  // 1) для каждого VPC-resource type грузим список ресурсов проекта.
  const listQueries = useQueries({
    queries: VPC_RESOURCES.map((r) => {
      const spec = REGISTRY[r.id];
      return {
        queryKey: [r.id, "list-for-ops", projectId],
        queryFn: () =>
          api.list<ResListResp>(spec.apiPath, {
            project_id: projectId!,
            pageSize: "200",
          }),
        enabled: !!projectId && !!spec,
        staleTime: 30_000,
      };
    }),
  });

  // 2) собираем плоский список (resourceId, kind, apiPath).
  const targets = useMemo(() => {
    if (!projectId) return [];
    const out: { id: string; kind: string; apiPath: string }[] = [];
    VPC_RESOURCES.forEach((r, i) => {
      const spec = REGISTRY[r.id];
      const resp = listQueries[i].data;
      const list = (resp?.[spec.payloadKey] as Array<{ id: string }> | undefined) ?? [];
      list.forEach((item) => {
        if (item?.id) out.push({ id: item.id, kind: r.id, apiPath: spec.apiPath });
      });
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, ...listQueries.map((q) => q.dataUpdatedAt)]);

  // 3) для каждого target грузим operations.
  const opsQueries = useQueries({
    queries: targets.map((t) => ({
      queryKey: [t.kind, "operations", t.id],
      queryFn: () =>
        api.list<{ operations: Op[] }>(`${t.apiPath}/${t.id}/operations`, {
          pageSize: "50",
        }),
      enabled: true,
      staleTime: 5_000,
      // Реже поллим весь per-resource фан-аут (сотни запросов) — снижаем
      // постоянную сетевую нагрузку; in-flight операции обновятся за ~20с.
      refetchInterval: 20_000,
    })),
  });

  // 4) merge + sort.
  const allOps = useMemo(() => {
    const out: Op[] = [];
    opsQueries.forEach((q, i) => {
      const t = targets[i];
      const ops = q.data?.operations ?? [];
      ops.forEach((o) => out.push({ ...o, resource_id: o.resource_id ?? t?.id, resource_kind: t?.kind }));
    });
    out.sort((a, b) => {
      const ta = a.created_at ? Date.parse(a.created_at) : 0;
      const tb = b.created_at ? Date.parse(b.created_at) : 0;
      return tb - ta;
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opsQueries.map((q) => q.dataUpdatedAt).join(","), targets.length]);

  // Спиннер — только пока грузятся списки ресурсов (быстрая первая волна) ИЛИ
  // пока НЕ пришла ни одна операция. Как только первые операции есть — стримим
  // их в таблицу, не дожидаясь завершения всего per-resource фан-аута (сотни
  // запросов): «сбор» ощущается мгновенным, остальные операции дотекают.
  const isLoading =
    listQueries.some((q) => q.isLoading) || (allOps.length === 0 && opsQueries.some((q) => q.isLoading));

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return allOps.filter((o) => {
      if (kind !== "all" && o.resource_kind !== kind) return false;
      if (status !== "all" && statusOf(o) !== status) return false;
      if (!q) return true;
      return (o.id ?? "").toLowerCase().includes(q);
    });
  }, [allOps, query, status, kind]);

  if (!projectId) {
    return (
      <ErrorResult
        status="warning"
        title="Выберите проект"
        subTitle="Глобальные операции отображаются для текущего проекта."
      />
    );
  }

  return (
    <div className="kc-surface" style={{ padding: 20, minHeight: "100%" }}>
      <Space direction="vertical" size={0} style={{ width: "100%" }}>
        {/* Единая шапка: общая VPC-иконка модуля (DeploymentUnitOutlined,
            отличная от network) + действие «Операции» + название «VPC» +
            счётчик; фильтры — справа. */}
        <PanelHeader
          icon={<DeploymentUnitOutlined />}
          eyebrow="Операции"
          title={
            // height 20 = строка заголовка (16·1.25); Tag ≤18 не распирает строку
            // → бейдж не прыгает относительно list-страниц (тот же фикс, что в
            // ResourceListPage).
            <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
              VPC
              <Tag
                style={{
                  margin: 0,
                  fontSize: 11.5,
                  fontWeight: 600,
                  lineHeight: "16px",
                  height: 18,
                  paddingInline: 6,
                  borderRadius: 5,
                }}
              >
                {filtered.length}
              </Tag>
            </span>
          }
          right={
            <>
              <Input
                placeholder="Фильтр по идентификатору"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                allowClear
                style={{ width: 280 }}
              />
              <Select value={status} onChange={setStatus} options={STATUS_OPTIONS} style={{ width: 180 }} />
              <Select value={kind} onChange={setKind} options={KIND_OPTIONS} style={{ width: 180 }} />
            </>
          }
        />

        <OperationsTable
          rows={filtered}
          loading={isLoading}
          showResourceKind
          empty={allOps.length > 0 && filtered.length === 0}
        />
      </Space>
    </div>
  );
}
