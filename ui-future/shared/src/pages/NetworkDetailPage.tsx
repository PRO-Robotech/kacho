// NetworkDetailPage — Network detail с табами по дочерним ресурсам.
// Tabs: Обзор (auto) / Таблицы маршрутизации / Группы безопасности /
//       DNS зоны / Операции.
//
// Per-tab header CTA через ResourceDetailPage.headerActionsByTab.
// Каждый child-tab имеет Title + filter (имя или id substring) над таблицей.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button, Input, Space, Typography } from "antd";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PlusOutlined } from "@ant-design/icons";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath, resourceProjectPath, type ResourceSpec } from "@shared/lib/resource-registry";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

export function NetworkDetailPage() {
  const { uid: networkId, projectId } = useParams();
  const navigate = useNavigate();
  const networkSpec = REGISTRY["networks"];
  const rtSpec = REGISTRY["route-tables"];
  const sgSpec = REGISTRY["security-groups"];

  const subnetSpec = REGISTRY["subnets"];

  // Create flow для всех child-ресурсов (Subnet/RT/SG) — через модалку
  // ResourceFormModal, открываемую по query `?modal=<spec.id>-create&networkId=<n>`.
  // URL остаётся на parent-странице → при close модалки user остаётся на
  // Network detail. presetFields подхватываются ResourceFormModal автоматически
  // (см. ResourceFormModal.tsx — networkId → network_id snake_case преобр.).
  const [searchParams, setSearchParams] = useSearchParams();

  const openCreateModal = useCallback(
    (specId: string) => {
      if (!networkId) return;
      const params = new URLSearchParams(searchParams);
      params.set("modal", `${specId}-create`);
      params.set("networkId", networkId);
      // Старый ?action=…-* флаг убираем — модалка теперь единый entry-point.
      params.delete("action");
      params.delete("createSubnet");
      setSearchParams(params, { replace: false });
    },
    [networkId, searchParams, setSearchParams],
  );

  // Back-compat для старых ссылок (KAC-67 v2..v5 — `?action=create-…` / `?createSubnet=1`):
  // конвертируем в `?modal=…-create`, чтобы старые закладки/линки работали.
  useEffect(() => {
    const action = searchParams.get("action");
    const createSubnetLegacy = searchParams.get("createSubnet") === "1";
    if (!networkId) return;
    let target: string | null = null;
    if (createSubnetLegacy || action === "create-subnet") target = "subnets";
    else if (action === "create-route-table") target = "route-tables";
    else if (action === "create-security-group") target = "security-groups";
    if (!target) return;
    const params = new URLSearchParams(searchParams);
    params.delete("action");
    params.delete("createSubnet");
    params.set("modal", `${target}-create`);
    params.set("networkId", networkId);
    setSearchParams(params, { replace: true });
  }, [networkId, searchParams, setSearchParams]);

  const { data: subnetData } = useQuery({
    queryKey: ["subnets", "list", projectId],
    queryFn: () =>
      api.list<{ subnets: Array<Record<string, unknown>> }>(subnetSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 5000,
    enabled: !!projectId,
  });

  const { data: rtData } = useQuery({
    queryKey: ["route-tables", "list", projectId],
    queryFn: () =>
      api.list<{ route_tables: Array<Record<string, unknown>> }>(rtSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 5000,
    enabled: !!projectId,
  });

  const { data: sgData } = useQuery({
    queryKey: ["security-groups", "list", projectId],
    queryFn: () =>
      api.list<{ security_groups: Array<Record<string, unknown>> }>(sgSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 5000,
    enabled: !!projectId,
  });

  const networkSubnets = useMemo(
    () => (subnetData?.subnets ?? []).filter((r) => r.network_id === networkId),
    [subnetData, networkId],
  );
  const networkRouteTables = useMemo(
    () => (rtData?.route_tables ?? []).filter((r) => r.network_id === networkId),
    [rtData, networkId],
  );
  const networkSGs = useMemo(
    () => (sgData?.security_groups ?? []).filter((r) => r.network_id === networkId),
    [sgData, networkId],
  );

  // RowActionsMenu Edit-кнопка ведёт на `${basePath}/${id}/edit` — для child-resources
  // на network-detail передаём nested basePath, чтобы edit URL остался под networks/.
  const nestedBase = (route: string) =>
    projectId && networkId ? `/projects/${projectId}/vpc/networks/${networkId}/${route}` : null;
  const subnetColumns = useChildColumns(subnetSpec, projectId, nestedBase("subnets"));
  const rtColumns = useChildColumns(rtSpec, projectId, nestedBase("route-tables"));
  const sgColumns = useChildColumns(sgSpec, projectId, nestedBase("security-groups"));

  const overviewExtras = useCallback(
    () => (
      <ChildSection
        title="Подсети"
        rows={networkSubnets}
        columns={subnetColumns}
        emptyText="В сети нет подсетей."
        onClick={(id) =>
          projectId && networkId && navigate(`/projects/${projectId}/vpc/networks/${networkId}/subnets/${id}`)
        }
      />
    ),
    [networkSubnets, subnetColumns, projectId, networkId, navigate],
  );

  const extraTabs = useMemo(
    () => (): DetailTab[] => [
      {
        id: "route-tables",
        label: "Таблицы маршрутизации",
        count: networkRouteTables.length,
        render: () => (
          <ChildSection
            title="Таблицы маршрутизации"
            rows={networkRouteTables}
            columns={rtColumns}
            emptyText="К сети не привязано ни одной таблицы маршрутизации."
            onClick={(id) =>
              projectId && networkId && navigate(`/projects/${projectId}/vpc/networks/${networkId}/route-tables/${id}`)
            }
          />
        ),
      },
      {
        id: "security-groups",
        label: "Группы безопасности",
        count: networkSGs.length,
        render: () => (
          <ChildSection
            title="Группы безопасности"
            rows={networkSGs}
            columns={sgColumns}
            emptyText="В сети нет групп безопасности."
            onClick={(id) =>
              projectId &&
              networkId &&
              navigate(`/projects/${projectId}/vpc/networks/${networkId}/security-groups/${id}`)
            }
          />
        ),
      },
      {
        id: "dns-zones",
        label: "DNS зоны",
        render: () => (
          <ErrorResult
            status="404"
            subTitle="DNS зоны пока не поддерживаются в Kachō (запланировано в дорожной карте)."
          />
        ),
      },
      // tab "Операции" автоматически добавляется ResourceDetailPage —
      // не дублируем здесь.
    ],
    [networkRouteTables, networkSGs, rtColumns, sgColumns, projectId, networkId, navigate],
  );

  const headerActionsByTab = useCallback(
    (tabId: string) => {
      if (!projectId || !networkId) return null;
      if (tabId === "route-tables") {
        return (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => openCreateModal("route-tables")}>
            Создать таблицу маршрутизации
          </Button>
        );
      }
      if (tabId === "security-groups") {
        return (
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            onClick={() => openCreateModal("security-groups")}
          >
            Создать группу безопасности
          </Button>
        );
      }
      return null;
    },
    [projectId, networkId, openCreateModal],
  );

  // "Создать подсеть" — открывает ту же модалку с specId=subnets.
  const overviewCreateOverride = useMemo(
    () =>
      projectId && networkId
        ? {
            label: "Создать подсеть",
            onClick: () => openCreateModal("subnets"),
          }
        : undefined,
    [projectId, networkId, openCreateModal],
  );

  return (
    <>
      <ResourceDetailPage
        spec={networkSpec}
        extraTabs={extraTabs}
        headerActionsByTab={headerActionsByTab}
        overviewCreateOverride={overviewCreateOverride}
        overviewExtras={overviewExtras}
      />
      {projectId && <ResourceFormModal projectId={projectId} />}
    </>
  );
}

// useChildColumns — buildSpecColumns + actions-колонка для child-tabs.
// basePathOverride — если задан, используется вместо flat /projects/<projectId>/<route>;
// нужно для nested-контекстов (RT/SG/Subnet под network) чтобы edit/delete
// links оставались под parent-путём.
function useChildColumns(
  spec: ResourceSpec,
  projectId: string | undefined,
  basePathOverride?: string | null,
): Column<Record<string, unknown>>[] {
  return useMemo(() => {
    const cols = buildSpecColumns(spec, { projectId });
    // KAC-198: include service segment (vpc/compute/nlb) so Subnet/SG/RT
    // child-table links под NetworkDetailPage ведут на actual route в App.tsx.
    const basePath = basePathOverride ?? resourceProjectPath(spec.id, projectId);
    if (basePath) {
      cols.push({
        header: "",
        className: "text-right whitespace-nowrap",
        cell: (row) => <RowActionsMenu spec={spec} row={row} basePath={basePath} projectId={projectId ?? null} />,
      });
    }
    return cols;
  }, [spec, projectId, basePathOverride]);
}

// ChildSection — Title + filter + table. Используется на каждой
// child-tab Network detail.
function ChildSection({
  title,
  rows,
  columns,
  emptyText,
  onClick,
}: {
  title: string;
  rows: Array<Record<string, unknown>>;
  columns: Column<Record<string, unknown>>[];
  emptyText: string;
  onClick: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((row) => {
      const name = (getByPath<string>(row, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(row, "id") ?? "").toLowerCase();
      return name.includes(q) || id.includes(q);
    });
  }, [rows, query]);

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={4} style={{ margin: 0 }}>
        {title}
      </Typography.Title>
      <Input.Search
        placeholder="Фильтр по имени или идентификатору"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        style={{ maxWidth: 360 }}
        allowClear
      />
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          {query ? "По фильтру ничего не найдено." : emptyText}
        </div>
      ) : (
        <ResourceTable
          rows={filtered}
          columns={columns}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          onRowClick={(r) => {
            const id = getByPath<string>(r, "id");
            if (id) onClick(id);
          }}
        />
      )}
    </Space>
  );
}
