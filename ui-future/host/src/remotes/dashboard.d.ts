declare module "dashboard/DashboardPage" {
  import type { FC } from "react";
  import type { HostContext } from "../utils";

  export interface DashboardPageProps {
    context?: HostContext;
    navigate?: (path: string) => void | Promise<void>;
  }

  const DashboardPage: FC<DashboardPageProps>;
  export default DashboardPage;
  export { DashboardPage };
}

declare module "dashboard/navigation" {
  export type RemoteIconName =
    | "activity"
    | "cable"
    | "camera"
    | "cloud"
    | "folder"
    | "git-branch"
    | "globe"
    | "hard-drive"
    | "key"
    | "layers"
    | "lock"
    | "network"
    | "route"
    | "scale"
    | "server"
    | "shield"
    | "users";

  export interface RemoteNavItem {
    key: string;
    icon: RemoteIconName;
    label: string;
    path: string;
    requiresProject?: boolean;
  }

  export interface RemoteNavSection {
    key: string;
    segment: string;
    icon: RemoteIconName;
    label: string;
    landingPath: string;
    requiresProject?: boolean;
    items: RemoteNavItem[];
  }

  export const DASHBOARD_NAVIGATION: RemoteNavSection[];
}
