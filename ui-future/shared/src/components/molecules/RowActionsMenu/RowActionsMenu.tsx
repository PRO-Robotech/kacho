import { useState } from "react";
import { useNavigate, useParams } from "react-router";
import { Button, Dropdown } from "antd";
import type { MenuProps } from "antd";
import {
  MoreOutlined,
  EyeOutlined,
  EditOutlined,
  DeleteOutlined,
  ArrowRightOutlined,
  DragOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import { DeleteDialog, requiresNameConfirm } from "@shared/components/molecules/DeleteDialog";
import { MoveStubDialog } from "@shared/components/molecules/MoveStubDialog";
import { getByPath, mutationBasePath, type ResourceSpec } from "@shared/lib/resource-registry";

interface Props {
  spec: ResourceSpec;
  row: Record<string, unknown>;
  basePath: string;
  projectId: string | null;
  /** KAC-231: когда true — «Редактировать» открывает форму-ПАНЕЛЬ
   *  (`${basePath}/${id}/edit` → ResourceShell mode=edit), а не модалку.
   *  Используется во встроенных таблицах дочерних ресурсов ResourceShell —
   *  единый panel-based флоу с созданием. На list-страницах — модалка (default). */
  editAsPanel?: boolean;
}

/**
 * Resources with no cross-project "move" semantics.
 *
 * The move dialog is a stub that prints the REST call it would make; offering it
 * for a resource whose API has no such verb advertises an operation that does
 * not exist. Every domain declared its own resources here — geo (regions/zones)
 * and iam (accounts/projects) and vpc address pools were already in the shared
 * copy; compute (instances), storage (volumes/snapshots/disk types) and registry
 * (registries/repositories/tags) each declared theirs in its own fork of this
 * file. One closed list means a resource cannot be movable in one app and not in
 * another.
 */
const MOVE_INCAPABLE = [
  "accounts",
  "projects",
  "regions",
  "zones",
  "address-pools",
  // compute
  "compute-instances",
  // storage
  "volumes",
  "snapshots",
  "disk-types",
  // registry (OCI entities)
  "registries",
  "repositories",
  "tags",
];

/**
 * Has this resource any row action at all?
 *
 * Mirrors the menu assembled below, so a read-only catalog resource does not get
 * a column holding a button that opens an empty menu. Networks are called out
 * because their menu carries "create subnet" even when the network itself is not
 * otherwise actionable.
 */
export function resourceHasRowActions(spec: ResourceSpec): boolean {
  return spec.ops.update || spec.ops.delete || !MOVE_INCAPABLE.includes(spec.id) || spec.id === "networks";
}

export function RowActionsMenu({ spec, row, basePath, projectId, editAsPanel }: Props) {
  const navigate = useNavigate();
  const params = useParams();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);

  const id = getByPath<string>(row, "id") ?? "";
  const name = getByPath<string>(row, "name") ?? id;
  const drillTarget = spec.childRoute ? spec.childRoute.replace(":id", id) : `${basePath}/${id}`;
  const drillIsChild = !!spec.childRoute;
  // Удаление/перемещение адресуют admin-плоскость, если она есть: DELETE по
  // публичному пути geo Region/Zone не смаршрутизирован.
  const editPath = `${mutationBasePath(spec)}/${id}`;

  const isDefaultSg = spec.id === "security-groups" && Boolean(getByPath<boolean>(row, "default_for_network"));
  const showDelete = spec.ops.delete && !isDefaultSg;

  const moveCapable = !MOVE_INCAPABLE.includes(spec.id);

  const isNetwork = spec.id === "networks";
  const currentProjectId = params.projectId ?? projectId ?? null;

  // antd Dropdown menu items рендерятся в portal, но React-event bubble идёт
  // через virtual-tree (а не DOM-tree). Без stopPropagation на domEvent клик по
  // menu-item доходит до строки таблицы и триггерит onRowClick → навигация
  // съедает наш setOpen / navigate. domEvent.stopPropagation() обязательно
  // на каждом item.
  //
  // `fn` объявлено действием без результата, и это не формальность: обработчик
  // клика antd результат не читает и промис не дожидается. `navigate` в
  // react-router 8 промис возвращает, поэтому на каждом вызове стоит явное
  // `void` — иначе отказ перехода превратился бы в необработанное отклонение,
  // которое некому увидеть.
  const stop =
    (fn: () => void) =>
    ({ domEvent }: { domEvent: React.MouseEvent | React.KeyboardEvent }) => {
      domEvent.stopPropagation();
      fn();
    };

  const items: MenuProps["items"] = [
    {
      key: "open",
      icon: drillIsChild ? <ArrowRightOutlined /> : <EyeOutlined />,
      label: drillIsChild ? "Открыть" : "Просмотр",
      onClick: stop(() => void navigate(drillTarget)),
    },
    isNetwork && currentProjectId
      ? {
          key: "create-subnet",
          icon: <PlusOutlined />,
          label: "Создать подсеть",
          // editAsPanel: форма-панель в зоне 3 shell сети (child-create).
          // Иначе (legacy list-модалка): ?modal-флаг над текущей страницей.
          onClick: stop(() =>
            void navigate(
              editAsPanel
                ? `/projects/${currentProjectId}/vpc/networks/${id}/subnets/create`
                : `/projects/${currentProjectId}/vpc/networks/${id}?modal=subnets-create&networkId=${id}`,
            ),
          ),
        }
      : null,
    spec.ops.update
      ? {
          key: "edit",
          icon: <EditOutlined />,
          label: "Редактировать",
          // editAsPanel (ResourceShell-контекст): форма-панель в зоне 3
          //   (`${basePath}/${id}/edit` → ResourceShell mode=edit), как создание.
          // Иначе (list-страница, KAC-70): модалка через ?modal-флаг.
          onClick: stop(() =>
            void navigate(editAsPanel ? `${basePath}/${id}/edit` : `${basePath}?modal=${spec.id}-edit&id=${id}`),
          ),
        }
      : null,
    moveCapable
      ? {
          key: "move",
          icon: <DragOutlined />,
          label: "Переместить",
          onClick: stop(() => setMoveOpen(true)),
        }
      : null,
    showDelete ? { type: "divider" as const } : null,
    showDelete
      ? {
          key: "delete",
          icon: <DeleteOutlined />,
          label: "Удалить",
          danger: true,
          onClick: stop(() => setDeleteOpen(true)),
        }
      : null,
  ].filter(Boolean);

  return (
    <>
      <Dropdown menu={{ items }} trigger={["click"]} placement="bottomRight">
        <Button
          type="text"
          size="small"
          icon={<MoreOutlined />}
          onClick={(e) => e.stopPropagation()}
          aria-label="Действия"
        />
      </Dropdown>

      {showDelete && (
        <DeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          apiPath={editPath}
          resourceId={spec.id}
          resourceLabel={spec.singular}
          name={name}
          projectId={projectId}
          requireNameConfirm={requiresNameConfirm(spec.id)}
          expectOperation={spec.mutationsReturnOperation !== false}
        />
      )}

      {moveCapable && (
        <MoveStubDialog
          open={moveOpen}
          onOpenChange={setMoveOpen}
          resourceLabel={spec.singular}
          name={name}
          apiPath={editPath}
        />
      )}
    </>
  );
}
