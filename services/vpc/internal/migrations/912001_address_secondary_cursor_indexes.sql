-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Страницу адресов берут ТРИ чтения, а порядок из индекса получало одно (#912)
-- =============================================================================
-- Задача #708 закрыла предмет НА УРОВНЕ ТАБЛИЦЫ: у `addresses` появился
-- `addresses_project_cursor_idx (project_id, created_at, id)`, и общий список
-- адресов проекта стал стоить страницу, а не проект.
--
-- Но страницу адресов берут ещё два чтения, и обязательное равенство у них
-- ДРУГОЕ — не `project_id`:
--
--   AddressesBySubnet     WHERE internal_subnet_id = $1
--   ListAddressesByPool   WHERE external_ipv4 ->> 'address_pool_id' = $1
--
-- Ни один существующий индекс им порядка не даёт:
--
--   addresses_internal_subnet_idx      (internal_subnet_id)
--   addresses_external_pool_ip_uniq    ((…pool_id), (…address))
--
-- Первый несёт только равенство: планировщик берёт по нему строки подсети и
-- сортирует их целиком, потому что ключей курсора в индексе нет. Второй
-- упорядочен по АДРЕСУ, а курсор идёт по времени создания — то есть его порядок
-- к делу не относится вовсе.
--
-- Следствие одинаково для обоих: страница стоит НЕ страницу, а весь набор под
-- равенством. Пока у подсети десяток адресов, это не видно; на подсети с тысячей
-- — видно сразу, и растёт линейно, а не по странице.
--
-- Ключи курсора идут ПОДРЯД после равенства: тогда обход даёт уже упорядоченное,
-- и сортировка исчезает из плана целиком, а не ускоряется.

-- (1) Снять INVALID-остатки прежних прерванных сборок ЭТИХ индексов.
--     VALID-индекс не трогаем: снятие открыло бы окно без порядка.
-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'addresses_subnet_cursor_idx',
        'addresses_pool_cursor_idx'
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
--
-- CONCURRENTLY по той же причине, что и в #708: обычный `CREATE INDEX` держит
-- SHARE-блокировку и останавливает запись в таблицу на всё время сборки.

CREATE INDEX CONCURRENTLY IF NOT EXISTS addresses_subnet_cursor_idx
    ON kacho_vpc.addresses (internal_subnet_id, created_at, id);

-- Выражение ключа повторяет условие чтения ДОСЛОВНО. Иначе планировщик индекс
-- не выберет: он сопоставляет выражения синтаксически, и `->>` против `->` с
-- приведением — для него разные ключи.
CREATE INDEX CONCURRENTLY IF NOT EXISTS addresses_pool_cursor_idx
    ON kacho_vpc.addresses ((external_ipv4 ->> 'address_pool_id'), created_at, id);

-- +goose Down

DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.addresses_subnet_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_vpc.addresses_pool_cursor_idx;
