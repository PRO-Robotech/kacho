/**
 * Имена разделов и сущностей консоли — ЕДИНСТВЕННЫЙ источник.
 *
 * Зачем модуль вообще заведён. До него подпись сущности выписывалась в каждом
 * месте навигации отдельно, и места разошлись: обработчик балансировщика
 * назывался ЧЕТЫРЬМЯ способами («Listeners» в агрегатном меню, «Обработчики» в
 * меню модуля, «Слушатели» в крошке хоста, «Обработчики (Listeners)» в крошке
 * реестра ресурсов), виртуальная машина — двумя («Виртуальные машины» и
 * «Инстансы»), привязка доступа — двумя, адрес — двумя. Пользователь читает это
 * как разные предметы, а правка подписи доезжает не всюду. Отсюда правило:
 * подпись **выводится** отсюда, а не пишется рядом с местом показа.
 *
 * Ключ ресурса — СЕГМЕНТ АДРЕСА (`listeners`, `instances`, `access-bindings`),
 * потому что именно он связывает три поверхности, которые обязаны совпасть:
 * пункт меню (`path`), запись реестра ресурсов (ключ) и крошку хоста (последний
 * сегмент `pathname`). Ключ раздела — имя домена Kachō (`nlb`, `compute`, …).
 *
 * Почему у раздела ДВА поля. Крошка и шапка ресурса показывают короткое имя
 * (`title`), заголовок группы в левом меню — развёрнутое (`menuTitle`). Это не
 * лазейка для третьего варианта: полей ровно два, оба здесь, и обойти их можно
 * только выписав литерал рядом с показом — что и есть предмет запрета.
 *
 * Что здесь НЕ живёт: подписи, не именующие сущность или раздел («Поиск»,
 * «Токены и ключи», «Ключи сервисных аккаунтов») — они принадлежат своему месту
 * и в двух местах не встречаются.
 *
 * Чем держится (правки этих значений доезжают до всех показов):
 *  - приложения с алиасом `@shared` (vpc · compute · storage · nlb · registry ·
 *    iam · system) **импортируют** отсюда — источник один по построению;
 *  - `host` и `dashboard` собираются образом из СВОЕГО дерева (их Dockerfile не
 *    копирует `shared/`), поэтому импортировать отсюда не могут; их подписи
 *    сверяются с этими значениями собственными пробами
 *    (`HostBreadcrumb.names.test.tsx`, `navigation.names.test.ts`), и расхождение
 *    не может приземлиться молча.
 */

/** Раздел консоли = домен Kachō. */
export interface ServiceName {
  /** Короткое имя — крошки, шапка ресурса, заглушка модуля. */
  title: string;
  /** Развёрнутое — заголовок группы в левом меню. */
  menuTitle: string;
}

/**
 * Разделы. Отраслевые термины оставлены (VPC, IAM — так называется предмет во
 * всей отрасли и это не чужая платформа); продуктовые имена чужих платформ
 * заменены на имя домена Kachō; уточнение при балансировщике снято — оно
 * отличало его от типов, которых у Kachō нет.
 */
export const SERVICES = {
  iam: { title: "IAM", menuTitle: "Identity and Access Management" },
  vpc: { title: "VPC", menuTitle: "Virtual Private Cloud" },
  compute: { title: "Compute", menuTitle: "Compute" },
  storage: { title: "Storage", menuTitle: "Storage" },
  nlb: { title: "Load Balancer", menuTitle: "Load Balancer" },
  registry: { title: "Registry", menuTitle: "Registry" },
  geo: { title: "Geography", menuTitle: "Geography" },
  system: { title: "Администрирование", menuTitle: "Администрирование" },
} as const satisfies Record<string, ServiceName>;

export type ServiceKey = keyof typeof SERVICES;

/** Сущность: одна подпись в единственном и одна во множественном числе. */
export interface EntityName {
  singular: string;
  plural: string;
}

/**
 * Сущности по сегменту адреса. Там, где раньше было несколько подписей, выбор
 * назван в PR: обработчик (`listeners`) — по роли ресурса в балансировщике,
 * а не буквальным переводом контрактного имени; виртуальная машина
 * (`instances`) — тем словом, которым её называет всё остальное дерево.
 */
export const ENTITIES = {
  // iam
  accounts: { singular: "Аккаунт", plural: "Аккаунты" },
  projects: { singular: "Проект", plural: "Проекты" },
  users: { singular: "Пользователь", plural: "Пользователи" },
  "service-accounts": {
    singular: "Сервисный аккаунт",
    plural: "Сервисные аккаунты",
  },
  groups: { singular: "Группа", plural: "Группы" },
  roles: { singular: "Роль", plural: "Роли" },
  "access-bindings": {
    singular: "Привязка доступа",
    plural: "Привязки доступа",
  },
  operations: { singular: "Операция", plural: "Операции" },
  // vpc
  networks: { singular: "Облачная сеть", plural: "Облачные сети" },
  subnets: { singular: "Подсеть", plural: "Подсети" },
  addresses: { singular: "IP-адрес", plural: "IP-адреса" },
  "route-tables": {
    singular: "Таблица маршрутов",
    plural: "Таблицы маршрутов",
  },
  "security-groups": {
    singular: "Группа безопасности",
    plural: "Группы безопасности",
  },
  "network-interfaces": {
    singular: "Сетевой интерфейс",
    plural: "Сетевые интерфейсы",
  },
  gateways: { singular: "Шлюз", plural: "Шлюзы" },
  "address-pools": { singular: "Пул адресов", plural: "Пулы адресов" },
  "cidr-groups": { singular: "Набор префиксов", plural: "Наборы префиксов" },
  // compute
  instances: { singular: "Виртуальная машина", plural: "Виртуальные машины" },
  "machine-types": { singular: "Тип машины", plural: "Типы машин" },
  "placement-groups": { singular: "Группа размещения", plural: "Группы размещения" },
  "guest-access-keys": { singular: "Ключ доступа", plural: "Ключи доступа" },
  // storage
  volumes: { singular: "Том", plural: "Тома" },
  snapshots: { singular: "Снимок", plural: "Снимки" },
  images: { singular: "Образ", plural: "Образы" },
  "disk-types": { singular: "Тип диска", plural: "Типы дисков" },
  // nlb
  "load-balancers": {
    singular: "Балансировщик нагрузки",
    plural: "Балансировщики нагрузки",
  },
  listeners: { singular: "Обработчик", plural: "Обработчики" },
  "target-groups": { singular: "Целевая группа", plural: "Целевые группы" },
  // registry
  registries: { singular: "Реестр", plural: "Реестры" },
  // geo
  regions: { singular: "Регион", plural: "Регионы" },
  zones: { singular: "Зона", plural: "Зоны" },
} as const satisfies Record<string, EntityName>;

export type EntityKey = keyof typeof ENTITIES;

/** Подпись сущности по сегменту адреса; `undefined` — сегмент не именует сущность. */
export function entityName(segment: string): EntityName | undefined {
  return (ENTITIES as Record<string, EntityName>)[segment];
}

/** Подпись раздела по имени домена; `undefined` — раздел не наш. */
export function serviceName(key: string): ServiceName | undefined {
  return (SERVICES as Record<string, ServiceName>)[key];
}
