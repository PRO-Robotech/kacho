-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Потолки кардинальности наборов, которые задаёт вызывающий (DB-backstop).
-- =============================================================================
-- Три набора приходят из тела запроса, накапливаются между вызовами и
-- переобрабатываются на горячем пути:
--   * security_groups.rules — каждое правило со ссылкой на другую группу
--     требует резолва этой ссылки;
--   * subnets.{v4,v6}_cidr_blocks — набор попарно проверяется на пересечение
--     (квадратично) и перекладывается по строке на диапазон в
--     subnet_cidr_blocks под row-lock подсети и share-lock сети;
--   * network_interfaces.security_group_ids — резолвится при каждом создании и
--     обновлении интерфейса, и участвует в предикате «на меня ссылаются» при
--     удалении группы.
-- Синхронный отказ в use-case ограничивает один запрос; эти проверки —
-- атомарный backstop на накопленный результат (тот же приём, что
-- networks_cidr_blocks_cardinality в миграции 0016).
--
-- Потолки держать в синхроне с domain/types.go:
--   MaxSecurityGroupRules=256, MaxSubnetCidrBlocks=64, MaxNICSecurityGroups=16.

SET search_path TO kacho_vpc, public;

-- Значение колонки бывает и JSON-скаляром `null` (маршалинг пустого набора),
-- поэтому проверка ограничивает длину ТОЛЬКО массива: скаляр — не набор сверх
-- потолка, а отсутствие набора.
-- Идемпотентность — DO-guard по pg_constraint (ALTER TABLE ADD CONSTRAINT не
-- поддерживает IF NOT EXISTS для CHECK), как в 0011/0016: защита от повторного
-- применения и от гонки двух параллельных migrate-init.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'security_groups_rules_cardinality') THEN
        ALTER TABLE kacho_vpc.security_groups
            ADD CONSTRAINT security_groups_rules_cardinality
            CHECK (rules IS NULL
                OR jsonb_typeof(rules) <> 'array'
                OR jsonb_array_length(rules) <= 256);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'subnets_cidr_blocks_cardinality') THEN
        ALTER TABLE kacho_vpc.subnets
            ADD CONSTRAINT subnets_cidr_blocks_cardinality
            CHECK (COALESCE(array_length(v4_cidr_blocks, 1), 0) <= 64
               AND COALESCE(array_length(v6_cidr_blocks, 1), 0) <= 64);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'network_interfaces_sg_cardinality') THEN
        ALTER TABLE kacho_vpc.network_interfaces
            ADD CONSTRAINT network_interfaces_sg_cardinality
            CHECK (security_group_ids IS NULL
                OR jsonb_typeof(security_group_ids) <> 'array'
                OR jsonb_array_length(security_group_ids) <= 16);
    END IF;
END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.network_interfaces
    DROP CONSTRAINT IF EXISTS network_interfaces_sg_cardinality;
ALTER TABLE kacho_vpc.subnets
    DROP CONSTRAINT IF EXISTS subnets_cidr_blocks_cardinality;
ALTER TABLE kacho_vpc.security_groups
    DROP CONSTRAINT IF EXISTS security_groups_rules_cardinality;

-- +goose StatementEnd
