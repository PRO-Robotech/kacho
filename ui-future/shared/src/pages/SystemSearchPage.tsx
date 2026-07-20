// SystemSearchPage — admin-search по resource ID и имени клиента.
// Запрашивает list endpoints всех ресурсов параллельно, фильтрует client-side
// substring match. Прорастает project/account breadcrumbs для каждого хита.

import { useState, useMemo } from "react";
import { Link } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api } from "@shared/api/client";

interface Hit {
  resource: string;
  id: string;
  name: string;
  project_id?: string;
  account_id?: string;
  link: string;
  extras?: Record<string, string>;
}

const DOMAINS = [
  // KAC-124: Resource Manager (orgs/clouds/folders) → IAM (accounts/projects).
  { resource: "accounts", path: "/iam/v1/accounts", key: "accounts", linkBase: "/iam/accounts" },
  { resource: "projects", path: "/iam/v1/projects", key: "projects", linkBase: "/projects/:id" },
  {
    resource: "networks",
    path: "/vpc/v1/networks",
    key: "networks",
    linkBase: "/projects/:project_id/vpc/networks/:id",
  },
  { resource: "subnets", path: "/vpc/v1/subnets", key: "subnets", linkBase: "/projects/:project_id/vpc/subnets/:id" },
  {
    resource: "addresses",
    path: "/vpc/v1/addresses",
    key: "addresses",
    linkBase: "/projects/:project_id/vpc/addresses/:id",
  },
  {
    resource: "network-interfaces",
    path: "/vpc/v1/networkInterfaces",
    key: "network_interfaces",
    linkBase: "/projects/:project_id/vpc/network-interfaces/:id",
  },
  { resource: "address-pools", path: "/vpc/v1/addressPools", key: "pools", linkBase: "/system/address-pools/:id" },
  { resource: "regions", path: "/compute/v1/regions", key: "regions", linkBase: "/system/regions/:id" },
  { resource: "zones", path: "/compute/v1/zones", key: "zones", linkBase: "/system/zones/:id" },
];

// ВАЖНО: VPC list endpoints (networks/subnets/addresses) обычно требуют projectId,
// но в нашем bекенде они работают и без него (cross-project, вернут все).
// Тогда client-side filter сделает остальное.

export function SystemSearchPage() {
  const [q, setQ] = useState("");

  const queries = useQueries({
    queries: DOMAINS.map((d) => ({
      queryKey: ["search", d.resource],
      queryFn: () => api.list<Record<string, unknown>>(d.path, { pageSize: "500" }),
      staleTime: 10_000,
    })),
  });

  const hits: Hit[] = useMemo(() => {
    const term = q.trim().toLowerCase();
    if (!term) return [];

    const out: Hit[] = [];
    queries.forEach((qry, i) => {
      const d = DOMAINS[i];
      if (!qry.data) return;
      const list = (qry.data[d.key] as Record<string, unknown>[] | undefined) ?? [];
      list.forEach((r) => {
        const id = String(r.id ?? "");
        const name = String(r.name ?? "");
        const matchesId = id.toLowerCase().includes(term);
        const matchesName = name.toLowerCase().includes(term);
        if (!matchesId && !matchesName) return;
        out.push({
          resource: d.resource,
          id,
          name,
          project_id: r.project_id as string | undefined,
          account_id: r.account_id as string | undefined,
          link: d.linkBase.replace(":project_id", String(r.project_id ?? "")).replace(":id", id),
          extras: extractExtras(d.resource, r),
        });
      });
    });
    return out.slice(0, 100);
  }, [q, queries]);

  const loading = queries.some((q) => q.isLoading);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold">System Search</h1>
        <p className="text-sm text-muted-foreground">
          Cross-resource поиск по ID и имени. Включает IAM accounts/projects и vpc-ресурсы (admin).
        </p>
      </div>

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="ID, имя ресурса, имя клиента…"
          className="w-full pl-10 pr-4 py-2 bg-secondary border border-border rounded text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      {loading && <div className="text-xs text-muted-foreground">Loading indices…</div>}
      <div className="text-xs text-muted-foreground">
        {q ? `${hits.length} match${hits.length === 1 ? "" : "es"}` : "Введите подстроку — ID или имя"}
      </div>

      {hits.length > 0 && (
        <div className="border border-border rounded overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-xs uppercase tracking-wide">
              <tr>
                <th className="text-left px-3 py-2">Resource</th>
                <th className="text-left px-3 py-2">Name / ID</th>
                <th className="text-left px-3 py-2">Project / Account</th>
                <th className="text-left px-3 py-2">Extra</th>
              </tr>
            </thead>
            <tbody>
              {hits.map((h) => (
                <tr key={`${h.resource}-${h.id}`} className="border-t border-border hover:bg-muted/20">
                  <td className="px-3 py-2 text-xs uppercase">{h.resource}</td>
                  <td className="px-3 py-2">
                    <Link to={h.link} className="text-blue-400 hover:underline font-medium">
                      {h.name || "(unnamed)"}
                    </Link>
                    <div className="text-[10px] font-mono text-muted-foreground">{h.id}</div>
                  </td>
                  <td className="px-3 py-2 text-xs font-mono">
                    {h.project_id && <div>P: {h.project_id.slice(0, 12)}…</div>}
                    {h.account_id && <div>A: {h.account_id.slice(0, 12)}…</div>}
                    {!h.project_id && !h.account_id && <span className="text-muted-foreground">—</span>}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    {Object.entries(h.extras ?? {}).map(([k, v]) => (
                      <div key={k}>
                        <span className="text-muted-foreground">{k}:</span> <span className="font-mono">{v}</span>
                      </div>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function extractExtras(resource: string, r: Record<string, unknown>): Record<string, string> {
  const e: Record<string, string> = {};
  switch (resource) {
    case "addresses": {
      const ext = r.external_ipv4_address as Record<string, unknown> | undefined;
      const intn = r.internal_ipv4_address as Record<string, unknown> | undefined;
      if (ext?.address) e.ext = String(ext.address);
      if (intn?.address) e.int = String(intn.address);
      if (r.type) e.type = String(r.type);
      break;
    }
    case "subnets": {
      // VPC-1: placement anchor is zone_id (ZONAL) or region_id (REGIONAL);
      // CIDR is ipv4_cidr_primary + additional ranges (v4_cidr_blocks retired).
      if (r.zone_id) e.zone = String(r.zone_id);
      else if (r.region_id) e.region = String(r.region_id);
      const cidrs = [
        typeof r.ipv4_cidr_primary === "string" ? r.ipv4_cidr_primary : "",
        typeof r.ipv6_cidr_primary === "string" ? r.ipv6_cidr_primary : "",
        ...(Array.isArray(r.ipv4_cidr_blocks) ? (r.ipv4_cidr_blocks as string[]) : []),
        ...(Array.isArray(r.ipv6_cidr_blocks) ? (r.ipv6_cidr_blocks as string[]) : []),
      ].filter(Boolean);
      if (cidrs.length > 0) e.cidrs = cidrs.join(",");
      break;
    }
    case "address-pools":
      if (r.zone_id) e.zone = String(r.zone_id);
      if (r.kind) e.kind = String(r.kind);
      if (Array.isArray(r.cidr_blocks)) e.cidrs = (r.cidr_blocks as string[]).join(",");
      break;
    case "zones":
      if (r.region_id) e.region = String(r.region_id);
      break;
  }
  return e;
}
