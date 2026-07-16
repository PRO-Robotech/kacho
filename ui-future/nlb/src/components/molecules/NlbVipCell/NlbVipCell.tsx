// NlbVipCell — VIP-адрес(а) балансировщика в колонке списка / строке обзора.
// Поля LoadBalancer `v4_address_id` / `v6_address_id` ссылаются на vpc Address
// (аллоцированный VIP). Рендерим единым видом ссылки на ресурс — иконка + имя
// адреса (+ сам IP моноширинно), кликабельно на detail адреса — как поле
// «IPv4-адрес» на NIC-детали (AddressRefTag). Оба id пустые → прочерк.

import type { FC } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { getByPath } from "@/lib/resource-registry";

export interface NlbVipCellProps {
  v4AddressId?: string;
  v6AddressId?: string;
}

// addressIp — вытаскивает сам IP из Address-ресурса (external/internal, v4/v6).
function addressIp(data: Record<string, unknown> | undefined): string {
  if (!data) return "";
  return (
    getByPath<string>(data, "external_ipv4_address.address") ??
    getByPath<string>(data, "external_ipv6_address.address") ??
    getByPath<string>(data, "internal_ipv4_address.address") ??
    getByPath<string>(data, "internal_ipv6_address.address") ??
    ""
  );
}

// VipAddressLink — резолвит Address по id и рендерит сам IP (моноширинно)
// ссылкой на detail адреса в модуле VPC. Резолв через ОДНУ общую LIST-выборку
// адресов проекта (queryKey per-project, TanStack-дедуп) — не per-id GET: все
// ячейки списка LB делят один запрос → мгновенно. Пока не загрузилось — id.
const VipAddressLink: FC<{ id: string }> = ({ id }) => {
  const { projectId } = useParams();
  const { data } = useQuery({
    queryKey: ["addresses-by-project", projectId],
    queryFn: () =>
      api.list<{ addresses: Array<Record<string, unknown>> }>("/vpc/v1/addresses", {
        project_id: projectId ?? "",
        pageSize: "1000",
      }),
    enabled: !!projectId,
    staleTime: 30_000,
  });
  const addr = (data?.addresses ?? []).find((a) => (a.id as string) === id);
  const label = addressIp(addr) || id.slice(0, 12);
  const content = <span style={{ fontFamily: "ui-monospace, monospace" }}>{label}</span>;
  return projectId ? (
    <Link
      to={`/projects/${projectId}/vpc/addresses/${id}`}
      onClick={(e) => e.stopPropagation()}
      className="text-primary hover:underline"
    >
      {content}
    </Link>
  ) : (
    <span className="text-foreground">{content}</span>
  );
};

export const NlbVipCell: FC<NlbVipCellProps> = ({ v4AddressId, v6AddressId }) => {
  const ids = [v4AddressId, v6AddressId].filter((x): x is string => !!x);
  if (ids.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <VipAddressLink key={id} id={id} />
      ))}
    </span>
  );
};
