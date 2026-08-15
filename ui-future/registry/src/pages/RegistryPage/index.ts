import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { RegistryPage as RegistryPageUnguarded } from "./RegistryPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const RegistryPage = withModuleBoundary(RegistryPageUnguarded, SERVICES.registry.menuTitle);
export default RegistryPage;
export type { RegistryPageProps } from "./RegistryPage";
