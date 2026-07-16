// AccessPage — «Права доступа» (KAC-125).
//
// Layout по скриншотам:
// - Header: «Права доступа» + табы «Облако» (Account-scope) / «Каталог» (Project-scope).
// - CTA «Настроить доступ» → route-backed grant page with Cascader roles and invite fallback.
// - Filter: имя/идентификатор, тип аккаунта, наследуемые роли.
// - Table: пользователь / роли / идентификатор / федерация / actions.

import { useState, useMemo } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Button, Cascader, Form, Input, Segmented, Select, Space, Table, Tabs, Tag, Typography, Alert } from "antd";
import { toast } from "@shared/lib/toast";
import { PlusOutlined, MailOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { iamApi, IAM, type User, type Role } from "@shared/api/iam";
import { CopyableMonoId } from "@shared/components/organisms/iam/IamCommon";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { IamListShell, useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { useContext } from "@shared/lib/context-store";

type ScopeTab = "cloud" | "folder";

export function AccessPage() {
  const account = useContext((s) => s.account);
  const project = useContext((s) => s.project);
  const navigate = useNavigate();
  const [scope, setScope] = useState<ScopeTab>("cloud");

  const accountId = account?.id ?? "";
  const projectId = project?.id ?? "";
  const resourceType = scope === "cloud" ? "account" : "project";
  const resourceId = scope === "cloud" ? accountId : projectId;
  const { wrapRef, scrollY } = useTableScrollY();
  const headerAction = useMemo(
    () => (
      <Button
        type="primary"
        icon={<PlusOutlined />}
        onClick={() => navigate(`/iam/access/grant?scope=${scope}`)}
        disabled={!resourceId}
      >
        Настроить доступ
      </Button>
    ),
    [navigate, resourceId, scope],
  );
  useHeaderRight(headerAction);

  const bindings = useQuery({
    queryKey: ["iam", "access-bindings", "by-resource", resourceType, resourceId],
    queryFn: () =>
      iamApi.listAccessBindingsByResource(resourceType, resourceId, {
        pageSize: "200",
      }),
    enabled: !!resourceId,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const users = useQuery({
    queryKey: ["iam", "users", "list", accountId],
    queryFn: () => iamApi.listUsers({ pageSize: "1000", account_id: accountId }),
    enabled: !!accountId,
    staleTime: 30_000,
  });

  const userById = useMemo(() => {
    const m = new Map<string, User>();
    for (const u of users.data?.users ?? []) m.set(u.id, u);
    return m;
  }, [users.data]);

  const roles = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "500" }),
    staleTime: 60_000,
  });

  const roleById = useMemo(() => {
    const m = new Map<string, Role>();
    for (const r of roles.data?.roles ?? []) m.set(r.id, r);
    return m;
  }, [roles.data]);

  type Row = {
    userId: string;
    user: User | undefined;
    roleNames: string[];
    bindingIds: string[];
  };
  const rows: Row[] = useMemo(() => {
    const byUser = new Map<string, Row>();
    for (const b of bindings.data?.access_bindings ?? []) {
      if (b.subject_type !== "user") continue;
      const r = byUser.get(b.subject_id) ?? {
        userId: b.subject_id,
        user: userById.get(b.subject_id),
        roleNames: [],
        bindingIds: [],
      };
      const role = roleById.get(b.role_id);
      r.roleNames.push(role?.name || b.role_id);
      r.bindingIds.push(b.id);
      byUser.set(b.subject_id, r);
    }
    return Array.from(byUser.values());
  }, [bindings.data, userById, roleById]);

  const columns: ColumnsType<Row> = [
    {
      title: "Пользователь",
      key: "user",
      render: (_v, row) => {
        const u = row.user;
        return (
          <Space size={6} direction="vertical" size-2>
            <Typography.Text strong>{u?.display_name || u?.email || row.userId}</Typography.Text>
            {u?.email ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {u.email}
              </Typography.Text>
            ) : null}
            {u?.invite_status === "PENDING" ? <Tag color="orange">приглашён</Tag> : null}
          </Space>
        );
      },
    },
    {
      title: "Роли",
      key: "roles",
      render: (_v, row) => (
        <Space size={4} wrap>
          {row.roleNames.map((n, i) => (
            <Tag key={i} color="blue">
              {n}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: "Идентификатор",
      key: "id",
      render: (_v, row) => <CopyableMonoId id={row.userId} />,
    },
    {
      title: "Федерация",
      key: "fed",
      width: 100,
      render: () => <Typography.Text type="secondary">—</Typography.Text>,
    },
  ];

  return (
    <IamListShell specId="access-bindings" title="Права доступа" count={rows.length}>
      <Space size={12} wrap style={{ marginBottom: 12, flexShrink: 0 }}>
        <Segmented
          value={scope}
          onChange={(v) => setScope(v as ScopeTab)}
          options={[
            { label: "Облако", value: "cloud" },
            { label: "Каталог", value: "folder", disabled: !projectId },
          ]}
        />
      </Space>

      {!resourceId ? (
        <Alert
          type="info"
          style={{ flexShrink: 0 }}
          message={
            scope === "cloud"
              ? "Выберите Account в шапке для просмотра прав доступа."
              : "Выберите Project в шапке для просмотра прав доступа."
          }
        />
      ) : (
        <>
          <Alert
            type="info"
            style={{ marginBottom: 12, flexShrink: 0 }}
            message={
              scope === "cloud"
                ? "В этом разделе вы можете настроить права доступа к Account."
                : "В этом разделе вы можете настроить права доступа к Project."
            }
            closable
          />
          <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
            <Table<Row>
              rowKey="userId"
              size="small"
              className="kc-table"
              loading={bindings.isLoading || users.isLoading || roles.isLoading}
              dataSource={rows}
              columns={columns}
              pagination={false}
              scroll={{ x: "max-content", y: scrollY }}
              locale={{ emptyText: "Пользователей с правами нет." }}
            />
          </div>
        </>
      )}
    </IamListShell>
  );
}

export function AccessGrantPage() {
  const account = useContext((s) => s.account);
  const project = useContext((s) => s.project);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const scope = searchParams.get("scope") === "folder" ? "folder" : "cloud";
  const accountId = account?.id ?? "";
  const projectId = project?.id ?? "";
  const [form] = Form.useForm();
  const qc = useQueryClient();
  const [subjectInput, setSubjectInput] = useState("");
  const [magicLink, setMagicLink] = useState<string | null>(null);
  useHeaderRight(useMemo(() => null, []));
  useBreadcrumb(
    useMemo(
      () => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Typography.Text type="secondary">IAM</Typography.Text>
          <Typography.Text type="secondary">/</Typography.Text>
          <Link to="/iam/access">
            <Typography.Text type="secondary">Права доступа</Typography.Text>
          </Link>
          <Typography.Text type="secondary">/</Typography.Text>
          <Typography.Text strong>Настроить</Typography.Text>
        </span>
      ),
      [],
    ),
  );

  const users = useQuery({
    queryKey: ["iam", "users", "for-invite", accountId],
    queryFn: () => iamApi.listUsers({ pageSize: "1000", account_id: accountId }),
    enabled: !!accountId,
    staleTime: 30_000,
  });

  const roles = useQuery({
    queryKey: ["iam", "roles", "for-invite"],
    queryFn: () => iamApi.listRoles({ pageSize: "500" }),
    staleTime: 60_000,
  });

  // Cascader-options: 3 уровня (module → resource → verb).
  // KAC-122: verbs "admin/edit/view" (НЕ editor/viewer); names без roles/ prefix.
  const cascaderOptions = useMemo(() => buildCascaderOptions(roles.data?.roles ?? []), [roles.data]);

  // Системные / Свои роли — два таба внутри Cascader-блока.
  const [roleTab, setRoleTab] = useState<"system" | "custom">("system");

  // Match email → existing user.
  const matchedUser = useMemo(() => {
    const q = subjectInput.trim().toLowerCase();
    if (!q || !users.data) return null;
    return (
      users.data.users.find(
        (u) => u.email?.toLowerCase() === q || u.id === subjectInput.trim() || u.display_name?.toLowerCase() === q,
      ) ?? null
    );
  }, [subjectInput, users.data]);

  // Если ввод — валидный email и в Account его нет → invite fallback.
  const inviteFallback = useMemo(() => {
    const q = subjectInput.trim();
    if (!q || matchedUser) return false;
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(q);
  }, [subjectInput, matchedUser]);

  async function handleSubmit() {
    try {
      const values = await form.validateFields();
      const selectedPaths: string[][] = values.role_paths ?? [];
      const roleIds = resolveRoleIds(selectedPaths, roles.data?.roles ?? []);
      if (roleIds.length === 0) {
        toast.error("Не выбрана ни одна роль");
        return;
      }

      const targetResourceType = scope === "cloud" ? "account" : "project";
      const targetResourceId = scope === "cloud" ? accountId : projectId;

      if (matchedUser) {
        // Existing user — bulk Create AccessBinding (по одной на каждую выбранную роль).
        for (const roleId of roleIds) {
          await fetch(IAM.accessBindings, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "include",
            body: JSON.stringify({
              subject_type: "user",
              subject_id: matchedUser.id,
              role_id: roleId,
              resource_type: targetResourceType,
              resource_id: targetResourceId,
            }),
          });
        }
        toast.success(`Доступ выдан пользователю ${matchedUser.email || matchedUser.id}`);
        qc.invalidateQueries({ queryKey: ["iam", "access-bindings"] });
        form.resetFields();
        setSubjectInput("");
        navigate("/iam/access");
        return;
      }

      if (inviteFallback) {
        // Email отсутствует в Account → invite.
        const resp = await iamApi.inviteUser({
          account_id: accountId,
          email: subjectInput.trim(),
          project_id: targetResourceType === "project" ? targetResourceId : undefined,
          role_id: roleIds[0], // одну роль кладём в invite payload; остальные — отдельные AB
        });
        const link = resp?.metadata?.magic_link_url;
        if (link) setMagicLink(link);
        toast.success("Пользователь приглашён");
        qc.invalidateQueries({ queryKey: ["iam", "access-bindings"] });
        qc.invalidateQueries({ queryKey: ["iam", "users"] });
        // НЕ закрываем модалку — показываем magic-link для копирования.
        return;
      }

      toast.error("Выберите пользователя или укажите email для приглашения");
    } catch {
      // The ApiError envelope (which may carry backend detail / a magic-link
      // payload) must not be dumped to the browser console — surface only the
      // user-facing toast.
      toast.error("Ошибка выдачи доступа");
    }
  }

  const subjectOptions = (users.data?.users ?? []).map((u) => ({
    value: u.id,
    label: `${u.email || u.display_name || u.id}${u.invite_status === "PENDING" ? " (приглашён)" : ""}`,
  }));

  return (
    <FormShell specId="access-bindings" mode="create" singular="Доступ" title="Выдача доступа">
      <Form form={form} layout="vertical">
        <Form.Item label="Ресурс">
          <Tag color={scope === "cloud" ? "blue" : "geekblue"}>
            {scope === "cloud" ? "Account (Облако)" : "Project (Каталог)"}
          </Tag>
          <Typography.Text type="secondary" style={{ marginLeft: 8 }}>
            {scope === "cloud" ? accountId : projectId}
          </Typography.Text>
        </Form.Item>

        <Form.Item
          label="Кому выдать доступ"
          name="subject_id"
          required
          tooltip="Имя, идентификатор или email. Если email не найден — будет создано приглашение."
        >
          <Select
            placeholder="Имя, идентификатор или email"
            options={subjectOptions}
            value={subjectInput || undefined}
            onChange={(v) => setSubjectInput((v as string) || "")}
            onSearch={(v) => setSubjectInput(v)}
            filterOption={(input, option) =>
              ((option?.label as string) || "").toLowerCase().includes(input.toLowerCase())
            }
            showSearch
            allowClear
            loading={users.isLoading}
          />
        </Form.Item>

        {inviteFallback ? (
          <Alert
            type="info"
            showIcon
            icon={<MailOutlined />}
            message={`Пользователь с адресом ${subjectInput} не найден в вашей организации.`}
            description="Вы можете отправить ему приглашение для присоединения к организации. Magic-link появится после Сохранить (admin копирует и отправляет вручную)."
          />
        ) : null}

        {magicLink ? (
          <Alert
            type="success"
            showIcon
            message="Приглашение создано!"
            description={
              <Space direction="vertical" style={{ width: "100%" }} size={4}>
                <Typography.Text>Скопируйте magic-link и отправьте пользователю:</Typography.Text>
                <Input.TextArea value={magicLink} rows={3} readOnly />
              </Space>
            }
          />
        ) : null}

        <Form.Item label="Роли" name="role_paths" required>
          <Tabs
            activeKey={roleTab}
            onChange={(k) => setRoleTab(k as "system" | "custom")}
            size="small"
            items={[
              {
                key: "system",
                label: `Системные (${cascaderOptions.system.length})`,
                children: (
                  <Cascader
                    options={cascaderOptions.system}
                    multiple
                    showCheckedStrategy="SHOW_CHILD"
                    placeholder="Выберите роли (модуль / ресурс / verb)"
                    style={{ width: "100%" }}
                    onChange={(v) => form.setFieldValue("role_paths", v as string[][])}
                  />
                ),
              },
              {
                key: "custom",
                label: `Свои роли (${cascaderOptions.custom.length})`,
                children:
                  cascaderOptions.custom.length === 0 ? (
                    <Typography.Text type="secondary">У вашей организации пока нет своих ролей.</Typography.Text>
                  ) : (
                    <Cascader
                      options={cascaderOptions.custom}
                      multiple
                      showCheckedStrategy="SHOW_CHILD"
                      placeholder="Выберите свои роли"
                      style={{ width: "100%" }}
                      onChange={(v) => form.setFieldValue("role_paths", v as string[][])}
                    />
                  ),
              },
            ]}
          />
        </Form.Item>
        <FormFooter
          submitLabel={magicLink ? "Готово" : "Сохранить"}
          submitting={false}
          onSubmit={
            magicLink
              ? () => {
                  setMagicLink(null);
                  navigate("/iam/access");
                }
              : handleSubmit
          }
          onCancel={() => {
            setMagicLink(null);
            setSubjectInput("");
            form.resetFields();
            navigate("/iam/access");
          }}
        />
      </Form>
    </FormShell>
  );
}

// ───────── Cascader helpers ─────────

interface CascaderOption {
  value: string;
  label: string;
  children?: CascaderOption[];
}

function buildCascaderOptions(roles: Role[]): {
  system: CascaderOption[];
  custom: CascaderOption[];
} {
  const buildTree = (filtered: Role[]): CascaderOption[] => {
    const tree: Record<string, Record<string, Set<string>>> = {};
    for (const r of filtered) {
      const parts = r.name.split(".");
      if (parts.length === 1) {
        // global wildcard (`admin`/`edit`/`view`) → особый pseudo-path [*, *, verb]
        tree["*"] = tree["*"] ?? {};
        tree["*"]["*"] = tree["*"]["*"] ?? new Set();
        tree["*"]["*"].add(parts[0]);
      } else if (parts.length === 3) {
        const [m, res, verb] = parts;
        tree[m] = tree[m] ?? {};
        tree[m][res] = tree[m][res] ?? new Set();
        tree[m][res].add(verb);
      }
    }
    const result: CascaderOption[] = [];
    for (const [mod, resources] of Object.entries(tree)) {
      const modOpt: CascaderOption = {
        value: mod,
        label: mod === "*" ? "Все модули" : mod,
        children: [],
      };
      for (const [res, verbs] of Object.entries(resources)) {
        modOpt.children!.push({
          value: res,
          label: res === "*" ? "Все ресурсы" : res,
          children: Array.from(verbs)
            .sort()
            .map((v) => ({ value: v, label: v })),
        });
      }
      result.push(modOpt);
    }
    return result;
  };

  return {
    system: buildTree(roles.filter((r) => r.is_system)),
    custom: buildTree(roles.filter((r) => !r.is_system)),
  };
}

function resolveRoleIds(paths: string[][], roles: Role[]): string[] {
  const out: string[] = [];
  for (const path of paths) {
    let name: string;
    if (path[0] === "*" && path[1] === "*") {
      name = path[2];
    } else {
      name = path.join(".");
    }
    const role = roles.find((r) => r.name === name);
    if (role) out.push(role.id);
  }
  return out;
}
