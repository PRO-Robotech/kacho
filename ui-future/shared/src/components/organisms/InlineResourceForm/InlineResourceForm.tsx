// InlineResourceForm — единый диспетчер inline-формы Create/Edit ресурса.
//
// По spec.id выбирает кастомную resource-specific форму (Subnet / SecurityGroup /
// AddressPool / NetworkInterface) либо generic InlineResourceCreateForm /
// InlineResourceEditForm. Используется:
//   • ResourceFormModal (модалка со списка) — оборачивает в <Modal>;
//   • ResourceShell form-panel (зона 3 detail-страницы) — рендерит как панель.
//
// Edit-форму ресурс грузит либо сам по id (custom-формы Subnet/SG/NIC/Pool),
// либо принимает уже загруженные `data` (generic). Поэтому caller для generic
// edit передаёт `data`, для custom — достаточно `id`.

import { type ReactNode } from "react";
import { InlineResourceCreateForm } from "@shared/components/organisms/InlineResourceCreateForm";
import { InlineResourceEditForm } from "@shared/components/organisms/InlineResourceEditForm";
import { InlineSubnetCreateForm } from "@shared/components/organisms/InlineSubnetCreateForm";
import { InlineSubnetEditForm } from "@shared/components/organisms/InlineSubnetEditForm";
import { InlineSecurityGroupEditForm } from "@shared/components/organisms/InlineSecurityGroupEditForm";
import { InlineAddressPoolCreateForm } from "@shared/components/organisms/InlineAddressPoolCreateForm";
import { InlineAddressPoolEditForm } from "@shared/components/organisms/InlineAddressPoolEditForm";
import { InlineNetworkInterfaceEditForm } from "@shared/components/organisms/InlineNetworkInterfaceEditForm";
import { InlineNetworkInterfaceCreateForm } from "@shared/components/organisms/InlineNetworkInterfaceCreateForm";
import type { ResourceSpec } from "@shared/lib/resource-registry";

export interface InlineResourceFormProps {
  spec: ResourceSpec;
  action: "create" | "edit";
  /** Для edit: id ресурса (custom-формы грузят сами). */
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
  /** Контекст-id для кастомных форм. */
  networkId?: string;
  subnetId?: string;
  /** Заголовок generic-формы (по умолчанию «Создание/Редактирование: <singular>»). */
  title?: string;
  onCancel: () => void;
  onSuccess: () => void;
}

// Кастомные inline-формы, зарегистрированные app'ом на старте (напр. IAM-remote
// регистрирует Role create/edit). Так shared-диспетчер остаётся app-agnostic:
// доменная форма инжектится потребителем, а не хардкодится здесь. Ключ —
// `${specId}::${action}`; регистрация перекрывает generic/custom-ветку ниже.
export type InlineFormRenderer = (props: InlineResourceFormProps) => ReactNode;

const registeredInlineForms: Record<string, InlineFormRenderer> = {};

// registerInlineForm — подключает доменную inline-форму для (specId, action)
// (вызывается app'ом на старте, до открытия форм).
export function registerInlineForm(specId: string, action: "create" | "edit", render: InlineFormRenderer): void {
  registeredInlineForms[`${specId}::${action}`] = render;
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
    networkId,
    subnetId,
    title,
    onCancel,
    onSuccess,
  } = props;
  const specId = spec.id;

  // ── Доменные формы, зарегистрированные app'ом (напр. IAM Role) ──
  const registered = registeredInlineForms[`${specId}::${action}`];
  if (registered) return registered(props);

  // ── Custom resource-specific формы по spec.id ──
  if (specId === "subnets" && action === "create") {
    return (
      <InlineSubnetCreateForm projectId={projectId} networkId={networkId} onCancel={onCancel} onSuccess={onSuccess} />
    );
  }
  if (specId === "subnets" && action === "edit" && id) {
    return <InlineSubnetEditForm projectId={projectId} subnetId={id} onCancel={onCancel} />;
  }
  if (specId === "security-groups" && action === "edit" && id) {
    return <InlineSecurityGroupEditForm projectId={projectId} sgId={id} onCancel={onCancel} />;
  }
  if (specId === "address-pools" && action === "create") {
    return <InlineAddressPoolCreateForm onCancel={onCancel} onSuccess={onSuccess} />;
  }
  if (specId === "address-pools" && action === "edit" && id) {
    return <InlineAddressPoolEditForm poolId={id} onCancel={onCancel} onSuccess={onSuccess} />;
  }
  if (specId === "network-interfaces" && action === "edit" && id) {
    return (
      <InlineNetworkInterfaceEditForm projectId={projectId} nicId={id} onCancel={onCancel} onSuccess={onSuccess} />
    );
  }
  if (specId === "network-interfaces" && action === "create") {
    return (
      <InlineNetworkInterfaceCreateForm
        projectId={projectId}
        subnetId={subnetId}
        onCancel={onCancel}
        onSuccess={onSuccess}
      />
    );
  }

  // addresses + create в контексте subnet (subnetId задан) — preset обеих веток
  // internal_ipv4/v6_address_spec.subnet_id; editable _address_kind только
  // internal v4/v6 (external под subnet смысла не имеет).
  if (specId === "addresses" && action === "create" && subnetId) {
    return (
      <InlineResourceCreateForm
        spec={spec}
        ctx={{ projectId }}
        presetFields={{
          "internal_ipv4_address_spec.subnet_id": subnetId,
          "internal_ipv6_address_spec.subnet_id": subnetId,
        }}
        editablePresetFields={{ _address_kind: "internal" }}
        fieldOptionsFilter={{ _address_kind: ["internal", "internal_v6"] }}
        projectId={projectId}
        title="Резервирование IP-адреса"
        onCancel={onCancel}
        onSuccess={onSuccess}
      />
    );
  }

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
  if (action === "edit" && data) {
    return (
      <InlineResourceEditForm spec={spec} data={data} projectId={projectId} onCancel={onCancel} onSuccess={onSuccess} />
    );
  }
  return null;
}
