// InlineRoleCreateForm — кастомная create-ветка InlineResourceForm для Role. Роль
// авторится из `rules[]` (источник истины), НЕ из `permissions[]` (внутренняя
// compiled-форма, на входе НЕ отправляется). Секции: Идентификация (account_id
// Select required, name regex custom-role, description) + Правила (per-rule
// RulesEditor — full-width вне label-grid). Account-scoped: создаются только
// custom-роли (account_id обязателен). Мутация — async Operation polling через
// useIamMutation. Ошибка не закрывает форму.

import { useMemo, useState } from "react";
import { Form, Input, Select } from "antd";
import { useQuery } from "@tanstack/react-query";
import { iamApi, IAM, type Account, type Rule } from "@shared/api/iam";
import { usePermissionCatalog } from "@shared/api/usePermissionCatalog";
import { useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { RulesEditor, emptyRule, rulesInvalid } from "@/components/organisms/iam/RulesEditor";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";

export function InlineRoleCreateForm({
  accountId,
  onCancel,
  onSuccess,
}: {
  /** Account из IAM-контекста — preset для account_id-селектора. */
  accountId?: string;
  onCancel: () => void;
  onSuccess: () => void;
}) {
  const [form] = Form.useForm();
  const [rules, setRules] = useState<Rule[]>([emptyRule()]);

  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const accountList = accounts.data?.accounts ?? [];

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
  // catalog — для labelSelectable-gating (match_labels по non-selectable типу
  // блокирует submit заранее, до backend INVALID_ARGUMENT).
  const catalog = usePermissionCatalog().data;
  const invalid = useMemo(() => rulesInvalid(rules, { isSystem: false, catalog }), [rules, catalog]);
  const submitDisabled = invalid.length > 0 || rules.length === 0;

  const submit = () => {
    void form.validateFields().then((v) => {
      if (submitDisabled) return;
      const body: Record<string, unknown> = {
        account_id: v.account_id,
        name: v.name,
        // RBAC rules-model: шлём rules[]. permissions НЕ отправляется.
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
        initialValues={{ account_id: accountId || undefined }}
      >
        <FormSection title="Идентификация">
          <Form.Item label="Аккаунт" name="account_id" required rules={[{ required: true, message: "Выберите Account" }]}>
            <Select
              placeholder="Выберите Account"
              options={accountList.map((a: Account) => ({
                value: a.id,
                label: `${a.name} · ${a.id}`,
              }))}
              loading={accounts.isLoading}
              showSearch
              optionFilterProp="label"
            />
          </Form.Item>
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
        submitLabel="Создать роль"
        submitting={mut.submitting}
        submitDisabled={submitDisabled}
        onSubmit={submit}
        onCancel={onCancel}
      />
    </FormShell>
  );
}
