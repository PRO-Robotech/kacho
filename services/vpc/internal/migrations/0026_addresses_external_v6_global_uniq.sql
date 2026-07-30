-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Внешний IPv6 — глобально уникален, как и внешний IPv4 (within-service
-- инвариант обязан жить на DB-уровне, ban #10).
-- =============================================================================
-- До этой миграции внешний IPv6 покрывал ровно один индекс —
-- addresses_external_v6_pool_ip_uniq, чьё условие требует непустой
-- external_ipv6.address_pool_id. Пул проставляет ТОЛЬКО автоматический
-- аллокатор, поэтому строка с адресом, заданным вызывающим, из индекса
-- выпадала целиком: один и тот же маршрутизируемый адрес мог принадлежать
-- двум ресурсам разных проектов, и ни одно ограничение этого не замечало.
-- Симметричная IPv4-полоса такой индекс несёт с первой миграции
-- (addresses_external_ip_uniq) — то есть платформа сама объявляет внешний
-- адрес глобально уникальным и энфорсила это лишь в одной из двух семей.
--
-- Адрес приводится к канонической форме на входе (use-case Address.Create) и
-- аллокатором (netip.Addr.String()), поэтому текстовое сравнение здесь
-- осмысленно.
--
-- IF NOT EXISTS — идемпотентность при повторном/частичном применении и при
-- гонке двух параллельных migrate-init (helm rollout + rollout restart).

SET search_path TO kacho_vpc, public;

CREATE UNIQUE INDEX IF NOT EXISTS addresses_external_v6_ip_uniq
    ON kacho_vpc.addresses (((external_ipv6 ->> 'address')))
    WHERE external_ipv6 IS NOT NULL
      AND (external_ipv6 ->> 'address') <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

DROP INDEX IF EXISTS kacho_vpc.addresses_external_v6_ip_uniq;

-- +goose StatementEnd
