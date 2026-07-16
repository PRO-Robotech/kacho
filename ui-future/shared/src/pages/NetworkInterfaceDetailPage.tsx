// NetworkInterfaceDetailPage — кастомизированная страница ресурса
// NetworkInterface (KAC-2). Добавляет в "Общее" две таблицы связанных
// ресурсов:
//
//  - Подключенные адреса — все Address-ресурсы, на которые ссылается
//    NIC через v4_address_ids ∪ v6_address_ids (resolved by id).
//  - Группы безопасности — Security Group по security_group_ids.
//
// Реализовано через `overviewExtras` в ResourceDetailPage (тот же hook
// что использует Network detail для inline-таблиц дочерних ресурсов).
// Сравнить с SubnetDetailPage (там адреса — отдельный tab, потому что
// их много; у NIC ≤2 адреса и обычно ≤несколько SG — компактнее в "Общее").

import { useMemo } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, Tag, Typography } from "antd";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import { REGISTRY } from "@shared/lib/resource-registry";
import { api } from "@shared/api/client";

type Address = Record<string, unknown> & { id: string };
type SG = Record<string, unknown> & { id: string };

export function NetworkInterfaceDetailPage() {
  const { uid: nicId, projectId } = useParams();
  const navigate = useNavigate();
  const spec = REGISTRY["network-interfaces"];

  // Загружаем все Address-ресурсы проекта — потом client-side filter
  // по v4_address_ids ∪ v6_address_ids текущего NIC.
  const { data: addrList } = useQuery({
    queryKey: ["addresses", "list-for-nic", projectId],
    queryFn: () =>
      api.list<{ addresses: Address[] }>("/vpc/v1/addresses", {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 10000,
    enabled: !!projectId,
  });

  // Аналогично — все SG проекта для resolve security_group_ids.
  const { data: sgList } = useQuery({
    queryKey: ["security-groups", "list-for-nic", projectId],
    queryFn: () =>
      api.list<{ security_groups: SG[] }>("/vpc/v1/securityGroups", {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 10000,
    enabled: !!projectId,
  });

  const overviewExtras = useMemo(
    () => (data: Record<string, unknown>) => {
      const v4Ids = (data.v4_address_ids as string[] | undefined) ?? [];
      const v6Ids = (data.v6_address_ids as string[] | undefined) ?? [];
      const sgIds = (data.security_group_ids as string[] | undefined) ?? [];

      const addrById = new Map((addrList?.addresses ?? []).map((a) => [a.id, a]));
      const sgById = new Map((sgList?.security_groups ?? []).map((g) => [g.id, g]));

      const linkedAddresses: Array<{ id: string; name: string; family: string; ip: string }> = [];
      for (const id of v4Ids) {
        const a = addrById.get(id);
        const ip =
          (a?.internal_ipv4_address as { address?: string } | undefined)?.address ??
          (a?.external_ipv4_address as { address?: string } | undefined)?.address ??
          "";
        linkedAddresses.push({
          id,
          name: (a?.name as string) ?? "",
          family: "IPv4",
          ip,
        });
      }
      for (const id of v6Ids) {
        const a = addrById.get(id);
        const ip =
          (a?.internal_ipv6_address as { address?: string } | undefined)?.address ??
          (a?.external_ipv6_address as { address?: string } | undefined)?.address ??
          "";
        linkedAddresses.push({
          id,
          name: (a?.name as string) ?? "",
          family: "IPv6",
          ip,
        });
      }

      return (
        <div className="space-y-4">
          <Card size="small" title={`Подключенные адреса (${linkedAddresses.length})`}>
            {linkedAddresses.length === 0 ? (
              <Typography.Text type="secondary">Адресов не привязано.</Typography.Text>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-xs text-muted-foreground">
                  <tr>
                    <th className="text-left py-1 pr-3">Имя</th>
                    <th className="text-left py-1 pr-3">Семейство</th>
                    <th className="text-left py-1 pr-3">IP</th>
                    <th className="text-left py-1">ID</th>
                  </tr>
                </thead>
                <tbody>
                  {linkedAddresses.map((row) => (
                    <tr
                      key={row.id}
                      className="border-t border-border hover:bg-muted/30 cursor-pointer"
                      onClick={() => navigate(`/projects/${projectId}/vpc/addresses/${row.id}`)}
                    >
                      <td className="py-1 pr-3">
                        {row.name ? (
                          <a className="text-primary hover:underline">{row.name}</a>
                        ) : (
                          <span className="text-muted-foreground">(без имени)</span>
                        )}
                      </td>
                      <td className="py-1 pr-3">
                        <Tag color={row.family === "IPv4" ? "blue" : "geekblue"}>{row.family}</Tag>
                      </td>
                      <td className="py-1 pr-3 font-mono text-xs">
                        {row.ip || <span className="text-muted-foreground">—</span>}
                      </td>
                      <td className="py-1 font-mono text-xs text-muted-foreground">{row.id}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>

          <Card size="small" title={`Группы безопасности (${sgIds.length})`}>
            {sgIds.length === 0 ? (
              <Typography.Text type="secondary">SG не привязаны.</Typography.Text>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-xs text-muted-foreground">
                  <tr>
                    <th className="text-left py-1 pr-3">Имя</th>
                    <th className="text-left py-1 pr-3">Default</th>
                    <th className="text-left py-1">ID</th>
                  </tr>
                </thead>
                <tbody>
                  {sgIds.map((id) => {
                    const sg = sgById.get(id);
                    return (
                      <tr
                        key={id}
                        className="border-t border-border hover:bg-muted/30 cursor-pointer"
                        onClick={() => navigate(`/projects/${projectId}/vpc/security-groups/${id}`)}
                      >
                        <td className="py-1 pr-3">
                          {(sg?.name as string) ? (
                            <a className="text-primary hover:underline">{sg?.name as string}</a>
                          ) : (
                            <span className="text-muted-foreground">(без имени)</span>
                          )}
                        </td>
                        <td className="py-1 pr-3">{sg?.default_for_network ? <Tag color="gold">default</Tag> : "—"}</td>
                        <td className="py-1 font-mono text-xs text-muted-foreground">{id}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </Card>
        </div>
      );
    },
    [addrList, sgList, projectId, navigate],
  );

  // Без uid/projectId — generic detail (он сам обработает loading/empty).
  if (!nicId || !projectId) {
    return <ResourceDetailPage spec={spec} />;
  }

  return (
    <>
      <ResourceDetailPage spec={spec} overviewExtras={overviewExtras} />
      <ResourceFormModal projectId={projectId} />
    </>
  );
}
