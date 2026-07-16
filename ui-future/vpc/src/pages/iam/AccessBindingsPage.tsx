// AccessBindingsPage — управление AccessBinding'ами.
// View modes:
//   - "byResource" → list per (resource_type + resource_id);
//   - "bySubject" → list per (subject_type + subject_id).
//   - "byAccount" → KAC item #1: admin-wide list по AccountID (новый RPC
//      AccessBindingService.ListByAccount, доступен только admin'у account'а).
//
// Create — отдельная модалка: subject_type/id + role + resource_type/id +
// поддержка resource_type=cluster (KAC item #5) для unified cluster-admin grant.
// На 409 ALREADY_EXISTS → inline Alert с verbatim message (KAC item #3).

import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { PlusOutlined, DeleteOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { api, ApiError } from "@shared/api/client";
import {
  iamApi,
  IAM,
  type AccessBinding,
  type AccessBindingList,
  type User,
  type ServiceAccount,
  type Group,
  type Account,
} from "@shared/api/iam";
import { useIamMutation, fmtTs, CopyableMonoId, groupedRoleOptions } from "@shared/components/organisms/iam/IamCommon";
import { useAuth } from "@shared/contexts/AuthContext";
import { usePermissions, isAlreadyExistsError, mapApiErrorToMessage } from "@shared/lib/permissions";

type ViewMode = "byResource" | "bySubject" | "byAccount";
type SubjectType = "user" | "service_account" | "group";
// KAC-224 (RBAC v2): только высокоуровневые скоупы, принимаемые backend
// validResourceTypes. Legacy resource-manager типы folder/organization/cloud
// удалены (KAC-124 / KAC-223 mig0008) — их выбор давал backend
// INVALID_ARGUMENT "Illegal argument resource_type". "cluster" — для unified
// cluster-admin grant (item #5, resource_id = "cluster_kacho_root").
type ResourceType = "account" | "project" | "cluster";

const SUBJECT_TYPES: SubjectType[] = ["user", "service_account", "group"];
export const RESOURCE_TYPES: ResourceType[] = ["account", "project", "cluster"];

/** Cluster singleton id для resource_type="cluster" (KAC item #5). */
export const CLUSTER_RESOURCE_ID = "cluster_kacho_root";

/** Cluster admin role id (для preset'ов / quick-grant link'ов). */
export const CLUSTER_ADMIN_ROLE_ID = "roles/admin";

export function AccessBindingsPage() {
  const { user } = useAuth();
  const perms = usePermissions();
  // KAC item #5: ClusterAdminsPage "Выдать через AccessBinding" CTA редиректит
  // сюда с query-параметрами `?modal=cluster-admin&resource_type=cluster&...`.
  // Авто-открываем модалку с preset'ом.
  const [searchParams] = useSearchParams();
  const presetFromUrl: AccessBindingPreset | undefined = useMemo(() => {
    if (searchParams.get("modal") !== "cluster-admin") return undefined;
    return {
      resource_type: (searchParams.get("resource_type") as ResourceType | null) ?? "cluster",
      resource_id: searchParams.get("resource_id") ?? CLUSTER_RESOURCE_ID,
      role_id: searchParams.get("role_id") ?? CLUSTER_ADMIN_ROLE_ID,
      subject_type: (searchParams.get("subject_type") as SubjectType | null) ?? "user",
      subject_id: searchParams.get("subject_id") ?? undefined,
    };
  }, [searchParams]);
  // KAC item #1: для админа дефолтная вкладка — "byAccount" (admin видит ВСЕ
  // bindings в account'е). Non-admin → "byResource" (старое поведение).
  // Дефолтное значение оставляем "byResource" на первом render'е (perms ещё не
  // загружены), а как только perms подгрузились — авто-переключаем на byAccount
  // (admin). User может потом сменить вручную — это поведение покрыто
  // `userTouchedModeRef`.
  const [mode, setMode] = useState<ViewMode>("byResource");
  const userTouchedModeRef = useRef(false);
  const [createOpen, setCreateOpen] = useState(!!presetFromUrl);
  // KAC item #1: account-id для byAccount-режима. По умолчанию первый
  // account из членства user'а.
  const [accountIdForList, setAccountIdForList] = useState<string>("");
  const [subjectTypeFilter, setSubjectTypeFilter] = useState<string>("");
  const [includeRevoked, setIncludeRevoked] = useState(false);

  // Авто-переключение на byAccount при первой загрузке perms для admin'а.
  useEffect(() => {
    if (!perms.loaded || userTouchedModeRef.current) return;
    if (perms.isSystemAdmin) setMode("byAccount");
    if (!accountIdForList && perms.accounts[0]?.account_id) {
      setAccountIdForList(perms.accounts[0].account_id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [perms.loaded, perms.isSystemAdmin, perms.accounts.length]);

  const handleSetMode = (v: ViewMode) => {
    userTouchedModeRef.current = true;
    setMode(v);
  };

  // Если URL-preset изменился (новая навигация) — открываем модалку повторно.
  useEffect(() => {
    if (presetFromUrl) setCreateOpen(true);
  }, [presetFromUrl]);

  // KAC-123: Мои AccessBinding'и — авто-вызов /iam/v1/accessBindings:listBySubject
  // для текущего user'а, показываем сверху страницы.
  const myBindings = useQuery({
    queryKey: ["iam", "access-bindings", "by-subject", "user", user?.id ?? ""],
    queryFn: () => iamApi.listAccessBindingsBySubject("user", user!.id, { pageSize: "200" }),
    enabled: !!user?.id,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  // byResource state
  const [resType, setResType] = useState<ResourceType>("account");
  const [resId, setResId] = useState<string>("");
  // bySubject state
  const [subjType, setSubjType] = useState<SubjectType>("user");
  const [subjId, setSubjId] = useState<string>("");

  const byResource = useQuery({
    queryKey: ["iam", "access-bindings", "by-resource", resType, resId],
    queryFn: () => iamApi.listAccessBindingsByResource(resType, resId, { pageSize: "200" }),
    enabled: mode === "byResource" && !!resId,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const bySubject = useQuery({
    queryKey: ["iam", "access-bindings", "by-subject", subjType, subjId],
    queryFn: () => iamApi.listAccessBindingsBySubject(subjType, subjId, { pageSize: "200" }),
    enabled: mode === "bySubject" && !!subjId,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  // KAC item #1: ListByAccount — admin видит ВСЕ bindings в account'е.
  const byAccount = useQuery({
    queryKey: ["iam", "access-bindings", "by-account", accountIdForList, subjectTypeFilter, includeRevoked],
    queryFn: () =>
      iamApi.listAccessBindingsByAccount(accountIdForList, {
        page_size: 200,
        subject_type_filter: subjectTypeFilter || undefined,
        include_revoked: includeRevoked,
      }),
    enabled: mode === "byAccount" && !!accountIdForList,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const data = mode === "byResource" ? byResource : mode === "bySubject" ? bySubject : byAccount;
  const bindings = (data?.data as AccessBindingList | undefined)?.access_bindings ?? [];

  const del = useIamMutation({
    method: "DELETE",
    path: (b) => `${IAM.accessBindings}/${b as string}`,
    invalidateKeys: [["iam", "access-bindings"]],
    successText: "AccessBinding удалён",
  });

  // Helpers for selectors
  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    staleTime: 30_000,
  });

  const users = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "1000" }),
    // KAC item #1: для byAccount-режима тоже нужен users lookup (Subject column
    // должен показать email вместо просто id).
    enabled: subjType === "user" || createOpen || mode === "byAccount",
    staleTime: 30_000,
  });

  // KAC-127: resolve role_id → name в таблице bindings.
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
  const userByIdLookup = useMemo(() => {
    const m = new Map<string, User>();
    for (const u of users.data?.users ?? []) m.set(u.id, u);
    return m;
  }, [users.data]);

  const columns: ColumnsType<AccessBinding> = [
    {
      title: "Субъект",
      key: "subject",
      render: (_v, row) => {
        const u = row.subject_type === "user" ? userByIdLookup.get(row.subject_id) : undefined;
        const human = u?.email || u?.display_name;
        return (
          <Space size={6} wrap>
            <Tag color={subjectColor(row.subject_type)}>{row.subject_type}</Tag>
            {human && (
              <Typography.Text strong style={{ fontSize: 12 }}>
                {human}
              </Typography.Text>
            )}
            <CopyableMonoId id={row.subject_id} />
          </Space>
        );
      },
    },
    {
      title: "Роль",
      dataIndex: "role_id",
      key: "role",
      render: (v: string) => {
        const name = roleNameById.get(v);
        return (
          <Space size={6}>
            {name && <Typography.Text strong>{name}</Typography.Text>}
            <CopyableMonoId id={v} />
          </Space>
        );
      },
    },
    {
      title: "Ресурс",
      key: "resource",
      render: (_v, row) => (
        <Space size={6}>
          <Tag>{row.resource_type}</Tag>
          <CopyableMonoId id={row.resource_id} />
        </Space>
      ),
    },
    {
      // RBAC v2 (KAC-224): output-only scope tier из ответа AccessBinding.
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
      render: (v) => fmtTs(v),
    },
    {
      title: "",
      key: "actions",
      width: 60,
      render: (_v, row) => (
        <Popconfirm
          title="Удалить AccessBinding?"
          okText="Удалить"
          okButtonProps={{ danger: true }}
          cancelText="Отмена"
          onConfirm={() => void del.run(row.id)}
        >
          <Button size="small" type="text" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      ),
    },
  ];

  const myBindingsRows = myBindings.data?.access_bindings ?? [];

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
        Access Bindings
      </Typography.Title>

      {user?.id && (
        <Card
          size="small"
          title={
            <Space>
              <span>Мои AccessBinding&apos;и</span>
              <Tag color="blue">{myBindingsRows.length}</Tag>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                субъект: <code>user:{user.id}</code>
              </Typography.Text>
            </Space>
          }
        >
          {myBindingsRows.length === 0 ? (
            <Typography.Text type="secondary">У вас нет привязанных ролей.</Typography.Text>
          ) : (
            <Table<AccessBinding>
              rowKey="id"
              size="small"
              loading={myBindings.isLoading}
              dataSource={myBindingsRows}
              columns={columns.filter((c) => c.key !== "subject")}
              pagination={false}
            />
          )}
        </Card>
      )}

      <Space size={12} wrap>
        <Segmented
          value={mode}
          onChange={(v) => handleSetMode(v as ViewMode)}
          options={[
            // KAC item #1: "По account'у" (admin tab) — первый, если user — admin.
            ...(perms.isSystemAdmin || perms.accounts.length > 0
              ? [{ label: "По account'у (admin)", value: "byAccount" }]
              : []),
            { label: "По ресурсу", value: "byResource" },
            { label: "По subject'у", value: "bySubject" },
          ]}
          data-testid="access-bindings-mode"
        />
        <Button
          type="primary"
          size="small"
          icon={<PlusOutlined />}
          onClick={() => setCreateOpen(true)}
          data-testid="access-bindings-create-btn"
        >
          Создать binding
        </Button>
      </Space>

      {mode === "byResource" && (
        <Space size={8} wrap>
          <Select
            value={resType}
            onChange={(v) => setResType(v)}
            options={RESOURCE_TYPES.map((t) => ({ value: t, label: t }))}
            style={{ width: 180 }}
          />
          <ResourceIdInput
            resourceType={resType}
            value={resId}
            onChange={setResId}
            accountList={accounts.data?.accounts ?? []}
          />
        </Space>
      )}
      {mode === "bySubject" && (
        <Space size={8} wrap>
          <Select
            value={subjType}
            onChange={(v) => setSubjType(v)}
            options={SUBJECT_TYPES.map((t) => ({ value: t, label: t }))}
            style={{ width: 180 }}
          />
          {subjType === "user" ? (
            <Select
              style={{ width: 420 }}
              value={subjId || undefined}
              onChange={(v) => setSubjId(v ?? "")}
              placeholder="Выберите User"
              showSearch
              optionFilterProp="label"
              options={(users.data?.users ?? []).map((u: User) => ({
                value: u.id,
                label: `${u.email || u.display_name || u.id} · ${u.id}`,
              }))}
            />
          ) : (
            <Input
              placeholder={`${subjType} id (sva-... / grp-...)`}
              value={subjId}
              onChange={(e) => setSubjId(e.target.value.trim())}
              style={{ width: 420, fontFamily: "monospace" }}
            />
          )}
        </Space>
      )}
      {mode === "byAccount" && (
        <Space size={8} wrap>
          <Select
            placeholder="Account"
            value={accountIdForList || undefined}
            onChange={(v) => setAccountIdForList(v ?? "")}
            options={(accounts.data?.accounts ?? []).map((a) => ({
              value: a.id,
              label: `${a.name} · ${a.id}`,
            }))}
            style={{ width: 360 }}
            showSearch
            optionFilterProp="label"
            data-testid="access-bindings-account-select"
          />
          <Select
            placeholder="subject_type (все)"
            value={subjectTypeFilter || undefined}
            onChange={(v) => setSubjectTypeFilter(v ?? "")}
            allowClear
            options={SUBJECT_TYPES.map((t) => ({ value: t, label: t }))}
            style={{ width: 200 }}
          />
          <Select
            value={includeRevoked ? "true" : "false"}
            onChange={(v) => setIncludeRevoked(v === "true")}
            options={[
              { value: "false", label: "Только активные" },
              { value: "true", label: "Включая отозванные" },
            ]}
            style={{ width: 200 }}
          />
        </Space>
      )}

      {emptyHint(mode, resId, subjId, accountIdForList) ? (
        <Typography.Text type="secondary">{emptyHint(mode, resId, subjId, accountIdForList)}</Typography.Text>
      ) : (
        <Table<AccessBinding>
          rowKey="id"
          size="small"
          loading={data?.isLoading}
          dataSource={bindings}
          columns={columns}
          pagination={false}
          locale={{ emptyText: "AccessBinding'ов нет." }}
          data-testid="access-bindings-table"
        />
      )}

      <AccessBindingCreateModal open={createOpen} onClose={() => setCreateOpen(false)} preset={presetFromUrl} />
    </Space>
  );
}

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

/** RBAC v2 (KAC-224): цвет тега scope-tier'а. */
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

/** Подсказка для пустого селектора — что нужно выбрать чтобы увидеть данные. */
function emptyHint(mode: ViewMode, resId: string, subjId: string, accountId: string): string | null {
  if (mode === "byResource" && !resId) return "Введите resource_id для просмотра bindings.";
  if (mode === "bySubject" && !subjId) return "Выберите subject для просмотра bindings.";
  if (mode === "byAccount" && !accountId) return "Выберите account для просмотра bindings.";
  return null;
}

function ResourceIdInput({
  resourceType,
  value,
  onChange,
  accountList,
}: {
  resourceType: string;
  value: string;
  onChange: (v: string) => void;
  accountList: Account[];
}) {
  // Если ресурс = account — даём drop-down из доступных Account.
  if (resourceType === "account") {
    return (
      <Select
        style={{ width: 420 }}
        value={value || undefined}
        onChange={(v) => onChange(v ?? "")}
        placeholder="Выберите Account"
        showSearch
        optionFilterProp="label"
        options={accountList.map((a) => ({
          value: a.id,
          label: `${a.name} · ${a.id}`,
        }))}
      />
    );
  }
  return (
    <Input
      style={{ width: 420, fontFamily: "monospace" }}
      placeholder={`${resourceType} id`}
      value={value}
      onChange={(e) => onChange(e.target.value.trim())}
    />
  );
}

/**
 * Preset для модалки — pre-fill полей (KAC item #5 "Grant Cluster Admin" CTA).
 * Если передать `resource_type: "cluster"` + `resource_id: "cluster_kacho_root"` +
 * `role_id: "roles/admin"` — модалка откроется с pre-fixated cluster-admin grant.
 */
export interface AccessBindingPreset {
  subject_type?: SubjectType;
  subject_id?: string;
  role_id?: string;
  resource_type?: ResourceType;
  resource_id?: string;
}

export function AccessBindingCreateModal({
  open,
  onClose,
  preset,
}: {
  open: boolean;
  onClose: () => void;
  preset?: AccessBindingPreset;
}) {
  const [form] = Form.useForm();
  const [subjectType, setSubjectType] = useState<SubjectType>(preset?.subject_type ?? "user");
  const [resourceType, setResourceType] = useState<ResourceType>(preset?.resource_type ?? "account");
  // KAC item #3: при 409 ALREADY_EXISTS / любой другой ошибке — inline
  // Alert внутри модалки, форма НЕ закрывается (см. CLAUDE.md §3.5).
  const [inlineError, setInlineError] = useState<{
    type: "warning" | "error";
    message: string;
  } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const qc = useQueryClient();

  // Сбрасываем форму при открытии (если был preset — применить).
  useEffect(() => {
    if (!open) return;
    setInlineError(null);
    setSubjectType(preset?.subject_type ?? "user");
    setResourceType(preset?.resource_type ?? "account");
    form.setFieldsValue({
      subject_type: preset?.subject_type ?? "user",
      subject_id: preset?.subject_id ?? undefined,
      role_id: preset?.role_id ?? undefined,
      resource_type: preset?.resource_type ?? "account",
      resource_id: preset?.resource_id ?? undefined,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, preset?.subject_type, preset?.subject_id, preset?.role_id, preset?.resource_type, preset?.resource_id]);

  const users = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "1000" }),
    enabled: open,
    staleTime: 30_000,
  });
  const roles = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "1000" }),
    enabled: open,
    staleTime: 30_000,
  });
  const sas = useQuery({
    queryKey: ["iam", "service-accounts", "all"],
    queryFn: async () => {
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: ServiceAccount[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listServiceAccounts({
          account_id: a.id,
          pageSize: "1000",
        });
        all.push(...(r.service_accounts ?? []));
      }
      return all;
    },
    enabled: open && subjectType === "service_account",
    staleTime: 30_000,
  });
  const groups = useQuery({
    queryKey: ["iam", "groups", "all"],
    queryFn: async () => {
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: Group[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listGroups({
          account_id: a.id,
          pageSize: "1000",
        });
        all.push(...(r.groups ?? []));
      }
      return all;
    },
    enabled: open && subjectType === "group",
    staleTime: 30_000,
  });

  const subjectOptions = useMemo(() => {
    switch (subjectType) {
      case "user":
        return (users.data?.users ?? []).map((u: User) => ({
          value: u.id,
          label: `${u.email || u.display_name || u.id} · ${u.id}`,
        }));
      case "service_account":
        return (sas.data ?? []).map((sa) => ({
          value: sa.id,
          label: `${sa.name} · ${sa.id}`,
        }));
      case "group":
        return (groups.data ?? []).map((g) => ({
          value: g.id,
          label: `${g.name} · ${g.id}`,
        }));
    }
  }, [subjectType, users.data, sas.data, groups.data]);

  // Submit: вызов POST /iam/v1/accessBindings напрямую через `api.create`,
  // чтобы поймать 409 ALREADY_EXISTS (verbatim сообщение
  // "these permissions are already granted to <subject_id> on
  // <resource_type>:<resource_id>") — KAC item #3.
  const onFinish = async (v: Record<string, string>) => {
    setSubmitting(true);
    setInlineError(null);
    const body = {
      subject_type: v.subject_type,
      subject_id: v.subject_id,
      role_id: v.role_id,
      resource_type: v.resource_type,
      // KAC item #5: для cluster — auto-fill cluster_kacho_root, если user не
      // ввёл (Input может быть disabled = preset на cluster).
      resource_id: v.resource_type === "cluster" ? v.resource_id || CLUSTER_RESOURCE_ID : v.resource_id,
    };
    try {
      await api.create(IAM.accessBindings, body);
      // Sync success или Operation envelope — invalidate всё и закрываем.
      void qc.invalidateQueries({ queryKey: ["iam", "access-bindings"] });
      void qc.invalidateQueries({ queryKey: ["cluster-admins"] });
      form.resetFields();
      setSubmitting(false);
      onClose();
    } catch (e) {
      setSubmitting(false);
      if (isAlreadyExistsError(e)) {
        // KAC item #3: верность verbatim текста ("these permissions are
        // already granted to <subject_id> on <resource_type>:<resource_id>").
        setInlineError({
          type: "warning",
          message: (e instanceof ApiError && e.message) || "These permissions are already granted",
        });
        return;
      }
      setInlineError({
        type: "error",
        message: mapApiErrorToMessage(e),
      });
    }
  };

  return (
    <Modal
      title="Создать AccessBinding"
      open={open}
      onCancel={onClose}
      maskClosable
      width={860}
      destroyOnHidden
      onOk={() => form.submit()}
      okText="Создать"
      cancelText="Отмена"
      confirmLoading={submitting}
    >
      {inlineError && (
        <Alert
          type={inlineError.type}
          showIcon
          style={{ marginBottom: 12 }}
          message={inlineError.message}
          closable
          onClose={() => setInlineError(null)}
          data-testid="access-bindings-create-error"
        />
      )}
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
        initialValues={{
          subject_type: preset?.subject_type ?? "user",
          resource_type: preset?.resource_type ?? "account",
          resource_id:
            preset?.resource_type === "cluster" ? (preset?.resource_id ?? CLUSTER_RESOURCE_ID) : preset?.resource_id,
          subject_id: preset?.subject_id,
          role_id: preset?.role_id,
        }}
        onFinish={onFinish}
      >
        <Form.Item label="Subject type" name="subject_type" required>
          <Select
            options={SUBJECT_TYPES.map((t) => ({ value: t, label: t }))}
            onChange={(v) => {
              setSubjectType(v as SubjectType);
              form.setFieldValue("subject_id", undefined);
            }}
          />
        </Form.Item>
        <Form.Item label="Subject" name="subject_id" required rules={[{ required: true, message: "Выберите subject" }]}>
          <Select
            placeholder={`Выберите ${subjectType}`}
            options={subjectOptions}
            showSearch
            optionFilterProp="label"
            loading={users.isLoading || sas.isLoading || groups.isLoading}
          />
        </Form.Item>
        <Form.Item label="Role" name="role_id" required rules={[{ required: true, message: "Выберите role" }]}>
          <Select
            placeholder="Выберите Role"
            options={groupedRoleOptions(roles.data?.roles ?? [])}
            showSearch
            optionFilterProp="label"
            loading={roles.isLoading}
          />
        </Form.Item>
        <Form.Item label="Resource type" name="resource_type" required>
          <Select
            options={RESOURCE_TYPES.map((t) => ({ value: t, label: t }))}
            onChange={(v) => {
              const rt = v as ResourceType;
              setResourceType(rt);
              // KAC item #5: cluster → auto-fill singleton id.
              if (rt === "cluster") {
                form.setFieldValue("resource_id", CLUSTER_RESOURCE_ID);
              } else if (form.getFieldValue("resource_id") === CLUSTER_RESOURCE_ID) {
                form.setFieldValue("resource_id", undefined);
              }
            }}
          />
        </Form.Item>
        <Form.Item
          label="Resource"
          name="resource_id"
          required
          rules={[{ required: true, message: "Введите resource_id" }]}
        >
          <Input
            style={{ fontFamily: "monospace" }}
            placeholder={resourceType === "cluster" ? CLUSTER_RESOURCE_ID : "acc-... / prj-... / любой идентификатор"}
            disabled={resourceType === "cluster"}
          />
        </Form.Item>
        <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0, marginLeft: 200 }}>
          {resourceType === "cluster" ? (
            <>
              Cluster admin grant: subject получит cluster-wide роль на singleton <code>{CLUSTER_RESOURCE_ID}</code>.
              Эквивалент legacy
              <code> POST /iam/v1/internal/cluster/admins</code> (KAC item #5).
            </>
          ) : (
            <>
              Подсказка: для resource_type=account → используйте id из вкладки Accounts; для project → из вкладки
              Projects.
            </>
          )}
        </Typography.Paragraph>
      </Form>
    </Modal>
  );
}
