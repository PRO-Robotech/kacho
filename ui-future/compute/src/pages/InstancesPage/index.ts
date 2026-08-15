import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { InstancesPage as InstancesPageUnguarded } from "./InstancesPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const InstancesPage = withModuleBoundary(InstancesPageUnguarded, "Compute Cloud");
export default InstancesPage;
export type { InstancesPageProps } from "./InstancesPage";
