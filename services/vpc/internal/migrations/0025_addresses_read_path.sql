-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Чтения по адресам стоят столько, сколько отдают, а не сколько лежит в таблице.
-- =============================================================================
-- 1) Страница адресов подсети читалась через дизъюнкцию по jsonb-полям, которую
--    не покрывает ни один индекс, — при том что рядом с первой миграции лежит
--    generated-колонка internal_subnet_id и индекс по ней. Запрос переписан на
--    неё; чтобы колонка была авторитетной, инвариант «внутренний адрес несёт
--    ровно одну семью» закрепляется проверкой: колонка выбирает v4, а затем v6,
--    поэтому строка с ДВУМЯ внутренними семьями (в разных подсетях) была бы
--    видна только по одной из них. Ни один путь записи такую строку сейчас не
--    производит (ветви спека взаимоисключающи), но опирающийся на это внешний
--    ключ и переписанный запрос обязаны иметь под собой проверку, а не
--    договорённость.
--
-- 2) Поиск адреса по значению внутреннего IPv4 (публичный AddressService) шёл
--    по неиндексированному выражению: существующий уникальный индекс ведёт
--    подсетью, поэтому фильтр по одному адресу его не задействует.

SET search_path TO kacho_vpc, public;

-- Идемпотентность — DO-guard по pg_constraint (как 0011/0016).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'addresses_single_internal_family') THEN
        ALTER TABLE kacho_vpc.addresses
            ADD CONSTRAINT addresses_single_internal_family
            CHECK (NOT (
                internal_ipv4 IS NOT NULL AND COALESCE(internal_ipv4 ->> 'subnet_id', '') <> ''
            AND internal_ipv6 IS NOT NULL AND COALESCE(internal_ipv6 ->> 'subnet_id', '') <> ''
            ));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS addresses_internal_v4_address_idx
    ON kacho_vpc.addresses (((internal_ipv4 ->> 'address')))
    WHERE internal_ipv4 IS NOT NULL
      AND (internal_ipv4 ->> 'address') <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

DROP INDEX IF EXISTS kacho_vpc.addresses_internal_v4_address_idx;
ALTER TABLE kacho_vpc.addresses
    DROP CONSTRAINT IF EXISTS addresses_single_internal_family;

-- +goose StatementEnd
