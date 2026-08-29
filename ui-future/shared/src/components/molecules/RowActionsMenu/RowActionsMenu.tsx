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
import { useContext } from "@shared/lib/context-store";
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
 * Сколько у ресурса НАСТОЯЩИХ действий строки?
 *
 * Один счёт отвечает на два вопроса сразу — «нужен ли столбец действий» (больше
 * нуля) и «прячется ли одиночное действие за кебабом» (ровно одно, #687).
 * Раздельные предикаты об одном предмете разошлись бы молча: столбец появлялся
 * бы от одного набора слагаемых, а форма кнопки решалась бы другим.
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
export function specRowActionCount(spec: ResourceSpec): number {
  return (
    (spec.ops.update ? 1 : 0) +
    (spec.ops.delete ? 1 : 0) +
    // Объявленный глагол — такое же действие строки, как правка и удаление.
    // Без этого слагаемого ресурс, у которого ЕДИНСТВЕННОЕ действие — глагол,
    // не получил бы столбца действий вовсе, и объявление осталось бы формой
    // без содержания: спека его несёт, а на экране его нет.
    (spec.rowVerbs?.length ?? 0) +
    // Названное исключение с причиной: меню сети несёт «Создать подсеть» —
    // пункт, которого нет ни в `ops`, ни в `rowVerbs`, он собирается из id
    // самим меню. Перечень исключений выписан в пробе поимённо и растёт
    // только вместе с причиной.
    (spec.id === "networks" ? 1 : 0)
  );
}

export function resourceHasRowActions(spec: ResourceSpec): boolean {
  return specRowActionCount(spec) > 0;
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
/**
 * Действие строки — ОДНО описание для обеих форм показа: пункта меню и кнопки с
 * подписью. Общая форма нужна не ради краткости: пока описаний было два, «что
 * показать» и «сколько их» решались разными выражениями и расходились молча.
 */
interface RowAction {
  key: string;
  icon: React.ReactNode;
  /** Подпись — СТРОКА: она становится доступным именем инлайн-кнопки. */
  label: string;
  /** Необратимое либо отнимающее доступ действие — красный тон. */
  danger?: boolean;
  /** Действие к строке неприменимо, и это ПРИЧИНА — она показывается подсказкой. */
  disabledReason?: string;
  run: () => void;
}

export const ROW_ACTION_TRIGGER: React.CSSProperties = {
  width: 30,
  height: 30,
  minWidth: 30,
  padding: 0,
  borderRadius: 6,
  color: "var(--kc-text-secondary)",
};

/**
 * Кнопка ЕДИНСТВЕННОГО действия строки.
 *
 * Высота — та же 30, что у значка: столбец действий не вправе поднимать строку
 * списка, каким бы способом он ни показан. Горизонтальные поля появляются
 * оттого, что здесь есть подпись, а у значка её нет.
 *
 * Цвет НЕ задан намеренно: тон выбирает `danger` самого действия, а не строка.
 * Заданный здесь цвет перебил бы красный у удаления — единственного действия,
 * о цене которого стоит знать до нажатия (правило 6 канона консоли).
 */
export const ROW_ACTION_INLINE: React.CSSProperties = {
  height: 30,
  padding: "0 8px",
  borderRadius: 6,
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
  // Аккаунт выбранной области — вторая половина предмета у глаголов, чей
  // предмет ПАРА (исключение человека из аккаунта). Берётся из того же
  // хранилища области, что и у страниц: выводить его из строки нельзя — у
  // человека аккаунтов бывает несколько, а на строке личности их больше нет.
  const scopeAccountId = useContext((st) => st.account)?.id;

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
    .map((verb) => ({ verb, state: verb.resolve(row, { selfId, accountId: scopeAccountId }) }))
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

  // Действия строки собираются ОДНИМ перечнем, а вид выбирается по нему же.
  // Иначе «сколько действий» и «что показать» стали бы двумя местами об одном
  // предмете: пункт, добавленный в меню и забытый в счёте, вернул бы кебаб над
  // единственным действием — то есть ровно дефект #687.
  const realActions: RowAction[] = ([
    isNetwork && currentProjectId
      ? {
          key: "create-subnet",
          icon: <PlusOutlined />,
          label: "Создать подсеть",
          // editAsPanel: форма-панель в зоне 3 shell сети (child-create).
          // Иначе (legacy list-модалка): ?modal-флаг над текущей страницей.
          run: () =>
            void navigate(
              editAsPanel
                ? `/projects/${currentProjectId}/vpc/networks/${id}/subnets/create`
                : `/projects/${currentProjectId}/vpc/networks/${id}?modal=subnets-create&networkId=${id}`,
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
          run: () =>
            void navigate(editAsPanel ? `${basePath}/${id}/edit` : `${basePath}?modal=${spec.id}-edit&id=${id}`),
        }
      : null,
    // Действия-глаголы ресурса. Подпись выключенного пункта несёт ПРИЧИНУ
    // подсказкой: пункт, выключенный молча, неотличим от возможности, которой
    // нет, и пользователь ищет её там, где её нет.
    ...verbs.map(({ verb, state }) => ({
      key: `verb-${verb.key}`,
      icon: state.icon,
      label: state.label,
      danger: state.danger,
      disabledReason: state.disabledReason,
      run: () => setOpenVerb(verb.key),
    })),
    showDelete
      ? {
          key: "delete",
          icon: <DeleteOutlined />,
          label: "Удалить",
          danger: true,
          run: () => setDeleteOpen(true),
        }
      : null,
  ] as (RowAction | null)[]).filter((a): a is RowAction => a !== null);

  /** Пункт меню из действия — подпись, значок, причина недоступности, тон. */
  const toMenuItem = (action: RowAction) => ({
    key: action.key,
    icon: action.icon,
    disabled: !!action.disabledReason,
    label: action.disabledReason ? (
      <Tooltip title={action.disabledReason}>
        <span>{action.label}</span>
      </Tooltip>
    ) : (
      action.label
    ),
    danger: action.danger,
    onClick: stop(() => {
      if (action.disabledReason) return;
      action.run();
    }),
  });

  const byKey = (key: string) => realActions.find((a) => a.key === key);
  const verbItems = realActions.filter((a) => a.key.startsWith("verb-"));
  const createSubnet = byKey("create-subnet");
  const edit = byKey("edit");
  const remove = byKey("delete");

  const items: MenuProps["items"] = [
    {
      key: "open",
      icon: drillIsChild ? <ArrowRightOutlined /> : <EyeOutlined />,
      label: drillIsChild ? "Открыть" : "Просмотр",
      onClick: stop(() => void navigate(drillTarget)),
    },
    createSubnet ? toMenuItem(createSubnet) : null,
    edit ? toMenuItem(edit) : null,
    ...verbItems.map(toMenuItem),
    moveCapable
      ? {
          key: "move",
          icon: <DragOutlined />,
          label: "Переместить",
          onClick: stop(() => setMoveOpen(true)),
        }
      : null,
    remove ? { type: "divider" as const } : null,
    remove ? toMenuItem(remove) : null,
  ].filter(Boolean);

  // Ровно одно настоящее действие — оно показывается кнопкой с подписью, и
  // нажатие остаётся одно вместо двух (#687).
  //
  // Решает СПЕКА, а не строка. Столбец заводится спекой на всю таблицу, и
  // «у одной строки кнопка, у соседней значок» читается как «действие есть не у
  // всех» — тот дефект, против которого написан `RowActionsMenu.trigger.test`.
  // Поэтому у группы безопасности по умолчанию (её строка теряет удаление)
  // значок остаётся. Вторая половина условия — про саму строку: если
  // единственное действие к ней не относится вовсе (глагол вернул `null`),
  // рисовать инлайн нечего.
  const inlineAction = specRowActionCount(spec) === 1 && realActions.length === 1 ? realActions[0] : null;

  const inlineButton = inlineAction ? (
    <Button
      type="text"
      icon={inlineAction.icon}
      danger={inlineAction.danger}
      disabled={!!inlineAction.disabledReason}
      onClick={(e) => {
        e.stopPropagation();
        if (inlineAction.disabledReason) return;
        inlineAction.run();
      }}
      style={ROW_ACTION_INLINE}
    >
      {inlineAction.label}
    </Button>
  ) : null;

  return (
    <>
      {inlineAction ? (
        // Причина недоступности остаётся видимой: кнопка, выключенная молча,
        // неотличима от возможности, которой нет.
        inlineAction.disabledReason ? (
          <Tooltip title={inlineAction.disabledReason}>
            <span>{inlineButton}</span>
          </Tooltip>
        ) : (
          inlineButton
        )
      ) : (
        <Dropdown menu={{ items }} trigger={["click"]} placement="bottomRight">
          <Button
            type="text"
            icon={<MoreOutlined />}
            onClick={(e) => e.stopPropagation()}
            aria-label="Действия"
            style={ROW_ACTION_TRIGGER}
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
