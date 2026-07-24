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
-- UPGRADE-SAFE ввод NOT NULL: ADD COLUMN nullable → backfill → SET NOT NULL (эталон —
-- vpc 0007_network_vrf_id). Прямой `ADD COLUMN region_id TEXT NOT NULL` без DEFAULT
-- падает (23502 «contains null values») на ЛЮБОМ стенде с уже созданными реестрами, а
-- починить это последующей миграцией НЕЛЬЗЯ: goose обрывается ровно здесь и 0007+ не
-- выполняются — единственный возможный фикс живёт в этом файле. DEFAULT-путь тут не
-- годится: placement-anchor CHECK требует region_id <> '', т.е. backfill обязан дать
-- РЕАЛЬНЫЙ регион, а не пустую строку.
--
-- Значение backfill'а: реестры, созданные ДО F4, физически лежат в единственном
-- регионе своего стенда. По умолчанию — baseline-регион платформы ('ru-central1',
-- тот же, что в geo-baseline). Мультирегиональный оператор задаёт свой:
--   PGOPTIONS / DSN options: -c kacho.registry_backfill_region=<regionId>
--   либо ALTER DATABASE <db> SET kacho.registry_backfill_region = '<regionId>';
-- Свежий (пустой) каталог: UPDATE трогает 0 строк — путь идентичен прежнему.
--
-- placement_type — DEFAULT 'REGIONAL' (единственное значение) + CHECK; DEFAULT
-- backfill'ит существующие строки by construction.
SET search_path TO kacho_registry, public;

ALTER TABLE registries ADD COLUMN region_id TEXT;

UPDATE registries
   SET region_id = COALESCE(NULLIF(current_setting('kacho.registry_backfill_region', true), ''), 'ru-central1')
 WHERE region_id IS NULL OR region_id = '';

ALTER TABLE registries ALTER COLUMN region_id SET NOT NULL;

ALTER TABLE registries ADD COLUMN placement_type TEXT NOT NULL DEFAULT 'REGIONAL';

ALTER TABLE registries
  ADD CONSTRAINT registries_placement_anchor_check
    CHECK (placement_type = 'REGIONAL' AND region_id <> '');

-- +goose Down
SET search_path TO kacho_registry, public;
ALTER TABLE registries DROP CONSTRAINT IF EXISTS registries_placement_anchor_check;
ALTER TABLE registries DROP COLUMN IF EXISTS placement_type;
ALTER TABLE registries DROP COLUMN IF EXISTS region_id;
