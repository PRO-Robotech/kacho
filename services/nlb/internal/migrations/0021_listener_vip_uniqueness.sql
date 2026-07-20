-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- NLB-1b MIGRATE F5 (NLB-1-30/31/55): VIP-anchor relocates LoadBalancer→Listener
-- =============================================================================
-- The authoritative VIP uniqueness invariant returns to the Listener level. Baseline
-- 0001 carried a partial-UNIQUE `listeners_region_vip_uniq (region_id,
-- allocated_address, port, protocol) WHERE status<>'DELETING' AND allocated_address<>''`
-- (GWT-DB-007). Migration 0009 DROPPED it when the tenant-facing VIP was consolidated
-- onto the LoadBalancer (anycast). The redesign (F5) puts the VIP anchor back on the
-- Listener, so the per-region VIP uniqueness must be DB-enforced there again
-- (data-integrity.md ban #10 — within-service invariant is DB-level, not software
-- check-then-act). This backs NLB-1-30 (ALREADY_EXISTS «address already in use»),
-- NLB-1-31 (concurrent-race — exactly one winner) and NLB-1-55 (recycle-on-delete:
-- a DELETING/deleted listener frees its slot; the partial predicate excludes DELETING).
--
-- ban #5 (never edit an applied migration): 0009's DROP is untouched — this is a NEW
-- forward migration that re-creates the index. VIP-on-LB columns/index/saga stay
-- present as a fallback until the CONTRACT pass removes them (out of scope here).
--
-- Production-safety: current listeners all carry allocated_address='' (Create no
-- longer allocates on the Listener while VIP lives on the LB), so the partial
-- predicate `allocated_address <> ''` excludes every existing row — the build scans
-- zero qualifying tuples and cannot fail on legacy duplicates.
--
-- CONCURRENTLY + NO TRANSACTION: build the UNIQUE index without a long write-lock on
-- a live table (a UNIQUE index has no NOT VALID option). A crashed CONCURRENTLY build
-- leaves an INVALID index that does NOT enforce uniqueness while `IF NOT EXISTS`
-- would refuse to rebuild it — so the migration (1) drops any INVALID remnant, (2)
-- (re)builds CONCURRENTLY, (3) asserts validity (mirrors the 0012 self-heal guard for
-- the LB region-uniq indexes). All objects are schema-qualified — search_path is
-- unreliable under NO TRANSACTION (statements may run on different pool connections).

-- (1) Drop an INVALID remnant of a prior interrupted build (VALID index untouched —
--     dropping a VALID one would open a window with no uniqueness).
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_index i
          JOIN pg_class c ON c.oid = i.indexrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = 'kacho_nlb'
           AND c.relname = 'listeners_region_vip_uniq'
           AND NOT i.indisvalid
    ) THEN
        DROP INDEX kacho_nlb.listeners_region_vip_uniq;
    END IF;
END
$$;
-- +goose StatementEnd

-- (2) (Re)build the partial-UNIQUE. Same signature as baseline 0001.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS listeners_region_vip_uniq
    ON kacho_nlb.listeners (region_id, allocated_address, port, protocol)
    WHERE status <> 'DELETING' AND allocated_address <> '';

-- (3) Post-condition: the index MUST exist AND be VALID — an interrupted build never
--     records as a successful migration; a re-run self-heals via step (1).
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index i
          JOIN pg_class c ON c.oid = i.indexrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = 'kacho_nlb'
           AND c.relname = 'listeners_region_vip_uniq'
           AND i.indisvalid
    ) THEN
        RAISE EXCEPTION 'listeners_region_vip_uniq missing or INVALID after rebuild — per-region listener VIP uniqueness is NOT enforced';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

DROP INDEX IF EXISTS kacho_nlb.listeners_region_vip_uniq;
