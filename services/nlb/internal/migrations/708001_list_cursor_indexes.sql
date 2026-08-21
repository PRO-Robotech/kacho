-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose NO TRANSACTION
-- +goose Up

-- =============================================================================
-- Индекс, который выглядел сделанным: направление ключа (задача #708)
-- =============================================================================
-- У трёх несущих таблиц nlb составной индекс СТОЯЛ и был неприменим:
--
--     объявлено (0001_initial):  (project_id, created_at DESC, id)
--     обход      (репозиторий):  ORDER BY created_at ASC, id ASC
--
-- btree читается в обе стороны, поэтому целиком инвертированный индекс порядок
-- отдаёт. Здесь направление СМЕШАННОЕ: прямое чтение даёт `created_at DESC, id
-- ASC`, обратное — `created_at ASC, id DESC`. Ни то, ни другое обходу не
-- подходит, и планировщик достраивает порядок сортировкой всего отобранного
-- набора — то есть страница списка стоит O(число балансировщиков проекта), а не
-- O(размера страницы).
--
-- Это хуже отсутствия индекса: ревизия видит правильный составной индекс с
-- нужными колонками и идёт дальше. Расхождение живёт в одном слове.
--
-- `targets` — четвёртый случай, и он простой: индекса под курсорный обход
-- дочернего списка (`WHERE target_group_id = $1 ORDER BY created_at ASC, id
-- ASC`) не было вовсе, `targets_tg_idx (target_group_id)` порядка не даёт.
--
-- `operations` — пятый: общий список операций (`pkg/operations`), у которого все
-- фильтры необязательны. Три существующих индекса ему не помогают —
-- `operations_project_created_idx` требует равенства по `project_id` (его в
-- запросе нет) и вдобавок несёт то же смешанное направление;
-- `operations_resource_history_idx` ведёт с `resource_type`, которого запрос не
-- фильтрует; `operations_account_id_idx` частичный.
--
-- Три прежних индекса СНИМАЮТСЯ, а не остаются рядом
-- -----------------------------------------------------------------------------
-- Новый индекс `(project_id, created_at, id)` строго перекрывает прежний
-- `(project_id, created_at DESC, id)` во всём, для чего тот применим: равенство
-- по ведущему `project_id` он обслуживает так же, а порядок — в отличие от него —
-- обслуживает правильно. Ни одного чтения с `DESC` у этих таблиц в дереве нет
-- (предикат: `grep -rn DESC --include=*.go services/nlb/internal/repo` без
-- тестов → пусто). Оставить их рядом значило бы держать вес на записи трёх
-- горячих таблиц и вторую, почти неотличимую форму того же индекса — то есть ту
-- самую ловушку, из-за которой расхождение и прожило так долго.
--
-- Индексы операций (`operations_project_created_idx`,
-- `operations_resource_history_idx`) здесь НЕ снимаются: они не перекрываются
-- новым (ведут другими колонками), и вопрос «есть ли у них читатель» — другой
-- предикат, чем предмет этой миграции. Он заведён отдельной задачей.
--
-- CONCURRENTLY: таблицы пишутся на пути запроса; останавливать их запись ради
-- сборки индекса нечем оправдать. Прерванная сборка оставляет INVALID-индекс,
-- который планировщик не использует, а `IF NOT EXISTS` при повторе матчит его по
-- имени и не пересобирает — закрыто пост-условием. Снятие идёт ПОСЛЕ сборки и
-- пост-условия: обратный порядок открыл бы окно, в котором ни старого, ни нового
-- индекса нет.

-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'load_balancers_project_cursor_idx',
        'listeners_project_cursor_idx',
        'target_groups_project_cursor_idx',
        'targets_group_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_nlb' AND c.relname = idx AND NOT i.indisvalid
        ) THEN
            EXECUTE format('DROP INDEX kacho_nlb.%I', idx);
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

CREATE INDEX CONCURRENTLY IF NOT EXISTS load_balancers_project_cursor_idx
    ON kacho_nlb.load_balancers (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS listeners_project_cursor_idx
    ON kacho_nlb.listeners (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS target_groups_project_cursor_idx
    ON kacho_nlb.target_groups (project_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS targets_group_cursor_idx
    ON kacho_nlb.targets (target_group_id, created_at, id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS operations_cursor_idx
    ON kacho_nlb.operations (created_at, id);

-- Пост-условие ДО снятия прежних: пока новый индекс не валиден, старый — всё,
-- что есть у равенства по проекту.
-- +goose StatementBegin
DO $$
DECLARE
    idx text;
BEGIN
    FOREACH idx IN ARRAY ARRAY[
        'load_balancers_project_cursor_idx',
        'listeners_project_cursor_idx',
        'target_groups_project_cursor_idx',
        'targets_group_cursor_idx',
        'operations_cursor_idx'
    ] LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_index i
              JOIN pg_class c ON c.oid = i.indexrelid
              JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'kacho_nlb' AND c.relname = idx AND i.indisvalid
        ) THEN
            RAISE EXCEPTION
                'kacho_nlb.% missing or INVALID after rebuild — cursor pages of this table would sort the whole set on every request', idx;
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.load_balancers_project_created_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.listeners_project_created_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.target_groups_project_created_idx;

-- +goose Down

CREATE INDEX CONCURRENTLY IF NOT EXISTS load_balancers_project_created_idx
    ON kacho_nlb.load_balancers (project_id, created_at DESC, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS listeners_project_created_idx
    ON kacho_nlb.listeners (project_id, created_at DESC, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS target_groups_project_created_idx
    ON kacho_nlb.target_groups (project_id, created_at DESC, id);

DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.load_balancers_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.listeners_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.target_groups_project_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.targets_group_cursor_idx;
DROP INDEX CONCURRENTLY IF EXISTS kacho_nlb.operations_cursor_idx;
