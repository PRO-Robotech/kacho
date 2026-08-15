import type { FC } from "react";
import { Button, Empty, Typography } from "antd";
import { useNavigate, useParams } from "react-router";
// Заголовок раздела — из того же зеркала канона, что и крошка хоста: своя
// карта здесь была третьей подписью одного раздела в одном продукте.
import { SERVICES } from "../../lib/entity-names";

export const ModulePlaceholderPage: FC = () => {
  const navigate = useNavigate();
  const params = useParams();
  const moduleKey = params.moduleKey ?? params.iamSection ?? params.systemSection ?? "module";
  const label = SERVICES[moduleKey]?.menuTitle ?? moduleKey;

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
