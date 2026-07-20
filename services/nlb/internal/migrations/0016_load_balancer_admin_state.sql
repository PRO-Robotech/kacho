-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- NLB-1b EXPAND (additive): load_balancers.admin_state — desired administrative
-- state (redesign replacement for the :start/:stop power-verbs). LIVE-mutable.
-- =============================================================================
-- Pure ADD COLUMN, NOT NULL DEFAULT 'ENABLED' → instant metadata-only ALTER,
-- every existing row backfilled to ENABLED (was implicitly enabled). CHECK pins
-- the value set (DB-level invariant, data-integrity.md ban #10). In EXPAND this
-- column does not yet drive status recompute (0013 trigger stays untouched — the
-- ENABLED/DISABLED→status wiring lands with the trigger rewrite in NLB-1c). The
-- legacy Start/Stop path and STARTING/STOPPING/STOPPED statuses are NOT removed.
--
-- Applied migrations 0001-0015 untouched (ban #5); NLB-1c evolves admin_state via
-- a further migration (status-trigger rewrite).
-- =============================================================================

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN IF NOT EXISTS admin_state text NOT NULL DEFAULT 'ENABLED';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_admin_state_check
        CHECK (admin_state IN ('ENABLED', 'DISABLED'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
ALTER TABLE kacho_nlb.load_balancers DROP CONSTRAINT IF EXISTS load_balancers_admin_state_check;
ALTER TABLE kacho_nlb.load_balancers DROP COLUMN IF EXISTS admin_state;
SET search_path TO public;
-- +goose StatementEnd
