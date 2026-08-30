// ResourceShell — единый registry-driven 3-зонный layout детализации ЛЮБОГО
// ресурса (KAC-231 эпик). Эталон выработан на VPC Network, раскатан на все
// модули.
//
// Зоны: (1) глобальный ServiceSidebar (Layout.tsx) | (2) DetailShell aside —
// имя + вертикальные табы + доки | (3) main — контент таба ИЛИ форма-панель.
//
// Табы: «Обзор» (5 обяз. полей + доменные строки расширения + «Редактировать»)
//   → per-type табы связанных ресурсов (spec.related) → доменные табы расширения
//   → «Операции» → «JSON» → «JSON (internal)» если есть internalGetPath.
//
// Формы — НЕ модалки, а разворот в зоне 3 (mode=edit | child-create), уникальный
// URI на таб/режим. Диспетч кастомных/generic форм — InlineResourceForm.
//
// Конфиг per-resource: spec.related / spec.docs / spec.emptyState (registry) +
// DETAIL_EXTENSIONS (доменный React-контент: см. resource-detail-extensions).

import { type ReactNode, useCallback, useMemo, useState } from "react";
import { DETAIL_CONTENT_WIDTH } from "@shared/components/organisms/DetailShell";
import { useParams, useNavigate, useLocation, Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Select, Spin, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import {
  DetailShell,
  DetailSurface,
  HeaderSlotPortal,
  JsonTab,
  PropertyRows,
  type DetailTab,
} from "@shared/components/organisms/DetailShell";
import { DetailHeaderProvider } from "@shared/components/molecules/PanelHeader";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { ResourceEmptyState } from "@shared/components/molecules/ResourceEmptyState";
import { ResourceTable } from "@shared/components/organisms/ResourceTable";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { MonoValue } from "@shared/components/atoms/CopyableId/MonoValue";
import { LabelsCell } from "@shared/components/atoms/LabelsCell";
import { formatDateTime } from "@shared/lib/datetime";
import { RowActionsMenu, resourceHasRowActions } from "@shared/components/molecules/RowActionsMenu";
import { OperationsTab } from "@shared/components/organisms/OperationsTab";
import { InlineResourceForm } from "@shared/components/organisms/InlineResourceForm";
import {
  TableSearch,
  ColumnSettings,
  useHiddenColumns,
  type ToggleCol,
} from "@shared/components/molecules/TableToolbar";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { detailExtension, type DescItem } from "@shared/components/organisms/ResourceDetailExtensions";
import { api } from "@shared/api/client";
import {
  REGISTRY,
  getByPath,
  resourceProjectPath,
  resourceServicePrefix,
  type ResourceSpec,
} from "@shared/lib/resource-registry";
import { operationsListPath } from "@shared/lib/operations-subroute";
import {
  childListPathScope,
  fillPathFromParams,
  hasUnresolvedPathSegment,
  relatedListQuery,
} from "@shared/lib/related-list-query";
import type { RelatedSpec } from "@shared/lib/resource-spec";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { useResourceList } from "@shared/lib/use-resource-list";
import { useResourceStream } from "@shared/lib/subscription/use-resource-stream";
import { clientScope, noMatchesText, rowsAreComplete } from "@shared/lib/list-scope";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { DetailOverviewActions } from "@shared/components/molecules/DetailOverviewActions";

export type ResourceShellMode = "edit" | "child-create";

function specByRoute(route: string): ResourceSpec | undefined {
  return Object.values(REGISTRY).find((s) => s.route === route);
}

/** RelatedTable — встроенная таблица дочернего ресурса (тот же ResourceTable,
 *  что на списке): поиск + конфигуратор колонок + «⋮» actions + welcome-empty +
 *  продолжение курсора. */
function RelatedTable({
  childSpec,
  filterFields,
  narrowBy,
  parentId,
  parentRow,
  routeParams,
  projectId,
  detailBase,
}: {
  childSpec: ResourceSpec;
  filterFields: string[];
  /** Чем владелец ребёнка принимает сужение по родителю НА СЕРВЕРЕ — выражением
   *  фильтра либо типизированным полем запроса (`spec.related[]`). Ничего не
   *  объявлено — сужает только клиент. */
  narrowBy: Pick<RelatedSpec, "serverFilterField" | "serverParamField">;
  parentId: string;
  /** Строка родителя — из неё берутся сегменты адреса ребёнка, адресуемого путём. */
  parentRow?: Record<string, unknown>;
  /** Параметры адреса страницы — первый источник сегментов: что знает маршрут,
   *  то он знает точно, тогда как строка родителя может поля и не нести. */
  routeParams: Record<string, string | undefined>;
  projectId: string;
  detailBase: string;
}) {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [facetVal, setFacetVal] = useState("");
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${childSpec.id}`);
  // Третий механизм сужения — АДРЕС: репозитории лежат под реестром, теги — под
  // репозиторием реестра. Тогда сужает сам путь, и параметр родителя в запросе
  // не нужен вовсе. Чем закрываются сегменты — решает одна функция.
  const childFromRoute = useMemo(
    () => fillPathFromParams(childSpec.apiPath, routeParams),
    [childSpec.apiPath, routeParams],
  );
  const { pathParams, pathScoped } = useMemo(
    () => childListPathScope(childFromRoute, filterFields, parentRow, parentId),
    [childFromRoute, filterFields, parentRow, parentId],
  );
  // Дочерний список тянется в scope своего родителя: account-scoped ресурсы
  // (Project/ServiceAccount) требуют account_id = uid аккаунта-родителя; прочие —
  // project_id из URL.
  const accountScoped = childSpec.scope === "account";
  // Сужение по родителю просит СЕРВЕР, когда владелец ребёнка его принимает:
  // тогда страница курсора состоит из детей ЭТОГО родителя, а не из первой
  // страницы списка проекта, в которой они могли и не оказаться. Чем именно
  // просить — решает одна функция (`relatedListQuery`), а не эта разметка:
  // механизмов два, и выбор между ними принадлежит владельцу ребёнка.
  const extraQuery = useMemo(
    () => (pathScoped ? undefined : relatedListQuery(narrowBy, parentId)),
    [pathScoped, narrowBy, parentId],
  );
  // Фасетный фильтр судит по НАБОРУ, поэтому набор обязан быть прочитан целиком:
  // судить по первой странице значит отвечать «таких нет» про то, чего не читал.
  const wantAll = pathScoped && childSpec.loadAllPages === true;
  const { data, isLoading, isError, error, hasMore, fetchMore, isFetchingMore } = useResourceList(
    childSpec,
    pathScoped ? null : accountScoped ? "account_id" : "project_id",
    pathScoped ? null : accountScoped ? parentId : projectId,
    undefined,
    extraQuery,
    // Сегменты адреса — из ОБОИХ источников: что закрыл маршрут и что закрыла
    // строка родителя. Отдать только вторые значило бы уронить первые: резолвер
    // читает исходный `spec.apiPath`, и незакрытый сегмент запретил бы запрос —
    // то есть охрана сработала бы против уже известного адреса.
    { pathParams: { ...routeParams, ...pathParams }, loadAllPages: wantAll },
  );
  const all = data?.[childSpec.payloadKey] ?? [];
  // Клиентское сужение — ПОДСТРАХОВКА, а не основной путь. Когда серверное поле
  // объявлено, страница уже состоит из детей этого родителя и фильтр ничего не
  // убирает. Он остаётся ради ребра БЕЗ такого поля (у владельца нет пригодного
  // поля — напр. адрес хранит подсеть внутри jsonb) и ради вложенных путей
  // (OR по нескольким полям, напр. subnet→addresses v4∪v6), которые выражением
  // фильтра не выражаются вовсе. Сам по себе он судит только о ПРОЧИТАННЫХ
  // страницах — поэтому ниже обязателен видимый курсор.
  //
  // Ребёнок, сужённый АДРЕСОМ, клиентского фильтра не получает вовсе: страница
  // уже состоит из детей этого родителя, а сверять её ещё раз пришлось бы по
  // полю, которого в строке может не быть (тег несёт имя репозитория, но не
  // идентификатор реестра) — и тогда фильтр вырезал бы законные строки.
  const ownRows = pathScoped
    ? all
    : all.filter((r) => filterFields.some((ff) => getByPath<string>(r, ff) === parentId));

  // Область, о которой судят ручки этой вкладки. Поиск здесь клиентский всегда
  // (см. выше), поэтому вопрос ровно один — дочитан ли курсор.
  const scope = clientScope(hasMore);

  // Поиск по имени или идентификатору (client-side).
  const q = search.trim().toLowerCase();
  const searched = q
    ? ownRows.filter((r) => {
        const nm = (getByPath<string>(r, "name") ?? "").toLowerCase();
        const id = (getByPath<string>(r, "id") ?? "").toLowerCase();
        return nm.includes(q) || id.includes(q);
      })
    : ownRows;

  // Фасетный фильтр (`spec.facet`) — поверх поиска. Поле-массив (репозиторий со
  // смешанным содержимым) сверяется по включению, скаляр — по равенству.
  const facet = childSpec.facet;
  const rows =
    facet && facetVal
      ? searched.filter((r) => {
          const v = getByPath<unknown>(r, facet.path);
          return Array.isArray(v) ? v.includes(facetVal) : v === facetVal;
        })
      : searched;

  // child-create — панель в зоне 3 shell РОДИТЕЛЯ (URI вложен под родителя).
  const createPath = `${detailBase}/${childSpec.route}/create`;
  // drill в ребёнка — на его собственный flat-URL (родитель → в хлебных крошках).
  // IAM-ресурсы не project-scoped → flat-база /iam/<route> (иначе drill уходил бы
  // в nested /iam/accounts/:uid/projects/:id, где нет detail-роута).
  const flatChildBase =
    resourceServicePrefix(childSpec.id) === "iam"
      ? `/iam/${childSpec.route}`
      : (resourceProjectPath(childSpec.id, projectId) ?? `${detailBase}/${childSpec.route}`);

  // Колонки: spec.columns без столбцов-ссылок на родителя (filterFields).
  const specNoParent: ResourceSpec = {
    ...childSpec,
    columns: childSpec.columns.filter((c) => !filterFields.includes(c.path)),
  };
  const toggleCols: ToggleCol[] = specNoParent.columns.map((c) => ({ key: c.header, label: c.header }));
  // Связанная вкладка — ТА ЖЕ таблица, что страница списка этого типа: те же
  // колонки, тот же вид имени, тот же столбец действий. Расхождение читается не
  // как «здесь вложенный список», а как другое место продукта.
  //
  // Значка типа у имени тут НЕТ — по той же причине, по какой его нет на
  // странице списка: вкладка показывает ОДИН тип, названный её же ярлыком, и
  // столбец одинаковых значков не различает ни одной строки. Прежде он стоял
  // только здесь, и одна и та же подсеть выглядела по-разному на своей странице
  // и на вкладке сети.
  const columns = buildSpecColumns(specNoParent, {
    projectId,
    // Тот же адрес, каким прежде был переход по клику на строку.
    nameHref: (r) => {
      const rid = getByPath<string>(r, "id");
      return rid ? `${flatChildBase}/${rid}` : null;
    },
  }).filter((c) => !hidden.has(c.header));
  // Столбец действий — только там, где у ребёнка есть действия строки. Тем же
  // предикатом судит страница списка; без него ребёнок только на чтение получал
  // столбец с кнопкой, открывающей пустое меню.
  if (resourceHasRowActions(childSpec)) {
    columns.push({
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (row) => (
        <RowActionsMenu spec={childSpec} row={row} basePath={flatChildBase} projectId={projectId || null} editAsPanel />
      ),
    });
  }

  if (isError) return <ErrorResult error={error} />;

  // Путь ребёнка несёт подстановку, которую закрыть нечем — списка НЕ БЫЛО.
  // Запрос при этом не уходит (о чём и заботится `resolved` у резолвера пути), а
  // значит пустота здесь означает «ещё не спрашивали», а не «детей нет». Выдать
  // её за отсутствие значило бы предложить создать первый тег поверх непрочитанного
  // списка.
  const pathBlocked = hasUnresolvedPathSegment(childFromRoute) && !pathScoped;

  // Пустое состояние — welcome (только когда детей реально нет; промах поиска
  // показывается внутри таблицы). Подпись кнопки — короткое «Создать», её
  // ставит сам экран пустого состояния: предмет назван его же заголовком.
  //
  // «Создайте первый» — утверждение об ОТСУТСТВИИ детей, поэтому оно допустимо
  // только когда список дочитан. Пока за курсором есть ещё, детей может не быть
  // на прочитанных страницах и быть на следующих: приглашение создать поверх
  // недочитанного списка сообщало бы об отсутствии, которого никто не проверял.
  if (!isLoading && !pathBlocked && ownRows.length === 0 && !hasMore) {
    // Прокрутка — СВОЯ. Пустое состояние просит себе высоту почти во весь
    // экран (плитка, объяснение, призыв и блок документации), а вкладка стоит
    // в области, которая ниже шапки и полосы вкладок уже короче экрана и
    // обрезает лишнее. Без своей прокрутки нижняя часть — как раз призыв и
    // ссылки — оказывалась за краем: вкладка объясняла предмет и не предлагала
    // первого шага, ради которого объяснение и написано.
    return (
      <div style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}>
        <ResourceEmptyState spec={childSpec} onCreate={() => navigate(createPath)} />
      </div>
    );
  }

  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      {/* Фильтры (поиск/колонки) поднимаются на уровень имени ресурса (зона 3,
          правый слот) через HeaderSlotPortal — req3. */}
      <HeaderSlotPortal>
        <TableSearch value={search} onChange={setSearch} scope={scope} />
        {facet && (
          <Select
            value={facetVal}
            onChange={setFacetVal}
            style={{ minWidth: 180 }}
            aria-label={facet.label}
            options={[{ value: "", label: `${facet.label}: все` }, ...facet.options]}
          />
        )}
        <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
      </HeaderSlotPortal>
      <ResourceTable
        rows={rows}
        columns={columns}
        loading={isLoading}
        rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
        // Сужение здесь клиентское (см. `ownRows`), поэтому и порядок законен
        // ровно тогда, когда курсор дочитан.
        complete={rowsAreComplete(scope)}
        empty={
          q
            ? noMatchesText(scope)
            : ownRows.length === 0
              ? "На прочитанных страницах списка таких ресурсов нет — за курсором есть ещё."
              : undefined
        }
      />
      {/* Продолжение курсора — ТОТ ЖЕ вид, что на странице списка: общего числа
          List не отдаёт, поэтому «ещё» — это наличие курсора, а не арифметика по
          общему числу. Показано и при серверном сужении: сужённый список тоже
          бывает длиннее страницы.

          Догрузку НЕЛЬЗЯ вешать на эффект: эффект, зовущий продолжение на каждый
          ответ, вызывает себя же — это бесконечный рендер, который в этой консоли
          дважды убивал прогон по памяти, не оставив вердикта ни одной пробе.
          Продолжение — по действию пользователя. */}
      {hasMore && (
        <div style={{ flexShrink: 0, marginTop: 12, textAlign: "center" }}>
          <Button loading={isFetchingMore} onClick={() => void fetchMore()}>
            Показать ещё
          </Button>
        </div>
      )}
    </div>
  );
}

export function ResourceShell({ spec, mode }: { spec: ResourceSpec; mode?: ResourceShellMode }) {
  const routeParams = useParams();
  const { projectId, uid, childRoute } = routeParams;
  const navigate = useNavigate();
  // `DetailExtCtx.navigate` объявлен как переход без результата, а react-router
  // возвращает из него промис. Передать сам `navigate` значило бы отдать
  // расширению промис под видом void: отказ перехода стал бы необработанным
  // отклонением, которое ни один обработчик уже не увидит. Адаптер снимает
  // результат ЯВНО, ровно в той точке, где контракт его не принимает.
  const go = useCallback((to: string) => void navigate(to), [navigate]);
  const location = useLocation();
  const invalidate = useInvalidateResourceList();

  // detailBase = URL до и включая /:uid (надёжно при любой вложенности/модуле).
  const marker = `/${uid ?? ""}`;
  const mIdx = uid ? location.pathname.indexOf(marker) : -1;
  const detailBase =
    mIdx >= 0
      ? location.pathname.slice(0, mIdx + marker.length)
      : `${resourceProjectPath(spec.id, projectId) ?? `/${spec.route}`}/${uid}`;

  // Адрес карточки собирается склейкой, а адрес ресурса бывает АДРЕСУЕМЫМ ЧЕРЕЗ
  // РОДИТЕЛЯ (`/registry/v1/registries/{registryId}/repositories`). Тогда в
  // склеенном пути остаётся подстановка, закрыть которую этот маршрут не может:
  // родителя URL карточки не называет.
  //
  // Списочное чтение той же оболочки это уже охраняет (`resolveListPath` →
  // `resolved:false` ⇒ запрос не уходит). Детальное чтение охраны не имело, и
  // подстановка уезжала в адрес ЛИТЕРАЛОМ — два чтения ОДНОГО ресурса с разной
  // дисциплиной, из которых верно одно. Ответ края на такой адрес неотличим от
  // «ресурса нет», поэтому дефект тих: пользователь видит отказ, а не то, что
  // спросили не то.
  // Сегменты, которые знает АДРЕС СТРАНИЦЫ, закрываются им: ресурс под родителем
  // существует только в области родителя, и родителя называет маршрут, а не
  // догадка консоли. Остальное по-прежнему охраняется ниже.
  //
  // Выражение адреса остаётся ЦЕЛЬНЫМ (`${spec.apiPath}/${uid}`), а подстановка
  // применяется к нему снаружи. Это не стиль: перепись поверхности API
  // (`api-path-surface.test.ts`) резолвит голову выражения по имени `spec.apiPath`
  // и путь, спрятанный за промежуточную переменную, перестаёт для неё
  // существовать. Первая редакция этой правки так и сделала — и увела из-под
  // наблюдения 28 путей, показав это УМЕНЬШЕНИЕМ остатка, то есть «стало лучше».
  const detailPath = fillPathFromParams(`${spec.apiPath}/${uid}`, routeParams);
  const detailAddressable = !hasUnresolvedPathSegment(detailPath);

  // Карточка узнаёт о своих изменениях ПОТОКОМ (#1021): опрос остаётся ровно до
  // тех пор, пока владелец журнала не назвал этот вид. Ключ перечитывания
  // называется здесь, а не выводится из идентификатора спеки: у карточки на
  // странице живут и другие чтения (лента операций, связанные вкладки), и
  // поток их не покрывает.
  const { streamed } = useResourceStream({
    specId: spec.id,
    projectId: projectId ?? null,
    invalidate: [spec.id, "shell-detail", detailPath],
    enabled: !!uid && detailAddressable,
  });

  const { data, isLoading, isError, error } = useQuery({
    // Разрешённый путь — часть ключа: у ресурса под родителем имя уникально
    // только внутри родителя, и два одноимённых ребёнка разных родителей без
    // него делили бы один кэш, показывая друг друга.
    queryKey: [spec.id, "shell-detail", detailPath],
    queryFn: () => api.get<Record<string, unknown>>(detailPath),
    enabled: !!uid && detailAddressable,
    refetchInterval: streamed ? false : 5_000,
    staleTime: 0,
  });

  const ext = useMemo(() => detailExtension(spec.id), [spec.id]);
  const name = (data ? getByPath<string>(data, "name") : "") || (data ? ext?.title?.(data) : "") || (uid ?? "");

  const listHref = resourceProjectPath(spec.id, projectId);
  const breadcrumb = useMemo(() => {
    const childSpec = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
    // Локализованная метка для кастомных child-create-роутов без REGISTRY-spec
    // (privileges → «Привилегии»), чтобы breadcrumb не показывал raw route.
    const CHILD_LABELS: Record<string, string> = { privileges: "Привилегии" };
    const childLabel = childSpec?.plural ?? (childRoute ? (CHILD_LABELS[childRoute] ?? childRoute) : "");
    const sec = (txt: string) => <Typography.Text type="secondary">{txt}</Typography.Text>;
    const sep = <Typography.Text type="secondary">/</Typography.Text>;
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
        {spec.serviceTitle && (
          <>
            {sec(spec.serviceTitle)}
            {sep}
          </>
        )}
        {listHref ? <Link to={listHref}>{sec(spec.plural)}</Link> : sec(spec.plural)}
        {sep}
        {mode ? (
          <>
            <Link to={detailBase}>{sec(name)}</Link>
            {sep}
            {mode === "edit" ? (
              <Typography.Text strong>Редактирование</Typography.Text>
            ) : (
              <>
                <Link to={`${detailBase}/${childRoute}`}>{sec(childLabel)}</Link>
                {sep}
                <Typography.Text strong>Создание</Typography.Text>
              </>
            )}
          </>
        ) : (
          <Typography.Text strong style={{ maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis" }}>
            {name}
          </Typography.Text>
        )}
      </span>
    );
  }, [spec.serviceTitle, spec.plural, listHref, detailBase, name, mode, childRoute]);
  useBreadcrumb(breadcrumb);

  // KAC-242: действия в ШАПКЕ страницы — КОНТЕКСТНЫЕ по активному табу (не
  // глобальные на всех табах):
  //   • «Обзор»        → Редактировать + ⋮Удалить ресурса (DetailOverviewActions)
  //   • related-child  → «Создать <child>» (подсеть / таблица маршрутов / SG / …);
  //                       удаление ребёнка — per-row в таблице (RowActionsMenu)
  //   • прочие табы (операции / JSON / ext) → нет
  // Скрыто в edit/child-create (форма уже в зоне 3). Активный таб берём из URL ДО
  // early-return (без `data`); сам набор кнопок мемоизируем.
  const headerTabFromUrl = location.pathname.startsWith(detailBase)
    ? location.pathname.slice(detailBase.length).replace(/^\/+/, "").split("/")[0]
    : "";
  const headerTabId =
    mode === "child-create" && childRoute ? childRoute : mode === "edit" ? "overview" : headerTabFromUrl || "overview";
  const headerActions = useMemo(() => {
    if (mode) return null;
    if (headerTabId === "overview") {
      return data ? (
        <DetailOverviewActions
          spec={spec}
          data={data}
          projectId={projectId ?? null}
          detailBase={detailBase}
          extActions={ext?.headerActions?.({ data, projectId: projectId ?? null, detailBase, navigate: go })}
        />
      ) : null;
    }
    const rel = (spec.related ?? []).find((r) => REGISTRY[r.childId]?.route === headerTabId);
    const childSpec = rel ? REGISTRY[rel.childId] : undefined;
    if (childSpec) {
      return (
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => navigate(`${detailBase}/${childSpec.route}/create`)}
        >
          {/* Короткое «Создать» — решение владельца. Предмет назван РЯДОМ:
              вкладкой, на которой эта кнопка стоит («Таблицы маршрутов»), — и
              повторять его на самой кнопке незачем.

              Здесь стояла сборка подписи из ИМЕНИТЕЛЬНОГО падежа («Создать
              таблица маршрутов»), а комментарий на этом месте требовал
              винительного. Оба утверждения пережили свой предмет: сборки больше
              нет ни одной, склонять нечего. Полная форма (`createActionLabel`)
              осталась там, где предмет рядом не назван — пункт выпадающего
              списка, подпись выбора, текст отказа. */}
          Создать
        </Button>
      );
    }
    // Активный extra-таб (напр. «Привилегии») может нести собственный header-CTA
    // («Выдать доступ») — рендерим его в шапке страницы, как у related-табов.
    if (data) {
      const activeExtra = (
        ext?.extraTabs?.({ data, projectId: projectId ?? null, detailBase, navigate: go }) ?? []
      ).find((t) => t.id === headerTabId);
      if (activeExtra?.headerAction) return activeExtra.headerAction;
    }
    return null;
  }, [mode, headerTabId, data, spec, projectId, detailBase, ext, navigate, go]);
  // Действия карточки стоят НА УРОВНЕ ИМЕНИ ресурса (`nameActions`), а не в
  // шапке страницы (решение владельца). Причина видна на экране: шапка страницы
  // общая для всей консоли, и кнопка «Удалить» в ней читается как действие над
  // разделом, а не над открытым ресурсом; расстояние до имени, к которому она
  // относится, — половина экрана.
  //
  // В шапке страницы не остаётся ничего: `useHeaderRight(null)` — не «забыли
  // передать», а «здесь пусто по решению». Без явного вызова слот удержал бы
  // кнопки предыдущей страницы.
  useHeaderRight(null);

  if (isLoading && !data) {
    return (
      <div style={{ padding: 48, textAlign: "center" }}>
        <Spin />
      </div>
    );
  }
  // Запроса НЕ БЫЛО — и сказать об этом надо именно так. Ветка стоит ПЕРЕД
  // отказом: `ErrorResult` здесь сообщил бы, что ресурс не найден, то есть
  // утверждение о ресурсе, которого никто не спрашивал. «Не спрашивали» и
  // «спросили и не нашли» — разные факты о мире, и путать их нельзя ни в
  // сообщении пользователю, ни в собственной отладке.
  if (!detailAddressable) {
    return (
      <div style={{ padding: 48, maxWidth: 640 }}>
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          Адрес неполон — запрос не отправлялся
        </Typography.Title>
        <Typography.Paragraph type="secondary">
          {spec.singular} адресуется через родителя, а адрес этой страницы родителя не называет. Запрос не отправлен:
          ответ на неполный адрес был бы утверждением о ресурсе, которого никто не спрашивал.
        </Typography.Paragraph>
      </div>
    );
  }
  if (isError || !data) {
    return <ErrorResult error={error} />;
  }

  const related = spec.related ?? [];
  const extCtx = { data, projectId: projectId ?? null, detailBase, navigate: go };

  // ── Обзор: 5 обязательных + доменные строки расширения ──
  // Копирование объявляется у той строки, чьё значение КЛАДУТ В ЧУЖОЕ ПОЛЕ:
  // идентификатор (он и есть внешняя координата ресурса), имя и описание.
  // Копирование у всех строк обзора одинаковое — общий значок справа.
  // Значение идентификатора кнопкой БОЛЬШЕ НЕ ЯВЛЯЕТСЯ: два разных поведения в
  // одной таблице читались как недоделка.
  // выделить обрезанное мышью нельзя. Дате и меткам копирование не объявлено —
  // отформатированную дату некуда вставить, а набор меток не строка.
  const description = getByPath<string>(data, "description") ?? "";
  const overviewItems: DescItem[] = [
    { label: "Идентификатор", value: <MonoValue value={getByPath<string>(data, "id") ?? ""} />, copy: getByPath<string>(data, "id") || undefined },
    { label: "Имя", value: name, copy: name || undefined },
    { label: "Описание", value: description || "—", copy: description || undefined },
    { label: "Дата создания", value: formatDateTime(getByPath<string>(data, "created_at")) },
    // KAC-246: метки в обзоре — read-only (chips); добавление/правка — в форме
    // создания/модификации (LabelsEditor, key=value-таблица).
    { label: "Метки", value: <LabelsCell labels={getByPath<Record<string, string>>(data, "labels")} max={12} /> },
    ...(ext?.overviewExtra?.(extCtx) ?? []),
  ];

  const tabs: DetailTab[] = [
    {
      id: "overview",
      label: "Обзор",
      render: () => (
        <div>
          {/* Обзор — поверхность-секция со строками «ключ · значение» (эталон).
              Прежде здесь стоял `Descriptions` antd: он рисует таблицу с рамкой
              на КАЖДОЙ ячейке, то есть решётку там, где язык карточки держит
              одну линию между строками. Шапки у секции нет намеренно: заголовок
              «Обзор» уже стоит над зоной 3, и второй такой же был бы им же. */}
          <div style={{ maxWidth: DETAIL_CONTENT_WIDTH }}>
            {/* Заголовок секции НЕ повторяет имя вкладки. Вкладка над ней уже
                говорит «Обзор»; секция, назвавшая себя так же, сообщала бы это
                во второй раз подряд и ничего не добавляла. Здесь — что именно
                лежит в секции, а не в каком разделе мы находимся. */}
              <DetailSurface title="Основные свойства">
              <PropertyRows items={overviewItems} />
            </DetailSurface>
          </div>
          {ext?.overviewBelow?.(extCtx)}
        </div>
      ),
    },
  ];

  // Связанные ресурсы — отдельный таб на каждый тип.
  related.forEach((r) => {
    const childSpec = REGISTRY[r.childId];
    if (!childSpec) return;
    const filterFields = Array.isArray(r.filterField) ? r.filterField : [r.filterField];
    tabs.push({
      id: childSpec.route,
      label: r.label ?? childSpec.plural,
      // Зона-2: связанный таб = список дочернего ресурса → «действие» Список,
      // тип/иконка ребёнка (а НЕ label таба над типом родителя).
      eyebrow: "Список",
      headerTitle: childSpec.plural,
      headerIcon: <ResourceIcon specId={childSpec.id} />,
      // related-таблица заполняет зону-3 и скроллит себя (фикс. шапка колонок).
      fill: true,
      render: () => (
        <RelatedTable
          childSpec={childSpec}
          filterFields={filterFields}
          narrowBy={r}
          parentId={getByPath<string>(data, "id") ?? uid ?? ""}
          parentRow={data}
          routeParams={routeParams}
          projectId={projectId ?? ""}
          detailBase={detailBase}
        />
      ),
    });
  });

  // Доменные табы расширения (SG rules, RT routes, Instance NIC, ...).
  (ext?.extraTabs?.(extCtx) ?? []).forEach((t) => tabs.push(t));

  // Операции — только у ресурсов, чей подмаршрут ствол действительно несёт.
  // Прежде решала ручка `hideOperations` расширения, которую не выставлял никто,
  // — то есть вкладка появлялась у всех, включая каталожные и админские ресурсы
  // без подмаршрута. Здесь решает контракт: нет пути — нет вкладки.
  // `operationsListPath` отвечает `null` там, где подмаршрута операций у ресурса
  // нет, и это законный исход — подставлять в него нечего.
  const operationsBase = operationsListPath(spec.apiPath, getByPath<string>(data, "id") ?? uid ?? "");
  const operationsPath = operationsBase === null ? null : fillPathFromParams(operationsBase, routeParams);
  if (operationsPath) {
    tabs.push({
      id: "operations",
      label: "Операции",
      fill: true,
      render: () => (
        <OperationsTab spec={spec} resourceId={getByPath<string>(data, "id") ?? uid ?? ""} listPath={operationsPath} />
      ),
    });
  }
  tabs.push({
    id: "json",
    label: "JSON",
    eyebrow: "JSON",
    // Полотно доступно только на чтение: копирование целиком — единственный
    // способ вынести ответ края наружу, и оно живёт в одном компоненте с
    // полотном, чтобы вторая оболочка карточки не завела своё.
    render: () => <JsonTab data={data} />,
  });
  // Вкладки «JSON (internal)» здесь НЕТ (решение владельца 2026-08-12): она
  // показывала арендатору вторую, служебную проекцию того же ресурса —
  // предмет, о котором пользователю нечего решать, рядом с обычной.

  // ── form-panel (зона 3) ──
  let mainOverride: ReactNode | undefined;
  if (mode === "edit") {
    mainOverride = (
      <InlineResourceForm
        spec={spec}
        action="edit"
        id={uid}
        data={data}
        projectId={projectId ?? ""}
        onCancel={() => navigate(detailBase)}
        onSuccess={() => {
          invalidate(spec.id, projectId);
          void navigate(detailBase);
        }}
      />
    );
  } else if (mode === "child-create" && childRoute) {
    const childSpec = specByRoute(childRoute);
    if (childSpec) {
      const back = `${detailBase}/${childRoute}`;
      const rel = related.find((r) => REGISTRY[r.childId]?.route === childRoute);
      const ff = rel ? (Array.isArray(rel.filterField) ? rel.filterField[0] : rel.filterField) : undefined;
      mainOverride = (
        <InlineResourceForm
          spec={childSpec}
          action="create"
          projectId={projectId ?? ""}
          networkId={spec.id === "networks" ? uid : undefined}
          subnetId={spec.id === "subnets" ? uid : undefined}
          presetFields={ff ? { [ff]: uid } : undefined}
          onCancel={() => navigate(back)}
          onSuccess={() => navigate(back)}
        />
      );
    } else {
      // childRoute не в REGISTRY → кастомная embedded create-форма расширения
      // (напр. «privileges» → AccessBinding с залоченным субъектом). Форма сама
      // навигирует через onSuccess/onCancel (extCtx.navigate).
      mainOverride = ext?.childCreate?.(childRoute, extCtx) ?? undefined;
    }
  }

  // Активный таб — из pathname (path-based, уникальный URI на таб).
  const sub = location.pathname.startsWith(detailBase)
    ? location.pathname.slice(detailBase.length).replace(/^\/+/, "")
    : "";
  const seg0 = sub.split("/")[0];
  let activeTabId = "overview";
  if (mode === "child-create" && childRoute) activeTabId = childRoute;
  else if (mode === "edit") activeTabId = "overview";
  else if (seg0 && tabs.some((t) => t.id === seg0)) activeTabId = seg0;

  const onTabSelect = (id: string) => {
    if (id === "overview") void navigate(detailBase);
    else void navigate(`${detailBase}/${id}`);
  };

  // Зона-2 шапка для форм (edit/child-create): действие + тип + иконка ресурса
  // формы — контекст переезжает в блок табов, форма в зоне 3 свою шапку не дублирует.
  const childForHeader = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
  // Кастомные child-create-роуты (нет в REGISTRY): тип/иконка из небольшой карты,
  // чтобы зона-2 не падала и показывала осмысленный заголовок (напр. privileges →
  // «Привязка доступа» + иконка access-bindings).
  const CUSTOM_CHILD_HEADER: Record<string, { title: string; specId: string }> = {
    privileges: { title: "Привязка доступа", specId: "access-bindings" },
  };
  const customChild =
    mode === "child-create" && childRoute && !childForHeader ? CUSTOM_CHILD_HEADER[childRoute] : undefined;
  // Слово действия — то же, что в шапке формы: «Изменение», не «Редактирование».
  const headerTitle =
    mode === "edit"
      ? // Заголовок правки — ИМЯ ресурса (умолчание `DetailShell`), а не тип во
        // множественном числе. Прежде здесь стояло `spec.plural`, и страница
        // правки одной сети называла себя «Облачные сети»: множественное число
        // на странице одного экземпляра сообщает, что открыт список.
        undefined
      : mode === "child-create"
        ? (childForHeader?.plural ?? customChild?.title)
        : undefined;
  const headerIcon =
    mode === "child-create" && childForHeader ? (
      <ResourceIcon specId={childForHeader.id} />
    ) : mode === "child-create" && customChild ? (
      <ResourceIcon specId={customChild.specId} />
    ) : undefined;

  return (
    // Прокидываем иконку ресурса вниз — все SectionHeader табов получают её
    // (единая шапка с формами через PanelHeader).
    <DetailHeaderProvider value={{ icon: <ResourceIcon specId={spec.id} /> }}>
      <DetailShell
        resourceName={name}
        tabs={tabs}
        mainOverride={mainOverride}
        activeTabId={activeTabId}
        onTabSelect={onTabSelect}
        nameActions={headerActions}
        headerTitle={headerTitle}
        headerIcon={headerIcon}
      />
    </DetailHeaderProvider>
  );
}
