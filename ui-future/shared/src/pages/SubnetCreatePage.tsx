// SubnetCreatePage — /vpc/subnets/create. KAC-231: форма-страница (не модалка):
// рендерит InlineSubnetCreateForm в контент-области (сайдбар сохраняется через
// Layout), единый panel-флоу с остальным VPC. networkId (из query) — preset +
// возврат в подсети сети.

import { Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMemo } from "react";
import { Typography } from "antd";
import { InlineSubnetCreateForm } from "@shared/components/organisms/InlineSubnetCreateForm";
import { useBreadcrumb } from "@shared/components/molecules/PageHeaderSlot";

export function SubnetCreatePage() {
  const { projectId } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const networkId = searchParams.get("networkId") ?? undefined;

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Virtual Private Cloud</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text type="secondary">Подсети</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Создание</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  if (!projectId) return <Navigate to="/" replace />;

  const back = networkId
    ? `/projects/${projectId}/vpc/networks/${networkId}/subnets`
    : `/projects/${projectId}/vpc/subnets`;

  return (
    <div style={{ maxWidth: 920 }}>
      <InlineSubnetCreateForm
        projectId={projectId}
        networkId={networkId}
        onCancel={() => navigate(back)}
        onSuccess={() => navigate(back)}
      />
    </div>
  );
}
