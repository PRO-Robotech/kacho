// NetworkInterfaceCreatePage — /vpc/network-interfaces/create. Standalone-страница
// под кастомную InlineNetworkInterfaceCreateForm (как SubnetCreatePage для
// подсети) → формат create == edit (InlineNetworkInterfaceEditForm), SG/адреса
// через ResourceRefChips. Раньше standalone-create шёл через generic
// ResourceCreatePage → расхождение формата + SG.

import { Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMemo } from "react";
import { Typography } from "antd";
import { InlineNetworkInterfaceCreateForm } from "@shared/components/organisms/InlineNetworkInterfaceCreateForm";
import { useBreadcrumb } from "@shared/components/molecules/PageHeaderSlot";

export function NetworkInterfaceCreatePage() {
  const { projectId } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const subnetId = searchParams.get("subnetId") ?? searchParams.get("subnet_id") ?? undefined;

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Virtual Private Cloud</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text type="secondary">Сетевые интерфейсы</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Создание</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  if (!projectId) return <Navigate to="/" replace />;

  const back = `/projects/${projectId}/vpc/network-interfaces`;

  return (
    <div style={{ maxWidth: 920 }}>
      <InlineNetworkInterfaceCreateForm
        projectId={projectId}
        subnetId={subnetId}
        onCancel={() => navigate(back)}
        onSuccess={() => navigate(back)}
      />
    </div>
  );
}
