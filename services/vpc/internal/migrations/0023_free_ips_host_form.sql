-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Ключ книги учёта свободных адресов — только host-форма (адрес без маски).
-- =============================================================================
-- В address_pool_free_ips один и тот же адрес мог лежать в ДВУХ разных ключах:
-- материализация диапазона писала `(network(cidr) + 1)::inet`, то есть адрес с
-- маской диапазона (198.51.100.1/28), а возврат при удалении — host-форму
-- (198.51.100.1/32). Тип inet считает их разными значениями, поэтому:
--   * первичный ключ (pool_id, ip) их НЕ схлопывает — один адрес мог оказаться
--     в свободном списке дважды, и вторая выдача упиралась бы в глобальную
--     уникальность внешнего адреса;
--   * любой предикат вида `ip = <адрес>::inet` (точечное занятие адреса,
--     заданного вызывающим) не находил бы строку и отвергал свободный адрес.
--
-- Приводим существующие строки к host-форме (сначала снимая те, у которых
-- host-форма уже есть — иначе UPDATE упёрся бы в первичный ключ) и закрепляем
-- инвариант проверкой, чтобы форма ключа не могла разъехаться снова.

SET search_path TO kacho_vpc, public;

DELETE FROM kacho_vpc.address_pool_free_ips f
 WHERE f.ip <> host(f.ip)::inet
   AND EXISTS (
        SELECT 1 FROM kacho_vpc.address_pool_free_ips g
         WHERE g.pool_id = f.pool_id
           AND g.ip = host(f.ip)::inet);

UPDATE kacho_vpc.address_pool_free_ips
   SET ip = host(ip)::inet
 WHERE ip <> host(ip)::inet;

ALTER TABLE kacho_vpc.address_pool_free_ips
    ADD CONSTRAINT address_pool_free_ips_host_form
    CHECK (ip = host(ip)::inet);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.address_pool_free_ips
    DROP CONSTRAINT IF EXISTS address_pool_free_ips_host_form;

-- +goose StatementEnd
