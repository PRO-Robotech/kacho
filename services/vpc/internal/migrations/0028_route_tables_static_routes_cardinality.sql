-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Потолок числа статических маршрутов в таблице маршрутизации (DB-backstop).
-- =============================================================================
-- route_tables.static_routes — набор, который задаёт вызывающий: RouteTable.Create
-- и Update с маской static_routes несут ИТОГОВЫЙ набор целиком (аддитивного
-- глагола у маршрутов нет — AddRoutes/RemoveRoutes/UpdateRoute отказывают по
-- имени, потому что StaticRoute не несёт идентичности). Длина набора
-- оплачивается трижды: синхронным разбором каждой записи, сериализацией набора
-- в JSONB и его полной выдачей в КАЖДОМ Get/List этой таблицы и в payload
-- каждого её события vpc_outbox. Без потолка единственным ограничителем был
-- предел размера одного gRPC-сообщения.
--
-- Синхронный отказ в use-case ограничивает ОДИН запрос, прошедший через
-- use-case; этот CHECK ограничивает саму строку — то есть отвергает превышение у
-- ЛЮБОГО writer'а (тот же приём, что networks_cidr_blocks_cardinality в 0016 и
-- три потолка в 0024). 23514 → InvalidArgument (helpers.WrapPgErr).
--
-- Величина — 256, как у правил группы безопасности: маршрут это единица
-- политики (по записи на пункт назначения), а не элемент адресного плана, где
-- стоит 64. Зеркало в коде — domain.MaxStaticRoutes; значения обязаны совпадать,
-- и это держит проба TestStaticRoutesCapMatchesDBConstraint
-- (services/vpc/internal/repo/route_table_static_routes_cardinality_integration_test.go),
-- которая читает предикат отсюда, а не помнит число.
--
-- Существующие строки тривиально валидны: колонка объявлена
-- `NOT NULL DEFAULT '[]'::jsonb` в базовой схеме, и оба writer'а (Insert/Update
-- в repo/kacho/pg/route_table.go) пишут её через helpers.MarshalStaticRoutes,
-- то есть массивом. Поэтому обычный ADD CONSTRAINT без NOT VALID.
--
-- Ветка jsonb_typeof — та же, что у security_groups_rules_cardinality в 0024:
-- она делает предикат ТОТАЛЬНЫМ по jsonb (jsonb_array_length на скаляре не
-- нарушает ограничение, а падает ошибкой 22023, которая не классифицируется и
-- уехала бы клиенту фиксированным INTERNAL). Формы, кроме массива, у колонки
-- сегодня нет ни одного производителя — ветка про сохранность предиката, а не
-- про допуск скаляра.
--
-- Идемпотентно (DO-guard по pg_constraint: ALTER TABLE ADD CONSTRAINT не
-- поддерживает IF NOT EXISTS для CHECK) — на случай повторного или параллельного
-- migrate-init при helm rollout, как 0016/0024.

SET search_path TO kacho_vpc, public;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'route_tables_static_routes_cardinality'
           AND conrelid = 'kacho_vpc.route_tables'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.route_tables
            ADD CONSTRAINT route_tables_static_routes_cardinality
            CHECK (jsonb_typeof(static_routes) <> 'array'
                OR jsonb_array_length(static_routes) <= 256);
    END IF;
END
$$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

ALTER TABLE kacho_vpc.route_tables
    DROP CONSTRAINT IF EXISTS route_tables_static_routes_cardinality;

-- +goose StatementEnd
