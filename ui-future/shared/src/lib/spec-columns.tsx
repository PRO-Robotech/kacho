// spec-columns — преобразование ResourceSpec.columns в Column<row> для ResourceTable.
// Та же логика, что в ResourceListPage, вынесена для переиспользования
// (например, на Subnet detail в tab "IP-адреса" мы рендерим Addresses-таблицу
// с теми же колонками, что и /projects/X/addresses).

import type { ReactNode } from "react";
import { Link } from "react-router";
import { Typography } from "antd";
import type { Column } from "@shared/components/organisms/ResourceTable";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { ResourceLink } from "@shared/components/molecules/ResourceLink";
import { getByPath, type ResourceColumn, type ResourceSpec } from "@shared/lib/resource-registry";
import { formatDateTime } from "@shared/lib/datetime";
import { referrerHref, referrerMeta } from "@shared/lib/referrer";
import { displayText } from "@shared/lib/display-text";

// Ре-экспорт для стабильности публичного API @shared/lib/spec-columns (route/label
// логика вынесена в чистый ./referrer для unit-тестируемости без antd-графа).
export { referrerHref, referrerMeta } from "@shared/lib/referrer";

// Маппинг kacho.cloud.reference.Reference.referrer.type → registry specId, чтобы
// рендерить потребителя как единую ссылку «иконка + имя» (RefNameLink) и иметь
// корректный detail-роут (включая network_interface → kacho-vpc).
const REFERRER_SPEC: Record<string, string> = {
  // Ключ — тип ссылки ТАК, КАК ЕГО ОТДАЁТ сервис: сервисы называют его в двух
  // формах — legacy underscore (`compute_instance`) и канонической dotted
  // `domain.resource` (`vpc.securityGroup` у потребителей набора префиксов).
  // Нормализация здесь НЕ применяется намеренно: dotted-тип storage-remote
  // (`compute.instance`) минует эту карту и линкуется прямым маршрутом
  // (`referrerHref`), потому что в СВОЁМ реестре того ресурса у него нет.
  "vpc.securityGroup": "security-groups",
  // Сеть-потребитель группы безопасности: та, для которой группа назначена
  // группой по умолчанию. Без этой записи она уходила в запасную ветку и
  // рисовалась сырым `network net-…` — при том что строкой выше на той же
  // карточке она же стоит нормальной ссылкой. Один предмет, два вида подряд:
  // со стороны это читается как задвоенный раздел (#446).
  network: "networks",
  compute_instance: "compute-instances",
  nlb_target_group: "target-groups",
  network_interface: "network-interfaces",
  network_load_balancer: "load-balancers",
  nlb_load_balancer: "load-balancers",
  load_balancer: "load-balancers",
};

// Опции для рендеринга generic-форматов, которым нужен контекст вокруг ячейки.
// Сейчас используется только `projectId` для построения SPA-ссылок в format:
// "references" (used_by → /projects/<projectId>/compute/instances/<id> и т.п.).
export interface FormatCellOpts {
  projectId?: string | null;
  /** База пути карточки для колонки имени. Нужна вложенным таблицам, где
   *  ресурс адресуется через родителя (`/networks/<id>/route-tables/<id>`). */
  nameBasePath?: string | null;
  /** Полный адрес карточки для строки — когда путь не «база + идентификатор»
   *  (список со своим `childRoute`). Задаётся ТЕМ ЖЕ выражением, каким прежде
   *  считался переход по клику на строку: иначе ссылка повела бы в другое место,
   *  чем вёл клик, и подмена одного другим молча сменила бы адресацию. */
  nameHref?: (row: Record<string, unknown>) => string | null;
  /** Иконка типа рядом с именем — только во ВЛОЖЕННЫХ таблицах, где в одном
   *  окне соседствуют разные типы. В списке самого ресурса тип и так назван
   *  заголовком страницы, и колонка иконок была бы столбцом одинаковых значков. */
  nameIcon?: boolean;
}

// ReferrerLink — общий рендер одного referrer'а как «{label} {id}» (plain text,
// no chip), где label — короткая type-метка с семантическим цветом текста, id —
// monospace (<Typography.Text code>, это не чип — просто моно-стиль). Всё
// обёрнуто в один <Link> если href доступен (compute_instance → SPA-route),
// либо в <span> для unknown referrer-типов (forward-compat fallback). Клик по
// link останавливает propagation, чтобы row-onClick в ResourceTable не
// триггерил navigation на parent-ресурс (см. ResourceTable.tsx — там есть
// дополнительный skip на closest('a'), это просто defense-in-depth).
export function ReferrerLink({
  projectId,
  referrer,
}: {
  projectId: string | null | undefined;
  referrer: { type?: string; id?: string } | undefined;
}): ReactNode {
  // Известный тип → единая ссылка «иконка + имя» через RefNameLink (резолв имени
  // + detail-роут). Неизвестный тип — forward-compat fallback (label + id ниже).
  const mappedSpec = referrer?.type ? REFERRER_SPEC[referrer.type] : undefined;
  if (mappedSpec && referrer?.id) {
    return <RefNameLink specId={mappedSpec} refId={referrer.id} projectId={projectId ?? undefined} maxChars={32} />;
  }
  const meta = referrerMeta(referrer?.type);
  const id = referrer?.id ?? "";
  const href = referrerHref(projectId, referrer);
  const inner = (
    <>
      <span style={{ color: meta.color, fontWeight: 500, fontSize: 12 }}>{meta.label}</span>
      <Typography.Text code style={{ fontSize: 12 }} title={id || undefined}>
        {id || "—"}
      </Typography.Text>
    </>
  );
  if (href) {
    return (
      <Link
        to={href}
        onClick={(e) => e.stopPropagation()}
        style={{ display: "inline-flex", alignItems: "baseline", gap: 4 }}
      >
        {inner}
      </Link>
    );
  }
  return <span style={{ display: "inline-flex", alignItems: "baseline", gap: 4 }}>{inner}</span>;
}

// reorderNameIdFirst — KAC-245: во всех таблицах первые две колонки по умолчанию
// Name (path="name"), затем ID (path="id"). Извлекаем эти колонки из spec (где бы
// они ни стояли) и ставим первыми, СОХРАНЯЯ их объекты (а значит и кастомные
// render — CopyableName/CopyableId); остальные колонки — в исходном порядке. Если
// name-колонки нет (системные справочники disk-types/compute-zones) — id остаётся
// первым (graceful). Идемпотентно для ресурсов, где порядок уже верный (VPC/compute).
export function reorderNameIdFirst(columns: ResourceColumn[]): ResourceColumn[] {
  const nameCol = columns.find((c) => c.path === "name");
  // Без name-колонки (системные справочники, IAM users) — НЕ выносим id
  // принудительно вперёд: сохраняем авторский порядок. У users первичный
  // идентификатор — email, он должен оставаться первой колонкой. Хойстинг id
  // имеет смысл только чтобы держать его рядом с Name.
  if (!nameCol) return columns;
  const idCol = columns.find((c) => c.path === "id");
  const lead: ResourceColumn[] = [nameCol];
  if (idCol) lead.push(idCol);
  const rest = columns.filter((c) => c !== nameCol && c !== idCol);
  return [...lead, ...rest];
}


// ResourceNameCell — имя ресурса в таблице как ССЫЛКА на его карточку: иконка
// типа + собственное содержимое колонки (обычно копируемое имя).
//
// Прежде имя было плоским текстом, а переход давал только клик по строке: у
// строки нет ни адреса, ни вида ссылки — её нельзя открыть в новой вкладке и по
// ней не видно, что она куда-то ведёт. Имя ресурса — самая частая точка перехода
// в консоли, и вести себя она должна как ссылка.
//
// Собственная отрисовка колонки СОХРАНЯЕТСЯ внутри ссылки, а не заменяется:
// копирование имени по клику остаётся (оно гасит событие и до перехода не
// доходит). Запроса здесь нет — имя пришло со строкой; тем и отличается от
// `RefNameLink`, который резолвит ЧУЖОЙ идентификатор.
function ResourceNameCell({
  spec,
  row,
  opts,
  identityPath,
}: {
  spec: ResourceSpec;
  row: Record<string, unknown>;
  opts: FormatCellOpts;
  /** Поле, по которому ресурс узнают: обычно `name`, у пользователя — почта. */
  identityPath: string;
}): ReactNode {
  const idRaw = getByPath(row, "id");
  const nameRaw = getByPath(row, identityPath);
  return (
    <ResourceLink
      specId={spec.id}
      id={typeof idRaw === "string" ? idRaw : ""}
      name={typeof nameRaw === "string" ? nameRaw : ""}
      href={opts.nameHref ? opts.nameHref(row) : opts.nameBasePath && idRaw ? `${opts.nameBasePath}/${typeof idRaw === "string" ? idRaw : ""}` : undefined}
      projectId={opts.projectId}
      icon={opts.nameIcon}
      copy
    />
  );
}

export function buildSpecColumns(spec: ResourceSpec, opts: FormatCellOpts = {}): Column<Record<string, unknown>>[] {
  const ordered = reorderNameIdFirst(spec.columns);
  // Колонка ИДЕНТИЧНОСТИ — та, по которой пользователь узнаёт ресурс и через
  // которую в него заходит. Обычно это `name`, но не у всех: у пользователя имени
  // нет вовсе, его узнают по почте. Правило «поле называется name» оставляло
  // такие ресурсы без перехода — поэтому при отсутствии `name` идентичностью
  // считается ПЕРВАЯ колонка, а её порядок уже нормализован выше.
  const identityPath = ordered.find((c) => c.path === "name")?.path ?? ordered[0]?.path;
  return ordered.map((c) => ({
    header: c.header,
    className: c.className,
    // Ширина — БОЛЬШЕЕ из «сколько нужно значению» и «сколько нужно подписи».
    // Ширины по типу одного заголовка не знают, и подпись вроде «Группа
    // безопасности по умолчанию» в 220 точек не помещалась — она переносилась на
    // вторую строку, поднимая шапку таблицы, при том что свободного места справа
    // оставалось полэкрана.
    width: c.path === identityPath ? undefined : columnWidth(c),
    // Набор значений и набор ссылок рисуются столбиком — обрезка клетки в одну
    // строку показала бы из них только первое.
    multiline: c.multiline || c.format === "list" || c.format === "references",
    cell: (row) => {
      const inner = c.render ? c.render(row) : formatCellByFormat(c, row, opts);
      // Колонка имени — ссылка на карточку; остальное как объявлено спекой.
      return c.path === identityPath ? ResourceNameCell({ spec, row, opts, identityPath: identityPath ?? "name" }) : inner;
    },
    sortKey: c.format === "datetime" || c.format === "text" || c.format === "uid-short" ? c.path : undefined,
  }));
}

/**
 * Ширина колонки: большее из «сколько нужно значению» и «сколько нужно подписи».
 *
 * Оценка подписи приближённая, и это честно: измерить текст до отрисовки нельзя,
 * а измерять после — значит менять ширину уже показанной таблицы. Приближение
 * выбрано С ЗАПАСОМ: промах в большую сторону оставляет лишний воздух, промах в
 * меньшую переносит заголовок на вторую строку, то есть возвращает тот дефект.
 */
function columnWidth(c: ResourceColumn): number | undefined {
  const byFormat = widthForFormat(c.format);
  if (byFormat === undefined) return undefined;
  return Math.ceil(Math.max(byFormat, c.header.length * 7.6 + 34));
}

/**
 * Ширина колонки по типу её значения.
 *
 * Задавать её нужно ВСЕМ: колонка без ширины забирает весь остаток себе, и
 * распорка таблицы остаётся ни с чем — значения снова расходятся на треть экрана
 * каждое, а между адресом и зоной встаёт пустота.
 *
 * Значения одного типа во всех таблицах продукта одной длины, поэтому одинаковая
 * ширина делает списки узнаваемыми: дата находится там же, где нашлась на
 * прошлой странице.
 */
function widthForFormat(format: ResourceColumn["format"]): number | undefined {
  switch (format) {
    case "uid-short":
      return 200;
    case "datetime":
      return 180;
    case "status":
      return 140;
    case "bool":
      return 150;
    case "code":
      return 190;
    case "list":
      return 200;
    case "references":
      return 220;
    default:
      return 220;
  }
}

export function formatCellByFormat(
  c: ResourceColumn,
  row: Record<string, unknown>,
  opts: FormatCellOpts = {},
): ReactNode {
  const v = getByPath(row, c.path);
  switch (c.format) {
    case "bool":
      // Булево — ФАКТ о ресурсе, названный следствием (правило 6 `ui.md`). Без
      // этой ветки булево уезжало в умолчание и печаталось как `true` —
      // служебное слово вместо факта; так делал и общий реестр (колонка «По
      // умолчанию» у типа диска).
      //
      // Отсутствие значения и ложь — РАЗНЫЕ утверждения: первое про ответ
      // сервера, второе про ресурс. Поэтому непришедшее поле остаётся прочерком,
      // а не превращается в «нет».
      if (v == null) return <Typography.Text type="secondary">—</Typography.Text>;
      if (!c.boolLabels) return <Typography.Text type="secondary">—</Typography.Text>;
      return <BoolFact value={v} yes={c.boolLabels.yes} no={c.boolLabels.no} />;
    case "status":
      return <StatusBadge state={typeof v === "string" ? v : undefined} />;
    case "uid-short":
      return typeof v === "string" && v ? <CopyableId id={v} /> : <Typography.Text type="secondary">—</Typography.Text>;
    case "datetime":
      return typeof v === "string" && v ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {formatDateTime(v)}
        </Typography.Text>
      ) : (
        <Typography.Text type="secondary">—</Typography.Text>
      );
    case "code":
      return typeof v === "string" || typeof v === "number" ? (
        <Typography.Text code style={{ fontSize: 12 }}>
          {String(v)}
        </Typography.Text>
      ) : (
        <Typography.Text type="secondary">—</Typography.Text>
      );
    case "list":
      if (Array.isArray(v) && v.length > 0) {
        return (
          <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            {v.map((item, i) => (
              <span
                key={i}
                style={{
                  fontFamily: "ui-monospace, SFMono-Regular, monospace",
                  fontSize: 12,
                  whiteSpace: "nowrap",
                }}
              >
                {displayText(item)}
              </span>
            ))}
          </div>
        );
      }
      return <Typography.Text type="secondary">—</Typography.Text>;
    case "references":
      // Набор ссылок на использующие ресурсы — КАЖДАЯ своей строкой.
      //
      // Прежде показывался только первый, а остальные — подписью «ещё N»: числом,
      // из которого не узнать ни одного ресурса. Читатель всё равно шёл на
      // карточку, то есть свёртка стоила лишнего перехода на каждой строке — тот
      // же довод, по которому снята свёртка у набора адресов.
      //
      // Известный тип рисуется ссылкой, неизвестный — текстом: тип приезжает из
      // чужого домена, и выдумывать адрес для незнакомого нельзя.
      if (Array.isArray(v) && v.length > 0) {
        const projectId = opts.projectId ?? (getByPath<string>(row, "project_id") || null);
        const list = v as Array<{ referrer?: { type?: string; id?: string } }>;
        return (
          <span
            style={{
              display: "inline-flex",
              flexDirection: "column",
              alignItems: "flex-start",
              gap: 2,
              fontSize: 12,
              maxWidth: "100%",
            }}
          >
            {list.map((r, k) => (
              <ReferrerLink key={`${r.referrer?.id ?? "?"}-${k}`} projectId={projectId} referrer={r.referrer} />
            ))}
          </span>
        );
      }
      return <Typography.Text type="secondary">—</Typography.Text>;
    case "text":
    default:
      if (v == null || v === "") return <Typography.Text type="secondary">—</Typography.Text>;
      return displayText(v);
  }
}
