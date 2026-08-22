// ResourceTable — тонкая обёртка над antd Table.
//
// Сохраняет старый API (Column<T>, sortKey) для совместимости с
// ResourceListPage и тестами, но делегирует рендер в antd.
//
// Три решения о ВИДЕ списка принимаются здесь, потому что они про таблицу
// целиком, а не про отдельную колонку, и потому что нарушаются они одинаково у
// всех восьми ресурсов сразу:
//
//   * строка — одной высоты у всех, клетка — одной строки (`cellClip`);
//   * края закрепляются, но их стык называется ЛИНИЕЙ, а не тенью
//     (`pinnedEdge`);
//   * прокрутка вбок принадлежит таблице, а не странице.

import { type ReactNode, useMemo, useRef, useState, useEffect } from "react";
import { ConfigProvider, Table } from "antd";
import type { ColumnType, TableProps } from "antd/es/table";
import { getByPath } from "@shared/lib/path";
import { displayText } from "@shared/lib/display-text";
import { CELL_INSET, CELL_MAX_WIDTH, CellClip, showTitleWhenClipped } from "./cellClip";
import { TABLE_EDGE_THEME, pinnedEdgeStyle } from "./pinnedEdge";

// Ширина закреплённых колонок. Числа не декоративные: без явной ширины antd
// закрепление молча игнорирует (знание оплачено в registry и держится пробой, а
// не комментарием), а служебной колонке широкая ширина превращала бы её в пустую
// полосу через пол-экрана.
const SERVICE_WIDTH = 48;
const IDENTITY_WIDTH = 260;
const ACTIONS_WIDTH = 64;

export interface Column<T> {
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
  /** Path в row для local-sort. Если не задан — колонка не сортируется. */
  sortKey?: string;
  /**
   * Ширина колонки в точках.
   *
   * Задавать её ОБЯЗАТЕЛЬНО там, где содержимое короткое и предсказуемое (адрес,
   * зона, дата, состояние). Без ширины таблица делит доступное место поровну, и
   * четыре коротких значения на широком экране расходятся на треть экрана каждое
   * — между «10.10.1.0/24» и «ru-central1-a» встаёт триста точек пустоты, и
   * строка перестаёт читаться как одна строка: глаз не связывает её края.
   *
   * Остаток ширины забирает распорка (см. ниже), а не сами колонки.
   */
  width?: number;
  /**
   * Значение колонки занимает НЕСКОЛЬКО СТРОК и обрезке в одну не подлежит.
   *
   * Общая обрезка держит клетку в одну строку (`white-space: nowrap`), и набор
   * значений — блоки адресов подсети, ссылки на использующие ресурсы — она
   * схлопывала в первую строку: остальные были в разметке и не были видны.
   * Читатель делал вывод «у подсети один блок» и шёл на карточку проверять.
   *
   * Каждая строка внутри такой клетки не переносится сама по себе — за это
   * отвечает её ячейка (`CidrListCell` и подобные), поэтому высота строки
   * остаётся предсказуемой.
   */
  multiline?: boolean;
}

interface Props<T> {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
  loading?: boolean;
  defaultSort?: { col: number; dir: "asc" | "desc" };
  /** Если задан — клик по строке вызывает callback (для drill-down в detail).
   *  Cells, у которых внутри есть button/link с stopPropagation, не триггерят. */
  onRowClick?: (row: T) => void;
  /**
   * Полон ли переданный набор строк — ОБЯЗАТЕЛЬНО и без умолчания (#373).
   *
   * Сортируется МАССИВ переданных строк. Пока за курсором остались
   * непрочитанные страницы, стрелка упорядочивает случайную часть списка и
   * молча переставляет её при каждой догрузке: верхняя строка такой таблицы
   * первая среди прочитанных, а читается как первая вообще.
   *
   * Умолчания у этого поля нет намеренно. Прежде им было `sortable = true`, и
   * четырнадцать мест рендера из пятнадцати ничего не объявляли — то есть
   * молчание означало «набор полон» там, где никто этого не утверждал.
   * Требование ответа переносит решение туда, где факт известен: полноту знает
   * тот, кто читал курсор, а не таблица.
   *
   * Порядок серверу не заказывается: поле порядка снято с контракта осознанно
   * (`reserved order_by` — страница читается keyset-курсором по
   * `(created_at, id)`, и заказанный вызывающим порядок оставил бы курсор
   * описывать позицию в порядке, которого больше нет).
   */
  complete: boolean;
  /**
   * Строка, чьё содержимое раскрыто рядом (боковая панель): подсвечивается,
   * чтобы панель было видно, ОТКУДА она открыта.
   *
   * Выделения строк под ГРУППОВОЕ действие здесь нет и не бывает: удаление по
   * флажкам снято решением владельца для всех ресурсов, удаляют по одной строке
   * из её меню действий. Проп `selection` (и вместе с ним `rowSelection`
   * таблицы) стоял здесь после снятия единственного вызывающего — вход без
   * производителя: столбец флажков он не рисовал никогда, но читался как живая
   * возможность и приглашал провязать её обратно.
   *
   * Приехало из форка registry (#405): панель тегов образа. Второй проп того
   * форка — `stickyFirst` — не приехал, потому что предмета у него больше нет:
   * закрепление начального отрезка до колонки идентичности здесь безусловно.
   */
  selectedRowKey?: string | null;
}

export function ResourceTable<T extends object>({
  rows,
  columns,
  rowKey,
  empty,
  loading,
  defaultSort,
  onRowClick,
  complete,
  selectedRowKey,
}: Props<T>) {
  const antColumns: ColumnType<T>[] = useMemo(() => {
    // Края таблицы закрепляются, потому что широкая таблица прокручивается
    // вбок: уехав вправо, читатель иначе видит значения, не понимая, к какому
    // ресурсу они относятся, и не достаёт до меню действий, не вернувшись
    // обратно. Закрепляются РОВНО два края — колонка идентичности и столбец
    // действий (последний, узнаваемый по пустому заголовку). Закреплять больше
    // нельзя: на узком экране закреплённые края съедают всю видимую ширину.
    //
    // Слева закрепляется НАЧАЛЬНЫЙ ОТРЕЗОК до колонки идентичности включительно,
    // а не «первая колонка»: перед именем часто стоят служебные колонки (выбор
    // строки), и antd закрепляет только сплошной префикс — закрепив одну лишь
    // колонку имени, получаем разъезжающуюся таблицу.
    const last = columns.length - 1;
    const actionsLast = columns[last]?.header === "" && last > 0;
    const identity = columns.findIndex((c) => c.header !== "");
    const stickyThrough = identity < 0 || identity > 2 ? 0 : identity;

    const built = columns.map((c, idx) => {
      // Служебная колонка (выбор строки, столбец действий) заголовка не несёт.
      // Её содержимое — флажок и кнопка, то есть предмет не строчный: обрезать
      // его нечем, а высоту он задаёт сам и одинаково у всех строк.
      const service = c.header === "";
      const pinnedStart = idx <= stickyThrough;
      const pinnedEnd = idx === last && actionsLast;
      const width = pinnedStart ? (service ? SERVICE_WIDTH : IDENTITY_WIDTH) : pinnedEnd ? ACTIONS_WIDTH : undefined;
      // Клетка не шире, чем колонке отведено: у закреплённой ширина объявлена,
      // и предел берётся из неё, иначе содержимое растянуло бы столбец шире
      // объявленного — и закрепление разъехалось бы с тем, что показано.
      const clipWidth = width === undefined ? CELL_MAX_WIDTH : Math.max(width - CELL_INSET, 0);
      // Линия стоит на СТЫКЕ закрепления и прокручиваемой части: у левого
      // закрепления это его последняя колонка, у правого — сам столбец действий.
      const edge =
        pinnedStart && idx === stickyThrough
          ? pinnedEdgeStyle("start")
          : pinnedEnd
            ? pinnedEdgeStyle("end")
            : {};

      const col: ColumnType<T> = {
        title: c.header,
        key: String(idx),
        className: c.className,
        // Клетка идентичности — В ДВЕ СТРОКИ: под именем стоит идентификатор.
        // Общая обрезка держит одну строку (`white-space: nowrap`), и вторая в
        // неё не влезала вовсе — идентификатор был в разметке и не был виден.
        render: (_value, row) =>
          service ? (
            c.cell(row)
          ) : c.multiline || (pinnedStart && idx === stickyThrough) ? (
            <span style={{ display: "block", maxWidth: clipWidth, minWidth: 0 }}>{c.cell(row)}</span>
          ) : (
            <CellClip maxWidth={clipWidth}>{c.cell(row)}</CellClip>
          ),
        // Клетки одной строки стоят по центру её высоты: столбец действий выше
        // строки текста, и без этого текст сидел бы по верхнему краю строки, а
        // кнопка — по середине.
        onCell: () => ({ style: { verticalAlign: "middle", ...edge } }),
        onHeaderCell: () => ({ style: { ...edge } }),
      };
      if (width !== undefined) col.width = width;
      else if (c.width !== undefined) col.width = c.width;
      if (pinnedStart) col.fixed = "left";
      else if (pinnedEnd) col.fixed = "right";

      if (c.sortKey && complete) {
        col.sorter = (a: T, b: T) => {
          const av = getByPath(a, c.sortKey!);
          const bv = getByPath(b, c.sortKey!);
          if (av == null && bv == null) return 0;
          if (av == null) return 1;
          if (bv == null) return -1;
          if (typeof av === "number" && typeof bv === "number") return av - bv;
          return displayText(av).localeCompare(displayText(bv));
        };
        if (defaultSort && defaultSort.col === idx) {
          col.defaultSortOrder = defaultSort.dir === "asc" ? "ascend" : "descend";
        }
      }
      return col;
    });

    // РАСПОРКА — колонка без содержимого, забирающая остаток ширины.
    //
    // Без неё antd делит свободное место между содержательными колонками, и на
    // широком экране четыре коротких значения расходятся на треть экрана каждое:
    // между адресом и зоной встаёт триста точек пустоты, и строка перестаёт
    // читаться как одна строка — глаз не связывает её края.
    //
    // Ставится ПЕРЕД столбцом действий (тот закреплён справа), и только когда у
    // всех содержательных колонок ширина известна: иначе остаток забирать не у
    // чего, и пустая колонка просто съела бы место у той, что должна тянуться.
    const lastIdx = columns.length - 1;
    const actionsLastCol = columns[lastIdx]?.header === "" && lastIdx > 0;
    const contentCols = actionsLastCol ? columns.slice(0, lastIdx) : columns;
    const allSized = contentCols.length > 0 && contentCols.every((c, i) => c.width !== undefined || i <= stickyThrough);
    if (allSized) {
      const spacer: ColumnType<T> = {
        title: "",
        key: "spacer",
        // Пустая клетка не объявляется заголовком колонки для тех, кто читает
        // страницу не глазами: сообщать там нечего.
        render: () => null,
        onCell: () => ({ style: { padding: 0 } }),
      };
      built.splice(actionsLastCol ? lastIdx : built.length, 0, spacer);
    }
    return built;
  }, [columns, defaultSort, complete]);

  // Тело таблицы скроллится внутри поверхности (h+v), а шапка колонок
  // (thead) фиксирована сверху. scroll.y = высота доступной области минус thead;
  // пересчитывается ResizeObserver'ом при изменении размеров окна/области.
  // Пока область не измерена (первый рендер) — y=undefined (обычный поток).
  const wrapRef = useRef<HTMLDivElement>(null);
  const [scrollY, setScrollY] = useState<number | undefined>(undefined);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const recompute = () => {
      // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion -- ложное срабатывание: querySelector<E extends Element = Element>, и E выводится из самого утверждения типа. Без него E = Element, у которого нет offsetHeight (проверено tsc: удаление даёт TS2339).
      const thead = el.querySelector(".ant-table-thead") as HTMLElement | null;
      const theadH = thead?.offsetHeight ?? 40;
      const avail = el.clientHeight - theadH;
      setScrollY(avail > 48 ? avail : undefined);
    };
    const ro = new ResizeObserver(recompute);
    ro.observe(el);
    recompute();
    return () => ro.disconnect();
  }, []);

  const tableProps: TableProps<T> = {
    columns: antColumns,
    dataSource: rows,
    rowKey: (row) => rowKey(row),
    pagination: false,
    size: "small",
    // kc-table — строки разделяет ЛИНИЯ, а не заливка: полосатость снята
    // (index.css), потому что она спорила с границей строки и давала третий тон
    // там, где палитра держит два. Остальное там же: приглушённая шапка,
    // комфортная высота строки, наведение. Правило подсветки выбранной строки
    // (`kc-row-selected`) тоже живёт в общем листе — здесь оно не дублируется.
    className: "kc-table",
    // scroll.x=max-content — столбец держит ширину своего содержимого, но не
    // шире предела клетки (`CELL_MAX_WIDTH`): без предела одно длинное описание
    // растянуло бы столбец на две тысячи точек и увезло вбок таблицу целиком.
    // Прокрутка вбок остаётся у САМОЙ таблицы (страницу она не тянет), а край
    // закрепления назван линией — см. `pinnedEdge`. scroll.y — тело
    // скроллится вертикально под фиксированной шапкой колонок.
    scroll: { x: "max-content", y: scrollY },
    loading,
    locale: {
      emptyText: empty ?? "Ресурсов не найдено",
    },
    rowClassName: selectedRowKey ? (row) => (rowKey(row) === selectedRowKey ? "kc-row-selected" : "") : undefined,
    onRow: onRowClick
      ? (row) => ({
          onClick: (e) => {
            // Click внутри button / link / dropdown-menu / modal / form-control —
            // НЕ триггерит row-navigation. Иначе клик на kebab в action-cell
            // съедает Delete/Move (state ставится, но компонент unmount'ится).
            const target = e.target as HTMLElement | null;
            if (target?.closest("button, a, input, select, textarea, .ant-dropdown, .ant-select, .ant-modal-root")) {
              return;
            }
            onRowClick(row);
          },
          style: { cursor: "pointer" },
        })
      : undefined,
  };

  // Обёртка заполняет доступную высоту (flex:1 родителя) — от неё считается
  // scroll.y, чтобы тело таблицы скроллилось внутри поверхности.
  //
  // Обработчик наведения стоит ОДИН на всю поверхность: он договаривает
  // подсказкой то, что не поместилось в клетку. Подписка на каждую клетку стоила
  // бы столько же, сколько измерение, — то есть числа строк, помноженного на
  // число колонок (см. `cellClip`).
  return (
    <div
      ref={wrapRef}
      className="kc-table-fill"
      style={{ height: "100%", minHeight: 0, minWidth: 0 }}
      onMouseOver={showTitleWhenClipped}
      // Тот же обработчик и на ФОКУС: подсказка объясняет, что значение в ячейке
      // обрезано, — и человеку, ведущему фокус с клавиатуры, она нужна ровно так
      // же, как ведущему указатель. Без этой строки обрезанное значение было бы
      // нечитаемо для того, кто мышью не пользуется.
      onFocus={showTitleWhenClipped}
    >
      <ConfigProvider theme={TABLE_EDGE_THEME}>
        <Table<T> {...tableProps} />
      </ConfigProvider>
    </div>
  );
}
