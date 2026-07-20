-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- COMP-1 F7: MachineType — the sync sizing catalog and single sizing channel for
-- the compute redesign (retires raw ResourcesSpec/platform_id on Instance, F2).
-- Flat table; effective_resources is unpacked into scalar columns (v_cpu /
-- memory_mib / gpus / gpu_type) rather than a JSONB blob so filter=minGpus= is an
-- indexable predicate. name is the stable human-readable slug ("std-v3-2"),
-- UNIQUE across the catalog (the alternate reference on Instance.machineTypeId;
-- canonical echo is always the mt- id). family/status are small enums pinned by a
-- DB CHECK (within-service invariant on the DB level, data-integrity §within-service).
-- Admin-managed via InternalMachineTypeService (:9091); public read is ambient.
CREATE TABLE machine_types (
  id              TEXT         PRIMARY KEY,
  name            TEXT         NOT NULL,
  description     TEXT         NOT NULL DEFAULT '',
  family          INTEGER      NOT NULL DEFAULT 0 CHECK (family BETWEEN 0 AND 4),
  v_cpu           INTEGER      NOT NULL DEFAULT 0 CHECK (v_cpu >= 0),
  memory_mib      BIGINT       NOT NULL DEFAULT 0 CHECK (memory_mib >= 0),
  gpus            INTEGER      NOT NULL DEFAULT 0 CHECK (gpus >= 0),
  gpu_type        TEXT         NOT NULL DEFAULT '',
  available_zones TEXT[]       NOT NULL DEFAULT '{}',
  status          INTEGER      NOT NULL DEFAULT 1 CHECK (status BETWEEN 0 AND 3),
  labels          JSONB        NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- name is UNIQUE across the whole catalog (cluster-scoped, not project-scoped):
-- the tenant references a size by mt- id OR by this stable name, so a duplicate
-- name would make name-resolution ambiguous. DB-backstop for ALREADY_EXISTS
-- (SQLSTATE 23505 → service maps to AlreadyExists), not software check-then-act.
CREATE UNIQUE INDEX machine_types_name_uniq ON machine_types (name);

-- family filter (List?family=GPU) — indexable class discovery.
CREATE INDEX machine_types_family_idx ON machine_types (family);

-- cursor pagination anchor (created_at, id) ASC — parity with the other catalogs.
CREATE INDEX machine_types_created_at_idx ON machine_types (created_at, id);

-- +goose Down
DROP TABLE machine_types;
