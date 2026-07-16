declare module "nlb/NlbPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface NlbPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const NlbPage: FC<NlbPageProps>;
  export default NlbPage;
  export { NlbPage };
}

declare module "nlb/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
