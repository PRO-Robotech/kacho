-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Cardinality cap for the declared network supernet (VPC-1 F2 hardening).
-- =============================================================================
-- ipv4_cidr_blocks / ipv6_cidr_blocks — tenant-управляемые аддитивные наборы:
-- NetworkService.AddCidrBlocks идемпотентно мержит и переписывает весь массив,
-- поэтому размер НАКАПЛИВАЕТСЯ между вызовами, а единственным ограничителем был
-- 4MB-лимит одного gRPC-сообщения. Набор при этом парсится заново на КАЖДОМ
-- Subnet.Create / Subnet.AddCidrBlocks (containment-проверка F7) и целиком
-- сериализуется в каждом Network.Get/List — то есть рост бьёт по горячему пути
-- и по всем тенантам инстанса.
--
-- Потолок держится на DB-уровне (data-integrity §within-service: простой
-- предикат → CHECK), а не только software-проверкой в use-case: CHECK — атомарный
-- backstop для любого writer'а. 23514 → InvalidArgument (helpers.WrapPgErr).
-- Зеркало в коде — domain.MaxNetworkCidrBlocks (значения обязаны совпадать).
--
-- Существующие строки тривиально валидны (супернет — net-new колонки 0015),
-- поэтому обычный ADD CONSTRAINT без NOT VALID. Идемпотентно (DO/IF NOT EXISTS)
-- на случай повторного/параллельного migrate-init (helm rollout), как 0015.

SET search_path TO kacho_vpc, public;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'networks_cidr_blocks_cardinality'
           AND conrelid = 'kacho_vpc.networks'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.networks
            ADD CONSTRAINT networks_cidr_blocks_cardinality
            CHECK (cardinality(ipv4_cidr_blocks) <= 64
               AND cardinality(ipv6_cidr_blocks) <= 64);
    END IF;
END
$$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.networks
    DROP CONSTRAINT IF EXISTS networks_cidr_blocks_cardinality;

-- +goose StatementEnd
