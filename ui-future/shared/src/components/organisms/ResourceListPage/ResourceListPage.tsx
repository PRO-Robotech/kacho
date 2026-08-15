// ResourceListPage — generic страница списка ресурсов на antd.
//
// Polling 3 сек (через useResourceList).

import { useEffect, useMemo, useState } from "react";
import { Link, useParams, useLocation, useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Checkbox, Input, Segmented, Select, Typography, Tag } from "antd";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PlusOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath, type ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu, resourceHasRowActions } from "@shared/components/molecules/RowActionsMenu";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { type ReactNode } from "react";
import { ResourceEmptyState } from "@shared/components/molecules/ResourceEmptyState";
import { ProjectRequiredEmpty } from "@shared/components/molecules/ProjectRequiredEmpty";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { ColumnSettings, useHiddenColumns, type ToggleCol } from "@shared/components/molecules/TableToolbar";
import { useResourceList } from "@shared/lib/use-resource-list";
import { listViewState, loadedCountLabel } from "@shared/lib/list-view-state";
import { searchFilterExpression } from "@shared/lib/list-search-filter";

interface Props {
  spec: ResourceSpec;
  parentField?: string;
  parentParam?: string;
  /** Явное значение scope-фильтра (account-scoped IAM-ресурсы берут account
   *  из context-store, а не из URL-параметра). Имеет приоритет над parentParam. */
  parentValue?: string | null;
  /** page_size запроса списка (Role — 1000: клиентский system/custom-фильтр
   *  требует всю страницу, иначе custom-роли на 2-й странице выпадут). */
  pageSize?: string;
  /**
   * Есть ли у этого ресурса В ЭТОМ приложении форма-страница/панель.
   *
   * true  → «Создать» ведёт на `${listBase}/create`, правка открывается панелью;
   * false → «Создать» ставит флаг модалки `?modal=<specId>-create`.
   *
   * Это факт о таблице маршрутов приложения, а не о ресурсе: один и тот же spec
   * открывается страницей в своём ремоуте и модалкой в чужом. Раньше значение
   * выводилось из service-префикса spec.id, поэтому каждая копия компонента
   * несла таблицу маршрутов своего приложения — и копии разошлись. Решает тот,
   * кто маршруты и зарегистрировал.
   */
  panelForms: boolean;
  /** Игнорировать spec.childRoute при drill (клик по строке ведёт на
   *  `${basePath}/${id}` detail, а не на childRoute). Projects внутри IAM-секции
   *  открывают IAM-деталь проекта, а не project-dashboard. */
  disableChildRoute?: boolean;
}

export function ResourceListPage({
  spec,
  parentField,
  parentParam,
  parentValue,
  pageSize,
  panelForms,
  disableChildRoute = false,
}: Props) {
  const params = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const filterValue = parentValue ?? (parentParam ? (params[parentParam] ?? null) : null);
  const [query, setQuery] = useState("");
  // Конфигуратор видимости колонок (⚙ рядом с поиском) — persist в localStorage
  // по specId; те же toggles, что у related-таблиц detail-страниц.
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${spec.id}`);
  const toggleCols: ToggleCol[] = spec.columns.map((c) => ({ key: c.header, label: c.header }));

  // Серверные фильтры списка (spec.listFilters) — уходят в query, а не режут
  // загруженную страницу: клиентский фильтр поверх курсорной страницы отфильтровал
  // бы только то, что успело приехать, и выдал бы это за весь список.
  const [serverFilters, setServerFilters] = useState<Record<string, string>>({});

  // Строка поиска: у владельца, разбирающего выражение, она уходит ЗАПРОСОМ и
  // спрашивает про весь список; у остальных остаётся клиентским срезом по
  // прочитанным страницам — и тогда обязана называть это сама (placeholder ниже).
  const serverSearch = spec.serverSearchField;
  // Пауза перед запросом: без неё каждое нажатие клавиши — отдельный запрос к
  // владельцу, и «поиск ушёл на сервер» оплачивался бы очередью запросов на
  // каждое слово. Пауза короче человеческой паузы между словами, поэтому
  // задержки ответа не создаёт.
  const debouncedQuery = useDebounced(query, 250);
  const searchExpr = serverSearch ? searchFilterExpression(serverSearch, debouncedQuery) : null;
  // Выражение — ЧАСТЬ серверных фильтров, а не отдельный механизм: оба уезжают
  // одним запросом, и ключ кэша обязан различать их оба.
  const listQuery = useMemo(
    () => (searchExpr ? { ...serverFilters, filter: searchExpr } : serverFilters),
    [serverFilters, searchExpr],
  );
  const { data, isLoading, isError, error, hasMore, fetchMore, isFetchingMore } = useResourceList(
    spec,
    parentField ?? null,
    filterValue,
    pageSize,
    listQuery,
  );
  const setServerFilter = (param: string, value: string) =>
    setServerFilters((prev) => {
      const next = { ...prev };
      if (value) next[param] = value;
      else delete next[param];
      return next;
    });

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        <Typography.Text strong>{spec.plural}</Typography.Text>
      </span>
    ),
    [spec.plural, spec.serviceTitle],
  );
  useBreadcrumb(breadcrumb);

  // KAC-231: модалки упразднены в пользу формы-страницы/панели там, где
  // приложение действительно зарегистрировало `/create` и панель правки.
  // Значение приходит пропом от того, кто эти маршруты объявил (см. Props).
  const listBase = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname;
  const createTarget = panelForms ? `${listBase}/create` : `${listBase}?modal=${spec.id}-create`;
  // KAC-246: CTA «Создать» — в header right-slot (шапка), НЕ в page-toolbar.
  const cta = useMemo(() => {
    if (!spec.ops.create) return null;
    return (
      <Link to={createTarget}>
        <Button type="primary" icon={<PlusOutlined />}>
          Создать {spec.singular.toLowerCase()}
        </Button>
      </Link>
    );
  }, [spec, createTarget]);
  useHeaderRight(cta);

  const basePath = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname;

  const items = data?.[spec.payloadKey] ?? [];

  // Дополнительный фильтр "Зона доступности" — для ресурсов, у которых есть
  // понятие zone. Subnet хранит zone напрямую, Address — внутри
  // internal_ipv4_address.zone_id / external_ipv4_address.zone_id.
  const hasZoneFilter = spec.id === "subnets" || spec.id === "addresses";
  const [zone, setZone] = useState<string>("all");
  // Для Role — доп. фильтр system/custom (Segmented [Все/Системные/Кастомные]),
  // client-side по is_system. Тот же паттерн, что hasZoneFilter (паритет kacho-ui).
  const hasSystemFilter = spec.id === "roles";
  const [roleKind, setRoleKind] = useState<"all" | "system" | "custom">("all");
  const zoneSpec = REGISTRY["zones"];
  const { data: zoneData } = useQuery({
    queryKey: ["zones", "list-for-filter"],
    queryFn: () =>
      api.list<{ zones: Array<{ id: string; name?: string }> }>(zoneSpec.apiPath, {
        pageSize: "200",
      }),
    enabled: hasZoneFilter,
    staleTime: 60_000,
  });
  const zoneOptions = useMemo(
    () => [
      { value: "all", label: "Все зоны доступности" },
      ...((zoneData?.zones ?? []).map((z) => ({
        value: z.id,
        label: z.name || z.id,
      })) as { value: string; label: string }[]),
    ],
    [zoneData],
  );

  function rowZone(row: Record<string, unknown>): string | undefined {
    if (spec.id === "subnets") return getByPath<string>(row, "zone_id");
    if (spec.id === "addresses") {
      return (
        getByPath<string>(row, "internal_ipv4_address.zone_id") ??
        getByPath<string>(row, "external_ipv4_address.zone_id")
      );
    }
    return undefined;
  }

  const filteredItems = useMemo(() => {
    // Когда сузил сервер — клиент НЕ пересевает: он судил бы по своему правилу
    // сравнения о строках, которые владелец уже признал подходящими, и отбросил
    // бы часть ответа. Это вернуло бы исходный дефект этажом выше и незаметно:
    // список выглядел бы отфильтрованным, просто короче настоящего.
    const q = serverSearch ? "" : query.trim().toLowerCase();
    return items.filter((row) => {
      // "Публичные IP" — это external addresses; internal IPs показываются
      // только в subnet detail (IP-адреса tab). Фильтруем по наличию
      // external_ipv4_address (либо external_ipv6_address в будущем).
      if (spec.id === "addresses") {
        const ext =
          getByPath<unknown>(row, "external_ipv4_address") ?? getByPath<unknown>(row, "external_ipv6_address");
        if (!ext) return false;
      }
      if (hasZoneFilter && zone !== "all" && rowZone(row) !== zone) return false;
      if (hasSystemFilter && roleKind !== "all") {
        const isSystem = getByPath<boolean>(row, "is_system") === true || getByPath<boolean>(row, "isSystem") === true;
        if (roleKind === "system" && !isSystem) return false;
        if (roleKind === "custom" && isSystem) return false;
      }
      if (!q) return true;
      const name = (getByPath<string>(row, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(row, "id") ?? "").toLowerCase();
      return name.includes(q) || id.includes(q);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, query, serverSearch, zone, hasZoneFilter, hasSystemFilter, roleKind, spec.id]);

  // Заглушка «проект не выбран» — НИЖЕ всех хуков страницы, а не выше.
  // Scope приходит из context-store (аккаунтные списки IAM) или из параметра
  // маршрута, и меняется БЕЗ размонтирования компонента. Ранний выход над
  // хуками означал бы, что при выборе проекта число вызванных хуков растёт
  // между двумя рендерами одного и того же компонента, — React такой рендер
  // отвергает целиком, и пользователь получает пустой экран вместо списка.
  // Проба: ResourceListPage.hookorder.test.tsx.
  if (parentField && !filterValue) return <ProjectRequiredEmpty resource={spec.plural} />;

  // params.projectId доступен для project-scoped listов (/projects/:projectId/...);
  // прокидываем в buildSpecColumns, чтобы format: "references" (used_by) мог
  // отрендерить ссылку на /projects/<projectId>/compute/instances/<id> и т.п.
  const columns: Column<Record<string, unknown>>[] = buildSpecColumns(spec, {
    projectId: params.projectId,
    // Адрес карточки считается ТЕМ ЖЕ выражением, каким прежде считался переход
    // по клику на строку: клик снят (строка перестала быть ссылкой), и ссылка
    // обязана вести ровно туда же, иначе подмена сменила бы адресацию молча.
    // Иконка здесь не нужна: это список одного типа, и колонка иконок была бы
    // столбцом одинаковых значков — тип назван заголовком страницы.
    nameHref: (row) => {
      const id = getByPath<string>(row, "id");
      if (!id) return null;
      return spec.childRoute && !disableChildRoute ? spec.childRoute.replace(":id", id) : `${basePath}/${id}`;
    },
  }).filter((c) => !hidden.has(c.header));

  // Столбец действий — только когда у ресурса есть строчные действия: иначе
  // read-only каталог получает пустой столбец с кнопкой, открывающей пустое меню.
  if (resourceHasRowActions(spec)) {
    columns.push({
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (row) => (
        <RowActionsMenu
          spec={spec}
          row={row}
          basePath={basePath}
          projectId={filterValue ?? null}
          editAsPanel={panelForms}
        />
      ),
    });
  }

  // Какое из пяти состояний показать. Порядок важен: отказ никогда не должен
  // проваливаться в приглашение «создайте первый» — на 403 это сообщает
  // оператору, что список пуст, хотя ему просто отказали, а на 404 делает то же
  // поверх ответа, который равно может означать «скрыт от вас».
  const anyFilterActive =
    query.trim() !== "" ||
    (hasZoneFilter && zone !== "all") ||
    (hasSystemFilter && roleKind !== "all") ||
    Object.keys(serverFilters).length > 0;
  const view = listViewState({
    isLoading,
    error: isError ? (error ?? new Error("list failed")) : null,
    rowCount: filteredItems.length,
    filtered: anyFilterActive,
    canCreate: spec.ops.create,
  });
  const showWelcome = view === "welcome";

  // Единая шапка списка (PanelHeader) — те же 3 части, что у табов/форм:
  // [иконка ресурса] + «Список» (действие) + plural (название) + счётчик.
  // CTA «Создать» — в шапке страницы (useHeaderRight). KAC-246.
  const listHeader = (right?: ReactNode) => (
    <PanelHeader
      icon={<ResourceIcon specId={spec.id} />}
      eyebrow="Список"
      title={
        // height:20 = строка заголовка (16px·1.25) — счётчик-Tag НЕ распирает
        // строку (был 24px), иначе текст бейджа «скачет» относительно detail
        // (там тега нет). Tag ≤18px помещается в строку.
        <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
          {spec.plural}
          {!isLoading && !isError && (
            <Tag
              title={hasMore ? "Загружено строк; за курсором есть ещё" : undefined}
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
              {loadedCountLabel(filteredItems.length, hasMore)}
            </Tag>
          )}
        </span>
      }
      right={right}
    />
  );

  // Welcome (пустой список) — та же surface-подложка, что и заполнённая страница,
  // чтобы заголовок не «прыгал» и не выглядел инородно (KAC-246).
  if (showWelcome) {
    return (
      <div className="kc-surface" style={{ padding: 20, height: "100%", overflow: "auto" }}>
        {listHeader()}
        <ResourceEmptyState spec={spec} onCreate={() => navigate(createTarget)} />
      </div>
    );
  }

  return (
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", overflow: "hidden", display: "flex", flexDirection: "column" }}
    >
      {/* Шапка списка (иконка + «Список» + plural + счётчик + фильтры) —
          фиксирована сверху, НЕ скроллится вместе с телом таблицы. */}
      <div style={{ flexShrink: 0, marginBottom: 12 }}>
        {listHeader(
          <>
            {/* Одна и та же строка ввода означает на разных страницах разное —
                значит она обязана об этом СКАЗАТЬ. Серверный поиск спрашивает
                весь список; клиентский судит о прочитанных страницах, и молча
                выдавать второе за первое нельзя: пользователь читает «ничего не
                найдено» как утверждение об отсутствии ресурса. */}
            <Input.Search
              placeholder={
                serverSearch ? "Поиск по имени — по всему списку" : "Фильтр по имени или идентификатору среди загруженных"
              }
              title={
                serverSearch
                  ? "Запрос уходит на сервер: ищется по всему списку, а не по загруженным строкам."
                  : "Этот ресурс не умеет искать на сервере: сужаются только уже загруженные строки. Нажмите «Показать ещё», чтобы расширить набор."
              }
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              style={{ width: 320 }}
              allowClear
            />
            {hasZoneFilter && <Select value={zone} onChange={setZone} options={zoneOptions} style={{ width: 220 }} />}
            {(spec.listFilters ?? []).map((f) =>
              f.kind === "toggle" ? (
                <Checkbox
                  key={f.param}
                  checked={serverFilters[f.param] === "true"}
                  onChange={(e) => setServerFilter(f.param, e.target.checked ? "true" : "")}
                  title={f.description}
                >
                  {f.label}
                </Checkbox>
              ) : (
                <ServerRefFilter
                  key={f.param}
                  filter={f}
                  value={serverFilters[f.param] ?? ""}
                  onChange={(v) => setServerFilter(f.param, v)}
                />
              ),
            )}
            {hasSystemFilter && (
              <Segmented
                value={roleKind}
                onChange={(v) => setRoleKind(v as "all" | "system" | "custom")}
                options={[
                  { label: "Все", value: "all" },
                  { label: "Системные", value: "system" },
                  { label: "Кастомные", value: "custom" },
                ]}
              />
            )}
            <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
          </>,
        )}
      </div>

      {/* Тело таблицы заполняет остаток белой поверхности и скроллится внутри
          (горизонтально при широких колонках, вертикально при длинном списке). */}
      <div style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        {view === "error" ? (
          <ErrorResult error={error} />
        ) : (
          <ResourceTable
            rows={filteredItems}
            loading={isLoading && items.length === 0}
            rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
            columns={columns}
            // Сортировать можно только прочитанный целиком список. Пока за
            // курсором есть страницы, стрелка упорядочивала бы случайную его
            // часть и переставляла бы её при каждой догрузке — читатель принял
            // бы первую строку прочитанного за первую вообще. Порядок серверу
            // не заказывается: поле порядка снято с контракта осознанно.
            sortable={!hasMore}
          />
        )}
      </div>

      {/* Курсорная пагинация: общего числа у List нет, поэтому «ещё» — это
          наличие next_page_token, а не арифметика по общему числу. */}
      {view !== "error" && hasMore && (
        <div style={{ flexShrink: 0, marginTop: 12, textAlign: "center" }}>
          <Button loading={isFetchingMore} onClick={() => void fetchMore()}>
            Показать ещё
          </Button>
        </div>
      )}
    </div>
  );
}

/**
 * Значение, отставшее от ввода на `ms` спокойных миллисекунд.
 *
 * Нужно ровно там, где значение становится ЧАСТЬЮ ЗАПРОСА: без паузы каждое
 * нажатие клавиши — отдельный запрос к владельцу. Возвращает исходное значение
 * первым же рендером, поэтому пустая строка не «догоняет» позже и не отменяет
 * только что введённый поиск.
 */
function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    if (value === settled) return;
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, ms]);
  return settled;
}

// ServerRefFilter — выпадающий выбор значения серверного фильтра из другого
// ресурса реестра (напр. зоны по региону). Список опций читается той же
// курсорной страницей, что и всё остальное: это выбор фильтра, а не витрина.
function ServerRefFilter({
  filter,
  value,
  onChange,
}: {
  filter: { param: string; label: string; refSpecId: string; allLabel: string };
  value: string;
  onChange: (next: string) => void;
}) {
  const refSpec = REGISTRY[filter.refSpecId];
  const { data } = useQuery({
    queryKey: ["list-filter-options", filter.refSpecId],
    queryFn: () => api.list<Record<string, Array<{ id: string; name?: string }>>>(refSpec.apiPath, { pageSize: "200" }),
    enabled: !!refSpec,
    staleTime: 60_000,
  });
  if (!refSpec) return null;
  const rows = (data?.[refSpec.payloadKey] ?? []) as Array<{ id: string; name?: string }>;
  return (
    <Select
      value={value || "all"}
      onChange={(v) => onChange(v === "all" ? "" : v)}
      style={{ width: 220 }}
      options={[
        { value: "all", label: filter.allLabel },
        ...rows.map((r) => ({ value: r.id, label: r.name ? `${r.name} · ${r.id}` : r.id })),
      ]}
    />
  );
}
