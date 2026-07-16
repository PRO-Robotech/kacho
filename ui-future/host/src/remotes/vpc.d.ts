declare module "vpc/VpcPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface VpcPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const VpcPage: FC<VpcPageProps>;
  export default VpcPage;
  export { VpcPage };
}

declare module "vpc/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
