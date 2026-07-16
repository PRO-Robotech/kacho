declare module "storage/StoragePage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface StoragePageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const StoragePage: FC<StoragePageProps>;
  export default StoragePage;
  export { StoragePage };
}

declare module "storage/navigation" {
  export { DASHBOARD_NAVIGATION, default } from "dashboard/navigation";
}
