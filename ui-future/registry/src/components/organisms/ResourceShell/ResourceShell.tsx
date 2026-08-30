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
// Конфиг per-resource: spec.related / spec.docs / spec.emptyState (реестр
// ресурсов модуля) + реестр расширений (доменный React-контент: см.
// `@/registerExtensions`).
//
// ПОЧЕМУ ЭТО ЕЩЁ КОПИЯ, А НЕ РЕ-ЭКСПОРТ ОБЩЕЙ ОБОЛОЧКИ. Различий с общей — 461
// строка. Держит форк ОДИН факт дерева, и он проверяется контрактом, а не
// памятью: общая оболочка адресует ребёнка по `id`, а `message Repository` и
// `message Tag` поля `id` не объявляют вовсе (первое поле обоих — `registry_id`,
// см. `proto/kacho/cloud/registry/v1/registry.proto`). Натуральный ключ здесь —
// имя внутри реестра и тег внутри репозитория; прямая замена сделала бы ключ
// строки случайным, а имена перестали бы быть ссылками. Снятие — научить общую
// оболочку ключу идентичности, объявленному спекой; это её предмет, а не предмет
// раздела, и идёт своей задачей.
//
// > Здесь стоял ВТОРОЙ держатель — «общий реестр ресурсов не несёт
// > `registries`/`repositories`/`tags`». Он опровергнут: `@/lib/resource-registry`
// > этого модуля — ре-экспорт общего (`export * from "@shared/lib/resource-registry"`),
// > то есть разность ключей пуста, и все три записи резолвятся общей оболочкой.
// > Утверждение пережило свой предмет и отговаривало от работы, которая уже стала
// > возможной, — предикат назван выше, чтобы следующий читатель проверил его
// > командой, а не поверил.
//
// > И здесь же стояло «у вкладки связанных нет продолжения курсора»: раздел держал
// > свою копию чтения списка, бравшую ровно первую страницу. Копия сведена к общей
// > реализации (см. `@/lib/use-resource-list`), продолжение курсора у вкладок есть.
// > Отставанием остаётся серверное сужение дочернего списка по родителю
// > (`spec.related[].serverFilterField`): здесь оно клиентское, поэтому ручки
// > сужения честно называют область прочитанной частью.
//
// Своим в этой копии является ровно адресация по натуральному ключу и боковая
// панель тегов; всё прочее сведено к общему.

import { type ReactNode, useMemo, useState } from "react";
import { useParams, useNavigate, useLocation, Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Select, Spin, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import {
  DETAIL_CONTENT_WIDTH,
  DetailShell,
  DetailSurface,
  HeaderSlotPortal,
  JsonTab,
  PropertyRows,
  type DetailTab,
} from "@/components/organisms/DetailShell";
import { DetailHeaderProvider } from "@/components/molecules/PanelHeader";
import { ResourceIcon } from "@/components/organisms/form/ResourceIcon";
import { ResourceEmptyState } from "@/components/molecules/ResourceEmptyState";
import { ResourceTable } from "@/components/organisms/ResourceTable";
import { ErrorResult } from "@/components/molecules/ErrorResult";
import { MonoValue } from "@shared/components/atoms/CopyableId/MonoValue";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import { formatDateTime } from "@/lib/datetime";
import { RowActionsMenu, resourceHasRowActions } from "@/components/molecules/RowActionsMenu";
import { OperationsTab } from "@/components/organisms/OperationsTab";
import { operationsListPath } from "@shared/lib/operations-subroute";
import { InlineResourceForm } from "@/components/organisms/InlineResourceForm";
import { TableSearch, ColumnSettings, useHiddenColumns, type ToggleCol } from "@/components/molecules/TableToolbar";
import { useBreadcrumb, useHeaderRight } from "@/components/molecules/PageHeaderSlot";
import { detailExtension, type DescItem } from "@shared/components/organisms/ResourceDetailExtensions";
import { api } from "@/api/client";
import { REGISTRY, getByPath, resourceProjectPath, type ResourceSpec } from "@/lib/resource-registry";
import { buildSpecColumns } from "@/lib/spec-columns";
import { useResourceList } from "@/lib/use-resource-list";
import { clientScope, noMatchesText, rowsAreComplete } from "@shared/lib/list-scope";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { useResourceStream } from "@shared/lib/subscription/use-resource-stream";
import { DetailOverviewActions } from "@/components/molecules/DetailOverviewActions";
import { RepositoryTagsPanel } from "@/components/organisms/RepositoryTagsPanel";
// Решения об адресе — ОДНО объявление на консоль: что такое подстановка, чем
// она закрывается из маршрута и чем сужается дочерний список.
import { childListPathScope, fillPathFromParams, hasUnresolvedPathSegment } from "@shared/lib/related-list-query";

export type ResourceShellMode = "edit" | "child-create";

function specByRoute(route: string): ResourceSpec | undefined {
  return Object.values(REGISTRY).find((s) => s.route === route);
}

/** RelatedTable — встроенная таблица дочернего ресурса (тот же ResourceTable,
 *  что на списке): поиск + конфигуратор колонок + «⋮» actions + welcome-empty. */
function RelatedTable({
  childSpec,
  filterFields,
  parentId,
  parentRow,
  routeParams,
  projectId,
  detailBase,
}: {
  childSpec: ResourceSpec;
  filterFields: string[];
  parentId: string;
  /** Строка родителя: из неё берутся сегменты адреса, которых нет в маршруте. */
  parentRow?: Record<string, unknown>;
  /** Параметры адреса страницы — первый и самый надёжный источник сегментов. */
  routeParams: Record<string, string | undefined>;
  projectId: string;
  detailBase: string;
}) {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [facetVal, setFacetVal] = useState("");
  // Открытый образ для боковой панели тегов (repositories → Drawer, без перехода).
  const [tagsRepo, setTagsRepo] = useState<string | null>(null);
  const [hidden, toggleHidden] = useHiddenColumns(`cols:${childSpec.id}`);
  // Ребёнок, адресуемый ПУТЁМ (`/registry/v1/registries/{registryId}/repositories`,
  // `.../{registryId}/repositories/{repository}/tags`), сужается самим адресом:
  // владелец берёт родителя из сегментов, и параметр в запросе не нужен вовсе.
  //
  // Здесь стояла своя ветка `плейсхолдеров ровно ОДИН`. У тегов их два, поэтому
  // она отвечала «не по пути» и отправляла запрос по адресу с литералом — то
  // есть вкладка не оживала ни при каком входе (#627). Сужение считает общая
  // функция: два решения об одном предмете расходятся молча, и разошлись.
  //
  // Порядок источников: сначала АДРЕС СТРАНИЦЫ (что знает маршрут — то знает
  // точно), затем поля родительской строки и его идентичность.
  const childFromRoute = fillPathFromParams(childSpec.apiPath, routeParams);
  const { pathParams, pathScoped } = childListPathScope(childFromRoute, filterFields, parentRow, parentId);
  // Фасетный отбор судит по НАБОРУ, поэтому набор обязан быть прочитан целиком:
  // судить по первой странице значит отвечать «таких нет» про то, чего не читал.
  // Дочитывание просится ПРИЗНАКОМ у того же хука, а не вторым хуком: два пути
  // чтения одного списка расходятся молча.
  const wantAll = pathScoped && childSpec.loadAllPages === true;
  const { data, isLoading, isError, error, hasMore, fetchMore, isFetchingMore } = useResourceList(
    childSpec,
    pathScoped ? null : "project_id",
    pathScoped ? null : projectId,
    undefined,
    undefined,
    // Сегменты адреса — из ОБОИХ источников: что закрыл маршрут и что закрыла
    // строка родителя. Отдать только вторые значило бы уронить первые: резолвер
    // читает исходный `spec.apiPath`, и незакрытый сегмент запретил бы запрос —
    // то есть охрана сработала бы против уже известного адреса.
    { pathParams: { ...routeParams, ...pathParams }, loadAllPages: wantAll },
  );
  // Область, о которой судят ручки вкладки. Сужение здесь клиентское (поиск и
  // фасет считаются в браузере), поэтому вопрос ровно один — дочитан ли курсор.
  // Отвечает на него сам хук: дочитывание тоже сохраняет курсор последней
  // страницы, если упёрлось в предел, и оборванное чтение не выдаёт себя за
  // полное.
  const scope = clientScope(hasMore);
  const all = (data?.[childSpec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
  // Фильтр по родителю (OR по нескольким полям — напр. subnet→addresses v4∪v6).
  // Ребёнок, сужённый АДРЕСОМ, клиентского фильтра не получает вовсе: страница
  // уже состоит из детей этого родителя, а сверять её ещё раз пришлось бы по
  // полю, которого в строке может не быть (тег несёт имя репозитория, но не
  // идентификатор реестра) — и тогда фильтр вырезал бы законные строки.
  const ownRows = pathScoped
    ? all
    : all.filter((r) => filterFields.some((ff) => getByPath<string>(r, ff) === parentId));

  // Поиск по имени или идентификатору (client-side).
  const q = search.trim().toLowerCase();
  const searched = q
    ? ownRows.filter((r) => {
        const nm = (getByPath<string>(r, "name") ?? "").toLowerCase();
        const id = (getByPath<string>(r, "id") ?? "").toLowerCase();
        return nm.includes(q) || id.includes(q);
      })
    : ownRows;
  // Facet-фильтр (напр. тип артефакта): поверх поиска. Поле-массив (artifact_types
  // смешанного репозитория) — по включению; скаляр — по точному значению.
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
  // Где живёт КАРТОЧКА ребёнка. Ребёнок, адресуемый через родителя, существует
  // только в его области — репозиторий в своём реестре, — поэтому и карточка его
  // лежит под родителем. Плоский адрес такую карточку не открывает by
  // construction: он не называет реестр, а без реестра адрес чтения не собрать.
  const childBase = pathScoped
    ? `${detailBase}/${childSpec.route}`
    : (resourceProjectPath(childSpec.id, projectId) ?? `${detailBase}/${childSpec.route}`);

  // Колонки: spec.columns без столбцов-ссылок на родителя (filterFields).
  const specNoParent: ResourceSpec = {
    ...childSpec,
    columns: childSpec.columns.filter((c) => !filterFields.includes(c.path)),
  };
  const toggleCols: ToggleCol[] = specNoParent.columns.map((c) => ({ key: c.header, label: c.header }));
  // Значка типа у имени тут НЕТ — по той же причине, по какой его нет на странице
  // списка этого типа: вкладка показывает ОДИН тип, названный её же ярлыком, и
  // столбец одинаковых значков не различает ни одной строки. Прежде он стоял
  // только здесь, и один и тот же репозиторий выглядел по-разному на своей
  // странице и на вкладке реестра.
  const columns = buildSpecColumns(specNoParent, {
    projectId,
    // Тот же адрес, каким прежде был переход по клику на строку.
    nameHref: (r) => {
      const rid = getByPath<string>(r, "id");
      // Идентичность в адресе — та же, которой ребёнок адресуется у владельца:
      // у репозитория собственного идентификатора НЕТ, его натуральный ключ —
      // имя внутри реестра. Нечем адресовать — ссылки не рисуем: ссылка в никуда
      // хуже её отсутствия, она обещает страницу, которой нет.
      const key = rid ?? getByPath<string>(r, "name");
      return key ? `${childBase}/${key}` : null;
    },
  }).filter((c) => !hidden.has(c.header));
  // Столбец действий — только когда у ресурса есть строчные действия. Для read-only
  // (напр. образы) не рисуем пустой столбец.
  if (resourceHasRowActions(childSpec)) {
    columns.push({
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (row) => (
        <RowActionsMenu spec={childSpec} row={row} basePath={childBase} projectId={projectId || null} editAsPanel />
      ),
    });
  }

  if (isError) return <ErrorResult error={error} />;

  // Пустое состояние — welcome (только когда детей реально нет; промах поиска
  // показывается внутри таблицы). Подпись кнопки — короткое «Создать», её
  // ставит сам экран пустого состояния: предмет назван его же заголовком.
  //
  // «Создайте первый» — утверждение об ОТСУТСТВИИ детей, поэтому оно допустимо
  // только когда список дочитан. Пока за курсором есть ещё, детей может не быть
  // на прочитанных страницах и быть на следующих: приглашение создать поверх
  // недочитанного списка сообщало бы об отсутствии, которого никто не проверял.
  if (!isLoading && ownRows.length === 0 && !hasMore) {
    return <ResourceEmptyState spec={childSpec} onCreate={() => navigate(createPath)} />;
  }

  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      {/* Фильтры (поиск/колонки) поднимаются на уровень имени ресурса (зона 3,
          правый слот) через HeaderSlotPortal — req3. */}
      <HeaderSlotPortal>
        {facet && (
          // Высота и радиус — как у соседей по ряду (поиск, выбор столбцов).
          // Здесь стоял `size="small"`: отбор был на восемь точек ниже поиска,
          // и ряд из трёх ручек читался как два ряда. Размер ручки задаёт ряд, а
          // не отдельная ручка.
          <Select
            style={{ minWidth: 180 }}
            value={facetVal}
            onChange={setFacetVal}
            aria-label={facet.label}
            options={[
              { value: "", label: `${facet.label}: все` },
              ...facet.options.map((o) => ({ value: o.value, label: o.label })),
            ]}
          />
        )}
        <TableSearch value={search} onChange={setSearch} scope={scope} />
        <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
      </HeaderSlotPortal>
      {/* Split-зона: таблица (сжимается) слева + встроенная панель тегов справа.
          Панель раздвигает таблицу вбок (не оверлей), живёт внутри лайаута. При
          сжатии у таблицы появляется h-скролл, а начальный отрезок до колонки
          идентичности залипает — общая таблица делает это безусловно. */}
      <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex" }}>
        <div style={{ flex: 1, minWidth: 0, minHeight: 0 }}>
          <ResourceTable
            rows={rows}
            columns={columns}
            loading={isLoading}
            selectedRowKey={tagsRepo}
            rowKey={(r) => getByPath<string>(r, "id") ?? getByPath<string>(r, "name") ?? Math.random().toString()}
            // Сужение здесь клиентское, поэтому порядок законен ровно тогда,
            // когда прочитан весь набор.
            complete={rowsAreComplete(scope)}
            empty={q || facetVal ? noMatchesText(scope) : undefined}
          />
        </div>
        {childSpec.id === "repositories" && (
          <div
            style={{
              width: tagsRepo ? 360 : 0,
              flexShrink: 0,
              minHeight: 0,
              overflow: "hidden",
              transition: "width .2s ease",
              marginLeft: tagsRepo ? 12 : 0,
            }}
          >
            {tagsRepo && (
              <RepositoryTagsPanel registryId={parentId} repository={tagsRepo} onClose={() => setTagsRepo(null)} />
            )}
          </div>
        )}
      </div>
      {/* Продолжение курсора — ТОТ ЖЕ вид, что на странице списка: общего числа
          List не отдаёт, поэтому «ещё» — это наличие курсора, а не арифметика по
          общему числу.

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
  // Адрес ресурса, адресуемого через родителя, закрывается ПАРАМЕТРАМИ МАРШРУТА:
  // репозиторий существует в своём реестре, и реестр называет адрес страницы, а
  // не догадка. Пока сегмент закрыть нечем, запрос не уходит — иначе он уезжает
  // с литералом `{registryId}`, а отказ края неотличим от «ресурса нет».
  //
  // Выражение адреса остаётся ЦЕЛЬНЫМ, а подстановка применяется снаружи:
  // перепись поверхности API резолвит голову по имени `spec.apiPath`, и путь,
  // спрятанный за промежуточную переменную, из-под её наблюдения выпадает.
  const detailPath = fillPathFromParams(`${spec.apiPath}/${uid}`, routeParams);
  const specAddressable = !hasUnresolvedPathSegment(detailPath);
  const navigate = useNavigate();
  const location = useLocation();
  const invalidate = useInvalidateResourceList();

  // detailBase = URL до и включая /:uid (надёжно при любой вложенности/модуле).
  const marker = `/${uid ?? ""}`;
  const mIdx = uid ? location.pathname.indexOf(marker) : -1;
  const detailBase =
    mIdx >= 0
      ? location.pathname.slice(0, mIdx + marker.length)
      : `${resourceProjectPath(spec.id, projectId) ?? `/${spec.route}`}/${uid}`;

  // Карточка узнаёт о своих изменениях ПОТОКОМ (#1021): опрос остаётся ровно до
  // тех пор, пока владелец журнала не назвал этот вид. У реестра журнал ведёт
  // ОДИН вид — сами реестры, — поэтому карточка репозитория и карточка тега
  // остаются на опросе САМИ, без второго решения здесь: покрытие читается по
  // проводу (`hub.covers`), а не выводится из имени домена.
  const { streamed } = useResourceStream({
    specId: spec.id,
    projectId: projectId ?? null,
    invalidate: [spec.id, "shell-detail", detailPath],
    enabled: !!uid && specAddressable,
  });

  const { data, isLoading, isError, error } = useQuery({
    // Разрешённый путь — часть ключа: у ресурса под родителем имя уникально
    // только внутри родителя, и два репозитория `nginx` в разных реестрах без
    // него делили бы один кэш, показывая друг друга.
    queryKey: [spec.id, "shell-detail", detailPath],
    queryFn: () => api.get<Record<string, unknown>>(detailPath),
    enabled: !!uid && specAddressable,
    refetchInterval: streamed ? false : 5_000,
    staleTime: 0,
  });

  const ext = useMemo(() => detailExtension(spec.id), [spec.id]);
  const name = (data ? getByPath<string>(data, "name") : "") || (data ? ext?.title?.(data) : "") || (uid ?? "");

  const listHref = resourceProjectPath(spec.id, projectId);
  const breadcrumb = useMemo(() => {
    const childSpec = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
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
                <Link to={`${detailBase}/${childRoute}`}>{sec(childSpec?.plural ?? childRoute ?? "")}</Link>
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
          extActions={ext?.headerActions?.({ data, projectId: projectId ?? null, detailBase, navigate })}
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
          Создать
        </Button>
      );
    }
    return null;
  }, [mode, headerTabId, data, spec, projectId, detailBase, ext, navigate]);
  useHeaderRight(headerActions);

  if (isLoading && !data) {
    return (
      <div style={{ padding: 48, textAlign: "center" }}>
        <Spin />
      </div>
    );
  }
  if (isError || !data) {
    return <ErrorResult error={error} />;
  }

  const related = spec.related ?? [];
  const extCtx = { data, projectId: projectId ?? null, detailBase, navigate };

  // ── Обзор: 5 обязательных + доменные строки расширения ──
  // Копирование объявляется у той строки, чьё значение КЛАДУТ В ЧУЖОЕ ПОЛЕ:
  // идентификатор (он и есть внешняя координата ресурса), имя и описание.
  // Дате и меткам оно не объявлено — отформатированную дату некуда вставить, а
  // набор меток не строка.
  const description = getByPath<string>(data, "description") ?? "";
  const overviewItems: DescItem[] = [
    {
      label: "Идентификатор",
      value: <MonoValue value={getByPath<string>(data, "id") ?? ""} />,
      copy: getByPath<string>(data, "id") || undefined,
    },
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
          {/* Обзор — поверхность-секция со строками «ключ · значение», как у
              остальных карточек продукта. Здесь стоял `Descriptions` antd: он
              рисует рамку на КАЖДОЙ ячейке, то есть решётку там, где язык
              карточки держит одну линию между строками, и столбца копирования у
              него нет вовсе — объявленное строкой `copy` не рисовалось НИ У
              ОДНОЙ строки обзора, включая идентификатор.

              Заголовок секции не повторяет имя вкладки: «Обзор» уже сказано над
              зоной 3, и второй такой же заголовок сообщал бы это дважды подряд. */}
          <div style={{ maxWidth: DETAIL_CONTENT_WIDTH }}>
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
  // — то есть вкладка появлялась у всех, включая вложенные и каталожные ресурсы
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
    // Вкладку строит общий `JsonTab`: вместе с полотном он приносит копирование
    // всего документа в правый слот строки имени — туда же, где стоят ручки
    // связанных таблиц. Здесь полотно рисовалось голым, и скопировать документ
    // было нечем: выделить прокручиваемое полотно мышью нельзя.
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
          navigate(detailBase);
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
    if (id === "overview") navigate(detailBase);
    else navigate(`${detailBase}/${id}`);
  };

  // Зона-2 шапка для форм (edit/child-create): действие + тип + иконка ресурса
  // формы — контекст переезжает в блок табов, форма в зоне 3 свою шапку не дублирует.
  const childForHeader = mode === "child-create" && childRoute ? specByRoute(childRoute) : undefined;
  const headerTitle = mode === "edit" ? spec.plural : mode === "child-create" ? childForHeader?.plural : undefined;
  const headerIcon =
    mode === "child-create" && childForHeader ? <ResourceIcon specId={childForHeader.id} /> : undefined;

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
        headerTitle={headerTitle}
        headerIcon={headerIcon}
      />
    </DetailHeaderProvider>
  );
}
