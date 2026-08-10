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
// AddressPool (has_list_endpoint=false в каталоге) намеренно НЕ в map — его List
// только Internal-only. Даже будь он в map — каталожный флаг отрезал бы picker.
//
// Рядом стояло такое же исключение для Condition. Исключать больше нечего:
// токена `iam.condition` нет в закрытой таблице grantable-типов ствола, ресурс
// снят целиком (в proto iam нет ни condition.proto, ни ConditionService), и
// маршрутов /iam/v1/conditions на поверхности не осталось. Исключение, у
// которого нет предмета, — находка: следующий читатель принимает его за
// описание действительности.

import { getResource, type ResourceSpec } from "@shared/lib/resource-registry";

// Токен каталога `<module>.<resource>` → id ресурса в REGISTRY. resource-токены —
// как в backend objectTypes (camelCase singular / loadbalancer plural).
export const TOKEN_TO_REGISTRY_ID: Record<string, string> = {
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
  // iam.accessBinding — без простого id-named per-object List picker'а (custom RPC).

  // НЕ покрыты, и это пробел, а не решение: storage.volumes / storage.snapshots /
  // storage.images и registry.registries / registry.repositories — grantable-токены
  // ствола, у которых публичный List есть, а записи здесь нет, поэтому picker
  // падает в free-text. Заводить их надо вместе со спеками этих ресурсов в
  // REGISTRY, иначе getResource вернёт undefined и запись не сработает.
  // geo.regions / geo.zones в закрытой таблице grantable-типов ствола ОТСУТСТВУЮТ —
  // им записи не полагается.
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
