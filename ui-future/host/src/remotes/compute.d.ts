declare module "compute/InstancesPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface InstancesPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const InstancesPage: FC<InstancesPageProps>;
  export default InstancesPage;
  export { InstancesPage };
}

declare module "compute/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
