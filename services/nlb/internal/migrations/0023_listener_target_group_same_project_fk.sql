-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Listener → TargetGroup wiring must be SAME-PROJECT — composite FK (DB-level).
-- =============================================================================
-- AS-IS (0018): the direct FK enforced only EXISTENCE
-- (listeners.default_tg_fk → target_groups(id)), so a listener could reference a
-- TargetGroup of ANOTHER project: the LB would forward live traffic to a victim
-- project's targets, and the victim's TG became undeletable (ON DELETE RESTRICT).
--
-- The owning-project scope is a WITHIN-SERVICE referential invariant (both rows
-- live in kacho_nlb) — per data-integrity.md ban #10 it must be expressed as a DB
-- construction, not a software check-then-act: the use-case precheck runs in the
-- handler thread while the row is INSERTed later by the async worker, so a
-- concurrent TargetGroup.Move committing in that window would otherwise leave a
-- durable cross-project reference. Widening the FK to (default_tg_fk, project_id)
-- closes it atomically.
--
-- Requires a UNIQUE (id, project_id) on the referenced side: `id` alone is the PK,
-- but Postgres needs a unique constraint covering EXACTLY the referenced columns.
-- It is redundant for uniqueness (id is already unique) — it exists solely as the
-- FK target.
--
-- The constraint NAME is preserved (`listeners_target_group_fk`) so the SQLSTATE
-- 23503 → contract-tone mapping in repo/kacho/pg/errors.go keeps matching (a
-- renamed constraint would silently fall through to the generic
-- "<kind> has dependent resources" text).
--
-- MATCH SIMPLE semantics are unchanged for the unwired case: default_tg_fk is the
-- generated NULLIF(default_target_group_id,'') projection, so an empty reference
-- (NULL) leaves the composite FK unchecked.

ALTER TABLE kacho_nlb.target_groups
    ADD CONSTRAINT target_groups_id_project_uniq UNIQUE (id, project_id);

ALTER TABLE kacho_nlb.listeners
    DROP CONSTRAINT IF EXISTS listeners_target_group_fk;

-- NOT VALID — added to a populated table (parity with 0018): existing rows are
-- grandfathered, but existence + same-project + ON DELETE RESTRICT are enforced
-- for every new/modified listener row and every target_groups project change.
ALTER TABLE kacho_nlb.listeners
    ADD CONSTRAINT listeners_target_group_fk
        FOREIGN KEY (default_tg_fk, project_id)
        REFERENCES kacho_nlb.target_groups (id, project_id)
        ON DELETE RESTRICT NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kacho_nlb.listeners
    DROP CONSTRAINT IF EXISTS listeners_target_group_fk;

ALTER TABLE kacho_nlb.listeners
    ADD CONSTRAINT listeners_target_group_fk
        FOREIGN KEY (default_tg_fk)
        REFERENCES kacho_nlb.target_groups (id)
        ON DELETE RESTRICT NOT VALID;

ALTER TABLE kacho_nlb.target_groups
    DROP CONSTRAINT IF EXISTS target_groups_id_project_uniq;

-- +goose StatementEnd
