-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- Partial index supporting the register-drainer's partition-head-only CLAIM
-- (corelib drainer Config.PartitionColumn = 'resource_id').
--
-- WHY: kacho_registry.registry_outbox is a REGISTER-outbox despite its name — it
-- carries BOTH fga.register AND fga.unregister of the SAME object (Registry
-- Create/Update/Delete; Repository register-on-first-push / unregister-on-last-tag;
-- repository-config adopt-owner / public-grant). iam's materialisation is only
-- PARTIALLY versioned: source_version-LWW (`resource_mirror` UPSERT `WHERE
-- resource_mirror.source_version < EXCLUDED.source_version`) guards the
-- ON-CONFLICT-UPDATE branch ONLY, while unregister is a HARD DELETE that leaves no
-- tombstone. So a reordered STALE register finds no row to compare against, takes the
-- INSERT branch and RESURRECTS the mirror row of a DELETED registry/repository —
-- permanently, because iam's reconciler is level-triggered off the mirror and
-- re-materialises the owner/parent tuple forever. There is no self-healing (and for a
-- repository that means a pull-grant surviving the deletion of its last tag).
--
-- The reorder is NOT caused by concurrency: the claim orders by (attempt_count, id),
-- so a transiently-bumped predecessor (attempt_count >= 1 after an iam blip) loses to
-- a fresh successor (attempt_count = 0) even at ApplyConcurrency = 1 (registry's
-- default) — and lands in a LATER batch. The per-repo pg_advisory_xact_lock taken by
-- emitRepoIntent does NOT help: it serialises the WRITE side (monotonic
-- source_version), not the drainer's claim order. PartitionColumn closes the class at
-- CLAIM level: a row is never claimed while a DELIVERABLE (sent_at IS NULL AND
-- attempt_count < MaxAttempts) same-partition predecessor with a smaller id exists,
-- via
--
--   AND NOT EXISTS (SELECT 1 FROM kacho_registry.registry_outbox p
--                    WHERE p.sent_at IS NULL AND p.attempt_count < $max
--                      AND p.id < t.id
--                      AND p.resource_id = t.resource_id)
--
-- Without an index this correlated NOT EXISTS is a seq-scan per candidate row on
-- every claim — quadratic under a backlog. This index makes it an index-only lookup:
-- seek to the resource key, then range-scan ids < t.id.
--
-- WHY resource_id is the right partition key: emitFGAIntent stamps it from
-- RegisterIntent.ResourceID, and every intent's tuples target exactly ONE FGA object
-- whose id is that value — "<regId>" for registry_registry, "<regId>/<repo>" for
-- registry_repository. Both are globally unique by construction (core rule #15 —
-- registry addressing is id-based), so "same partition" is exactly "same iam-mirror
-- object", and a registry never shares a partition with its repositories.
--
-- WHAT: btree on (resource_id, id), PARTIAL on sent_at IS NULL so its size tracks
-- only the PENDING backlog (drained rows fall out), never the whole (append-mostly)
-- table. The leading key matches Config.PartitionColumn byte-for-byte; the trailing
-- id column serves the `p.id < t.id` range and the ORDER BY (…, id) tie-break.
--
-- Behavioural lock: pkg/outbox/drainer Test_1_4_45_RegisterOutbox_UnregisterThenStale
-- Register (no PartitionColumn → resurrect; with it → correctly ABSENT).
--
-- Plain (in-tx) CREATE INDEX IF NOT EXISTS, matching the table's sibling pending
-- index (migration 0001). The pending backlog is small on a healthy stand; on a
-- matured cluster where the build's brief table write-lock would stall the
-- drainer/writers, switch to CREATE INDEX CONCURRENTLY (needs the goose
-- NO-TRANSACTION directive, keep IF NOT EXISTS).
CREATE INDEX IF NOT EXISTS registry_outbox_partition_head_idx
    ON kacho_registry.registry_outbox (resource_id, id)
    WHERE sent_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kacho_registry.registry_outbox_partition_head_idx;

-- +goose StatementEnd
