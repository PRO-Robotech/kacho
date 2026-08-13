-- +goose Up
-- Предикат уникальности имени объекта у бэкенда исключает НЕПРИСВОЕННОЕ имя.
--
-- 0017 объявил индексы с `WHERE backend_object IS NOT NULL` на колонках, которые
-- NOT NULL DEFAULT ''. Предикат тождественно истинен, поэтому пустая строка —
-- законное состояние «имя ещё не присвоено» — становилась уникальной на всю
-- таблицу: ВТОРОЙ ресурс без присвоенного имени падал уникальностью. Форма
-- проверки была, содержания не было, и цена — не косметическая: развёртывание
-- без префикса установки не смогло бы завести больше одного тома.
DROP INDEX IF EXISTS kacho_storage.volumes_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.snapshots_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.images_backend_object_uniq;

CREATE UNIQUE INDEX volumes_backend_object_uniq
    ON kacho_storage.volumes (backend_object) WHERE backend_object <> '';
CREATE UNIQUE INDEX snapshots_backend_object_uniq
    ON kacho_storage.snapshots (backend_object) WHERE backend_object <> '';
CREATE UNIQUE INDEX images_backend_object_uniq
    ON kacho_storage.images (backend_object) WHERE backend_object <> '';

-- +goose Down
DROP INDEX IF EXISTS kacho_storage.volumes_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.snapshots_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.images_backend_object_uniq;
