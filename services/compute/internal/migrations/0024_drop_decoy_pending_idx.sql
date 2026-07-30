-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- Drop compute_fga_register_outbox_pending_idx — the `(id) WHERE sent_at IS NULL` index created together with the
-- table. It serves no query today, and its mere EXISTENCE is what lets the planner
-- build the O(backlog) claim that the partition-head and claim-order indexes were
-- added to prevent.
--
-- WHY (this is a plan fix, not tidying)
--
-- The drainer claims with `PartitionColumn = "resource_id"`, i.e.
--
--   SELECT t.id FROM compute_fga_register_outbox t
--    WHERE t.sent_at IS NULL AND t.attempt_count < $max
--      AND NOT EXISTS (SELECT 1 FROM compute_fga_register_outbox p
--                       WHERE p.sent_at IS NULL AND p.attempt_count < $max
--                         AND p.id < t.id AND p.resource_id = t.resource_id)
--    ORDER BY t.attempt_count, t.id
--    FOR UPDATE OF t SKIP LOCKED
--    LIMIT $n
--
-- Its whole efficiency rests on the LIMIT stopping an INDEX-ORDERED outer scan
-- after the first handful of partition heads; only an index in `(attempt_count, id)`
-- order can deliver that order, and ANY other access path forces a Sort. A queue
-- table is empty almost all of the time, so its last ANALYZE almost always ran on an
-- empty backlog and the estimate it carries INTO a burst is `rows=1`. On a one-row
-- estimate a Sort is free — so the planner happily picks
--
--   Index Scan (compute_fga_register_outbox_pending_idx) -> Sort -> LockRows -> Limit
--
-- because this narrower index offers a cheap-looking scan over the same pending
-- rows. That plan cannot stop early: the Sort must materialise the WHOLE pending set
-- first, so the partition anti-join runs once per pending row and the claim's cost
-- becomes quadratic in the backlog — the claim reads the whole queue it is draining.
--
-- The same decoy was measured on a live stand in kacho_iam.fga_outbox (Postgres 16,
-- 8 600 pending rows, statistics gathered while the queue was empty, the SAME claim
-- statement differing only in whether the decoy exists): 6 990 ms per claim with it
-- against 29 ms without. It was dropped there by migration 0068 and the rule was
-- written down in pkg/outbox/drainer Config.PartitionColumn — an outbox table
-- carries EXACTLY TWO partial indexes over `sent_at IS NULL`, (partition_key, id)
-- and (attempt_count, id), and NO OTHER. This service kept a third one.
--
-- The autoanalyze storage parameter added earlier shortens the window of bad
-- statistics; it does not change the bad PLAN. Removing the decoy does.
--
-- NOTHING READS IT
--
--   * the claim — never used it as intended; it was only ever the decoy above.
--     Its own creating migration justifies it by an `ORDER BY id` claim shape that
--     no longer exists;
--   * the backlog / oldest-pending-age metric (pkg/outbox/metrics) — aggregates the
--     WHOLE table with FILTER clauses and no WHERE, so it seq-scans regardless;
--   * the per-partition wedge scan (pkg/outbox/drainer) — groups by the partition
--     key and is served by the (resource_id, id) partition-head index.
--
-- Locked by the service-side index-SET test, which asserts the whole set of partial
-- indexes over the pending rows rather than the presence of the two wanted ones —
-- a presence check never reports a third.

DROP INDEX IF EXISTS compute_fga_register_outbox_pending_idx;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE INDEX IF NOT EXISTS compute_fga_register_outbox_pending_idx
    ON compute_fga_register_outbox (id)
    WHERE sent_at IS NULL;

-- +goose StatementEnd
