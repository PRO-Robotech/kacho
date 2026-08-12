// OperationsTable — generic вид списка LRO-операций.
// Используется в:
//   • OperationsTab (per-resource detail-page)
//   • OperationsPage (global project-level)
//
// Колонки: Идентификатор / Статус (icon+string) / Пользователь (email) /
//          Операция / Дата начала / Дата изменения / Сообщение об ошибке /
//          Идентификатор ресурса.
//
// «Пользователь» — email инициатора. created_by приходит как user-id; KAC-239 (#4)
// резолвим его в email через глобальный справочник /iam/v1/users (scope:global).
// Фоллбэк (нет матча / справочник не загрузился) — created_by/principal как есть.

import { useEffect, useRef, useState } from "react";
import { Empty, Space, Table, Typography } from "antd";
import { CheckCircleFilled, CloseCircleFilled, LoadingOutlined, MinusCircleFilled } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { formatDateTime } from "@shared/lib/datetime";
import { CopyableId } from "@shared/components/atoms/CopyableId";
import { statusOf, statusLabel, type OperationStatus } from "./opFilter";

// Пере-экспорт чистой фильтр-логики (opFilter) — потребители импортируют её из
// OperationsTable как единой точки. Юнит-тесты тянут opFilter напрямую (без antd).
export { statusOf, statusLabel };
export type { OperationStatus };

export interface Op {
  id: string;
  description?: string;
  created_at?: string;
  modified_at?: string;
  created_by?: string;
  done?: boolean;
  error?: { code?: number | string; message?: string };
  metadata?: Record<string, unknown>;
  /** Заполняется через aggregation либо парсингом metadata.<resource>_id. */
  resource_id?: string;
  /** Тип ресурса (registry id). Заполняется при aggregation в global-странице. */
  resource_kind?: string;
  /** IAM principal — поля operation.proto (sub-phase 2.0 IAM E0, KAC-105). */
  principal_type?: string;
  principal_id?: string;
  principal_display_name?: string;
}

interface IamUser {
  id: string;
  email?: string;
  display_name?: string;
}

/** userFallback — что показать без справочника: created_by/principal как есть. */
function userFallback(op: Op): string {
  return op.created_by || op.principal_display_name || op.principal_id || "";
}

function statusCell(op: Op) {
  const s = statusOf(op);
  const iconStyle = { fontSize: 16 };
  const icon =
    s === "done" ? (
      <CheckCircleFilled style={{ ...iconStyle, color: "#52c41a" }} />
    ) : s === "error" ? (
      <CloseCircleFilled style={{ ...iconStyle, color: "#ff4d4f" }} />
    ) : s === "cancelled" ? (
      <MinusCircleFilled style={{ ...iconStyle, color: "#8c8c8c" }} />
    ) : (
      <LoadingOutlined style={{ ...iconStyle, color: "#faad14" }} spin />
    );
  return (
    <Space size={6}>
      {icon}
      <span>{statusLabel(s)}</span>
    </Space>
  );
}

function fmtTs(ts?: string): string {
  return formatDateTime(ts);
}

interface Props {
  rows: Op[];
  loading?: boolean;
  /** Заголовки колонок, снятых пользователем. */
  hiddenColumns?: Set<string>;
  /** Когда true — показывать колонку "Тип ресурса" (для global-страницы). */
  showResourceKind?: boolean;
  /** Когда true — показывать пустое состояние при rows.length===0 и !loading. */
  empty?: boolean;
}

/** Заголовки колонок таблицы операций — для конфигуратора столбцов. Перечень
 *  выводится из самой таблицы, а не выписывается вторым списком: два места об
 *  одном предмете разошлись бы на первой же новой колонке. */
export function operationColumnTitles(showResourceKind?: boolean): string[] {
  const base = [
    "Идентификатор",
    "Статус",
    "Пользователь",
    "Операция",
    "Дата начала",
    "Дата изменения",
    "Сообщение об ошибке",
  ];
  return showResourceKind ? [...base, "Тип ресурса", "Идентификатор ресурса"] : [...base, "Идентификатор ресурса"];
}

export function OperationsTable({ rows, loading, showResourceKind, empty, hiddenColumns }: Props) {
  // KAC-239 (#4): справочник пользователей для резолва created_by(id) → email.
  // /iam/v1/users — scope:global, грузится один раз и дедуплицируется TanStack.
  const { data: usersData } = useQuery({
    queryKey: ["ops-users"],
    queryFn: () => api.list<{ users?: IamUser[] }>("/iam/v1/users", { pageSize: "1000" }),
    staleTime: 60_000,
  });
  const userEmail = (op: Op): string => {
    const u = (usersData?.users ?? []).find((x) => x.id === op.created_by);
    return u?.email || u?.display_name || userFallback(op);
  };

  const allColumns: ColumnsType<Op> = [
    {
      title: "Идентификатор",
      dataIndex: "id",
      key: "id",
      width: 240,
      render: (v: string) => <CopyableId id={v} />,
    },
    {
      title: "Статус",
      key: "status",
      width: 160,
      render: (_v, op) => statusCell(op),
    },
    {
      title: "Пользователь",
      key: "user",
      width: 240,
      render: (_v, op) => {
        const email = userEmail(op);
        return email ? <span>{email}</span> : <Typography.Text type="secondary">—</Typography.Text>;
      },
    },
    {
      title: "Операция",
      dataIndex: "description",
      key: "description",
      render: (v: string | undefined, op) => v || <Typography.Text type="secondary">{op.id}</Typography.Text>,
    },
    {
      title: "Дата начала",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      render: (v: string) => fmtTs(v),
    },
    {
      title: "Дата изменения",
      dataIndex: "modified_at",
      key: "modified_at",
      width: 180,
      render: (v: string) => fmtTs(v),
    },
    {
      title: "Сообщение об ошибке",
      key: "error",
      render: (_v, op) =>
        op.error?.message ? (
          <Typography.Text type="danger" style={{ whiteSpace: "pre-wrap" }}>
            {op.error.message}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    ...(showResourceKind
      ? ([
          {
            title: "Тип ресурса",
            dataIndex: "resource_kind",
            key: "resource_kind",
            width: 160,
            render: (v: string | undefined) => v || "—",
          },
        ] as ColumnsType<Op>)
      : []),
    {
      title: "Идентификатор ресурса",
      dataIndex: "resource_id",
      key: "resource_id",
      width: 240,
      render: (v: string | undefined) => (v ? <CopyableId id={v} /> : "—"),
    },
  ];

  // Тело таблицы скроллится внутри области (h при широких колонках, v при длинном
  // списке), шапка колонок фиксирована — как generic ResourceTable.
  const wrapRef = useRef<HTMLDivElement>(null);
  // Колонки, снятые пользователем, не рендерятся. Первая — закреплена: при
  // горизонтальной прокрутке широкой таблицы без неё не видно, к какой строке
  // относятся уехавшие вправо значения.
  const columns: ColumnsType<Op> = allColumns
    .filter((c) => !hiddenColumns?.has(String(c.title ?? "")))
    // Ширина обязательна: без неё antd закрепление молча игнорирует.
    .map((c, i) => (i === 0 ? { ...c, fixed: "left" as const, width: 220 } : c));

  const [scrollY, setScrollY] = useState<number | undefined>(undefined);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const recompute = () => {
      // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion -- ложное срабатывание: querySelector<E extends Element = Element>, и E выводится из самого утверждения типа. Без него E = Element, у которого нет offsetHeight (проверено tsc: удаление даёт TS2339).
      const thead = el.querySelector(".ant-table-thead") as HTMLElement | null;
      const avail = el.clientHeight - (thead?.offsetHeight ?? 40);
      setScrollY(avail > 48 ? avail : undefined);
    };
    const ro = new ResizeObserver(recompute);
    ro.observe(el);
    recompute();
    return () => ro.disconnect();
  }, []);

  return (
    <div ref={wrapRef} className="kc-table-fill" style={{ height: "100%", minHeight: 0, minWidth: 0 }}>
      <Table<Op>
        rowKey="id"
        dataSource={rows}
        columns={columns}
        loading={loading}
        size="small"
        className="kc-table"
        scroll={{ x: "max-content", y: scrollY }}
        pagination={false}
        locale={{
          emptyText: (
            <Empty
              description={
                <Typography.Text type="secondary">
                  {empty ? "По фильтру ничего не найдено." : "Операций пока нет."}
                </Typography.Text>
              }
            />
          ),
        }}
      />
    </div>
  );
}
