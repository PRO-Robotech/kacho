declare module "registry/RegistryPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface RegistryPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const RegistryPage: FC<RegistryPageProps>;
  export default RegistryPage;
  export { RegistryPage };
}

declare module "registry/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
