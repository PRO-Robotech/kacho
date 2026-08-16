// IamRefLink — ссылка на ресурс IAM по идентификатору (аккаунт/проект/
// пользователь/роль/…).
//
// Отличие от `RefNameLink` ровно одно и оно про ЗАПРОС: ресурсы IAM не сужаются
// проектом, поэтому имя резолвится точечным чтением `/iam/v1/<route>/<id>`, а не
// списочным запросом с `project_id`. Всё остальное — вид ссылки, иконка типа,
// усечение, копирование значения — берётся у общей `ResourceLink`.
//
// Прежде разметка собиралась здесь руками, и это была ВТОРАЯ реализация «иконка +
// имя + ссылка». Два места об одном предмете разошлись предсказуемо: копирование
// значения появилось у общей ссылки и не появилось здесь (#446).
//
// Живёт в shared (app-agnostic), чтобы колонки реестра для ресурсов IAM
// резолвились в любом приложении.

import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { ResourceLink } from "@shared/components/molecules/ResourceLink";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";

interface Props {
  /** plural-ключ ресурса IAM в реестре: accounts/projects/users/service-accounts/roles/groups. */
  specId: string;
  refId: string | null | undefined;
  /** поле отображаемого имени (по умолчанию «name»; у пользователя — «email»). */
  nameField?: string;
  maxChars?: number;
}

export function IamRefLink({ specId, refId, nameField = "name", maxChars = 36 }: Props) {
  const spec = REGISTRY[specId];

  const { data } = useQuery({
    queryKey: ["iam-ref", specId, refId],
    queryFn: () => api.get<Record<string, unknown>>(`${spec.apiPath}/${refId}`),
    enabled: !!spec && !!refId,
    staleTime: 30_000,
    retry: false,
  });

  // Тип, которого нет в реестре, адресовать нечем: маршрут карточки строится из
  // его записи. Показываем идентификатор без ссылки — обещать переход, которого
  // нет, хуже, чем не обещать ничего.
  if (!spec) return <span className="text-muted-foreground">{refId || "—"}</span>;

  const resolved = data ? getByPath<string>(data, nameField) || getByPath<string>(data, "name") : undefined;

  return (
    <ResourceLink
      specId={specId}
      id={refId}
      name={resolved ?? ""}
      // Адрес задаётся явно: ресурсы IAM смонтированы под `/iam/<route>` и
      // project-scoped пути не имеют — общая функция отдала бы для них null.
      href={refId ? `/iam/${spec.route}/${refId}` : null}
      icon
      copy
      maxChars={maxChars}
      plain
    />
  );
}
