// ResourceListPage — generic страница списка ресурсов на antd.
//
// Polling 3 сек (через useResourceList).

import { useMemo, useState } from "react";
import { Link, useParams, useLocation, useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Checkbox, Input, Segmented, Select, Typography } from "antd";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PlusOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath, type ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu, resourceHasRowActions } from "@shared/components/molecules/RowActionsMenu";
import { filterExpressionValue, useDebouncedValue } from "@shared/lib/list-search";
import { type ReactNode } from "react";
import { ResourceEmptyState } from "@shared/components/molecules/ResourceEmptyState";
import { ProjectRequiredEmpty } from "@shared/components/molecules/ProjectRequiredEmpty";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { PageHead, PAGE_PADDING } from "@shared/components/organisms/DetailShell/PageHead";
import {
  ColumnSettings,
  TableSearch,
  useHiddenColumns,
  type ToggleCol,
} from "@shared/components/molecules/TableToolbar";
import { useResourceList } from "@shared/lib/use-resource-list";
import { listViewState } from "@shared/lib/list-view-state";
import { searchFilterExpression } from "@shared/lib/list-search-filter";
import { labelFilterActive, parseLabelQuery, rowMatchesLabels } from "@shared/lib/list-label-filter";
import { clientScope, scopeSuffix, type NarrowingScope } from "@shared/lib/list-scope";

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

  // Выделения строк и группового удаления ЗДЕСЬ НЕТ — решением владельца
  // «удаление по чекбоксам убрать для всех ресурсов». Удаляют по одной строке
  // из её меню действий. Абзац стоит вместо снятого кода намеренно: без него
  // следующий читатель заводит флажки заново, приняв их отсутствие за упущение.

  // Поиск: спрашивает СЕРВЕР, если ресурс это объявил. Способов два, и они НЕ
  // взаимозаменяемы — у них разные операторы и разные предметы:
  //   `search.serverTerm`  — ВЫДЕЛЕННОЕ слово запроса, которое владелец толкует
  //                          сам (`search="…"`: у пользователя имени нет вовсе,
  //                          ищут по почте или идентификатору);
  //   `serverSearchField`  — НАСТОЯЩЕЕ поле ресурса, по которому ищут подстроку
  //                          (`name CONTAINS "…"`).
  // Объявлять оба на одном ресурсе запрещено (см. `resource-spec.ts`). Если это
  // всё же случилось, выигрывает `serverTerm` — разрешение здесь ради
  // детерминизма, а не как второй законный режим.
  const serverSearchTerm = spec.search?.serverTerm ?? null;
  const serverSearch = spec.serverSearchField;
  const asksServer = Boolean(serverSearchTerm || serverSearch);
  // Ввод отстаёт на четверть секунды — иначе каждая нажатая клавиша уходит
  // отдельным запросом. При клиентском сужении спрашивать некого, поэтому
  // отставание не берётся вовсе (`0` отдаёт значение как есть, а не через
  // нулевой таймер).
  const debouncedQuery = useDebouncedValue(query, asksServer ? 250 : 0);
  const searchExpr = useMemo(() => {
    if (serverSearchTerm) {
      const q = filterExpressionValue(debouncedQuery);
      return q ? `${serverSearchTerm}="${q}"` : null;
    }
    return serverSearch ? searchFilterExpression(serverSearch, debouncedQuery) : null;
  }, [serverSearchTerm, serverSearch, debouncedQuery]);
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
  // Область, о которой судит строка поиска. Три состояния, не два: сужал сервер
  // (`server`) · сужает браузер над дочитанным списком (`whole`) · сужает
  // браузер над прочитанной частью (`loaded`). Подписи ко всем трём живут в
  // одном месте — иначе соседние списки говорят об одном предмете разными
  // словами (`@shared/lib/list-scope`).
  const searchScope: NarrowingScope = asksServer ? "server" : clientScope(hasMore);
  const setServerFilter = (param: string, value: string) =>
    setServerFilters((prev) => {
      const next = { ...prev };
      if (value) next[param] = value;
      else delete next[param];
      return next;
    });

  // КРОШКИ НАЗЫВАЮТ ПУТЬ, ЗАГОЛОВОК — ПРЕДМЕТ, И ДВАЖДЫ ОДНО НЕ ГОВОРЯТ.
  //
  // Здесь крошки оканчивались именем раздела — тем же словом, что стоит
  // заголовком страницы двадцатью точками ниже и вчетверо крупнее. Пока
  // заголовка у списка не было, это было единственное место, где раздел
  // назывался; теперь он назван, и последний шаг пути стал повторением.
  //
  // На КАРТОЧКЕ ресурса такого повторения нет и крошки остаются полными: там
  // заголовок — имя экземпляра, а раздел в пути ведёт назад, к списку.
  const breadcrumb = useMemo(
    () => (spec.serviceTitle ? <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text> : null),
    [spec.serviceTitle],
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
          {/* Короткое «Создать» — решение владельца. Предмет назван заголовком
              страницы в двадцати точках левее и вчетверо крупнее; полная подпись
              повторяла его и занимала пол-строки инструментов.

              Полная форма (`createActionLabel`) остаётся там, где предмет рядом
              НЕ назван: пункт выпадающего списка и текст отказа. */}
          Создать
        </Button>
      </Link>
    );
  }, [spec, createTarget]);
  // Слот шапки приложения ПУСТ намеренно: «Создать» переехала в строку
  // инструментов таблицы (см. `actions` ниже). Оставить её ещё и здесь значило
  // бы показать одну кнопку дважды — а убрать вызов вовсе нельзя: слот держит
  // состояние между страницами, и не сброшенный, он донёс бы чужую кнопку на
  // следующую открытую страницу.
  useHeaderRight(null);

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

  // ОТБОР ПО МЕТКАМ — КЛИЕНТСКИЙ, И ЭТО РЕШЕНИЕ ПЛАТФОРМЫ, А НЕ УПУЩЕНИЕ (#1021).
  //
  // Оси подписки на поток изменений объявлены НЕИЗМЕНЯЕМЫМИ (`kinds` × `project`
  // × `ids`), а метка мутабельна: ресурс входит в выборку и выходит из неё
  // правкой метки. Сервер не может сказать «вышел из выборки» иначе как
  // синтетическим снятием, а подписчик прочитал бы его как «ресурс удалён» и
  // показал бы человеку снос при живом ресурсе. Поэтому судит тот, у кого есть
  // ОБА состояния строки, — страница, перечитавшая список.
  //
  // Показывается там, где ресурс метки НЕСЁТ, и признак этого ВЫВОДИТСЯ из
  // спеки, а не выписывается перечнем идентификаторов рядом (как `hasZoneFilter`
  // выше): выписанный перечень разошёлся бы с реестром молча — завели бы метки
  // четырнадцатому ресурсу, а ручку ему никто не дал. Ручка, сужающая по тому,
  // чего у ресурса нет, отвечала бы «ничего не найдено» на любой ввод.
  const hasLabelFilter = (spec.columns ?? []).some((c) => c.path === "labels");
  const [labelQuery, setLabelQuery] = useState("");
  // Отставания нет намеренно: спрашивать некого — отбор целиком в браузере.
  const labelTerms = useMemo(
    () => (hasLabelFilter ? parseLabelQuery(labelQuery) : []),
    [hasLabelFilter, labelQuery],
  );
  // СУЖАЕТ ЛИ ОТБОР — ОДНО имя на оба употребления: ветку отбрасывания строки и
  // признак `anyFilterActive`. Второе выражение того же предмета разошлось бы с
  // первым молча, и разошлось бы именно там, где расхождение не видно: список
  // выглядел бы отфильтрованным, а пустой его результат звал бы создать первый
  // ресурс (`labelFilterActive`, дефект #927).
  const labelNarrowing = hasLabelFilter && labelFilterActive(labelQuery);
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
      // ЗДЕСЬ СТОЯЛ ОТБОР АДРЕСОВ ПО НАЛИЧИЮ ВНЕШНЕГО — снят как дефект #927.
      //
      // Он отбрасывал внутренние адреса молча и НЕ считался сужением, поэтому
      // список уходил не в «ничего не найдено», а в приветственное состояние:
      // консоль утверждала арендатору «адресов нет» там, где край ответил
      // «есть». Тихо — у арендатора с обоими видами список просто короче
      // настоящего, и по экрану этого не понять.
      //
      // Три места дерева говорили обратное этому отбору: раздел назван
      // «IP-адреса», а не «Публичные IP»; его пустое состояние обещает, что
      // адрес бывает внутренним и публичным; функция вида умеет печатать
      // «Внутренний» — значение, которого на странице не бывало by
      // construction. Комментарий отбора ссылался на карточку подсети, но она
      // отвечает на другой вопрос — «какие адреса в ЭТОЙ подсети», а не «какие
      // адреса есть у проекта».
      //
      // Четвёртое место — в этом же компоненте: `rowZone` читает зону
      // ВНУТРЕННЕГО адреса первой, то есть фильтр зоны рассчитан на строки,
      // которые отбор до него не допускал.
      //
      // Заодно ушёл спецслучай одного ресурса в общем компоненте.
      // Метки — ПЕРВЫМИ: они сужают НАБОР строк, среди которых потом ищут, а не
      // уточняют результат поиска. Предикат чистый и своего состояния не держит:
      // держи он запомненный набор подходящих идентификаторов, строка, потерявшая
      // метку, осталась бы в списке до перезагрузки — ровно тот дефект, ради
      // которого отбор и пишется.
      if (labelNarrowing && !rowMatchesLabels(row, labelTerms)) return false;
      if (hasZoneFilter && zone !== "all" && rowZone(row) !== zone) return false;
      if (hasSystemFilter && roleKind !== "all") {
        const isSystem = getByPath<boolean>(row, "is_system") === true || getByPath<boolean>(row, "isSystem") === true;
        if (roleKind === "system" && !isSystem) return false;
        if (roleKind === "custom" && isSystem) return false;
      }
      if (!q) return true;
      // Сузил сервер — резать его ответ повторно нельзя: он отбирал строки по
      // своим полям, а здесь их может не оказаться вовсе.
      if (serverSearchTerm) return true;
      return (spec.search?.fields ?? ["name", "id"]).some((f) =>
        (getByPath<string>(row, f) ?? "").toLowerCase().includes(q),
      );
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, query, serverSearch, serverSearchTerm, zone, hasZoneFilter, hasSystemFilter, roleKind, spec.id, labelNarrowing, labelTerms]);

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
  // ОТБОР ПО МЕТКАМ ОБЪЯВЛЕН СУЖЕНИЕМ. Ветка выше выбрасывает строку, значит она
  // обязана попасть сюда: признак сужения выбирает, что человек увидит на пустом
  // результате — «ничего не найдено» или приглашение «создайте первый». Второе —
  // утверждение о ресурсах арендатора, и оно ложно, когда край ответил «есть», а
  // строки спрятал отбор (правило 13 `ui.md`, дефект #927).
  const anyFilterActive =
    labelNarrowing ||
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

  // CTA «Создать» — в шапке страницы (useHeaderRight).
  //
  // ─────────────────────────────────────────────────────────────────────────
  // ПОЧЕМУ У СПИСКА НЕТ НИ ИКОНКИ, НИ НАЗВАНИЯ
  //
  // Второй уровень сайдбара называет выбранный тип — иконкой и подписью, — и
  // подсвечивает его. Хлебные крошки называют его же. Шапка таблицы говорила
  // ТО ЖЕ САМОЕ третий раз: та же иконка, то же название. Три одинаковых
  // утверждения на одном экране не сообщают втрое больше, они спорят за
  // внимание с содержимым, ради которого страницу открыли.
  //
  // Осталось то, чего больше не говорит НИКТО: инструменты над таблицей.
  // Надзаголовок «Список» снят по той же причине — он называл способ показа
  // вместо предмета и стоял одинаковым на каждой странице списка, то есть не
  // различал ничего.
  //
  // СЧЁТЧИКА СТРОК ЗДЕСЬ БОЛЬШЕ НЕТ — решением владельца «отображать кол-во
  // элементов не нужно». Прежняя редакция этого абзаца называла его средним
  // звеном порядка («сузить → прочитать, сколько получилось → сделать
  // что-нибудь») и пережила его снятие.
  //
  // ПОРЯДОК СТРОКИ ИНСТРУМЕНТОВ отвечает порядку работы: сузить (поиск и
  // отборы) → сделать что-нибудь с показанным (выбор столбцов). Обе группы
  // прижаты вправо и стоят НАД таблицей — так же, как во встроенной таблице
  // карточки ресурса.
  const listToolbar = ({
    narrowing,
    actions,
  }: {
    narrowing?: ReactNode;
    actions?: ReactNode;
  }) => (
    // ТА ЖЕ ШАПКА, ЧТО У КАРТОЧКИ РЕСУРСА (`PageHead`), решение владельца.
    //
    //   список:   ——— VPC                карточка:  ——— ОБЛАЧНАЯ СЕТЬ
    //             Облачные сети                     networks-722779
    //
    // Принцип один на обе страницы: надзаголовок — РОДИТЕЛЬ предмета, заголовок —
    // сам предмет. У списка родитель — сервис, предмет — тип во множественном
    // числе; у карточки родитель — тип, предмет — экземпляр. Переход между ними
    // перестаёт читаться как переход в другой продукт: кегль, вес, межбуквенное
    // расстояние, высота блока и линия снизу приходят из одного места.
    //
    // Здесь стоял собственный заголовок списка — 17px/600 в строке ручек. Он
    // называл предмет, но конструкцией не совпадал с карточкой ни в чём, и две
    // страницы одного ресурса выглядели сделанными разными руками.
    //
    // Чего в шапке НЕТ и почему — см. `PageHead`: ни надзаголовка «Список» (он
    // сообщал способ показа вместо предмета и не различал страницы), ни
    // счётчика (снят решением владельца ВЕЗДЕ), ни иконки-плитки (иконка —
    // признак идентичности экземпляра, у типа её место в навигации).
    <PageHead
      title={spec.plural}
      right={
        // Ручки — ОДНОЙ группой, как в правом слоте шапки встроенной таблицы
        // карточки. Порядок отвечает порядку работы: сузить (поиск, отборы) →
        // выбрать, что показывать (столбцы) → добавить своё (создать).
        //
        // Перенос разрешён: у ресурсов с отбором по зоне и серверными фильтрами
        // группа шире, чем остаток строки на узком окне, и без переноса она
        // выдавливала бы заголовок.
        <div
          // Класс задаёт ОДНУ высоту и один радиус всем ручкам ряда — см. его
          // объявление в общем листе. Прежде каждая приносила своё: поле поиска
          // от antd, отбор-селект, кнопка со значком и первичная кнопка — четыре
          // разные высоты в одном ряду, и полоса читалась как случайно
          // составленная.
          className="kc-list-tools"
          style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", justifyContent: "flex-end" }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>{narrowing}</div>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>{actions}</div>
        </div>
      }
    />
  );

  // Пустой список: та же поверхность и ТОТ ЖЕ ЗАГОЛОВОК, что у заполненной
  // страницы, но БЕЗ строки инструментов.
  //
  // Ручек нет: сужать нечего — в это состояние страница приходит только при
  // пустом наборе и БЕЗ сужения (см. `listViewState`); выбирать столбцы не у
  // чего. Осталась бы полоса ручек: подпись к тому, чего нет.
  //
  // ЗАГОЛОВОК ЕСТЬ, И ЭТО НЕ СИММЕТРИЯ РАДИ СИММЕТРИИ. Крошки перестали называть
  // раздел последним звеном ИМЕННО потому, что его называет заголовок страницы
  // (см. разбор у `breadcrumb` выше). Здесь заголовок сняли вместе со строкой
  // инструментов — и на единственной странице, где нет ни одной строки, по
  // которой тип угадывается, раздел перестал называться вообще: ни крошкой, ни
  // заголовком. Два решения, каждое по отдельности верное, сложились в потерю.
  //
  // Прежняя редакция этого абзаца объясняла отсутствие заголовка тем, что «ни
  // заголовка, ни счётчика в полосе не осталось, так что прыгать нечему». Про
  // счётчик это верно; про заголовок — нет: он въехал в `PageHead` той же
  // правкой, которая уносила полосу.
  if (showWelcome) {
    return (
      <div
        className="kc-surface"
        // ПУСТОЕ СОСТОЯНИЕ СТОИТ ПО ЦЕНТРУ — по обеим осям (решение владельца).
        //
        // Прежде оно жило в блоке с отступом, то есть прижималось к левому
        // верхнему углу: экран, объясняющий, что смотреть не на что, читался как
        // сбившаяся вёрстка, а не как обращение к человеку.
        //
        // Центровку исполняет сама панель (`StatePanel`), но её `minHeight: 100%`
        // резолвится, только если высота есть у РОДИТЕЛЯ. Пока между ними стоял
        // блок по содержимому, процент считался от нуля, и панель оставалась
        // сверху при заполненной высотой поверхности. Поэтому звеном заполнения
        // здесь становится сама поверхность — колонка с `flex: 1`.
        style={{
          padding: PAGE_PADDING,
          flex: 1,
          minHeight: 0,
          height: "100%",
          overflow: "auto",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Заголовок — та же конструкция и та же геометрия, что у заполненного
            списка и у карточки: высота блока и место под линию в `PageHead`
            зарезервированы независимо от содержимого правого слота, поэтому
            переход «пусто → появились строки» не сдвигает заголовок. Правый
            слот здесь пуст — ручек нет. */}
        <PageHead title={spec.plural} />
        <div style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
          <ResourceEmptyState spec={spec} onCreate={() => navigate(createTarget)} />
        </div>
      </div>
    );
  }

  return (
    <div
      className="kc-surface"
      // Поля страницы живут ЗДЕСЬ, а не на обёртке рабочей области: та отдана
      // под боковую панель ресурса, примыкающую к рейлу вплотную.
      //
      // `flex: 1` рядом с `height: 100%` — не избыточность. Процентная высота
      // резолвится, только если у родителя высота определена; звеном заполнения
      // поверхность становится сама, и тогда она заполняет рабочую область у
      // любого вызывающего, а не только у того, чья обёртка — колонка с заданной
      // высотой. У блочного родителя `flex` инертен и ничего не меняет.
      style={{
        padding: PAGE_PADDING,
        flex: 1,
        minHeight: 0,
        height: "100%",
        overflow: "hidden",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {/* Строка инструментов фиксирована сверху и НЕ скроллится вместе с телом
          таблицы. Отдельной линии под ней нет: полоса во всю ширину с
          разделителем читалась как ещё одна шапка над шапкой страницы, и
          решением владельца ручки стоят одной группой у правого края.

          Перечня содержимого здесь больше нет: он назывался «иконка + „Список“ +
          plural + счётчик + фильтры», пережил снятие первых трёх и остался
          описанием того, чего в строке уже не было. Четвёртое — счётчик — снято
          тем же решением, что и полоса. */}
      <div style={{ flexShrink: 0 }}>
        {listToolbar({
          narrowing: (
            <>
              {/* Одна и та же строка ввода означает на разных страницах разное —
                значит она обязана об этом СКАЗАТЬ. Серверный поиск спрашивает
                весь список; клиентский судит о прочитанных страницах, и молча
                выдавать второе за первое нельзя: пользователь читает «ничего не
                найдено» как утверждение об отсутствии ресурса. Область называет
                сам TableSearch — тот же, что во встроенных таблицах дочерних
                ресурсов, поэтому строка поиска в продукте одна. Прежняя
                Input.Search несла вдобавок кнопку-лупу: второй способ сделать
                то, что и так происходит на вводе, и единственное поле продукта
                с приклеенной кнопкой. */}
              {/* ОТБОРЫ — ПЕРЕД ПОИСКОМ (решение владельца). Сужение по зоне
                  меняет НАБОР строк, среди которых потом ищут; стоя после поля
                  поиска, оно читалось как уточнение к нему, хотя порядок
                  обратный. */}
              {hasZoneFilter && <Select value={zone} onChange={setZone} options={zoneOptions} style={{ width: 220 }} />}
              {/* Отбор по МЕТКАМ. Стоит среди отборов, а не рядом с поиском, и это
                  не раскладка, а разные вопросы: поиск сравнивает ПОДСТРОКУ имени,
                  отбор метки — значение ЦЕЛИКОМ. Свести их в одно поле значило бы
                  сделать точный вопрос незадаваемым.

                  Область называется в самом плейсхолдере — тем же хвостом, что у
                  строки поиска, и по той же причине: одна и та же ручка означает
                  на разных страницах разное. Область здесь ВСЕГДА клиентская
                  (`clientScope`), а не `searchScope`: сервер по меткам не сужает
                  ни на одном ресурсе, и назвать этот отбор серверным на странице
                  с серверным поиском значило бы соврать про него. */}
              {hasLabelFilter && (
                <Input
                  value={labelQuery}
                  onChange={(e) => setLabelQuery(e.target.value)}
                  allowClear
                  placeholder={`Метки: env=prod ${scopeSuffix(clientScope(hasMore))}`}
                  title={
                    "Условия через пробел, соединяются И. `ключ` — метка есть, значение любое; " +
                    "`ключ=значение` — значение совпадает целиком; `ключ=` — значение пустое."
                  }
                  style={{ width: 260 }}
                />
              )}
              <TableSearch
                value={query}
                onChange={setQuery}
                scope={searchScope}
                // Перекрытие ресурса называет ПРЕДМЕТ поиска (у пользователя имени
                // нет вовсе — ищут по почте), область по-прежнему называем мы:
                // иначе единственный ресурс с перекрытием оказывался бы и
                // единственным, чья строка поиска о своей области молчит.
                placeholder={
                  spec.search?.placeholder ? `${spec.search.placeholder} ${scopeSuffix(searchScope)}` : undefined
                }
                width={320}
              />
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
            </>
          ),
          actions: (
            <>
              <ColumnSettings columns={toggleCols} hidden={hidden} onToggle={toggleHidden} />
              {/* «Создать» — ПОСЛЕДНЕЙ в строке инструментов (решение владельца).
                  
                  Прежде она жила в правом слоте шапки приложения, то есть
                  этажом выше и в другой зоне: рука шла к ней через всю страницу,
                  а сама кнопка стояла в одном ряду с элементами каркаса (выбор
                  области, профиль) и читалась как принадлежащая им, а не
                  таблице.
                  
                  Место после «Столбцов» отвечает порядку работы: сузить → выбрать,
                  что показывать → добавить своё. Действие, ИЗМЕНЯЮЩЕЕ набор,
                  стоит после всех, которые его только показывают. */}
              {cta}
            </>
          ),
        })}
      </div>

      {/* Тело таблицы заполняет остаток поверхности и скроллится внутри
          (горизонтально при широких колонках, вертикально при длинном списке).
          Прокрутка одна и она ЗДЕСЬ: поверхность выше стоит с `overflow: hidden`.

          `display: flex` на этой обёртке — не украшение раскладки, а условие
          заполнения. Обёртка таблицы (`.kc-table-fill`) просит `height: 100%`, а
          процентная высота через БЛОЧНУЮ границу, чья высота взялась из `flex`,
          в Chrome не резолвится — и таблица оседала по высоте строк, оставляя
          под собой полэкрана пустоты при заполненной высотой поверхности. Ровно
          это звено уже приходилось чинить этажом выше, у обёртки antd в шапке
          приложения; здесь тот же случай. */}
      <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
        {view === "error" ? (
          // Отказ — это текст, а не таблица: он держит отступ сам. У таблицы его
          // нет намеренно, чтобы границы строк доходили до краёв поверхности.
          <div style={{ padding: 20 }}>
            <ErrorResult error={error} />
          </div>
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
            //
            // Серверный поиск полноты НЕ добавляет: он сужает весь список, но
            // ответ по-прежнему приезжает страницей.
            complete={!hasMore}
            // Выбора строк НЕТ (решение владельца): удаление идёт по одной
            // строке из её же меню действий. Массовое удаление по галочкам —
            // самая дорогая ошибка в списке: она необратима, а подтверждение
            // называет число, которое читатель и так видел неверно.
          />
        )}
      </div>

      {/* Курсорная пагинация: общего числа у List нет, поэтому «ещё» — это
          наличие next_page_token, а не арифметика по общему числу. */}
      {view !== "error" && hasMore && (
        <div
          style={{
            flexShrink: 0,
            padding: "12px 18px",
            textAlign: "center",
            // Подвал отделён линией так же, как шапка: у поверхности две крышки,
            // и обе — линией от края до края, а не отступом.
            borderTop: "1px solid var(--kc-border)",
          }}
        >
          <Button loading={isFetchingMore} onClick={() => void fetchMore()}>
            Показать ещё
          </Button>
        </div>
      )}
    </div>
  );
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
