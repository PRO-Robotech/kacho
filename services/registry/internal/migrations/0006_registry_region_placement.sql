-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- REG-1 F4 — regional placement реестра. Registry получает region_id (REGIONAL
-- placement-якорь) + placement_type (always-REGIONAL const, spine placement-parity).
-- Within-service инварианты — ТОЛЬКО на DB-уровне (ban #10, data-integrity.md):
--   * region_id непуст            → CHECK (region_id <> '') (в placement-CHECK ниже);
--   * placement_type ∈ {REGIONAL} → CHECK (placement_type = 'REGIONAL').
-- Registry — regional-anycast: своей колонки zone_id НЕ несёт (зоне-независим by
-- construction — из зональной coherence-проверки исключён, остаётся региональная,
-- data-integrity.md anycast-исключение). Placement-anchor CHECK замыкает домен:
-- placement_type='REGIONAL' И region_id непуст (нет zone_id → anycast).
--
-- region_id — cross-domain ref на geo.Region (TEXT, БЕЗ FK: DB-per-service). Обязателен
-- на Create (peer-validate geo.v1.RegionService.Get, fail-closed). Immutable после Create
-- (перенос региона сломал бы storage-locality блобов) — энфорс в use-case (update_mask).
--
-- Fresh-catalog: NOT NULL region_id вводится БЕЗ DEFAULT-backfill (пустой каталог
-- безопасен). placement_type — DEFAULT 'REGIONAL' (единственное значение) + CHECK.
SET search_path TO kacho_registry, public;

ALTER TABLE registries ADD COLUMN region_id TEXT NOT NULL;

ALTER TABLE registries ADD COLUMN placement_type TEXT NOT NULL DEFAULT 'REGIONAL';

ALTER TABLE registries
  ADD CONSTRAINT registries_placement_anchor_check
    CHECK (placement_type = 'REGIONAL' AND region_id <> '');

-- +goose Down
SET search_path TO kacho_registry, public;
ALTER TABLE registries DROP CONSTRAINT IF EXISTS registries_placement_anchor_check;
ALTER TABLE registries DROP COLUMN IF EXISTS placement_type;
ALTER TABLE registries DROP COLUMN IF EXISTS region_id;
