-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up

-- =============================================================================
-- NLB-1b MIGRATE F5 (NLB-1-33): ZONAL LoadBalancer VIP zone-coherence anchor
-- =============================================================================
-- A ZONAL load balancer serves a single zone. With the VIP anchor now on the
-- Listener (auto subnet_id → a fresh Address from a ZONAL subnet), every ZONAL
-- listener VIP of the same LB MUST resolve to the SAME zone (data-integrity.md
-- §placement-coherence, NLB-1-33). This is a WITHIN-service invariant across the
-- LB's listener rows → it MUST be enforced at the DB level via a set-once anchor
-- + atomic CAS, NOT a software check-then-act sibling scan (ban #10 — TOCTOU).
--
-- `vip_zone_id` pins the zone of the FIRST auto-VIP-subnet listener. Subsequent
-- listener binds CAS against it: `WHERE vip_zone_id = '' OR vip_zone_id = $zone`.
-- A mismatching second bind gets 0 rows → FAILED_PRECONDITION «load balancer VIP
-- must be in the same zone». '' = unpinned (no ZONAL auto-VIP listener yet, or a
-- REGIONAL/anycast LB — excluded by construction, its subnets carry no zone).
--
-- The CAS runs in the listener INSERT writer-TX right after the child INSERT (which
-- takes FOR NO KEY UPDATE on the LB row), so the LB is guaranteed to exist+be locked
-- → 0 rows unambiguously means zone-mismatch, and two concurrent binds serialise on
-- the row lock (exactly one pins/matches, the other sees the pinned value).
--
-- DEFAULT '' backfills every existing row as unpinned; the column is additive and
-- the transition is instant (no rewrite). Nullability avoided (NOT NULL DEFAULT '')
-- for parity with the other TEXT columns (address_v4/v6, vip_origin_*).

ALTER TABLE kacho_nlb.load_balancers
    ADD COLUMN IF NOT EXISTS vip_zone_id text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE kacho_nlb.load_balancers
    DROP COLUMN IF EXISTS vip_zone_id;
