import type { FC } from "react";
import { Button, Empty, Typography } from "antd";
import { useNavigate, useParams } from "react-router-dom";

const MODULE_LABELS: Record<string, string> = {
  vpc: "Virtual Private Cloud",
  compute: "Compute Cloud",
  nlb: "Network Load Balancer",
  iam: "Identity and Access Management",
  system: "Администрирование",
};

export const ModulePlaceholderPage: FC = () => {
  const navigate = useNavigate();
  const params = useParams();
  const moduleKey = params.moduleKey ?? params.iamSection ?? params.systemSection ?? "module";
  const label = MODULE_LABELS[moduleKey] ?? moduleKey;

  return (
    <section className="workbench" data-testid="module-placeholder-page">
      <Empty
        description={
          <>
            <Typography.Text strong>{label}</Typography.Text>
            <br />
            <Typography.Text type="secondary">
              Route is registered in the host. Remote page implementation is next.
            </Typography.Text>
          </>
        }
      >
        <Button type="primary" onClick={() => navigate("/dashboard")}>
          Все сервисы
        </Button>
      </Empty>
    </section>
  );
};
