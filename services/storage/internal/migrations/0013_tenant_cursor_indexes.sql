-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- Композитный индекс под КУРСОРНУЮ страницу тенантных списков.
--
-- Запрос всех трёх списков одной формы:
--   WHERE project_id = $1 [AND name = $2] AND (created_at, id) > ($3, $4)
--   ORDER BY created_at ASC, id ASC LIMIT page_size + 1
--
-- Индексов под эту пару не было: только раздельные однополевые (по проекту и по
-- времени создания). Планировщику оставались два плана, и ни один не по странице:
--   * скан по проекту + СОРТИРОВКА всех строк проекта на КАЖДУЮ страницу;
--   * скан по времени создания от позиции курсора + отбрасывание чужих проектов —
--     то есть чтение тем дороже, чем меньше доля проекта в таблице.
-- Полный обход проекта постранично при этом становится квадратичным, тогда как
-- курсорная пагинация продаётся клиенту как ПОСТОЯННАЯ стоимость страницы при
-- размере до 1000.
--
-- Ведущий столбец — project_id (единственное равенство запроса), затем
-- (created_at, id) в порядке сортировки: страница читается как непрерывный отрезок
-- индекса, без сортировки и без отбрасывания чужих строк.
--
-- Что это было упущение, а не решение: в этих же деревьях композит под курсор
-- выставлен везде, где о пути доступа подумали — таблица операций
-- (account_id, created_at, id), каталог размеров машин (created_at, id), все
-- тенантные таблицы балансировщика и реестров.
--
-- Обычный (внутритранзакционный) CREATE INDEX, как у соседних миграций этих
-- таблиц. На созревшем кластере, где кратковременная блокировка записи мешала бы
-- писателям, переключить на CREATE INDEX CONCURRENTLY (нужна директива goose
-- NO TRANSACTION; IF NOT EXISTS оставить).
--
-- Замок — services/storage/internal/repo/pg/cursor_index_integration_test.go:
-- он утверждает ПЛАН и число прочитанных строк, а не наличие строки в каталоге
-- индексов (индекс, который планировщик не выбирает, не стоит ничего).

CREATE INDEX IF NOT EXISTS volumes_project_cursor_idx
    ON kacho_storage.volumes (project_id, created_at, id);

CREATE INDEX IF NOT EXISTS snapshots_project_cursor_idx
    ON kacho_storage.snapshots (project_id, created_at, id);

CREATE INDEX IF NOT EXISTS images_project_cursor_idx
    ON kacho_storage.images (project_id, created_at, id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kacho_storage.volumes_project_cursor_idx;
DROP INDEX IF EXISTS kacho_storage.snapshots_project_cursor_idx;
DROP INDEX IF EXISTS kacho_storage.images_project_cursor_idx;

-- +goose StatementEnd
