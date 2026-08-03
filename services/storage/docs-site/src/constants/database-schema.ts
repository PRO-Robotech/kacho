import { defineDbSchemaDiagramFromDbml } from '@site/src/utils/dbmlToDiagram'

// DBML-описание схемы `kacho_storage`. Источник истины —
// `services/storage/internal/migrations/*.sql`: доменные таблицы заведены
// миграцией 0003, каталог типов засеян 0004, образы и происхождение тома от
// образа добавлены 0007. Показаны ресурсные таблицы, таблица привязок и
// служебные (operations, очередь регистрации в модели прав).
const DATABASE_SCHEMA_DBML = `
Table "kacho_storage"."disk_types" {
  "id" text [pk, not null, note: 'человекочитаемый slug, напр. block-balanced']
  "name" text [not null]
  "description" text [not null]
  "zone_ids" jsonb [not null, note: 'массив зон доступности; [] — ограничения нет']
  "performance_tier" text [not null, note: 'нативный класс производительности']
  "created_at" timestamptz [not null]
  Note: 'DiskType — каталог типов диска. Read-only публично, admin-CRUD только на внутреннем листенере.'
}

Table "kacho_storage"."volumes" {
  "id" text [pk, not null, note: 'vol + 17 crockford-base32']
  "project_id" text [not null, note: 'ссылка через границу сервиса → kacho-iam (без FK)']
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  "name" text [note: "partial UNIQUE (project_id, name) WHERE name<>''"]
  "description" text [not null]
  "labels" jsonb [not null]
  "zone_id" text [not null, note: 'ссылка через границу сервиса → kacho-geo (без FK)']
  "disk_type_id" text [not null, note: 'FK → disk_types (RESTRICT)']
  "size_bytes" bigint [not null, note: 'CHECK > 0; Update — только увеличение']
  "block_size" bigint [not null, note: 'CHECK > 0; по умолчанию 4096']
  "source_snapshot_id" text [note: 'FK → snapshots (SET NULL)']
  "source_image_id" text [note: 'FK → images (SET NULL) — происхождение, не живая зависимость']
  "state" text [not null, note: 'CHECK IN (CREATING, READY, DELETING, ERROR)']
  Note: 'Volume — зональный блочный том. AVAILABLE и IN_USE выводятся из наличия привязки и колонкой не являются.'
}

Table "kacho_storage"."volume_attachments" {
  "volume_id" text [pk, not null, note: 'PK + FK → volumes (RESTRICT): одна привязка на том и запрет удаления привязанного тома']
  "instance_id" text [not null, note: 'ссылка через границу сервиса → kacho-compute (без FK)']
  "instance_name" text [not null, note: 'снимок значения на момент привязки']
  "project_id" text [not null]
  "zone_id" text [not null]
  "device_name" text [not null, note: 'UNIQUE (instance_id, device_name)']
  "is_boot" bool [not null, note: 'EXCLUDE USING gist (instance_id) WHERE is_boot — не более одного загрузочного тома на машину']
  "mode" text [not null, note: 'CHECK IN (READ_WRITE, READ_ONLY)']
  "auto_delete" bool [not null]
  "attached_at" timestamptz [not null]
  Note: 'VolumeAttachment — источник истины о привязке тома к машине. Живёт у владельца-storage.'
}

Table "kacho_storage"."snapshots" {
  "id" text [pk, not null, note: 'snp + 17 crockford-base32']
  "project_id" text [not null]
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  "name" text [note: "partial UNIQUE (project_id, name) WHERE name<>''"]
  "description" text [not null]
  "labels" jsonb [not null]
  "source_volume_id" text [note: 'FK → volumes (SET NULL) — снимок переживает исходный том']
  "size_bytes" bigint [not null]
  "state" text [not null, note: 'CHECK IN (CREATING, READY, DELETING, ERROR)']
  Note: 'Snapshot — снимок тома. Собственной колонки размещения не несёт: о размещении свидетельствует только исходный том.'
}

Table "kacho_storage"."images" {
  "id" text [pk, not null, note: 'img + 17 crockford-base32']
  "project_id" text [not null]
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  "name" text [note: "partial UNIQUE (project_id, name) WHERE name<>''"]
  "description" text [not null]
  "labels" jsonb [not null]
  "region_id" text [not null, note: 'ссылка через границу сервиса → kacho-geo (без FK); якорь REGIONAL']
  "source_snapshot_id" text [note: 'FK → snapshots (SET NULL)']
  "source_volume_id" text [note: 'FK → volumes (SET NULL)']
  "size_bytes" bigint [not null]
  "min_disk_bytes" bigint [not null, note: 'минимальный размер тома; сверяется внутри вставки тома']
  "format" text [not null, note: 'CHECK IN (STANDARD)']
  "state" text [not null, note: 'CHECK IN (CREATING, READY, DELETING, ERROR)']
  Note: 'Image — региональный (anycast) образ. CHECK: источник — снимок ЛИБО том, не оба сразу.'
}

Table "kacho_storage"."operations" {
  "id" text [pk, not null, note: 'sop + 17 crockford-base32']
  "description" text
  "created_at" timestamptz [not null]
  "done" bool [not null]
  "metadata" jsonb
  "result" jsonb
  Note: 'Operation — общая для всех сервисов таблица долгих операций из corelib. Мутации возвращают её, клиент поллит Get.'
}

Table "kacho_storage"."fga_register_outbox" {
  "id" bigserial [pk, not null]
  "subject_id" text [not null]
  "relation" text [not null]
  "object" text [not null]
  "sent_at" timestamptz [note: 'NULL — ещё не доставлено']
  "attempt_count" int [not null]
  Note: 'Очередь регистрации владельца ресурса в модели прав: намерение пишется в той же транзакции, доставка — не менее одного раза.'
}
`

// tone + раскладка по сетке (column/row). resource — ресурсные таблицы,
// binding — таблица привязок, system — служебные.
const DATABASE_SCHEMA_TABLE_META = {
  disk_types: { tone: 'resource', position: { column: 0, row: 0 } },
  volumes: { tone: 'resource', position: { column: 1, row: 0 } },
  snapshots: { tone: 'resource', position: { column: 1, row: 1 } },
  images: { tone: 'resource', position: { column: 1, row: 2 } },
  volume_attachments: { tone: 'binding', position: { column: 2, row: 0 } },
  operations: { tone: 'system', position: { column: 2, row: 1 } },
  fga_register_outbox: { tone: 'system', position: { column: 2, row: 2 } },
} as const

export const DATABASE_SCHEMA_DIAGRAM = defineDbSchemaDiagramFromDbml({
  title: 'ER-диаграмма схемы `kacho_storage`',
  description:
    'Таблицы схемы kacho_storage с ключами и связями. Сплошные связи — настоящие FK внутри одной БД сервиса; ссылки на project, zone, region и instance живут как TEXT без FK, потому что эти ресурсы принадлежат другим сервисам и проверяются вызовом владельца.',
  columns: 3,
  legend: [
    { label: 'Ресурсы', tone: 'resource' },
    { label: 'Привязки', tone: 'binding' },
    { label: 'Служебные таблицы', tone: 'system' },
  ],
  dbml: DATABASE_SCHEMA_DBML,
  tableMeta: DATABASE_SCHEMA_TABLE_META,
})
