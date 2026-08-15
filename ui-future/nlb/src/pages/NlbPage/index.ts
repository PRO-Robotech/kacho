import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { NlbPage as NlbPageUnguarded } from "./NlbPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const NlbPage = withModuleBoundary(NlbPageUnguarded, "Network Load Balancer");
export default NlbPage;
export type { NlbPageProps } from "./NlbPage";
