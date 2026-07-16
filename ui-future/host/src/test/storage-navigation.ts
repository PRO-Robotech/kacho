import type { RemoteNavSection } from "dashboard/navigation";

// Тест-стаб storage/navigation для host-jest. ПУСТОЙ намеренно: rail-ассерты берут
// контент секций из агрегатного dashboard-navigation стаба, а этот стаб нужен
// ТОЛЬКО чтобы HostRail's Promise.allSettled(import("storage/navigation")) РЕЗОЛВИЛСЯ
// под jest. Без стаба незастабленный bare-specifier динамический import ВИСНЕТ на
// CI-ранере (--experimental-vm-modules never settles → allSettled не резолвится →
// setSections не вызывается → рендер shell'а виснет); локально reject'ится быстро.
// Виновники: App/HostShell/HostRail (все рендерят HostRail). См. PRO-Robotech/kacho#7.
export const STORAGE_NAVIGATION: RemoteNavSection[] = [];
export const DASHBOARD_NAVIGATION = STORAGE_NAVIGATION;
export default STORAGE_NAVIGATION;
