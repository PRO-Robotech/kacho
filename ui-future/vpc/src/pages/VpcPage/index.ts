import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { VpcPage as VpcPageUnguarded } from "./VpcPage";

// Граница отказа СВОЕГО модуля (#371): host оборачивает удалённую страницу
// своей границей, но у модуля есть и собственная точка входа
// (standalone-разработка), где host-границы нет вовсе. Имя раздела — то же, что
// в меню консоли: пользователь читает одно имя в обоих местах. Поэтому оно и
// БЕРЁТСЯ из меню — из единственного источника `@shared/lib/entity-names`, а не
// выписывается литералом рядом: литерал совпадал бы с меню ровно до первой
// правки имени, и разошёлся бы молча.
export const VpcPage = withModuleBoundary(VpcPageUnguarded, SERVICES.vpc.menuTitle);
export default VpcPage;
export type { VpcPageProps } from "./VpcPage";
