// UsersPage — список User-mirror'ов из kacho-iam + invite-flow.
//
// KAC-127: добавлен «Пригласить пользователя» — POST /iam/v1/users:invite
// (iamApi.inviteUser). account_id берётся из выбранного в IAM-секции Account.
// На успех показываем magic_link_url (если backend его вернул).
//
// Прямого Create (signup) по-прежнему нет — пользователь активируется по
// magic-link либо через OIDC-callback.

import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Button, Cascader, Form, Input, Popconfirm, Select, Space, Table, Tag, Typography, Alert, Tooltip } from "antd";
import { DeleteOutlined, UserAddOutlined, LinkOutlined, StopOutlined, UnlockOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { iamApi, IAM, type User, type InviteStatus } from "@shared/api/iam";
import { useIamMutation, fmtTs, CopyableMonoId, groupedRoleOptions } from "@shared/components/organisms/iam/IamCommon";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { IamListShell, useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { useContext } from "@shared/lib/context-store";
import { toast } from "@shared/lib/toast";
import { useAuth } from "@shared/contexts/AuthContext";

function InviteStatusTag({ status }: { status?: InviteStatus }) {
  if (!status) return <Typography.Text type="secondary">—</Typography.Text>;
  const color = status === "ACTIVE" ? "green" : status === "PENDING" ? "gold" : "red";
  return <Tag color={color}>{status}</Tag>;
}

/**
 * BlockToggle — построчное действие «запретить участие» / «вернуть участие».
 *
 * Действие ВЫБИРАЕТСЯ по текущему состоянию, а не показывается парой кнопок:
 * состояние здесь — предмет, и предлагать «запретить» уже запрещённому значит
 * предлагать вызов, который ничего не меняет.
 *
 * PENDING недоступен НАМЕРЕННО и с названной причиной. Backend такой вызов
 * отвергает (у неподтверждённого приглашения нет внешней личности, и переводить
 * его в действующее — это активация при первом входе, другой путь), поэтому
 * живая кнопка здесь обещала бы возможность, которой нет. Приглашение отзывают,
 * а не разблокируют.
 *
 * САМОБЛОКИРОВКА не запрещается, но предупреждение говорит ПРЯМО, чем она
 * кончится: снять запрет с себя нельзя — самостоятельного пути снятия не
 * существует по построению (восстановление пароля блокировку не снимает), и
 * вернуть доступ придётся администратору аккаунта или администратору облака.
 * Промолчать об этом значило бы дать оператору выключить себя одним нажатием и
 * узнать цену потом.
 */
function BlockToggle({
  row,
  selfUserId,
  onBlock,
  onUnblock,
}: {
  row: User;
  selfUserId?: string;
  onBlock: (id: string) => Promise<unknown>;
  onUnblock: (id: string) => Promise<unknown>;
}) {
  const status = row.invite_status;
  const who = row.email || row.id;

  if (status === "PENDING") {
    return (
      <Tooltip title="Приглашение ещё не подтверждено — запрещать нечего. Отзовите приглашение.">
        <Button size="small" type="text" disabled icon={<StopOutlined />} />
      </Tooltip>
    );
  }

  if (status === "BLOCKED") {
    return (
      <Popconfirm
        title="Вернуть участие?"
        description={`Разрешить «${who}» снова входить в этот аккаунт.`}
        okText="Вернуть"
        cancelText="Отмена"
        onConfirm={() => void onUnblock(row.id)}
      >
        <Tooltip title="Вернуть участие">
          <Button size="small" type="text" icon={<UnlockOutlined />} />
        </Tooltip>
      </Popconfirm>
    );
  }

  const isSelf = !!selfUserId && selfUserId === row.id;
  return (
    <Popconfirm
      title={isSelf ? "Запретить участие СЕБЕ?" : "Запретить участие?"}
      description={
        isSelf
          ? `Вы запрещаете участие себе («${who}»). Снять запрет самостоятельно будет НЕЛЬЗЯ: ` +
            `восстановление пароля блокировку не снимает. Вернуть доступ сможет только ` +
            `администратор аккаунта или администратор облака.`
          : `«${who}» больше не сможет входить в этот аккаунт. Уже выданный токен доживёт свой срок; ` +
            `новый не выдадут. Участие в других аккаунтах не затрагивается.`
      }
      okText={isSelf ? "Да, запретить себе" : "Запретить"}
      okButtonProps={{ danger: true }}
      cancelText="Отмена"
      onConfirm={() => void onBlock(row.id)}
    >
      <Tooltip title="Запретить участие">
        <Button size="small" type="text" danger icon={<StopOutlined />} />
      </Tooltip>
    </Popconfirm>
  );
}

export function UsersPage() {
  const account = useContext((s) => s.account);
  const navigate = useNavigate();
  // Своя строка членства — чтобы предупредить про самоблокировку до нажатия, а не
  // после. `user_id` в /me и есть каноническая действующая строка вызывающего.
  const { whoami } = useAuth();
  const selfUserId = whoami?.user_id;
  const headerAction = useMemo(
    () => (
      <Tooltip title={account ? undefined : "Выберите Account вверху секции, чтобы пригласить пользователя"}>
        <Button
          type="primary"
          icon={<UserAddOutlined />}
          disabled={!account}
          onClick={() => navigate("/iam/users/invite")}
        >
          Пригласить пользователя
        </Button>
      </Tooltip>
    ),
    [account, navigate],
  );
  useHeaderRight(headerAction);

  const { data, isLoading } = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "200" }),
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const users = data?.users ?? [];
  const { wrapRef, scrollY } = useTableScrollY();

  const del = useIamMutation({
    method: "DELETE",
    path: (b) => `${IAM.users}/${b as string}`,
    invalidateKeys: [["iam", "users", "list"]],
    successText: "User удалён",
  });

  // Запрет участию и его снятие — ДЕЙСТВИЯ (`:block` / `:unblock`), а не правка
  // поля: у действия нет маски, поэтому «забыть поле» и выключить всех, кого
  // коснулся, здесь невозможно. Права решает backend (`v_update` на этом
  // пользователе); консоль ничего не предугадывает — при отказе прилетит 403,
  // и его покажет общий механизм ошибок операции.
  const blockMut = useIamMutation({
    method: "ACTION",
    path: (b) => `${IAM.users}/${b as string}:block`,
    invalidateKeys: [["iam", "users", "list"]],
    successText: "Участие запрещено",
  });
  const unblockMut = useIamMutation({
    method: "ACTION",
    path: (b) => `${IAM.users}/${b as string}:unblock`,
    invalidateKeys: [["iam", "users", "list"]],
    successText: "Участие возвращено",
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
      width: 96,
      render: (_v, row) => (
        <Space size={0}>
          <BlockToggle row={row} selfUserId={selfUserId} onBlock={blockMut.run} onUnblock={unblockMut.run} />
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
        </Space>
      ),
    },
  ];

  return (
    <IamListShell specId="users" title="Пользователи" count={users.length}>
      {users.length === 0 && !isLoading && (
        <Alert
          type="info"
          showIcon
          style={{ flexShrink: 0 }}
          message="User'ов нет"
          description={
            <span>
              Пригласите пользователя по email (кнопка выше) — он получит magic-link для активации. Также User создаётся
              автоматически из OIDC-callback Zitadel.
            </span>
          }
        />
      )}

      <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <Table<User>
          rowKey="id"
          size="small"
          className="kc-table"
          loading={isLoading}
          dataSource={users}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          onRow={(row) => ({
            onClick: (e) => {
              if ((e.target as HTMLElement)?.closest("button, a, .ant-dropdown, .ant-popover, .ant-select")) return;
              navigate(`/iam/users/${row.id}`);
            },
            style: { cursor: "pointer" },
          })}
          locale={{ emptyText: "User'ов нет." }}
        />
      </div>
    </IamListShell>
  );
}

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
