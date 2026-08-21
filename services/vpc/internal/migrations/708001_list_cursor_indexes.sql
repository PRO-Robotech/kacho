-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Страница списка стоит страницу, а не проект (задача #708)
-- =============================================================================
-- Пагинация продукта курсорная: страница берётся
--
--     WHERE project_id = $1 [AND …] AND (created_at, id) > ($k, $k+1)
--     ORDER BY created_at ASC, id ASC
--     LIMIT <размер+1>
--
-- Под такой обход нужен составной индекс, у которого ключи курсора идут ПОДРЯД,
-- в том же порядке и в том же направлении. У восьми несущих таблиц vpc и у
-- таблицы операций его не было: `0001_initial` объявляет `<t>_project_idx
-- (project_id)` и `<t>_created_at_idx (created_at)` ПОРОЗНЬ, и пару не
-- обслуживает ни один из двух. Либо чтение по проекту с полной сортировкой
-- отобранного, либо чтение по времени с отбрасыванием чужих строк.
--
-- Следствие для цены названо тем, чем оно является: страница перестаёт стоить
-- O(размер страницы) и начинает стоить O(число ресурсов арендатора). На стенде
-- это незаметно и остаётся незаметным ровно до первого крупного арендатора.
--
-- Ведущая колонка — проект
-- -----------------------------------------------------------------------------
-- `project_id` у списков vpc не «один из фильтров», а ТРЕБОВАНИЕ: все восемь
-- use-case'ов списка отвергают пустой проект синхронно
-- (`status.Error(codes.InvalidArgument, "project_id required")`), закрывая
-- перечисление чужих проектов. Значит равенство по нему есть в КАЖДОМ запросе, и
-- ведущей колонкой оно экономит и отбор, и порядок сразу. Форма совпадает с той,
-- что уже выбрали compute (`instances_project_cursor_idx`), storage
-- (`volumes_project_cursor_idx`) и registry (`registries_project_cursor_idx`).
--
-- Две таблицы стоят особняком, и обе — осознанно:
--
--   * `address_pools` — админский ресурс (Internal*), колонки проекта у него нет
--     вовсе; фильтры списка (`kind`, `zone_id`) необязательны, поэтому ведущего
--     равенства не существует и индекс несёт только ключи курсора;
--   * `operations` — общий список операций (`pkg/operations`), фильтры которого
--     (`resource_id`, `account_id`, предикат владельца) тоже необязательны.
--     Существующий `operations_account_id_idx (account_id, created_at, id)
--     WHERE account_id IS NOT NULL` обслуживает ТОЛЬКО account-scoped ветку: он
--     частичный и требует равенства по account_id, которого у общей ветки нет.
--
-- CONCURRENTLY: обычный `CREATE INDEX` держит SHARE-блокировку и останавливает
-- запись в таблицу на всё время сборки. Здесь это несущие таблицы арендатора, и
-- останавливать их запись ради индекса нечем оправдать. Цена решения названа
-- ниже и закрыта пост-условием: прерванная сборка оставляет INVALID-индекс,
-- который планировщик не использует, а `IF NOT EXISTS` при повторном прогоне
-- матчит его по имени и НЕ пересобирает — то есть миграция отрапортовала бы
-- успех, не сделав своей работы. Пост-условие роняет прогон, пока хоть один
-- индекс отсутствует или INVALID; повторный прогон само-лечится.
--
-- Имена объектов квалифицируются схемой явно: в NO TRANSACTION `search_path`
-- ненадёжен (та же оговорка стоит в nlb 0012).

-- (1) Снять INVALID-остатки прежних прерванных сборок ЭТИХ индексов.
--     VALID-индекс не трогаем: снятие открыло бы окно без порядка.
-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'subnets_project_cursor_idx',
        'networks_project_cursor_idx',
        'security_groups_project_cursor_idx',
        'route_tables_project_cursor_idx',
        'addresses_project_cursor_idx',
        'gateways_project_cursor_idx',
        'network_interfaces_project_cursor_idx',
        'address_pools_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_vpc' AND c.relname = idx AND NOT i.indisvalid
        ) THEN
            EXECUTE format('DROP INDEX kacho_vpc.%I', idx);
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- (2) Собрать недостающие. IF NOT EXISTS → повторный прогон no-op.

CREATE INDEX CONCURRENTLY IF NOT EXISTS subnets_project_cursor_idx
    ON kacho_vpc.subnets (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS networks_project_cursor_idx
    ON kacho_vpc.networks (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS security_groups_project_cursor_idx
    ON kacho_vpc.security_groups (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS route_tables_project_cursor_idx
    ON kacho_vpc.route_tables (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS addresses_project_cursor_idx
    ON kacho_vpc.addresses (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS gateways_project_cursor_idx
    ON kacho_vpc.gateways (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS network_interfaces_project_cursor_idx
    ON kacho_vpc.network_interfaces (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS address_pools_cursor_idx
    ON kacho_vpc.address_pools (created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_cursor_idx
    ON kacho_vpc.operations (created_at, id);

-- (3) Пост-условие: каждый индекс существует И валиден. Иначе прерванная сборка
--     записалась бы успехом, а страница осталась бы без порядка из индекса.
-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'subnets_project_cursor_idx',
        'networks_project_cursor_idx',
        'security_groups_project_cursor_idx',
        'route_tables_project_cursor_idx',
        'addresses_project_cursor_idx',
        'gateways_project_cursor_idx',
        'network_interfaces_project_cursor_idx',
        'address_pools_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_vpc' AND c.relname = idx AND i.indisvalid
        ) THEN
            RAISE EXCEPTION
                'kacho_vpc.% missing or INVALID after rebuild — cursor pages of this table would sort the whole tenant set on every request', idx;
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down

DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.subnets_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.networks_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.security_groups_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.route_tables_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.addresses_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.gateways_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.network_interfaces_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.address_pools_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.operations_cursor_idx;
