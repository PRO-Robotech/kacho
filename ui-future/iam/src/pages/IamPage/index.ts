import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { IamPage as IamPageUnguarded } from "./IamPage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const IamPage = withModuleBoundary(IamPageUnguarded, "Identity and Access Management");
export default IamPage;
