-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- Seed каталога disk_types нативными Kachō-tier-slug'ами (ban #2 — собственный
-- нейминг, без чужого облачного). performance_tier — нативный класс диска:
--   block-standard  — базовый durable-том (economy)
--   block-balanced  — сбалансированный durable-том (общего назначения, дефолт)
--   block-fast      — high-IOPS durable-том (low-latency)
--   block-single    — single-replica том (non-redundant, temp/scratch)
--   block-io-max    — максимальный IOPS/throughput-том (premium)
-- zone_ids = [] → тип не ограничен зонами (доступен во всех; scoping — позже).
-- ON CONFLICT DO NOTHING — идемпотентно (повторное применение не ломается).

SET search_path TO kacho_storage, public;

INSERT INTO kacho_storage.disk_types (id, name, description, zone_ids, performance_tier) VALUES
    ('block-standard', 'block-standard', 'Economy durable block volume',            '[]'::jsonb, 'standard'),
    ('block-balanced', 'block-balanced', 'Balanced general-purpose durable volume', '[]'::jsonb, 'balanced'),
    ('block-fast',     'block-fast',     'High-IOPS low-latency durable volume',    '[]'::jsonb, 'fast'),
    ('block-single',   'block-single',   'Single-replica non-redundant volume',     '[]'::jsonb, 'single'),
    ('block-io-max',   'block-io-max',   'Maximum IOPS/throughput premium volume',  '[]'::jsonb, 'io-max')
ON CONFLICT (id) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;
DELETE FROM kacho_storage.disk_types
 WHERE id IN ('block-standard','block-balanced','block-fast','block-single','block-io-max');
-- +goose StatementEnd
