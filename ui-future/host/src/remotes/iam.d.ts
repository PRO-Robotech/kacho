declare module "iam/IamPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface IamPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const IamPage: FC<IamPageProps>;
  export default IamPage;
  export { IamPage };
}

declare module "iam/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
