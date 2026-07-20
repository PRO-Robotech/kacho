-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- REG-1 F3/F4 — Namespace placement (region_id/placement_type) + two-identity
-- (global_slug). Fresh-catalog reshape: каталог стартует пустым в этой breaking-фазе
-- (нет id-preserving reg→ns backfill), поэтому NOT NULL region_id/global_slug
-- вводятся БЕЗ DEFAULT-backfill (пустая таблица безопасна). Within-service инварианты
-- — на DB-уровне (ban #10): UNIQUE(global_slug) глобальный + CHECK placement_type.
SET search_path TO kacho_registry, public;

-- region_id — REGIONAL placement-якорь; cross-domain ref на geo.Region (TEXT, без FK:
-- DB-per-service). Обязателен (peer-validate geo на Create). Immutable после Create.
ALTER TABLE registries ADD COLUMN region_id TEXT NOT NULL;

-- placement_type — always-REGIONAL const (spine placement-discriminator parity).
-- DEFAULT 'REGIONAL' корректен (единственное значение) + CHECK замыкает домен.
ALTER TABLE registries ADD COLUMN placement_type TEXT NOT NULL DEFAULT 'REGIONAL';
ALTER TABLE registries
  ADD CONSTRAINT registries_placement_type_check CHECK (placement_type = 'REGIONAL');

-- global_slug — derived глобально-уникальный slug (первый сегмент pull-пути).
-- partial UNIQUE(global_slug) WHERE status<>'DELETING' — ГЛОБАЛЬНЫЙ (не project-scoped)
-- арбитр bare-global collision (REG-1-12 concurrent-race). partial-live форма (parity с
-- registries_project_name_live_uq): slug, освобождённый переходом реестра в DELETING,
-- немедленно доступен для re-Create того же (project,name)→того же derived-slug (REG-1-37).
-- NOT NULL (сервер всегда деривит непустой).
ALTER TABLE registries ADD COLUMN global_slug TEXT NOT NULL;
CREATE UNIQUE INDEX registries_global_slug_live_uq
  ON registries (global_slug) WHERE status <> 'DELETING';

-- +goose Down
SET search_path TO kacho_registry, public;
DROP INDEX IF EXISTS registries_global_slug_live_uq;
ALTER TABLE registries DROP COLUMN IF EXISTS global_slug;
ALTER TABLE registries DROP CONSTRAINT IF EXISTS registries_placement_type_check;
ALTER TABLE registries DROP COLUMN IF EXISTS placement_type;
ALTER TABLE registries DROP COLUMN IF EXISTS region_id;
