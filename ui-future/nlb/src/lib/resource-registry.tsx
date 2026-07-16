// Реестр ресурсов NLB-remote: метаданные для generic ListPage / DetailShell /
// Create-Edit. Единственный источник истины по форме ресурса (route/columns/
// fields/template/sanitize/ops), как в VPC-remote. Домен — Network Load
// Balancer: LoadBalancer / Listener / TargetGroup. `compute-regions` — read-only
// справочник для ref-полей region_id (cross-service ref → geo.Region).

import type { ReactNode } from "react";
import type { FormField } from "./form-schema";
import { setByPath } from "./path";
import { CopyableId } from "@/components/atoms/CopyableId";
import { CopyableName } from "@/components/atoms/CopyableName";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { LabelsCell } from "@/components/atoms/LabelsCell";
import { NlbVipCell } from "@/components/molecules/NlbVipCell";
import {
  NlbVipSourceField,
  NlbDisabledZonesField,
  buildVipSourceOrNull,
} from "@/components/organisms/form/NlbVipSourceField";

export interface ResourceColumn {
  header: string;
  // Путь в плоском объекте: "name", "status", "region_id"
  path: string;
  format?: "text" | "uid-short" | "datetime" | "status" | "code" | "list" | "references";
  className?: string;
  render?: (row: Record<string, unknown>) => ReactNode;
}

export interface ResourceSpec {
  id: string;
  // route path в SPA (без leading slash)
  route: string;
  // Полный URL-path для REST: /<domain>/v1/<plural>
  apiPath: string;
  // ключ массива в List response
  payloadKey: string;
  // singular label для UI
  singular: string;
  // plural label
  plural: string;
  // родительный падеж ед.ч. — заголовок мастер-ресурса в зоне обзора. Fallback: plural.
  genitive?: string;
  description?: string;
  /** Service-domain заголовок (в breadcrumb перед именем категории). */
  serviceTitle?: string;
  // global = cluster-scoped, project = в выбранном Project, account = в Account
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
  childRoute?: string;
  // skeleton-объект для Create-формы.
  template: (ctx: { projectId?: string; accountId?: string }) => unknown;
  // Нормализация payload перед отправкой на API (form-internal → wire).
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
  // Обратная sanitize: wire → form (edit-режим).
  hydrate?: (obj: Record<string, unknown>) => Record<string, unknown>;
  // Клиентская валидация ДО submit (Create). Возвращает текст ошибки или null.
  // Используется для инвариантов, которые backend отверг бы асинхронно через
  // 1-2с (напр. LB: хотя бы одно семейство VIP включено).
  validate?: (obj: Record<string, unknown>) => string | null;
  /** Path-template для internal/infra-проекции ресурса (плейсхолдер `{id}`). */
  internalGetPath?: string;
  /** Связанные дочерние ресурсы — отдельные табы во ResourceShell. */
  related?: { childId: string; filterField: string | string[]; label?: string }[];
  /** Ссылки на документацию (блок «Документация» в aside DetailShell). */
  docs?: { label: string; href: string }[];
  /** Welcome-копирайт для пустой таблицы этого ресурса. */
  emptyState?: { title: string; body: string; docs?: string[] };
}

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
  label: "Project",
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
    route: "compute-regions",
    apiPath: "/geo/v1/regions",
    payloadKey: "regions",
    singular: "Регион",
    plural: "Регионы",
    serviceTitle: "Compute Cloud",
    scope: "global",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
      { header: "Статус", path: "status", format: "status" },
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
    plural: "Виртуальные машины",
    serviceTitle: "Compute Cloud",
    scope: "project",
    ops: { create: false, update: false, delete: false },
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
    singular: "Сетевой интерфейс",
    plural: "Сетевые интерфейсы",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "text", className: "font-mono" },
    ],
    template: () => ({}),
  },
  "zones": {
    id: "zones",
    route: "zones",
    apiPath: "/geo/v1/zones",
    payloadKey: "zones",
    singular: "Зона",
    plural: "Зоны",
    serviceTitle: "Администрирование",
    scope: "global",
    ops: { create: false, update: false, delete: false },
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
    singular: "Подсеть",
    plural: "Подсети",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: false, update: false, delete: false },
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
    singular: "Адрес",
    plural: "Адреса",
    serviceTitle: "Virtual Private Cloud",
    scope: "project",
    ops: { create: false, update: false, delete: false },
    columns: [
      { header: "Имя", path: "name", format: "text" },
      { header: "Идентификатор", path: "id", format: "uid-short" },
    ],
    template: () => ({}),
  },

  // ====== nlb ======
  // proto: kacho.cloud.nlb.v1.NetworkLoadBalancerService.

  "load-balancers": {
    id: "load-balancers",
    route: "load-balancers",
    apiPath: "/nlb/v1/networkLoadBalancers",
    // proto ListNetworkLoadBalancersResponse repeated-поле — `network_load_balancers`.
    payloadKey: "network_load_balancers",
    singular: "Балансировщик нагрузки",
    plural: "Балансировщики нагрузки",
    genitive: "Балансировщика нагрузки",
    docs: [
      { label: "Балансировщики нагрузки", href: "#" },
      { label: "Обработчики и целевые группы", href: "#" },
    ],
    serviceTitle: "Network Load Balancer",
    scope: "project",
    // Start/Stop намеренно НЕ в ops — lifecycle-действия балансировщика в UI не
    // экспонируются (управление статусом идёт через create/delete + data-plane).
    ops: { create: true, update: true, delete: true },
    // Листенеры — связанный дочерний ресурс (within-service FK load_balancer_id):
    // отдельный registry-driven таб + auto-CTA «Создать листенер». Целевые группы
    // не выражаются filterField (pivot attached_target_groups) — их вкладку
    // подаёт bespoke LoadBalancerDetailPage.
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
      { header: "Регион", path: "region_id", format: "text" },
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
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description: "Регион размещения балансировщика (immutable после Create). Cross-service ref → geo.Region.",
      },
      {
        name: "type",
        label: "Схема",
        type: "enum",
        required: true,
        immutable: true,
        default: "INTERNAL",
        options: [
          { value: "INTERNAL", label: "INTERNAL — приватный VIP" },
          { value: "EXTERNAL", label: "EXTERNAL — публичный VIP" },
        ],
        description:
          "Схема балансировщика (immutable после Create). INTERNAL — приватный VIP (из подсети или линк internal Address). EXTERNAL — публичный VIP (платформенный public или линк public Address).",
      },
      {
        name: "placement_type",
        label: "Размещение",
        type: "enum",
        required: true,
        immutable: true,
        default: "ZONAL",
        // Размещение задаётся только для INTERNAL; для EXTERNAL неприменимо.
        visibleWhen: { field: "type", equals: "INTERNAL" },
        options: [
          { value: "ZONAL", label: "ZONAL — unicast, одна зона" },
          { value: "REGIONAL", label: "REGIONAL — anycast, регион" },
        ],
        description:
          "Размещение INTERNAL-VIP (immutable после Create). ZONAL — unicast-VIP в одной зоне. REGIONAL — anycast-VIP региона (active-active из здоровых зон).",
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
        visibleWhen: { field: "placement_type", equals: "REGIONAL" },
        description:
          "Зоны, из которых anycast-VIP не анонсируется (drain). Пусто — анонс из всех здоровых зон региона.",
        render: ({ value, onChange }) => <NlbDisabledZonesField value={value} onChange={onChange} />,
      },
      {
        name: "session_affinity",
        label: "Session affinity",
        type: "enum",
        default: "FIVE_TUPLE",
        options: [
          { value: "FIVE_TUPLE", label: "5-tuple (src ip+port, dst ip+port, proto)" },
          { value: "CLIENT_IP_ONLY", label: "Client IP only (src ip)" },
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
      type: "INTERNAL",
      placement_type: "ZONAL",
      session_affinity: "FIVE_TUPLE",
      deletion_protection: false,
      disabled_announce_zones: [],
      // vip_source — UI-представление источника VIP per-family (NlbVipSourceField).
      // По умолчанию оба семейства в режиме «из подсети» с пустым выбором:
      // семейство уходит в wire, только если у него задан источник. sanitize
      // собирает oneof v4_source/v6_source (ровно один кейс на непустое семейство).
      vip_source: {
        _v4_mode: "subnet",
        v4: { subnet_id: "", address_id: "" },
        _v6_mode: "subnet",
        v6: { subnet_id: "", address_id: "" },
      },
      labels: {},
    }),
    // Клиент-валидация ДО submit: источник VIP должен быть задан хотя бы для
    // одного семейства (IPv4/IPv6) — иначе backend отвергнет InvalidArgument.
    validate: (obj) => {
      const type = (obj.type as string) || "INTERNAL";
      const vs = (obj.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(type, vs._v4_mode as string | undefined, vs.v4 as Record<string, unknown> | undefined);
      const v6 = buildVipSourceOrNull(type, vs._v6_mode as string | undefined, vs.v6 as Record<string, unknown> | undefined);
      if (!v4 && !v6) {
        return "Укажите источник VIP хотя бы для одного семейства (IPv4 или IPv6).";
      }
      return null;
    },
    // Собирает per-family oneof v4_source/v6_source из UI-представления
    // (NlbVipSourceField): семейство эмитится, только если у активного режима
    // есть значение (buildVipSourceOrNull ≠ null) — пустой addressId/subnetId
    // никогда не уходит на бэкенд. placement_type шлётся только для INTERNAL,
    // disabled_announce_zones — только для REGIONAL (иначе backend отклонит).
    sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const type = (out.type as string) || "INTERNAL";

      const vs = (out.vip_source as Record<string, unknown> | undefined) ?? {};
      const v4 = buildVipSourceOrNull(type, vs._v4_mode as string | undefined, vs.v4 as Record<string, unknown> | undefined);
      const v6 = buildVipSourceOrNull(type, vs._v6_mode as string | undefined, vs.v6 as Record<string, unknown> | undefined);
      if (v4) out.v4_source = v4;
      if (v6) out.v6_source = v6;
      delete out.vip_source;

      // placement_type — INTERNAL only.
      if (type !== "INTERNAL") delete out.placement_type;
      // disabled_announce_zones — REGIONAL only.
      if ((out.placement_type as string) !== "REGIONAL") delete out.disabled_announce_zones;

      return out;
    },
  },

  listeners: {
    id: "listeners",
    route: "listeners",
    apiPath: "/nlb/v1/listeners",
    payloadKey: "listeners",
    singular: "Обработчик",
    plural: "Listeners",
    docs: [
      { label: "Обработчики (Listeners)", href: "#" },
      { label: "Балансировщики нагрузки", href: "#" },
    ],
    serviceTitle: "Network Load Balancer",
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      { header: "Порт на target", path: "target_port", format: "text" },
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
        description: "Балансировщик-родитель (immutable после Create). Within-service FK → load_balancers.",
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
        description: "L4 транспорт (immutable после Create).",
      },
      {
        name: "port",
        label: "Порт",
        type: "int",
        required: true,
        immutable: true,
        min: 1,
        max: 65535,
        description: "Порт, на котором listener принимает входящий трафик (1..65535, immutable после Create).",
      },
      {
        name: "target_port",
        label: "Порт на target",
        type: "int",
        required: false,
        min: 1,
        max: 65535,
        description: "Порт на target-е (1..65535). Если не задан — равен «Порт».",
      },
      {
        name: "proxy_protocol_v2",
        label: "PROXY-protocol v2",
        type: "bool",
        required: false,
        default: false,
        description: "PROXY-protocol v2 framing на входящих соединениях (передаёт target'у исходный адрес клиента).",
      },
      {
        name: "default_target_group_id",
        label: "Target group по умолчанию",
        type: "ref",
        refResource: "target-groups",
        refProjectScoped: true,
        required: false,
        description:
          "Целевая группа, принимающая трафик по умолчанию. TG должна быть приаттаджена к балансировщику (обычно задаётся в режиме редактирования, после attach).",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      load_balancer_id: "",
      protocol: "TCP",
      // port / target_port НЕ дефолтим в 0 — API отвергает порт 0 (диапазон
      // [1,65535]). Пусто → пользователь обязан ввести (port), либо опущено
      // (target_port → равен port через sanitize). Диапазон валидируется InputNumber.
      default_target_group_id: "",
      labels: {},
    }),
    // target_port по умолчанию равен port (конвенция LB). Backend требует
    // target_port в [1,65535]: пустой/0 → 400 "port must be in range". Без этого
    // sanitize пользователь, оставив «Порт на target» пустым, получал скрытую
    // ошибку create (listener не создавался).
    sanitize: (obj) => {
      const o = { ...obj } as Record<string, unknown>;
      const tp = o.target_port;
      const port = o.port;
      const portSet = typeof port === "number" ? port > 0 : typeof port === "string" && port !== "";
      const tpUnset = tp === undefined || tp === null || tp === "" || tp === 0;
      if (tpUnset && portSet) o.target_port = port;
      return o;
    },
  },

  "target-groups": {
    id: "target-groups",
    route: "target-groups",
    apiPath: "/nlb/v1/targetGroups",
    payloadKey: "target_groups",
    singular: "Целевая группа",
    plural: "Target Groups",
    docs: [
      { label: "Целевые группы (Target Groups)", href: "#" },
      { label: "Балансировщики нагрузки", href: "#" },
    ],
    genitive: "Целевой группы",
    serviceTitle: "Network Load Balancer",
    scope: "project",
    ops: { create: true, update: true, delete: true },
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
      { header: "Регион", path: "region_id", format: "text" },
      { header: "Дата создания", path: "created_at", format: "datetime" },
      {
        header: "Метки",
        path: "labels",
        render: (row) => <LabelsCell labels={row.labels as Record<string, string> | undefined} />,
      },
    ],
    fields: [
      FIELD_NAME_COMPUTE,
      FIELD_DESCRIPTION,
      {
        name: "region_id",
        label: "Регион",
        type: "ref",
        refResource: "compute-regions",
        required: true,
        immutable: true,
        description: "Регион размещения target-group (immutable после Create). Cross-service ref → geo.Region.",
      },
      {
        name: "deregistration_delay_seconds",
        label: "Drain timeout (с)",
        type: "int",
        required: false,
        default: 300,
        min: 0,
        max: 3600,
        description:
          "Сколько ждать прекращения трафика перед удалением target'а из активного набора (0..3600). По умолчанию 300.",
      },
      {
        name: "health_check.name",
        label: "HC: имя",
        type: "string",
        required: true,
        description:
          "Имя health-check'а (3-63 символа, lowercase + цифры + дефисы). Уникально в пределах target-group.",
      },
      {
        name: "health_check.tcp_options.port",
        label: "HC: TCP-порт",
        type: "int",
        required: true,
        default: 80,
        min: 1,
        max: 65535,
        description: "TCP-порт для health-check'а (1..65535). По умолчанию 80.",
      },
      {
        name: "health_check.interval",
        label: "HC: интервал",
        type: "string",
        required: true,
        default: "2s",
        description: "Интервал между health-check'ами (Duration в формате 'Ns', range 1s-600s). По умолчанию 2s.",
      },
      {
        name: "health_check.timeout",
        label: "HC: таймаут",
        type: "string",
        required: true,
        default: "1s",
        description: "Таймаут одного health-check'а (Duration). По умолчанию 1s.",
      },
      {
        name: "health_check.unhealthy_threshold",
        label: "HC: failure threshold",
        type: "int",
        required: true,
        default: 2,
        min: 2,
        max: 10,
        description: "Сколько failed checks подряд до перевода в UNHEALTHY (2..10). По умолчанию 2.",
      },
      {
        name: "health_check.healthy_threshold",
        label: "HC: success threshold",
        type: "int",
        required: true,
        default: 2,
        min: 2,
        max: 10,
        description: "Сколько успешных checks подряд до перевода в HEALTHY (2..10). По умолчанию 2.",
      },
      FIELD_LABELS,
      FIELD_PROJECT_ID,
    ],
    template: ({ projectId }) => ({
      project_id: projectId ?? "",
      name: "",
      description: "",
      region_id: "",
      deregistration_delay_seconds: 300,
      health_check: {
        name: "default-hc",
        tcp_options: { port: 80 },
        interval: "2s",
        timeout: "1s",
        unhealthy_threshold: 2,
        healthy_threshold: 2,
      },
      labels: {},
    }),
  },
};

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}

// resourceServicePrefix — service-segment под /projects/:projectId/ per spec.id.
export function resourceServicePrefix(specId: string): "nlb" | "compute" {
  if (specId.startsWith("compute-")) return "compute";
  switch (specId) {
    case "regions":
    case "zones":
      return "compute";
    default:
      // NLB ресурсы: load-balancers, listeners, target-groups
      return "nlb";
  }
}

// resourceProjectPath — полный SPA-путь до listing ресурса в контексте project'а.
export function resourceProjectPath(specId: string, projectId: string | null | undefined): string | null {
  if (!projectId) return null;
  const spec = REGISTRY[specId];
  if (!spec) return null;
  const prefix = resourceServicePrefix(specId);
  return `/projects/${projectId}/${prefix}/${spec.route}`;
}

export function getByPath<T = unknown>(obj: unknown, path: string): T | undefined {
  return path.split(".").reduce<unknown>((acc, key) => {
    if (acc == null || typeof acc !== "object") return undefined;
    return (acc as Record<string, unknown>)[key];
  }, obj) as T | undefined;
}

// applyDefaults — для Create-формы прогоняем все поля и подставляем default-ы.
export function applyFieldDefaults(
  fields: FormField[] | undefined,
  obj: Record<string, unknown>,
): Record<string, unknown> {
  if (!fields) return obj;
  let cur = obj;
  for (const f of fields) {
    if (f.type === "string" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "int" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "enum" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    } else if (f.type === "bool" && f.default !== undefined) {
      cur = setByPath(cur, f.name, getByPath(cur, f.name) ?? f.default);
    }
  }
  return cur;
}
