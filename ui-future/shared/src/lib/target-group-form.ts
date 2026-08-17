// Форма группы целей: ветви проверки живости и ветви идентичности цели.
//
// ПОЧЕМУ ОТДЕЛЬНЫЙ МОДУЛЬ, А НЕ КУСОК РЕЕСТРА. Группу целей объявляют ДВА
// реестра — `@shared` (обслуживает vpc/iam/system) и `nlb` (обслуживает
// `/projects/:projectId/nlb/*`, то есть то, что видит пользователь). Пока поля
// жили внутри одного из них, правка доезжала до одного и не доезжала до
// другого: четыре ветви проверки живости были заведены в `@shared` и остались
// невыразимыми в `nlb` (#375). Реализация здесь одна, оба реестра её импортируют.
//
// ДИСКРИМИНАТОР — ПОЛЕ ФОРМЫ, А НЕ КОНТРАКТА (отсюда подчёркивание в имени).
// У взаимоисключающей группы нет значения, у неё есть ИМЯ ветви: на проводе
// ветвь называет себя тем, что она единственная заполненная. Поэтому форма
// держит поля всех ветвей, а лишние срезает сама — надеяться, что пользователь
// их не тронул, нельзя.

import type { FormField } from "./form-schema";

// ── Проверка живости ─────────────────────────────────────────────────────────

/** Ветви проверки живости, спрашивающие приложение (`http` и `https`), различаются
 *  только шифрованием — поля у них одни и те же, и выписывать их дважды значило бы
 *  завести два места об одном предмете. */
export const HEALTH_CHECK_APP_BRANCHES = ["http", "https"] as const;

/** Имена ветвей проверки живости, ЕДИНСТВЕННЫЙ перечень в консоли. */
export const HEALTH_CHECK_BRANCHES = ["tcp", "http", "https", "grpc"] as const;

function healthCheckAppFields(branch: "http" | "https"): FormField[] {
  const у = branch === "https" ? "HTTPS" : "HTTP";
  const when = { field: "_health_check_protocol", equals: branch } as const;
  return [
    {
      name: `health_check.${branch}.port`,
      label: `HC: ${у}-порт`,
      type: "int",
      required: false,
      visibleWhen: when,
      description: "Порт проверки. Пусто — берётся порт бэкенда группы.",
    },
    {
      name: `health_check.${branch}.path`,
      label: `HC: путь`,
      type: "string",
      required: false,
      visibleWhen: when,
      placeholder: "/healthz",
      description: "Путь запроса проверки. Пусто — корень «/».",
    },
    {
      name: `health_check.${branch}.expected_codes`,
      label: "HC: коды ответа",
      type: "string",
      required: false,
      visibleWhen: when,
      placeholder: "200-299",
      description: "Какие коды считать здоровыми: диапазон «200-299» или перечень «200,204». Пусто — любой 2xx.",
    },
    {
      name: `health_check.${branch}.host`,
      label: branch === "https" ? "HC: имя узла (Host / SNI)" : "HC: имя узла (Host)",
      type: "string",
      required: false,
      visibleWhen: when,
      description:
        branch === "https"
          ? "Заголовок Host и имя в рукопожатии TLS. Пусто — берётся адрес цели."
          : "Заголовок Host запроса проверки. Пусто — берётся адрес цели.",
    },
    {
      name: `health_check.${branch}.headers`,
      label: "HC: заголовки",
      type: "labels",
      required: false,
      visibleWhen: when,
      description: "Дополнительные заголовки запроса проверки.",
    },
  ];
}

/** Поля проверки живости целиком: дискриминатор + все четыре ветви + общие пороги. */
export function healthCheckFields(): FormField[] {
  return [
    {
      // Дискриминатор взаимоисключающей группы `HealthCheck.options`. Без него
      // выбрать ветвь нечем, и три из четырёх были невыразимы (#375): проверка
      // умела только TCP, а пути, коды ответа, заголовки и имя службы объявлены
      // контрактом и недостижимы из консоли.
      name: "_health_check_protocol",
      label: "HC: протокол",
      type: "enum",
      required: true,
      default: "tcp",
      options: [
        { value: "tcp", label: "TCP — соединение установилось" },
        { value: "http", label: "HTTP — ответ по пути" },
        { value: "https", label: "HTTPS — ответ по пути, шифрованно" },
        { value: "grpc", label: "gRPC — служба отвечает здоровой" },
      ],
      description:
        "Чем проверяется живость цели. TCP отвечает только за установленное соединение; остальные три спрашивают приложение.",
    },
    {
      name: "health_check.tcp.port",
      label: "HC: TCP-порт",
      type: "int",
      required: true,
      default: 80,
      visibleWhen: { field: "_health_check_protocol", equals: "tcp" },
      description: "TCP-порт для health-check'а (1..65535). По умолчанию 80.",
    },
    ...healthCheckAppFields("http"),
    ...healthCheckAppFields("https"),
    {
      name: "health_check.grpc.port",
      label: "HC: gRPC-порт",
      type: "int",
      required: false,
      visibleWhen: { field: "_health_check_protocol", equals: "grpc" },
      description: "Порт проверки. Пусто — берётся порт бэкенда группы.",
    },
    {
      name: "health_check.grpc.service_name",
      label: "HC: имя службы",
      type: "string",
      required: false,
      visibleWhen: { field: "_health_check_protocol", equals: "grpc" },
      placeholder: "grpc.health.v1.Health",
      description: "Имя службы в протоколе проверки здоровья. Пусто — спрашивается сервер целиком.",
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
      label: "Порог отказа проверки",
      type: "int",
      required: true,
      default: 2,
      description: "Сколько failed checks подряд до перевода в UNHEALTHY (2..10). По умолчанию 2.",
    },
    {
      name: "health_check.healthy_threshold",
      label: "Порог успеха проверки",
      type: "int",
      required: true,
      default: 2,
      description: "Сколько успешных checks подряд до перевода в HEALTHY (2..10). По умолчанию 2.",
    },
  ];
}

/**
 * Ветвь проверки живости, названная формой, остаётся в теле ОДНА.
 *
 * Группа взаимоисключающая (`exactly_one`): две заполненные ветви — отказ
 * сервера. Пустые строки внутри выбранной ветви не отправляются: у пути, кодов
 * ответа и имени узла есть СЕРВЕРНЫЕ умолчания, и пустая строка означала бы
 * «пусто», а не «как по умолчанию». Числа сохраняются целиком, включая 0: у
 * порта это законное значение со смыслом «взять порт группы».
 */
export function sanitizeHealthCheck(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  const hc = out["health_check"];
  const выбор = out["_health_check_protocol"];
  delete out["_health_check_protocol"];
  if (!hc || typeof hc !== "object") return out;

  const src = hc as Record<string, unknown>;
  const ветвь =
    typeof выбор === "string" && (HEALTH_CHECK_BRANCHES as readonly string[]).includes(выбор)
      ? выбор
      : (HEALTH_CHECK_BRANCHES.find((b) => src[b] !== undefined) ?? "tcp");

  const next: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(src)) {
    if ((HEALTH_CHECK_BRANCHES as readonly string[]).includes(k)) continue;
    next[k] = v;
  }
  const тело = src[ветвь];
  if (тело && typeof тело === "object") {
    const очищенное: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(тело as Record<string, unknown>)) {
      if (typeof v === "string" && v.trim() === "") continue;
      if (v === undefined || v === null) continue;
      if (typeof v === "object" && !Array.isArray(v) && Object.keys(v).length === 0) continue;
      очищенное[k] = v;
    }
    next[ветвь] = очищенное;
  } else {
    next[ветвь] = {};
  }
  out["health_check"] = next;
  return out;
}

/** Обратное: по заполненной ветви ответа восстановить выбор формы. */
export function hydrateHealthCheck(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  const hc = out["health_check"];
  if (hc && typeof hc === "object") {
    const src = hc as Record<string, unknown>;
    out["_health_check_protocol"] = HEALTH_CHECK_BRANCHES.find((b) => src[b] !== undefined) ?? "tcp";
  }
  return out;
}

// ── Цели группы ──────────────────────────────────────────────────────────────

/** Имена ветвей `Target.identity`, ЕДИНСТВЕННЫЙ перечень в консоли. */
export const TARGET_IDENTITY_BRANCHES = ["instance_id", "nic_id", "ip_ref", "external_ip"] as const;

/**
 * Поле целей формы создания.
 *
 * ПОЧЕМУ ОНО ЗДЕСЬ, А НЕ ТОЛЬКО НА КАРТОЧКЕ. `CreateTargetGroupRequest` несёт
 * `targets` и валидирует их так же, как `:addTargets`. Пока формы не было,
 * возможность существовала на контракте и не работала ни при каком вводе:
 * группу приходилось создавать пустой и наполнять вторым действием — то есть
 * контракт обещал один заход, а консоль требовала два.
 */
export function targetsField(): FormField {
  return {
    name: "targets",
    label: "Цели",
    type: "array",
    itemLabel: "Цель",
    required: false,
    // Правка целей идёт отдельными глаголами (`:addTargets` / `:removeTargets`),
    // и тело правки группы их не несёт вовсе — поэтому поле только для создания.
    editHidden: true,
    description:
      "Кого группа балансирует. Можно оставить пустым и добавить позже на карточке — контракт принимает и то, и другое.",
    newItem: () => ({ _identity_kind: "instance_id", weight: 1 }),
    itemFields: [
      {
        name: "_identity_kind",
        label: "Чем названа цель",
        type: "enum",
        required: true,
        default: "instance_id",
        options: [
          { value: "instance_id", label: "Виртуальная машина" },
          { value: "nic_id", label: "Сетевой интерфейс" },
          { value: "ip_ref", label: "Адрес в облаке" },
          { value: "external_ip", label: "Внешний адрес" },
        ],
        description: "Ветвь идентичности цели: ровно одна на цель.",
      },
      {
        name: "instance_id",
        label: "Машина",
        type: "ref",
        refResource: "compute-instances",
        refProjectScoped: true,
        visibleWhen: { field: "_identity_kind", equals: "instance_id" },
        description: "Адрес берётся с основного интерфейса машины на стороне сервера.",
      },
      {
        name: "nic_id",
        label: "Интерфейс",
        type: "ref",
        refResource: "network-interfaces",
        refProjectScoped: true,
        visibleWhen: { field: "_identity_kind", equals: "nic_id" },
        description: "Адрес читается с этого интерфейса.",
      },
      {
        name: "ip_ref.subnet_id",
        label: "Подсеть адреса",
        type: "ref",
        refResource: "subnets",
        refProjectScoped: true,
        visibleWhen: { field: "_identity_kind", equals: "ip_ref" },
        description: "Подсеть, которой принадлежит адрес.",
      },
      {
        name: "ip_ref.address",
        label: "Адрес в облаке",
        type: "string",
        visibleWhen: { field: "_identity_kind", equals: "ip_ref" },
        placeholder: "10.0.0.5",
        description: "Адрес внутри CIDR указанной подсети.",
      },
      {
        name: "external_ip.address",
        label: "Внешний адрес",
        type: "string",
        visibleWhen: { field: "_identity_kind", equals: "external_ip" },
        placeholder: "203.0.113.7",
        description: "Адрес вне облака. Служебные классы адресов сервер отвергает.",
      },
      {
        name: "external_ip.zone_id",
        label: "Зона (необязательно)",
        type: "ref",
        refResource: "zones",
        visibleWhen: { field: "_identity_kind", equals: "external_ip" },
        description: "Подсказка размещения. Пусто — решение принимается на уровне региона.",
      },
      {
        name: "weight",
        label: "Вес",
        type: "int",
        default: 1,
        min: 0,
        max: 1000,
        description: "Доля трафика цели внутри группы (0..1000). 0 снимает трафик, не удаляя строку.",
      },
    ],
  };
}

/** Значение ветви идентичности из строки формы; `undefined` — строка не заполнена. */
function identityValue(kind: string, row: Record<string, unknown>): unknown {
  const s = (v: unknown) => (typeof v === "string" ? v.trim() : "");
  switch (kind) {
    case "instance_id":
      return s(row.instance_id) || undefined;
    case "nic_id":
      return s(row.nic_id) || undefined;
    case "ip_ref": {
      const ref = (row.ip_ref ?? {}) as Record<string, unknown>;
      const subnet = s(ref.subnet_id);
      const address = s(ref.address);
      return subnet && address ? { subnet_id: subnet, address } : undefined;
    }
    case "external_ip": {
      const ext = (row.external_ip ?? {}) as Record<string, unknown>;
      const address = s(ext.address);
      if (!address) return undefined;
      const zone = s(ext.zone_id);
      return zone ? { address, zone_id: zone } : { address };
    }
    default:
      return undefined;
  }
}

/**
 * Строки целей → тело запроса: у каждой цели остаётся ОДНА ветвь идентичности.
 *
 * Недозаполненная строка выбрасывается — так же, как строка статического
 * маршрута: отправить её значит получить отказ сервера на то, чего
 * пользователь не вводил. Пустой перечень не отправляется вовсе: группа без
 * целей — законный исход создания, и лишний ключ в теле её не описывает.
 *
 * Полнота считается ПО ВЫБРАННОЙ ВЕТВИ, а не по одному полю: прежде так уже
 * ошиблись со статическим маршрутом, где полнота строки считалась по адресу и
 * маршрут на шлюз выбрасывался молча.
 */
export function sanitizeTargets(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...obj };
  const raw = out["targets"];
  if (!Array.isArray(raw)) {
    delete out["targets"];
    return out;
  }
  const targets: Array<Record<string, unknown>> = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const row = item as Record<string, unknown>;
    const kind =
      typeof row._identity_kind === "string" && (TARGET_IDENTITY_BRANCHES as readonly string[]).includes(row._identity_kind)
        ? row._identity_kind
        : (TARGET_IDENTITY_BRANCHES.find((b) => row[b] !== undefined) ?? "instance_id");
    const value = identityValue(kind, row);
    if (value === undefined) continue;
    const next: Record<string, unknown> = { [kind]: value };
    const weight = row.weight;
    if (typeof weight === "number" && Number.isFinite(weight)) next.weight = weight;
    else if (typeof weight === "string" && weight.trim() !== "" && Number.isFinite(Number(weight)))
      next.weight = Number(weight);
    targets.push(next);
  }
  if (targets.length === 0) delete out["targets"];
  else out["targets"] = targets;
  return out;
}
