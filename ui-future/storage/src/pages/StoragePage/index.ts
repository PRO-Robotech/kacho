import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { SERVICES } from "@shared/lib/entity-names";
import { StoragePage as StoragePageUnguarded } from "./StoragePage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const StoragePage = withModuleBoundary(StoragePageUnguarded, SERVICES.storage.menuTitle);
export default StoragePage;
export type { StoragePageProps } from "./StoragePage";
