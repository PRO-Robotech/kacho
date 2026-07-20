-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- NLB-1b MIGRATE (F3 / NLB-1-16): cross_zone_enabled REVIVAL + REGIONAL-only guard.
-- =============================================================================
-- cross_zone_enabled was dropped by 0011 (folded into placement in the AS-IS
-- redesign draft). MIGRATE REVIVES it as a real REGIONAL-only cross-zone toggle:
-- a ZONAL LB serves a single zone, so cross-zone is inapplicable there. The
-- Create/Update use-case rejects cross_zone_enabled=true on ZONAL placement
-- synchronously (actionable InvalidArgument, verbatim contract tone); this migration
-- re-adds the column and pins the same invariant at the DB level as defense-in-depth
-- (data-integrity.md ban #10).
--
-- Fresh ADD COLUMN NOT NULL DEFAULT false → every existing row backfilled to false,
-- so the CHECK validates cleanly (NOT false ≡ true). Applied migrations 0001-0018
-- untouched (ban #5).

ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN cross_zone_enabled boolean NOT NULL DEFAULT false;

ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_cross_zone_placement_check
        CHECK (NOT cross_zone_enabled OR placement_type <> 'ZONAL');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT IF EXISTS load_balancers_cross_zone_placement_check;

ALTER TABLE kacho_nlb.load_balancers
    DROP COLUMN IF EXISTS cross_zone_enabled;

-- +goose StatementEnd
