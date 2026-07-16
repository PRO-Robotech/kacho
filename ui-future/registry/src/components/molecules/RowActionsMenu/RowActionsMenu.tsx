import { useState } from "react";
import type { ReactNode } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Button, Dropdown, Tooltip } from "antd";
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
import { DeleteDialog, requiresNameConfirm } from "@/components/molecules/DeleteDialog";
import { MoveStubDialog } from "@/components/molecules/MoveStubDialog";
import { getByPath, type ResourceSpec } from "@/lib/resource-registry";

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

// RowAction — единичное строчное действие: иконка + подпись + обработчик.
// Общий тип для обоих способов рендера (инлайн-кнопка / пункт кебаба).
type RowAction = { key: string; icon: ReactNode; label: string; danger?: boolean; run: () => void };

// Ресурсы без семантики cross-project «Переместить»: системные + OCI-сущности
// реестра (registry/репозиторий/тег).
const MOVE_INCAPABLE = ["accounts", "projects", "regions", "zones", "address-pools", "registries", "repositories", "tags"];

// resourceHasRowActions — есть ли у ресурса хоть одно строчное действие
// (мутация или move/create-subnet). Совпадает с логикой mutationActions ниже —
// таблицы используют его, чтобы не рисовать пустой столбец действий.
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
  const editPath = `${spec.apiPath}/${id}`;

  const isDefaultSg = spec.id === "security-groups" && Boolean(getByPath<boolean>(row, "default_for_network"));
  const showDelete = spec.ops.delete && !isDefaultSg;

  // Move — заглушка cross-project перемещения. Неприменима к системным ресурсам
  // и к OCI-сущностям реестра (registry/репозиторий/тег) — у них нет такой семантики.
  const moveCapable = !MOVE_INCAPABLE.includes(spec.id);

  const isNetwork = spec.id === "networks";
  const currentProjectId = params.projectId ?? projectId ?? null;

  // «Просмотр»/«Открыть» — чистая навигация в detail; не считается мутирующим
  // действием (строка и так открывается кликом).
  const openAction: RowAction = {
    key: "open",
    icon: drillIsChild ? <ArrowRightOutlined /> : <EyeOutlined />,
    label: drillIsChild ? "Открыть" : "Просмотр",
    run: () => navigate(drillTarget),
  };

  // Мутирующие действия строки.
  const mutationActions: RowAction[] = [];
  if (isNetwork && currentProjectId) {
    mutationActions.push({
      key: "create-subnet",
      icon: <PlusOutlined />,
      label: "Создать подсеть",
      // editAsPanel: форма-панель в зоне 3 shell сети (child-create).
      // Иначе (legacy list-модалка): ?modal-флаг над текущей страницей.
      run: () =>
        navigate(
          editAsPanel
            ? `/projects/${currentProjectId}/vpc/networks/${id}/subnets/create`
            : `/projects/${currentProjectId}/vpc/networks/${id}?modal=subnets-create&networkId=${id}`,
        ),
    });
  }
  if (spec.ops.update) {
    mutationActions.push({
      key: "edit",
      icon: <EditOutlined />,
      label: "Редактировать",
      // editAsPanel (ResourceShell-контекст): форма-панель в зоне 3
      //   (`${basePath}/${id}/edit` → ResourceShell mode=edit), как создание.
      // Иначе (list-страница): модалка через ?modal-флаг.
      run: () => navigate(editAsPanel ? `${basePath}/${id}/edit` : `${basePath}?modal=${spec.id}-edit&id=${id}`),
    });
  }
  if (moveCapable) {
    mutationActions.push({ key: "move", icon: <DragOutlined />, label: "Переместить", run: () => setMoveOpen(true) });
  }
  if (showDelete) {
    mutationActions.push({
      key: "delete",
      icon: <DeleteOutlined />,
      label: "Удалить",
      danger: true,
      run: () => setDeleteOpen(true),
    });
  }

  // Дисциплина «не прятать одиночное действие за кебабом»:
  //   0 мутаций → строка открывается кликом, отдельная кнопка не нужна;
  //   1 мутация → показываем её инлайн иконкой (напр. «Удалить»), а не ⋮;
  //   ≥2 мутаций → кебаб с полным меню (включая «Просмотр»).
  const singleInline = mutationActions.length === 1 ? mutationActions[0] : null;

  // stop — antd Dropdown menu-items рендерятся в portal, но React-event bubble
  // идёт через virtual-tree (не DOM-tree). Без stopPropagation клик по item
  // доходит до строки → onRowClick съедает наш navigate/setOpen.
  const stop =
    (fn: () => void) =>
    ({ domEvent }: { domEvent: React.MouseEvent | React.KeyboardEvent }) => {
      domEvent.stopPropagation();
      fn();
    };
  const stopRun = (fn: () => void) => (e: React.MouseEvent) => {
    e.stopPropagation();
    fn();
  };

  const menuItems: MenuProps["items"] = [
    { key: openAction.key, icon: openAction.icon, label: openAction.label, onClick: stop(openAction.run) },
  ];
  for (const a of mutationActions) {
    if (a.key === "delete") menuItems.push({ type: "divider" as const });
    menuItems.push({ key: a.key, icon: a.icon, label: a.label, danger: a.danger, onClick: stop(a.run) });
  }

  return (
    <>
      {mutationActions.length === 0 ? null : singleInline ? (
        <Tooltip title={singleInline.label} placement="topRight">
          <Button
            type="text"
            size="small"
            danger={singleInline.danger}
            icon={singleInline.icon}
            onClick={stopRun(singleInline.run)}
            aria-label={singleInline.label}
          />
        </Tooltip>
      ) : (
        <Dropdown menu={{ items: menuItems }} trigger={["click"]} placement="bottomRight">
          <Button
            type="text"
            size="small"
            icon={<MoreOutlined />}
            onClick={(e) => e.stopPropagation()}
            aria-label="Действия"
          />
        </Dropdown>
      )}

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
