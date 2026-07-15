-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Kachō Storage — доменные таблицы (Volume / VolumeAttachment / Snapshot / DiskType)
-- =============================================================================
-- Все within-service инварианты выражены DB-конструкциями (FK / UNIQUE / EXCLUDE /
-- CHECK), не software check-then-act (ban #10). Cross-service ссылки (zone_id→geo,
-- instance_id→compute) — TEXT без FK, валидируются peer-API в коде сервиса.
--
-- Циклический FK (volumes.source_snapshot_id → snapshots, snapshots.source_volume_id
-- → volumes) разрешен так: сначала создаются обе таблицы БЕЗ этих двух FK, затем
-- ALTER TABLE … ADD CONSTRAINT добавляет оба (ниже). Это единственный корректный
-- порядок для взаимной ссылки без DEFERRABLE — на момент CREATE каждая таблица уже
-- существует. Остальные CHECK/FK объявлены inline (никаких post-hoc ALTER для них).

SET search_path TO kacho_storage, public;

-- btree_gist — для EXCLUDE (instance_id WITH =) в volume_attachments (gist по
-- text-равенству). Extension-owned, остается в public.
CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;

-- +goose StatementEnd

-- +goose StatementBegin
-- kacho_labels_valid — валидация labels JSONB (cardinality ≤64, key regex, value
-- length ≤63). Используется в CHECK всех ресурсов с labels. Own-schema копия
-- (schema-per-service; функция иммутабельна, годна для CHECK).
CREATE OR REPLACE FUNCTION kacho_storage.kacho_labels_valid(lbls jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE AS $fn$
DECLARE
    k text;
    v text;
    n int;
BEGIN
    IF lbls IS NULL THEN
        RETURN true;
    END IF;
    -- JSONB-null ('null'::jsonb) — легитимная сериализация Go nil-map'а из repo.
    IF jsonb_typeof(lbls) = 'null' THEN
        RETURN true;
    END IF;
    IF jsonb_typeof(lbls) <> 'object' THEN
        RETURN false;
    END IF;
    SELECT count(*) INTO n FROM jsonb_object_keys(lbls);
    IF n > 64 THEN
        RETURN false;
    END IF;
    FOR k, v IN SELECT key, value FROM jsonb_each_text(lbls) LOOP
        IF k !~ '^[a-z][-_./\\@a-z0-9]{0,62}$' THEN
            RETURN false;
        END IF;
        IF length(v) > 63 THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin

-- =============================================================================
-- disk_types (каталог типов диска; id — человекочитаемый slug, admin-assigned)
-- =============================================================================
-- Ссылается volumes.disk_type_id → disk_types(id) ON DELETE RESTRICT (том держит
-- свой тип; тип с томами удалить нельзя). zone_ids — jsonb-массив зон, где тип
-- доступен ([] = не ограничен). performance_tier — нативный Kachō-tier (ban #2).

CREATE TABLE kacho_storage.disk_types (
    id               text         PRIMARY KEY,          -- slug, напр. "block-balanced"
    name             text         NOT NULL DEFAULT '',
    description      text         NOT NULL DEFAULT '',
    zone_ids         jsonb        NOT NULL DEFAULT '[]'::jsonb,
    performance_tier text         NOT NULL DEFAULT '',
    created_at       timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT disk_types_description_check
        CHECK (length(description) <= 256),
    CONSTRAINT disk_types_zone_ids_is_array
        CHECK (jsonb_typeof(zone_ids) = 'array')
);

-- =============================================================================
-- snapshots (снапшот тома; переживает исходный том → source_volume_id SET NULL)
-- =============================================================================
-- source_volume_id FK → volumes(id) добавляется ALTER-ом ниже (циклическая ссылка).
-- state ∈ {CREATING,READY,DELETING,ERROR} (IN_USE/AVAILABLE — derived, НЕ колонка).

CREATE TABLE kacho_storage.snapshots (
    id               text         PRIMARY KEY,           -- "snp<crockford>"
    project_id       text         NOT NULL,
    created_at       timestamptz  NOT NULL DEFAULT now(),
    updated_at       timestamptz  NOT NULL DEFAULT now(),
    name             text         NOT NULL DEFAULT '',
    description      text         NOT NULL DEFAULT '',
    labels           jsonb        NOT NULL DEFAULT '{}'::jsonb,
    source_volume_id text,                                -- FK добавляется ALTER-ом (cyclic)
    size_bytes       bigint       NOT NULL DEFAULT 0,
    state            text         NOT NULL DEFAULT 'CREATING',

    CONSTRAINT snapshots_name_check
        CHECK (name = '' OR name ~ '^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$'),
    CONSTRAINT snapshots_description_check
        CHECK (length(description) <= 256),
    CONSTRAINT snapshots_labels_valid
        CHECK (kacho_storage.kacho_labels_valid(labels)),
    CONSTRAINT snapshots_size_bytes_check
        CHECK (size_bytes >= 0),
    CONSTRAINT snapshots_state_check
        CHECK (state IN ('CREATING','READY','DELETING','ERROR'))
);

CREATE UNIQUE INDEX snapshots_name_uniq
    ON kacho_storage.snapshots (project_id, name) WHERE name <> '';
CREATE INDEX snapshots_project_idx       ON kacho_storage.snapshots (project_id);
CREATE INDEX snapshots_source_volume_idx ON kacho_storage.snapshots (source_volume_id);
CREATE INDEX snapshots_created_at_idx    ON kacho_storage.snapshots (created_at);

-- =============================================================================
-- volumes (блочный диск)
-- =============================================================================
-- disk_type_id → disk_types(id) RESTRICT (same-DB FK, immutable). source_snapshot_id
-- → snapshots(id) SET NULL добавляется ALTER-ом ниже (cyclic). zone_id — TEXT→geo
-- (peer-validated, БЕЗ FK). state ∈ {CREATING,READY,DELETING,ERROR}; IN_USE/AVAILABLE
-- — DERIVED из наличия volume_attachments-строки (НЕ колонка, фикс дрейфа B3).
-- size_bytes>0; Update — только увеличение (increase-only CAS в repo, §3b).

CREATE TABLE kacho_storage.volumes (
    id                 text         PRIMARY KEY,          -- "vol<crockford>"
    project_id         text         NOT NULL,
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    name               text         NOT NULL DEFAULT '',
    description        text         NOT NULL DEFAULT '',
    labels             jsonb        NOT NULL DEFAULT '{}'::jsonb,
    zone_id            text         NOT NULL,             -- cross-service → geo, БЕЗ FK
    disk_type_id       text         NOT NULL REFERENCES kacho_storage.disk_types(id) ON DELETE RESTRICT,
    size_bytes         bigint       NOT NULL,
    block_size         bigint       NOT NULL DEFAULT 4096,
    source_snapshot_id text,                              -- FK добавляется ALTER-ом (cyclic)
    state              text         NOT NULL DEFAULT 'CREATING',

    CONSTRAINT volumes_name_check
        CHECK (name = '' OR name ~ '^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$'),
    CONSTRAINT volumes_description_check
        CHECK (length(description) <= 256),
    CONSTRAINT volumes_labels_valid
        CHECK (kacho_storage.kacho_labels_valid(labels)),
    CONSTRAINT volumes_size_bytes_check
        CHECK (size_bytes > 0),
    CONSTRAINT volumes_block_size_check
        CHECK (block_size > 0),
    CONSTRAINT volumes_state_check
        CHECK (state IN ('CREATING','READY','DELETING','ERROR'))
);

CREATE UNIQUE INDEX volumes_name_uniq
    ON kacho_storage.volumes (project_id, name) WHERE name <> '';
CREATE INDEX volumes_project_idx         ON kacho_storage.volumes (project_id);
CREATE INDEX volumes_disk_type_idx       ON kacho_storage.volumes (disk_type_id);        -- DiskType.used_by derive
CREATE INDEX volumes_source_snapshot_idx ON kacho_storage.volumes (source_snapshot_id);  -- Snapshot.used_by derive
CREATE INDEX volumes_created_at_idx      ON kacho_storage.volumes (created_at);

-- =============================================================================
-- volume_attachments (co-located join — attach-state; source of truth Volume.attachments)
-- =============================================================================
-- volume_id PK + FK RESTRICT → один attachment на том глобально (①) И Volume.Delete
-- блокируется, пока том примонтирован — оба DB-enforced (не software refcount).
-- instance_id — cross-service (compute owns Instance), БЕЗ FK. Attach — атомарный
-- INSERT … ON CONFLICT CAS в repo (§3.2), не TOCTOU. status IN_USE — derived из
-- наличия этой строки.

CREATE TABLE kacho_storage.volume_attachments (
    volume_id     text         PRIMARY KEY REFERENCES kacho_storage.volumes(id) ON DELETE RESTRICT,  -- ①
    instance_id   text         NOT NULL,                 -- cross-service, БЕЗ FK
    instance_name text         NOT NULL DEFAULT '',      -- write-time snapshot на Attach
    project_id    text         NOT NULL,
    zone_id       text         NOT NULL,
    device_name   text         NOT NULL,
    is_boot       boolean      NOT NULL DEFAULT false,
    mode          text         NOT NULL DEFAULT 'READ_WRITE',
    auto_delete   boolean      NOT NULL DEFAULT false,
    attached_at   timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT volume_attachments_mode_check
        CHECK (mode IN ('READ_WRITE','READ_ONLY')),
    CONSTRAINT volume_attachments_instance_device_uniq
        UNIQUE (instance_id, device_name),                                     -- ② уникальное имя устройства в инстансе
    CONSTRAINT volume_attachments_one_boot
        EXCLUDE USING gist (instance_id WITH =) WHERE (is_boot)                -- ③ ≤1 boot-том на инстанс
);

CREATE INDEX volume_attachments_instance_idx
    ON kacho_storage.volume_attachments (instance_id);   -- batched ListAttachments

-- =============================================================================
-- Циклические FK volumes ↔ snapshots — ADD после создания обеих таблиц.
-- =============================================================================
ALTER TABLE kacho_storage.volumes
    ADD CONSTRAINT volumes_source_snapshot_fk
    FOREIGN KEY (source_snapshot_id) REFERENCES kacho_storage.snapshots(id) ON DELETE SET NULL;

ALTER TABLE kacho_storage.snapshots
    ADD CONSTRAINT snapshots_source_volume_fk
    FOREIGN KEY (source_volume_id) REFERENCES kacho_storage.volumes(id) ON DELETE SET NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- FK-циклы разрываем явным DROP CONSTRAINT (порядок DROP TABLE иначе неоднозначен),
-- затем таблицы. btree_gist остается в public (общий объект).
ALTER TABLE IF EXISTS kacho_storage.snapshots DROP CONSTRAINT IF EXISTS snapshots_source_volume_fk;
ALTER TABLE IF EXISTS kacho_storage.volumes   DROP CONSTRAINT IF EXISTS volumes_source_snapshot_fk;

DROP TABLE IF EXISTS kacho_storage.volume_attachments;
DROP TABLE IF EXISTS kacho_storage.volumes;
DROP TABLE IF EXISTS kacho_storage.snapshots;
DROP TABLE IF EXISTS kacho_storage.disk_types;
DROP FUNCTION IF EXISTS kacho_storage.kacho_labels_valid(jsonb);
-- +goose StatementEnd
