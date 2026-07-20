-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- NLB-1b MIGRATE (F4 / grind-note #3): Listener.targetGroupId authoritative + direct
-- FK RESTRICT to TargetGroup — replaces the pivot-composite FK.
-- =============================================================================
-- AS-IS (0004): listeners.default_target_group_id carried a COMPOSITE FK to the M:N
-- pivot attached_target_groups(load_balancer_id, target_group_id) — the listener's
-- wired TG had to be ATTACHED to the LB first. MIGRATE switches the wiring AUTHORITY:
-- a listener wires to a TargetGroup DIRECTLY (single authoritative targetGroupId),
-- no pivot attachment required. The pivot table + Attach/Detach remain PRESENT
-- (removed only in CONTRACT / NLB-1c) but are no longer the wiring path.
--
-- Reuse the existing generated projection default_tg_fk = NULLIF(default_target_group_id,'')
-- (added in 0004): an empty reference (NULL) skips the FK; a set reference enforces
-- existence + ON DELETE RESTRICT against target_groups(id). RESTRICT: deleting a
-- TargetGroup referenced by any listener is blocked at the DB level (23503 →
-- FailedPrecondition, fixed contract tone in mapPgErr — no pgx leak).

ALTER TABLE kacho_nlb.listeners
    DROP CONSTRAINT IF EXISTS listeners_default_tg_attached_fk;

-- NOT VALID — added to a populated table: existing listener rows are grandfathered
-- (not retro-validated), but existence + ON DELETE RESTRICT is enforced for all
-- new/modified rows and for every TargetGroup delete. On a fresh DB the effect is
-- equivalent to a plain FK.
ALTER TABLE kacho_nlb.listeners
    ADD CONSTRAINT listeners_target_group_fk
        FOREIGN KEY (default_tg_fk)
        REFERENCES kacho_nlb.target_groups (id)
        ON DELETE RESTRICT NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_nlb.listeners
    DROP CONSTRAINT IF EXISTS listeners_target_group_fk;

ALTER TABLE kacho_nlb.listeners
    ADD CONSTRAINT listeners_default_tg_attached_fk
        FOREIGN KEY (load_balancer_id, default_tg_fk)
        REFERENCES kacho_nlb.attached_target_groups (load_balancer_id, target_group_id)
        ON DELETE RESTRICT NOT VALID;

-- +goose StatementEnd
