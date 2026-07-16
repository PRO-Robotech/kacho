// InlineRoleEditForm — кастомная edit-ветка InlineResourceForm для Role. Грузит
// роль по id, рендерит секции Идентификация (name read-only/immutable по контракту,
// description) + Правила (per-rule RulesEditor, full-width). Роль редактируется из
// `rules[]` (источник истины), НЕ из `permissions[]` (внутренняя compiled-форма; на
// входе НЕ отправляется). update_mask собирается только из реально изменённых полей.
// system-роли сюда не попадают (edit/delete гейтятся is_system=false на detail/list).
// Ошибка не закрывает форму.

import { useEffect, useMemo, useState } from "react";
import { Form, Input, Alert } from "antd";
import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { IAM, type Role, type Rule } from "@shared/api/iam";
import { usePermissionCatalog } from "@shared/api/usePermissionCatalog";
import { useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { RulesEditor, emptyRule, rulesInvalid } from "@/components/organisms/iam/RulesEditor";
import { ImmutableField } from "@shared/components/organisms/form/ImmutableField";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";

/** Стабильная сигнатура набора rules — для diff (изменились ли правила). */
function rulesSig(rules: Rule[]): string {
  return JSON.stringify(rules);
}

export function InlineRoleEditForm({
  roleId,
  onCancel,
  onSuccess,
}: {
  roleId: string;
  onCancel: () => void;
  onSuccess: () => void;
}) {
  const [form] = Form.useForm();
  const [rules, setRules] = useState<Rule[]>([]);
  const [loaded, setLoaded] = useState(false);

  const { data: role, isLoading } = useQuery({
    queryKey: ["roles", "detail", roleId],
    queryFn: () => api.get<Role>(`${IAM.roles}/${roleId}`),
    enabled: !!roleId,
  });

  useEffect(() => {
    if (role && !loaded) {
      // Роль рендерится из rules[] (НЕ из permissions — оно пустое для rules-ролей).
      const initial = role.rules && role.rules.length > 0 ? role.rules : [emptyRule()];
      setRules(initial);
      form.setFieldsValue({ description: role.description ?? "" });
      setLoaded(true);
    }
  }, [role, loaded, form]);

  const mut = useIamMutation({
    method: "PATCH",
    path: () => `${IAM.roles}/${roleId}`,
    invalidateKeys: [
      ["iam", "roles", "list"],
      ["roles", "list"],
      ["roles", "detail", roleId],
    ],
    successText: "Роль обновлена",
    onSuccess: () => {
      onSuccess();
      onCancel();
    },
  });

  // catalog — для labelSelectable-gating (match_labels по non-selectable типу).
  const catalog = usePermissionCatalog().data;
  const invalid = useMemo(() => rulesInvalid(rules, { isSystem: false, catalog }), [rules, catalog]);
  const submitDisabled = invalid.length > 0;

  const submit = () => {
    void form.validateFields().then((v) => {
      const update_mask: string[] = [];
      const body: Record<string, unknown> = {};
      if ((v.description ?? "") !== (role?.description ?? "")) {
        update_mask.push("description");
        body.description = v.description;
      }
      const origRules = role?.rules ?? [];
      const rulesChanged = rulesSig(rules) !== rulesSig(origRules);
      if (rulesChanged) {
        if (invalid.length > 0) return;
        update_mask.push("rules");
        // RBAC rules-model: шлём rules[]. permissions НЕ отправляется.
        body.rules = rules;
        // OCC (Role.Update под конкуренцией) — резолюция через resource_version,
        // если backend его вернул (публичный Get несёт resource_version).
        if (role?.resource_version) body.resource_version = role.resource_version;
      }
      if (update_mask.length === 0) {
        onCancel();
        return;
      }
      body.update_mask = update_mask.join(",");
      void mut.run(body);
    });
  };

  if (isLoading || !role) {
    return (
      <FormShell specId="roles" mode="edit" singular="Роль">
        <Alert type="info" message="Загрузка роли…" />
      </FormShell>
    );
  }

  return (
    <FormShell specId="roles" mode="edit" singular="Роль">
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
      >
        <FormSection title="Идентификация">
          <Form.Item label="Имя">
            <ImmutableField value={role.name} reason="Имя роли неизменяемо после создания" />
          </Form.Item>
          <Form.Item label="Описание" name="description">
            <Input.TextArea rows={2} />
          </Form.Item>
        </FormSection>

        {/* Правила роли — full-width editor вне label-grid. */}
        <FormSection title="Правила">
          <RulesEditor value={rules} onChange={setRules} />
        </FormSection>
      </Form>
      <FormFooter
        submitLabel="Сохранить"
        submitting={mut.submitting}
        submitDisabled={submitDisabled}
        onSubmit={submit}
        onCancel={onCancel}
      />
    </FormShell>
  );
}
