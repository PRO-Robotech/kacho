-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0015: дом для политики бэкенда — зарегистрированный бэкенд и НЕИЗМЕНЯЕМАЯ ревизия
-- привязки класса к нему.
--
-- WHY — что было сломано и чем это лечится.
--
-- Класс диска сегодня резолвится ССЫЛКОЙ: том хранит disk_type_id и читает свойства
-- класса из справочника В МОМЕНТ ИСПОЛЬЗОВАНИЯ. Справочник при этом изменяем, а его
-- правка синхронна, не оставляет ни операции, ни следа. Значит правка справочника
-- задним числом меняет свойства уже созданных томов, и заметить это неоткуда.
-- С появлением бэкенда цена вырастает качественно: сменив координату у класса,
-- администратор молча переадресует данные существующих томов.
--
-- Лечится не запретом правки, а её формой: ревизии привязки append-only и
-- НЕИЗМЕНЯЕМЫ. Правка класса создаёт НОВУЮ ревизию, прежняя остаётся жить, пока на
-- неё ссылается хоть один ресурс. Ресурс ссылается на ревизию, под которой создан,
-- поэтому «что обещали ЭТОМУ тому» восстановимо навсегда, а джойн к справочнику не
-- может уехать: цель неизменяема by construction. Обновления у ревизии нет и не
-- должно быть — отсутствие обновления и есть механизм.
--
-- Способности живут ЗДЕСЬ, а не на классе. Это свойство бэкенда, а не продуктовой
-- политики: умеет ли он снимки, зависит ли клон от родителя, есть ли у него корзина
-- и с каким сроком. Публичный класс их ВЫВОДИТ пересечением по своим действующим
-- ревизиям — консервативно, потому что класс предлагается в нескольких зонах, а
-- зоны могут обслуживаться разными бэкендами.
--
-- Учётные данные хранятся ССЫЛКОЙ. Секрет не проходит через API и не ложится в БД:
-- строка таблицы переживает ротацию и попадает в резервные копии.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

CREATE TABLE kacho_storage.storage_backends (
    id              text         PRIMARY KEY,                   -- 'sb-<crockford>'
    name            text         NOT NULL,
    kind            text         NOT NULL,
    description     text         NOT NULL DEFAULT '',
    zone_ids        jsonb        NOT NULL DEFAULT '[]'::jsonb,
    endpoint        text         NOT NULL,
    credentials_ref text         NOT NULL,
    status          text         NOT NULL DEFAULT 'ACTIVE',
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT storage_backends_kind_known
        CHECK (kind IN ('CEPH_RBD')),
    CONSTRAINT storage_backends_status_known
        CHECK (status IN ('ACTIVE','DRAINING','DISABLED')),
    CONSTRAINT storage_backends_zone_ids_is_array
        CHECK (jsonb_typeof(zone_ids) = 'array'),
    CONSTRAINT storage_backends_endpoint_present
        CHECK (endpoint <> ''),
    CONSTRAINT storage_backends_credentials_ref_present
        CHECK (credentials_ref <> '')
);

CREATE UNIQUE INDEX storage_backends_name_uniq ON kacho_storage.storage_backends (name);

CREATE TABLE kacho_storage.disk_type_bindings (
    id             text         PRIMARY KEY,                    -- 'dtb-<crockford>'
    disk_type_id   text         NOT NULL REFERENCES kacho_storage.disk_types(id)      ON DELETE RESTRICT,
    zone_id        text         NOT NULL,
    backend_id     text         NOT NULL REFERENCES kacho_storage.storage_backends(id) ON DELETE RESTRICT,
    revision       integer      NOT NULL,
    pool           text         NOT NULL,
    namespace_template text     NOT NULL DEFAULT '',
    cap_snapshots           boolean NOT NULL DEFAULT false,
    cap_clone_from_snapshot boolean NOT NULL DEFAULT false,
    cap_clone_from_image    boolean NOT NULL DEFAULT false,
    cap_clone_keeps_parent  boolean NOT NULL DEFAULT false,
    cap_online_grow         boolean NOT NULL DEFAULT false,
    cap_multi_attach        boolean NOT NULL DEFAULT false,
    cap_encryption_at_rest  boolean NOT NULL DEFAULT false,
    trash_ttl_seconds       bigint  NOT NULL DEFAULT 0,
    qos            jsonb        NOT NULL DEFAULT '{}'::jsonb,
    status         text         NOT NULL DEFAULT 'ACTIVE',
    created_at     timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT disk_type_bindings_status_known
        CHECK (status IN ('ACTIVE','SUPERSEDED')),
    CONSTRAINT disk_type_bindings_revision_positive
        CHECK (revision > 0),
    CONSTRAINT disk_type_bindings_zone_present
        CHECK (zone_id <> ''),
    CONSTRAINT disk_type_bindings_pool_present
        CHECK (pool <> ''),
    CONSTRAINT disk_type_bindings_trash_ttl_not_negative
        CHECK (trash_ttl_seconds >= 0),
    CONSTRAINT disk_type_bindings_qos_is_object
        CHECK (jsonb_typeof(qos) = 'object')
);

-- Ровно одна ДЕЙСТВУЮЩАЯ ревизия на пару (класс, зона). Частичная уникальность, а не
-- программная проверка: две конкурентные регистрации иначе обе прошли бы чтение и
-- обе записали.
CREATE UNIQUE INDEX disk_type_bindings_active_uniq
    ON kacho_storage.disk_type_bindings (disk_type_id, zone_id) WHERE status = 'ACTIVE';

-- Номер ревизии уникален в пределах пары — вместе с append-only это делает историю
-- восстановимой без догадок.
CREATE UNIQUE INDEX disk_type_bindings_revision_uniq
    ON kacho_storage.disk_type_bindings (disk_type_id, zone_id, revision);

CREATE INDEX disk_type_bindings_backend_idx ON kacho_storage.disk_type_bindings (backend_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP TABLE IF EXISTS kacho_storage.disk_type_bindings;
DROP TABLE IF EXISTS kacho_storage.storage_backends;
-- +goose StatementEnd
