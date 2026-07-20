-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- COMP-1 Instance redesign: retire the raw YC-cruft sizing/boot/placement columns
-- (ban #2) and add the redesigned identity/sizing/boot/kind surface. Sizing flows
-- through machine_type_id (effective_resources mirrored into eff_* scalars resolved
-- from the machine_types catalog at Create); the OS enters through boot_source
-- (bs_* scalars + form-only image_kind); instance_kind gates one of vm_spec /
-- container_spec (JSONB). placement_group_id is an opaque passthrough slug (COMP-1;
-- existence/coherence → COMP-3). status_reason carries the next-boot deferral marker.
--
-- The partial UNIQUE(project_id, name) WHERE name<>'' already exists
-- (instances_project_name_uniq, migration 0001 + rename 0009): a hard DELETE
-- releases the slot so the same non-empty name is Create-able again (F15
-- name-recycle) — no soft tombstone holds it. status already DEFAULTs to
-- 'PROVISIONING' (0001), the COMP-1 resting status (durable; RUNNING transition +
-- launch-saga materialize → COMP-2).

-- 1) Add the redesign columns.
ALTER TABLE instances
  ADD COLUMN status_reason      TEXT    NOT NULL DEFAULT '',
  ADD COLUMN instance_kind      INTEGER NOT NULL DEFAULT 0 CHECK (instance_kind BETWEEN 0 AND 2),
  ADD COLUMN machine_type_id    TEXT    NOT NULL DEFAULT '',
  ADD COLUMN eff_vcpu           INTEGER NOT NULL DEFAULT 0 CHECK (eff_vcpu >= 0),
  ADD COLUMN eff_memory_mib     BIGINT  NOT NULL DEFAULT 0 CHECK (eff_memory_mib >= 0),
  ADD COLUMN eff_gpus           INTEGER NOT NULL DEFAULT 0 CHECK (eff_gpus >= 0),
  ADD COLUMN eff_gpu_type       TEXT    NOT NULL DEFAULT '',
  ADD COLUMN bs_type            TEXT    NOT NULL DEFAULT '',
  ADD COLUMN bs_id              TEXT    NOT NULL DEFAULT '',
  ADD COLUMN bs_image_kind      INTEGER NOT NULL DEFAULT 0 CHECK (bs_image_kind BETWEEN 0 AND 2),
  ADD COLUMN placement_group_id TEXT    NOT NULL DEFAULT '',
  ADD COLUMN vm_spec            JSONB,
  ADD COLUMN container_spec     JSONB;

-- 2) Retire the YC-cruft columns (ban #2). platform_id/cores/memory/core_fraction/
--    gpus → machine_type_id + eff_*; metadata_options (old gce/aws blob) →
--    vm_spec.metadata_options; scheduling_preemptible/gpu_cluster_id/
--    reserved_instance_pool_id/application → gone; placement_policy
--    (HostAffinityRule{yc.*}) → Internal* (COMP-4); image/image_digest → boot_source;
--    network_settings_type/serial_port_ssh_authorization/maintenance_*/
--    hardware_generation/host_group_id/host_id → not on the redesigned Instance.
ALTER TABLE instances
  DROP COLUMN platform_id,
  DROP COLUMN cores,
  DROP COLUMN memory,
  DROP COLUMN core_fraction,
  DROP COLUMN gpus,
  DROP COLUMN metadata_options,
  DROP COLUMN network_settings_type,
  DROP COLUMN scheduling_preemptible,
  DROP COLUMN placement_policy,
  DROP COLUMN serial_port_ssh_authorization,
  DROP COLUMN gpu_cluster_id,
  DROP COLUMN hardware_generation,
  DROP COLUMN maintenance_policy,
  DROP COLUMN maintenance_grace_period_seconds,
  DROP COLUMN reserved_instance_pool_id,
  DROP COLUMN host_group_id,
  DROP COLUMN host_id,
  DROP COLUMN application,
  DROP COLUMN image,
  DROP COLUMN image_digest;

-- +goose Down
ALTER TABLE instances
  ADD COLUMN platform_id                      TEXT    NOT NULL DEFAULT '',
  ADD COLUMN cores                            BIGINT  NOT NULL DEFAULT 2,
  ADD COLUMN memory                           BIGINT  NOT NULL DEFAULT 2147483648,
  ADD COLUMN core_fraction                    BIGINT  NOT NULL DEFAULT 100,
  ADD COLUMN gpus                             BIGINT  NOT NULL DEFAULT 0,
  ADD COLUMN metadata_options                 JSONB,
  ADD COLUMN network_settings_type            TEXT    NOT NULL DEFAULT 'STANDARD',
  ADD COLUMN scheduling_preemptible           BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN placement_policy                 JSONB,
  ADD COLUMN serial_port_ssh_authorization    TEXT    NOT NULL DEFAULT 'SSH_AUTHORIZATION_UNSPECIFIED',
  ADD COLUMN gpu_cluster_id                   TEXT    NOT NULL DEFAULT '',
  ADD COLUMN hardware_generation              JSONB,
  ADD COLUMN maintenance_policy               TEXT    NOT NULL DEFAULT '',
  ADD COLUMN maintenance_grace_period_seconds BIGINT  NOT NULL DEFAULT 0,
  ADD COLUMN reserved_instance_pool_id        TEXT    NOT NULL DEFAULT '',
  ADD COLUMN host_group_id                    TEXT    NOT NULL DEFAULT '',
  ADD COLUMN host_id                          TEXT    NOT NULL DEFAULT '',
  ADD COLUMN application                      JSONB,
  ADD COLUMN image                            TEXT    NOT NULL DEFAULT '',
  ADD COLUMN image_digest                     TEXT    NOT NULL DEFAULT '';

ALTER TABLE instances
  DROP COLUMN status_reason,
  DROP COLUMN instance_kind,
  DROP COLUMN machine_type_id,
  DROP COLUMN eff_vcpu,
  DROP COLUMN eff_memory_mib,
  DROP COLUMN eff_gpus,
  DROP COLUMN eff_gpu_type,
  DROP COLUMN bs_type,
  DROP COLUMN bs_id,
  DROP COLUMN bs_image_kind,
  DROP COLUMN placement_group_id,
  DROP COLUMN vm_spec,
  DROP COLUMN container_spec;
