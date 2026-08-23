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
  // Пользователь — членство в аккаунте, а не проектный ресурс: глагола
  // перемещения у него нет ни на контракте, ни на крае. Соседи по домену выше
  // стояли здесь с самого начала, а он был пропущен, и его строка предлагала
  // заглушку рядом с настоящим действием-глаголом («Запретить участие»).
  "users",
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
 * Есть ли у ресурса хоть одно действие строки?
 *
 * Предикат ЗАКРЫТ: меню появляется от НАЛИЧИЯ действия, а не от отсутствия
 * имени в перечне. Слагаемое `!MOVE_INCAPABLE.includes(spec.id)` отсюда снято
 * (#1081) — оно было открыто по умолчанию, то есть право на столбец получал
 * всякий, кого не внесли в закрытый список. Справочник, о котором забыли,
 * получал столбец с единственным пунктом «Переместить» — окном-заглушкой,
 * печатающим REST-вызов, какого у ресурса нет ни на контракте, ни на крае.
 * Тот же класс, что перечень, задающий «кому запрещено» вместо «кому
 * разрешено»: на пустом месте он разрешает всем.
 *
 * `MOVE_INCAPABLE` остался — им решается пункт меню «Переместить» ниже. «Можно
 * ли перемещать» и «нужен ли столбец действий» — разные вопросы, и путать их
 * значит выдавать заглушку за действие.
 *
 * Соответствие меню ниже держится так: «Просмотр» действием НЕ считается — он
 * повторяет ссылку в колонке идентичности, и столбец ради него был бы столбцом
 * без содержания.
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
    // Названное исключение с причиной: меню сети несёт «Создать подсеть» —
    // пункт, которого нет ни в `ops`, ни в `rowVerbs`, он собирается из id
    // самим меню. Перечень исключений выписан в пробе поимённо и растёт
    // только вместе с причиной.
    spec.id === "networks"
  );
}

/**
 * Кнопка действий строки — ОДНА форма на все строки и все ресурсы.
 *
 * Она стоит в КАЖДОЙ строке списка, поэтому яркой быть не вправе: иначе столбец
 * действий перетягивает внимание с данных, ради которых список и открыт. Но и
 * теряться она не должна — «видно у одних строк и не видно у других» читается
 * как «действие есть не у всех», хотя действия одинаковы у всей таблицы.
 * Отсюда вторичный тон, а не третичный: он тише имени ресурса и заметно
 * различим на поверхности без наведения.
 *
 * Значок не появляется по наведению и не зависит ни от строки, ни от состава
 * меню: наведение — способ УЗНАТЬ про действия, а не условие их существования,
 * и на сенсорном экране его нет вовсе.
 *
 * Размер задан явно, а не взят у размера `small`: тот меняется вместе с общей
 * высотой элементов управления (36), а здесь нужен размер ячейки — 30×30, чтобы
 * строка списка не выросла из-за столбца, в котором нет данных.
 *
 * Объявлен ОДНИМ объектом на модуль: одна форма для всех строк тогда не
 * обещание, а следствие — вида, зависящего от строки, взяться неоткуда.
 */
export const ROW_ACTION_TRIGGER: React.CSSProperties = {
  width: 30,
  height: 30,
  minWidth: 30,
  padding: 0,
  borderRadius: 6,
  color: "var(--kc-text-secondary)",
};

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

  const moveCapable = !MOVE_INCAPABLE.includes(spec.id);

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
          icon={<MoreOutlined />}
          onClick={(e) => e.stopPropagation()}
          aria-label="Действия"
          style={ROW_ACTION_TRIGGER}
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
