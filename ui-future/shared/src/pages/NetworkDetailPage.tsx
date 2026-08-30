// NetworkDetailPage — Network detail с табами по дочерним ресурсам.
// Tabs: Обзор (auto) / Таблицы маршрутов / Группы безопасности /
//       DNS зоны / Операции.
//
// Per-tab header CTA через ResourceDetailPage.headerActionsByTab.
// Каждый child-tab имеет Title + filter (имя или id substring) над таблицей.

import { useCallback, useEffect, useMemo, useState } from "react";
import { DETAIL_CONTENT_WIDTH } from "@shared/components/organisms/DetailShell";
import { useParams, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Input, Space, Typography } from "antd";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PlusOutlined } from "@ant-design/icons";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { useResourceStream } from "@shared/lib/subscription/use-resource-stream";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import { NetworkCidrManager } from "@shared/components/organisms/NetworkCidrManager";
import { SectionHeader } from "@shared/components/molecules/SectionHeader";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath, resourceProjectPath, type ResourceSpec } from "@shared/lib/resource-registry";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { ColumnSettings, useHiddenColumns } from "@shared/components/molecules/TableToolbar";
import {
  clientScope,
  narrowingTitle,
  noMatchesText,
  rowsAreComplete,
  searchPlaceholder,
  type NarrowingScope,
} from "@shared/lib/list-scope";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

export function NetworkDetailPage() {
  const { uid: networkId, projectId } = useParams();
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

  // ЧТЕНИЕ ПО СОБЫТИЮ, ОПРОС — ПОКА СОБЫТИЙ НЕТ (#1021).
  //
  // Эти списки — чужие ресурсы, показанные на карточке, и меняются они реже,
  // чем опрашивались. Признак покрытия свой на КАЖДЫЙ вид: владелец объявляет
  // словарь целиком, но покрытым считается ровно названный им вид, а не домен.
  const { streamed: subnetsStreamed } = useResourceStream({
    specId: "subnets",
    projectId: projectId ?? null,
    invalidate: ["subnets", "list", projectId],
    enabled: !!projectId,
  });
  const { streamed: routeTablesStreamed } = useResourceStream({
    specId: "route-tables",
    projectId: projectId ?? null,
    invalidate: ["route-tables", "list", projectId],
    enabled: !!projectId,
  });
  const { streamed: securityGroupsStreamed } = useResourceStream({
    specId: "security-groups",
    projectId: projectId ?? null,
    invalidate: ["security-groups", "list", projectId],
    enabled: !!projectId,
  });

  const { data: subnetData } = useQuery({
    queryKey: ["subnets", "list", projectId],
    queryFn: () =>
      api.list<{ subnets: Array<Record<string, unknown>>; next_page_token?: string }>(subnetSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: subnetsStreamed ? false : 5000,
    enabled: !!projectId,
  });

  const { data: rtData } = useQuery({
    queryKey: ["route-tables", "list", projectId],
    queryFn: () =>
      api.list<{ route_tables: Array<Record<string, unknown>>; next_page_token?: string }>(rtSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: routeTablesStreamed ? false : 5000,
    enabled: !!projectId,
  });

  const { data: sgData } = useQuery({
    queryKey: ["security-groups", "list", projectId],
    queryFn: () =>
      api.list<{ security_groups: Array<Record<string, unknown>>; next_page_token?: string }>(sgSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: securityGroupsStreamed ? false : 5000,
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

  // Область каждой вкладки — по её СОБСТВЕННОМУ курсору: три списка читаются
  // тремя запросами, и усечён может оказаться любой из них по отдельности.
  // Курсор при этом относится к списку ПРОЕКТА, а не к строкам этой сети:
  // отбор по `network_id` идёт уже в браузере, поэтому непрочитанная страница
  // проекта может нести подсети именно этой сети.
  const subnetScope = clientScope(!!subnetData?.next_page_token);
  const rtScope = clientScope(!!rtData?.next_page_token);
  const sgScope = clientScope(!!sgData?.next_page_token);

  // RowActionsMenu Edit-кнопка ведёт на `${basePath}/${id}/edit` — для child-resources
  // на network-detail передаём nested basePath, чтобы edit URL остался под networks/.
  const nestedBase = (route: string) =>
    projectId && networkId ? `/projects/${projectId}/vpc/networks/${networkId}/${route}` : null;
  const subnetColumns = useChildColumns(subnetSpec, projectId, nestedBase("subnets"));
  const rtColumns = useChildColumns(rtSpec, projectId, nestedBase("route-tables"));
  const sgColumns = useChildColumns(sgSpec, projectId, nestedBase("security-groups"));

  // VPC-1: Обзор сети показывает менеджер супернета (declared IPv4/IPv6 CIDR,
  // мутируется :add/:remove-cidr-blocks) + таблицу подсетей.
  const overviewExtras = useCallback(
    (data: Record<string, unknown>) => {
      const v4 = getByPath<string[]>(data, "ipv4_cidr_blocks") ?? [];
      const v6 = getByPath<string[]>(data, "ipv6_cidr_blocks") ?? [];
      return (
        <Space direction="vertical" size={24} style={{ width: "100%" }}>
          {networkId && (
            <div style={{ maxWidth: DETAIL_CONTENT_WIDTH }}>
              <SectionHeader eyebrow="Адресное пространство" title="CIDR" />
              <NetworkCidrManager networkId={networkId} v4Blocks={v4} v6Blocks={v6} />
            </div>
          )}
          <ChildSection
            title="Подсети"
            rows={networkSubnets}
            scope={subnetScope}
            columns={subnetColumns}
            emptyText="В сети нет подсетей."
            storageKey="network-subnets"
          />
        </Space>
      );
    },
    [networkSubnets, subnetColumns, networkId, subnetScope],
  );

  const extraTabs = useMemo(
    () => (): DetailTab[] => [
      {
        id: "route-tables",
        label: "Таблицы маршрутов",
        count: networkRouteTables.length,
        render: () => (
          <ChildSection
            title="Таблицы маршрутов"
            rows={networkRouteTables}
            scope={rtScope}
            columns={rtColumns}
            emptyText="К сети не привязано ни одной таблицы маршрутов."
            storageKey="network-route-tables"
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
            scope={sgScope}
            columns={sgColumns}
            emptyText="В сети нет групп безопасности."
            storageKey="network-security-groups"
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
    [networkRouteTables, networkSGs, rtColumns, sgColumns, rtScope, sgScope],
  );

  const headerActionsByTab = useCallback(
    (tabId: string) => {
      if (!projectId || !networkId) return null;
      if (tabId === "route-tables") {
        return (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => openCreateModal("route-tables")}>
            Создать таблицу маршрутов
          </Button>
        );
      }
      if (tabId === "security-groups") {
        return (
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
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
    const basePath = basePathOverride ?? resourceProjectPath(spec.id, projectId);
    // Вложенная таблица: имя ведёт на карточку ЧЕРЕЗ родителя и несёт иконку
    // типа — в одном окне соседствуют подсети, таблицы маршрутов и группы.
    const cols = buildSpecColumns(spec, { projectId, nameBasePath: basePath, nameIcon: true });
    // KAC-198: include service segment (vpc/compute/nlb) so Subnet/SG/RT
    // child-table links под NetworkDetailPage ведут на actual route в App.tsx.
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
  storageKey,
  scope,
}: {
  title: string;
  rows: Array<Record<string, unknown>>;
  columns: Column<Record<string, unknown>>[];
  emptyText: string;
  /** Ключ, под которым запоминается выбор столбцов этой таблицы. */
  storageKey: string;
  /**
   * Область, о которой судят фильтр и стрелка сортировки (#373).
   *
   * Эта вкладка читает список проекта ОДНИМ запросом и продолжения не
   * предлагает, поэтому за курсором может остаться то, чего фильтр никогда не
   * увидит: «по фильтру ничего не найдено» означало бы отсутствие, которого
   * никто не проверял.
   */
  scope: NarrowingScope;
}) {
  const [query, setQuery] = useState("");
  // Где есть фильтр — есть и выбор столбцов: обе ручки про то, что показывать,
  // и наличие одной без другой читается как недоделка (требование владельца
  // 2026-08-12).
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${storageKey}`);
  const shown = useMemo(() => columns.filter((c) => !hidden.has(c.header)), [columns, hidden]);
  const toggleCols = useMemo(
    () => columns.filter((c) => c.header).map((c) => ({ key: c.header, label: c.header })),
    [columns],
  );
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
      <Space size={8} style={{ width: "100%" }}>
        <Input.Search
          placeholder={searchPlaceholder(scope)}
          title={narrowingTitle(scope)}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          style={{ width: 360 }}
          allowClear
        />
        <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
      </Space>
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          {query ? noMatchesText(scope) : emptyText}
        </div>
      ) : (
        <ResourceTable
          rows={filtered}
          columns={shown}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          complete={rowsAreComplete(scope)}
        />
      )}
    </Space>
  );
}
