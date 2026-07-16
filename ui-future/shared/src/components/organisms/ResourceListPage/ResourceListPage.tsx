// ResourceListPage — generic страница списка ресурсов на antd.
//
// Polling 3 сек (через useResourceList).

import { useMemo, useState } from "react";
import { Link, useParams, useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button, Input, Segmented, Select, Typography, Tag } from "antd";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PlusOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath, resourceServicePrefix, type ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { type ReactNode } from "react";
import { ResourceEmptyState } from "@shared/components/molecules/ResourceEmptyState";
import { ProjectRequiredEmpty } from "@shared/components/molecules/ProjectRequiredEmpty";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { ColumnSettings, useHiddenColumns, type ToggleCol } from "@shared/components/molecules/TableToolbar";
import { useResourceList } from "@shared/lib/use-resource-list";

interface Props {
  spec: ResourceSpec;
  parentField?: string;
  parentParam?: string;
  /** Явное значение scope-фильтра (account-scoped IAM-ресурсы берут account
   *  из context-store, а не из URL-параметра). Имеет приоритет над parentParam. */
  parentValue?: string | null;
  /** page_size запроса списка (Role — 1000: клиентский system/custom-фильтр
   *  требует всю страницу, иначе custom-роли на 2-й странице выпадут). */
  pageSize?: string;
  /** Игнорировать spec.childRoute при drill (клик по строке ведёт на
   *  `${basePath}/${id}` detail, а не на childRoute). Projects внутри IAM-секции
   *  открывают IAM-деталь проекта, а не project-dashboard. */
  disableChildRoute?: boolean;
}

export function ResourceListPage({
  spec,
  parentField,
  parentParam,
  parentValue,
  pageSize,
  disableChildRoute = false,
}: Props) {
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const filterValue = parentValue ?? (parentParam ? (params[parentParam] ?? null) : null);
  const [query, setQuery] = useState("");
  // Конфигуратор видимости колонок (⚙ рядом с поиском) — persist в localStorage
  // по specId; те же toggles, что у related-таблиц detail-страниц.
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${spec.id}`);
  const toggleCols: ToggleCol[] = spec.columns.map((c) => ({ key: c.header, label: c.header }));

  const { data, isLoading, isError, error } = useResourceList(spec, parentField ?? null, filterValue, pageSize);

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        <Typography.Text strong>{spec.plural}</Typography.Text>
      </span>
    ),
    [spec.plural, spec.serviceTitle],
  );
  useBreadcrumb(breadcrumb);

  // KAC-231: модалки упразднены в пользу формы-страницы/панели (логика Network)
  // у модулей с полноценными panel/page-формами: VPC (ResourceShell edit-панель,
  // ResourceCreatePage) + admin (ResourceCreatePage/ResourceEditPage страницы).
  // Compute/NLB/IAM остаются на модалках до своей раскатки (их detail ещё не
  // ResourceShell, /edit редиректит в модалку). panelForms — этот гейт.
  const panelForms =
    resourceServicePrefix(spec.id) === "vpc" ||
    resourceServicePrefix(spec.id) === "iam" ||
    spec.id === "regions" ||
    spec.id === "zones" ||
    spec.id === "address-pools";
  const listBase = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname;
  const createTarget = panelForms ? `${listBase}/create` : `${listBase}?modal=${spec.id}-create`;
  // KAC-246: CTA «Создать» — в header right-slot (шапка), НЕ в page-toolbar.
  const cta = useMemo(() => {
    if (!spec.ops.create) return null;
    return (
      <Link to={createTarget}>
        <Button type="primary" icon={<PlusOutlined />}>
          Создать {spec.singular.toLowerCase()}
        </Button>
      </Link>
    );
  }, [spec, createTarget]);
  useHeaderRight(cta);

  if (parentField && !filterValue) return <ProjectRequiredEmpty resource={spec.plural} />;

  const basePath = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname;

  const items = (data?.[spec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];

  // Дополнительный фильтр "Зона доступности" — для ресурсов, у которых есть
  // понятие zone. Subnet хранит zone напрямую, Address — внутри
  // internal_ipv4_address.zone_id / external_ipv4_address.zone_id.
  const hasZoneFilter = spec.id === "subnets" || spec.id === "addresses";
  const [zone, setZone] = useState<string>("all");
  // Для Role — доп. фильтр system/custom (Segmented [Все/Системные/Кастомные]),
  // client-side по is_system. Тот же паттерн, что hasZoneFilter (паритет kacho-ui).
  const hasSystemFilter = spec.id === "roles";
  const [roleKind, setRoleKind] = useState<"all" | "system" | "custom">("all");
  const zoneSpec = REGISTRY["zones"];
  const { data: zoneData } = useQuery({
    queryKey: ["zones", "list-for-filter"],
    queryFn: () =>
      api.list<{ zones: Array<{ id: string; name?: string }> }>(zoneSpec.apiPath, {
        pageSize: "200",
      }),
    enabled: hasZoneFilter,
    staleTime: 60_000,
  });
  const zoneOptions = useMemo(
    () => [
      { value: "all", label: "Все зоны доступности" },
      ...((zoneData?.zones ?? []).map((z) => ({
        value: z.id,
        label: z.name || z.id,
      })) as { value: string; label: string }[]),
    ],
    [zoneData],
  );

  function rowZone(row: Record<string, unknown>): string | undefined {
    if (spec.id === "subnets") return getByPath<string>(row, "zone_id");
    if (spec.id === "addresses") {
      return (
        getByPath<string>(row, "internal_ipv4_address.zone_id") ??
        getByPath<string>(row, "external_ipv4_address.zone_id")
      );
    }
    return undefined;
  }

  const filteredItems = useMemo(() => {
    const q = query.trim().toLowerCase();
    return items.filter((row) => {
      // "Публичные IP" — это external addresses; internal IPs показываются
      // только в subnet detail (IP-адреса tab). Фильтруем по наличию
      // external_ipv4_address (либо external_ipv6_address в будущем).
      if (spec.id === "addresses") {
        const ext =
          getByPath<unknown>(row, "external_ipv4_address") ?? getByPath<unknown>(row, "external_ipv6_address");
        if (!ext) return false;
      }
      if (hasZoneFilter && zone !== "all" && rowZone(row) !== zone) return false;
      if (hasSystemFilter && roleKind !== "all") {
        const isSystem =
          getByPath<boolean>(row, "is_system") === true || getByPath<boolean>(row, "isSystem") === true;
        if (roleKind === "system" && !isSystem) return false;
        if (roleKind === "custom" && isSystem) return false;
      }
      if (!q) return true;
      const name = (getByPath<string>(row, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(row, "id") ?? "").toLowerCase();
      return name.includes(q) || id.includes(q);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, query, zone, hasZoneFilter, hasSystemFilter, roleKind, spec.id]);

  // params.projectId доступен для project-scoped listов (/projects/:projectId/...);
  // прокидываем в buildSpecColumns, чтобы format: "references" (used_by) мог
  // отрендерить ссылку на /projects/<projectId>/compute/instances/<id> и т.п.
  const columns: Column<Record<string, unknown>>[] = buildSpecColumns(spec, {
    projectId: params.projectId,
  }).filter((c) => !hidden.has(c.header));

  columns.push({
    header: "",
    className: "text-right whitespace-nowrap",
    cell: (row) => (
      <RowActionsMenu
        spec={spec}
        row={row}
        basePath={basePath}
        projectId={filterValue ?? null}
        editAsPanel={panelForms}
      />
    ),
  });

  // Пустой список (без активных пользовательских фильтров) → welcome, как у
  // дочерних таблиц. По filteredItems (учитывает intrinsic-фильтр addresses
  // «только внешние»): нет отображаемых строк при пустом поиске/зоне → welcome.
  const showWelcome =
    !isLoading &&
    !isError &&
    filteredItems.length === 0 &&
    spec.ops.create &&
    query.trim() === "" &&
    (!hasZoneFilter || zone === "all") &&
    (!hasSystemFilter || roleKind === "all");

  // Единая шапка списка (PanelHeader) — те же 3 части, что у табов/форм:
  // [иконка ресурса] + «Список» (действие) + plural (название) + счётчик.
  // CTA «Создать» — в шапке страницы (useHeaderRight). KAC-246.
  const listHeader = (right?: ReactNode) => (
    <PanelHeader
      icon={<ResourceIcon specId={spec.id} />}
      eyebrow="Список"
      title={
        // height:20 = строка заголовка (16px·1.25) — счётчик-Tag НЕ распирает
        // строку (был 24px), иначе текст бейджа «скачет» относительно detail
        // (там тега нет). Tag ≤18px помещается в строку.
        <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
          {spec.plural}
          {!isLoading && !isError && (
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
              {filteredItems.length}
            </Tag>
          )}
        </span>
      }
      right={right}
    />
  );

  // Welcome (пустой список) — та же surface-подложка, что и заполнённая страница,
  // чтобы заголовок не «прыгал» и не выглядел инородно (KAC-246).
  if (showWelcome) {
    return (
      <div className="kc-surface" style={{ padding: 20, height: "100%", overflow: "auto" }}>
        {listHeader()}
        <ResourceEmptyState spec={spec} onCreate={() => navigate(createTarget)} />
      </div>
    );
  }

  return (
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", overflow: "hidden", display: "flex", flexDirection: "column" }}
    >
      {/* Шапка списка (иконка + «Список» + plural + счётчик + фильтры) —
          фиксирована сверху, НЕ скроллится вместе с телом таблицы. */}
      <div style={{ flexShrink: 0, marginBottom: 12 }}>
        {listHeader(
          <>
            <Input.Search
              placeholder="Фильтр по имени или идентификатору"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              style={{ width: 320 }}
              allowClear
            />
            {hasZoneFilter && <Select value={zone} onChange={setZone} options={zoneOptions} style={{ width: 220 }} />}
            {hasSystemFilter && (
              <Segmented
                value={roleKind}
                onChange={(v) => setRoleKind(v as "all" | "system" | "custom")}
                options={[
                  { label: "Все", value: "all" },
                  { label: "Системные", value: "system" },
                  { label: "Кастомные", value: "custom" },
                ]}
              />
            )}
            <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
          </>,
        )}
      </div>

      {/* Тело таблицы заполняет остаток белой поверхности и скроллится внутри
          (горизонтально при широких колонках, вертикально при длинном списке). */}
      <div style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        {isError ? (
          <ErrorResult error={error} />
        ) : (
          <ResourceTable
            rows={filteredItems}
            loading={isLoading && items.length === 0}
            rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
            columns={columns}
            onRowClick={(row) => {
              const id = getByPath<string>(row, "id");
              if (!id) return;
              // childRoute шаблон: /projects/:id, ...; disableChildRoute → detail
              // в текущей секции (`${basePath}/${id}`).
              const target =
                spec.childRoute && !disableChildRoute ? spec.childRoute.replace(":id", id) : `${basePath}/${id}`;
              navigate(target);
            }}
          />
        )}
      </div>
    </div>
  );
}
