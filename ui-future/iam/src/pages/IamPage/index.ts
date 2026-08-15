import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { IamPage as IamPageUnguarded } from "./IamPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const IamPage = withModuleBoundary(IamPageUnguarded, SERVICES.iam.menuTitle);
export default IamPage;
