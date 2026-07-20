-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0007: STOR-1 — net-new `images` (VM boot-image, REGIONAL/anycast) + Volume boot
-- materialisation column `volumes.source_image_id`.
--
-- Within-service инварианты — на DB-уровне (FK / partial-UNIQUE / CHECK), не
-- software check-then-act (ban #10):
--   - images.source_snapshot_id → snapshots(id) ON DELETE SET NULL (provenance);
--   - images.source_volume_id   → volumes(id)   ON DELETE SET NULL (provenance);
--   - at-most-one source (snapshot XOR volume, но 0 допустимо) — mutual-exclusion CHECK,
--     СОГЛАСОВАН с provenance-FK SET NULL: удаление источника, засевшего Image, зануляет
--     source-колонку → Image переживает source-less (STOR-1-28/F5). «exactly-one» энфорсится
--     в domain-слое sync-validate (F12/STOR-1-24: both/none → InvalidArgument), а НЕ в DB —
--     иначе SET NULL ронял бы 23514 на удалении источника (см. images_source_at_most_one);
--   - volumes.source_image_id   → images(id)    ON DELETE SET NULL (provenance,
--     STOR-1-28: удаление Image не блокируется томом, а очищает lineage — блочные
--     данные уже засеяны и независимы от Image; контраст с attachment→volume RESTRICT).
--
-- Циклическая ссылка volumes ↔ images (images.source_volume_id → volumes,
-- volumes.source_image_id → images) разрешена порядком: `images` создаётся ПОСЛЕ
-- volumes/snapshots (0003, уже существуют) с inline-FK на них; затем ALTER volumes
-- добавляет FK на уже созданную `images`. DEFERRABLE не требуется — на момент каждого
-- ADD обе таблицы существуют.
--
-- region_id — cross-service TEXT → kacho-geo (peer-validated на Create, БЕЗ FK).
-- placement REGIONAL (anycast): у Image нет zone_id; ZONAL boot-Volume когерентен через
-- zone.region ∈ image.region (peer-check, geoconsumer-gated). format — native Kachō
-- single-tier enum (ban #2): CHECK IN ('STANDARD'). size_bytes/min_disk_bytes derived
-- из источника на Insert. state ∈ {CREATING,READY,DELETING,ERROR}; Create → READY сразу.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

CREATE TABLE kacho_storage.images (
    id                 text         PRIMARY KEY,          -- "img<crockford>"
    project_id         text         NOT NULL,
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    name               text         NOT NULL DEFAULT '',
    description        text         NOT NULL DEFAULT '',
    labels             jsonb        NOT NULL DEFAULT '{}'::jsonb,
    region_id          text         NOT NULL,             -- cross-service → geo, БЕЗ FK (REGIONAL anchor)
    source_snapshot_id text         REFERENCES kacho_storage.snapshots(id) ON DELETE SET NULL,
    source_volume_id   text         REFERENCES kacho_storage.volumes(id)   ON DELETE SET NULL,
    size_bytes         bigint       NOT NULL DEFAULT 0,
    min_disk_bytes     bigint       NOT NULL DEFAULT 0,
    format             text         NOT NULL DEFAULT 'STANDARD',
    state              text         NOT NULL DEFAULT 'CREATING',

    CONSTRAINT images_name_check
        CHECK (name = '' OR name ~ '^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$'),
    CONSTRAINT images_description_check
        CHECK (length(description) <= 256),
    CONSTRAINT images_labels_valid
        CHECK (kacho_storage.kacho_labels_valid(labels)),
    CONSTRAINT images_size_bytes_check
        CHECK (size_bytes >= 0),
    CONSTRAINT images_min_disk_bytes_check
        CHECK (min_disk_bytes >= 0),
    CONSTRAINT images_format_check
        CHECK (format IN ('STANDARD')),
    CONSTRAINT images_state_check
        CHECK (state IN ('CREATING','READY','DELETING','ERROR')),
    -- at-most-one source (mutual-exclusion, 0 допустимо) — backstop к sync-validate (F12).
    -- НЕ «exactly-one»: provenance-FK source_snapshot_id/source_volume_id — ON DELETE SET
    -- NULL (STOR-1-28/F5), удаление источника зануляет source-колонку → Image становится
    -- source-less (блочные данные уже материализованы, независимы от источника). «exactly-one»
    -- CHECK противоречил бы SET NULL (23514-abort на source-delete). Domain.Validate() (F12/
    -- STOR-1-24) энфорсит at-least-one на Create (both/none → InvalidArgument); DB-CHECK ловит
    -- лишь «оба непусты» — единственный инвариант, который SET NULL нарушить не может.
    CONSTRAINT images_source_at_most_one
        CHECK (NOT (source_snapshot_id IS NOT NULL AND source_volume_id IS NOT NULL))
);

CREATE UNIQUE INDEX images_name_uniq
    ON kacho_storage.images (project_id, name) WHERE name <> '';
CREATE INDEX images_project_idx         ON kacho_storage.images (project_id);
CREATE INDEX images_source_snapshot_idx ON kacho_storage.images (source_snapshot_id);
CREATE INDEX images_source_volume_idx   ON kacho_storage.images (source_volume_id);
CREATE INDEX images_created_at_idx      ON kacho_storage.images (created_at);

-- volumes.source_image_id — boot-Volume материализация из Image (F9). FK ON DELETE
-- SET NULL (provenance, STOR-1-28) — цикл volumes↔images разрешён после CREATE images.
ALTER TABLE kacho_storage.volumes
    ADD COLUMN source_image_id text;
ALTER TABLE kacho_storage.volumes
    ADD CONSTRAINT volumes_source_image_fk
    FOREIGN KEY (source_image_id) REFERENCES kacho_storage.images(id) ON DELETE SET NULL;
CREATE INDEX volumes_source_image_idx ON kacho_storage.volumes (source_image_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.volumes_source_image_idx;
ALTER TABLE IF EXISTS kacho_storage.volumes DROP CONSTRAINT IF EXISTS volumes_source_image_fk;
ALTER TABLE IF EXISTS kacho_storage.volumes DROP COLUMN IF EXISTS source_image_id;
DROP TABLE IF EXISTS kacho_storage.images;
-- +goose StatementEnd
