// InlineRoleCreateForm — кастомная create-ветка InlineResourceForm для Role. Роль
// авторится из `rules[]` (источник истины), НЕ из `permissions[]` (compiled-форма,
// IAM-1 F5 — на входе НЕ отправляется).
//
// IAM-1 F4: роль определяется на уровне `definitionTier{tierType,tierId}` (dotted) —
// iam.account (anchor = Account) ИЛИ iam.project (anchor = Project). iam.cluster —
// system-only (derived isSystem°), из custom-create недоступен. Заменяет плоский
// account_id-селектор AS-IS. Мутация — async Operation polling через useIamMutation.

import { useMemo, useRef, useState } from "react";
import { Form, Input, Select } from "antd";
import { useQuery } from "@tanstack/react-query";
import { iamApi, IAM, type Account, type Project, type Rule, type TierType } from "@shared/api/iam";
import { usePermissionCatalog } from "@shared/api/usePermissionCatalog";
import { useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { RulesEditor, emptyRule, rulesInvalid } from "@/components/organisms/iam/RulesEditor";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope } from "@shared/lib/picker-search";

// Custom-роль определяется на account- ИЛИ project-уровне (cluster = system-only).
const TIER_OPTIONS: { value: Extract<TierType, "iam.account" | "iam.project">; label: string }[] = [
  { value: "iam.account", label: "iam.account — роль уровня аккаунта" },
  { value: "iam.project", label: "iam.project — роль уровня проекта" },
];

/**
 * Чем сужается список якорей у владельца (#528).
 *
 * Account и Project iam сужает подстрокой по настоящему полю `name`: белый
 * список выражения — ровно `name`, разобранный узел применяется через `ToSQL`,
 * то есть оператор `CONTAINS` доезжает до SQL, а не схлопывается в равенство.
 *
 * Прежде ввод не покидал вкладку: якорь читался ОДНОЙ страницей
 * (`pageSize: 1000`) и сужался по загруженной метке, а поле отвечало «нет
 * совпадений» — то есть утверждало об отсутствии аккаунта или проекта то, чего
 * не спрашивало. Продолжения («показать ещё») у выпадающего списка нет by
 * construction, поэтому узнать правду пользователю было неоткуда, и роль просто
 * нельзя было завести на якорь за пределами первой тысячи.
 */
const ANCHOR_SCOPE = pickerScope({ serverSearchField: "name" });

export function InlineRoleCreateForm({
  accountId,
  onCancel,
  onSuccess,
}: {
  /** Account из IAM-контекста — preset для anchor'а account-tier. */
  accountId?: string;
  onCancel: () => void;
  onSuccess: () => void;
}) {
  const [form] = Form.useForm();
  const [rules, setRules] = useState<Rule[]>([emptyRule()]);
  // tierType управляет тем, какой anchor-селектор (Account vs Project) показан.
  const tierType = Form.useWatch<TierType>("tier_type", form) ?? "iam.account";

  // Ввод один на оба якорных поля, и это не экономия: полей два, но в один
  // момент времени рендерится РОВНО одно (ветка по tierType), а смена уровня
  // сбрасывает и выбранный якорь, и набранное. Разнести ввод по двум состояниям
  // значило бы завести второе, которое никогда не расходится с первым.
  const [anchorTerm, setAnchorTerm] = useState("");
  const debouncedAnchorTerm = useDebouncedValue(anchorTerm, ANCHOR_SCOPE.asksServer ? 250 : 0);
  const anchorQuery = ANCHOR_SCOPE.query(debouncedAnchorTerm);
  const accountQuery = tierType === "iam.account" ? anchorQuery : {};
  const projectQuery = tierType === "iam.project" ? anchorQuery : {};

  const accounts = useQuery({
    // Ключ несёт ввод: без него react-query отдал бы прежний ответ на новый
    // вопрос, и сужение выглядело бы сломанным именно там, где оно работает.
    queryKey: ["iam", "accounts", "list", accountQuery.filter ?? ""],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000", ...accountQuery }),
    staleTime: 30_000,
  });
  const accountList = accounts.data?.accounts ?? [];

  const projects = useQuery({
    queryKey: ["iam", "projects", "list", projectQuery.filter ?? ""],
    queryFn: () => iamApi.listProjects({ pageSize: "1000", ...projectQuery }),
    staleTime: 30_000,
    enabled: tierType === "iam.project",
  });
  const projectList = projects.data?.projects ?? [];

  const accountOptions = accountList.map((a: Account) => ({ value: a.id, label: `${a.name} · ${a.id}` }));
  const projectOptions = projectList.map((p: Project) => ({ value: p.id, label: `${p.name} · ${p.id}` }));

  // Выбранный якорь обязан пережить сужение: сервер отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ попадать не обязан. Без запоминания метки поле
  // показало бы вместо имени идентификатор — ровно то, что канон консоли
  // (правило 2) и запрещает. Тот же приём, что в `RefSelect`.
  const tierId = Form.useWatch<string | undefined>("tier_id", form);
  const anchorOptions = tierType === "iam.project" ? projectOptions : accountOptions;
  const chosenAnchorRef = useRef<{ value: string; label: string } | null>(null);
  const chosenAnchor = anchorOptions.find((o) => o.value === tierId);
  if (chosenAnchor) chosenAnchorRef.current = chosenAnchor;
  const anchorSelectOptions =
    tierId && !chosenAnchor && chosenAnchorRef.current?.value === tierId
      ? [chosenAnchorRef.current, ...anchorOptions]
      : anchorOptions;

  const mut = useIamMutation({
    method: "POST",
    path: IAM.roles,
    invalidateKeys: [
      ["iam", "roles", "list"],
      ["roles", "list"],
    ],
    successText: "Роль создана",
    onSuccess: () => {
      form.resetFields();
      setRules([emptyRule()]);
      onSuccess();
      onCancel();
    },
  });

  // custom-роль (isSystem=false): module/resource-`*` запрещён, verb-`*` ок.
  const catalog = usePermissionCatalog().data;
  const invalid = useMemo(() => rulesInvalid(rules, { isSystem: false, catalog }), [rules, catalog]);
  const submitDisabled = invalid.length > 0 || rules.length === 0;

  const submit = () => {
    void form.validateFields().then((v) => {
      if (submitDisabled) return;
      const body: Record<string, unknown> = {
        name: v.name,
        // IAM-1 F4: definitionTier{tierType,tierId} (dotted) вместо flat account_id.
        definition_tier: { tier_type: v.tier_type, tier_id: v.tier_id },
        // IAM-1 F5: rules[] — авторская политика; permissions[] НЕ отправляется.
        rules,
      };
      if (v.description) body.description = v.description;
      void mut.run(body);
    });
  };

  return (
    <FormShell specId="roles" mode="create" singular="Роль">
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
        initialValues={{ tier_type: "iam.account", tier_id: accountId || undefined }}
      >
        <FormSection title="Идентификация">
          <Form.Item
            label="Уровень (tierType)"
            name="tier_type"
            required
            rules={[{ required: true, message: "Выберите уровень роли" }]}
            tooltip="На каком уровне определена роль: аккаунт или проект. Уровень задаёт границу, в которой роль действует."
          >
            <Select
              options={TIER_OPTIONS}
              onChange={() => {
                form.setFieldValue("tier_id", undefined);
                // Ввод принадлежал ПРЕЖНЕМУ уровню: оставив его, мы сузили бы
                // список проектов словом, набранным про аккаунт.
                setAnchorTerm("");
              }}
              data-testid="role-tier-type"
            />
          </Form.Item>
          {tierType === "iam.project" ? (
            <Form.Item
              label="Якорь (проект)"
              name="tier_id"
              required
              rules={[{ required: true, message: "Выберите проект" }]}
            >
              <Select
                placeholder="Выберите проект"
                options={anchorSelectOptions}
                loading={projects.isLoading}
                showSearch
                onSearch={setAnchorTerm}
                // Сузил сервер — клиент НЕ пересеивает: владелец сравнивает с
                // полем `name`, а метка варианта склеена из имени и
                // идентификатора, и повторное сужение вычло бы из ответа строки,
                // присланные именно по этому вводу.
                {...(ANCHOR_SCOPE.asksServer
                  ? { filterOption: false as const }
                  : { optionFilterProp: "label" as const })}
                title={ANCHOR_SCOPE.notice}
                // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила
                // ложь: «нет совпадений» на месте «нет среди загруженных».
                notFoundContent={projects.isLoading ? undefined : ANCHOR_SCOPE.emptyText}
              />
            </Form.Item>
          ) : (
            <Form.Item
              label="Якорь (аккаунт)"
              name="tier_id"
              required
              rules={[{ required: true, message: "Выберите аккаунт" }]}
            >
              <Select
                placeholder="Выберите аккаунт"
                options={anchorSelectOptions}
                loading={accounts.isLoading}
                showSearch
                onSearch={setAnchorTerm}
                {...(ANCHOR_SCOPE.asksServer
                  ? { filterOption: false as const }
                  : { optionFilterProp: "label" as const })}
                title={ANCHOR_SCOPE.notice}
                notFoundContent={accounts.isLoading ? undefined : ANCHOR_SCOPE.emptyText}
              />
            </Form.Item>
          )}
          <Form.Item
            label="Имя"
            name="name"
            required
            rules={[
              {
                required: true,
                // Backend: custom-role name ^[a-z][a-z0-9_]{0,40}$ — без дефиса.
                pattern: /^[a-z][a-z0-9_]{0,40}$/,
                message: "строчные латинские буквы, цифры, подчёркивания; начинается с буквы; до 41 символа",
              },
            ]}
          >
            <Input placeholder="my_role" />
          </Form.Item>
          <Form.Item label="Описание" name="description">
            <Input.TextArea rows={2} />
          </Form.Item>
        </FormSection>

        {/* Правила роли (module/resources/verbs + селектор all/names/labels) —
            full-width editor вне label-grid (RulesEditor — сложный составной блок). */}
        <FormSection title="Правила">
          <RulesEditor value={rules} onChange={setRules} />
        </FormSection>
      </Form>
      <FormFooter
        submitLabel="Создать"
        submitting={mut.submitting}
        submitDisabled={submitDisabled}
        onSubmit={submit}
        onCancel={onCancel}
      />
    </FormShell>
  );
}
