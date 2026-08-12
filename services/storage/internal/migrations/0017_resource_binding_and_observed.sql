-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0017: желаемое отдельно от наблюдённого + политика, закреплённая на ресурсе.
--
-- WHY (две колонки вместо одной). Колонка state была ОДНОЙ на два разных смысла:
-- «что мы намерены иметь» и «что там на самом деле». Пока плоскости данных нет,
-- разницы между ними нет тоже: каждая вставка пишет READY немедленно, а удаление
-- снимает строку. С появлением бэкенда это перестаёт быть верным by construction —
-- появляется состояние «строка закоммичена, объекта нет» и обратное «объект есть,
-- строки нет». Одна колонка делает такое расхождение НЕНАХОДИМЫМ: она отвечает на
-- вопрос о намерении и выдаёт ответ за факт.
--
-- Колонка state НЕ переименовывается, хотя означает именно желаемое. Переименование
-- на живом развёртывании даёт окно, в котором прежние поды читают колонку, которой
-- уже нет, и лечится это парой выпусков, а не одним. Цена честного имени здесь выше
-- цены комментария, поэтому смысл назван комментарием, а не сломанной раскаткой.
--
-- WHY (ссылка на ревизию). Политика фиксируется ССЫЛКОЙ НА НЕИЗМЕНЯЕМУЮ ревизию
-- (0015), а не копией полей: строка ревизии не редактируется, значит джойн к ней не
-- может уехать, и денормализация не нужна. Ограничительная связь держит ревизию
-- живой, пока на неё ссылаются, — история «что обещали этому тому» восстановима.
--
-- WHY (имя объекта у бэкенда). Выводится детерминированно из неизменяемого id
-- ресурса с префиксом установки. Отсюда идемпотентность повтора by construction:
-- повторное создание попадает в тот же объект. Уникальность нужна как страховка от
-- второго развёртывания, нацеленного на тот же кластер: без префикса такие два
-- облака усыновили бы объекты друг друга.
--
-- Рабочий список сверщика — ЧАСТИЧНЫЙ ИНДЕКС по расхождению, а не очередь. Очередь
-- уже заводилась в этом сервисе, не имела потребителя и была снята: она вводится
-- вместе со своим потребителем или не вводится вовсе.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.volumes
    ADD COLUMN binding_id          text REFERENCES kacho_storage.disk_type_bindings(id) ON DELETE RESTRICT,
    ADD COLUMN desired_binding_id  text REFERENCES kacho_storage.disk_type_bindings(id) ON DELETE RESTRICT,
    ADD COLUMN backend_object      text,
    ADD COLUMN backend_namespace   text NOT NULL DEFAULT '',
    ADD COLUMN observed_state      text NOT NULL DEFAULT 'ABSENT',
    ADD COLUMN observed_at         timestamptz,
    ADD COLUMN observed_size_bytes bigint,
    ADD COLUMN used_bytes          bigint,
    ADD COLUMN status_reason       text NOT NULL DEFAULT '',
    ADD CONSTRAINT volumes_observed_state_known
        CHECK (observed_state IN ('ABSENT','READY','ERROR','UNKNOWN')),
    ADD CONSTRAINT volumes_status_reason_known
        CHECK (status_reason IN ('','BACKEND_UNAVAILABLE','BACKEND_REJECTED',
                                 'BACKEND_CAPACITY_EXHAUSTED','SOURCE_NOT_READY',
                                 'PRECONDITION_FAILED','INTERNAL_ERROR'));

ALTER TABLE kacho_storage.snapshots
    ADD COLUMN binding_id     text REFERENCES kacho_storage.disk_type_bindings(id) ON DELETE RESTRICT,
    ADD COLUMN backend_object text,
    ADD COLUMN observed_state text NOT NULL DEFAULT 'ABSENT',
    ADD COLUMN observed_at    timestamptz,
    ADD COLUMN status_reason  text NOT NULL DEFAULT '',
    ADD CONSTRAINT snapshots_observed_state_known
        CHECK (observed_state IN ('ABSENT','READY','ERROR','UNKNOWN')),
    ADD CONSTRAINT snapshots_status_reason_known
        CHECK (status_reason IN ('','BACKEND_UNAVAILABLE','BACKEND_REJECTED',
                                 'BACKEND_CAPACITY_EXHAUSTED','SOURCE_NOT_READY',
                                 'PRECONDITION_FAILED','INTERNAL_ERROR'));

ALTER TABLE kacho_storage.images
    ADD COLUMN binding_id     text REFERENCES kacho_storage.disk_type_bindings(id) ON DELETE RESTRICT,
    ADD COLUMN backend_object text,
    ADD COLUMN observed_state text NOT NULL DEFAULT 'ABSENT',
    ADD COLUMN observed_at    timestamptz,
    ADD COLUMN status_reason  text NOT NULL DEFAULT '',
    ADD CONSTRAINT images_observed_state_known
        CHECK (observed_state IN ('ABSENT','READY','ERROR','UNKNOWN')),
    ADD CONSTRAINT images_status_reason_known
        CHECK (status_reason IN ('','BACKEND_UNAVAILABLE','BACKEND_REJECTED',
                                 'BACKEND_CAPACITY_EXHAUSTED','SOURCE_NOT_READY',
                                 'PRECONDITION_FAILED','INTERNAL_ERROR'));

-- Существующие строки объявлены наблюдёнными: до плоскости данных они и были
-- одновременно намерением и фактом. Выдумкой это не является — объекта у бэкенда
-- у них нет by construction, а нет и самого бэкенда.
UPDATE kacho_storage.volumes   SET observed_state = state WHERE state IN ('READY','ERROR');
UPDATE kacho_storage.snapshots SET observed_state = state WHERE state IN ('READY','ERROR');
UPDATE kacho_storage.images    SET observed_state = state WHERE state IN ('READY','ERROR');

CREATE UNIQUE INDEX volumes_backend_object_uniq
    ON kacho_storage.volumes (backend_object) WHERE backend_object IS NOT NULL;
CREATE UNIQUE INDEX snapshots_backend_object_uniq
    ON kacho_storage.snapshots (backend_object) WHERE backend_object IS NOT NULL;
CREATE UNIQUE INDEX images_backend_object_uniq
    ON kacho_storage.images (backend_object) WHERE backend_object IS NOT NULL;

CREATE INDEX volumes_drift_idx
    ON kacho_storage.volumes (updated_at) WHERE state <> observed_state;
CREATE INDEX snapshots_drift_idx
    ON kacho_storage.snapshots (created_at) WHERE state <> observed_state;
CREATE INDEX images_drift_idx
    ON kacho_storage.images (updated_at) WHERE state <> observed_state;

CREATE INDEX volumes_binding_idx   ON kacho_storage.volumes (binding_id);
CREATE INDEX snapshots_binding_idx ON kacho_storage.snapshots (binding_id);
CREATE INDEX images_binding_idx    ON kacho_storage.images (binding_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.images_binding_idx;
DROP INDEX IF EXISTS kacho_storage.snapshots_binding_idx;
DROP INDEX IF EXISTS kacho_storage.volumes_binding_idx;
DROP INDEX IF EXISTS kacho_storage.images_drift_idx;
DROP INDEX IF EXISTS kacho_storage.snapshots_drift_idx;
DROP INDEX IF EXISTS kacho_storage.volumes_drift_idx;
DROP INDEX IF EXISTS kacho_storage.images_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.snapshots_backend_object_uniq;
DROP INDEX IF EXISTS kacho_storage.volumes_backend_object_uniq;

ALTER TABLE kacho_storage.images
    DROP CONSTRAINT IF EXISTS images_status_reason_known,
    DROP CONSTRAINT IF EXISTS images_observed_state_known,
    DROP COLUMN IF EXISTS status_reason,
    DROP COLUMN IF EXISTS observed_at,
    DROP COLUMN IF EXISTS observed_state,
    DROP COLUMN IF EXISTS backend_object,
    DROP COLUMN IF EXISTS binding_id;

ALTER TABLE kacho_storage.snapshots
    DROP CONSTRAINT IF EXISTS snapshots_status_reason_known,
    DROP CONSTRAINT IF EXISTS snapshots_observed_state_known,
    DROP COLUMN IF EXISTS status_reason,
    DROP COLUMN IF EXISTS observed_at,
    DROP COLUMN IF EXISTS observed_state,
    DROP COLUMN IF EXISTS backend_object,
    DROP COLUMN IF EXISTS binding_id;

ALTER TABLE kacho_storage.volumes
    DROP CONSTRAINT IF EXISTS volumes_status_reason_known,
    DROP CONSTRAINT IF EXISTS volumes_observed_state_known,
    DROP COLUMN IF EXISTS status_reason,
    DROP COLUMN IF EXISTS used_bytes,
    DROP COLUMN IF EXISTS observed_size_bytes,
    DROP COLUMN IF EXISTS observed_at,
    DROP COLUMN IF EXISTS observed_state,
    DROP COLUMN IF EXISTS backend_namespace,
    DROP COLUMN IF EXISTS backend_object,
    DROP COLUMN IF EXISTS desired_binding_id,
    DROP COLUMN IF EXISTS binding_id;
-- +goose StatementEnd
