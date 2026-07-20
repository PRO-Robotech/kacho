-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Network declared supernet + system default route table id (redesign VPC-1 F2/F3).
-- =============================================================================
-- Network теперь несёт объявленный супернет (ipv4_cidr_blocks / ipv6_cidr_blocks):
-- набор супернет-блоков, из которых нарезаются подсети. В отличие от Subnet у
-- Network НЕТ primary-блока — это чистый набор. Каждый Subnet.ipv4_cidr_primary
-- обязан быть подмножеством одного из блоков (валидация на Subnet.Create против
-- этой строки в той же БД — within-service).
--
-- default_route_table_id — единственный источник истины «какой RT дефолтный для
-- сети»; заполняется системой при Network.Create и авто-ассоциируется к новым
-- подсетям (заменяет прежний trigger-выбор «самый ранний RT»).
--
-- Все три колонки — additive, NOT NULL DEFAULT (пустой набор / пустая строка),
-- поэтому существующие строки валидны без backfill. Идемпотентно (IF NOT EXISTS)
-- на случай повторного/параллельного migrate-init (helm rollout).

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.networks
    ADD COLUMN IF NOT EXISTS ipv4_cidr_blocks       text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN IF NOT EXISTS ipv6_cidr_blocks       text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN IF NOT EXISTS default_route_table_id text   NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.networks
    DROP COLUMN IF EXISTS default_route_table_id,
    DROP COLUMN IF EXISTS ipv6_cidr_blocks,
    DROP COLUMN IF EXISTS ipv4_cidr_blocks;

-- +goose StatementEnd
