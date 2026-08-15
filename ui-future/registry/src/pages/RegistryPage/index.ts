import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { RegistryPage as RegistryPageUnguarded } from "./RegistryPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const RegistryPage = withModuleBoundary(RegistryPageUnguarded, "Container Registry");
export default RegistryPage;
export type { RegistryPageProps } from "./RegistryPage";
