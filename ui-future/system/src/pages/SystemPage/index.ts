import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import SystemPageUnguarded, { SystemRoutes } from "./SystemPage";

// Граница отказа СВОЕГО модуля (#371) — см. vpc/src/pages/VpcPage/index.ts.
export { SystemRoutes };
export const SystemPage = withModuleBoundary(SystemPageUnguarded, SERVICES.system.menuTitle);
export default SystemPage;
