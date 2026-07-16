// RolesPage — список Role.
// - System roles (is_system=true) read-only;
// - custom roles (account_id !== "") — editable: name/description/rules.
//
// Роль описывается RBAC rules-model (rules[]: module/resources/verbs +
// resource_names XOR match_labels). Формы создания/редактирования — RulesEditor
// поверх backend permissionCatalog (InlineRoleCreateForm / InlineRoleEditForm);
// редактирование открывается в зоне 3 (ResourceShell mode=edit).

import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Button, Input, Popconfirm, Segmented, Space, Table, Tag, Typography } from "antd";
import { PlusOutlined, DeleteOutlined, EditOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { iamApi, IAM, type Role } from "@shared/api/iam";
import { useIamMutation, fmtTs, CopyableMonoId, SystemTag } from "@shared/components/organisms/iam/IamCommon";
import { InlineRoleCreateForm } from "@/components/organisms/iam/InlineRoleCreateForm";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { IamListShell, useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { useContext } from "@shared/lib/context-store";

export function RolesPage() {
  const navigate = useNavigate();
  // Фильтр списка ролей: сфера (Все/Системные/Кастомные) + поиск по имени/id —
  // Segmented + Input в шапке списка (паритет с generic-списками).
  const [roleKind, setRoleKind] = useState<"all" | "system" | "custom">("all");
  const [query, setQuery] = useState("");
  const headerAction = useMemo(
    () => (
      <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate("/iam/roles/create")}>
        Создать пользовательскую роль
      </Button>
    ),
    [navigate],
  );
  useHeaderRight(headerAction);

  const { data, isLoading } = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "1000" }),
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const roles = useMemo(() => data?.roles ?? [], [data?.roles]);
  const systemRoles = useMemo(() => roles.filter((r) => r.is_system), [roles]);
  const customRoles = useMemo(() => roles.filter((r) => !r.is_system), [roles]);
  const byKind = roleKind === "system" ? systemRoles : roleKind === "custom" ? customRoles : roles;
  const q = query.trim().toLowerCase();
  const visibleRoles = q
    ? byKind.filter((r) => (r.name ?? "").toLowerCase().includes(q) || (r.id ?? "").toLowerCase().includes(q))
    : byKind;
  const { wrapRef, scrollY } = useTableScrollY();

  const del = useIamMutation({
    method: "DELETE",
    path: (b) => `${IAM.roles}/${b as string}`,
    invalidateKeys: [["iam", "roles", "list"]],
    successText: "Role удалена",
  });

  const columns: ColumnsType<Role> = [
    {
      title: "Имя",
      dataIndex: "name",
      key: "name",
      render: (v) => <Typography.Text strong>{v}</Typography.Text>,
    },
    {
      title: "Тип",
      key: "system",
      width: 110,
      render: (_v, row) => <SystemTag isSystem={row.is_system} />,
    },
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      width: 220,
      render: (v) => <CopyableMonoId id={v} />,
    },
    {
      title: "Аккаунт",
      dataIndex: "account_id",
      key: "account",
      width: 200,
      render: (v) => (v ? <CopyableMonoId id={v} /> : <Typography.Text type="secondary">—</Typography.Text>),
    },
    {
      title: "Описание",
      dataIndex: "description",
      key: "description",
      render: (v) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: "Разрешения",
      dataIndex: "permissions",
      key: "perms",
      render: (v: string[] | undefined) => (
        <Space size={4} wrap>
          {(v ?? []).slice(0, 4).map((p) => (
            <Tag key={p} style={{ fontFamily: "monospace", fontSize: 11 }}>
              {p}
            </Tag>
          ))}
          {(v?.length ?? 0) > 4 && <Typography.Text type="secondary">+{(v?.length ?? 0) - 4} more</Typography.Text>}
        </Space>
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
      width: 110,
      render: (_v, row) => (
        <Space size={4}>
          <Button
            size="small"
            type="text"
            icon={<EditOutlined />}
            disabled={row.is_system}
            title={row.is_system ? "system roles read-only" : "Изменить"}
            onClick={() => navigate(`/iam/roles/${row.id}/edit`)}
          />
          <Popconfirm
            title="Удалить Role?"
            description={`Удалить «${row.name}»? Custom role с активными AccessBinding → FailedPrecondition.`}
            okText="Удалить"
            okButtonProps={{ danger: true }}
            cancelText="Отмена"
            onConfirm={() => void del.run(row.id)}
            disabled={row.is_system}
          >
            <Button
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined />}
              disabled={row.is_system}
              title={row.is_system ? "system roles read-only" : "Удалить"}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <IamListShell
      specId="roles"
      title="Роли"
      count={roles.length}
      right={
        <Space size={8}>
          <Input.Search
            placeholder="Фильтр по имени или идентификатору"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            allowClear
            style={{ width: 280 }}
          />
          <Segmented
            value={roleKind}
            onChange={(v) => setRoleKind(v as "all" | "system" | "custom")}
            options={[
              { label: `Все (${roles.length})`, value: "all" },
              { label: `Системные (${systemRoles.length})`, value: "system" },
              { label: `Кастомные (${customRoles.length})`, value: "custom" },
            ]}
          />
        </Space>
      }
    >
      <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <Table<Role>
          rowKey="id"
          size="small"
          className="kc-table"
          loading={isLoading}
          dataSource={visibleRoles}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          onRow={(row) => ({
            onClick: (e) => {
              if ((e.target as HTMLElement)?.closest("button, a, .ant-dropdown, .ant-popover, .ant-select")) return;
              navigate(`/iam/roles/${row.id}`);
            },
            style: { cursor: "pointer" },
          })}
          locale={{
            emptyText: q ? "Ничего не найдено." : "Ролей нет.",
          }}
        />
      </div>
    </IamListShell>
  );
}

// RoleCreatePage — standalone-страница создания пользовательской роли
// (маршрут /iam/roles/create). Тело формы — RBAC rules-model через
// InlineRoleCreateForm (RulesEditor + backend permissionCatalog). Account
// выбирается в самой форме (account_id Select), контекст лишь пресетит его.
// Редактирование роли открывается в зоне 3 detail-страницы (ResourceShell
// mode=edit → InlineRoleEditForm), отдельной страницы у него нет.
export function RoleCreatePage() {
  const navigate = useNavigate();
  const account = useContext((s) => s.account);
  useHeaderRight(useMemo(() => null, []));
  useBreadcrumb(
    useMemo(
      () => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Typography.Text type="secondary">IAM</Typography.Text>
          <Typography.Text type="secondary">/</Typography.Text>
          <Link to="/iam/roles">
            <Typography.Text type="secondary">Roles</Typography.Text>
          </Link>
          <Typography.Text type="secondary">/</Typography.Text>
          <Typography.Text strong>Создать</Typography.Text>
        </span>
      ),
      [],
    ),
  );

  return (
    <InlineRoleCreateForm
      accountId={account?.id}
      onCancel={() => navigate("/iam/roles")}
      onSuccess={() => navigate("/iam/roles")}
    />
  );
}
