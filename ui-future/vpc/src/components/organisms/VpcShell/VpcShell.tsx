// VpcShell — обёртка для list / detail VPC-страниц, добавляющая mount
// <ResourceFormModal/> для всех ресурсов (open через ?modal=<spec>-create
// или ?modal=<spec>-edit&id=<uid>).
//
// Один компонент per route — заменяет прямой mount ResourceListPage /
// ResourceDetailPage в роутинге, чтобы у каждой VPC-страницы автоматически
// был mount-point модалки.

import { useParams } from "react-router";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import type { ResourceSpec } from "@shared/lib/resource-registry";

interface ListProps {
  spec: ResourceSpec;
  parentField?: string;
  parentParam?: string;
  /** Есть ли у ресурса форма-страница/панель в этом разделе — решает тот,
   *  кто зарегистрировал маршруты (см. ResourceListPage.Props). */
  panelForms: boolean;
}

export function VpcListShell({ spec, parentField, parentParam, panelForms }: ListProps) {
  const { projectId } = useParams();
  return (
    <>
      <ResourceListPage spec={spec} parentField={parentField} parentParam={parentParam} panelForms={panelForms} />
      {projectId && <ResourceFormModal projectId={projectId} />}
    </>
  );
}

interface DetailProps {
  spec: ResourceSpec;
  paramKey?: string;
}

export function VpcDetailShell({ spec, paramKey }: DetailProps) {
  const { projectId } = useParams();
  return (
    <>
      <ResourceDetailPage spec={spec} paramKey={paramKey} />
      {projectId && <ResourceFormModal projectId={projectId} />}
    </>
  );
}
