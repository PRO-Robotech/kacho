-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Storage-split contract phase: compute stops owning block storage.
--
-- kacho-storage is the declared owner of Volume / Snapshot / Image / DiskType, but
-- compute kept serving a SECOND, independent copy of Disk / Image / Snapshot — its
-- own tables, gRPC services, REST routes and catalog entries. Two owners for one
-- resource type is not a migration state anyone can reason about: a caller had no
-- way to know which copy answered, and the two never reconciled. The compute copy
-- is withdrawn whole (proto, routes, both catalog copies, service layer,
-- repositories, black boxes, docs); these three tables are the last of it.
--
-- No data moves with this. Nothing ever seeded a volume, image or snapshot here —
-- the only rows any migration inserts are the disk-type and geography catalogues —
-- and the split was designed without a carry-over from the start: the join table
-- that bound volumes to instances (`attached_disks`) was dropped outright in 0013,
-- not copied. An id-preserving copy is not available even in principle, because the
-- provenance columns on the storage side are foreign keys into storage's own rows;
-- the two id spaces were never the same.
--
-- The attach-state FK that used to point at `disks` went with `attached_disks` in
-- 0013, so these three tables are referenced by nothing and drop independently of
-- each other and of `disk_types` (which the DiskType retire drops separately).
DROP TABLE IF EXISTS disks;
DROP TABLE IF EXISTS images;
DROP TABLE IF EXISTS snapshots;

-- +goose Down
-- Recreate the three tables in their pre-retire shape: the 0001 baseline as amended
-- by 0009, which renamed the tenant-container column to project_id and its indexes
-- to match. Empty, as they were — the Up dropped no rows that existed.
CREATE TABLE disks (
  id                    TEXT         PRIMARY KEY,
  project_id            TEXT         NOT NULL,
  created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
  name                  TEXT         NOT NULL DEFAULT '',
  description           TEXT         NOT NULL DEFAULT '',
  labels                JSONB        NOT NULL DEFAULT '{}'::jsonb,
  type_id               TEXT         NOT NULL DEFAULT 'network-ssd',
  zone_id               TEXT         NOT NULL,
  size                  BIGINT       NOT NULL,
  block_size            BIGINT       NOT NULL DEFAULT 4096,
  product_ids           JSONB        NOT NULL DEFAULT '[]'::jsonb,
  status                TEXT         NOT NULL DEFAULT 'READY',
  source_image_id       TEXT         NOT NULL DEFAULT '',
  source_snapshot_id    TEXT         NOT NULL DEFAULT '',
  disk_placement_policy JSONB,
  hardware_generation   JSONB,
  kms_key               JSONB
);
CREATE INDEX disks_project_idx    ON disks (project_id);
CREATE INDEX disks_created_at_idx ON disks (created_at);
CREATE UNIQUE INDEX disks_project_name_uniq ON disks (project_id, name) WHERE name <> '';

CREATE TABLE images (
  id                  TEXT         PRIMARY KEY,
  project_id          TEXT         NOT NULL,
  created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
  name                TEXT         NOT NULL DEFAULT '',
  description         TEXT         NOT NULL DEFAULT '',
  labels              JSONB        NOT NULL DEFAULT '{}'::jsonb,
  family              TEXT         NOT NULL DEFAULT '',
  storage_size        BIGINT       NOT NULL DEFAULT 0,
  min_disk_size       BIGINT       NOT NULL DEFAULT 0,
  product_ids         JSONB        NOT NULL DEFAULT '[]'::jsonb,
  status              TEXT         NOT NULL DEFAULT 'READY',
  os_type             TEXT         NOT NULL DEFAULT 'LINUX',
  os_nvidia_driver    TEXT         NOT NULL DEFAULT '',
  pooled              BOOLEAN      NOT NULL DEFAULT false,
  hardware_generation JSONB,
  kms_key             JSONB,
  source_image_id     TEXT         NOT NULL DEFAULT '',
  source_snapshot_id  TEXT         NOT NULL DEFAULT '',
  source_disk_id      TEXT         NOT NULL DEFAULT '',
  source_uri          TEXT         NOT NULL DEFAULT ''
);
CREATE INDEX images_project_idx    ON images (project_id);
CREATE INDEX images_created_at_idx ON images (created_at);
CREATE INDEX images_family_idx     ON images (project_id, family, created_at DESC) WHERE family <> '';
CREATE UNIQUE INDEX images_project_name_uniq ON images (project_id, name) WHERE name <> '';

CREATE TABLE snapshots (
  id                  TEXT         PRIMARY KEY,
  project_id          TEXT         NOT NULL,
  created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
  name                TEXT         NOT NULL DEFAULT '',
  description         TEXT         NOT NULL DEFAULT '',
  labels              JSONB        NOT NULL DEFAULT '{}'::jsonb,
  storage_size        BIGINT       NOT NULL DEFAULT 0,
  disk_size           BIGINT       NOT NULL DEFAULT 0,
  product_ids         JSONB        NOT NULL DEFAULT '[]'::jsonb,
  status              TEXT         NOT NULL DEFAULT 'READY',
  source_disk_id      TEXT         NOT NULL DEFAULT '',
  hardware_generation JSONB,
  kms_key             JSONB
);
CREATE INDEX snapshots_project_idx     ON snapshots (project_id);
CREATE INDEX snapshots_created_at_idx  ON snapshots (created_at);
CREATE INDEX snapshots_source_disk_idx ON snapshots (source_disk_id) WHERE source_disk_id <> '';
CREATE UNIQUE INDEX snapshots_project_name_uniq ON snapshots (project_id, name) WHERE name <> '';
