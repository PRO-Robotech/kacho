-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up

-- =============================================================================
-- Drop the listener address model — a schema with no live code behind it
-- =============================================================================
-- The VIP is anchored on the LoadBalancer, one per IP family
-- (`load_balancers.address_v4/_v6` + `address_id_v4/_v6` + `vip_origin_v4/_v6`),
-- allocated by the LoadBalancer.Create saga and released by LoadBalancer.Delete /
-- create-compensation / free_ip_runner (which scans ONLY load_balancers). A Listener
-- is a (port, protocol) on that VIP and allocates nothing.
--
-- These five columns are the last remnant of the pre-consolidation, listener-anchored
-- VIP. They are dead by construction, not merely unused:
--
--   * the public contract does not carry them — `listener.proto` reserves
--     `ip_version=12, address_id=13, allocated_address=14, subnet_id=15`, and the
--     Listener message/update_mask expose no address field at all;
--   * `Listener.Create` is a plain INSERT of a domain.Listener the use-case never
--     populates with an address (`domain.NewListener` takes no address argument);
--   * the only writers of `address_id`/`allocated_address` — `SetVIP` and
--     `SetAllocatedAddress` — have ZERO production callers (their sole callers are
--     unit-test fakes and one repo coverage test);
--   * therefore `listeners.address_id` is '' on every row the service can produce,
--     which in turn made (a) the `Listener.Delete` release-VIP branch
--     (`if address_id <> '' → FreeIP/ClearReference`) unreachable, (b) the
--     `vip_origin` boot-backfill a permanent no-op (it selects
--     `WHERE address_id <> ''`), and (c) `listeners_address_idx`
--     (partial `WHERE address_id <> ''`) an index over the empty set.
--
-- Keeping them is not free: they document an address-owning Listener that the
-- service does not implement, so the next contributor "restores" listener-level VIP
-- allocation to make them meaningful (architecture.md doc-truthfulness + LEAN).
-- Migration 0025 dropped the matching dead UNIQUE and explicitly deferred these
-- columns because removing them required the code change that lands with this
-- migration; that change is now made (release branch, address client port,
-- vip_origin backfill job and readiness gate all removed).
--
-- ban #5 (never edit an applied migration): 0001/0005/0009/0025 are untouched; this
-- is a new forward migration.
--
-- Locking: DROP COLUMN is a catalog-only rewrite-free operation, but it takes
-- ACCESS EXCLUSIVE on `listeners` for its duration (sub-millisecond at this table
-- size). Dependent objects (`listeners_vip_origin_check`, `listeners_ip_version_check`,
-- `listeners_address_idx`) are dropped explicitly first so the intent is visible in
-- the migration rather than happening as an implicit cascade.

DROP INDEX IF EXISTS kacho_nlb.listeners_address_idx;

ALTER TABLE kacho_nlb.listeners
    DROP CONSTRAINT IF EXISTS listeners_vip_origin_check,
    DROP CONSTRAINT IF EXISTS listeners_ip_version_check;

ALTER TABLE kacho_nlb.listeners
    DROP COLUMN IF EXISTS vip_origin,
    DROP COLUMN IF EXISTS subnet_id,
    DROP COLUMN IF EXISTS allocated_address,
    DROP COLUMN IF EXISTS address_id,
    DROP COLUMN IF EXISTS ip_version;

-- +goose Down

-- Restore the pre-0028 shape (columns + CHECKs + partial index). The data is NOT
-- restorable — it was empty by construction (see above), so re-adding the columns
-- reproduces the exact prior state.
--
-- `ip_version` was `NOT NULL` WITHOUT a default in 0001 (the use-case always sent a
-- vestigial value); on a non-empty table that form cannot be re-added, so the
-- rollback seeds the same value the code used to write ('IPV4') and then drops the
-- default, leaving the original `NOT NULL` + CHECK shape.
ALTER TABLE kacho_nlb.listeners
    ADD COLUMN IF NOT EXISTS ip_version        text NOT NULL DEFAULT 'IPV4',
    ADD COLUMN IF NOT EXISTS address_id        text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS allocated_address text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subnet_id         text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS vip_origin        text NOT NULL DEFAULT 'auto';

ALTER TABLE kacho_nlb.listeners
    ALTER COLUMN ip_version DROP DEFAULT;

ALTER TABLE kacho_nlb.listeners
    ADD CONSTRAINT listeners_ip_version_check
        CHECK (ip_version IN ('IPV4', 'IPV6')),
    ADD CONSTRAINT listeners_vip_origin_check
        CHECK (vip_origin IN ('byo', 'auto'));

CREATE INDEX IF NOT EXISTS listeners_address_idx
    ON kacho_nlb.listeners (address_id) WHERE address_id <> '';
