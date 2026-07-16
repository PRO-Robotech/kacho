import type { FC } from "react";
import type { HostContext } from "../utils";

export interface DashboardPageProps {
  context?: HostContext;
}

export const DashboardPage: FC<DashboardPageProps> = ({ context }) => {
  const label = context?.project?.name || context?.project?.id || "Host shell for future federated modules";
  return (
    <section data-testid="dashboard-remote">
      <h3>Сервисы облака</h3>
      <span>{label}</span>
    </section>
  );
};

export default DashboardPage;
