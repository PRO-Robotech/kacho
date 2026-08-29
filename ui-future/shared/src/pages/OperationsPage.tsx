// OperationsPage — project-scoped global список LRO операций по всем VPC ресурсам.
// Aggregation client-side: для каждого VPC-resource type списком собираются
// ресурсы проекта, затем по каждому делается ListOperations. Все операции
// объединяются и сортируются по created_at desc.
//
// Фильтры: id / Статус / Тип ресурса.

import { useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { Input, Select, Tag, Typography } from "antd";
import { DeploymentUnitOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { ColumnSettings, useHiddenColumns } from "@shared/components/molecules/TableToolbar";
import { OperationsTable, operationColumnTitles, type Op, statusOf, type OperationStatus } from "@shared/components/molecules/OperationsTable";
import { useProjectStore } from "@shared/lib/context-store";
import { operationsListPath } from "@shared/lib/operations-subroute";
import { REGISTRY } from "@shared/lib/resource-registry";
import { clientScope, narrowingTitle, scopeSuffix } from "@shared/lib/list-scope";
// Подписи ресурсов — из единственного источника: выписанная здесь копия уже
// разошлась с ним (была английской при русском реестре), см. продукт #478.
import { ENTITIES } from "@shared/lib/entity-names";

// Список VPC-ресурсов, у которых есть per-resource ListOperations.
const VPC_RESOURCES = [
  { id: "networks", label: ENTITIES.networks.singular },
  { id: "subnets", label: ENTITIES.subnets.singular },
  { id: "network-interfaces", label: ENTITIES["network-interfaces"].singular },
  { id: "addresses", label: ENTITIES.addresses.singular },
  { id: "route-tables", label: ENTITIES["route-tables"].singular },
  { id: "security-groups", label: ENTITIES["security-groups"].singular },
  { id: "gateways", label: ENTITIES.gateways.singular },
] as const;

const STATUS_OPTIONS: { value: OperationStatus | "all"; label: string }[] = [
  { value: "all", label: "Все статусы" },
  { value: "running", label: "Выполняется" },
  { value: "done", label: "Выполнена" },
  { value: "error", label: "Ошибка" },
  { value: "cancelled", label: "Отменена" },
];

const KIND_OPTIONS = [
  { value: "all", label: "Все типы" },
  // Русские названия из реестра (singular), а не английские VPC_RESOURCES.label.
  ...VPC_RESOURCES.map((r) => ({ value: r.id, label: REGISTRY[r.id]?.singular ?? r.label })),
];

interface ResListResp {
  // Динамическое поле: payloadKey → массив ресурсов
  [k: string]: Array<{ id: string }> | string | undefined;
}

export function OperationsPage() {
  const project = useProjectStore((s) => s.project);
  const projectId = project?.id ?? null;

  // КНОПКИ «ОБНОВИТЬ» ЗДЕСЬ НЕТ (решение владельца).
  //
  // Список операций опрашивается сам — раз в несколько секунд, — поэтому кнопка
  // предлагала сделать то, что и так происходит. Хуже: её присутствие говорит
  // обратное, будто без неё страница показывает устаревшее, и человек жмёт её
  // на всякий случай.
  //
  // Слот шапки сбрасывается ЯВНО, а не просто перестаёт заполняться: он держит
  // состояние между страницами, и не сброшенный донёс бы сюда чужую кнопку с
  // предыдущей страницы.
  useHeaderRight(null);

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Virtual Private Cloud</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Операции</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<OperationStatus | "all">("all");
  const [kind, setKind] = useState<string>("all");
  const [hiddenCols, toggleCol] = useHiddenColumns("cols:vpc-operations");

  // 1) для каждого VPC-resource type грузим список ресурсов проекта.
  const listQueries = useQueries({
    queries: VPC_RESOURCES.map((r) => {
      const spec = REGISTRY[r.id];
      return {
        queryKey: [r.id, "list-for-ops", projectId],
        queryFn: () =>
          api.list<ResListResp>(spec.apiPath, {
            project_id: projectId!,
            pageSize: "200",
          }),
        enabled: !!projectId && !!spec,
        staleTime: 30_000,
      };
    }),
  });

  // 2) собираем плоский список (resourceId, kind, путь операций).
  // Путь берётся из перечня подмаршрутов ствола, а не склеивается из apiPath:
  // ресурс, у которого подмаршрута нет, в агрегатор не попадает вовсе — вместо
  // фан-аута запросов в несуществующий адрес. Перечень VPC_RESOURCES при этом
  // остаётся рукописным, поэтому проба ниже утверждает, что сегодня отсеивать
  // нечего: у всех семи подмаршрут есть.
  const targets = useMemo(() => {
    if (!projectId) return [];
    const out: { id: string; kind: string; opsPath: string }[] = [];
    VPC_RESOURCES.forEach((r, i) => {
      const spec = REGISTRY[r.id];
      const resp = listQueries[i].data;
      const list = (resp?.[spec.payloadKey] as Array<{ id: string }> | undefined) ?? [];
      list.forEach((item) => {
        if (!item?.id) return;
        const opsPath = operationsListPath(spec.apiPath, item.id);
        if (opsPath) out.push({ id: item.id, kind: r.id, opsPath });
      });
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, ...listQueries.map((q) => q.dataUpdatedAt)]);

  // 3) для каждого target грузим operations.
  const opsQueries = useQueries({
    queries: targets.map((t) => ({
      queryKey: [t.kind, "operations", t.id],
      queryFn: () =>
        api.list<{ operations: Op[] }>(t.opsPath, {
          pageSize: "50",
        }),
      enabled: true,
      staleTime: 5_000,
      // Реже поллим весь per-resource фан-аут (сотни запросов) — снижаем
      // постоянную сетевую нагрузку; in-flight операции обновятся за ~20с.
      // поллинг остаётся: лента операций по каждому ресурсу — подписки на
      // операции нет и не будет (решение 5 эпика #1016): у провалившейся
      // мутации события нет вовсе.
      refetchInterval: 20_000,
    })),
  });

  // 4) merge + sort.
  const allOps = useMemo(() => {
    const out: Op[] = [];
    opsQueries.forEach((q, i) => {
      const t = targets[i];
      const ops = q.data?.operations ?? [];
      ops.forEach((o) => out.push({ ...o, resource_id: o.resource_id ?? t?.id, resource_kind: t?.kind }));
    });
    out.sort((a, b) => {
      const ta = a.created_at ? Date.parse(a.created_at) : 0;
      const tb = b.created_at ? Date.parse(b.created_at) : 0;
      return tb - ta;
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opsQueries.map((q) => q.dataUpdatedAt).join(","), targets.length]);

  // Спиннер — только пока грузятся списки ресурсов (быстрая первая волна) ИЛИ
  // пока НЕ пришла ни одна операция. Как только первые операции есть — стримим
  // их в таблицу, не дожидаясь завершения всего per-resource фан-аута (сотни
  // запросов): «сбор» ощущается мгновенным, остальные операции дотекают.
  const isLoading =
    listQueries.some((q) => q.isLoading) || (allOps.length === 0 && opsQueries.some((q) => q.isLoading));

  // Область ручек (#373). Страница СТРОИТ свой набор из двух ярусов чтения:
  // списки ресурсов проекта по 200 и операции каждого ресурса по 50. Ни у
  // одного яруса продолжения здесь нет, поэтому «по фильтру ничего не
  // найдено» означает «нет среди собранного», а не «нет такой операции».
  // Область объявлена прочитанной частью безусловно: доказать полноту этого
  // веера нечем — курсор каждого из сотен запросов пришлось бы проверить
  // отдельно, и любой один непрочитанный делает набор неполным.
  const scope = clientScope(true);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return allOps.filter((o) => {
      if (kind !== "all" && o.resource_kind !== kind) return false;
      if (status !== "all" && statusOf(o) !== status) return false;
      if (!q) return true;
      return (o.id ?? "").toLowerCase().includes(q);
    });
  }, [allOps, query, status, kind]);

  if (!projectId) {
    return (
      <ErrorResult
        status="warning"
        title="Выберите проект"
        subTitle="Глобальные операции отображаются для текущего проекта."
      />
    );
  }

  return (
    // Колонка на всю высоту: шапка фиксированной высоты, таблица забирает
    // остаток и прокручивается ВНУТРИ себя. Прежде страница была обычным
    // потоком (`minHeight: 100%` + `Space`), и таблица, рассчитанная на высоту
    // родителя, получала её от контейнера нулевой высоты — список не
    // растягивался на доступное место, а прокрутка появлялась у страницы,
    // из-за чего шапка с фильтрами уезжала вверх вместе с содержимым.
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", minHeight: 0, display: "flex", flexDirection: "column" }}
    >
      {/* Без `Space`: его элементы получают `flex: 0 1 auto` и НЕ растягиваются,
          поэтому таблица внутри оставалась высотой в собственную шапку (182px
          при окне 900) — измерено на живой странице. Колонка строится явно. */}
      {/* Шапка не сжимается: остаток высоты забирает таблица.
          Единая шапка: общая VPC-иконка модуля + действие «Операции» +
          название «VPC» + счётчик; фильтры — справа. */}
      <div style={{ flexShrink: 0 }}>
        <PanelHeader
          icon={<DeploymentUnitOutlined />}
          title={
            // height 20 = строка заголовка (16·1.25); Tag ≤18 не распирает строку
            // → бейдж не прыгает относительно list-страниц (тот же фикс, что в
            // ResourceListPage).
            <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
              VPC
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
                placeholder={`Фильтр по идентификатору ${scopeSuffix(scope)}`}
                title={narrowingTitle(scope)}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                allowClear
                style={{ width: 280 }}
              />
              <Select
                value={status}
                onChange={setStatus}
                options={STATUS_OPTIONS}
                title={narrowingTitle(scope)}
                style={{ width: 180 }}
              />
              <Select value={kind} onChange={setKind} options={KIND_OPTIONS} style={{ width: 180 }} />
              {/* Где есть фильтр — есть и выбор столбцов. */}
              <ColumnSettings
                columns={operationColumnTitles(true).map((t) => ({ key: t, label: t }))}
                hidden={hiddenCols}
                onToggle={toggleCol}
              />
            </>
          }
        />
      </div>

      <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
        <OperationsTable
          rows={filtered}
          loading={isLoading}
          hiddenColumns={hiddenCols}
          showResourceKind
          empty={allOps.length > 0 && filtered.length === 0}
        />
      </div>
    </div>
  );
}
