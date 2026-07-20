-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up

-- =============================================================================
-- NLB-1b MIGRATE F3 (NLB-1-13/17/18): lb_status_recompute() rewrite — new status
-- glossary ACTIVE/DEGRADED/INACTIVE/DISABLED, driven by admin_state + per-listener
-- targetGroupId RESOLUTION (not the M:N attached_target_groups pivot).
-- =============================================================================
-- AS-IS (0013): ACTIVE ⟺ (has_listener AND has_attached-pivot); preserve set
-- {INACTIVE,ACTIVE}. The redesign (F4) makes the listener's own default_target_group_id
-- the single authoritative wiring (direct FK RESTRICT) — the pivot is no longer the
-- wiring path — and (F3) replaces :start/:stop with admin_state. So status must be
-- recomputed from admin_state + listener-TG-resolution, NOT pivot attachment:
--
--   DISABLED  ⟺ admin_state = 'DISABLED'                       (admin-off, config intact)
--   INACTIVE  ⟺ enabled ∧ no non-DELETING listeners            (config incomplete)
--   DEGRADED  ⟺ enabled ∧ ≥1 non-DELETING listener with EMPTY  (misconfigured — a
--               default_target_group_id                          listener blackholes)
--   ACTIVE    ⟺ enabled ∧ ≥1 non-DELETING listener ∧ EVERY one resolves its TG
--               (default_target_group_id non-empty; FK RESTRICT guarantees the TG row
--                exists — a non-empty ref always resolves)
--
-- admin_state → status feed: a new AFTER UPDATE OF admin_state trigger on
-- load_balancers routes admin_state flips through the same recompute. Recursion-safe:
-- the recompute's write touches ONLY `status` (never admin_state), so the
-- `UPDATE OF admin_state` trigger cannot re-fire; and no trigger watches
-- load_balancers.status, so the status write fans out to nothing.
--
-- CAS-guard preserved and its eligible set EXPANDED to {INACTIVE,ACTIVE,DEGRADED,
-- DISABLED} (recompute must be able to transition OUT of DISABLED/DEGRADED when
-- admin_state or wiring changes). Explicit lifecycle transitions
-- (CREATING/STARTING/STOPPING/STOPPED/DELETING) are still NOT clobbered — the
-- guarded UPDATE `... WHERE id=$ AND status=cur_status` loses to any concurrent
-- explicit SetStatusCAS holding the row lock (project-rule #10, lost-update-safe;
-- sec-hardening r3 finding #2 kept intact).
--
-- CREATE OR REPLACE (not an edit of 0001/0013 — project-rule #5: applied migrations
-- are immutable). The status CHECK from 0001 omits DEGRADED/DISABLED → widened here
-- FIRST (else the recompute's write raises 23514). The legacy Start/Stop path and
-- STARTING/STOPPING/STOPPED statuses stay PRESENT (removed only in CONTRACT); the
-- attached_target_groups pivot triggers stay attached (harmless — recompute no longer
-- reads the pivot).

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT IF EXISTS load_balancers_status_check;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_status_check
        CHECK (status IN ('CREATING','STARTING','ACTIVE','STOPPING','STOPPED',
                          'DELETING','INACTIVE','DEGRADED','DISABLED'));
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_nlb.lb_status_recompute() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    affected_lb_id  text;
    cur_status      text;
    cur_admin_state text;
    cur_project_id  text;
    has_listener    boolean;
    has_unresolved  boolean;
    new_status      text;
    recomputed_rows integer;
BEGIN
    -- Which LB is affected by the current listener/pivot/admin_state operation.
    IF TG_TABLE_NAME = 'listeners' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSIF TG_TABLE_NAME = 'attached_target_groups' THEN
        affected_lb_id := COALESCE(NEW.load_balancer_id, OLD.load_balancer_id);
    ELSIF TG_TABLE_NAME = 'load_balancers' THEN
        affected_lb_id := NEW.id;      -- admin_state update feed
    ELSE
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF affected_lb_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    SELECT status, admin_state, project_id
      INTO cur_status, cur_admin_state, cur_project_id
      FROM kacho_nlb.load_balancers
     WHERE id = affected_lb_id;

    -- LB gone or not found — nothing to recompute.
    IF cur_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    -- Recompute only among the auto-managed statuses; preserve explicit lifecycle
    -- transitions (CREATING/STARTING/STOPPING/STOPPED/DELETING).
    IF cur_status NOT IN ('INACTIVE','ACTIVE','DEGRADED','DISABLED') THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF cur_admin_state = 'DISABLED' THEN
        new_status := 'DISABLED';
    ELSE
        SELECT EXISTS (
            SELECT 1 FROM kacho_nlb.listeners
             WHERE load_balancer_id = affected_lb_id
               AND status <> 'DELETING'
        ) INTO has_listener;

        IF NOT has_listener THEN
            new_status := 'INACTIVE';
        ELSE
            SELECT EXISTS (
                SELECT 1 FROM kacho_nlb.listeners
                 WHERE load_balancer_id = affected_lb_id
                   AND status <> 'DELETING'
                   AND COALESCE(default_target_group_id, '') = ''
            ) INTO has_unresolved;

            IF has_unresolved THEN
                new_status := 'DEGRADED';
            ELSE
                new_status := 'ACTIVE';
            END IF;
        END IF;
    END IF;

    IF new_status <> cur_status THEN
        -- CAS: write only if a concurrent explicit transition did not move status
        -- out from under us between the SELECT and this UPDATE (row-lock serialises
        -- us with SetStatusCAS; EvalPlanQual re-checks status=cur_status → CAS-miss →
        -- 0 rows → STOPPING/DELETING/etc. survive; lost-update stays fixed).
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

-- admin_state → status feed. AFTER UPDATE OF admin_state only (recursion-safe: the
-- recompute writes only status). Fires the shared recompute with TG_TABLE_NAME =
-- 'load_balancers'.
-- +goose StatementBegin
CREATE TRIGGER load_balancers_admin_state_recompute_trg
    AFTER UPDATE OF admin_state ON kacho_nlb.load_balancers
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.lb_status_recompute();
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TRIGGER IF EXISTS load_balancers_admin_state_recompute_trg ON kacho_nlb.load_balancers;
-- +goose StatementEnd

-- Restore the 0013 CAS-guard function body (pivot-driven ACTIVE, preserve {INACTIVE,ACTIVE}).
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
                jsonb_build_object('id', affected_lb_id, 'status', new_status, 'recomputed', true)
            );
        END IF;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    DROP CONSTRAINT IF EXISTS load_balancers_status_check;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE kacho_nlb.load_balancers
    ADD CONSTRAINT load_balancers_status_check
        CHECK (status IN ('CREATING','STARTING','ACTIVE','STOPPING','STOPPED',
                          'DELETING','INACTIVE'));
-- +goose StatementEnd
