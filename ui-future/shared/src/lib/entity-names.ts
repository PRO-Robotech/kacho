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

/**
 * Род имени в единственном числе.
 *
 * Объявляется, а НЕ выводится из окончания. Русский род по хвосту слова — правило
 * с исключениями («Шлюз» и «Роль» кончаются на согласную, род разный; «Тип диска»
 * кончается на «а», род мужской, потому что склоняется опорное слово), и вывод по
 * строке ошибается МОЛЧА — тем же классом, что деривация региона из имени зоны.
 * Тот же довод, по которому рядом объявляется винительный падеж
 * (`resource-label.ts`).
 */
export type Gender = "m" | "f" | "n";

/** Сущность: одна подпись в единственном и одна во множественном числе. */
export interface EntityName {
  singular: string;
  plural: string;
  /**
   * Род `singular` — для согласования причастия в сигнале об исходе мутации
   * («Облачная сеть создана», а не «создан»). Поле ОБЯЗАТЕЛЬНОЕ: необязательное
   * молча дало бы мужской род по умолчанию, то есть ровно тот дефект, ради
   * которого заведено. Потребитель — `mutation-signal.ts`.
   */
  gender: Gender;
}

/**
 * Сущности по сегменту адреса. Там, где раньше было несколько подписей, выбор
 * назван в PR: обработчик (`listeners`) — по роли ресурса в балансировщике,
 * а не буквальным переводом контрактного имени; виртуальная машина
 * (`instances`) — тем словом, которым её называет всё остальное дерево.
 */
export const ENTITIES = {
  // iam
  accounts: { singular: "Аккаунт", plural: "Аккаунты", gender: "m" },
  projects: { singular: "Проект", plural: "Проекты", gender: "m" },
  users: { singular: "Пользователь", plural: "Пользователи", gender: "m" },
  "service-accounts": {
    singular: "Сервисный аккаунт",
    plural: "Сервисные аккаунты",
    gender: "m",
  },
  groups: { singular: "Группа", plural: "Группы", gender: "f" },
  roles: { singular: "Роль", plural: "Роли", gender: "f" },
  "access-bindings": {
    singular: "Привязка доступа",
    plural: "Привязки доступа",
    gender: "f",
  },
  operations: { singular: "Операция", plural: "Операции", gender: "f" },
  // vpc
  networks: { singular: "Облачная сеть", plural: "Облачные сети", gender: "f" },
  subnets: { singular: "Подсеть", plural: "Подсети", gender: "f" },
  addresses: { singular: "IP-адрес", plural: "IP-адреса", gender: "m" },
  "route-tables": {
    singular: "Таблица маршрутов",
    plural: "Таблицы маршрутов",
    gender: "f",
  },
  "security-groups": {
    singular: "Группа безопасности",
    plural: "Группы безопасности",
    gender: "f",
  },
  "network-interfaces": {
    singular: "Сетевой интерфейс",
    plural: "Сетевые интерфейсы",
    gender: "m",
  },
  gateways: { singular: "Шлюз", plural: "Шлюзы", gender: "m" },
  "address-pools": { singular: "Пул адресов", plural: "Пулы адресов", gender: "m" },
  "cidr-groups": { singular: "Набор префиксов", plural: "Наборы префиксов", gender: "m" },
  // compute
  instances: { singular: "Виртуальная машина", plural: "Виртуальные машины", gender: "f" },
  "machine-types": { singular: "Тип машины", plural: "Типы машин", gender: "m" },
  "placement-groups": { singular: "Группа размещения", plural: "Группы размещения", gender: "f" },
  "guest-access-keys": { singular: "Ключ доступа", plural: "Ключи доступа", gender: "m" },
  // storage
  volumes: { singular: "Том", plural: "Тома", gender: "m" },
  snapshots: { singular: "Снимок", plural: "Снимки", gender: "m" },
  images: { singular: "Образ", plural: "Образы", gender: "m" },
  "disk-types": { singular: "Тип диска", plural: "Типы дисков", gender: "m" },
  // nlb
  "load-balancers": {
    singular: "Балансировщик нагрузки",
    plural: "Балансировщики нагрузки",
    gender: "m",
  },
  listeners: { singular: "Обработчик", plural: "Обработчики", gender: "m" },
  "target-groups": { singular: "Целевая группа", plural: "Целевые группы", gender: "f" },
  // registry
  registries: { singular: "Реестр", plural: "Реестры", gender: "m" },
  // geo
  regions: { singular: "Регион", plural: "Регионы", gender: "m" },
  zones: { singular: "Зона", plural: "Зоны", gender: "f" },
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
