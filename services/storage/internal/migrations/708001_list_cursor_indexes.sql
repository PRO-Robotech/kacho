-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Четыре курсорных списка storage без служащего индекса (задача #708)
-- =============================================================================
-- Тенантские таблицы блочного хранения свои курсорные индексы получили ещё
-- миграцией `0013_tenant_cursor_indexes` (`volumes`/`images`/`snapshots`,
-- форма `(project_id, created_at, id)`). Без индекса остались четыре, и все
-- четыре — не тенантские, поэтому ведущего равенства у них нет вовсе:
--
--   * `disk_types` — глобальный каталог классов диска. Индексов у таблицы не
--     было НИ ОДНОГО, кроме первичного ключа: `DiskTypeRepo.List` читает
--     курсором `(d.created_at, d.id)` без единого обязательного условия;
--   * `disk_type_bindings` — ревизии привязки класса к зоне. Три существующих
--     индекса — уникальные по (класс, зона[, ревизия]) и по внутреннему носителю;
--     ключей курсора нет ни в одном;
--   * `storage_backends` — каталог внутренних носителей, единственный индекс —
--     `UNIQUE (name)`;
--   * `operations` — общий список операций (`pkg/operations`), у которого все
--     фильтры необязательны. `operations_created_at_idx (created_at)` из
--     `0002_operations` несёт только первый ключ курсора, а
--     `operations_account_id_idx (account_id, created_at, id) WHERE account_id IS
--     NOT NULL` оттуда же — частичный и требует равенства по account_id.
--
-- Почему `disk_types` не нашёл предыдущий замер: его `FROM disk_types d` живёт в
-- константе `diskTypeSelect`, а запрос собирается форматированием из неё и
-- условий. Предикат, искавший `FROM <имя>` в ОДНОМ литерале с `ORDER BY`, такое
-- чтение не видит.
--
-- CONCURRENTLY: `operations` пишется на каждую мутацию, каталоги читаются на
-- пути создания тома; останавливать запись ради сборки индекса нечем оправдать.
-- Прерванная сборка оставляет INVALID-индекс, который планировщик не использует,
-- а `IF NOT EXISTS` при повторе матчит его по имени и не пересобирает — закрыто
-- пост-условием.

-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'disk_types_cursor_idx',
        'disk_type_bindings_cursor_idx',
        'storage_backends_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_storage' AND c.relname = idx AND NOT i.indisvalid
        ) THEN
            EXECUTE format('DROP INDEX kacho_storage.%I', idx);
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

CREATE INDEX CONCURRENTLY IF NOT EXISTS disk_types_cursor_idx
    ON kacho_storage.disk_types (created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS disk_type_bindings_cursor_idx
    ON kacho_storage.disk_type_bindings (created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS storage_backends_cursor_idx
    ON kacho_storage.storage_backends (created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_cursor_idx
    ON kacho_storage.operations (created_at, id);

-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'disk_types_cursor_idx',
        'disk_type_bindings_cursor_idx',
        'storage_backends_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_storage' AND c.relname = idx AND i.indisvalid
        ) THEN
            RAISE EXCEPTION
                'kacho_storage.% missing or INVALID after rebuild — cursor pages of this table would sort the whole set on every request', idx;
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down

DROP INDEX CONCURRENTLY IF EXISTS kacho_storage.disk_types_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_storage.disk_type_bindings_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_storage.storage_backends_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_storage.operations_cursor_idx;
