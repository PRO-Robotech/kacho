// Идентификаторы спек общего реестра, разделы которых монтирует это приложение.
//
// Вынесено из App.tsx не ради опрятности: маршруты строятся как
// `IDS.map((id) => REGISTRY[id]).filter(Boolean)`, и несуществующий id этот
// `filter` выбрасывает МОЛЧА — раздел просто не появляется. Отличить «спеки нет,
// поэтому раздела нет» от «раздел и не задумывался» по дереву нельзя: и то и
// другое выглядит как пустая ветка роутера. Список, лежащий отдельным модулем,
// становится предметом проверки (scoped-resources.test.ts), и удаление спеки из
// общего реестра перестаёт быть беззвучным.

/** VPC-ресурсы под /projects/:projectId/vpc/<route>. */
export const VPC_SCOPED_IDS = [
  "networks",
  "subnets",
  "addresses",
  "route-tables",
  "security-groups",
  "network-interfaces",
  "gateways",
  "cidr-groups",
] as const;

/**
 * Compute-ресурсы под /projects/:projectId/compute/<route>.
 *
 * Только инстанс: блочное хранение — домен storage (Volume/Image/Snapshot/
 * DiskType на /storage/v1/*), у него свой remote и свой раздел консоли.
 * Маршрутов /compute/v1/{disks,images,snapshots} в стволе нет.
 */
export const COMPUTE_SCOPED_IDS = ["compute-instances", "placement-groups"] as const;

/** NLB-ресурсы под /projects/:projectId/nlb/<route>. */
export const NLB_SCOPED_IDS = ["load-balancers", "listeners", "target-groups"] as const;

/** Глобальные (admin-only) ресурсы под /system/<route>. */
export const SYSTEM_SCOPED_IDS = ["regions", "zones", "address-pools"] as const;

/** Все id, которые это приложение резолвит в общем реестре. */
export const ALL_SCOPED_IDS: readonly string[] = [
  ...VPC_SCOPED_IDS,
  ...COMPUTE_SCOPED_IDS,
  ...NLB_SCOPED_IDS,
  ...SYSTEM_SCOPED_IDS,
  "projects",
];
