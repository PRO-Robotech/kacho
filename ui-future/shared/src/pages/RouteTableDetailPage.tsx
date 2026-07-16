// RouteTableDetailPage — обёртка над generic ResourceDetailPage,
// собирающая nested-breadcrumb когда RT открыт под network detail
// (URL `/projects/<projectId>/networks/<n>/route-tables/<id>`).

import { useParams } from "react-router-dom";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useNestedBreadcrumb } from "@shared/lib/use-nested-breadcrumb";

export function RouteTableDetailPage() {
  const { projectId, networkId } = useParams();
  const spec = REGISTRY["route-tables"];

  const { segments: breadcrumbSegments, backHref: backHrefOverride } = useNestedBreadcrumb({
    projectId,
    networkId,
    currentResourcePlural: spec.plural,
  });

  return <ResourceDetailPage spec={spec} backHrefOverride={backHrefOverride} breadcrumbSegments={breadcrumbSegments} />;
}
