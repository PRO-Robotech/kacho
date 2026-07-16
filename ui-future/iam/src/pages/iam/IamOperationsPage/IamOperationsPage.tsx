// IamOperationsPage — account-scoped лента операций IAM.
//
// Единый backend-RPC AccountService.ListAllOperations
// (GET /iam/v1/accounts/{account_id}/operations:all) с cursor-пагинацией по
// next_page_token: первая страница поллится live (5с), «Показать ещё» дотягивает
// следующие страницы и аккумулирует. Account берётся из context-store (шапочная
// пилюля), НЕ из project — IAM-секция живёт на уровне /iam/*.

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Empty, Input, Select, Tag, Typography } from "antd";
import { HistoryOutlined, ReloadOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { OperationsTable, type Op, statusOf, type OperationStatus } from "@shared/components/molecules/OperationsTable";
import { useContext } from "@shared/lib/context-store";

interface ListAllResp {
  operations?: Op[];
  next_page_token?: string;
}

const STATUS_OPTIONS: { value: OperationStatus | "all"; label: string }[] = [
  { value: "all", label: "Все статусы" },
  { value: "running", label: "Выполняется" },
  { value: "done", label: "Выполнена" },
  { value: "error", label: "Ошибка" },
  { value: "cancelled", label: "Отменена" },
];

export function IamOperationsPage() {
  const account = useContext((s) => s.account);
  const accountId = account?.id ?? null;
  const qc = useQueryClient();

  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<OperationStatus | "all">("all");
  const [pageToken, setPageToken] = useState<string | null>(null);
  const [acc, setAcc] = useState<Op[]>([]);

  // Смена account — сбрасываем накопление и курсор.
  useEffect(() => {
    setAcc([]);
    setPageToken(null);
  }, [accountId]);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ["iam-operations", accountId, pageToken ?? ""],
    queryFn: () =>
      api.list<ListAllResp>(`/iam/v1/accounts/${accountId}/operations:all`, {
        pageSize: "100",
        ...(pageToken ? { pageToken } : {}),
      }),
    enabled: !!accountId,
    // Live-обновление только на первой странице (без курсора).
    refetchInterval: pageToken ? false : 5000,
    staleTime: 0,
  });

  // Аккумуляция страниц: первая (pageToken=null) — свежий срез; далее merge по id.
  useEffect(() => {
    if (!data) return;
    const fresh = data.operations ?? [];
    setAcc((prev) => {
      const byId = new Map<string, Op>();
      (pageToken ? prev : []).forEach((o) => byId.set(o.id, o));
      fresh.forEach((o) => byId.set(o.id, o));
      return Array.from(byId.values());
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, pageToken]);

  const nextToken = data?.next_page_token || null;

  const headerRight = useMemo(
    () => (
      <Button
        size="small"
        icon={<ReloadOutlined />}
        onClick={() => {
          setAcc([]);
          setPageToken(null);
          qc.invalidateQueries({
            predicate: (q) => Array.isArray(q.queryKey) && q.queryKey[0] === "iam-operations",
          });
        }}
      >
        Обновить
      </Button>
    ),
    [qc],
  );
  useHeaderRight(headerRight);

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Identity and Access Management</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Операции</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  const sorted = useMemo(
    () =>
      [...acc].sort((a, b) => {
        const ta = a.created_at ? Date.parse(a.created_at) : 0;
        const tb = b.created_at ? Date.parse(b.created_at) : 0;
        return tb - ta;
      }),
    [acc],
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sorted.filter((o) => {
      if (status !== "all" && statusOf(o) !== status) return false;
      if (!q) return true;
      return (o.id ?? "").toLowerCase().includes(q);
    });
  }, [sorted, query, status]);

  if (!accountId) {
    return (
      <div className="kc-surface" style={{ padding: 20, minHeight: "100%" }}>
        <Empty description="Выберите Account вверху секции, чтобы увидеть операции." style={{ padding: "48px 0" }} />
      </div>
    );
  }

  return (
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", overflow: "hidden", display: "flex", flexDirection: "column" }}
    >
      {/* Шапка (иконка + «Операции» + IAM + счётчик + фильтры) — фиксирована сверху. */}
      <div style={{ flexShrink: 0, marginBottom: 12 }}>
        <PanelHeader
          icon={<HistoryOutlined />}
          eyebrow="Операции"
          title={
            <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
              IAM
              <Tag
                style={{
                  margin: 0,
                  fontSize: 11.5,
                  fontWeight: 600,
                  lineHeight: "16px",
                  height: 18,
                  paddingInline: 6,
                  borderRadius: 5,
                }}
              >
                {filtered.length}
              </Tag>
            </span>
          }
          right={
            <>
              <Input
                placeholder="Фильтр по идентификатору"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                allowClear
                style={{ width: 280 }}
              />
              <Select value={status} onChange={setStatus} options={STATUS_OPTIONS} style={{ width: 180 }} />
            </>
          }
        />
      </div>

      {/* Тело таблицы заполняет остаток поверхности и скроллит СЕБЯ (фикс. шапка
          колонок + вертикальный скролл тела) — иначе внутри Space высота
          коллапсирует и видно лишь ~4 строки. */}
      <div style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <OperationsTable
          rows={filtered}
          loading={isLoading && acc.length === 0}
          showResourceKind
          empty={acc.length > 0 && filtered.length === 0}
        />
      </div>

      {nextToken && (
        <div style={{ flexShrink: 0, marginTop: 12, textAlign: "center" }}>
          <Button loading={isFetching} onClick={() => setPageToken(nextToken)}>
            Показать ещё
          </Button>
        </div>
      )}
    </div>
  );
}
