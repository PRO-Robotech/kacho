-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up

-- =============================================================================
-- NLB CONTRACT: drop the attached_target_groups M:N pivot + the transitional
-- STARTING/STOPPING/STOPPED statuses (with the :start/:stop / Attach/Detach RPCs).
-- =============================================================================
-- A target group's association with a load balancer is now DERIVED from its
-- listeners' wiring (listeners.default_target_group_id, direct FK RESTRICT since
-- 0018). The pivot table + its status-recompute triggers are removed; the
-- recompute function is rewritten to base LB.status on wired listeners.
--
-- New status model: administrative enable/disable lives in load_balancers.admin_state
-- (ENABLED|DISABLED); the LB.status column keeps only CREATING / ACTIVE / INACTIVE /
-- DELETING. STARTING/STOPPING/STOPPED are removed from the CHECK.
--
-- CREATE OR REPLACE of lb_status_recompute (not an edit of 0001/0013 — project-rule
-- #5: applied migrations are immutable).

-- 1. Drop the pivot's status-recompute triggers (they fire on a table about to be
--    dropped).
DROP TRIGGER IF EXISTS attached_tg_lb_status_recompute_ins_trg ON kacho_nlb.attached_target_groups;
DROP TRIGGER IF EXISTS attached_tg_lb_status_recompute_del_trg ON kacho_nlb.attached_target_groups;

-- 2. Rewrite the recompute function: a LB is ACTIVE when it has >=1 non-DELETING
--    listener wired to a target group (default_target_group_id set), else INACTIVE.
--    Fires only from `listeners` now (pivot gone). Keeps the 0013 CAS-guard on the
--    final write (WHERE status = cur_status) so explicit transitions (STOPPING is
--    gone; DELETING/CREATING) are never clobbered, and the outbox event fires only
--    when the recompute actually applied.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_nlb.lb_status_recompute() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    affected_lb_id  text;
    cur_status      text;
    cur_project_id  text;
    has_wired       boolean;
    new_status      text;
    recomputed_rows integer;
BEGIN
    -- The pivot is gone; only listener changes drive the recompute.
    IF TG_TABLE_NAME = 'listeners' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSE
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF affected_lb_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT status, project_id INTO cur_status, cur_project_id
      FROM kacho_nlb.load_balancers
     WHERE id = affected_lb_id;

    -- LB deleted or absent — nothing to recompute.
    IF cur_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- Preserve explicit transitions (CREATING / DELETING).
    IF cur_status NOT IN ('INACTIVE','ACTIVE') THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- ACTIVE iff at least one non-DELETING listener is wired to a target group.
    SELECT EXISTS (
        SELECT 1 FROM kacho_nlb.listeners
         WHERE load_balancer_id = affected_lb_id
           AND status <> 'DELETING'
           AND default_target_group_id <> ''
    ) INTO has_wired;

    IF has_wired THEN
        new_status := 'ACTIVE';
    ELSE
        new_status := 'INACTIVE';
    END IF;

    IF new_status <> cur_status THEN
        -- CAS: write only if status was not moved out from under us by a
        -- concurrent explicit transition between our SELECT and this UPDATE.
        UPDATE kacho_nlb.load_balancers
           SET status = new_status
         WHERE id = affected_lb_id
           AND status = cur_status;
        GET DIAGNOSTICS recomputed_rows = ROW_COUNT;

        IF recomputed_rows > 0 THEN
            INSERT INTO kacho_nlb.nlb_outbox
                (resource_type, resource_id, project_id, action, payload)
            VALUES (
                'nlb_load_balancer',
                affected_lb_id,
                cur_project_id,
                'UPDATED',
                jsonb_build_object(
                    'id', affected_lb_id,
                    'status', new_status,
                    'recomputed', true
                )
            );
        END IF;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

-- 3. The listener UPDATE trigger must also fire when the wiring
--    (default_target_group_id) changes, not only on status — repointing a listener
--    to/from a target group has to recompute LB.status.
DROP TRIGGER IF EXISTS listeners_lb_status_recompute_upd_trg ON kacho_nlb.listeners;
-- +goose StatementBegin
CREATE TRIGGER listeners_lb_status_recompute_upd_trg
    AFTER UPDATE OF status, default_target_group_id ON kacho_nlb.listeners
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status
          OR OLD.default_target_group_id IS DISTINCT FROM NEW.default_target_group_id)
    EXECUTE FUNCTION kacho_nlb.lb_status_recompute();
-- +goose StatementEnd

-- 4. Drop the M:N pivot table. After 0018 listeners FK directly to target_groups,
--    so there is no inbound FK to block this.
DROP TABLE IF EXISTS kacho_nlb.attached_target_groups;

-- 5. Narrow the LB status CHECK: STARTING/STOPPING/STOPPED removed. Map any residual
--    rows to a safe terminal first (idempotent on a fresh DB) so the constraint swap
--    never fails on populated deployments.
UPDATE kacho_nlb.load_balancers
   SET status = 'INACTIVE'
 WHERE status IN ('STARTING','STOPPING','STOPPED');

ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT load_balancers_status_check;
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_status_check
        CHECK (status IN ('CREATING','ACTIVE','DELETING','INACTIVE'));

-- +goose Down

-- Restore the wider status CHECK.
ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT load_balancers_status_check;
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_status_check
        CHECK (status IN ('CREATING','STARTING','ACTIVE','STOPPING','STOPPED','DELETING','INACTIVE'));

-- Restore the listener UPDATE trigger (status-only).
DROP TRIGGER IF EXISTS listeners_lb_status_recompute_upd_trg ON kacho_nlb.listeners;
-- +goose StatementBegin
CREATE TRIGGER listeners_lb_status_recompute_upd_trg
    AFTER UPDATE OF status ON kacho_nlb.listeners
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION kacho_nlb.lb_status_recompute();
-- +goose StatementEnd

-- Recreate the pivot table (mirrors 0001).
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS kacho_nlb.attached_target_groups (
    load_balancer_id text        NOT NULL
        REFERENCES kacho_nlb.load_balancers (id) ON DELETE RESTRICT,
    target_group_id  text        NOT NULL
        REFERENCES kacho_nlb.target_groups (id) ON DELETE RESTRICT,
    priority         integer     NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (load_balancer_id, target_group_id)
);
-- +goose StatementEnd
CREATE INDEX IF NOT EXISTS attached_target_groups_tg_idx
    ON kacho_nlb.attached_target_groups (target_group_id);

CREATE TRIGGER attached_tg_lb_status_recompute_ins_trg
    AFTER INSERT ON kacho_nlb.attached_target_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.lb_status_recompute();

CREATE TRIGGER attached_tg_lb_status_recompute_del_trg
    AFTER DELETE ON kacho_nlb.attached_target_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.lb_status_recompute();

-- Restore the pivot-aware recompute function (0013 version).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_nlb.lb_status_recompute() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    affected_lb_id  text;
    cur_status      text;
    cur_project_id  text;
    has_listener    boolean;
    has_attached    boolean;
    new_status      text;
    recomputed_rows integer;
BEGIN
    IF TG_TABLE_NAME = 'listeners' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSIF TG_TABLE_NAME = 'attached_target_groups' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSE
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF affected_lb_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT status, project_id INTO cur_status, cur_project_id
      FROM kacho_nlb.load_balancers
     WHERE id = affected_lb_id;

    IF cur_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF cur_status NOT IN ('INACTIVE','ACTIVE') THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT EXISTS (
        SELECT 1 FROM kacho_nlb.listeners
         WHERE load_balancer_id = affected_lb_id
           AND status <> 'DELETING'
    ) INTO has_listener;

    SELECT EXISTS (
        SELECT 1 FROM kacho_nlb.attached_target_groups
         WHERE load_balancer_id = affected_lb_id
    ) INTO has_attached;

    IF has_listener AND has_attached THEN
        new_status := 'ACTIVE';
    ELSE
        new_status := 'INACTIVE';
    END IF;

    IF new_status <> cur_status THEN
        UPDATE kacho_nlb.load_balancers
           SET status = new_status
         WHERE id = affected_lb_id
           AND status = cur_status;
        GET DIAGNOSTICS recomputed_rows = ROW_COUNT;

        IF recomputed_rows > 0 THEN
            INSERT INTO kacho_nlb.nlb_outbox
                (resource_type, resource_id, project_id, action, payload)
            VALUES (
                'nlb_load_balancer',
                affected_lb_id,
                cur_project_id,
                'UPDATED',
                jsonb_build_object(
                    'id', affected_lb_id,
                    'status', new_status,
                    'recomputed', true
                )
            );
        END IF;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd
