// ResourceShell — единый registry-driven 3-зонный layout детализации ЛЮБОГО
// ресурса (KAC-231 эпик). Эталон выработан на VPC Network, раскатан на все
// модули.
//
// Зоны: (1) глобальный ServiceSidebar (Layout.tsx) | (2) DetailShell aside —
// имя + вертикальные табы + доки | (3) main — контент таба ИЛИ форма-панель.
//
// Табы: «Обзор» (5 обяз. полей + доменные строки расширения + «Редактировать»)
//   → per-type табы связанных ресурсов (spec.related) → доменные табы расширения
//   → «Операции» → «JSON» → «JSON (internal)» если есть internalGetPath.
//
// Формы — НЕ модалки, а разворот в зоне 3 (mode=edit | child-create), уникальный
// URI на таб/режим. Диспетч кастомных/generic форм — InlineResourceForm.
//
// Конфиг per-resource: spec.related / spec.docs / spec.emptyState (registry) +
// DETAIL_EXTENSIONS (доменный React-контент: см. resource-detail-extensions).

import { type ReactNode, useMemo, useState } from "react";
import { useParams, useNavigate, useLocation, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button, Descriptions, Spin, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { DetailShell, HeaderSlotPortal, type DetailTab, type DocLink } from "@shared/components/organisms/DetailShell";
import { DetailHeaderProvider } from "@shared/components/molecules/PanelHeader";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { ResourceEmptyState } from "@shared/components/molecules/ResourceEmptyState";
import { ResourceTable } from "@shared/components/organisms/ResourceTable";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { LabelsCell } from "@shared/components/atoms/LabelsCell";
import { formatDateTime } from "@shared/lib/datetime";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { LazyJsonMonacoView } from "@shared/components/molecules/JsonMonacoView";
import { OperationsTab } from "@shared/components/organisms/OperationsTab";
import { InlineResourceForm } from "@shared/components/organisms/InlineResourceForm";
import { TableSearch, ColumnSettings, useHiddenColumns, type ToggleCol } from "@shared/components/molecules/TableToolbar";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { detailExtension, type DescItem } from "@shared/components/organisms/ResourceDetailExtensions";
import { api } from "@shared/api/client";
import {
  REGISTRY,
  getByPath,
  resourceProjectPath,
  resourceServicePrefix,
  type ResourceSpec,
} from "@shared/lib/resource-registry";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { useResourceList } from "@shared/lib/use-resource-list";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { DetailOverviewActions } from "@shared/components/molecules/DetailOverviewActions";

export type ResourceShellMode = "edit" | "child-create";

function specByRoute(route: string): ResourceSpec | undefined {
  return Object.values(REGISTRY).find((s) => s.route === route);
}

/** JsonIntView — internal/infra-проекция ресурса (GET spec.internalGetPath). */
function JsonIntView({ path }: { path: string }) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["jsonint", path],
    queryFn: () => api.get<Record<string, unknown>>(path),
    // JSON-таб — read-only снимок; частый поллинг только гонял бы Monaco. Обновляем
    // существенно реже (перекормка редактора вместо реального обновления UX).
    refetchInterval: 30_000,
    staleTime: 10_000,
  });
  if (isError) return <ErrorResult error={error} />;
  if (isLoading && !data)
    return (
      <div style={{ padding: 32, textAlign: "center" }}>
        <Spin />
      </div>
    );
  return <LazyJsonMonacoView data={data} />;
}

/** RelatedTable — встроенная таблица дочернего ресурса (тот же ResourceTable,
 *  что на списке): поиск + конфигуратор колонок + «⋮» actions + welcome-empty. */
function RelatedTable({
  childSpec,
  filterFields,
  parentId,
  projectId,
  detailBase,
}: {
  childSpec: ResourceSpec;
  filterFields: string[];
  parentId: string;
  projectId: string;
  detailBase: string;
}) {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${childSpec.id}`);
  // Дочерний список тянется в scope своего родителя: account-scoped ресурсы
  // (Project/ServiceAccount) требуют account_id = uid аккаунта-родителя; прочие —
  // project_id из URL. Затем ownRows дофильтровывает по filterField.
  const accountScoped = childSpec.scope === "account";
  const { data, isLoading, isError, error } = useResourceList(
    childSpec,
    accountScoped ? "account_id" : "project_id",
    accountScoped ? parentId : projectId,
  );
  const all = (data?.[childSpec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
  // Фильтр по родителю (OR по нескольким полям — напр. subnet→addresses v4∪v6).
  const ownRows = all.filter((r) => filterFields.some((ff) => getByPath<string>(r, ff) === parentId));

  // Поиск по имени или идентификатору (client-side).
  const q = search.trim().toLowerCase();
  const rows = q
    ? ownRows.filter((r) => {
        const nm = (getByPath<string>(r, "name") ?? "").toLowerCase();
        const id = (getByPath<string>(r, "id") ?? "").toLowerCase();
        return nm.includes(q) || id.includes(q);
      })
    : ownRows;

  // child-create — панель в зоне 3 shell РОДИТЕЛЯ (URI вложен под родителя).
  const createPath = `${detailBase}/${childSpec.route}/create`;
  // drill в ребёнка — на его собственный flat-URL (родитель → в хлебных крошках).
  // IAM-ресурсы не project-scoped → flat-база /iam/<route> (иначе drill уходил бы
  // в nested /iam/accounts/:uid/projects/:id, где нет detail-роута).
  const flatChildBase =
    resourceServicePrefix(childSpec.id) === "iam"
      ? `/iam/${childSpec.route}`
      : (resourceProjectPath(childSpec.id, projectId) ?? `${detailBase}/${childSpec.route}`);
  const createLabel = `Создать ${childSpec.singular.toLowerCase()}`;

  // Колонки: spec.columns без столбцов-ссылок на родителя (filterFields).
  const specNoParent: ResourceSpec = {
    ...childSpec,
    columns: childSpec.columns.filter((c) => !filterFields.includes(c.path)),
  };
  const toggleCols: ToggleCol[] = specNoParent.columns.map((c) => ({ key: c.header, label: c.header }));
  const columns = buildSpecColumns(specNoParent, { projectId }).filter((c) => !hidden.has(c.header));
  columns.push({
    header: "",
    className: "text-right whitespace-nowrap",
    cell: (row) => (
      <RowActionsMenu spec={childSpec} row={row} basePath={flatChildBase} projectId={projectId || null} editAsPanel />
    ),
  });

  if (isError) return <ErrorResult error={error} />;

  // Пустое состояние — welcome (только когда детей реально нет; промах поиска
  // показывается внутри таблицы). createLabel передаём отдельно (тот же текст).
  if (!isLoading && ownRows.length === 0) {
    return <ResourceEmptyState spec={childSpec} onCreate={() => navigate(createPath)} createLabel={createLabel} />;
  }

  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      {/* Фильтры (поиск/колонки) поднимаются на уровень имени ресурса (зона 3,
          правый слот) через HeaderSlotPortal — req3. */}
      <HeaderSlotPortal>
        <TableSearch value={search} onChange={setSearch} />
        <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
      </HeaderSlotPortal>
      <ResourceTable
        rows={rows}
        columns={columns}
        loading={isLoading}
        rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
        empty={q ? "По запросу ничего не найдено." : undefined}
        onRowClick={(r) => {
          const id = getByPath<string>(r, "id");
          if (id) navigate(`${flatChildBase}/${id}`);
        }}
      />
    </div>
  );
}

export function ResourceShell({ spec, mode }: { spec: ResourceSpec; mode?: ResourceShellMode }) {
  const { projectId, uid, childRoute } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const invalidate = useInvalidateResourceList();

  // detailBase = URL до и включая /:uid (надёжно при любой вложенности/модуле).
  const marker = `/${uid ?? ""}`;
  const mIdx = uid ? location.pathname.indexOf(marker) : -1;
  const detailBase =
    mIdx >= 0
      ? location.pathname.slice(0, mIdx + marker.length)
      : `${resourceProjectPath(spec.id, projectId) ?? `/${spec.route}`}/${uid}`;

  const { data, isLoading, isError, error } = useQuery({
    queryKey: [spec.id, "shell-detail", uid],
    queryFn: () => api.get<Record<string, unknown>>(`${spec.apiPath}/${uid}`),
    enabled: !!uid,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const ext = useMemo(() => detailExtension(spec.id), [spec.id]);
  const name = (data ? getByPath<string>(data, "name") : "") || (data ? ext?.title?.(data) : "") || (uid ?? "");

  const listHref = resourceProjectPath(spec.id, projectId);
  const breadcrumb = useMemo(() => {
    const childSpec = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
    // Локализованная метка для кастомных child-create-роутов без REGISTRY-spec
    // (privileges → «Привилегии»), чтобы breadcrumb не показывал raw route.
    const CHILD_LABELS: Record<string, string> = { privileges: "Привилегии" };
    const childLabel = childSpec?.plural ?? (childRoute ? CHILD_LABELS[childRoute] ?? childRoute : "");
    const sec = (txt: string) => <Typography.Text type="secondary">{txt}</Typography.Text>;
    const sep = <Typography.Text type="secondary">/</Typography.Text>;
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
        {spec.serviceTitle && (
          <>
            {sec(spec.serviceTitle)}
            {sep}
          </>
        )}
        {listHref ? <Link to={listHref}>{sec(spec.plural)}</Link> : sec(spec.plural)}
        {sep}
        {mode ? (
          <>
            <Link to={detailBase}>{sec(name)}</Link>
            {sep}
            {mode === "edit" ? (
              <Typography.Text strong>Редактирование</Typography.Text>
            ) : (
              <>
                <Link to={`${detailBase}/${childRoute}`}>{sec(childLabel)}</Link>
                {sep}
                <Typography.Text strong>Создание</Typography.Text>
              </>
            )}
          </>
        ) : (
          <Typography.Text strong style={{ maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis" }}>
            {name}
          </Typography.Text>
        )}
      </span>
    );
  }, [spec.serviceTitle, spec.plural, listHref, detailBase, name, mode, childRoute]);
  useBreadcrumb(breadcrumb);

  // KAC-242: действия в ШАПКЕ страницы — КОНТЕКСТНЫЕ по активному табу (не
  // глобальные на всех табах):
  //   • «Обзор»        → Редактировать + ⋮Удалить ресурса (DetailOverviewActions)
  //   • related-child  → «Создать <child>» (подсеть / таблица маршрутизации / SG / …);
  //                       удаление ребёнка — per-row в таблице (RowActionsMenu)
  //   • прочие табы (операции / JSON / ext) → нет
  // Скрыто в edit/child-create (форма уже в зоне 3). Активный таб берём из URL ДО
  // early-return (без `data`); сам набор кнопок мемоизируем.
  const headerTabFromUrl = location.pathname.startsWith(detailBase)
    ? location.pathname.slice(detailBase.length).replace(/^\/+/, "").split("/")[0]
    : "";
  const headerTabId =
    mode === "child-create" && childRoute ? childRoute : mode === "edit" ? "overview" : headerTabFromUrl || "overview";
  const headerActions = useMemo(() => {
    if (mode) return null;
    if (headerTabId === "overview") {
      return data ? (
        <DetailOverviewActions
          spec={spec}
          data={data}
          projectId={projectId ?? null}
          detailBase={detailBase}
          extActions={ext?.headerActions?.({ data, projectId: projectId ?? null, detailBase, navigate })}
        />
      ) : null;
    }
    const rel = (spec.related ?? []).find((r) => REGISTRY[r.childId]?.route === headerTabId);
    const childSpec = rel ? REGISTRY[rel.childId] : undefined;
    if (childSpec) {
      return (
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => navigate(`${detailBase}/${childSpec.route}/create`)}
        >
          Создать {childSpec.singular.toLowerCase()}
        </Button>
      );
    }
    // Активный extra-таб (напр. «Привилегии») может нести собственный header-CTA
    // («Выдать доступ») — рендерим его в шапке страницы, как у related-табов.
    if (data) {
      const activeExtra = (ext?.extraTabs?.({ data, projectId: projectId ?? null, detailBase, navigate }) ?? []).find(
        (t) => t.id === headerTabId,
      );
      if (activeExtra?.headerAction) return activeExtra.headerAction;
    }
    return null;
  }, [mode, headerTabId, data, spec, projectId, detailBase, ext, navigate]);
  useHeaderRight(headerActions);

  if (isLoading && !data) {
    return (
      <div style={{ padding: 48, textAlign: "center" }}>
        <Spin />
      </div>
    );
  }
  if (isError || !data) {
    return <ErrorResult error={error} />;
  }

  const related = spec.related ?? [];
  const extCtx = { data, projectId: projectId ?? null, detailBase, navigate };

  // ── Обзор: 5 обязательных + доменные строки расширения ──
  const overviewItems: DescItem[] = [
    { label: "Идентификатор", value: <CopyableId id={getByPath<string>(data, "id") ?? ""} /> },
    { label: "Имя", value: name },
    { label: "Описание", value: getByPath<string>(data, "description") || "—" },
    { label: "Дата создания", value: formatDateTime(getByPath<string>(data, "created_at")) },
    // KAC-246: метки в обзоре — read-only (chips); добавление/правка — в форме
    // создания/модификации (LabelsEditor, key=value-таблица).
    { label: "Метки", value: <LabelsCell labels={getByPath<Record<string, string>>(data, "labels")} max={12} /> },
    ...(ext?.overviewExtra?.(extCtx) ?? []),
  ];

  const tabs: DetailTab[] = [
    {
      id: "overview",
      label: "Обзор",
      render: () => (
        <div>
          <Descriptions
            column={1}
            size="small"
            bordered
            style={{ maxWidth: 920 }}
            labelStyle={{ width: 260, whiteSpace: "nowrap", verticalAlign: "top" }}
            items={overviewItems.map((it, i) => ({ key: String(i), label: it.label, children: it.value }))}
          />
          {ext?.overviewBelow?.(extCtx)}
        </div>
      ),
    },
  ];

  // Связанные ресурсы — отдельный таб на каждый тип.
  related.forEach((r) => {
    const childSpec = REGISTRY[r.childId];
    if (!childSpec) return;
    const filterFields = Array.isArray(r.filterField) ? r.filterField : [r.filterField];
    tabs.push({
      id: childSpec.route,
      label: r.label ?? childSpec.plural,
      // Зона-2: связанный таб = список дочернего ресурса → «действие» Список,
      // тип/иконка ребёнка (а НЕ label таба над типом родителя).
      eyebrow: "Список",
      headerTitle: childSpec.plural,
      headerIcon: <ResourceIcon specId={childSpec.id} />,
      // related-таблица заполняет зону-3 и скроллит себя (фикс. шапка колонок).
      fill: true,
      render: () => (
        <RelatedTable
          childSpec={childSpec}
          filterFields={filterFields}
          parentId={getByPath<string>(data, "id") ?? uid ?? ""}
          projectId={projectId ?? ""}
          detailBase={detailBase}
        />
      ),
    });
  });

  // Доменные табы расширения (SG rules, RT routes, Instance NIC, ...).
  (ext?.extraTabs?.(extCtx) ?? []).forEach((t) => tabs.push(t));

  // Операции (если не sync-ресурс).
  if (!ext?.hideOperations) {
    tabs.push({
      id: "operations",
      label: "Операции",
      fill: true,
      render: () => <OperationsTab spec={spec} resourceId={getByPath<string>(data, "id") ?? uid ?? ""} />,
    });
  }
  tabs.push({
    id: "json",
    label: "JSON",
    eyebrow: "JSON",
    render: () => (
      <div>
        <LazyJsonMonacoView data={data} />
      </div>
    ),
  });
  if (spec.internalGetPath) {
    const intPath = spec.internalGetPath.replace("{id}", getByPath<string>(data, "id") ?? uid ?? "");
    tabs.push({
      id: "jsonint",
      label: "JSON (internal)",
      eyebrow: "JSON",
      render: () => (
        <div>
          <JsonIntView path={intPath} />
        </div>
      ),
    });
  }

  // ── form-panel (зона 3) ──
  let mainOverride: ReactNode | undefined;
  if (mode === "edit") {
    mainOverride = (
      <InlineResourceForm
        spec={spec}
        action="edit"
        id={uid}
        data={data}
        projectId={projectId ?? ""}
        onCancel={() => navigate(detailBase)}
        onSuccess={() => {
          invalidate(spec.id, projectId);
          navigate(detailBase);
        }}
      />
    );
  } else if (mode === "child-create" && childRoute) {
    const childSpec = specByRoute(childRoute);
    if (childSpec) {
      const back = `${detailBase}/${childRoute}`;
      const rel = related.find((r) => REGISTRY[r.childId]?.route === childRoute);
      const ff = rel ? (Array.isArray(rel.filterField) ? rel.filterField[0] : rel.filterField) : undefined;
      mainOverride = (
        <InlineResourceForm
          spec={childSpec}
          action="create"
          projectId={projectId ?? ""}
          networkId={spec.id === "networks" ? uid : undefined}
          subnetId={spec.id === "subnets" ? uid : undefined}
          presetFields={ff ? { [ff]: uid } : undefined}
          onCancel={() => navigate(back)}
          onSuccess={() => navigate(back)}
        />
      );
    } else {
      // childRoute не в REGISTRY → кастомная embedded create-форма расширения
      // (напр. «privileges» → AccessBinding с залоченным субъектом). Форма сама
      // навигирует через onSuccess/onCancel (extCtx.navigate).
      mainOverride = ext?.childCreate?.(childRoute, extCtx) ?? undefined;
    }
  }

  // Активный таб — из pathname (path-based, уникальный URI на таб).
  const sub = location.pathname.startsWith(detailBase)
    ? location.pathname.slice(detailBase.length).replace(/^\/+/, "")
    : "";
  const seg0 = sub.split("/")[0];
  let activeTabId = "overview";
  if (mode === "child-create" && childRoute) activeTabId = childRoute;
  else if (mode === "edit") activeTabId = "overview";
  else if (seg0 && tabs.some((t) => t.id === seg0)) activeTabId = seg0;

  const onTabSelect = (id: string) => {
    if (id === "overview") navigate(detailBase);
    else navigate(`${detailBase}/${id}`);
  };

  // Зона-2 шапка для форм (edit/child-create): действие + тип + иконка ресурса
  // формы — контекст переезжает в блок табов, форма в зоне 3 свою шапку не дублирует.
  const childForHeader = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
  // Кастомные child-create-роуты (нет в REGISTRY): тип/иконка из небольшой карты,
  // чтобы зона-2 не падала и показывала осмысленный заголовок (напр. privileges →
  // «Привязка доступа» + иконка access-bindings).
  const CUSTOM_CHILD_HEADER: Record<string, { title: string; specId: string }> = {
    privileges: { title: "Привязка доступа", specId: "access-bindings" },
  };
  const customChild =
    mode === "child-create" && childRoute && !childForHeader ? CUSTOM_CHILD_HEADER[childRoute] : undefined;
  const headerEyebrow = mode === "edit" ? "Редактирование" : mode === "child-create" ? "Создание" : undefined;
  const headerTitle =
    mode === "edit"
      ? spec.plural
      : mode === "child-create"
        ? childForHeader?.plural ?? customChild?.title
        : undefined;
  const headerIcon =
    mode === "child-create" && childForHeader ? (
      <ResourceIcon specId={childForHeader.id} />
    ) : mode === "child-create" && customChild ? (
      <ResourceIcon specId={customChild.specId} />
    ) : undefined;

  return (
    // Прокидываем иконку ресурса вниз — все SectionHeader табов получают её
    // (единая шапка с формами через PanelHeader).
    <DetailHeaderProvider value={{ icon: <ResourceIcon specId={spec.id} /> }}>
      <DetailShell
        resourceLabel={spec.genitive ?? spec.plural}
        resourceName={name}
        nameEyebrow={spec.singular}
        tabs={tabs}
        docLinks={(spec.docs as DocLink[] | undefined) ?? []}
        mainOverride={mainOverride}
        activeTabId={activeTabId}
        onTabSelect={onTabSelect}
        headerEyebrow={headerEyebrow}
        headerTitle={headerTitle}
        headerIcon={headerIcon}
      />
    </DetailHeaderProvider>
  );
}
