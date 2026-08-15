import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { NlbPage as NlbPageUnguarded } from "./NlbPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const NlbPage = withModuleBoundary(NlbPageUnguarded, SERVICES.nlb.menuTitle);
export default NlbPage;
export type { NlbPageProps } from "./NlbPage";
