-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- Keep planner statistics fresh on the kacho_storage.fga_register_outbox queue table. Second half of the
-- claim-throughput fix; migration 0009 (the ordering index) does not land without it.
--
-- WHY: the drainer's partition-head-only claim filters on `sent_at IS NULL`, and
-- the planner's row estimate for that predicate comes from the LAST ANALYZE. A
-- queue table is empty most of the time, so the last analyze almost always
-- happened while the backlog was ZERO — and the estimate the planner carries into
-- a burst is therefore `rows=1`. On a one-row estimate it discards the partial
-- claim indexes entirely and picks a nested loop over the pending index,
-- re-scanning the pending set once per candidate row.
--
-- Measured on kacho-iam's fga_outbox, which carries the same claim shape, the
-- SAME statement against the SAME table differing only in statistics freshness:
--
--     backlog   stale stats (last ANALYZE on empty queue)   after ANALYZE
--       2 495                                   4 488 ms
--       7 849                                                      3.6 ms
--
-- i.e. a stale-stats plan is ~1000x slower on a DEEPER backlog, re-creating
-- exactly the O(backlog) claim the ordering index was meant to remove. The index
-- alone is necessary but NOT sufficient — the planner has to be able to see that
-- the pending set is large enough to be worth an ordered index scan.
--
-- Autovacuum's default analyze trigger is `50 + 0.1 * n_live_tup`, which on an
-- append-mostly outbox grows far beyond the churn of a single burst, freezing
-- statistics at "queue is empty" for precisely the window where the good plan
-- matters.
--
-- WHAT: per-table autovacuum settings that decouple the trigger from table SIZE
-- and tie it to ABSOLUTE churn (scale_factor = 0 + fixed threshold), the standard
-- treatment for a queue table:
--
--   * analyze every ~1000 modifications — bounds statistics staleness to a few
--     seconds of burst instead of an entire run. ANALYZE samples a fixed 30 000
--     rows (default_statistics_target 100) and costs tens of ms.
--   * vacuum every ~1000 dead tuples — the drainer UPDATEs every row once to set
--     sent_at, so each drained row leaves a dead tuple. Reclaiming them promptly
--     keeps the partial claim indexes (`WHERE sent_at IS NULL`) tracking the live
--     backlog rather than accumulating dead entries the claim must skip past.
--
-- Catalog-only change: brief lock, rewrites nothing, does not alter query
-- semantics — only how often the statistics behind the plan are refreshed.
ALTER TABLE kacho_storage.fga_register_outbox SET (
    autovacuum_analyze_scale_factor = 0.0,
    autovacuum_analyze_threshold    = 1000,
    autovacuum_vacuum_scale_factor  = 0.0,
    autovacuum_vacuum_threshold     = 1000
);

-- Seed the statistics immediately: without this the first burst after deploy still
-- runs on whatever stale snapshot the table carried, since autovacuum only reacts
-- to churn that happens AFTER this migration.
ANALYZE kacho_storage.fga_register_outbox;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_storage.fga_register_outbox RESET (
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold,
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold
);

-- +goose StatementEnd
