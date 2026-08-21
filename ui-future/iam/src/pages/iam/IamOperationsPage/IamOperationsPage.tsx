// IamOperationsPage — account-scoped лента операций IAM.
//
// Единый backend-RPC AccountService.ListAllOperations
// (GET /iam/v1/accounts/{account_id}/operations:all) с cursor-пагинацией по
// next_page_token: первая страница поллится live (5с), «Показать ещё» дотягивает
// следующие страницы и аккумулирует. Account берётся из context-store (шапочная
// пилюля), НЕ из project — IAM-секция живёт на уровне /iam/*.

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Select, Typography } from "antd";
import { api } from "@shared/api/client";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { TableSearch } from "@shared/components/molecules/TableToolbar";
import { ScopeRequiredEmpty } from "@/components/molecules/ScopeRequiredEmpty";
import { IamListShell } from "@/components/organisms/iam/IamListShell";
import { OperationsTable, type Op, statusOf, type OperationStatus } from "@shared/components/molecules/OperationsTable";
import { useContext } from "@shared/lib/context-store";
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";
import { clientScope, narrowingTitle, scopeSuffix } from "@shared/lib/list-scope";

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
  }, [data, pageToken]);

  const nextToken = data?.next_page_token || null;
  // Область ручек (#373): у этой страницы продолжение ЕСТЬ, поэтому вопрос
  // сводится к тому, дотянут ли курсор до конца.
  const scope = clientScope(!!nextToken);

  // КНОПКИ «ОБНОВИТЬ» ЗДЕСЬ НЕТ, и это снятие, а не пропуск.
  //
  // Первая страница ленты поллится сама раз в пять секунд (`refetchInterval`
  // ниже). Кнопка предлагала сделать то, что и так происходит, — и самим своим
  // присутствием утверждала обратное: раз её показывают, значит без неё лента
  // стоит. Слот шапки приложения при этом сбрасывается: он держит состояние
  // между страницами и донёс бы сюда чужую кнопку.
  useHeaderRight(null);

  // Крошки называют ПУТЬ, заголовок — предмет, и дважды одно не говорят:
  // последнее звено «Операции» повторяло заголовок страницы. Подпись раздела —
  // из единственного источника: здесь стояло «Identity and Access Management»,
  // тогда как все соседние страницы IAM называют его «IAM», — два имени одного
  // раздела на соседних экранах.
  const breadcrumb = useMemo(() => <Typography.Text type="secondary">{SERVICES.iam.title}</Typography.Text>, []);
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
    return <ScopeRequiredEmpty purpose={`увидеть ${ENTITIES.operations.plural.toLowerCase()}`} />;
  }

  return (
    // ТА ЖЕ ОБОЛОЧКА, ЧТО У ОСТАЛЬНЫХ СПИСКОВ РАЗДЕЛА (`IamListShell`).
    //
    // Здесь была своя: шапка `PanelHeader` с иконкой-плиткой, заголовком «IAM»
    // и счётчиком строк. Заголовок называл МОДУЛЬ, а не предмет страницы, —
    // то есть на странице операций слово «Операции» не стояло вовсе; счётчик
    // снят решением владельца ВЕЗДЕ; поля страницы были `20` против `20px 24px`
    // у соседей.
    <IamListShell
      title={ENTITIES.operations.plural}
      narrowing={
        <>
          {/* ОТБОР — ПЕРЕД ПОИСКОМ: он меняет набор строк, среди которых потом
              ищут. */}
          <Select
            value={status}
            onChange={setStatus}
            options={STATUS_OPTIONS}
            title={narrowingTitle(scope)}
            style={{ width: 180 }}
          />
          <TableSearch
            value={query}
            onChange={setQuery}
            scope={scope}
            placeholder={`Фильтр по идентификатору ${scopeSuffix(scope)}`}
            width={320}
          />
        </>
      }
    >
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
    </IamListShell>
  );
}
