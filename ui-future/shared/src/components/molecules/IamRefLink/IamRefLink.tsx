// IamRefLink — ссылка на IAM-ресурс по id (account/project/user/role/…).
//
// RefNameLink (VPC) делает project-scoped list-query (project_id=…), что не
// подходит для IAM-ресурсов (они не scoped к проекту). Здесь — точечный GET
// /iam/v1/<route>/<id> (дедуплицируется TanStack), резолв name (или email для
// user), ссылка на /iam/<route>/<id>. Cross-domain dangling-ref грациозен: при
// NOT_FOUND показываем raw id моноширинно, без падения.
//
// App-agnostic: живёт в shared, чтобы REGISTRY-колонки IAM-ресурсов (shared)
// резолвились в любом app'е (только @shared-зависимости).

import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";

interface Props {
  /** plural-ключ IAM-ресурса в REGISTRY: accounts/projects/users/service-accounts/roles/groups. */
  specId: string;
  refId: string | null | undefined;
  /** поле для отображаемого имени (default «name»; для user — «email»). */
  nameField?: string;
  maxChars?: number;
}

export function IamRefLink({ specId, refId, nameField = "name", maxChars = 36 }: Props) {
  const spec = REGISTRY[specId];

  const { data } = useQuery({
    queryKey: ["iam-ref", specId, refId],
    queryFn: () => api.get<Record<string, unknown>>(`${spec!.apiPath}/${refId}`),
    enabled: !!spec && !!refId,
    staleTime: 30_000,
    retry: false,
  });

  if (!refId) return <span className="text-muted-foreground">—</span>;
  if (!spec) return <span className="text-muted-foreground">{refId}</span>;

  const resolved = data ? getByPath<string>(data, nameField) || getByPath<string>(data, "name") : undefined;
  const fullName = resolved || refId;
  const display = fullName.length > maxChars ? fullName.slice(0, maxChars) + "…" : fullName;
  const href = `/iam/${spec.route}/${refId}`;

  return (
    <Link to={href} onClick={(e) => e.stopPropagation()} title={fullName} className="text-primary hover:underline">
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <ResourceIcon specId={specId} />
        {display}
      </span>
    </Link>
  );
}
