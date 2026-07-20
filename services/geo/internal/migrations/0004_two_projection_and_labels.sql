-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- GEO-1 redesign: two-projection (raw status + infra° columns for Internal
-- projection), fresh fail-safe DOWN, and required global-UNIQUE name label.
--
--  (a) regions gains status (fail-safe DOWN), country_code, region-infra
--      numeric_infra_id (immutable). zones gains its infra° columns.
--  (b) fresh-default fail-safe: zones.status DEFAULT flipped 'UP' → 'DOWN'
--      (regions.status starts 'DOWN'). Admin explicitly opens via Internal Update.
--  (c) name becomes a REQUIRED global-UNIQUE label on both (DROP DEFAULT '' +
--      UNIQUE(name)). Catalog starts empty → backfill safe.
--  (d) tighten zones.status CHECK to ('UP','DOWN') (fresh default is now DOWN and
--      the repo only writes UP/DOWN — STATUS_UNSPECIFIED is no longer persisted).
--
-- Within-service invariants stay at the DB level (ban #10): UNIQUE(name) → 23505,
-- FK RESTRICT zones→regions unchanged.
SET search_path TO kacho_geo, public;

-- (a)+(b) regions: status + descriptor + region-infra.
ALTER TABLE regions
  ADD COLUMN status           TEXT    NOT NULL DEFAULT 'DOWN',
  ADD COLUMN country_code     TEXT    NOT NULL DEFAULT '',
  ADD COLUMN numeric_infra_id BIGINT  NOT NULL DEFAULT 0;
ALTER TABLE regions
  ADD CONSTRAINT regions_status_check CHECK (status IN ('UP','DOWN'));

-- (b) zones: flip fail-safe default UP → DOWN + zone-infra columns.
ALTER TABLE zones ALTER COLUMN status SET DEFAULT 'DOWN';
ALTER TABLE zones
  ADD COLUMN numeric_infra_id     BIGINT  NOT NULL DEFAULT 0,
  ADD COLUMN host_classes         TEXT[]  NOT NULL DEFAULT '{}',
  ADD COLUMN failure_domain_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN underlay_anchor      TEXT    NOT NULL DEFAULT '',
  ADD COLUMN capacity_hint        TEXT    NOT NULL DEFAULT '';

-- (d) tighten zones.status CHECK (was UP/DOWN/STATUS_UNSPECIFIED).
ALTER TABLE zones DROP CONSTRAINT IF EXISTS zones_status_check;
ALTER TABLE zones ADD  CONSTRAINT zones_status_check CHECK (status IN ('UP','DOWN'));

-- (c) name → required global-UNIQUE label (drop the '' default; add UNIQUE).
ALTER TABLE regions ALTER COLUMN name DROP DEFAULT;
ALTER TABLE zones   ALTER COLUMN name DROP DEFAULT;
ALTER TABLE regions ADD CONSTRAINT regions_name_key UNIQUE (name);
ALTER TABLE zones   ADD CONSTRAINT zones_name_key   UNIQUE (name);

-- +goose Down
SET search_path TO kacho_geo, public;

ALTER TABLE zones   DROP CONSTRAINT IF EXISTS zones_name_key;
ALTER TABLE regions DROP CONSTRAINT IF EXISTS regions_name_key;
ALTER TABLE zones   ALTER COLUMN name SET DEFAULT '';
ALTER TABLE regions ALTER COLUMN name SET DEFAULT '';

ALTER TABLE zones DROP CONSTRAINT IF EXISTS zones_status_check;
ALTER TABLE zones ADD  CONSTRAINT zones_status_check CHECK (status IN ('UP','DOWN','STATUS_UNSPECIFIED'));

ALTER TABLE zones
  DROP COLUMN IF EXISTS capacity_hint,
  DROP COLUMN IF EXISTS underlay_anchor,
  DROP COLUMN IF EXISTS failure_domain_count,
  DROP COLUMN IF EXISTS host_classes,
  DROP COLUMN IF EXISTS numeric_infra_id;
ALTER TABLE zones ALTER COLUMN status SET DEFAULT 'UP';

ALTER TABLE regions DROP CONSTRAINT IF EXISTS regions_status_check;
ALTER TABLE regions
  DROP COLUMN IF EXISTS numeric_infra_id,
  DROP COLUMN IF EXISTS country_code,
  DROP COLUMN IF EXISTS status;
