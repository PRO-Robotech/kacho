// UsersPage — список User-mirror'ов из kacho-iam + invite-flow.
//
// KAC-127: добавлен «Пригласить пользователя» — POST /iam/v1/users:invite
// (iamApi.inviteUser). account_id берётся из выбранного в IAM-секции Account.
// На успех показываем magic_link_url (если backend его вернул).
//
// Прямого Create (signup) по-прежнему нет — пользователь активируется по
// magic-link либо через OIDC-callback.

import { useState } from "react";
import { Button, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography, Alert, Tooltip } from "antd";
import { DeleteOutlined, UserAddOutlined, LinkOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { iamApi, IAM, type User, type InviteStatus } from "@shared/api/iam";
import { useIamMutation, fmtTs, CopyableMonoId, groupedRoleOptions } from "@shared/components/organisms/iam/IamCommon";
import { useContext } from "@shared/lib/context-store";
import { toast } from "@shared/lib/toast";

function InviteStatusTag({ status }: { status?: InviteStatus }) {
  if (!status) return <Typography.Text type="secondary">—</Typography.Text>;
  const color = status === "ACTIVE" ? "green" : status === "PENDING" ? "gold" : "red";
  return <Tag color={color}>{status}</Tag>;
}

export function UsersPage() {
  const account = useContext((s) => s.account);
  const [inviteOpen, setInviteOpen] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "200" }),
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const users = data?.users ?? [];

  const del = useIamMutation({
    method: "DELETE",
    path: (b) => `${IAM.users}/${b as string}`,
    invalidateKeys: [["iam", "users", "list"]],
    successText: "User удалён",
  });

  const columns: ColumnsType<User> = [
    {
      title: "Эл. почта",
      dataIndex: "email",
      key: "email",
      render: (v) => (v ? <Typography.Text strong>{v}</Typography.Text> : "—"),
    },
    {
      title: "Отображаемое имя",
      dataIndex: "display_name",
      key: "display_name",
      render: (v) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: "Статус",
      dataIndex: "invite_status",
      key: "invite_status",
      width: 110,
      render: (v) => <InviteStatusTag status={v as InviteStatus | undefined} />,
    },
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      render: (v) => <CopyableMonoId id={v} />,
    },
    {
      title: "External ID (Zitadel sub)",
      dataIndex: "external_id",
      key: "external_id",
      render: (v) => <CopyableMonoId id={v} />,
    },
    {
      title: "Создан",
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
          title="Удалить User?"
          description={`Удалить «${row.email || row.id}»? Owned Account/AccessBinding — см. backend rules.`}
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

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
        Users
      </Typography.Title>

      <Space size={8} wrap>
        <Tooltip title={account ? undefined : "Выберите Account вверху секции, чтобы пригласить пользователя"}>
          <Button
            type="primary"
            size="small"
            icon={<UserAddOutlined />}
            disabled={!account}
            onClick={() => setInviteOpen(true)}
          >
            Пригласить пользователя
          </Button>
        </Tooltip>
      </Space>

      {users.length === 0 && !isLoading && (
        <Alert
          type="info"
          showIcon
          message="User'ов нет"
          description={
            <span>
              Пригласите пользователя по email (кнопка выше) — он получит magic-link для активации. Также User создаётся
              автоматически из OIDC-callback Zitadel.
            </span>
          }
        />
      )}

      <Table<User>
        rowKey="id"
        size="small"
        loading={isLoading}
        dataSource={users}
        columns={columns}
        pagination={false}
        locale={{ emptyText: "User'ов нет." }}
      />

      <InviteUserModal
        open={inviteOpen}
        accountId={account?.id ?? ""}
        accountName={account?.name ?? ""}
        onClose={() => setInviteOpen(false)}
      />
    </Space>
  );
}

function InviteUserModal({
  open,
  accountId,
  accountName,
  onClose,
}: {
  open: boolean;
  accountId: string;
  accountName: string;
  onClose: () => void;
}) {
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [magicLink, setMagicLink] = useState<string | null>(null);

  const projects = useQuery({
    queryKey: ["iam", "projects", "by-account", accountId],
    queryFn: () => iamApi.listProjects({ account_id: accountId, pageSize: "1000" }),
    enabled: open && !!accountId,
    staleTime: 30_000,
  });
  const roles = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "1000" }),
    enabled: open,
    staleTime: 30_000,
  });

  const close = () => {
    form.resetFields();
    setMagicLink(null);
    onClose();
  };

  const onFinish = async (v: { email: string; display_name?: string; project_id?: string; role_id?: string }) => {
    setSubmitting(true);
    try {
      const resp = await iamApi.inviteUser({
        account_id: accountId,
        email: v.email,
        ...(v.display_name ? { display_name: v.display_name } : {}),
        ...(v.project_id ? { project_id: v.project_id } : {}),
        ...(v.role_id ? { role_id: v.role_id } : {}),
      });
      if (resp.error) {
        toast.error(resp.error.message || "Не удалось пригласить пользователя");
        return;
      }
      const link = resp.metadata?.magic_link_url;
      toast.success("Приглашение отправлено");
      if (link) {
        setMagicLink(link);
      } else {
        close();
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Ошибка приглашения");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="Пригласить пользователя"
      open={open}
      onCancel={close}
      maskClosable
      width={860}
      destroyOnClose
      footer={
        magicLink
          ? [
              <Button key="close" type="primary" onClick={close}>
                Готово
              </Button>,
            ]
          : [
              <Button key="cancel" onClick={close}>
                Отмена
              </Button>,
              <Button key="ok" type="primary" loading={submitting} onClick={() => form.submit()}>
                Пригласить
              </Button>,
            ]
      }
    >
      {magicLink ? (
        <Space direction="vertical" size={12} style={{ width: "100%" }}>
          <Alert
            type="success"
            showIcon
            message="Пользователь приглашён"
            description="Передайте пользователю magic-link для активации аккаунта."
          />
          <Input addonBefore={<LinkOutlined />} value={magicLink} readOnly onFocus={(e) => e.currentTarget.select()} />
          <Button
            size="small"
            icon={<LinkOutlined />}
            onClick={() => {
              void navigator.clipboard.writeText(magicLink);
              toast.success("Ссылка скопирована");
            }}
          >
            Скопировать ссылку
          </Button>
        </Space>
      ) : (
        <Form
          form={form}
          layout="horizontal"
          labelCol={{ flex: "200px" }}
          wrapperCol={{ flex: "auto" }}
          labelAlign="left"
          colon={false}
          onFinish={onFinish}
        >
          <Form.Item label="Account">
            <Typography.Text>
              {accountName || accountId}{" "}
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                · {accountId}
              </Typography.Text>
            </Typography.Text>
          </Form.Item>
          <Form.Item
            label="Email"
            name="email"
            required
            rules={[
              { required: true, message: "Укажите email" },
              { type: "email", message: "Некорректный email" },
            ]}
          >
            <Input placeholder="user@example.com" />
          </Form.Item>
          <Form.Item label="Отображаемое имя" name="display_name">
            <Input placeholder="Иван Петров" />
          </Form.Item>
          <Form.Item label="Проект" name="project_id">
            <Select
              allowClear
              placeholder="Без проекта"
              loading={projects.isLoading}
              showSearch
              optionFilterProp="label"
              options={(projects.data?.projects ?? []).map((p) => ({
                value: p.id,
                label: `${p.name || p.id} · ${p.id}`,
              }))}
            />
          </Form.Item>
          <Form.Item label="Роль" name="role_id">
            <Select
              allowClear
              placeholder="Без роли"
              loading={roles.isLoading}
              showSearch
              optionFilterProp="label"
              options={groupedRoleOptions(roles.data?.roles ?? [])}
            />
          </Form.Item>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0, marginLeft: 200 }}>
            Проект и роль необязательны — можно назначить позже через Access Bindings.
          </Typography.Paragraph>
        </Form>
      )}
    </Modal>
  );
}
