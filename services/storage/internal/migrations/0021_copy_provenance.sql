-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0021: происхождение КОПИИ снимка и образа.
--
-- WHY. Зона тома неизменяема, и это правильно: перенос данных между зонами есть
-- межкластерная работа, а не правка поля. Но тогда у арендатора обязан существовать
-- законный путь такого переноса — иначе неизменяемость превращается из решения в
-- тупик. Путь ровно один: скопировать снимок в целевую зону и засеять том оттуда.
--
-- Копия — это НОВЫЙ ресурс, а не правка существующего, поэтому ей нужна собственная
-- координата происхождения. Сегодня снимок помнит только том, с которого снят, а
-- образ — снимок либо том; «снят с другого снимка» и «скопирован с другого образа»
-- выразить нечем.
--
-- Связь — ON DELETE SET NULL, как и прочее происхождение в этой схеме: копия
-- переживает источник. Данные у неё СВОИ с момента материализации, и держать
-- источник живым ради строки-родителя значило бы запретить удаление того, от чего
-- копия давно не зависит.
--
-- Отдельно: колонка нужна не только истории. По ней сверщик отличает, КАКИМ глаголом
-- материализовать снимок — снять с тома либо скопировать с другого снимка. Без неё он
-- звал бы снятие с тома для копии, у которой тома нет вовсе.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.snapshots
    ADD COLUMN source_snapshot_id text
        REFERENCES kacho_storage.snapshots(id) ON DELETE SET NULL;

ALTER TABLE kacho_storage.images
    ADD COLUMN source_image_id text
        REFERENCES kacho_storage.images(id) ON DELETE SET NULL;

-- Снимок снят ЛИБО с тома, ЛИБО с другого снимка — но не с обоих сразу. Ноль
-- источников допустим: строка переживает удаление источника, и запрет «ровно один»
-- ронял бы обнуление связи.
ALTER TABLE kacho_storage.snapshots
    ADD CONSTRAINT snapshots_source_at_most_one
        CHECK (NOT (source_volume_id IS NOT NULL AND source_snapshot_id IS NOT NULL));

CREATE INDEX snapshots_source_snapshot_idx ON kacho_storage.snapshots (source_snapshot_id);
CREATE INDEX images_source_image_idx       ON kacho_storage.images (source_image_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.images_source_image_idx;
DROP INDEX IF EXISTS kacho_storage.snapshots_source_snapshot_idx;
ALTER TABLE IF EXISTS kacho_storage.snapshots DROP CONSTRAINT IF EXISTS snapshots_source_at_most_one;
ALTER TABLE IF EXISTS kacho_storage.images    DROP COLUMN IF EXISTS source_image_id;
ALTER TABLE IF EXISTS kacho_storage.snapshots DROP COLUMN IF EXISTS source_snapshot_id;
-- +goose StatementEnd
