// InviteUserPage — приглашение пользователя в аккаунт: POST /iam/v1/users:invite.
//
// Прямого создания пользователя нет и не предполагается: он появляется либо по
// magic-link из приглашения, либо при первом входе через поставщика личности.
// Аккаунт берётся из выбранного в разделе IAM.

import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Button, Cascader, Form, Input, Select, Space, Typography, Alert } from "antd";
import { LinkOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { iamApi } from "@shared/api/iam";
import { groupedRoleOptions } from "@shared/components/organisms/iam/IamCommon";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { useContext } from "@shared/lib/context-store";
import { toast } from "@shared/lib/toast";

export function InviteUserPage() {
  const account = useContext((s) => s.account);
  const accountId = account?.id ?? "";
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [magicLink, setMagicLink] = useState<string | null>(null);
  const noHeaderRight = useMemo(() => null, []);
  useHeaderRight(noHeaderRight);

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">IAM</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Link to="/iam/users">
          <Typography.Text type="secondary">Users</Typography.Text>
        </Link>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Пригласить</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  // Каскадер «Аккаунт → проект»: eager-грузим все аккаунты и их проекты, чтобы
  // работал поиск по всему дереву (lazy-load ломает showSearch по неоткрытым
  // ветвям). Значение [accId, prjId?]; changeOnSelect → можно выбрать только
  // аккаунт (проект необязателен).
  const cascaderQuery = useQuery({
    queryKey: ["iam", "invite-cascader", "accounts-projects"],
    queryFn: async () => {
      const accs = (await iamApi.listAccounts({ pageSize: "1000" })).accounts ?? [];
      return Promise.all(
        accs.map(async (a) => {
          const prs = (await iamApi.listProjects({ account_id: a.id, pageSize: "1000" })).projects ?? [];
          return {
            value: a.id,
            label: a.name || a.id,
            children: prs.map((p) => ({ value: p.id, label: p.name || p.id })),
          };
        }),
      );
    },
    staleTime: 30_000,
  });
  // Значение каскадера: по умолчанию — аккаунт из контекста (level 1).
  const [scope, setScope] = useState<string[]>(accountId ? [accountId] : []);
  const scopeAccountId = scope[0] ?? accountId;
  const scopeProjectId = scope[1];

  const roles = useQuery({
    queryKey: ["iam", "roles", "list"],
    queryFn: () => iamApi.listRoles({ pageSize: "1000" }),
    enabled: !!accountId,
    staleTime: 30_000,
  });

  const close = () => {
    form.resetFields();
    setMagicLink(null);
    navigate("/iam/users");
  };

  const onFinish = async (v: { email: string; display_name?: string; role_id?: string }) => {
    if (!scopeAccountId) {
      toast.error("Выберите аккаунт");
      return;
    }
    setSubmitting(true);
    try {
      const resp = await iamApi.inviteUser({
        account_id: scopeAccountId,
        email: v.email,
        ...(v.display_name ? { display_name: v.display_name } : {}),
        ...(scopeProjectId ? { project_id: scopeProjectId } : {}),
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
        navigate("/iam/users");
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Ошибка приглашения");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormShell specId="users" mode="create" singular="Пользователь" title="Приглашение пользователя">
      {!accountId ? (
        <Alert
          type="info"
          showIcon
          message="Выберите Account"
          description="Чтобы пригласить пользователя, сначала выберите Account в шапке."
        />
      ) : null}
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
            icon={<LinkOutlined />}
            onClick={() => {
              void navigator.clipboard.writeText(magicLink);
              toast.success("Ссылка скопирована");
            }}
          >
            Скопировать ссылку
          </Button>
          <FormFooter submitLabel="Готово" submitting={false} onSubmit={close} onCancel={close} />
        </Space>
      ) : accountId ? (
        <Form
          form={form}
          layout="horizontal"
          labelCol={{ flex: "200px" }}
          wrapperCol={{ flex: "auto" }}
          labelAlign="left"
          colon={false}
          onFinish={onFinish}
        >
          <Form.Item label="Аккаунт / проект" required>
            <Cascader
              options={cascaderQuery.data ?? []}
              value={scope}
              onChange={(val) => setScope((val as string[]) ?? [])}
              loading={cascaderQuery.isLoading}
              changeOnSelect
              allowClear={false}
              expandTrigger="hover"
              showSearch={{
                filter: (input, path) =>
                  path.some((o) => String(o.label).toLowerCase().includes(input.toLowerCase())),
              }}
              placeholder="Сначала аккаунт, затем проект (необязательно)"
              displayRender={(labels) => labels.join(" / ")}
              style={{ width: "100%" }}
            />
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
          <FormFooter
            submitLabel="Пригласить"
            submitting={submitting}
            onSubmit={() => form.submit()}
            onCancel={close}
          />
        </Form>
      ) : (
        <FormFooter
          submitLabel="Пригласить"
          submitting={false}
          submitDisabled
          onSubmit={() => undefined}
          onCancel={close}
        />
      )}
    </FormShell>
  );
}
