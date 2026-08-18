import { useState } from "react";
import { useNavigate, useParams } from "react-router";
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
import { DeleteDialog, requiresNameConfirm } from "@shared/components/molecules/DeleteDialog";
import { MoveStubDialog } from "@shared/components/molecules/MoveStubDialog";
import { RowVerbDialog } from "@shared/components/molecules/RowActionsMenu/RowVerbDialog";
import { useSelfUserId } from "@shared/contexts/AuthContext";
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
 * Ресурсы, у которых перемещение ОБЪЯВЛЕНО контрактом.
 *
 * Перечень — РАЗРЕШАЮЩИЙ, и это существо правила, а не стиль. Прежде здесь стоял
 * перечень исключений («перемещать нечем»), тогда как перемещаемых ресурсов во
 * всём продукте два: при таком умолчании каждый новый ресурс получал заглушку
 * перемещения САМ, и автор ресурса о списке не знал. #561 (пользователь) был не
 * пропуском одной записи, а первым замеченным экземпляром класса — перепись
 * нашла 21 ресурс, которому меню предлагало несуществующую операцию.
 *
 * Истина — у контракта: `:move` объявлен ровно на двух REST-адресах nlb
 * (`/nlb/v1/networkLoadBalancers`, `/nlb/v1/targetGroups`). Сходимость этого
 * перечня с контрактом держит гейт `lib/move-verb-contract-parity.test.ts`: он
 * ВЫВОДИТ перечень из `proto/`, поэтому новый глагол включает пункт сам, а
 * снятый — выключает. Дописывать сюда ресурс, у которого глагола нет, гейт не
 * даст.
 */
const MOVE_CAPABLE = ["load-balancers", "target-groups"];

/**
 * Has this resource any row action at all?
 *
 * Mirrors the menu assembled below, so a read-only catalog resource does not get
 * a column holding a button that opens an empty menu. Networks are called out
 * because their menu carries "create subnet" even when the network itself is not
 * otherwise actionable.
 */
export function resourceHasRowActions(spec: ResourceSpec): boolean {
  return (
    spec.ops.update ||
    spec.ops.delete ||
    // Объявленный глагол — такое же действие строки, как правка и удаление.
    // Без этого слагаемого ресурс, у которого ЕДИНСТВЕННОЕ действие — глагол,
    // не получил бы столбца действий вовсе, и объявление осталось бы формой
    // без содержания: спека его несёт, а на экране его нет.
    (spec.rowVerbs?.length ?? 0) > 0 ||
    MOVE_CAPABLE.includes(spec.id) ||
    spec.id === "networks"
  );
}

export function RowActionsMenu({ spec, row, basePath, projectId, editAsPanel }: Props) {
  const navigate = useNavigate();
  const params = useParams();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);
  // Открытый глагол хранится КЛЮЧОМ, а не флагом: у ресурса их бывает несколько,
  // и флаг не сказал бы, который из них подтверждают.
  const [openVerb, setOpenVerb] = useState<string | null>(null);
  const selfId = useSelfUserId();

  const id = getByPath<string>(row, "id") ?? "";
  const name = getByPath<string>(row, "name") ?? id;
  const drillTarget = spec.childRoute ? spec.childRoute.replace(":id", id) : `${basePath}/${id}`;
  const drillIsChild = !!spec.childRoute;
  // Удаление/перемещение адресуют admin-плоскость, если она есть: DELETE по
  // публичному пути geo Region/Zone не смаршрутизирован.
  const editPath = `${mutationBasePath(spec)}/${id}`;

  const isDefaultSg = spec.id === "security-groups" && Boolean(getByPath<boolean>(row, "default_for_network"));
  const showDelete = spec.ops.delete && !isDefaultSg;

  const moveCapable = MOVE_CAPABLE.includes(spec.id);

  const isNetwork = spec.id === "networks";
  const currentProjectId = params.projectId ?? projectId ?? null;

  // Глаголы, применимые к ЭТОЙ строке. `null` от `resolve` означает «действие к
  // строке не относится вовсе» — и такой пункт не рисуется; недоступность с
  // названной причиной — это другое состояние, и оно остаётся видимым.
  const verbs = (spec.rowVerbs ?? [])
    .map((verb) => ({ verb, state: verb.resolve(row, { selfId }) }))
    .filter((entry): entry is { verb: (typeof entry)["verb"]; state: NonNullable<(typeof entry)["state"]> } =>
      entry.state !== null,
    );
  const openVerbState = verbs.find((v) => v.verb.key === openVerb)?.state ?? null;

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
    // Действия-глаголы ресурса. Подпись выключенного пункта несёт ПРИЧИНУ
    // подсказкой: пункт, выключенный молча, неотличим от возможности, которой
    // нет, и пользователь ищет её там, где её нет.
    ...verbs.map(({ verb, state }) => ({
      key: `verb-${verb.key}`,
      icon: state.icon,
      disabled: !!state.disabledReason,
      label: state.disabledReason ? (
        <Tooltip title={state.disabledReason}>
          <span>{state.label}</span>
        </Tooltip>
      ) : (
        state.label
      ),
      danger: state.danger,
      onClick: stop(() => {
        if (state.disabledReason) return;
        setOpenVerb(verb.key);
      }),
    })),
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

      {openVerbState && (
        <RowVerbDialog
          state={openVerbState}
          resourceId={spec.id}
          projectId={projectId}
          onClose={() => setOpenVerb(null)}
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
