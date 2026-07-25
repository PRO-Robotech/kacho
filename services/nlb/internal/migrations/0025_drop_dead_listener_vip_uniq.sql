-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Drop `listeners_region_vip_uniq` — a UNIQUE invariant with no live model behind it
-- =============================================================================
-- The VIP is anchored on the LoadBalancer, not on the Listener. A LoadBalancer holds
-- one VIP per IP family (`load_balancers.address_v4/_v6` + `address_id_v4/_v6`),
-- allocated by the LoadBalancer.Create saga and released by LoadBalancer.Delete /
-- create-compensation / free_ip_runner. A Listener is a (port, protocol) on that VIP
-- and allocates nothing.
--
-- `listeners_region_vip_uniq (region_id, allocated_address, port, protocol)
--  WHERE status <> 'DELETING' AND allocated_address <> ''` was created by baseline
-- 0001 for the original listener-anchored VIP, dropped by 0009 when the VIP moved to
-- the LoadBalancer, and re-created by 0021 for a listener-anchored VIP that the
-- service does not implement: `Listener.Create` is a plain INSERT that leaves
-- `allocated_address` empty, and `SetAllocatedAddress`/`SetVIP` — the only writers of
-- that column — have no production callers. The partial predicate
-- `allocated_address <> ''` therefore matches no row the service can ever produce:
-- the index enforces nothing, and it documents an invariant that contradicts the
-- implemented model (a contributor reading it would "restore" listener-level VIP
-- allocation to make it meaningful). architecture.md doc-truthfulness + LEAN.
--
-- What still enforces VIP uniqueness (unchanged, LoadBalancer-anchored):
--   * `load_balancers_region_v4_uniq` / `_v6_uniq` (0009, validity-healed by 0012) —
--     one anycast IP per (region, family) across load balancers, 23505 → generic
--     FAILED_PRECONDITION on CAS-attach;
--   * single-VIP-per-LB — row cardinality + the `AttachVIP` CAS
--     (`WHERE id = $1 AND (address_v4 = '' OR address_v4 = $2)`).
-- What a Listener still guarantees: `listeners_lb_port_proto_uniq`
-- (load_balancer_id, port, protocol) — since the LB owns exactly one VIP per family,
-- that transitively yields one (VIP, port, protocol) binding.
--
-- ban #5 (never edit an applied migration): 0001/0009/0021 are untouched; this is a
-- new forward migration. The listener address columns (`address_id`,
-- `allocated_address`, `subnet_id`, `ip_version`, `vip_origin`) are NOT dropped here:
-- they are still SELECTed by `listenerCols` and read by the Listener.Delete
-- legacy-release branch, so removing them is a code change, not a schema change.
--
-- CONCURRENTLY + NO TRANSACTION: drop without taking ACCESS EXCLUSIVE on a live
-- table. All objects are schema-qualified — `search_path` is unreliable under NO
-- TRANSACTION (statements may run on different pool connections).

DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.listeners_region_vip_uniq;

-- +goose Down

-- Restore the 0021 state. A crashed CONCURRENTLY build leaves an INVALID index that
-- enforces nothing while `IF NOT EXISTS` refuses to rebuild it — so drop any INVALID
-- remnant first, then rebuild, then assert validity (mirrors 0012/0021).
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

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS listeners_region_vip_uniq
    ON kacho_nlb.listeners (region_id, allocated_address, port, protocol)
    WHERE status <> 'DELETING' AND allocated_address <> '';

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
        RAISE EXCEPTION 'listeners_region_vip_uniq missing or INVALID after rebuild';
    END IF;
END
$$;
-- +goose StatementEnd
