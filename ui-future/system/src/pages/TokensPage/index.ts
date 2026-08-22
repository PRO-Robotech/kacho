import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { TOKENS_SECTION_LABEL } from "@/labels";
import TokensPageUnguarded, { TokensRoutes } from "./TokensPage";

// Граница отказа СВОЕГО модуля (#371) — см. vpc/src/pages/VpcPage/index.ts.
// Имя части берётся из её единственного источника: экран отказа называет то же,
// что меню и рейл, — иначе продукт зовёт одно место двумя словами ровно тогда,
// когда человеку и так нечего понять.
export { TokensRoutes };
export const TokensPage = withModuleBoundary(TokensPageUnguarded, TOKENS_SECTION_LABEL);
export default TokensPage;
