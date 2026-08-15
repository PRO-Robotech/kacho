/**
 * Имена разделов и сущностей — ЗЕРКАЛО канона `shared/src/lib/entity-names.ts`.
 *
 * Почему зеркало, а не импорт. Образ хоста собирается из ЕГО дерева: Dockerfile
 * копирует только `host/`, каталога `shared/` в контексте сборки нет — импорт
 * оттуда сломал бы сборку образа. Поэтому у хоста своя копия, и она обязана
 * совпадать с каноном ЗНАЧЕНИЕ В ЗНАЧЕНИЕ.
 *
 * Чем это держится: `HostBreadcrumb.names.test.tsx` читает канон по
 * относительному пути (пробы исполняются из дерева исходников, где `shared/`
 * есть) и сравнивает обе карты. Расхождение не может приземлиться молча.
 *
 * Правка идёт СНАЧАЛА в канон, потом сюда. Обратный порядок оставляет канон
 * неверным, а проба всё равно покраснеет.
 */

/** Разделы консоли: короткое имя для крошки, развёрнутое — для заголовка. */
export const SERVICES: Record<string, { title: string; menuTitle: string }> = {
  iam: { title: "IAM", menuTitle: "Identity and Access Management" },
  vpc: { title: "VPC", menuTitle: "Virtual Private Cloud" },
  compute: { title: "Compute", menuTitle: "Compute" },
  storage: { title: "Storage", menuTitle: "Storage" },
  nlb: { title: "Load Balancer", menuTitle: "Load Balancer" },
  registry: { title: "Registry", menuTitle: "Registry" },
  geo: { title: "Geography", menuTitle: "Geography" },
  system: { title: "Администрирование", menuTitle: "Администрирование" },
};

/** Сущности по сегменту адреса. */
export const ENTITIES: Record<string, { singular: string; plural: string }> = {
  // iam
  accounts: { singular: "Аккаунт", plural: "Аккаунты" },
  projects: { singular: "Проект", plural: "Проекты" },
  users: { singular: "Пользователь", plural: "Пользователи" },
  "service-accounts": { singular: "Сервисный аккаунт", plural: "Сервисные аккаунты" },
  groups: { singular: "Группа", plural: "Группы" },
  roles: { singular: "Роль", plural: "Роли" },
  "access-bindings": { singular: "Привязка доступа", plural: "Привязки доступа" },
  operations: { singular: "Операция", plural: "Операции" },
  // vpc
  networks: { singular: "Облачная сеть", plural: "Облачные сети" },
  subnets: { singular: "Подсеть", plural: "Подсети" },
  addresses: { singular: "IP-адрес", plural: "IP-адреса" },
  "route-tables": { singular: "Таблица маршрутов", plural: "Таблицы маршрутов" },
  "security-groups": { singular: "Группа безопасности", plural: "Группы безопасности" },
  "network-interfaces": { singular: "Сетевой интерфейс", plural: "Сетевые интерфейсы" },
  gateways: { singular: "Шлюз", plural: "Шлюзы" },
  "address-pools": { singular: "Пул адресов", plural: "Пулы адресов" },
  // compute
  instances: { singular: "Виртуальная машина", plural: "Виртуальные машины" },
  "machine-types": { singular: "Тип машины", plural: "Типы машин" },
  // storage
  volumes: { singular: "Том", plural: "Тома" },
  snapshots: { singular: "Снимок", plural: "Снимки" },
  images: { singular: "Образ", plural: "Образы" },
  "disk-types": { singular: "Тип диска", plural: "Типы дисков" },
  // nlb
  "load-balancers": { singular: "Балансировщик нагрузки", plural: "Балансировщики нагрузки" },
  listeners: { singular: "Обработчик", plural: "Обработчики" },
  "target-groups": { singular: "Целевая группа", plural: "Целевые группы" },
  // registry
  registries: { singular: "Реестр", plural: "Реестры" },
  // geo
  regions: { singular: "Регион", plural: "Регионы" },
  zones: { singular: "Зона", plural: "Зоны" },
};
