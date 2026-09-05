import { defineDbSchemaDiagramFromDbml } from '@site/src/utils/dbmlToDiagram'

// DBML-описание схемы `kacho_storage`. Источник истины —
// `services/storage/internal/migrations/*.sql`: доменные таблицы заведены
// миграцией 0003, образы и происхождение тома от образа добавлены 0007,
// собственный якорь снимка — 0014, реестр бэкендов и ревизии привязки — 0015,
// политика класса (ярус словарём, состояние обращения, границы) — 0016,
// ссылка на ревизию и наблюдаемые колонки на трёх ресурсах — 0017, составной
// первичный ключ привязок — 0018. Показаны ресурсные таблицы, реестр бэкендов,
// ревизии привязки, таблица привязок и служебные (operations, очередь
// регистрации в модели прав).
const DATABASE_SCHEMA_DBML = `
Table "kacho_storage"."disk_types" {
  "id" text [pk, not null, note: 'slug, назначает администратор при регистрации класса']
  "name" text [not null]
  "description" text [not null]
  "zone_ids" jsonb [not null, note: 'массив зон, где класс предлагается; [] — ограничения нет']
  "performance_tier" text [not null, note: "CHECK IN ('','CAPACITY','BALANCED','FAST','SINGLE','IO_MAX') — закрытый словарь, не свободная строка"]
  "lifecycle" text [not null, note: "CHECK IN ('ACTIVE','DEPRECATED','RETIRED'); меняется глаголом SetLifecycle"]
  "min_size_bytes" bigint [not null, note: '0 — класс не сужает снизу']
  "max_size_bytes" bigint [not null, note: '0 — класс не сужает сверху']
  "size_step_bytes" bigint [not null, note: 'CHECK: границы кратны шагу; 0 — кратность не требуется']
  "created_at" timestamptz [not null]
  Note: 'DiskType — класс диска: продуктовая политика. Read-only публично, регистрация только на внутреннем листенере. Каталог НЕ засеян: пустой каталог — законное состояние.'
}

Table "kacho_storage"."storage_backends" {
  "id" text [pk, not null, note: "'sb-' + crockford-base32"]
  "name" text [not null, note: 'UNIQUE в реестре; адресация идёт по id, имя косметическое']
  "kind" text [not null, note: 'CHECK по закрытому перечню родов — за каждым значением стоит адаптер в нашем коде']
  "description" text [not null]
  "zone_ids" jsonb [not null, note: 'зоны, которые бэкенд обслуживает']
  "endpoint" text [not null, note: 'непрозрачная координата <схема>://<путь>; CHECK: непусто']
  "credentials_ref" text [not null, note: 'ССЫЛКА на учётные данные, не секрет; CHECK: непусто']
  "status" text [not null, note: "CHECK IN ('ACTIVE','DRAINING','DISABLED')"]
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  Note: 'StorageBackend — зарегистрированный бэкенд блочного хранения. Internal-only: запись инфра-чувствительна целиком и на публичную поверхность не проецируется ни одним полем.'
}

Table "kacho_storage"."disk_type_bindings" {
  "id" text [pk, not null, note: "'dtb-' + crockford-base32"]
  "disk_type_id" text [not null, note: 'FK → disk_types (RESTRICT)']
  "zone_id" text [not null, note: 'ссылка через границу сервиса → kacho-geo (без FK); CHECK: непусто']
  "backend_id" text [not null, note: 'FK → storage_backends (RESTRICT)']
  "revision" int [not null, note: 'UNIQUE (disk_type_id, zone_id, revision); CHECK > 0; присваивает сервис']
  "pool" text [not null, note: 'инфра-чувствительно: пул бэкенда; CHECK: непусто']
  "namespace_template" text [not null, note: 'шаблон единицы изоляции арендатора, подставляется project_id']
  "cap_snapshots" bool [not null]
  "cap_clone_from_snapshot" bool [not null]
  "cap_clone_from_image" bool [not null]
  "cap_clone_keeps_parent" bool [not null, note: 'true → удаление источника с живыми клонами отвергается']
  "cap_online_grow" bool [not null]
  "cap_multi_attach" bool [not null, note: 'читается предикатом вставки привязки, а не проверкой в коде']
  "cap_encryption_at_rest" bool [not null]
  "trash_ttl_seconds" bigint [not null, note: 'CHECK >= 0; > 0 — ёмкость освобождается отложенно']
  "qos" jsonb [not null, note: 'формула baseline + за_ГиБ × размер, ограниченная max; CHECK: объект']
  "status" text [not null, note: "CHECK IN ('ACTIVE','SUPERSEDED'); partial UNIQUE (disk_type_id, zone_id) WHERE status='ACTIVE'"]
  "created_at" timestamptz [not null]
  Note: 'DiskTypeBinding — НЕИЗМЕНЯЕМАЯ ревизия политики для пары (класс, зона). Append-only: строки не редактируются и не удаляются, пока на них ссылаются ресурсы. Отсюда правка класса не может задним числом изменить свойства созданного тома.'
}

Table "kacho_storage"."volumes" {
  "id" text [pk, not null, note: 'vol + 17 crockford-base32']
  "project_id" text [not null, note: 'ссылка через границу сервиса → kaname (без FK)']
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  "name" text [note: "partial UNIQUE (project_id, name) WHERE name<>''"]
  "description" text [not null]
  "labels" jsonb [not null]
  "zone_id" text [not null, note: 'ссылка через границу сервиса → kacho-geo (без FK)']
  "disk_type_id" text [not null, note: 'FK → disk_types (RESTRICT)']
  "binding_id" text [note: 'FK → disk_type_bindings (RESTRICT) — политика, под которой том создан']
  "desired_binding_id" text [note: 'FK → disk_type_bindings (RESTRICT) — целевая ревизия на время ChangeDiskType']
  "size_bytes" bigint [not null, note: 'CHECK > 0; Update — только увеличение, в границах класса']
  "block_size" bigint [not null, note: 'колонка схемы со своим умолчанием; контрактом НЕ адресуется — у поля не было читателя, меняющего поведение']
  "backend_object" text [note: 'partial UNIQUE WHERE NOT NULL — имя объекта у бэкенда, инфра-чувствительно']
  "backend_namespace" text [not null, note: 'единица изоляции арендатора, выведенная из project_id']
  "source_snapshot_id" text [note: 'FK → snapshots (SET NULL)']
  "source_image_id" text [note: 'FK → images (SET NULL) — происхождение, не живая зависимость']
  "state" text [not null, note: 'ЖЕЛАЕМОЕ: CHECK IN (CREATING, READY, DELETING, ERROR)']
  "observed_state" text [not null, note: 'НАБЛЮДЁННОЕ: CHECK IN (ABSENT, READY, ERROR, UNKNOWN) — отдельная колонка, иначе дрейф ненаходим']
  "observed_at" timestamptz [note: 'NULL — сверщик не смотрел ни разу']
  "observed_size_bytes" bigint
  "used_bytes" bigint [note: 'NULL — бэкенд потребления не сообщил; это НЕ ноль']
  "status_reason" text [not null, note: 'CHECK: закрытый словарь наших полос, не текст бэкенда']
  Note: 'Volume — зональный блочный том. AVAILABLE и IN_USE выводятся из наличия привязки; CREATING/MIGRATING/DELETING/ERROR — из расхождения желаемого и наблюдённого. Частичный индекс расхождения (state <> observed_state) — рабочий список сверщика.'
}

Table "kacho_storage"."volume_attachments" {
  "volume_id" text [pk, not null, note: 'часть составного PK (volume_id, instance_id) + FK → volumes (RESTRICT)']
  "instance_id" text [not null, note: 'часть составного PK; ссылка через границу сервиса → kacho-compute (без FK)']
  "instance_name" text [not null, note: 'снимок значения на момент привязки']
  "project_id" text [not null]
  "zone_id" text [not null]
  "device_name" text [not null, note: 'UNIQUE (instance_id, device_name)']
  "is_boot" bool [not null, note: 'EXCLUDE USING gist (instance_id) WHERE is_boot — не более одного загрузочного тома на машину']
  "mode" text [not null, note: 'CHECK IN (READ_WRITE, READ_ONLY)']
  "auto_delete" bool [not null]
  "attached_at" timestamptz [not null]
  Note: 'VolumeAttachment — источник истины о привязке тома к машине. Живёт у владельца-storage. PK составной: сколько привязок допускает том, решает способность его ревизии, а не форма ключа.'
}

Table "kacho_storage"."snapshots" {
  "id" text [pk, not null, note: 'snp + 17 crockford-base32']
  "project_id" text [not null]
  "created_at" timestamptz [not null]
  "updated_at" timestamptz [not null]
  "name" text [note: "partial UNIQUE (project_id, name) WHERE name<>''"]
  "description" text [not null]
  "labels" jsonb [not null]
  "zone_id" text [not null, note: 'СОБСТВЕННЫЙ якорь размещения: снимается с зоны исходного тома, переживает его удаление']
  "binding_id" text [note: 'FK → disk_type_bindings (RESTRICT)']
  "backend_object" text [note: 'partial UNIQUE WHERE NOT NULL']
  "source_volume_id" text [note: 'FK → volumes (SET NULL) — снимок переживает исходный том']
  "size_bytes" bigint [not null]
  "state" text [not null, note: 'ЖЕЛАЕМОЕ: CHECK IN (CREATING, READY, DELETING, ERROR)']
  "observed_state" text [not null, note: 'НАБЛЮДЁННОЕ: CHECK IN (ABSENT, READY, ERROR, UNKNOWN)']
  "observed_at" timestamptz
  "status_reason" text [not null, note: 'CHECK: закрытый словарь наших полос']
  Note: 'Snapshot — снимок тома. Несёт собственную зону: ссылка на источник обнуляется при его удалении, и зона, добираемая через источник, однажды становится пустой — проверка когерентности выродилась бы в тождественно-истинную.'
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
  "binding_id" text [note: 'FK → disk_type_bindings (RESTRICT)']
  "backend_object" text [note: 'partial UNIQUE WHERE NOT NULL; у зарегистрированного образа приходит от регистрации']
  "source_snapshot_id" text [note: 'FK → snapshots (SET NULL)']
  "source_volume_id" text [note: 'FK → volumes (SET NULL)']
  "size_bytes" bigint [not null]
  "min_disk_bytes" bigint [not null, note: 'минимальный размер тома; сверяется внутри вставки тома']
  "format" text [not null, note: 'CHECK IN (STANDARD)']
  "state" text [not null, note: 'ЖЕЛАЕМОЕ: CHECK IN (CREATING, READY, DELETING, ERROR)']
  "observed_state" text [not null, note: 'НАБЛЮДЁННОЕ: CHECK IN (ABSENT, READY, ERROR, UNKNOWN)']
  "observed_at" timestamptz
  "status_reason" text [not null, note: 'CHECK: закрытый словарь наших полос']
  Note: 'Image — региональный (anycast) образ. CHECK: источник — снимок ЛИБО том, не оба сразу. Образ вносится либо публичным Create из своего источника, либо регистрацией внесённого провайдером объекта.'
}

Table "kacho_storage"."operations" {
  "id" text [pk, not null, note: 'sop + 17 crockford-base32']
  "description" text
  "created_at" timestamptz [not null]
  "done" bool [not null]
  "metadata" jsonb
  "result" jsonb
  Note: 'Operation — общая для всех сервисов таблица долгих операций из corelib. Мутации возвращают её, клиент поллит Get. done = «намерение закоммичено», не «объект у бэкенда готов».'
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

// Связи внутри одной БД — настоящие внешние ключи. RESTRICT стоит там, где ссылка
// означает ИСПОЛЬЗОВАНИЕ (удалять нельзя, пока пользуются), SET NULL — там, где она
// означает ПРОИСХОЖДЕНИЕ (источник можно удалить, данные остаются).
Ref: "kacho_storage"."disk_type_bindings"."disk_type_id" > "kacho_storage"."disk_types"."id"
Ref: "kacho_storage"."disk_type_bindings"."backend_id" > "kacho_storage"."storage_backends"."id"
Ref: "kacho_storage"."volumes"."disk_type_id" > "kacho_storage"."disk_types"."id"
Ref: "kacho_storage"."volumes"."binding_id" > "kacho_storage"."disk_type_bindings"."id"
Ref: "kacho_storage"."volumes"."source_snapshot_id" > "kacho_storage"."snapshots"."id"
Ref: "kacho_storage"."volumes"."source_image_id" > "kacho_storage"."images"."id"
Ref: "kacho_storage"."snapshots"."binding_id" > "kacho_storage"."disk_type_bindings"."id"
Ref: "kacho_storage"."snapshots"."source_volume_id" > "kacho_storage"."volumes"."id"
Ref: "kacho_storage"."images"."binding_id" > "kacho_storage"."disk_type_bindings"."id"
Ref: "kacho_storage"."images"."source_snapshot_id" > "kacho_storage"."snapshots"."id"
Ref: "kacho_storage"."images"."source_volume_id" > "kacho_storage"."volumes"."id"
Ref: "kacho_storage"."volume_attachments"."volume_id" > "kacho_storage"."volumes"."id"
`

// tone + раскладка по сетке (column/row). resource — ресурсные таблицы,
// binding — таблицы связи (привязка тома к машине, ревизия политики),
// system — служебные.
const DATABASE_SCHEMA_TABLE_META = {
  storage_backends: { tone: 'system', position: { column: 0, row: 0 } },
  disk_type_bindings: { tone: 'binding', position: { column: 0, row: 1 } },
  disk_types: { tone: 'resource', position: { column: 0, row: 2 } },
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
    'Таблицы схемы kacho_storage с ключами и связями. Сплошные связи — настоящие FK внутри одной БД сервиса; ссылки на project, zone, region и instance живут как TEXT без FK, потому что эти ресурсы принадлежат другим сервисам и проверяются вызовом владельца. Ресурс ссылается на НЕИЗМЕНЯЕМУЮ ревизию привязки, поэтому правка справочника классов не меняет свойств уже созданных строк.',
  columns: 3,
  legend: [
    { label: 'Ресурсы', tone: 'resource' },
    { label: 'Привязки и ревизии', tone: 'binding' },
    { label: 'Служебные таблицы', tone: 'system' },
  ],
  dbml: DATABASE_SCHEMA_DBML,
  tableMeta: DATABASE_SCHEMA_TABLE_META,
})
