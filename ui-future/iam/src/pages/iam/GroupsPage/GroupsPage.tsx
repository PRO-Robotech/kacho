// GroupsPage — список Group per Account + Create + Edit + Delete +
// inline Members-panel (раскрывается через expandedRowRender → table)
// со списком member'ов (User/SA) + Add/Remove.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Button, Form, Input, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import { PlusOutlined, DeleteOutlined, EditOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { api } from "@shared/api/client";
import { iamApi, IAM, type Group, type User, type ServiceAccount } from "@shared/api/iam";
import { useIamMutation, fmtTs, CopyableMonoId } from "@shared/components/organisms/iam/IamCommon";
import { DETAIL_CONTENT_WIDTH, DetailSurface } from "@shared/components/organisms/DetailShell";
import {
  EDITOR_ACTIONS_WIDTH,
  editorBodyStyle,
  editorEmptyStyle,
  editorFirstRowStyle,
  editorHeadCellStyle,
  editorIconButtonStyle,
  editorRowStyle,
} from "@shared/components/organisms/form/editor-surface";
import { ScopeRequiredEmpty } from "@/components/molecules/ScopeRequiredEmpty";
import { IamRefLink } from "@/components/molecules/IamRefLink";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { IamListShell, useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { useContext } from "@shared/lib/context-store";
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";
import { LabelsEditor, labelsFromEntries, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope, type PickerScope } from "@shared/lib/picker-search";
import { groupDetailPathFromOp } from "./groupNav";

/**
 * Чем сужается список кандидатов в участники у своего владельца (#528).
 *
 * Ключей два, и подставить один вместо другого нельзя: iam отвергает `CONTAINS`
 * на пользователе ЯВНО (`InvalidArgument` на всю страницу), а слова `search` не
 * знает никто, кроме пользователя. Причина различия не в стиле: у пользователя
 * имени нет вовсе — его узнают по почте, поэтому владелец завёл выделенное
 * слово, смотрящее на почту И на идентификатор сразу; у служебной учётки имя
 * есть, и её белый список — настоящее поле `name`, применяемое через `ToSQL`.
 *
 * Прежде ввод не покидал вкладку: обе стороны читались ОДНОЙ страницей
 * (`pageSize: 1000`) и сужались по загруженной метке, а поле отвечало «нет
 * совпадений» — то есть утверждало об отсутствии человека или учётки то, чего
 * не спрашивало. Тысяча первого участника нельзя было добавить в группу вовсе,
 * и продолжения («показать ещё») у выпадающего списка нет by construction.
 */
const MEMBER_SCOPE: Record<"user" | "service_account", PickerScope> = {
  user: pickerScope({ serverTerm: "search" }),
  service_account: pickerScope({ serverSearchField: "name" }),
};

export function GroupsPage() {
  const account = useContext((s) => s.account);
  const accountId = account?.id ?? null;
  const navigate = useNavigate();
  // Слот шапки приложения ПУСТ: «Создать» стоит последней в ряду ручек списка,
  // как у generic-страницы. Сбросить его всё равно нужно — слот держит состояние
  // между страницами и донёс бы сюда чужую кнопку.
  useHeaderRight(null);
  // Крошки называют ПУТЬ, заголовок — предмет, и дважды одно не говорят:
  // последнее звено повторяло бы заголовок страницы двадцатью точками ниже.
  useBreadcrumb(useMemo(() => <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>, []));
  const cta = useMemo(
    () => (
      // Кнопка называет ДЕЙСТВИЕ: предмет уже назван заголовком страницы левее
      // и вчетверо крупнее.
      <Button
        type="primary"
        icon={<PlusOutlined />}
        disabled={!accountId}
        onClick={() => navigate("/iam/groups/create")}
      >
        Создать
      </Button>
    ),
    [accountId, navigate],
  );

  const list = useQuery({
    queryKey: ["iam", "groups", "list", accountId],
    queryFn: () => iamApi.listGroups({ account_id: accountId!, pageSize: "200" }),
    enabled: !!accountId,
    // поллинг остаётся: журнала у iam нет — среди владельцев глагола подписки
    // его не значится (владельцев называет карта предметов, `STREAM_SUBJECTS`).
    refetchInterval: 5_000,
    staleTime: 0,
  });

  const del = useIamMutation({
    method: "DELETE",
    path: (b) => `${IAM.groups}/${b as string}`,
    invalidateKeys: [["iam", "groups", "list"]],
    successText: "Group удалён",
  });

  const groups = list.data?.groups ?? [];
  const { wrapRef, scrollY } = useTableScrollY();

  const columns: ColumnsType<Group> = [
    {
      title: "Имя",
      dataIndex: "name",
      key: "name",
      render: (v) => <Typography.Text strong>{v}</Typography.Text>,
    },
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      render: (v) => <CopyableMonoId id={v} />,
    },
    {
      title: "Описание",
      dataIndex: "description",
      key: "description",
      render: (v) => v || <Typography.Text type="secondary">—</Typography.Text>,
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
      width: 110,
      render: (_v, row) => (
        <Space size={4}>
          <Button
            size="small"
            type="text"
            icon={<EditOutlined />}
            onClick={() => navigate(`/iam/groups/${row.id}/edit`)}
          />
          <Popconfirm
            title="Удалить группу?"
            description={`Удалить «${row.name}»?`}
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

  if (!accountId) return <ScopeRequiredEmpty purpose={`увидеть ${ENTITIES.groups.plural}`} />;

  return (
    <IamListShell title={ENTITIES.groups.plural} actions={cta}>
      <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <Table<Group>
          rowKey="id"
          size="small"
          className="kc-table"
          loading={list.isLoading}
          dataSource={groups}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          onRow={(row) => ({
            onClick: (e) => {
              if (
                (e.target as HTMLElement)?.closest(
                  "button, a, .ant-dropdown, .ant-popover, .ant-select, .ant-table-row-expand-icon",
                )
              )
                return;
              navigate(`/iam/groups/${row.id}`);
            },
            style: { cursor: "pointer" },
          })}
          expandable={{
            expandedRowRender: (row) => <GroupMembersPanel group={row} accountId={accountId} />,
          }}
          locale={{ emptyText: "Групп нет. Создайте первую." }}
        />
      </div>
    </IamListShell>
  );
}

export function GroupCreatePage() {
  const account = useContext((s) => s.account);
  const accountId = account?.id ?? null;
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  useHeaderRight(useMemo(() => null, []));
  useBreadcrumb(
    useMemo(
      () => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>
          <Typography.Text type="secondary">/</Typography.Text>
          <Link to="/iam/groups">
            <Typography.Text type="secondary">{ENTITIES.groups.plural}</Typography.Text>
          </Link>
          <Typography.Text type="secondary">/</Typography.Text>
          <Typography.Text strong>Создать</Typography.Text>
        </span>
      ),
      [],
    ),
  );
  const mut = useIamMutation({
    method: "POST",
    path: IAM.groups,
    invalidateKeys: [["iam", "groups", "list"]],
    successText: "Group создана",
    onSuccess: (op) => {
      form.resetFields();
      navigate(groupDetailPathFromOp(op));
    },
  });

  return (
    <FormShell specId="groups" mode="create" singular="Группа">
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
        onFinish={(v) => {
          if (!accountId) return;
          const body: Record<string, unknown> = {
            account_id: accountId,
            name: v.name,
          };
          if (v.description) body.description = v.description;
          const labelMap = labelsFromEntries(labels);
          if (Object.keys(labelMap).length > 0) body.labels = labelMap;
          void mut.run(body);
        }}
      >
        <Form.Item
          label="Имя"
          name="name"
          required
          rules={[
            {
              required: true,
              pattern: /^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$/,
              message: "lowercase, цифры, дефисы; 3-63 символа",
            },
          ]}
        >
          <Input placeholder="developers" />
        </Form.Item>
        <Form.Item label="Метки">
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>
        <Form.Item label="Описание" name="description">
          <Input.TextArea rows={2} />
        </Form.Item>
        <FormFooter
          submitLabel="Создать"
          submitting={mut.submitting}
          submitDisabled={!accountId}
          onSubmit={() => form.submit()}
          onCancel={() => navigate("/iam/groups")}
        />
      </Form>
    </FormShell>
  );
}

export function GroupEditPage() {
  const { uid } = useParams();
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const { data: group } = useQuery({
    queryKey: ["iam", "groups", "detail", uid],
    queryFn: () => api.get<Group>(`${IAM.groups}/${uid}`),
    enabled: !!uid,
  });
  useEffect(() => {
    if (!group) return;
    form.setFieldsValue({ name: group.name ?? "", description: group.description ?? "" });
  }, [form, group]);
  useHeaderRight(useMemo(() => null, []));
  useBreadcrumb(
    useMemo(
      () => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>
          <Typography.Text type="secondary">/</Typography.Text>
          <Link to="/iam/groups">
            <Typography.Text type="secondary">{ENTITIES.groups.plural}</Typography.Text>
          </Link>
          <Typography.Text type="secondary">/</Typography.Text>
          <Typography.Text strong>Редактирование</Typography.Text>
        </span>
      ),
      [],
    ),
  );
  const mut = useIamMutation({
    method: "PATCH",
    path: () => `${IAM.groups}/${uid}`,
    invalidateKeys: [["iam", "groups", "list"]],
    successText: "Group обновлена",
    onSuccess: () => navigate("/iam/groups"),
  });

  return (
    <FormShell specId="groups" mode="edit" singular="Группа">
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
        initialValues={{
          name: group?.name ?? "",
          description: group?.description ?? "",
        }}
        onFinish={(v) => {
          const update_mask: string[] = [];
          const body: Record<string, unknown> = {};
          if ((v.name ?? "") !== (group?.name ?? "")) {
            update_mask.push("name");
            body.name = v.name;
          }
          if ((v.description ?? "") !== (group?.description ?? "")) {
            update_mask.push("description");
            body.description = v.description;
          }
          if (update_mask.length === 0) {
            navigate("/iam/groups");
            return;
          }
          body.update_mask = update_mask.join(",");
          void mut.run(body);
        }}
      >
        <Form.Item label="Имя" name="name">
          <Input />
        </Form.Item>
        <Form.Item label="Описание" name="description">
          <Input.TextArea rows={2} />
        </Form.Item>
        <FormFooter
          submitLabel="Сохранить"
          submitting={mut.submitting}
          onSubmit={() => form.submit()}
          onCancel={() => navigate("/iam/groups")}
        />
      </Form>
    </FormShell>
  );
}

export function GroupMembersPanel({ group, accountId }: { group: Group; accountId: string | null }) {
  const members = useQuery({
    queryKey: ["iam", "groups", group.id, "members"],
    queryFn: () => iamApi.listGroupMembers(group.id, { pageSize: "200" }),
    // поллинг остаётся: состав группы — предмет iam, а журнала у iam нет.
    refetchInterval: 5_000,
    staleTime: 0,
  });

  // Тип кандидата объявлен ДО списков: он решает, кого спрашивать и каким
  // ключом. Ввод уходит запросом ТОЛЬКО тому владельцу, чей тип сейчас выбран —
  // иначе набранное имя учётки уезжало бы ещё и в список пользователей: запрос,
  // которого никто не просил, и сброс чужого кэша от чужого ввода.
  const [pickerType, setPickerType] = useState<"user" | "service_account">("user");
  const [pickerValue, setPickerValue] = useState<string | null>(null);
  const memberScope = MEMBER_SCOPE[pickerType];
  const [memberTerm, setMemberTerm] = useState("");
  const debouncedMemberTerm = useDebouncedValue(memberTerm, memberScope.asksServer ? 250 : 0);
  const userQuery = pickerType === "user" ? MEMBER_SCOPE.user.query(debouncedMemberTerm) : {};
  const saQuery = pickerType === "service_account" ? MEMBER_SCOPE.service_account.query(debouncedMemberTerm) : {};

  const users = useQuery({
    // Ключ несёт ввод: без него react-query отдал бы прежний ответ на новый
    // вопрос, и сужение выглядело бы сломанным именно там, где оно работает.
    queryKey: ["iam", "users", "list", userQuery.filter ?? ""],
    queryFn: () => iamApi.listUsers({ pageSize: "1000", ...userQuery }),
    staleTime: 30_000,
  });

  const sas = useQuery({
    queryKey: ["iam", "service-accounts", "list", accountId, saQuery.filter ?? ""],
    queryFn: () => iamApi.listServiceAccounts({ account_id: accountId!, pageSize: "1000", ...saQuery }),
    enabled: !!accountId,
    staleTime: 30_000,
  });

  const addMut = useIamMutation({
    method: "ACTION",
    path: `${IAM.groups}/${group.id}:addMember`,
    invalidateKeys: [["iam", "groups", group.id, "members"]],
    successText: "Участник добавлен",
  });

  const removeMut = useIamMutation({
    method: "ACTION",
    path: `${IAM.groups}/${group.id}:removeMember`,
    invalidateKeys: [["iam", "groups", group.id, "members"]],
    successText: "Участник удалён",
  });

  const memberList = members.data?.members ?? [];

  const memberOptions =
    pickerType === "user"
      ? (users.data?.users ?? []).map((u: User) => ({
          value: u.id,
          label: `${u.email || u.display_name || u.id} · ${u.id}`,
        }))
      : (sas.data?.service_accounts ?? []).map((sa: ServiceAccount) => ({
          value: sa.id,
          label: `${sa.name} · ${sa.id}`,
        }));

  // Выбранный кандидат обязан пережить сужение: сервер отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ попадать не обязан. Без запоминания метки поле
  // показало бы вместо почты идентификатор — ровно то, что канон консоли
  // (правило 2) и запрещает. Тот же приём, что в `RefSelect`.
  const chosenMemberRef = useRef<{ value: string; label: string } | null>(null);
  const chosenMember = memberOptions.find((o) => o.value === pickerValue);
  if (chosenMember) chosenMemberRef.current = chosenMember;
  const memberSelectOptions =
    pickerValue && !chosenMember && chosenMemberRef.current?.value === pickerValue
      ? [chosenMemberRef.current, ...memberOptions]
      : memberOptions;

  const MEMBER_TYPE_LABEL: Record<string, string> = { user: "пользователь", service_account: "сервисный аккаунт" };

  return (
    // Ширина блока — ОДНА на все секции карточки (`DETAIL_CONTENT_WIDTH`): здесь
    // стояло своё число 820 против 920 у соседних, и правый край страницы шёл
    // лесенкой.
    <div style={{ marginTop: 24, maxWidth: DETAIL_CONTENT_WIDTH }}>
      {/* Ряд ручек на вкладке карточки живёт по тем же правилам, что ряд
          инструментов списка: одна высота и один радиус у всех (32 и 8). Класс
          задаёт их в одном месте — иначе два селекта и кнопка приносят каждый
          свою высоту, и полоса читается составленной случайно. */}
      <Space className="kc-list-tools" size={8} wrap style={{ marginBottom: 12 }}>
        <Select
          value={pickerType}
          style={{ width: 200 }}
          onChange={(v) => {
            setPickerType(v);
            setPickerValue(null);
            // Ввод принадлежал ПРЕЖНЕМУ типу: оставив его, мы сузили бы список
            // служебных учёток словом, набранным про человека, — и другим ключом.
            setMemberTerm("");
          }}
          options={[
            { value: "user", label: "Пользователь" },
            { value: "service_account", label: "Сервисный аккаунт" },
          ]}
        />
        <Select
          style={{ width: 360 }}
          value={pickerValue ?? undefined}
          onChange={(v) => setPickerValue(v)}
          placeholder={pickerType === "user" ? "Выберите пользователя" : "Выберите сервисный аккаунт"}
          options={memberSelectOptions}
          showSearch
          onSearch={setMemberTerm}
          // Сузил сервер — клиент НЕ пересеивает: у человека владелец смотрит на
          // почту и идентификатор, у учётки — на `name`, а метка варианта склеена
          // из имени и идентификатора. Повторное сужение вычло бы из ответа
          // строки, присланные краем именно по этому вводу.
          {...(memberScope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
          title={memberScope.notice}
          // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила ложь:
          // «нет совпадений» на месте «нет среди загруженных».
          notFoundContent={
            (pickerType === "user" ? users.isLoading : sas.isLoading) ? undefined : memberScope.emptyText
          }
          loading={pickerType === "user" ? users.isLoading : sas.isLoading}
        />
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={!pickerValue}
          onClick={() => {
            if (!pickerValue) return;
            void addMut.run({ member_type: pickerType, member_id: pickerValue });
            setPickerValue(null);
          }}
        >
          Добавить
        </Button>
      </Space>

      {/* ШАПКА СЕКЦИИ — СТРОКА ТОЙ ЖЕ ТАБЛИЦЫ (`DetailSurface`), а не блок над
          ней. Здесь стояли ДВЕ конструкции подряд: `SectionHeader` со своей
          иконкой, надзаголовком «Список» и счётчиком — и отдельная рамка со
          своим радиусом под таблицей. На стыке шли две линии и два радиуса,
          шапка читалась приделанной сверху, а счётчик повторял то, что и так
          сосчитано глазами. Геометрия строк и ячеек берётся из общего набора
          (`editor-surface`), которым нарисованы метки, маршруты и блоки CIDR:
          это один предмет — «набор значений, который правят по одному». */}
      <DetailSurface title="Участники" note="Пользователи и сервисные аккаунты">
        <div style={editorBodyStyle}>
          <table className="w-full" style={{ tableLayout: "fixed", borderCollapse: "collapse" }}>
            <colgroup>
              <col style={{ width: 170 }} />
              <col />
              <col style={{ width: 180 }} />
              <col style={{ width: EDITOR_ACTIONS_WIDTH }} />
            </colgroup>
            <thead>
              <tr>
                {["Тип", "Участник", "Добавлен"].map((h) => (
                  <th key={h} className="text-left" style={editorHeadCellStyle}>
                    {h}
                  </th>
                ))}
                <th style={editorHeadCellStyle} />
              </tr>
            </thead>
            <tbody>
              {memberList.length === 0 && (
                <tr>
                  <td colSpan={4} style={editorEmptyStyle}>
                    Участников нет
                  </td>
                </tr>
              )}
              {memberList.map((m, i) => (
                <tr
                  key={`${m.member_type}:${m.member_id}`}
                  className="kc-kv-row"
                  // Линия РАЗДЕЛЯЕТ: первой строке она не нужна — над ней уже
                  // стоит нижняя граница шапки колонок.
                  style={i === 0 ? editorFirstRowStyle : editorRowStyle}
                >
                  <td style={{ padding: "0 16px", verticalAlign: "middle" }}>
                    <Tag color={m.member_type === "user" ? "blue" : "gold"} style={{ margin: 0 }}>
                      {MEMBER_TYPE_LABEL[m.member_type] ?? m.member_type}
                    </Tag>
                  </td>
                  <td style={{ padding: "0 16px", verticalAlign: "middle" }}>
                    <IamRefLink
                      specId={m.member_type === "user" ? "users" : "service-accounts"}
                      refId={m.member_id}
                      nameField={m.member_type === "user" ? "email" : "name"}
                    />
                  </td>
                  <td style={{ padding: "0 16px", verticalAlign: "middle" }}>
                    <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                      {fmtTs(m.added_at)}
                    </Typography.Text>
                  </td>
                  <td style={{ textAlign: "center", verticalAlign: "middle" }}>
                    <Popconfirm
                      title="Удалить участника?"
                      okText="Удалить"
                      okButtonProps={{ danger: true }}
                      cancelText="Отмена"
                      onConfirm={() => void removeMut.run({ member_type: m.member_type, member_id: m.member_id })}
                    >
                      <Button size="small" type="text" danger icon={<DeleteOutlined />} style={editorIconButtonStyle} />
                    </Popconfirm>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </DetailSurface>
    </div>
  );
}
