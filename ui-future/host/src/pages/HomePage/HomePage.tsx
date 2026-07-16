import type { FC } from "react";
import { Typography } from "antd";

export const HomePage: FC = () => {
  return (
    <section className="workbench">
      <div className="panel-heading">
        <div>
          <Typography.Title level={3}>Сервисы облака</Typography.Title>
          <Typography.Text type="secondary">Host shell for future federated modules</Typography.Text>
        </div>
      </div>
    </section>
  );
};
