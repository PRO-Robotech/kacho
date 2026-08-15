// Домен-владелец ресурса и сборка SPA-адреса его списка — ОДНА реализация на
// всё дерево консоли.
//
// Почему отдельным файлом, а не внутри `resource-registry`. Реестр у каждого
// модуля СВОЙ: в нём лежат ровно те ресурсы, которые модуль показывает, вместе
// с колонками, формами и React-отрисовкой. Правило же «какому домену
// принадлежит ресурс и каким адресом он открывается» от состава реестра не
// зависит вовсе — это чистая функция идентификатора. Держать её в реестре
// значило бы: либо тянуть весь чужой реестр (со всеми его компонентами) ради
// одной функции, либо заводить пятую копию правила. Копии и завелись — и
// разошлись: три из пяти не знали про cluster-scoped каталог `/system/*`, а
// одна вместо домена владельца подставляла имя своего модуля.
//
// Соответствие сегментов маршрутам хоста — `host/src/App.tsx`.

/** Сегмент маршрута хоста — по одному на домен-владелец. */
export type ServicePrefix = "vpc" | "compute" | "storage" | "nlb" | "registry" | "iam";

/**
 * Домен-ВЛАДЕЛЕЦ ресурса по идентификатору его спеки.
 *
 * Владелец, а не смотрящий: том принадлежит storage и тогда, когда ссылка на
 * него стоит на карточке машины. Модуль обслуживает только свои маршруты,
 * поэтому чужой ресурс, адресованный сегментом смотрящего, попадает в его
 * catch-all — переход выглядит рабочим и никуда не ведёт.
 *
 * Неизвестный идентификатор отвечает `null` — «домен не назван», и вызывающий
 * ссылку не рисует (правило 5 канона консоли: без адреса ссылки нет).
 * Корзины «прочее» здесь нет намеренно: прежняя редакция сваливала неизвестное
 * в `vpc`, из-за чего ресурс не-VPC домена, забытый в перечне, получал не
 * отсутствие ссылки, а ссылку в чужой домен — тот же неработающий переход,
 * только менее заметный. Полноту перечня держит перепись реестра: проба
 * `resource-registry.owner-domain` у каждого модуля требует не-null от КАЖДОГО
 * идентификатора своего REGISTRY.
 */
export function resourceServicePrefix(specId: string): ServicePrefix | null {
  if (specId.startsWith("compute-")) return "compute";
  switch (specId) {
    // VPC
    case "networks":
    case "subnets":
    case "addresses":
    case "route-tables":
    case "security-groups":
    case "network-interfaces":
    case "gateways":
    // Именованный набор префиксов — ресурс vpc; на него ссылается правило
    // группы безопасности третьей ветвью цели.
    case "cidr-groups":
      return "vpc";
    // Compute
    case "machine-types":
    // Группа размещения — ресурс compute, но её идентификатор спеки префикса
    // `compute-` не несёт: раздел монтируют оба приложения по одному адресу,
    // и переименование спеки ради предиката сломало бы ссылки на карточку.
    case "placement-groups":
    // Ключ доступа в гостевую систему — тот же случай.
    case "guest-access-keys":
      return "compute";
    // Storage — блочное хранение
    case "volumes":
    case "snapshots":
    case "images":
    case "disk-types":
      return "storage";
    // NLB
    case "network-load-balancers":
    case "load-balancers":
    case "listeners":
    case "target-groups":
      return "nlb";
    // Registry — OCI
    case "registries":
    case "repositories":
    case "tags":
      return "registry";
    // IAM — пути под /iam/<route>, не под /projects/
    case "accounts":
    case "projects":
    case "users":
    case "service-accounts":
    case "groups":
    case "roles":
    case "access-bindings":
      return "iam";
    // Cluster-scoped админ-каталог: project-путей у него нет, адрес строит
    // ветка /system/* ниже. Сегмент назван для полноты перечня.
    case "regions":
    case "zones":
    case "address-pools":
      return "compute";
    default:
      return null;
  }
}

/** Cluster-scoped админ-ресурсы, живущие под /system/*, а не внутри проекта. */
const SYSTEM_SCOPED: ReadonlySet<string> = new Set(["regions", "zones", "address-pools"]);

/** Живёт ли ресурс в cluster-scoped каталоге `/system/*`. */
export function isSystemScopedResource(specId: string): boolean {
  return SYSTEM_SCOPED.has(specId);
}

/**
 * SPA-адрес списка ресурса: идентификатор спеки + её маршрут + проект.
 *
 * Маршрут передаётся вызывающим, потому что он живёт в реестре модуля, а
 * правило сборки — здесь. `null` означает, что адреса нет: домен не назван,
 * ресурс живёт вне проекта (IAM), либо проект в контексте неизвестен.
 */
export function resourceListPath(
  specId: string,
  route: string,
  projectId: string | null | undefined,
): string | null {
  // Проверка ДО требования projectId: у глобального каталога измерения
  // «проект» нет вовсе, и требовать его значило бы не строить ссылку там, где
  // проекта в контексте нет (страницы /system/*).
  if (isSystemScopedResource(specId)) return `/system/${route}`;
  const prefix = resourceServicePrefix(specId);
  if (prefix === null) return null;
  if (prefix === "iam") return null;
  if (!projectId) return null;
  return `/projects/${projectId}/${prefix}/${route}`;
}
