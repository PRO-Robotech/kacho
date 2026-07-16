// ResourceTable — тонкая обёртка над antd Table.
//
// Сохраняет старый API (Column<T>, sortKey) для совместимости с
// ResourceListPage и тестами, но делегирует рендер в antd.

import { type ReactNode, useMemo, useRef, useState, useEffect } from "react";
import { Table } from "antd";
import type { ColumnType, TableProps } from "antd/es/table";
import { getByPath } from "@/lib/path";

export interface Column<T> {
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
  /** Path в row для local-sort. Если не задан — колонка не сортируется. */
  sortKey?: string;
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
}

export function ResourceTable<T extends object>({
  rows,
  columns,
  rowKey,
  empty,
  loading,
  defaultSort,
  onRowClick,
}: Props<T>) {
  const antColumns: ColumnType<T>[] = useMemo(
    () =>
      columns.map((c, idx) => {
        const col: ColumnType<T> = {
          title: c.header,
          key: String(idx),
          className: c.className,
          render: (_value, row) => c.cell(row),
        };
        if (c.sortKey) {
          col.sorter = (a: T, b: T) => {
            const av = getByPath(a, c.sortKey!);
            const bv = getByPath(b, c.sortKey!);
            if (av == null && bv == null) return 0;
            if (av == null) return 1;
            if (bv == null) return -1;
            if (typeof av === "number" && typeof bv === "number") return av - bv;
            return String(av).localeCompare(String(bv));
          };
          if (defaultSort && defaultSort.col === idx) {
            col.defaultSortOrder = defaultSort.dir === "asc" ? "ascend" : "descend";
          }
        }
        return col;
      }),
    [columns, defaultSort],
  );

  // Тело таблицы скроллится внутри белой поверхности (h+v), а шапка колонок
  // (thead) фиксирована сверху. scroll.y = высота доступной области минус thead;
  // пересчитывается ResizeObserver'ом при изменении размеров окна/области.
  // Пока область не измерена (первый рендер) — y=undefined (обычный поток).
  const wrapRef = useRef<HTMLDivElement>(null);
  const [scrollY, setScrollY] = useState<number | undefined>(undefined);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const recompute = () => {
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
    // kc-table — zebra-striping + контрастный header + комфортный row-height +
    // чёткий hover (стили в index.css, theme-aware через vars).
    className: "kc-table",
    // scroll.x=max-content — колонки держат натуральную ширину, широкая таблица
    // получает СВОЙ горизонтальный скролл (не тянет страницу). scroll.y — тело
    // скроллится вертикально под фиксированной шапкой колонок.
    scroll: { x: "max-content", y: scrollY },
    loading,
    locale: {
      emptyText: empty ?? "Ресурсов не найдено",
    },
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
  // scroll.y, чтобы тело таблицы скроллилось внутри белой поверхности.
  return (
    <div ref={wrapRef} className="kc-table-fill" style={{ height: "100%", minHeight: 0, minWidth: 0 }}>
      <Table<T> {...tableProps} />
    </div>
  );
}
