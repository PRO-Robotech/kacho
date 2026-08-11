// RefNameLink — name+ссылка на detail для любого project-scoped ресурса по id.
// Заменяет SgNameById. Берёт spec из registry, делает один project-scoped list-query
// (дедуплицируется TanStack по (specId, projectId)), находит row.name по id.
// При клике stopPropagation чтобы не триггерить row-click таблицы-родителя.

import { Link, useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Tag } from "antd";
import { api } from "@shared/api/client";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { useProjectStore } from "@shared/lib/context-store";
import { REGISTRY, resourceProjectPath } from "@shared/lib/resource-registry";

interface Props {
  specId: string; // "networks" | "route-tables" | "security-groups" | ...
  refId: string | null | undefined;
  projectId?: string;
  /** Render как antd Tag (chip-стиль). Default — обычная ссылка. */
  asTag?: boolean;
  /** Если задан — обрезать имя по N символов с многоточием. Title даёт полное имя. */
  maxChars?: number;
}

export function RefNameLink({ specId, refId, projectId: projectOverride, asTag, maxChars }: Props) {
  const params = useParams();
  const project = useProjectStore((s) => s.project);
  const projectId = projectOverride ?? params.projectId ?? project?.id ?? null;
  const spec = REGISTRY[specId];

  // Область видимости ресурса решает, чем его спрашивать. Регион и зона —
  // ГЛОБАЛЬНЫЙ каталог размещения: измерения «проект» у него нет, поэтому
  // `project_id` в таком запросе — чужой параметр. Ссылка на него при этом
  // стоит на страницах ВНУТРИ проекта (подсеть называет свой регион), так что
  // проект в контексте есть — и требовать его для запуска запроса значит
  // молча не резолвить имя там, где проекта нет вовсе (страницы /system/*).
  const projectScoped = spec?.scope === "project";
  const { data } = useQuery({
    queryKey: ["ref-name", specId, projectScoped ? projectId : null],
    queryFn: () =>
      api.list<Record<string, Array<{ id: string; name?: string }>>>(spec.apiPath, {
        ...(projectScoped ? { project_id: projectId! } : {}),
        pageSize: "500",
      }),
    enabled: !!spec && !!refId && (!projectScoped || !!projectId),
    staleTime: 30_000,
  });

  if (!refId) return <span className="text-muted-foreground">—</span>;
  if (!spec) return <span className="text-muted-foreground">{refId}</span>;

  const items = data?.[spec.payloadKey] ?? [];
  const row = items.find((r) => r.id === refId);
  const fullName = row?.name || refId.slice(0, 12) + "…";
  const display = maxChars && fullName.length > maxChars ? fullName.slice(0, maxChars) + "…" : fullName;
  // KAC-198: include service segment (vpc/compute/nlb) — без него detail-route
  // в App.tsx не матчился → клик по ссылке шёл в SPA-fallback (blank/404).
  const basePath = resourceProjectPath(specId, projectId);
  const href = basePath ? `${basePath}/${refId}` : null;

  // Единый вид ссылки на ресурс: иконка типа ресурса + имя (как в документации).
  const content = (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <ResourceIcon specId={specId} />
      {display}
    </span>
  );
  const inner = href ? (
    <Link to={href} onClick={(e) => e.stopPropagation()} title={fullName} className="text-primary hover:underline">
      {content}
    </Link>
  ) : (
    <span className="text-foreground" title={fullName}>
      {content}
    </span>
  );

  if (asTag) {
    return <Tag style={{ margin: 0, padding: "0 6px", lineHeight: "20px" }}>{inner}</Tag>;
  }
  return inner;
}
