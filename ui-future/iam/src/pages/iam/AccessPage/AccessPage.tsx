// AccessPage — «Права доступа» (KAC-125).
//
// Layout по скриншотам:
// - Header: «Права доступа» + табы «Аккаунт» / «Проект» — по словарю подписей
//   (`entity-names`), теми же словами, что на пилюле выбора в шапке и в меню.
//   Здесь стояли «Облако» и «Каталог»: слова из модели, которой в продукте нет
//   (уровни — Account → Project), причём «Каталог» уже занят админскими
//   справочниками («Каталог типов дисков»). На экране выдачи прав ошибка выбора
//   области означает выданный не туда доступ (#1609).
// - CTA «Настроить доступ» → route-backed grant page with Cascader roles and invite fallback.
// - Filter: имя/идентификатор, тип аккаунта, наследуемые роли.
// - Table: пользователь / роли / идентификатор / федерация / actions.

import { useState, useMemo } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { Button, Cascader, Form, Input, Segmented, Select, Space, Table, Tabs, Tag, Typography, Alert } from "antd";
import { toast } from "@shared/lib/toast";
import { PlusOutlined, MailOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { ColumnsType } from "antd/es/table";
import { buildCreateAccessBindingBody, iamApi, IAM, SUBJECT_TYPE_ENUM, type User, type Role } from "@shared/api/iam";
import { api } from "@shared/api/client";
import { CopyableMonoId } from "@shared/components/organisms/iam/IamCommon";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ScopeRequiredEmpty } from "@/components/molecules/ScopeRequiredEmpty";
import { IamListShell, useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { useContext } from "@shared/lib/context-store";
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";
import { errorText } from "@shared/lib/error-presentation";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope } from "@shared/lib/picker-search";

/** Уровень выдачи — теми же словами, что у платформы: аккаунт и проект. */
type ScopeTab = "account" | "project";

/**
 * Чем сужается список пользователей у владельца (#528).
 *
 * Выделенным словом `search`: подстрока по почте И по идентификатору сразу.
 * Подставить сюда общий ключ полей нельзя — `CONTAINS` на пользователе iam
 * отвергает ЯВНО (`InvalidArgument` на всю страницу, с текстом, называющим
 * правильное написание). Причина различия не в стиле: у пользователя имени нет
 * вовсе, его узнают по почте, поэтому владелец и завёл отдельное слово вместо
 * `name`.
 *
 * Прежде ввод не покидал вкладку: список читался ОДНОЙ страницей
 * (`pageSize: 1000`) и сужался по загруженной метке, а поле отвечало «нет
 * совпадений» — то есть утверждало об отсутствии человека то, чего не
 * спрашивало, и предлагало пригласить того, кто в организации уже есть.
 *
 * Цена, которую надо назвать, а не спрятать: отображаемое имя в это слово НЕ
 * входит — владелец ищет по почте и идентификатору. Поэтому подпись поля
 * говорит именно про них.
 */
const USERS_SCOPE = pickerScope({ serverTerm: "search" });

export function AccessPage() {
  const account = useContext((s) => s.account);
  const project = useContext((s) => s.project);
  const navigate = useNavigate();
  const [scope, setScope] = useState<ScopeTab>("account");

  const accountId = account?.id ?? "";
  const projectId = project?.id ?? "";
  const resourceType = scope;
  const resourceId = scope === "account" ? accountId : projectId;
  const { wrapRef, scrollY } = useTableScrollY();
  // Слот шапки приложения ПУСТ: действие стоит последним в ряду ручек списка,
  // как у generic-страницы. Сбросить его всё равно нужно — слот держит состояние
  // между страницами.
  useHeaderRight(null);
  // Крошки называют ПУТЬ, заголовок — предмет.
  useBreadcrumb(useMemo(() => <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>, []));
  const cta = useMemo(
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

  const bindings = useQuery({
    queryKey: ["iam", "access-bindings", "by-resource", resourceType, resourceId],
    queryFn: () =>
      iamApi.listAccessBindingsByResource(resourceType, resourceId, {
        pageSize: "200",
      }),
    enabled: !!resourceId,
    // поллинг остаётся: журнала у iam нет, подписаться не на что.
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
          <Space size={6} direction="vertical">
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
  ];
  // Здесь стояла колонка «Федерация», чей рендер был КОНСТАНТОЙ — прочерк в
  // каждой строке при любых данных. Заголовок обещал факт, у которого нет
  // производителя: ни ответ края, ни ресурс его не несут. Такой столбец —
  // ровно то же обещание, что мёртвая ссылка: он занимает ширину и утверждает,
  // что о федерации здесь что-то сказано. Вернётся вместе с источником.

  return (
    <IamListShell
      title="Права доступа"
      // Переключатель области стоит В РЯДУ РУЧЕК, а не отдельной строкой над
      // таблицей: он сужает набор строк, то есть принадлежит группе «сузить».
      // Своей строкой он занимал высоту и читался как ещё одна шапка.
      narrowing={
        <Segmented
          value={scope}
          onChange={(v) => setScope(v as ScopeTab)}
          options={[
            { label: ENTITIES.accounts.singular, value: "account" },
            { label: ENTITIES.projects.singular, value: "project", disabled: !projectId },
          ]}
        />
      }
      actions={cta}
    >
      {!resourceId ? (
        // Одна форма «область не выбрана» на весь раздел: прежде здесь стоял
        // `Alert` вверху страницы, у списка ресурсов — `Empty` от antd, у групп
        // — голая строка. Три вида одного предмета.
        <ScopeRequiredEmpty purpose="увидеть права доступа" scope={scope} />
      ) : (
        <>
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
  // Прежние написания уровня (`cloud`/`folder`) читаются как свои — ссылку с
  // ними могли сохранить закладкой. Молча свести их к умолчанию нельзя: адрес
  // с `scope=folder` показал бы выдачи АККАУНТА вместо проекта, то есть не ту
  // область на экране, где ошибка области и есть цена ошибки.
  const scopeParam = searchParams.get("scope");
  const scope: ScopeTab = scopeParam === "project" || scopeParam === "folder" ? "project" : "account";
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
          <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>
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

  // Ввод и есть значение поля: `onSearch` кладёт набранное в `subjectInput`, а
  // выбор кладёт туда идентификатор. Оба уезжают одним и тем же `search=` — и
  // выбранный пользователь остаётся в суженном ответе, потому что владелец
  // смотрит этим словом И на почту, И на идентификатор. Поэтому метку выбранного
  // здесь запоминать не нужно: выпасть из ответа она не может.
  const debouncedSubject = useDebouncedValue(subjectInput, USERS_SCOPE.asksServer ? 250 : 0);
  const subjectQuery = USERS_SCOPE.query(debouncedSubject);

  const users = useQuery({
    // Ключ несёт ввод: без него react-query отдал бы прежний ответ на новый
    // вопрос, и сужение выглядело бы сломанным именно там, где оно работает.
    queryKey: ["iam", "users", "for-invite", accountId, subjectQuery.filter ?? ""],
    queryFn: () => iamApi.listUsers({ pageSize: "1000", account_id: accountId, ...subjectQuery }),
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

      // Anchor-тир гранта: вкладка «Аккаунт» → аккаунт, «Проект» → проект.
      const targetScopeTier = scope === "account" ? "ACCOUNT" : "PROJECT";
      const targetScopeId = scope === "account" ? accountId : projectId;

      if (matchedUser) {
        // Existing user — bulk Create AccessBinding (по одной на каждую выбранную роль).
        // Тело собирается buildCreateAccessBindingBody (форма CreateAccessBindingRequest)
        // и уходит через api.create: он делает request-side snake→camel и БРОСАЕТ
        // ApiError на не-2xx. Прямой fetch не делал ни того ни другого — ключи улетали
        // в snake_case, край их выбрасывал (DiscardUnknown), а отказ был неотличим от
        // успеха, потому что res.ok никто не читал.
        for (const roleId of roleIds) {
          await api.create(
            IAM.accessBindings,
            buildCreateAccessBindingBody({
              subjects: [{ type: SUBJECT_TYPE_ENUM.user, id: matchedUser.id }],
              roleId,
              scopeTier: targetScopeTier,
              scopeId: targetScopeId,
            }),
          );
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
          project_id: targetScopeTier === "PROJECT" ? targetScopeId : undefined,
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
    } catch (err) {
      // Отказ обязан быть виден. Сообщение сервера показывается пользователю (как
      // во всех остальных формах); в консоль браузера конверт ApiError не пишется.
      const detail = errorText(err);
      toast.error(detail ? `Ошибка выдачи доступа: ${detail}` : "Ошибка выдачи доступа");
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
          <Tag color={scope === "account" ? "blue" : "geekblue"}>
            {scope === "account" ? ENTITIES.accounts.singular : ENTITIES.projects.singular}
          </Tag>
          <Typography.Text type="secondary" style={{ marginLeft: 8 }}>
            {scope === "account" ? accountId : projectId}
          </Typography.Text>
        </Form.Item>

        <Form.Item
          label="Кому выдать доступ"
          name="subject_id"
          required
          // Подсказка называет то, чем ИЩЕТСЯ, а не то, что показано. Прежняя
          // обещала поиск по имени — владелец по нему не ищет, и обещание было
          // невыполнимо при любом вводе.
          tooltip="Почта или идентификатор — ищется по всему списку. Если почта не найдена — будет создано приглашение."
        >
          <Select
            placeholder="Почта или usr-… — ищется по всему списку"
            options={subjectOptions}
            value={subjectInput || undefined}
            onChange={(v) => setSubjectInput((v as string) || "")}
            onSearch={(v) => setSubjectInput(v)}
            // Сузил сервер — клиент НЕ пересеивает: `search` смотрит на почту и
            // идентификатор, а метка варианта собрана из имени, почты и
            // идентификатора, и повторное сужение вычло бы из ответа строки,
            // присланные краем именно по этому вводу.
            filterOption={false}
            showSearch
            allowClear
            loading={users.isLoading}
            title={USERS_SCOPE.notice}
            // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила ложь:
            // «нет совпадений» на месте «нет среди загруженных» — и на ней
            // строилось предложение пригласить уже существующего человека.
            notFoundContent={users.isLoading ? undefined : USERS_SCOPE.emptyText}
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
                <Typography.Text>Скопируйте ссылку для входа и отправьте пользователю:</Typography.Text>
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
