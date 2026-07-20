// ResourceDetailPage — детальная страница ресурса (flat API, 1.0).
// Поллит GET <spec.apiPath>/{id} каждые 3 сек.
// Restart/Start/Stop → POST <spec.apiPath>/{id}:verb → Operation.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, Link, useSearchParams, useLocation } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Descriptions, Dropdown, Space, Spin, Tag, Typography } from "antd";
import type { MenuProps } from "antd";
import {
  ArrowLeftOutlined,
  ReloadOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  EditOutlined,
  DeleteOutlined,
  PlusOutlined,
  MoreOutlined,
  DragOutlined,
} from "@ant-design/icons";
import { LazyJsonMonacoView } from "@shared/components/molecules/JsonMonacoView";
import { formatDateTime } from "@shared/lib/datetime";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { InlineResourceEditForm } from "@shared/components/organisms/InlineResourceEditForm";
import { OperationsTab } from "@shared/components/organisms/OperationsTab";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { DeleteDialog } from "@shared/components/molecules/DeleteDialog";
import { MoveStubDialog } from "@shared/components/molecules/MoveStubDialog";
import { OperationDialog, extractOperationId } from "@shared/components/molecules/OperationDialog";
import { DetailShell, type DetailTab } from "@shared/components/organisms/DetailShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { api, ApiError } from "@shared/api/client";
import { useProjectStore } from "@shared/lib/context-store";
import { getByPath, resourceProjectPath, type ResourceSpec } from "@shared/lib/resource-registry";
import { ReferrerLink } from "@shared/lib/spec-columns";
import { useInvalidateResourceList } from "@shared/lib/use-operation";

interface Props {
  spec: ResourceSpec;
  paramKey?: string;
  extraTabs?: (data: Record<string, unknown>) => DetailTab[];
  /** Опциональный ряд secondary-actions кнопок над tab content (Subnet «Перенести в зону»). */
  secondaryActions?: (data: Record<string, unknown>) => React.ReactNode;
  /** По умолчанию показывается JSON-tab последним. Установить true чтобы скрыть. */
  hideJsonTab?: boolean;
  /** По умолчанию для VPC-ресурсов добавляется tab "Операции" с per-resource
   *  ListOperations. Установить true чтобы скрыть (например, для admin-ресурсов
   *  без LRO — Region/Zone/AddressPool). */
  hideOperationsTab?: boolean;
  /** Per-tab override header-right slot. Возвращает null/undefined → fallback на default
   *  (Создать <singular> + Редактировать + kebab Move/Delete). */
  headerActionsByTab?: (tabId: string, data: Record<string, unknown>) => React.ReactNode | null | undefined;
  /** Подменить primary "Создать <singular>" в default overview-actions на другую кнопку.
   *  Например, на Network detail логично "Создать подсеть" вместо "Создать Network". */
  overviewCreateOverride?: { label: string; onClick: () => void };
  /** Добавить дополнительные секции в Обзор-tab после "Общее"-Descriptions.
   *  Используется для inline-таблиц дочерних ресурсов (Network → Подсети). */
  overviewExtras?: (data: Record<string, unknown>) => React.ReactNode;
  /** Полностью заменить содержимое Обзор-tab (вместо Descriptions + overviewExtras).
   *  Используется когда Overview переходит в edit-state inline (например,
   *  Network detail → "Создать подсеть" разворачивает форму на месте "Общее"). */
  overviewReplace?: (data: Record<string, unknown>) => React.ReactNode;
  /** Если true — primary-create кнопка в default overview-actions скрыта.
   *  Полезно когда форма создания уже развёрнута через overviewReplace. */
  hideOverviewCreate?: boolean;
  /** Опциональный override URL для back-навигации и breadcrumb-ссылки на список.
   *  По умолчанию вычисляется как `/projects/<projectId>/<spec.route>`. Используется
   *  для nested-роутов (Subnet под Network → back к network detail). */
  backHrefOverride?: string;
  /** Опциональный override label для back-link breadcrumb (если задан). */
  backLabelOverride?: string;
  /** Опциональная цепочка breadcrumb-сегментов между serviceTitle и текущим
   *  ресурсом. Сегмент без href — не кликабелен. По умолчанию используется
   *  один сегмент `{label: spec.plural, href: backHref}`. */
  breadcrumbSegments?: Array<{ label: string; href?: string }>;
  /** Если задано — клик по "Редактировать" вызовет этот callback вместо
   *  встроенной inline-edit логики. Редко нужен. */
  onEditClick?: () => void;
  /** Опциональный override inline-edit формы. Если задан — рендерится вместо
   *  generic InlineResourceEditForm когда detail в edit-mode. Используется для
   *  resource-specific layouts (например, кастомные формы для subnet). */
  renderInlineEdit?: (data: Record<string, unknown>, exitEdit: () => void) => React.ReactNode;
}

export function ResourceDetailPage({
  spec,
  paramKey = "uid",
  extraTabs,
  secondaryActions,
  hideJsonTab,
  hideOperationsTab,
  headerActionsByTab,
  overviewCreateOverride,
  overviewExtras,
  overviewReplace,
  hideOverviewCreate,
  backHrefOverride,
  backLabelOverride,
  breadcrumbSegments,
  onEditClick,
  renderInlineEdit,
}: Props) {
  const params = useParams();
  const uid = params[paramKey];
  const navigate = useNavigate();
  const location = useLocation();
  const project = useProjectStore((s) => s.project);
  const invalidate = useInvalidateResourceList();
  const [searchParams] = useSearchParams();

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);

  // KAC-70: edit-flow перенесён в модалку (ResourceFormModal). Старый /edit
  // URL — back-compat: редиректим на detail + ?modal=<spec>-edit&id=<uid>.
  // editing-state больше не используется здесь — модалка сама управляет.
  const isEditUrl = location.pathname.endsWith("/edit");
  const detailPath = isEditUrl ? location.pathname.slice(0, -"/edit".length) : location.pathname;
  useEffect(() => {
    if (isEditUrl && uid) {
      const params = new URLSearchParams(searchParams);
      params.set("modal", `${spec.id}-edit`);
      params.set("id", uid);
      navigate(`${detailPath}?${params.toString()}`, { replace: true });
    }
  }, [isEditUrl, uid, detailPath, navigate, searchParams, spec.id]);

  const enterEdit = useCallback(() => {
    if (!uid) return;
    const params = new URLSearchParams(searchParams);
    params.set("modal", `${spec.id}-edit`);
    params.set("id", uid);
    navigate(`${detailPath}?${params.toString()}`, { replace: false });
  }, [detailPath, navigate, searchParams, spec.id, uid]);

  const exitEdit = useCallback(() => {
    const params = new URLSearchParams(searchParams);
    params.delete("modal");
    params.delete("id");
    const qs = params.toString();
    navigate(qs ? `${detailPath}?${qs}` : detailPath, { replace: true });
  }, [detailPath, navigate, searchParams]);

  // editing — legacy для renderInlineEdit. С KAC-70 edit-flow в модалке,
  // inline-edit не используется → всегда false.
  const editing = false;

  const { data, isLoading, isError, error } = useQuery({
    queryKey: [spec.id, "detail", uid],
    queryFn: () => api.get<Record<string, unknown>>(`${spec.apiPath}/${uid}`),
    refetchInterval: 3_000,
    enabled: !!uid,
    staleTime: 0,
  });

  const [actionOpId, setActionOpId] = useState<string | null>(null);
  const [actionTitle, setActionTitle] = useState("Action");
  const [actionErr, setActionErr] = useState<string | null>(null);

  const handleActionDone = useCallback(() => {
    setActionOpId(null);
    invalidate(spec.id, project?.id);
  }, [invalidate, spec.id, project?.id]);

  const actionMutation = useMutation({
    mutationFn: (verb: string) => api.action(`${spec.apiPath}/${uid}:${verb}`),
    onSuccess: (resp) => {
      setActionErr(null);
      const id = extractOperationId(resp);
      if (id) setActionOpId(id);
      else invalidate(spec.id, project?.id);
    },
    onError: (e) => {
      setActionErr(e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message);
    },
  });

  const doAction = (verb: string, title: string) => {
    setActionTitle(title);
    setActionErr(null);
    actionMutation.mutate(verb);
  };

  const name = data ? (getByPath<string>(data, "name") ?? "") : "";
  const statusValue = data ? getByPath<string>(data, "status") : undefined;
  const resourceId = data ? (getByPath<string>(data, "id") ?? uid ?? "") : (uid ?? "");
  const editPath = `${spec.apiPath}/${resourceId}`;

  const backHref = useMemo(() => {
    if (backHrefOverride) return backHrefOverride;
    const projectId = params.projectId;
    // KAC-198: include service segment (vpc/compute/nlb) so back-button
    // ведёт на actual listing route в App.tsx (раньше `/projects/<pid>/<route>`
    // не матчился → SPA fallback на blank).
    const listPath = resourceProjectPath(spec.id, projectId);
    if (listPath) return listPath;
    // KAC-124: Resource Manager (Organization/Cloud/Folder) удалён — заменён на
    // IAM (Account/Project). Fallback ведёт в IAM Projects list.
    return "/iam/projects";
  }, [params.projectId, spec.id, backHrefOverride]);

  const segments = useMemo(
    () =>
      breadcrumbSegments && breadcrumbSegments.length > 0
        ? breadcrumbSegments
        : [{ label: backLabelOverride ?? spec.plural, href: backHref }],
    [breadcrumbSegments, backLabelOverride, spec.plural, backHref],
  );

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        {segments.map((seg, i) => (
          <span key={i} style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            {seg.href ? (
              <Link to={seg.href}>
                <Typography.Text type="secondary">{seg.label}</Typography.Text>
              </Link>
            ) : (
              <Typography.Text type="secondary">{seg.label}</Typography.Text>
            )}
            <Typography.Text type="secondary">/</Typography.Text>
          </span>
        ))}
        <Typography.Text strong style={{ maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis" }}>
          {name || resourceId}
        </Typography.Text>
      </span>
    ),
    [segments, spec.serviceTitle, name, resourceId],
  );
  useBreadcrumb(breadcrumb);

  // Move-capable: те же ресурсы, что в RowActionsMenu (Account/Project/Region/Zone/AddressPool — нет).
  const moveCapable = useMemo(
    () => !["accounts", "projects", "regions", "zones", "address-pools"].includes(spec.id),
    [spec.id],
  );

  const overviewActions = useMemo(() => {
    const kebabItems: MenuProps["items"] = [
      moveCapable
        ? {
            key: "move",
            icon: <DragOutlined />,
            label: "Переместить",
            onClick: () => setMoveOpen(true),
          }
        : null,
      spec.ops.delete && data
        ? {
            key: "delete",
            icon: <DeleteOutlined />,
            label: "Удалить",
            danger: true,
            onClick: () => setDeleteOpen(true),
          }
        : null,
    ].filter(Boolean) as MenuProps["items"];

    return (
      <Space size="small">
        {/* Primary "Создать ..." кнопка показывается ТОЛЬКО когда detail-страница
            явно объявляет дочернюю сущность через overviewCreateOverride
            (например, Network → "Создать подсеть"). Default "Создать <self>"
            убран: на detail-странице ресурса логично создавать связанные
            сущности, а не клонировать сам ресурс — для этого есть list. */}
        {hideOverviewCreate || !overviewCreateOverride ? null : (
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={overviewCreateOverride.onClick}>
            {overviewCreateOverride.label}
          </Button>
        )}
        {spec.ops.restart && (
          <Button
            size="small"
            icon={<ReloadOutlined spin={actionMutation.isPending && actionMutation.variables === "restart"} />}
            onClick={() => doAction("restart", "Restarting")}
            disabled={actionMutation.isPending}
          >
            Перезапустить
          </Button>
        )}
        {spec.ops.start && (
          <Button
            size="small"
            icon={<PlayCircleOutlined />}
            onClick={() => doAction("start", "Starting")}
            disabled={actionMutation.isPending}
          >
            Запустить
          </Button>
        )}
        {spec.ops.stop && (
          <Button
            size="small"
            icon={<PauseCircleOutlined />}
            onClick={() => doAction("stop", "Stopping")}
            disabled={actionMutation.isPending}
          >
            Остановить
          </Button>
        )}
        {spec.ops.update && data && !editing && (
          <Button size="small" icon={<EditOutlined />} onClick={() => (onEditClick ? onEditClick() : enterEdit())}>
            Редактировать
          </Button>
        )}
        {kebabItems && kebabItems.length > 0 && (
          <Dropdown menu={{ items: kebabItems }} trigger={["click"]} placement="bottomRight">
            <Button size="small" icon={<MoreOutlined />} aria-label="Действия" />
          </Dropdown>
        )}
      </Space>
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    spec,
    data,
    moveCapable,
    backHref,
    overviewCreateOverride,
    hideOverviewCreate,
    onEditClick,
    enterEdit,
    editing,
    location.pathname,
    actionMutation.isPending,
    actionMutation.variables,
  ]);

  // Per-tab header CTA (через ?tab) — если задано и возвращает не-null,
  // используется вместо overviewActions. useMemo обязателен, иначе
  // useHeaderRight видит новый node-ref на каждый рендер и зацикливает setState.
  const activeTabId = searchParams.get("tab") ?? "overview";
  const finalHeaderRight = useMemo(() => {
    if (!data) return null;
    const override = headerActionsByTab ? headerActionsByTab(activeTabId, data) : null;
    return override ?? overviewActions;
  }, [headerActionsByTab, activeTabId, data, overviewActions]);
  useHeaderRight(finalHeaderRight);

  if (isLoading && !data) {
    return (
      <div style={{ padding: 24 }}>
        <Spin tip="Загрузка…" />
      </div>
    );
  }

  if (isError && !data) {
    return (
      <ErrorResult
        error={error}
        extra={
          <Link to={backHref}>
            <Button icon={<ArrowLeftOutlined />}>Назад</Button>
          </Link>
        }
      />
    );
  }

  if (!data) {
    return (
      <ErrorResult
        status="404"
        subTitle="Ресурс не найден."
        extra={
          <Link to={backHref}>
            <Button icon={<ArrowLeftOutlined />}>Назад</Button>
          </Link>
        }
      />
    );
  }

  const overviewItems = [
    { label: "ID", value: <CopyableId id={resourceId} /> },
    { label: "Имя", value: name || "—" },
    statusValue ? { label: "Статус", value: <StatusBadge state={statusValue} /> } : null,
    getByPath<string>(data, "created_at")
      ? {
          label: "Дата создания",
          value: formatDateTime(getByPath<string>(data, "created_at")),
        }
      : null,
    getByPath<string>(data, "project_id")
      ? { label: "Проект", value: <CopyableId id={getByPath<string>(data, "project_id")!} /> }
      : null,
    getByPath<string>(data, "zone_id")
      ? {
          label: "Зона",
          value: <Typography.Text code>{getByPath<string>(data, "zone_id")!}</Typography.Text>,
        }
      : null,
    getByPath<string>(data, "network_id")
      ? {
          label: "Сеть",
          value: <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")!} />,
        }
      : null,
    getByPath<string>(data, "description")
      ? { label: "Описание", value: getByPath<string>(data, "description")! }
      : null,
    // Address-specific boolean fields: reserved/used (см. Address в types.ts).
    // Generic — рендерятся для любого ресурса, у которого эти поля есть.
    typeof getByPath<boolean>(data, "reserved") === "boolean"
      ? {
          label: "Зарезервирован",
          value: getByPath<boolean>(data, "reserved") ? "Да" : "Нет",
        }
      : null,
    typeof getByPath<boolean>(data, "used") === "boolean"
      ? {
          label: "Используется",
          value: getByPath<boolean>(data, "used") ? "Да" : "Нет",
        }
      : null,
    // Network-specific: declared supernet (VPC-1) — IPv4 then IPv6, multi-line.
    spec.id === "networks"
      ? {
          label: "Супернет",
          value: (() => {
            const v4 = (getByPath<string[]>(data, "ipv4_cidr_blocks") ?? []) as string[];
            const v6 = (getByPath<string[]>(data, "ipv6_cidr_blocks") ?? []) as string[];
            const all = [...v4, ...v6];
            if (all.length === 0) return <Typography.Text type="secondary">—</Typography.Text>;
            return (
              <Space direction="vertical" size={2} style={{ width: "100%" }}>
                {all.map((c, i) => (
                  <Typography.Text key={i} code style={{ fontFamily: "monospace" }}>
                    {c}
                  </Typography.Text>
                ))}
              </Space>
            );
          })(),
        }
      : null,
    // Network-specific: Группа безопасности по умолчанию.
    spec.id === "networks" && getByPath<string>(data, "default_security_group_id")
      ? {
          label: "Группа безопасности по умолчанию",
          value: <RefNameLink specId="security-groups" refId={getByPath<string>(data, "default_security_group_id")!} />,
        }
      : null,
    // Network-specific: system-provisioned default RT (VPC-1, echoed on create).
    spec.id === "networks" && getByPath<string>(data, "default_route_table_id")
      ? {
          label: "Таблица маршрутизации по умолчанию",
          value: <RefNameLink specId="route-tables" refId={getByPath<string>(data, "default_route_table_id")!} />,
        }
      : null,
    // SecurityGroup-specific: Правила (count + empty state).
    spec.id === "security-groups"
      ? {
          label: "Правила",
          value: (() => {
            const rules = (getByPath<unknown[]>(data, "rules") ?? []) as unknown[];
            if (rules.length === 0) {
              return (
                <Space direction="vertical" size={2}>
                  <Typography.Text type="secondary">empty</Typography.Text>
                  <Typography.Text strong>Задайте правила для группы безопасности</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    Правила управляют входящим трафиком ВМ.
                  </Typography.Text>
                </Space>
              );
            }
            return <Typography.Text>{rules.length} правило(а)</Typography.Text>;
          })(),
        }
      : null,
    // Subnet-specific: derived placement (ZONAL zone / REGIONAL region).
    spec.id === "subnets"
      ? {
          label: "Размещение",
          value: (() => {
            const region = getByPath<string>(data, "region_id") ?? "";
            const zone = getByPath<string>(data, "zone_id") ?? "";
            const pt = getByPath<string>(data, "placement_type") ?? "";
            const isRegional = pt === "REGIONAL" || (!zone && !!region);
            const anchor = isRegional ? region : zone;
            return (
              <Space size={8}>
                <Tag color={isRegional ? "geekblue" : "blue"}>{isRegional ? "REGIONAL" : "ZONAL"}</Tag>
                {anchor ? (
                  <Typography.Text code style={{ fontFamily: "monospace" }}>
                    {anchor}
                  </Typography.Text>
                ) : (
                  <Typography.Text type="secondary">—</Typography.Text>
                )}
              </Space>
            );
          })(),
        }
      : null,
    // Subnet-specific: IPv4 CIDR — immutable primary anchor + additional ranges
    // (managed via :add/:remove-cidr-blocks). v4_cidr_blocks[] retired (VPC-1).
    spec.id === "subnets"
      ? {
          label: "IPv4 CIDR",
          value: (() => {
            const primary = getByPath<string>(data, "ipv4_cidr_primary") ?? "";
            const extra = (getByPath<string[]>(data, "ipv4_cidr_blocks") ?? []) as string[];
            const all = [...(primary ? [primary] : []), ...extra];
            if (all.length === 0) return <Typography.Text type="secondary">—</Typography.Text>;
            return (
              <Space direction="vertical" size={2} style={{ width: "100%" }}>
                {all.map((c, i) => (
                  <Typography.Text key={i} code style={{ fontFamily: "monospace" }}>
                    {c}
                    {i === 0 && primary ? " · основной" : ""}
                  </Typography.Text>
                ))}
              </Space>
            );
          })(),
        }
      : null,
    // Subnet-specific: IPv6 CIDR — primary anchor + additional ranges.
    spec.id === "subnets"
      ? {
          label: "IPv6 CIDR",
          value: (() => {
            const primary = getByPath<string>(data, "ipv6_cidr_primary") ?? "";
            const extra = (getByPath<string[]>(data, "ipv6_cidr_blocks") ?? []) as string[];
            const all = [...(primary ? [primary] : []), ...extra];
            if (all.length === 0) return <Typography.Text type="secondary">—</Typography.Text>;
            return (
              <Space direction="vertical" size={2} style={{ width: "100%" }}>
                {all.map((c, i) => (
                  <Typography.Text key={i} code style={{ fontFamily: "monospace" }}>
                    {c}
                    {i === 0 && primary ? " · основной" : ""}
                  </Typography.Text>
                ))}
              </Space>
            );
          })(),
        }
      : null,
    spec.id === "subnets" && getByPath<string>(data, "route_table_id")
      ? {
          label: "Таблица маршрутизации",
          value: <CopyableId id={getByPath<string>(data, "route_table_id")!} />,
        }
      : null,
    // NetworkInterface: MAC address (output-only, KAC-48). Префикс 0e: + 40 бит
    // crypto/rand, стабилен на жизни NIC, уникален в пределах cloud.
    spec.id === "network-interfaces" && getByPath<string>(data, "mac_address")
      ? {
          label: "MAC",
          value: <CopyableId id={getByPath<string>(data, "mac_address")!} />,
        }
      : null,
  ].filter(Boolean) as { label: string; value: React.ReactNode }[];

  const tabs: DetailTab[] = [
    {
      id: "overview",
      label: "Обзор",
      render: () =>
        actionErr ? (
          // При ошибке action (Restart/Start/Stop/etc) показываем ТОЛЬКО ErrorResult,
          // скрывая Общее + overviewExtras + inline-edit. Иначе таблицы дочерних
          // ресурсов остаются ниже центрированного Result — визуальный bug.
          <ErrorResult subTitle={actionErr} />
        ) : (
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            {editing && spec.ops.update ? (
              renderInlineEdit ? (
                renderInlineEdit(data, exitEdit)
              ) : (
                <InlineResourceEditForm spec={spec} data={data} projectId={project?.id ?? null} onCancel={exitEdit} />
              )
            ) : overviewReplace ? (
              overviewReplace(data)
            ) : (
              <>
                <Descriptions
                  title="Общее"
                  bordered
                  column={1}
                  size="small"
                  labelStyle={{ width: 200 }}
                  items={overviewItems.map((it, i) => ({
                    key: String(i),
                    label: it.label,
                    children: it.value,
                  }))}
                />
                {/* Generic — рендерится для любого ресурса с непустым used_by
                  (kacho.cloud.reference.Reference[]). Для Address — кто
                  использует адрес (ephemeral compute NIC, и т.д.). */}
                <UsedByBlock data={data} />
                {overviewExtras && overviewExtras(data)}
              </>
            )}
          </Space>
        ),
    },
    ...(extraTabs ? extraTabs(data) : []),
    ...(hideOperationsTab
      ? []
      : [
          {
            id: "operations",
            label: "Операции",
            render: () => <OperationsTab spec={spec} resourceId={resourceId} />,
          },
        ]),
    ...(spec.internalGetPath
      ? [
          {
            id: "jsonint",
            label: "jsonint",
            render: () => (
              <JsonIntTab
                path={spec.internalGetPath!.replace("{id}", resourceId)}
                queryKey={[spec.id, "jsonint", resourceId]}
              />
            ),
          },
        ]
      : []),
    ...(hideJsonTab
      ? []
      : [
          {
            id: "raw",
            label: "JSON",
            render: () => <LazyJsonMonacoView data={data} />,
          },
        ]),
  ];

  return (
    <>
      <DetailShell
        resourceLabel={spec.genitive ?? spec.plural}
        resourceName={name || resourceId}
        nameEyebrow={spec.singular}
        badges={statusValue ? <StatusBadge state={statusValue} /> : null}
        tabs={tabs}
        secondaryActions={secondaryActions ? secondaryActions(data) : undefined}
      />

      {spec.ops.delete && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          apiPath={editPath}
          resourceId={spec.id}
          resourceLabel={spec.singular}
          name={name || resourceId}
          projectId={project?.id ?? null}
          onSuccess={() => navigate(backHref)}
        />
      )}

      {moveCapable && (
        <MoveStubDialog
          open={moveOpen}
          onOpenChange={setMoveOpen}
          resourceLabel={spec.singular}
          name={name || resourceId}
          apiPath={editPath}
        />
      )}

      <OperationDialog opId={actionOpId} title={actionTitle} onSuccess={handleActionDone} onClose={handleActionDone} />
    </>
  );

  // Suppress unused
  void navigate;
}

// JsonIntTab — generic "jsonint" tab: GET <internalGetPath с подставленным {id}>
// и pretty-print JSON-ответа (read-only Monaco viewer, тот же, что у "JSON"-таба).
// Показывается только для ресурсов с spec.internalGetPath (internal/infra-проекция
// ресурса — Network → +vpn_id; NetworkInterface → +hv_id/sid/host_iface/...).
// 404 / not-implemented → дружелюбное сообщение вместо raw-ошибки.
function JsonIntTab({ path, queryKey }: { path: string; queryKey: unknown[] }) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey,
    queryFn: () => api.get<unknown>(path),
    // JSON-таб — read-only снимок; частый поллинг только гонял бы Monaco. Обновляем
    // существенно реже (перекормка редактора вместо реального обновления UX).
    refetchInterval: 30_000,
    staleTime: 10_000,
  });

  if (isLoading && data === undefined) {
    return <Spin tip="Загрузка…" />;
  }
  if (isError) {
    const notFound = error instanceof ApiError && (error.status === 404 || error.status === 501);
    return (
      <ErrorResult
        error={notFound ? undefined : error}
        status={notFound ? "404" : undefined}
        title={notFound ? "404" : undefined}
        subTitle={notFound ? "Internal-проекция для этого ресурса недоступна." : undefined}
      />
    );
  }
  return <LazyJsonMonacoView data={data} />;
}

// UsedByBlock — generic "Used by" rendering for any resource whose API response
// has an output-only `used_by` list of kacho.cloud.reference.Reference. For
// Address: ephemeral compute NIC addresses come back with
// used_by=[{referrer:{type:"compute_instance", id:<instance id>}}]; reserved
// user addresses get the same when attached to an instance. Renders nothing if
// `used_by` is absent or empty. Каждый referrer рендерится как «<Tag>{label}</Tag>
// {id}» в одном кликабельном <Link> (для известных referrer-типов) либо plain
// (для unknown — forward-compat fallback), через общий ReferrerLink helper
// (та же визуальная форма, что и в list-view used_by column). projectId берём
// из data.project_id, либо из URL-параметров (:projectId) как fallback. В отличие
// от list-view, здесь все рефереры показаны полностью (нет "+N" — stack-вью).
function UsedByBlock({ data }: { data: Record<string, unknown> }) {
  const params = useParams();
  const projectId = (getByPath<string>(data, "project_id") || null) ?? params.projectId ?? null;
  const raw = getByPath<unknown>(data, "used_by");
  if (!Array.isArray(raw) || raw.length === 0) return null;
  const items = raw as Array<{
    referrer?: { type?: string; id?: string };
    type?: string;
  }>;
  return (
    <div className="rounded-lg border border-border p-4 space-y-2">
      <h3 className="font-semibold text-sm">Used by</h3>
      <ul className="space-y-1 text-sm">
        {items.map((r, i) => {
          const type = r.referrer?.type ?? "?";
          const id = r.referrer?.id ?? "";
          return (
            <li key={`${type}-${id}-${i}`} className="flex items-center gap-2">
              <ReferrerLink projectId={projectId} referrer={r.referrer} />
            </li>
          );
        })}
      </ul>
    </div>
  );
}
