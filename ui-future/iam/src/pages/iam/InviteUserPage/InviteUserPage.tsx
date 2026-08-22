// InviteUserPage — приглашение пользователя в аккаунт: POST /iam/v1/users:invite.
//
// Прямого создания пользователя нет и не предполагается: он появляется либо по
// magic-link из приглашения, либо при первом входе через поставщика личности.
// Аккаунт берётся из выбранного в разделе IAM.

import { useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Alert, Button, Cascader, Form, Input, Select, Space, Typography } from "antd";
import { LinkOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { iamApi } from "@shared/api/iam";
import { groupedRoleOptions } from "@shared/components/organisms/iam/IamCommon";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ScopeRequiredEmpty } from "@/components/molecules/ScopeRequiredEmpty";
import { useContext } from "@shared/lib/context-store";
import { SERVICES, ENTITIES } from "@shared/lib/entity-names";
import { toast } from "@shared/lib/toast";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope } from "@shared/lib/picker-search";

/**
 * Роль владелец сужает подстрокой по настоящему полю `name` (#528): белый список
 * выражения — ровно `name`, разобранный узел применяется через `ToSQL`, то есть
 * оператор `CONTAINS` доезжает до SQL, а не схлопывается в равенство.
 *
 * Прежде ввод не покидал вкладку: роли читались ОДНОЙ страницей
 * (`pageSize: 1000`) и сужались по загруженной метке, а поле отвечало «нет
 * совпадений» — то есть утверждало об отсутствии роли то, чего не спрашивало.
 */
const ROLE_SCOPE = pickerScope({ serverSearchField: "name" });

/**
 * Дерево «аккаунт → проект» сервер сузить ОДНИМ запросом не может, и это
 * свойство самого дерева, а не недоделка.
 *
 * Набранное слово законно относится и к имени аккаунта, и к имени проекта:
 * сузив внешний список по `name`, мы спрятали бы аккаунт, у которого искомый
 * проект как раз и есть, — то есть поиск стал бы ХУЖЕ прежнего. Правильный
 * ответ требует объединения двух сужений по двум осям, а одного запроса на это
 * нет; выдумывать поле запроса нельзя — незнакомое имя это не «фильтр без
 * эффекта», а отказ на всю страницу.
 *
 * Значит остаётся второй законный исход: сужаем в браузере и НАЗЫВАЕМ область
 * в пустом ответе вместо «нет совпадений».
 */
const SCOPE_TREE_SCOPE = pickerScope(undefined);

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
        <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Link to="/iam/users">
          <Typography.Text type="secondary">{ENTITIES.users.plural}</Typography.Text>
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

  const [roleTerm, setRoleTerm] = useState("");
  const debouncedRoleTerm = useDebouncedValue(roleTerm, ROLE_SCOPE.asksServer ? 250 : 0);
  const roleQuery = ROLE_SCOPE.query(debouncedRoleTerm);

  const roles = useQuery({
    // Ключ несёт ввод: без него react-query отдал бы прежний ответ на новый
    // вопрос, и сужение выглядело бы сломанным именно там, где оно работает.
    queryKey: ["iam", "roles", "list", roleQuery.filter ?? ""],
    queryFn: () => iamApi.listRoles({ pageSize: "1000", ...roleQuery }),
    enabled: !!accountId,
    staleTime: 30_000,
  });

  // Выбранная роль обязана пережить сужение: сервер отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ попадать не обязан. Без запоминания метки поле
  // показало бы вместо имени роли идентификатор — ровно то, что канон консоли
  // (правило 2) и запрещает. Тот же приём, что в `RefSelect`.
  const roleGroups = groupedRoleOptions(roles.data?.roles ?? []);
  const roleId = Form.useWatch<string | undefined>("role_id", form);
  const chosenRoleRef = useRef<{ value: string; label: string } | null>(null);
  const chosenRole = roleGroups.flatMap((g) => g.options).find((o) => o.value === roleId);
  if (chosenRole) chosenRoleRef.current = chosenRole;
  const roleOptions =
    roleId && !chosenRole && chosenRoleRef.current?.value === roleId
      ? [{ label: "Выбрано", options: [chosenRoleRef.current] }, ...roleGroups]
      : roleGroups;

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

  // Без аккаунта приглашать некуда — и об этом говорит ТА ЖЕ форма, что у
  // остальных страниц раздела (`ScopeRequiredEmpty`). Здесь стоял `Alert` внутри
  // формы, под которым висел заведомо отключённый подвал: страница показывала
  // ручки, ни одна из которых не работала.
  if (!accountId) {
    return <ScopeRequiredEmpty purpose="пригласить пользователя" />;
  }

  return (
    <FormShell specId="users" mode="create" singular="Пользователь" title="Приглашение пользователя">
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
                filter: (input, path) => path.some((o) => String(o.label).toLowerCase().includes(input.toLowerCase())),
              }}
              placeholder="Сначала аккаунт, затем проект (необязательно)"
              displayRender={(labels) => labels.join(" / ")}
              title={SCOPE_TREE_SCOPE.notice}
              // Пустой ответ обязан называть свою ОБЛАСТЬ: «нет среди
              // загруженных», а не «такого аккаунта или проекта нет». Дерево
              // читается двумя ярусами страниц по тысяче, и за их краем ответ
              // молчит — см. SCOPE_TREE_SCOPE.
              notFoundContent={SCOPE_TREE_SCOPE.emptyText}
              style={{ width: "100%" }}
            />
          </Form.Item>
          <Form.Item
            label="Эл. почта"
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
              onSearch={setRoleTerm}
              // Сузил сервер — клиент НЕ пересеивает: владелец сравнивает с полем
              // `name`, а метка варианта склеена из имени и идентификатора, и
              // повторное сужение вычло бы из ответа строки, присланные краем
              // именно по этому вводу.
              {...(ROLE_SCOPE.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
              title={ROLE_SCOPE.notice}
              // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила
              // ложь: «нет совпадений» на месте «нет среди загруженных».
              notFoundContent={roles.isLoading ? undefined : ROLE_SCOPE.emptyText}
              options={roleOptions}
            />
          </Form.Item>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0, marginLeft: 200 }}>
            Проект и роль необязательны — можно назначить позже через привязки доступа.
          </Typography.Paragraph>
          <FormFooter
            submitLabel="Пригласить"
            submitting={submitting}
            onSubmit={() => form.submit()}
            onCancel={close}
          />
        </Form>
      )}
    </FormShell>
  );
}
