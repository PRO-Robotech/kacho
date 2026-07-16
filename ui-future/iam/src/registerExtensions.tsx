// registerExtensions — регистрация доменных IAM-расширений generic detail-страницы
// и inline-форм. Импортируется как side-effect входной точкой IAM-remote (IamPage),
// поэтому расширения подключаются на старте бандла, до рендера страниц.
//
// shared/-компоненты (ResourceShell / InlineResourceForm) остаются app-agnostic:
// доменная специфика IAM (привилегии субъекта, токены SA/User, Role-формы,
// участники группы) инжектится ЗДЕСЬ через registerDetailExtension /
// registerInlineForm, а не хардкодится в shared.

import { useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { PlusOutlined, KeyOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { Button, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";

import {
  registerDetailExtension,
  type DetailExtCtx,
  type DescItem,
} from "@shared/components/organisms/ResourceDetailExtensions";
import { registerInlineForm } from "@shared/components/organisms/InlineResourceForm";
import type { DetailTab } from "@shared/components/organisms/DetailShell";
import { IamRefLink } from "@shared/components/molecules/IamRefLink";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { CopyableMonoId, fmtTs } from "@shared/components/organisms/iam/IamCommon";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { getByPath } from "@shared/lib/resource-registry";
import { iamApi, type AccessBinding, type Group, type SubjectPrivilege, type User } from "@shared/api/iam";

import { useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { GroupMembersPanel } from "@/pages/iam/GroupsPage";
import { AccessBindingCreateForm, type SubjectType } from "@/components/organisms/iam/AccessBindingCreateForm";
import { SaKeysPanel } from "@/components/organisms/SaKeysPanel";
import { UserTokensPanel } from "@/components/organisms/UserTokensPanel";
import { InlineRoleCreateForm } from "@/components/organisms/iam/InlineRoleCreateForm";
import { InlineRoleEditForm } from "@/components/organisms/iam/InlineRoleEditForm";

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function mono(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? <span style={{ fontFamily: "ui-monospace, monospace", fontSize: 12 }}>{s}</span> : dash;
}

// ─────────────────────── IAM: привилегии субъекта ───────────────────────
// «Привилегии» — вложенный таб detail-страницы IAM-ресурса. Показывает
// AccessBinding'и, где данный ресурс — субъект (User/ServiceAccount/Group,
// listBySubject) либо ресурс-скоуп (Account, listByResource). Только чтение;
// выдача/отзыв — на странице «Привязки доступа».

type PrivilegesMode =
  | { kind: "subject"; subjectType: "user" | "service_account" | "group"; subjectId: string }
  | { kind: "resource"; resourceType: "account" | "project" | "cluster"; resourceId: string };

// Цвет тега типа субъекта — единая палитра со страницей «Привязки доступа».
function subjectColor(t: string): string {
  switch (t) {
    case "user":
      return "blue";
    case "service_account":
      return "gold";
    case "group":
      return "purple";
    default:
      return "default";
  }
}

// Цвет тега scope-tier'а (Область) — output-only поле AccessBinding.scope.
function scopeColor(s: string): string {
  switch (s) {
    case "CLUSTER":
      return "red";
    case "ACCOUNT":
      return "blue";
    case "PROJECT":
      return "green";
    default:
      return "default";
  }
}

// SubjectPrivilegesTab — «Привилегии»-вкладка. По оси субъекта (User/SA/Group)
// показывает SubjectPrivilege'и (listSubjectPrivileges: self ИЛИ account-admin →
// админ видит привилегии SA/юзера), по оси ресурса-скоупа (Account) —
// AccessBinding'и (listByResource). Только чтение; выдача/отзыв — на «Привязках».
function SubjectPrivilegesTab({ mode }: { mode: PrivilegesMode }) {
  return mode.kind === "subject" ? (
    <SubjectPrivilegesSubjectTable mode={mode} />
  ) : (
    <SubjectPrivilegesResourceTable mode={mode} />
  );
}

// PrivilegesTableShell — общий каркас таблицы привилегий: fill-контейнер + скролл.
// Ошибку запроса поднимаем через ErrorResult (различаем 403/недоступность от
// честного «пусто»), а не показываем ложный empty-state «Привилегий нет.».
function PrivilegesTableShell<T extends object>({
  loading,
  isError,
  error,
  rows,
  columns,
  rowKey,
}: {
  loading: boolean;
  isError: boolean;
  error: unknown;
  rows: T[];
  columns: ColumnsType<T>;
  rowKey: string;
}) {
  const { wrapRef, scrollY } = useTableScrollY();
  if (isError) return <ErrorResult error={error} />;
  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <Table<T>
          rowKey={rowKey}
          size="small"
          className="kc-table"
          loading={loading}
          dataSource={rows}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          locale={{ emptyText: "Привилегий нет." }}
          data-testid="subject-privileges-table"
        />
      </div>
    </div>
  );
}

// SubjectPrivilegesSubjectTable — привилегии субъекта через listSubjectPrivileges.
// role_name резолвит сервер (dangling role → пусто, fallback на role_id) —
// локальный roleNameById-резолв тут НЕ нужен. Субъект фиксирован → колонки:
// Роль | Ресурс | Область | Создано.
function SubjectPrivilegesSubjectTable({
  mode,
}: {
  mode: { kind: "subject"; subjectType: "user" | "service_account" | "group"; subjectId: string };
}) {
  const list = useQuery({
    queryKey: ["iam", "subject-privileges", mode.subjectType, mode.subjectId],
    queryFn: () => iamApi.listSubjectPrivileges(mode.subjectType, mode.subjectId, { page_size: "200" }),
    enabled: !!mode.subjectId,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const privileges = list.data?.privileges ?? [];

  const columns: ColumnsType<SubjectPrivilege> = [
    {
      title: "Роль",
      key: "role",
      render: (_v, row) => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          {row.role_name && <Typography.Text strong>{row.role_name}</Typography.Text>}
          <CopyableMonoId id={row.role_id} />
        </span>
      ),
    },
    {
      title: "Ресурс",
      key: "resource",
      render: (_v, row) => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          {row.resource_type && <Tag>{row.resource_type}</Tag>}
          {row.resource_id ? <CopyableMonoId id={row.resource_id} /> : dash}
        </span>
      ),
    },
    {
      title: "Область",
      dataIndex: "scope",
      key: "scope",
      width: 120,
      render: (v?: string) =>
        v && v !== "SCOPE_UNSPECIFIED" ? (
          <Tag color={scopeColor(v)}>{v}</Tag>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: "Создано",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      render: (v) => fmtTs(v as string | undefined),
    },
  ];

  return (
    <PrivilegesTableShell<SubjectPrivilege>
      loading={list.isLoading}
      isError={list.isError}
      error={list.error}
      rows={privileges}
      columns={columns}
      rowKey="binding_id"
    />
  );
}

// SubjectPrivilegesResourceTable — привязки, выданные НА ресурс-скоуп (Account),
// через listByResource. Субъекты разные → резолвим email (user) + role_id → name.
// Ресурс-скоуп фиксирован → колонки: Субъект | Роль | Область | Создано.
function SubjectPrivilegesResourceTable({
  mode,
}: {
  mode: { kind: "resource"; resourceType: "account" | "project" | "cluster"; resourceId: string };
}) {
  const list = useQuery({
    queryKey: ["iam", "access-bindings", "by-resource", mode.resourceType, mode.resourceId],
    queryFn: () => iamApi.listAccessBindingsByResource(mode.resourceType, mode.resourceId, { pageSize: "200" }),
    enabled: !!mode.resourceId,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  // Резолв role_id → name (как на странице «Привязки доступа»).
  const rolesList = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const roleNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const r of rolesList.data?.roles ?? []) m.set(r.id, r.name);
    return m;
  }, [rolesList.data]);

  // Субъекты разные — резолвим email для user.
  const usersList = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const userById = useMemo(() => {
    const m = new Map<string, User>();
    for (const u of usersList.data?.users ?? []) m.set(u.id, u);
    return m;
  }, [usersList.data]);

  const bindings = list.data?.access_bindings ?? [];

  const columns: ColumnsType<AccessBinding> = [
    {
      title: "Субъект",
      key: "subject",
      render: (_v, row) => {
        const u = row.subject_type === "user" ? userById.get(row.subject_id) : undefined;
        const human = u?.email || u?.display_name;
        return (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
            <Tag color={subjectColor(row.subject_type)}>{row.subject_type}</Tag>
            {human && (
              <Typography.Text strong style={{ fontSize: 12 }}>
                {human}
              </Typography.Text>
            )}
            <CopyableMonoId id={row.subject_id} />
          </span>
        );
      },
    },
    {
      title: "Роль",
      dataIndex: "role_id",
      key: "role",
      render: (v: string) => {
        const roleName = roleNameById.get(v);
        return (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            {roleName && <Typography.Text strong>{roleName}</Typography.Text>}
            <CopyableMonoId id={v} />
          </span>
        );
      },
    },
    {
      title: "Область",
      dataIndex: "scope",
      key: "scope",
      width: 120,
      render: (v?: string) =>
        v && v !== "SCOPE_UNSPECIFIED" ? (
          <Tag color={scopeColor(v)}>{v}</Tag>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: "Создано",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      render: (v) => fmtTs(v as string | undefined),
    },
  ];

  return (
    <PrivilegesTableShell<AccessBinding>
      loading={list.isLoading}
      isError={list.isError}
      error={list.error}
      rows={bindings}
      columns={columns}
      rowKey="id"
    />
  );
}

// GrantPrivilegeButton — CTA «Выдать доступ» в шапке страницы на табе «Привилегии»:
// разворачивает embedded create-форму в зоне-3 (`${detailBase}/privileges/create`)
// с залоченным субъектом — контекст ресурса сохраняется.
function GrantPrivilegeButton({ detailBase }: { detailBase: string }) {
  const navigate = useNavigate();
  return (
    <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate(`${detailBase}/privileges/create`)}>
      Выдать доступ
    </Button>
  );
}

// privilegesTab — DetailTab «Привилегии» для detail-страницы субъекта/скоупа.
function privilegesTab(mode: PrivilegesMode, detailBase: string): DetailTab {
  return {
    id: "privileges",
    label: "Привилегии",
    eyebrow: "Список",
    headerTitle: "Привилегии",
    headerIcon: <ResourceIcon specId="access-bindings" />,
    headerAction: <GrantPrivilegeButton detailBase={detailBase} />,
    fill: true,
    render: () => <SubjectPrivilegesTab mode={mode} />,
  };
}

// tokensTab — DetailTab «Токены» для detail-страницы сервисного аккаунта: список
// OAuth-ключей (SAKeyService) + выпуск токена с одноразовым показом секрета + отзыв.
function tokensTab(serviceAccountId: string): DetailTab {
  return {
    id: "tokens",
    label: "Токены",
    eyebrow: "Список",
    headerTitle: "Токены",
    headerIcon: <KeyOutlined />,
    fill: true,
    render: () => <SaKeysPanel serviceAccountId={serviceAccountId} />,
  };
}

// userTokensTab — DetailTab «Токены» для detail-страницы пользователя: список
// персональных OAuth-токенов (UserTokenService). Зеркалит tokensTab SA.
function userTokensTab(userId: string): DetailTab {
  return {
    id: "tokens",
    label: "Токены",
    eyebrow: "Список",
    headerTitle: "Токены",
    headerIcon: <KeyOutlined />,
    fill: true,
    render: () => <UserTokensPanel userId={userId} />,
  };
}

// privilegesChildCreate — билдер childCreate: embedded AccessBindingCreateForm в
// зоне-3. subject-режим → субъект ЗАЛОЧЕН (subjectAccountId = home-account субъекта);
// resource-режим (account-скоуп) → субъект НЕ залочен, preset-область account:<id>.
function privilegesChildCreate(
  spec:
    | { kind: "subject"; subjectType: SubjectType }
    | { kind: "resource"; resourceType: "account" | "project" | "cluster" },
): (childRoute: string, ctx: DetailExtCtx) => ReactNode {
  return (childRoute, { data, detailBase, navigate }) => {
    if (childRoute !== "privileges") return null;
    const id = getByPath<string>(data, "id") ?? "";
    const back = `${detailBase}/privileges`;
    if (spec.kind === "subject") {
      const subjectAccountId = getByPath<string>(data, "account_id") ?? null;
      return (
        <AccessBindingCreateForm
          lockedSubject={{ type: spec.subjectType, id }}
          subjectAccountId={subjectAccountId}
          onSuccess={() => navigate(back)}
          onCancel={() => navigate(back)}
        />
      );
    }
    return (
      <AccessBindingCreateForm
        preset={{ resource_type: spec.resourceType, resource_id: id }}
        onSuccess={() => navigate(back)}
        onCancel={() => navigate(back)}
      />
    );
  };
}

// ─────────────────────────── Role (RBAC rules-model) ───────────────────────────

// RoleRule — правило RBAC-модели (rules[]): module + resources/verbs, опц.
// resource_names (ARM_NAMES) / match_labels (ARM_LABELS).
interface RoleRule {
  module?: string;
  resources?: string[];
  verbs?: string[];
  resource_names?: string[];
  match_labels?: Record<string, string>;
}

// arm выводится из формы правила (наличие resource_names / match_labels).
function roleRuleArm(rule: RoleRule): "ARM_NAMES" | "ARM_LABELS" | "ARM_ANCHOR" {
  if ((rule.resource_names ?? []).length > 0) return "ARM_NAMES";
  if (Object.keys(rule.match_labels ?? {}).length > 0) return "ARM_LABELS";
  return "ARM_ANCHOR";
}

// roleRulesView — рендер rules[] роли: карточка на правило с arm-бейджем +
// module/resources/verbs (+ resourceNames/matchLabels для соответствующего arm).
function roleRulesView(rules: RoleRule[] | undefined): ReactNode {
  if (!rules || rules.length === 0) return dash;
  const chips = (xs: string[]) =>
    xs.map((x) => (
      <Tag key={x} style={{ margin: 0, fontFamily: "ui-monospace, monospace", fontSize: 12 }}>
        {x}
      </Tag>
    ));
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 8, alignItems: "stretch" }}>
      {rules.map((rule, i) => {
        const arm = roleRuleArm(rule);
        const armLabel =
          arm === "ARM_NAMES"
            ? "По именам (resourceNames)"
            : arm === "ARM_LABELS"
              ? "По меткам (matchLabels)"
              : "Все инстансы в scope";
        const armColor = arm === "ARM_NAMES" ? "geekblue" : arm === "ARM_LABELS" ? "purple" : "default";
        return (
          <div
            key={i}
            style={{
              border: "1px solid var(--kc-border)",
              borderRadius: 6,
              padding: "8px 10px",
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            <Tag color={armColor} style={{ alignSelf: "flex-start" }}>
              {armLabel}
            </Tag>
            <span style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center" }}>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                module:
              </Typography.Text>
              <Tag style={{ margin: 0, fontFamily: "ui-monospace, monospace", fontSize: 12 }}>{rule.module || "—"}</Tag>
            </span>
            <span style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center" }}>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                resources:
              </Typography.Text>
              {chips(rule.resources ?? [])}
            </span>
            <span style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center" }}>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                verbs:
              </Typography.Text>
              {chips(rule.verbs ?? [])}
            </span>
            {arm === "ARM_NAMES" && (
              <span style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center" }}>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  resourceNames:
                </Typography.Text>
                {chips(rule.resource_names ?? [])}
              </span>
            )}
            {arm === "ARM_LABELS" && (
              <span style={{ display: "flex", flexWrap: "wrap", gap: 4, alignItems: "center" }}>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  matchLabels:
                </Typography.Text>
                {Object.entries(rule.match_labels ?? {}).map(([k, v]) => (
                  <Tag key={k} color="purple" style={{ margin: 0, fontFamily: "ui-monospace, monospace", fontSize: 12 }}>
                    {k}={v}
                  </Tag>
                ))}
              </span>
            )}
          </div>
        );
      })}
    </span>
  );
}

// ─────────────────────────── регистрация ───────────────────────────

// Role: Тип (system/custom) + Правила (rules[]) + Область в overview.
registerDetailExtension("roles", {
  overviewExtra: ({ data }) => {
    const rows: DescItem[] = [
      {
        label: "Тип",
        value:
          getByPath<boolean>(data, "is_system") === true || getByPath<boolean>(data, "isSystem") === true ? (
            <Tag color="purple">system</Tag>
          ) : (
            <Tag>custom</Tag>
          ),
      },
      { label: "Правила", value: roleRulesView(getByPath<RoleRule[]>(data, "rules")) },
    ];
    const acc = getByPath<string>(data, "account_id");
    const cluster = getByPath<string>(data, "cluster_id");
    const project = getByPath<string>(data, "project_id");
    if (acc) rows.push({ label: "Область (Account)", value: <IamRefLink specId="accounts" refId={acc} /> });
    if (cluster) rows.push({ label: "Область (кластер)", value: mono(cluster) });
    if (project) rows.push({ label: "Область (проект)", value: <IamRefLink specId="projects" refId={project} /> });
    return rows;
  },
});

// Account — не субъект AccessBinding'а, а ресурс-скоуп: таб показывает привязки,
// выданные НА этот account (listByResource account).
registerDetailExtension("accounts", {
  overviewExtra: ({ data }) => [
    {
      label: "Владелец",
      value: <IamRefLink specId="users" refId={getByPath<string>(data, "owner_user_id")} nameField="email" />,
    },
  ],
  extraTabs: ({ data, detailBase }) => {
    const id = getByPath<string>(data, "id") ?? "";
    return id ? [privilegesTab({ kind: "resource", resourceType: "account", resourceId: id }, detailBase)] : [];
  },
  childCreate: privilegesChildCreate({ kind: "resource", resourceType: "account" }),
});

// ServiceAccount — субъект типа service_account. Вкладки: «Привилегии» + «Токены».
registerDetailExtension("service-accounts", {
  overviewExtra: ({ data }) => [
    { label: "Аккаунт", value: <IamRefLink specId="accounts" refId={getByPath<string>(data, "account_id")} /> },
  ],
  extraTabs: ({ data, detailBase }) => {
    const id = getByPath<string>(data, "id") ?? "";
    if (!id) return [];
    return [
      privilegesTab({ kind: "subject", subjectType: "service_account", subjectId: id }, detailBase),
      tokensTab(id),
    ];
  },
  childCreate: privilegesChildCreate({ kind: "subject", subjectType: "service_account" }),
});

// User — субъект типа user. Обзор: статус приглашения, external id, аккаунт,
// пригласивший (output-only). Вкладки: «Привилегии» + «Токены».
registerDetailExtension("users", {
  overviewExtra: ({ data }) => [
    { label: "Статус приглашения", value: <StatusBadge state={getByPath<string>(data, "invite_status")} /> },
    {
      label: "External ID",
      value: getByPath<string>(data, "external_id") ? (
        <CopyableId id={getByPath<string>(data, "external_id")!} />
      ) : (
        <span className="text-muted-foreground">—</span>
      ),
    },
    { label: "Аккаунт", value: <IamRefLink specId="accounts" refId={getByPath<string>(data, "account_id")} /> },
    {
      label: "Пригласил",
      value: <IamRefLink specId="users" refId={getByPath<string>(data, "invited_by")} nameField="email" />,
    },
  ],
  extraTabs: ({ data, detailBase }) => {
    const id = getByPath<string>(data, "id") ?? "";
    if (!id) return [];
    return [privilegesTab({ kind: "subject", subjectType: "user", subjectId: id }, detailBase), userTokensTab(id)];
  },
  childCreate: privilegesChildCreate({ kind: "subject", subjectType: "user" }),
});

// Group — субъект типа group. Обзор: «Аккаунт»; «Участники» — секция под Обзором
// (GroupMembersPanel); «Привилегии» — отдельный таб.
registerDetailExtension("groups", {
  overviewExtra: ({ data }) => [
    { label: "Аккаунт", value: <IamRefLink specId="accounts" refId={getByPath<string>(data, "account_id")} /> },
  ],
  overviewBelow: ({ data }) => (
    <GroupMembersPanel group={data as unknown as Group} accountId={getByPath<string>(data, "account_id") ?? null} />
  ),
  extraTabs: ({ data, detailBase }) => {
    const id = getByPath<string>(data, "id") ?? "";
    return id ? [privilegesTab({ kind: "subject", subjectType: "group", subjectId: id }, detailBase)] : [];
  },
  childCreate: privilegesChildCreate({ kind: "subject", subjectType: "group" }),
});

// AccessBinding — сводка биндинга в Обзоре: субъект/роль/ресурс (IamRefLink) +
// статус/область/условие/защита.
registerDetailExtension("access-bindings", {
  overviewExtra: ({ data }) => {
    const subjType = String(getByPath<string>(data, "subject_type") ?? "");
    const subjSpec =
      subjType === "user"
        ? "users"
        : subjType === "group"
          ? "groups"
          : subjType === "service_account"
            ? "service-accounts"
            : undefined;
    const subjId = getByPath<string>(data, "subject_id") ?? "";
    const resType = String(getByPath<string>(data, "resource_type") ?? "");
    const resSpec = resType === "account" ? "accounts" : resType === "project" ? "projects" : undefined;
    const resId = getByPath<string>(data, "resource_id") ?? "";
    const scope = String(getByPath<string>(data, "scope") ?? "");
    const cond = getByPath<string>(data, "builtin_condition");
    return [
      {
        // Тип субъекта несёт иконка IamRefLink — тип-тег не дублируем.
        label: "Субъект",
        value: subjSpec ? (
          <IamRefLink specId={subjSpec} refId={subjId} nameField={subjType === "user" ? "email" : "name"} />
        ) : (
          <CopyableId id={subjId} />
        ),
      },
      { label: "Роль", value: <IamRefLink specId="roles" refId={getByPath<string>(data, "role_id")} /> },
      {
        // Тип ресурса несёт иконка IamRefLink — тип-тег не дублируем.
        label: "Ресурс",
        value: resSpec ? <IamRefLink specId={resSpec} refId={resId} /> : <CopyableId id={resId} />,
      },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
      {
        label: "Область",
        value:
          scope && scope !== "SCOPE_UNSPECIFIED" ? (
            <Tag color={scopeColor(scope)}>{scope}</Tag>
          ) : (
            <span className="text-muted-foreground">—</span>
          ),
      },
      {
        label: "Встроенное условие",
        value: <span style={{ fontFamily: "ui-monospace, SFMono-Regular, monospace", fontSize: 12 }}>{cond || "—"}</span>,
      },
      {
        label: "Защита от удаления",
        value: getByPath<boolean>(data, "deletion_protection") ? (
          <Tag color="gold">Да</Tag>
        ) : (
          <span className="text-muted-foreground">Нет</span>
        ),
      },
    ];
  },
});

// Role inline-формы (RBAC rules-model): rules[] через RulesEditor + backend
// permissionCatalog. Create — account-scoped custom-роль; Edit — грузит роль по id.
registerInlineForm("roles", "create", ({ accountId, onCancel, onSuccess }) => (
  <InlineRoleCreateForm accountId={accountId} onCancel={onCancel} onSuccess={onSuccess} />
));
registerInlineForm("roles", "edit", ({ id, onCancel, onSuccess }) =>
  id ? <InlineRoleEditForm roleId={id} onCancel={onCancel} onSuccess={onSuccess} /> : null,
);
