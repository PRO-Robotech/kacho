declare module "system/SystemPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface SystemPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const SystemPage: FC<SystemPageProps>;
  export default SystemPage;
  export { SystemPage };
}

declare module "system/TokensPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface TokensPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const TokensPage: FC<TokensPageProps>;
  export default TokensPage;
  export { TokensPage };
}

declare module "system/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
