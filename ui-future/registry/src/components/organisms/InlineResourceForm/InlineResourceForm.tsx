// InlineResourceForm — единый диспетчер inline-формы Create/Edit ресурса.
//
// Для NLB-домена (LoadBalancer / Listener / TargetGroup) все формы generic:
// InlineResourceCreateForm / InlineResourceEditForm по spec.fields из реестра.
// Кастомных inline-форм пока нет (VIP-picker для LoadBalancer подключится на
// следующем этапе). Используется:
//   • ResourceFormModal (модалка со списка) — оборачивает в <Modal>;
//   • ResourceShell form-panel (зона 3 detail-страницы) — рендерит как панель.

import { type ReactNode } from "react";
import { InlineResourceCreateForm } from "@/components/organisms/InlineResourceCreateForm";
import { InlineResourceEditForm } from "@/components/organisms/InlineResourceEditForm";
import type { ResourceSpec } from "@/lib/resource-registry";

export interface InlineResourceFormProps {
  spec: ResourceSpec;
  action: "create" | "edit";
  /** Для edit: id ресурса. */
  id?: string;
  /** Для generic edit: уже загруженный ресурс. */
  data?: Record<string, unknown>;
  projectId: string;
  /** Account-scoped IAM-ресурсы (Project / ServiceAccount). */
  accountId?: string;
  /** Preset-поля для generic create (snake_case paths). */
  presetFields?: Record<string, unknown>;
  editablePresetFields?: Record<string, unknown>;
  fieldOptionsFilter?: Record<string, string[]>;
  /** Контекст-id для кастомных форм (родитель при child-create). */
  networkId?: string;
  subnetId?: string;
  /** Заголовок generic-формы (по умолчанию «Создание/Редактирование: <singular>»). */
  title?: string;
  onCancel: () => void;
  onSuccess: () => void;
}

export function InlineResourceForm(props: InlineResourceFormProps): ReactNode {
  const {
    spec,
    action,
    id,
    data,
    projectId,
    accountId,
    presetFields,
    editablePresetFields,
    fieldOptionsFilter,
    title,
    onCancel,
    onSuccess,
  } = props;

  // ── Generic spec-based формы ──
  if (action === "create") {
    return (
      <InlineResourceCreateForm
        spec={spec}
        ctx={{ projectId, accountId }}
        presetFields={presetFields}
        editablePresetFields={editablePresetFields}
        fieldOptionsFilter={fieldOptionsFilter}
        projectId={projectId}
        title={title}
        onCancel={onCancel}
        onSuccess={onSuccess}
      />
    );
  }
  if (action === "edit" && (data || id)) {
    return (
      <InlineResourceEditForm
        spec={spec}
        data={data ?? {}}
        projectId={projectId}
        onCancel={onCancel}
        onSuccess={onSuccess}
      />
    );
  }
  return null;
}
