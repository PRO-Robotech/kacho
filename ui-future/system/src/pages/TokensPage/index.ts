import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import TokensPageUnguarded, { TokensRoutes } from "./TokensPage";

// Граница отказа СВОЕГО модуля (#371) — см. vpc/src/pages/VpcPage/index.ts.
export { TokensRoutes };
export const TokensPage = withModuleBoundary(TokensPageUnguarded, "Токены и ключи");
export default TokensPage;
