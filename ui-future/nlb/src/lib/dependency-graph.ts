// dependency-graph — построение дерева «что подвязано к ресурсу» для confirm-модалки
// удаления. Generic-механизм: per registry-id резолвер, который через REST собирает
// зависимые ресурсы (рекурсивно), помечая, какие из них блокируют удаление родителя.
//
// Модель зависимостей (kacho-vpc):
//   NIC → (blocks) → Subnet → (blocks) → Network
//   NIC → (blocks) → Address
//   NIC-attached-to-instance: instance → (blocks) → NIC
// Раскрытие:
//   networks  → subnets (RESTRICT, blocks) [рекурсивно → addresses · network-interfaces]
//              · route-tables (RESTRICT, blocks)
//              · security-groups (RESTRICT, blocks; кроме default-SG — авто-удаляется
//                при Network.Delete → blocks=false)
//   subnets   → addresses (RESTRICT, blocks)
//              · network-interfaces (RESTRICT, blocks): NIC всегда блокирует удаление
//                своей подсети — сначала удали NIC'и.
//   addresses → network-interfaces, которые ссылаются на этот адрес в
//                v4_address_ids/v6_address_ids (Address.Delete → FailedPrecondition
//                «address is in use by network interface …» → blocks=true).
//   network-interfaces → инстанс, к которому NIC приаттачен (used_by) — NIC.Delete его
//                не пустит → blocks=true.

import { api } from "@/api/client";
import { REGISTRY } from "@/lib/resource-registry";

export interface DepNode {
  /** Уникальный ключ для antd Tree. */
  key: string;
  /** registry id (например "subnets"). */
  resourceId: string;
  id: string;
  name: string;
  /** project id ресурса — для построения ссылки. */
  projectId: string;
  /** URL-сегмент под /projects/:projectId/ (например "vpc/subnets"). */
  routeSegment: string;
  /** Блокирует удаление родителя? */
  blocks: boolean;
  children: DepNode[];
}

type AnyRec = Record<string, any>;

async function listAll(apiPath: string, payloadKey: string, query?: Record<string, string>): Promise<AnyRec[]> {
  const r = await api.list<Record<string, AnyRec[]>>(apiPath, { pageSize: "1000", ...(query ?? {}) });
  return r?.[payloadKey] ?? [];
}

function routeSegmentFor(resourceId: string): string {
  const spec = REGISTRY[resourceId];
  const route = spec?.route ?? resourceId;
  return resourceId.startsWith("compute-") ? `compute/${route}` : `vpc/${route}`;
}

function mkNode(resourceId: string, r: AnyRec, blocks: boolean, children: DepNode[] = []): DepNode {
  return {
    key: `${resourceId}:${r.id}`,
    resourceId,
    id: String(r.id),
    name: (r.name as string) || String(r.id),
    projectId: (r.project_id as string) || "",
    routeSegment: routeSegmentFor(resourceId),
    blocks,
    children,
  };
}

/** id инстанса, к которому приаттачен NIC (used_by referrer), либо "". */
function nicAttachedInstanceId(ni: AnyRec): string {
  const ref = ni?.used_by?.referrer;
  return ref?.id ? String(ref.id) : "";
}

/** Дети подсети: internal-Address'ы (RESTRICT) и NetworkInterface'ы (RESTRICT). */
async function subnetChildren(subnetId: string, projectId: string): Promise<DepNode[]> {
  if (!projectId) return [];
  const [addrs, nics] = await Promise.all([
    listAll("/vpc/v1/addresses", "addresses", { project_id: projectId }),
    listAll("/vpc/v1/networkInterfaces", "network_interfaces", { project_id: projectId }),
  ]);
  const out: DepNode[] = [];
  for (const a of addrs) {
    const sid = a.internal_ipv4_address?.subnet_id ?? a.internal_ipv6_address?.subnet_id;
    if (sid === subnetId) out.push(mkNode("addresses", a, true));
  }
  for (const ni of nics) {
    if (ni.subnet_id !== subnetId) continue;
    // FK network_interfaces.subnet_id = ON DELETE RESTRICT: NIC всегда блокирует
    // удаление своей подсети (независимо от того, приаттачен ли он к инстансу).
    out.push(mkNode("network-interfaces", ni, true));
  }
  return out;
}

/** NIC'и, ссылающиеся на адрес в v4_address_ids / v6_address_ids. */
async function addressDependents(addressId: string, projectId: string): Promise<DepNode[]> {
  if (!projectId) return [];
  const nics = await listAll("/vpc/v1/networkInterfaces", "network_interfaces", { project_id: projectId });
  const out: DepNode[] = [];
  for (const ni of nics) {
    const v4: string[] = ni.v4_address_ids ?? [];
    const v6: string[] = ni.v6_address_ids ?? [];
    if (v4.includes(addressId) || v6.includes(addressId)) out.push(mkNode("network-interfaces", ni, true));
  }
  return out;
}

/** Есть ли резолвер зависимостей для этого registry-id. */
export function hasDependencyResolver(resourceId: string): boolean {
  return (
    resourceId === "networks" ||
    resourceId === "subnets" ||
    resourceId === "addresses" ||
    resourceId === "network-interfaces"
  );
}

/** Собрать дерево зависимостей ресурса. `resource` — минимум {id, project_id}. */
export async function loadDependents(
  resourceId: string,
  resource: { id: string; project_id?: string | null },
): Promise<DepNode[]> {
  const projectId = resource.project_id ?? "";

  if (resourceId === "networks") {
    const [subnets, routeTables, sgs] = await Promise.all([
      listAll(`/vpc/v1/networks/${resource.id}/subnets`, "subnets"),
      listAll(`/vpc/v1/networks/${resource.id}/route_tables`, "route_tables"),
      listAll(`/vpc/v1/networks/${resource.id}/security_groups`, "security_groups"),
    ]);
    const out: DepNode[] = [];
    for (const s of subnets) {
      const kids = await subnetChildren(String(s.id), (s.project_id as string) || projectId);
      out.push(mkNode("subnets", s, true, kids));
    }
    for (const rt of routeTables) out.push(mkNode("route-tables", rt, true));
    for (const sg of sgs) {
      const isDefault = !!sg.default_for_network;
      out.push(
        mkNode(
          "security-groups",
          { ...sg, name: (isDefault ? "default · " : "") + ((sg.name as string) || sg.id) },
          !isDefault,
        ),
      );
    }
    return out;
  }

  if (resourceId === "subnets") {
    return subnetChildren(resource.id, projectId);
  }

  if (resourceId === "addresses") {
    return addressDependents(resource.id, projectId);
  }

  if (resourceId === "network-interfaces") {
    // NIC, приаттаченный к инстансу, нельзя удалить (NIC.Delete → FailedPrecondition).
    // Загружаем сам NIC, чтобы узнать used_by.
    let ni: AnyRec | null = null;
    try {
      ni = await api.get<AnyRec>(`/vpc/v1/networkInterfaces/${resource.id}`);
    } catch {
      ni = null;
    }
    const instId = ni ? nicAttachedInstanceId(ni) : "";
    if (!instId) return [];
    return [
      mkNode(
        "compute-instances",
        { id: instId, name: instId, project_id: (ni?.project_id as string) || projectId },
        true,
      ),
    ];
  }

  return [];
}

/** Все блокирующие узлы дерева (рекурсивно) — для предупреждения «сначала удалите …». */
export function blockingNodes(nodes: DepNode[]): DepNode[] {
  const acc: DepNode[] = [];
  const walk = (ns: DepNode[]) => {
    for (const n of ns) {
      if (n.blocks) acc.push(n);
      walk(n.children);
    }
  };
  walk(nodes);
  return acc;
}
