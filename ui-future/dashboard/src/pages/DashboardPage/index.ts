import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { DashboardPage as DashboardPageUnguarded } from "./DashboardPage";

// Граница отказа СВОЕГО модуля (#371) — см. vpc/src/pages/VpcPage/index.ts.
export const DashboardPage = withModuleBoundary(DashboardPageUnguarded, "Все сервисы");
export default DashboardPage;
export type { DashboardPageProps } from "./DashboardPage";
