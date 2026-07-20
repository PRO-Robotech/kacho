-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- NLB-1b MIGRATE (F2 / NLB-1-51/52): security_group_ids REVIVAL + INTERNAL CHECK.
-- =============================================================================
-- security_group_ids (added 0008) was dropped by 0011 in the AS-IS redesign draft.
-- MIGRATE REVIVES it as vpc SecurityGroup refs firewalling the LB VIP (frontend
-- access control): cross-service TEXT refs (no FK), same-project existence
-- peer-validated via vpc.SecurityGroupService.Get on the request-path (fail-closed
-- UNAVAILABLE). LIVE-mutable (the set is replaced whole under the row-lock of the
-- single-statement Update). Valid only for INTERNAL placement (SGs are
-- network-scoped) — pinned by the DB CHECK as defense-in-depth behind the use-case
-- INTERNAL-only guard (data-integrity.md ban #10).
--
-- Fresh ADD COLUMN NOT NULL DEFAULT '{}' → existing rows backfilled to empty, so the
-- CHECK validates cleanly (cardinality 0). Applied migrations 0001-0019 untouched
-- (ban #5).

ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN security_group_ids text[] NOT NULL DEFAULT '{}';

ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_sg_internal_check
        CHECK (cardinality(security_group_ids) = 0 OR type = 'INTERNAL');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT IF EXISTS load_balancers_sg_internal_check;

ALTER TABLE kacho_nlb.load_balancers
    DROP COLUMN IF EXISTS security_group_ids;

-- +goose StatementEnd
