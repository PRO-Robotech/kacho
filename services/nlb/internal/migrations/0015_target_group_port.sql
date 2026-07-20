-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- NLB-1b F6-co-req: TargetGroup.port — single backend port of the group.
-- =============================================================================
-- Net-new required field (AS-IS: backend port was per-listener `target_port`,
-- dropped in the NLB-1b Listener redesign). Every target of the group receives
-- forwarded traffic on this port; it is the sole backend-port source echoed by
-- Listener.resolved_backend_port. Range 1..65535 pinned by CHECK (DB-level
-- invariant, data-integrity.md ban #10). Set-at-create in NLB-1b; LIVE-mutable
-- re-echo semantics land in NLB-1c.
--
-- No legacy target_group rows exist on the redesign schema, so the transient
-- DEFAULT 0 is dropped immediately (every future INSERT must supply an explicit
-- backend port; domain enforces required). Applied migration — never edited
-- (ban #5); NLB-1c evolves port via a further migration.
-- =============================================================================

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.target_groups
    ADD COLUMN port integer NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.target_groups
    ALTER COLUMN port DROP DEFAULT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.target_groups
    ADD CONSTRAINT target_groups_port_check
        CHECK (port BETWEEN 1 AND 65535);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;
ALTER TABLE kacho_nlb.target_groups DROP CONSTRAINT IF EXISTS target_groups_port_check;
ALTER TABLE kacho_nlb.target_groups DROP COLUMN IF EXISTS port;
SET search_path TO public;
-- +goose StatementEnd
