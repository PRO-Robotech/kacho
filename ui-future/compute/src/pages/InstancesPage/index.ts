import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { InstancesPage as InstancesPageUnguarded } from "./InstancesPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const InstancesPage = withModuleBoundary(InstancesPageUnguarded, SERVICES.compute.menuTitle);
export default InstancesPage;
export type { InstancesPageProps } from "./InstancesPage";
