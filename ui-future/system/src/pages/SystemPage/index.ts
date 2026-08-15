import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import SystemPageUnguarded, { SystemRoutes } from "./SystemPage";

// Граница отказа СВОЕГО модуля (#371) — см. vpc/src/pages/VpcPage/index.ts.
export { SystemRoutes };
export const SystemPage = withModuleBoundary(SystemPageUnguarded, "Администрирование");
export default SystemPage;
