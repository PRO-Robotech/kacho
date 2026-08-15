import { withModuleBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { StoragePage as StoragePageUnguarded } from "./StoragePage";

// Граница отказа СВОЕГО модуля (#371) — см. VpcPage/index.ts.
export const StoragePage = withModuleBoundary(StoragePageUnguarded, "Storage");
export default StoragePage;
export type { StoragePageProps } from "./StoragePage";
