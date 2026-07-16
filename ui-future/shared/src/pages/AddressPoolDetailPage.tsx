// AddressPoolDetailPage — admin страница AddressPool: utilization + выделенные адреса.
//
// Корневой системный тенант (нет account/project для самого pool). Вкладка
// «Адреса» (KAC-273) показывает выделенные из пула адреса колонками
// имя / идентификатор / IP-адрес / дата создания.

import { Link, useParams } from "react-router-dom";
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { IpamUtilizationBar, CIDRBreakdown } from "@shared/components/molecules/IpamUtilizationBar";
import { AddressPoolCidrManager } from "@shared/components/organisms/AddressPoolCidrManager";
import { CopyableName } from "@shared/components/atoms/CopyableName";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { api } from "@shared/api/client";
import { REGISTRY } from "@shared/lib/resource-registry";
import { formatDateTime } from "@shared/lib/datetime";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

interface PoolAddrEntry {
  id: string;
  project_id: string;
  name: string;
  ipv4: string;
  zone_id: string;
  reserved: boolean;
  used: boolean;
  created_at: string;
}

export function AddressPoolDetailPage() {
  const { uid: poolId } = useParams();
  const spec = REGISTRY["address-pools"];

  const { data: util } = useQuery({
    queryKey: ["pool-util", poolId],
    queryFn: () =>
      api.get<{
        pool_id: string;
        total_ips: string | number;
        used_ips: string | number;
        free_ips: string | number;
        used_percent: number;
        cidrs: { cidr: string; total: string | number; used: string | number }[];
      }>(`/vpc/v1/addressPools/${poolId}/utilization`),
    refetchInterval: 5000,
    enabled: !!poolId,
  });

  const { data: addresses } = useQuery({
    queryKey: ["pool-addresses", poolId],
    queryFn: () => api.get<{ addresses: PoolAddrEntry[] }>(`/vpc/v1/addressPools/${poolId}/addresses?pageSize=200`),
    refetchInterval: 5000,
    enabled: !!poolId,
  });

  const extraTabs = useMemo(
    () => (): DetailTab[] => {
      const addrCount = addresses?.addresses?.length ?? 0;
      return [
        {
          id: "ipam",
          label: "Использование",
          render: () =>
            util ? (
              <div className="space-y-6">
                <IpamUtilizationBar
                  label="Утилизация пула"
                  total={util.total_ips}
                  used={util.used_ips}
                  free={util.free_ips}
                  percent={util.used_percent}
                />
                <div>
                  <div className="text-xs font-medium text-muted-foreground mb-2">По CIDR</div>
                  <CIDRBreakdown cidrs={util.cidrs ?? []} />
                </div>
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">Загрузка…</div>
            ),
        },
        {
          id: "addresses",
          label: "Адреса",
          count: addrCount,
          render: () => (
            <div className="rounded-lg border border-border overflow-hidden bg-card">
              <table className="w-full text-sm">
                <thead className="bg-muted/40 text-xs uppercase tracking-wide">
                  <tr>
                    <th className="text-left px-3 py-2">Имя</th>
                    <th className="text-left px-3 py-2">Идентификатор</th>
                    <th className="text-left px-3 py-2">IP-адрес</th>
                    <th className="text-left px-3 py-2">Дата создания</th>
                  </tr>
                </thead>
                <tbody>
                  {(addresses?.addresses ?? []).map((a) => (
                    <tr key={a.id} className="border-t border-border hover:bg-muted/20">
                      <td className="px-3 py-2">
                        <Link to={`/projects/${a.project_id}/vpc/addresses/${a.id}`}>
                          <CopyableName name={a.name} fallback={a.id} />
                        </Link>
                      </td>
                      <td className="px-3 py-2">
                        <CopyableId id={a.id} />
                      </td>
                      <td className="px-3 py-2 font-mono">{a.ipv4 || "—"}</td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">{formatDateTime(a.created_at)}</td>
                    </tr>
                  ))}
                  {(!addresses?.addresses || addresses.addresses.length === 0) && (
                    <tr>
                      <td colSpan={4} className="px-3 py-8 text-center text-muted-foreground">
                        Из этого пула адреса ещё не выделены
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          ),
        },
      ];
    },
    [util, addresses],
  );

  // CIDR-блоки пула — отдельная панель управления под «Общим» в Обзоре (паритет
  // с Subnet CIDR). KAC-269: мутируются :addCidrBlocks / :removeCidrBlocks, не
  // PATCH (Update больше не меняет CIDR).
  const overviewExtras = (data: Record<string, unknown>) => {
    const id = (data.id as string) ?? poolId ?? "";
    const v4 = (data.v4_cidr_blocks as string[] | undefined) ?? [];
    const v6 = (data.v6_cidr_blocks as string[] | undefined) ?? [];
    return (
      <div style={{ marginTop: 24, maxWidth: 760 }}>
        <AddressPoolCidrManager poolId={id} v4Blocks={v4} v6Blocks={v6} />
      </div>
    );
  };

  return <ResourceDetailPage spec={spec} extraTabs={extraTabs} overviewExtras={overviewExtras} />;
}
