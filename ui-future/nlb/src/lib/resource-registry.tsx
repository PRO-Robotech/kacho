// Реестр ресурсов NLB-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC-remote. Домен — Network Load
// Balancer: LoadBalancer / Listener / TargetGroup. `compute-regions` — read-only
// справочник для ref-полей region_id (cross-service ref → geo.Region).

import type { FormField } from "@shared/lib/form-schema";
import { getByPath as getByPathImpl, setByPath } from "./path";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { LabelsCell } from "@/components/atoms/LabelsCell";
// Логическое свойство — ОДИН рендер на всю консоль (правило 6 `ui.md`).
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { NlbVipCell } from "@/components/molecules/NlbVipCell";
import {
  NlbVipSourceField,
  NlbDisabledZonesField,
  buildVipSourceOrNull,
  lbTypeFromPlacement,
  lbPlacementTypeFromPlacement,
} from "@/components/organisms/form/NlbVipSourceField";
import type { ResourceColumn, ResourceSpec } from "@shared/lib/resource-spec";
// Форма группы целей — ОДНА реализация на оба реестра (`@shared` и этот):
// ветви проверки живости были заведены только в `@shared`, а `/nlb/*` рисует
// этот модуль, поэтому три ветви из четырёх до пользователя не доезжали (#375).
import {
  healthCheckFields,
  hydrateHealthCheck,
  sanitizeHealthCheck,
  sanitizeTargets,
  targetsField,
} from "@shared/lib/target-group-form";
// Подписи сущностей и разделов — из единственного источника (@shared/lib/entity-names):
// литерал рядом с местом показа расходится молча, ссылка — нет.
import { ENTITIES, SERVICES } from "@shared/lib/entity-names";
import {
  isSystemScopedResource,
  resourceListPath,
  resourceServicePrefix,
  type ServicePrefix,
} from "@shared/lib/service-prefix";

/**
 * Ячейка логического поля контракта — словом, а не литералом `false`.
 *
 * Словаря форматов колонок логический вариант не несёт: списки этого remote
 * рисует общий рендер (`@shared/lib/spec-columns`), где ветка "text" отсеивает
 * только пустое значение, поэтому `false` доехал бы до `String(v)` и напечатался
 * английским литералом ровно в том случае, ради которого колонка и заводится, —
 * регион, закрытый для размещения. Пока в общем словаре нет логического
 * формата, ячейка задаётся собственным `render` колонки (его общий рендер
 * уважает), а не расширением локального union'а: локальное расширение
 * разъехалось бы с типом общего компонента, которому эта спека и передаётся.
 *
 * Отсутствие значения — НЕ «нет»: сервер, не приславший поле, ничего не
 * утверждает.
 */

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`, и импортируется
// сюда. Реэкспорт оставлен, чтобы потребители этого модуля не меняли импорты: у него
// нет тела, поэтому разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы
// здесь запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.

export type { ResourceColumn, ResourceSpec };

// ── Общие FormField-константы ──

// Compute/NLB name-regex — DNS-1123 (lowercase + цифры + дефисы).
const FIELD_NAME_COMPUTE: FormField = {
  name: "name",
  label: "Имя",
  type: "string",
  placeholder: "my-load-balancer",
  description:
    "Строчные латинские буквы, цифры, «-» и «_». Должно начинаться с буквы, длина до 63 символов. Можно оставить пустым.",
  pattern: "^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$",
};

const FIELD_DESCRIPTION: FormField = {
  name: "description",
  label: "Описание",
  type: "text",
  rows: 2,
  placeholder: "Краткое описание ресурса (опционально)",
};

const FIELD_PROJECT_ID: FormField = {
  name: "project_id",
  label: "Проект",
  type: "string",
  hidden: true,
};

const FIELD_LABELS: FormField = {
  name: "labels",
  label: "Метки",
  type: "labels",
};

export const REGISTRY: Record<string, ResourceSpec> = {
  // ====== geo/compute (read-only справочник для ref region_id) ======
  // Region — cross-service ref-цель (owner — geo). Read-only: registry-запись
  // нужна RefSelect'у для резолва apiPath/payloadKey/имени в dropdown'ах.
  "compute-regions": {
    id: "compute-regions",
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
    route: "compute-regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    accusative: "регион",
    plural: "Регионы",
    serviceTitle: SERVICES.compute.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Каталог регионов пуст",
      body:
        "Регион — верхний уровень оси размещения Kachō: внутри него живут зоны доступности, " +
        "и на него ссылаются балансировщики и группы целей. Каталог заводит администратор " +
        "облака — обратитесь к нему, если список пуст.",
      docs: ["Регионы и зоны доступности"],
    },
    columns: [
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      // Здесь стояла колонка «Статус» по полю `status`. Публичный geo.Region
      // такого поля не несёт и не нёс никогда: состояние региона живёт только
      // в `InternalRegion` на cluster-internal листенере (two-projection).
      // Колонка была пуста при любом ответе сервера. Доступность размещения
      // регион сообщает логическим `open_for_placement`.
      {
        header: "Открыт для размещения",
        path: "open_for_placement",
        // Следствие, а не ответ «Да» (правило 6 `ui.md`): заголовок задаёт
        // вопрос, ячейка обязана дать содержательный ответ.
        //
        // Рисуется `render`, а не `format:"bool"`, и это НЕ обход общего
        // словаря: словарь логический вариант уже несёт, но сборщик колонок
        // этого модуля — форк, своей ветки `bool` у него нет, и колонка
        // печаталась бы литералом `false` ровно там, где она заводится.
        // `render` уважают ОБА сборщика, поэтому оба показывают одно и то же —
        // это и утверждает `spec-columns.bool.test.tsx`. Ветка формата вернётся
        // сюда вместе со сведением сборщика.
        //
        // Отсутствие значения — НЕ «закрыто»: сервер, не приславший поле, ничего
        // не утверждает, и прочерк здесь остаётся третьим состоянием. `BoolFact`
        // знает только истину и ложь (у него их ровно два), поэтому отсутствие
        // отсеивается ДО него — ровно так же, как это делает логическая ветка
        // общего словаря форматов.
        render: (row) =>
          typeof row.open_for_placement === "boolean" ? (
            <BoolFact
              value={row.open_for_placement}
              yes="Размещение доступно"
              no="Размещение закрыто"
              yesTone="good"
              noTone="attention"
            />
          ) : (
            <span style={{ opacity: 0.45 }}>—</span>
          ),
      },
    ],
    template: () => ({}),
  },

  // Compute Instance / VPC NIC / Zone — cross-service ref-цели для target-picker'а
  // TargetsManager (read-only, нужны RefSelect'у для apiPath/payloadKey/имени).
  "compute-instances": {
    id: "compute-instances",
    route: "instances",
    apiPath: "/compute/v1/instances",
    payloadKey: "instances",
    singular: "Виртуальная машина",
    accusative: "виртуальную машину",
    plural: "Виртуальные машины",
    serviceTitle: SERVICES.compute.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Создайте первую виртуальную машину",
      body:
        "Виртуальная машина — вычислительный ресурс Kachō с собственными дисками и сетевыми " +
        "интерфейсами. Балансировщик направляет на неё трафик, когда машина добавлена в целевую группу.",
      docs: ["Виртуальные машины"],
    },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },
  "network-interfaces": {
    id: "network-interfaces",
    route: "network-interfaces",
    apiPath: "/vpc/v1/networkInterfaces",
    payloadKey: "network_interfaces",
    singular: ENTITIES["network-interfaces"].singular,
    accusative: "сетевой интерфейс",
    plural: ENTITIES["network-interfaces"].plural,
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Создайте первый сетевой интерфейс",
      body:
        "Сетевой интерфейс — подключение ресурса к подсети Kachō: он держит IP-адреса и группы " +
        "безопасности. Целевая группа может ссылаться прямо на интерфейс, а не на машину целиком.",
      docs: ["Сетевые интерфейсы"],
    },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },
  zones: {
    id: "zones",
    // Идентификатор и есть имя: подписи у каталога размещения нет (#716).
    idIsTheName: true,
    route: "zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: ENTITIES.zones.singular,
    accusative: "зону",
    plural: ENTITIES.zones.plural,
    serviceTitle: SERVICES.system.title,
    scope: "global",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Каталог зон пуст",
      body:
        "Зона доступности — точка размещения внутри региона Kachō: к ней привязаны подсети, машины " +
        "и зональные балансировщики. Каталог заводит администратор облака — обратитесь к нему, " +
        "если список пуст.",
      docs: ["Регионы и зоны доступности"],
    },
    columns: [{ header: "Идентификатор", path: "id", format: "text", className: "font-mono" }],
    template: () => ({}),
  },

  // ====== vpc (read-only ref-цели для VIP-picker'а) ======
  // Subnet / Address — cross-service ref-цели (owner — vpc). Read-only
  // registry-записи нужны RefSelect'у в NlbVipSourceField, чтобы резолвить
  // apiPath/payloadKey + показать CIDR/IP в dropdown'е (extraInfoFor).
  subnets: {
    id: "subnets",
    route: "subnets",
    apiPath: "/vpc/v1/subnets",
    payloadKey: "subnets",
    singular: ENTITIES.subnets.singular,
    accusative: "подсеть",
    plural: ENTITIES.subnets.plural,
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Создайте первую подсеть",
      body:
        "Подсеть — диапазон IP-адресов внутри облачной сети Kachō, привязанный к зоне доступности. " +
        "Машины и балансировщики получают адреса именно из неё.",
      docs: ["Облачные сети и подсети"],
    },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "uid-short" },
    ],
    template: () => ({}),
  },

  addresses: {
    id: "addresses",
    route: "addresses",
    apiPath: "/vpc/v1/addresses",
    payloadKey: "addresses",
    singular: ENTITIES.addresses.singular,
    // Винительный падеж — от ИМЕНИ сущности, а не от прежней подписи: здесь
    // стояло «адрес», написанное под старое «Адрес», а имя сведено к «IP-адрес»
    // (@shared/lib/entity-names). Тот же ресурс в shared-реестре уже так и назван.
    accusative: "IP-адрес",
    plural: ENTITIES.addresses.plural,
    serviceTitle: SERVICES.vpc.title,
    scope: "project",
    ops: { create: false, update: false, delete: false },
    emptyState: {
      title: "Зарезервируйте первый IP-адрес",
      body:
        "IP-адрес — адрес, закреплённый за проектом: внутренний из подсети или публичный для доступа " +
        "извне. Балансировщик берёт отсюда свой VIP и держит его, пока адрес не освобождён.",
      docs: ["Адреса облачных ресурсов"],
    },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "uid-short" },
    ],
    template: () => ({}),
  },

  // ====== nlb ======
  // proto: kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService. Здесь стояло
  // имя пакета, выведенное из REST-сегмента `/nlb/v1/...`, — такого пакета в
  // стволе нет. Сегмент пути и имя пакета у этого домена РАЗНЫЕ, и оба верны;
  // мёртвое имя не воспроизводится даже в разборе ошибки, иначе разбор сам стал
  // бы её повторением (гейт lib/proto-package-names.test.ts считает такую цитату
  // живым утверждением — и правильно делает).

  "load-balancers": {
    id: "load-balancers",
    route: "load-balancers",
    apiPath: "/nlb/v1/networkLoadBalancers",
    // proto ListNetworkLoadBalancersResponse repeated-поле — `network_load_balancers`.
    payloadKey: "network_load_balancers",
    singular: ENTITIES["load-balancers"].singular,
    accusative: "балансировщик нагрузки",
    plural: ENTITIES["load-balancers"].plural,
    genitive: "Балансировщика нагрузки",
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    // Действий-глаголов у балансировщика нет: `:start`/`:stop` сняты с контракта,
    // административное включение/выключение выражается полем admin_state.
    ops: { create: true, update: true, delete: true },
    emptyState: {
      title: "Создайте первый балансировщик нагрузки",
      body:
        "Балансировщик нагрузки принимает трафик на VIP-адрес и разносит его между целями внутри " +
        "региона Kachō. Дальше к нему добавляют обработчики — они задают протокол и порт приёма.",
      docs: ["Балансировщики нагрузки"],
    },
    // Листенеры — связанный дочерний ресурс (within-service FK load_balancer_id):
    // отдельный registry-driven таб + auto-CTA «Создать листенер». Целевые группы
    // одним filterField не выражаются (связь идёт ЧЕРЕЗ листенер) — их вкладку
    // подаёт расширение карточки по spec.id (`lib/nlb-detail-extensions`).
    related: [{ childId: "listeners", filterField: "load_balancer_id", label: "Листенеры" }],
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        // Регион — ресурс каталога geo со своей карточкой, поэтому ссылка, а не
        // плоский текст: соседние колонки той же таблицы («Имя», «Адрес») ссылкой
        // уже были, и один предмет выглядел двумя (канон §9).
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      { header: "Схема", path: "type", format: "code" },
      {
        header: "Адрес",
        path: "v4_address_id",
        render: (row) => (
          <NlbVipCell
            v4AddressId={row.v4_address_id as string | undefined}
            v6AddressId={row.v6_address_id as string | undefined}
          />
        ),
      },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      {
        // NLB CONTRACT: placement — ЕДИНСТВЕННЫЙ авторитетный ввод режима на Create.
        // `type` / `placement_type` — производные output-only проекции; в запросе они
        // оставлены ТОЛЬКО чтобы клиент, который их выставил, получил явный
        // InvalidArgument. Форма их не шлёт.
        name: "placement",
        label: "Размещение",
        type: "enum",
        required: true,
        immutable: true,
        default: "EXTERNAL_REGIONAL",
        options: [
          { value: "EXTERNAL_REGIONAL", label: "EXTERNAL_REGIONAL — публичный, региональный" },
          { value: "INTERNAL_REGIONAL", label: "INTERNAL_REGIONAL — внутренний, региональный" },
          { value: "INTERNAL_ZONAL", label: "INTERNAL_ZONAL — внутренний, в одной зоне" },
        ],
        description: "Режим балансировщика. Неизменяем после создания; сочетания «внешний + зональный» в наборе нет.",
      },
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description: "Регион размещения балансировщика. Неизменяем после создания.",
      },
      {
        // Источник VIP-адреса (per-family oneof v4_source/v6_source) — интерактивный
        // picker: пофамильный (v4/v6) выбор subnet/address/public; в edit источник
        // неизменяем (read-only резолвнутый Address). sanitize собирает wire-oneof.
        name: "vip_source",
        label: "Источник VIP",
        type: "custom",
        immutable: true,
        render: ({ value, onChange, editMode }) => (
          <NlbVipSourceField value={value} onChange={onChange} editMode={editMode} />
        ),
      },
      {
        name: "disabled_announce_zones",
        label: "Зоны без анонса",
        type: "custom",
        // Drain только для REGIONAL; mutable через Update. fullWidth:false — label
        // слева (как обычное поле), multi-select зон справа.
        fullWidth: false,
        // Гейт по `placement` — единственному, что форма несёт. `placement_type`
        // объект формы не содержит (write-reject проекция), поэтому условие
        // никогда не выполнялось и поле было недостижимо.
        visibleWhen: { field: "placement", equals: ["EXTERNAL_REGIONAL", "INTERNAL_REGIONAL"] },
        description:
          "Зоны, из которых anycast-VIP не анонсируется (drain). Пусто — анонс из всех здоровых зон региона.",
        render: ({ value, onChange }) => <NlbDisabledZonesField value={value} onChange={onChange} />,
      },
      {
        name: "session_affinity",
        label: "Привязка сессий",
        type: "enum",
        default: "FIVE_TUPLE",
        options: [
          { value: "FIVE_TUPLE", label: "По пяти полям (src ip+port, dst ip+port, proto)" },
          { value: "CLIENT_IP_ONLY", label: "Только по адресу клиента" },
        ],
        description:
          "Привязка соединений к target: FIVE_TUPLE — по 5-tuple, CLIENT_IP_ONLY — только по IP клиента. Control-plane намерение (распределение трафика — data-plane).",
      },
      {
        name: "deletion_protection",
        label: "Защита от удаления",
        type: "bool",
        default: false,
        description: "Если включена, балансировщик нельзя удалить, пока защита не снята.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      placement: "EXTERNAL_REGIONAL",
      session_affinity: "FIVE_TUPLE",
      deletion_protection: false,
      disabled_announce_zones: [],
      // vip_source — UI-представление источника VIP per-family (NlbVipSourceField).
      //
      // IPv4 — в авто-режиме своей схемы («из подсети» для INTERNAL,
      // нормализуется в «публичный» для EXTERNAL). IPv6 — ЯВНО не задаётся:
      // двойной стек включается по решению арендатора, а не по умолчанию.
      //
      // Прежде оба семейства стояли в режиме «из подсети». Для INTERNAL это
      // означало «пусто → семейство опущено», а для EXTERNAL (умолчание
      // размещения!) режим схлопывался в «публичный», который источник даёт
      // ВСЕГДА, — то есть внешний балансировщик по умолчанию уезжал с ОБОИМИ
      // семействами, и отказаться от одного было нечем.
      vip_source: {
        _v4_mode: "subnet",
        v4: { subnet_id: "", address_id: "" },
        _v6_mode: "off",
        v6: { subnet_id: "", address_id: "" },
      },
      labels: {},
    }),
    // Клиент-валидация ДО submit: источник VIP должен быть задан хотя бы для
    // одного семейства (IPv4/IPv6) — иначе backend отвергнет InvalidArgument.
    validate: (obj) => {
      const type = lbTypeFromPlacement(obj.placement as string | undefined);
      const vs = (obj.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(
        type,
        vs._v4_mode as string | undefined,
        vs.v4 as Record<string, unknown> | undefined,
      );
      const v6 = buildVipSourceOrNull(
        type,
        vs._v6_mode as string | undefined,
        vs.v6 as Record<string, unknown> | undefined,
      );
      if (!v4 && !v6) {
        return "Укажите источник VIP хотя бы для одного семейства (IPv4 или IPv6).";
      }
      return null;
    },
    // Собирает per-family oneof v4_source/v6_source из UI-представления
    // (NlbVipSourceField): семейство эмитится, только если у активного режима
    // есть значение (buildVipSourceOrNull ≠ null) — пустой addressId/subnetId
    // никогда не уходит на бэкенд. Ветвь oneof нормализуется под РЕЖИМ, а не под
    // то, что осталось в виджете: subnet_id валиден только для INTERNAL, public —
    // только для EXTERNAL (validateSourceTypeMatrix на стороне сервиса), поэтому
    // подсеть, выбранная в INTERNAL-черновике, после переключения на EXTERNAL
    // схлопывается в public, а не уезжает отвергаемым телом.
    // disabled_announce_zones — только для REGIONAL-placement (иначе backend отклонит).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const placement = out.placement as string | undefined;
      const type = lbTypeFromPlacement(placement);

      const vs = (out.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(
        type,
        vs._v4_mode as string | undefined,
        vs.v4 as Record<string, unknown> | undefined,
      );
      const v6 = buildVipSourceOrNull(
        type,
        vs._v6_mode as string | undefined,
        vs.v6 as Record<string, unknown> | undefined,
      );
      if (v4) out.v4_source = v4;
      if (v6) out.v6_source = v6;
      delete out.vip_source;

      // disabled_announce_zones — REGIONAL only.
      if (lbPlacementTypeFromPlacement(placement) !== "REGIONAL") delete out.disabled_announce_zones;

      return out;
    },
  },

  listeners: {
    id: "listeners",
    route: "listeners",
    apiPath: "/nlb/v1/listeners",
    payloadKey: "listeners",
    singular: ENTITIES.listeners.singular,
    accusative: "обработчик",
    plural: ENTITIES.listeners.plural,
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
    emptyState: {
      title: "Создайте первый обработчик",
      body:
        "Обработчик — точка приёма трафика балансировщика: протокол и порт, на которых он слушает. " +
        "Обработчик указывает целевую группу, и с него начинается путь запроса к машинам.",
      docs: ["Обработчики"],
    },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        header: "Балансировщик",
        path: "load_balancer_id",
        render: (row) => (
          <RefNameLink specId="load-balancers" refId={row.load_balancer_id as string | undefined} maxChars={36} />
        ),
      },
      { header: "Протокол", path: "protocol", format: "code" },
      { header: "Порт", path: "port", format: "text" },
      { header: "Порт на цели", path: "resolved_backend_port", format: "text" },
      { header: "Статус", path: "status", format: "status" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "load_balancer_id",
        label: "Балансировщик",
        type: "ref",
        refResource: "load-balancers",
        refProjectScoped: true,
        required: true,
        immutable: true,
        description: "Балансировщик, которому принадлежит слушатель. Неизменяем после создания.",
      },
      {
        name: "protocol",
        label: "Протокол",
        type: "enum",
        required: true,
        immutable: true,
        options: [
          { value: "TCP", label: "TCP" },
          { value: "UDP", label: "UDP" },
        ],
        description: "Транспортный протокол. Неизменяем после создания.",
      },
      {
        name: "port",
        label: "Порт",
        type: "int",
        required: true,
        immutable: true,
        min: 1,
        max: 65535,
        description: "Порт, на котором слушатель принимает входящий трафик (1..65535). Неизменяем после создания.",
      },
      {
        name: "default_target_group_id",
        label: "Целевая группа по умолчанию",
        type: "ref",
        refResource: "target-groups",
        refProjectScoped: true,
        required: false,
        description:
          "Целевая группа, принимающая трафик. Привязка живёт ЗДЕСЬ: у балансировщика собственной привязки к группе нет.",
      },
      FIELD_LABELS,
    ],
    template: () => ({
      name: "",
      description: "",
      load_balancer_id: "",
      protocol: "TCP",
      // port НЕ дефолтим в 0 — край отвергает порт 0 (диапазон [1,65535]).
      // Пусто → пользователь обязан ввести. Диапазон валидируется InputNumber.
      //
      // Порта на цели здесь нет: он живёт на группе целей и приходит обратно
      // вычисляемым resolved_backend_port.
      default_target_group_id: "",
      labels: {},
    }),
  },

  "target-groups": {
    id: "target-groups",
    route: "target-groups",
    apiPath: "/nlb/v1/targetGroups",
    payloadKey: "target_groups",
    singular: ENTITIES["target-groups"].singular,
    accusative: "целевую группу",
    plural: ENTITIES["target-groups"].plural,
    genitive: "Целевой группы",
    serviceTitle: SERVICES.nlb.title,
    scope: "project",
    ops: { create: true, update: true, delete: true },
    emptyState: {
      title: "Создайте первую целевую группу",
      body:
        "Целевая группа собирает машины и сетевые интерфейсы, принимающие трафик на общем порту " +
        "бэкенда. На группу ссылается обработчик — без неё направлять трафик балансировщику некуда.",
      docs: ["Целевые группы"],
    },
    columns: [
      {
        header: "Имя",
        path: "name",
        render: (row) => <CopyableName name={(row.name as string) ?? ""} fallback={row.id as string} />,
      },
      {
        header: "Идентификатор",
        path: "id",
        render: (row) => <CopyableId id={(row.id as string) ?? ""} />,
      },
      {
        // Та же ссылка, что в списке балансировщиков: один предмет — один вид.
        header: "Регион",
        path: "region_id",
        render: (row) => <RefNameLink specId="regions" refId={row.region_id as string | undefined} maxChars={28} />,
      },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      {
        // CreateTargetGroupRequest.port — required: единый backend-порт группы, на
        // который таргеты получают перенаправленный трафик; эхо в
        // Listener.resolvedBackendPort.
        name: "port",
        label: "Порт бэкенда",
        type: "int",
        required: true,
        min: 1,
        max: 65535,
        default: 80,
        description: "Порт, на котором таргеты принимают перенаправленный трафик (1..65535).",
      },
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description: "Регион размещения группы целей. Неизменяем после создания.",
      },
      {
        // NLB-1c (B8): на проводе google.protobuf.Duration ("300s"); прежнее
        // int-секундное имя deregistration_delay_seconds — reserved и на Create,
        // и на Update. Форма редактирует число, sanitize/hydrate переводят.
        name: "deregistration_delay",
        label: "Время вывода из-под нагрузки (с)",
        type: "int",
        required: false,
        default: 300,
        min: 0,
        max: 3600,
        description:
          "Сколько ждать прекращения трафика перед удалением target'а из активного набора (0..3600). По умолчанию 300.",
      },
      ...healthCheckFields(),
      targetsField(),
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      port: 80,
      deregistration_delay: "300s",
      _health_check_protocol: "tcp",
      health_check: {
        tcp: { port: 80 },
        interval: "2s",
        timeout: "1s",
        unhealthy_threshold: 2,
        healthy_threshold: 2,
      },
      labels: {},
    }),
    // Форма правит секунды числом; контракт принимает Duration. Плюс ветвь
    // проверки живости и ветвь идентичности каждой цели: форма держит поля всех
    // ветвей, в теле остаётся по одной.
    sanitize: (obj) => {
      const out: Record<string, unknown> = sanitizeTargets(sanitizeHealthCheck(obj));
      const raw = out["deregistration_delay"];
      // Пусто → не шлём вовсе, чтобы сервер применил СВОЙ дефолт. 0 — легальное
      // значение и обязано доехать явным "0s", а не быть спутанным с пустотой.
      const n =
        typeof raw === "number"
          ? raw
          : typeof raw === "string" && raw.trim() !== ""
            ? Number(raw.endsWith("s") ? raw.slice(0, -1) : raw)
            : NaN;
      if (Number.isFinite(n)) out["deregistration_delay"] = `${n}s`;
      else delete out["deregistration_delay"];
      return out;
    },
    // Duration → число, которое рендерит int-поле формы; заполненная ветвь
    // проверки живости → выбор в дискриминаторе.
    hydrate: (obj) => {
      const out: Record<string, unknown> = hydrateHealthCheck(obj);
      const raw = out["deregistration_delay"];
      if (typeof raw === "string" && raw.endsWith("s")) {
        const n = Number(raw.slice(0, -1));
        if (Number.isFinite(n)) out["deregistration_delay"] = n;
      }
      return out;
    },
  },
};

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// Домен-владелец и сборка SPA-адреса — ОДНА реализация на дерево, в
// `@shared/lib/service-prefix`. Здесь стояла своя копия правила: она называла
// доменом ссылки СВОЙ модуль, поэтому ссылка на чужой ресурс адресовалась
// сегментом, которого у владельца нет, — маршрут не находился, и catch-all
// уводил человека с карточки. Реестр остаётся модульным (в нём ровно те
// ресурсы, что показывает модуль), а правило сборки адреса — общее.
export { resourceServicePrefix, resourceListPath, isSystemScopedResource };
export type { ServicePrefix };

export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  const spec = REGISTRY[specId];
  if (!spec) return null;
  return resourceListPath(specId, spec.route, projectId);
}

// Чтение значения по пути — ОДНА реализация на дерево (`@shared/lib/path`).
// Здесь стояла своя: она разбирала только точки, а общая понимает ещё и индекс
// в скобках (`spec.rules[0].direction`). Две реализации одного предмета
// расходятся молча, и расхождение видно только на том поле, которое одна из них
// прочитать не умеет. Обёртка оставлена ради <T> у вызывающих.
export function getByPath<T = unknown>(obj: unknown, path: string): T | undefined {
  return getByPathImpl(obj, path) as T | undefined;
}

// applyDefaults — для Create-формы прогоняем все поля и подставляем default-ы.
export function applyFieldDefaults(
  fields: FormField[] | undefined,
  obj: Record<string, unknown>,
): Record<string, unknown> {
  if (!fields) return obj;
  let cur = obj;
  for (const f of fields) {
    // An update-only field is never seeded. On Create its message has no such
    // field at all, so a default would ride out as an unknown key and be dropped
    // in silence; on Update the value comes from the fetched resource, and seeding
    // one would push a field the operator never touched into update_mask.
    // ONE guard, ahead of the type split — a guard inside a per-type branch fixes
    // the field that prompted it and leaves the next one of another type open.
    if (f.updateOnly) continue;
    if (f.type !== "string" && f.type !== "int" && f.type !== "enum" && f.type !== "bool") continue;
    if (f.default === undefined) continue;
    cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
  }
  return cur;
}
