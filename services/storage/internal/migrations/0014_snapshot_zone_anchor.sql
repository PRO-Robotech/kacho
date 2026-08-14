-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0014: собственный якорь размещения снимка.
--
-- WHY. У снимка своего размещения не было. Зона добиралась через исходный том
-- (snapshots.source_volume_id), а эта ссылка обнуляется при удалении тома — связь
-- объявлена как ON DELETE SET NULL, потому что снимок обязан переживать источник.
-- Следствие: у снимка, чей том удалён, размещения не остаётся ВОВСЕ, и проверка
-- когерентности при засеве тома из такого снимка вырождается в тождественно
-- истинную — сравнивать оказывается не с чем. Она пропускает ровно тот случай,
-- ради которого писалась.
--
-- Пока плоскости данных нет, цена этого — расхождение с объявленным контрактом.
-- С появлением бэкенда цена другая: данные снимка лежат в конкретном пуле
-- конкретного кластера, и восстановление в другую зону — межкластерная копия, а не
-- создание тома. Без собственной зоны снимок нечем адресовать.
--
-- Заполнение существующих строк: зона берётся у исходного тома там, где он ещё
-- жив. Там, где ссылка уже обнулена, зона остаётся ПУСТОЙ — и это честное
-- состояние «размещение неизвестно», а не выдуманное значение. Пустая зона
-- запрещает засев (fail-closed): доказать когерентность нечем, а придумать
-- размещение задним числом нельзя.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.snapshots
    ADD COLUMN zone_id text NOT NULL DEFAULT '';

UPDATE kacho_storage.snapshots s
   SET zone_id = v.zone_id
  FROM kacho_storage.volumes v
 WHERE v.id = s.source_volume_id
   AND s.zone_id = '';

CREATE INDEX snapshots_zone_idx ON kacho_storage.snapshots (zone_id) WHERE zone_id <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP INDEX IF EXISTS kacho_storage.snapshots_zone_idx;
ALTER TABLE kacho_storage.snapshots DROP COLUMN IF EXISTS zone_id;
-- +goose StatementEnd
