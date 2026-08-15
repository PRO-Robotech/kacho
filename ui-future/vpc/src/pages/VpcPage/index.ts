import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { VpcPage as VpcPageUnguarded } from "./VpcPage";

// Граница отказа СВОЕГО модуля (#371): host оборачивает удалённую страницу
// своей границей, но у модуля есть и собственная точка входа
// (standalone-разработка), где host-границы нет вовсе. Имя раздела — то же, что
// в меню консоли: пользователь читает одно имя в обоих местах.
export const VpcPage = withModuleBoundary(VpcPageUnguarded, "Virtual Private Cloud");
export default VpcPage;
export type { VpcPageProps } from "./VpcPage";
