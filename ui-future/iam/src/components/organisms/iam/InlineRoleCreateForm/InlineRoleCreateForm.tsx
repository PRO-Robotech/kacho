// InlineRoleCreateForm — кастомная create-ветка InlineResourceForm для Role. Роль
// авторится из `rules[]` (источник истины), НЕ из `permissions[]` (compiled-форма,
// IAM-1 F5 — на входе НЕ отправляется).
//
// IAM-1 F4: роль определяется на уровне `definitionTier{tierType,tierId}` (dotted) —
// iam.account (anchor = Account) ИЛИ iam.project (anchor = Project). iam.cluster —
// system-only (derived isSystem°), из custom-create недоступен. Заменяет плоский
// account_id-селектор AS-IS. Мутация — async Operation polling через useIamMutation.

import { useMemo, useState } from "react";
import { Form, Input, Select } from "antd";
import { useQuery } from "@tanstack/react-query";
import { iamApi, IAM, type Account, type Project, type Rule, type TierType } from "@shared/api/iam";
import { usePermissionCatalog } from "@shared/api/usePermissionCatalog";
import { useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { RulesEditor, emptyRule, rulesInvalid } from "@/components/organisms/iam/RulesEditor";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";

// Custom-роль определяется на account- ИЛИ project-уровне (cluster = system-only).
const TIER_OPTIONS: { value: Extract<TierType, "iam.account" | "iam.project">; label: string }[] = [
  { value: "iam.account", label: "iam.account — роль уровня Account" },
  { value: "iam.project", label: "iam.project — роль уровня Project" },
];

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

  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const accountList = accounts.data?.accounts ?? [];

  const projects = useQuery({
    queryKey: ["iam", "projects", "list"],
    queryFn: () => iamApi.listProjects({ pageSize: "1000" }),
    staleTime: 30_000,
    enabled: tierType === "iam.project",
  });
  const projectList = projects.data?.projects ?? [];

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
            tooltip="Где определена роль: account-уровень (anchor = Account) или project-уровень (anchor = Project)."
          >
            <Select
              options={TIER_OPTIONS}
              onChange={() => form.setFieldValue("tier_id", undefined)}
              data-testid="role-tier-type"
            />
          </Form.Item>
          {tierType === "iam.project" ? (
            <Form.Item
              label="Anchor (Project)"
              name="tier_id"
              required
              rules={[{ required: true, message: "Выберите Project" }]}
            >
              <Select
                placeholder="Выберите Project"
                options={projectList.map((p: Project) => ({ value: p.id, label: `${p.name} · ${p.id}` }))}
                loading={projects.isLoading}
                showSearch
                optionFilterProp="label"
              />
            </Form.Item>
          ) : (
            <Form.Item
              label="Anchor (Account)"
              name="tier_id"
              required
              rules={[{ required: true, message: "Выберите Account" }]}
            >
              <Select
                placeholder="Выберите Account"
                options={accountList.map((a: Account) => ({ value: a.id, label: `${a.name} · ${a.id}` }))}
                loading={accounts.isLoading}
                showSearch
                optionFilterProp="label"
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
        submitLabel="Создать роль"
        submitting={mut.submitting}
        submitDisabled={submitDisabled}
        onSubmit={submit}
        onCancel={onCancel}
      />
    </FormShell>
  );
}
