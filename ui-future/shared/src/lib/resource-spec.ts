// Форма описания ресурса — ОДНО объявление на всю консоль.
//
// Здесь живут `ResourceColumn`, `ListFilter` и `ResourceSpec`: типы, которыми
// generic ListPage / DetailShell / Create-Edit читают любой ресурс. Реестры
// (`resource-registry.tsx`) у приложений остаются СВОИ — они несут содержимое
// (какие ресурсы, какие колонки и поля), — но ФОРМУ импортируют отсюда и не
// объявляют у себя.
//
// Почему форма вынесена (KAC #132). Прежде она была выписана в пяти файлах:
// в общем реестре и по копии у compute / nlb / registry / storage. Копии
// разошлись — и разошлись МОЛЧА, потому что сверять их было нечем:
//   · `admin`, `mutationsReturnOperation`, `listFilters` были только в общей;
//   · `facet`, `loadAllPages` — только в трёх приватных;
//   · комментарий у `immutable?` в `form-schema.ts` существовал в ТРЁХ разных
//     редакциях, из которых верна была одна.
// Обнаружилось это не проверкой, а падением сборки сразу трёх приложений: проба
// `dev-proxy.test.ts` читала `spec.admin`, которого в их форме не было. То есть
// первым о расхождении узнал не автор правки, а тот, кто через неделю чинил
// чужую сборку.
//
// Что изменилось по существу: расхождение форм теперь НЕВЫРАЗИМО — объявление
// одно, и второго места, где его можно было бы завести, нет. Реэкспорт формы
// приложением (`export type { ResourceSpec } from "@shared/lib/resource-spec"`)
// расхождения не создаёт: у реэкспорта нет тела, он следует за источником сам.
// Повторное ОБЪЯВЛЕНИЕ — создаёт, и его ловит `scripts/check-resource-spec-single-source.mjs`.

import type { ReactNode } from "react";
import type { FormField } from "./form-schema";

export interface ResourceColumn {
  header: string;
  // Путь в плоском объекте: "name", "status", "zone_id"
  path: string;
  format?: "text" | "uid-short" | "datetime" | "status" | "code" | "list" | "references";
  className?: string;
  render?: (row: Record<string, unknown>) => ReactNode;
}

/** Фильтр списка, отправляемый в query-параметре (server-side, см. `listFilters`). */
export type ListFilter =
  | { kind: "toggle"; param: string; label: string; description?: string }
  | { kind: "ref"; param: string; label: string; refSpecId: string; allLabel: string };

/** Связанный дочерний ресурс — вкладка со встроенной таблицей в `ResourceShell`.
 *
 *  `childId` — ключ ребёнка в реестре; `filterField` — поле(я) ребёнка, ссылающееся
 *  на этого родителя (клиентское сужение; массив = ИЛИ по нескольким полям, напр.
 *  подсеть→адреса v4∪v6); `label` — заголовок вкладки (по умолчанию множественное
 *  число ребёнка).
 *
 *  КТО сужает список — ровно один из двух серверных механизмов ниже либо никто.
 *  Оба сразу объявить нельзя: тип это запрещает (`never`), поэтому противоречие
 *  «выражением ИЛИ параметром» не представимо и не решается втихую тем, что одна
 *  ветка кода выиграла у другой. */
export type RelatedSpec = {
  childId: string;
  filterField: string | string[];
  label?: string;
} & (
  | {
      /** Имя поля в белом списке выражения `filter` ВЛАДЕЛЬЦА ребёнка. Объявлено —
       *  вкладка просит `filter=<поле>="<id родителя>"`, и сужает СЕРВЕР.
       *
       *  Отличие от `filterField` существенно и не косметично, даже когда имена
       *  совпадают текстуально: `filterField` — путь в СТРОКЕ ответа (может быть
       *  вложенным), и фильтрует КЛИЕНТ, то есть только то, что успело приехать.
       *
       *  Список владельца ЗАКРЫТ: поле вне него — не «фильтр без эффекта», а
       *  `InvalidArgument` на всю вкладку. Паритет держит
       *  `related-server-filter-parity.test.ts` (читает белые списки из прод-кода
       *  сервиса), форму запроса — `ResourceShell.related.test.tsx`. */
      serverFilterField?: string;
      serverParamField?: never;
    }
  | {
      /** Имя ТИПИЗИРОВАННОГО поля списочного запроса владельца ребёнка (напр.
       *  `subnet_id` у адреса). Объявлено — вкладка передаёт его отдельным
       *  параметром запроса, и сужает СЕРВЕР.
       *
       *  Это ДРУГОЙ механизм, а не синоним выражения `filter`, и выбор между ними
       *  делает ВЛАДЕЛЕЦ, а не консоль: выражение разбирается по белому списку имён
       *  КОЛОНОК, поэтому ссылка, которой у владельца нет отдельной колонкой, им не
       *  выражается вовсе. Адрес — ровно этот случай: подсеть лежит внутри jsonb и
       *  в двух семьях, поэтому у него сужение по подсети — типизированное поле
       *  запроса.
       *
       *  Паритет с владельцем держит `related-server-param-parity.test.ts` (читает
       *  контракт и его читателя из дерева сервиса), форму запроса —
       *  `ResourceShell.related.test.tsx`. */
      serverParamField?: string;
      serverFilterField?: never;
    }
);

export interface ResourceSpec {
  id: string;
  // route path в SPA (без leading slash)
  route: string;
  // Полный URL-path для REST: /<domain>/v1/<plural>
  // Verbatim из proto google.api.http annotations.
  apiPath: string;
  // ключ массива в List response: "networks", "projects"
  payloadKey: string;
  // singular label для UI
  singular: string;
  // plural label
  plural: string;
  // родительный падеж ед.ч. («Обзор шлюзА», «Операции сетИ») — заголовок
  // мастер-ресурса в зоне-3 (обзор/операции/json). Fallback: plural.
  genitive?: string;
  description?: string;
  /** Service-domain заголовок (отображается в breadcrumb перед именем категории).
   *  Примеры: "Virtual Private Cloud", "IAM", "Администрирование". */
  serviceTitle?: string;
  // global = cluster-scoped, project = в выбранном Project, account = в выбранном Account
  scope: "global" | "project" | "account";
  // поддерживаемые операции (кнопки действий рендерятся по этим флагам)
  ops: {
    create: boolean;
    update: boolean;
    delete: boolean;
    restart?: boolean;
    start?: boolean;
    stop?: boolean;
  };
  // колонки для list-таблицы
  columns: ResourceColumn[];
  // schema полей формы (если undefined — fallback к JSON-editor)
  fields?: FormField[];
  // Path-template для drill-down link при клике на строку (плейсхолдер `:id`).
  // Если задан — кнопка в строке ведёт сюда вместо DetailPage. Используется
  // для иерархического drill-flow Account → Projects → VPC.
  childRoute?: string;
  // skeleton-объект для Create-формы.
  // projectId — выбранный Project (VPC/Compute scope).
  // accountId — выбранный Account (kacho.cloud.iam.v1.Project.account_id).
  template: (ctx: { projectId?: string; accountId?: string }) => unknown;
  // Опциональная нормализация payload перед отправкой на API.
  // Используется для конвертации form-internal представления (wrapper-объекты, toggle-поля)
  // в wire format (plain arrays, oneof etc.).
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
  /** Обратная sanitize: wire → form. Вызывается InlineResourceEditForm перед
   *  установкой initial form-state. Используется когда у формы есть array-of-ref
   *  или array-of-string поля, для которых wire-format = массив строк, а
   *  form-format = массив объектов `{value: "..."}` (см. NIC v4/v6_address_ids
   *  / security_group_ids; Subnet v4/v6_cidr_blocks). Без hydrate-адаптера
   *  RefSelect получает массив строк вместо объектов и не отображает
   *  выбранные значения в edit-режиме. */
  hydrate?: (obj: Record<string, unknown>) => Record<string, unknown>;
  /** Клиентская pre-submit валидация формы (form-values → сообщение об ошибке |
   *  null). Возвращает первый нарушенный инвариант (напр. IAM-1: Role.rules[]
   *  non-empty, definitionTier XOR); null = ок. Дополняет per-field required —
   *  для кросс-полевых/структурных гейтов. Используется и для инвариантов,
   *  которые backend отверг бы асинхронно через 1-2с (напр. LB: хотя бы одно
   *  семейство VIP включено). */
  validate?: (obj: Record<string, unknown>) => string | null;
  /** Path-template для internal/infra-проекции ресурса (плейсхолдер `{id}`).
   *  Если задан — на DetailPage появляется tab "jsonint", который делает
   *  GET <internalGetPath с подставленным {id}> и pretty-print'ит JSON-ответ.
   *  Пример: "/vpc/v1/networks/{id}:internal" — форма ГЛАГОЛЬНАЯ, через
   *  двоеточие: так объявлена единственная :internal-аннотация vpc
   *  (`InternalNetworkService.GetNetwork`). Прежний пример показывал слэшевую
   *  форму `/{id}/internal`, которой на поверхности нет, — и по образцу такое
   *  поле завели ещё раз.
   *  Поле ставится ТОЛЬКО когда у Internal*-сервиса есть http-аннотация:
   *  `InternalNetworkInterfaceService` её не имеет вовсе (pure gRPC
   *  service→service), поэтому вкладки internal у NIC быть не может.
   *  Большинство ресурсов поля не имеют. */
  internalGetPath?: string;
  /** Admin-плоскость ресурса (`Internal*`-сервис на cluster-internal listener).
   *  `apiPath` остаётся поверхностью ЧТЕНИЯ; когда задан `admin`, Create/Update/
   *  Delete уходят на `admin.basePath`. Нужно там, где публичный путь мутации
   *  вообще не смаршрутизирован — geo Region/Zone читаются на /geo/v1/{regions,
   *  zones}, а меняются на /geo/v1/internal/…; POST на публичный путь не
   *  обслуживается никем.
   *
   *  `readForEdit` — читать начальное состояние формы редактирования с
   *  `admin.basePath/{id}` (GetInternal): у двухпроекционного ресурса мутируемые
   *  поля (`status`, `infra°`) на публичной проекции отсутствуют, и форма,
   *  заполненная публичным чтением, показала бы оператору пустое поле там, где
   *  на самом деле есть значение. */
  admin?: { basePath: string; readForEdit?: boolean };
  /** Мутации ресурса отвечают `operation.Operation` (ban #9). Тогда ответ без
   *  operation-id — нарушение контракта, а не синхронный успех, и страница
   *  говорит об этом вместо того, чтобы отрапортовать «готово». Существенно
   *  потому, что internal-листенер не эмитит нулевые значения: Operation с
   *  `done=false` приходит вообще без ключа `done`. Ресурсы, чей admin-RPC
   *  честно отвечает самим ресурсом (vpc AddressPool), флаг не ставят. */
  mutationsReturnOperation?: boolean;
  /** Фильтры списка, уезжающие в query — то есть применяемые НА СЕРВЕРЕ, ко
   *  всему списку, а не к загруженной странице. Клиентский фильтр поверх
   *  курсорной страницы отфильтровал бы то, что успело приехать, и выдал бы
   *  результат за полный. */
  listFilters?: ListFilter[];
  /** Facet-фильтр списка (client-side по значению уже загруженного поля): напр.
   *  тип артефакта образа. Рендерит Select рядом с поиском и фильтрует
   *  загруженные строки. Отличие от `listFilters` существенно и не косметично:
   *  тот уезжает в query и потому судит обо ВСЁМ списке, а этот — только о том,
   *  что приехало на страницу. Поэтому facet осмыслен в паре с `loadAllPages`
   *  либо на заведомо коротких path-scoped списках. */
  facet?: { path: string; label: string; options: { value: string; label: string }[] };
  /** Грузить ВСЕ страницы списка (follow next_page_token) до рендера — чтобы
   *  client-side facet видел полный набор (path-scoped дети, напр. образы). */
  loadAllPages?: boolean;
  /** KAC-233: связанные дочерние ресурсы — отдельные табы со встроенными
   *  таблицами в ResourceShell. childId — ключ ребёнка в REGISTRY; filterField —
   *  поле(я) ребёнка, ссылающееся на этот ресурс (client-side фильтр; массив =
   *  OR по нескольким полям, напр. subnet→addresses v4∪v6). label —
   *  переопределение заголовка таба (по умолчанию childSpec.plural). */
  related?: RelatedSpec[];
  /** KAC-233: ссылки на документацию по типу ресурса (блок «Документация» в
   *  aside DetailShell). Kachō-style. */
  docs?: { label: string; href: string }[];
  /** KAC-233: welcome-копирайт для пустой таблицы этого ресурса (когда он
   *  показан как ребёнок и список пуст). Kachō-style. */
  emptyState?: { title: string; body: string; docs?: string[] };
}
