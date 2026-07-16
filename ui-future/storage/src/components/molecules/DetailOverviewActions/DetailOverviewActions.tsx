// DetailOverviewActions — действия ресурса в ШАПКЕ detail-страницы на табе «Обзор»
// (правый слот хедера — ResourceShell через useHeaderRight; KAC-242):
//   • «Редактировать» — primary-кнопка (единый стиль с «Создать <child>» на child-табах);
//   • «Удалить»       — видимая danger-кнопка (НЕ спрятана в ⋮-меню — KAC-242 правка);
//   • extActions      — доменные действия расширения (ext.headerActions), рендерятся первыми.
// Гейтинг: ops.update / ops.delete & not-default-SG. После удаления — переход на список.

import { type ReactNode, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "antd";
import { EditOutlined, DeleteOutlined } from "@ant-design/icons";
import { DeleteDialog, requiresNameConfirm } from "@/components/molecules/DeleteDialog";
import { getByPath, resourceProjectPath, type ResourceSpec } from "@/lib/resource-registry";

interface Props {
  spec: ResourceSpec;
  data: Record<string, unknown>;
  projectId: string | null;
  detailBase: string;
  /** Доменные действия расширения (ext.headerActions) — рендерятся первыми. */
  extActions?: ReactNode;
}

export function DetailOverviewActions({ spec, data, projectId, detailBase, extActions }: Props) {
  const navigate = useNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const id = getByPath<string>(data, "id") ?? "";
  const name = getByPath<string>(data, "name") ?? id;
  const apiPath = `${spec.apiPath}/${id}`;
  const listPath = resourceProjectPath(spec.id, projectId) ?? `/${spec.route}`;

  const isDefaultSg = spec.id === "security-groups" && Boolean(getByPath<boolean>(data, "default_for_network"));
  const showDelete = spec.ops.delete && !isDefaultSg;

  return (
    <>
      {extActions}
      {spec.ops.update && (
        <Button type="primary" icon={<EditOutlined />} onClick={() => navigate(`${detailBase}/edit`)}>
          Редактировать
        </Button>
      )}
      {showDelete && (
        <Button danger icon={<DeleteOutlined />} onClick={() => setDeleteOpen(true)}>
          Удалить
        </Button>
      )}

      {showDelete && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          apiPath={apiPath}
          resourceId={spec.id}
          resourceLabel={spec.singular}
          name={name}
          projectId={projectId}
          requireNameConfirm={requiresNameConfirm(spec.id)}
          onSuccess={() => navigate(listPath)}
        />
      )}
    </>
  );
}
