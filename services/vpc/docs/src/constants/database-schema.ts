import { defineDbSchemaDiagramFromDbml } from '@site/src/utils/dbmlToDiagram'

// DBML-описание схемы `kacho_vpc`. Источник истины — `internal/migrations/*.sql`
// этого сервиса.
//
// ГРАНИЦА ДИАГРАММЫ ЗАДАНА СВОЙСТВОМ, А НЕ НОМЕРОМ МИГРАЦИИ. Рисуются:
//
//   * таблица КАЖДОГО ресурса, которым домен управляет полным набором глаголов
//     `Create/Get/List/Update/Delete` — их девять, по числу ресурсных служб
//     контракта `kacho.cloud.vpc.v1` (`networks`, `subnets`, `addresses`,
//     `route_tables`, `security_groups`, `gateways`, `network_interfaces`,
//     `cidr_groups`, `address_pools`);
//   * две инфраструктурные — `operations` и `vpc_outbox`.
//
// Итого одиннадцать из двадцати шести живых таблиц схемы. Остальные пятнадцать —
// служебные: подтаблицы состава ресурса, внутренние структуры IPAM, очереди и
// счётчики. Они не рисуются НАМЕРЕННО, и это не «отставание»: полный перечень
// схемы, все двадцать шесть, стоит на странице модели данных — там же сказано,
// какое из двух изображений чему подмножество.
//
// Прежняя редакция задавала границу иначе — «baseline `0001` плюс инкременты
// 0002..0005», — и граница эта устарела молча: миграций в дереве уже пятьдесят
// три, а последняя зовётся `912001_address_secondary_cursor_indexes.sql`. Номер
// миграции не свойство схемы, поэтому такая граница не может ни выполняться, ни
// нарушаться — она просто стареет. Свойство выше проверяемо: службы контракта
// перечисляются `grep -hoP '^service \K\w+' proto/kacho/cloud/vpc/v1/*.proto`.
//
// ДВЕ ТАБЛИЦЫ, КОТОРЫХ ЗДЕСЬ НЕТ ПО ДРУГОЙ ПРИЧИНЕ — они СНЯТЫ, а не скрыты:
//
//   * `cloud_pool_selector` — заведена базовой схемой `0001_initial.sql` и
//     дропнута `0002_drop_override_and_cloud_pool_selector.sql` вместе с
//     cloud-selector-шагом IPAM-каскада (`dropguard.json`: version 2, kind
//     `retire`). Применённую миграцию не правят, поэтому `0001` создаёт таблицу
//     вечно, а до головы цепочки она не доживает;
//   * `vpc_watch_cursors` — снята `20260828114800_drop_watch_cursors.sql`
//     (kacho#1148): позиция подписки принадлежит клиенту, серверных курсоров по
//     подписчику не существует.
//
// Ни той, ни другой нет и в перечне на странице модели данных — и быть не должно.
const DATABASE_SCHEMA_DBML = `
Table "kacho_vpc"."networks" {
  "id" text [pk, not null]
  "project_id" text [not null, note: 'cross-service ref → project (no FK)']
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "description" text
  "labels" jsonb
  "default_security_group_id" text [note: 'nullable FK → security_groups (SET NULL, миграция 0005) + partial UNIQUE ≤1 default-SG на сеть']
  "created_at" timestamptz [not null]
  Note: 'Network — VPC-контейнер. Project-level. Root ресурсной иерархии.'
}

Table "kacho_vpc"."subnets" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "network_id" text [not null, note: 'FK → networks (RESTRICT)']
  "zone_id" text [note: 'cross-service ref → geo.Zone (no FK); владелец каталога размещения — kacho-geo, не compute']
  "v4_cidr_blocks" text_array
  "v6_cidr_blocks" text_array
  "route_table_id" text [note: 'FK → route_tables (SET NULL)']
  "v4_cidr_primary" cidr [note: 'GENERATED STORED']
  "v6_cidr_primary" cidr [note: 'GENERATED STORED']
  "created_at" timestamptz [not null]
  Note: 'Subnet — подсеть в зоне. EXCLUDE-констрейнт против overlap CIDR.'
}

Table "kacho_vpc"."addresses" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "used" bool
  "deletion_protection" bool
  "external_ipv4" jsonb
  "internal_ipv4" jsonb
  "internal_ipv6" jsonb
  "internal_subnet_id" text [note: 'GENERATED из v4/v6; FK → subnets (RESTRICT)']
  "created_at" timestamptz [not null]
  Note: 'Address — internal / external IP. external = project-level, internal = в subnet.'
}

Table "kacho_vpc"."route_tables" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "network_id" text [not null, note: 'FK → networks (NO ACTION)']
  "static_routes" jsonb
  "created_at" timestamptz [not null]
  Note: 'RouteTable — auto-association с Subnet через PL/pgSQL триггеры.'
}

Table "kacho_vpc"."security_groups" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "network_id" text [note: 'FK → networks (RESTRICT, nullable)']
  "default_for_network" bool
  "rules" jsonb
  "created_at" timestamptz [not null]
  Note: 'SecurityGroup — правила в jsonb, OCC через xmin. Default-SG inline при Network.Create.'
}

Table "kacho_vpc"."gateways" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "gateway_type" text
  "created_at" timestamptz [not null]
  Note: 'Gateway — выход наружу. Вид (NAT / EGRESS_ONLY) и подсеть-якорь задаются при создании и неизменяемы; сеть и размещение наследуются от подсети.'
}

Table "kacho_vpc"."network_interfaces" {
  "id" text [pk, not null]
  "project_id" text [not null]
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "subnet_id" text [not null, note: 'FK → subnets (RESTRICT)']
  "v4_address_ids" jsonb [note: 'soft-ref → addresses; CHECK len<=1']
  "v6_address_ids" jsonb [note: 'soft-ref → addresses; CHECK len<=1']
  "security_group_ids" jsonb [note: 'soft-ref → security_groups']
  "used_by_id" text [note: 'atomic CAS (attach/detach)']
  "mac_address" text [unique, note: 'UNIQUE cloud-wide, output-only']
  "status" text
  "created_at" timestamptz [not null]
  Note: 'NetworkInterface — самостоятельный сетевой интерфейс, отдельно от нагрузки. ≤1 v4 / ≤1 v6.'
}

Table "kacho_vpc"."cidr_groups" {
  "id" text [pk, not null]
  "project_id" text [not null, note: 'cross-service ref → project (no FK)']
  "name" text [not null, note: 'UNIQUE (project_id, name)']
  "description" text
  "labels" jsonb
  "v4_count" int [note: 'материализованная кардинальность состава: CHECK не умеет считать строки дочерней таблицы']
  "v6_count" int [note: 'то же по второму семейству; инкремент берёт row-lock родителя и сериализует конкурентные вставки']
  "created_at" timestamptz [not null]
  Note: 'CidrGroup — именованный набор CIDR-блоков, на который ссылается правило SecurityGroup. Состав — в дочерней cidr_group_blocks (на диаграмме не рисуется).'
}

Table "kacho_vpc"."address_pools" {
  "id" text [pk, not null]
  "name" text
  "v4_cidr_blocks" text_array
  "v6_cidr_blocks" text_array
  "kind" smallint
  "is_default" bool [note: 'partial UNIQUE (zone_id, kind) WHERE is_default']
  "selector_labels" jsonb [note: 'GIN index']
  "selector_priority" int
  "zone_id" text [note: 'cross-service ref (no FK)']
  "created_at" timestamptz [not null]
  Note: '(admin) Глобальный пул CIDR для IPAM. Internal-only, нет в публичном API.'
}

Table "kacho_vpc"."operations" {
  "id" text [pk, not null]
  "description" text
  "done" bool
  "metadata_type" text
  "metadata_data" bytea
  "resource_id" text
  "response_type" text
  "response_data" bytea
  "created_at" timestamptz
  "modified_at" timestamptz
  Note: 'Long-running async-операции (LRO). Каждая мутация возвращает Operation.'
}

Table "kacho_vpc"."vpc_outbox" {
  "sequence_no" bigint [pk, not null, note: 'DEFAULT nextval(seq)']
  "resource_kind" text
  "resource_id" text
  "event_type" text
  "payload" jsonb
  "created_at" timestamptz
  Note: 'Транзакционный outbox + pg_notify(vpc_outbox) для InternalWatch-стрима.'
}

// ── hard FK (within-service, DB-уровень) ─────────────────────────────────────
Ref "network_id":"kacho_vpc"."networks"."id" < "kacho_vpc"."subnets"."network_id" [delete: restrict]
Ref "network_id":"kacho_vpc"."networks"."id" < "kacho_vpc"."route_tables"."network_id" [delete: no action]
Ref "network_id":"kacho_vpc"."networks"."id" < "kacho_vpc"."security_groups"."network_id" [delete: restrict]
Ref "subnet_id":"kacho_vpc"."subnets"."id" < "kacho_vpc"."network_interfaces"."subnet_id" [delete: restrict]
Ref "internal_subnet_id":"kacho_vpc"."subnets"."id" < "kacho_vpc"."addresses"."internal_subnet_id" [delete: restrict]
Ref "route_table_id":"kacho_vpc"."route_tables"."id" < "kacho_vpc"."subnets"."route_table_id" [delete: set null]
Ref "default_security_group_id":"kacho_vpc"."security_groups"."id" < "kacho_vpc"."networks"."default_security_group_id" [delete: set null]

// ── soft-ref (по id, без FK — массивы / nullable указатели) ───────────────────
Ref "address_ids[] soft":"kacho_vpc"."addresses"."id" < "kacho_vpc"."network_interfaces"."v4_address_ids" [soft]
Ref "security_group_ids[] soft":"kacho_vpc"."security_groups"."id" < "kacho_vpc"."network_interfaces"."security_group_ids" [soft]
`

// tone + раскладка по сетке (column/row). resource — ресурсные таблицы,
// binding — admin/IPAM, system — служебные.
const DATABASE_SCHEMA_TABLE_META = {
  networks: { tone: 'resource', position: { column: 0, row: 1 } },
  route_tables: { tone: 'resource', position: { column: 1, row: 0 } },
  subnets: { tone: 'resource', position: { column: 1, row: 1 } },
  security_groups: { tone: 'resource', position: { column: 1, row: 2 } },
  gateways: { tone: 'resource', position: { column: 1, row: 3 } },
  network_interfaces: { tone: 'resource', position: { column: 2, row: 0 } },
  addresses: { tone: 'resource', position: { column: 2, row: 1 } },
  cidr_groups: { tone: 'resource', position: { column: 0, row: 2 } },
  address_pools: { tone: 'binding', position: { column: 2, row: 3 } },
  operations: { tone: 'system', position: { column: 3, row: 0 } },
  vpc_outbox: { tone: 'system', position: { column: 3, row: 1 } },
} as const

export const DATABASE_SCHEMA_DIAGRAM = defineDbSchemaDiagramFromDbml({
  title: 'ER-диаграмма схемы `kacho_vpc`',
  description:
    'Основные таблицы схемы kacho_vpc с ключами и связями. Сплошные связи — hard-FK (within-service, DB-уровень); связи с пометкой «soft» — ссылки по id без FK (массивы адресов/SG).',
  columns: 4,
  legend: [
    { label: 'Ресурсы', tone: 'resource' },
    { label: 'IPAM / admin', tone: 'binding' },
    { label: 'Служебные таблицы', tone: 'system' },
  ],
  dbml: DATABASE_SCHEMA_DBML,
  tableMeta: DATABASE_SCHEMA_TABLE_META,
})
