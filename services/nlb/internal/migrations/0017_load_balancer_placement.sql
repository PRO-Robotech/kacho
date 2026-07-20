-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- NLB-1b EXPAND (additive): load_balancers.placement — merged placement value
-- (redesign fusion of type + placement_type into one discriminator).
-- =============================================================================
-- Pure ADD COLUMN, NOT NULL DEFAULT '' → instant metadata-only ALTER; existing
-- rows keep '' and read derives placement° from the legacy type/placement_type
-- (type2pb compat). NEW rows persist a value derived-consistent with
-- type/placement_type (Create use-case). CHECK pins the value set incl. '' —
-- "EXTERNAL_ZONAL" is inexpressible by construction (not in the set). In EXPAND
-- this column is stored + echoed but NOT authoritative: the legacy
-- type/placement_type still drive behaviour; the authority switch (derive
-- type/placement_type FROM placement + reject legacy inputs) is NLB-1c/MIGRATE.
--
-- Applied migrations 0001-0016 untouched (ban #5).
-- =============================================================================

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN IF NOT EXISTS placement text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_placement_value_check
        CHECK (placement IN ('', 'EXTERNAL_REGIONAL', 'INTERNAL_REGIONAL', 'INTERNAL_ZONAL'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
ALTER TABLE kacho_nlb.load_balancers DROP CONSTRAINT IF EXISTS load_balancers_placement_value_check;
ALTER TABLE kacho_nlb.load_balancers DROP COLUMN IF EXISTS placement;
SET search_path TO public;
-- +goose StatementEnd
