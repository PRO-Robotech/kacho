-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Общий список операций получает порядок из индекса (задача #708)
-- =============================================================================
-- `OperationService.List` — курсорное чтение по (created_at, id):
--
--     SELECT … FROM public.operations [WHERE …]
--     ORDER BY created_at ASC, id ASC LIMIT <размер+1>
--
-- Запрос строит общий `pkg/operations` (`pgRepo.listWithOwner`), и ВСЕ его
-- фильтры необязательны: `resource_id`, `account_id`, предикат владельца. Значит
-- ведущего равенства у обхода нет, и порядок обязан приходить из индекса по
-- самим ключам курсора.
--
-- Что было. `operations_created_at_idx (created_at)` из `0001_initial` несёт
-- только первый ключ: ничьи не разрешает, поэтому предикат продолжения
-- `(created_at, id) > ($1, $2)` диапазоном по индексу не выражается, а порядок
-- достраивается сортировкой. `operations_account_id_idx (account_id, created_at,
-- id) WHERE account_id IS NOT NULL` (миграция 0012_operations_account_id) обслуживает ТОЛЬКО
-- account-scoped ветку — он частичный и требует равенства по account_id.
--
-- Почему это заметили последним. Замер, с которого началась #708, искал в коде
-- `FROM <имя таблицы>`. Здесь имя ВЫЧИСЛЯЕТСЯ (`pgRepo.tableName()` →
-- `<схема>.operations`), в тексте запроса стоит глагол форматирования — и самый
-- нагруженный список продукта оказался невидим предикату, во всех семи сервисах
-- сразу.
--
-- CONCURRENTLY: таблица операций пишется на КАЖДУЮ мутацию, останавливать её
-- запись ради сборки индекса нечем оправдать. Цена решения — прерванная сборка
-- оставляет INVALID-индекс, который планировщик не использует, а `IF NOT EXISTS`
-- при повторе матчит его по имени и не пересобирает; закрыто пост-условием.

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_index i
          JOIN pg_class c ON c.oid = i.indexrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = 'public' AND c.relname = 'operations_cursor_idx' AND NOT i.indisvalid
    ) THEN
        DROP INDEX public.operations_cursor_idx;
    END IF;
END
$$;
-- +goose StatementEnd

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_cursor_idx
    ON public.operations (created_at, id);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index i
          JOIN pg_class c ON c.oid = i.indexrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = 'public' AND c.relname = 'operations_cursor_idx' AND i.indisvalid
    ) THEN
        RAISE EXCEPTION
            'public.operations_cursor_idx missing or INVALID after rebuild — every operation page would sort the whole table';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

DROP INDEX CONCURRENTLY IF EXISTS public.operations_cursor_idx;
