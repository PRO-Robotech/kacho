// SgRulesPanel — управление правилами Security Group (KAC-239).
//
// Один таб «Правила» (INGRESS+EGRESS вместе; направление — первый столбец).
// Режимы: list (таблица: чекбоксы + per-row ⋮ Редактировать/Удалить + bulk-delete)
// ↔ edit (редактор правил через SgRulesEditor; direction выбирается в самом
// правиле). Каждая операция — UpdateRules по стабильному id:
//   • add    → { addition_rule_specs: [...] }
//   • edit   → { deletion_rule_ids: [id], addition_rule_specs: [spec] }
//   • delete → { deletion_rule_ids: [...] }

import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Checkbox, Dropdown, Modal, Typography } from "antd";
import { MoreOutlined, EditOutlined, DeleteOutlined, PlusOutlined, ExclamationCircleFilled } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { DirectionFact } from "@shared/components/atoms/DirectionFact";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { ROW_ACTION_TRIGGER } from "@shared/components/molecules/RowActionsMenu";
import { ResourceTable } from "@shared/components/organisms/ResourceTable";
// ПРЯМО ИЗ ФАЙЛА, а не через баррель `DetailShell/index.ts`.
//
// Баррель реэкспортирует и сам `DetailShell`, а тот тянет `react-router`, —
// пакет с ESM-сборкой, который jest в этом дереве не трансформирует
// (`transformIgnorePatterns` оставлен по умолчанию). Через баррель эта
// зависимость приезжала к КАЖДОМУ, кто импортирует форму: цепочка
// `resource-registry → RoutesEditor → RefSelect → InlineResourceCreateForm →
// ResourceFormBody → FormShell → DetailShell → react-router` кладёт суиту
// целиком — не «падает проба», а «suite failed to run», то есть вердикта нет
// ни у одной пробы файла. Замер в момент починки: 153 суиты из 219 не
// исполнялись вовсе.
//
// Шапка страницы про маршрутизацию не знает и знать не должна — импорт из
// файла оставляет её зависимости её собственными.
import { PageHead } from "@shared/components/organisms/DetailShell/PageHead";
import { useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { RuleBody, emptyRule, type RuleExt } from "@shared/components/organisms/form/SgRulesEditor";
import { MONO_FONT, editorSurfaceStyle } from "@shared/components/organisms/form/editor-surface";
import { ruleFieldError, type RuleFieldError } from "@shared/components/organisms/form/SgRulesEditor/rule-field-error";
import { hasProtocolNumber, REGISTRY, sanitizeSgRule } from "@shared/lib/resource-registry";
import { createActionLabel } from "@shared/lib/resource-label";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";

/**
 * Место полной замены набора. Правка правила — это `deletion_rule_ids` +
 * `addition_rule_specs`, то есть правило пересоздаётся ЦЕЛИКОМ из черновика:
 * поле контракта, которого черновик не назвал, у этого правила исчезает.
 * Состав обоих типов сверяется с `SecurityGroupRuleSpec` гейтом
 * `test/set-replacement-draft-composition`.
 */
export const SG_RULE_SPECS_REPLACEMENT: SetReplacementDraft = {
  field: "addition_rule_specs",
  contract: "kacho/cloud/vpc/v1/security_group_service.proto",
  message: "SecurityGroupRuleSpec",
  drafts: ["SgRule", "RuleExt"],
};

export interface SgRule {
  id?: string;
  direction?: string;
  description?: string;
  /**
   * Метки правила — НАЗВАНЫ, а не оставлены на индексную сигнатуру ниже: правка
   * пересоздаёт правило целиком из этого объекта, и поле, которого он не
   * называет, у правила исчезает.
   */
  labels?: Record<string, string>;
  protocol_name?: string;
  // int64 on the wire → a JSON string when it comes from the server.
  protocol_number?: number | string;
  ports?: { from_port?: number | string; to_port?: number | string };
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  /** Третья ветвь `oneof target` — ссылка на именованный набор префиксов. */
  cidr_group_id?: string;
  [k: string]: unknown;
}

interface Props {
  sgId: string;
  projectId: string | null;
  /** Все правила SG (из detail) — оба направления в одной таблице. */
  rules: SgRule[];
  /** KAC-243 (scenario 18): network_id редактируемой SG. SG-target picker в
   *  редакторе правил показывает только SG из этой же сети. */
  networkId?: string;
}

function dirOf(r: SgRule): "INGRESS" | "EGRESS" {
  return (r.direction ?? "INGRESS").toUpperCase() === "EGRESS" ? "EGRESS" : "INGRESS";
}
function protoLabel(r: SgRule): string {
  if (r.protocol_name) return r.protocol_name;
  if (hasProtocolNumber(r.protocol_number)) return `proto ${r.protocol_number}`;
  // «Любой» — тем же словом, каким этот же выбор назван в самой форме правила
  // (`Протокол → Любой`). Здесь стояло `Any`: единственное английское слово на
  // русской вкладке, и означало оно ровно то же самое. Одно значение, названное
  // на экране двумя языками, читается как два разных значения.
  return "Любой";
}
function portsLabel(r: SgRule): string {
  if (!r.ports) return "—";
  const f = r.ports.from_port;
  const t = r.ports.to_port;
  if (f == null && t == null) return "—";
  if (f === t || t == null) return String(f);
  return `${f}–${t}`;
}
/**
 * Предмет формы — ПРАВИЛО, а не группа безопасности, чью карточку мы правим.
 *
 * Падеж объявлен, а не выведен из строки: русское склонение по окончанию —
 * правило с исключениями, и вывод по хвосту слова ошибается молча (см. разбор
 * в `resource-label`). Правило — не запись реестра ресурсов, поэтому падеж
 * стоит здесь; способ сборки подписи при этом остаётся общий.
 */
const RULE_SUBJECT = { singular: "Правило", accusative: "правило" };

/** Блоки правила обоих семейств — в том порядке, в каком их называет контракт. */
function cidrBlocks(r: SgRule): string[] {
  return [...(r.cidr_blocks?.v4_cidr_blocks ?? []), ...(r.cidr_blocks?.v6_cidr_blocks ?? [])];
}

function targetParts(r: SgRule): { kind: string; value: string } {
  if (r.cidr_blocks) {
    return { kind: "CIDR", value: cidrBlocks(r).join(", ") || "—" };
  }
  if (r.security_group_id) return { kind: "Группа безопасности", value: r.security_group_id };
  if (r.cidr_group_id) return { kind: "Набор префиксов", value: r.cidr_group_id };
  // Прочерк здесь означает правило БЕЗ цели — по закрытой модели оно не разрешает
  // ничего. Такое правило край больше не принимает (сервис отвергает с указанием
  // поля `<путь>.target`), поэтому прочерк остался ровно для строк, сохранённых
  // прежним контрактом; миграция 0029 приводит их к выразимому виду.
  return { kind: "—", value: "—" };
}

/**
 * Машинное значение — моноширинным рядом: адрес, префикс, диапазон портов.
 *
 * Задаётся ТОЛЬКО начертание, без своего кегля: клетки одной строки, набранные
 * разным размером, ломают её общую линию. Тот же выбор, что у моноширинных
 * клеток редакторов (`editorValueCellStyle` там задаёт и кегль — там строка
 * своя, а здесь она общая с остальными столбцами таблицы).
 */
function Mono({ children }: { children: ReactNode }) {
  return <span style={{ fontFamily: MONO_FONT }}>{children}</span>;
}

// Цель-ССЫЛКА показывается ссылкой (канон консоли, правило 2): иконка типа, имя,
// переход. Обе ссылочные ветви — группа безопасности и набор префиксов — рисуются
// ОДИНАКОВО: оставить одну ссылкой, а другую моноширинным идентификатором значило
// бы показать один предмет двумя видами. Набор блоков ссылкой не становится —
// ссылаться там не на что.
function targetCell(r: SgRule, projectId: string | null): ReactNode {
  if (r.security_group_id) {
    return <RefNameLink specId="security-groups" refId={r.security_group_id} projectId={projectId ?? undefined} />;
  }
  if (r.cidr_group_id) {
    return <RefNameLink specId="cidr-groups" refId={r.cidr_group_id} projectId={projectId ?? undefined} />;
  }
  const blocks = cidrBlocks(r);
  if (blocks.length === 0) return targetParts(r).value;
  // Блоки — КАЖДЫЙ своей строкой (тот же вид, что у набора значений в остальных
  // списках консоли, `format: "list"`). Прежде они склеивались запятой в одну
  // строку, а общая обрезка клетки держит её в одну строку: из трёх блоков
  // читатель видел первый и многоточие и шёл на карточку проверять, есть ли там
  // ещё. Моноширинный ряд здесь и был обещан комментарием — но не задан ничем.
  return (
    <span
      style={{ display: "inline-flex", flexDirection: "column", alignItems: "flex-start", gap: 2, maxWidth: "100%" }}
    >
      {blocks.map((b) => (
        <Mono key={b}>{b}</Mono>
      ))}
    </span>
  );
}

// Отвечают ли мутации группы безопасности операцией — свойство РЕСУРСА, и
// объявляет его спека. Величина взята на уровне модуля, а не в теле функции:
// её читает обработчик под `useCallback`, и чтение поля спеки внутри него
// потребовало бы держать саму спеку в перечне зависимостей — то есть заводить
// зависимость там, где меняться нечему.
const SG_EXPECTS_OPERATION = REGISTRY["security-groups"].mutationsReturnOperation !== false;

export function SgRulesPanel({ sgId, projectId, rules, networkId }: Props) {
  const sgSpec = REGISTRY["security-groups"];
  const qc = useQueryClient();

  const [editObj, setEditObj] = useState<RuleExt | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null); // null = добавление
  const [selected, setSelected] = useState<Set<string>>(new Set());
  /** Отказ края по ОТКРЫТОЙ форме — показывается в ней, а не всплывающим сообщением. */
  const [formError, setFormError] = useState<RuleFieldError | null>(null);

  const mutation = useMutation({
    mutationFn: (payload: unknown) => api.update(`${sgSpec.apiPath}/${sgId}/rules`, payload),
  });
  const { mutateAsync } = mutation;

  const refresh = useCallback(() => qc.invalidateQueries({ queryKey: [sgSpec.id] }), [qc, sgSpec.id]);

  // Стабильна по той же причине, что и обработчики ниже: они попадают в узел,
  // который слот шапки кладёт в состояние, и новая функция на каждом рендере
  // означала бы новый узел на каждом рендере.
  /**
   * Отправка, которая ОТДАЁТ отказ вызывающему.
   *
   * Список показывает отказ всплывающим сообщением: терять там нечего, действие
   * умещается в один клик. Форма — не может: в ней набранное, и её отказ обязан
   * остаться на экране рядом с полем. Поэтому решение «как показать» принимает
   * вызывающий, а не эта функция.
   */
  const submit = useCallback(
    async (payload: { deletion_rule_ids?: string[]; addition_rule_specs?: unknown[] }, opTitle: string) => {
      const resp = await mutateAsync(payload);
      // Отказ ОТДАЁТСЯ вызывающему — тем же способом, каким эта функция уже
      // отдаёт отказ края (см. её шапку выше): решение «как показать»
      // принимает вызывающий, а не она.
      const resolved = resolveMutationResponse(resp, SG_EXPECTS_OPERATION);
      if (resolved.kind === "violation") throw new Error(resolved.message);
      if (resolved.kind === "operation") {
        operationStore.start({ id: resolved.opId, title: opTitle, resourceId: sgSpec.id, projectId });
      }
      void refresh();
    },
    [mutateAsync, projectId, refresh, sgSpec.id],
  );

  const runOp = useCallback(
    async (payload: { deletion_rule_ids?: string[]; addition_rule_specs?: unknown[] }, opTitle: string) => {
      try {
        await submit(payload, opTitle);
      } catch (err) {
        const m = errorText(err);
        toast.error(`Правило группы безопасности: ${m}`);
      }
    },
    [submit],
  );

  // Выбор — только правила с id (после backfill id есть у всех).
  const selectableIds = useMemo(() => rules.map((r) => r.id).filter((id): id is string => !!id), [rules]);
  const allSelected = selectableIds.length > 0 && selectableIds.every((id) => selected.has(id));
  const someSelected = selectableIds.some((id) => selected.has(id));
  const selCount = selectableIds.filter((id) => selected.has(id)).length;

  const toggleOne = (id: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  const toggleAll = useCallback((on: boolean) => setSelected(on ? new Set(selectableIds) : new Set()), [selectableIds]);

  const confirmDeleteSelected = useCallback(() => {
    const ids = selectableIds.filter((id) => selected.has(id));
    if (ids.length === 0) return;
    Modal.confirm({
      title: `Удалить выбранные правила (${ids.length})`,
      icon: <ExclamationCircleFilled />,
      content: "Действие необратимо.",
      okText: "Удалить",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: async () => {
        await runOp({ deletion_rule_ids: ids }, `Удаление правил группы безопасности (${ids.length})`);
        setSelected(new Set());
      },
    });
  }, [selectableIds, selected, runOp]);

  const confirmDelete = (r: SgRule) => {
    if (!r.id) return;
    Modal.confirm({
      title: "Удалить правило",
      icon: <ExclamationCircleFilled />,
      // Правило названо ТЕМ ЖЕ видом, каким оно стоит в строке списка:
      // направление — признаком, цель — моноширинным рядом. Прежде здесь
      // подставлялось машинное значение контракта (`INGRESS`), которого на
      // экране нет больше нигде: подтверждение спрашивают у человека, а слово
      // запроса адресовано не ему.
      content: (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <DirectionFact value={dirOf(r)} />
          <span aria-hidden>·</span>
          <span>{protoLabel(r)}</span>
          <span aria-hidden>·</span>
          <Mono>{targetParts(r).value}</Mono>
        </span>
      ),
      okText: "Удалить правило",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: () => runOp({ deletion_rule_ids: [r.id as string] }, "Удаление правила группы безопасности"),
    });
  };

  const startAdd = () => {
    setEditingId(null);
    setEditObj(emptyRule());
  };
  const startEdit = (r: SgRule) => {
    setEditingId(r.id ?? null);
    setEditObj({ ...(r as RuleExt) });
  };
  const cancelEdit = () => {
    setEditObj(null);
    setEditingId(null);
    setFormError(null);
  };

  const saveEdit = async () => {
    if (!editObj) {
      cancelEdit();
      return;
    }
    // Одно правило: direction/протокол/порты/источник — из самой формы (RuleBody).
    const clean = sanitizeSgRule({ ...(editObj as Record<string, unknown>) });
    delete clean.id;
    const payload: { deletion_rule_ids?: string[]; addition_rule_specs?: unknown[] } = {
      addition_rule_specs: [clean],
    };
    if (editingId) payload.deletion_rule_ids = [editingId]; // edit = delete+add

    // Форма закрывается ТОЛЬКО после успеха. Прежде `cancelEdit()` стоял ЗДЕСЬ,
    // до отправки, и отказ края приходил в размонтированную форму: набранное
    // правило пропадало целиком, а причину показывали всплывающим сообщением —
    // рядом с пустым списком, где чинить уже нечего.
    setFormError(null);
    try {
      await submit(
        payload,
        editingId ? "Изменение правила группы безопасности" : "Добавление правила группы безопасности",
      );
      cancelEdit();
    } catch (err) {
      setFormError(ruleFieldError(err));
    }
  };

  // ── режим редактора ОДНОГО правила — плоская форма (RuleBody), без Collapse ──
  // Хук зовётся ДО ветки формы: порядок вызовов обязан быть одинаковым на
  // каждом рендере, поэтому в режиме формы передаётся null, а не пропускается.
  //
  // useMemo здесь ОБЯЗАТЕЛЕН, а не «для скорости»: слот кладёт узел в состояние
  // эффектом с зависимостью от самого узла. Новый JSX на каждом рендере даёт
  // бесконечный цикл — прогон проб на этом просто съел память и умер.
  const listActions = useMemo(
    () =>
      editObj ? null : (
        <>
          {/* Групповое действие и его выбор стоят ВМЕСТЕ, а основное действие —
              последним. Прежде «Добавить» стояло между флажком и «Удалить», то
              есть разбивало пару «что выбрано → что с этим сделать» на две
              половины по разные стороны от чужой кнопки.

              Пары контролов нет вовсе, пока правил нет: выключенный флажок и
              выключенное «Удалить» над пустой вкладкой обещают групповое
              действие над набором, которого не существует.

              «Выбрать все» стоит В ШАПКЕ, а не в заголовке столбца — и это два
              разных ограничения, а не одно. Заголовок столбца общей таблицы
              типизирован строкой (`Column.header: string`), узел в него не
              положить. Столбец флажков самой таблицы (`ResourceTable.selection`)
              рассмотрен и отвергнут: у него в дереве НОЛЬ вызывающих (то есть
              это не «как у всех», а новая форма), он не умеет закрывать выбор
              строке без `id`, а заменитель `antd` рисует его заголовок пустым —
              «выбрать все» стало бы ненаблюдаемо для проб. Флажок с подписью в
              правом слоте шапки — ровно то, чем страница списка показывает свои
              переключатели (`ResourceListPage`, `listFilters`). */}
          {rules.length > 0 && (
            <>
              <Checkbox
                checked={allSelected}
                indeterminate={someSelected && !allSelected}
                onChange={(e) => toggleAll(e.target.checked)}
                disabled={selectableIds.length === 0}
              >
                Выбрать все
              </Checkbox>
              <Button danger icon={<DeleteOutlined />} disabled={!someSelected} onClick={confirmDeleteSelected}>
                Удалить{selCount > 0 ? ` (${selCount})` : ""}
              </Button>
            </>
          )}
          <Button type="primary" icon={<PlusOutlined />} onClick={startAdd}>
            Добавить правило
          </Button>
        </>
      ),
    [
      editObj,
      rules.length,
      allSelected,
      someSelected,
      selectableIds.length,
      selCount,
      toggleAll,
      confirmDeleteSelected,
    ],
  );
  useHeaderRight(listActions);

  if (editObj) {
    // Та же оболочка формы, что у всех форм консоли: единая ширина, шапка
    // «действие + тип» с иконкой ресурса, подвал с кнопками. Прежде здесь была
    // своя разметка — отсюда и другая ширина, и другие отступы, и кнопки не на
    // общем месте.
    return (
      <FormShell specId="security-groups" mode={editingId ? "edit" : "create"} singular="правило">
        {/* Форма НАЗЫВАЕТ СВОЁ ДЕЙСТВИЕ — и делает это сама.
            `FormShell` внутри карточки ресурса шапку не рисует намеренно: там её
            показывает зона 2, куда `ResourceShell` кладёт действие своей формы
            правки. У этой панели зоны 2 нет — она меняет содержимое ВКЛАДКИ, а
            зона 2 продолжает называть саму группу безопасности. Поэтому на
            экране форма правила выходила без единой подписи: под вкладкой
            «Правила» появлялся голый столбец полей, и «создаю» от «меняю»
            отличалось только надписью на кнопке подвала.
            Шапка — та же `PageHead`, что у всех форм консоли, и подпись
            собирается ТЕМ ЖЕ способом: глагол плюс предмет в винительном падеже
            (`createActionLabel`), как это делает `FormShell` для всех остальных
            форм. Здесь стояло «Создание правила» — форма, которой в консоли
            больше нет ни у одной формы (везде «Создать подсеть», «Изменить
            сеть»), и вдобавок называвшая действие иначе, чем ЭТОТ ЖЕ файл
            двадцатью строками выше: кнопка списка «Добавить правило», операция
            «Добавление правила группы безопасности». Одно действие, названное на
            одном экране двумя словами, читается как два разных действия. */}
        <PageHead title={createActionLabel(RULE_SUBJECT, editingId ? "Изменить" : "Добавить")} />
        <RuleBody rule={editObj} onChange={setEditObj} editingNetworkId={networkId || undefined} />
        {formError && (
          // Причина стоит НАД подвалом — там, где на неё смотрят перед повторным
          // нажатием, и рядом с полями, а не поверх экрана.
          <div
            role="alert"
            style={{
              marginTop: 12,
              padding: "9px 12px",
              borderRadius: 8,
              border: "1px solid color-mix(in srgb, var(--kc-danger) 30%, transparent)",
              background: "color-mix(in srgb, var(--kc-danger) 8%, transparent)",
              color: "var(--kc-danger)",
              fontSize: 12,
            }}
          >
            {formError.field ? `${formError.field}: ${formError.message}` : formError.message}
          </div>
        )}
        <FormFooter
          submitLabel={editingId ? "Сохранить" : "Добавить"}
          submitting={mutation.isPending}
          onSubmit={saveEdit}
          onCancel={cancelEdit}
        />
      </FormShell>
    );
  }

  // ── режим списка ──
  return (
    // Вкладка-СПИСОК заполняет отведённую ей область и прокручивает СЕБЯ — та же
    // оболочка, что у встроенных таблиц дочерних ресурсов (`RelatedTable`).
    // Прежде здесь стоял обычный `<div>`: общая таблица меряет доступную высоту
    // от своего контейнера, у контейнера без высоты мерять нечего, и вместо
    // прокрутки тела под закреплённой шапкой прокручивалась вся вкладка вместе
    // с шапкой колонок.
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      {/* Шапку «Правила» показывает зона-3 (название таба); действия ушли в
          правый слот шапки СТРАНИЦЫ — туда же, где «Создать» у всех списков. */}
      {rules.length === 0 ? (
        // Пустой набор — УТВЕРЖДЕНИЕ о безопасности («не разрешено ничего»), а не
        // «здесь пока пусто», поэтому фраза остаётся дословно. Рядом с ней —
        // первый шаг: вкладка, называющая предмет и не дающая с ним ничего
        // сделать, отправляет читателя искать действие глазами по всему экрану.
        //
        // Кегль — строки, а не подписи столбца: прежде фраза набиралась рядом
        // подписей (11, третичный цвет), и единственное сообщение вкладки
        // читалось выключенным.
        <div
          style={{
            ...editorSurfaceStyle,
            display: "grid",
            justifyItems: "center",
            gap: 14,
            padding: "40px 16px",
          }}
        >
          <span style={{ fontSize: 13, color: "var(--kc-text-secondary)" }}>
            Правил нет — трафик блокируется (default-deny).
          </span>
          <Button type="primary" icon={<PlusOutlined />} onClick={startAdd}>
            Добавить правило
          </Button>
        </div>
      ) : (
        // Таблица правил — та же `ResourceTable`, что у всех списков консоли.
        // Прежде здесь стояла СВОЯ разметка с собственными классами: она
        // отличалась от остальных таблиц шапкой, отступами и поведением
        // сортировки, то есть один и тот же предмет — список строк ресурса —
        // выглядел на этой вкладке иначе, чем везде.
        <ResourceTable<SgRule>
          rows={rules}
          // Правила приезжают полем группы, а не списком у края: курсора нет,
          // набор полон by construction.
          complete
          rowKey={(r) => r.id ?? String(rules.indexOf(r))}
          columns={[
            {
              header: "",
              cell: (row) => {
                const r = row;
                return (
                  <Checkbox
                    checked={!!r.id && selected.has(r.id)}
                    disabled={!r.id}
                    onChange={(e) => r.id && toggleOne(r.id, e.target.checked)}
                  />
                );
              },
            },
            {
              header: "Направление",
              cell: (row) => <DirectionFact value={dirOf(row)} />,
            },
            // Ширина объявлена у столбцов с КОРОТКИМ и предсказуемым значением, а
            // «Источник» и «Описание» её не несут намеренно — они забирают
            // остаток. Так же устроены столбцы всех списков консоли
            // (`spec-columns`: ширина выводится из типа значения, длинные и
            // непредсказуемые поля оставлены содержимому), и числа взяты оттуда
            // же: 140 — ряд короткого значения из закрытого набора, 150 и 180 —
            // по длине самой подписи столбца.
            //
            // Прежде ширины не было НИ У ОДНОГО столбца, и таблица делила экран
            // между восемью поровну: между «TCP» и «80–443» вставало по трети
            // экрана пустоты, и строка переставала читаться как одна строка.
            { header: "Протокол", width: 140, cell: (row) => protoLabel(row) },
            { header: "Диапазон портов", width: 150, cell: (row) => <Mono>{portsLabel(row)}</Mono> },
            {
              header: "Тип источника",
              width: 180,
              cell: (row) => (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {targetParts(row).kind}
                </Typography.Text>
              ),
            },
            {
              header: "Источник",
              // Набор блоков стоит столбиком, поэтому клетка НЕ обрезается в одну
              // строку: общая обрезка показала бы из трёх блоков только первый.
              multiline: true,
              cell: (row) => targetCell(row, projectId),
            },
            {
              header: "Описание",
              cell: (row) => row.description || "—",
            },
            {
              header: "",
              className: "text-right whitespace-nowrap",
              cell: (row) => {
                const r = row;
                return (
                  <Dropdown
                    trigger={["click"]}
                    placement="bottomRight"
                    menu={{
                      items: [
                        { key: "edit", icon: <EditOutlined />, label: "Редактировать", onClick: () => startEdit(r) },
                        { type: "divider" as const },
                        {
                          key: "delete",
                          icon: <DeleteOutlined />,
                          label: "Удалить",
                          danger: true,
                          disabled: !r.id,
                          onClick: () => confirmDelete(r),
                        },
                      ],
                    }}
                  >
                    {/* Кнопка действий — ОДНА форма на все списки консоли
                        (`ROW_ACTION_TRIGGER`: 30×30, радиус 6, вторичный тон).
                        Здесь стоял `size="small"`, а он привязан к общей высоте
                        элементов управления (36) — то есть строка этой таблицы
                        росла из-за столбца, в котором нет данных. */}
                    <Button type="text" icon={<MoreOutlined />} aria-label="Действия" style={ROW_ACTION_TRIGGER} />
                  </Dropdown>
                );
              },
            },
          ]}
        />
      )}
    </div>
  );
}
