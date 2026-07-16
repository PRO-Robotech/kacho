// useNestedBreadcrumb — собирает breadcrumb-цепочку для nested-маршрутов
// (Subnet/RT/SG под Network, Address под Subnet под Network).
//
// Возвращаемая структура совместима с `breadcrumbSegments` ResourceDetailPage /
// ResourceEditPage. Промежуточные сегменты-группы ("Подсети", "Таблицы
// маршрутизации", "IP-адреса") — не кликабельны (нет href). Сегменты-имена
// (Network, Subnet) — кликабельны и ведут на их detail.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { REGISTRY } from "@shared/lib/resource-registry";

export interface BreadcrumbSegment {
  label: string;
  href?: string;
}

interface Args {
  projectId: string | undefined;
  /** Когда текущий ресурс лежит под Network. */
  networkId?: string | undefined;
  /** Когда текущий ресурс лежит под Subnet (внутри Network). */
  subnetId?: string | undefined;
  /** Множественное имя группы текущего ресурса ("Подсети", "Таблицы маршрутизации",
   *  "Группы безопасности", "IP-адреса"). Появится последним сегментом перед
   *  именем самого ресурса. */
  currentResourcePlural: string;
  /** Опциональный URL для группы-сегмента currentResourcePlural — делает её
   *  кликабельной. Полезно когда у parent-ресурса есть tab со списком (например,
   *  "IP-адреса" → subnet detail с ?tab=addresses). */
  currentResourceListHref?: string;
}

interface Result {
  /** Готовая цепочка для breadcrumbSegments prop. undefined если нет nested-контекста. */
  segments: BreadcrumbSegment[] | undefined;
  /** URL непосредственного parent для кнопки "Назад". */
  backHref: string | undefined;
}

export function useNestedBreadcrumb({
  projectId,
  networkId,
  subnetId,
  currentResourcePlural,
  currentResourceListHref,
}: Args): Result {
  const { data: networkData } = useQuery({
    queryKey: ["networks", "detail", networkId],
    queryFn: () => api.get<Record<string, unknown>>(`${REGISTRY.networks.apiPath}/${networkId}`),
    enabled: !!networkId,
    staleTime: 30_000,
  });

  const { data: subnetData } = useQuery({
    queryKey: ["subnets", "detail", subnetId],
    queryFn: () => api.get<Record<string, unknown>>(`${REGISTRY.subnets.apiPath}/${subnetId}`),
    enabled: !!subnetId,
    staleTime: 30_000,
  });

  return useMemo(() => {
    if (!projectId || (!networkId && !subnetId)) {
      return { segments: undefined, backHref: undefined };
    }

    const segs: BreadcrumbSegment[] = [];

    if (networkId) {
      const networkName = (networkData?.name as string | undefined) || networkId;
      segs.push({
        label: "Облачные сети",
        href: `/projects/${projectId}/vpc/networks`,
      });
      segs.push({
        label: networkName,
        href: `/projects/${projectId}/vpc/networks/${networkId}`,
      });
    }

    if (subnetId) {
      const subnetName = (subnetData?.name as string | undefined) || subnetId;
      const subnetHref = networkId
        ? `/projects/${projectId}/vpc/networks/${networkId}/subnets/${subnetId}`
        : `/projects/${projectId}/vpc/subnets/${subnetId}`;
      // "Подсети" — группа-родитель; кликабельна только при flat-контексте (нет networkId).
      segs.push({
        label: "Подсети",
        href: networkId ? undefined : `/projects/${projectId}/vpc/subnets`,
      });
      segs.push({ label: subnetName, href: subnetHref });
    }

    segs.push({ label: currentResourcePlural, href: currentResourceListHref });

    let backHref: string | undefined;
    for (let i = segs.length - 1; i >= 0; i--) {
      if (segs[i].href) {
        backHref = segs[i].href;
        break;
      }
    }
    return { segments: segs, backHref };
  }, [projectId, networkId, subnetId, networkData, subnetData, currentResourcePlural, currentResourceListHref]);
}
