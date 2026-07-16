// AddressDetailPage — обёртка над generic ResourceDetailPage,
// собирающая nested-breadcrumb когда Address открыт под subnet detail
// под network detail (URL
// `/projects/<projectId>/networks/<n>/subnets/<s>/addresses/<id>`).
//
// Project-level Address (внешний IP) использует flat-маршрут и обычный breadcrumb.

import { useParams } from "react-router-dom";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useNestedBreadcrumb } from "@shared/lib/use-nested-breadcrumb";

export function AddressDetailPage() {
  const { projectId, networkId, subnetId } = useParams();
  const spec = REGISTRY["addresses"];

  // "IP-адреса" в breadcrumb ведёт на subnet-detail с активным tab=addresses.
  const addressesTabHref =
    projectId && subnetId
      ? networkId
        ? `/projects/${projectId}/vpc/networks/${networkId}/subnets/${subnetId}?tab=addresses`
        : `/projects/${projectId}/vpc/subnets/${subnetId}?tab=addresses`
      : undefined;

  const { segments: breadcrumbSegments, backHref: backHrefOverride } = useNestedBreadcrumb({
    projectId,
    networkId,
    subnetId,
    currentResourcePlural: "IP-адреса",
    currentResourceListHref: addressesTabHref,
  });

  return <ResourceDetailPage spec={spec} backHrefOverride={backHrefOverride} breadcrumbSegments={breadcrumbSegments} />;
}
