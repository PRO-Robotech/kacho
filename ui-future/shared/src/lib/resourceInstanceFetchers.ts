// resourceInstanceFetchers — отображение grantable-токена каталога
// `(module, resource)` → публичный per-object filtered List endpoint (через
// resource-registry), для resource_names real-instance picker'а (RBAC rules-model).
//
// Picker рендерится ТОЛЬКО когда у `(module,resource)` `has_list_endpoint=true` в
// каталоге И существует mapping ниже. Каталог — источник истины «есть ли публичный
// List»; этот map лишь связывает токен с конкретным REGISTRY-spec (apiPath/
// payloadKey/scope) уже-зарегистрированных публичных List<Resource>. Нет записи в
// map ИЛИ has_list_endpoint=false → free-text fallback (НИКОГДА Select, бэкенящийся
// несуществующим публичным List — security-инвариант).
//
// AddressPool/Condition (has_list_endpoint=false в каталоге) намеренно НЕ в map —
// их List только Internal-only (AddressPool) / не зарегистрирован на external
// (Condition). Даже будь они в map — каталожный флаг отрезал бы picker.

import { getResource, type ResourceSpec } from "@shared/lib/resource-registry";

// Токен каталога `<module>.<resource>` → id ресурса в REGISTRY. resource-токены —
// как в backend objectTypes (camelCase singular / loadbalancer plural).
const TOKEN_TO_REGISTRY_ID: Record<string, string> = {
  // ── vpc (per-object filtered public List) ──
  "vpc.network": "networks",
  "vpc.subnet": "subnets",
  "vpc.address": "addresses",
  "vpc.securityGroup": "security-groups",
  "vpc.routeTable": "route-tables",
  "vpc.gateway": "gateways",
  "vpc.networkInterface": "network-interfaces",
  // vpc.addressPool — НЕ здесь (Internal-only List, has_list_endpoint=false).

  // ── compute ──
  "compute.instance": "compute-instances",
  "compute.disk": "compute-disks",
  "compute.image": "compute-images",
  "compute.snapshot": "compute-snapshots",

  // ── loadbalancer (токены каталога — pluralized как в objectTypes) ──
  "loadbalancer.networkLoadBalancers": "load-balancers",
  "loadbalancer.targetGroups": "target-groups",
  "loadbalancer.listeners": "listeners",

  // ── iam ──
  "iam.role": "roles",
  "iam.serviceAccount": "service-accounts",
  "iam.group": "groups",
  "iam.user": "users",
  "iam.account": "accounts",
  "iam.project": "projects",
  // iam.condition — НЕ здесь (не зарегистрирован на external, has_list_endpoint=false).
  // iam.accessBinding — без простого id-named per-object List picker'а (custom RPC).
};

/** Описание fetcher'а инстансов для resource_names-picker. */
export interface InstanceFetcher {
  /** REGISTRY-spec (apiPath/payloadKey/scope/singular). */
  spec: ResourceSpec;
  /** Нужен ли project_id в List-запросе (scope=project). */
  needsProject: boolean;
  /** Нужен ли account_id (scope=account). */
  needsAccount: boolean;
}

/**
 * Возвращает fetcher для `(module, resource)`, если есть mapping на публичный
 * List<Resource>. undefined → у токена нет фетчера → free-text fallback. НЕ
 * проверяет has_list_endpoint — это решает вызывающий код (каталог = источник
 * истины «есть ли публичный List»); тут только связь токен → REGISTRY-spec.
 */
export function instanceFetcherFor(module: string, resource: string): InstanceFetcher | undefined {
  const registryId = TOKEN_TO_REGISTRY_ID[`${module}.${resource}`];
  if (!registryId) return undefined;
  const spec = getResource(registryId);
  if (!spec) return undefined;
  return {
    spec,
    needsProject: spec.scope === "project",
    needsAccount: spec.scope === "account",
  };
}
