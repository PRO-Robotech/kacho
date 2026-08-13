// Подмаршрут операций ресурса — `GET <apiPath>/{id}/operations`.
//
// Он есть НЕ у всякого ресурса. Каталожные и админские ресурсы (типы дисков и
// машин, каталог размещения geo, пулы адресов) такого связывания в стволе не
// объявляют вовсе, и вкладка «Операции», собранная из `apiPath` любой спеки,
// адресовала бы у них то, чего нет: край отвечает отказом, а оператор читает это
// как поломку, а не как «у ресурса нет операций».
//
// Поэтому путь берётся ЗДЕСЬ, из перечня, а не собирается на месте вызова.
// Перечень — единственное место, где консоль утверждает, у кого подмаршрут есть,
// и он сверяется с `google.api.http` связываниями ствола в обе стороны пробой
// `operations-subroute.test.ts`: лишняя запись и недостающая одинаково находки.
//
// Записи — ЦЕЛЬНЫЕ пути с подстановкой `{id}`, а не «apiPath + суффикс»: в такой
// форме их читает проба поверхности (`api-path-surface.test.ts`), сверяющая
// каждый путь shared с деревом proto. Собранный на месте вызова путь для неё не
// существует — именно так подмаршрут у пяти адресов и прожил незамеченным.

/** Ресурсы, у которых ствол несёт `GET <base>/{id}/operations`. */
export const OPERATIONS_LIST_PATHS = [
  "/compute/v1/guestAccessKeys/{id}/operations",
  "/compute/v1/instances/{id}/operations",
  "/compute/v1/placementGroups/{id}/operations",
  "/iam/v1/accessBindings/{id}/operations",
  "/iam/v1/accounts/{id}/operations",
  "/iam/v1/groups/{id}/operations",
  "/iam/v1/projects/{id}/operations",
  "/iam/v1/roles/{id}/operations",
  "/iam/v1/serviceAccounts/{id}/operations",
  "/iam/v1/users/{id}/operations",
  "/nlb/v1/listeners/{id}/operations",
  "/nlb/v1/networkLoadBalancers/{id}/operations",
  "/nlb/v1/targetGroups/{id}/operations",
  "/registry/v1/registries/{id}/operations",
  "/storage/v1/images/{id}/operations",
  "/storage/v1/snapshots/{id}/operations",
  "/storage/v1/volumes/{id}/operations",
  "/vpc/v1/addresses/{id}/operations",
  "/vpc/v1/gateways/{id}/operations",
  "/vpc/v1/networkInterfaces/{id}/operations",
  "/vpc/v1/networks/{id}/operations",
  "/vpc/v1/routeTables/{id}/operations",
  "/vpc/v1/securityGroups/{id}/operations",
  "/vpc/v1/subnets/{id}/operations",
] as const;

const SUFFIX = "/{id}/operations";

/** apiPath ресурса → его подмаршрут операций. Ключ — ЦЕЛЫЙ apiPath, не префикс. */
const BY_API_PATH = new Map<string, string>(
  OPERATIONS_LIST_PATHS.map((p) => [p.slice(0, p.length - SUFFIX.length), p]),
);

/** Несёт ли ствол подмаршрут операций для ресурса с таким `apiPath`. */
export function hasOperationsSubroute(apiPath: string): boolean {
  return BY_API_PATH.has(apiPath);
}

/**
 * Путь списка операций ресурса — либо путь ствола, либо `null`.
 *
 * `null` — это ответ «у ресурса такого подмаршрута нет», а не «я не смог»:
 * вызывающий обязан не показывать вкладку, а не подставлять запасной адрес.
 */
export function operationsListPath(apiPath: string, resourceId: string): string | null {
  const template = BY_API_PATH.get(apiPath);
  return template === undefined ? null : template.replace("{id}", resourceId);
}
